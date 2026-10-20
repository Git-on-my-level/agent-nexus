package auth

import (
	"context"
	"fmt"
	"os"
	"strconv"
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
	purgeStart := time.Now()
	result, err := store.PurgeExpiredAuthRows(ctx, AuthRowPurgeOptions{})
	if err != nil {
		t.Fatalf("purge: %v", err)
	}
	elapsed := time.Since(purgeStart)
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
