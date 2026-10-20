package auth

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"agent-nexus-core/internal/storage"
)

func TestPurgeExpiredAuthRowsKeepsLiveRowsAndNewestAccessToken(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	workspace, err := storage.InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatalf("initialize workspace: %v", err)
	}
	defer workspace.Close()
	db := workspace.DB()
	store := NewStore(db)

	agent, live, err := store.IssueTokenFromWorkspaceHumanGrant(ctx, WorkspaceHumanGrantIdentity{
		Issuer:  "https://issuer.example",
		Subject: "person-1",
		JTI:     "jti-purge-1",
	}, GrantExchangeOptions{})
	if err != nil {
		t.Fatalf("exchange grant: %v", err)
	}

	now := time.Now().UTC()
	old := now.Add(-3 * DefaultExpiredAuthRowRetention)
	recent := now.Add(-time.Hour)
	insertAccess := func(id string, created time.Time, expires time.Time, agentID string) {
		t.Helper()
		if _, err := db.ExecContext(ctx, `INSERT INTO auth_access_tokens(id, agent_id, token_hash, created_at, expires_at) VALUES (?, ?, ?, ?, ?)`,
			id, agentID, "hash-"+id, created.Format(time.RFC3339Nano), expires.Format(time.RFC3339Nano)); err != nil {
			t.Fatalf("insert access token %s: %v", id, err)
		}
	}
	insertRefresh := func(id string, expires time.Time, revoked *time.Time) {
		t.Helper()
		var revokedAt any
		if revoked != nil {
			revokedAt = revoked.Format(time.RFC3339Nano)
		}
		if _, err := db.ExecContext(ctx, `INSERT INTO auth_refresh_sessions(id, agent_id, token_hash, created_at, expires_at, revoked_at) VALUES (?, ?, ?, ?, ?, ?)`,
			id, agent.AgentID, "hash-"+id, old.Add(-time.Hour).Format(time.RFC3339Nano), expires.Format(time.RFC3339Nano), revokedAt); err != nil {
			t.Fatalf("insert refresh session %s: %v", id, err)
		}
	}

	// Long-expired access tokens are purged in favor of the newer live one.
	for _, id := range []string{"access_old_1", "access_old_2", "access_old_3"} {
		insertAccess(id, old.Add(-time.Hour), old, agent.AgentID)
	}
	// Recently expired: inside the retention window, kept.
	insertAccess("access_recent", recent.Add(-15*time.Minute), recent, agent.AgentID)

	// A second principal whose only token expired long ago keeps it, so its
	// last-seen time survives.
	other, _, err := store.IssueTokenFromWorkspaceHumanGrant(ctx, WorkspaceHumanGrantIdentity{
		Issuer:  "https://issuer.example",
		Subject: "person-2",
		JTI:     "jti-purge-2",
	}, GrantExchangeOptions{AccessOnly: true})
	if err != nil {
		t.Fatalf("exchange second grant: %v", err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE auth_access_tokens SET created_at = ?, expires_at = ? WHERE agent_id = ?`,
		old.Add(-time.Hour).Format(time.RFC3339Nano), old.Format(time.RFC3339Nano), other.AgentID); err != nil {
		t.Fatalf("age second principal token: %v", err)
	}

	revokedLongAgo := old
	revokedRecently := recent
	insertRefresh("refresh_expired", old, nil)
	insertRefresh("refresh_revoked_old", now.Add(20*24*time.Hour), &revokedLongAgo)
	insertRefresh("refresh_revoked_recent", now.Add(20*24*time.Hour), &revokedRecently)
	insertRefresh("refresh_expired_recent", recent, nil)

	result, err := store.PurgeExpiredAuthRows(ctx, AuthRowPurgeOptions{BatchSize: 2})
	if err != nil {
		t.Fatalf("purge: %v", err)
	}
	if result.AccessTokens != 3 || result.RefreshSessions != 2 || result.Truncated {
		t.Fatalf("unexpected purge result: %#v", result)
	}

	for _, id := range []string{"access_old_1", "access_old_2", "access_old_3"} {
		assertAuthRowCount(t, db, `SELECT COUNT(*) FROM auth_access_tokens WHERE id = ?`, id, 0)
	}
	assertAuthRowCount(t, db, `SELECT COUNT(*) FROM auth_access_tokens WHERE id = ?`, "access_recent", 1)
	assertAuthRowCount(t, db, `SELECT COUNT(*) FROM auth_access_tokens WHERE agent_id = ?`, other.AgentID, 1)
	for _, id := range []string{"refresh_expired", "refresh_revoked_old"} {
		assertAuthRowCount(t, db, `SELECT COUNT(*) FROM auth_refresh_sessions WHERE id = ?`, id, 0)
	}
	for _, id := range []string{"refresh_revoked_recent", "refresh_expired_recent"} {
		assertAuthRowCount(t, db, `SELECT COUNT(*) FROM auth_refresh_sessions WHERE id = ?`, id, 1)
	}

	// The live session keeps working end to end.
	if _, err := store.AuthenticateAccessToken(ctx, live.AccessToken); err != nil {
		t.Fatalf("live access token stopped working: %v", err)
	}
	if _, err := store.IssueTokenFromRefresh(ctx, live.RefreshToken); err != nil {
		t.Fatalf("live refresh token stopped working: %v", err)
	}
}

func TestPurgeExpiredAuthRowsStopsAtBatchBudget(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	workspace, err := storage.InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatalf("initialize workspace: %v", err)
	}
	defer workspace.Close()
	db := workspace.DB()
	store := NewStore(db)

	agent, _, err := store.IssueTokenFromWorkspaceHumanGrant(ctx, WorkspaceHumanGrantIdentity{
		Issuer:  "https://issuer.example",
		Subject: "person-budget",
		JTI:     "jti-budget",
	}, GrantExchangeOptions{AccessOnly: true})
	if err != nil {
		t.Fatalf("exchange grant: %v", err)
	}
	old := time.Now().UTC().Add(-3 * DefaultExpiredAuthRowRetention)
	for i := 0; i < 5; i++ {
		id := "access_budget_" + string(rune('a'+i))
		if _, err := db.ExecContext(ctx, `INSERT INTO auth_access_tokens(id, agent_id, token_hash, created_at, expires_at) VALUES (?, ?, ?, ?, ?)`,
			id, agent.AgentID, "hash-"+id, old.Add(-time.Hour).Format(time.RFC3339Nano), old.Format(time.RFC3339Nano)); err != nil {
			t.Fatalf("insert access token: %v", err)
		}
	}

	first, err := store.PurgeExpiredAuthRows(ctx, AuthRowPurgeOptions{BatchSize: 2, MaxBatches: 1})
	if err != nil {
		t.Fatalf("first purge: %v", err)
	}
	if first.AccessTokens != 2 || !first.Truncated {
		t.Fatalf("expected one bounded batch, got %#v", first)
	}
	second, err := store.PurgeExpiredAuthRows(ctx, AuthRowPurgeOptions{BatchSize: 2})
	if err != nil {
		t.Fatalf("second purge: %v", err)
	}
	if second.AccessTokens != 3 || second.Truncated {
		t.Fatalf("expected the rest on the next run, got %#v", second)
	}
}

func TestPurgeExpiredAuthRowsUsesIndexes(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	workspace, err := storage.InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatalf("initialize workspace: %v", err)
	}
	defer workspace.Close()

	for name, query := range map[string]string{
		"access":          purgeExpiredAccessTokensSQL,
		"refresh_expired": purgeExpiredRefreshSessionsSQL,
		"refresh_revoked": purgeRevokedRefreshSessionsSQL,
	} {
		rows, err := workspace.DB().QueryContext(ctx, "EXPLAIN QUERY PLAN "+query, "2026-01-01T00:00:00Z", 10)
		if err != nil {
			t.Fatalf("%s: explain: %v", name, err)
		}
		var plan []string
		for rows.Next() {
			var id, parent, notUsed int
			var detail string
			if err := rows.Scan(&id, &parent, &notUsed, &detail); err != nil {
				t.Fatalf("%s: scan plan: %v", name, err)
			}
			plan = append(plan, detail)
		}
		rows.Close()
		joined := strings.Join(plan, "\n")
		t.Logf("%s plan:\n%s", name, joined)
		for _, line := range plan {
			if strings.HasPrefix(line, "SCAN ") && !strings.Contains(line, "USING COVERING INDEX") && !strings.Contains(line, "USING INDEX") {
				t.Fatalf("%s: purge must not scan a table:\n%s", name, joined)
			}
			if strings.Contains(line, "TEMP B-TREE") {
				t.Fatalf("%s: purge must not sort:\n%s", name, joined)
			}
		}
	}
}

func assertAuthRowCount(t *testing.T, db *sql.DB, query string, arg string, want int) {
	t.Helper()
	var got int
	if err := db.QueryRow(query, arg).Scan(&got); err != nil {
		t.Fatalf("count rows: %v", err)
	}
	if got != want {
		t.Fatalf("%s [%s]: got %d rows, want %d", query, arg, got, want)
	}
}

func TestManagedAgentGrantAccessOnlyStoresNoRefreshSession(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	workspace, err := storage.InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatalf("initialize workspace: %v", err)
	}
	defer workspace.Close()
	store := NewStore(workspace.DB())

	agent, tokens, err := store.IssueTokenFromWorkspaceManagedAgentGrant(ctx, WorkspaceManagedAgentGrantIdentity{
		Issuer:         "https://issuer.example",
		Subject:        "slot-subject",
		WorkspaceID:    "ws_main",
		OrganizationID: "org-1",
		SlotID:         "slot-1",
		JTI:            "jti-managed-access-only",
	}, GrantExchangeOptions{AccessOnly: true})
	if err != nil {
		t.Fatalf("exchange managed grant: %v", err)
	}
	if tokens.AccessToken == "" || tokens.RefreshToken != "" {
		t.Fatalf("expected an access token only, got %#v", tokens)
	}
	assertAuthRowCount(t, workspace.DB(), `SELECT COUNT(*) FROM auth_refresh_sessions WHERE agent_id = ?`, agent.AgentID, 0)
	if _, err := store.AuthenticateAccessToken(ctx, tokens.AccessToken); err != nil {
		t.Fatalf("authenticate access-only token: %v", err)
	}
}

func TestPurgeExpiredAuthRowsDoesNotStarveRefreshSessions(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	workspace, err := storage.InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatalf("initialize workspace: %v", err)
	}
	defer workspace.Close()
	db := workspace.DB()
	store := NewStore(db)

	agent, _, err := store.IssueTokenFromWorkspaceHumanGrant(ctx, WorkspaceHumanGrantIdentity{
		Issuer:  "https://issuer.example",
		Subject: "person-backlog",
		JTI:     "jti-backlog",
	}, GrantExchangeOptions{AccessOnly: true})
	if err != nil {
		t.Fatalf("exchange grant: %v", err)
	}
	old := time.Now().UTC().Add(-3 * DefaultExpiredAuthRowRetention)
	oldText := old.Format(time.RFC3339Nano)
	// An access-token backlog far larger than one run's budget.
	for i := 0; i < 40; i++ {
		id := "access_backlog_" + time.Duration(i).String()
		if _, err := db.ExecContext(ctx, `INSERT INTO auth_access_tokens(id, agent_id, token_hash, created_at, expires_at) VALUES (?, ?, ?, ?, ?)`,
			id, agent.AgentID, "hash-"+id, old.Add(-time.Hour).Format(time.RFC3339Nano), oldText); err != nil {
			t.Fatalf("insert access token: %v", err)
		}
	}
	for _, row := range []struct{ id, revoked string }{{"refresh_backlog_expired", ""}, {"refresh_backlog_revoked", oldText}} {
		var revoked any
		expires := oldText
		if row.revoked != "" {
			revoked = row.revoked
			expires = time.Now().UTC().Add(24 * time.Hour).Format(time.RFC3339Nano)
		}
		if _, err := db.ExecContext(ctx, `INSERT INTO auth_refresh_sessions(id, agent_id, token_hash, created_at, expires_at, revoked_at) VALUES (?, ?, ?, ?, ?, ?)`,
			row.id, agent.AgentID, "hash-"+row.id, oldText, expires, revoked); err != nil {
			t.Fatalf("insert refresh session: %v", err)
		}
	}

	result, err := store.PurgeExpiredAuthRows(ctx, AuthRowPurgeOptions{BatchSize: 2, MaxBatches: 3})
	if err != nil {
		t.Fatalf("purge: %v", err)
	}
	if result.AccessTokens != 6 || !result.Truncated {
		t.Fatalf("expected the access-token step to stop at its own budget, got %#v", result)
	}
	if result.RefreshSessions != 2 {
		t.Fatalf("refresh sessions were starved by the access-token backlog: %#v", result)
	}
}
