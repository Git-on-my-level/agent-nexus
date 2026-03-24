package main

import (
	"context"
	"testing"

	"organization-autorunner-core/internal/blob"
	"organization-autorunner-core/internal/storage"
)

func TestBuildBlobBackendDefaultsToFilesystemWorkspaceContentDir(t *testing.T) {
	t.Parallel()

	layout := storage.NewLayout("/tmp/oar-workspace")
	backend, effectiveRoot, err := buildBlobBackendWithS3Factory(context.Background(), layout, blobBackendConfig{}, nil)
	if err != nil {
		t.Fatalf("buildBlobBackendWithS3Factory: %v", err)
	}

	if _, ok := backend.(*blob.FilesystemBackend); !ok {
		t.Fatalf("expected filesystem backend, got %T", backend)
	}
	if effectiveRoot != layout.ArtifactContentDir {
		t.Fatalf("expected default blob root %q, got %q", layout.ArtifactContentDir, effectiveRoot)
	}
}

func TestBuildBlobBackendSupportsObjectBackend(t *testing.T) {
	t.Parallel()

	layout := storage.NewLayout("/tmp/oar-workspace")
	backend, effectiveRoot, err := buildBlobBackendWithS3Factory(context.Background(), layout, blobBackendConfig{
		Backend: "object",
		Root:    "/var/lib/oar/blobs",
	}, nil)
	if err != nil {
		t.Fatalf("buildBlobBackendWithS3Factory: %v", err)
	}

	if _, ok := backend.(*blob.ObjectStoreBackend); !ok {
		t.Fatalf("expected object backend, got %T", backend)
	}
	if effectiveRoot != "/var/lib/oar/blobs" {
		t.Fatalf("unexpected blob root: %q", effectiveRoot)
	}
}

func TestBuildBlobBackendRejectsUnknownBackend(t *testing.T) {
	t.Parallel()

	layout := storage.NewLayout("/tmp/oar-workspace")
	_, _, err := buildBlobBackendWithS3Factory(context.Background(), layout, blobBackendConfig{
		Backend: "mystery",
	}, nil)
	if err == nil {
		t.Fatal("expected unknown backend error")
	}
}

func TestBuildBlobBackendRejectsInvalidS3Config(t *testing.T) {
	t.Parallel()

	layout := storage.NewLayout("/tmp/oar-workspace")
	_, _, err := buildBlobBackendWithS3Factory(context.Background(), layout, blobBackendConfig{
		Backend: "s3",
		S3: blob.S3BackendConfig{
			Region: "auto",
		},
	}, nil)
	if err == nil {
		t.Fatal("expected S3 validation error")
	}
}

func TestBuildBlobBackendBuildsS3Namespace(t *testing.T) {
	t.Parallel()

	layout := storage.NewLayout("/tmp/oar-workspace")
	expectedConfig := blob.S3BackendConfig{
		Bucket:         "workspace-blobs",
		Prefix:         "workspaces/ws_123",
		Region:         "auto",
		Endpoint:       "https://example.invalid",
		ForcePathStyle: true,
	}

	var receivedConfig blob.S3BackendConfig
	backend, effectiveRoot, err := buildBlobBackendWithS3Factory(context.Background(), layout, blobBackendConfig{
		Backend: "s3",
		Root:    "/should/be/ignored",
		S3:      expectedConfig,
	}, func(ctx context.Context, config blob.S3BackendConfig) (blob.Backend, error) {
		receivedConfig = config
		return blob.NewFilesystemBackend("/unused"), nil
	})
	if err != nil {
		t.Fatalf("buildBlobBackendWithS3Factory: %v", err)
	}

	if backend == nil {
		t.Fatal("expected backend")
	}
	if receivedConfig != expectedConfig.Normalize() {
		t.Fatalf("unexpected S3 config: %#v", receivedConfig)
	}
	if effectiveRoot != "s3://workspace-blobs/workspaces/ws_123" {
		t.Fatalf("unexpected effective root: %q", effectiveRoot)
	}
}
