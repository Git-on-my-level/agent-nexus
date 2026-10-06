package scopemigrate_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"agent-nexus-core/internal/scopemigrate"
	"agent-nexus-core/internal/storage"
	"modernc.org/sqlite"
)

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// This adapter exercises a real canonical metadata keyset, without historical
// content reads. It uses explicit synthetic audience facts, not a production
// replacement for A's legacy-authority classifier.
type artifactSource struct {
	after func(*sql.Tx) error
	crash bool
}

func (s artifactSource) Epoch(ctx context.Context, tx *sql.Tx) (int64, error) {
	var epoch int64
	err := tx.QueryRowContext(ctx, `SELECT version FROM resource_access_epoch WHERE singleton=1`).Scan(&epoch)
	return epoch, err
}
func (s artifactSource) Page(ctx context.Context, tx *sql.Tx, after string, limit, maxBytes int) ([]scopemigrate.Record, bool, error) {
	rows, err := tx.QueryContext(ctx, `SELECT id,content_refs_json IS NULL FROM artifacts WHERE id>? ORDER BY id LIMIT ?`, after, limit)
	if err != nil {
		return nil, false, err
	}
	var batch []scopemigrate.Record
	for rows.Next() {
		var id string
		var unknown bool
		if err := rows.Scan(&id, &unknown); err != nil {
			rows.Close()
			return nil, false, err
		}
		batch = append(batch, scopemigrate.Record{Key: id, Kind: "artifact", ID: id, Version: 1, Creator: scopemigrate.Candidate{ScopeID: "owner-private", Subset: !unknown}, Uncertain: unknown, ContentUnavailable: unknown})
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, false, err
	}
	if s.after != nil {
		if err := s.after(tx); err != nil {
			return nil, false, err
		}
	}
	if s.crash {
		// Leave durable evidence outside SQLite for the controlling parent, then
		// die while holding an uncommitted assignment and a write transaction.
		_, err = tx.Exec(`INSERT INTO scope_migration_placements VALUES('placement',1,'crash','artifact','crash',1,'owner-private',0,0)`)
		if err != nil {
			return nil, false, err
		}
		os.Exit(23)
	}
	return batch, len(batch) < limit, nil
}

func fixture(t *testing.T, n int) (*storage.Workspace, *scopemigrate.Runner) {
	t.Helper()
	w, err := storage.InitializeWorkspace(context.Background(), t.TempDir())
	must(t, err)
	t.Cleanup(func() { w.Close() })
	tx, err := w.DB().Begin()
	must(t, err)
	must(t, storage.InstallScopeMigration(context.Background(), tx))
	for i := 0; i < n; i++ {
		_, err = tx.Exec(`INSERT INTO artifacts(id,kind,created_at,created_by,content_type,content_hash,content_refs_json) VALUES(?,'note','now','owner','text/plain','unavailable',?)`, fmt.Sprintf("blob-%06d", i), func() any {
			if i%10 == 0 {
				return nil
			}
			return "[]"
		}())
		must(t, err)
	}
	must(t, tx.Commit())
	must(t, w.EnableScopeServingLease(context.Background()))
	r := &scopemigrate.Runner{DB: w.DB(), Source: artifactSource{}, Job: "placement", Owner: "attempt-1", SealedScope: "no-grants", LeaseDuration: time.Minute, MaxBytes: 4 << 20}
	return w, r
}

func begin(t *testing.T, r *scopemigrate.Runner) int64 {
	t.Helper()
	token, err := r.Acquire(context.Background())
	must(t, err)
	p, err := r.Step(context.Background(), token, 64)
	must(t, err)
	if p.Generation != 1 || p.Cursor != "" || p.Processed != 0 {
		t.Fatalf("initial generation: %+v", p)
	}
	return token
}

func TestPlacementNeverWidensAudience(t *testing.T) {
	base := scopemigrate.Record{Key: "1", Kind: "card", ID: "1", Version: 1}
	cases := []struct {
		name      string
		edit      func(*scopemigrate.Record)
		scope     string
		exception bool
	}{
		{"container", func(r *scopemigrate.Record) { r.Container = scopemigrate.Candidate{ScopeID: "board", Subset: true} }, "board", false},
		{"creator", func(r *scopemigrate.Record) { r.Creator = scopemigrate.Candidate{ScopeID: "creator", Subset: true} }, "creator", false},
		{"admins", func(r *scopemigrate.Record) { r.Admins = scopemigrate.Candidate{ScopeID: "admins", Subset: true} }, "admins", false},
		{"other owner mention", func(r *scopemigrate.Record) {
			r.Creator = scopemigrate.Candidate{ScopeID: "creator"}
			r.RestrictingOwners = []string{"owner", "owner"}
			r.RestrictionOwner = scopemigrate.Candidate{ScopeID: "owner", Subset: true}
		}, "owner", true},
		{"multiple owners", func(r *scopemigrate.Record) {
			r.RestrictingOwners = []string{"one", "two"}
			r.RestrictionOwner = scopemigrate.Candidate{ScopeID: "one", Subset: true}
		}, "sink", true},
		{"owner also denied", func(r *scopemigrate.Record) {
			r.RestrictingOwners = []string{"owner"}
			r.RestrictionOwner = scopemigrate.Candidate{ScopeID: "owner"}
		}, "sink", true},
		{"admin expansion", func(r *scopemigrate.Record) { r.Admins = scopemigrate.Candidate{ScopeID: "admins"} }, "sink", true},
		{"uncertain public parent", func(r *scopemigrate.Record) {
			r.Uncertain = true
			r.Container = scopemigrate.Candidate{ScopeID: "public", Subset: true}
			r.Creator = scopemigrate.Candidate{ScopeID: "creator", Subset: true}
		}, "creator", false},
		{"missing content", func(r *scopemigrate.Record) {
			r.ContentUnavailable = true
			r.Container = scopemigrate.Candidate{ScopeID: "public", Subset: true}
			r.Creator = scopemigrate.Candidate{ScopeID: "creator", Subset: true}
		}, "creator", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := base
			tc.edit(&r)
			p, err := scopemigrate.Place(r, "sink")
			must(t, err)
			if p.ScopeID != tc.scope || p.Exception != tc.exception || p.ContentUnavailable != r.ContentUnavailable {
				t.Fatalf("placement %+v", p)
			}
		})
	}
}

func TestSyntheticPersonalAnd10xResume(t *testing.T) {
	if testing.Short() {
		t.Skip("real synthetic migration scale fixture")
	}
	for _, n := range []int{806, 8060} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			w, r := fixture(t, n)
			token := begin(t, r)
			ctx := context.Background()
			for i := 0; i < 3; i++ {
				p, err := r.Step(ctx, token, 64)
				must(t, err)
				if p.Processed != int64((i+1)*64) {
					t.Fatal(p)
				}
			}
			root := w.Layout().RootDir
			must(t, w.Close())
			w2, err := storage.InitializeWorkspace(ctx, root)
			must(t, err)
			defer w2.Close()
			r.DB = w2.DB()
			for {
				before, err := r.Report(ctx)
				must(t, err)
				p, err := r.Step(ctx, token, 64)
				must(t, err)
				if p.Processed-before.Processed > 64 {
					t.Fatal("unbounded chunk")
				}
				if p.Done {
					if p.Processed != int64(n) || p.Exceptions != int64((n+9)/10) {
						t.Fatal(p)
					}
					break
				}
			}
			var total, unavailable int
			must(t, w2.DB().QueryRow(`SELECT count(*),sum(content_unavailable) FROM scope_migration_placements WHERE generation=1`).Scan(&total, &unavailable))
			if total != n || unavailable != (n+9)/10 {
				t.Fatal(total, unavailable)
			}
			var plan string
			must(t, w2.DB().QueryRow(`EXPLAIN QUERY PLAN SELECT id FROM artifacts WHERE id>'blob-000064' ORDER BY id LIMIT 64`).Scan(new(int), new(int), new(int), &plan))
			if !strings.Contains(plan, "SEARCH") || !strings.Contains(plan, "id>?") {
				t.Fatal(plan)
			}
			t.Logf("records=%d exceptions=%d bounded_keyset=%s", n, unavailable, plan)
		})
	}
}

func TestFailuresAdvanceNoWatermark(t *testing.T) {
	w, r := fixture(t, 130)
	ctx := context.Background()
	token := begin(t, r)
	p, err := r.Step(ctx, token, 64)
	must(t, err)
	// Fail AFTER the first row has been inserted, proving transaction rollback.
	_, err = w.DB().Exec(`CREATE TRIGGER fail_chunk BEFORE INSERT ON scope_migration_placements WHEN NEW.source_key='blob-000065' BEGIN SELECT RAISE(ABORT,'injected chunk failure'); END`)
	must(t, err)
	_, err = r.Step(ctx, token, 64)
	if err == nil {
		t.Fatal("chunk unexpectedly succeeded")
	}
	got, err := r.Report(ctx)
	must(t, err)
	if got != p {
		t.Fatal("failed chunk advanced checkpoint", got, p)
	}
	var count int
	must(t, w.DB().QueryRow(`SELECT count(*) FROM scope_migration_placements`).Scan(&count))
	if count != 64 {
		t.Fatal(count)
	}
	_, err = w.DB().Exec(`DROP TRIGGER fail_chunk`)
	must(t, err)
	r.CheckDisk = func(context.Context) error { return scopemigrate.ErrLowDisk }
	_, err = r.Step(ctx, token, 64)
	if !errors.Is(err, scopemigrate.ErrLowDisk) {
		t.Fatal(err)
	}
	got, err = r.Report(ctx)
	must(t, err)
	if got != p {
		t.Fatal("low disk advanced checkpoint")
	}
	r.CheckDisk = nil
	// Expire/take over from another attempt; the previous token cannot commit.
	_, err = w.DB().Exec(`UPDATE scope_migration_jobs SET lease_until=0`)
	must(t, err)
	other := *r
	other.Owner = "attempt-2"
	next, err := other.Acquire(ctx)
	must(t, err)
	if next <= token {
		t.Fatal("fencing token reused")
	}
	_, err = r.Step(ctx, token, 64)
	if !errors.Is(err, scopemigrate.ErrLeaseLost) {
		t.Fatal(err)
	}
	_, err = other.Step(ctx, next, 64)
	must(t, err)
}

func TestEpochInvalidationAndCancellation(t *testing.T) {
	w, r := fixture(t, 100)
	ctx := context.Background()
	token := begin(t, r)
	p, err := r.Step(ctx, token, 32)
	must(t, err)
	r.Source = artifactSource{after: func(tx *sql.Tx) error {
		_, err := tx.Exec(`UPDATE resource_access_epoch SET version=version+1`)
		return err
	}}
	_, err = r.Step(ctx, token, 32)
	if !errors.Is(err, scopemigrate.ErrSourceChanged) {
		t.Fatal(err)
	}
	got, err := r.Report(ctx)
	must(t, err)
	if got != p {
		t.Fatal("unstable source committed")
	}
	r.Source = artifactSource{}
	_, err = w.DB().Exec(`UPDATE resource_access_epoch SET version=version+1`)
	must(t, err)
	got, err = r.Step(ctx, token, 32)
	must(t, err)
	if got.Generation != 2 || got.Cursor != "" || got.Processed != 0 || got.Done {
		t.Fatal(got)
	}
	var retained int
	must(t, w.DB().QueryRow(`SELECT count(*) FROM scope_migration_placements WHERE generation=1`).Scan(&retained))
	if retained != 32 {
		t.Fatal("epoch restart performed cleanup")
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	_, err = r.Step(cancelled, token, 32)
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestCrashMidChunkAndResume(t *testing.T) {
	if testing.Short() {
		t.Skip("crash subprocess")
	}
	w, r := fixture(t, 100)
	token := begin(t, r)
	root := w.Layout().RootDir
	must(t, w.Close())
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestMigrationCrashChild$")
	cmd.Env = append(os.Environ(), "ANX_SCOPE_CRASH_ROOT="+root)
	out, err := cmd.CombinedOutput()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 23 {
		t.Fatalf("crash child: %v %s", err, out)
	}
	w2, err := storage.InitializeWorkspace(context.Background(), root)
	must(t, err)
	defer w2.Close()
	r.DB = w2.DB()
	var count int
	must(t, w2.DB().QueryRow(`SELECT count(*) FROM scope_migration_placements`).Scan(&count))
	if count != 0 {
		t.Fatal("crash committed partial chunk")
	}
	p, err := r.Report(context.Background())
	must(t, err)
	if p.Cursor != "" || p.Processed != 0 {
		t.Fatal(p)
	}
	// Process ownership is released by death; DB worker ownership expires and
	// must be explicitly taken over with a new token.
	_, err = w2.DB().Exec(`UPDATE scope_migration_jobs SET lease_until=0`)
	must(t, err)
	r.Owner = "after-crash"
	next, err := r.Acquire(context.Background())
	must(t, err)
	if next <= token {
		t.Fatal(next)
	}
	p, err = r.Step(context.Background(), next, 64)
	must(t, err)
	if p.Processed != 64 {
		t.Fatal(p)
	}
}

func TestMigrationCrashChild(t *testing.T) {
	root := os.Getenv("ANX_SCOPE_CRASH_ROOT")
	if root == "" {
		t.Skip("subprocess only")
	}
	w, err := storage.InitializeWorkspace(context.Background(), root)
	must(t, err)
	defer w.Close()
	r := &scopemigrate.Runner{DB: w.DB(), Source: artifactSource{crash: true}, Job: "placement", Owner: "attempt-1", SealedScope: "no-grants", LeaseDuration: time.Minute, MaxBytes: 4 << 20}
	_, err = r.Step(context.Background(), 1, 64)
	must(t, err)
	t.Fatal("crash was not injected")
}

func TestSQLiteFullPreservesCheckpoint(t *testing.T) {
	w, r := fixture(t, 10)
	token := begin(t, r)
	ctx := context.Background()
	// Real SQLITE_FULL using SQLite's hard page ceiling, not a mocked error.
	// The new placements are enlarged so a page allocation is required.
	w.DB().SetMaxOpenConns(1)
	_, err := w.DB().Exec(`PRAGMA wal_checkpoint(TRUNCATE)`)
	must(t, err)
	var pages int
	must(t, w.DB().QueryRow(`PRAGMA page_count`).Scan(&pages))
	_, err = w.DB().Exec(fmt.Sprintf(`PRAGMA max_page_count=%d`, pages))
	must(t, err)
	r.SealedScope = strings.Repeat("x", 64<<10)
	before, err := r.Report(ctx)
	must(t, err)
	_, err = r.Step(ctx, token, 10)
	var sqliteErr *sqlite.Error
	if !errors.As(err, &sqliteErr) || sqliteErr.Code() != 13 {
		t.Fatalf("expected real SQLite FULL: %v", err)
	}
	after, err := r.Report(ctx)
	must(t, err)
	if after != before {
		t.Fatal("FULL advanced checkpoint")
	}
	_, err = w.DB().Exec(`PRAGMA max_page_count=1073741823`)
	must(t, err)
	r.SealedScope = "no-grants"
	_, err = r.Step(ctx, token, 10)
	must(t, err)
}

type lifecycle struct {
	calls atomic.Int64
	fail  bool
	delay time.Duration
}

func (l *lifecycle) Step(ctx context.Context, tx *sql.Tx, limit int, token int64) (bool, error) {
	if limit != 64 || token < 1 {
		return false, errors.New("invalid lifecycle bounds")
	}
	l.calls.Add(1)
	_, err := tx.ExecContext(ctx, `INSERT INTO scope_migration_placements VALUES('lifecycle',1,'one','card','one',1,'private',0,0)`)
	if err != nil {
		return false, err
	}
	if l.delay > 0 {
		time.Sleep(l.delay)
	}
	if l.fail {
		return false, errors.New("private sentinel must not be reported")
	}
	return true, nil
}
func TestLifecycleRollbackAndSupervision(t *testing.T) {
	w, r := fixture(t, 1)
	token := begin(t, r)
	l := &lifecycle{fail: true}
	r.Rebuilder = l
	_, err := r.LifecycleStep(context.Background(), token)
	if err == nil {
		t.Fatal("lifecycle failure lost")
	}
	var n int
	must(t, w.DB().QueryRow(`SELECT count(*) FROM scope_migration_placements WHERE job='lifecycle'`).Scan(&n))
	if n != 0 {
		t.Fatal("failed lifecycle committed")
	}
	l.fail = false
	done, err := r.LifecycleStep(context.Background(), token)
	must(t, err)
	if !done {
		t.Fatal("lifecycle didn't finish")
	}
	r.Rebuilder = nil
	r.CheckDisk = func(context.Context) error { return scopemigrate.ErrLowDisk }
	ctx, cancel := context.WithCancel(context.Background())
	var report string
	err = r.Run(ctx, time.Millisecond, func(category string) { report = category; cancel() })
	if !errors.Is(err, context.Canceled) || report != "low_disk" {
		t.Fatal(err, report)
	}
	if _, err = os.Stat(filepath.Join(w.Layout().RootDir, ".scope-serving.lock")); err != nil {
		t.Fatal(err)
	}
}

func TestLifecycleDeadlineRollsBackLateSuccess(t *testing.T) {
	w, r := fixture(t, 1)
	token := begin(t, r)
	r.Rebuilder = &lifecycle{delay: 80 * time.Millisecond}
	done, err := r.LifecycleStep(context.Background(), token)
	if err == nil || done {
		t.Fatal("late worker committed", done, err)
	}
	var n int
	must(t, w.DB().QueryRow(`SELECT count(*) FROM scope_migration_placements WHERE job='lifecycle'`).Scan(&n))
	if n != 0 {
		t.Fatal("expired lifecycle committed")
	}
}
