package blob

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
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

func (b *FilesystemBackend) Usage(ctx context.Context) (Usage, error) {
	_ = ctx
	return usageForRoot(b.rootDir, ".cas-")
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

type ObjectStoreBackend struct {
	rootDir string
}

func NewObjectStoreBackend(rootDir string) *ObjectStoreBackend {
	return &ObjectStoreBackend{rootDir: rootDir}
}

func (b *ObjectStoreBackend) Write(ctx context.Context, hash string, data []byte) (StagedWrite, error) {
	_ = ctx
	if err := os.MkdirAll(b.rootDir, 0o755); err != nil {
		return nil, err
	}

	finalPath := b.objectPath(hash)
	file, err := os.CreateTemp(b.rootDir, ".obj-*")
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

	return &objectStoreStagedWrite{tempPath: tempPath, finalPath: finalPath}, nil
}

func (b *ObjectStoreBackend) Read(ctx context.Context, hash string) ([]byte, error) {
	_ = ctx
	path := b.objectPath(hash)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, ErrBlobNotFound
		}
		return nil, err
	}
	return data, nil
}

func (b *ObjectStoreBackend) Exists(ctx context.Context, hash string) (bool, error) {
	_ = ctx
	path := b.objectPath(hash)
	_, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (b *ObjectStoreBackend) Usage(ctx context.Context) (Usage, error) {
	_ = ctx
	return usageForRoot(b.rootDir, ".obj-")
}

func (b *ObjectStoreBackend) objectPath(hash string) string {
	hash = strings.TrimSpace(hash)
	if len(hash) < 4 {
		return filepath.Join(b.rootDir, hash)
	}
	return filepath.Join(b.rootDir, hash[:2], hash[2:4], hash)
}

type objectStoreStagedWrite struct {
	tempPath  string
	finalPath string
}

func (w *objectStoreStagedWrite) Promote() error {
	if w.tempPath == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(w.finalPath), 0o755); err != nil {
		return err
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

func (w *objectStoreStagedWrite) Cleanup() error {
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
