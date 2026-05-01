package blob

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestBackendConformanceWriteStreamAndOpenReadStream(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	payload := []byte("stream-payload-hello")
	sum := sha256.Sum256(payload)
	hashWant := hex.EncodeToString(sum[:])

	tests := []struct {
		name    string
		backend Backend
	}{
		{
			name:    "filesystem",
			backend: NewFilesystemBackend(t.TempDir()),
		},
		{
			name:    "object_store",
			backend: NewObjectStoreBackend(t.TempDir()),
		},
		{
			name:    "s3",
			backend: mustS3Backend(t, newFakeS3Client()),
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			maxBytes := int64(1024)
			hash, n, staged, err := tc.backend.WriteStream(ctx, bytes.NewReader(payload), maxBytes)
			if err != nil {
				t.Fatalf("WriteStream: %v", err)
			}
			if n != int64(len(payload)) {
				t.Fatalf("written: got %d want %d", n, len(payload))
			}
			if hash != hashWant {
				t.Fatalf("hash: got %s want %s", hash, hashWant)
			}
			if err := staged.Promote(); err != nil {
				t.Fatalf("Promote: %v", err)
			}

			rc, size, err := tc.backend.OpenReadStream(ctx, hash)
			if err != nil {
				t.Fatalf("OpenReadStream: %v", err)
			}
			t.Cleanup(func() { _ = rc.Close() })
			if size != int64(len(payload)) {
				t.Fatalf("OpenReadStream size: got %d want %d", size, len(payload))
			}
			got, err := io.ReadAll(rc)
			if err != nil {
				t.Fatalf("ReadAll: %v", err)
			}
			if !bytes.Equal(got, payload) {
				t.Fatalf("payload mismatch")
			}
		})
	}
}

func TestBackendWriteStreamRejectsOverflow(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	b := NewFilesystemBackend(t.TempDir())
	_, _, _, err := b.WriteStream(ctx, strings.NewReader("abcdef"), 3)
	if !errors.Is(err, ErrUploadTooLarge) {
		t.Fatalf("expected ErrUploadTooLarge, got %v", err)
	}
}

func mustS3Backend(t *testing.T, client *fakeS3Client) *S3Backend {
	t.Helper()
	b, err := NewS3BackendWithClient(S3BackendConfig{
		Bucket: "workspace-blobs",
		Prefix: "t",
		Region: "us-east-1",
	}, client)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
