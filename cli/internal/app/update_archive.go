package app

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"strconv"
	"strings"
)

const releaseExpandedLimit = 128 << 20
const releaseEntryLimit = 1024

// tar.Reader hides PAX/GNU metadata entries inside Next. Count physical
// headers before that interpretation so metadata chains cannot evade the cap.
type releaseTarReader struct {
	reader  io.Reader
	header  [512]byte
	used    int
	payload int64
	entries int
}

func (r *releaseTarReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if r.payload > 0 {
		if int64(len(p)) > r.payload {
			p = p[:r.payload]
		}
		n, err := r.reader.Read(p)
		r.payload -= int64(n)
		return n, err
	}
	if len(p) > 512-r.used {
		p = p[:512-r.used]
	}
	n, err := r.reader.Read(p)
	copy(r.header[r.used:], p[:n])
	r.used += n
	if r.used == 512 {
		r.used = 0
		if r.header != [512]byte{} {
			r.entries++
			if r.entries > releaseEntryLimit {
				return 0, fmt.Errorf("too many release entries")
			}
			// Release archives need only small positive octal sizes. Reject
			// base-256 and malformed sizes rather than losing header alignment.
			size, parseErr := strconv.ParseInt(strings.Trim(string(r.header[124:136]), " \x00"), 8, 64)
			if parseErr != nil || size < 0 || size > releaseExpandedLimit {
				return 0, fmt.Errorf("invalid or oversized release entry")
			}
			r.payload = (size + 511) / 512 * 512
		}
	}
	return n, err
}

type releaseBudgetReader struct {
	ctx       context.Context
	reader    io.Reader
	remaining int64
}

func (r *releaseBudgetReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	if int64(len(p)) > r.remaining+1 {
		p = p[:r.remaining+1]
	}
	n, err := r.reader.Read(p)
	r.remaining -= int64(n)
	if r.remaining < 0 {
		return 0, fmt.Errorf("release expanded bytes exceed 128 MiB")
	}
	return n, err
}
func safeArchiveName(name string) bool {
	return name != "" && !strings.HasPrefix(name, "/") && !strings.Contains(name, "\\") && name == path.Clean(name) && name != ".." && !strings.HasPrefix(name, "../")
}
func extractReleaseBinary(archive string, b []byte) ([]byte, os.FileMode, error) {
	return extractReleaseBinaryContext(context.Background(), archive, b)
}
func extractReleaseBinaryContext(ctx context.Context, archive string, b []byte) ([]byte, os.FileMode, error) {
	if strings.HasSuffix(archive, ".zip") {
		return extractZIPBinaryContext(ctx, b)
	}
	return extractTarGZBinaryContext(ctx, b)
}
func extractZIPBinaryContext(ctx context.Context, b []byte) ([]byte, os.FileMode, error) {
	z, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		return nil, 0, err
	}
	if len(z.File) > releaseEntryLimit {
		return nil, 0, fmt.Errorf("too many release entries")
	}
	var result []byte
	mode := os.FileMode(0755)
	remaining := int64(releaseExpandedLimit)
	for _, f := range z.File {
		if err := ctx.Err(); err != nil {
			return nil, 0, err
		}
		if !safeArchiveName(strings.TrimSuffix(f.Name, "/")) || !f.Mode().IsRegular() && !f.Mode().IsDir() {
			return nil, 0, fmt.Errorf("unsafe release entry")
		}
		rc, err := f.Open()
		if err != nil {
			return nil, 0, err
		}
		budget := &releaseBudgetReader{ctx: ctx, reader: rc, remaining: remaining}
		if f.Name == "anx.exe" {
			if result != nil {
				rc.Close()
				return nil, 0, fmt.Errorf("duplicate release binary")
			}
			result, err = io.ReadAll(budget)
			mode = f.Mode().Perm()
		} else {
			_, err = io.Copy(io.Discard, budget)
		}
		remaining = budget.remaining
		_ = rc.Close()
		if err != nil {
			return nil, 0, err
		}
	}
	if result == nil {
		return nil, 0, fmt.Errorf("release has no anx.exe")
	}
	if mode == 0 {
		mode = 0755
	}
	return result, mode, nil
}
func extractTarGZBinaryContext(ctx context.Context, b []byte) ([]byte, os.FileMode, error) {
	gz, err := gzip.NewReader(bytes.NewReader(b))
	if err != nil {
		return nil, 0, err
	}
	defer gz.Close()
	expanded := &releaseBudgetReader{ctx: ctx, reader: gz, remaining: releaseExpandedLimit}
	reader := tar.NewReader(&releaseTarReader{reader: expanded})
	var result []byte
	mode := os.FileMode(0755)
	for count := 0; ; count++ {
		if err := ctx.Err(); err != nil {
			return nil, 0, err
		}
		h, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, 0, err
		}
		if count >= releaseEntryLimit {
			return nil, 0, fmt.Errorf("too many release entries")
		}
		if !safeArchiveName(strings.TrimSuffix(h.Name, "/")) || h.Typeflag != tar.TypeReg && h.Typeflag != tar.TypeDir {
			return nil, 0, fmt.Errorf("unsafe release entry")
		}
		if h.Name != "anx" {
			continue
		}
		if result != nil {
			return nil, 0, fmt.Errorf("duplicate release binary")
		}
		result, err = io.ReadAll(reader)
		if err != nil {
			return nil, 0, err
		}
		mode = os.FileMode(h.Mode).Perm()
	}
	// Next stops at the tar terminator. Drain gzip too, covering concatenated
	// streams, ignored members, padding, and gzip integrity within the same budget.
	if _, err := io.Copy(io.Discard, expanded); err != nil {
		return nil, 0, err
	}
	if result == nil {
		return nil, 0, fmt.Errorf("release has no anx")
	}
	if mode == 0 {
		mode = 0755
	}
	return result, mode, nil
}
