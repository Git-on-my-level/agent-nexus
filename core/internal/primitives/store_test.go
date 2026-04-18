package primitives_test

import (
	"context"
	"encoding/json"
	"errors"
	"agent-nexus-core/internal/blob"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/storage"
)

func TestStoreAppendAndGetEventUnknownTypeAccepted(t *testing.T) {
	t.Parallel()

	workspace, err := storage.InitializeWorkspace(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("initialize workspace: %v", err)
	}
	defer workspace.Close()

	store := primitives.NewStore(workspace.DB(), blob.NewFilesystemBackend(workspace.Layout().ArtifactContentDir), workspace.Layout().ArtifactContentDir)

	event, err := store.AppendEvent(context.Background(), "actor-1", map[string]any{
		"type":       "custom_event_type",
		"refs":       []any{"customprefix:abc"},
		"summary":    "custom event",
		"provenance": map[string]any{"sources": []any{"inferred"}},
	})
	if err != nil {
		t.Fatalf("append event: %v", err)
	}

	loaded, err := store.GetEvent(context.Background(), event["id"].(string))
	if err != nil {
		t.Fatalf("get event: %v", err)
	}

	if loaded["type"] != "custom_event_type" {
		t.Fatalf("unexpected event type: %#v", loaded["type"])
	}
}

func TestListEventsAfterUsesChronologicalTimestampOrdering(t *testing.T) {
	t.Parallel()

	workspace, err := storage.InitializeWorkspace(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("initialize workspace: %v", err)
	}
	defer workspace.Close()

	store := primitives.NewStore(workspace.DB(), blob.NewFilesystemBackend(workspace.Layout().ArtifactContentDir), workspace.Layout().ArtifactContentDir)

	insertEvent := func(eventID string, ts string) {
		body := map[string]any{
			"id":        eventID,
			"type":      "message_posted",
			"ts":        ts,
			"actor_id":  "actor-1",
			"thread_id": "thread-1",
			"refs":      []string{"thread:thread-1"},
			"payload":   map[string]any{"text": eventID},
		}
		bodyJSON, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal event body: %v", err)
		}
		payloadJSON, err := json.Marshal(body["payload"])
		if err != nil {
			t.Fatalf("marshal event payload: %v", err)
		}
		refsJSON, err := json.Marshal(body["refs"])
		if err != nil {
			t.Fatalf("marshal event refs: %v", err)
		}
		if _, err := workspace.DB().ExecContext(
			context.Background(),
			`INSERT INTO events(id, type, ts, actor_id, thread_id, refs_json, payload_json, body_json, created_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)`,
			eventID,
			"message_posted",
			ts,
			"actor-1",
			"thread-1",
			string(refsJSON),
			string(payloadJSON),
			string(bodyJSON),
		); err != nil {
			t.Fatalf("insert raw event: %v", err)
		}
	}

	insertEvent("event-whole", "2026-03-29T15:28:28Z")
	insertEvent("event-fractional", "2026-03-29T15:28:28.1Z")

	events, err := store.ListEventsAfter(context.Background(), primitives.EventListFilter{Types: []string{"message_posted"}}, primitives.EventCursor{
		TS: "2026-03-29T15:28:28Z",
		ID: "event-whole",
	}, 10)
	if err != nil {
		t.Fatalf("list events after: %v", err)
	}

	if len(events) != 1 {
		t.Fatalf("expected one event after cursor, got %#v", events)
	}
	if got := events[0]["id"]; got != "event-fractional" {
		t.Fatalf("expected fractional event after cursor, got %#v", got)
	}
}

func TestArchiveEventCascadesMessageThreadReplies(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	workspace, err := storage.InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatalf("initialize workspace: %v", err)
	}
	defer workspace.Close()

	store := primitives.NewStore(workspace.DB(), blob.NewFilesystemBackend(workspace.Layout().ArtifactContentDir), workspace.Layout().ArtifactContentDir)

	parent, err := store.AppendEvent(ctx, "actor-1", map[string]any{
		"type":      "message_posted",
		"thread_id": "thread-cascade",
		"refs":      []any{"thread:thread-cascade"},
		"payload":   map[string]any{"text": "root"},
	})
	if err != nil {
		t.Fatalf("append parent: %v", err)
	}
	parentID, _ := parent["id"].(string)

	child1, err := store.AppendEvent(ctx, "actor-1", map[string]any{
		"type":      "message_posted",
		"thread_id": "thread-cascade",
		"refs":      []any{"thread:thread-cascade", "event:" + parentID},
		"payload":   map[string]any{"text": "reply1"},
	})
	if err != nil {
		t.Fatalf("append child1: %v", err)
	}
	child1ID, _ := child1["id"].(string)

	_, err = store.AppendEvent(ctx, "actor-1", map[string]any{
		"type":      "message_posted",
		"thread_id": "thread-cascade",
		"refs":      []any{"thread:thread-cascade", "event:" + child1ID},
		"payload":   map[string]any{"text": "reply2"},
	})
	if err != nil {
		t.Fatalf("append child2: %v", err)
	}

	if _, err := store.ArchiveEvent(ctx, "actor-2", parentID); err != nil {
		t.Fatalf("archive root: %v", err)
	}

	byThread, err := store.ListEventsByThread(ctx, "thread-cascade")
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	for _, ev := range byThread {
		if ev["type"] != "message_posted" {
			continue
		}
		if ev["archived_at"] == nil || ev["archived_at"] == "" {
			t.Fatalf("expected archived_at on %#v", ev["id"])
		}
		if ev["archived_by"] != "actor-2" {
			t.Fatalf("expected archived_by actor-2 on %v, got %#v", ev["id"], ev["archived_by"])
		}
	}

	if _, err := store.UnarchiveEvent(ctx, "actor-2", parentID); err != nil {
		t.Fatalf("unarchive root: %v", err)
	}
	byThread, err = store.ListEventsByThread(ctx, "thread-cascade")
	if err != nil {
		t.Fatalf("list events after unarchive: %v", err)
	}
	for _, ev := range byThread {
		if ev["type"] != "message_posted" {
			continue
		}
		if _, ok := ev["archived_at"]; ok {
			t.Fatalf("expected no archived_at on %#v", ev["id"])
		}
	}
}

func TestArchiveEventDoesNotCascadeNonMessagePosted(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	workspace, err := storage.InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatalf("initialize workspace: %v", err)
	}
	defer workspace.Close()

	store := primitives.NewStore(workspace.DB(), blob.NewFilesystemBackend(workspace.Layout().ArtifactContentDir), workspace.Layout().ArtifactContentDir)

	parent, err := store.AppendEvent(ctx, "actor-1", map[string]any{
		"type":      "note_added",
		"thread_id": "thread-x",
		"refs":      []any{"thread:thread-x"},
		"payload":   map[string]any{"text": "n"},
	})
	if err != nil {
		t.Fatalf("append: %v", err)
	}
	parentID, _ := parent["id"].(string)

	child, err := store.AppendEvent(ctx, "actor-1", map[string]any{
		"type":      "message_posted",
		"thread_id": "thread-x",
		"refs":      []any{"thread:thread-x", "event:" + parentID},
		"payload":   map[string]any{"text": "m"},
	})
	if err != nil {
		t.Fatalf("append child: %v", err)
	}
	childID, _ := child["id"].(string)

	if _, err := store.ArchiveEvent(ctx, "actor-2", parentID); err != nil {
		t.Fatalf("archive: %v", err)
	}

	other, err := store.GetEvent(ctx, childID)
	if err != nil {
		t.Fatalf("get child: %v", err)
	}
	if _, ok := other["archived_at"]; ok {
		t.Fatal("non-message_posted archive should not cascade to descendants")
	}
}

func TestCreateArtifactAcceptsSafeIDAndRejectsUnsafeIDs(t *testing.T) {
	t.Parallel()

	workspace, err := storage.InitializeWorkspace(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("initialize workspace: %v", err)
	}
	defer workspace.Close()

	store := primitives.NewStore(workspace.DB(), blob.NewFilesystemBackend(workspace.Layout().ArtifactContentDir), workspace.Layout().ArtifactContentDir)

	validIDs := []string{
		"artifact-123",
		"550e8400-e29b-41d4-a716-446655440000",
		"alpha_beta.1",
	}
	for _, validID := range validIDs {
		artifact, err := store.CreateArtifact(context.Background(), "actor-1", map[string]any{
			"id":   validID,
			"kind": "doc",
			"refs": []string{"thread:thread-1"},
		}, "content", "text")
		if err != nil {
			t.Fatalf("create artifact with valid id %q: %v", validID, err)
		}
		if artifact["id"] != validID {
			t.Fatalf("unexpected artifact id for %q: %#v", validID, artifact["id"])
		}
	}

	invalidIDs := []string{
		"dir/file",
		`dir\file`,
		"..",
		".",
		"/tmp/evil",
		"../../etc/passwd",
	}
	for _, invalidID := range invalidIDs {
		_, err := store.CreateArtifact(context.Background(), "actor-1", map[string]any{
			"id":   invalidID,
			"kind": "doc",
			"refs": []string{"thread:thread-1"},
		}, "content", "text")
		if !errors.Is(err, primitives.ErrInvalidArtifactID) {
			t.Fatalf("expected ErrInvalidArtifactID for %q, got %v", invalidID, err)
		}
		if err == nil || !strings.Contains(err.Error(), "artifact.id") {
			t.Fatalf("expected clear artifact.id error for %q, got %v", invalidID, err)
		}
	}
}

func TestCreateDocumentRejectsOversizedUpload(t *testing.T) {
	t.Parallel()

	workspace, err := storage.InitializeWorkspace(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("initialize workspace: %v", err)
	}
	defer workspace.Close()

	store := primitives.NewStore(
		workspace.DB(),
		blob.NewFilesystemBackend(workspace.Layout().ArtifactContentDir),
		workspace.Layout().ArtifactContentDir,
		primitives.WithWorkspaceQuota(primitives.WorkspaceQuota{
			MaxUploadBytes: 4,
			MaxBlobBytes:   1024,
		}),
	)

	_, _, err = store.CreateDocument(context.Background(), "actor-1", map[string]any{
		"id":    "doc-too-large",
		"title": "Too large",
	}, "hello", "text", nil)
	if err == nil {
		t.Fatal("expected upload quota error")
	}

	var violation *primitives.QuotaViolation
	if !errors.As(err, &violation) {
		t.Fatalf("expected quota violation, got %v", err)
	}
	if violation.Code != "request_too_large" || violation.Metric != "upload_bytes" {
		t.Fatalf("unexpected quota violation: %#v", violation)
	}
}

func TestCreateDocumentRejectsBlobQuotaExceeded(t *testing.T) {
	t.Parallel()

	workspace, err := storage.InitializeWorkspace(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("initialize workspace: %v", err)
	}
	defer workspace.Close()

	store := primitives.NewStore(
		workspace.DB(),
		blob.NewFilesystemBackend(workspace.Layout().ArtifactContentDir),
		workspace.Layout().ArtifactContentDir,
		primitives.WithWorkspaceQuota(primitives.WorkspaceQuota{
			MaxBlobBytes:   7,
			MaxUploadBytes: 1024,
		}),
	)

	if _, _, err := store.CreateDocument(context.Background(), "actor-1", map[string]any{
		"id":    "doc-1",
		"title": "Doc 1",
	}, "1111", "text", nil); err != nil {
		t.Fatalf("create first document: %v", err)
	}

	_, _, err = store.CreateDocument(context.Background(), "actor-1", map[string]any{
		"id":    "doc-2",
		"title": "Doc 2",
	}, "2222", "text", nil)
	if err == nil {
		t.Fatal("expected blob quota error")
	}

	var violation *primitives.QuotaViolation
	if !errors.As(err, &violation) {
		t.Fatalf("expected quota violation, got %v", err)
	}
	if violation.Code != "workspace_quota_exceeded" || violation.Metric != "blob_bytes" {
		t.Fatalf("unexpected quota violation: %#v", violation)
	}
}

func TestUpdateDocumentRejectsRevisionQuotaExceeded(t *testing.T) {
	t.Parallel()

	workspace, err := storage.InitializeWorkspace(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("initialize workspace: %v", err)
	}
	defer workspace.Close()

	store := primitives.NewStore(
		workspace.DB(),
		blob.NewFilesystemBackend(workspace.Layout().ArtifactContentDir),
		workspace.Layout().ArtifactContentDir,
		primitives.WithWorkspaceQuota(primitives.WorkspaceQuota{
			MaxBlobBytes:         1024,
			MaxUploadBytes:       1024,
			MaxDocumentRevisions: 1,
		}),
	)

	document, revision, err := store.CreateDocument(context.Background(), "actor-1", map[string]any{
		"id":    "doc-revisions",
		"title": "Doc revisions",
	}, "1111", "text", nil)
	if err != nil {
		t.Fatalf("create document: %v", err)
	}

	_, _, err = store.UpdateDocument(context.Background(), "actor-1", document["id"].(string), map[string]any{
		"title": "Doc revisions updated",
	}, revision["revision_id"].(string), "2222", "text", nil, nil)
	if err == nil {
		t.Fatal("expected revision quota error")
	}

	var violation *primitives.QuotaViolation
	if !errors.As(err, &violation) {
		t.Fatalf("expected quota violation, got %v", err)
	}
	if violation.Code != "workspace_quota_exceeded" || violation.Metric != "document_revision_count" {
		t.Fatalf("unexpected quota violation: %#v", violation)
	}
}

func TestCreateArtifactConflictDoesNotLeakStagedContent(t *testing.T) {
	t.Parallel()

	workspace, err := storage.InitializeWorkspace(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("initialize workspace: %v", err)
	}
	defer workspace.Close()

	store := primitives.NewStore(workspace.DB(), blob.NewFilesystemBackend(workspace.Layout().ArtifactContentDir), workspace.Layout().ArtifactContentDir)

	artifactID := "artifact-fixed"
	if _, err := store.CreateArtifact(context.Background(), "actor-1", map[string]any{
		"id":   artifactID,
		"kind": "doc",
		"refs": []string{"thread:thread-1"},
	}, "first content", "text"); err != nil {
		t.Fatalf("create initial artifact: %v", err)
	}

	if got := countArtifactContentFiles(t, workspace.Layout().ArtifactContentDir); got != 1 {
		t.Fatalf("expected 1 content file after initial create, got %d", got)
	}

	if _, err := store.CreateArtifact(context.Background(), "actor-2", map[string]any{
		"id":   artifactID,
		"kind": "doc",
		"refs": []string{"thread:thread-2"},
	}, "conflicting content", "text"); err == nil {
		t.Fatal("expected duplicate artifact id to fail")
	}

	if got := countArtifactContentFiles(t, workspace.Layout().ArtifactContentDir); got != 1 {
		t.Fatalf("expected duplicate artifact create not to leak content files, got %d", got)
	}

	content, _, err := store.GetArtifactContent(context.Background(), artifactID)
	if err != nil {
		t.Fatalf("get original artifact content: %v", err)
	}
	if string(content) != "first content" {
		t.Fatalf("unexpected original artifact content after conflict: %q", string(content))
	}
}

func TestWorkspaceUsageSummaryInitializesBlobLedgerFromCanonicalState(t *testing.T) {
	t.Parallel()

	workspace, err := storage.InitializeWorkspace(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("initialize workspace: %v", err)
	}
	defer workspace.Close()

	store := primitives.NewStore(
		workspace.DB(),
		blob.NewObjectStoreBackend(workspace.Layout().ArtifactContentDir),
		workspace.Layout().ArtifactContentDir,
	)

	if _, err := store.CreateArtifact(context.Background(), "actor-1", map[string]any{
		"id":   "artifact-summary",
		"kind": "doc",
		"refs": []string{"thread:thread-1"},
	}, "alpha", "text"); err != nil {
		t.Fatalf("create artifact: %v", err)
	}
	if _, _, err := store.CreateDocument(context.Background(), "actor-1", map[string]any{
		"id":    "doc-summary",
		"title": "Summary doc",
	}, "bravo", "text", nil); err != nil {
		t.Fatalf("create document: %v", err)
	}

	if _, err := workspace.DB().Exec(`DELETE FROM blob_usage_ledger`); err != nil {
		t.Fatalf("clear blob usage ledger: %v", err)
	}
	if _, err := workspace.DB().Exec(`DELETE FROM blob_usage_totals`); err != nil {
		t.Fatalf("clear blob usage totals: %v", err)
	}

	summary, err := store.GetWorkspaceUsageSummary(context.Background())
	if err != nil {
		t.Fatalf("get workspace usage summary: %v", err)
	}
	if summary.Usage.Artifacts != 2 {
		t.Fatalf("expected 2 artifacts, got %d", summary.Usage.Artifacts)
	}
	if summary.Usage.Documents != 1 {
		t.Fatalf("expected 1 document, got %d", summary.Usage.Documents)
	}
	if summary.Usage.Revisions != 1 {
		t.Fatalf("expected 1 document revision, got %d", summary.Usage.Revisions)
	}
	if summary.Usage.BlobObjects != 2 {
		t.Fatalf("expected 2 blob objects, got %d", summary.Usage.BlobObjects)
	}
	if summary.Usage.BlobBytes != int64(len("alpha")+len("bravo")) {
		t.Fatalf("unexpected blob bytes: got %d", summary.Usage.BlobBytes)
	}
}

func TestWorkspaceUsageSummaryDeduplicatesDuplicateBlobContent(t *testing.T) {
	t.Parallel()

	workspace, err := storage.InitializeWorkspace(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("initialize workspace: %v", err)
	}
	defer workspace.Close()

	store := primitives.NewStore(workspace.DB(), blob.NewFilesystemBackend(workspace.Layout().ArtifactContentDir), workspace.Layout().ArtifactContentDir)

	for _, artifactID := range []string{"artifact-duplicate-1", "artifact-duplicate-2"} {
		if _, err := store.CreateArtifact(context.Background(), "actor-1", map[string]any{
			"id":   artifactID,
			"kind": "doc",
			"refs": []string{"thread:thread-1"},
		}, "same-content", "text"); err != nil {
			t.Fatalf("create duplicate artifact %s: %v", artifactID, err)
		}
	}

	summary, err := store.GetWorkspaceUsageSummary(context.Background())
	if err != nil {
		t.Fatalf("get workspace usage summary: %v", err)
	}
	if summary.Usage.Artifacts != 2 {
		t.Fatalf("expected 2 artifacts, got %d", summary.Usage.Artifacts)
	}
	if summary.Usage.BlobObjects != 1 {
		t.Fatalf("expected 1 blob object, got %d", summary.Usage.BlobObjects)
	}
	if summary.Usage.BlobBytes != int64(len("same-content")) {
		t.Fatalf("expected %d blob bytes, got %d", len("same-content"), summary.Usage.BlobBytes)
	}
}

func TestWorkspaceUsageSummaryTracksCreateAndUpdateFlows(t *testing.T) {
	t.Parallel()

	workspace, err := storage.InitializeWorkspace(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("initialize workspace: %v", err)
	}
	defer workspace.Close()

	store := primitives.NewStore(workspace.DB(), blob.NewFilesystemBackend(workspace.Layout().ArtifactContentDir), workspace.Layout().ArtifactContentDir)

	document, revision, err := store.CreateDocument(context.Background(), "actor-1", map[string]any{
		"id":    "doc-update-summary",
		"title": "Summary doc",
	}, "bravo", "text", nil)
	if err != nil {
		t.Fatalf("create document: %v", err)
	}

	if _, _, err := store.UpdateDocument(context.Background(), "actor-2", document["id"].(string), map[string]any{
		"title": "Summary doc updated",
	}, revision["revision_id"].(string), "charlie", "text", nil, nil); err != nil {
		t.Fatalf("update document: %v", err)
	}

	summary, err := store.GetWorkspaceUsageSummary(context.Background())
	if err != nil {
		t.Fatalf("get workspace usage summary: %v", err)
	}
	if summary.Usage.Artifacts != 2 {
		t.Fatalf("expected 2 artifacts, got %d", summary.Usage.Artifacts)
	}
	if summary.Usage.Documents != 1 {
		t.Fatalf("expected 1 document, got %d", summary.Usage.Documents)
	}
	if summary.Usage.Revisions != 2 {
		t.Fatalf("expected 2 document revisions, got %d", summary.Usage.Revisions)
	}
	if summary.Usage.BlobObjects != 2 {
		t.Fatalf("expected 2 blob objects, got %d", summary.Usage.BlobObjects)
	}
	if summary.Usage.BlobBytes != int64(len("bravo")+len("charlie")) {
		t.Fatalf("unexpected blob bytes: got %d", summary.Usage.BlobBytes)
	}
}

func TestRebuildBlobUsageLedgerRepairsDrift(t *testing.T) {
	t.Parallel()

	workspace, err := storage.InitializeWorkspace(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("initialize workspace: %v", err)
	}
	defer workspace.Close()

	store := primitives.NewStore(workspace.DB(), blob.NewFilesystemBackend(workspace.Layout().ArtifactContentDir), workspace.Layout().ArtifactContentDir)

	firstArtifact, err := store.CreateArtifact(context.Background(), "actor-1", map[string]any{
		"id":   "artifact-rebuild-1",
		"kind": "doc",
		"refs": []string{"thread:thread-1"},
	}, "alpha", "text")
	if err != nil {
		t.Fatalf("create first artifact: %v", err)
	}
	secondArtifact, err := store.CreateArtifact(context.Background(), "actor-1", map[string]any{
		"id":   "artifact-rebuild-2",
		"kind": "doc",
		"refs": []string{"thread:thread-1"},
	}, "bravo", "text")
	if err != nil {
		t.Fatalf("create second artifact: %v", err)
	}

	if _, err := workspace.DB().Exec(`DELETE FROM blob_usage_ledger`); err != nil {
		t.Fatalf("clear blob usage ledger: %v", err)
	}
	if _, err := workspace.DB().Exec(`DELETE FROM blob_usage_totals`); err != nil {
		t.Fatalf("clear blob usage totals: %v", err)
	}

	secondHash, _ := secondArtifact["content_hash"].(string)
	if err := os.Remove(filepath.Join(workspace.Layout().ArtifactContentDir, secondHash)); err != nil {
		t.Fatalf("remove second blob content: %v", err)
	}

	rebuild, err := store.RebuildBlobUsageLedger(context.Background())
	if err != nil {
		t.Fatalf("rebuild blob usage ledger: %v", err)
	}
	if rebuild.CanonicalHashes != 2 {
		t.Fatalf("expected 2 canonical hashes, got %d", rebuild.CanonicalHashes)
	}
	if rebuild.MissingBlobObjects != 1 {
		t.Fatalf("expected 1 missing blob object, got %d", rebuild.MissingBlobObjects)
	}
	if rebuild.BlobObjects != 1 {
		t.Fatalf("expected 1 rebuilt blob object, got %d", rebuild.BlobObjects)
	}
	if rebuild.BlobBytes != int64(len("alpha")) {
		t.Fatalf("expected %d rebuilt blob bytes, got %d", len("alpha"), rebuild.BlobBytes)
	}

	summary, err := store.GetWorkspaceUsageSummary(context.Background())
	if err != nil {
		t.Fatalf("get workspace usage summary after rebuild: %v", err)
	}
	if summary.Usage.Artifacts != 2 {
		t.Fatalf("expected 2 artifacts after rebuild, got %d", summary.Usage.Artifacts)
	}
	if summary.Usage.BlobObjects != 1 {
		t.Fatalf("expected 1 blob object after rebuild, got %d", summary.Usage.BlobObjects)
	}
	if summary.Usage.BlobBytes != int64(len("alpha")) {
		t.Fatalf("expected %d blob bytes after rebuild, got %d", len("alpha"), summary.Usage.BlobBytes)
	}

	if _, _, err := store.GetArtifactContent(context.Background(), firstArtifact["id"].(string)); err != nil {
		t.Fatalf("get surviving artifact content: %v", err)
	}
}

func TestUpdateDocumentWriteFailureDoesNotLeakStagedContent(t *testing.T) {
	t.Parallel()

	workspace, err := storage.InitializeWorkspace(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("initialize workspace: %v", err)
	}
	defer workspace.Close()

	store := primitives.NewStore(workspace.DB(), blob.NewFilesystemBackend(workspace.Layout().ArtifactContentDir), workspace.Layout().ArtifactContentDir)

	document, revision, err := store.CreateDocument(context.Background(), "actor-1", map[string]any{
		"id":    "doc-locked",
		"title": "Locked doc",
	}, "initial text", "text", nil)
	if err != nil {
		t.Fatalf("create document: %v", err)
	}

	if got := countArtifactContentFiles(t, workspace.Layout().ArtifactContentDir); got != 1 {
		t.Fatalf("expected 1 content file after document create, got %d", got)
	}

	lockTx, err := workspace.DB().BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("begin lock transaction: %v", err)
	}
	defer func() { _ = lockTx.Rollback() }()

	if _, err := lockTx.ExecContext(context.Background(), `UPDATE documents SET updated_at = updated_at WHERE id = ?`, document["id"]); err != nil {
		t.Fatalf("acquire document write lock: %v", err)
	}

	updateCtx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, _, err = store.UpdateDocument(updateCtx, "actor-2", "doc-locked", map[string]any{
		"title": "Updated while locked",
	}, revision["revision_id"].(string), "updated text", "text", nil, nil)
	if err == nil {
		t.Fatal("expected locked document update to fail")
	}

	if got := countArtifactContentFiles(t, workspace.Layout().ArtifactContentDir); got != 1 {
		t.Fatalf("expected failed document update not to leak content files, got %d", got)
	}
}

func TestPatchThreadPreservesUnknownFieldsAndEmitsChangedFields(t *testing.T) {
	t.Parallel()

	workspace, err := storage.InitializeWorkspace(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("initialize workspace: %v", err)
	}
	defer workspace.Close()

	store := primitives.NewStore(workspace.DB(), blob.NewFilesystemBackend(workspace.Layout().ArtifactContentDir), workspace.Layout().ArtifactContentDir)

	initialBody := map[string]any{
		"title":         "original title",
		"tags":          []string{"alpha", "beta"},
		"unknown_field": map[string]any{"foo": "bar"},
	}
	initialBodyJSON, err := json.Marshal(initialBody)
	if err != nil {
		t.Fatalf("marshal initial thread body: %v", err)
	}

	_, err = workspace.DB().ExecContext(
		context.Background(),
		`INSERT INTO threads(id, kind, thread_id, updated_at, updated_by, body_json, provenance_json)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		"thread-patch-row-1",
		"thread",
		"thread-1",
		"2026-03-04T00:00:00Z",
		"actor-0",
		string(initialBodyJSON),
		`{"sources":["inferred"]}`,
	)
	if err != nil {
		t.Fatalf("insert initial thread row: %v", err)
	}

	patchResult, err := store.PatchThread(context.Background(), "actor-1", "thread-patch-row-1", map[string]any{
		"title": "updated title",
		"tags":  []any{"gamma"},
	}, nil)
	if err != nil {
		t.Fatalf("patch thread: %v", err)
	}

	if patchResult.Thread["title"] != "updated title" {
		t.Fatalf("title not patched: %#v", patchResult.Thread["title"])
	}

	unknown, ok := patchResult.Thread["unknown_field"].(map[string]any)
	if !ok || unknown["foo"] != "bar" {
		t.Fatalf("unknown field not preserved: %#v", patchResult.Thread["unknown_field"])
	}

	tags, ok := patchResult.Thread["tags"].([]any)
	if !ok || len(tags) != 1 || tags[0] != "gamma" {
		t.Fatalf("tags were not replaced wholesale: %#v", patchResult.Thread["tags"])
	}

	if patchResult.Event["type"] != "thread_updated" {
		t.Fatalf("unexpected event type: %#v", patchResult.Event["type"])
	}
	assertActorStatementProvenance(t, patchResult.Event)

	eventRefs, ok := patchResult.Event["refs"].([]string)
	if !ok || len(eventRefs) != 1 || eventRefs[0] != "thread:thread-patch-row-1" {
		t.Fatalf("unexpected event refs: %#v", patchResult.Event["refs"])
	}

	if patchResult.Event["thread_id"] != "thread-1" {
		t.Fatalf("expected thread_id on emitted event, got %#v", patchResult.Event["thread_id"])
	}

	payload, ok := patchResult.Event["payload"].(map[string]any)
	if !ok {
		t.Fatalf("missing event payload: %#v", patchResult.Event["payload"])
	}
	rawChanged, ok := payload["changed_fields"].([]string)
	if !ok {
		t.Fatalf("changed_fields should be []string, got %#v", payload["changed_fields"])
	}
	sort.Strings(rawChanged)
	if !reflect.DeepEqual(rawChanged, []string{"tags", "title"}) {
		t.Fatalf("unexpected changed_fields: %#v", rawChanged)
	}

	var eventCount int
	if err := workspace.DB().QueryRowContext(
		context.Background(),
		`SELECT COUNT(*) FROM events WHERE type = ? AND thread_id = ?`,
		"thread_updated",
		"thread-1",
	).Scan(&eventCount); err != nil {
		t.Fatalf("count thread_updated events: %v", err)
	}
	if eventCount != 1 {
		t.Fatalf("expected exactly one thread_updated event, got %d", eventCount)
	}

	secondPatch, err := store.PatchThread(context.Background(), "actor-2", "thread-patch-row-1", map[string]any{
		"title": "final title",
	}, nil)
	if err != nil {
		t.Fatalf("patch thread second time: %v", err)
	}

	secondTags, ok := secondPatch.Thread["tags"].([]any)
	if !ok || len(secondTags) != 1 || secondTags[0] != "gamma" {
		t.Fatalf("tags should remain unchanged when absent from patch: %#v", secondPatch.Thread["tags"])
	}

	secondPayload, ok := secondPatch.Event["payload"].(map[string]any)
	if !ok {
		t.Fatalf("missing second event payload: %#v", secondPatch.Event["payload"])
	}
	assertActorStatementProvenance(t, secondPatch.Event)
	secondChanged, ok := secondPayload["changed_fields"].([]string)
	if !ok || len(secondChanged) != 1 || secondChanged[0] != "title" {
		t.Fatalf("unexpected second changed_fields: %#v", secondPayload["changed_fields"])
	}
}

func TestGetThreadCanonicalizesLegacyTopicRefToSubjectRef(t *testing.T) {
	t.Parallel()

	workspace, err := storage.InitializeWorkspace(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("initialize workspace: %v", err)
	}
	defer workspace.Close()

	store := primitives.NewStore(workspace.DB(), blob.NewFilesystemBackend(workspace.Layout().ArtifactContentDir), workspace.Layout().ArtifactContentDir)

	initialBodyJSON, err := json.Marshal(map[string]any{
		"title":     "legacy backing thread",
		"topic_ref": "topic:topic-legacy-1",
	})
	if err != nil {
		t.Fatalf("marshal initial thread body: %v", err)
	}

	_, err = workspace.DB().ExecContext(
		context.Background(),
		`INSERT INTO threads(id, kind, thread_id, updated_at, updated_by, body_json, provenance_json)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		"thread-legacy-1",
		"thread",
		"thread-legacy-1",
		"2026-03-04T00:00:00Z",
		"actor-0",
		string(initialBodyJSON),
		`{"sources":["inferred"]}`,
	)
	if err != nil {
		t.Fatalf("insert legacy thread row: %v", err)
	}

	thread, err := store.GetThread(context.Background(), "thread-legacy-1")
	if err != nil {
		t.Fatalf("get thread: %v", err)
	}

	if got, _ := thread["subject_ref"].(string); got != "topic:topic-legacy-1" {
		t.Fatalf("expected canonical subject_ref, got %#v", thread)
	}
	if _, exists := thread["topic_ref"]; exists {
		t.Fatalf("expected legacy topic_ref to be hidden from thread reads, got %#v", thread)
	}
}

func TestPatchThreadOptimisticLockingIfUpdatedAt(t *testing.T) {
	t.Parallel()

	workspace, err := storage.InitializeWorkspace(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("initialize workspace: %v", err)
	}
	defer workspace.Close()

	store := primitives.NewStore(workspace.DB(), blob.NewFilesystemBackend(workspace.Layout().ArtifactContentDir), workspace.Layout().ArtifactContentDir)

	initialBodyJSON, err := json.Marshal(map[string]any{
		"title": "original",
		"tags":  []string{"alpha"},
	})
	if err != nil {
		t.Fatalf("marshal initial thread body: %v", err)
	}

	const initialUpdatedAt = "2026-03-04T00:00:00Z"
	_, err = workspace.DB().ExecContext(
		context.Background(),
		`INSERT INTO threads(id, kind, thread_id, updated_at, updated_by, body_json, provenance_json)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		"thread-opt-lock-1",
		"thread",
		"thread-lock-1",
		initialUpdatedAt,
		"actor-0",
		string(initialBodyJSON),
		`{"sources":["inferred"]}`,
	)
	if err != nil {
		t.Fatalf("insert initial thread row: %v", err)
	}

	match := initialUpdatedAt
	matchedPatch, err := store.PatchThread(
		context.Background(),
		"actor-1",
		"thread-opt-lock-1",
		map[string]any{"title": "matched update"},
		&match,
	)
	if err != nil {
		t.Fatalf("patch thread with matching if_updated_at: %v", err)
	}
	if matchedPatch.Thread["title"] != "matched update" {
		t.Fatalf("expected matched update title, got %#v", matchedPatch.Thread["title"])
	}
	assertActorStatementProvenance(t, matchedPatch.Event)

	var eventsBeforeConflict int
	if err := workspace.DB().QueryRowContext(context.Background(), `SELECT COUNT(*) FROM events`).Scan(&eventsBeforeConflict); err != nil {
		t.Fatalf("count events before conflict patch: %v", err)
	}

	stale := initialUpdatedAt
	_, err = store.PatchThread(
		context.Background(),
		"actor-2",
		"thread-opt-lock-1",
		map[string]any{"title": "stale update"},
		&stale,
	)
	if !errors.Is(err, primitives.ErrConflict) {
		t.Fatalf("expected ErrConflict for stale if_updated_at, got %v", err)
	}

	loadedAfterConflict, err := store.GetThread(context.Background(), "thread-opt-lock-1")
	if err != nil {
		t.Fatalf("get thread after conflict patch: %v", err)
	}
	if loadedAfterConflict["title"] != "matched update" {
		t.Fatalf("thread changed despite conflict: %#v", loadedAfterConflict["title"])
	}

	var eventsAfterConflict int
	if err := workspace.DB().QueryRowContext(context.Background(), `SELECT COUNT(*) FROM events`).Scan(&eventsAfterConflict); err != nil {
		t.Fatalf("count events after conflict patch: %v", err)
	}
	if eventsAfterConflict != eventsBeforeConflict {
		t.Fatalf("events changed on conflict: before=%d after=%d", eventsBeforeConflict, eventsAfterConflict)
	}

	if _, err := store.PatchThread(
		context.Background(),
		"actor-3",
		"thread-opt-lock-1",
		map[string]any{"title": "no lock update"},
		nil,
	); err != nil {
		t.Fatalf("patch thread without if_updated_at: %v", err)
	}
}

func TestCreateThreadStoresProvenanceOnlyInProvenanceJSON(t *testing.T) {
	t.Parallel()

	workspace, err := storage.InitializeWorkspace(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("initialize workspace: %v", err)
	}
	defer workspace.Close()

	store := primitives.NewStore(workspace.DB(), blob.NewFilesystemBackend(workspace.Layout().ArtifactContentDir), workspace.Layout().ArtifactContentDir)

	threadResult, err := store.CreateThread(context.Background(), "actor-1", map[string]any{
		"title":            "Thread provenance create",
		"type":             "incident",
		"status":           "active",
		"priority":         "p1",
		"tags":             []string{"ops"},
		"cadence":          "daily",
		"next_check_in_at": "2026-03-10T00:00:00Z",
		"current_summary":  "summary",
		"next_actions":     []string{"step-1"},
		"key_artifacts":    []string{},
		"provenance": map[string]any{
			"sources": []string{"actor_statement:event-create"},
			"notes":   "created by actor",
		},
	})
	if err != nil {
		t.Fatalf("create thread: %v", err)
	}

	threadID, _ := threadResult.Thread["id"].(string)
	if threadID == "" {
		t.Fatal("expected thread id")
	}

	var bodyJSON string
	var provenanceJSON string
	if err := workspace.DB().QueryRowContext(
		context.Background(),
		`SELECT body_json, provenance_json FROM threads WHERE id = ?`,
		threadID,
	).Scan(&bodyJSON, &provenanceJSON); err != nil {
		t.Fatalf("query stored thread row: %v", err)
	}

	body := map[string]any{}
	if err := json.Unmarshal([]byte(bodyJSON), &body); err != nil {
		t.Fatalf("decode body_json: %v", err)
	}
	if _, has := body["provenance"]; has {
		t.Fatalf("expected body_json not to include provenance, got %#v", body["provenance"])
	}

	provenance := map[string]any{}
	if err := json.Unmarshal([]byte(provenanceJSON), &provenance); err != nil {
		t.Fatalf("decode provenance_json: %v", err)
	}
	provenanceNotes, _ := provenance["notes"].(string)
	if provenanceNotes != "created by actor" {
		t.Fatalf("stored provenance notes mismatch: %#v", provenance["notes"])
	}
	provenanceSources := toSortedStrings(provenance["sources"])
	if !reflect.DeepEqual(provenanceSources, []string{"actor_statement:event-create"}) {
		t.Fatalf("stored provenance sources mismatch: %#v", provenance["sources"])
	}
}

func TestPatchThreadProvenanceRoundTripAndPreserveWhenOmitted(t *testing.T) {
	t.Parallel()

	workspace, err := storage.InitializeWorkspace(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("initialize workspace: %v", err)
	}
	defer workspace.Close()

	store := primitives.NewStore(workspace.DB(), blob.NewFilesystemBackend(workspace.Layout().ArtifactContentDir), workspace.Layout().ArtifactContentDir)

	threadResult, err := store.CreateThread(context.Background(), "actor-1", map[string]any{
		"title":            "Thread provenance patch",
		"type":             "incident",
		"status":           "active",
		"priority":         "p1",
		"tags":             []string{"ops"},
		"cadence":          "daily",
		"next_check_in_at": "2026-03-10T00:00:00Z",
		"current_summary":  "summary",
		"next_actions":     []string{"step-1"},
		"key_artifacts":    []string{},
		"provenance": map[string]any{
			"sources": []string{"actor_statement:event-initial"},
		},
	})
	if err != nil {
		t.Fatalf("create thread: %v", err)
	}
	threadID, _ := threadResult.Thread["id"].(string)

	updatedProvenance := map[string]any{
		"sources": []string{"actor_statement:event-updated"},
		"notes":   "updated by patch",
	}
	patchWithProvenance, err := store.PatchThread(context.Background(), "actor-2", threadID, map[string]any{
		"title":      "Thread provenance patch updated",
		"provenance": updatedProvenance,
	}, nil)
	if err != nil {
		t.Fatalf("patch thread with provenance: %v", err)
	}

	if !reflect.DeepEqual(patchWithProvenance.Thread["provenance"], updatedProvenance) {
		t.Fatalf("patch thread response provenance mismatch: got %#v want %#v", patchWithProvenance.Thread["provenance"], updatedProvenance)
	}

	var bodyJSON string
	var provenanceJSON string
	if err := workspace.DB().QueryRowContext(
		context.Background(),
		`SELECT body_json, provenance_json FROM threads WHERE id = ?`,
		threadID,
	).Scan(&bodyJSON, &provenanceJSON); err != nil {
		t.Fatalf("query thread after provenance patch: %v", err)
	}
	body := map[string]any{}
	if err := json.Unmarshal([]byte(bodyJSON), &body); err != nil {
		t.Fatalf("decode body_json after provenance patch: %v", err)
	}
	if _, has := body["provenance"]; has {
		t.Fatalf("expected body_json not to include provenance after patch, got %#v", body["provenance"])
	}

	storedProvenance := map[string]any{}
	if err := json.Unmarshal([]byte(provenanceJSON), &storedProvenance); err != nil {
		t.Fatalf("decode provenance_json after provenance patch: %v", err)
	}
	storedNotes, _ := storedProvenance["notes"].(string)
	if storedNotes != "updated by patch" {
		t.Fatalf("stored provenance notes after patch mismatch: %#v", storedProvenance["notes"])
	}
	storedSources := toSortedStrings(storedProvenance["sources"])
	if !reflect.DeepEqual(storedSources, []string{"actor_statement:event-updated"}) {
		t.Fatalf("stored provenance sources after patch mismatch: %#v", storedProvenance["sources"])
	}

	patchWithoutProvenance, err := store.PatchThread(context.Background(), "actor-3", threadID, map[string]any{
		"current_summary": "summary updated",
	}, nil)
	if err != nil {
		t.Fatalf("patch thread without provenance: %v", err)
	}
	finalProvenance, ok := patchWithoutProvenance.Thread["provenance"].(map[string]any)
	if !ok {
		t.Fatalf("expected final provenance object, got %#v", patchWithoutProvenance.Thread["provenance"])
	}
	finalNotes, _ := finalProvenance["notes"].(string)
	if finalNotes != "updated by patch" {
		t.Fatalf("provenance notes changed unexpectedly when omitted: %#v", finalProvenance["notes"])
	}
	finalSources := toSortedStrings(finalProvenance["sources"])
	if !reflect.DeepEqual(finalSources, []string{"actor_statement:event-updated"}) {
		t.Fatalf("provenance sources changed unexpectedly when omitted: %#v", finalProvenance["sources"])
	}
}

func TestListRecentEventsByThreadLimitAndOrder(t *testing.T) {
	t.Parallel()

	workspace, err := storage.InitializeWorkspace(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("initialize workspace: %v", err)
	}
	defer workspace.Close()

	store := primitives.NewStore(workspace.DB(), blob.NewFilesystemBackend(workspace.Layout().ArtifactContentDir), workspace.Layout().ArtifactContentDir)
	threadID := "thread-limit-order"

	insert := func(id string, ts string, eventType string) {
		t.Helper()
		_, err := workspace.DB().ExecContext(
			context.Background(),
			`INSERT INTO events(id, type, ts, actor_id, thread_id, refs_json, payload_json, body_json, created_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)`,
			id,
			eventType,
			ts,
			"actor-1",
			threadID,
			`["thread:`+threadID+`"]`,
			`{}`,
			`{}`,
		)
		if err != nil {
			t.Fatalf("insert event %s: %v", id, err)
		}
	}

	insert("evt-1", "2026-03-05T12:00:00Z", "context_probe_1")
	insert("evt-2", "2026-03-05T12:00:01Z", "context_probe_2")
	insert("evt-3", "2026-03-05T12:00:02Z", "context_probe_3")

	recentTwo, err := store.ListRecentEventsByThread(context.Background(), threadID, 2)
	if err != nil {
		t.Fatalf("list recent thread events with limit=2: %v", err)
	}
	if len(recentTwo) != 2 {
		t.Fatalf("expected 2 events, got %d", len(recentTwo))
	}
	if recentTwo[0]["id"] != "evt-2" || recentTwo[1]["id"] != "evt-3" {
		t.Fatalf("unexpected order/content for recent events: %#v", recentTwo)
	}

	recentZero, err := store.ListRecentEventsByThread(context.Background(), threadID, 0)
	if err != nil {
		t.Fatalf("list recent thread events with limit=0: %v", err)
	}
	if len(recentZero) != 0 {
		t.Fatalf("expected 0 events for limit=0, got %d", len(recentZero))
	}
}

func toSortedStrings(raw any) []string {
	switch values := raw.(type) {
	case []string:
		out := append([]string(nil), values...)
		sort.Strings(out)
		return out
	case []any:
		out := make([]string, 0, len(values))
		for _, value := range values {
			text, ok := value.(string)
			if !ok {
				continue
			}
			out = append(out, text)
		}
		sort.Strings(out)
		return out
	default:
		return nil
	}
}

func assertActorStatementProvenance(t *testing.T, event map[string]any) {
	t.Helper()

	eventID, _ := event["id"].(string)
	if eventID == "" {
		t.Fatalf("expected event id, got %#v", event["id"])
	}

	provenance, ok := event["provenance"].(map[string]any)
	if !ok {
		t.Fatalf("expected event provenance object, got %#v", event["provenance"])
	}

	sources := toSortedStrings(provenance["sources"])
	want := []string{"actor_statement:" + eventID}
	if !reflect.DeepEqual(sources, want) {
		t.Fatalf("unexpected actor statement provenance: got %#v want %#v", sources, want)
	}
}

func countArtifactContentFiles(t *testing.T, dir string) int {
	t.Helper()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read artifact content dir: %v", err)
	}
	return len(entries)
}
