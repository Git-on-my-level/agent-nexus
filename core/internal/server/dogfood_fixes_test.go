package server

import (
	"context"
	"fmt"
	"testing"
)

func TestWorkCreatePlanCadenceGoalRoundTrip(t *testing.T) {
	t.Parallel()
	h := newPrimitivesTestServer(t)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Writer","created_at":"2026-10-18T00:00:00Z"}}`, 201).Body.Close()
	input := map[string]any{
		"actor_id": "actor-1", "title": "Periodic verification", "request_key": "plan-cadence-goal",
		"plan":    map[string]any{"steps": []any{map[string]any{"id": "verify", "title": "Verify results", "after": []any{}, "due": "2026-10-20", "status": "active"}}},
		"cadence": map[string]any{"every_seconds": 86400},
		"goal":    map[string]any{"metric": "score", "direction": "up", "target": 90},
	}
	created := planRequest(t, "POST", h.baseURL+"/work", input, 201)["work"].(map[string]any)
	ref := anyString(created["ref"])
	for _, response := range []map[string]any{created, planRequest(t, "POST", h.baseURL+"/work", input, 201)["work"].(map[string]any), planRequest(t, "GET", h.baseURL+"/work/"+ref, nil, 200)["work"].(map[string]any)} {
		for _, key := range []string{"plan", "cadence", "goal"} {
			if fmt.Sprint(response[key]) != fmt.Sprint(input[key]) {
				t.Fatalf("%s lost: %#v", key, response)
			}
		}
	}
	plan := planRequest(t, "GET", h.baseURL+"/cards/"+ref+"/plan", nil, 200)
	if fmt.Sprint(plan["plan"]) != fmt.Sprint(input["plan"]) {
		t.Fatalf("plan was inert metadata: %#v", plan)
	}
	// Invalid plans reject before creating any default board or card.
	bad := planRequest(t, "POST", h.baseURL+"/work", map[string]any{"actor_id": "actor-1", "title": "Rejected", "plan": map[string]any{"steps": []any{"silently dropped"}}}, 400)
	if anyString(bad["error"].(map[string]any)["code"]) != "invalid_request" {
		t.Fatal(bad)
	}
	var count int
	if err := h.workspace.DB().QueryRowContext(context.Background(), `SELECT count(*) FROM cards WHERE title='Rejected'`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("rejected create persisted card count=%d err=%v", count, err)
	}
}
