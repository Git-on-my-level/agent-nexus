package server

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"sort"
	"testing"
	"time"

	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/storage"
	"agent-nexus-core/internal/testutil/perfguard"
)

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
	env := newAuthIntegrationEnv(t, authIntegrationOptions{})
	db := env.workspace.DB()
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
	defer cancel()
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
	held, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Rollback()
	if err = env.workspace.InitializeCompactAccessShadow(ctx); err == nil {
		t.Fatal("install unexpectedly acquired held writer")
	}
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
		env.workspace.RunCompactAccessMigration(ctx, func(p storage.CompactAccessVerification, e error) { result <- outcome{p, e, time.Since(start)} })
	}()
	defer func() { cancel(); <-finished }()
	// The default startup jitter is 1–2s: guarantee at least one busy attempt.
	select {
	case <-finished:
		t.Fatal("migration exited under startup writer contention")
	case <-time.After(2500 * time.Millisecond):
	}
	if err = held.Rollback(); err != nil {
		t.Fatal(err)
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
	overlapping := 0
	for i, id := range ids {
		select {
		case <-finished:
		default:
			overlapping++
		}
		began := time.Now()
		status, body := hostHTTP(t, http.MethodPost, env.server.URL+"/inbox/"+url.PathEscape(id)+"/respond", owner.AccessToken, map[string]any{"idempotency_key": fmt.Sprintf("migration-answer-%d", i), "response_text": "Approved", "outcome": "answered", "notify_mode": "none"})
		samples = append(samples, time.Since(began))
		if status != http.StatusCreated {
			t.Fatalf("fresh answer %d: %d %#v", i, status, body)
		}
	}
	if overlapping == 0 {
		t.Fatal("answers did not overlap migration")
	}
	sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
	p95 := samples[(len(samples)*95-1)/100]
	t.Logf("answer_samples=%d overlapping=%d answer_p95=%s", len(samples), overlapping, p95)
	if p95 > 2*time.Second {
		t.Fatalf("answer p95=%s exceeds 2s budget", p95)
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
	t.Logf("certification=%s rows=%d answer_samples=%d overlapping=%d answer_p95=%s background_writes=%d", r.elapsed, r.proof.Rows, len(samples), overlapping, p95, writes)
}
