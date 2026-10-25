package server

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"agent-nexus-core/internal/askpresentation"
)

func TestAskPresentationInboxRoundTrip(t *testing.T) {
	const source = `{"id":"ask-1","ts":"2026-10-25T00:00:00Z","thread_id":"thread-1","refs":["thread:thread-1","card:launch"],"payload":{"kind":"ask","title":"Approve the guard?","subject_ref":"card:launch","requester_actor_id":"agent","response_proposals":["Approve the guard."],"body":"Legacy context.","situation":"Deleted data can reappear.","impact":"This breaks our promise.","recommendation":{"action":"Check before writing.","why":"Reuse the existing guard."},"alternatives":[{"action":"Wait.","consequence":"The change stays blocked."}],"if_no_answer":"Blocked until Friday.","details":"external_write_fence implementation","evidence":[{"label":"Change","url":"https://example.org/change"}]}}`
	var event map[string]any
	if err := json.Unmarshal([]byte(source), &event); err != nil {
		t.Fatal(err)
	}
	payload := event["payload"].(map[string]any)
	if err := askpresentation.Validate(payload); err != nil {
		t.Fatal(err)
	}
	item, ok := deriveHumanAttentionInboxItem(event)
	if !ok {
		t.Fatal("no inbox item")
	}
	for _, key := range []string{"body", "situation", "impact", "recommendation", "alternatives", "if_no_answer", "details", "evidence"} {
		if !reflect.DeepEqual(item.Data[key], payload[key]) {
			t.Fatalf("%s lost: %#v", key, item.Data)
		}
	}
	// Answered history keeps exactly the authored presentation too.
	response := map[string]any{"id": "answer-1", "ts": "2026-10-25T01:00:00Z", "payload": map[string]any{"inbox_item_id": item.ID, "response_text": "Approve the guard.", "outcome": "answered"}}
	completed := completedInboxItemPayload(context.Background(), nil, response, event)
	for _, key := range []string{"situation", "recommendation", "alternatives", "details", "evidence"} {
		if !reflect.DeepEqual(completed[key], payload[key]) {
			t.Fatalf("completed %s lost: %#v", key, completed)
		}
	}
	// Filed batches retain copy on every member and on the compatible lead item.
	payload["batch_id"] = "batch-1"
	other := map[string]any{"id": "ask-2", "ts": "2026-10-25T00:01:00Z", "thread_id": "thread-1", "refs": event["refs"], "payload": payload}
	batch, ok := deriveHumanAttentionBatchInboxItem([]map[string]any{event, other}, nil)
	if !ok {
		t.Fatal("no batch item")
	}
	for _, key := range []string{"situation", "impact", "recommendation", "alternatives", "if_no_answer", "details", "evidence"} {
		if !reflect.DeepEqual(batch.Data[key], payload[key]) {
			t.Fatalf("batch lead %s lost", key)
		}
		for _, member := range batch.Data["batch"].(map[string]any)["asks"].([]map[string]any) {
			if !reflect.DeepEqual(member[key], payload[key]) {
				t.Fatalf("batch member %s lost", key)
			}
		}
	}
	// Historical payloads stay valid and renderable without structured fields.
	for _, key := range []string{"situation", "impact", "recommendation", "alternatives", "if_no_answer", "details", "evidence"} {
		delete(payload, key)
	}
	if err := askpresentation.Validate(payload); err != nil {
		t.Fatal(err)
	}
	legacy, ok := deriveHumanAttentionInboxItem(event)
	if !ok || legacy.Data["body"] != "Legacy context." {
		t.Fatalf("legacy: %#v", legacy)
	}
}
