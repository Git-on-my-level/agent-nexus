package primitives_test

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sort"
	"strings"
	"testing"

	"organization-autorunner-core/internal/primitives"
	"organization-autorunner-core/internal/storage"
)

func TestStoreAppendAndGetEventUnknownTypeAccepted(t *testing.T) {
	t.Parallel()

	workspace, err := storage.InitializeWorkspace(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("initialize workspace: %v", err)
	}
	defer workspace.Close()

	store := primitives.NewStore(workspace.DB(), workspace.Layout().ArtifactContentDir)

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

func TestCreateArtifactAcceptsSafeIDAndRejectsUnsafeIDs(t *testing.T) {
	t.Parallel()

	workspace, err := storage.InitializeWorkspace(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("initialize workspace: %v", err)
	}
	defer workspace.Close()

	store := primitives.NewStore(workspace.DB(), workspace.Layout().ArtifactContentDir)

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

func TestPatchSnapshotPreservesUnknownFieldsAndEmitsChangedFields(t *testing.T) {
	t.Parallel()

	workspace, err := storage.InitializeWorkspace(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("initialize workspace: %v", err)
	}
	defer workspace.Close()

	store := primitives.NewStore(workspace.DB(), workspace.Layout().ArtifactContentDir)

	initialBody := map[string]any{
		"title":         "original title",
		"tags":          []string{"alpha", "beta"},
		"unknown_field": map[string]any{"foo": "bar"},
	}
	initialBodyJSON, err := json.Marshal(initialBody)
	if err != nil {
		t.Fatalf("marshal initial snapshot body: %v", err)
	}

	_, err = workspace.DB().ExecContext(
		context.Background(),
		`INSERT INTO snapshots(id, kind, thread_id, updated_at, updated_by, body_json, provenance_json)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		"snapshot-1",
		"thread",
		"thread-1",
		"2026-03-04T00:00:00Z",
		"actor-0",
		string(initialBodyJSON),
		`{"sources":["inferred"]}`,
	)
	if err != nil {
		t.Fatalf("insert initial snapshot: %v", err)
	}

	patchResult, err := store.PatchSnapshot(context.Background(), "actor-1", "snapshot-1", map[string]any{
		"title": "updated title",
		"tags":  []any{"gamma"},
	}, nil)
	if err != nil {
		t.Fatalf("patch snapshot: %v", err)
	}

	if patchResult.Snapshot["title"] != "updated title" {
		t.Fatalf("title not patched: %#v", patchResult.Snapshot["title"])
	}

	unknown, ok := patchResult.Snapshot["unknown_field"].(map[string]any)
	if !ok || unknown["foo"] != "bar" {
		t.Fatalf("unknown field not preserved: %#v", patchResult.Snapshot["unknown_field"])
	}

	tags, ok := patchResult.Snapshot["tags"].([]any)
	if !ok || len(tags) != 1 || tags[0] != "gamma" {
		t.Fatalf("tags were not replaced wholesale: %#v", patchResult.Snapshot["tags"])
	}

	if patchResult.Event["type"] != "snapshot_updated" {
		t.Fatalf("unexpected event type: %#v", patchResult.Event["type"])
	}
	assertActorStatementProvenance(t, patchResult.Event)

	eventRefs, ok := patchResult.Event["refs"].([]string)
	if !ok || len(eventRefs) != 1 || eventRefs[0] != "snapshot:snapshot-1" {
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
		"snapshot_updated",
		"thread-1",
	).Scan(&eventCount); err != nil {
		t.Fatalf("count snapshot_updated events: %v", err)
	}
	if eventCount != 1 {
		t.Fatalf("expected exactly one snapshot_updated event, got %d", eventCount)
	}

	secondPatch, err := store.PatchSnapshot(context.Background(), "actor-2", "snapshot-1", map[string]any{
		"title": "final title",
	}, nil)
	if err != nil {
		t.Fatalf("patch snapshot second time: %v", err)
	}

	secondTags, ok := secondPatch.Snapshot["tags"].([]any)
	if !ok || len(secondTags) != 1 || secondTags[0] != "gamma" {
		t.Fatalf("tags should remain unchanged when absent from patch: %#v", secondPatch.Snapshot["tags"])
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

func TestPatchSnapshotOptimisticLockingIfUpdatedAt(t *testing.T) {
	t.Parallel()

	workspace, err := storage.InitializeWorkspace(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("initialize workspace: %v", err)
	}
	defer workspace.Close()

	store := primitives.NewStore(workspace.DB(), workspace.Layout().ArtifactContentDir)

	initialBodyJSON, err := json.Marshal(map[string]any{
		"title": "original",
		"tags":  []string{"alpha"},
	})
	if err != nil {
		t.Fatalf("marshal initial snapshot body: %v", err)
	}

	const initialUpdatedAt = "2026-03-04T00:00:00Z"
	_, err = workspace.DB().ExecContext(
		context.Background(),
		`INSERT INTO snapshots(id, kind, thread_id, updated_at, updated_by, body_json, provenance_json)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		"snapshot-lock-1",
		"thread",
		"thread-lock-1",
		initialUpdatedAt,
		"actor-0",
		string(initialBodyJSON),
		`{"sources":["inferred"]}`,
	)
	if err != nil {
		t.Fatalf("insert initial snapshot: %v", err)
	}

	match := initialUpdatedAt
	matchedPatch, err := store.PatchSnapshot(
		context.Background(),
		"actor-1",
		"snapshot-lock-1",
		map[string]any{"title": "matched update"},
		&match,
	)
	if err != nil {
		t.Fatalf("patch snapshot with matching if_updated_at: %v", err)
	}
	if matchedPatch.Snapshot["title"] != "matched update" {
		t.Fatalf("expected matched update title, got %#v", matchedPatch.Snapshot["title"])
	}
	assertActorStatementProvenance(t, matchedPatch.Event)

	var eventsBeforeConflict int
	if err := workspace.DB().QueryRowContext(context.Background(), `SELECT COUNT(*) FROM events`).Scan(&eventsBeforeConflict); err != nil {
		t.Fatalf("count events before conflict patch: %v", err)
	}

	stale := initialUpdatedAt
	_, err = store.PatchSnapshot(
		context.Background(),
		"actor-2",
		"snapshot-lock-1",
		map[string]any{"title": "stale update"},
		&stale,
	)
	if !errors.Is(err, primitives.ErrConflict) {
		t.Fatalf("expected ErrConflict for stale if_updated_at, got %v", err)
	}

	loadedAfterConflict, err := store.GetSnapshot(context.Background(), "snapshot-lock-1")
	if err != nil {
		t.Fatalf("get snapshot after conflict patch: %v", err)
	}
	if loadedAfterConflict["title"] != "matched update" {
		t.Fatalf("snapshot changed despite conflict: %#v", loadedAfterConflict["title"])
	}

	var eventsAfterConflict int
	if err := workspace.DB().QueryRowContext(context.Background(), `SELECT COUNT(*) FROM events`).Scan(&eventsAfterConflict); err != nil {
		t.Fatalf("count events after conflict patch: %v", err)
	}
	if eventsAfterConflict != eventsBeforeConflict {
		t.Fatalf("events changed on conflict: before=%d after=%d", eventsBeforeConflict, eventsAfterConflict)
	}

	if _, err := store.PatchSnapshot(
		context.Background(),
		"actor-3",
		"snapshot-lock-1",
		map[string]any{"title": "no lock update"},
		nil,
	); err != nil {
		t.Fatalf("patch snapshot without if_updated_at: %v", err)
	}
}

func TestCreateThreadStoresProvenanceOnlyInProvenanceJSON(t *testing.T) {
	t.Parallel()

	workspace, err := storage.InitializeWorkspace(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("initialize workspace: %v", err)
	}
	defer workspace.Close()

	store := primitives.NewStore(workspace.DB(), workspace.Layout().ArtifactContentDir)

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

	threadID, _ := threadResult.Snapshot["id"].(string)
	if threadID == "" {
		t.Fatal("expected thread id")
	}

	var bodyJSON string
	var provenanceJSON string
	if err := workspace.DB().QueryRowContext(
		context.Background(),
		`SELECT body_json, provenance_json FROM snapshots WHERE id = ?`,
		threadID,
	).Scan(&bodyJSON, &provenanceJSON); err != nil {
		t.Fatalf("query stored thread snapshot row: %v", err)
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

	store := primitives.NewStore(workspace.DB(), workspace.Layout().ArtifactContentDir)

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
	threadID, _ := threadResult.Snapshot["id"].(string)

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

	if !reflect.DeepEqual(patchWithProvenance.Snapshot["provenance"], updatedProvenance) {
		t.Fatalf("patch thread response provenance mismatch: got %#v want %#v", patchWithProvenance.Snapshot["provenance"], updatedProvenance)
	}

	var bodyJSON string
	var provenanceJSON string
	if err := workspace.DB().QueryRowContext(
		context.Background(),
		`SELECT body_json, provenance_json FROM snapshots WHERE id = ?`,
		threadID,
	).Scan(&bodyJSON, &provenanceJSON); err != nil {
		t.Fatalf("query snapshot after provenance patch: %v", err)
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
	finalProvenance, ok := patchWithoutProvenance.Snapshot["provenance"].(map[string]any)
	if !ok {
		t.Fatalf("expected final provenance object, got %#v", patchWithoutProvenance.Snapshot["provenance"])
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

func TestCommitmentOpenCommitmentsMaintenance(t *testing.T) {
	t.Parallel()

	workspace, err := storage.InitializeWorkspace(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("initialize workspace: %v", err)
	}
	defer workspace.Close()

	store := primitives.NewStore(workspace.DB(), workspace.Layout().ArtifactContentDir)

	threadResult, err := store.CreateThread(context.Background(), "actor-1", map[string]any{
		"title":           "Thread A",
		"type":            "incident",
		"status":          "active",
		"priority":        "p1",
		"tags":            []string{},
		"cadence":         "reactive",
		"current_summary": "summary",
		"next_actions":    []string{},
		"key_artifacts":   []string{},
		"provenance":      map[string]any{"sources": []string{"inferred"}},
	})
	if err != nil {
		t.Fatalf("create thread: %v", err)
	}
	threadID, _ := threadResult.Snapshot["id"].(string)
	if threadID == "" {
		t.Fatal("expected thread id")
	}
	assertActorStatementProvenance(t, threadResult.Event)

	firstCommitment, err := store.CreateCommitment(context.Background(), "actor-1", map[string]any{
		"thread_id":          threadID,
		"title":              "Commitment 1",
		"owner":              "actor-1",
		"due_at":             "2026-03-10T00:00:00Z",
		"status":             "open",
		"definition_of_done": []string{"done condition"},
		"links":              []string{"url:https://example.com/1"},
		"provenance":         map[string]any{"sources": []string{"inferred"}},
	})
	if err != nil {
		t.Fatalf("create first commitment: %v", err)
	}
	firstCommitmentID, _ := firstCommitment.Snapshot["id"].(string)
	assertActorStatementProvenance(t, firstCommitment.Event)

	secondCommitment, err := store.CreateCommitment(context.Background(), "actor-1", map[string]any{
		"thread_id":          threadID,
		"title":              "Commitment 2",
		"owner":              "actor-1",
		"due_at":             "2026-03-11T00:00:00Z",
		"status":             "blocked",
		"definition_of_done": []string{"done condition"},
		"links":              []string{"url:https://example.com/2"},
		"provenance":         map[string]any{"sources": []string{"inferred"}},
	})
	if err != nil {
		t.Fatalf("create second commitment: %v", err)
	}
	secondCommitmentID, _ := secondCommitment.Snapshot["id"].(string)
	assertActorStatementProvenance(t, secondCommitment.Event)

	threadAfterCreate, err := store.GetThread(context.Background(), threadID)
	if err != nil {
		t.Fatalf("get thread after commitments create: %v", err)
	}
	openAfterCreate := toSortedStrings(threadAfterCreate["open_commitments"])
	expectedOpenAfterCreate := toSortedStrings([]string{firstCommitmentID, secondCommitmentID})
	if !reflect.DeepEqual(openAfterCreate, expectedOpenAfterCreate) {
		t.Fatalf("unexpected open commitments after create: %#v", threadAfterCreate["open_commitments"])
	}

	patchDone, err := store.PatchCommitment(
		context.Background(),
		"actor-1",
		firstCommitmentID,
		map[string]any{"status": "done"},
		[]string{"artifact:receipt-1"},
		nil,
	)
	if err != nil {
		t.Fatalf("patch commitment to done: %v", err)
	}
	provenance, ok := patchDone.Snapshot["provenance"].(map[string]any)
	if !ok {
		t.Fatalf("expected commitment provenance object, got %#v", patchDone.Snapshot["provenance"])
	}
	byField, ok := provenance["by_field"].(map[string]any)
	if !ok {
		t.Fatalf("expected provenance.by_field object, got %#v", provenance["by_field"])
	}
	statusSources := toSortedStrings(byField["status"])
	if !reflect.DeepEqual(statusSources, []string{"receipt:receipt-1"}) {
		t.Fatalf("unexpected provenance.by_field.status: %#v", byField["status"])
	}
	assertActorStatementProvenance(t, patchDone.Event)

	threadAfterDone, err := store.GetThread(context.Background(), threadID)
	if err != nil {
		t.Fatalf("get thread after done patch: %v", err)
	}
	openAfterDone := toSortedStrings(threadAfterDone["open_commitments"])
	if !reflect.DeepEqual(openAfterDone, []string{secondCommitmentID}) {
		t.Fatalf("unexpected open commitments after done patch: %#v", threadAfterDone["open_commitments"])
	}

	if _, err := store.PatchCommitment(
		context.Background(),
		"actor-1",
		secondCommitmentID,
		map[string]any{"status": "canceled"},
		[]string{"event:decision-1"},
		nil,
	); err != nil {
		t.Fatalf("patch commitment to canceled: %v", err)
	}

	threadAfterCanceled, err := store.GetThread(context.Background(), threadID)
	if err != nil {
		t.Fatalf("get thread after canceled patch: %v", err)
	}
	openAfterCanceled := toSortedStrings(threadAfterCanceled["open_commitments"])
	if len(openAfterCanceled) != 0 {
		t.Fatalf("expected no open commitments after canceled patch, got %#v", threadAfterCanceled["open_commitments"])
	}
}

func TestPatchCommitmentOptimisticLockingIfUpdatedAt(t *testing.T) {
	t.Parallel()

	workspace, err := storage.InitializeWorkspace(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("initialize workspace: %v", err)
	}
	defer workspace.Close()

	store := primitives.NewStore(workspace.DB(), workspace.Layout().ArtifactContentDir)

	threadResult, err := store.CreateThread(context.Background(), "actor-1", map[string]any{
		"title":           "Thread for lock test",
		"type":            "incident",
		"status":          "active",
		"priority":        "p1",
		"tags":            []string{},
		"cadence":         "reactive",
		"current_summary": "summary",
		"next_actions":    []string{},
		"key_artifacts":   []string{},
		"provenance":      map[string]any{"sources": []string{"inferred"}},
	})
	if err != nil {
		t.Fatalf("create thread: %v", err)
	}
	threadID, _ := threadResult.Snapshot["id"].(string)

	commitmentResult, err := store.CreateCommitment(context.Background(), "actor-1", map[string]any{
		"thread_id":          threadID,
		"title":              "Original commitment",
		"owner":              "actor-1",
		"due_at":             "2026-03-10T00:00:00Z",
		"status":             "open",
		"definition_of_done": []string{"done condition"},
		"links":              []string{"url:https://example.com/1"},
		"provenance":         map[string]any{"sources": []string{"inferred"}},
	})
	if err != nil {
		t.Fatalf("create commitment: %v", err)
	}
	commitmentID, _ := commitmentResult.Snapshot["id"].(string)
	initialUpdatedAt, _ := commitmentResult.Snapshot["updated_at"].(string)

	match := initialUpdatedAt
	patchMatched, err := store.PatchCommitment(
		context.Background(),
		"actor-2",
		commitmentID,
		map[string]any{"title": "Matched commitment update"},
		nil,
		&match,
	)
	if err != nil {
		t.Fatalf("patch commitment with matching if_updated_at: %v", err)
	}
	if patchMatched.Snapshot["title"] != "Matched commitment update" {
		t.Fatalf("unexpected commitment title after matched update: %#v", patchMatched.Snapshot["title"])
	}
	assertActorStatementProvenance(t, patchMatched.Event)

	var eventsBeforeConflict int
	if err := workspace.DB().QueryRowContext(context.Background(), `SELECT COUNT(*) FROM events`).Scan(&eventsBeforeConflict); err != nil {
		t.Fatalf("count events before commitment conflict: %v", err)
	}

	stale := initialUpdatedAt
	_, err = store.PatchCommitment(
		context.Background(),
		"actor-3",
		commitmentID,
		map[string]any{"title": "Stale commitment update"},
		nil,
		&stale,
	)
	if !errors.Is(err, primitives.ErrConflict) {
		t.Fatalf("expected ErrConflict for stale commitment if_updated_at, got %v", err)
	}

	loadedAfterConflict, err := store.GetCommitment(context.Background(), commitmentID)
	if err != nil {
		t.Fatalf("get commitment after conflict patch: %v", err)
	}
	if loadedAfterConflict["title"] != "Matched commitment update" {
		t.Fatalf("commitment changed despite conflict: %#v", loadedAfterConflict["title"])
	}

	var eventsAfterConflict int
	if err := workspace.DB().QueryRowContext(context.Background(), `SELECT COUNT(*) FROM events`).Scan(&eventsAfterConflict); err != nil {
		t.Fatalf("count events after commitment conflict: %v", err)
	}
	if eventsAfterConflict != eventsBeforeConflict {
		t.Fatalf("events changed on commitment conflict: before=%d after=%d", eventsBeforeConflict, eventsAfterConflict)
	}

	if _, err := store.PatchCommitment(
		context.Background(),
		"actor-4",
		commitmentID,
		map[string]any{"title": "No-lock commitment update"},
		nil,
		nil,
	); err != nil {
		t.Fatalf("patch commitment without if_updated_at: %v", err)
	}
}

func TestPatchCommitmentRestrictedTransitionRequiresEvidence(t *testing.T) {
	t.Parallel()

	workspace, err := storage.InitializeWorkspace(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("initialize workspace: %v", err)
	}
	defer workspace.Close()

	store := primitives.NewStore(workspace.DB(), workspace.Layout().ArtifactContentDir)

	threadResult, err := store.CreateThread(context.Background(), "actor-1", map[string]any{
		"title":           "Thread A",
		"type":            "incident",
		"status":          "active",
		"priority":        "p1",
		"tags":            []string{},
		"cadence":         "reactive",
		"current_summary": "summary",
		"next_actions":    []string{},
		"key_artifacts":   []string{},
		"provenance":      map[string]any{"sources": []string{"inferred"}},
	})
	if err != nil {
		t.Fatalf("create thread: %v", err)
	}
	threadID, _ := threadResult.Snapshot["id"].(string)

	commitmentResult, err := store.CreateCommitment(context.Background(), "actor-1", map[string]any{
		"thread_id":          threadID,
		"title":              "Commitment 1",
		"owner":              "actor-1",
		"due_at":             "2026-03-10T00:00:00Z",
		"status":             "open",
		"definition_of_done": []string{"done condition"},
		"links":              []string{"url:https://example.com/1"},
		"provenance":         map[string]any{"sources": []string{"inferred"}},
	})
	if err != nil {
		t.Fatalf("create commitment: %v", err)
	}
	commitmentID, _ := commitmentResult.Snapshot["id"].(string)

	_, err = store.PatchCommitment(context.Background(), "actor-1", commitmentID, map[string]any{
		"status": "done",
	}, nil, nil)
	if !errors.Is(err, primitives.ErrInvalidCommitmentTransition) {
		t.Fatalf("expected ErrInvalidCommitmentTransition, got %v", err)
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
