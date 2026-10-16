package primitives

import (
	"context"
	"database/sql"
	"testing"
)

func TestPointSnapshotMissingAndExpiredProofDenies(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	snapshot := &denialSnapshot{rows: `[["card","private"],["card","private\u0000tail"],["work_evidence_alias",42],[null,null]]`}
	snapshot.preparePointIndex()
	if !snapshot.denies("work_evidence_alias", "42") || snapshot.denies("work_evidence_alias", "43") {
		t.Fatal("ordinary and point lookups disagree on integer identities")
	}
	token := pointSnapshotSequence.Add(1)
	pointSnapshots.Store(token, snapshot)
	defer pointSnapshots.Delete(token)
	check := func(key any, kind any, id any, want int) {
		t.Helper()
		var got int
		if err := ws.DB().QueryRowContext(ctx, `SELECT anx_point_snapshot_denied(?,?,CAST(? AS BLOB))`, key, kind, id).Scan(&got); err != nil || got != want {
			t.Fatalf("got=%d want=%d error=%v", got, want, err)
		}
	}
	check(token, "card", "private", 1)
	check(token, "card", "private\x00tail", 1)
	check(token, "work_evidence_alias", int64(42), 1)
	check(token, "work_evidence_alias", int64(43), 0)
	check(token, "card", "public", 0)
	check(token, "card", nil, 1)
	check(nil, "card", "public", 1)
	check(int64(-1), "card", "public", 1)
	var rows string
	if err := ws.DB().QueryRowContext(ctx, `SELECT anx_point_snapshot_rows(?,'all')`, token).Scan(&rows); err != nil || rows != snapshot.rows {
		t.Fatalf("snapshot rows=%q error=%v", rows, err)
	}
	pointSnapshots.Delete(token)
	check(token, "card", "public", 1)
	if err := ws.DB().QueryRowContext(ctx, `SELECT anx_point_snapshot_rows(?,'all')`, token).Scan(&rows); err == nil {
		t.Fatal("expired snapshot rows must fail closed")
	}
}

func TestPointSnapshotInvalidUTF8RetainsCanonicalFallback(t *testing.T) {
	t.Parallel()
	snapshot := &denialSnapshot{rows: "[[\"card\",\"private\xff\"]]"}
	snapshot.preparePointIndex()
	if snapshot.pointIndex != nil {
		t.Fatal("lossy UTF-8 proof admitted")
	}
}

func TestPointSnapshotMalformedRowsFailClosed(t *testing.T) {
	t.Parallel()
	for _, rows := range []string{`[["card","private"]] []`, `[["card","private"]] trailing`, `[["card",1.5]]`, `[["card","private","extra"]]`, `null`} {
		snapshot := &denialSnapshot{rows: rows}
		snapshot.preparePointIndex()
		if snapshot.pointIndex != nil || !snapshot.denies("card", "public") {
			t.Fatalf("malformed snapshot admitted visibility: %q", rows)
		}
	}
}

// Compare hybrid series policy with the canonical graph, including arbitrary
// labels, private historical state, unknown legacy provenance and cache reuse.
func TestPointSnapshotSeriesPolicyMatchesCanonical(t *testing.T) {
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	s := NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	card, err := s.CreateWork(ctx, "owner", "", map[string]any{"title": "Private series source"})
	if err != nil {
		t.Fatal(err)
	}
	exec := func(q string, args ...any) {
		t.Helper()
		if _, e := ws.DB().ExecContext(ctx, q, args...); e != nil {
			t.Fatal(e)
		}
	}
	exec(`INSERT INTO agents(id,username,actor_id,created_at,updated_at,metadata_json) VALUES('series-test','series-test','owner','2026-01-01','2026-01-01','{}')`)
	exec(`INSERT INTO hosts(id,slug,display_name,os_user,hostname,discovered_adapters_json,created_at) VALUES('series-test','series-test','test','test','test','[]','2026-01-01')`)
	exec(`INSERT INTO series_adapters(name,description,agent_id,host_id,expected_interval,created_at,last_push) VALUES('series-test','public','series-test','series-test',60,'2026-01-01','2026-01-01')`)
	ref := "card:" + card["id"].(string)
	for _, item := range []struct{ name, labels, state string }{
		{"public", "{}", "public"},
		{"private-state", "{}", ref},
		{"private-label", `{"source":"` + ref + `"}`, "public"},
		{"private-prose-label", `{"source":"Copied from ` + ref + ` yesterday"}`, "public"},
		{"unknown", "{}", "public"},
	} {
		exec(`INSERT INTO series_definitions(name,adapter,unit,kind) VALUES(?,'series-test','count','gauge')`, item.name)
		exec(`INSERT INTO series_labels(series,labels) VALUES(?,?)`, item.name, item.labels)
		exec(`INSERT INTO series_points(series,labels,ts,state,received_day) VALUES(?,?,1,?,0)`, item.name, item.labels, item.state)
		exec(`INSERT INTO series_daily(series,labels,day,n,last_ts,last_state) VALUES(?,?,0,1,1,?)`, item.name, item.labels, item.state)
	}
	exec(`INSERT INTO resource_access_series_unknown(series,labels) VALUES('unknown','{}')`)
	for _, owner := range []string{"owner", "", "owner"} {
		if _, err = s.PatchThread(ctx, "owner", card["thread_id"].(string), map[string]any{"pm_actor_id": owner}, nil); err != nil {
			t.Fatal(err)
		}
		for _, actor := range []string{"owner", "stranger"} {
			// Second read exercises the shared proof cache and its series subset.
			for repeat := 0; repeat < 2; repeat++ {
				scoped := WithRequestAccessScope(ctx, AccessScope{ActorID: actor})
				pin, closeRead, e := s.BeginResourceRead(scoped)
				if e != nil {
					t.Fatal(e)
				}
				canonical := context.WithValue(pin, summaryPointReadKey{}, false)
				for _, table := range []string{"series_labels", "series_points", "series_daily", "series_live_daily"} {
					query := `SELECT count(*) FROM ` + table
					var got, want int
					if e = s.db.QueryRowContext(pin, query).Scan(&got); e != nil {
						closeRead()
						t.Fatal(e)
					}
					if e = s.db.QueryRowContext(canonical, query).Scan(&want); e != nil {
						closeRead()
						t.Fatal(e)
					}
					expected := 5
					if actor != "owner" && owner != "" {
						expected = 1
					}
					if got != want || got != expected {
						closeRead()
						t.Fatalf("owner=%q actor=%s %s point=%d canonical=%d want=%d", owner, actor, table, got, want, expected)
					}
				}
				var got, want sql.NullString
				query := `SELECT last_push FROM series_adapters WHERE name='series-test' AND EXISTS(SELECT 1 FROM series_labels)`
				if e = s.db.QueryRowContext(pin, query).Scan(&got); e != nil {
					closeRead()
					t.Fatal(e)
				}
				if e = s.db.QueryRowContext(canonical, query).Scan(&want); e != nil {
					closeRead()
					t.Fatal(e)
				}
				if got != want {
					closeRead()
					t.Fatalf("adapter visibility differs: point=%v canonical=%v", got, want)
				}
				closeRead()
			}
		}
	}
}
