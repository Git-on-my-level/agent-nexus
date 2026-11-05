package server

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/testutil/perfguard"
)

// GET /artifacts publishes owner_revision_ref and revision refs per row. A
// revision ref the page batch did not cache falls back to a point resolution,
// which must seek the (parent, revision_number) index instead of evaluating
// lower(handle||'-r'||n) over every revision once per listed artifact.
func TestArtifactListRevisionRefFallbackIsBounded(t *testing.T) {
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	store := primitives.NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	doc, _, err := store.CreateDocument(ctx, "owner", map[string]any{"title": "Revision parent"}, "body", "text", nil)
	if err != nil {
		t.Fatal(err)
	}
	docID, docHandle := anyString(doc["id"]), anyString(doc["handle"])
	refs := []string{"document_revision:" + docHandle + "-r1", "document_revision:missing-doc-r3", "card_revision:missing-card-r2"}
	const listed = 4
	for i := 0; i < listed; i++ {
		artifact, err := store.CreateArtifact(ctx, "owner", map[string]any{"kind": "doc", "summary": fmt.Sprint("listed ", i), "refs": refs}, "x", "text")
		if err != nil {
			t.Fatal(err)
		}
		// A non-UUID revision id spelled parent-rN is also a revision handle
		// spelling, so owner_revision_ref takes the handle route first.
		if _, err = ws.DB().ExecContext(ctx, `INSERT INTO document_revisions(revision_id,document_id,revision_number,artifact_id,refs_json,revision_hash,created_at,created_by) VALUES(?,?,?,?,'[]','h','2026-01-01T00:00:00Z','owner')`, fmt.Sprint("listed-r", i+10), docID, i+2, artifact["id"]); err != nil {
			t.Fatal(err)
		}
	}
	pool, capture, err := perfguard.Open("file:" + ws.Layout().DatabasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	measured := primitives.NewTestStore(pool, ws.Layout().ArtifactContentDir)
	scope := primitives.WithRequestAccessScope(ctx, primitives.AccessScope{ActorID: "owner"})
	var baseline string
	for _, noise := range []int{0, 4096} {
		if noise > 0 {
			if _, err = ws.DB().ExecContext(ctx, `WITH RECURSIVE n(i) AS(VALUES(1) UNION ALL SELECT i+1 FROM n WHERE i<4096) INSERT INTO document_revisions(revision_id,document_id,revision_number,artifact_id,refs_json,revision_hash,created_at,created_by) SELECT 'noise-rev-'||i,'noise-doc-'||(i%64),i,'noise-art-'||i,'[]','h','2026-01-01T00:00:00Z','owner' FROM n`); err != nil {
				t.Fatal(err)
			}
		}
		if _, err = measured.ListArtifacts(scope, primitives.ArtifactListFilter{}); err != nil {
			t.Fatal(err)
		}
		capture.Start()
		got, err := measured.ListArtifacts(scope, primitives.ArtifactListFilter{})
		statements, queries, rows := capture.Stop()
		if err != nil {
			t.Fatal(err)
		}
		work := capture.Work()
		if err = capture.WorkError(); err != nil {
			t.Fatal(err)
		}
		t.Logf("noise=%d artifacts=%d queries=%d rows=%d vm=%d scans=%d", noise, len(got), queries, rows, work.VMSteps, work.FullScanSteps)
		for _, stmt := range statements {
			if strings.Contains(stmt.SQL, "'-r' || dr.revision_number) = ?") || strings.Contains(stmt.SQL, "'-r' || cr.revision_number) = ?") {
				t.Fatalf("revision fallback evaluates computed handles over the relation: %s", stmt.SQL)
			}
		}
		if len(got) != listed+1 {
			t.Fatalf("artifacts = %d", len(got))
		}
		for i, artifact := range got[1:] {
			if want := fmt.Sprintf("document_revision:%s-r%d", docHandle, i+2); artifact["owner_revision_ref"] != want {
				t.Fatalf("owner_revision_ref = %v, want %s", artifact["owner_revision_ref"], want)
			}
			if pub := fmt.Sprint(artifact["refs"]); !strings.Contains(pub, "document_revision:"+docHandle+"-r1") || !strings.Contains(pub, "document_revision:missing-doc-r3") {
				t.Fatalf("public refs = %s", pub)
			}
		}
		capture.Start()
		resolved, err := measured.ResolveResourceRef(scope, primitives.ResourceRefInput{Ref: "document_revision:" + strings.ToUpper(docHandle) + "-r1"})
		_, pointQueries, _ := capture.Stop()
		if err != nil || resolved.CanonicalRef != "document_revision:"+docHandle+"-r1" {
			t.Fatalf("resolve revision handle = %+v, %v", resolved, err)
		}
		if point := capture.Work(); point.FullScanSteps >= 4096 || pointQueries > 2 {
			t.Fatalf("revision handle resolve: queries=%d work=%+v", pointQueries, point)
		}
		if noise == 0 {
			baseline = fmt.Sprint(got)
			continue
		}
		if fmt.Sprint(got) != baseline {
			t.Fatalf("noise changed the response:\n%s\n%v", baseline, got)
		}
		// Visiting the 4096 noise revisions even once costs >4096 scan steps.
		if work.FullScanSteps >= 4096 {
			t.Fatalf("artifact list scans revisions: work=%+v", work)
		}
	}
}
