package app

import (
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
)

func TestApplyCommandShapeCompatibilityAliasNoLegacyShapes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []string
	}{
		{name: "packets receipts create", args: []string{"packets", "receipts", "create", "--from-file", "payload.json"}},
		{name: "artifacts content get", args: []string{"artifacts", "content", "get", "--artifact-id", "artifact_123"}},
		{name: "topics update", args: []string{"topics", "update", "--topic-id", "topic_123"}},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, ok := applyCommandShapeCompatibilityAlias(tt.args)
			if ok {
				t.Fatalf("expected no alias rewrite, got ok=true args=%#v", got)
			}
			if !reflect.DeepEqual(got, tt.args) {
				t.Fatalf("expected args unchanged, got %#v want %#v", got, tt.args)
			}
		})
	}
}

func TestCanonicalCommandPathsHitRegistryHandlers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		args        []string
		stdin       string
		wantMethod  string
		wantPath    string
		wantCommand string
	}{
		{
			name:        "receipts create",
			args:        []string{"receipts", "create"},
			stdin:       `{"receipt":{"thread_id":"thread_1"}}`,
			wantMethod:  http.MethodPost,
			wantPath:    "/packets/receipts",
			wantCommand: "receipts create",
		},
		{
			name:        "reviews create",
			args:        []string{"reviews", "create"},
			stdin:       `{"review":{"thread_id":"thread_1"}}`,
			wantMethod:  http.MethodPost,
			wantPath:    "/packets/reviews",
			wantCommand: "reviews create",
		},
		{
			name:        "artifacts content with id flag",
			args:        []string{"artifacts", "content", "--artifact-id", "artifact_1"},
			wantMethod:  http.MethodGet,
			wantPath:    "/artifacts/artifact_1/content",
			wantCommand: "artifacts content",
		},
		{
			name:        "topics patch",
			args:        []string{"topics", "patch", "--topic-id", "topic_1"},
			stdin:       `{"patch":{"status":"resolved"}}`,
			wantMethod:  http.MethodPatch,
			wantPath:    "/topics/topic_1",
			wantCommand: "topics patch",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var hitCanonical atomic.Bool
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != tt.wantMethod || r.URL.Path != tt.wantPath {
					http.NotFound(w, r)
					return
				}
				hitCanonical.Store(true)
				switch tt.wantCommand {
				case "artifacts content":
					w.Header().Set("Content-Type", "text/plain")
					_, _ = w.Write([]byte("artifact body"))
				default:
					w.Header().Set("Content-Type", "application/json")
					if r.Method == http.MethodPost {
						w.WriteHeader(http.StatusCreated)
					}
					_, _ = w.Write([]byte(`{"ok":true}`))
				}
			}))
			defer server.Close()

			var stdin io.Reader
			if tt.stdin != "" {
				stdin = strings.NewReader(tt.stdin)
			}
			raw := runCLIForTest(t, t.TempDir(), map[string]string{}, stdin, append([]string{"--json", "--base-url", server.URL}, tt.args...))
			payload := assertEnvelopeOK(t, raw)
			if got := anyStringValue(payload["command"]); got != tt.wantCommand {
				t.Fatalf("expected canonical command %q, got %q payload=%#v", tt.wantCommand, got, payload)
			}
			if !hitCanonical.Load() {
				t.Fatalf("expected canonical handler request %s %s", tt.wantMethod, tt.wantPath)
			}
		})
	}
}

func TestApplyCommandShapeCompatibilityAliasPreservesArtifactsContentPositionalGetID(t *testing.T) {
	t.Parallel()

	args := []string{"artifacts", "content", "get"}
	got, ok := applyCommandShapeCompatibilityAlias(args)
	if ok {
		t.Fatalf("expected no alias match for positional artifact id `get`, got rewritten args %#v", got)
	}
	if !reflect.DeepEqual(got, args) {
		t.Fatalf("expected args to remain unchanged, got %#v", got)
	}
}

func TestCommandShapeCompatibilityAliasNegativeNoMatch(t *testing.T) {
	t.Parallel()

	args := []string{"packets", "receipts"}
	got, ok := applyCommandShapeCompatibilityAlias(args)
	if ok {
		t.Fatalf("expected no alias match, got rewritten args %#v", got)
	}
	if !reflect.DeepEqual(got, args) {
		t.Fatalf("expected args to remain unchanged, got %#v", got)
	}

	raw := runCLIForTest(t, t.TempDir(), map[string]string{}, nil, []string{"--json", "packets", "receipts"})
	payload := assertEnvelopeError(t, raw)
	errObj, _ := payload["error"].(map[string]any)
	if anyStringValue(errObj["code"]) != "unknown_command" {
		t.Fatalf("expected unknown_command for non-matching alias shape, payload=%#v", payload)
	}
}
