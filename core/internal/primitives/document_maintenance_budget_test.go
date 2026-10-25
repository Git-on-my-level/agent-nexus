package primitives_test

import (
	p "agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/testutil/perfguard"
	"context"
	"strings"
	"testing"
	"time"
)

func TestAggregateRepairDoesNotScanUnrelatedFTS(t *testing.T) {
	budget := idleMaintenanceBudget(t)["aggregate_repair"]
	t.Setenv("ANX_INDEPENDENT_DOCUMENT_SEARCH", "false")
	ctx := context.Background()
	w, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	s := p.NewTestStore(w.DB(), w.Layout().ArtifactContentDir)
	doc, _, err := s.CreateDocument(ctx, "owner", map[string]any{"title": "target"}, "needle", "text", nil)
	if err != nil {
		t.Fatal(err)
	}
	id := doc["id"].(string)
	_, err = w.DB().ExecContext(ctx, `WITH RECURSIVE n(i) AS(VALUES(1) UNION ALL SELECT i+1 FROM n WHERE i<4096) INSERT INTO document_fts(document_id,title,body,summary,source,tags,comments) SELECT 'unrelated-'||i,'unrelated',?,'','','','' FROM n`, strings.Repeat("unrelated corpus ", 256))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = w.DB().ExecContext(ctx, `INSERT OR IGNORE INTO document_search_dirty(document_id) VALUES(?)`, id); err != nil {
		t.Fatal(err)
	}
	db, capture, err := perfguard.Open("file:" + w.Layout().DatabasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	measured := p.NewTestStore(db, w.Layout().ArtifactContentDir)
	capture.Start()
	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		measured.RunDocumentSearchMaintenance(runCtx, func(e error) { t.Errorf("maintenance: %v", e) })
	}()
	cleared := false
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		var dirty int
		if e := w.DB().QueryRow(`SELECT count(*) FROM document_search_dirty WHERE document_id=?`, id).Scan(&dirty); e != nil {
			t.Error(e)
			break
		}
		if dirty == 0 {
			cleared = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	<-done
	_, _, _ = capture.Stop()
	if !cleared {
		t.Fatal("fixture repair never ran")
	}
	work := capture.Work()
	if err != nil || capture.WorkError() != nil {
		t.Fatalf("repair=%v instrumentation=%v", err, capture.WorkError())
	}
	t.Logf("vm=%d scans=%d", work.VMSteps, work.FullScanSteps)
	if work.VMSteps > budget["vm"] || work.FullScanSteps > budget["scan"] {
		t.Fatalf("one-document repair scanned unrelated corpus: vm=%d scans=%d", work.VMSteps, work.FullScanSteps)
	}
}
