package app

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/errnorm"
)

func TestCardMoveRecoveryReadsWithoutReapplying(t *testing.T) {
	for _, tc := range []struct {
		name, phase, code          string
		status                     int
		anchors, hidden, recovered bool
	}{
		{"committed", "blocked", "outcome_unknown", 409, false, false, true},
		{"busy_already_target", "blocked", "busy", 503, false, false, true},
		{"not_applied", "ready", "outcome_unknown", 409, false, false, false},
		{"busy_not_applied", "ready", "busy", 503, false, false, false},
		{"placement_unknown", "blocked", "outcome_unknown", 409, true, false, false},
		{"readback_inaccessible", "blocked", "outcome_unknown", 409, false, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var moves, reads atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch {
				case r.Method == "POST" && r.URL.Path == "/cards/card-1/move":
					moves.Add(1)
					if tc.status == 503 {
						w.Header().Set("Retry-After", "3")
					}
					w.WriteHeader(tc.status)
					fmt.Fprintf(w, `{"error":{"code":%q,"recoverable":%t,"hint":"Read back; do not repeat an applied move."}}`, tc.code, tc.status == 503)
				case r.Method == "GET" && r.URL.Path == "/work/card-1":
					reads.Add(1)
					if tc.hidden {
						w.WriteHeader(404)
						fmt.Fprint(w, `{"error":{"code":"not_found"}}`)
						return
					}
					fmt.Fprintf(w, `{"work":{"ref":"card:card-1","column_key":%q,"version":2}}`, tc.phase)
				default:
					t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
					w.WriteHeader(500)
				}
			}))
			defer server.Close()
			a := newTestApp(t)
			cfg := config.Resolved{BaseURL: server.URL, AccessToken: "fixture", Sources: map[string]string{}}
			body := map[string]any{"column_key": "blocked", "if_board_updated_at": "2026-01-01T00:00:00Z"}
			if tc.anchors {
				body["before_card_id"] = "anchor"
			}
			result, err := a.invokeTypedJSON(context.Background(), cfg, "cards move", "cards.move", map[string]string{"card_id": "card-1"}, nil, body)
			if (err == nil) != tc.recovered || moves.Load() != 1 || reads.Load() != 1 {
				t.Fatalf("result=%v error=%v moves=%d reads=%d", result, err, moves.Load(), reads.Load())
			}
			if tc.recovered {
				if commandResultBody(result)["recovered"] != true {
					t.Fatal("missing recovery provenance")
				}
				if asMap(commandResultBody(result)["card"])["version"] != nil || commandResultBody(result)["work_version"] != float64(2) {
					t.Fatal("work version misrepresented as card revision version")
				}
			} else {
				e := errnorm.Normalize(err)
				errnorm.EnrichForCommand(e, "cards.move")
				if e.Code != tc.code || (tc.code == "outcome_unknown" && (e.Recoverable == nil || *e.Recoverable)) {
					t.Fatalf("lost non-retryable uncertainty: %+v", e)
				}
				if tc.status == 503 && e.Details.(map[string]any)["retry_after"] != "3" {
					t.Fatal("lost Retry-After")
				}
			}
		})
	}
}

func TestCardMoveRecoveryRequiresMatchingResolution(t *testing.T) {
	card := map[string]any{"column_key": "done", "resolution": "completed", "resolution_refs": []string{"event:one"}}
	if cardMoveReadbackMatches(card, map[string]any{"column_key": "done", "resolution": "cancelled"}) || cardMoveReadbackMatches(card, map[string]any{"column_key": "done", "resolution_refs": []string{"event:two"}}) {
		t.Fatal("recovered a different resolution")
	}
}
