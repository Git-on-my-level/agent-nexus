package auth

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"agent-nexus-core/internal/storage"
)

// TestPerformanceAuthRowIndexesAndPurge measures building migration 110's
// indexes over a large auth table and one default purge run over its
// backlog. Sizes default to 2,000,000 access tokens and 200,000 refresh
// sessions; ANX_AUTH_SCALE_ACCESS_TOKENS and ANX_AUTH_SCALE_REFRESH_SESSIONS
// override them.
func TestPerformanceAuthRowIndexesAndPurge(t *testing.T) {
	if os.Getenv("ANX_PERFORMANCE_TEST") == "" {
		t.Skip("set ANX_PERFORMANCE_TEST=1")
	}
	accessRows := scaleEnvInt(t, "ANX_AUTH_SCALE_ACCESS_TOKENS", 2_000_000)
	refreshRows := scaleEnvInt(t, "ANX_AUTH_SCALE_REFRESH_SESSIONS", 200_000)
	const principals = 2_000

	ctx := context.Background()
	workspace, err := storage.InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatalf("initialize workspace: %v", err)
	}
	defer workspace.Close()
	db := workspace.DB()

	indexes := []string{
		`CREATE INDEX IF NOT EXISTS idx_auth_access_tokens_expires_at ON auth_access_tokens(expires_at)`,
		`CREATE INDEX IF NOT EXISTS idx_auth_access_tokens_agent_created ON auth_access_tokens(agent_id, created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_auth_refresh_sessions_expires_at ON auth_refresh_sessions(expires_at)`,
		`CREATE INDEX IF NOT EXISTS idx_auth_refresh_sessions_revoked_at ON auth_refresh_sessions(revoked_at) WHERE revoked_at IS NOT NULL`,
	}
	for _, name := range []string{"idx_auth_access_tokens_expires_at", "idx_auth_access_tokens_agent_created", "idx_auth_refresh_sessions_expires_at", "idx_auth_refresh_sessions_revoked_at"} {
		if _, err := db.ExecContext(ctx, `DROP INDEX `+name); err != nil {
			t.Fatalf("drop %s: %v", name, err)
		}
	}

	// Seed as a workspace that has issued tokens for 30 days without a purge.
	seedStart := time.Now()
	now := time.Now().UTC()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin seed: %v", err)
	}
	nowText := now.Format(time.RFC3339Nano)
	for p := 0; p < principals; p++ {
		id := fmt.Sprintf("scale-%04d", p)
		if _, err := tx.ExecContext(ctx, `INSERT INTO actors(id, display_name, tags_json, created_at, metadata_json) VALUES (?, ?, '[]', ?, '{}')`, "actor-"+id, id, nowText); err != nil {
			t.Fatalf("seed actor: %v", err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO agents(id, username, actor_id, created_at, updated_at, metadata_json) VALUES (?, ?, ?, ?, ?, '{}')`, "agent-"+id, "user."+id, "actor-"+id, nowText, nowText); err != nil {
			t.Fatalf("seed agent: %v", err)
		}
	}
	access, err := tx.PrepareContext(ctx, `INSERT INTO auth_access_tokens(id, agent_id, token_hash, created_at, expires_at) VALUES (?, ?, ?, ?, ?)`)
	if err != nil {
		t.Fatalf("prepare access: %v", err)
	}
	span := 30 * 24 * time.Hour
	for i := 0; i < accessRows; i++ {
		created := now.Add(-span / time.Duration(accessRows) * time.Duration(i))
		if _, err := access.ExecContext(ctx, "access_scale_"+strconv.Itoa(i), fmt.Sprintf("agent-scale-%04d", i%principals), fmt.Sprintf("%064x", i),
			created.Format(time.RFC3339Nano), created.Add(15*time.Minute).Format(time.RFC3339Nano)); err != nil {
			t.Fatalf("seed access token: %v", err)
		}
	}
	access.Close()
	refresh, err := tx.PrepareContext(ctx, `INSERT INTO auth_refresh_sessions(id, agent_id, token_hash, created_at, expires_at, revoked_at) VALUES (?, ?, ?, ?, ?, ?)`)
	if err != nil {
		t.Fatalf("prepare refresh: %v", err)
	}
	for i := 0; i < refreshRows; i++ {
		created := now.Add(-span / time.Duration(refreshRows) * time.Duration(i))
		var revoked any
		if i%2 == 0 { // rotated
			revoked = created.Add(15 * time.Minute).Format(time.RFC3339Nano)
		}
		if _, err := refresh.ExecContext(ctx, "refresh_scale_"+strconv.Itoa(i), fmt.Sprintf("agent-scale-%04d", i%principals), fmt.Sprintf("r%063x", i),
			created.Format(time.RFC3339Nano), created.Add(30*24*time.Hour).Format(time.RFC3339Nano), revoked); err != nil {
			t.Fatalf("seed refresh session: %v", err)
		}
	}
	refresh.Close()
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit seed: %v", err)
	}
	t.Logf("seeded %d access tokens and %d refresh sessions in %s", accessRows, refreshRows, time.Since(seedStart).Round(time.Millisecond))

	buildStart := time.Now()
	for _, statement := range indexes {
		start := time.Now()
		if _, err := db.ExecContext(ctx, statement); err != nil {
			t.Fatalf("build index: %v", err)
		}
		t.Logf("%s: %s", statement, time.Since(start).Round(time.Millisecond))
	}
	build := time.Since(buildStart)
	t.Logf("migration 110 index build total: %s", build.Round(time.Millisecond))
	if build > 90*time.Second {
		t.Fatalf("index build %s exceeds the 90-second upgrade budget", build)
	}

	var eligible int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM auth_access_tokens WHERE expires_at < ?`, now.Add(-DefaultExpiredAuthRowRetention).Format(time.RFC3339Nano)).Scan(&eligible); err != nil {
		t.Fatalf("count eligible: %v", err)
	}
	var sample string
	_ = db.QueryRowContext(ctx, `SELECT expires_at FROM auth_access_tokens ORDER BY expires_at LIMIT 1`).Scan(&sample)
	t.Logf("expired access tokens past retention: %d (oldest %s)", eligible, sample)

	store := NewStore(db)

	// A writer issuing tokens, as live traffic does, measured alone and then
	// while the purge drains the backlog.
	baseline := measureAuthWrites(ctx, t, db, "baseline", 3*time.Second, nil)
	purgeDone := make(chan struct{})
	var result AuthRowPurgeResult
	var purgeErr error
	var elapsed time.Duration
	go func() {
		defer close(purgeDone)
		purgeStart := time.Now()
		result, purgeErr = store.PurgeExpiredAuthRows(ctx, AuthRowPurgeOptions{})
		elapsed = time.Since(purgeStart)
	}()
	during := measureAuthWrites(ctx, t, db, "during-purge", 0, purgeDone)
	<-purgeDone
	if purgeErr != nil {
		t.Fatalf("purge: %v", purgeErr)
	}
	t.Logf("write latency baseline p50=%s p99=%s max=%s busy=%d; during purge p50=%s p99=%s max=%s busy=%d",
		baseline.p50, baseline.p99, baseline.max, baseline.busy, during.p50, during.p99, during.max, during.busy)
	if during.busy > 0 {
		t.Fatalf("concurrent writes failed with SQLITE_BUSY %d times during the purge", during.busy)
	}
	t.Logf("one default purge run: %d access tokens, %d refresh sessions, truncated=%t, in %s (%.0f rows/s)",
		result.AccessTokens, result.RefreshSessions, result.Truncated, elapsed.Round(time.Millisecond),
		float64(result.AccessTokens+result.RefreshSessions)/elapsed.Seconds())
	if result.RefreshSessions == 0 {
		t.Fatal("refresh sessions starved by the access-token backlog")
	}
	if elapsed > 5*time.Minute {
		t.Fatalf("one purge run took %s, over its five-minute timeout", elapsed)
	}
}

func scaleEnvInt(t *testing.T, name string, fallback int) int {
	t.Helper()
	raw := os.Getenv(name)
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		t.Fatalf("%s must be a positive integer", name)
	}
	return value
}

type authWriteLatency struct {
	p50, p99, max time.Duration
	busy          int
}

// measureAuthWrites inserts a live access token every 2ms for duration, or
// until done closes, and reports the latency distribution.
func measureAuthWrites(ctx context.Context, t *testing.T, db *sql.DB, label string, duration time.Duration, done <-chan struct{}) authWriteLatency {
	t.Helper()
	var samples []time.Duration
	busy := 0
	deadline := time.Now().Add(duration)
	for i := 0; ; i++ {
		if done != nil {
			select {
			case <-done:
				goto report
			default:
			}
		} else if time.Now().After(deadline) {
			break
		}
		now := time.Now().UTC()
		start := time.Now()
		_, err := db.ExecContext(ctx, `INSERT INTO auth_access_tokens(id, agent_id, token_hash, created_at, expires_at) VALUES (?, ?, ?, ?, ?)`,
			fmt.Sprintf("access_live_%s_%d", label, i), "agent-scale-0000", fmt.Sprintf("live-%s-%d", label, i),
			now.Format(time.RFC3339Nano), now.Add(15*time.Minute).Format(time.RFC3339Nano))
		samples = append(samples, time.Since(start))
		if err != nil {
			if strings.Contains(err.Error(), "busy") || strings.Contains(err.Error(), "locked") {
				busy++
			} else {
				t.Fatalf("%s write: %v", label, err)
			}
		}
		time.Sleep(2 * time.Millisecond)
	}
report:
	sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
	if len(samples) == 0 {
		return authWriteLatency{busy: busy}
	}
	return authWriteLatency{
		p50:  samples[len(samples)/2].Round(time.Microsecond),
		p99:  samples[len(samples)*99/100].Round(time.Microsecond),
		max:  samples[len(samples)-1].Round(time.Microsecond),
		busy: busy,
	}
}
