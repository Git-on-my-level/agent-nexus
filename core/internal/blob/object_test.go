package blob

import (
	"context"
	"testing"
)

func TestObjectStoreBackendWriteReadRoundTrip(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	backend := NewObjectStoreBackend(root)

	hash := "abc123def4567890"
	data := []byte("hello, object store")

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

func TestObjectStoreBackendUsageCountsObjectsAndBytes(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	backend := NewObjectStoreBackend(root)

	first := []byte("one")
	second := []byte("two-two")

	staged, err := backend.Write(ctx, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", first)
	if err != nil {
		t.Fatalf("Write first: %v", err)
	}
	if err := staged.Promote(); err != nil {
		t.Fatalf("Promote first: %v", err)
	}

	staged, err = backend.Write(ctx, "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", second)
	if err != nil {
		t.Fatalf("Write second: %v", err)
	}
	if err := staged.Promote(); err != nil {
		t.Fatalf("Promote second: %v", err)
	}

	usage, err := backend.Usage(ctx)
	if err != nil {
		t.Fatalf("Usage: %v", err)
	}
	if usage.Objects != 2 {
		t.Fatalf("expected 2 objects, got %d", usage.Objects)
	}
	if usage.Bytes != int64(len(first)+len(second)) {
		t.Fatalf("expected %d bytes, got %d", len(first)+len(second), usage.Bytes)
	}
}
