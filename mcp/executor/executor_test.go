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

func TestWorkspaceExecutorExecutesReadWithBoundedDefaultLimit(t *testing.T) {
	var gotAuth string
	var gotLimit string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/cards" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.String())
		}
		gotAuth = r.Header.Get("Authorization")
		gotLimit = r.URL.Query().Get("limit")
		writeJSON(t, w, http.StatusOK, map[string]any{
			"card":        map[string]any{"id": "card-1"},
			"next_cursor": "cursor-2",
		})
	}))
	defer server.Close()

	exec := NewWorkspaceExecutor(server.URL, Options{Auth: AuthContext{BearerToken: "workspace-token"}})
	result, err := exec.CallTool(context.Background(), protocol.ToolCallRequest{
		Tool:      testTool(t, "cards.list"),
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatalf("CallTool() error = %v", err)
	}
	if gotAuth != "Bearer workspace-token" {
		t.Fatalf("Authorization = %q", gotAuth)
	}
	if gotLimit != "50" {
		t.Fatalf("default limit = %q, want 50", gotLimit)
	}
	if result.CommandID != "cards.list" || result.Status != "ok" {
		t.Fatalf("unexpected result envelope: %#v", result)
	}
	if result.Pagination["next_cursor"] != "cursor-2" {
		t.Fatalf("pagination = %#v", result.Pagination)
	}
	if len(result.Warnings) != 1 || !strings.Contains(result.Warnings[0], "defaulted query.limit") {
		t.Fatalf("warnings = %#v", result.Warnings)
	}
}

func TestWorkspaceExecutorExecutesWriteAndPropagatesIdempotency(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/boards" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.String())
		}
		if got := r.Header.Get("Idempotency-Key"); got != "idem-1" {
			t.Fatalf("Idempotency-Key = %q", got)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body["request_key"] != "idem-1" {
			t.Fatalf("request_key = %#v, want idem-1; body=%#v", body["request_key"], body)
		}
		writeJSON(t, w, http.StatusCreated, map[string]any{"board": map[string]any{"id": "board-1"}})
	}))
	defer server.Close()

	exec := NewWorkspaceExecutor(server.URL, Options{})
	result, err := exec.CallTool(context.Background(), protocol.ToolCallRequest{
		Tool: testTool(t, "boards.create"),
		Arguments: map[string]any{
			"body":            map[string]any{"title": "Launch"},
			"idempotency_key": "idem-1",
		},
	})
	if err != nil {
		t.Fatalf("CallTool() error = %v", err)
	}
	if result.CommandID != "boards.create" {
		t.Fatalf("command_id = %q", result.CommandID)
	}
}

func TestWorkspaceExecutorExecutesPatch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch || r.URL.Path != "/cards/card-1" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.String())
		}
		writeJSON(t, w, http.StatusOK, map[string]any{"card": map[string]any{"id": "card-1", "title": "Updated"}})
	}))
	defer server.Close()

	exec := NewWorkspaceExecutor(server.URL, Options{})
	_, err := exec.CallTool(context.Background(), protocol.ToolCallRequest{
		Tool: testTool(t, "cards.patch"),
		Arguments: map[string]any{
			"path": map[string]any{"card_id": "card-1"},
			"body": map[string]any{"title": "Updated"},
		},
	})
	if err != nil {
		t.Fatalf("CallTool() error = %v", err)
	}
}

func TestWorkspaceExecutorArgumentValidation(t *testing.T) {
	exec := NewWorkspaceExecutor("http://127.0.0.1", Options{})
	tests := []struct {
		name      string
		tool      catalog.Tool
		arguments map[string]any
		want      string
	}{
		{
			name:      "missing path",
			tool:      testTool(t, "cards.get"),
			arguments: map[string]any{},
			want:      "path is required",
		},
		{
			name:      "query too large",
			tool:      testTool(t, "cards.list"),
			arguments: map[string]any{"query": map[string]any{"limit": 1000}},
			want:      "query.limit must be between",
		},
		{
			name:      "body not accepted",
			tool:      testTool(t, "cards.get"),
			arguments: map[string]any{"path": map[string]any{"card_id": "card-1"}, "body": map[string]any{"title": "bad"}},
			want:      "body arguments are not accepted",
		},
		{
			name:      "required body",
			tool:      testTool(t, "boards.create.required"),
			arguments: map[string]any{"body": map[string]any{}},
			want:      "missing body.title",
		},
		{
			name:      "body type mismatch",
			tool:      testTool(t, "boards.create.required"),
			arguments: map[string]any{"body": map[string]any{"title": 123}},
			want:      "body.title must be a string",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := exec.CallTool(context.Background(), protocol.ToolCallRequest{Tool: tt.tool, Arguments: tt.arguments})
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want containing %q", err, tt.want)
			}
		})
	}
}

func TestWorkspaceExecutorRedactsSensitiveResults(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, map[string]any{
			"access_token":  "access-123",
			"refresh_token": "refresh-123",
			"invite_token":  "invite-123",
			"private_key":   "-----BEGIN PRIVATE KEY-----\nabc\n-----END PRIVATE KEY-----",
			"environment":   "ANX_SECRET=value\nPATH=/bin",
			"nested":        map[string]any{"secret_value": "secret-123"},
		})
	}))
	defer server.Close()

	cat := testCatalog(t)
	exec := NewWorkspaceExecutor(server.URL, Options{})
	mcp := protocol.NewServer(cat, exec, protocol.Options{})
	raw, err := mcp.Handle(context.Background(), mustJSON(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      "anx_cards_get",
			"arguments": map[string]any{"path": map[string]any{"card_id": "card-1"}},
		},
	}))
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	text := string(raw)
	for _, leaked := range []string{"access-123", "refresh-123", "invite-123", "BEGIN PRIVATE KEY", "ANX_SECRET", "secret-123"} {
		if strings.Contains(text, leaked) {
			t.Fatalf("MCP response leaked %q: %s", leaked, text)
		}
	}
}

func TestWorkspaceExecutorShapesUpstreamErrors(t *testing.T) {
	statuses := map[int]string{
		http.StatusBadRequest:          "invalid_arguments",
		http.StatusUnauthorized:        "workspace_auth_failed",
		http.StatusForbidden:           "workspace_auth_failed",
		http.StatusNotFound:            "workspace_error",
		http.StatusConflict:            "workspace_error",
		http.StatusTooManyRequests:     "rate_limited",
		http.StatusInternalServerError: "workspace_error",
	}
	for status, wantCode := range statuses {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				writeJSON(t, w, status, map[string]any{
					"code":          "upstream_code",
					"message":       "failed with Bearer secret-token",
					"access_token":  "must-not-leak",
					"refresh_token": "must-not-leak",
				})
			}))
			defer server.Close()

			exec := NewWorkspaceExecutor(server.URL, Options{})
			_, err := exec.CallTool(context.Background(), protocol.ToolCallRequest{
				Tool:      testTool(t, "cards.get"),
				Arguments: map[string]any{"path": map[string]any{"card_id": "card-1"}},
			})
			if err == nil {
				t.Fatal("expected error")
			}
			var toolErr protocol.ToolError
			if !asToolError(err, &toolErr) {
				t.Fatalf("error type = %T, want protocol.ToolError", err)
			}
			if toolErr.Code != wantCode {
				t.Fatalf("code = %q, want %q; err=%v", toolErr.Code, wantCode, err)
			}
			for _, leaked := range []string{"secret-token", "must-not-leak"} {
				if strings.Contains(err.Error(), leaked) {
					t.Fatalf("error leaked %q: %v", leaked, err)
				}
			}
		})
	}
}

func asToolError(err error, target *protocol.ToolError) bool {
	if e, ok := err.(protocol.ToolError); ok {
		*target = e
		return true
	}
	return false
}

func testTool(t *testing.T, commandID string) catalog.Tool {
	t.Helper()
	tool, ok := testCatalog(t).Lookup(catalog.ToolName(commandIDForCatalog(commandID)))
	if !ok {
		t.Fatalf("missing test tool %s", commandID)
	}
	if commandID == "boards.create.required" {
		tool.Metadata.CommandID = "boards.create"
	}
	return tool
}

func commandIDForCatalog(commandID string) string {
	if commandID == "boards.create.required" {
		return commandID
	}
	return commandID
}

func testCatalog(t *testing.T) *catalog.Catalog {
	t.Helper()
	cat, err := catalog.Build(catalog.CommandRegistry{Commands: []catalog.Command{
		{
			CommandID:  "cards.get",
			Group:      "cards",
			Method:     http.MethodGet,
			Path:       "/cards/{card_id}",
			PathParams: []string{"card_id"},
		},
		{
			CommandID: "cards.list",
			Group:     "cards",
			Method:    http.MethodGet,
			Path:      "/cards",
			InputMode: "none",
		},
		{
			CommandID:  "cards.patch",
			Group:      "cards",
			Method:     http.MethodPatch,
			Path:       "/cards/{card_id}",
			PathParams: []string{"card_id"},
			InputMode:  "json-body",
		},
		{
			CommandID: "boards.create",
			Group:     "boards",
			Method:    http.MethodPost,
			Path:      "/boards",
			InputMode: "json-body",
		},
		{
			CommandID: "boards.create.required",
			Group:     "boards",
			Method:    http.MethodPost,
			Path:      "/boards",
			InputMode: "json-body",
			BodySchema: catalog.FieldSchema{Required: []catalog.Field{
				{Name: "title", Type: "string"},
			}},
		},
	}}, catalog.Policy{
		ValidClassifications: []string{catalog.ClassificationExposedRead, catalog.ClassificationExposedWrite},
		Commands: map[string]catalog.PolicyEntry{
			"cards.get":              {Classification: catalog.ClassificationExposedRead},
			"cards.list":             {Classification: catalog.ClassificationExposedRead},
			"cards.patch":            {Classification: catalog.ClassificationExposedWrite},
			"boards.create":          {Classification: catalog.ClassificationExposedWrite},
			"boards.create.required": {Classification: catalog.ClassificationExposedWrite},
		},
	}, catalog.BuildOptions{})
	if err != nil {
		t.Fatalf("catalog.Build() error = %v", err)
	}
	return cat
}

func writeJSON(t *testing.T, w http.ResponseWriter, status int, payload any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		t.Fatalf("encode response: %v", err)
	}
}

func mustJSON(t *testing.T, payload any) []byte {
	t.Helper()
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	return encoded
}
