package httpclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"agent-nexus-cli/internal/config"
)

func TestRawCall(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/test" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("X-ANX-CLI-Version") == "" {
			t.Fatalf("expected X-ANX-CLI-Version header")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	client, err := New(config.Resolved{BaseURL: server.URL, Timeout: 2 * time.Second, Agent: "agent-1"})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	resp, err := client.RawCall(context.Background(), RawRequest{Method: http.MethodGet, Path: "/v1/test"})
	if err != nil {
		t.Fatalf("raw call: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected status code: %d", resp.StatusCode)
	}
	if string(resp.Body) != `{"ok":true}` {
		t.Fatalf("unexpected body: %q", string(resp.Body))
	}
}

// TestResolveURL_preservesHostedWorkspacePrefix locks in URL resolution when the CLI base URL
// includes a hosted workspace prefix (/ws/{org}/{workspace}). Request paths must join under
// that prefix (not replace it); query strings from the request path must be preserved.
func TestResolveURL_preservesHostedWorkspacePrefix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		reqPath   string
		wantPath  string
		wantQuery string
	}{
		{
			name:     "readyz_leading_slash",
			reqPath:  "/readyz",
			wantPath: "/ws/acme/demo/readyz",
		},
		{
			name:     "api_relative_no_leading_slash",
			reqPath:  "api/v1/threads",
			wantPath: "/ws/acme/demo/api/v1/threads",
		},
		{
			name:      "api_leading_slash_with_query",
			reqPath:   "/api/v1/threads?limit=1",
			wantPath:  "/ws/acme/demo/api/v1/threads",
			wantQuery: "limit=1",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var gotPath, gotRawQuery string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotPath = r.URL.Path
				gotRawQuery = r.URL.RawQuery
				w.WriteHeader(http.StatusOK)
			}))
			t.Cleanup(server.Close)

			base := strings.TrimRight(server.URL, "/") + "/ws/acme/demo"
			client, err := New(config.Resolved{BaseURL: base, Timeout: 2 * time.Second})
			if err != nil {
				t.Fatalf("new client: %v", err)
			}

			_, err = client.RawCall(context.Background(), RawRequest{Method: http.MethodGet, Path: tc.reqPath})
			if err != nil {
				t.Fatalf("raw call: %v", err)
			}
			if gotPath != tc.wantPath {
				t.Fatalf("request path: got %q want %q", gotPath, tc.wantPath)
			}
			if tc.wantQuery != "" && gotRawQuery != tc.wantQuery {
				t.Fatalf("raw query: got %q want %q", gotRawQuery, tc.wantQuery)
			}
		})
	}
}
