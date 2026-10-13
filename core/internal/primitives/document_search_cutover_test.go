package primitives

import (
	"agent-nexus-core/internal/blob"
	"agent-nexus-core/internal/storage"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestDocumentSearchCutoverPagingPrivacyAndFallback(t *testing.T) {
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { ws.Close() }()
	s := NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	create := func(title string) map[string]any {
		t.Helper()
		d, _, e := s.CreateDocument(ctx, "owner", map[string]any{"title": title, "tags": []string{"knowledge", "ops"}, "hosts": []string{"laptop"}}, "headneedle", "text", nil)
		if e != nil {
			t.Fatal(e)
		}
		return d
	}
	a, b, private := create("first"), create("second"), create("private")
	if _, err = s.PatchThread(ctx, "owner", anyStringValue(private["thread_id"]), map[string]any{"pm_actor_id": "owner"}, nil); err != nil {
		t.Fatal(err)
	}
	var comment string
	for i := 0; i < 12; i++ {
		c, e := s.CreateDocumentComment(ctx, "owner", anyStringValue(a["id"]), "commentneedle", "")
		if e != nil {
			t.Fatal(e)
		}
		comment = anyStringValue(c["id"])
	}
	for _, d := range []map[string]any{b, private} {
		if _, err = s.CreateDocumentComment(ctx, "owner", anyStringValue(d["id"]), "commentneedle", ""); err != nil {
			t.Fatal(err)
		}
	}
	limit := 1
	coverage := DocumentSearchCoverage{}
	request := WithRequestAccessScope(ctx, AccessScope{ActorID: "reader"})
	f := DocumentSearchFilter{Query: "commentneedle", Limit: &limit, Coverage: &coverage}
	first, cursor, err := s.SearchDocuments(request, f)
	if err != nil || len(first) != 1 || first[0]["id"] != a["id"] || cursor == "" || coverage.Mode != "independent" {
		t.Fatal(first, cursor, coverage, err)
	}
	f.Cursor = cursor
	second, _, err := s.SearchDocuments(request, f)
	if err != nil || len(second) != 1 || second[0]["id"] != b["id"] {
		t.Fatal("paging repeated first document or lost next", second, err)
	}
	// Query, filters and actor are all continuation bindings.
	for _, bad := range []DocumentSearchFilter{{Query: "headneedle", Cursor: cursor}, {Query: "commentneedle", Tag: "wrong", Cursor: cursor}, {Query: "commentneedle", Host: "wrong", Cursor: cursor}, {Query: "commentneedle", Knowledge: true, Cursor: cursor}} {
		if _, _, err = s.SearchDocuments(request, bad); !errors.Is(err, ErrInvalidCursor) {
			t.Fatal("accepted cross-query/filter cursor", err)
		}
	}
	if _, _, err = s.SearchDocuments(WithRequestAccessScope(ctx, AccessScope{ActorID: "other"}), f); !errors.Is(err, ErrInvalidCursor) {
		t.Fatal("accepted cross-principal cursor", err)
	}
	// Private-only matches must reveal neither hits nor a continuation.
	if _, err = s.CreateDocumentComment(ctx, "owner", anyStringValue(private["id"]), "secretcanary", ""); err != nil {
		t.Fatal(err)
	}
	for _, actor := range []string{"reader", "unauthorized-agent"} {
		got, next, e := s.SearchDocuments(WithRequestAccessScope(ctx, AccessScope{ActorID: actor}), DocumentSearchFilter{Query: "secretcanary", Limit: &limit})
		if e != nil || len(got) != 0 || next != "" {
			t.Fatal("private query leaked", got, next, e)
		}
	}
	if _, _, err = s.SearchDocuments(request, f); !errors.Is(err, ErrInvalidCursor) {
		t.Fatal("content mutation did not expire cursor", err)
	}
	if _, err = s.UpdateDocumentComment(ctx, "owner", anyStringValue(a["id"]), comment, "freshneedle"); err != nil {
		t.Fatal(err)
	}
	if got, _, e := s.SearchDocuments(request, DocumentSearchFilter{Query: "freshneedle"}); e != nil || len(got) != 1 {
		t.Fatal(got, e)
	}
	if _, err = s.DeleteDocumentComment(ctx, "owner", anyStringValue(a["id"]), comment); err != nil {
		t.Fatal(err)
	}
	if got, _, e := s.SearchDocuments(request, DocumentSearchFilter{Query: "freshneedle"}); e != nil || len(got) != 0 {
		t.Fatal(got, e)
	}
	if _, err = s.RestoreEvent(ctx, "owner", comment); err != nil {
		t.Fatal(err)
	}
	if got, _, e := s.SearchDocuments(request, DocumentSearchFilter{Query: "freshneedle", Knowledge: true, Host: "laptop", Tag: "ops"}); e != nil || len(got) != 1 {
		t.Fatal(got, e)
	}
	if got, _, e := s.SearchDocuments(request, DocumentSearchFilter{Query: "freshneedle", Host: "elsewhere"}); e != nil || len(got) != 0 {
		t.Fatal(got, e)
	}
	if _, _, err = s.ArchiveDocument(ctx, "owner", anyStringValue(a["id"])); err != nil {
		t.Fatal(err)
	}
	if got, _, e := s.SearchDocuments(request, DocumentSearchFilter{Query: "freshneedle"}); e != nil || len(got) != 0 {
		t.Fatal("archived search", got, e)
	}
	if got, _, e := s.SearchDocuments(request, DocumentSearchFilter{Query: "freshneedle", States: []string{"archived"}}); e != nil || len(got) != 1 {
		t.Fatal(got, e)
	}
	// Activate fallback immediately; repair progresses by one dirty document,
	// and no successful result may use the stale cache during that transition.
	t.Setenv("ANX_INDEPENDENT_DOCUMENT_SEARCH", "false")
	f = DocumentSearchFilter{Query: "freshneedle", States: []string{"archived"}, Coverage: &coverage}
	for i := 0; ; i++ {
		if i > 8 {
			t.Fatal("fallback failed to converge")
		}
		got, _, e := s.SearchDocuments(request, f)
		if errors.Is(e, ErrSearchRepairPending) {
			continue
		}
		if e != nil || len(got) != 1 || coverage.Mode != "aggregate" {
			t.Fatal(got, coverage, e)
		}
		break
	}
	// A new edit while fallback is selected invalidates the aggregate and is
	// repaired on the very next read, with no aggregate work on the write.
	if _, err = s.UpdateDocumentComment(ctx, "owner", anyStringValue(a["id"]), comment, "fallbackfresh"); err != nil {
		t.Fatal(err)
	}
	f.Query = "fallbackfresh"
	if got, _, e := s.SearchDocuments(request, f); e != nil || len(got) != 1 {
		t.Fatal(got, e)
	}
	t.Setenv("ANX_INDEPENDENT_DOCUMENT_SEARCH", "true")
	if got, _, e := s.SearchDocuments(request, f); e != nil || len(got) != 1 || coverage.Mode != "independent" {
		t.Fatal(got, e)
	}
}

func TestDocumentSearchHeadBackfillReopenAndRevisionBinding(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	ws, err := initializeTestWorkspace(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	s := NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	var documents []map[string]any
	for i := 0; i < 3; i++ {
		d, _, e := s.CreateDocument(ctx, "owner", map[string]any{"title": fmt.Sprintf("head %d", i)}, "bodyneedle", "text", nil)
		if e != nil {
			t.Fatal(e)
		}
		documents = append(documents, d)
		if _, e = s.CreateDocumentComment(ctx, "owner", anyStringValue(d["id"]), "backfillneedle", ""); e != nil {
			t.Fatal(e)
		}
	}
	// Reconstruct unbuilt metadata and exercise the actual head/link worker.
	if _, err = ws.DB().Exec(`DELETE FROM document_search_heads;UPDATE document_search_state SET done=0,cursor=NULL`); err != nil {
		t.Fatal(err)
	}
	if _, err = s.MaintainDocumentSearchBatch(ctx); err != nil {
		t.Fatal(err)
	}
	ws.Close()
	ws, err = storage.InitializeWorkspace(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { ws.Close() }()
	s = NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	for i := 0; ; i++ {
		if i > 30 {
			t.Fatal("head worker failed to converge")
		}
		done, e := s.MaintainDocumentSearchBatch(ctx)
		if e != nil {
			t.Fatal(e)
		}
		if done {
			break
		}
	}
	for _, q := range []string{"bodyneedle", "backfillneedle"} {
		got, _, e := s.SearchDocuments(ctx, DocumentSearchFilter{Query: q})
		if e != nil || len(got) != 3 {
			t.Fatal(q, got, e)
		}
	}
	limit := 1
	_, cursor, err := s.SearchDocuments(ctx, DocumentSearchFilter{Query: "bodyneedle", Limit: &limit})
	if err != nil || cursor == "" {
		t.Fatal(cursor, err)
	}
	ws.Close()
	ws, err = storage.InitializeWorkspace(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	s = NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	if got, _, e := s.SearchDocuments(ctx, DocumentSearchFilter{Query: "bodyneedle", Limit: &limit, Cursor: cursor}); e != nil || len(got) != 1 {
		t.Fatal("persistent cursor did not survive reopen", got, e)
	}
	d := documents[0]
	if _, _, err = s.UpdateDocument(ctx, "owner", anyStringValue(d["id"]), map[string]any{}, anyStringValue(d["head_revision_id"]), "changedhead", "text", nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, _, err = s.SearchDocuments(ctx, DocumentSearchFilter{Query: "bodyneedle", Limit: &limit, Cursor: cursor}); !errors.Is(err, ErrInvalidCursor) {
		t.Fatal("head edit did not expire continuation", err)
	}
	if got, _, e := s.SearchDocuments(ctx, DocumentSearchFilter{Query: "changedhead"}); e != nil || len(got) != 1 {
		t.Fatal(got, e)
	}
}

// Pause blob hydration at precisely the I/O/transaction boundary and mutate the
// canonical artifact without changing the document revision identity.
type changingSearchBlob struct {
	blob.Backend
	afterRead func()
}

func (b changingSearchBlob) Read(ctx context.Context, hash string) ([]byte, error) {
	data, err := b.Backend.Read(ctx, hash)
	if err == nil && b.afterRead != nil {
		b.afterRead()
	}
	return data, err
}
func TestDocumentSearchBackfillRejectsChangedArtifact(t *testing.T) {
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	s := NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	a, _, err := s.CreateDocument(ctx, "owner", map[string]any{"title": "a"}, strings.Repeat("oldbody ", 10000), "text", nil)
	if err != nil {
		t.Fatal(err)
	}
	b, _, err := s.CreateDocument(ctx, "owner", map[string]any{"title": "b"}, "replacementbody", "text", nil)
	if err != nil {
		t.Fatal(err)
	}
	var artifact, hash string
	if err = ws.DB().QueryRow(`SELECT artifact_id FROM document_revisions WHERE revision_id=?`, a["head_revision_id"]).Scan(&artifact); err != nil {
		t.Fatal(err)
	}
	if err = ws.DB().QueryRow(`SELECT a.content_hash FROM artifacts a JOIN document_revisions r ON r.artifact_id=a.id WHERE r.revision_id=?`, b["head_revision_id"]).Scan(&hash); err != nil {
		t.Fatal(err)
	}
	var truncated bool
	if err = ws.DB().QueryRow(`SELECT truncated FROM document_search_heads WHERE document_id=?`, a["id"]).Scan(&truncated); err != nil || !truncated {
		t.Fatal("raw body truncation lost", truncated, err)
	}
	if _, err = ws.DB().Exec(`UPDATE document_search_heads SET ready=0 WHERE document_id=?`, a["id"]); err != nil {
		t.Fatal(err)
	}
	original := s.blob
	s.blob = changingSearchBlob{original, func() {
		if _, e := ws.DB().Exec(`UPDATE artifacts SET content_hash=? WHERE id=?`, hash, artifact); e != nil {
			t.Fatal(e)
		}
	}}
	if _, err = s.MaintainDocumentSearchBatch(ctx); err != nil {
		t.Fatal(err)
	}
	var ready bool
	if err = ws.DB().QueryRow(`SELECT ready FROM document_search_heads WHERE document_id=?`, a["id"]).Scan(&ready); err != nil || ready {
		t.Fatal("certified stale body after concurrent blob mutation", ready, err)
	}
	s.blob = original
	for i := 0; i < 10; i++ {
		done, e := s.MaintainDocumentSearchBatch(ctx)
		if e != nil {
			t.Fatal(e)
		}
		if done {
			break
		}
		if i == 9 {
			t.Fatal("backfill failed to converge")
		}
	}
	got, _, err := s.SearchDocuments(ctx, DocumentSearchFilter{Query: "replacementbody"})
	if err != nil || len(got) != 2 {
		t.Fatal(got, err)
	}
	got, _, err = s.SearchDocuments(ctx, DocumentSearchFilter{Query: "oldbody"})
	if err != nil || len(got) != 0 {
		t.Fatal("stale head survived", got, err)
	}
}

func TestDocumentSearchUncertifiedHeadWriteAndReparent(t *testing.T) {
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	s := NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	a, _, err := s.CreateDocument(ctx, "owner", map[string]any{"title": "first"}, "head", "text", nil)
	if err != nil {
		t.Fatal(err)
	}
	b, _, err := s.CreateDocument(ctx, "owner", map[string]any{"title": "second"}, "head", "text", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.CreateDocumentComment(ctx, "owner", a["id"].(string), "historicalcomment", ""); err != nil {
		t.Fatal(err)
	}
	// Emulate the interval after upgrading an existing source corpus but before
	// its comment backfill. A head write must not certify empty aggregate text.
	if _, err = ws.DB().Exec(`DELETE FROM comment_search_heads;UPDATE comment_search_job SET done=0,cursor=NULL`); err != nil {
		t.Fatal(err)
	}
	if _, _, err = s.UpdateDocument(ctx, "owner", a["id"].(string), map[string]any{}, a["head_revision_id"].(string), "newhead", "text", nil, nil); err != nil {
		t.Fatal(err)
	}
	coverage := DocumentSearchCoverage{}
	got, _, err := s.SearchDocuments(ctx, DocumentSearchFilter{Query: "historicalcomment", Coverage: &coverage})
	if err != nil || len(got) != 1 || coverage.Mode != "aggregate" {
		t.Fatal("uncertified head made fallback stale", got, coverage, err)
	}
	for i := 0; i < 20; i++ {
		done, e := ws.MaintainCommentSearchBatch(ctx)
		if e != nil {
			t.Fatal(e)
		}
		if done {
			break
		}
	}
	if _, err = s.CreateDocumentComment(ctx, "owner", b["id"].(string), "newthreadcomment", ""); err != nil {
		t.Fatal(err)
	}
	if _, err = ws.DB().Exec(`INSERT INTO document_search_links_pending(document_id,cursor) VALUES(?,'zzzz') ON CONFLICT(document_id) DO UPDATE SET cursor='zzzz'`, a["id"]); err != nil {
		t.Fatal(err)
	}
	if _, err = ws.DB().Exec(`UPDATE documents SET thread_id=? WHERE id=?`, b["thread_id"], a["id"]); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 20; i++ {
		done, e := s.MaintainDocumentSearchBatch(ctx)
		if e != nil {
			t.Fatal(e)
		}
		if done {
			break
		}
		if i == 19 {
			t.Fatal("attachment failed")
		}
	}
	got, _, err = s.SearchDocuments(ctx, DocumentSearchFilter{Query: "newthreadcomment", Coverage: &coverage})
	if err != nil || len(got) != 2 || coverage.Mode != "independent" {
		t.Fatal("reparent reused old thread cursor", got, coverage, err)
	}
	got, _, err = s.SearchDocuments(ctx, DocumentSearchFilter{Query: "historicalcomment"})
	if err != nil || len(got) != 0 {
		t.Fatal("old thread terms survived reparent", got, err)
	}
}
