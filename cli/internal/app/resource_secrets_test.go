package app

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestFindSecretByNameOrID(t *testing.T) {
	t.Parallel()

	body := map[string]any{
		"secrets": []any{
			map[string]any{"id": "sec_1", "name": "ALPHA"},
			map[string]any{"id": "sec_2", "name": "BETA"},
		},
	}
	if got := findSecretByNameOrID(body, "ALPHA"); got == nil || got["id"] != "sec_1" {
		t.Fatalf("by name: got %#v", got)
	}
	if got := findSecretByNameOrID(body, "sec_2"); got == nil || got["name"] != "BETA" {
		t.Fatalf("by id: got %#v", got)
	}
	if findSecretByNameOrID(body, "missing") != nil {
		t.Fatal("expected nil for missing secret")
	}
	if findSecretByNameOrID("not-a-map", "x") != nil {
		t.Fatal("expected nil for non-object body")
	}
}

func TestExtractSecretEnvPairs(t *testing.T) {
	t.Parallel()

	body := map[string]any{
		"secrets": []any{
			map[string]any{"name": "K1", "value": "v1"},
			map[string]any{"name": "", "value": "skip"},
		},
	}
	pairs := extractSecretEnvPairs(body)
	if len(pairs) != 1 || pairs["K1"] != "v1" {
		t.Fatalf("got %#v", pairs)
	}
	if extractSecretEnvPairs(map[string]any{}) != nil {
		t.Fatal("expected nil when secrets key missing")
	}
}

func TestFormatSecretListText(t *testing.T) {
	t.Parallel()

	out := formatSecretListText(map[string]any{
		"secrets": []any{
			map[string]any{"name": "N", "description": "d", "updated_at": "t"},
		},
	})
	if out == "" || out == "No secrets." {
		t.Fatalf("unexpected output: %q", out)
	}
}

func TestSecretPreConfigUsagePreflightBeatsAmbiguousProfileResolution(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	writeAgentProfile(t, home, "agent-a", `{"agent":"agent-a","actor_id":"actor_a","base_url":"http://127.0.0.1:1","access_token":"token","access_token_expires_at":"2099-01-01T00:00:00Z"}`)
	writeAgentProfile(t, home, "agent-b", `{"agent":"agent-b","actor_id":"actor_b","base_url":"http://127.0.0.1:1","access_token":"token","access_token_expires_at":"2099-01-01T00:00:00Z"}`)

	tests := []struct {
		name        string
		args        []string
		command     string
		code        string
		messagePart string
	}{
		{
			name:        "create unknown flag",
			args:        []string{"secret", "create", "OPENAI_API_KEY", "--unknown"},
			command:     "secret create",
			code:        "invalid_flags",
			messagePart: "unknown",
		},
		{
			name:        "create missing description value",
			args:        []string{"secret", "create", "OPENAI_API_KEY", "--description"},
			command:     "secret create",
			code:        "invalid_flags",
			messagePart: "description",
		},
		{
			name:        "get unknown flag",
			args:        []string{"secret", "get", "OPENAI_API_KEY", "--unknown"},
			command:     "secret get",
			code:        "invalid_flags",
			messagePart: "unknown",
		},
		{
			name:        "update missing description value",
			args:        []string{"secret", "update", "OPENAI_API_KEY", "--description"},
			command:     "secret update",
			code:        "invalid_flags",
			messagePart: "description",
		},
		{
			name:        "exec missing secret value",
			args:        []string{"secret", "exec", "--secret"},
			command:     "secret exec",
			code:        "invalid_flags",
			messagePart: "secret",
		},
		{
			name:        "create valid local flags reach config",
			args:        []string{"secret", "create", "OPENAI_API_KEY", "--from-stdin", "--description", "API key"},
			command:     "secret create",
			code:        "config_resolution_failed",
			messagePart: "failed to resolve cli config",
		},
		{
			name:        "get valid local flags reach config",
			args:        []string{"secret", "get", "OPENAI_API_KEY", "--reveal"},
			command:     "secret get",
			code:        "config_resolution_failed",
			messagePart: "failed to resolve cli config",
		},
		{
			name:        "update valid local flags reach config",
			args:        []string{"secret", "update", "OPENAI_API_KEY", "--from-stdin", "--description", "API key"},
			command:     "secret update",
			code:        "config_resolution_failed",
			messagePart: "failed to resolve cli config",
		},
		{
			name:        "exec valid local flags reach config",
			args:        []string{"secret", "exec", "--secret", "OPENAI_API_KEY", "--", "env"},
			command:     "secret exec",
			code:        "config_resolution_failed",
			messagePart: "failed to resolve cli config",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			raw := runCLIForTest(t, home, nil, strings.NewReader("secret-value"), append([]string{"--json"}, tt.args...))
			payload := assertEnvelopeError(t, raw)
			if got := anyStringValue(payload["command"]); got != tt.command {
				t.Fatalf("expected command %q, got %#v", tt.command, payload)
			}
			errObj, _ := payload["error"].(map[string]any)
			if got := anyStringValue(errObj["code"]); got != tt.code {
				t.Fatalf("expected %s, got %#v", tt.code, payload)
			}
			if got := anyStringValue(errObj["message"]); !strings.Contains(got, tt.messagePart) {
				t.Fatalf("expected message to contain %q, got %#v", tt.messagePart, payload)
			}
		})
	}
}

func TestSecretCreateAndUpdateRequireExplicitStdin(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	writeAgentProfile(t, home, "agent-a", `{"agent":"agent-a","actor_id":"actor_a","base_url":"http://127.0.0.1:1","access_token":"token","access_token_expires_at":"2099-01-01T00:00:00Z"}`)

	tests := []struct {
		name    string
		args    []string
		command string
	}{
		{
			name:    "create",
			args:    []string{"--json", "secret", "create", "OPENAI_API_KEY"},
			command: "secret create",
		},
		{
			name:    "update",
			args:    []string{"--json", "secret", "update", "OPENAI_API_KEY"},
			command: "secret update",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			stdout := &bytes.Buffer{}
			stderr := &bytes.Buffer{}
			cli := New()
			cli.Stdout = stdout
			cli.Stderr = stderr
			cli.Stdin = strings.NewReader("secret-value")
			cli.StdinIsTTY = func() bool { return false }
			cli.UserHomeDir = func() (string, error) { return home, nil }
			cli.ReadFile = os.ReadFile
			cli.Getenv = func(string) string { return "" }

			exitCode := cli.Run(tt.args)
			if exitCode != 2 {
				t.Fatalf("expected exit code 2, got %d stdout=%s stderr=%s", exitCode, stdout.String(), stderr.String())
			}
			if strings.Contains(stderr.String(), "Enter ") || strings.Contains(stdout.String(), "Enter ") {
				t.Fatalf("secret command emitted prompt text: stdout=%q stderr=%q", stdout.String(), stderr.String())
			}
			payload := assertEnvelopeError(t, stdout.String())
			if got := anyStringValue(payload["command"]); got != tt.command {
				t.Fatalf("expected command %q, got %#v", tt.command, payload)
			}
			errObj, _ := payload["error"].(map[string]any)
			if got := anyStringValue(errObj["code"]); got != "invalid_request" {
				t.Fatalf("expected invalid_request, got %#v", payload)
			}
			if got := anyStringValue(errObj["message"]); !strings.Contains(got, "--from-stdin") {
				t.Fatalf("expected --from-stdin guidance, got %#v", payload)
			}
		})
	}
}

func TestSecretCreateAndUpdateFromStdin(t *testing.T) {
	t.Parallel()

	var createBody map[string]any
	var updateBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/secrets":
			if err := json.NewDecoder(r.Body).Decode(&createBody); err != nil {
				t.Fatalf("decode create body: %v", err)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"secret":{"id":"sec_1","name":"OPENAI_API_KEY"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/secrets":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"secrets":[{"id":"sec_1","name":"OPENAI_API_KEY"}]}`))
		case r.Method == http.MethodPut && r.URL.Path == "/secrets/sec_1":
			if err := json.NewDecoder(r.Body).Decode(&updateBody); err != nil {
				t.Fatalf("decode update body: %v", err)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"secret":{"id":"sec_1","name":"OPENAI_API_KEY"}}`))
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	home := t.TempDir()
	writeAgentProfile(t, home, "agent-a", `{"agent":"agent-a","actor_id":"actor_a","base_url":"`+server.URL+`","access_token":"token","access_token_expires_at":"2099-01-01T00:00:00Z"}`)

	raw := runCLIForTest(t, home, nil, strings.NewReader("create-secret\n"), []string{
		"--json", "secret", "create", "--from-stdin", "--description", "API key", "OPENAI_API_KEY",
	})
	assertEnvelopeOK(t, raw)
	if got := anyStringValue(createBody["value"]); got != "create-secret" {
		t.Fatalf("expected create value from stdin, got %#v", createBody)
	}
	if got := anyStringValue(createBody["description"]); got != "API key" {
		t.Fatalf("expected create description, got %#v", createBody)
	}

	raw = runCLIForTest(t, home, nil, strings.NewReader("update-secret\r\n"), []string{
		"--json", "secret", "update", "--from-stdin", "--description", "updated", "OPENAI_API_KEY",
	})
	assertEnvelopeOK(t, raw)
	if got := anyStringValue(updateBody["value"]); got != "update-secret" {
		t.Fatalf("expected update value from stdin, got %#v", updateBody)
	}
	if got := anyStringValue(updateBody["description"]); got != "updated" {
		t.Fatalf("expected update description, got %#v", updateBody)
	}
}
