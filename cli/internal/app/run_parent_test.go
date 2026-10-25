package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/runcontext"
)

func runParentFixture(labels string, calls *int) *runcontext.Resolver {
	return &runcontext.Resolver{Getenv: func(k string) string {
		switch k {
		case "AGENTCTL_EXECUTION_ID":
			return "exec-test"
		case "AGENTCTL_BIN":
			return "/test/agentctl"
		}
		return ""
	}, Status: func(ctx context.Context, _, _ string) ([]byte, error) {
		*calls++
		if labels == "timeout" {
			<-ctx.Done()
			return nil, ctx.Err()
		}
		return []byte(`{"ok":true,"result":{"id":"exec-test","labels":` + labels + `}}`), nil
	}}
}
func TestRunParentCreateSurfaces(t *testing.T) {
	for _, tt := range []struct {
		name               string
		args               []string
		labels, want, code string
		unreadable         bool
	}{
		{name: "batch", args: []string{"boards", "cards", "create-batch", "--board-id", "board:tasks", "--from-file", "BATCH_FILE"}, labels: `["anx.card.parent"]`, want: "card:parent"},
		{name: "batch top level", args: []string{"boards", "cards", "create-batch", "--board-id", "board:tasks", "--from-file", "BATCH_FILE", "--top-level"}, labels: `["anx.card.parent"]`},
		{name: "work", args: []string{"work", "create", "--from-file", "FILE"}, labels: `["anx.card.parent"]`, want: "card:parent"},
		{name: "cards", args: []string{"cards", "create", "--board", "board:tasks", "--title", "Child", "--body", "Details"}, labels: `["anx.card.parent"]`, want: "card:parent"},
		{name: "cards json", args: []string{"cards", "create", "--from-file", "CARD_FILE"}, labels: `["anx.card.parent"]`, want: "card:parent"},
		{name: "explicit", args: []string{"work", "create", "--from-file", "FILE", "--under", "card:chosen"}, labels: `["anx.card.parent"]`, want: "card:chosen"},
		{name: "top level", args: []string{"work", "create", "--from-file", "FILE", "--top-level"}, labels: `["anx.card.parent"]`},
		{name: "cards top level", args: []string{"cards", "create", "--from-file", "CARD_FILE", "--top-level"}, labels: `["anx.card.parent"]`},
		{name: "multiple", args: []string{"work", "create", "--from-file", "FILE"}, labels: `["anx.card.a","anx.card.b"]`, code: "run_parent_ambiguous"},
		{name: "cross workspace", args: []string{"work", "create", "--from-file", "FILE"}, labels: `["anx.card.parent"]`, unreadable: true, code: "run_parent_unreadable"},
		{name: "lookup timeout", args: []string{"work", "create", "--from-file", "FILE"}, labels: "timeout", code: "run_parent_unavailable"},
		{name: "no label", args: []string{"work", "create", "--from-file", "FILE"}, labels: `[]`},
		{name: "generic api", args: []string{"api", "work.create", "--json-body", `{"title":"Child"}`}, labels: `["anx.card.parent"]`, want: "card:parent"},
		{name: "ask", args: []string{"ask", "Ship it?", "--body", "Context", "--recommend", "Yes", "--by", "1d"}, labels: `["anx.card.parent"]`, want: "card:parent"},
		{name: "ask explicit", args: []string{"ask", "Ship it?", "--body", "Context", "--recommend", "Yes", "--by", "1d", "--under", "card:chosen"}, labels: `["anx.card.parent"]`, want: "card:chosen"},
		{name: "ask top level", args: []string{"ask", "Ship it?", "--body", "Context", "--recommend", "Yes", "--by", "1d", "--top-level"}, labels: `["anx.card.parent"]`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			creates := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch {
				case r.Method == "GET" && r.URL.Path == "/work/card:parent":
					if tt.unreadable {
						http.NotFound(w, r)
						return
					}
					fmt.Fprint(w, `{"work":{"ref":"card:parent","phase":"ready"}}`)
				case r.Method == "POST" && (r.URL.Path == "/work" || r.URL.Path == "/cards" || strings.HasSuffix(r.URL.Path, "/cards/batch")):
					creates++
					var body map[string]any
					_ = json.NewDecoder(r.Body).Decode(&body)
					under := anyString(body["under"])
					if strings.Contains(tt.name, "batch") {
						under = anyString(asMap(asSlice(body["items"])[0])["under"])
					}
					if got := under; got != tt.want {
						t.Errorf("under=%q want=%q body=%v", got, tt.want, body)
					}
					if _, leaked := body["top_level"]; leaked {
						t.Error("local override leaked")
					}
					if body["request_key"] == nil && tt.name != "generic api" {
						t.Error("missing replay key")
					}
					if strings.Contains(tt.name, "batch") {
						_ = json.NewEncoder(w).Encode(map[string]any{"cards": []any{map[string]any{"ref": "card:child", "parent_ref": tt.want}}, "likely_home": []any{map[string]any{"ref": "card:home"}}})
						return
					}
					key := "work"
					if r.URL.Path == "/cards" {
						key = "card"
					}
					_ = json.NewEncoder(w).Encode(map[string]any{key: map[string]any{"ref": "card:child", "title": "Child", "board_ref": "board:tasks", "parent_ref": tt.want}, "likely_home": []any{map[string]any{"ref": "card:home"}}})
				case r.Method == "GET" && r.URL.Path == "/work":
					rows := []any{}
					for i := 0; i < 16; i++ {
						rows = append(rows, map[string]any{"ref": fmt.Sprintf("card:root-%d", i), "phase": "ready"})
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"work": rows})
				case r.URL.Path == "/agents/agent-1":
					fmt.Fprint(w, `{"agent":{"id":"agent-1","actor_id":"actor-1","handle":"worker.host"},"open_asks":[]}`)
				case strings.HasPrefix(r.URL.Path, "/cards/"):
					fmt.Fprint(w, `{"card":{"ref":"card:child","id":"child","thread_id":"thread-child"}}`)
				case r.URL.Path == "/events" && r.Method == "POST":
					fmt.Fprint(w, `{"event":{"id":"ask-1","type":"human_attention_requested"}}`)
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			a, out := dailyTestApp(t, server.URL)
			calls := 0
			a.runParent = runParentFixture(tt.labels, &calls)
			file := filepath.Join(t.TempDir(), "work.json")
			_ = os.WriteFile(file, []byte(`{"title":"Child"}`), 0600)
			cardFile := filepath.Join(t.TempDir(), "card.json")
			_ = os.WriteFile(cardFile, []byte(`{"board_id":"board:tasks","card":{"title":"Child","summary":"Details"}}`), 0600)
			batchFile := filepath.Join(t.TempDir(), "batch.json")
			_ = os.WriteFile(batchFile, []byte(`{"items":[{"card":{"title":"Child","summary":"Details"}}]}`), 0600)
			if strings.HasPrefix(tt.name, "ask") {
				tt.args = append(tt.args, "--situation", "A release is ready.", "--impact", "Users are waiting for the fix.", "--why", "The checks passed and shipping resolves the problem.")
			}
			args := append([]string{"--json", "--as", "worker"}, tt.args...)
			for i, arg := range args {
				if arg == "BATCH_FILE" {
					args[i] = batchFile
				}
				if arg == "FILE" {
					args[i] = file
				}
				if arg == "CARD_FILE" {
					args[i] = cardFile
				}
			}
			if exit := a.Run(args); exit != 0 {
				t.Fatalf("exit=%d output=%s", exit, out.String())
			}
			if creates != 1 {
				t.Fatalf("creates=%d output=%s", creates, out.String())
			}
			if tt.name == "work" {
				saved := out.String()
				out.Reset()
				if exit := a.Run(args[1:]); exit != 0 {
					t.Fatalf("text exit=%d %s", exit, out.String())
				}
				if !strings.Contains(out.String(), "Created under card:parent (from run label anx.card.parent); pass --top-level to opt out") || !strings.Contains(out.String(), "--ref card:child") {
					t.Fatalf("missing text notice/undo: %s", out.String())
				}
				out.Reset()
				out.WriteString(saved)
			}
			doc := dailyJSON(t, out)
			result := asMap(doc["result"])
			if tt.want == "card:parent" {
				if !strings.Contains(anyString(asMap(result["run_parent_default"])["message"]), "Created under card:parent (from run label anx.card.parent); pass --top-level to opt out") {
					t.Fatal(doc)
				}
				actions, _ := json.Marshal(doc["next_actions"])
				if !strings.Contains(string(actions), `"rm","card:parent","--ref","card:child"`) {
					t.Fatalf("missing undo: %s", actions)
				}
			}
			if tt.code != "" && warningCodes(doc)[tt.code] == "" {
				t.Fatalf("missing %s: %v", tt.code, doc)
			}
			if tt.want == "" {
				if warningCodes(doc)["work_over_target"] == "" {
					t.Fatalf("missing budget nudge: %v", doc)
				}
				if len(asSlice(result["likely_home"])) != 1 {
					t.Fatal("lost homes", doc)
				}
			}
			if strings.Contains(tt.name, "explicit") || strings.Contains(tt.name, "top level") {
				if calls != 0 {
					t.Fatalf("override probed agentctl %d times", calls)
				}
			}
		})
	}
}
func TestRunParentPlanUndoByRef(t *testing.T) {
	for _, args := range [][]string{{"plan", "step", "rm", "card:parent", "--ref", "card:child"}, {"work", "create", "--from-file", "-", "--top-level"}, {"cards", "create", "--top-level", "--from-file", "-"}, {"ask", "Question?", "--top-level"}} {
		if _, err := preflightConfigIndependentUsage(args); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
	}
	a, _ := workspaceTestApp(t, t.TempDir(), t.TempDir(), nil)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			fmt.Fprint(w, `{"card_ref":"card:parent","if_updated_at":"2026-10-01T00:00:00Z","plan":{"steps":[{"id":"keep","ref":"card:other","title":"Keep","after":[]},{"id":"remove","ref":"card:child","title":"Child","after":[]}]}}`)
			return
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["if_updated_at"] != "2026-10-01T00:00:00Z" {
			t.Fatal(body)
		}
		steps := asSlice(asMap(body["plan"])["steps"])
		if len(steps) != 1 || asMap(steps[0])["id"] != "keep" {
			t.Fatal(body)
		}
		fmt.Fprint(w, `{"card_ref":"card:parent","plan":{"steps":[]}}`)
	}))
	defer server.Close()
	_, _, err := a.runPlanCommand(context.Background(), []string{"plan", "step", "rm", "card:parent", "--ref", "card:child"}, config.Resolved{BaseURL: server.URL, ActorID: "worker", AccessToken: "fixture"})
	if err != nil {
		t.Fatal(err)
	}
}

func TestRunParentParentReadTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer server.Close()
	a, _ := workspaceTestApp(t, t.TempDir(), t.TempDir(), nil)
	calls := 0
	a.runParent = runParentFixture(`["anx.card.parent"]`, &calls)
	body := map[string]any{"title": "Child"}
	hint := a.prepareRunParent(context.Background(), config.Resolved{BaseURL: server.URL, AccessToken: "fixture"}, body, false)
	if len(hint.Warnings) != 1 || hint.Warnings[0].Code != "run_parent_unreadable" || body["under"] != nil {
		t.Fatalf("hint=%+v body=%v", hint, body)
	}
}
func TestCardCreateTopLevelFlagValues(t *testing.T) {
	for _, tt := range []struct {
		args []string
		want bool
	}{
		{[]string{"--title", "--top-level"}, false},
		{[]string{"--dry-run", "--top-level"}, true},
		{[]string{"--top-level", "--top-level=false"}, false},
	} {
		a, _ := workspaceTestApp(t, t.TempDir(), t.TempDir(), nil)
		args := append([]string{"--board", "board:tasks", "--title", "Child", "--body", "Details"}, tt.args...)
		_, _, got, err := a.parseCardCreateInput(args, config.Resolved{}, "cards create")
		if err != nil || got != tt.want {
			t.Fatalf("%v: got=%v err=%v", args, got, err)
		}
	}
}

func TestRunParentDryRun(t *testing.T) {
	for _, surface := range []string{"cards", "ask"} {
		for _, override := range []string{"", "--top-level", "--under"} {
			t.Run(surface+override, func(t *testing.T) {
				reads := 0
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Method != "GET" {
						t.Errorf("dry-run mutated: %s %s", r.Method, r.URL)
						http.Error(w, "mutation", 500)
						return
					}
					switch r.URL.Path {
					case "/work/card:parent":
						reads++
						fmt.Fprint(w, `{"work":{"ref":"card:parent","phase":"ready"}}`)
					case "/agents/agent-1":
						fmt.Fprint(w, `{"agent":{"id":"agent-1","actor_id":"actor-1","handle":"worker.host"},"open_asks":[]}`)
					default:
						http.NotFound(w, r)
					}
				}))
				defer server.Close()
				a, out := dailyTestApp(t, server.URL)
				calls := 0
				a.runParent = runParentFixture(`["anx.card.parent"]`, &calls)
				args := []string{"--json", "--as", "worker"}
				if surface == "cards" {
					args = append(args, "cards", "create", "--board", "board:tasks", "--title", "Child", "--body", "Details", "--dry-run")
				} else {
					args = append(args, "ask", "Ship the release?", "--situation", "A release is ready.", "--impact", "Users need the fix.", "--recommend", "Ship the release.", "--why", "The checks passed.", "--dry-run")
				}
				want := "card:parent"
				if override != "" {
					args = append(args, override)
					want = ""
				}
				if override == "--under" {
					args = append(args, "card:chosen")
					want = "card:chosen"
				}
				if exit := a.Run(args); exit != 0 {
					t.Fatalf("exit=%d %s", exit, out.String())
				}
				data := asMap(dailyJSON(t, out)["result"])
				if surface == "cards" {
					data = asMap(data["body"])
				}
				if got := anyString(data["under"]); got != want {
					t.Fatalf("under=%q want=%q output=%s", got, want, out.String())
				}
				if override == "" && (reads != 1 || calls != 1) {
					t.Fatalf("reads=%d lookups=%d", reads, calls)
				}
				if override != "" && (reads != 0 || calls != 0) {
					t.Fatalf("override reads=%d lookups=%d", reads, calls)
				}
			})
		}
	}
}
