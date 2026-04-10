package server

import (
	"context"
	"testing"
	"time"
)

// integrationMarkThreadProjectionAfterMutation mirrors the projection side-effects that
// successful primitive writes trigger in production (dirty queue + maintainer step).
func integrationMarkThreadProjectionAfterMutation(t *testing.T, store PrimitiveStore, maintainer *ProjectionMaintainer, threadID string) {
	t.Helper()
	if store == nil || threadID == "" {
		return
	}
	_ = store.MarkTopicProjectionsDirty(context.Background(), []string{threadID}, time.Now().UTC())
	if maintainer != nil {
		if err := maintainer.Step(context.Background(), time.Now().UTC()); err != nil {
			t.Fatalf("projection maintainer step: %v", err)
		}
		maintainer.Notify()
	}
}

// integrationSeedThread creates a backing thread via PrimitiveStore (not HTTP) and updates projections.
func integrationSeedThread(t *testing.T, h primitivesTestHarness, actorID string, thread map[string]any) string {
	t.Helper()
	return integrationSeedThreadWithStore(t, h.primitiveStore, h.maintainer, actorID, thread)
}

func integrationSeedThreadWithStore(t *testing.T, store PrimitiveStore, maintainer *ProjectionMaintainer, actorID string, thread map[string]any) string {
	t.Helper()
	if store == nil {
		t.Fatal("integrationSeedThreadWithStore: nil primitive store")
	}
	res, err := store.CreateThread(context.Background(), actorID, thread)
	if err != nil {
		t.Fatalf("CreateThread: %v", err)
	}
	threadID := firstNonEmptyString(anyString(res.Thread["thread_id"]), anyString(res.Thread["id"]))
	if threadID == "" {
		t.Fatalf("CreateThread returned thread without id: %#v", res.Thread)
	}
	integrationMarkThreadProjectionAfterMutation(t, store, maintainer, threadID)
	return threadID
}

// integrationPatchThread applies a thread patch via PrimitiveStore (not HTTP) and updates projections.
func integrationPatchThread(t *testing.T, h primitivesTestHarness, actorID, threadID string, patch map[string]any, ifUpdatedAt *string) map[string]any {
	t.Helper()
	return integrationPatchThreadWithStore(t, h.primitiveStore, h.maintainer, actorID, threadID, patch, ifUpdatedAt)
}

func integrationPatchThreadWithStore(t *testing.T, store PrimitiveStore, maintainer *ProjectionMaintainer, actorID, threadID string, patch map[string]any, ifUpdatedAt *string) map[string]any {
	t.Helper()
	if store == nil {
		t.Fatal("integrationPatchThreadWithStore: nil primitive store")
	}
	res, err := store.PatchThread(context.Background(), actorID, threadID, patch, ifUpdatedAt)
	if err != nil {
		t.Fatalf("PatchThread: %v", err)
	}
	integrationMarkThreadProjectionAfterMutation(t, store, maintainer, threadID)
	return res.Thread
}

// paginationTestThread matches the historical integration thread shape used by list/cursor tests.
func paginationTestThread(id, title string) map[string]any {
	return map[string]any{
		"id":               id,
		"title":            title,
		"type":             "incident",
		"status":           "active",
		"priority":         "p1",
		"tags":             []any{},
		"cadence":          "daily",
		"next_check_in_at": "2020-01-01T00:00:00Z",
		"current_summary":  "Summary",
		"next_actions":     []any{},
		"key_artifacts":    []any{},
		"provenance":       map[string]any{"sources": []any{"inferred"}},
	}
}
