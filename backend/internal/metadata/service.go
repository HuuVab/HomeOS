package metadata

import (
	"context"
	"errors"
	"os"
	"path"

	"github.com/HuuVab/HomeOS/backend/internal/drive"
	"github.com/HuuVab/HomeOS/backend/internal/metadatapb"
	"github.com/HuuVab/HomeOS/backend/internal/store"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Service is intended for an in-process gRPC transport, not a public listener.
// Its callers must authenticate the owner before supplying owner_id.
type Service struct {
	metadatapb.UnimplementedMetadataServiceServer
	DB    *store.Store
	Files *drive.Store
}

func (s *Service) ready(ownerID int64) error {
	if ownerID < 1 {
		return status.Error(codes.InvalidArgument, "owner_id must be positive")
	}
	if !s.DB.AuthoritativeMetadata() {
		return status.Error(codes.FailedPrecondition, "PostgreSQL metadata mode is required")
	}
	return nil
}

func fileEntry(entry drive.Entry) *metadatapb.FileEntry {
	return &metadatapb.FileEntry{
		Name: entry.Name, Path: entry.Path, Kind: entry.Kind,
		Size: entry.Size, ModifiedAtUnix: entry.ModifiedAt.Unix(), Version: entry.Version,
	}
}

func metadataError(err error) error {
	switch {
	case errors.Is(err, os.ErrNotExist):
		return status.Error(codes.NotFound, "file or folder not found")
	case errors.Is(err, os.ErrExist):
		return status.Error(codes.AlreadyExists, "file or folder already exists")
	case errors.Is(err, drive.ErrInvalidPath):
		return status.Error(codes.InvalidArgument, "invalid drive path")
	default:
		return status.Error(codes.Unavailable, "HomeDrive metadata is unavailable")
	}
}

func (s *Service) GetFile(_ context.Context, request *metadatapb.GetFileRequest) (*metadatapb.FileEntry, error) {
	if err := s.ready(request.GetOwnerId()); err != nil {
		return nil, err
	}
	entry, err := s.DB.FileEntry(request.GetOwnerId(), request.GetPath())
	if err != nil {
		return nil, metadataError(err)
	}
	return fileEntry(entry), nil
}

func (s *Service) ListFiles(_ context.Context, request *metadatapb.ListFilesRequest) (*metadatapb.ListFilesResponse, error) {
	if err := s.ready(request.GetOwnerId()); err != nil {
		return nil, err
	}
	entries, err := s.DB.FileEntries(request.GetOwnerId(), request.GetFolder())
	if err != nil {
		return nil, metadataError(err)
	}
	result := &metadatapb.ListFilesResponse{Entries: make([]*metadatapb.FileEntry, 0, len(entries))}
	for _, entry := range entries {
		result.Entries = append(result.Entries, fileEntry(entry))
	}
	return result, nil
}

func (s *Service) CreateFolder(_ context.Context, request *metadatapb.CreateFolderRequest) (*metadatapb.FileEntry, error) {
	ownerID := request.GetOwnerId()
	if err := s.ready(ownerID); err != nil {
		return nil, err
	}
	name := request.GetPath()
	s.DB.LockDriveMutations()
	defer s.DB.UnlockDriveMutations()
	files, err := s.Files.ForUser(ownerID)
	if err != nil {
		return nil, metadataError(err)
	}
	before, err := files.AllEntries()
	if err != nil {
		return nil, metadataError(err)
	}
	matched, err := s.DB.MetadataMatchesSnapshot(ownerID, before)
	if err != nil {
		return nil, metadataError(err)
	}
	if !matched {
		return nil, status.Error(codes.Aborted, "HomeDrive files differ from committed metadata")
	}
	trash, err := files.ListTrash()
	if err != nil {
		return nil, metadataError(err)
	}
	matched, err = s.DB.TrashMatchesSnapshot(ownerID, trash)
	if err != nil {
		return nil, metadataError(err)
	}
	if !matched {
		return nil, status.Error(codes.Aborted, "HomeDrive Trash differs from committed metadata")
	}
	if _, err := s.DB.FileEntry(ownerID, name); err == nil {
		return nil, status.Error(codes.AlreadyExists, "folder already exists")
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, metadataError(err)
	}
	if parent := path.Dir(name); parent != "." {
		entry, err := s.DB.FileEntry(ownerID, parent)
		if err != nil {
			return nil, metadataError(err)
		}
		if entry.Kind != "folder" {
			return nil, status.Error(codes.InvalidArgument, "parent is not a folder")
		}
	}
	if err := s.DB.BeginFileMutation(ownerID); err != nil {
		return nil, metadataError(err)
	}
	if err := files.CreateFolder(name); err != nil {
		if cancelErr := s.DB.CancelFileMutation(ownerID); cancelErr != nil {
			return nil, metadataError(cancelErr)
		}
		return nil, metadataError(err)
	}
	after, err := files.AllEntries()
	if err == nil {
		err = s.DB.ReconcileDriveMutation(ownerID, after, trash, files.PendingUploads())
	}
	if err != nil {
		return nil, metadataError(err) // Recovery marker remains durable.
	}
	entry, err := s.DB.FileEntry(ownerID, name)
	if err != nil {
		return nil, metadataError(err)
	}
	return fileEntry(entry), nil
}
