package primitives_test

import (
	"context"
	"testing"
	"time"

	"agent-nexus-core/internal/primitives"
)

func TestOverviewPrioritizesOpenWorkWithinCandidateLimit(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	s := primitives.NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	b, err := s.CreateBoard(ctx, "actor", map[string]any{"title": "Portfolio"})
	if err != nil {
		t.Fatal(err)
	}
	w, err := s.CreateWork(ctx, "actor", b["id"].(string), map[string]any{"title": "Closed history"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = ws.DB().Exec(`UPDATE cards SET column_key='done' WHERE id=?`, w["id"]); err != nil {
		t.Fatal(err)
	}
	if _, err = ws.DB().Exec(`WITH RECURSIVE history(n) AS (SELECT 1 UNION ALL SELECT n+1 FROM history WHERE n<2001)
 INSERT INTO cards(id,handle,board_id,thread_id,title,summary,column_key,rank,created_at,created_by,updated_at,updated_by,provenance_json)
 SELECT 'history-'||n,'history-'||n,board_id,thread_id,title,summary,column_key,rank,created_at,created_by,updated_at,updated_by,provenance_json FROM cards,history WHERE cards.id=?`, w["id"]); err != nil {
		t.Fatal(err)
	}
	open, err := s.CreateWork(ctx, "actor", b["id"].(string), map[string]any{"title": "Current initiative"})
	if err != nil {
		t.Fatal(err)
	}
	overview, err := s.OverviewVisible(ctx, nil, nil, nil, time.Now(), 0)
	if err != nil {
		t.Fatal(err)
	}
	items := overview["initiatives"].(map[string]any)["items"].([]map[string]any)
	if len(items) != 1 || items[0]["ref"] != open["ref"] || overview["initiatives"].(map[string]any)["truncated"] != true || len(overview["_visit_work"].([]map[string]any)) != 101 {
		t.Fatal("open work missing or candidate limit lost:", overview["initiatives"])
	}
}
