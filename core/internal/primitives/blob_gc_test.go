package primitives

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"agent-nexus-core/internal/blob"
)

// reuseBlobsDuringPurge installs the purge seam: after the purge has committed
// its row deletions and before it collects blobs, a new artifact with the same
// bytes is created. Creation reuses the existing blob object, which is exactly
// the interleaving that used to delete content out from under the new row.
func reuseBlobsDuringPurge(t *testing.T, ctx context.Context, s *Store) *[]string {
	t.Helper()
	var created []string
	s.blobCollectHook = func(hashes []string) {
		s.blobCollectHook = nil
		for _, hash := range hashes {
			content, err := s.blob.Read(ctx, hash)
			if err != nil {
				t.Errorf("read blob %s before collection: %v", hash, err)
				continue
			}
			artifact, err := s.CreateArtifact(ctx, "writer", map[string]any{"kind": "note", "refs": []string{}}, string(content), "text")
			if err != nil {
				t.Errorf("create artifact reusing blob %s: %v", hash, err)
				continue
			}
			if got := anyStringValue(artifact["content_hash"]); got != hash {
				t.Errorf("reusing artifact hash = %s, want %s", got, hash)
				continue
			}
			created = append(created, anyStringValue(artifact["id"]))
		}
	}
	return &created
}

func assertReusedBlobsSurvive(t *testing.T, ctx context.Context, s *Store, created []string) {
	t.Helper()
	if len(created) == 0 {
		t.Fatal("purge collected no blobs, so the interleaving was not exercised")
	}
	for _, id := range created {
		if _, _, err := s.GetArtifactContent(ctx, id); err != nil {
			t.Fatalf("artifact %s lost its content after a concurrent purge: %v", id, err)
		}
		var hash string
		if err := s.db.QueryRowContext(ctx, `SELECT content_hash FROM artifacts WHERE id = ?`, id).Scan(&hash); err != nil {
			t.Fatal(err)
		}
		if _, found, err := s.lookupBlobLedgerEntry(ctx, hash); err != nil || !found {
			t.Fatalf("ledger entry for referenced blob %s: found=%v err=%v", hash, found, err)
		}
	}
}

func newBlobGCTestStore(t *testing.T) (context.Context, *Store) {
	t.Helper()
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ws.Close() })
	return ctx, NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
}

func TestPurgeTrashedArtifactKeepsBlobReusedBeforeCollection(t *testing.T) {
	t.Parallel()
	ctx, s := newBlobGCTestStore(t)
	original, err := s.CreateArtifact(ctx, "writer", map[string]any{"kind": "note", "refs": []string{}}, "shared bytes", "text")
	if err != nil {
		t.Fatal(err)
	}
	id := anyStringValue(original["id"])
	if _, err = s.TrashArtifact(ctx, "writer", id, "cleanup"); err != nil {
		t.Fatal(err)
	}
	created := reuseBlobsDuringPurge(t, ctx, s)
	if err = s.PurgeTrashedArtifact(ctx, id); err != nil {
		t.Fatal(err)
	}
	assertReusedBlobsSurvive(t, ctx, s, *created)
}

func TestPurgeDocumentKeepsBlobReusedBeforeCollection(t *testing.T) {
	t.Parallel()
	ctx, s := newBlobGCTestStore(t)
	document, _, err := s.CreateDocument(ctx, "writer", map[string]any{"title": "notes"}, "document bytes", "text", nil)
	if err != nil {
		t.Fatal(err)
	}
	id := anyStringValue(document["id"])
	if _, _, err = s.TrashDocument(ctx, "writer", id, "cleanup"); err != nil {
		t.Fatal(err)
	}
	created := reuseBlobsDuringPurge(t, ctx, s)
	if err = s.PurgeDocument(ctx, id); err != nil {
		t.Fatal(err)
	}
	assertReusedBlobsSurvive(t, ctx, s, *created)
}

func TestPurgeArchivedBoardCardKeepsBlobReusedBeforeCollection(t *testing.T) {
	t.Parallel()
	ctx, s := newBlobGCTestStore(t)
	board, err := s.CreateBoard(ctx, "writer", map[string]any{"title": "board"})
	if err != nil {
		t.Fatal(err)
	}
	card, err := s.CreateBoardCard(ctx, "writer", anyStringValue(board["id"]), AddBoardCardInput{Title: "card", Body: "card bytes", ColumnKey: "ready"})
	if err != nil {
		t.Fatal(err)
	}
	id := anyStringValue(card.Card["id"])
	if _, err = s.ArchiveBoardCard(ctx, "writer", "", id, RemoveBoardCardInput{}); err != nil {
		t.Fatal(err)
	}
	created := reuseBlobsDuringPurge(t, ctx, s)
	if err = s.PurgeArchivedBoardCard(ctx, "", id); err != nil {
		t.Fatal(err)
	}
	assertReusedBlobsSurvive(t, ctx, s, *created)
}

func TestPurgeTrashedArtifactDeletesUnreferencedBlob(t *testing.T) {
	t.Parallel()
	ctx, s := newBlobGCTestStore(t)
	original, err := s.CreateArtifact(ctx, "writer", map[string]any{"kind": "note", "refs": []string{}}, "lonely bytes", "text")
	if err != nil {
		t.Fatal(err)
	}
	id, hash := anyStringValue(original["id"]), anyStringValue(original["content_hash"])
	if _, err = s.TrashArtifact(ctx, "writer", id, "cleanup"); err != nil {
		t.Fatal(err)
	}
	if err = s.PurgeTrashedArtifact(ctx, id); err != nil {
		t.Fatal(err)
	}
	if _, err = s.blob.Read(ctx, hash); !errors.Is(err, blob.ErrBlobNotFound) {
		t.Fatalf("unreferenced blob survived purge: %v", err)
	}
	if _, found, err := s.lookupBlobLedgerEntry(ctx, hash); err != nil || found {
		t.Fatalf("ledger entry for purged blob: found=%v err=%v", found, err)
	}

	// A later create of the same bytes materializes a fresh copy and ledger entry.
	again, err := s.CreateArtifact(ctx, "writer", map[string]any{"kind": "note", "refs": []string{}}, "lonely bytes", "text")
	if err != nil {
		t.Fatal(err)
	}
	assertReusedBlobsSurvive(t, ctx, s, []string{anyStringValue(again["id"])})
}

// A plan prepared while the ledger still held the entry must not skip the insert
// when the blob was collected before the creator's transaction began.
func TestStaleLedgerPlanReinsertsCollectedEntry(t *testing.T) {
	t.Parallel()
	ctx, s := newBlobGCTestStore(t)
	original, err := s.CreateArtifact(ctx, "writer", map[string]any{"kind": "note", "refs": []string{}}, "stale plan", "text")
	if err != nil {
		t.Fatal(err)
	}
	hash := anyStringValue(original["content_hash"])
	plan, err := s.prepareBlobLedgerWritePlan(ctx, hash, int64(len("stale plan")))
	if err != nil || !plan.ledgerPresent {
		t.Fatalf("plan before collection: %+v %v", plan, err)
	}
	if _, err = s.db.ExecContext(ctx, `DELETE FROM artifacts WHERE id = ?`, anyStringValue(original["id"])); err != nil {
		t.Fatal(err)
	}
	if err = s.collectUnreferencedBlobs(ctx, []string{hash}); err != nil {
		t.Fatal(err)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	if err = s.applyBlobLedgerWritePlanTx(ctx, tx, plan); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if _, found, err := s.lookupBlobLedgerEntry(ctx, hash); err != nil || !found {
		t.Fatalf("stale plan left no ledger entry: found=%v err=%v", found, err)
	}
}

// committedLedgerBackend checks, at each blob delete, which ledger entries are
// visible to a reader outside the collector's transaction. That shows whether
// earlier hashes were committed (and the write lock released) before the next
// delete started.
type committedLedgerBackend struct {
	blob.Backend
	t       *testing.T
	store   *Store
	deleted []string
}

func (b *committedLedgerBackend) Delete(ctx context.Context, hash string) error {
	b.t.Helper()
	if _, found, err := b.store.lookupBlobLedgerEntry(context.Background(), hash); err != nil || !found {
		b.t.Errorf("ledger removal for %s committed before its delete: found=%v err=%v", hash, found, err)
	}
	for _, previous := range b.deleted {
		if _, found, err := b.store.lookupBlobLedgerEntry(context.Background(), previous); err != nil || found {
			b.t.Errorf("collection of %s still uncommitted while deleting %s: found=%v err=%v", previous, hash, found, err)
		}
	}
	b.deleted = append(b.deleted, hash)
	return b.Backend.Delete(ctx, hash)
}

func TestPurgeDocumentCollectsEachRevisionBlobInItsOwnTransaction(t *testing.T) {
	t.Parallel()
	ctx, s := newBlobGCTestStore(t)
	backend := &committedLedgerBackend{Backend: s.blob, t: t, store: s}
	s.blob = backend
	const revisions = 6
	document, revision, err := s.CreateDocument(ctx, "writer", map[string]any{"title": "history"}, "revision 0", "text", nil)
	if err != nil {
		t.Fatal(err)
	}
	id := anyStringValue(document["id"])
	for i := 1; i < revisions; i++ {
		_, revision, err = s.UpdateDocument(ctx, "writer", id, nil, anyStringValue(revision["revision_id"]), fmt.Sprintf("revision %d", i), "text", nil, nil)
		if err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err = s.TrashDocument(ctx, "writer", id, "cleanup"); err != nil {
		t.Fatal(err)
	}
	if err = s.PurgeDocument(ctx, id); err != nil {
		t.Fatal(err)
	}
	if len(backend.deleted) != revisions {
		t.Fatalf("deleted %d revision blobs, want %d", len(backend.deleted), revisions)
	}
	for _, hash := range backend.deleted {
		if _, err := s.blob.Read(ctx, hash); !errors.Is(err, blob.ErrBlobNotFound) {
			t.Fatalf("revision blob %s survived purge: %v", hash, err)
		}
	}
}

// promoteFailBackend makes every staged write fail to promote.
type promoteFailBackend struct{ blob.Backend }
type promoteFailStage struct{ blob.StagedWrite }

func (b promoteFailBackend) Write(ctx context.Context, hash string, data []byte) (blob.StagedWrite, error) {
	stage, err := b.Backend.Write(ctx, hash, data)
	if err != nil {
		return nil, err
	}
	return promoteFailStage{stage}, nil
}
func (promoteFailStage) Promote() error { return errors.New("injected promotion failure") }

func countRows(t *testing.T, ctx context.Context, s *Store, query string, args ...any) int {
	t.Helper()
	var n int
	if err := s.db.QueryRowContext(CanonicalMaintenanceContext(ctx), query, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestContentPromotionFailureCommitsNothing(t *testing.T) {
	t.Parallel()
	ctx, s := newBlobGCTestStore(t)
	board, err := s.CreateBoard(ctx, "writer", map[string]any{"title": "board"})
	if err != nil {
		t.Fatal(err)
	}
	boardID := anyStringValue(board["id"])
	topic, err := s.CreateTopic(ctx, "writer", map[string]any{"title": "subject", "summary": "subject"})
	if err != nil {
		t.Fatal(err)
	}
	cardsBefore := countRows(t, ctx, s, `SELECT COUNT(*) FROM cards`)
	artifactsBefore := countRows(t, ctx, s, `SELECT COUNT(*) FROM artifacts`)
	asksBefore := countRows(t, ctx, s, `SELECT COUNT(*) FROM events WHERE type = 'human_attention_requested'`)
	s.blob = promoteFailBackend{s.blob}

	if _, err := s.CreateBoardCard(ctx, "writer", boardID, AddBoardCardInput{Title: "single", Body: "single body", ColumnKey: "ready"}); err == nil {
		t.Fatal("card create succeeded although its content failed to promote")
	} else {
		var unknown *MutationOutcomeUnknown
		if errors.As(err, &unknown) {
			t.Fatalf("pre-commit promotion failure reported an unknown outcome: %v", err)
		}
	}
	if _, err := s.CreateBoardCardsBatch(ctx, "writer", boardID, nil, []AddBoardCardInput{
		{Title: "batch one", Body: "batch body one", ColumnKey: "ready"},
		{Title: "batch two", Body: "batch body two", ColumnKey: "ready"},
	}); err == nil {
		t.Fatal("card batch succeeded although its content failed to promote")
	}
	ask := func() map[string]any {
		return map[string]any{"type": "human_attention_requested", "thread_id": topic.Topic["thread_id"], "refs": []string{fmt.Sprint(topic.Topic["ref"])}, "payload": map[string]any{"subject_ref": topic.Topic["ref"], "title": "ask", "requester_actor_id": "writer"}}
	}
	if _, err := s.AppendTaskAttentionEvent(ctx, "writer", ask()); err == nil {
		t.Fatal("ask succeeded although its content failed to promote")
	}
	if _, _, err := s.AppendTaskAttentionEventBatch(ctx, "writer", "", "", []map[string]any{ask(), ask()}); err == nil {
		t.Fatal("ask batch succeeded although its content failed to promote")
	}

	if got := countRows(t, ctx, s, `SELECT COUNT(*) FROM cards`); got != cardsBefore {
		t.Fatalf("cards committed despite promotion failure: %d -> %d", cardsBefore, got)
	}
	if got := countRows(t, ctx, s, `SELECT COUNT(*) FROM artifacts`); got != artifactsBefore {
		t.Fatalf("artifacts committed despite promotion failure: %d -> %d", artifactsBefore, got)
	}
	if got := countRows(t, ctx, s, `SELECT COUNT(*) FROM events WHERE type = 'human_attention_requested'`); got != asksBefore {
		t.Fatalf("asks committed despite promotion failure: %d -> %d", asksBefore, got)
	}
}
