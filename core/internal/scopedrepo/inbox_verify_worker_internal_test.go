package scopedrepo

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"agent-nexus-core/internal/pm"
	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/storage"
	"modernc.org/sqlite"
)

func verificationWorkerFixture(t *testing.T, extraSQL string) (*sql.DB, *Store, string) {
	t.Helper()
	ctx := context.Background()
	w, err := storage.InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = w.Close() })
	db := w.DB()
	if _, err = pm.NewStore(db); err != nil {
		t.Fatal(err)
	}
	s := New(db)
	for _, init := range []func(context.Context) error{s.Initialize, s.InitializeFeedSchema, s.InitializeInboxOrderSchema, s.InitializeInboxVerificationSchema} {
		if err = init(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if extraSQL != "" {
		if _, err = db.ExecContext(ctx, extraSQL); err != nil {
			t.Fatal(err)
		}
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if result, e := primitives.InstallScopeInboxInvalidation(ctx, tx); e != nil || !result.Complete {
		t.Fatal(result, e)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	id, err := s.StartInboxVerification(ctx, "owner", "")
	if err != nil {
		t.Fatal(err)
	}
	return db, s, id
}

func TestInboxVerificationActiveSQLShutdownJoins(t *testing.T) {
	started := make(chan struct{})
	var once sync.Once
	name, err := opaque()
	if err != nil {
		t.Fatal(err)
	}
	function := "anx_test_comparison_started_" + name
	if err = sqlite.RegisterScalarFunction(function, 0, func(*sqlite.FunctionContext, []driver.Value) (driver.Value, error) {
		once.Do(func() { close(started) })
		return int64(1), nil
	}); err != nil {
		t.Fatal(err)
	}
	db, s, id := verificationWorkerFixture(t, `INSERT INTO scope_domains VALUES('public','active',1); INSERT INTO scope_memberships VALUES('owner','public','owner',1);
 CREATE TRIGGER slow_join_comparison AFTER INSERT ON scope_inbox_verification_scopes BEGIN SELECT `+function+`();
 UPDATE scope_inbox_verification_jobs SET examined=(WITH RECURSIVE seq(n) AS (SELECT 1 UNION ALL SELECT n+1 FROM seq WHERE n<100000000) SELECT sum(n) FROM seq) WHERE id=NEW.job_id; END;`)
	w, err := StartInboxVerificationWorker(context.Background(), true, s, id, DefaultInboxVerificationWorkerOptions())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { w.Close() })
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("worker never entered active SQL")
	}
	if err = w.Close(); !errors.Is(err, context.Canceled) {
		t.Fatal("active close", err)
	}
	var rows, examined, leases int
	if err = db.QueryRow(`SELECT examined,lease_until,(SELECT count(*) FROM scope_inbox_verification_scopes) FROM scope_inbox_verification_jobs WHERE id=?`, id).Scan(&examined, &leases, &rows); err != nil {
		t.Fatal(err)
	}
	if examined != 0 || rows != 0 || leases != 0 {
		t.Fatal("active SQL survived joined close", examined, rows, leases)
	}
}

func TestInboxVerificationReopensAbandonedLease(t *testing.T) {
	db, s, id := verificationWorkerFixture(t, "")
	r := workerRunner(t, s, id, 0)
	lease, err := r.acquireLease(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = r.verifySlice(context.Background(), lease); err != nil {
		t.Fatal(err)
	}
	var seq int
	var dbName, path string
	if err = db.QueryRow(`PRAGMA database_list`).Scan(&seq, &dbName, &path); err != nil {
		t.Fatal(err)
	}
	// Close without releasing the old lease, as a terminated process would.
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	uri := url.URL{Scheme: "file", Path: path, RawQuery: "_txlock=immediate&_pragma=journal_mode(WAL)"}
	reopened, err := sql.Open("sqlite", uri.String())
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	next := workerRunner(t, New(reopened), id, 0)
	if _, err = next.acquireLease(context.Background()); !errors.Is(err, ErrInboxVerificationLease) {
		t.Fatal("abandoned unexpired lease taken", err)
	}
	// Advance persisted lease time, avoiding a real minute of test waiting.
	if _, err = reopened.Exec(`UPDATE scope_inbox_verification_jobs SET lease_until=0 WHERE id=?`, id); err != nil {
		t.Fatal(err)
	}
	recovered, err := next.acquireLease(context.Background())
	if err != nil || recovered.token <= lease.token {
		t.Fatal("reopen token ABA", recovered, lease, err)
	}
	defer next.releaseLease(recovered)
	var phase string
	if err = reopened.QueryRow(`SELECT phase FROM scope_inbox_verification_jobs WHERE id=?`, id).Scan(&phase); err != nil || phase != "bindings" {
		t.Fatal("durable progress lost", phase, err)
	}
	for i := 0; i < 20; i++ {
		out, err := next.verifySlice(context.Background(), recovered)
		if err != nil {
			t.Fatal(err)
		}
		if out.Complete {
			return
		}
	}
	t.Fatal("reopened comparison did not complete")
}

type observingInboxDriver struct {
	base             driver.Driver
	started, proceed chan struct{}
	once             sync.Once
}
type observingInboxConn struct {
	driver.Conn
	observer *observingInboxDriver
}

func (d *observingInboxDriver) Open(name string) (driver.Conn, error) {
	c, e := d.base.Open(name)
	if e != nil {
		return nil, e
	}
	return &observingInboxConn{Conn: c, observer: d}, nil
}
func (c *observingInboxConn) BeginTx(ctx context.Context, o driver.TxOptions) (driver.Tx, error) {
	return c.Conn.(driver.ConnBeginTx).BeginTx(ctx, o)
}
func (c *observingInboxConn) ExecContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Result, error) {
	return c.Conn.(driver.ExecerContext).ExecContext(ctx, q, args)
}
func (c *observingInboxConn) QueryContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	if strings.Contains(q, `SELECT legacy.id FROM (`) {
		c.observer.once.Do(func() { close(c.observer.started) })
		select {
		case <-c.observer.proceed:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return c.Conn.(driver.QueryerContext).QueryContext(ctx, q, args)
}

func TestInboxVerificationOracleDoesNotHoldWriterAndRejectsMixedEpochs(t *testing.T) {
	db, _, id := verificationWorkerFixture(t, `INSERT INTO derived_inbox_items(id,thread_id,category,trigger_at,generated_at,data_json) VALUES('hidden','missing','ask','2026-10-07T10:00:00Z','2026-10-07T10:00:00Z','{"recipient_actor_id":"someone-else"}');`)
	if _, err := db.Exec(`UPDATE scope_inbox_verification_jobs SET phase='canonical' WHERE id=?`, id); err != nil {
		t.Fatal(err)
	}
	var seq int
	var dbName, path string
	if err := db.QueryRow(`PRAGMA database_list`).Scan(&seq, &dbName, &path); err != nil {
		t.Fatal(err)
	}
	name, err := opaque()
	if err != nil {
		t.Fatal(err)
	}
	d := &observingInboxDriver{started: make(chan struct{}), proceed: make(chan struct{})}
	uri := url.URL{Scheme: "file", Path: path, RawQuery: "_txlock=immediate&_pragma=journal_mode(WAL)"}
	registered, err := sql.Open("sqlite", uri.String())
	if err != nil {
		t.Fatal(err)
	}
	d.base = registered.Driver()
	registered.Close()
	sql.Register(name, d)
	observed, err := sql.Open(name, uri.String())
	if err != nil {
		t.Fatal(err)
	}
	defer observed.Close()
	r := workerRunner(t, New(observed), id, 0)
	lease, err := r.acquireLease(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer r.releaseLease(lease)
	result := make(chan error, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _, err := r.verifySlice(ctx, lease); result <- err }()
	defer func() {
		cancel()
		select {
		case <-result:
		case <-time.After(3 * time.Second):
			t.Error("oracle not joined")
		}
	}()
	select {
	case <-d.started:
	case <-time.After(3 * time.Second):
		t.Fatal("oracle never started")
	}
	writer, err := db.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	if _, err = writer.ExecContext(context.Background(), `PRAGMA busy_timeout=0`); err != nil {
		t.Fatal(err)
	}
	_, writeErr := writer.ExecContext(context.Background(), `UPDATE derived_inbox_items SET generated_at=generated_at WHERE id='hidden'`)
	close(d.proceed)
	if writeErr != nil {
		t.Fatal("legacy oracle held writer lock", writeErr)
	}
	err = <-result
	// Keep a result for the joining defer without a second receive blocking.
	result <- err
	if !errors.Is(err, ErrInboxVerificationStale) {
		t.Fatal("mixed read/write snapshots accepted", err)
	}
	var receipts int
	if err = db.QueryRow(`SELECT count(*) FROM scope_inbox_comparison_receipts`).Scan(&receipts); err != nil {
		t.Fatal(err)
	}
	if receipts != 0 {
		t.Fatal("concurrent canonical change minted receipt")
	}
}

func workerRunner(t *testing.T, s *Store, id string, duration time.Duration) *inboxVerificationRunner {
	t.Helper()
	opts := DefaultInboxVerificationWorkerOptions()
	if duration > 0 {
		opts.SliceDuration = duration
	}
	r, err := newInboxVerificationRunner(s, id, opts)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestInboxVerificationWorkerLeaseTakeoverAndABA(t *testing.T) {
	db, s, id := verificationWorkerFixture(t, "")
	ctx := context.Background()
	r1, r2 := workerRunner(t, s, id, 0), workerRunner(t, s, id, 0)
	a, err := r1.acquireLease(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = r2.acquireLease(ctx); !errors.Is(err, ErrInboxVerificationLease) {
		t.Fatal("live lease taken", err)
	}
	b, err := r1.acquireLease(ctx)
	if err != nil || b.token <= a.token {
		t.Fatal("renewal did not fence old handle", a, b, err)
	}
	if _, err = r1.verifySlice(ctx, a); !errors.Is(err, ErrInboxVerificationLease) {
		t.Fatal("old renewal handle admitted", err)
	}
	r1.releaseLease(a)
	if _, err = r2.acquireLease(ctx); !errors.Is(err, ErrInboxVerificationLease) {
		t.Fatal("old release cleared newer lease", err)
	}
	if _, err = db.ExecContext(ctx, `UPDATE scope_inbox_verification_jobs SET lease_until=0 WHERE id=?`, id); err != nil {
		t.Fatal(err)
	}
	c, err := r2.acquireLease(ctx)
	if err != nil || c.token <= b.token {
		t.Fatal("takeover token reused", b, c, err)
	}
	if _, err = r1.verifySlice(ctx, b); !errors.Is(err, ErrInboxVerificationLease) {
		t.Fatal("expired owner admitted", err)
	}
	r1.releaseLease(b)
	if _, err = r2.verifySlice(ctx, c); err != nil {
		t.Fatal("successor damaged by old release", err)
	}
	r2.releaseLease(c)
	if _, err = db.ExecContext(ctx, `UPDATE scope_inbox_verification_jobs SET lease_token=9223372036854775807 WHERE id=?`, id); err != nil {
		t.Fatal(err)
	}
	if _, err = r1.acquireLease(ctx); !errors.Is(err, ErrInboxVerificationLease) {
		t.Fatal("overflowed token admitted", err)
	}
}

func TestInboxVerificationWorkerLateExpiryRollsBackCheckpointAndReceipt(t *testing.T) {
	for _, receipt := range []bool{false, true} {
		t.Run(map[bool]string{false: "checkpoint", true: "receipt"}[receipt], func(t *testing.T) {
			q := `INSERT INTO scope_domains VALUES('public','active',1); INSERT INTO scope_memberships VALUES('owner','public','owner',1);
 CREATE TRIGGER expire_comparison AFTER INSERT ON scope_inbox_verification_scopes BEGIN UPDATE scope_inbox_verification_jobs SET lease_until=0 WHERE id=NEW.job_id; END;`
			if receipt {
				q = `CREATE TRIGGER expire_comparison AFTER INSERT ON scope_inbox_comparison_receipts BEGIN UPDATE scope_inbox_verification_jobs SET lease_until=0 WHERE id=NEW.job_id; END;`
			}
			db, s, id := verificationWorkerFixture(t, q)
			phase := "directory"
			if receipt {
				phase = "missing_counters"
				if _, err := db.Exec(`UPDATE scope_inbox_verification_jobs SET phase=? WHERE id=?`, phase, id); err != nil {
					t.Fatal(err)
				}
			}
			r := workerRunner(t, s, id, 0)
			lease, err := r.acquireLease(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			defer r.releaseLease(lease)
			out, err := r.verifySlice(context.Background(), lease)
			if !errors.Is(err, ErrInboxVerificationLease) || out.Complete || out.Examined != 0 {
				t.Fatal("late lease loss survived", out, err)
			}
			var scopes, receipts, examined int
			var got, checkpoint string
			if err = db.QueryRow(`SELECT phase,checkpoint,examined,(SELECT count(*) FROM scope_inbox_verification_scopes),(SELECT count(*) FROM scope_inbox_comparison_receipts) FROM scope_inbox_verification_jobs WHERE id=?`, id).Scan(&got, &checkpoint, &examined, &scopes, &receipts); err != nil {
				t.Fatal(err)
			}
			if got != phase || checkpoint != "{}" || examined != 0 || scopes != 0 || receipts != 0 {
				t.Fatal("partial commit", got, checkpoint, examined, scopes, receipts)
			}
		})
	}
}

func TestInboxVerificationWorkerMismatchRollsBackPartialPhase(t *testing.T) {
	db, s, id := verificationWorkerFixture(t, `PRAGMA ignore_check_constraints=ON; INSERT INTO scope_domains VALUES('a','active',1),('b','active',1); INSERT INTO scope_memberships VALUES('owner','a','owner',1),('owner','b','invalid',1);`)
	_, err := s.RunInboxVerificationSlice(context.Background(), id)
	if !errors.Is(err, ErrInboxVerificationMismatch) {
		t.Fatal(err)
	}
	var n, examined, finished int
	var failure, checkpoint string
	if err = db.QueryRow(`SELECT failure,checkpoint,examined,finished_at,(SELECT count(*) FROM scope_inbox_verification_scopes) FROM scope_inbox_verification_jobs WHERE id=?`, id).Scan(&failure, &checkpoint, &examined, &finished, &n); err != nil {
		t.Fatal(err)
	}
	if failure != "mismatch" || checkpoint != "{}" || examined != 0 || finished <= 0 || n != 0 {
		t.Fatal("partial mismatch committed", failure, checkpoint, examined, finished, n)
	}
}

func TestInboxVerificationWorkerDeadlineAndBusyTimeout(t *testing.T) {
	db, s, id := verificationWorkerFixture(t, `INSERT INTO scope_domains VALUES('public','active',1); INSERT INTO scope_memberships VALUES('owner','public','owner',1);
 CREATE TRIGGER slow_comparison AFTER INSERT ON scope_inbox_verification_scopes BEGIN
 UPDATE scope_inbox_verification_jobs SET examined=(WITH RECURSIVE seq(n) AS (SELECT 1 UNION ALL SELECT n+1 FROM seq WHERE n<100000000) SELECT sum(n) FROM seq) WHERE id=NEW.job_id; END;`)
	r := workerRunner(t, s, id, 20*time.Millisecond)
	lease, err := r.acquireLease(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer r.releaseLease(lease)
	started := time.Now()
	out, err := r.verifySlice(context.Background(), lease)
	if !errors.Is(err, context.DeadlineExceeded) || out.Complete {
		t.Fatal("deadline admitted", out, err)
	}
	if time.Since(started) > 2*time.Second {
		t.Fatal("deadline failed to interrupt worker SQL")
	}
	var n, examined int
	if err = db.QueryRow(`SELECT examined,(SELECT count(*) FROM scope_inbox_verification_scopes) FROM scope_inbox_verification_jobs WHERE id=?`, id).Scan(&examined, &n); err != nil {
		t.Fatal(err)
	}
	if examined != 0 || n != 0 {
		t.Fatal("deadline advanced checkpoint", examined, n)
	}
	// Check the exact pooled connection after a normal chunk as well: cancelled
	// connections may be discarded by database/sql instead of restored.
	conn, err := db.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = conn.ExecContext(context.Background(), `PRAGMA busy_timeout=4321`); err != nil {
		t.Fatal(err)
	}
	conn.Close()
	db.SetMaxOpenConns(1)
	tx, cleanup, err := beginInboxVerificationChunk(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	var timeout int
	if err = tx.QueryRow(`PRAGMA busy_timeout`).Scan(&timeout); err != nil || timeout != 0 {
		t.Fatal("worker timeout", timeout, err)
	}
	cleanup()
	if err = db.QueryRow(`PRAGMA busy_timeout`).Scan(&timeout); err != nil || timeout != 4321 {
		t.Fatal("serving timeout changed", timeout, err)
	}
}

func TestInboxVerificationPruningRejectsOldRunnerAndRetainsLiveLease(t *testing.T) {
	db, s, id := verificationWorkerFixture(t, "")
	r := workerRunner(t, s, id, 0)
	lease, err := r.acquireLease(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// Model a completed old comparison with enough temporary rows to require
	// two pruning slices. The live lease must protect its evidence first.
	if _, err = db.Exec(`UPDATE scope_inbox_verification_jobs SET phase='done',finished_at=1 WHERE id=?;
 INSERT INTO scope_inbox_verification_seen VALUES(?,1,'public','owner',1),(?,2,'public','owner',1)`, id, id, id); err != nil {
		t.Fatal(err)
	}
	if _, err = s.PruneInboxVerification(context.Background(), time.Now(), 1); !errors.Is(err, ErrInboxVerificationLease) {
		t.Fatal("live evidence pruned", err)
	}
	r.releaseLease(lease)
	progress, err := s.PruneInboxVerification(context.Background(), time.Now(), 1)
	if err != nil || progress.Deleted != 1 || progress.Complete {
		t.Fatal(progress, err)
	}
	if _, err = r.acquireLease(context.Background()); !errors.Is(err, ErrInboxVerificationCancelled) {
		t.Fatal("retired comparison reacquired", err)
	}
	if _, err = r.verifySlice(context.Background(), lease); !errors.Is(err, ErrInboxVerificationLease) {
		t.Fatal("retired lease resumed", err)
	}
	var finished, remaining int
	if err = db.QueryRow(`SELECT finished_at,(SELECT count(*) FROM scope_inbox_verification_seen WHERE job_id=?) FROM scope_inbox_verification_jobs WHERE id=?`, id, id).Scan(&finished, &remaining); err != nil {
		t.Fatal(err)
	}
	if finished != 1 || remaining != 1 {
		t.Fatal("retired job re-dated or changed", finished, remaining)
	}
}
