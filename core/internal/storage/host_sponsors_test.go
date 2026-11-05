package storage

import (
	"context"
	"database/sql"
	"testing"
)

// hostSponsorFixture seeds hosts and only audit history: completed
// host_enrollments rows are deleted an hour after creation, so the backfill
// must not depend on them.
func hostSponsorFixture(t *testing.T, humans ...string) *sql.DB {
	t.Helper()
	ws, err := InitializeWorkspace(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ws.Close() })
	db := ws.DB()
	now := "2026-10-01T00:00:00Z"
	statements := []string{
		`INSERT INTO agents(id,username,actor_id,created_at,updated_at,metadata_json) VALUES
		 ('p-admin-agent','admin-agent','admin-agent','` + now + `','` + now + `','{"principal_kind":"agent","auth_admin":true}')`,
		`INSERT INTO hosts(id,slug,display_name,os_user,hostname,discovered_adapters_json,created_at) VALUES
		 ('h-device','device','device','u','h','[]','` + now + `'),('h-agent','agent','agent','u','h','[]','` + now + `'),
		 ('h-late','late','late','u','h','[]','` + now + `'),('h-token','token','token','u','h','[]','` + now + `'),
		 ('h-agent-token','agent-token','agent-token','u','h','[]','` + now + `')`,
		`INSERT INTO host_enrollment_tokens(id,label,token_hash,created_at,expires_at,consumed_at,created_by_agent_id) VALUES
		 ('t-human','l','th','` + now + `','` + now + `','` + now + `','p-human'),('t-agent','l','ta','` + now + `','` + now + `','` + now + `','p-admin-agent')`,
		`INSERT INTO auth_audit_events(id,event_type,occurred_at,actor_agent_id,actor_actor_id,metadata_json) VALUES
		 ('a1','host_enroll_approved','2026-10-01T00:01:00.123456789Z','p-human','human','{"enrollment_id":"gone-1","slug":"device"}'),
		 ('c1','host_enroll_completed','2026-10-01T00:03:00.5Z',NULL,NULL,'{"host_id":"h-device","slug":"device"}'),
		 ('a2','host_enroll_approved','2026-10-01T00:01:00Z','p-admin-agent','admin-agent','{"enrollment_id":"gone-2","slug":"agent"}'),
		 ('c2','host_enroll_completed','2026-10-01T00:02:00Z',NULL,NULL,'{"host_id":"h-agent","slug":"agent"}'),
		 ('a3','host_enroll_approved','2026-10-01T00:01:00Z','p-human','human','{"enrollment_id":"gone-3","slug":"late"}'),
		 ('c3','host_enroll_completed','2026-10-01T01:00:00Z',NULL,NULL,'{"host_id":"h-late","slug":"late"}'),
		 ('a4','host_enroll_approved','2026-10-01T00:01:00Z','p-human','human','{"enrollment_id":"gone-4","slug":"token"}'),
		 ('c4','host_enroll_completed','2026-10-01T00:02:00Z',NULL,NULL,'{"host_id":"h-token","slug":"token"}'),
		 ('k4','host_enrollment_token_consumed','2026-10-01T00:02:00Z',NULL,NULL,'{"token_id":"t-human","host_id":"h-token"}'),
		 ('k5','host_enrollment_token_consumed','2026-10-01T00:02:00Z',NULL,NULL,'{"token_id":"t-agent","host_id":"h-agent-token"}')`,
	}
	for _, human := range humans {
		statements = append(statements, `INSERT INTO agents(id,username,actor_id,created_at,updated_at,metadata_json) VALUES('p-`+human+`','`+human+`','`+human+`','`+now+`','`+now+`','{"principal_kind":"human"}')`)
	}
	for _, q := range statements {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err = installHostSponsors(context.Background(), tx); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	return db
}

func hostSponsorRows(t *testing.T, db *sql.DB) map[string]string {
	t.Helper()
	got := map[string]string{}
	rows, err := db.Query(`SELECT host_id,sponsor_actor_id||':'||source FROM host_sponsors`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var host, sponsor string
		if err := rows.Scan(&host, &sponsor); err != nil {
			t.Fatal(err)
		}
		got[host] = sponsor
	}
	return got
}

// A single-human workspace backfills device hosts from approval/completion
// audit pairs; agent approvers, completions outside the enrollment lifetime
// and token-enrolled hosts are not device sponsors.
func TestInstallHostSponsorsBackfillsSingleHumanDeviceHosts(t *testing.T) {
	got := hostSponsorRows(t, hostSponsorFixture(t, "human"))
	want := map[string]string{"h-device": "human:device_approval", "h-token": "human:enrollment_token"}
	if len(got) != len(want) || got["h-device"] != want["h-device"] || got["h-token"] != want["h-token"] {
		t.Fatalf("sponsors=%v want %v", got, want)
	}
}

// With two humans, historical device approvals prove nothing about who runs the
// host (any human could approve), so only token issuers are backfilled.
func TestInstallHostSponsorsSkipsDeviceHistoryWithTwoHumans(t *testing.T) {
	got := hostSponsorRows(t, hostSponsorFixture(t, "human", "other"))
	if len(got) != 1 || got["h-token"] != "human:enrollment_token" {
		t.Fatalf("sponsors=%v want only the token host", got)
	}
}
