package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAskAutoCreatedCardSuggestsInitiatives(t *testing.T) {
	var workGets int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/agents/agent-1":
			fmt.Fprint(w, `{"agent":{"id":"agent-1","actor_id":"actor-1","handle":"worker.host"},"open_asks":[]}`)
		case r.URL.Path == "/work" && r.Method == http.MethodPost:
			fmt.Fprint(w, `{"work":{"ref":"card:new-subject","board_ref":"board:tasks","title":"Ship it?"}}`)
		case r.URL.Path == "/work" && r.Method == http.MethodGet:
			workGets++
			if r.URL.Query().Get("limit") != "50" {
				t.Errorf("suggestion page limit=%q", r.URL.RawQuery)
			}
			// Unsorted on purpose: the CLI keeps the three newest open cards.
			fmt.Fprint(w, `{"work":[
				{"ref":"card:fourth","title":"Fourth","phase":"ready","updated_at":"2026-10-14T00:00:00Z"},
				{"ref":"card:done","title":"Closed","phase":"done","updated_at":"2026-10-15T09:00:00Z"},
				{"ref":"card:new-subject","title":"Ship it?","phase":"ready","updated_at":"2026-10-15T08:00:00Z"},
				{"ref":"card:third","title":"Third","phase":"blocked","updated_at":"2026-10-15T01:00:00Z"},
				{"ref":"card:newest","title":"Newest initiative","phase":"ready","updated_at":"2026-10-15T03:00:00Z"},
				{"ref":"card:second","title":"Second","phase":"in_progress","updated_at":"2026-10-15T02:00:00Z"}
			]}`)
		case strings.HasPrefix(r.URL.Path, "/cards/"):
			fmt.Fprint(w, `{"card":{"id":"card-2","ref":"card:new-subject","thread_id":"thread-2"}}`)
		case r.URL.Path == "/events" && r.Method == http.MethodPost:
			fmt.Fprint(w, `{"event":{"id":"ask-1","type":"human_attention_requested"}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	a, out := dailyTestApp(t, server.URL)
	args := []string{"--as", "worker", "ask", "Ship it?", "--body", "Context", "--recommend", "Yes", "--by", "1d"}
	if exit := a.Run(append([]string{"--json"}, args...)); exit != 0 {
		t.Fatalf("exit=%d %s", exit, out.String())
	}
	doc := dailyJSON(t, out)
	msg := warningCodes(doc)["ask_auto_created_card"]
	for _, want := range []string{
		"No --subject-ref: created card:new-subject on board:tasks",
		"Asks belong on an initiative",
		"pass --subject-ref card:<slug>",
		"card:newest (Newest initiative)",
		"card:second (Second)",
		"card:third (Third)",
	} {
		if !strings.Contains(msg, want) {
			t.Fatalf("missing %q in %q", want, msg)
		}
	}
	if strings.Contains(msg, "card:fourth") || strings.Contains(msg, "card:done") || strings.Contains(msg, "card:new-subject (") {
		t.Fatalf("suggestions must be the 3 newest other open initiatives: %q", msg)
	}
	if idx, later := strings.Index(msg, "card:newest"), strings.Index(msg, "card:second"); idx < 0 || later < idx || strings.Index(msg, "card:third") < later {
		t.Fatalf("suggestions out of order: %q", msg)
	}
	stderr := a.Stderr.(*bytes.Buffer).String()
	if !strings.Contains(stderr, "warning: "+msg) {
		t.Fatalf("stderr=%q", stderr)
	}
	if workGets != 1 {
		t.Fatalf("suggestion reads=%d", workGets)
	}
	result := asMap(doc["result"])
	if result["ref"] != "event:ask-1" || result["ask_id"] != "event:ask-1" {
		t.Fatalf("result=%#v", result)
	}

	out.Reset()
	a.Stderr.(*bytes.Buffer).Reset()
	if exit := a.Run(args); exit != 0 {
		t.Fatalf("text exit=%d %s", exit, out.String())
	}
	first, rest, _ := strings.Cut(out.String(), "\n")
	if first != "event:ask-1" {
		t.Fatalf("first line=%q\n%s", first, out.String())
	}
	if !strings.Contains(rest, "ok command=") {
		t.Fatalf("human text missing: %s", rest)
	}
	if !strings.Contains(a.Stderr.(*bytes.Buffer).String(), "No --subject-ref: created card:new-subject on board:tasks") {
		t.Fatalf("text stderr=%q", a.Stderr.(*bytes.Buffer).String())
	}
}

func TestNamedCreateCommandsPrintRefFirst(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/agents/agent-1" || r.URL.Path == "/agents/me":
			fmt.Fprint(w, `{"agent":{"id":"agent-1","actor_id":"actor-1","handle":"worker.host","current_card_ref":"card:labels"}}`)
		case r.URL.Path == "/work" && r.Method == http.MethodGet:
			fmt.Fprint(w, `{"work":[]}`)
		case r.URL.Path == "/work" && r.Method == http.MethodPost:
			fmt.Fprint(w, `{"work":{"ref":"card:foo","title":"Launch","phase":"ready"}}`)
		case r.URL.Path == "/cards" && r.Method == http.MethodPost:
			fmt.Fprint(w, `{"card":{"id":"card-1","ref":"card:foo","title":"Implement","column_key":"backlog"},"board":{"id":"board_launch","ref":"board:launch"}}`)
		case r.URL.Path == "/docs" && r.Method == http.MethodPost:
			fmt.Fprint(w, `{"document":{"id":"doc-1","ref":"document:runbook","title":"Runbook"}}`)
		case r.URL.Path == "/topics" && r.Method == http.MethodPost:
			fmt.Fprint(w, `{"topic":{"id":"topic-1","ref":"topic:launch","title":"Launch"}}`)
		case r.URL.Path == "/boards" && r.Method == http.MethodPost:
			fmt.Fprint(w, `{"board":{"id":"board-1","ref":"board:launch","title":"Launch"}}`)
		case strings.HasPrefix(r.URL.Path, "/cards/") && strings.HasSuffix(r.URL.Path, "/plan") && r.Method == http.MethodGet:
			fmt.Fprint(w, `{"card_ref":"card:foo","if_updated_at":"2026-10-05T00:00:00Z","plan":{"steps":[]}}`)
		case strings.HasPrefix(r.URL.Path, "/cards/") && strings.HasSuffix(r.URL.Path, "/plan") && r.Method == http.MethodPut:
			fmt.Fprint(w, `{"card_ref":"card:foo","if_updated_at":"2026-10-05T01:00:00Z","plan":{"steps":[{"id":"build","title":"Build","after":[]}]},"plan_state":{"steps":[],"progress":{"done":0,"total":1}}}`)
		case strings.HasPrefix(r.URL.Path, "/cards/"):
			fmt.Fprint(w, `{"card":{"id":"card-1","ref":"card:x","thread_id":"thread-1","title":"X","column_key":"ready"}}`)
		case r.URL.Path == "/events" && r.Method == http.MethodPost:
			var doc map[string]any
			_ = json.NewDecoder(r.Body).Decode(&doc)
			id := anyString(asMap(doc["event"])["id"])
			if id == "" {
				id = "evt-9"
			}
			fmt.Fprintf(w, `{"event":{"id":%q,"type":"message_posted"}}`, id)
		case r.URL.Path == "/events" && r.Method == http.MethodGet:
			fmt.Fprint(w, `{"events":[]}`)
		case r.URL.Path == "/agents/me/presence":
			fmt.Fprint(w, `{}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	workFile := filepath.Join(t.TempDir(), "work.json")
	if err := os.WriteFile(workFile, []byte(`{"title":"Launch"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		args []string
		ref  string
	}{
		{"work create", []string{"work", "create", "--from-file", workFile}, "card:foo"},
		{"cards create", []string{"cards", "create", "--board", "board_launch", "--title", "Implement", "--body", "hello"}, "card:foo"},
		{"plan step add", []string{"plan", "step", "add", "card:foo", "--step-id", "build", "--title", "Build"}, "card:foo#build"},
		{"ask", []string{"ask", "Ship it?", "--body", "Context", "--recommend", "Yes", "--subject-ref", "card:labels", "--thread-id", "thr1", "--by", "1d"}, ""},
		{"note", []string{"note", "card:x", "Progress", "--next", "1d", "checkpoint"}, "event:evt-9"},
		{"docs create", []string{"docs", "create", "--title", "Runbook", "--body", "hello"}, "document:runbook"},
		{"topics create", []string{"topics", "create", "--title", "Launch", "--summary", "Coordinate"}, "topic:launch"},
		{"boards create", []string{"boards", "create", "--title", "Launch"}, "board:launch"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a, out := dailyTestApp(t, server.URL)
			if exit := a.Run(append([]string{"--json", "--as", "worker"}, tc.args...)); exit != 0 {
				t.Fatalf("json exit=%d %s", exit, out.String())
			}
			doc := dailyJSON(t, out)
			result := asMap(doc["result"])
			ref := anyString(result["ref"])
			if tc.ref != "" && ref != tc.ref {
				t.Fatalf("result.ref=%q result=%#v", ref, result)
			}
			if ref == "" || !strings.Contains(ref, ":") && !strings.Contains(ref, "#") {
				t.Fatalf("missing result.ref: %#v", result)
			}
			if tc.name == "ask" && ref != anyString(result["ask_id"]) {
				t.Fatalf("ask ref=%q ask_id=%q", ref, result["ask_id"])
			}
			if tc.name == "note" && ref != anyString(result["note_ref"]) {
				t.Fatalf("note ref=%q note_ref=%q", ref, result["note_ref"])
			}

			out.Reset()
			if exit := a.Run(append([]string{"--as", "worker"}, tc.args...)); exit != 0 {
				t.Fatalf("text exit=%d %s", exit, out.String())
			}
			first, rest, _ := strings.Cut(out.String(), "\n")
			want := ref
			if tc.name == "ask" {
				want = first
				if !strings.HasPrefix(first, "event:") || !strings.Contains(rest, "result.ask_id="+first) {
					t.Fatalf("ask text ref=%q\n%s", first, out.String())
				}
			}
			if first != want {
				t.Fatalf("first line=%q want %q\n%s", first, want, out.String())
			}
			if strings.HasPrefix(first, "created ") {
				t.Fatalf("ref line still has a created prefix: %q", first)
			}
			if !strings.Contains(rest, "ok command=") {
				t.Fatalf("existing human text missing:\n%s", out.String())
			}
		})
	}
}

func TestCreatedRefStampsMissingResultRef(t *testing.T) {
	cases := []struct {
		command string
		in      map[string]any
		want    string
	}{
		{"artifacts create", map[string]any{"artifact": map[string]any{"id": "artifact_1", "ref": "artifact:brief"}}, "artifact:brief"},
		{"events create", map[string]any{"event": map[string]any{"id": "evt-9"}}, "event:evt-9"},
		{"boards cards create-batch", map[string]any{"cards": []any{map[string]any{"ref": "card:a"}, map[string]any{"card": map[string]any{"ref": "card:b"}}}}, "card:a card:b"},
		{"ask batch", map[string]any{"asks": []any{map[string]any{"ask_id": "event:a"}, map[string]any{"ask_id": "event:b"}}}, "event:a event:b"},
		{"ask", map[string]any{"dry_run": true, "ask_id": "event:a1"}, ""},
		{"work get", map[string]any{"work": map[string]any{"ref": "card:old"}}, ""},
	}
	for _, tc := range cases {
		got := ensureCreatedRef(tc.command, tc.in)
		if got != tc.want {
			t.Fatalf("%s ref=%q want %q", tc.command, got, tc.want)
		}
		if tc.want != "" && anyString(tc.in["ref"]) != tc.want {
			t.Fatalf("%s did not stamp result.ref: %#v", tc.command, tc.in)
		}
		if tc.want == "" && anyString(tc.in["ref"]) != "" {
			t.Fatalf("%s stamped a ref it should not: %#v", tc.command, tc.in)
		}
	}
	already := map[string]any{"ref": "card:kept", "work": map[string]any{"ref": "card:new"}}
	if got := ensureCreatedRef("work create", already); got != "card:new" || already["ref"] != "card:kept" {
		t.Fatalf("existing result.ref must stay put: got %q map=%#v", got, already)
	}
}

func TestPlanRefRejectsBareTrackerAndPRNumber(t *testing.T) {
	hits := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		http.NotFound(w, r)
	}))
	defer server.Close()
	for _, ref := range []string{"SCA-123", "LINEAR-9", "#123", "#7"} {
		for _, args := range [][]string{
			{"plan", "step", "add", "card:foo", "--title", "Build", "--ref", ref},
			{"plan", "step", "update", "card:foo", "build", "--ref", ref},
		} {
			raw := runCLIForTestJSONError(t, t.TempDir(), map[string]string{"ANX_BASE_URL": server.URL, "ANX_ACCESS_TOKEN": "token"}, append([]string{"--json"}, args...))
			payload := assertEnvelopeError(t, raw)
			errObj := asMap(payload["error"])
			if errObj["code"] != "invalid_request" || errObj["exit_code"] != float64(2) {
				t.Fatalf("%v: %#v", args, errObj)
			}
			msg := anyString(errObj["message"])
			if !strings.Contains(msg, ref) || !strings.Contains(msg, "full URL") || !strings.Contains(msg, "Linear") || !strings.Contains(msg, "GitHub") {
				t.Fatalf("%v message=%q", args, msg)
			}
		}
	}
	if hits != 0 {
		t.Fatalf("bare id reached the server %d times", hits)
	}
	for _, ref := range []string{"card:other", "https://github.com/org/repo/pull/12", "https://linear.app/acme/issue/SCA-123"} {
		if _, err := parsePlanCommand([]string{"plan", "step", "add", "card:foo", "--title", "Build", "--ref", ref}); err != nil {
			t.Fatalf("%s: %v", ref, err)
		}
	}
	help := runCLIForTest(t, t.TempDir(), nil, nil, []string{"help", "plan", "step", "add"})
	for _, want := range []string{"SCA-123", "#123", "full URL", "Linear", "GitHub"} {
		if !strings.Contains(help, want) {
			t.Fatalf("plan step add help missing %q:\n%s", want, help)
		}
	}
	askHelp := runCLIForTest(t, t.TempDir(), nil, nil, []string{"help", "ask"})
	if !strings.Contains(askHelp, "--subject-ref card:<slug>") {
		t.Fatalf("ask help should recommend --subject-ref:\n%s", askHelp)
	}
	guide := agentGuideText()
	if !strings.Contains(guide, "Pass `--subject-ref card:<slug>`") || !strings.Contains(guide, "not a bare id like SCA-123 or #123") {
		t.Fatalf("agent guide missing subject and ref guidance")
	}
}
