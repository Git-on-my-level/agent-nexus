package primitives

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"agent-nexus-core/internal/storage"
)

type documentHistoryFixture struct {
	ws        *storage.Workspace
	s         *Store
	ctx       context.Context
	secretRef string
}

func newDocumentHistoryFixture(t *testing.T) *documentHistoryFixture {
	t.Helper()
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ws.Close() })
	s := NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	private, err := s.CreateWork(ctx, "owner", "", map[string]any{"title": "private history target"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.PatchThread(ctx, "owner", anyStringValue(private["thread_id"]), map[string]any{"pm_actor_id": "owner"}, nil); err != nil {
		t.Fatal(err)
	}
	return &documentHistoryFixture{ws: ws, s: s, ctx: ctx, secretRef: anyStringValue(private["ref"])}
}

// dashboard is structured content with many stable refs plus one per revision.
func dashboard(i int, extra ...string) map[string]any {
	refs := []any{}
	for n := 0; n < 20; n++ {
		refs = append(refs, fmt.Sprintf("card:stable-%d", n))
	}
	refs = append(refs, fmt.Sprintf("event:rolling-%d", i))
	for _, ref := range extra {
		refs = append(refs, ref)
	}
	return map[string]any{"title": fmt.Sprintf("Dashboard %d", i), "refs": refs}
}

// revise writes n further revisions and returns every revision ID in order.
func (f *documentHistoryFixture) revise(t *testing.T, doc, first map[string]any, n int, content func(int) any) []string {
	t.Helper()
	revisions := []string{anyStringValue(first["revision_id"])}
	for i := 1; i <= n; i++ {
		_, revision, err := f.s.UpdateDocument(f.ctx, "owner", anyStringValue(doc["id"]), nil, revisions[len(revisions)-1], content(i), "structured", nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		revisions = append(revisions, anyStringValue(revision["revision_id"]))
	}
	return revisions
}

func (f *documentHistoryFixture) count(t *testing.T, query string, args ...any) int {
	t.Helper()
	var n int
	if err := f.ws.DB().QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// perRevisionEdges counts edges still owned by a document's revisions or
// revision artifacts rather than by the document's deduplicated set.
func (f *documentHistoryFixture) perRevisionEdges(t *testing.T, documentID string) int {
	return f.count(t, `SELECT count(*) FROM resource_access_edges e JOIN document_revisions r ON (e.source_kind='artifact' AND e.source_id=r.artifact_id) OR (e.source_kind='document_revision' AND e.source_id=r.revision_id) WHERE r.document_id=?`, documentID)
}

func (f *documentHistoryFixture) documentEdges(t *testing.T, documentID string) int {
	return f.count(t, `SELECT count(*) FROM resource_access_edges WHERE source_kind='document_history' AND source_id=?`, documentID)
}

type documentAccessOutcome struct {
	document, head, oldRevision, oldArtifact, oldArtifactContent bool
	search                                                       int
}

func (f *documentHistoryFixture) outcome(t *testing.T, actor, documentID string, revisions []string, query string) documentAccessOutcome {
	t.Helper()
	scope := WithAccessScope(f.ctx, AccessScope{ActorID: actor})
	var oldArtifact string
	if err := f.ws.DB().QueryRow(`SELECT artifact_id FROM document_revisions WHERE revision_id=?`, revisions[0]).Scan(&oldArtifact); err != nil {
		t.Fatal(err)
	}
	var out documentAccessOutcome
	out.document = f.s.CanAccessResource(scope, "document", documentID)
	out.head = f.s.CanAccessResource(scope, "document_revision", revisions[len(revisions)-1])
	_, err := f.s.GetDocumentRevision(scope, documentID, revisions[0])
	if err != nil && !errors.Is(err, ErrNotFound) {
		t.Fatalf("%s old revision: %v", actor, err)
	}
	out.oldRevision = err == nil
	out.oldArtifact = f.s.CanAccessResource(scope, "artifact", oldArtifact)
	_, _, err = f.s.GetArtifactContent(scope, oldArtifact)
	if err != nil && !errors.Is(err, ErrNotFound) {
		t.Fatalf("%s old artifact content: %v", actor, err)
	}
	out.oldArtifactContent = err == nil
	matches, _, err := f.s.SearchDocuments(scope, DocumentSearchFilter{Query: query})
	if err != nil {
		t.Fatal(err)
	}
	for _, match := range matches {
		if anyStringValue(match["id"]) == documentID {
			out.search++
		}
	}
	return out
}

func allowedOutcome(search bool) documentAccessOutcome {
	out := documentAccessOutcome{true, true, true, true, true, 0}
	if search {
		out.search = 1
	}
	return out
}

func TestDocumentRevisionAccessEdgesStayPerDocument(t *testing.T) {
	t.Parallel()
	f := newDocumentHistoryFixture(t)
	// A superseded revision that referenced a private card keeps constraining
	// the document, its head and every revision, as before.
	tainted, first, err := f.s.CreateDocument(f.ctx, "owner", map[string]any{"title": "tainted dashboard"}, dashboard(0, f.secretRef), "structured", nil)
	if err != nil {
		t.Fatal(err)
	}
	taintedRevisions := f.revise(t, tainted, first, 30, func(i int) any { return dashboard(i) })
	public, first, err := f.s.CreateDocument(f.ctx, "owner", map[string]any{"title": "public dashboard"}, dashboard(0), "structured", nil)
	if err != nil {
		t.Fatal(err)
	}
	afterOne := f.documentEdges(t, anyStringValue(public["id"]))
	publicRevisions := f.revise(t, public, first, 30, func(i int) any { return dashboard(i) })

	for _, doc := range []map[string]any{tainted, public} {
		id := anyStringValue(doc["id"])
		if n := f.perRevisionEdges(t, id); n != 0 {
			t.Fatalf("%s kept %d per-revision edges", id, n)
		}
	}
	// Each revision adds only its few new atoms (rolling ref, title, its own
	// IDs, hash and timestamp), not another copy of the stable ones.
	if got := f.documentEdges(t, anyStringValue(public["id"])); got > afterOne+6*30 || got*4 > afterOne*31 || afterOne < 20 {
		t.Fatalf("document edges grew with revisions: %d after one, %d after 31", afterOne, got)
	}

	for _, actor := range []string{"stranger", "unauthorized-agent", "owner"} {
		got := f.outcome(t, actor, anyStringValue(tainted["id"]), taintedRevisions, "Dashboard")
		want := documentAccessOutcome{}
		if actor == "owner" {
			want = allowedOutcome(true)
		}
		if got != want {
			t.Errorf("%s tainted outcome %+v, want %+v", actor, got, want)
		}
		if got := f.outcome(t, actor, anyStringValue(public["id"]), publicRevisions, "Dashboard"); got != allowedOutcome(true) {
			t.Errorf("%s public outcome %+v", actor, got)
		}
	}
}

func TestDocumentRevisionAccessKeepsLateProseTargets(t *testing.T) {
	t.Parallel()
	f := newDocumentHistoryFixture(t)
	doc, first, err := f.s.CreateDocument(f.ctx, "owner", map[string]any{"title": "notes"}, "See document:late-secret for context.", "text", nil)
	if err != nil {
		t.Fatal(err)
	}
	revisions := f.revise(t, doc, first, 5, func(i int) any { return dashboard(i) })
	id := anyStringValue(doc["id"])
	if got := f.outcome(t, "stranger", id, revisions, "Dashboard"); got != allowedOutcome(true) {
		t.Fatalf("public notes hidden before target exists: %+v", got)
	}
	// A private target created after the superseded prose still constrains it.
	late, _, err := f.s.CreateDocument(f.ctx, "owner", map[string]any{"title": "late", "handle": "late-secret"}, "private", "text", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.PatchThread(f.ctx, "owner", anyStringValue(late["thread_id"]), map[string]any{"pm_actor_id": "owner"}, nil); err != nil {
		t.Fatal(err)
	}
	if got := f.outcome(t, "stranger", id, revisions, "Dashboard"); got != (documentAccessOutcome{}) {
		t.Fatalf("late private prose target escaped: %+v", got)
	}
	if got := f.outcome(t, "owner", id, revisions, "Dashboard"); got != allowedOutcome(true) {
		t.Fatalf("owner lost notes: %+v", got)
	}
}

func TestDocumentRevisionAccessRebuildAndPurge(t *testing.T) {
	t.Parallel()
	f := newDocumentHistoryFixture(t)
	doc, first, err := f.s.CreateDocument(f.ctx, "owner", map[string]any{"title": "rebuilt"}, dashboard(0, f.secretRef), "structured", nil)
	if err != nil {
		t.Fatal(err)
	}
	revisions := f.revise(t, doc, first, 3, func(i int) any { return dashboard(i) })
	id := anyStringValue(doc["id"])
	if f.outcome(t, "stranger", id, revisions, "Dashboard").document {
		t.Fatal("tainted document visible")
	}
	// A direct write that removes the private target from the only revision
	// that held it rebuilds the document's set from canonical rows.
	var artifact string
	if err = f.ws.DB().QueryRow(`SELECT artifact_id FROM document_revisions WHERE revision_id=?`, revisions[0]).Scan(&artifact); err != nil {
		t.Fatal(err)
	}
	before := f.documentEdges(t, id)
	if _, err = f.ws.DB().Exec(`UPDATE artifacts SET content_refs_json=json_array('card:stable-0') WHERE id=?`, artifact); err != nil {
		t.Fatal(err)
	}
	if got := f.outcome(t, "stranger", id, revisions, "Dashboard"); got != allowedOutcome(true) {
		t.Fatalf("removed target still constrains: %+v (edges %d -> %d)", got, before, f.documentEdges(t, id))
	}
	if n := f.perRevisionEdges(t, id); n != 0 {
		t.Fatalf("rebuild left %d per-revision edges", n)
	}
	if _, _, err = f.s.TrashDocument(f.ctx, "owner", id, "done"); err != nil {
		t.Fatal(err)
	}
	if err = f.s.PurgeDocument(f.ctx, id); err != nil {
		t.Fatal(err)
	}
	if n := f.documentEdges(t, id); n != 0 {
		t.Fatalf("purge kept %d document edges", n)
	}
	if n := f.count(t, `SELECT count(*) FROM resource_access_edges WHERE source_kind='artifact' AND source_id=?`, artifact); n != 0 {
		t.Fatalf("purge restored %d artifact edges", n)
	}
}

// Legacy databases hold one copy of every ref per revision. The bounded
// backfill moves them into the document set without changing any outcome.
func TestDocumentRevisionAccessBackfill(t *testing.T) {
	t.Parallel()
	f := newDocumentHistoryFixture(t)
	db := f.ws.DB()
	rows, err := db.Query(`SELECT name,sql FROM sqlite_schema WHERE type='trigger' AND name LIKE 'access_document_history_%' ORDER BY name`)
	if err != nil {
		t.Fatal(err)
	}
	var triggers [][2]string
	for rows.Next() {
		var name, sql string
		if err = rows.Scan(&name, &sql); err != nil {
			t.Fatal(err)
		}
		triggers = append(triggers, [2]string{name, sql})
	}
	rows.Close()
	if len(triggers) == 0 {
		t.Fatal("document history triggers missing")
	}
	for _, tr := range triggers {
		if _, err = db.Exec(`DROP TRIGGER ` + tr[0]); err != nil {
			t.Fatal(err)
		}
	}
	tainted, first, err := f.s.CreateDocument(f.ctx, "owner", map[string]any{"title": "legacy tainted"}, dashboard(0, f.secretRef), "structured", nil)
	if err != nil {
		t.Fatal(err)
	}
	taintedRevisions := f.revise(t, tainted, first, 12, func(i int) any { return dashboard(i) })
	public, first, err := f.s.CreateDocument(f.ctx, "owner", map[string]any{"title": "legacy public"}, dashboard(0), "structured", nil)
	if err != nil {
		t.Fatal(err)
	}
	publicRevisions := f.revise(t, public, first, 12, func(i int) any { return dashboard(i) })
	docs := map[string][]string{anyStringValue(tainted["id"]): taintedRevisions, anyStringValue(public["id"]): publicRevisions}
	outcomes := map[string]documentAccessOutcome{}
	legacy := 0
	for id, revisions := range docs {
		legacy += f.perRevisionEdges(t, id)
		for _, actor := range []string{"stranger", "owner"} {
			outcomes[actor+id] = f.outcome(t, actor, id, revisions, "Dashboard")
		}
	}
	totalBefore := f.count(t, `SELECT count(*) FROM resource_access_edges`)
	if legacy < 2*13*21 {
		t.Fatalf("fixture did not accumulate per-revision edges: %d", legacy)
	}
	for _, tr := range triggers {
		if _, err = db.Exec(tr[1]); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = db.Exec(`UPDATE resource_access_document_history_job SET cursor='',done=0`); err != nil {
		t.Fatal(err)
	}
	chunks := 0
	for done := false; !done; chunks++ {
		if chunks > 10000 {
			t.Fatal("backfill did not converge")
		}
		if done, err = f.ws.MaintainDocumentAccessHistoryBatch(f.ctx); errors.Is(err, context.DeadlineExceeded) {
			// A chunk that outlives its lease rolls back; the cap shrinks.
			continue
		} else if err != nil {
			t.Fatal(err)
		}
		if chunks == 1 {
			// Partially migrated state is already authorization-equivalent.
			for id, revisions := range docs {
				for _, actor := range []string{"stranger", "owner"} {
					if got := f.outcome(t, actor, id, revisions, "Dashboard"); got != outcomes[actor+id] {
						t.Fatalf("mid-backfill %s %s: %+v want %+v", actor, id, got, outcomes[actor+id])
					}
				}
			}
		}
	}
	for id, revisions := range docs {
		if n := f.perRevisionEdges(t, id); n != 0 {
			t.Fatalf("%s kept %d per-revision edges", id, n)
		}
		for _, actor := range []string{"stranger", "owner"} {
			if got := f.outcome(t, actor, id, revisions, "Dashboard"); got != outcomes[actor+id] {
				t.Fatalf("after backfill %s %s: %+v want %+v", actor, id, got, outcomes[actor+id])
			}
		}
	}
	totalAfter := f.count(t, `SELECT count(*) FROM resource_access_edges`)
	merged := 0
	for id := range docs {
		merged += f.documentEdges(t, id)
	}
	if merged*3 > legacy || totalAfter >= totalBefore-legacy+merged+1 {
		t.Fatalf("backfill did not shrink the graph: %d per-revision -> %d document edges, %d -> %d total", legacy, merged, totalBefore, totalAfter)
	}
	t.Logf("per-revision edges %d -> document edges %d; total %d -> %d in %d chunks", legacy, merged, totalBefore, totalAfter, chunks)
	// Idempotent: a restarted job finds nothing left to move.
	if _, err = db.Exec(`UPDATE resource_access_document_history_job SET cursor='',done=0`); err != nil {
		t.Fatal(err)
	}
	for done := false; !done; {
		if done, err = f.ws.MaintainDocumentAccessHistoryBatch(f.ctx); err != nil && !errors.Is(err, context.DeadlineExceeded) {
			t.Fatal(err)
		}
	}
	if got := f.count(t, `SELECT count(*) FROM resource_access_edges`); got != totalAfter {
		t.Fatalf("rerun changed edges: %d -> %d", totalAfter, got)
	}
	if !strings.Contains(fmt.Sprint(outcomes), "true") {
		t.Fatal("fixture recorded no visible outcome")
	}
}
