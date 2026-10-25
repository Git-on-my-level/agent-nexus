package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Git-on-my-level/agent-nexus/mcp/catalog"
	"github.com/Git-on-my-level/agent-nexus/mcp/protocol"
)

func TestMCPRunParentDefault(t *testing.T) {
	for _, tt := range []struct {
		name, id, labels, want    string
		top, explicit, unreadable bool
	}{
		{name: "batch", id: "boards.cards.batch_add", labels: `["anx.card.parent"]`, want: "card:parent"},
		{name: "work", id: "work.create", labels: `["anx.card.parent"]`, want: "card:parent"},
		{name: "cards", id: "cards.create", labels: `["anx.card.parent"]`, want: "card:parent"},
		{name: "explicit", id: "work.create", labels: `["anx.card.parent"]`, explicit: true, want: "card:chosen"},
		{name: "top level", id: "work.create", labels: `["anx.card.parent"]`, top: true},
		{name: "multiple", id: "work.create", labels: `["anx.card.a","anx.card.b"]`},
		{name: "cross workspace", id: "work.create", labels: `["anx.card.parent"]`, unreadable: true},
		{name: "timeout", id: "work.create", labels: "timeout"},
		{name: "no context", id: "work.create"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			creates, lookups := 0, 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer fixture" {
					t.Error("lost principal")
				}
				switch {
				case r.Method == "GET" && r.URL.Path == "/work/card:parent":
					if tt.unreadable {
						w.WriteHeader(403)
						return
					}
					fmt.Fprint(w, `{"work":{"ref":"card:parent","phase":"ready"}}`)
				case r.Method == "GET" && r.URL.Path == "/work":
					if r.URL.Query().Get("limit") != "200" {
						t.Error("unbounded budget read")
					}
					rows := []any{}
					for i := 0; i < 16; i++ {
						rows = append(rows, map[string]any{"ref": fmt.Sprintf("card:r-%d", i), "phase": "ready"})
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"work": rows})
				case r.Method == "POST":
					creates++
					var body map[string]any
					_ = json.NewDecoder(r.Body).Decode(&body)
					under, _ := body["under"].(string)
					if tt.id == "boards.cards.batch_add" {
						items, _ := body["items"].([]any)
						item, _ := items[0].(map[string]any)
						under, _ = item["under"].(string)
					}
					if under != tt.want {
						t.Errorf("under=%q want=%q", under, tt.want)
					}
					if _, leak := body["top_level"]; leak {
						t.Fatal("override sent upstream")
					}
					if tt.id == "boards.cards.batch_add" {
						_ = json.NewEncoder(w).Encode(map[string]any{"cards": []any{map[string]any{"ref": "card:child", "parent_ref": tt.want}}})
						return
					}
					key := "work"
					if tt.id == "cards.create" {
						key = "card"
					}
					_ = json.NewEncoder(w).Encode(map[string]any{key: map[string]any{"ref": "card:child", "parent_ref": tt.want}, "likely_home": []any{map[string]any{"ref": "card:home"}}})
				default:
					t.Errorf("unexpected %s %s", r.Method, r.URL)
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			cat := generatedTestCatalog(t, map[string]bool{catalog.ClassificationExposedRead: true, catalog.ClassificationExposedWrite: true})
			tool, ok := cat.Lookup(catalog.ToolName(tt.id))
			if !ok {
				t.Fatal("missing tool")
			}
			props, _ := tool.InputSchema["properties"].(map[string]any)
			if props["top_level"] == nil {
				t.Fatal("missing opt-out schema")
			}
			e := NewWorkspaceExecutor(server.URL, Options{Auth: AuthContext{BearerToken: "fixture"}})
			e.runParent.Getenv = func(k string) string {
				if tt.labels == "" {
					return ""
				}
				switch k {
				case "AGENTCTL_EXECUTION_ID":
					return "exec-test"
				case "AGENTCTL_BIN":
					return "/test/agentctl"
				}
				return ""
			}
			e.runParent.Status = func(ctx context.Context, _, _ string) ([]byte, error) {
				lookups++
				if tt.labels == "timeout" {
					<-ctx.Done()
					return nil, ctx.Err()
				}
				return []byte(`{"ok":true,"result":{"id":"exec-test","labels":` + tt.labels + `}}`), nil
			}
			body := map[string]any{"title": "Child"}
			if tt.id == "cards.create" {
				body = map[string]any{"board_id": "board:tasks", "card": map[string]any{"title": "Child", "summary": "Details"}}
			}
			if tt.id == "boards.cards.batch_add" {
				body = map[string]any{"items": []any{map[string]any{"card": map[string]any{"title": "Child", "summary": "Details"}}}}
			}
			if tt.explicit {
				body["under"] = "card:chosen"
			}
			args := map[string]any{"body": body, "idempotency_key": "test-create"}
			if tt.id == "boards.cards.batch_add" {
				args["path"] = map[string]any{"board_id": "tasks"}
			}
			if tt.top {
				args["top_level"] = true
			}
			result, err := e.CallTool(context.Background(), protocol.ToolCallRequest{Tool: tool, Arguments: args})
			if err != nil {
				t.Fatal(err)
			}
			if creates != 1 {
				t.Fatal(creates)
			}
			data, _ := result.Result.(map[string]any)
			if tt.want == "card:parent" {
				if data["run_parent_default"] == nil {
					t.Fatal(data)
				}
				raw, _ := json.Marshal(data["next_actions"])
				if !strings.Contains(string(raw), `"rm","card:parent","--ref","card:child"`) {
					t.Fatalf("missing undo %s", raw)
				}
			} else if tt.want == "" {
				if !strings.Contains(strings.Join(result.Warnings, " "), "initiative budget 15") {
					t.Fatal(result.Warnings)
				}
				if data["likely_home"] == nil {
					t.Fatal(data)
				}
			}
			if (tt.explicit || tt.top || tt.labels == "") && lookups != 0 {
				t.Fatal("override looked up run")
			}
			// Every call revalidates workspace visibility, but labels stay cached.
			_, err = e.CallTool(context.Background(), protocol.ToolCallRequest{Tool: tool, Arguments: args})
			if err != nil {
				t.Fatal(err)
			}
			if lookups > 1 {
				t.Fatalf("uncached labels: %d", lookups)
			}
		})
	}
}
