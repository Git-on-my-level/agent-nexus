package primitives

import (
	"context"
	"reflect"
	"testing"
	"time"
)

func TestAskPresentationStorageAndBriefRoundTrip(t *testing.T) {
	if testing.Short() {
		t.Skip("full storage stack")
	}
	s, _, card, _ := askDeliveryFixture(t)
	ctx := context.Background()
	fields := map[string]any{"situation": "Deleted data can reappear.", "impact": "This breaks our promise.", "recommendation": map[string]any{"action": "Check before writing.", "why": "Reuse the guard."}, "alternatives": []any{map[string]any{"action": "Wait.", "consequence": "Work stays blocked."}}, "if_no_answer": "Blocked until Friday.", "details": "Technical explanation", "evidence": []any{map[string]any{"label": "Change", "url": "https://example.org/change"}}}
	payload := map[string]any{"kind": "ask", "title": "Approve the guard?", "requester_actor_id": "requester", "subject_ref": card["ref"], "response_proposals": []any{"Approve the guard."}}
	for key, v := range fields {
		payload[key] = v
	}
	evt, err := s.AppendEvent(ctx, "requester", map[string]any{"type": "human_attention_requested", "thread_id": card["thread_id"], "refs": []string{"thread:" + anyStringValue(card["thread_id"]), anyStringValue(card["ref"])}, "payload": payload})
	if err != nil {
		t.Fatal(err)
	}
	stored, err := s.GetEvent(ctx, anyStringValue(evt["id"]))
	if err != nil {
		t.Fatal(err)
	}
	for key, v := range fields {
		if !reflect.DeepEqual(asMapValue(stored["payload"])[key], v) {
			t.Fatalf("stored %s lost", key)
		}
	}
	brief, err := s.Brief(WithAccessScope(ctx, AccessScope{ActorID: "human"}), BriefReader{ActorID: "human", Kind: "human"}, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, item := range brief["decisions"].([]map[string]any) {
		if item["event_ref"] == "event:"+anyStringValue(evt["id"]) {
			found = true
			for key, v := range fields {
				if !reflect.DeepEqual(item[key], v) {
					t.Fatalf("brief %s lost: %#v", key, item)
				}
			}
			member := briefBatchMember(item, briefDecisionMeta{})
			for key, v := range fields {
				if !reflect.DeepEqual(member[key], v) {
					t.Fatalf("batch member %s lost", key)
				}
			}
		}
	}
	if !found {
		t.Fatalf("not in brief: %#v", brief["decisions"])
	}
}
