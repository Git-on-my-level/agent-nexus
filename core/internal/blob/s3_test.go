package blob

import (
	"bytes"
	"context"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
)

func TestS3BackendWriteReadRoundTrip(t *testing.T) {
	t.Parallel()

	client := newFakeS3Client()
	backend, err := NewS3BackendWithClient(S3BackendConfig{
		Bucket: "workspace-blobs",
		Prefix: "workspaces/ws_123",
		Region: "auto",
	}, client)
	if err != nil {
		t.Fatalf("NewS3BackendWithClient: %v", err)
	}

	hash := "abc123def4567890"
	data := []byte("hello, S3")

	staged, err := backend.Write(context.Background(), hash, data)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := staged.Promote(); err != nil {
		t.Fatalf("Promote: %v", err)
	}

	readData, err := backend.Read(context.Background(), hash)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if !bytes.Equal(readData, data) {
		t.Fatalf("Read data mismatch: got %q, want %q", string(readData), string(data))
	}

	wantKey := "workspaces/ws_123/ab/c1/abc123def4567890"
	if _, ok := client.objects[wantKey]; !ok {
		t.Fatalf("expected object at key %q", wantKey)
	}
}

func TestS3BackendPromoteSkipsExistingObject(t *testing.T) {
	t.Parallel()

	client := newFakeS3Client()
	client.objects["prefix/id/em/idempotent1234"] = []byte("original")

	backend, err := NewS3BackendWithClient(S3BackendConfig{
		Bucket: "workspace-blobs",
		Prefix: "prefix",
		Region: "us-east-1",
	}, client)
	if err != nil {
		t.Fatalf("NewS3BackendWithClient: %v", err)
	}

	staged, err := backend.Write(context.Background(), "idempotent1234", []byte("new"))
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := staged.Promote(); err != nil {
		t.Fatalf("Promote: %v", err)
	}

	readData, err := backend.Read(context.Background(), "idempotent1234")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if string(readData) != "original" {
		t.Fatalf("expected original object to remain, got %q", string(readData))
	}
}

func TestS3BackendPromoteAcceptsConditionalConflict(t *testing.T) {
	t.Parallel()

	client := newFakeS3Client()
	client.putHook = func(ctx context.Context, params *s3.PutObjectInput) error {
		data, err := io.ReadAll(params.Body)
		if err != nil {
			return err
		}
		client.objects[*params.Key] = data
		return fakeS3APIError{code: "PreconditionFailed", message: "already exists"}
	}

	backend, err := NewS3BackendWithClient(S3BackendConfig{
		Bucket: "workspace-blobs",
		Prefix: "prefix",
		Region: "us-east-1",
	}, client)
	if err != nil {
		t.Fatalf("NewS3BackendWithClient: %v", err)
	}

	staged, err := backend.Write(context.Background(), "conflict1234", []byte("payload"))
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := staged.Promote(); err != nil {
		t.Fatalf("Promote: %v", err)
	}
}

func TestS3BackendReadNotFound(t *testing.T) {
	t.Parallel()

	backend, err := NewS3BackendWithClient(S3BackendConfig{
		Bucket: "workspace-blobs",
		Region: "us-east-1",
	}, newFakeS3Client())
	if err != nil {
		t.Fatalf("NewS3BackendWithClient: %v", err)
	}

	_, err = backend.Read(context.Background(), "missing")
	if !errors.Is(err, ErrBlobNotFound) {
		t.Fatalf("expected ErrBlobNotFound, got %v", err)
	}
}

func TestS3BackendUsageCountsOnlyConfiguredPrefix(t *testing.T) {
	t.Parallel()

	client := newFakeS3Client()
	client.objects["tenant-a/aa/aa/aaaaaaaa"] = []byte("one")
	client.objects["tenant-a/bb/bb/bbbbbbbb"] = []byte("three")
	client.objects["tenant-b/cc/cc/cccccccc"] = []byte("ignored")

	backend, err := NewS3BackendWithClient(S3BackendConfig{
		Bucket: "workspace-blobs",
		Prefix: "tenant-a",
		Region: "us-east-1",
	}, client)
	if err != nil {
		t.Fatalf("NewS3BackendWithClient: %v", err)
	}

	usage, err := backend.Usage(context.Background())
	if err != nil {
		t.Fatalf("Usage: %v", err)
	}
	if usage.Objects != 2 {
		t.Fatalf("expected 2 objects, got %d", usage.Objects)
	}
	if usage.Bytes != int64(len("one")+len("three")) {
		t.Fatalf("unexpected byte count: %d", usage.Bytes)
	}
}

func TestS3BackendConfigValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		config S3BackendConfig
		wantOK bool
	}{
		{
			name: "minimal valid config",
			config: S3BackendConfig{
				Bucket: "workspace-blobs",
				Region: "auto",
			},
			wantOK: true,
		},
		{
			name: "missing bucket",
			config: S3BackendConfig{
				Region: "auto",
			},
		},
		{
			name: "missing region",
			config: S3BackendConfig{
				Bucket: "workspace-blobs",
			},
		},
		{
			name: "partial credentials",
			config: S3BackendConfig{
				Bucket:      "workspace-blobs",
				Region:      "auto",
				AccessKeyID: "key",
			},
		},
		{
			name: "bad endpoint",
			config: S3BackendConfig{
				Bucket:   "workspace-blobs",
				Region:   "auto",
				Endpoint: "localhost:9000",
			},
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			err := test.config.Validate()
			if test.wantOK && err != nil {
				t.Fatalf("Validate: %v", err)
			}
			if !test.wantOK && err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

type fakeS3Client struct {
	objects map[string][]byte
	putHook func(ctx context.Context, params *s3.PutObjectInput) error
}

func newFakeS3Client() *fakeS3Client {
	return &fakeS3Client{objects: make(map[string][]byte)}
}

func (c *fakeS3Client) PutObject(ctx context.Context, params *s3.PutObjectInput, _ ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if c.putHook != nil {
		if err := c.putHook(ctx, params); err != nil {
			return nil, err
		}
		return &s3.PutObjectOutput{}, nil
	}
	if _, exists := c.objects[*params.Key]; exists {
		return nil, fakeS3APIError{code: "PreconditionFailed", message: "already exists"}
	}

	data, err := io.ReadAll(params.Body)
	if err != nil {
		return nil, err
	}
	c.objects[*params.Key] = data
	return &s3.PutObjectOutput{}, nil
}

func (c *fakeS3Client) GetObject(ctx context.Context, params *s3.GetObjectInput, _ ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	data, ok := c.objects[*params.Key]
	if !ok {
		return nil, fakeS3APIError{code: "NoSuchKey", message: "missing"}
	}
	size := int64(len(data))
	return &s3.GetObjectOutput{
		Body:          io.NopCloser(bytes.NewReader(data)),
		ContentLength: &size,
	}, nil
}

func (c *fakeS3Client) HeadObject(ctx context.Context, params *s3.HeadObjectInput, _ ...func(*s3.Options)) (*s3.HeadObjectOutput, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	data, ok := c.objects[*params.Key]
	if !ok {
		return nil, fakeS3APIError{code: "NotFound", message: "missing"}
	}
	size := int64(len(data))
	return &s3.HeadObjectOutput{ContentLength: &size}, nil
}

func (c *fakeS3Client) ListObjectsV2(ctx context.Context, params *s3.ListObjectsV2Input, _ ...func(*s3.Options)) (*s3.ListObjectsV2Output, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	prefix := ""
	if params.Prefix != nil {
		prefix = *params.Prefix
	}

	keys := make([]string, 0, len(c.objects))
	for key := range c.objects {
		if prefix == "" || strings.HasPrefix(key, prefix) {
			keys = append(keys, key)
		}
	}
	slices.Sort(keys)

	contents := make([]s3types.Object, 0, len(keys))
	for _, key := range keys {
		size := int64(len(c.objects[key]))
		keyCopy := key
		contents = append(contents, s3types.Object{
			Key:  &keyCopy,
			Size: &size,
		})
	}

	isTruncated := false
	return &s3.ListObjectsV2Output{
		Contents:    contents,
		IsTruncated: &isTruncated,
	}, nil
}

type fakeS3APIError struct {
	code    string
	message string
}

func (e fakeS3APIError) Error() string {
	if e.message != "" {
		return e.message
	}
	return e.code
}

func (e fakeS3APIError) ErrorCode() string {
	return e.code
}

func (e fakeS3APIError) ErrorMessage() string {
	return e.message
}

func (e fakeS3APIError) ErrorFault() smithy.ErrorFault {
	return smithy.FaultClient
}
