package primitives

import (
	"agent-nexus-core/internal/storage"
	"context"
	"strings"
	"testing"
)

func TestReportWorkQueryIsScopedAndBounded(t *testing.T) {
	ctx := context.Background()
	ws, err := storage.InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	s := NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	board, err := s.CreateBoard(ctx, "actor", map[string]any{"title": "Selected"})
	if err != nil {
		t.Fatal(err)
	}
	other, err := s.CreateBoard(ctx, "actor", map[string]any{"title": "Other"})
	if err != nil {
		t.Fatal(err)
	}
	topic, err := s.CreateTopic(ctx, "actor", map[string]any{"title": "Project", "summary": "Project scope"})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		_, err = s.CreateWork(ctx, "actor", workString(board["id"]), map[string]any{"title": "Selected work", "project_ref": topic.Topic["ref"]})
		if err != nil {
			t.Fatal(err)
		}
	}
	// An out-of-scope candidate must not consume the selected scope's cap.
	_, err = s.CreateWork(ctx, "actor", workString(other["id"]), map[string]any{"title": "Other work"})
	if err != nil {
		t.Fatal(err)
	}
	for _, filter := range []ReportWorkFilter{
		{BoardIDs: []string{workString(board["id"])}, Limit: 2},
		{ProjectRef: workString(topic.Topic["ref"]), Limit: 2},
		{BoardIDs: []string{workString(board["id"])}, ProjectRef: workString(topic.Topic["ref"]), Limit: 2},
	} {
		query, args := reportWorkQuery(filter)
		plan := explainQueryPlan(t, ws.DB(), query, args...)
		if strings.Contains(plan, "USE TEMP B-TREE") {
			t.Fatalf("unbounded sort: %s", plan)
		}
		index := "idx_report_cards_board_active"
		if filter.ProjectRef != "" {
			index = "idx_work_metadata_project"
		}
		indexedBoard := filter.ProjectRef == "" && strings.Contains(plan, "idx_cards_access_board")
		if !strings.Contains(plan, index) && !indexedBoard {
			t.Fatalf("scope does not use index %s: %s", index, plan)
		}
		page, err := s.ListReportWork(ctx, filter)
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Work) != 2 || !page.Truncated {
			t.Fatalf("cap not applied: %#v", page)
		}
		for _, work := range page.Work {
			if workString(work["board_ref"]) != workString(board["ref"]) {
				t.Fatalf("out of scope: %#v", work)
			}
		}
	}
	page, err := s.ListReportWork(ctx, ReportWorkFilter{BoardIDs: []string{workString(other["id"])}, Limit: 2})
	if err != nil || len(page.Work) != 1 || page.Truncated {
		t.Fatalf("small scope: %#v %v", page, err)
	}
	_, err = s.ArchiveBoard(ctx, "actor", workString(board["id"]))
	if err != nil {
		t.Fatal(err)
	}
	page, err = s.ListReportWork(ctx, ReportWorkFilter{ProjectRef: workString(topic.Topic["ref"]), Limit: 2})
	if err != nil || len(page.Work) != 0 {
		t.Fatalf("archived board leaked: %#v %v", page, err)
	}
}
