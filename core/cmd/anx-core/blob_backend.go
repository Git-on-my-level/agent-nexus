package main

import (
	"context"
	"fmt"
	"strings"

	"agent-nexus-core/internal/blob"
	"agent-nexus-core/internal/storage"
)

type blobBackendConfig struct {
	Backend string
	Root    string
	S3      blob.S3BackendConfig
}

func (c blobBackendConfig) Normalize() blobBackendConfig {
	c.Backend = strings.TrimSpace(c.Backend)
	c.Root = strings.TrimSpace(c.Root)
	c.S3 = c.S3.Normalize()
	if c.Backend == "" {
		c.Backend = "filesystem"
	}
	return c
}

func buildBlobBackend(ctx context.Context, layout storage.Layout, config blobBackendConfig) (blob.Backend, string, error) {
	return buildBlobBackendWithS3Factory(ctx, layout, config, func(ctx context.Context, config blob.S3BackendConfig) (blob.Backend, error) {
		return blob.NewS3Backend(ctx, config)
	})
}

func buildBlobBackendWithS3Factory(
	ctx context.Context,
	layout storage.Layout,
	config blobBackendConfig,
	newS3Backend func(context.Context, blob.S3BackendConfig) (blob.Backend, error),
) (blob.Backend, string, error) {
	config = config.Normalize()

	effectiveBlobRoot := config.Root
	if effectiveBlobRoot == "" {
		effectiveBlobRoot = layout.ArtifactContentDir
	}

	switch config.Backend {
	case "filesystem":
		return blob.NewFilesystemBackend(effectiveBlobRoot), effectiveBlobRoot, nil
	case "object":
		return blob.NewObjectStoreBackend(effectiveBlobRoot), effectiveBlobRoot, nil
	case "s3":
		if err := config.S3.Validate(); err != nil {
			return nil, "", err
		}
		if newS3Backend == nil {
			return nil, "", fmt.Errorf("S3 blob backend factory is not configured")
		}
		backend, err := newS3Backend(ctx, config.S3)
		if err != nil {
			return nil, "", err
		}
		return backend, config.S3.Namespace(), nil
	default:
		return nil, "", fmt.Errorf("unknown blob backend: %s (supported: filesystem, object, s3)", config.Backend)
	}
}
