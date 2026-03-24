package blob

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
	smithyhttp "github.com/aws/smithy-go/transport/http"
)

type S3BackendConfig struct {
	Bucket          string
	Prefix          string
	Region          string
	Endpoint        string
	AccessKeyID     string
	SecretAccessKey string
	SessionToken    string
	ForcePathStyle  bool
}

func (c S3BackendConfig) Normalize() S3BackendConfig {
	c.Bucket = strings.TrimSpace(c.Bucket)
	c.Prefix = strings.Trim(strings.TrimSpace(c.Prefix), "/")
	c.Region = strings.TrimSpace(c.Region)
	c.Endpoint = strings.TrimSpace(c.Endpoint)
	c.AccessKeyID = strings.TrimSpace(c.AccessKeyID)
	c.SecretAccessKey = strings.TrimSpace(c.SecretAccessKey)
	c.SessionToken = strings.TrimSpace(c.SessionToken)
	return c
}

func (c S3BackendConfig) Validate() error {
	c = c.Normalize()

	problems := make([]string, 0, 4)
	if c.Bucket == "" {
		problems = append(problems, "bucket is required")
	}
	if c.Region == "" {
		problems = append(problems, "region is required")
	}
	if (c.AccessKeyID == "") != (c.SecretAccessKey == "") {
		problems = append(problems, "access key ID and secret access key must be provided together")
	}
	if c.SessionToken != "" && c.AccessKeyID == "" {
		problems = append(problems, "session token requires access key ID and secret access key")
	}
	if c.Endpoint != "" {
		parsed, err := url.Parse(c.Endpoint)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" {
			problems = append(problems, "endpoint must be an absolute URL when set")
		}
	}
	if len(problems) > 0 {
		return fmt.Errorf("invalid S3 blob backend config: %s", strings.Join(problems, "; "))
	}

	return nil
}

func (c S3BackendConfig) Namespace() string {
	c = c.Normalize()
	if c.Bucket == "" {
		return "s3://"
	}
	if c.Prefix == "" {
		return "s3://" + c.Bucket
	}
	return "s3://" + c.Bucket + "/" + c.Prefix
}

type s3Client interface {
	PutObject(ctx context.Context, params *s3.PutObjectInput, optFns ...func(*s3.Options)) (*s3.PutObjectOutput, error)
	GetObject(ctx context.Context, params *s3.GetObjectInput, optFns ...func(*s3.Options)) (*s3.GetObjectOutput, error)
	HeadObject(ctx context.Context, params *s3.HeadObjectInput, optFns ...func(*s3.Options)) (*s3.HeadObjectOutput, error)
	ListObjectsV2(ctx context.Context, params *s3.ListObjectsV2Input, optFns ...func(*s3.Options)) (*s3.ListObjectsV2Output, error)
}

type S3Backend struct {
	config S3BackendConfig
	client s3Client
}

func NewS3Backend(ctx context.Context, config S3BackendConfig) (*S3Backend, error) {
	config = config.Normalize()
	if err := config.Validate(); err != nil {
		return nil, err
	}

	loadOptions := []func(*awsconfig.LoadOptions) error{
		awsconfig.WithRegion(config.Region),
	}
	if config.AccessKeyID != "" {
		loadOptions = append(loadOptions, awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(config.AccessKeyID, config.SecretAccessKey, config.SessionToken),
		))
	}

	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, loadOptions...)
	if err != nil {
		return nil, fmt.Errorf("load AWS config: %w", err)
	}

	client := s3.NewFromConfig(awsCfg, func(options *s3.Options) {
		options.UsePathStyle = config.ForcePathStyle
		if config.Endpoint != "" {
			options.BaseEndpoint = &config.Endpoint
		}
	})

	return NewS3BackendWithClient(config, client)
}

func NewS3BackendWithClient(config S3BackendConfig, client s3Client) (*S3Backend, error) {
	config = config.Normalize()
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if client == nil {
		return nil, fmt.Errorf("S3 client is required")
	}
	return &S3Backend{config: config, client: client}, nil
}

func (b *S3Backend) Write(ctx context.Context, hash string, data []byte) (StagedWrite, error) {
	if err := contextOrBackground(ctx).Err(); err != nil {
		return nil, err
	}
	copied := append([]byte(nil), data...)
	return &s3StagedWrite{
		ctx:     contextOrBackground(ctx),
		backend: b,
		hash:    strings.TrimSpace(hash),
		key:     b.objectKey(hash),
		data:    copied,
	}, nil
}

func (b *S3Backend) Read(ctx context.Context, hash string) ([]byte, error) {
	output, err := b.client.GetObject(contextOrBackground(ctx), &s3.GetObjectInput{
		Bucket: &b.config.Bucket,
		Key:    stringPtr(b.objectKey(hash)),
	})
	if err != nil {
		if isS3ObjectNotFound(err) {
			return nil, ErrBlobNotFound
		}
		return nil, fmt.Errorf("get blob %q: %w", strings.TrimSpace(hash), err)
	}
	defer output.Body.Close()

	data, err := io.ReadAll(output.Body)
	if err != nil {
		return nil, fmt.Errorf("read blob %q body: %w", strings.TrimSpace(hash), err)
	}
	return data, nil
}

func (b *S3Backend) Exists(ctx context.Context, hash string) (bool, error) {
	_, err := b.Stat(ctx, hash)
	if errors.Is(err, ErrBlobNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func (b *S3Backend) Stat(ctx context.Context, hash string) (Stat, error) {
	output, err := b.client.HeadObject(contextOrBackground(ctx), &s3.HeadObjectInput{
		Bucket: &b.config.Bucket,
		Key:    stringPtr(b.objectKey(hash)),
	})
	if err != nil {
		if isS3ObjectNotFound(err) {
			return Stat{}, ErrBlobNotFound
		}
		return Stat{}, fmt.Errorf("head blob %q: %w", strings.TrimSpace(hash), err)
	}

	return Stat{Bytes: derefInt64(output.ContentLength)}, nil
}

func (b *S3Backend) Usage(ctx context.Context) (Usage, error) {
	input := &s3.ListObjectsV2Input{
		Bucket: &b.config.Bucket,
	}
	if prefix := b.listPrefix(); prefix != "" {
		input.Prefix = &prefix
	}

	var usage Usage
	for {
		output, err := b.client.ListObjectsV2(contextOrBackground(ctx), input)
		if err != nil {
			return Usage{}, fmt.Errorf("list S3 blob objects: %w", err)
		}
		for _, object := range output.Contents {
			usage.Objects++
			usage.Bytes += derefInt64(object.Size)
		}
		if !boolPtrValue(output.IsTruncated) || output.NextContinuationToken == nil || *output.NextContinuationToken == "" {
			break
		}
		input.ContinuationToken = output.NextContinuationToken
	}

	return usage, nil
}

func (b *S3Backend) objectKey(hash string) string {
	relative := contentAddressedRelativePath(hash)
	if b.config.Prefix == "" {
		return relative
	}
	if relative == "" {
		return b.config.Prefix
	}
	return path.Join(b.config.Prefix, relative)
}

func (b *S3Backend) listPrefix() string {
	if b.config.Prefix == "" {
		return ""
	}
	return b.config.Prefix + "/"
}

type s3StagedWrite struct {
	ctx      context.Context
	backend  *S3Backend
	hash     string
	key      string
	data     []byte
	promoted bool
}

func (w *s3StagedWrite) Promote() error {
	if w == nil || w.promoted {
		return nil
	}
	if w.backend == nil {
		return fmt.Errorf("S3 blob staged write backend is not configured")
	}
	if w.data == nil {
		w.promoted = true
		return nil
	}

	ctx := contextOrBackground(w.ctx)
	_, err := w.backend.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:        &w.backend.config.Bucket,
		Key:           stringPtr(w.key),
		Body:          bytes.NewReader(w.data),
		ContentLength: int64Ptr(int64(len(w.data))),
		IfNoneMatch:   stringPtr("*"),
	})
	if err == nil {
		w.data = nil
		w.promoted = true
		return nil
	}

	if !isS3ConditionalWriteFailure(err) {
		exists, statErr := w.backend.Exists(ctx, w.hash)
		if statErr == nil && exists {
			w.data = nil
			w.promoted = true
			return nil
		}
		if statErr != nil && !errors.Is(statErr, ErrBlobNotFound) {
			return fmt.Errorf("put blob %q: %v (follow-up head failed: %w)", w.hash, err, statErr)
		}
		return fmt.Errorf("put blob %q: %w", w.hash, err)
	}

	exists, statErr := w.backend.Exists(ctx, w.hash)
	if statErr != nil {
		return fmt.Errorf("put blob %q: %v (follow-up head failed: %w)", w.hash, err, statErr)
	}
	if !exists {
		return fmt.Errorf("put blob %q: %w", w.hash, err)
	}

	w.data = nil
	w.promoted = true
	return nil
}

func (w *s3StagedWrite) Cleanup() error {
	if w == nil {
		return nil
	}
	w.data = nil
	return nil
}

func contextOrBackground(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

func isS3ObjectNotFound(err error) bool {
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		switch apiErr.ErrorCode() {
		case "NoSuchKey", "NotFound", "404":
			return true
		case "NoSuchBucket":
			return false
		}
	}

	var responseErr *smithyhttp.ResponseError
	if errors.As(err, &responseErr) && responseErr.HTTPStatusCode() == http.StatusNotFound {
		return true
	}

	return false
}

func isS3ConditionalWriteFailure(err error) bool {
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		switch apiErr.ErrorCode() {
		case "PreconditionFailed", "ConditionalRequestConflict":
			return true
		}
	}

	var responseErr *smithyhttp.ResponseError
	if errors.As(err, &responseErr) {
		switch responseErr.HTTPStatusCode() {
		case http.StatusConflict, http.StatusPreconditionFailed:
			return true
		}
	}

	return false
}

func stringPtr(value string) *string {
	return &value
}

func int64Ptr(value int64) *int64 {
	return &value
}

func derefInt64(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}

func boolPtrValue(value *bool) bool {
	return value != nil && *value
}
