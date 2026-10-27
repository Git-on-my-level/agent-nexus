package primitives_test

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/storage"
	"agent-nexus-core/internal/testutil/perfguard"
)

func TestAttentionPagesStayBoundedWithTenThousandUnrelatedRecords(t *testing.T) {
	if testing.Short() {
		t.Skip("scale-corpus budget guard; the non-short CI tier keeps the full fixture")
	}
	t.Parallel()
	ctx := context.Background()
	ws, err := storage.InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	seedDB := ws.DB()
	s := primitives.NewTestStore(seedDB, ws.Layout().ArtifactContentDir)
	if _, err = seedDB.Exec(`INSERT INTO actors(id,display_name,created_at) VALUES('human','Human','now')`); err != nil {
		t.Fatal(err)
	}
	if _, err = seedDB.Exec(`INSERT INTO agents(id,username,actor_id,created_at,updated_at,metadata_json) VALUES('human-id','human','human','now','now','{"principal_kind":"human"}')`); err != nil {
		t.Fatal(err)
	}
	w, err := s.CreateWork(ctx, "placer", "", map[string]any{"title": "Budget target"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.PlaceAttention(ctx, "placer", primitives.AttentionPlacement{SubjectRef: w["ref"].(string), For: "actor:human", To: "watch", Reason: "Budget target"}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.RecordAttentionFeedback(ctx, "human", primitives.AttentionFeedback{SubjectRef: w["ref"].(string), Signal: "more_like_this", Surface: "watch"}); err != nil {
		t.Fatal(err)
	}
	// Unrelated recipients and old feedback must not enter the admitted window.
	for _, q := range []string{
		`WITH RECURSIVE n(x) AS(SELECT 1 UNION ALL SELECT x+1 FROM n WHERE x<10000) INSERT INTO attention_subjects SELECT 'unrelated-'||x,'card:unrelated-'||x FROM n`,
		`WITH RECURSIVE n(x) AS(SELECT 1 UNION ALL SELECT x+1 FROM n WHERE x<10000) INSERT INTO attention_placements SELECT 'unrelated-'||x,'card:unrelated-'||x,'actor:placer','placement-'||x,'2020-01-01T00:00:00.000000000Z','{}' FROM n`,
		`WITH RECURSIVE n(x) AS(SELECT 1 UNION ALL SELECT x+1 FROM n WHERE x<10000) INSERT INTO events(id,type,ts,actor_id,payload_json) SELECT 'feedback-'||x,'attention_feedback','2020-01-01T00:00:00.000000000Z','other','{"payload":{"for":"actor:other","signal":"more_like_this","surface":"watch"}}' FROM n`,
	} {
		if _, err = seedDB.ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	var seq int
	var name, path string
	if err = seedDB.QueryRowContext(ctx, `PRAGMA database_list`).Scan(&seq, &name, &path); err != nil {
		t.Fatal(err)
	}
	db, capture, err := perfguard.Open("file:" + path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	measured := primitives.NewTestStore(db, "")
	raw, err := os.ReadFile("testdata/attention_budget.json")
	if err != nil {
		t.Fatal(err)
	}
	var budget map[string]uint64
	if err = json.Unmarshal(raw, &budget); err != nil {
		t.Fatal(err)
	}
	scoped := primitives.WithRequestAccessScope(context.Background(), primitives.AccessScope{ActorID: "human"})
	// Traverse the mandatory-ask phase, then measure the actual populated
	// placement phase using the exact encrypted continuation returned by core.
	first, err := measured.AttentionItems(scoped, "human", "human", "all", "", "builtin:initiatives", "", time.Now())
	if err != nil || first.NextCursor == nil {
		t.Fatal(first, err)
	}
	capture.Start()
	page, err := measured.AttentionItems(scoped, "human", "human", "all", "", "builtin:initiatives", *first.NextCursor, time.Now())
	_, queries, rows := capture.Stop()
	work := capture.Work()
	if err != nil || len(page.Items) != 1 || page.Items[0].Reason != "Budget target" || capture.WorkError() != nil || uint64(queries) > budget["max_queries"] || uint64(rows) > budget["max_rows"] || work.VMSteps > budget["max_vm_steps"] {
		t.Fatalf("placements=%+v err=%v queries=%d rows=%d work=%+v instrumentation=%v", page, err, queries, rows, work, capture.WorkError())
	}
	t.Logf("placements queries=%d rows=%d work=%+v", queries, rows, work)
	capture.Start()
	feedback, err := measured.AttentionFeedbackList(scoped, "human", "", "")
	_, queries, rows = capture.Stop()
	work = capture.Work()
	if err != nil || len(feedback["items"].([]map[string]any)) != 1 || capture.WorkError() != nil || uint64(queries) > budget["max_queries"] || uint64(rows) > budget["max_rows"] || work.VMSteps > budget["max_vm_steps"] {
		t.Fatalf("feedback=%+v err=%v queries=%d rows=%d work=%+v instrumentation=%v", feedback, err, queries, rows, work, capture.WorkError())
	}
	t.Logf("feedback queries=%d rows=%d work=%+v", queries, rows, work)
}
