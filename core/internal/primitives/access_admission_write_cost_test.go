package primitives_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/storage"
	"agent-nexus-core/internal/testutil/perfguard"
)

func TestAdmissionWriteSeeksChangedOrigin(t *testing.T) {
	ctx := context.Background()
	ws, err := storage.InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	s := primitives.NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	card, err := s.CreateWork(ctx, "owner", "", map[string]any{"title": "Original public title"})
	if err != nil {
		t.Fatal(err)
	}
	// Many independent origins share one public dependency. Changing an
	// unrelated source must neither scan nor rewrite those contributions.
	if _, err := ws.DB().Exec(`INSERT INTO resource_access_admission_links(origin,origin_id,via_id,parent_kind,parent_id,child_kind,child_id,ledger) WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i+1 FROM n WHERE i<10000) SELECT 'exact',CAST(100000+i AS TEXT),0,'card','unrelated-parent','card','unrelated-child',1 FROM n`); err != nil {
		t.Fatal(err)
	}
	db, capture, err := perfguard.Open("file:" + ws.Layout().DatabasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	capture.Start()
	_, err = db.Exec(`UPDATE cards SET title='Changed public title' WHERE id=?`, card["id"])
	capture.Stop()
	if err != nil || capture.WorkError() != nil {
		t.Fatalf("write=%v instrumentation=%v", err, capture.WorkError())
	}
	work := capture.Work()
	t.Logf("write VM=%d scans=%d", work.VMSteps, work.FullScanSteps)
	if work.VMSteps > 50000 {
		t.Fatalf("unrelated origin population inflated one write above 50,000 VM: %d", work.VMSteps)
	}
	var contributions int
	if err := db.QueryRow(`SELECT contributions FROM resource_access_admission_graph WHERE child_kind='card' AND child_id='unrelated-child' AND ledger=1 AND parent_kind='card' AND parent_id='unrelated-parent'`).Scan(&contributions); err != nil || contributions != 10000 {
		t.Fatalf("unrelated contribution count=%d err=%v", contributions, err)
	}
}

func TestTopicLifecycleWriteSeeksProjectIndex(t *testing.T) {
	ctx := context.Background()
	ws, err := storage.InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	for _, q := range []string{
		`INSERT INTO topics(id,handle,title,summary,extensions_json,provenance_json,created_at,created_by,updated_at,updated_by) VALUES('indexed-topic','indexed-topic','Project','','{}','{}','now','owner','now','owner')`,
		fmt.Sprintf(`INSERT INTO cards(id,handle,title,summary,column_key,created_at,created_by,updated_at,updated_by) WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i+1 FROM n WHERE i<%d) SELECT 'unrelated-'||i,'unrelated-'||i,'Public','','ready','now','owner','now','owner' FROM n`, primitivesFixtureRows(512, 10000)),
		`INSERT INTO cards(id,handle,title,summary,column_key,created_at,created_by,updated_at,updated_by) VALUES('member','member','Public','','ready','now','owner','now','owner')`,
		fmt.Sprintf(`INSERT INTO work_metadata(card_id,authority,metadata_json,updated_at,updated_by) WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i+1 FROM n WHERE i<%d) SELECT 'unrelated-'||i,'nexus',json_object('project_ref','topic:unrelated-'||i),'now','owner' FROM n`, primitivesFixtureRows(512, 10000)),
		`INSERT INTO work_metadata(card_id,authority,metadata_json,updated_at,updated_by) VALUES('member','nexus','{"project_ref":"topic:indexed-topic"}','now','owner')`,
	} {
		if _, err := ws.DB().Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	db, capture, err := perfguard.Open("file:" + ws.Layout().DatabasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	capture.Start()
	_, err = db.Exec(`UPDATE topics SET archived_at='now' WHERE id='indexed-topic'`)
	capture.Stop()
	if err != nil || capture.WorkError() != nil {
		t.Fatalf("write=%v instrumentation=%v", err, capture.WorkError())
	}
	work := capture.Work()
	t.Logf("topic update VM=%d scans=%d", work.VMSteps, work.FullScanSteps)
	if work.VMSteps > 50000 {
		t.Fatalf("topic update exceeded unchanged 50,000 VM ceiling: %d", work.VMSteps)
	}
	// Exercise the actual installed trigger's selector directly as well: SQLite
	// can avoid evaluating that subquery in a particular refresh plan.
	var trigger string
	if err := ws.DB().QueryRow(`SELECT sql FROM sqlite_schema WHERE name='admission_work_lifecycle_topics_update'`).Scan(&trigger); err != nil {
		t.Fatal(err)
	}
	start := strings.Index(trigger, `SELECT card_id FROM work_metadata WHERE `)
	const ending = `IN ('topic:'||OLD.id,'topic:'||OLD.handle)`
	if start < 0 {
		t.Fatal("missing topic selector")
	}
	end := strings.Index(trigger[start:], ending)
	if end < 0 {
		t.Fatal("missing topic selector bounds")
	}
	probe := strings.ReplaceAll(strings.ReplaceAll(trigger[start:start+end+len(ending)], "OLD.id", "'indexed-topic'"), "OLD.handle", "'indexed-topic'")
	plan, err := ws.DB().Query(`EXPLAIN QUERY PLAN ` + probe)
	if err != nil {
		t.Fatal(err)
	}
	indexed := false
	for plan.Next() {
		var id, parent, unused int
		var detail string
		if err := plan.Scan(&id, &parent, &unused, &detail); err != nil {
			t.Fatal(err)
		}
		indexed = indexed || strings.Contains(detail, "idx_work_access_project")
		t.Log(detail)
	}
	if err := plan.Err(); err != nil {
		t.Fatal(err)
	}
	plan.Close()
	if !indexed {
		t.Error("topic selector did not use the project expression index")
	}
	capture.Start()
	rows, err := db.Query(probe)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var card string
		if err := rows.Scan(&card); err != nil {
			t.Fatal(err)
		}
		if card != "member" {
			t.Fatalf("unexpected project member %q", card)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	rows.Close()
	capture.Stop()
	if capture.WorkError() != nil {
		t.Fatal(capture.WorkError())
	}
	t.Logf("topic selector VM=%d scans=%d", capture.Work().VMSteps, capture.Work().FullScanSteps)
	if capture.Work().VMSteps > 50000 {
		t.Errorf("topic selector exceeded unchanged 50,000 VM ceiling: %d", capture.Work().VMSteps)
	}
}
