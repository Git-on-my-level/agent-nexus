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

	"agent-nexus-cli/internal/registry"
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
			if r.URL.Query().Get("limit") != "50" && r.URL.Query().Get("limit") != "200" {
				t.Errorf("suggestion page limit=%q", r.URL.RawQuery)
			}
			if r.URL.Query().Get("phase") != "" {
				t.Errorf("work list has no open-only phase filter: %s", r.URL.RawQuery)
			}
			// Newest-first, including a fractional second that string-sorts backward.
			// .99Z is newer than .9Z, but ".9Z" > ".99Z" lexicographically.
			fmt.Fprint(w, `{"work":[
				{"ref":"card:done","title":"Closed","phase":"done","updated_at":"2026-10-15T12:00:01Z"},
				{"ref":"card:newest","title":"Newest initiative","phase":"ready","updated_at":"2026-10-15T12:00:00.99Z","is_initiative":true},
				{"ref":"card:second","title":"Second","phase":"in_progress","updated_at":"2026-10-15T12:00:00.9Z","is_initiative":true},
				{"ref":"card:new-subject","title":"Ship it?","phase":"ready","updated_at":"2026-10-15T12:00:00.8Z"},
				{"ref":"card:third","title":"Third","phase":"blocked","updated_at":"2026-10-15T12:00:00.7Z","is_initiative":true},
				{"ref":"card:fourth","title":"Fourth","phase":"ready","updated_at":"2026-10-15T12:00:00.6Z","is_initiative":true}
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
	args := []string{"--as", "worker", "ask", "Ship it?", "--body", "Context", "--situation", "A launch decision is waiting.", "--impact", "The team cannot finish until we choose.", "--why", "This is the quickest safe approach.", "--recommend", "Yes", "--by", "1d"}
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
	if workGets != 2 {
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
		{"ask", []string{"ask", "Ship it?", "--body", "Context", "--situation", "A launch decision is waiting.", "--impact", "The team cannot finish until we choose.", "--why", "This is the quickest safe approach.", "--recommend", "Yes", "--subject-ref", "card:labels", "--thread-id", "thr1", "--by", "1d"}, ""},
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
		{"boards cards create-batch", map[string]any{"cards": []any{map[string]any{"ref": "card:a"}, map[string]any{"card": map[string]any{"ref": "card:b"}}}}, "card:a\ncard:b"},
		{"ask batch", map[string]any{"batch_id": "askbatch_1", "asks": []any{map[string]any{"ask_id": "event:a"}, map[string]any{"ask_id": "event:b"}}}, "event:a\nevent:b"},
		{"ask", map[string]any{"dry_run": true, "ask_id": "event:a1"}, ""},
		{"work get", map[string]any{"work": map[string]any{"ref": "card:old"}}, ""},
	}
	for _, tc := range cases {
		got := ensureCreatedRef(tc.command, tc.in)
		if got != tc.want {
			t.Fatalf("%s ref=%q want %q", tc.command, got, tc.want)
		}
		stamped := anyString(tc.in["ref"])
		switch tc.command {
		case "ask batch":
			if stamped != "askbatch_1" || strings.Contains(stamped, " ") {
				t.Fatalf("ask batch result.ref=%q want the batch id", stamped)
			}
		case "boards cards create-batch":
			if stamped != "" {
				t.Fatalf("card batch must leave result.ref unset: %#v", tc.in)
			}
		default:
			if tc.want != "" && stamped != tc.want {
				t.Fatalf("%s did not stamp result.ref: %#v", tc.command, tc.in)
			}
			if tc.want == "" && stamped != "" {
				t.Fatalf("%s stamped a ref it should not: %#v", tc.command, tc.in)
			}
		}
	}
	typedAsks := map[string]any{"batch_id": "askbatch_typed", "asks": []map[string]any{{"ask_id": "event:a"}, {"ask_id": "event:b"}}}
	if got := ensureCreatedRef("ask batch", typedAsks); got != "event:a\nevent:b" || typedAsks["ref"] != "askbatch_typed" {
		t.Fatalf("typed ask slice ref=%q map=%#v", got, typedAsks)
	}
	noBatchID := map[string]any{"asks": []any{map[string]any{"ask_id": "event:a"}, map[string]any{"ask_id": "event:b"}}}
	if got := ensureCreatedRef("ask batch", noBatchID); got != "event:a\nevent:b" || anyString(noBatchID["ref"]) != "" {
		t.Fatalf("ask batch without an id must not invent result.ref: %q %#v", got, noBatchID)
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
	if !strings.Contains(guide, "Pass `--subject-ref card:<slug>`") || !strings.Contains(guide, "Plans never establish parentage") {
		t.Fatalf("agent guide missing subject and ref guidance")
	}
	setHelp := runCLIForTest(t, t.TempDir(), nil, nil, []string{"help", "plan", "set"})
	for _, want := range []string{"--from-file", "SCA-123", "#123", "names the step", "full URL"} {
		if !strings.Contains(setHelp, want) {
			t.Fatalf("plan set help missing %q:\n%s", want, setHelp)
		}
	}
}

func TestAskSuggestionsPagePastClosedWork(t *testing.T) {
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/agents/agent-1":
			fmt.Fprint(w, `{"agent":{"id":"agent-1","actor_id":"actor-1","handle":"worker.host"},"open_asks":[]}`)
		case r.URL.Path == "/work" && r.Method == http.MethodPost:
			fmt.Fprint(w, `{"work":{"ref":"card:new-subject","board_ref":"board:tasks","title":"Ship it?"}}`)
		case r.URL.Path == "/work" && r.Method == http.MethodGet:
			paths = append(paths, r.URL.RawQuery)
			if r.URL.Query().Get("phase") != "" {
				t.Errorf("phase filter cannot mean open work: %s", r.URL.RawQuery)
			}
			if r.URL.Query().Get("cursor") == "" {
				fmt.Fprint(w, `{"work":[{"ref":"card:closed","title":"Closed","phase":"cancelled","updated_at":"2026-10-15T12:00:00.99Z"}],"next_cursor":"page-2"}`)
				return
			}
			if r.URL.Query().Get("cursor") != "page-2" {
				t.Errorf("cursor=%q", r.URL.Query().Get("cursor"))
			}
			fmt.Fprint(w, `{"work":[
				{"ref":"card:newest","title":"Newest","phase":"ready","updated_at":"2026-10-15T12:00:00.99Z","is_initiative":true},
				{"ref":"card:second","title":"Second","phase":"ready","updated_at":"2026-10-15T12:00:00.9Z","is_initiative":true}
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
	if exit := a.Run([]string{"--json", "--as", "worker", "ask", "Ship it?", "--body", "Context", "--situation", "A launch decision is waiting.", "--impact", "The team cannot finish until we choose.", "--why", "This is the quickest safe approach.", "--recommend", "Yes", "--by", "1d"}); exit != 0 {
		t.Fatalf("exit=%d %s", exit, out.String())
	}
	msg := warningCodes(dailyJSON(t, out))["ask_auto_created_card"]
	if !strings.Contains(msg, "card:newest (Newest)") || !strings.Contains(msg, "card:second (Second)") || strings.Contains(msg, "card:closed") {
		t.Fatalf("suggestions=%q", msg)
	}
	if strings.Index(msg, "card:newest") > strings.Index(msg, "card:second") {
		t.Fatalf("server order lost: %q", msg)
	}
	if len(paths) != 3 || !strings.Contains(paths[2], "cursor=page-2") {
		t.Fatalf("queries=%v", paths)
	}
}

func TestBatchCreatesPrintOneRefPerLine(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/agents/agent-1" || r.URL.Path == "/agents/me":
			fmt.Fprint(w, `{"agent":{"id":"agent-1","actor_id":"actor-1","handle":"worker.host"},"open_asks":[]}`)
		case r.URL.Path == "/boards/board_launch/cards/batch" && r.Method == http.MethodPost:
			fmt.Fprint(w, `{"board":{"id":"board_launch","ref":"board:launch"},"cards":[{"ref":"card:a","title":"A"},{"ref":"card:b","title":"B"}]}`)
		case r.URL.Path == "/asks/batch" && r.Method == http.MethodPost:
			fmt.Fprint(w, `{"batch_id":"askbatch_test123","events":[{"id":"ev1"},{"id":"ev2"}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	batchFile := filepath.Join(t.TempDir(), "batch.json")
	if err := os.WriteFile(batchFile, []byte(`{"items":[{"title":"A"},{"title":"B"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	a, out := dailyTestApp(t, server.URL)
	if exit := a.Run([]string{"--json", "--as", "worker", "boards", "cards", "create-batch", "--board-id", "board_launch", "--from-file", batchFile}); exit != 0 {
		t.Fatalf("json exit=%d %s", exit, out.String())
	}
	result := asMap(dailyJSON(t, out)["result"])
	if _, ok := result["ref"]; ok {
		t.Fatalf("card batch result.ref=%#v", result["ref"])
	}
	out.Reset()
	if exit := a.Run([]string{"--as", "worker", "boards", "cards", "create-batch", "--board-id", "board_launch", "--from-file", batchFile}); exit != 0 {
		t.Fatalf("text exit=%d %s", exit, out.String())
	}
	lines := strings.Split(out.String(), "\n")
	if len(lines) < 3 || lines[0] != "card:a" || lines[1] != "card:b" || !strings.Contains(lines[2], "ok command=") {
		t.Fatalf("text=\n%s", out.String())
	}

	askFile := filepath.Join(t.TempDir(), "asks.json")
	if err := os.WriteFile(askFile, []byte(`{"asks":[
		{"title":"Ship?","subject_ref":"card:launch","thread_id":"thr1","situation":"A launch decision is waiting.","impact":"The team cannot finish until we choose.","recommendation":{"action":"Yes","why":"This is the quickest safe approach."},"recommend":"Yes"},
		{"title":"Wait?","subject_ref":"card:launch","thread_id":"thr1","situation":"A launch decision is waiting.","impact":"The team cannot finish until we choose.","recommendation":{"action":"No","why":"This is the quickest safe approach."},"recommend":"No"}
	]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if exit := a.Run([]string{"--json", "--as", "worker", "ask", "batch", "--from-file", askFile}); exit != 0 {
		t.Fatalf("ask batch exit=%d %s", exit, out.String())
	}
	askResult := asMap(dailyJSON(t, out)["result"])
	if askResult["ref"] != "askbatch_test123" || strings.Contains(anyString(askResult["ref"]), " ") {
		t.Fatalf("ask batch result.ref=%#v", askResult["ref"])
	}
	out.Reset()
	if exit := a.Run([]string{"--as", "worker", "ask", "batch", "--from-file", askFile}); exit != 0 {
		t.Fatalf("ask batch text exit=%d %s", exit, out.String())
	}
	text := out.String()
	first, rest, _ := strings.Cut(text, "\n")
	second, rest, _ := strings.Cut(rest, "\n")
	if !strings.HasPrefix(first, "event:") || !strings.HasPrefix(second, "event:") || strings.Contains(first, " ") || !strings.Contains(rest, "ok command=") {
		t.Fatalf("ask batch text=\n%s", text)
	}
}

func TestPlanSetRejectsBareRefInFile(t *testing.T) {
	hits := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		http.NotFound(w, r)
	}))
	defer server.Close()
	file := filepath.Join(t.TempDir(), "plan.json")
	body := `{"steps":[{"id":"build","title":"Build","ref":"https://github.com/org/repo/pull/12"},{"id":"track","title":"Track","ref":"SCA-123"},{"title":"Pull","ref":"#123"}]}`
	if err := os.WriteFile(file, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	raw := runCLIForTestJSONError(t, t.TempDir(), map[string]string{"ANX_BASE_URL": server.URL, "ANX_ACCESS_TOKEN": "token"}, []string{"--json", "plan", "set", "card:foo", "--from-file", file})
	payload := assertEnvelopeError(t, raw)
	errObj := asMap(payload["error"])
	msg := anyString(errObj["message"])
	if errObj["code"] != "invalid_request" || errObj["exit_code"] != float64(2) {
		t.Fatalf("%#v", errObj)
	}
	for _, want := range []string{`"track"`, "SCA-123", "full URL", "Linear", "GitHub"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("message=%q", msg)
		}
	}
	if strings.Contains(msg, "#123") {
		t.Fatalf("named the later step instead of the first offender: %q", msg)
	}
	if hits != 0 {
		t.Fatalf("bare plan ref reached the server %d times", hits)
	}
	pr := filepath.Join(t.TempDir(), "pr.json")
	if err := os.WriteFile(pr, []byte(`{"steps":[{"title":"Pull","ref":"#7"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	raw = runCLIForTestJSONError(t, t.TempDir(), map[string]string{"ANX_BASE_URL": server.URL, "ANX_ACCESS_TOKEN": "token"}, []string{"--json", "plan", "set", "card:foo", "--from-file", pr})
	msg = anyString(asMap(assertEnvelopeError(t, raw)["error"])["message"])
	if !strings.Contains(msg, "steps[0]") || !strings.Contains(msg, "#7") {
		t.Fatalf("unnamed step message=%q", msg)
	}
	good := filepath.Join(t.TempDir(), "good.json")
	if err := os.WriteFile(good, []byte(`{"steps":[{"id":"build","title":"Build","ref":"card:other"},{"id":"link","title":"Link","ref":"https://linear.app/acme/issue/SCA-123"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := parsePlanCommand([]string{"plan", "set", "card:foo", "--from-file", good}); err != nil {
		t.Fatal(err)
	}
	meta, err := registry.LoadEmbedded()
	if err != nil {
		t.Fatal(err)
	}
	var notes string
	for _, cmd := range meta.Commands {
		if cmd.CommandID == "plan.set" {
			notes = cmd.AgentNotes
		}
	}
	for _, want := range []string{"SCA-123", "#123", "usage error", "naming that step"} {
		if !strings.Contains(notes, want) {
			t.Fatalf("plan.set agent notes missing %q: %s", want, notes)
		}
	}
}
