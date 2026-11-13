package primitives

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

// Nested batch suggestions collapse to the largest; the brief tells an agent
// which suggestions concern its own asks and tells a human how long upkeep
// has waited.
func TestCurationUpkeepCollapsesNestedBatchesAndMarksOwnership(t *testing.T) {
	t.Parallel()
	s, ws, card, ask := askDeliveryFixture(t)
	ctx := context.Background()
	askRef := "event:" + anyStringValue(ask["id"])
	old := time.Now().UTC().Add(-9 * 24 * time.Hour).Format(time.RFC3339Nano)
	suggest := func(key string, targets ...string) {
		t.Helper()
		encoded, _ := json.Marshal(targets)
		payload, _ := json.Marshal(map[string]any{"payload": map[string]any{"title": "Batch related asks?", "body": "", "curation": map[string]any{"action": "batch", "targets": targets, "key": key}}})
		if _, err := ws.DB().ExecContext(ctx, `INSERT INTO events(id,type,ts,actor_id,thread_id,refs_json,payload_json) VALUES(?,?,?,?,?,'[]',?)`, key, "curation_suggested", old, "system", card["thread_id"], string(payload)); err != nil {
			t.Fatal(err)
		}
		if _, err := ws.DB().ExecContext(ctx, `INSERT INTO curation_proposals(key,proposal_event_id,action,source,status,targets_json,created_at) VALUES(?,?,'batch','formula','open',?,?)`, key, key, string(encoded), old); err != nil {
			t.Fatal(err)
		}
	}
	suggest("two", askRef, "event:b")
	suggest("three", askRef, "event:b", "event:c")
	suggest("four", askRef, "event:b", "event:c", "event:d")
	suggest("other", "event:x", "event:y")
	closed, err := s.DismissSupersededBatchSuggestions(ctx)
	if err != nil || closed != 2 {
		t.Fatalf("closed=%d err=%v, want the two covered suggestions", closed, err)
	}
	if closed, err = s.DismissSupersededBatchSuggestions(ctx); err != nil || closed != 0 {
		t.Fatalf("second pass closed=%d err=%v", closed, err)
	}
	brief, err := s.Brief(ctx, BriefReader{ActorID: "requester", Kind: "agent"}, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	yours := map[string]bool{}
	for _, suggestion := range brief["curation_suggestions"].([]map[string]any) {
		yours[anyStringValue(suggestion["key"])] = suggestion["yours"] == true
	}
	if len(yours) != 2 || !yours["four"] || yours["other"] {
		t.Fatalf("suggestions=%v", yours)
	}
	human, err := s.Brief(ctx, BriefReader{ActorID: "boss", Kind: "human"}, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	upkeep := human["upkeep"].(map[string]any)
	if upkeep["open_suggestions"] != 2 || upkeep["oldest_waiting_seconds"].(int64) < 8*24*3600 || upkeep["untended"] != true {
		t.Fatalf("upkeep=%v", upkeep)
	}
}
