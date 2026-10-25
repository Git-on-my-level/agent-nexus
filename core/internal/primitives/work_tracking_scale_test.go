package primitives_test

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/testutil/perfguard"
)

func TestPerformanceWorkTrackingDigestBudget(t *testing.T) {
	if os.Getenv("ANX_PERFORMANCE_TEST") != "1" {
		t.Skip("set ANX_PERFORMANCE_TEST=1")
	}
	var budget struct {
		Admission int    `json:"physical_admission"`
		Preview   int    `json:"preview_limit"`
		Queries   int    `json:"max_queries"`
		Rows      int    `json:"max_rows"`
		VM        uint64 `json:"max_vm_steps"`
		Denied    int    `json:"unrelated_private_cards"`
	}
	raw, err := os.ReadFile("testdata/work_tracking_read_budget.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(raw, &budget); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	s := primitives.NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	for i := 0; i < budget.Preview; i++ {
		card, err := s.CreateWork(ctx, "owner", "", map[string]any{"title": "Visible responsibility", "owner": "actor:reader"})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = ws.DB().Exec(`UPDATE work_tracking SET orphaned_at='2000-01-01',fallback_owner='actor:reader',next_digest_at='2000-01-01' WHERE card_id=?`, card["id"]); err != nil {
			t.Fatal(err)
		}
	}
	db, capture, err := perfguard.Open("file:" + ws.Layout().DatabasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	reader := primitives.NewTestStore(db, ws.Layout().ArtifactContentDir)
	for _, denied := range []bool{false, true} {
		if denied {
			board, err := s.CreateBoard(ctx, "owner", map[string]any{"title": "Private responsibility"})
			if err != nil {
				t.Fatal(err)
			}
			if _, err = s.PatchThread(ctx, "owner", board["thread_id"].(string), map[string]any{"pm_actor_id": "owner"}, nil); err != nil {
				t.Fatal(err)
			}
			if _, err = ws.DB().Exec(`WITH RECURSIVE n(i) AS(SELECT 0 UNION ALL SELECT i+1 FROM n WHERE i+1<?) INSERT INTO cards(id,board_id,title,summary,column_key,created_at,created_by,updated_at,updated_by) SELECT printf('private-tracking-%d',i),?,'Private secret','','ready','1999-01-01','owner','1999-01-01','owner' FROM n`, budget.Denied, board["id"]); err != nil {
				t.Fatal(err)
			}
			if _, err = ws.DB().Exec(`INSERT INTO work_tracking(card_id,orphaned_at,fallback_owner,next_digest_at,updated_at) SELECT id,'1999-01-01','actor:reader','1999-01-01','now' FROM cards WHERE id LIKE 'private-tracking-%'`); err != nil {
				t.Fatal(err)
			}
		}
		for _, actor := range []string{"reader", "stranger"} {
			scope := primitives.WithRequestAccessScope(ctx, primitives.AccessScope{ActorID: actor})
			scope, closeRead, err := reader.BeginResourceRead(scope)
			if err != nil {
				t.Fatal(err)
			}
			capture.Start()
			items, err := reader.TrackingDigestPreview(scope, actor)
			statements, queries, rows := capture.Stop()
			work := capture.Work()
			closeRead()
			if err != nil || capture.WorkError() != nil {
				t.Fatal(err, capture.WorkError())
			}
			want := budget.Preview
			if denied || actor != "reader" {
				want = 0
			}
			if len(items) != want || queries > budget.Queries || rows > budget.Rows || work.VMSteps > budget.VM {
				t.Fatalf("denied=%v actor=%s items=%d queries=%d rows=%d work=%+v", denied, actor, len(items), queries, rows, work)
			}
			for _, item := range items {
				if strings.Contains(item["title"].(string), "Private") {
					t.Fatal("private digest leaked", item)
				}
			}
			found := false
			for _, statement := range statements {
				if !strings.Contains(statement.SQL, "work_tracking_digest_positions") {
					continue
				}
				plan, err := perfguard.Explain(ctx, ws.DB(), statement)
				if err != nil {
					t.Fatal(err)
				}
				joined := strings.Join(plan, "\n")
				if strings.Contains(joined, "idx_work_tracking_owner_digest") {
					found = true
				}
				if strings.Contains(joined, "SCAN work_tracking\n") {
					t.Fatal("unbounded tracking scan", joined)
				}
			}
			if !found {
				t.Fatal("missing owner/digest indexed admission")
			}
			t.Logf("denied=%v actor=%s rows=%d queries=%d VM=%d full_scan=%d", denied, actor, rows, queries, work.VMSteps, work.FullScanSteps)
		}
	}
}
