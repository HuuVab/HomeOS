package store

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"os"
	"strings"
	"time"

	"github.com/HuuVab/HomeOS/backend/internal/drive"
)

type catalogNode struct {
	path, kind, checksum      string
	size, modifiedAt, version int64
}

func (s *Store) catalogSubtreeTx(tx *sql.Tx, ownerID int64, root string) ([]catalogNode, error) {
	rows, err := tx.Query(`SELECT path,kind,size,modified_at,version,COALESCE(checksum,'') FROM file_entries
		WHERE owner_id=$1 AND (path=$2 OR left(path,length($2)+1)=$2||'/') ORDER BY length(path),path`, ownerID, root)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var nodes []catalogNode
	for rows.Next() {
		var node catalogNode
		if err := rows.Scan(&node.path, &node.kind, &node.size, &node.modifiedAt, &node.version, &node.checksum); err != nil {
			return nil, err
		}
		nodes = append(nodes, node)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(nodes) == 0 {
		return nil, os.ErrNotExist
	}
	return nodes, nil
}

func (s *Store) catalogDestinationTx(tx *sql.Tx, ownerID int64, name string) error {
	if err := s.catalogParentTx(tx, ownerID, name); err != nil {
		return err
	}
	var exists int
	err := tx.QueryRow(`SELECT 1 FROM file_entries WHERE owner_id=$1 AND path=$2`, ownerID, name).Scan(&exists)
	if err == nil {
		return os.ErrExist
	}
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	return err
}

func catalogMappedPath(root, destination, old string) string {
	return destination + strings.TrimPrefix(old, root)
}

func (s *Store) CatalogMoveOrCopy(ownerID int64, from, to string, copyItem bool, expectedVersion int64) error {
	if !drive.ValidCatalogPath(from) || !drive.ValidCatalogPath(to) || from == to || strings.HasPrefix(to, from+"/") || expectedVersion < 1 {
		return drive.ErrInvalidPath
	}
	tx, err := s.catalogTx(ownerID)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	nodes, err := s.catalogSubtreeTx(tx, ownerID, from)
	if err != nil {
		return err
	}
	if nodes[0].version != expectedVersion {
		return ErrCatalogRevision
	}
	if err := s.catalogDestinationTx(tx, ownerID, to); err != nil {
		return err
	}
	for _, node := range nodes {
		mapped := catalogMappedPath(from, to, node.path)
		var exists int
		err := tx.QueryRow(`SELECT 1 FROM file_entries WHERE owner_id=$1 AND path=$2`, ownerID, mapped).Scan(&exists)
		if err == nil {
			return os.ErrExist
		}
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
	}
	now := time.Now().Unix()
	if !copyItem {
		for i := len(nodes) - 1; i >= 0; i-- {
			if _, err := tx.Exec(`DELETE FROM file_entries WHERE owner_id=$1 AND path=$2`, ownerID, nodes[i].path); err != nil {
				return err
			}
			if _, err := tx.Exec(`INSERT INTO change_events(owner_id,path,operation,occurred_at) VALUES($1,$2,'delete',$3)`, ownerID, nodes[i].path, now); err != nil {
				return err
			}
		}
	}
	for _, node := range nodes {
		mapped := catalogMappedPath(from, to, node.path)
		var last int64
		if node.kind == "file" {
			if err := tx.QueryRow(`SELECT COALESCE(MAX(version),0) FROM file_versions WHERE owner_id=$1 AND path=$2`, ownerID, mapped).Scan(&last); err != nil {
				return err
			}
		}
		version := last + 1
		if _, err := tx.Exec(`INSERT INTO file_entries(owner_id,path,kind,size,modified_at,version,checksum)
			VALUES($1,$2,$3,$4,$5,$6,NULLIF($7,''))`, ownerID, mapped, node.kind, node.size, now, version, node.checksum); err != nil {
			return err
		}
		if node.kind == "file" {
			if _, err := tx.Exec(`INSERT INTO file_versions(owner_id,path,version,size,checksum,storage_path,created_at)
				VALUES($1,$2,$3,$4,$5,$6,$7)`, ownerID, mapped, version, node.size, node.checksum, ".homeos-versions/"+node.checksum, now); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(`INSERT INTO change_events(owner_id,path,operation,version,occurred_at)
			VALUES($1,$2,'create',$3,$4)`, ownerID, mapped, version, now); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) CatalogTrash(ownerID int64, name string, expectedVersion int64) error {
	if !drive.ValidCatalogPath(name) || expectedVersion < 1 {
		return drive.ErrInvalidPath
	}
	tx, err := s.catalogTx(ownerID)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	nodes, err := s.catalogSubtreeTx(tx, ownerID, name)
	if err != nil {
		return err
	}
	if nodes[0].version != expectedVersion {
		return ErrCatalogRevision
	}
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return err
	}
	id := hex.EncodeToString(random[:])
	now := time.Now().Unix()
	if _, err := tx.Exec(`INSERT INTO trash_entries(id,owner_id,path,deleted_at) VALUES($1,$2,$3,$4)`, id, ownerID, name, now); err != nil {
		return err
	}
	for i := len(nodes) - 1; i >= 0; i-- {
		node := nodes[i]
		if _, err := tx.Exec(`INSERT INTO trash_blobs(owner_id,trash_id,path,kind,size,modified_at,version,checksum)
			VALUES($1,$2,$3,$4,$5,$6,$7,NULLIF($8,''))`, ownerID, id, node.path, node.kind, node.size, node.modifiedAt, node.version, node.checksum); err != nil {
			return err
		}
		if _, err := tx.Exec(`DELETE FROM file_entries WHERE owner_id=$1 AND path=$2`, ownerID, node.path); err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO change_events(owner_id,path,operation,occurred_at) VALUES($1,$2,'delete',$3)`, ownerID, node.path, now); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) CatalogDeleteForever(ownerID int64, id string) error {
	if len(id) != 32 {
		return drive.ErrInvalidPath
	}
	if _, err := hex.DecodeString(id); err != nil {
		return drive.ErrInvalidPath
	}
	tx, err := s.catalogTx(ownerID)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.Exec(`DELETE FROM trash_entries WHERE owner_id=$1 AND id=$2`, ownerID, id)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return os.ErrNotExist
	}
	return tx.Commit()
}
