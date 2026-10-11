package server

import (
	"net/http"
	"strings"
)

// Keep this allowlist exact: adding a PM route must not silently grant it the
// runner's ability to load private turn envelopes before lease validation.
func pmRunnerRequest(r *http.Request) bool {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/pm/turns/"), "/")
	if !strings.HasPrefix(r.URL.Path, "/pm/turns/") {
		return false
	}
	if r.Method == http.MethodGet {
		return len(parts) == 1 && parts[0] != "" && r.Header.Get("X-ANX-PM-Lease-Token") != ""
	}
	if r.Method != http.MethodPost {
		return false
	}
	if len(parts) == 1 {
		return parts[0] == "claim"
	}
	if len(parts) != 2 || parts[0] == "" {
		return false
	}
	switch parts[1] {
	case "context", "decisions", "heartbeat", "release", "complete", "fail":
		return true
	}
	return false
}
