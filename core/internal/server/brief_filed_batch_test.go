package server

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"agent-nexus-core/internal/primitives"
)

func TestBriefV1CollapsesFiledAskBatch(t *testing.T) {
	requireIntegrationTest(t)
	env := newAuthIntegrationEnv(t, authIntegrationOptions{bootstrapToken: testBootstrapToken})
	agent := seedNotificationTestAgent(t, env, "briefbatch.agent")
	threadID := integrationSeedThreadWithStore(t, env.primitiveStore, nil, agent.ActorID, map[string]any{
		"title": "Brief batch thread", "type": "incident", "status": "active", "priority": "p2",
		"tags": []any{"asks"}, "cadence": "daily", "next_check_in_at": "2026-03-06T00:00:00Z",
		"current_summary": "summary", "next_actions": []any{"check"}, "key_artifacts": []any{},
		"provenance": map[string]any{"sources": []any{"inferred"}},
	})
	ask := func(title string) map[string]any {
		return map[string]any{
			"type":      "human_attention_requested",
			"thread_id": threadID,
			"summary":   title,
			"refs":      []string{"thread:" + threadID},
			"payload": map[string]any{
				"kind": "ask", "title": title,
				"subject_ref":        "thread:" + threadID,
				"requester_actor_id": agent.ActorID,
				"requester_label":    agent.Username,
				"response_proposals": []any{"Accept", "Wait"},
				"delivery":           "batch",
			},
			"provenance": map[string]any{"sources": []string{"event:cli.ask"}},
		}
	}
	asks := []map[string]any{}
	for i := 1; i <= 3; i++ {
		asks = append(asks, ask(fmt.Sprintf("Filed batch decision %d", i)))
	}
	status, result := hostHTTP(t, http.MethodPost, env.server.URL+"/asks/batch", agent.AccessToken, map[string]any{
		"request_key": "brief-batch-1", "asks": asks,
	})
	if status != http.StatusCreated {
		t.Fatalf("file ask batch: %d %#v", status, result)
	}
	if _, err := env.primitiveStore.AppendEvent(context.Background(), agent.ActorID, ask("Lone single ask")); err != nil {
		t.Fatalf("file single ask: %v", err)
	}

	body := readBriefV1(t, env.primitiveStore.(*primitives.Store))
	decisions, _ := body["decisions"].([]any)
	batches, singles := 0, 0
	for _, raw := range decisions {
		d, _ := raw.(map[string]any)
		batch, _ := d["batch"].(map[string]any)
		title := fmt.Sprint(d["title"])
		if batch == nil {
			if strings.HasPrefix(title, "Filed batch decision") {
				t.Fatalf("batch member appears as a separate decision: %#v", d)
			}
			if title == "Lone single ask" {
				singles++
			}
			continue
		}
		batches++
		if key, _ := batch["key"].(string); !strings.HasPrefix(key, "filed:") {
			t.Fatalf("batch key = %#v", batch["key"])
		}
		if c, _ := batch["count"].(float64); c != 3 {
			t.Fatalf("batch count = %#v in %#v", batch["count"], batch)
		}
		members, _ := batch["members"].([]any)
		if len(members) != 3 {
			t.Fatalf("members = %#v", members)
		}
		for _, m := range members {
			mm, _ := m.(map[string]any)
			if s, _ := mm["inbox_item_id"].(string); s == "" {
				t.Fatalf("member without inbox_item_id: %#v", mm)
			}
			if ev, _ := mm["event_ref"].(string); !strings.HasPrefix(ev, "event:") || len(ev) <= len("event:") {
				t.Fatalf("member without authoritative event_ref: %#v", mm)
			}
		}
	}
	if singles != 1 {
		t.Fatalf("expected the single ask as its own decision, got %d in %#v", singles, decisions)
	}
	if batches != 1 {
		t.Fatalf("expected exactly one batch decision, got %d in %#v", batches, decisions)
	}
}
