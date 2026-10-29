package app

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestHostExclusionSessionGateNamesBrowserAction(t *testing.T) {
	for _, verb := range []string{"exclude", "include"} {
		t.Run(verb, func(t *testing.T) {
			var edits atomic.Int64
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.Method == "GET" && r.URL.Path == "/hosts/host-test":
					fmt.Fprint(w, `{"host":{"id":"host-test","excluded_names":[]}}`)
				case r.Method == "PATCH" && r.URL.Path == "/hosts/host-test":
					edits.Add(1)
					if r.Header.Get("Authorization") != "" || r.Header.Get("X-ANX-Host-Signature") == "" {
						t.Error("expected a host proof without a bearer")
					}
					w.WriteHeader(http.StatusUnauthorized)
					fmt.Fprint(w, `{"error":{"code":"auth_required","message":"authorization token is required"}}`)
				default:
					t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			out := runCLIForTest(t, t.TempDir(), map[string]string{"ANX_BASE_URL": server.URL}, nil, []string{"--json", "--as", "generic", "host", verb, "codex"})
			payload := assertEnvelopeError(t, out)
			details := asMap(asMap(payload["error"])["details"])
			hint := fmt.Sprint(details["hint"])
			if edits.Load() != 1 || !strings.Contains(hint, "signed-in browser session") || !strings.Contains(hint, "Access > Hosts") || strings.Contains(hint, "auth whoami") {
				t.Fatalf("wrong recovery: edits=%d %s", edits.Load(), out)
			}
		})
	}
}
