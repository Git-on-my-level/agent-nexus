package primitives_test

import (
	"context"
	"encoding/json"
	"reflect"
	"sort"
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
	})
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
	})
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
	secondChanged, ok := secondPayload["changed_fields"].([]string)
	if !ok || len(secondChanged) != 1 || secondChanged[0] != "title" {
		t.Fatalf("unexpected second changed_fields: %#v", secondPayload["changed_fields"])
	}
}
