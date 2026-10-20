package primitives

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"agent-nexus-core/internal/blob"
)

// remoteBlobBackend gives the filesystem backend the preload behavior of a
// remote object store: Preload and PreloadContent store the object before the
// write transaction, and the staged copy stays available so the Promote inside
// the transaction can restore an object that was removed in between.
type remoteBlobBackend struct {
	blob.Backend
	mu            sync.Mutex
	preloadCalls  int
	failPreloadOn int
	onPreload     func()
}

type remoteStage struct {
	inner   blob.StagedWrite
	backend *remoteBlobBackend
	hash    string
	data    []byte
}

func newRemoteBlobBackend(s *Store) *remoteBlobBackend {
	backend := &remoteBlobBackend{Backend: s.blob}
	s.blob = backend
	return backend
}

func (b *remoteBlobBackend) Write(ctx context.Context, hash string, data []byte) (blob.StagedWrite, error) {
	inner, err := b.Backend.Write(ctx, hash, data)
	if err != nil {
		return nil, err
	}
	return &remoteStage{inner: inner, backend: b, hash: hash, data: append([]byte(nil), data...)}, nil
}

func (b *remoteBlobBackend) WriteStream(ctx context.Context, src io.Reader, maxBytes int64) (string, int64, blob.StagedWrite, error) {
	data, err := io.ReadAll(src)
	if err != nil {
		return "", 0, nil, err
	}
	hash, n, inner, err := b.Backend.WriteStream(ctx, bytes.NewReader(data), maxBytes)
	if err != nil {
		return "", 0, nil, err
	}
	return hash, n, &remoteStage{inner: inner, backend: b, hash: hash, data: data}, nil
}

func (b *remoteBlobBackend) store(ctx context.Context, hash string, data []byte) (bool, error) {
	if b.onPreload != nil {
		b.onPreload()
	}
	if exists, err := b.Backend.Exists(ctx, hash); err != nil || exists {
		return false, err
	}
	staged, err := b.Backend.Write(ctx, hash, data)
	if err != nil {
		return false, err
	}
	defer func() { _ = staged.Cleanup() }()
	return true, staged.Promote()
}

func (b *remoteBlobBackend) PreloadContent(ctx context.Context, hash string, data []byte) (bool, error) {
	b.mu.Lock()
	b.preloadCalls++
	call := b.preloadCalls
	b.mu.Unlock()
	if b.failPreloadOn != 0 && call == b.failPreloadOn {
		return false, errors.New("injected preload failure")
	}
	return b.store(ctx, hash, data)
}

func (w *remoteStage) Preload() (bool, error) {
	return w.backend.store(context.Background(), w.hash, w.data)
}
func (w *remoteStage) Promote() error { return w.inner.Promote() }
func (w *remoteStage) Cleanup() error { return w.inner.Cleanup() }

// purgeAfterPreload trashes an artifact holding content and arranges for it to
// be purged after the next create that preloads the same bytes, before that
// create's write transaction begins. The purge sees no other reference, so it
// deletes the blob the create has just preloaded.
func purgeAfterPreload(t *testing.T, ctx context.Context, s *Store, content []byte) (hash string, purged *bool) {
	t.Helper()
	victim, err := s.CreateArtifact(ctx, "writer", map[string]any{"kind": "note", "refs": []string{}}, string(content), "text")
	if err != nil {
		t.Fatal(err)
	}
	hash = anyStringValue(victim["content_hash"])
	if _, err = s.TrashArtifact(ctx, "writer", anyStringValue(victim["id"]), "cleanup"); err != nil {
		t.Fatal(err)
	}
	done := false
	s.blobPreloadedHook = func(hashes []string) {
		found := false
		for _, h := range hashes {
			found = found || h == hash
		}
		if !found {
			return
		}
		s.blobPreloadedHook = nil
		if err := s.PurgeTrashedArtifact(ctx, anyStringValue(victim["id"])); err != nil {
			t.Errorf("purge after preload: %v", err)
			return
		}
		if _, err := s.blob.Read(ctx, hash); !errors.Is(err, blob.ErrBlobNotFound) {
			t.Errorf("purge did not delete the preloaded blob: %v", err)
			return
		}
		done = true
	}
	return hash, &done
}

func assertBlobReferencedAndPresent(t *testing.T, ctx context.Context, s *Store, hash string, purged bool) {
	t.Helper()
	if !purged {
		t.Fatal("the purge never ran between the preload and the write transaction")
	}
	if n := countRows(t, ctx, s, `SELECT COUNT(*) FROM artifacts WHERE content_hash = ?`, hash); n == 0 {
		t.Fatal("create committed no reference to the preloaded blob")
	}
	if _, err := s.blob.Read(ctx, hash); err != nil {
		t.Fatalf("committed reference points at a missing blob: %v", err)
	}
	if _, found, err := s.lookupBlobLedgerEntry(ctx, hash); err != nil || !found {
		t.Fatalf("ledger entry for referenced blob: found=%v err=%v", found, err)
	}
}

func mustCardContent(t *testing.T, title, body string) []byte {
	t.Helper()
	data, err := cardRevisionContent(title, body, nil)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestCreatesRestoreBlobsPurgedAfterPreload(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		content func(t *testing.T) []byte
		create  func(t *testing.T, ctx context.Context, s *Store) error
	}{
		{
			name:    "artifact",
			content: func(*testing.T) []byte { return []byte("artifact bytes") },
			create: func(t *testing.T, ctx context.Context, s *Store) error {
				_, err := s.CreateArtifact(ctx, "writer", map[string]any{"kind": "note", "refs": []string{}}, "artifact bytes", "text")
				return err
			},
		},
		{
			name:    "document create",
			content: func(*testing.T) []byte { return []byte("document bytes") },
			create: func(t *testing.T, ctx context.Context, s *Store) error {
				_, _, err := s.CreateDocument(ctx, "writer", map[string]any{"title": "doc"}, "document bytes", "text", nil)
				return err
			},
		},
		{
			name:    "document update",
			content: func(*testing.T) []byte { return []byte("revised bytes") },
			create: func(t *testing.T, ctx context.Context, s *Store) error {
				hook := s.blobPreloadedHook
				s.blobPreloadedHook = nil
				document, revision, err := s.CreateDocument(ctx, "writer", map[string]any{"title": "doc"}, "first bytes", "text", nil)
				s.blobPreloadedHook = hook
				if err != nil {
					return err
				}
				_, _, err = s.UpdateDocument(ctx, "writer", anyStringValue(document["id"]), nil, anyStringValue(revision["revision_id"]), "revised bytes", "text", nil, nil)
				return err
			},
		},
		{
			name:    "attachment",
			content: func(*testing.T) []byte { return []byte("attachment bytes") },
			create: func(t *testing.T, ctx context.Context, s *Store) error {
				_, err := s.CreateArtifactAttachment(ctx, "writer", map[string]any{"kind": "attachment", "refs": []string{}}, "text/plain", "notes.txt", strings.NewReader("attachment bytes"), 1<<20)
				return err
			},
		},
		{
			name:    "card",
			content: func(t *testing.T) []byte { return mustCardContent(t, "card title", "card body") },
			create: func(t *testing.T, ctx context.Context, s *Store) error {
				_, err := s.CreateBoardCard(ctx, "writer", testBoardID(t, ctx, s), AddBoardCardInput{Title: "card title", Body: "card body", ColumnKey: "ready"})
				return err
			},
		},
		{
			name:    "card batch",
			content: func(t *testing.T) []byte { return mustCardContent(t, "batch title", "batch body") },
			create: func(t *testing.T, ctx context.Context, s *Store) error {
				_, err := s.CreateBoardCardsBatch(ctx, "writer", testBoardID(t, ctx, s), nil, []AddBoardCardInput{
					{Title: "batch title", Body: "batch body", ColumnKey: "ready"},
					{Title: "other title", Body: "other body", ColumnKey: "ready"},
				})
				return err
			},
		},
		{
			name:    "ask",
			content: func(t *testing.T) []byte { return mustCardContent(t, "ask title", "") },
			create: func(t *testing.T, ctx context.Context, s *Store) error {
				_, err := s.AppendTaskAttentionEvent(ctx, "writer", testAsk(t, ctx, s, "ask title"))
				return err
			},
		},
		{
			name:    "ask batch",
			content: func(t *testing.T) []byte { return mustCardContent(t, "batch ask", "") },
			create: func(t *testing.T, ctx context.Context, s *Store) error {
				_, _, err := s.AppendTaskAttentionEventBatch(ctx, "writer", "", "", []map[string]any{testAsk(t, ctx, s, "batch ask"), testAsk(t, ctx, s, "other ask")})
				return err
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx, s := newBlobGCTestStore(t)
			newRemoteBlobBackend(s)
			hash, purged := purgeAfterPreload(t, ctx, s, tc.content(t))
			if err := tc.create(t, ctx, s); err != nil {
				t.Fatal(err)
			}
			assertBlobReferencedAndPresent(t, ctx, s, hash, *purged)
		})
	}
}

func testBoardID(t *testing.T, ctx context.Context, s *Store) string {
	t.Helper()
	hook := s.blobPreloadedHook
	s.blobPreloadedHook = nil
	defer func() { s.blobPreloadedHook = hook }()
	board, err := s.CreateBoard(ctx, "writer", map[string]any{"title": "board"})
	if err != nil {
		t.Fatal(err)
	}
	return anyStringValue(board["id"])
}

func testAsk(t *testing.T, ctx context.Context, s *Store, title string) map[string]any {
	t.Helper()
	hook := s.blobPreloadedHook
	s.blobPreloadedHook = nil
	defer func() { s.blobPreloadedHook = hook }()
	topic, err := s.CreateTopic(ctx, "writer", map[string]any{"title": "subject " + title, "summary": "subject"})
	if err != nil {
		t.Fatal(err)
	}
	return map[string]any{"type": "human_attention_requested", "thread_id": topic.Topic["thread_id"], "refs": []string{fmt.Sprint(topic.Topic["ref"])}, "payload": map[string]any{"subject_ref": topic.Topic["ref"], "title": title, "requester_actor_id": "writer"}}
}

// A create that preloads its blob while a collection holds the write lock,
// after the collection's recount and before its delete, must still commit with
// its content: it waits for the lock, finds the object gone and restores it.
func TestCreatePreloadedDuringCollectionRestoresBlob(t *testing.T) {
	t.Parallel()
	ctx, s := newBlobGCTestStore(t)
	newRemoteBlobBackend(s)
	victim, err := s.CreateArtifact(ctx, "writer", map[string]any{"kind": "note", "refs": []string{}}, "contended bytes", "text")
	if err != nil {
		t.Fatal(err)
	}
	hash := anyStringValue(victim["content_hash"])
	if _, err = s.TrashArtifact(ctx, "writer", anyStringValue(victim["id"]), "cleanup"); err != nil {
		t.Fatal(err)
	}

	preloaded := make(chan struct{})
	s.blobPreloadedHook = func([]string) { close(preloaded) }
	type result struct {
		artifact map[string]any
		err      error
	}
	created := make(chan result, 1)
	var once sync.Once
	s.blobCollectLockedHook = func(h string) {
		if h != hash {
			return
		}
		once.Do(func() {
			go func() {
				artifact, err := s.CreateArtifact(ctx, "writer", map[string]any{"kind": "note", "refs": []string{}}, "contended bytes", "text")
				created <- result{artifact, err}
			}()
			select {
			case <-preloaded:
			case <-time.After(10 * time.Second):
				t.Error("create did not preload while the collection held the lock")
			}
			select {
			case r := <-created:
				t.Errorf("create committed while the collection held the write lock: %v", r.err)
				created <- r
			case <-time.After(200 * time.Millisecond):
			}
		})
	}
	if err = s.PurgeTrashedArtifact(ctx, anyStringValue(victim["id"])); err != nil {
		t.Fatal(err)
	}
	r := <-created
	if r.err != nil {
		t.Fatal(r.err)
	}
	if _, _, err = s.GetArtifactContent(ctx, anyStringValue(r.artifact["id"])); err != nil {
		t.Fatalf("create that waited out the collection lost its content: %v", err)
	}
}

func assertBlobsAbsent(t *testing.T, ctx context.Context, s *Store, hashes ...string) {
	t.Helper()
	for _, hash := range hashes {
		if _, err := s.blob.Read(ctx, hash); !errors.Is(err, blob.ErrBlobNotFound) {
			t.Errorf("blob %s left behind by a failed create: %v", hash, err)
		}
		if _, found, err := s.lookupBlobLedgerEntry(ctx, hash); err != nil || found {
			t.Errorf("ledger entry %s left behind by a failed create: found=%v err=%v", hash, found, err)
		}
	}
}

func TestFailedCardBatchDiscardsPreloadedBlobs(t *testing.T) {
	t.Parallel()
	inputs := []AddBoardCardInput{
		{Title: "first", Body: "first body", ColumnKey: "ready"},
		{Title: "second", Body: "second body", ColumnKey: "ready"},
		{Title: "third", Body: "third body", ColumnKey: "ready"},
	}
	hashes := make([]string, len(inputs))
	for i, in := range inputs {
		hashes[i] = sha256Hex(mustCardContent(t, in.Title, in.Body))
	}

	t.Run("transaction fails after preload", func(t *testing.T) {
		t.Parallel()
		ctx, s := newBlobGCTestStore(t)
		newRemoteBlobBackend(s)
		if _, err := s.CreateBoardCardsBatch(ctx, "writer", "missing-board", nil, inputs); err == nil {
			t.Fatal("batch on a missing board succeeded")
		}
		assertBlobsAbsent(t, ctx, s, hashes...)
	})
	t.Run("later preload fails", func(t *testing.T) {
		t.Parallel()
		ctx, s := newBlobGCTestStore(t)
		board := testBoardID(t, ctx, s)
		backend := newRemoteBlobBackend(s)
		backend.failPreloadOn = 2
		if _, err := s.CreateBoardCardsBatch(ctx, "writer", board, nil, inputs); err == nil {
			t.Fatal("batch with a failed preload succeeded")
		}
		assertBlobsAbsent(t, ctx, s, hashes...)
	})
}

// A document create nested in an idempotent replay only commits a savepoint.
// When the replay's outer transaction then rolls back, the blob its preload
// created must be collected, not left untracked.
func TestReplayRollbackDiscardsNestedPreload(t *testing.T) {
	t.Parallel()
	for name, run := range map[string]func(map[string]any) (map[string]any, error){
		"handler error": func(map[string]any) (map[string]any, error) { return nil, errors.New("injected handler failure") },
		"receipt failure": func(document map[string]any) (map[string]any, error) {
			return map[string]any{"document": document, "unencodable": make(chan int)}, nil
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx, s := newBlobGCTestStore(t)
			newRemoteBlobBackend(s)
			content := "replayed document bytes"
			hash := sha256Hex([]byte(content))
			_, err := s.ExecuteWithReplay(ctx, "docs.create", "writer", "key-1", "hash-1", 201, func(ctx context.Context) (map[string]any, error) {
				document, _, err := s.CreateDocument(ctx, "writer", map[string]any{"title": "replayed"}, content, "text", nil)
				if err != nil {
					return nil, err
				}
				return run(document)
			})
			if err == nil {
				t.Fatal("replay succeeded although its outer transaction should roll back")
			}
			if n := countRows(t, ctx, s, `SELECT COUNT(*) FROM documents`); n != 0 {
				t.Fatalf("outer rollback kept %d documents", n)
			}
			assertBlobsAbsent(t, ctx, s, hash)
		})
	}
}

// With the content preloaded before the replay opens its outer transaction,
// the nested create stores nothing remotely while it holds the write lock.
func TestReplayDocumentContentPreloadsOutsideOuterTransaction(t *testing.T) {
	t.Parallel()
	ctx, s := newBlobGCTestStore(t)
	backend := newRemoteBlobBackend(s)
	var inside atomic.Bool
	var preloadsInside, preloadsOutside atomic.Int32
	backend.onPreload = func() {
		if inside.Load() {
			preloadsInside.Add(1)
		} else {
			preloadsOutside.Add(1)
		}
	}
	content := "preloaded replay bytes"
	replayCtx, finish, err := s.PreloadDocumentContentForReplay(ctx, content, "text")
	if err != nil {
		t.Fatal(err)
	}
	var documentID string
	_, err = s.ExecuteWithReplay(replayCtx, "docs.create", "writer", "key-2", "hash-2", 201, func(ctx context.Context) (map[string]any, error) {
		inside.Store(true)
		defer inside.Store(false)
		document, _, err := s.CreateDocument(ctx, "writer", map[string]any{"title": "preloaded"}, content, "text", nil)
		documentID = anyStringValue(document["id"])
		return map[string]any{"document": document}, err
	})
	finish()
	if err != nil {
		t.Fatal(err)
	}
	if got := preloadsInside.Load(); got != 0 {
		t.Fatalf("nested create preloaded %d object(s) under the outer write lock", got)
	}
	if got := preloadsOutside.Load(); got != 1 {
		t.Fatalf("preloads before the replay = %d, want 1", got)
	}
	hash := sha256Hex([]byte(content))
	if _, err := s.blob.Read(ctx, hash); err != nil {
		t.Fatalf("replayed document content missing: %v", err)
	}
	if _, found, err := s.lookupBlobLedgerEntry(ctx, hash); err != nil || !found {
		t.Fatalf("ledger entry: found=%v err=%v", found, err)
	}
	if _, _, err := s.GetDocument(ctx, documentID); err != nil {
		t.Fatal(err)
	}
}

// A replay that fails after the content was preloaded before it leaves no blob.
func TestFailedReplayDiscardsContentPreloadedBeforeIt(t *testing.T) {
	t.Parallel()
	ctx, s := newBlobGCTestStore(t)
	newRemoteBlobBackend(s)
	content := "abandoned replay bytes"
	replayCtx, finish, err := s.PreloadDocumentContentForReplay(ctx, content, "text")
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.ExecuteWithReplay(replayCtx, "docs.create", "writer", "key-3", "hash-3", 201, func(ctx context.Context) (map[string]any, error) {
		if _, _, err := s.CreateDocument(ctx, "writer", map[string]any{"title": "abandoned"}, content, "text", nil); err != nil {
			return nil, err
		}
		return nil, errors.New("injected handler failure")
	})
	finish()
	if err == nil {
		t.Fatal("replay succeeded")
	}
	assertBlobsAbsent(t, ctx, s, sha256Hex([]byte(content)))
}

// A failed pre-replay preload is a retryable storage error, and leaves nothing
// written; the caller fails the request instead of opening the replay.
func TestFailedReplayPreloadIsRetryable(t *testing.T) {
	t.Parallel()
	ctx, s := newBlobGCTestStore(t)
	backend := newRemoteBlobBackend(s)
	backend.failPreloadOn = 1
	content := "unstorable replay bytes"
	_, finish, err := s.PreloadDocumentContentForReplay(ctx, content, "text")
	finish()
	if !errors.Is(err, ErrBlobStorageUnavailable) {
		t.Fatalf("preload error = %v, want ErrBlobStorageUnavailable", err)
	}
	if n := countRows(t, ctx, s, `SELECT COUNT(*) FROM documents`); n != 0 {
		t.Fatalf("failed preload wrote %d documents", n)
	}
	assertBlobsAbsent(t, ctx, s, sha256Hex([]byte(content)))
}
