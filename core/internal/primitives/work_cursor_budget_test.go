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

func TestWorkCursorPersistentKeyReadBudget(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	db, capture, err := perfguard.Open("file:" + ws.Layout().DatabasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s := primitives.NewTestStore(db, ws.Layout().ArtifactContentDir)
	capture.Start()
	_, err = s.ListWork(ctx, primitives.WorkListFilter{Limit: 1})
	statements, _, _ := capture.Stop()
	if err != nil {
		t.Fatal(err)
	}
	var reads []perfguard.Statement
	for _, stmt := range statements {
		if strings.Contains(stmt.SQL, "SELECT cursor_key FROM document_search_state") {
			reads = append(reads, stmt)
		}
	}
	if len(reads) != 1 {
		t.Fatalf("expected one persistent-key lookup, got %d", len(reads))
	}
	plans, err := perfguard.Explain(ctx, ws.DB(), reads[0])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(plans, "\n"), "SEARCH document_search_state USING INTEGER PRIMARY KEY") {
		t.Fatalf("key lookup did not seek singleton: %v", plans)
	}
	raw, err := os.ReadFile("testdata/work_cursor_budget.json")
	if err != nil {
		t.Fatal(err)
	}
	var budget struct {
		MaxVMSteps uint64 `json:"max_vm_steps"`
	}
	if err = json.Unmarshal(raw, &budget); err != nil {
		t.Fatal(err)
	}
	capture.Start()
	var key []byte
	err = db.QueryRowContext(ctx, reads[0].SQL, reads[0].Args...).Scan(&key)
	_, queries, rows := capture.Stop()
	work := capture.Work()
	if err != nil || capture.WorkError() != nil || queries != 1 || rows != 1 || work.VMSteps > budget.MaxVMSteps || work.FullScanSteps != 0 {
		t.Fatalf("key lookup: SQL=%d rows=%d work=%+v error=%v instrument=%v", queries, rows, work, err, capture.WorkError())
	}
	t.Logf("persistent-key lookup: VM steps=%d SQL=%d rows=%d full scans=%d", work.VMSteps, queries, rows, work.FullScanSteps)
}
