package primitives_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/resourceaccess"
	"agent-nexus-core/internal/testutil/perfguard"
)

func TestGroupingUngroupedBoundedBehindTenThousandDeniedCards(t *testing.T) {
	if testing.Short() {
		t.Skip("real 10k-card denial scale fixture")
	}
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	s := primitives.NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	public, err := s.CreateWork(ctx, "writer", "", map[string]any{"title": "Visible card"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.CreateBoard(ctx, "owner", map[string]any{"title": "Private"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.PatchThread(ctx, "owner", b["thread_id"].(string), map[string]any{"pm_actor_id": "owner"}, nil); err != nil {
		t.Fatal(err)
	}
	_, err = ws.DB().ExecContext(ctx, `WITH RECURSIVE n(value) AS (SELECT 1 UNION ALL SELECT value+1 FROM n WHERE value<10000)
 INSERT INTO cards(id,board_id,title,summary,column_key,created_at,created_by,updated_at,updated_by)
 SELECT 'denied-group-'||value,?,'Private','','ready','2050-01-01T00:00:00Z','owner','2050-01-01T00:00:00Z','owner' FROM n`, b["id"])
	if err != nil {
		t.Fatal(err)
	}
	readCtx := primitives.WithRequestAccessScope(ctx, primitives.AccessScope{ActorID: "reader"})
	q := resourceaccess.NewDB(ws.DB())
	var visible int
	if err = q.QueryRowContext(readCtx, `SELECT count(*) FROM cards WHERE id='denied-group-1'`).Scan(&visible); err != nil || visible != 0 {
		t.Fatalf("invalid denied fixture: visible=%d err=%v", visible, err)
	}
	db, capture, err := perfguard.Open("file:" + ws.Layout().DatabasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	reader := primitives.NewTestStore(db, ws.Layout().ArtifactContentDir)
	capture.Start()
	items, more, err := reader.UngroupedWork(readCtx, 20)
	_, queries, rows := capture.Stop()
	work := capture.Work()
	if err != nil || capture.WorkError() != nil || more || queries > 12 || rows > 500 || work.VMSteps > 50000 {
		t.Fatalf("items=%d more=%v err=%v instrument=%v queries=%d rows=%d work=%+v", len(items), more, err, capture.WorkError(), queries, rows, work)
	}
	t.Logf("items=%d VM=%d full_scans=%d", len(items), work.VMSteps, work.FullScanSteps)
	if _, err = ws.DB().ExecContext(ctx, `INSERT OR IGNORE INTO brief_initiatives(card_id,open,last_evidence_at) SELECT id,1,'2000-01-01T00:00:00Z' FROM cards`); err != nil {
		t.Fatal(err)
	}
	readCtx = primitives.WithRequestAccessScope(ctx, primitives.AccessScope{ActorID: "reader"})
	readCtx, closeRead, err := reader.BeginResourceRead(readCtx)
	if err != nil {
		t.Fatal(err)
	}
	defer closeRead()
	capture.Start()
	brief, err := reader.Brief(readCtx, primitives.BriefReader{ActorID: "reader", Kind: "human"}, time.Now())
	_, queries, rows = capture.Stop()
	work = capture.Work()
	if err != nil || capture.WorkError() != nil || queries > 100 || rows > 1024 || work.VMSteps > 50000 {
		t.Fatalf("brief err=%v instrument=%v queries=%d rows=%d work=%+v", err, capture.WorkError(), queries, rows, work)
	}
	raw, _ := json.Marshal(brief)
	if strings.Contains(string(raw), "Private") || !brief["initiative_budget"].(map[string]any)["truncated"].(bool) {
		t.Fatalf("hidden rows or admission metadata leaked: %s", raw)
	}
	t.Logf("brief VM=%d full_scans=%d", work.VMSteps, work.FullScanSteps)
	capture.Start()
	err = reader.EnrichCardPlans(readCtx, []map[string]any{public}, nil, time.Now())
	_, queries, rows = capture.Stop()
	work = capture.Work()
	if err != nil || capture.WorkError() != nil || queries > 100 || rows > 1024 || work.VMSteps > 50000 {
		t.Fatalf("health err=%v instrument=%v queries=%d rows=%d work=%+v", err, capture.WorkError(), queries, rows, work)
	}
	t.Logf("health VM=%d full_scans=%d", work.VMSteps, work.FullScanSteps)
}
