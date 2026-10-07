package storage_test

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"strings"
	"sync/atomic"
	"testing"

	"agent-nexus-core/internal/blob"
	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/storage"
)

type upgradeCountingBlobs struct {
	blob.Backend
	reads atomic.Int64
}

func (b *upgradeCountingBlobs) Read(ctx context.Context, hash string) ([]byte, error) {
	b.reads.Add(1)
	return b.Backend.Read(ctx, hash)
}

func (b *upgradeCountingBlobs) OpenReadStream(ctx context.Context, hash string) (io.ReadCloser, int64, error) {
	b.reads.Add(1)
	return b.Backend.OpenReadStream(ctx, hash)
}

// Model a schema-63 store interrupted during the old content backfill: some
// manifests committed, while present and missing blobs still have NULL manifests.
// NewStore and another restart must preserve that state without retrying it.
func TestScopeUpgradeUnindexedPrivateArtifactsStayDenied(t *testing.T) {
	if testing.Short() {
		t.Skip("real storage upgrade privacy gate")
	}
	ctx := context.Background()
	root := t.TempDir()
	w, err := storage.InitializeWorkspace(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { w.Close() })
	s := primitives.NewTestStore(w.DB(), w.Layout().ArtifactContentDir)
	private, _, err := s.CreateDocument(ctx, "owner", map[string]any{"title": "private target"}, "secret", "text", []string{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.PatchThread(ctx, "owner", private["thread_id"].(string), map[string]any{"pm_actor_id": "owner"}, nil); err != nil {
		t.Fatal(err)
	}
	content := "UpgradeSecret body-only reference " + private["ref"].(string)
	create := func(title string) map[string]any {
		t.Helper()
		a, err := s.CreateArtifact(ctx, "owner", map[string]any{"kind": "note", "title": title, "refs": []string{}}, content, "text")
		if err != nil {
			t.Fatal(err)
		}
		return a
	}
	indexed := create("previously backfilled")
	unknown := create("unindexed private artifact")
	missing := create("missing historical content")
	doc, rev, err := s.CreateDocument(ctx, "owner", map[string]any{"title": "unindexed revision"}, content, "text", []string{})
	if err != nil {
		t.Fatal(err)
	}
	// Make the control an actual explicit-backfill result, rather than relying
	// only on a new upload's manifest. This persisted row must survive upgrade.
	if _, err = w.DB().Exec(`UPDATE artifacts SET content_refs_json=NULL WHERE id=?`, indexed["id"]); err != nil {
		t.Fatal(err)
	}
	if err = s.BackfillArtifactAccess(ctx); err != nil {
		t.Fatal(err)
	}
	for _, id := range []any{unknown["id"], missing["id"], rev["artifact_id"]} {
		if _, err = w.DB().Exec(`UPDATE artifacts SET content_refs_json=NULL WHERE id=?`, id); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = w.DB().Exec(`UPDATE artifacts SET content_hash=? WHERE id=?`, strings.Repeat("f", 64), missing["id"]); err != nil {
		t.Fatal(err)
	}
	if err = w.Close(); err != nil {
		t.Fatal(err)
	}
	b := &upgradeCountingBlobs{Backend: blob.NewFilesystemBackend(storage.NewLayout(root).ArtifactContentDir)}
	for attempt := 0; attempt < 2; attempt++ {
		startupReads := b.reads.Load()
		w, err = storage.InitializeWorkspace(ctx, root)
		if err != nil {
			t.Fatal(err)
		}
		s = primitives.NewStore(w.DB(), b, w.Layout().ArtifactContentDir)
		if got := b.reads.Load(); got != startupReads {
			t.Fatalf("attempt=%d constructor read historical blobs: before=%d after=%d", attempt, startupReads, got)
		}
		before := b.reads.Load()
		if err = w.Ping(ctx); err != nil {
			t.Fatal(err)
		}
		for _, actor := range []string{"owner", "stranger", "unauthorized-agent", "selected-pm", ""} {
			scope := primitives.WithAccessScope(ctx, primitives.AccessScope{ActorID: actor, PMActorID: "selected-pm"})
			for _, a := range []map[string]any{unknown, missing} {
				id := a["id"].(string)
				if _, err = s.GetArtifact(scope, id); !errors.Is(err, primitives.ErrNotFound) {
					t.Fatalf("attempt=%d actor=%q metadata: %v", attempt, actor, err)
				}
				if _, _, err = s.GetArtifactContent(scope, id); !errors.Is(err, primitives.ErrNotFound) {
					t.Fatalf("attempt=%d actor=%q content: %v", attempt, actor, err)
				}
				if _, err = s.GetArtifactContentHTTP(scope, id); !errors.Is(err, primitives.ErrNotFound) {
					t.Fatalf("attempt=%d actor=%q HTTP content: %v", attempt, actor, err)
				}
				for _, ref := range []string{id, a["ref"].(string)} {
					if s.CanAccessResource(scope, "artifact", ref) {
						t.Fatalf("actor=%q resolved unknown manifest %q", actor, ref)
					}
				}
			}
			rows, err := s.ListArtifacts(scope, primitives.ArtifactListFilter{IDs: []string{unknown["id"].(string), missing["id"].(string)}})
			if err != nil || len(rows) != 0 {
				t.Fatalf("actor=%q list=%v err=%v", actor, rows, err)
			}
			if _, _, err = s.GetDocument(scope, doc["id"].(string)); !errors.Is(err, primitives.ErrNotFound) {
				t.Fatalf("actor=%q unindexed document: %v", actor, err)
			}
			if _, err = s.GetDocumentRevisionByID(scope, rev["revision_id"].(string)); !errors.Is(err, primitives.ErrNotFound) {
				t.Fatalf("actor=%q unindexed revision: %v", actor, err)
			}
			matches, _, err := s.SearchDocuments(scope, primitives.DocumentSearchFilter{Query: "UpgradeSecret"})
			if err != nil || len(matches) != 0 {
				t.Fatalf("actor=%q unindexed search=%v err=%v", actor, matches, err)
			}
		}
		if got := b.reads.Load(); got != before {
			t.Fatalf("denied reads touched historical blobs: before=%d after=%d", before, got)
		}
		for _, actor := range []string{"owner", "stranger"} {
			scope := primitives.WithAccessScope(ctx, primitives.AccessScope{ActorID: actor})
			body, _, err := s.GetArtifactContent(scope, indexed["id"].(string))
			if actor == "owner" && (err != nil || string(body) != content) {
				t.Fatalf("previously backfilled owner content=%q err=%v", body, err)
			}
			if actor == "stranger" && !errors.Is(err, primitives.ErrNotFound) {
				t.Fatalf("previously backfilled private content exposed: %v", err)
			}
		}
		var pending int
		if err = w.DB().QueryRow(`SELECT count(*) FROM artifacts WHERE content_refs_json IS NULL`).Scan(&pending); err != nil || pending != 3 {
			t.Fatalf("pending manifests changed: %d %v", pending, err)
		}
		var manifest sql.NullString
		if err = w.DB().QueryRow(`SELECT content_refs_json FROM artifacts WHERE id=?`, indexed["id"]).Scan(&manifest); err != nil || !manifest.Valid {
			t.Fatalf("backfilled manifest lost: %v %v", manifest, err)
		}
		var installed int
		if err = w.DB().QueryRow(`SELECT count(*) FROM sqlite_schema WHERE name LIKE 'scope_%'`).Scan(&installed); err != nil || installed != 0 {
			t.Fatalf("unregistered scope schema installed at startup: %d %v", installed, err)
		}
		if err = w.Close(); err != nil {
			t.Fatal(err)
		}
	}
}
