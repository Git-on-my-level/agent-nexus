package scopeboundary_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"agent-nexus-core/internal/blob"
	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/resourceaccess"
	"agent-nexus-core/internal/server"
	"agent-nexus-core/internal/storage"
)

type slowBlobs struct {
	blob.Backend
	reads     atomic.Int64
	available atomic.Bool
}

func (s *slowBlobs) Read(ctx context.Context, hash string) ([]byte, error) {
	s.reads.Add(1)
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(100 * time.Millisecond):
	}
	if !s.available.Load() {
		return nil, blob.ErrBlobNotFound
	}
	return []byte("public synthetic content"), nil
}

// Due work and cursor survive failed attempts and reopening. Discovery visits J
// primary-key rows without an unindexed NULL filter; failed blobs do not pin it.
func bridgeTables(db *sql.DB) error {
	_, e := db.Exec(`CREATE TABLE IF NOT EXISTS experiment_blob_jobs(id TEXT PRIMARY KEY,hash TEXT,type TEXT,due INTEGER,attempts INTEGER DEFAULT 0);CREATE INDEX IF NOT EXISTS experiment_blob_due ON experiment_blob_jobs(due,id);CREATE TABLE IF NOT EXISTS experiment_discovery(singleton INTEGER PRIMARY KEY,cursor TEXT);INSERT OR IGNORE INTO experiment_discovery VALUES(1,'');CREATE TABLE IF NOT EXISTS experiment_lease(singleton INTEGER PRIMARY KEY,owner TEXT);`)
	return e
}
func discover(db *sql.DB, j int) (int, error) {
	tx, e := db.Begin()
	if e != nil {
		return 0, e
	}
	defer tx.Rollback()
	var cursor string
	if e = tx.QueryRow(`SELECT cursor FROM experiment_discovery WHERE singleton=1`).Scan(&cursor); e != nil {
		return 0, e
	}
	rows, e := tx.Query(`SELECT id,content_hash,content_type,content_refs_json FROM artifacts WHERE id>? ORDER BY id LIMIT ?`, cursor, j)
	if e != nil {
		return 0, e
	}
	type entry struct {
		id, hash, typ string
		refs          sql.NullString
	}
	var batch []entry
	for rows.Next() {
		var a entry
		if e = rows.Scan(&a.id, &a.hash, &a.typ, &a.refs); e != nil {
			rows.Close()
			return 0, e
		}
		batch = append(batch, a)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return 0, e
	}
	for _, a := range batch {
		if !a.refs.Valid {
			if _, e = tx.Exec(`INSERT OR IGNORE INTO experiment_blob_jobs(id,hash,type,due) VALUES(?,?,?,0)`, a.id, a.hash, a.typ); e != nil {
				return 0, e
			}
		}
		cursor = a.id
	}
	if _, e = tx.Exec(`UPDATE experiment_discovery SET cursor=? WHERE singleton=1`, cursor); e != nil {
		return 0, e
	}
	return len(batch), tx.Commit()
}
func blobTick(db *sql.DB, backend *slowBlobs, now int, lowDisk, crash bool) error {
	if lowDisk {
		return errors.New("low disk: conversion paused")
	}
	var id, hash, typ string
	e := db.QueryRow(`SELECT id,hash,type FROM experiment_blob_jobs WHERE due<=? ORDER BY due,id LIMIT 1`, now).Scan(&id, &hash, &typ)
	if e != nil {
		return e
	}
	if crash {
		return errors.New("injected worker failure")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	if backend.available.Load() {
		cancel()
		ctx, cancel = context.WithTimeout(context.Background(), time.Second)
	}
	defer cancel()
	body, readErr := backend.Read(ctx, hash)
	if readErr != nil {
		_, e = db.Exec(`UPDATE experiment_blob_jobs SET due=?,attempts=attempts+1 WHERE id=?`, now+60, id)
		return e
	}
	tx, e := db.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	_, e = tx.Exec(`UPDATE artifacts SET content_refs_json=? WHERE id=? AND content_hash=? AND content_refs_json IS NULL`, resourceaccess.ContentReferenceAtomsJSON(string(body), typ), id, hash)
	if e != nil {
		return e
	}
	_, e = tx.Exec(`DELETE FROM experiment_blob_jobs WHERE id=?`, id)
	if e != nil {
		return e
	}
	return tx.Commit()
}

// Actual storage open, primitive constructor, core HTTP handler and network /readyz.
// Does not launch the CLI, a container, or bypass authentication on business routes.
func coldBridge(t *testing.T, root string, backend *slowBlobs) (*storage.Workspace, *primitives.Store, *httptest.Server) {
	t.Helper()
	start := time.Now()
	readsBefore := backend.reads.Load()
	var w *storage.Workspace
	var e error
	w, e = storage.InitializeWorkspace(context.Background(), root)
	must(t, e)
	must(t, bridgeTables(w.DB()))
	_, e = w.DB().Exec(`INSERT INTO experiment_lease VALUES(1,'running')`)
	must(t, e)
	s := primitives.NewScopeExperimentBridgeStore(w.DB(), backend, w.Layout().ArtifactContentDir)
	h := httptest.NewServer(server.NewHandler("experiment", server.WithPrimitiveStore(s), server.WithHealthCheck(w.Ping)))
	client := &http.Client{Timeout: time.Second}
	res, e := client.Get(h.URL + "/readyz")
	must(t, e)
	io.Copy(io.Discard, res.Body)
	res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatal(res.StatusCode)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatalf("cold readiness %s", time.Since(start))
	}
	if backend.reads.Load() != readsBefore {
		t.Fatal("startup read blob backend")
	}
	t.Logf("cold readiness=%s startup_blob_reads=%d", time.Since(start), backend.reads.Load()-readsBefore)
	return w, s, h
}
func TestColdBridgeSlowUnavailableRecovery(t *testing.T) {
	if testing.Short() {
		t.Skip("actual HTTP/storage cold-start experiment")
	}
	for _, n := range []int{100, 1000} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			root := t.TempDir()
			w, e := storage.InitializeWorkspace(context.Background(), root)
			must(t, e)
			tx, e := w.DB().Begin()
			must(t, e)
			for i := 0; i < n; i++ {
				_, e = tx.Exec(`INSERT INTO artifacts(id,kind,created_at,created_by,content_type,content_hash,metadata_json) VALUES(?,'note','now','owner','text/plain',?,'{}')`, fmt.Sprintf("blob-%04d", i), fmt.Sprintf("hash-%04d", i))
				must(t, e)
			}
			must(t, tx.Commit())
			must(t, w.Close())
			backend := &slowBlobs{}
			w, s, h := coldBridge(t, root, backend)
			if backend.reads.Load() != 0 {
				t.Fatal("startup did blob I/O")
			}
			if _, e = w.DB().Exec(`INSERT INTO experiment_lease VALUES(1,'second')`); e == nil {
				t.Fatal("duplicate lease admitted")
			}
			count, e := discover(w.DB(), 16)
			must(t, e)
			if count != 16 {
				t.Fatal(count)
			}
			if e = blobTick(w.DB(), backend, 0, true, false); e == nil || backend.reads.Load() != 0 {
				t.Fatal("low disk made progress")
			}
			if e = blobTick(w.DB(), backend, 0, false, true); e == nil || backend.reads.Load() != 0 {
				t.Fatal("worker crash lost job")
			}
			must(t, blobTick(w.DB(), backend, 0, false, false))
			if backend.reads.Load() != 1 {
				t.Fatal("unbounded blob work")
			}
			var jobs, attempts, due int
			must(t, w.DB().QueryRow(`SELECT count(*) FROM experiment_blob_jobs`).Scan(&jobs))
			must(t, w.DB().QueryRow(`SELECT attempts,due FROM experiment_blob_jobs WHERE id='blob-0000'`).Scan(&attempts, &due))
			if jobs != 16 || attempts != 1 || due != 60 {
				t.Fatal("fault/retry state lost", jobs, attempts, due)
			}
			// Hold the other discovered jobs so the later successful attempt must
			// recover the exact unavailable blob, not merely index another one.
			_, e = w.DB().Exec(`UPDATE experiment_blob_jobs SET due=120 WHERE id<>'blob-0000'`)
			must(t, e)
			scope := primitives.WithAccessScope(context.Background(), primitives.AccessScope{ActorID: "unrelated"})
			if _, e = s.GetArtifact(scope, "blob-0000"); e == nil {
				t.Fatal("unindexed artifact leaked")
			}
			h.Close()
			_, e = w.DB().Exec(`DELETE FROM experiment_lease`)
			must(t, e)
			must(t, w.Close())
			// Cold restart neither retries all failed blobs nor loses discovery progress.
			previousReads := backend.reads.Load()
			w, s, h = coldBridge(t, root, backend)
			if backend.reads.Load() != previousReads {
				t.Fatal("restart retried blobs")
			}
			var checkpoint string
			must(t, w.DB().QueryRow(`SELECT cursor FROM experiment_discovery`).Scan(&checkpoint))
			if checkpoint != "blob-0015" {
				t.Fatal(checkpoint)
			}
			backend.available.Store(true)
			must(t, blobTick(w.DB(), backend, 61, false, false))
			var indexed int
			must(t, w.DB().QueryRow(`SELECT count(*) FROM artifacts WHERE content_refs_json IS NOT NULL`).Scan(&indexed))
			if indexed != 1 {
				t.Fatal(indexed)
			}
			var recovered string
			must(t, w.DB().QueryRow(`SELECT id FROM artifacts WHERE content_refs_json IS NOT NULL`).Scan(&recovered))
			if recovered != "blob-0000" {
				t.Fatal("did not retry failed blob", recovered)
			}
			h.Close()
			_, e = w.DB().Exec(`DELETE FROM experiment_lease;ALTER TABLE schema_migrations RENAME TO scope_schema_migrations;CREATE VIEW schema_migrations AS SELECT version,applied_at FROM scope_schema_migrations WHERE anx_requires_scope_format_v1()`)
			must(t, e)
			must(t, w.Close())
			if old, err := storage.InitializeWorkspace(context.Background(), root); err == nil {
				old.Close()
				t.Fatal("old initializer accepted cutover")
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestCompatibleRecoveryChild$", "-test.v")
			cmd.Env = append(os.Environ(), "SCOPE_RECOVERY_CHILD_ROOT="+root)
			output, err := cmd.CombinedOutput()
			cancel()
			must(t, err)
			t.Logf("compatible recovery subprocess: %s", output)

			t.Logf("blobs=%d checkpoint=%s low_disk_and_failure_preserve_jobs=true compatible_recovery_ready=true", n, checkpoint)
		})
	}
}

// Invoked as a fresh compatible test executable, against the fenced synthetic DB.
func TestCompatibleRecoveryChild(t *testing.T) {
	root := os.Getenv("SCOPE_RECOVERY_CHILD_ROOT")
	if root == "" {
		t.Skip("subprocess only")
	}
	db, e := sql.Open("sqlite", filepath.Join(root, "state.sqlite"))
	must(t, e)
	defer db.Close()
	db.SetMaxOpenConns(1)
	var version int
	must(t, db.QueryRow(`SELECT max(version) FROM scope_schema_migrations`).Scan(&version))
	if version != 63 {
		t.Fatal(version)
	}
	_, e = db.Exec(`INSERT INTO experiment_lease VALUES(1,'recovery')`)
	must(t, e)
	backend := &slowBlobs{}
	store := primitives.NewScopeExperimentBridgeStore(db, backend, filepath.Join(root, "artifacts/content"))
	h := httptest.NewServer(server.NewHandler("experiment-recovery", server.WithPrimitiveStore(store), server.WithHealthCheck(db.PingContext)))
	defer h.Close()
	client := &http.Client{Timeout: time.Second}
	res, e := client.Get(h.URL + "/readyz")
	must(t, e)
	res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatal(res.StatusCode)
	}
	scope := primitives.WithAccessScope(context.Background(), primitives.AccessScope{ActorID: "unrelated"})
	if _, e = store.GetArtifact(scope, "blob-0002"); e == nil {
		t.Fatal("recovery released unknown blob")
	}
	if backend.reads.Load() != 0 {
		t.Fatal("recovery startup did blob I/O")
	}
	t.Log("new process: forward ledger readable; readyz=200; unknown blob denied; no startup blob I/O")
}
