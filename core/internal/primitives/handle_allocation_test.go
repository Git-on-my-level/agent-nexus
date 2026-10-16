package primitives

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"strings"
	"testing"

	"agent-nexus-core/internal/blob"
	"agent-nexus-core/internal/handles"
)

type countingRower struct {
	queryRower
	queries int
}

func (c *countingRower) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	c.queries++
	return c.queryRower.QueryRowContext(ctx, query, args...)
}

func insertArtifactHandle(t *testing.T, ctx context.Context, db *sql.DB, id, handle string) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `
		INSERT INTO artifacts(id, handle, kind, created_at, created_by, content_type, content_hash, refs_json, metadata_json, content_refs_json)
		VALUES (?, ?, 'note', '2026-01-01T00:00:00Z', 'actor-1', 'text', 'seed', '[]', '{}', '[]')`,
		id, handle,
	); err != nil {
		t.Fatalf("insert artifact %s: %v", handle, err)
	}
}

func TestUniqueHandleKeepsSequentialPrefixes(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()

	want := []string{"my-title", "my-title-2", "my-title-3", "my-title-4", "my-title-5", "my-title-6", "my-title-7", "my-title-8"}
	for i, handle := range want {
		got, err := uniqueHandleTx(ctx, ws.DB(), "artifact", "My Title", "artifact-seq-"+handle)
		if err != nil {
			t.Fatal(err)
		}
		if got != handle {
			t.Fatalf("attempt %d handle = %q, want %q", i, got, handle)
		}
		insertArtifactHandle(t, ctx, ws.DB(), "seq-"+handle, got)
	}

	sum := sha256.Sum256([]byte("artifact-seq-fallback"))
	fallback := "my-title-" + hex.EncodeToString(sum[:])[:handleHashSuffixStart]
	got, err := uniqueHandleTx(ctx, ws.DB(), "artifact", "My Title", "artifact-seq-fallback")
	if err != nil {
		t.Fatal(err)
	}
	if got == "my-title-9" || got != fallback {
		t.Fatalf("ninth handle = %q, want %q", got, fallback)
	}
}

func TestUniqueHandleFallbackIsBoundedDeterministicAndFits(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()

	const seed = "artifact-length-seed"
	sum := sha256.Sum256([]byte(seed))
	digest := hex.EncodeToString(sum[:])
	base := strings.Repeat("n", handles.MaxLength)
	for attempt := 0; attempt < handleSequentialAttempts; attempt++ {
		candidate, ok := handleAllocationCandidate(base, seed, attempt)
		if !ok {
			t.Fatalf("sequential attempt %d missing", attempt)
		}
		insertArtifactHandle(t, ctx, ws.DB(), "len-"+candidate, candidate)
	}

	counter := &countingRower{queryRower: ws.DB()}
	got, err := uniqueHandleTx(ctx, counter, "artifact", strings.Repeat("n", handles.MaxLength+20), seed)
	if err != nil {
		t.Fatal(err)
	}
	again, err := uniqueHandleTx(ctx, ws.DB(), "artifact", strings.Repeat("n", handles.MaxLength+20), seed)
	if err != nil {
		t.Fatal(err)
	}
	if got != again {
		t.Fatalf("seed %q allocated %q then %q", seed, got, again)
	}
	if len(got) > handles.MaxLength || !strings.HasSuffix(got, "-"+digest[:handleHashSuffixStart]) {
		t.Fatalf("fallback %q len %d, want suffix -%s within %d", got, len(got), digest[:handleHashSuffixStart], handles.MaxLength)
	}
	if counter.queries != handleSequentialAttempts*4+4 {
		t.Fatalf("fallback used %d queries, want %d", counter.queries, handleSequentialAttempts*4+4)
	}

	insertArtifactHandle(t, ctx, ws.DB(), "len-short-hash", got)
	lengthened, err := uniqueHandleTx(ctx, ws.DB(), "artifact", strings.Repeat("n", handles.MaxLength+20), seed)
	if err != nil {
		t.Fatal(err)
	}
	if lengthened == got || len(lengthened) > handles.MaxLength || !strings.HasSuffix(lengthened, "-"+digest[:handleHashSuffixStart+handleHashSuffixStep]) {
		t.Fatalf("colliding short suffix grew to %q, want suffix -%s", lengthened, digest[:handleHashSuffixStart+handleHashSuffixStep])
	}

	otherSum := sha256.Sum256([]byte("artifact-other-seed"))
	other, err := uniqueHandleTx(ctx, ws.DB(), "artifact", strings.Repeat("n", handles.MaxLength+20), "artifact-other-seed")
	if err != nil {
		t.Fatal(err)
	}
	if other == got || !strings.HasSuffix(other, "-"+hex.EncodeToString(otherSum[:])[:handleHashSuffixStart]) {
		t.Fatalf("other seed handle = %q", other)
	}

	longParent := revisionArtifactDesiredHandle(strings.Repeat("p", handles.MaxLength), 12)
	if len(longParent) > handles.MaxLength || !strings.HasSuffix(longParent, "-r12") {
		t.Fatalf("revision artifact handle = %q", longParent)
	}
	if revisionArtifactDesiredHandle("Ship the fix", 1) != "ship-the-fix-r1" {
		t.Fatalf("revision artifact handle = %q", revisionArtifactDesiredHandle("Ship the fix", 1))
	}
}

func TestCardAndDocumentRevisionHandlesSurviveFixedPrefixExhaustion(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	store := NewStore(ws.DB(), blob.NewFilesystemBackend(ws.Layout().ArtifactContentDir), ws.Layout().ArtifactContentDir)

	seedExhaustedHandles(t, ctx, ws.DB(), "exhaust-card-", "card-revision")
	seedExhaustedHandles(t, ctx, ws.DB(), "exhaust-doc-", "document-revision")

	for _, ref := range []string{"artifact:card-revision", "artifact:card-revision-1000", "artifact:document-revision", "artifact:document-revision-1000"} {
		resolved, err := store.ResolveResourceRef(ctx, ResourceRefInput{Ref: ref})
		if err != nil {
			t.Fatalf("resolve %s: %v", ref, err)
		}
		if resolved.Handle != strings.TrimPrefix(ref, "artifact:") || resolved.Type != "artifact" {
			t.Fatalf("resolved %s as %#v", ref, resolved)
		}
	}

	const cardSeed = "artifact-still-card-revision"
	counter := &countingRower{queryRower: ws.DB()}
	fallback, err := uniqueHandleTx(ctx, counter, "artifact", "card-revision", cardSeed)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(cardSeed))
	if fallback != "card-revision-"+hex.EncodeToString(sum[:])[:handleHashSuffixStart] {
		t.Fatalf("exhausted card-revision fallback = %q", fallback)
	}
	if counter.queries > handleSequentialAttempts*4+4 {
		t.Fatalf("exhausted allocation used %d queries", counter.queries)
	}
	docSeed := "artifact-still-document-revision"
	docFallback, err := uniqueHandleTx(ctx, ws.DB(), "artifact", "document-revision", docSeed)
	if err != nil {
		t.Fatal(err)
	}
	docSum := sha256.Sum256([]byte(docSeed))
	if docFallback != "document-revision-"+hex.EncodeToString(docSum[:])[:handleHashSuffixStart] {
		t.Fatalf("exhausted document-revision fallback = %q", docFallback)
	}

	board, err := store.CreateBoard(ctx, "actor-1", map[string]any{"title": "Ops"})
	if err != nil {
		t.Fatal(err)
	}
	added, err := store.AddBoardCard(ctx, "actor-1", board["id"].(string), AddBoardCardInput{Title: "Ship the fix", ColumnKey: "backlog"})
	if err != nil {
		t.Fatal(err)
	}
	cardHandle := added.Card["handle"].(string)
	artifactHandle := artifactHandleForCard(t, ctx, ws.DB(), added.Card["id"].(string))
	if artifactHandle != cardHandle+"-r1" {
		t.Fatalf("card artifact handle = %q, want %s-r1", artifactHandle, cardHandle)
	}
	assertUniqueArtifactHandle(t, ctx, ws.DB(), artifactHandle)
	summary := "updated"
	if _, _, err := store.CreateCardRevision(ctx, "actor-1", added.Card["id"].(string), CreateCardRevisionInput{Summary: &summary}); err != nil {
		t.Fatal(err)
	}
	revisedHandle := artifactHandleForCardRevision(t, ctx, ws.DB(), added.Card["id"].(string), 2)
	if revisedHandle != cardHandle+"-r2" {
		t.Fatalf("card revision artifact handle = %q, want %s-r2", revisedHandle, cardHandle)
	}
	assertUniqueArtifactHandle(t, ctx, ws.DB(), revisedHandle)

	document, revision, err := store.CreateDocument(ctx, "actor-1", map[string]any{"title": "Design note"}, "v1", "text", nil)
	if err != nil {
		t.Fatal(err)
	}
	docHandle := document["handle"].(string)
	docID := document["id"].(string)
	if got := artifactHandleForDocumentRevision(t, ctx, ws.DB(), docID, 1); got != docHandle+"-r1" {
		t.Fatalf("document create artifact = %q, want %s-r1", got, docHandle)
	}
	if _, _, err := store.UpdateDocument(ctx, "actor-1", docID, nil, revision["revision_id"].(string), "v2", "text", nil, nil); err != nil {
		t.Fatal(err)
	}
	if got := artifactHandleForDocumentRevision(t, ctx, ws.DB(), docID, 2); got != docHandle+"-r2" {
		t.Fatalf("document revision artifact = %q, want %s-r2", got, docHandle)
	}
	loaded, loadedRevision, err := store.GetDocument(ctx, docID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded["handle"] != docHandle || loadedRevision["artifact_ref"] != "artifact:"+docHandle+"-r2" {
		t.Fatalf("loaded document handle %#v artifact %#v", loaded["handle"], loadedRevision["artifact_ref"])
	}
	assertUniqueArtifactHandle(t, ctx, ws.DB(), docHandle+"-r1")
	assertUniqueArtifactHandle(t, ctx, ws.DB(), docHandle+"-r2")

	for _, ref := range []string{"artifact:card-revision", "artifact:card-revision-1000", "artifact:document-revision", "artifact:document-revision-1000"} {
		resolved, err := store.ResolveResourceRef(ctx, ResourceRefInput{Ref: ref})
		if err != nil || resolved.Handle != strings.TrimPrefix(ref, "artifact:") {
			t.Fatalf("existing ref %s changed: %#v %v", ref, resolved, err)
		}
	}
}

func seedExhaustedHandles(t *testing.T, ctx context.Context, db *sql.DB, idPrefix, base string) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `
		WITH RECURSIVE seq(n) AS (
			SELECT 1
			UNION ALL
			SELECT n+1 FROM seq WHERE n < 1000
		)
		INSERT INTO artifacts(id, handle, kind, created_at, created_by, content_type, content_hash, refs_json, metadata_json, content_refs_json)
		SELECT ? || n,
		       CASE WHEN n = 1 THEN ? ELSE ? || '-' || n END,
		       'note', '2026-01-01T00:00:00Z', 'actor-1', 'text', 'seed', '[]', '{}', '[]'
		FROM seq`, idPrefix, base, base); err != nil {
		t.Fatalf("seed %s handles: %v", base, err)
	}
}

func artifactHandleForDocumentRevision(t *testing.T, ctx context.Context, db *sql.DB, documentID string, revisionNumber int) string {
	t.Helper()
	var handle string
	if err := db.QueryRowContext(ctx, `
		SELECT a.handle
		  FROM document_revisions dr
		  JOIN artifacts a ON a.id = dr.artifact_id
		 WHERE dr.document_id = ? AND dr.revision_number = ?`, documentID, revisionNumber).Scan(&handle); err != nil {
		t.Fatal(err)
	}
	return handle
}

func artifactHandleForCard(t *testing.T, ctx context.Context, db *sql.DB, cardID string) string {
	t.Helper()
	return artifactHandleForCardRevision(t, ctx, db, cardID, 1)
}

func artifactHandleForCardRevision(t *testing.T, ctx context.Context, db *sql.DB, cardID string, revisionNumber int) string {
	t.Helper()
	var handle string
	if err := db.QueryRowContext(ctx, `
		SELECT a.handle
		  FROM card_revisions cr
		  JOIN artifacts a ON a.id = cr.artifact_id
		 WHERE cr.card_id = ? AND cr.revision_number = ?`, cardID, revisionNumber).Scan(&handle); err != nil {
		t.Fatal(err)
	}
	return handle
}

func assertUniqueArtifactHandle(t *testing.T, ctx context.Context, db *sql.DB, handle string) {
	t.Helper()
	var n int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM artifacts WHERE handle = ?`, handle).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 || handle == "" {
		t.Fatalf("artifact handle %q count = %d", handle, n)
	}
}
