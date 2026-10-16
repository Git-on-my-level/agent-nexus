package storage

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"agent-nexus-core/internal/testutil/perfguard"
)

func TestBlockerWriteTenThousandAskReferencesBudget(t *testing.T) {
	ctx := context.Background()
	ws, err := InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	db := ws.DB()
	if _, err = db.Exec(`INSERT INTO cards(id,handle,board_id,rank,title,summary,column_key,created_at,created_by,updated_at,updated_by) VALUES('blocker','blocker','','','Blocker','','in_progress','2026-10-16T00:00:00Z','owner','2026-10-16T00:00:00Z','owner');
 WITH RECURSIVE n(x) AS (VALUES(1) UNION ALL SELECT x+1 FROM n WHERE x<10000)
 INSERT INTO brief_asks(ask_id,inbox_item_id,kind,requester,created_at,blocks_json,blocks_count) SELECT printf('ask-%05d',x),printf('ask:%05d',x),'ask','owner','2026-10-16T00:00:00Z','["card:blocker"]',1 FROM n;
 `); err != nil {
		t.Fatal(err)
	}
	var seq int
	var name, path string
	if err = db.QueryRow(`PRAGMA database_list`).Scan(&seq, &name, &path); err != nil {
		t.Fatal(err)
	}
	measured, capture, err := perfguard.Open("file:" + path)
	if err != nil {
		t.Fatal(err)
	}
	defer measured.Close()
	raw, err := os.ReadFile("testdata/blocker_write_budget.json")
	if err != nil {
		t.Fatal(err)
	}
	var budget struct {
		MaxVMSteps uint64 `json:"max_vm_steps"`
	}
	if err = json.Unmarshal(raw, &budget); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{`UPDATE cards SET column_key='done' WHERE id='blocker'`, `UPDATE cards SET title='Retitled',column_key='done' WHERE id='blocker'`} {
		capture.Start()
		_, err = measured.ExecContext(ctx, q)
		capture.Stop()
		work := capture.Work()
		if err != nil || capture.WorkError() != nil || work.VMSteps > budget.MaxVMSteps || work.FullScanSteps > 0 {
			t.Fatalf("%s: work=%+v error=%v instrument=%v", q, work, err, capture.WorkError())
		}
		t.Logf("%s: VM steps=%d full scans=%d", q, work.VMSteps, work.FullScanSteps)
	}
}
