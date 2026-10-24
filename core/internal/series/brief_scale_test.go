package series

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/testutil/perfguard"
)

// Exercise a full page with 100 admitted refs (250 attached refs), dense raw history and a real
// denied contributor. Both principals retain the ordinary /brief route budget.
func TestPerformanceBriefSeriesBudget(t *testing.T) {
	if os.Getenv("ANX_PERFORMANCE_TEST") != "1" || testing.Short() {
		t.Skip("set ANX_PERFORMANCE_TEST=1")
	}
	s, _, _, _ := fixture(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	store := primitives.NewTestStore(s.DB, t.TempDir())
	private, err := store.CreateWork(ctx, "owner", "", map[string]any{"title": "Private contributor"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.PatchThread(ctx, "owner", fmt.Sprint(private["thread_id"]), map[string]any{"pm_actor_id": "owner"}, nil); err != nil {
		t.Fatal(err)
	}
	ids := []string{}
	for i := 0; i < 50; i++ {
		card, e := store.CreateWork(ctx, "owner", "", map[string]any{"title": fmt.Sprintf("Loop %02d", i)})
		if e != nil {
			t.Fatal(e)
		}
		ids = append(ids, fmt.Sprint(card["id"]))
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	for i, id := range ids {
		refs := []string{}
		for j := 0; j < 5; j++ {
			name := fmt.Sprintf("metric%03d", i*5+j)
			refs = append(refs, "series:"+name)
			if _, err = tx.Exec(`INSERT INTO series_definitions(name,adapter,unit,kind) VALUES(?,'collector','count','gauge')`, name); err != nil {
				t.Fatal(err)
			}
			if _, err = tx.Exec(`INSERT INTO series_labels(series,labels) VALUES(?,'{}')`, name); err != nil {
				t.Fatal(err)
			}
			for d := 0; d < 8; d++ {
				stamp := now.Add(-time.Duration(d) * 24 * time.Hour).UnixNano()
				if _, err = tx.Exec(`INSERT INTO series_points(series,labels,ts,value,received_day) VALUES(?,'{}',?,10,0)`, name, stamp); err != nil {
					t.Fatal(err)
				}
			}
		}
		raw, _ := json.Marshal(refs)
		if _, err = tx.Exec(`UPDATE cards SET refs_json=? WHERE id=?`, string(raw), id); err != nil {
			t.Fatal(err)
		}
	}
	// Unrelated dense raw history must not increase flatness work.
	if _, err = tx.Exec(`WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i+1 FROM n WHERE i<10000)
 INSERT INTO series_points SELECT 'metric000','{}',?-i,10,NULL,0 FROM n`, now.UnixNano()); err != nil {
		t.Fatal(err)
	}
	// Dense participant history from other principals on an admitted card must
	// not walk participant/session history when an agent reader loads /brief.
	if _, err = tx.Exec(`INSERT INTO agents(id,username,actor_id,created_at,updated_at,metadata_json) VALUES('scale-agent-other','scale-other-agent','actor-other','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z','{}'),('scale-agent-reader','scale-reader-agent','agent-reader','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z','{}')`); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(`WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i+1 FROM n WHERE i<10000)
 INSERT INTO agent_sessions(id,agent_id,actor_id,provider,host_scope,native_session_id,native_session_id_kind,capabilities_json,activity,sequence,request_json,created_at,last_seen_at,expires_at)
 SELECT printf('sess-other-%05d',i),'scale-agent-other','actor-other','codex','host-1',printf('native-%05d',i),'opaque','{"resume":"unsupported","history":"unsupported","logs":"unsupported"}','active',1,'{}','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z','2026-12-31T00:00:00Z' FROM n`); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(`WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i+1 FROM n WHERE i<10000)
 INSERT INTO work_participants(id,card_id,session_id,activity,sequence,request_json,created_at,last_seen_at,expires_at)
 SELECT printf('part-other-%05d',i),?,printf('sess-other-%05d',i),'active',1,'{}','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z','2026-12-31T00:00:00Z' FROM n`, ids[0]); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(`WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i+1 FROM n WHERE i<10000)
 INSERT INTO agent_sessions(id,agent_id,actor_id,provider,host_scope,native_session_id,native_session_id_kind,capabilities_json,activity,sequence,request_json,created_at,last_seen_at,expires_at)
 SELECT printf('sess-reader-%05d',i),'scale-agent-reader','agent-reader','codex','host-1',printf('reader-native-%05d',i),'opaque','{"resume":"unsupported","history":"unsupported","logs":"unsupported"}','active',1,'{}','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z','2026-12-31T00:00:00Z' FROM n`); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(`WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i+1 FROM n WHERE i<10000)
 INSERT INTO work_participants(id,card_id,session_id,activity,sequence,request_json,created_at,last_seen_at,expires_at)
 SELECT printf('part-reader-%05d',i),?,printf('sess-reader-%05d',i),'active',1,'{}','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z','2026-12-31T00:00:00Z' FROM n`, ids[1]); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	for drained := false; !drained; {
		drained, err = store.RefreshBriefInitiatives(ctx, 200)
		if err != nil {
			t.Fatal(err)
		}
	}
	var seq int
	var dbname, path string
	if err = s.DB.QueryRow(`PRAGMA database_list`).Scan(&seq, &dbname, &path); err != nil {
		t.Fatal(err)
	}
	db, capture, err := perfguard.Open("file:" + path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	measured := primitives.NewTestStore(db, "")
	checkReader := func(label string, reader primitives.BriefReader, wantYours int) {
		t.Helper()
		actor := reader.ActorID
		scope := primitives.WithRequestAccessScope(ctx, primitives.AccessScope{ActorID: actor})
		scope, closeRead, e := measured.BeginResourceRead(scope)
		if e != nil {
			t.Fatal(e)
		}
		capture.Start()
		body, e := measured.Brief(scope, reader, now)
		statements, queries, rows := capture.Stop()
		work := capture.Work()
		closeRead()
		if e != nil || capture.WorkError() != nil {
			t.Fatalf("%s: brief=%v capture=%v", label, e, capture.WorkError())
		}
		if queries > 100 || rows > 1024 || work.VMSteps > 50000 {
			t.Errorf("%s actor=%s kind=%s SQL=%d rows=%d VM=%d", label, actor, reader.Kind, queries, rows, work.VMSteps)
		}
		initiatives := body["initiatives"].([]map[string]any)
		if len(initiatives) != 50 {
			t.Fatalf("%s: got %d initiatives", label, len(initiatives))
		}
		if reader.Kind == "agent" {
			yoursCount := 0
			for _, item := range initiatives {
				if item["yours"] == true {
					yoursCount++
				}
			}
			if yoursCount != wantYours {
				t.Fatalf("%s: agent-reader yoursCount=%d, want %d", label, yoursCount, wantYours)
			}
		}
		raw, _ := json.Marshal(initiatives)
		if !strings.Contains(string(raw), `"flat":true`) {
			t.Fatalf("%s: populated flat control missing", label)
		}
		metricQueries := 0
		for _, statement := range statements {
			if !strings.Contains(statement.SQL, "FROM series_points") && !strings.Contains(statement.SQL, "JOIN series_live_daily") {
				continue
			}
			metricQueries++
			details, e := perfguard.Explain(ctx, s.DB, statement)
			if e != nil {
				t.Fatal(e)
			}
			plan := strings.Join(details, "\n")
			for _, table := range []string{"series_points", "series_live_daily", "series_daily"} {
				if strings.Contains(plan, "SCAN "+table) || strings.Contains(plan, "MATERIALIZE "+table) {
					t.Fatalf("%s: unbounded metric plan: %s", label, plan)
				}
			}
			if !strings.Contains(plan, "USING INDEX sqlite_autoindex_series_points_1") && !strings.Contains(plan, "USING INDEX sqlite_autoindex_series_live_daily_1") {
				t.Fatal("missing indexed metric seek:\n" + plan)
			}
		}
		if metricQueries != 2 {
			t.Fatalf("%s: expected one ref batch + one daily batch, got %d", label, metricQueries)
		}
		if reader.Kind == "agent" {
			participantQueries := 0
			for _, statement := range statements {
				if !strings.Contains(statement.SQL, "work_participants") {
					continue
				}
				participantQueries++
				details, e := perfguard.Explain(ctx, s.DB, statement)
				if e != nil {
					t.Fatal(e)
				}
				plan := strings.Join(details, "\n")
				for _, table := range []string{"work_participants", "agent_sessions", "cards"} {
					if strings.Contains(plan, "SCAN "+table) || strings.Contains(plan, "MATERIALIZE "+table) {
						t.Fatalf("%s: unbounded agent ownership plan:\n%s", label, plan)
					}
				}
				if !strings.Contains(plan, "idx_work_participants_actor_card_active") {
					t.Fatalf("%s: missing indexed agent ownership seek:\n%s", label, plan)
				}
			}
			// Assignees are already in the admitted rows. Participation needs
			// one indexed batch only when some cards remain unassigned to the reader.
			if participantQueries > 1 {
				t.Fatalf("%s: expected at most 1 agent ownership query, got %d", label, participantQueries)
			}
		}
		t.Logf("variant=%s actor=%s full 50-card/100-admitted-ref/12000-point brief SQL=%d rows=%d VM=%d metric batches=%d", label, actor, queries, rows, work.VMSteps, metricQueries)
	}

	for _, reader := range []primitives.BriefReader{
		{ActorID: "owner", Kind: "human"},
		{ActorID: "human", Kind: "human"},
		{ActorID: "agent-reader", Kind: "agent"},
	} {
		checkReader("dense-reader-session-history", reader, 1)
	}

	encodedIDs, _ := json.Marshal(ids)
	if _, err = s.DB.ExecContext(ctx, `UPDATE cards SET assignee='actor:agent-reader' WHERE id IN (SELECT value FROM json_each(?))`, string(encodedIDs)); err != nil {
		t.Fatal(err)
	}
	checkReader("50-assigned-initiatives", primitives.BriefReader{ActorID: "agent-reader", Kind: "agent"}, 50)
	if _, err = s.DB.ExecContext(ctx, `UPDATE cards SET assignee='' WHERE id IN (SELECT value FROM json_each(?))`, string(encodedIDs)); err != nil {
		t.Fatal(err)
	}

	// Close the reader's 10,000 sessions without changing participant activity.
	// Eligibility must use the open-session partial index, not probe history.
	if _, err = s.DB.ExecContext(ctx, `UPDATE agent_sessions SET activity='closed' WHERE actor_id='agent-reader'`); err != nil {
		t.Fatal(err)
	}
	checkReader("10k-closed-reader-sessions", primitives.BriefReader{ActorID: "agent-reader", Kind: "agent"}, 0)

	if _, err = s.DB.ExecContext(ctx, `UPDATE cards SET assignee='actor:agent-reader' WHERE id IN (SELECT value FROM json_each(?))`, string(encodedIDs)); err != nil {
		t.Fatal(err)
	}
	checkReader("closed-history-plus-50-assigned", primitives.BriefReader{ActorID: "agent-reader", Kind: "agent"}, 50)

	// Scoped notes on an unrelated private card must add no history work.
	for i := 0; i < 1000; i++ {
		payload := map[string]any{"text": "Private note", "card_id": private["id"]}
		if _, err = store.AppendEvent(ctx, "owner", map[string]any{
			"type": "message_posted", "thread_id": private["thread_id"], "refs": []string{"card:" + fmt.Sprint(private["id"])}, "payload": payload,
		}); err != nil {
			t.Fatal(err)
		}
	}
	var noteCount int
	if err = s.DB.QueryRowContext(ctx, `SELECT count(*) FROM brief_notes WHERE card_id=?`, private["id"]).Scan(&noteCount); err != nil || noteCount != 1000 {
		t.Fatalf("private-note fixture count=%d error=%v", noteCount, err)
	}
	checkReader("closed-assigned-plus-1000-private-notes", primitives.BriefReader{ActorID: "agent-reader", Kind: "agent"}, 50)
}
