package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/errnorm"
)

func TestWorkTreePreflight(t *testing.T) {
	for _, argv := range [][]string{
		{"mv", "card:x", "--root"}, {"work", "move", "card:x", "--root", "--unsorted", "--reason", "x"},
		{"mv", "card:x", "--under", ""}, {"tree", "--depth", "4"}, {"where"}, {"work", "ls"},
		{"mv", "card:x", "--under", "card:p", "--reason", strings.Repeat("x", 2001)},
	} {
		if _, err := preflightConfigIndependentUsage(argv); err == nil {
			t.Fatalf("accepted %v", argv)
		}
	}
	for _, argv := range [][]string{
		{"mv", "unsorted/x", "--under", "initiatives/p"}, {"work", "move", "card:x", "--unsorted"},
		{"tree"}, {"work", "tree", "initiatives/p", "--depth", "3", "--include-done"},
		{"where", "billing retries"}, {"work", "ls", "initiatives/p"},
		{"work", "create", "--title", "Child", "--under", "initiatives/p"}, {"work", "create", "--title", "Root", "--initiative"},
	} {
		if _, err := preflightConfigIndependentUsage(argv); err != nil {
			t.Fatalf("%v: %v", argv, err)
		}
	}
	if _, err := parseWorkCommand([]string{"work", "create", "--title", "X", "--under", "card:p", "--initiative"}); err == nil {
		t.Fatal("accepted conflicting creation home")
	}
	for _, name := range []string{"where", "mv", "tree", "work move", "work tree", "work ls"} {
		if help, ok := helpTopicText(name); !ok || !strings.Contains(help, "Usage: anx "+name) {
			t.Fatalf("missing help for %s: %s", name, help)
		}
	}
	if commandSideEffectClass("mv") != "remote_coordination_write" || commandSideEffectClass("where") != "read_only" {
		t.Fatal("wrong command effects")
	}
}

func TestWorkTreePathsAndMoveBody(t *testing.T) {
	var bodies []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/work/resolve":
			switch r.URL.Query().Get("path") {
			case "unsorted/c":
				fmt.Fprint(w, `{"ref":"card:c","path":"unsorted/c"}`)
			case "initiatives/p":
				fmt.Fprint(w, `{"ref":"card:p","path":"initiatives/p"}`)
			default:
				t.Error(r.URL)
			}
		case "/work/card:c/move":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			bodies = append(bodies, body)
			fmt.Fprint(w, `{"work":{"ref":"card:c","parent_ref":"card:p","needs_home":false}}`)
		default:
			t.Error(r.URL)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	a, _ := workspaceTestApp(t, t.TempDir(), t.TempDir(), nil)
	cfg := config.Resolved{BaseURL: server.URL, AccessToken: "fixture"}
	for _, argv := range [][]string{{"mv", "unsorted/c", "--under", "initiatives/p", "--reason", "Home confirmed", "--after", "step-1"}, {"work", "move", "card:c", "--root", "--reason", "Independent"}, {"mv", "card:c", "--unsorted"}} {
		if _, _, err := a.runWorkTreeCommand(context.Background(), argv, cfg); err != nil {
			t.Fatal(err)
		}
	}
	want := []map[string]any{{"under": "card:p", "reason": "Home confirmed", "after": "step-1"}, {"root": true, "reason": "Independent"}, {"unsorted": true}}
	if !reflect.DeepEqual(bodies, want) {
		t.Fatalf("bodies=%v want=%v", bodies, want)
	}
}

func TestWorkTreeWhereAndLs(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != "GET" {
			t.Error("where must be read only")
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/work/where":
			if r.URL.Query().Get("ref") != "card:c" && r.URL.Query().Get("q") != "billing retries" {
				t.Error(r.URL)
			}
			fmt.Fprint(w, `{"ref":"card:c","path":"unsorted/c","needs_home":true,"parent_ref":null,"is_initiative":false,"suggestions":[{"ref":"card:p","title":"Billing","path":"initiatives/p","reasons":["same_project"]}],"next_actions":[{"argv":["anx","work","move","card:c","--under","card:p"]}]}`)
		case "/work/tree":
			if r.URL.Query().Get("root") != "card:p" || r.URL.Query().Get("depth") != "2" || r.URL.Query().Get("include_done") != "true" {
				t.Error(r.URL)
			}
			fmt.Fprint(w, `{"node":{"ref":"card:p","title":"Billing","children":[{"ref":"card:c","title":"Retries","state":"ready","path":"initiatives/p/c","children":[],"has_more":true}],"has_more":true},"truncated":true}`)
		default:
			t.Error(r.URL)
		}
	}))
	defer server.Close()
	a, _ := workspaceTestApp(t, t.TempDir(), t.TempDir(), nil)
	cfg := config.Resolved{BaseURL: server.URL, AccessToken: "fixture"}
	for _, input := range []string{"card:c", "billing retries"} {
		r, _, err := a.runWorkTreeCommand(context.Background(), []string{"where", input}, cfg)
		if err != nil || !strings.Contains(r.Text, "suggestion card:p") {
			t.Fatalf("result=%v err=%v", r, err)
		}
	}
	r, _, err := a.runWorkTreeCommand(context.Background(), []string{"work", "ls", "card:p", "--include-done"}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(r.Text, "Billing") || !strings.Contains(r.Text, "Retries") || !strings.Contains(r.Text, "truncated") || commandResultBody(r)["has_more"] != true {
		t.Fatal(r)
	}
	if calls != 3 {
		t.Fatal(calls)
	}
}

func TestWorkTreeVersionSkew(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()
	a, _ := workspaceTestApp(t, t.TempDir(), t.TempDir(), nil)
	cfg := config.Resolved{BaseURL: server.URL, AccessToken: "fixture"}
	_, _, err := a.runWorkTreeCommand(context.Background(), []string{"tree"}, cfg)
	if err == nil || !strings.Contains(err.Error(), "update your anx server/CLI") {
		t.Fatalf("err=%v", err)
	}
	_, _, err = a.runWorkTreeCommand(context.Background(), []string{"tree", "card:missing"}, cfg)
	if err == nil || (errnorm.ExitCode(err) != 3 && asMap(errnorm.Normalize(err).Details)["status"] != 404) {
		t.Fatalf("missing card must remain not found: %v", err)
	}
}

func TestUnsortedCreationWarning(t *testing.T) {
	warnings, actions := resultWarnings("work create", nil, map[string]any{"work": map[string]any{"ref": "card:x", "needs_home": true}})
	if len(warnings) != 1 || warnings[0].Code != "needs_home" || !strings.Contains(warnings[0].Message, "anx work move card:x --under") || len(actions) != 1 {
		t.Fatal(warnings, actions)
	}
	if warnings, _ = resultWarnings("work create", nil, map[string]any{"work": map[string]any{"ref": "card:x", "needs_home": false}}); len(warnings) != 0 {
		t.Fatal(warnings)
	}
}

func TestWorkTreeJSONAliases(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/work/tree":
			fmt.Fprint(w, `{"initiatives":[],"unsorted":[],"truncated":false}`)
		case "/work/card:x/move":
			fmt.Fprint(w, `{"work":{"ref":"card:x","needs_home":true}}`)
		case "/work/where":
			fmt.Fprint(w, `{"ref":"card:x","path":"unsorted/x","parent_ref":null,"is_initiative":false,"needs_home":true,"suggestions":[],"next_actions":[]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	for _, args := range [][]string{{"tree"}, {"mv", "card:x", "--unsorted"}, {"where", "card:x"}} {
		a, out := dailyTestApp(t, server.URL)
		if code := a.Run(append([]string{"--json", "--as", "worker"}, args...)); code != 0 {
			t.Fatalf("%v code=%d %s", args, code, out.String())
		}
		doc := dailyJSON(t, out)
		wantID := map[string]string{"mv": "work.move", "tree": "work.tree", "where": "work.where"}[args[0]]
		if machineEnvelopeCommandID(doc) != wantID {
			t.Fatalf("identity=%v want=%s", doc, wantID)
		}
		if doc["ok"] != true || doc["result"] == nil {
			t.Fatal(doc)
		}
	}
}

func TestWorkCreateExplicitHomeAndHandle(t *testing.T) {
	var creates []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/work/resolve":
			fmt.Fprint(w, `{"ref":"card:p","path":"initiatives/p"}`)
		case r.URL.Path == "/work" && r.Method == "POST":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			creates = append(creates, body)
			fmt.Fprint(w, `{"work":{"ref":"card:new","needs_home":true,"parent_ref":null}}`)
		case r.URL.Path == "/work":
			fmt.Fprint(w, `{"work":[]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	for _, extra := range [][]string{{"--under", "initiatives/p"}, {"--initiative"}, {}} {
		a, _ := workspaceTestApp(t, t.TempDir(), t.TempDir(), nil)
		args := append([]string{"work", "create", "--title", "New", "--handle", "retry-webhooks"}, extra...)
		if _, _, err := a.runWorkCommand(context.Background(), args, config.Resolved{BaseURL: server.URL, AccessToken: "fixture"}); err != nil {
			t.Fatal(err)
		}
	}
	if len(creates) != 3 || creates[0]["under"] != "card:p" || creates[1]["initiative"] != true || creates[2]["under"] != nil || creates[2]["initiative"] != nil {
		t.Fatal(creates)
	}
	for _, body := range creates {
		if body["title"] != "New" || body["handle"] != "retry-webhooks" || body["request_key"] == nil {
			t.Fatal(body)
		}
	}
}

func TestInitiativeSuggestionsUseServerTreePredicate(t *testing.T) {
	for _, row := range []map[string]any{{"phase": "ready"}, {"phase": "ready", "needs_home": true}, {"phase": "ready", "is_initiative": false}, {"phase": "done", "is_initiative": true}} {
		if isOpenInitiative(row) {
			t.Fatalf("non-initiative admitted: %v", row)
		}
	}
	if !isOpenInitiative(map[string]any{"phase": "ready", "is_initiative": true, "goal": map[string]any{"metric": "score"}}) {
		t.Fatal("goal must not override stored tree identity")
	}
}
