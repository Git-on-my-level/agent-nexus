package primitives

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func TestBoundedWorkFiltersMatchProjectionAndCursorOrder(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
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
	filter.Cursor, filter.Limit = "", 20
	filter.Visible = func(string, string) bool { return false }
	hidden, err := s.ListWork(ctx, filter)
	if err != nil || len(hidden.Work) != 0 || hidden.NextCursor != "" {
		t.Fatalf("visibility callback exposed a row or cursor: %+v %v", hidden, err)
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

func TestClosedWorkSelectorsPreserveExternalOverrides(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	s := NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	for _, row := range []struct{ id, phase, authority, metadata string }{
		{"native-open", "ready", "nexus", `{"source":{"authority":"nexus"},"phase":"done"}`},
		{"native-closed", "done", "nexus", `{"source":{"authority":"nexus"}}`},
		{"external-closed", "ready", "github", `{"source":{"authority":"github"},"phase":"done"}`},
		{"external-open", "done", "github", `{"source":{"authority":"github"},"phase":null}`},
	} {
		if _, err = ws.DB().Exec(`INSERT INTO cards(id,title,column_key,created_at,created_by,updated_at,updated_by) VALUES(?,'test',?,'2026-01-01T00:00:00Z','actor','2026-01-01T00:00:00Z','actor')`, row.id, row.phase); err != nil {
			t.Fatal(err)
		}
		if _, err = ws.DB().Exec(`INSERT INTO work_metadata(card_id,authority,native_id,metadata_json,version,updated_at,updated_by) VALUES(?,?,?,?,1,'now','actor')`, row.id, row.authority, row.id, row.metadata); err != nil {
			t.Fatal(err)
		}
	}
	closed := true
	filter := ReportWorkFilter{OverviewClosed: &closed, IncludeClosed: true, Limit: 100}
	page, err := s.ListReportWork(ctx, filter)
	if err != nil || len(page.Work) != 2 || page.Work[0]["id"] != "native-closed" || page.Work[1]["id"] != "external-closed" {
		t.Fatalf("closed native/external projection: %+v %v", page.Work, err)
	}
	query, args := reportWorkQuery(ctx, filter)
	plan := explainQueryPlan(t, ws.DB(), query, args...)
	assertPlanUsesIndex(t, "closed native candidates", plan, "idx_cards_closed_work_page")
	assertPlanUsesIndex(t, "external candidates", plan, "idx_work_metadata_external")
	if _, err = ws.DB().Exec(`UPDATE work_metadata SET metadata_json=json_set(metadata_json,'$.project_ref','topic:project')`); err != nil {
		t.Fatal(err)
	}
	filter.ProjectRef = "topic:project"
	projectPage, err := s.ListReportWork(ctx, filter)
	if err != nil || len(projectPage.Work) != 2 || projectPage.Work[0]["id"] != "native-closed" || projectPage.Work[1]["id"] != "external-closed" {
		t.Fatalf("project closed selector duplicated native work: %+v %v", projectPage.Work, err)
	}
	filter.Limit = 1
	boundedPage, err := s.ListReportWork(ctx, filter)
	if err != nil || len(boundedPage.Work) != 1 || boundedPage.Work[0]["id"] != "native-closed" || !boundedPage.Truncated {
		t.Fatalf("closed candidate continuation lost: %+v %v", boundedPage, err)
	}
}

func TestDecisionWorkSnapshotsMatchPointProjection(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	s := NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	b, err := s.CreateBoard(ctx, "actor", map[string]any{"title": "board"})
	if err != nil {
		t.Fatal(err)
	}
	for _, authority := range []string{"nexus", "github"} {
		c, err := s.CreateBoardCard(ctx, "actor", b["id"].(string), AddBoardCardInput{CardID: authority, Title: authority, ColumnKey: "ready"})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = ws.DB().Exec(`INSERT INTO work_observations(id,card_id,idempotency_key,digest,observed_at,received_at,status,body_json) VALUES(?,?,?,'digest','now','now','ok','{"facts":{"phase":null},"source_revision":"external-7"}')`, authority+"-obs", authority, authority); err != nil {
			t.Fatal(err)
		}
		if _, err = ws.DB().Exec(`INSERT INTO work_metadata(card_id,authority,native_id,version,latest_observation_id,metadata_json,updated_at,updated_by) VALUES(?,?,?,3,?,?,'now','actor')`, authority, authority, authority, authority+"-obs", fmt.Sprintf(`{"source":{"authority":%q},"phase":"done"}`, authority)); err != nil {
			t.Fatal(err)
		}
		if authority == "nexus" {
			// Legacy placement is authoritative even when the denormalized card
			// board differs and placement metadata has no phase.
			if _, err = ws.DB().Exec(`UPDATE cards SET board_id=NULL WHERE id=?`, authority); err != nil {
				t.Fatal(err)
			}
			if _, err = ws.DB().Exec(`UPDATE ref_edges SET metadata_json='{}' WHERE edge_type='board_card' AND target_id=?`, authority); err != nil {
				t.Fatal(err)
			}
		}
		ref := c.Card["ref"].(string)
		point, err := s.GetWork(ctx, ref)
		if err != nil {
			t.Fatal(err)
		}
		batch, err := s.LiveWorkSnapshots(ctx, []string{ref, "card:missing"})
		if err != nil || len(batch) != 1 || batch[ref]["phase"] != point["phase"] || WorkDecisionRevision(batch[ref]) != WorkDecisionRevision(point) || workString(workMap(batch[ref]["source"])["authority"]) != authority {
			t.Fatalf("%s decision projection differs: point=%v batch=%v err=%v", authority, point, batch, err)
		}
	}
}
