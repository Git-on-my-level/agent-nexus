package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"testing"
	"time"

	"agent-nexus-core/internal/primitives"
)

func TestAgentSummaryOptInBoundsParityAndPrivacy(t *testing.T) {
	requireIntegrationTest(t)
	env := newAuthIntegrationEnv(t, authIntegrationOptions{})
	ctx := context.Background()
	db := env.workspace.DB()
	agent := seedMachinePrincipalForLockoutTest(t, ctx, db, "summary-agent", "summary-actor", "summary.agent", "summary-token")
	if _, err := db.Exec(`INSERT INTO hosts(id,slug,display_name,os_user,hostname,discovered_adapters_json,created_at) VALUES('summary-host','summary-host','Summary host','fixture','fixture','[]','now')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO host_agents(host_id,name,agent_id,identity_kind) VALUES('summary-host','summary-agent',?,'derived')`, agent.AgentID); err != nil {
		t.Fatal(err)
	}
	reader := seedHumanPrincipalForLockoutTest(t, ctx, db, "summary-human", "summary-human-actor", "summary.human", "summary-human-token")
	s := env.primitiveStore.(*primitives.Store)
	publicBoard, err := s.CreateBoard(ctx, agent.ActorID, map[string]any{"title": "Agent work"})
	if err != nil {
		t.Fatal(err)
	}
	w, err := s.CreateWork(ctx, agent.ActorID, anyString(publicBoard["id"]), map[string]any{"title": "Current work", "summary": "Current prose", "phase": "in_progress"})
	if err != nil {
		t.Fatal(err)
	}
	resp := patchJSONExpectStatusWithAuth(t, env.server.URL+"/agents/me/presence", map[string]any{"current_card_ref": w["ref"]}, agent.AccessToken, http.StatusOK)
	resp.Body.Close()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for i := 0; i < 60; i++ {
		owner := agent.ActorID
		if i%2 == 1 {
			owner = "actor:" + owner
		}
		_, err = db.Exec(`INSERT INTO cards(id,handle,board_id,rank,title,summary,column_key,assignee,created_at,created_by,updated_at,updated_by) VALUES(?,?,?,'',?,'Assigned prose','in_progress',?,?,'fixture',?,'fixture')`, fmt.Sprintf("assigned-%02d", i), fmt.Sprintf("assigned-%02d", i), publicBoard["id"], fmt.Sprintf("Assigned %02d", i), owner, now, now)
		if err != nil {
			t.Fatal(err)
		}
	}
	b, err := s.CreateBoard(ctx, "private-owner", map[string]any{"title": "Private board"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.PatchThread(ctx, "private-owner", anyString(b["thread_id"]), map[string]any{"pm_actor_id": "private-owner"}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err = s.CreateWork(ctx, "private-owner", anyString(b["id"]), map[string]any{"title": "PrivateSummarySecret", "owner": "actor:" + agent.ActorID}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ListCards(ctx, primitives.CardListFilter{}); err != nil {
		t.Fatal("invalid card fixture", err)
	}
	read := func(path string) map[string]any {
		t.Helper()
		response := getJSONExpectStatusWithAuth(t, env.server.URL+path, reader.AccessToken, http.StatusOK)
		defer response.Body.Close()
		var body map[string]any
		if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		return body
	}
	path := "/agents/" + agent.AgentID
	legacy := read(path)
	assertLegacyCardShape(t, legacy)
	if _, ok := legacy["cards_truncated"]; ok {
		t.Fatal("legacy response gained truncation")
	}
	computed := read(path + "?summary=1")
	cards := computed["recent_cards"].([]any)
	if len(cards) != 50 || computed["cards_truncated"] != true {
		t.Fatalf("unbounded cards: %d %v", len(cards), computed["cards_truncated"])
	}
	control := false
	for _, raw := range cards {
		card := raw.(map[string]any)
		if card["title"] == "PrivateSummarySecret" {
			t.Fatal("private card leaked")
		}
		summary := card["work_summary"].(map[string]any)
		if summary["status"].(map[string]any)["state"] != "in_progress" || summary["set_status"] != nil || !reflect.DeepEqual(summary["hints"], []any{"no_plan"}) {
			t.Fatal(summary)
		}
		if card["ref"] == w["ref"] {
			control = true
			if card["summary_text"] != "Current prose" || !reflect.DeepEqual(summary, card["summary"]) {
				t.Fatal(card)
			}
			detail := read("/cards/" + anyString(w["ref"]) + "?summary=1")["card"].(map[string]any)
			if !reflect.DeepEqual(detail["work_summary"], summary) {
				t.Fatal("agent summary differs from card read", summary, detail)
			}
		}
	}
	if !control {
		t.Fatal("current card was displaced by assignment candidates")
	}
}
