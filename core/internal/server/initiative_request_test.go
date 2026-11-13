package server

import (
	"context"
	"testing"

	"agent-nexus-core/internal/primitives"
)

// A human sets top-level goals. An agent may when a human asked and is named;
// on its own, its new card lands in Unsorted and a promotion is refused with
// the way forward.
func TestTopLevelGoalsNeedAHumanRequest(t *testing.T) {
	requireIntegrationTest(t)
	env := newAuthIntegrationEnv(t, authIntegrationOptions{bootstrapToken: testBootstrapToken})
	ctx := context.Background()
	agent := seedNotificationTestAgent(t, env, "goals.agent")
	human := seedHumanPrincipalForLockoutTest(t, ctx, env.workspace.DB(), "goals-human", "goals-human-actor", "goals-human", "goals-human-token")
	create := func(token string, body map[string]any) map[string]any {
		t.Helper()
		status, p := hostHTTP(t, "POST", env.server.URL+"/work", token, body)
		hostStatus(t, status, 201, p)
		return p
	}
	own := create(agent.AccessToken, map[string]any{"actor_id": agent.ActorID, "title": "Lane home for the pipeline driver", "initiative": true})
	work := own["work"].(map[string]any)
	if own["redirected"] != "unsorted" || work["is_initiative"] == true || work["needs_home"] != true {
		t.Fatalf("an unrequested agent goal must land in Unsorted: %v", own)
	}
	status, p := hostHTTP(t, "POST", env.server.URL+"/work/"+anyString(work["ref"])+"/move", agent.AccessToken, map[string]any{"actor_id": agent.ActorID, "root": true, "reason": "Independent outcome"})
	hostStatus(t, status, 403, p)
	if code := anyString(p["error"].(map[string]any)["code"]); code != "human_request_required" {
		t.Fatalf("code=%q", code)
	}
	status, p = hostHTTP(t, "POST", env.server.URL+"/work/"+anyString(work["ref"])+"/move", agent.AccessToken, map[string]any{"actor_id": agent.ActorID, "root": true, "reason": "Independent outcome", "requested_by": "Dana"})
	hostStatus(t, status, 200, p)
	if p["work"].(map[string]any)["is_initiative"] != true {
		t.Fatalf("a requested promotion must apply: %v", p)
	}
	asked := create(agent.AccessToken, map[string]any{"actor_id": agent.ActorID, "title": "Churn: 10% monthly", "initiative": true, "requested_by": "Dana"})
	if asked["redirected"] != nil || asked["work"].(map[string]any)["is_initiative"] != true {
		t.Fatalf("a requested agent goal must be top level: %v", asked)
	}
	direct := create(human.AccessToken, map[string]any{"title": "Margin: 50% variable", "initiative": true})
	if direct["redirected"] != nil || direct["work"].(map[string]any)["is_initiative"] != true {
		t.Fatalf("a human goal must be top level: %v", direct)
	}
	// Layout is not authority: an agent's card on an initiatives board still
	// waits in Unsorted, while a human's is top level there.
	store := env.primitiveStore.(*primitives.Store)
	board, err := store.CreateBoard(ctx, human.ActorID, map[string]any{"title": "Portfolio", "role": "initiatives"})
	if err != nil {
		t.Fatal(err)
	}
	onBoard := create(agent.AccessToken, map[string]any{"actor_id": agent.ActorID, "title": "Audit trail home", "board_id": board["id"]})
	if onBoard["work"].(map[string]any)["is_initiative"] == true {
		t.Fatalf("an agent's card on an initiatives board must not be top level: %v", onBoard)
	}
	status, card := hostHTTP(t, "POST", env.server.URL+"/boards/"+anyString(board["id"])+"/cards", agent.AccessToken, map[string]any{"actor_id": agent.ActorID, "card": map[string]any{"title": "Second audit trail home", "column_key": "backlog"}})
	hostStatus(t, status, 201, card)
	var needsHome bool
	if err = env.workspace.DB().QueryRow(`SELECT needs_home FROM cards WHERE id=?`, card["card"].(map[string]any)["id"]).Scan(&needsHome); err != nil || !needsHome {
		t.Fatalf("needs_home=%v err=%v for an agent card on an initiatives board", needsHome, err)
	}
	requestedOnBoard := create(agent.AccessToken, map[string]any{"actor_id": agent.ActorID, "title": "Retention: 60% at day 30", "board_id": board["id"], "initiative": true, "requested_by": "Dana"})
	if requestedOnBoard["work"].(map[string]any)["is_initiative"] != true {
		t.Fatalf("a requested goal on an initiatives board must be top level: %v", requestedOnBoard)
	}
	// Recurring work is not a goal initiative: an agent may still start a loop.
	loop := create(agent.AccessToken, map[string]any{"actor_id": agent.ActorID, "title": "Nightly error sweep", "cadence": map[string]any{"every_seconds": 86400}})
	if loop["redirected"] != nil {
		t.Fatalf("a loop must not be redirected: %v", loop)
	}
	status, brief := hostHTTP(t, "GET", env.server.URL+"/brief", human.AccessToken, nil)
	hostStatus(t, status, 200, brief)
	reasons := ""
	for _, change := range brief["initiative_changes"].(map[string]any)["items"].([]any) {
		reasons += anyString(change.(map[string]any)["payload"].(map[string]any)["reason"]) + "\n"
	}
	for _, want := range []string{"Independent outcome (requested by Dana)", "Created as a top-level goal (requested by Dana)"} {
		if !containsLine(reasons, want) {
			t.Fatalf("recent changes must name who asked (%q): %q", want, reasons)
		}
	}
}

func containsLine(text, want string) bool {
	for start := 0; start <= len(text); {
		end := start
		for end < len(text) && text[end] != '\n' {
			end++
		}
		if text[start:end] == want {
			return true
		}
		start = end + 1
	}
	return false
}
