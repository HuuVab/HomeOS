package store

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/HuuVab/HomeOS/backend/internal/drive"
)

type BlobMigrationReport struct {
	ActiveEntries int
	TrashEntries  int
	TrashNodes    int
	VerifiedBlobs int
}

// MigrateOwnerToBlobs verifies a stopped owner's legacy catalog and bytes,
// then publishes the Trash manifests and storage-format switch in one commit.
// It never deletes the legacy live tree; removal is a separate post-cutover step.
func (s *Store) MigrateOwnerToBlobs(ownerID int64, files *drive.Store, apply bool) (BlobMigrationReport, error) {
	var report BlobMigrationReport
	if !s.AuthoritativeMetadata() || ownerID < 1 {
		return report, drive.ErrInvalidPath
	}
	format, err := s.StorageFormat(ownerID)
	if err != nil {
		return report, err
	}
	if format != "legacy" {
		return report, errors.New("owner is already using blob storage")
	}
	pending, err := s.HasPendingFileMutation(ownerID)
	if err != nil {
		return report, err
	}
	if pending {
		return report, errors.New("legacy filesystem mutation recovery is pending")
	}
	var activeUploads int
	if err := s.db.QueryRow(`SELECT count(*) FROM upload_sessions WHERE owner_id=$1 AND status='pending'`, ownerID).Scan(&activeUploads); err != nil {
		return report, err
	}
	if activeUploads != 0 || len(files.PendingUploads()) != 0 {
		return report, errors.New("finish or cancel pending uploads before migration")
	}
	active, err := files.AllEntries()
	if err != nil {
		return report, err
	}
	report.ActiveEntries = len(active)
	matches, err := s.MetadataMatchesSnapshot(ownerID, active)
	if err != nil {
		return report, err
	}
	if !matches {
		return report, errors.New("active filesystem differs from PostgreSQL catalog")
	}
	trash, err := files.ListTrash()
	if err != nil {
		return report, err
	}
	report.TrashEntries = len(trash)
	matches, err = s.TrashMatchesSnapshot(ownerID, trash)
	if err != nil {
		return report, err
	}
	if !matches {
		return report, errors.New("Trash filesystem differs from PostgreSQL catalog")
	}
	verified := make(map[string]int64)
	for _, entry := range active {
		if !drive.ValidCatalogPath(entry.Path) {
			return report, drive.ErrInvalidPath
		}
		if entry.Kind != "file" {
			continue
		}
		committed, err := s.FileEntry(ownerID, entry.Path)
		if err != nil {
			return report, err
		}
		if committed.Checksum == "" || committed.Checksum != entry.Checksum || committed.Size != entry.Size {
			return report, fmt.Errorf("missing or mismatched committed checksum for %s", entry.Path)
		}
		verified[entry.Checksum] = entry.Size
	}
	trashNodes := make(map[string][]drive.Entry)
	for _, item := range trash {
		nodes, err := files.SnapshotTrashItem(item)
		if err != nil {
			return report, err
		}
		report.TrashNodes += len(nodes)
		trashNodes[item.ID] = nodes
		for _, node := range nodes {
			if node.Kind == "file" {
				verified[node.Checksum] = node.Size
			}
		}
	}
	rows, err := s.db.Query(`SELECT checksum,size FROM file_versions WHERE owner_id=$1 AND checksum IS NOT NULL`, ownerID)
	if err != nil {
		return report, err
	}
	for rows.Next() {
		var checksum string
		var size int64
		if err := rows.Scan(&checksum, &size); err != nil {
			rows.Close()
			return report, err
		}
		if old, exists := verified[checksum]; exists && old != size {
			rows.Close()
			return report, errors.New("version blob has inconsistent sizes")
		}
		verified[checksum] = size
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return report, err
	}
	rows.Close()
	for checksum, size := range verified {
		file, err := files.OpenBlob(checksum, size)
		if err != nil {
			return report, err
		}
		hash := sha256.New()
		copied, copyErr := io.Copy(hash, file)
		closeErr := file.Close()
		if copyErr != nil {
			return report, copyErr
		}
		if closeErr != nil {
			return report, closeErr
		}
		if copied != size || hex.EncodeToString(hash.Sum(nil)) != checksum {
			return report, fmt.Errorf("blob checksum mismatch: %s", checksum)
		}
	}
	report.VerifiedBlobs = len(verified)
	if !apply {
		return report, nil
	}
	tx, err := s.catalogTx(ownerID)
	if err != nil {
		return report, err
	}
	defer tx.Rollback()
	var current string
	err = tx.QueryRow(`SELECT format FROM owner_storage WHERE owner_id=$1`, ownerID).Scan(&current)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return report, err
	}
	if err == nil && current != "legacy" {
		return report, errors.New("storage format changed during migration")
	}
	for id, nodes := range trashNodes {
		for _, node := range nodes {
			if _, err := tx.Exec(`INSERT INTO trash_blobs(owner_id,trash_id,path,kind,size,modified_at,version,checksum)
				VALUES($1,$2,$3,$4,$5,$6,$7,NULLIF($8,''))`, ownerID, id, node.Path, node.Kind, node.Size, node.ModifiedAt.Unix(), node.Version, node.Checksum); err != nil {
				return report, err
			}
		}
	}
	if _, err := tx.Exec(`INSERT INTO owner_storage(owner_id,format,migrated_at) VALUES($1,'blobs',$2)
		ON CONFLICT(owner_id) DO UPDATE SET format='blobs',migrated_at=EXCLUDED.migrated_at`, ownerID, time.Now().Unix()); err != nil {
		return report, err
	}
	if err := tx.Commit(); err != nil {
		return report, err
	}
	return report, nil
}
