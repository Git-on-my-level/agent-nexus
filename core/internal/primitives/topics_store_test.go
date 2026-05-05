package primitives

import (
	"context"
	"encoding/json"
	"testing"

	"agent-nexus-core/internal/storage"
)

func TestTopicPatchAndLifecycleDoNotPersistPublicIdentityInExtensions(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ws, err := storage.InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatalf("initialize workspace: %v", err)
	}
	defer ws.Close()
	store := NewTestStore(ws.DB(), "")

	created, err := store.CreateTopic(ctx, "actor-1", map[string]any{
		"title":   "Public Identity Topic",
		"summary": "initial",
	})
	if err != nil {
		t.Fatalf("create topic: %v", err)
	}
	topicID := created.Topic["id"].(string)
	if created.Topic["handle"] == "" || created.Topic["ref"] == "" {
		t.Fatalf("create response missing public identity: %#v", created.Topic)
	}

	patched, err := store.PatchTopic(ctx, "actor-2", topicID, map[string]any{
		"summary": "patched",
	}, nil)
	if err != nil {
		t.Fatalf("patch topic: %v", err)
	}
	if patched.Topic["handle"] != created.Topic["handle"] || patched.Topic["ref"] != created.Topic["ref"] {
		t.Fatalf("patch response changed public identity: got %#v want handle=%#v ref=%#v", patched.Topic, created.Topic["handle"], created.Topic["ref"])
	}
	assertTopicExtensionsOmitPublicIdentity(t, ws, topicID)

	archived, err := store.ArchiveTopic(ctx, "actor-3", topicID)
	if err != nil {
		t.Fatalf("archive topic: %v", err)
	}
	if archived["handle"] != created.Topic["handle"] || archived["ref"] != created.Topic["ref"] {
		t.Fatalf("archive response changed public identity: got %#v want handle=%#v ref=%#v", archived, created.Topic["handle"], created.Topic["ref"])
	}
	assertTopicExtensionsOmitPublicIdentity(t, ws, topicID)
}

func assertTopicExtensionsOmitPublicIdentity(t *testing.T, ws *storage.Workspace, topicID string) {
	t.Helper()
	var raw string
	if err := ws.DB().QueryRowContext(context.Background(), `SELECT extensions_json FROM topics WHERE id = ?`, topicID).Scan(&raw); err != nil {
		t.Fatalf("load topic extensions: %v", err)
	}
	var ext map[string]any
	if err := json.Unmarshal([]byte(raw), &ext); err != nil {
		t.Fatalf("decode topic extensions %q: %v", raw, err)
	}
	if _, ok := ext["handle"]; ok {
		t.Fatalf("topic extensions persisted handle: %#v", ext)
	}
	if _, ok := ext["ref"]; ok {
		t.Fatalf("topic extensions persisted ref: %#v", ext)
	}
}
