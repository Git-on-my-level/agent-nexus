package auth_test

import (
	"agent-nexus-core/internal/auth"
	"agent-nexus-core/internal/storage"
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"
)

func TestPersonaStem(t *testing.T) {
	for name, want := range map[string]string{
		"codex-spkfinal-r2":  "codex-spkfinal",
		"codex-spkfinal-v10": "codex-spkfinal",
		"codex-spkfinal-3":   "codex-spkfinal",
		"codex-spkfinal":     "codex-spkfinal",
		"reviewer-run":       "reviewer-run",
		"r2":                 "r2",
	} {
		if got := auth.PersonaStem(name); got != want {
			t.Errorf("PersonaStem(%q) = %q, want %q", name, got, want)
		}
	}
}

type personaSeed struct {
	host, name, kind string
	tokenAge         time.Duration // 0: no token
	presenceAge      time.Duration // 0: no presence
	served           bool
}

func seedPersonas(t *testing.T, db *sql.DB, now time.Time, seeds []personaSeed) {
	t.Helper()
	ts := func(age time.Duration) string { return now.Add(-age).UTC().Format(time.RFC3339Nano) }
	for _, host := range []string{"host-a", "host-b"} {
		if _, err := db.Exec(`INSERT INTO hosts(id,slug,display_name,os_user,hostname,discovered_adapters_json,created_at) VALUES(?,?,?,'user','machine','["cursor"]',?)`, host, host, host, ts(90*24*time.Hour)); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO host_bridge_presence VALUES(?,'instance',1,1,'test',?,?,'2999-01-01T00:00:00Z')`, host, ts(0), ts(0)); err != nil {
			t.Fatal(err)
		}
	}
	for _, s := range seeds {
		id := s.host + "-" + s.name
		for _, q := range []struct {
			sql  string
			args []any
		}{
			{`INSERT INTO actors VALUES(?,?,'["agent"]',?,'{}')`, []any{id, s.name, ts(60 * 24 * time.Hour)}},
			{`INSERT INTO agents(id,username,actor_id,created_at,updated_at,metadata_json) VALUES(?,?,?,?,?,'{}')`, []any{id, s.name + "." + s.host, id, ts(60 * 24 * time.Hour), ts(60 * 24 * time.Hour)}},
			{`INSERT INTO host_agents(host_id,name,agent_id,identity_kind) VALUES(?,?,?,?)`, []any{s.host, s.name, id, s.kind}},
		} {
			if _, err := db.Exec(q.sql, q.args...); err != nil {
				t.Fatal(err)
			}
		}
		if s.tokenAge > 0 {
			if _, err := db.Exec(`INSERT INTO auth_access_tokens(id,agent_id,token_hash,created_at,expires_at) VALUES(?,?,?,?,?)`, "access-"+id, id, "hash-"+id, ts(s.tokenAge), ts(s.tokenAge-15*time.Minute)); err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec(`INSERT INTO auth_refresh_sessions(id,agent_id,token_hash,created_at,expires_at) VALUES(?,?,?,?,?)`, "refresh-"+id, id, "rhash-"+id, ts(s.tokenAge), ts(-24*time.Hour)); err != nil {
				t.Fatal(err)
			}
		}
		if s.presenceAge > 0 {
			if _, err := db.Exec(`INSERT INTO agent_presence(agent_id,observed_at) VALUES(?,?)`, id, ts(s.presenceAge)); err != nil {
				t.Fatal(err)
			}
		}
		if s.served {
			if _, err := db.Exec(`INSERT INTO host_bridge_jobs VALUES(?,?,'answers',1,1)`, s.host, id); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func archivedReasons(t *testing.T, db *sql.DB) map[string]string {
	t.Helper()
	rows, err := db.Query(`SELECT host_id||'/'||name,archived_reason FROM host_agents WHERE archived_at IS NOT NULL`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var key, reason string
		if err := rows.Scan(&key, &reason); err != nil {
			t.Fatal(err)
		}
		out[key] = reason
	}
	return out
}

func TestArchiveInactivePersonasIsBoundedAndSkipsActiveAndAdapters(t *testing.T) {
	ctx := context.Background()
	ws, err := storage.InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	db := ws.DB()
	now := time.Now().UTC()
	day := 24 * time.Hour
	seedPersonas(t, db, now, []personaSeed{
		{host: "host-a", name: "stale-persona", kind: "derived", tokenAge: 40 * day},
		{host: "host-a", name: "fresh-persona", kind: "derived", tokenAge: day},
		{host: "host-a", name: "codex", kind: "derived", tokenAge: 40 * day},  // known adapter
		{host: "host-a", name: "cursor", kind: "derived", tokenAge: 40 * day}, // discovered adapter
		{host: "host-a", name: "served-persona", kind: "derived", tokenAge: 40 * day, served: true},
		{host: "host-a", name: "adopted-persona", kind: "adopted", tokenAge: 40 * day},
		{host: "host-a", name: "never-used", kind: "derived"}, // no evidence
		{host: "host-a", name: "present-persona", kind: "derived", tokenAge: 40 * day, presenceAge: day},
		{host: "host-b", name: "other-stale", kind: "derived", tokenAge: 31 * day},
	})
	store := auth.NewStore(db)

	if result, err := store.ArchiveInactivePersonas(ctx, auth.PersonaSweepOptions{Now: now}); err != nil || result.Scanned != 0 || result.Archived != 0 {
		t.Fatalf("disabled sweep ran: %+v %v", result, err)
	}

	// Two rows per batch, one batch per run: each run reads at most two rows
	// and the cursor carries the rest to the next run.
	opts := auth.PersonaSweepOptions{InactiveFor: 30 * day, BatchSize: 2, MaxBatches: 1, Now: now}
	runs, scanned := 0, 0
	for {
		result, err := store.ArchiveInactivePersonas(ctx, opts)
		if err != nil {
			t.Fatal(err)
		}
		if result.Scanned > 2 {
			t.Fatalf("run read %d rows past its budget", result.Scanned)
		}
		runs++
		scanned += result.Scanned
		if result.Wrapped {
			break
		}
		if result.Next == (auth.PersonaSweepCursor{}) {
			t.Fatalf("unwrapped run lost its cursor: %+v", result)
		}
		opts.After = result.Next
		if runs > 10 {
			t.Fatal("sweep never wrapped")
		}
	}
	if scanned != 9 || runs != 5 {
		t.Fatalf("scanned %d rows in %d runs", scanned, runs)
	}
	got := archivedReasons(t, db)
	want := map[string]string{"host-a/stale-persona": "inactive", "host-b/other-stale": "inactive"}
	if len(got) != len(want) || got["host-a/stale-persona"] != "inactive" || got["host-b/other-stale"] != "inactive" {
		t.Fatalf("archived %v, want %v", got, want)
	}
	var liveTokens, liveSessions int
	if err := db.QueryRow(`SELECT COUNT(*) FROM auth_access_tokens WHERE agent_id='host-a-stale-persona' AND revoked_at IS NULL`).Scan(&liveTokens); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM auth_refresh_sessions WHERE agent_id='host-a-stale-persona' AND revoked_at IS NULL`).Scan(&liveSessions); err != nil {
		t.Fatal(err)
	}
	if liveTokens != 0 || liveSessions != 0 {
		t.Fatalf("sweep left credentials live: tokens=%d sessions=%d", liveTokens, liveSessions)
	}
	var history int
	if err := db.QueryRow(`SELECT COUNT(*) FROM agents a JOIN actors x ON x.id=a.actor_id WHERE a.id='host-a-stale-persona' AND a.revoked_at IS NULL`).Scan(&history); err != nil || history != 1 {
		t.Fatalf("archive must keep the principal and actor: %d %v", history, err)
	}

	// A second full pass archives nothing more.
	result, err := store.ArchiveInactivePersonas(ctx, auth.PersonaSweepOptions{InactiveFor: 30 * day, Now: now})
	if err != nil || result.Archived != 0 || !result.Wrapped {
		t.Fatalf("second pass: %+v %v", result, err)
	}
}

func TestManualArchiveIsHumanOnlyAndRejectsTheName(t *testing.T) {
	ctx := context.Background()
	ws, err := storage.InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	db := ws.DB()
	now := time.Now().UTC()
	seedPersonas(t, db, now, []personaSeed{{host: "host-a", name: "worker", kind: "derived", tokenAge: time.Minute}})
	if _, err := db.Exec(`UPDATE auth_access_tokens SET expires_at=? WHERE agent_id='host-a-worker'`, now.Add(10*time.Minute).Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO actors VALUES('human','Human','[]','now','{}')`,
		`INSERT INTO agents(id,username,actor_id,created_at,updated_at,metadata_json) VALUES('human','human','human','now','now','{"principal_kind":"human"}')`,
		`INSERT INTO actors VALUES('admin-agent','Admin agent','[]','now','{}')`,
		`INSERT INTO agents(id,username,actor_id,created_at,updated_at,metadata_json) VALUES('admin-agent','admin-agent','admin-agent','now','now','{"principal_kind":"agent","auth_admin":true}')`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	store := auth.NewStore(db)
	human := auth.Principal{AgentID: "human", ActorID: "human", PrincipalKind: "human"}
	agentAdmin := auth.Principal{AgentID: "admin-agent", ActorID: "admin-agent", PrincipalKind: "agent", AuthAdmin: true}

	if _, err := store.ArchiveHostAgent(ctx, "host-a", "worker", agentAdmin); !errors.Is(err, auth.ErrHumanRequired) {
		t.Fatalf("agent auth-admin archived a persona: %v", err)
	}
	if _, err := store.ArchiveHostAgent(ctx, "host-a", "missing", human); !errors.Is(err, auth.ErrHostNotFound) {
		t.Fatalf("missing persona: %v", err)
	}
	host, err := store.ArchiveHostAgent(ctx, "host-a", "worker", human)
	if err != nil || len(host.Agents) != 1 || host.Agents[0].ArchivedAt == nil || host.Agents[0].BridgeOnline {
		t.Fatalf("archive: %+v %v", host, err)
	}
	if got := archivedReasons(t, db); got["host-a/worker"] != "manual" {
		t.Fatalf("archive reason: %v", got)
	}
	if _, err := store.ArchiveHostAgent(ctx, "host-a", "worker", human); err != nil {
		t.Fatalf("archive must be idempotent: %v", err)
	}
	// Restore re-enables registration; tokens revoked by the archive stay revoked.
	host, err = store.RestoreHostAgent(ctx, "host-a", "worker", human)
	if err != nil || host.Agents[0].ArchivedAt != nil {
		t.Fatalf("restore: %+v %v", host, err)
	}
	var revoked int
	if err := db.QueryRow(`SELECT COUNT(*) FROM auth_access_tokens WHERE agent_id='host-a-worker' AND revoked_at IS NOT NULL`).Scan(&revoked); err != nil || revoked != 1 {
		t.Fatalf("restore must not revive revoked tokens: %d %v", revoked, err)
	}
	var events int
	if err := db.QueryRow(`SELECT COUNT(*) FROM auth_audit_events WHERE event_type IN ('host_agent_archived','host_agent_restored')`).Scan(&events); err != nil || events != 2 {
		t.Fatalf("audit events: %d %v", events, err)
	}
}

// A restore counts as use: the sweep must not re-archive a persona a human
// just restored, even though its newest token is still old. Once the restore
// itself is older than the cutoff, the persona is eligible again.
func TestSweepDoesNotReArchiveRestoredPersona(t *testing.T) {
	ctx := context.Background()
	ws, err := storage.InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	db := ws.DB()
	now := time.Now().UTC()
	day := 24 * time.Hour
	seedPersonas(t, db, now, []personaSeed{{host: "host-a", name: "occasional", kind: "derived", tokenAge: 40 * day}})
	seedHuman(t, db)
	store := auth.NewStore(db)
	human := auth.Principal{AgentID: "human", ActorID: "human", PrincipalKind: "human"}
	sweep := func() int {
		t.Helper()
		result, err := store.ArchiveInactivePersonas(ctx, auth.PersonaSweepOptions{InactiveFor: 30 * day, Now: now})
		if err != nil {
			t.Fatal(err)
		}
		return result.Archived
	}
	if sweep() != 1 {
		t.Fatal("stale persona was not archived")
	}
	if _, err := store.RestoreHostAgent(ctx, "host-a", "occasional", human); err != nil {
		t.Fatal(err)
	}
	if n := sweep(); n != 0 || len(archivedReasons(t, db)) != 0 {
		t.Fatalf("sweep re-archived a just-restored persona: %d %v", n, archivedReasons(t, db))
	}
	if _, err := db.Exec(`UPDATE host_agents SET restored_at=? WHERE name='occasional'`, now.Add(-31*day).Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	if sweep() != 1 {
		t.Fatal("persona idle since its old restore was not archived")
	}
}

func TestArchiveAndRestoreRefuseRevokedPrincipal(t *testing.T) {
	ctx := context.Background()
	ws, err := storage.InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	db := ws.DB()
	seedPersonas(t, db, time.Now().UTC(), []personaSeed{{host: "host-a", name: "gone", kind: "derived", tokenAge: time.Hour}})
	seedHuman(t, db)
	if _, err := db.Exec(`UPDATE agents SET revoked_at='2026-01-01T00:00:00Z' WHERE id='host-a-gone'`); err != nil {
		t.Fatal(err)
	}
	store := auth.NewStore(db)
	human := auth.Principal{AgentID: "human", ActorID: "human", PrincipalKind: "human"}
	if _, err := store.ArchiveHostAgent(ctx, "host-a", "gone", human); !errors.Is(err, auth.ErrAgentRevoked) {
		t.Fatalf("archive of revoked principal: %v", err)
	}
	if _, err := store.RestoreHostAgent(ctx, "host-a", "gone", human); !errors.Is(err, auth.ErrAgentRevoked) {
		t.Fatalf("restore of revoked principal: %v", err)
	}
}

func seedHuman(t *testing.T, db *sql.DB) {
	t.Helper()
	for _, q := range []string{
		`INSERT INTO actors VALUES('human','Human','[]','now','{}')`,
		`INSERT INTO agents(id,username,actor_id,created_at,updated_at,metadata_json) VALUES('human','human','human','now','now','{"principal_kind":"human"}')`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
}
