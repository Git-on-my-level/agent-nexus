package blob

import (
	"bytes"
	"context"
	"io"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/s3"
)

func newPreloadTestBackend(t *testing.T) (*S3Backend, *fakeS3Client, *int) {
	t.Helper()
	client := newFakeS3Client()
	puts := 0
	// The hook replaces the fake's own put, so it stores the object itself.
	client.putHook = func(_ context.Context, params *s3.PutObjectInput) error {
		puts++
		if _, exists := client.objects[*params.Key]; exists {
			return fakeS3APIError{code: "PreconditionFailed", message: "already exists"}
		}
		data, err := io.ReadAll(params.Body)
		if err != nil {
			return err
		}
		client.objects[*params.Key] = data
		return nil
	}
	backend, err := NewS3BackendWithClient(S3BackendConfig{Bucket: "blobs", Prefix: "ws", Region: "auto"}, client)
	if err != nil {
		t.Fatal(err)
	}
	return backend, client, &puts
}

func TestS3PreloadThenPromoteOnlyConfirms(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	for name, stage := range map[string]func(*S3Backend) (StagedWrite, error){
		"bytes": func(b *S3Backend) (StagedWrite, error) { return b.Write(ctx, "preload0001", []byte("payload")) },
		"stream": func(b *S3Backend) (StagedWrite, error) {
			_, _, staged, err := b.WriteStream(ctx, bytes.NewReader([]byte("payload")), 1<<20)
			return staged, err
		},
	} {
		t.Run(name, func(t *testing.T) {
			backend, _, puts := newPreloadTestBackend(t)
			staged, err := stage(backend)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = staged.Cleanup() }()
			created, err := Preload(staged)
			if err != nil || !created || *puts != 1 {
				t.Fatalf("preload: created=%v err=%v puts=%d", created, err, *puts)
			}
			if err := staged.Promote(); err != nil {
				t.Fatal(err)
			}
			if *puts != 1 {
				t.Fatalf("promote after preload uploaded again: puts=%d", *puts)
			}
		})
	}
}

func TestS3PromoteRestoresObjectRemovedAfterPreload(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	for name, stage := range map[string]func(*S3Backend) (string, StagedWrite, error){
		"bytes": func(b *S3Backend) (string, StagedWrite, error) {
			staged, err := b.Write(ctx, "preload0002", []byte("payload"))
			return "preload0002", staged, err
		},
		"stream": func(b *S3Backend) (string, StagedWrite, error) {
			hash, _, staged, err := b.WriteStream(ctx, bytes.NewReader([]byte("payload")), 1<<20)
			return hash, staged, err
		},
	} {
		t.Run(name, func(t *testing.T) {
			backend, _, puts := newPreloadTestBackend(t)
			hash, staged, err := stage(backend)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = staged.Cleanup() }()
			if _, err := Preload(staged); err != nil {
				t.Fatal(err)
			}
			if err := backend.Delete(ctx, hash); err != nil {
				t.Fatal(err)
			}
			if err := staged.Promote(); err != nil {
				t.Fatal(err)
			}
			if *puts != 2 {
				t.Fatalf("promote did not restore the removed object: puts=%d", *puts)
			}
			data, err := backend.Read(ctx, hash)
			if err != nil || string(data) != "payload" {
				t.Fatalf("restored object: %q %v", data, err)
			}
		})
	}
}

func TestS3PreloadContentSkipsExistingObject(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	backend, _, puts := newPreloadTestBackend(t)
	created, err := PreloadContent(ctx, backend, "preload0003", []byte("payload"))
	if err != nil || !created || *puts != 1 {
		t.Fatalf("first preload: created=%v err=%v puts=%d", created, err, *puts)
	}
	created, err = PreloadContent(ctx, backend, "preload0003", []byte("payload"))
	if err != nil || created || *puts != 1 {
		t.Fatalf("second preload: created=%v err=%v puts=%d", created, err, *puts)
	}
}

func TestLocalBackendsDoNotPreload(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	backend := NewFilesystemBackend(t.TempDir())
	staged, err := backend.Write(ctx, "local0001", []byte("payload"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = staged.Cleanup() }()
	if created, err := Preload(staged); created || err != nil {
		t.Fatalf("filesystem preload: created=%v err=%v", created, err)
	}
	if exists, _ := backend.Exists(ctx, "local0001"); exists {
		t.Fatal("filesystem preload stored the object before Promote")
	}
	if created, err := PreloadContent(ctx, backend, "local0001", []byte("payload")); created || err != nil {
		t.Fatalf("filesystem content preload: created=%v err=%v", created, err)
	}
}
