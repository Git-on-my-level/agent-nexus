package server

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"modernc.org/sqlite"
	"net/http"
	"net/url"
	"os"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/storage"
	"agent-nexus-core/internal/testutil/perfguard"
)

var compactAnswerGateID atomic.Uint64

func TestCompactMigrationConcurrentAnswers(t *testing.T) {
	requireIntegrationTest(t)
	compactMigrationAnswers(t, false)
}

func TestPerformanceCompactMigrationConcurrentAnswers(t *testing.T) {
	requirePerformanceTest(t)
	compactMigrationAnswers(t, true)
}

// Exercise fresh answer commits (not replay hits) while the real migration
// competes with an independent writer. The scale variant uses the shared
// canonical corpus rather than padding the file with unrelated bytes.
func compactMigrationAnswers(t *testing.T, scale bool) {
	var capture *perfguard.Capture
	var phase atomic.Int32
	entered := []chan struct{}{make(chan struct{}), make(chan struct{})}
	began := []chan struct{}{make(chan struct{}), make(chan struct{})}
	var admitted [2]sync.Once
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
	defer cancel()
	var functions [2]string
	for i := 0; i < 2; i++ {
		index := i
		function := fmt.Sprintf("compact_answer_gate_%d", compactAnswerGateID.Add(1))
		functions[i] = function
		var fired atomic.Bool
		if err := sqlite.RegisterScalarFunction(function, 0, func(*sqlite.FunctionContext, []driver.Value) (driver.Value, error) {
			if !fired.CompareAndSwap(false, true) {
				return int64(0), nil
			}
			phase.Store(int32(index + 1))
			close(entered[index])
			defer phase.Store(0)
			timer := time.NewTimer(750 * time.Millisecond)
			defer timer.Stop()
			select {
			case <-began[index]:
				return int64(0), nil
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-timer.C:
				return nil, fmt.Errorf("answer never reached writer admission")
			}
		}); err != nil {
			t.Fatal(err)
		}

	}
	env := newAuthIntegrationEnv(t, authIntegrationOptions{servingPool: func(w *storage.Workspace) *sql.DB {
		pool, c, err := perfguard.Open("file:" + w.Layout().DatabasePath + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(20000)&_txlock=immediate")
		if err != nil {
			t.Fatal(err)
		}
		capture = c
		t.Cleanup(func() { pool.Close() })
		return pool
	}})
	capture.BeforeBegin(func(o driver.TxOptions) {
		if p := phase.Load(); p > 0 && !o.ReadOnly {
			admitted[p-1].Do(func() { close(began[p-1]) })
		}
	})
	db := env.workspace.DB()
	owner := seedHumanPrincipalForLockoutTest(t, ctx, db, "migration-owner", "migration-owner-actor", "migration-owner", "migration-owner-token")
	if scale {
		if err := perfguard.Seed(ctx, db, env.workspace.Layout().ArtifactContentDir, owner.ActorID, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)); err != nil {
			t.Fatal(err)
		}
	}
	store := env.primitiveStore.(*primitives.Store)
	ids := make([]string, 25)
	for i := range ids {
		ask, _ := seedInboxResponsePrivacyAsk(t, store, owner.ActorID, "public")
		ids[i] = ask.ID
	}
	if _, err := db.ExecContext(ctx, `CREATE TABLE migration_writer_fixture(id INTEGER PRIMARY KEY, value INTEGER); INSERT INTO migration_writer_fixture VALUES(1,0)`); err != nil {
		t.Fatal(err)
	}
	var busy, pages, copied int
	if err := db.QueryRowContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`).Scan(&busy, &pages, &copied); err != nil || busy != 0 {
		t.Fatalf("checkpoint busy=%d err=%v", busy, err)
	}
	info, err := os.Stat(env.workspace.Layout().DatabasePath)
	if err != nil {
		t.Fatal(err)
	}
	var edges int
	if err = db.QueryRowContext(ctx, `SELECT count(*) FROM resource_access_exact_edges`).Scan(&edges); err != nil {
		t.Fatal(err)
	}
	t.Logf("synthetic db_bytes=%d access_edges=%d", info.Size(), edges)
	for i := 0; i < 2; i++ {
		function := functions[i]
		var q string
		if i == 0 {
			if _, err := db.ExecContext(ctx, `CREATE TABLE resource_access_compact_job(singleton INTEGER PRIMARY KEY CHECK(singleton=1),cursor INTEGER,done INTEGER NOT NULL CHECK(done IN (0,1)))`); err != nil {
				t.Fatal(err)
			}
			q = `CREATE TRIGGER answer_install_gate BEFORE INSERT ON resource_access_compact_job BEGIN SELECT ` + function + `(); END`
		} else {
			q = `CREATE TRIGGER answer_cutover_gate BEFORE UPDATE OF reader ON resource_access_compact_control WHEN OLD.reader=0 AND NEW.reader=1 BEGIN SELECT ` + function + `(); END`
		}
		if _, err := db.ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	held, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Rollback()
	busyObserved, releaseRetry, releaseCutover := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var busyOnce sync.Once
	var releaseOnce sync.Once
	var cutoverOnce sync.Once
	defer func() {
		releaseOnce.Do(func() { close(releaseRetry) })
		cutoverOnce.Do(func() { close(releaseCutover) })
	}()
	workerCtx := storage.WithCompactAccessMigrationObserver(ctx, func(stage string, err error) {
		if stage == "shadow_install" && err != nil {
			busyOnce.Do(func() {
				close(busyObserved)
				select {
				case <-releaseRetry:
				case <-ctx.Done():
				}
			})
		}
		if stage == "verification" && err == nil {
			select {
			case <-releaseCutover:
			case <-ctx.Done():
			}
		}
	})
	type outcome struct {
		proof   storage.CompactAccessVerification
		err     error
		elapsed time.Duration
	}
	result := make(chan outcome, 1)
	finished := make(chan struct{})
	start := time.Now()
	go func() {
		defer close(finished)
		env.workspace.RunCompactAccessMigration(workerCtx, func(p storage.CompactAccessVerification, e error) { result <- outcome{p, e, time.Since(start)} })
	}()
	defer func() {
		cancel()
		releaseOnce.Do(func() { close(releaseRetry) })
		cutoverOnce.Do(func() { close(releaseCutover) })
		<-finished
	}()
	select {
	case <-busyObserved:
	case <-finished:
		t.Fatal("migration exited before busy observation")
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if err = held.Rollback(); err != nil {
		t.Fatal(err)
	}
	releaseOnce.Do(func() { close(releaseRetry) })
	select {
	case <-entered[0]:
	case <-finished:
		t.Fatal("install never acquired writer")
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	writerCtx, stopWriter := context.WithCancel(ctx)
	writerDone := make(chan error, 1)
	go func() {
		ticker := time.NewTicker(15 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-writerCtx.Done():
				writerDone <- nil
				return
			case <-ticker.C:
			}
			tx, e := db.BeginTx(writerCtx, nil)
			if e == nil {
				_, e = tx.ExecContext(writerCtx, `UPDATE migration_writer_fixture SET value=value+1 WHERE id=1`)
				if e == nil {
					e = tx.Commit()
				}
				tx.Rollback()
			}
			if e != nil {
				if writerCtx.Err() != nil {
					e = nil
				}
				writerDone <- e
				return
			}
		}
	}()
	defer func() {
		stopWriter()
		if e := <-writerDone; e != nil {
			t.Errorf("background writer: %v", e)
		}
	}()
	samples := make([]time.Duration, 0, len(ids))
	for i, id := range ids {
		if i == len(ids)-1 {
			cutoverOnce.Do(func() { close(releaseCutover) })
			select {
			case <-entered[1]:
			case <-finished:
				t.Fatal("cutover never acquired writer")
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
		}
		began := time.Now()
		status, body := hostHTTP(t, http.MethodPost, env.server.URL+"/inbox/"+url.PathEscape(id)+"/respond", owner.AccessToken, map[string]any{"idempotency_key": fmt.Sprintf("migration-answer-%d", i), "response_text": "Approved", "outcome": "answered", "notify_mode": "none"})
		samples = append(samples, time.Since(began))
		if status != http.StatusCreated {
			t.Fatalf("fresh answer %d: %d %#v", i, status, body)
		}
	}
	for _, signal := range began {
		select {
		case <-signal:
		default:
			t.Fatal("answer did not enter BEGIN during migration writer lease")
		}
	}
	sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
	p95 := samples[(len(samples)*95-1)/100]
	maximum := samples[len(samples)-1]
	t.Logf("answer_samples=%d gated_writer_overlaps=2 answer_p95=%s answer_max=%s", len(samples), p95, maximum)
	if maximum > 2*time.Second {
		t.Fatalf("answer max=%s exceeds 2s budget", maximum)
	}
	select {
	case <-finished:
	case <-ctx.Done():
		t.Fatal("migration failed to complete")
	}
	if len(result) != 1 {
		t.Fatalf("final outcomes=%d", len(result))
	}
	r := <-result
	if r.err != nil || r.proof.Rows == 0 {
		t.Fatalf("certification failed: %+v", r)
	}
	var reader, claims, writes int
	if err = db.QueryRowContext(ctx, `SELECT reader,(SELECT count(*) FROM human_attention_response_claims WHERE response_json<>'{}'),(SELECT value FROM migration_writer_fixture WHERE id=1) FROM resource_access_compact_control`).Scan(&reader, &claims, &writes); err != nil || reader != 1 || claims != len(ids) || writes == 0 {
		t.Fatalf("reader=%d claims=%d writes=%d err=%v", reader, claims, writes, err)
	}
	t.Logf("certification=%s rows=%d answer_samples=%d gated_writer_overlaps=2 answer_p95=%s answer_max=%s background_writes=%d", r.elapsed, r.proof.Rows, len(samples), p95, maximum, writes)
}
