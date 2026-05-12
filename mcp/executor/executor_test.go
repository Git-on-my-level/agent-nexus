package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
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

func TestWorkspaceExecutorRepresentativeCommandGroups(t *testing.T) {
	tests := []struct {
		name          string
		commandID     string
		arguments     map[string]any
		wantMethod    string
		wantPath      string
		wantQuery     map[string]string
		wantBodyField map[string]any
	}{
		{
			name:       "actors admin read",
			commandID:  "actors.list",
			wantMethod: http.MethodGet,
			wantPath:   "/actors",
			wantQuery:  map[string]string{"limit": "50"},
		},
		{
			name:       "agent notifications read",
			commandID:  "agent.notifications.list",
			wantMethod: http.MethodGet,
			wantPath:   "/agent-notifications",
		},
		{
			name:       "agent notifications write",
			commandID:  "agent.notifications.read",
			arguments:  map[string]any{"body": map[string]any{"notification_ids": []any{"note-1"}}},
			wantMethod: http.MethodPost,
			wantPath:   "/agent-notifications/read",
		},
		{
			name:       "agents me read",
			commandID:  "agents.me.get",
			wantMethod: http.MethodGet,
			wantPath:   "/agents/me",
		},
		{
			name:          "agents me update",
			commandID:     "agents.me.patch",
			arguments:     map[string]any{"body": map[string]any{"display_name": "Researcher"}},
			wantMethod:    http.MethodPatch,
			wantPath:      "/agents/me",
			wantBodyField: map[string]any{"display_name": "Researcher"},
		},
		{
			name:       "artifacts read",
			commandID:  "artifacts.get",
			arguments:  map[string]any{"path": map[string]any{"artifact_id": "artifact-1"}},
			wantMethod: http.MethodGet,
			wantPath:   "/artifacts/artifact-1",
		},
		{
			name:          "board cards write",
			commandID:     "boards.cards.create",
			arguments:     map[string]any{"path": map[string]any{"board_id": "board-1"}, "body": map[string]any{"card.title": "Do it"}},
			wantMethod:    http.MethodPost,
			wantPath:      "/boards/board-1/cards",
			wantBodyField: map[string]any{"card.title": "Do it"},
		},
		{
			name:          "card revisions write",
			commandID:     "cards.revisions.create",
			arguments:     map[string]any{"path": map[string]any{"card_id": "card-1"}, "body": map[string]any{"revision.summary": "Updated", "revision.title": "Card v2", "if_base_revision": "rev-1"}},
			wantMethod:    http.MethodPost,
			wantPath:      "/cards/card-1/revisions",
			wantBodyField: map[string]any{"revision.summary": "Updated"},
		},
		{
			name:          "doc revisions write",
			commandID:     "docs.revisions.create",
			arguments:     map[string]any{"path": map[string]any{"document_id": "doc-1"}, "body": map[string]any{"content": "body", "content_type": "text", "if_base_revision": "rev-1"}},
			wantMethod:    http.MethodPost,
			wantPath:      "/docs/doc-1/revisions",
			wantBodyField: map[string]any{"content_type": "text"},
		},
		{
			name:          "events bounded write",
			commandID:     "events.create",
			arguments:     map[string]any{"body": map[string]any{"event.actor_id": "actor-1", "event.provenance.sources": []any{}, "event.refs": []any{}, "event.summary": "noted", "event.type": "custom"}},
			wantMethod:    http.MethodPost,
			wantPath:      "/events",
			wantBodyField: map[string]any{"event.type": "custom"},
		},
		{
			name:       "events bounded read",
			commandID:  "events.list",
			wantMethod: http.MethodGet,
			wantPath:   "/events",
			wantQuery:  map[string]string{"limit": "50"},
		},
		{
			name:       "inbox read",
			commandID:  "inbox.get",
			arguments:  map[string]any{"path": map[string]any{"inbox_id": "inbox-1"}},
			wantMethod: http.MethodGet,
			wantPath:   "/inbox/inbox-1",
		},
		{
			name:       "meta read",
			commandID:  "meta.commands.get",
			arguments:  map[string]any{"path": map[string]any{"command_id": "cards.get"}},
			wantMethod: http.MethodGet,
			wantPath:   "/meta/commands/cards.get",
		},
		{
			name:       "topics timeline",
			commandID:  "topics.timeline",
			arguments:  map[string]any{"path": map[string]any{"topic_id": "topic-1"}},
			wantMethod: http.MethodGet,
			wantPath:   "/topics/topic-1/timeline",
			wantQuery:  map[string]string{"limit": "50"},
		},
		{
			name:       "threads projection read",
			commandID:  "threads.context",
			arguments:  map[string]any{"path": map[string]any{"thread_id": "thread-1"}},
			wantMethod: http.MethodGet,
			wantPath:   "/threads/thread-1/context",
		},
		{
			name:       "usage admin summary",
			commandID:  "usage.summary.v1",
			wantMethod: http.MethodGet,
			wantPath:   "/v1/usage/summary",
		},
	}

	cat := generatedTestCatalog(t, map[string]bool{
		catalog.ClassificationExposedRead:  true,
		catalog.ClassificationExposedWrite: true,
		catalog.ClassificationGatedAdmin:   true,
	})
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != tt.wantMethod || r.URL.Path != tt.wantPath {
					t.Fatalf("unexpected request %s %s, want %s %s", r.Method, r.URL.String(), tt.wantMethod, tt.wantPath)
				}
				for key, want := range tt.wantQuery {
					if got := r.URL.Query().Get(key); got != want {
						t.Fatalf("query %s = %q, want %q", key, got, want)
					}
				}
				if len(tt.wantBodyField) > 0 {
					var body map[string]any
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Fatalf("decode body: %v", err)
					}
					for key, want := range tt.wantBodyField {
						if got := body[key]; got != want {
							t.Fatalf("body.%s = %#v, want %#v; body=%#v", key, got, want, body)
						}
					}
				}
				writeJSON(t, w, http.StatusOK, map[string]any{"ok": true})
			}))
			defer server.Close()

			tool, ok := cat.Lookup(catalog.ToolName(tt.commandID))
			if !ok {
				t.Fatalf("missing generated test tool %s", tt.commandID)
			}
			exec := NewWorkspaceExecutor(server.URL, Options{})
			if _, err := exec.CallTool(context.Background(), protocol.ToolCallRequest{Tool: tool, Arguments: tt.arguments}); err != nil {
				t.Fatalf("CallTool() error = %v", err)
			}
		})
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

func TestWorkspaceExecutorRedactsSecretRevealValueResults(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/secrets/reveal-batch" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.String())
		}
		writeJSON(t, w, http.StatusOK, map[string]any{
			"secrets": []any{
				map[string]any{"name": "db_password", "value": "batch-secret"},
			},
			"metadata": map[string]any{"value": "metadata-secret"},
		})
	}))
	defer server.Close()

	allowed := catalog.DefaultAllowedClassifications()
	allowed[catalog.ClassificationGatedSensitive] = true
	cat := generatedTestCatalog(t, allowed)
	exec := NewWorkspaceExecutor(server.URL, Options{})
	mcp := protocol.NewServer(cat, exec, protocol.Options{})
	raw, err := mcp.Handle(context.Background(), mustJSON(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name": "anx_secrets_reveal_batch",
			"arguments": map[string]any{
				"body": map[string]any{"names": []any{"db_password"}},
			},
		},
	}))
	if err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	text := string(raw)
	for _, leaked := range []string{"batch-secret", "metadata-secret"} {
		if strings.Contains(text, leaked) {
			t.Fatalf("MCP response leaked %q: %s", leaked, text)
		}
	}
	if !strings.Contains(text, "[REDACTED]") {
		t.Fatalf("MCP response did not include redaction marker: %s", text)
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

func generatedTestCatalog(t *testing.T, allowed map[string]bool) *catalog.Catalog {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	mcpRoot := filepath.Dir(filepath.Dir(file))
	commandsFile, err := os.Open(filepath.Join(mcpRoot, "..", "contracts", "gen", "meta", "commands.json"))
	if err != nil {
		t.Fatalf("open commands metadata: %v", err)
	}
	defer commandsFile.Close()
	registry, err := catalog.LoadCommandRegistry(commandsFile)
	if err != nil {
		t.Fatalf("LoadCommandRegistry() error = %v", err)
	}
	policyFile, err := os.Open(filepath.Join(mcpRoot, "policy", "default_tool_policy.yaml"))
	if err != nil {
		t.Fatalf("open default policy: %v", err)
	}
	defer policyFile.Close()
	policy, err := catalog.LoadPolicy(policyFile)
	if err != nil {
		t.Fatalf("LoadPolicy() error = %v", err)
	}
	cat, err := catalog.Build(registry, policy, catalog.BuildOptions{AllowedClassifications: allowed})
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
