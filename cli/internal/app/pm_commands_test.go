package app

import (
	"agent-nexus-cli/internal/config"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestPMProposalPreflight(t *testing.T) {
	for _, args := range [][]string{{}, {"card:x"}, {"card:x", "--status", "done", "--why", "Done"}, {"card:x", "--status", "ready", "--note", "a", "--why", "b"}, {"card:x", "--status", "invalid", "--why", "b"}, {"card:x", "--note", "a", "--why", "b", "--evidence", "untyped"}} {
		if _, err := preflightConfigIndependentUsage(append([]string{"pm", "propose"}, args...)); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
}
func TestPMProposeBuildsPayloadAndHidesPlumbing(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		calls++
		if body["lease_token"] != "lease" {
			t.Errorf("missing lease: %v", body)
		}
		if calls == 1 {
			if body["view"] != "card" || body["context_ref"] != "card:x" {
				t.Error(body)
			}
			_, _ = w.Write([]byte(`{"items":[{"ref":"card:x","title":"X","decision_revision":"1.2"}]}`))
			return
		}
		if body["work_ref"] != "card:x" || body["target_revision"] != "1.2" || body["instruction"] != "Evidence reviewed" {
			t.Error(body)
		}
		payload := asMap(body["payload"])
		if payload["phase"] != "done" || payload["resolution_refs"].([]any)[0] != "event:proof" {
			t.Error(body)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "d", "work_ref": "card:x", "status": "awaiting_answer", "scope": "work.phase", "payload": payload})
	}))
	defer srv.Close()
	a := New()
	a.Getenv = func(k string) string {
		switch k {
		case "ANX_PM_TURN_ID":
			return "turn"
		case "ANX_PM_LEASE_TOKEN":
			return "lease"
		}
		return ""
	}
	r, err := a.runPMPropose(context.Background(), []string{"card:x", "--status", "done", "--why", "Evidence reviewed", "--evidence", "event:proof"}, config.Resolved{BaseURL: srv.URL, AccessToken: "token"})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 || strings.Contains(r.Text, "work") || strings.Contains(r.Text, "lease") {
		t.Fatal(r.Text, calls)
	}
	if asMap(commandResultBody(r))["scope"] != "status" {
		t.Fatal(r.Data)
	}
}

func TestPMTurnPromptMeasurement(t *testing.T) {
	before, err := os.ReadFile("testdata/pm_prompt_before.txt")
	if err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile("testdata/pm_prompt_after.txt")
	if err != nil {
		t.Fatal(err)
	}
	prompt := buildPMPrompt("pm", map[string]any{"id": "pm_turn_1", "actor_id": "human-reader", "deadline": "2026-10-10T10:00:00Z", "text": "What needs my decision?", "context_refs": []string{"card:eval-card"}}, 16000)
	if prompt != string(after) {
		t.Fatal("refresh rendered after prompt fixture")
	}
	if len(after)*100 >= len(before)*40 {
		t.Fatalf("prompt over 40%%: %d/%d", len(after), len(before))
	}
	t.Logf("prompt bytes before=%d after=%d ratio=%.1f%%", len(before), len(after), 100*float64(len(after))/float64(len(before)))
}
