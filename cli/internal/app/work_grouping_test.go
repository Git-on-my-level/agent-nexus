package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"agent-nexus-cli/internal/config"
)

func TestGroupingCLIParsersAndOutput(t *testing.T) {
	for _, argv := range [][]string{{"work", "group", "card:child", "--under", "card:parent", "--after", "first"}, {"work", "fold", "card:child", "--into", "card:parent", "--request-key", "fold-1"}, {"work", "unfold", "card:child"}, {"work", "ungrouped", "--limit", "20"}, {"work", "create", "--from-file", "-", "--under", "card:parent"}} {
		parsed, err := parseWorkCommand(argv)
		if err != nil {
			t.Fatalf("%v: %v", argv, err)
		}
		if parsed.name == "work group" && (parsed.under != "card:parent" || parsed.after != "first") {
			t.Fatal(parsed)
		}
	}
	for _, argv := range [][]string{{"work", "group", "card:c"}, {"work", "fold", "card:c"}, {"work", "ungrouped", "--limit", "51"}} {
		if _, err := parseWorkCommand(argv); err == nil {
			t.Fatalf("accepted %v", argv)
		}
	}
	result := map[string]any{"work": map[string]any{"ref": "card:child"}, "likely_home": []any{map[string]any{"ref": "card:parent", "reasons": []string{"title_family"}}}}
	actions := deriveNextActions("work create", nil, result)
	raw, _ := json.Marshal(actions)
	if !strings.Contains(string(raw), "--under") || !strings.Contains(string(raw), "card:parent") {
		t.Fatalf("no runnable group action: %s", raw)
	}
	text := renderOrientText(map[string]any{"initiative_budget": map[string]any{"count": 7, "target": 15, "truncated": false}}, "")
	if !strings.Contains(text, "initiatives 7/15") {
		t.Fatal(text)
	}
}

func TestGroupingCLIGroupSendsOneReplaySafeMutation(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/work/card:child/group" {
			t.Errorf("unexpected %s %s", r.Method, r.URL)
			w.WriteHeader(404)
			return
		}
		calls++
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body["under"] != "card:parent" || body["after"] != "first" || body["request_key"] != "group-1" {
			t.Error(body)
		}
		fmt.Fprint(w, `{"work":{"ref":"card:child","parent_ref":"card:parent"}}`)
	}))
	defer server.Close()
	a, _ := workspaceTestApp(t, t.TempDir(), t.TempDir(), nil)
	_, _, err := a.runWorkCommand(context.Background(), []string{"work", "group", "card:child", "--under", "card:parent", "--after", "first", "--request-key", "group-1"}, config.Resolved{BaseURL: server.URL, ActorID: "worker", AccessToken: "fixture"})
	if err != nil || calls != 1 {
		t.Fatalf("calls=%d err=%v", calls, err)
	}
}

func TestGroupingCLICardsCreateFlags(t *testing.T) {
	a, _ := workspaceTestApp(t, t.TempDir(), t.TempDir(), nil)
	body, _, _, err := a.parseCardCreateInput([]string{"--board", "board:tasks", "--title", "Child", "--body", "Details", "--under", "card:parent", "--after", "first"}, config.Resolved{ActorID: "worker"}, "cards create")
	if err != nil || body["under"] != "card:parent" || body["after"] != "first" {
		t.Fatalf("body=%v err=%v", body, err)
	}
}

func TestGroupingHookKeepsOwnLoopPromises(t *testing.T) {
	loops := []any{}
	for i := 0; i < 7; i++ {
		loops = append(loops, map[string]any{"ref": fmt.Sprintf("card:own-%d", i), "yours": true, "state": "moving", "promise": map[string]any{"at": "2026-10-25T12:00:00Z", "what": fmt.Sprintf("Checkpoint %d", i)}})
	}
	loops = append(loops, map[string]any{"ref": "card:other", "state": "no_promise"})
	text := renderHookOrient(map[string]any{"principal": map[string]any{"id": "worker", "kind": "agent"}, "loops": loops})
	if !strings.Contains(text, "loops:") || !strings.Contains(text, "Checkpoint 0") || strings.Contains(text, "Checkpoint 5") || strings.Contains(text, "card:other") || len(text) > 6<<10 {
		t.Fatalf("loop context missing or unbounded: %s", text)
	}
}

func TestGroupingAskAutoSubjectRetainsCoreHome(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/agents/agent-1":
			fmt.Fprint(w, `{"agent":{"id":"agent-1","actor_id":"actor-1","handle":"worker.host"},"open_asks":[]}`)
		case r.URL.Path == "/work" && r.Method == "POST":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if body["create_surface"] != "ask.auto_create" {
				t.Errorf("surface=%v", body["create_surface"])
			}
			fmt.Fprint(w, `{"work":{"ref":"card:new-subject","board_ref":"board:tasks"},"likely_home":[{"ref":"card:launch","reasons":["title_family"]}],"next_actions":[{"argv":["anx","work","group","card:new-subject","--under","card:launch"]}]}`)
		case strings.HasPrefix(r.URL.Path, "/cards/"):
			fmt.Fprint(w, `{"card":{"id":"new-subject","ref":"card:new-subject","thread_id":"thread-2"}}`)
		case r.URL.Path == "/events" && r.Method == "POST":
			fmt.Fprint(w, `{"event":{"id":"ask-1","type":"human_attention_requested"}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	a, out := dailyTestApp(t, server.URL)
	if exit := a.Run([]string{"--json", "--as", "worker", "ask", "Ship it?", "--body", "Context", "--situation", "A launch decision is waiting.", "--impact", "The team cannot finish until we choose.", "--why", "This is the quickest safe approach.", "--recommend", "Yes", "--by", "1d"}); exit != 0 {
		t.Fatalf("exit=%d %s", exit, out.String())
	}
	doc := dailyJSON(t, out)
	homes := asSlice(asMap(doc["result"])["likely_home"])
	if len(homes) != 1 || asMap(homes[0])["ref"] != "card:launch" {
		t.Fatalf("lost core homes: %v", doc)
	}
	actions, _ := json.Marshal(doc["next_actions"])
	if !strings.Contains(string(actions), `"--under","card:launch"`) {
		t.Fatalf("lost group action: %s", actions)
	}
}

func TestInitiativeCLICommandsAndOrientTarget(t *testing.T) {
	for _, verb := range []string{"promote", "retire"} {
		args := []string{"initiative", verb, "card:launch", "--reason", "Independent scope", "--request-key", "stable"}
		if verb == "retire" {
			args = append(args, "--to-board", "board:tasks", "--under", "card:parent")
		}
		parsed, err := parseWorkCommand(args)
		if err != nil || parsed.reason != "Independent scope" {
			t.Fatalf("parsed=%v err=%v", parsed, err)
		}
		calls := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls++
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if r.Method != "POST" || r.URL.Path != "/work/card:launch/"+verb || body["reason"] != "Independent scope" || body["request_key"] != "stable" {
				t.Errorf("request=%s %s body=%v", r.Method, r.URL, body)
			}
			if verb == "retire" && (body["to_board"] != "board:tasks" || body["under"] != "card:parent") {
				t.Error(body)
			}
			fmt.Fprint(w, `{"work":{"ref":"card:launch"},"initiative_budget":{"count":2,"target":1,"truncated":true}}`)
		}))
		a, _ := workspaceTestApp(t, t.TempDir(), t.TempDir(), nil)
		_, _, err = a.runWorkCommand(context.Background(), args, config.Resolved{BaseURL: server.URL, ActorID: "writer", AccessToken: "test"})
		server.Close()
		if err != nil || calls != 1 {
			t.Fatalf("calls=%d err=%v", calls, err)
		}
	}
	for _, args := range [][]string{{"initiative", "promote", "card:c"}, {"initiative", "retire", "card:c", "--reason", ""}, {"initiative", "promote", "card:c", "--reason", "why", "--under", "card:p"}} {
		if _, err := parseWorkCommand(args); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
	text := renderOrientText(map[string]any{"initiative_budget": map[string]any{"count": 3, "target": 2}, "over_target": map[string]any{"count": 3, "target": 2, "next_actions": []any{map[string]any{"argv": []string{"anx", "ask", "Fold or keep?", "--subject-ref", "card:launch", "--recommend", "Fold into parent"}}}}}, "")
	if !strings.Contains(text, "initiatives 3/2") || !strings.Contains(text, "over_target") || !strings.Contains(text, `"Fold or keep?"`) {
		t.Fatal(text)
	}
}
