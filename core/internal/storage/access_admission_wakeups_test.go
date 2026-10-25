package storage

import (
	"context"
	"database/sql"
	"reflect"
	"strings"
	"testing"

	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/resourceaccess"
)

func TestHistoricalWakeupBackfillSeeksRetainedOrigins(t *testing.T) {
	db := mainHistoryDatabase(t, t.TempDir(), 115)
	defer db.Close()
	rows, err := db.Query(`EXPLAIN QUERY PLAN ` + admissionNonWakeupIdentities)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	originSeek := false
	for rows.Next() {
		var id, parent, unused int
		var detail string
		if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
			t.Fatal(err)
		}
		t.Log(detail)
		originSeek = originSeek || strings.Contains(detail, "(origin=?)")
		if strings.Contains(detail, "(kind=?)") {
			t.Error("retained-spelling backfill scans untouched wakeup identities")
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if !originSeek {
		t.Error("retained-spelling backfill did not seek by origin")
	}
}

func TestHistoricalWakeupAdmissionMatchesCanonical(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	db := mainHistoryDatabase(t, root, 115)
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := db.Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO threads(id,updated_at,updated_by,body_json) VALUES('historical-private','now','owner','{"pm_actor_id":"owner"}'),('historical-public','now','owner','{}'),('','now','owner','{"pm_actor_id":"owner"}')`)
	exec(`INSERT INTO events(id,type,ts,actor_id,thread_id,refs_json,payload_json) VALUES('historical-event','message_posted','now','owner','historical-private','[]','{}')`)
	exec(`INSERT INTO agent_wakeups(wakeup_id,status,notification_status,target_handle,target_actor_id,thread_id,trigger_event_id,refs_json,created_at,updated_at) VALUES
 ('old-private','requested','unread','agent','target','historical-private','','[]','now','now'),
 ('old-public','requested','unread','agent','target','historical-public','','[]','now','now'),
 ('old-reference','requested','unread','agent','target','historical-public','','["thread:historical-private"]','now','now'),
 ('old-empty-parent','requested','unread','agent','target','','','[]','now','now'),
 ('old-event','requested','unread','agent','target','historical-public','historical-event','[]','now','now')`)
	exec(`INSERT INTO cards(id,handle,title,summary,column_key,thread_id,refs_json,created_at,created_by,updated_at,updated_by) VALUES('historical-dependent','historical-dependent','Public','','ready','historical-public','["wakeup:old-private"]','now','owner','now','owner')`)
	exec(`INSERT INTO agent_wakeups(wakeup_id,status,notification_status,target_handle,target_actor_id,thread_id,refs_json,created_at,updated_at) WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i+1 FROM n WHERE i<128) SELECT 'old-chain-'||i,'requested','unread','agent','target','historical-public',CASE WHEN i=1 THEN '["wakeup:old-private","wakeup:old-chain-128"]' ELSE json_array('wakeup:old-chain-'||(i-1)) END,'now','now' FROM n`)
	db.Close()
	ws, err := InitializeWorkspace(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	db = ws.DB()
	var eager int
	if err := db.QueryRow(`SELECT count(*) FROM resource_access_admission_links WHERE origin='struct_agent_wakeups' AND origin_id='old-public'`).Scan(&eager); err != nil || eager != 0 {
		t.Fatalf("public history copied: count=%d err=%v", eager, err)
	}
	check := func(stage string) {
		t.Helper()
		for _, actor := range []string{"owner", "second-owner", "reader", ""} {
			read := func(rows *sql.Rows, err error) map[[2]string]bool {
				t.Helper()
				if err != nil {
					t.Fatal(err)
				}
				defer rows.Close()
				out := map[[2]string]bool{}
				for rows.Next() {
					var key [2]string
					if err := rows.Scan(&key[0], &key[1]); err != nil {
						t.Fatal(err)
					}
					out[key] = true
				}
				if err := rows.Err(); err != nil {
					t.Fatal(err)
				}
				return out
			}
			want := read(resourceaccess.NewDB(db).QueryContext(primitives.WithAccessScope(ctx, primitives.AccessScope{ActorID: actor}), `SELECT kind,CAST(id AS TEXT) FROM _anx_denied`))
			got := read(db.Query(`SELECT DISTINCT kind,id FROM resource_access_admission_owners WHERE ledger=CASE WHEN `+resourceaccess.CompactReaderSQL+` THEN 2 WHEN NOT (`+resourceaccess.CompactReaderSQL+`) THEN 1 ELSE -1 END AND (unconditional=1 OR owner<>?)`, actor))
			if !reflect.DeepEqual(want, got) {
				t.Fatalf("%s actor=%q canonical=%v indexed=%v", stage, actor, want, got)
			}
		}
	}
	check("upgrade")
	for _, q := range []string{
		`UPDATE agent_wakeups SET thread_id='historical-public' WHERE wakeup_id='old-private'`,
		`UPDATE agent_wakeups SET thread_id='historical-public' WHERE wakeup_id='old-empty-parent'`,
		`UPDATE agent_wakeups SET thread_id='historical-private' WHERE wakeup_id='old-public'`,
		`UPDATE threads SET body_json='{}' WHERE id='historical-private'`,
		`UPDATE threads SET body_json='{"pm_actor_id":"second-owner"}' WHERE id='historical-private'`,
		`UPDATE agent_wakeups SET thread_id='historical-private' WHERE wakeup_id='old-private'`,
		`UPDATE agent_wakeups SET wakeup_id='renamed-reference' WHERE wakeup_id='old-reference'`,
		`UPDATE agent_wakeups SET trigger_event_id='' WHERE wakeup_id='old-event'`,
		`DELETE FROM agent_wakeups WHERE wakeup_id='old-public'`,
		`UPDATE resource_access_compact_control SET reader=1,force_legacy=0`,
		`UPDATE resource_access_compact_control SET force_legacy=1`,
	} {
		exec(q)
		check(q)
	}
}
