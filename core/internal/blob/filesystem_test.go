package blob

import (
	"context"
	"os"
	"testing"
)

func TestFilesystemBackendWriteReadRoundTrip(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	backend := NewFilesystemBackend(root)

	hash := "abc123def456"
	data := []byte("hello, world")

	staged, err := backend.Write(ctx, hash, data)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}

	if err := staged.Promote(); err != nil {
		t.Fatalf("Promote: %v", err)
	}

	readData, err := backend.Read(ctx, hash)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}

	if string(readData) != string(data) {
		t.Fatalf("Read data mismatch: got %q, want %q", string(readData), string(data))
	}
}

func TestFilesystemBackendExists(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	backend := NewFilesystemBackend(root)

	hash := "exists123"
	data := []byte("test data")

	exists, err := backend.Exists(ctx, hash)
	if err != nil {
		t.Fatalf("Exists (before write): %v", err)
	}
	if exists {
		t.Fatal("Expected blob to not exist before write")
	}

	staged, err := backend.Write(ctx, hash, data)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := staged.Promote(); err != nil {
		t.Fatalf("Promote: %v", err)
	}

	exists, err = backend.Exists(ctx, hash)
	if err != nil {
		t.Fatalf("Exists (after write): %v", err)
	}
	if !exists {
		t.Fatal("Expected blob to exist after write")
	}
}

func TestFilesystemBackendReadNotFound(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	backend := NewFilesystemBackend(root)

	_, err := backend.Read(ctx, "nonexistent")
	if err == nil {
		t.Fatal("Expected error reading nonexistent blob")
	}
	if err != ErrBlobNotFound {
		t.Fatalf("Expected ErrBlobNotFound, got: %v", err)
	}
}

func TestFilesystemBackendWriteIdempotent(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	backend := NewFilesystemBackend(root)

	hash := "idempotent123"
	data := []byte("same data")

	staged1, err := backend.Write(ctx, hash, data)
	if err != nil {
		t.Fatalf("Write 1: %v", err)
	}
	if err := staged1.Promote(); err != nil {
		t.Fatalf("Promote 1: %v", err)
	}

	staged2, err := backend.Write(ctx, hash, data)
	if err != nil {
		t.Fatalf("Write 2: %v", err)
	}
	if err := staged2.Promote(); err != nil {
		t.Fatalf("Promote 2: %v", err)
	}

	readData, err := backend.Read(ctx, hash)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if string(readData) != string(data) {
		t.Fatalf("Read data mismatch after idempotent writes")
	}
}

func TestFilesystemBackendCleanup(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	backend := NewFilesystemBackend(root)

	hash := "cleanup123"
	data := []byte("cleanup data")

	staged, err := backend.Write(ctx, hash, data)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}

	tempPath := staged.(*filesystemStagedWrite).tempPath
	if tempPath == "" {
		t.Fatal("Expected tempPath to be set before cleanup")
	}

	if err := staged.Cleanup(); err != nil {
		t.Fatalf("Cleanup: %v", err)
	}

	if _, err := os.Stat(tempPath); !os.IsNotExist(err) {
		t.Fatal("Expected temp file to be removed after cleanup")
	}

	exists, err := backend.Exists(ctx, hash)
	if err != nil {
		t.Fatalf("Exists after cleanup: %v", err)
	}
	if exists {
		t.Fatal("Expected blob to not exist after cleanup without promote")
	}
}

func TestFilesystemBackendPromoteSkipsExisting(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	backend := NewFilesystemBackend(root)

	hash := "existing123"
	originalData := []byte("original")

	staged, err := backend.Write(ctx, hash, originalData)
	if err != nil {
		t.Fatalf("Write original: %v", err)
	}
	if err := staged.Promote(); err != nil {
		t.Fatalf("Promote original: %v", err)
	}

	newData := []byte("new data")
	staged2, err := backend.Write(ctx, hash, newData)
	if err != nil {
		t.Fatalf("Write new: %v", err)
	}
	if err := staged2.Promote(); err != nil {
		t.Fatalf("Promote new (should skip): %v", err)
	}

	readData, err := backend.Read(ctx, hash)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}

	if string(readData) != string(originalData) {
		t.Fatalf("Data was overwritten: got %q, want %q", string(readData), string(originalData))
	}
}
