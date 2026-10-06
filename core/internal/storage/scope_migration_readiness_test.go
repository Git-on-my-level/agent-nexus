package storage_test

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"agent-nexus-core/internal/blob"
	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/server"
	"agent-nexus-core/internal/storage"
)

type unavailableScopeBlobs struct {
	blob.Backend
	reads atomic.Int64
	delay time.Duration
}

func (b *unavailableScopeBlobs) Read(ctx context.Context, hash string) ([]byte, error) {
	b.reads.Add(1)
	select {
	case <-ctx.Done():
	case <-time.After(b.delay):
	}
	return nil, blob.ErrBlobNotFound
}
func (b *unavailableScopeBlobs) OpenReadStream(ctx context.Context, hash string) (io.ReadCloser, int64, error) {
	b.reads.Add(1)
	select {
	case <-ctx.Done():
	case <-time.After(b.delay):
	}
	return nil, 0, blob.ErrBlobNotFound
}

// Exercise real workspace initialization, the production constructor and HTTP
// readiness. Slow, unavailable historical blobs must never be read or retried.
func TestScopeColdReadinessZeroHistoricalBlobReads(t *testing.T) {
	if testing.Short() {
		t.Skip("real HTTP cold startup gate")
	}
	previousLog := log.Writer()
	log.SetOutput(io.Discard)
	defer log.SetOutput(previousLog)
	for _, n := range []int{100, 1000} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			ctx := context.Background()
			root := t.TempDir()
			w, err := storage.InitializeWorkspace(ctx, root)
			if err != nil {
				t.Fatal(err)
			}
			tx, err := w.DB().Begin()
			if err != nil {
				t.Fatal(err)
			}
			if err := storage.InstallScopeMigration(ctx, tx); err != nil {
				t.Fatal(err)
			}
			for i := 0; i < n; i++ {
				if _, err := tx.Exec(`INSERT INTO artifacts(id,kind,created_at,created_by,content_type,content_hash) VALUES(?,'note','now','owner','text/plain',?)`, fmt.Sprintf("unknown-%06d", i), fmt.Sprintf("missing-%06d", i)); err != nil {
					t.Fatal(err)
				}
			}
			if err := tx.Commit(); err != nil {
				t.Fatal(err)
			}
			if err := w.InstallScopeFormatFence(ctx); err != nil {
				t.Fatal(err)
			}
			if err := w.Close(); err != nil {
				t.Fatal(err)
			}
			b := &unavailableScopeBlobs{delay: 10 * time.Millisecond}
			for attempt := 0; attempt < 2; attempt++ {
				start := time.Now()
				w, err = storage.InitializeWorkspace(ctx, root)
				if err != nil {
					t.Fatal(err)
				}
				s := primitives.NewStore(w.DB(), b, w.Layout().ArtifactContentDir)
				h := httptest.NewServer(server.NewHandler("scope-readiness", server.WithPrimitiveStore(s), server.WithHealthCheck(w.Ping)))
				client := &http.Client{Timeout: time.Second}
				res, err := client.Get(h.URL + "/readyz")
				if err != nil {
					h.Close()
					w.Close()
					t.Fatal(err)
				}
				res.Body.Close()
				h.Close()
				if res.StatusCode != 200 {
					w.Close()
					t.Fatal(res.StatusCode)
				}
				scope := primitives.WithAccessScope(ctx, primitives.AccessScope{ActorID: "unrelated"})
				if _, err := s.GetArtifact(scope, "unknown-000000"); err == nil {
					w.Close()
					t.Fatal("unknown manifest became visible")
				}
				if err := w.Close(); err != nil {
					t.Fatal(err)
				}
				if reads := b.reads.Load(); reads != 0 {
					t.Fatalf("startup read %d historical blobs", reads)
				}
				if time.Since(start) > 5*time.Second {
					t.Fatal("cold readiness exceeded five seconds")
				}
				t.Logf("attempt=%d unknown_blobs=%d readyz=200 elapsed=%s blob_reads=0", attempt, n, time.Since(start))
			}
		})
	}
}
