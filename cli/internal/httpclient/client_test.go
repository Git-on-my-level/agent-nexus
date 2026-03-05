package httpclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"organization-autorunner-cli/internal/config"
)

func TestRawCall(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/test" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("X-OAR-CLI-Version") == "" {
			t.Fatalf("expected X-OAR-CLI-Version header")
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
