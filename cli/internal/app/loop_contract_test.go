package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"agent-nexus-cli/internal/config"
)

func TestCreateCommandsPrintRefFirst(t *testing.T) {
	rec := &noteRecorder{}
	server := noteServer(t, rec)
	defer server.Close()
	a, out := dailyTestApp(t, server.URL)
	if exit := a.Run([]string{"--as", "worker", "note", "card:x", "Progress", "--next", "1d", "checkpoint"}); exit != 0 {
		t.Fatalf("exit=%d %s", exit, out.String())
	}
	if first := strings.SplitN(out.String(), "\n", 2)[0]; first != "event:evt-9" {
		t.Fatalf("first line=%q\n%s", first, out.String())
	}
	if ref := createdRef("work create", map[string]any{"work": map[string]any{"ref": "card:new"}}); ref != "card:new" {
		t.Fatalf("work create ref=%q", ref)
	}
	if ref := createdRef("ask", map[string]any{"ask_id": "event:a1"}); ref != "event:a1" {
		t.Fatalf("ask ref=%q", ref)
	}
	if ref := createdRef("ask", map[string]any{"dry_run": true, "ask_id": "event:a1"}); ref != "" {
		t.Fatalf("dry runs create nothing: %q", ref)
	}
}

func errorEnvelope(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("json: %v: %s", err, raw)
	}
	return asMap(doc["error"])
}

func TestErrorPhasePreflight(t *testing.T) {
	raw := runCLIForTest(t, t.TempDir(), nil, nil, []string{"--json", "note", "card:x", "text", "--next", "soon"})
	e := errorEnvelope(t, []byte(raw))
	if e["phase"] != "preflight" || e["write_sent"] != false {
		t.Fatalf("error=%#v", e)
	}
}

func TestErrorPhaseWriteAndPostflight(t *testing.T) {
	for _, tc := range []struct {
		name, phase string
		postStatus  int
		presence    int
	}{
		{"write", "write", http.StatusConflict, http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch {
				case strings.HasPrefix(r.URL.Path, "/cards/"):
					fmt.Fprint(w, `{"card":{"id":"card-1","ref":"card:x","thread_id":"thread-1"}}`)
				case r.URL.Path == "/events" && r.Method == http.MethodPost:
					w.WriteHeader(tc.postStatus)
					fmt.Fprint(w, `{"error":{"code":"conflict","message":"nope"}}`)
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			a, out := dailyTestApp(t, server.URL)
			if exit := a.Run([]string{"--json", "--as", "worker", "note", "card:x", "text", "--next", "1d"}); exit == 0 {
				t.Fatalf("expected failure: %s", out.String())
			}
			e := errorEnvelope(t, out.Bytes())
			if e["phase"] != tc.phase || e["write_sent"] != true {
				t.Fatalf("error=%#v", e)
			}
		})
	}
	// Postflight: the write succeeded and a later read failed.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/work" && r.Method == http.MethodPost:
			fmt.Fprint(w, `{"work":{"ref":"card:new"}}`)
		case r.URL.Path == "/fail":
			w.WriteHeader(http.StatusInternalServerError)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	a, _ := dailyTestApp(t, server.URL)
	cfg := config.Resolved{BaseURL: server.URL, AccessToken: "test-token", AccessTokenExpiresAt: "2099-01-01T00:00:00Z", Timeout: 5 * time.Second, Sources: map[string]string{}}
	if _, err := a.invokeRawJSON(context.Background(), cfg, "work create", "POST", "/work", map[string]any{"title": "x"}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.invokeRawJSON(context.Background(), cfg, "work get", "GET", "/fail", nil); err == nil {
		t.Fatal("expected read failure")
	}
	if phase, sent := a.writes.phase(); phase != "postflight" || !sent {
		t.Fatalf("phase=%s sent=%v", phase, sent)
	}
}

func TestAPIOperationPassthrough(t *testing.T) {
	var gotMethod, gotPath, gotQuery string
	var gotBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		gotMethod, gotPath, gotQuery = r.Method, r.URL.Path, r.URL.RawQuery
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		fmt.Fprint(w, `{"ok":true}`)
	}))
	defer server.Close()
	a, out := dailyTestApp(t, server.URL)
	if exit := a.Run([]string{"--json", "--as", "worker", "api", "workGet", "--param", "card_ref=card:launch", "--param", "limit=5"}); exit != 0 {
		t.Fatalf("exit=%d %s", exit, out.String())
	}
	if gotMethod != http.MethodGet || gotPath != "/work/card:launch" || gotQuery != "limit=5" {
		t.Fatalf("request=%s %s?%s", gotMethod, gotPath, gotQuery)
	}
	out.Reset()
	if exit := a.Run([]string{"--json", "--as", "worker", "api", "work.create", "--json-body", `{"title":"Launch"}`}); exit != 0 {
		t.Fatalf("exit=%d %s", exit, out.String())
	}
	if gotMethod != http.MethodPost || gotPath != "/work" || gotBody["title"] != "Launch" {
		t.Fatalf("request=%s %s body=%#v", gotMethod, gotPath, gotBody)
	}
	for _, bad := range [][]string{
		{"api", "noSuchOperation"},
		{"api", "workGet"},
		{"api", "workGet", "--param", "nokey"},
	} {
		raw := runCLIForTest(t, t.TempDir(), nil, nil, append([]string{"--json", "--base-url", server.URL}, bad...))
		if e := errorEnvelope(t, []byte(raw)); anyString(e["code"]) == "" {
			t.Fatalf("%v must fail: %s", bad, raw)
		}
	}
}

// Every warning id the CLI emits is listed in cli/docs/warnings.md with a
// trigger phrase that is an exact substring of its message.
func TestWarningsDocListsEveryWarning(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "docs", "warnings.md"))
	if err != nil {
		t.Fatal(err)
	}
	row := regexp.MustCompile("^\\|\\s*`?([a-z_]+)`?\\s*\\|\\s*([^|]+?)\\s*\\|\\s*`([^`]+)`\\s*\\|")
	triggers := map[string]string{}
	for _, line := range strings.Split(string(raw), "\n") {
		if m := row.FindStringSubmatch(line); m != nil {
			triggers[m[1]] = m[3]
		}
	}
	messages := map[string]string{
		"note_no_next":                "no --next; a card renews by noting its next promise",
		"note_beyond_horizon":         "--next 5d is beyond the 2-day horizon; promise a nearer checkpoint",
		"note_moved_without_evidence": "promise moved without evidence; only an --evidence ref resets the clock",
		"ask_no_deadline":             "no --by or --blocks; this sorts below every decision with a deadline or blocked work",
		"ask_overtaken_subject":       "merged since the subject last moved",
		"ask_auto_created_card":       "No --subject-ref: created card:x on board:tasks",
	}
	for _, w := range workCreateWarningsFor("Fix flaky BLE test", func() []map[string]any {
		rows := []map[string]any{{"ref": "card:ble", "title": "Fix flaky BLE tests"}}
		for i := 0; i < 16; i++ {
			rows = append(rows, map[string]any{"ref": fmt.Sprintf("card:%d", i), "title": fmt.Sprintf("zz %d", i)})
		}
		return rows
	}()) {
		messages[w.Code] = w.Message
	}
	for id, msg := range messages {
		trigger, ok := triggers[id]
		if !ok {
			t.Fatalf("warnings.md is missing %s", id)
		}
		if !strings.Contains(msg, trigger) {
			t.Fatalf("%s: trigger %q is not in message %q", id, trigger, msg)
		}
	}
	if len(triggers) != len(messages) {
		t.Fatalf("warnings.md ids %v != emitted ids %v", triggers, messages)
	}
}
