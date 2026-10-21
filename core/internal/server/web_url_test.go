package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func handshakeWebURL(t *testing.T, handler http.Handler) (any, bool) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/meta/handshake", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("unexpected status: got %d body=%s", rr.Code, rr.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to decode body: %v", err)
	}
	value, ok := payload["web_url"]
	return value, ok
}

func TestHandshakeOmitsWebURLWhenUnset(t *testing.T) {
	t.Parallel()

	if value, ok := handshakeWebURL(t, NewHandler("0.2.2")); ok {
		t.Fatalf("expected no web_url without configuration, got %#v", value)
	}
}

func TestHandshakeIncludesConfiguredWebURL(t *testing.T) {
	t.Parallel()

	webURL, err := WorkspaceWebURL("https://example.com/o/acme/w/main/", "")
	if err != nil {
		t.Fatalf("WorkspaceWebURL: %v", err)
	}
	value, ok := handshakeWebURL(t, NewHandler("0.2.2", WithWebURL(webURL)))
	if !ok || value != "https://example.com/o/acme/w/main" {
		t.Fatalf("expected configured web_url without trailing slash, got %#v (present=%v)", value, ok)
	}
}

func TestWorkspaceWebURL(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		webBase   string
		publicURL string
		want      string
		wantErr   bool
	}{
		{name: "unset", want: ""},
		{name: "explicit", webBase: "https://example.com/o/acme/w/main", want: "https://example.com/o/acme/w/main"},
		{name: "explicit trims trailing slash", webBase: " http://127.0.0.1:5173/app/o/local/w/local/ ", want: "http://127.0.0.1:5173/app/o/local/w/local"},
		{name: "explicit any path", webBase: "https://ui.example.com", want: "https://ui.example.com"},
		{name: "falls back to enrollment workspace URL", publicURL: "http://127.0.0.1:5291/o/local/w/local", want: "http://127.0.0.1:5291/o/local/w/local"},
		{name: "explicit wins over fallback", webBase: "https://a.example.com/o/x/w/y", publicURL: "https://b.example.com/o/x/w/y", want: "https://a.example.com/o/x/w/y"},
		{name: "invalid fallback is omitted", publicURL: "not a url", want: ""},
		{name: "relative rejected", webBase: "/o/acme/w/main", wantErr: true},
		{name: "non-http scheme rejected", webBase: "javascript:alert(1)", wantErr: true},
		{name: "ftp rejected", webBase: "ftp://example.com/o/a/w/b", wantErr: true},
		{name: "credentials rejected", webBase: "https://user:pass@example.com/o/a/w/b", wantErr: true},
		{name: "query rejected", webBase: "https://example.com/o/a/w/b?x=1", wantErr: true},
		{name: "fragment rejected", webBase: "https://example.com/o/a/w/b#top", wantErr: true},
		{name: "missing host rejected", webBase: "https:///o/a/w/b", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := WorkspaceWebURL(tc.webBase, tc.publicURL)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}
