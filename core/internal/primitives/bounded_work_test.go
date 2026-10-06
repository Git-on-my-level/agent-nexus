package primitives

import (
	"agent-nexus-core/internal/storage"
	"context"
	"fmt"
	"strings"
	"testing"
)

func TestBoundedWorkFiltersMatchProjectionAndCursorOrder(t *testing.T) {
	ctx := context.Background()
	ws, err := storage.InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	s := NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	stamps := []string{"2026-01-01T00:00:00Z", "2026-01-01T01:00:00.000000002+01:00", "2026-01-01T00:00:00.000000001Z"}
	for i, stamp := range stamps {
		id := fmt.Sprintf("work-%d", i)
		if _, err := ws.DB().ExecContext(ctx, `INSERT INTO cards(id,handle,title,summary,column_key,assignee,created_at,created_by,updated_at,updated_by) VALUES(?,?,'ÉCOLE 100%','résumé','ready','alice',?,'actor',?,'actor')`, id, id, stamp, stamp); err != nil {
			t.Fatal(err)
		}
		if _, err := ws.DB().ExecContext(ctx, `INSERT INTO work_metadata(card_id,authority,native_id,metadata_json,version,updated_at,updated_by) VALUES(?,'github',?,'{"source":{"authority":"github"},"phase":" review ","owner":" actor:alice ","project_ref":"topic:standalone"}',1,'now','actor')`, id, id); err != nil {
			t.Fatal(err)
		}
	}
	// An explicit null is an override, even when canonical values would match.
	if _, err := ws.DB().ExecContext(ctx, `UPDATE work_metadata SET metadata_json=json_set(metadata_json,'$.phase',NULL) WHERE card_id='work-0'`); err != nil {
		t.Fatal(err)
	}
	filter := WorkListFilter{Limit: 1, ProjectRef: "topic:standalone", Phase: "review", Owner: "actor:alice", Query: "école 100%"}
	first, err := s.ListWork(ctx, filter)
	if err != nil || len(first.Work) != 1 || first.Work[0]["id"] != "work-1" || first.NextCursor == "" {
		t.Fatalf("first page: %+v %v", first, err)
	}
	filter.Cursor = first.NextCursor
	second, err := s.ListWork(ctx, filter)
	if err != nil || len(second.Work) != 1 || second.Work[0]["id"] != "work-2" || second.NextCursor != "" {
		t.Fatalf("second page: %+v %v", second, err)
	}
	b, err := s.CreateBoard(ctx, "actor", map[string]any{"title": "Board"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ws.DB().ExecContext(ctx, `UPDATE cards SET board_id=? WHERE id='work-1'`, b["id"]); err != nil {
		t.Fatal(err)
	}
	if _, err := ws.DB().ExecContext(ctx, `INSERT INTO ref_edges(id,source_type,source_id,target_type,target_id,edge_type,created_at,metadata_json) VALUES('placement','board',?,'card','work-1','board_card','now','{}')`, b["id"]); err != nil {
		t.Fatal(err)
	}
	snapshots, err := s.LiveWorkSnapshots(ctx, []string{"card:work-1", "card:missing", "card:work-2"})
	if err != nil || len(snapshots) != 1 || strings.TrimSpace(workString(snapshots["card:work-1"]["phase"])) != "review" {
		t.Fatalf("snapshots: %+v %v", snapshots, err)
	}

	if _, err := s.ArchiveBoard(ctx, "actor", b["id"].(string)); err != nil {
		t.Fatal(err)
	}
	live, err := s.LiveWorkSnapshots(ctx, []string{"card:work-1"})
	if err != nil || len(live) != 1 {
		t.Fatalf("PM parent lifecycle changed: %v %v", live, err)
	}
	report, err := s.ReportWorkSnapshots(ctx, []string{"card:work-1"})
	if err != nil || len(report) != 0 {
		t.Fatalf("report includes archived parent: %v %v", report, err)
	}
	closed := false
	query, args := reportWorkQuery(ctx, ReportWorkFilter{OverviewClosed: &closed, Limit: 100})
	plan := explainQueryPlan(t, ws.DB(), query, args...)
	if !strings.Contains(plan, "MATERIALIZE _work_candidates") {
		t.Fatalf("unbounded enrichment plan: %s", plan)
	}
	assertPlanUsesIndex(t, "work candidates", plan, "idx_cards_work_page")
}
