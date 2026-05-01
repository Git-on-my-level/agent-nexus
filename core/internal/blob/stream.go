package blob

import (
	"crypto/sha256"
	"encoding/hex"
	"hash"
	"io"
)

const streamCopyBufferSize = 256 * 1024

// streamToWriterAndHash copies from src into dst and hasher using at most maxBytes bytes from src.
// If src would deliver more than maxBytes bytes, returns (maxBytes+1, ErrUploadTooLarge).
func streamToWriterAndHash(dst io.Writer, src io.Reader, hasher hash.Hash, maxBytes int64) (written int64, err error) {
	if maxBytes < 0 {
		return 0, ErrUploadTooLarge
	}
	limited := io.LimitReader(src, maxBytes+1)
	mw := io.MultiWriter(dst, hasher)
	buf := make([]byte, streamCopyBufferSize)
	n, err := io.CopyBuffer(mw, limited, buf)
	if n > maxBytes {
		return n, ErrUploadTooLarge
	}
	return n, err
}

func sha256HexDigest(hasher hash.Hash) string {
	return hex.EncodeToString(hasher.Sum(nil))
}

func newSHA256Hasher() hash.Hash {
	return sha256.New()
}
