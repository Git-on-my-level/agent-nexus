package blob

import (
	"context"
	"errors"
	"io"
)

var (
	ErrBlobNotFound   = errors.New("blob not found")
	ErrUploadTooLarge = errors.New("upload exceeds maximum allowed size")
)

type StagedWrite interface {
	Promote() error
	Cleanup() error
}

// Preloader is implemented by staged writes whose Promote would otherwise do
// slow remote I/O. Preload stores the object before the caller's write
// transaction and keeps the staged copy, so the Promote inside the transaction
// only confirms the object and restores it if a collector removed it in
// between. It reports whether this call created the object.
type Preloader interface {
	Preload() (created bool, err error)
}

// Preload preloads w when it supports it and is a no-op otherwise.
func Preload(w StagedWrite) (bool, error) {
	if p, ok := w.(Preloader); ok {
		return p.Preload()
	}
	return false, nil
}

// ContentPreloader is implemented by backends that can store content ahead of
// the write transaction that will stage and promote it.
type ContentPreloader interface {
	PreloadContent(ctx context.Context, hash string, data []byte) (created bool, err error)
}

// PreloadContent preloads data when b supports it and is a no-op otherwise.
func PreloadContent(ctx context.Context, b Backend, hash string, data []byte) (bool, error) {
	if p, ok := b.(ContentPreloader); ok {
		return p.PreloadContent(ctx, hash, data)
	}
	return false, nil
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
