package protocol

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Git-on-my-level/agent-nexus/mcp/catalog"
	"github.com/Git-on-my-level/agent-nexus/mcp/policy"
)

// Exercise the shipped policy and generated registry through JSON-RPC, including
// an admin-enabled catalog: human decisions must never reach an agent executor.
func TestAccessPolicyAllowsRequestsAndRejectsHumanTools(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	f, err := os.Open(filepath.Join(filepath.Dir(file), "../../contracts/gen/meta/commands.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	registry, err := catalog.LoadCommandRegistry(f)
	if err != nil {
		t.Fatal(err)
	}
	p, err := catalog.LoadPolicy(strings.NewReader(policy.DefaultToolPolicyYAML))
	if err != nil {
		t.Fatal(err)
	}
	for _, admin := range []bool{false, true} {
		name := "default"
		if admin {
			name = "admin-enabled"
		}
		t.Run(name, func(t *testing.T) {
			allowed := catalog.DefaultAllowedClassifications()
			allowed[catalog.ClassificationGatedAdmin] = admin
			cat, err := catalog.Build(registry, p, catalog.BuildOptions{AllowedClassifications: allowed})
			if err != nil {
				t.Fatal(err)
			}
			var calls []ToolCallRequest
			server := NewServer(cat, executorFunc(func(_ context.Context, req ToolCallRequest) (ToolCallResult, error) {
				calls = append(calls, req)
				return ToolCallResult{Result: map[string]any{}}, nil
			}), Options{})
			for _, id := range []string{
				"auth.access-requests.list", "auth.access-requests.approve",
				"auth.access-requests.deny", "auth.access-requests.summary", "inbox.respond",
			} {
				t.Run(id, func(t *testing.T) {
					toolName := catalog.ToolName(id)
					if _, ok := cat.Lookup(toolName); ok {
						t.Fatalf("human-only tool %s is advertised", id)
					}
					resp := handleJSON(t, server, map[string]any{
						"jsonrpc": "2.0", "id": id, "method": "tools/call",
						"params": map[string]any{"name": toolName, "arguments": map[string]any{
							"path": map[string]any{"request_id": "request-1", "inbox_id": "access-item"},
							"body": map[string]any{"outcome": "approved"},
						}},
					})
					assertErrorCode(t, resp, "tool_not_allowed")
				})
			}
			if len(calls) != 0 {
				t.Fatal("human-only tools reached the workspace executor")
			}
			for _, id := range []string{"auth.access-requests.request", "inbox.summary"} {
				args := map[string]any{"query": map[string]any{"limit": 0}}
				if id == "auth.access-requests.request" {
					args = map[string]any{"body": map[string]any{"grant": "auth-admin", "reason": "Maintain workspace authentication"}}
					tool, _ := cat.Lookup(catalog.ToolName(id))
					props := tool.InputSchema["properties"].(map[string]any)
					if _, ok := props["idempotency_key"]; ok {
						t.Fatal("naturally idempotent request advertises an unsupported retry key")
					}
				} else {
					tool, _ := cat.Lookup(catalog.ToolName(id))
					props := tool.InputSchema["properties"].(map[string]any)
					if _, ok := props["query"]; !ok {
						t.Fatal("summary does not advertise query arguments")
					}
				}
				resp := handleJSON(t, server, map[string]any{
					"jsonrpc": "2.0", "id": id, "method": "tools/call",
					"params": map[string]any{"name": catalog.ToolName(id), "arguments": args},
				})
				if _, ok := resp["error"]; ok {
					t.Fatalf("agent tool %s rejected: %#v", id, resp)
				}
			}
			if len(calls) != 2 || calls[0].Tool.Metadata.CommandID != "auth.access-requests.request" || calls[1].Tool.Metadata.CommandID != "inbox.summary" {
				t.Fatalf("unexpected dispatched calls: %#v", calls)
			}
		})
	}
}
