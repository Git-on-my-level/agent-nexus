package protocol

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/Git-on-my-level/agent-nexus/mcp/catalog"
)

func TestInitialize(t *testing.T) {
	server := NewServer(testCatalog(t), executorFunc(nil), Options{Name: "test-mcp", Version: "dev"})
	resp := handleJSON(t, server, map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{}})

	result := resp["result"].(map[string]any)
	if result["protocolVersion"] == "" {
		t.Fatalf("missing protocolVersion: %#v", result)
	}
	info := result["serverInfo"].(map[string]any)
	if info["name"] != "test-mcp" || info["version"] != "dev" {
		t.Fatalf("unexpected serverInfo: %#v", info)
	}
}

func TestToolsListPagination(t *testing.T) {
	server := NewServer(testCatalog(t), executorFunc(nil), Options{})
	resp := handleJSON(t, server, map[string]any{"jsonrpc": "2.0", "id": "list-1", "method": "tools/list", "params": map[string]any{"limit": 1}})
	result := resp["result"].(map[string]any)
	tools := result["tools"].([]any)
	if len(tools) != 1 {
		t.Fatalf("page 1 tool count = %d, want 1", len(tools))
	}
	if result["nextCursor"] != "1" {
		t.Fatalf("nextCursor = %#v, want 1", result["nextCursor"])
	}

	resp = handleJSON(t, server, map[string]any{"jsonrpc": "2.0", "id": "list-2", "method": "tools/list", "params": map[string]any{"cursor": result["nextCursor"], "limit": 10}})
	result = resp["result"].(map[string]any)
	tools = result["tools"].([]any)
	if len(tools) != 1 {
		t.Fatalf("page 2 tool count = %d, want 1", len(tools))
	}
	if _, ok := result["nextCursor"]; ok {
		t.Fatalf("unexpected nextCursor on final page: %#v", result)
	}
}

func TestToolsCallDispatchesToExecutor(t *testing.T) {
	var got ToolCallRequest
	server := NewServer(testCatalog(t), executorFunc(func(_ context.Context, req ToolCallRequest) (ToolCallResult, error) {
		got = req
		return ToolCallResult{Result: map[string]any{"card": map[string]any{"id": "card-1"}}}, nil
	}), Options{})

	resp := handleJSON(t, server, map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "anx_cards_get",
			"arguments": map[string]any{
				"path": map[string]any{"card_id": "card-1"},
			},
		},
	})

	if got.Tool.Metadata.CommandID != "cards.get" {
		t.Fatalf("executor command = %q, want cards.get", got.Tool.Metadata.CommandID)
	}
	if got.Arguments["path"].(map[string]any)["card_id"] != "card-1" {
		t.Fatalf("executor args = %#v", got.Arguments)
	}
	result := resp["result"].(map[string]any)
	content := result["content"].([]any)
	if len(content) != 1 || content[0].(map[string]any)["type"] != "text" {
		t.Fatalf("unexpected content: %#v", content)
	}
	structured := result["structuredContent"].(map[string]any)
	if structured["command_id"] != "cards.get" || structured["status"] != "ok" {
		t.Fatalf("unexpected structured content: %#v", structured)
	}
}

func TestToolsCallRejectsUnknownToolAndArguments(t *testing.T) {
	server := NewServer(testCatalog(t), executorFunc(nil), Options{})
	resp := handleJSON(t, server, map[string]any{
		"jsonrpc": "2.0",
		"id":      3,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      "anx_secrets_reveal_batch",
			"arguments": map[string]any{},
		},
	})
	assertErrorCode(t, resp, "tool_not_allowed")

	resp = handleJSON(t, server, map[string]any{
		"jsonrpc": "2.0",
		"id":      30,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      "anx_not_a_real_command",
			"arguments": map[string]any{},
		},
	})
	assertErrorCode(t, resp, "tool_not_found")

	resp = handleJSON(t, server, map[string]any{
		"jsonrpc": "2.0",
		"id":      4,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "anx_cards_get",
			"arguments": map[string]any{
				"unexpected": "value",
			},
		},
	})
	assertErrorCode(t, resp, "invalid_arguments")
}

func TestToolsCallExecutorErrorShape(t *testing.T) {
	server := NewServer(testCatalog(t), executorFunc(func(context.Context, ToolCallRequest) (ToolCallResult, error) {
		return ToolCallResult{}, ToolError{Code: "workspace_auth_failed", Message: "workspace auth failed", JSONRPCCode: ErrInvalidParams}
	}), Options{})
	resp := handleJSON(t, server, map[string]any{
		"jsonrpc": "2.0",
		"id":      5,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      "anx_cards_get",
			"arguments": map[string]any{},
		},
	})
	assertErrorCode(t, resp, "workspace_auth_failed")
}

func TestMethodNotFoundAndParseError(t *testing.T) {
	server := NewServer(testCatalog(t), executorFunc(nil), Options{})
	resp := handleJSON(t, server, map[string]any{"jsonrpc": "2.0", "id": 6, "method": "resources/list"})
	assertErrorCode(t, resp, "method_not_found")

	raw, err := server.Handle(context.Background(), []byte(`{`))
	if err != nil {
		t.Fatalf("Handle() transport error = %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("response JSON error = %v: %s", err, raw)
	}
	assertErrorCode(t, parsed, "parse_error")
}

func TestNotificationsDoNotReturnJSONRPCError(t *testing.T) {
	server := NewServer(testCatalog(t), executorFunc(nil), Options{})
	for _, input := range []string{
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","method":"tools/list"}`,
	} {
		raw, err := server.Handle(context.Background(), []byte(input))
		if err != nil {
			t.Fatalf("Handle() transport error = %v", err)
		}
		if len(raw) != 0 {
			t.Fatalf("notification response = %s, want no response", raw)
		}
	}
}

func TestNullIDIsReturnedAsNull(t *testing.T) {
	server := NewServer(testCatalog(t), executorFunc(nil), Options{})
	resp := handleJSON(t, server, map[string]any{"jsonrpc": "2.0", "id": nil, "method": "resources/list"})
	if _, ok := resp["id"]; !ok {
		t.Fatalf("response omitted id for null-id request: %#v", resp)
	}
	if resp["id"] != nil {
		t.Fatalf("response id = %#v, want null", resp["id"])
	}
}

type executorFunc func(context.Context, ToolCallRequest) (ToolCallResult, error)

func (f executorFunc) CallTool(ctx context.Context, req ToolCallRequest) (ToolCallResult, error) {
	if f == nil {
		return ToolCallResult{}, errors.New("unexpected executor call")
	}
	return f(ctx, req)
}

func testCatalog(t *testing.T) *catalog.Catalog {
	t.Helper()
	cat, err := catalog.Build(catalog.CommandRegistry{Commands: []catalog.Command{
		{CommandID: "cards.get", Group: "cards", Method: "GET", Path: "/cards/{card_id}", PathParams: []string{"card_id"}},
		{CommandID: "docs.revisions.create", Group: "docs", Method: "POST", Path: "/docs/{document_id}/revisions", InputMode: "json-body", PathParams: []string{"document_id"}},
		{CommandID: "secrets.reveal-batch", Group: "secret", Method: "POST", Path: "/secrets/reveal-batch", InputMode: "json-body"},
	}}, catalog.Policy{
		ValidClassifications: []string{catalog.ClassificationExposedRead, catalog.ClassificationExposedWrite, catalog.ClassificationGatedSensitive},
		Commands: map[string]catalog.PolicyEntry{
			"cards.get":             {Classification: catalog.ClassificationExposedRead},
			"docs.revisions.create": {Classification: catalog.ClassificationExposedWrite},
			"secrets.reveal-batch":  {Classification: catalog.ClassificationGatedSensitive},
		},
	}, catalog.BuildOptions{})
	if err != nil {
		t.Fatalf("catalog.Build() error = %v", err)
	}
	return cat
}

func handleJSON(t *testing.T, server *Server, req map[string]any) map[string]any {
	t.Helper()
	input, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("request JSON error = %v", err)
	}
	raw, err := server.Handle(context.Background(), input)
	if err != nil {
		t.Fatalf("Handle() transport error = %v", err)
	}
	var resp map[string]any
	if err := json.Unmarshal(raw, &resp); err != nil {
		t.Fatalf("response JSON error = %v: %s", err, raw)
	}
	return resp
}

func assertErrorCode(t *testing.T, resp map[string]any, want string) {
	t.Helper()
	errObj, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatalf("missing error in response: %#v", resp)
	}
	data := errObj["data"].(map[string]any)
	if data["code"] != want {
		t.Fatalf("error code = %#v, want %q; response=%#v", data["code"], want, resp)
	}
}
