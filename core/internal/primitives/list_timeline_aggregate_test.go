package primitives_test

import (
	"context"
	"encoding/json"
	"testing"

	"agent-nexus-core/internal/blob"
	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/storage"
)

// Regression: timeline_message_count aggregates must treat thread_id equality on trim.
func TestListTopicsEmbedsTimelineMessageCountWithWhitespaceThreadMismatch(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	workspace, err := storage.InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatalf("initialize workspace: %v", err)
	}
	defer workspace.Close()

	store := primitives.NewStore(workspace.DB(), blob.NewFilesystemBackend(workspace.Layout().ArtifactContentDir), workspace.Layout().ArtifactContentDir)

	canonicalThread := "thread-ws-msg"
	createRes, err := store.CreateTopic(ctx, "actor-1", map[string]any{
		"id":            "topic-ws-msg",
		"thread_id":     canonicalThread,
		"title":         "WS trim aggregation",
		"summary":       "s",
		"owner_refs":    []string{},
		"document_refs": []string{},
		"board_refs":    []string{},
		"related_refs":  []string{},
		"provenance":    map[string]any{"sources": []string{"test"}},
	})
	if err != nil {
		t.Fatalf("create topic: %v", err)
	}

	topicID, _ := createRes.Topic["id"].(string)
	if topicID == "" {
		t.Fatalf("missing topic id: %#v", createRes.Topic)
	}

	prep := func(threadCol string, eventSuffix string) {
		evID := "evt-ws-msg-" + eventSuffix
		body := map[string]any{
			"id":         evID,
			"type":       "message_posted",
			"ts":         "2026-05-03T12:00:00.000Z",
			"actor_id":   "actor-1",
			"thread_id":  canonicalThread,
			"refs":       []string{"thread:" + canonicalThread},
			"payload":    map[string]any{"text": evID},
			"provenance": map[string]any{"sources": []string{"inferred"}},
		}
		wrapper := primitives.EventPayloadWrapperFromBodyMap(body)
		payloadCol, err := json.Marshal(wrapper)
		if err != nil {
			t.Fatalf("marshal event payload column: %v", err)
		}
		refsJSON, err := json.Marshal(body["refs"])
		if err != nil {
			t.Fatalf("marshal event refs: %v", err)
		}
		if _, err := workspace.DB().ExecContext(ctx,
			`INSERT INTO events(id, type, ts, actor_id, thread_id, refs_json, payload_json, created_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)`,
			evID,
			"message_posted",
			body["ts"],
			"actor-1",
			threadCol,
			string(refsJSON),
			string(payloadCol),
		); err != nil {
			t.Fatalf("insert event %s: %v", evID, err)
		}
	}

	// Two rows whose persisted thread_id differs raw but trims to the topic's backing id.
	prep(canonicalThread+"    ", "a")
	prep("   "+canonicalThread, "b")

	topics, _, err := store.ListTopics(ctx, primitives.TopicListFilter{States: []string{"active"}})
	if err != nil {
		t.Fatalf("list topics: %v", err)
	}
	var got map[string]any
	for _, tp := range topics {
		if anyString(tp["id"]) == topicID {
			got = tp
			break
		}
	}
	if got == nil {
		t.Fatalf("topic %s not returned from list", topicID)
	}
	cnt, ok := got["timeline_message_count"].(int)
	if !ok {
		t.Fatalf("timeline_message_count type got %T value %#v", got["timeline_message_count"], got["timeline_message_count"])
	}
	if cnt != 2 {
		row := workspace.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM events WHERE type='message_posted' AND trim(COALESCE(thread_id,'')) = ?`, canonicalThread)
		var raw int
		_ = row.Scan(&raw)
		t.Fatalf("timeline_message_count=%d want=2 timeline_raw_sql_count=%d", cnt, raw)
	}
}

func anyString(v any) string {
	s, _ := v.(string)
	return s
}
