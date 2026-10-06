//go:build darwin || linux

package scopemigrate_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"agent-nexus-core/internal/storage"
	"golang.org/x/sys/unix"
	"modernc.org/sqlite"
)

// Use only a deliberately provisioned, small disposable filesystem. The marker
// and capacity bound prevent an accidental invocation from filling a host disk.
// This is opt-in because ordinary CI runners do not provide a separate mount.
func TestFilesystemENOSPCPreservesCheckpoint(t *testing.T) {
	if testing.Short() {
		t.Skip("disposable filesystem integration probe")
	}
	volume := os.Getenv("ANX_SCOPE_ENOSPC_ROOT")
	if volume == "" {
		t.Skip("requires a disposable <=128 MiB filesystem with .anx-disposable-enospc marker")
	}
	marker, err := os.ReadFile(filepath.Join(volume, ".anx-disposable-enospc"))
	must(t, err)
	if string(marker) != "scope migration disposable filesystem\n" {
		t.Fatal("invalid disposable filesystem marker")
	}
	var fs unix.Statfs_t
	must(t, unix.Statfs(volume, &fs))
	if fs.Bsize <= 0 || fs.Blocks == 0 || uint64(fs.Blocks)*uint64(fs.Bsize) > 128<<20 {
		t.Fatal("filesystem exceeds disposable probe capacity")
	}
	root, err := os.MkdirTemp(volume, "anx-enospc-")
	must(t, err)
	t.Cleanup(func() { os.RemoveAll(root) })
	w, r := fixtureAt(t, 10, root)
	token := begin(t, r)
	ctx := context.Background()
	w.DB().SetMaxOpenConns(1)
	_, err = w.DB().Exec(`PRAGMA wal_checkpoint(TRUNCATE)`)
	must(t, err)
	before, err := r.Report(ctx)
	must(t, err)
	r.SealedScope = strings.Repeat("x", 64<<10)

	fillerPath := filepath.Join(root, "filesystem-filler")
	filler, err := os.OpenFile(fillerPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	must(t, err)
	t.Cleanup(func() { filler.Close(); os.Remove(fillerPath) })
	buf := make([]byte, 512<<10)
	for i := range buf {
		buf[i] = byte(i%251 + 1)
	}
	var written int64
	chunk := len(buf)
	exhausted := false
	for written <= 128<<20 {
		n, writeErr := filler.Write(buf[:chunk])
		written += int64(n)
		if writeErr != nil {
			err = writeErr
			// A failed large allocation can leave enough blocks for SQLite's
			// smaller page writes. Exhaust even one-byte allocations first.
			if errors.Is(writeErr, syscall.ENOSPC) && chunk > 1 {
				chunk /= 2
				continue
			}
			exhausted = errors.Is(writeErr, syscall.ENOSPC) && chunk == 1
			break
		}
	}
	if !exhausted {
		t.Fatalf("expected filesystem ENOSPC after %d bytes: %v", written, err)
	}
	t.Logf("filesystem write returned ENOSPC after %d bytes", written)
	_, err = r.Step(ctx, token, 10)
	var sqliteErr *sqlite.Error
	if !errors.As(err, &sqliteErr) || sqliteErr.Code() != 13 {
		t.Fatalf("expected SQLite FULL on exhausted filesystem: %v", err)
	}
	// Reclaim headroom before reopening; no migration operation runs between the
	// failed chunk and the durable checkpoint/placement assertions.
	_ = filler.Close()
	must(t, os.Remove(fillerPath))
	must(t, w.Close())
	w, err = storage.InitializeWorkspace(ctx, root)
	must(t, err)
	t.Cleanup(func() { w.Close() })
	r.DB = w.DB()
	after, err := r.Report(ctx)
	must(t, err)
	if after != before {
		t.Fatal("ENOSPC advanced durable checkpoint", before, after)
	}
	var placements int
	must(t, w.DB().QueryRow(`SELECT count(*) FROM scope_migration_placements`).Scan(&placements))
	if placements != 0 {
		t.Fatal("ENOSPC committed partial placements", placements)
	}
	r.SealedScope = "no-grants"
	after, err = r.Step(ctx, token, 64)
	must(t, err)
	if after.Processed != 10 || !after.Done {
		t.Fatal("migration did not resume after headroom recovery", after)
	}
}
