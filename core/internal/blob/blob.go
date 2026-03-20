package blob

import (
	"context"
	"errors"
)

var ErrBlobNotFound = errors.New("blob not found")

type StagedWrite interface {
	Promote() error
	Cleanup() error
}

type Backend interface {
	Write(ctx context.Context, hash string, data []byte) (StagedWrite, error)
	Read(ctx context.Context, hash string) ([]byte, error)
	Exists(ctx context.Context, hash string) (bool, error)
}
