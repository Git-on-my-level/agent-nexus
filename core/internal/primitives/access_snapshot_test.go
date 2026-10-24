package primitives

import (
	"agent-nexus-core/internal/resourceaccess"
	"context"
	"errors"
	"testing"
)

func TestResourceAccessReadCachePrefersNewestEpochAfterDelayedFill(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	s := NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	doc, _, err := s.CreateDocument(ctx, "owner", map[string]any{"title": "cache order control"}, "body", "text", nil)
	if err != nil {
		t.Fatal(err)
	}
	scope := AccessScope{ActorID: "reader"}
	olderRequest := canonicalRequestScope(ctx, scope)
	policy, _ := resourceaccess.PolicyFrom(olderRequest)
	// Capture through the actual SQL path without publishing to the shared
	// cache yet, as if this request's fill completed after a newer request.
	query, args := policy.ReadOnDB(olderRequest, struct{ resourceaccess.QueryRower }{ws.DB()}, `SELECT id FROM documents WHERE id=?`, []any{doc["id"]})
	var id string
	if err = ws.DB().QueryRowContext(olderRequest, query, args...).Scan(&id); err != nil {
		t.Fatal(err)
	}
	older := denialSnapshotFrom(olderRequest)
	if older == nil {
		t.Fatal("older request did not capture a denial snapshot")
	}
	if _, err = s.PatchThread(ctx, "owner", doc["thread_id"].(string), map[string]any{"pm_actor_id": "owner"}, nil); err != nil {
		t.Fatal(err)
	}
	newerRequest := canonicalRequestScope(ctx, scope)
	if s.CanAccessResource(newerRequest, "document", doc["id"].(string)) {
		t.Fatal("new ownership exposed the private document")
	}
	newer := denialSnapshotFrom(newerRequest)
	if newer == nil || newer.epoch <= older.epoch {
		t.Fatal("ownership change did not capture a newer epoch")
	}
	rememberReadDenial(ws.DB(), scope, older)
	for i := 0; i < 3; i++ {
		request := canonicalRequestScope(ctx, scope)
		if s.CanAccessResource(request, "document", doc["id"].(string)) {
			t.Fatal("delayed old capture exposed the private document")
		}
		if denialSnapshotFrom(request) != newer {
			t.Fatal("delayed old fill caused a rebuild instead of reusing the current closure")
		}
	}
}

func TestResourceAccessReadCacheSeparatesDatabaseAndPMScope(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	var captured *denialSnapshot
	for i := 0; i < 2; i++ {
		ws, err := initializeTestWorkspace(ctx, t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		defer ws.Close()
		s := NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
		doc, _, err := s.CreateDocument(ctx, "owner", map[string]any{"id": "cache-control", "title": "control"}, "body", "text", nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = s.PatchThread(ctx, "owner", doc["thread_id"].(string), map[string]any{"pm_actor_id": "owner"}, nil); err != nil {
			t.Fatal(err)
		}
		request := canonicalRequestScope(ctx, AccessScope{ActorID: "reader"})
		if s.CanAccessResource(request, "document", "cache-control") {
			t.Fatal("private document visible")
		}
		snapshot := denialSnapshotFrom(request)
		if snapshot == nil || snapshot == captured {
			t.Fatal("database identities shared a closure")
		}
		captured = snapshot
		pmRequest := canonicalRequestScope(ctx, AccessScope{ActorID: "reader", PMActorID: "reader"})
		if !s.CanAccessResource(pmRequest, "document", "cache-control") || denialSnapshotFrom(pmRequest) == snapshot {
			t.Fatal("PM authority shared another scope's closure")
		}
	}
}

func TestResourceAccessRequestSnapshotInvalidatesInsideStatement(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	s := NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	doc, _, err := s.CreateDocument(ctx, "owner", map[string]any{"id": "[]", "title": "source"}, "source", "text", nil)
	if err != nil {
		t.Fatal(err)
	}
	request := canonicalRequestScope(ctx, AccessScope{ActorID: "stranger"})
	assertVisible := func(want bool) {
		t.Helper()
		_, _, err := s.GetDocument(request, "[]")
		if (err == nil) != want {
			t.Fatalf("visible=%v want=%v err=%v", err == nil, want, err)
		}
	}
	assertVisible(true)
	snapshot := denialSnapshotFrom(request)
	if snapshot == nil {
		t.Fatal("request did not capture snapshot")
	}
	otherRequest := canonicalRequestScope(ctx, AccessScope{ActorID: "stranger"})
	if !s.CanAccessResource(otherRequest, "document", "[]") || denialSnapshotFrom(otherRequest) != snapshot {
		t.Fatal("unchanged database, principal and epoch did not reuse the bounded read closure")
	}
	for _, owner := range []string{"owner", "", "owner"} {
		if _, err := s.PatchThread(ctx, "owner", doc["thread_id"].(string), map[string]any{"pm_actor_id": owner}, nil); err != nil {
			t.Fatal(err)
		}
		assertVisible(owner == "")
		freshRequest := canonicalRequestScope(ctx, AccessScope{ActorID: "stranger"})
		if s.CanAccessResource(freshRequest, "document", "[]") != (owner == "") || denialSnapshotFrom(freshRequest) == snapshot {
			t.Fatal("new request reused a closure from an earlier authorization epoch")
		}
		if denialSnapshotFrom(request) != snapshot {
			t.Fatal("expected statement epoch fallback, not replacement of immutable snapshot")
		}
	}
	for _, scope := range []AccessScope{{ActorID: "owner"}, {ActorID: "stranger", PMActorID: "stranger"}} {
		nested := WithAccessScope(request, scope)
		if denialSnapshotFrom(nested) != nil {
			t.Fatal("rebound scope retained another principal's snapshot")
		}
		if !s.CanAccessResource(nested, "document", "[]") {
			t.Fatalf("nested privileged scope lost access: %+v", scope)
		}
	}
	privileged := canonicalRequestScope(ctx, AccessScope{ActorID: "stranger", PMActorID: "stranger"})
	if !s.CanAccessResource(privileged, "document", "[]") {
		t.Fatal("selected PM lost access")
	}
	if s.CanAccessResource(WithAccessScope(privileged, AccessScope{ActorID: "stranger"}), "document", "[]") {
		t.Fatal("PM scope downgrade reused privileged cache")
	}
	// Empty encoded reference arrays must not become the scalar document ID [].
	if _, err := s.CreateArtifact(request, "stranger", map[string]any{"kind": "note", "refs": []string{}}, "public artifact", "text"); err != nil {
		t.Fatalf("public write with empty JSON refs=%v", err)
	}
	if _, err := s.CreateArtifact(request, "stranger", map[string]any{"kind": "note", "refs": []string{}}, "", "text"); err != nil {
		t.Fatalf("public empty internal manifest=%v", err)
	}
	// Transactions do not consume request caches. Their own writes must be seen by
	// their following reads, and write argument checks cannot reuse stale answers.
	tx, err := s.db.BeginTx(request, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	var count int
	if err := tx.QueryRowContext(request, `SELECT count(*) FROM documents WHERE id='[]'`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("transaction visibility=%d err=%v", count, err)
	}
	if _, err := tx.ExecContext(request, `INSERT INTO artifacts(id,kind,created_at,created_by,content_type,content_hash,refs_json,content_refs_json) VALUES('forbidden','note','now','stranger','text','none',json_array(?),'[]')`, "[]"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("JSON scalar builder hid a private ID: %v", err)
	}
	if _, err := tx.ExecContext(request, `UPDATE documents SET title=? WHERE id=?`, "changed", "[]"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("transaction write guard=%v", err)
	}
}

func TestResourceAccessRequestSnapshotMissingEpochFallsBack(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	s := NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	doc, _, err := s.CreateDocument(ctx, "owner", map[string]any{"title": "source"}, "source", "text", nil)
	if err != nil {
		t.Fatal(err)
	}
	request := canonicalRequestScope(ctx, AccessScope{ActorID: "stranger"})
	if !s.CanAccessResource(request, "document", doc["id"].(string)) {
		t.Fatal("public source missing")
	}
	if _, err := ws.DB().ExecContext(ctx, `DELETE FROM resource_access_epoch`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PatchThread(ctx, "owner", doc["thread_id"].(string), map[string]any{"pm_actor_id": "owner"}, nil); err != nil {
		t.Fatal(err)
	}
	if s.CanAccessResource(request, "document", doc["id"].(string)) {
		t.Fatal("missing cache sentinel exposed private resource")
	}
	freshRequest := canonicalRequestScope(ctx, AccessScope{ActorID: "stranger"})
	if s.CanAccessResource(freshRequest, "document", doc["id"].(string)) || denialSnapshotFrom(freshRequest) != nil {
		t.Fatal("missing epoch allowed a shared cache hit on a new request")
	}
}

func TestResourceAccessRequestSnapshotSeesLateAliasAndProse(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	s := NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	doc, _, err := s.CreateDocument(ctx, "owner", map[string]any{"title": "source"}, "source", "text", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.PatchThread(ctx, "owner", doc["thread_id"].(string), map[string]any{"pm_actor_id": "owner"}, nil); err != nil {
		t.Fatal(err)
	}
	copy, _, err := s.CreateDocument(ctx, "owner", map[string]any{"title": "copy"}, "See _document:late_alias_", "text", nil)
	if err != nil {
		t.Fatal(err)
	}
	request := canonicalRequestScope(ctx, AccessScope{ActorID: "stranger"})
	if !s.CanAccessResource(request, "document", copy["id"].(string)) {
		t.Fatal("unresolved reference should remain visible")
	}
	if _, err := ws.DB().ExecContext(ctx, `INSERT INTO resource_handle_aliases(resource_type,alias_handle,resource_id,canonical_handle,created_at) VALUES('document','late_alias',?,?,'now')`, doc["id"], doc["handle"]); err != nil {
		t.Fatal(err)
	}
	if s.CanAccessResource(request, "document", copy["id"].(string)) {
		t.Fatal("late alias escaped cached visibility")
	}
}

// Epoch churn must not keep decoded copies of obsolete authority in the shared
// cache. Out-of-order fills cannot revive an earlier visibility decision.
func TestResourceAccessReadCacheReleasesObsoleteEpochs(t *testing.T) {
	t.Parallel()
	ws, err := initializeTestWorkspace(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	scope := AccessScope{ActorID: "epoch-memory-reader"}
	var latest *denialSnapshot
	for epoch := int64(1); epoch <= 40; epoch++ {
		latest = &denialSnapshot{epoch: epoch, rows: `[["card","private"]]`}
		rememberReadDenial(ws.DB(), scope, latest)
	}
	rememberReadDenial(ws.DB(), scope, &denialSnapshot{epoch: 1, rows: `[]`})
	if got := cachedReadDenial(ws.DB(), scope); got != latest || !got.denies("card", "private") {
		t.Fatal("delayed capture revived obsolete visibility")
	}
	readDenials.Lock()
	defer readDenials.Unlock()
	count := 0
	for _, key := range readDenials.keys {
		if key.db == ws.DB() && key.scope == scope {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("retained %d epochs for one reader, want 1", count)
	}
}

// A capture built for one exact-edge ledger rereads the selection in its own
// snapshot. A stale per-pool guess must be recomputed, not cached: the inactive
// ledger can be missing edges, which would hide a denial.
func TestResourceAccessRequestSnapshotRejectsStaleLedger(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	s := NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	private, _, err := s.CreateDocument(ctx, "owner", map[string]any{"title": "private"}, "private", "text", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.PatchThread(ctx, "owner", private["thread_id"].(string), map[string]any{"pm_actor_id": "owner"}, nil); err != nil {
		t.Fatal(err)
	}
	referrer, _, err := s.CreateDocument(ctx, "owner", map[string]any{"title": "referrer"}, "referrer", "text", nil)
	if err != nil {
		t.Fatal(err)
	}
	// Only the legacy exact ledger holds this edge; the compact ledger is empty
	// until the migration worker certifies the workspace.
	if _, err := ws.DB().ExecContext(ctx, `INSERT INTO resource_access_edges(source_kind,source_id,target_ref) VALUES('document',?,?)`, referrer["id"], "document:"+private["id"].(string)); err != nil {
		t.Fatal(err)
	}
	var legacy, compact int
	if err := ws.DB().QueryRowContext(ctx, `SELECT (SELECT count(*) FROM resource_access_exact_edges),(SELECT count(*) FROM resource_access_compact_edges),`+resourceaccess.CompactReaderSQL).Scan(&legacy, &compact, new(int)); err != nil || legacy == 0 || compact != 0 {
		t.Fatalf("fixture needs a legacy-only edge: legacy=%d compact=%d err=%v", legacy, compact, err)
	}
	denied := func() bool {
		t.Helper()
		if _, err := ws.DB().ExecContext(ctx, `UPDATE resource_access_epoch SET version=version+1`); err != nil {
			t.Fatal(err)
		}
		return !s.CanAccessResource(canonicalRequestScope(ctx, AccessScope{ActorID: "stranger"}), "document", referrer["id"].(string))
	}
	if !denied() || predictedAccessLedger(ws.DB()) != resourceaccess.LegacyLedger {
		t.Fatal("legacy capture missed its exact-edge denial or did not record its ledger")
	}
	rememberAccessLedger(ws.DB(), resourceaccess.CompactLedger)
	if !denied() {
		t.Fatal("stale compact ledger guess hid a legacy exact-edge denial")
	}
	if got := predictedAccessLedger(ws.DB()); got != resourceaccess.LegacyLedger {
		t.Fatalf("stale guess was not replaced by the observed ledger: %v", got)
	}
}

// These tests exercise the retained canonical closure cache used by legacy and
// receipt policies. Ordinary request admission has independent candidate parity
// and cold/warm/invalidation gates in access_admission_test.go and server tests.
func canonicalRequestScope(ctx context.Context, scope AccessScope) context.Context {
	return withRequestAccessEpoch(ctx, scope, "resource_access_epoch")
}
