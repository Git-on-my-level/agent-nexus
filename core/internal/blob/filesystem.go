package blob

import (
	"context"
	"errors"
	"os"
	"path/filepath"
)

type FilesystemBackend struct {
	rootDir string
}

func NewFilesystemBackend(rootDir string) *FilesystemBackend {
	return &FilesystemBackend{rootDir: rootDir}
}

func (b *FilesystemBackend) Write(ctx context.Context, hash string, data []byte) (StagedWrite, error) {
	if err := os.MkdirAll(b.rootDir, 0o755); err != nil {
		return nil, err
	}

	finalPath := b.blobPath(hash)
	file, err := os.CreateTemp(b.rootDir, ".cas-*")
	if err != nil {
		return nil, err
	}
	tempPath := file.Name()

	cleanup := func() {
		_ = file.Close()
		_ = os.Remove(tempPath)
	}

	if _, err := file.Write(data); err != nil {
		cleanup()
		return nil, err
	}
	if err := file.Chmod(0o644); err != nil {
		cleanup()
		return nil, err
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(tempPath)
		return nil, err
	}

	return &filesystemStagedWrite{tempPath: tempPath, finalPath: finalPath}, nil
}

func (b *FilesystemBackend) Read(ctx context.Context, hash string) ([]byte, error) {
	path := b.blobPath(hash)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, ErrBlobNotFound
		}
		return nil, err
	}
	return data, nil
}

func (b *FilesystemBackend) Exists(ctx context.Context, hash string) (bool, error) {
	path := b.blobPath(hash)
	_, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (b *FilesystemBackend) blobPath(hash string) string {
	return filepath.Join(b.rootDir, hash)
}

type filesystemStagedWrite struct {
	tempPath  string
	finalPath string
}

func (w *filesystemStagedWrite) Promote() error {
	if w.tempPath == "" {
		return nil
	}
	if _, err := os.Stat(w.finalPath); err == nil {
		return w.Cleanup()
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Rename(w.tempPath, w.finalPath); err != nil {
		if _, statErr := os.Stat(w.finalPath); statErr == nil {
			return w.Cleanup()
		}
		return err
	}
	w.tempPath = ""
	return nil
}

func (w *filesystemStagedWrite) Cleanup() error {
	if w.tempPath == "" {
		return nil
	}
	err := os.Remove(w.tempPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	w.tempPath = ""
	return nil
}
