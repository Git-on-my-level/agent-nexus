package app

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAskTitleKeysMatchCore(t *testing.T) {
	if askTitleKey("  Restore the  loop​-audit card,\twhich keeps failing?! ") != "restore the loop audit card which keeps failing" {
		t.Fatal(askTitleKey("Restore the loop-audit card, which keeps failing?"))
	}
	if askTitlePattern("Daily digest 2026-11-12: change priorities?") != askTitlePattern("Daily digest 2026-11-13: change priorities?") {
		t.Fatal("dated copies must share one pattern")
	}
	if askTitlePattern("Daily digest 2026-11-12") != "daily digest # # #" {
		t.Fatal(askTitlePattern("Daily digest 2026-11-12"))
	}
}

// An ask core does not file is reported as what happened, with the answer or
// the open ask, and stakes and --again travel in the payload.
func TestAskReportsCoreRedirects(t *testing.T) {
	for name, tc := range map[string]struct {
		response string
		want     []string
	}{
		"repeat":            {`{"deduplicated":true,"event":{"id":"open-1","payload":{"title":"Rerun?"}}}`, []string{"event:open-1", "ask_already_open"}},
		"ruling":            {`{"answered_by_ruling":true,"ruling":{"ref":"ruling:r1","kind":"answer","answer":"Yes, rerun freely"}}`, []string{"Yes, rerun freely", "ask_covered_by_ruling", "ruling:r1"}},
		"not a decision":    {`{"redirected":"tell","ruling":{"ref":"ruling:r2","kind":"not_a_decision"},"event":{"id":"fyi-1"}}`, []string{"ask_not_a_decision", "anx tell"}},
		"filed with intent": {`{"event":{"id":"new-1","thread_id":"thread-1"}}`, []string{"event:new-1"}},
	} {
		t.Run(name, func(t *testing.T) {
			var payload map[string]any
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch {
				case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/cards/"):
					fmt.Fprint(w, `{"card":{"id":"c1","ref":"card:task","thread_id":"thread-1"}}`)
				case r.Method == http.MethodGet && r.URL.Path == "/rulings":
					fmt.Fprint(w, `{"rulings":[{"ref":"ruling:r9","kind":"answer","question":"Rerun the export diagnostic twice?","answer":"Once is enough","active":true}]}`)
				case r.Method == http.MethodPost && r.URL.Path == "/events":
					var input map[string]any
					_ = json.NewDecoder(r.Body).Decode(&input)
					payload = asMap(asMap(input["event"])["payload"])
					fmt.Fprint(w, tc.response)
				default:
					fmt.Fprint(w, `{"events":[],"your_asks":[],"decisions":[]}`)
				}
			}))
			defer server.Close()
			home := t.TempDir()
			writeDerivedAgentFixture(t, home, "agent-a", `{"agent":"agent-a","username":"agent.alpha","actor_id":"actor_asker","access_token":"token-a","access_token_expires_at":"2099-01-01T00:00:00Z"}`)
			raw := runCLIForTest(t, home, map[string]string{}, nil, []string{
				"--json", "--base-url", server.URL, "--as", "agent-a",
				"ask", "Rerun the export diagnostic?", "--subject-ref", "card:task", "--again", "--irreversible", "--cost", "$40 of compute",
				"--situation", "The diagnostic reads counts only and changes nothing in the export.", "--impact", "The serializer work waits on each rerun until someone approves it.", "--why", "It is read-only and cheap.", "--recommend", "Yes, rerun it",
			})
			assertEnvelopeOK(t, raw)
			for _, want := range tc.want {
				if !strings.Contains(raw, want) {
					t.Errorf("output lacks %q: %s", want, raw)
				}
			}
			stakes := asMap(payload["stakes"])
			if payload["again"] != true || stakes["irreversible"] != true || stakes["cost"] != "$40 of compute" {
				t.Errorf("payload intent=%v stakes=%v", payload["again"], stakes)
			}
		})
	}
}

func TestTellPostsAnFYIAndRulingsListReads(t *testing.T) {
	var payload map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/cards/"):
			fmt.Fprint(w, `{"card":{"id":"c1","ref":"card:support","thread_id":"thread-1"}}`)
		case r.Method == http.MethodGet && r.URL.Path == "/rulings":
			fmt.Fprint(w, `{"rulings":[{"ref":"ruling:r1","kind":"answer","question":"Rerun?","answer":"Yes","uses":3,"active":true},{"ref":"ruling:r2","kind":"not_a_decision","question":"Digest 1","requester":"guard","uses":1,"active":true}]}`)
		case r.Method == http.MethodPost && r.URL.Path == "/events":
			var input map[string]any
			_ = json.NewDecoder(r.Body).Decode(&input)
			payload = asMap(asMap(input["event"])["payload"])
			fmt.Fprint(w, `{"event":{"id":"fyi-1","thread_id":"thread-1"}}`)
		default:
			fmt.Fprint(w, `{}`)
		}
	}))
	defer server.Close()
	home := t.TempDir()
	writeDerivedAgentFixture(t, home, "agent-a", `{"agent":"agent-a","username":"agent.alpha","actor_id":"actor_asker","access_token":"token-a","access_token_expires_at":"2099-01-01T00:00:00Z"}`)
	raw := runCLIForTest(t, home, map[string]string{}, nil, []string{"--json", "--base-url", server.URL, "--as", "agent-a", "tell", "card:support", "Nightly backlog: 3 of 41 tickets older than a week", "--body", "Same as yesterday."})
	assertEnvelopeOK(t, raw)
	if payload["kind"] != "fyi" || payload["title"] != "Nightly backlog: 3 of 41 tickets older than a week" || !strings.Contains(raw, "event:fyi-1") {
		t.Fatalf("payload=%v output=%s", payload, raw)
	}
	text := runCLIForTest(t, home, map[string]string{}, nil, []string{"--base-url", server.URL, "--as", "agent-a", "rulings", "list"})
	for _, want := range []string{`ruling:r1  "Rerun?" → "Yes" · applies whoever asks · used 3×`, "not a decision when guard asks: use anx tell"} {
		if !strings.Contains(text, want) {
			t.Errorf("rulings list lacks %q: %s", want, text)
		}
	}
	for _, bad := range [][]string{{"tell", "card:support"}, {"tell", "card:support", "two\nlines"}, {"rulings"}} {
		if out := runCLIForTestJSONError(t, home, map[string]string{}, append([]string{"--json", "--base-url", server.URL, "--as", "agent-a"}, bad...)); !strings.Contains(out, "usage:") && !strings.Contains(out, "one line") {
			t.Errorf("%v should be a usage error: %s", bad, out)
		}
	}
}
