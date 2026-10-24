package primitives_test

import (
	"context"
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
