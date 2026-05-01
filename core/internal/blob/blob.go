package blob

import (
	"context"
	"errors"
	"io"
)

var (
	ErrBlobNotFound    = errors.New("blob not found")
	ErrUploadTooLarge  = errors.New("upload exceeds maximum allowed size")
)

type StagedWrite interface {
	Promote() error
	Cleanup() error
}

type Backend interface {
	Write(ctx context.Context, hash string, data []byte) (StagedWrite, error)
	// WriteStream reads src until EOF, hashing bytes for content addressing. At most maxBytes
	// are accepted (exclusive); reading beyond that returns ErrUploadTooLarge.
	WriteStream(ctx context.Context, src io.Reader, maxBytes int64) (hash string, written int64, staged StagedWrite, err error)
	Read(ctx context.Context, hash string) ([]byte, error)
	OpenReadStream(ctx context.Context, hash string) (io.ReadCloser, int64, error)
	Exists(ctx context.Context, hash string) (bool, error)
	Stat(ctx context.Context, hash string) (Stat, error)
	Usage(ctx context.Context) (Usage, error)
	Delete(ctx context.Context, hash string) error
}

type Stat struct {
	Bytes int64
}

type Usage struct {
	Bytes   int64
	Objects int64
}
