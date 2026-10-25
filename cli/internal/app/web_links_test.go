package app

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type webLinkCore struct {
	server     *httptest.Server
	handshakes atomic.Int32
}

// newWebLinkCore serves a minimal core. handshake is the /meta/handshake body;
// an empty string answers 500 so callers exercise handshake failure.
func newWebLinkCore(t *testing.T, handshake string, delay time.Duration) *webLinkCore {
	t.Helper()
	core := &webLinkCore{}
	core.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		path := r.URL.EscapedPath()
		switch {
		case path == "/meta/handshake":
			core.handshakes.Add(1)
			if delay > 0 {
				time.Sleep(delay)
			}
			if handshake == "" {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = io.WriteString(w, `{"error":{"code":"internal","message":"boom"}}`)
				return
			}
			_, _ = io.WriteString(w, handshake)
		case r.Method == http.MethodGet && path == "/work/card:launch":
			_, _ = io.WriteString(w, `{"work":{"ref":"card:launch","phase":"in_progress"}}`)
		case r.Method == http.MethodGet && path == "/work/card:launch/observations":
			_, _ = io.WriteString(w, `{"observations":[]}`)
		case r.Method == http.MethodGet && path == "/work/card:launch/refresh":
			_, _ = io.WriteString(w, `{"refresh":{"state":"idle"}}`)
		case r.Method == http.MethodGet && (path == "/cards/card:launch" || path == "/cards/card_launch" || path == "/cards/launch"):
			_, _ = io.WriteString(w, `{"card":{"id":"card_launch","ref":"card:launch","handle":"launch","title":"Launch","thread_id":"thread_launch","board_ref":"board:main"}}`)
		case r.Method == http.MethodGet && path == "/docs/document:runbook":
			_, _ = io.WriteString(w, `{"document":{"id":"doc_runbook","ref":"document:runbook","title":"Runbook"},"revision":{"content":"Hello"}}`)
		case r.Method == http.MethodGet && strings.HasPrefix(path, "/topics/"):
			_, _ = io.WriteString(w, `{"topic":{"id":"topic_launch","ref":"topic:launch","handle":"launch","thread_id":"thread_topic"}}`)
		case r.Method == http.MethodGet && strings.HasPrefix(path, "/boards/"):
			_, _ = io.WriteString(w, `{"board":{"id":"board_main","ref":"board:main","handle":"main"}}`)
		case r.Method == http.MethodPost && path == "/cards":
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, `{"card":{"id":"card_new","ref":"card:new-card","handle":"new-card","board_ref":"board:main"}}`)
		case r.Method == http.MethodPost && path == "/topics":
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, `{"topic":{"id":"topic_new","ref":"topic:new-topic","handle":"new-topic"}}`)
		case r.Method == http.MethodPost && path == "/boards":
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, `{"board":{"id":"board_new","ref":"board:new-board","handle":"new-board"}}`)
		case r.Method == http.MethodPost && path == "/docs":
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, `{"document":{"id":"doc_new","ref":"document:new-runbook","title":"Runbook"},"revision":{"revision_number":1}}`)
		case r.Method == http.MethodPost && path == "/events":
			var posted map[string]any
			_ = json.NewDecoder(r.Body).Decode(&posted)
			eventType := anyString(asMap(posted["event"])["type"])
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, `{"event":{"id":"event_1","type":"`+eventType+`","thread_id":"thread_launch","refs":["card:launch"]}}`)
		case r.Method == http.MethodGet && path == "/brief":
			_, _ = io.WriteString(w, `{}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(core.server.Close)
	return core
}

func runWebLinkCLI(t *testing.T, args ...string) (string, string, int) {
	t.Helper()
	home := t.TempDir()
	env := map[string]string{"HOME": home, "ANX_ACCESS_TOKEN": "test-token"}
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	cli := newTestAppWithHome(home)
	cli.Stdout, cli.Stderr, cli.Stdin = stdout, stderr, strings.NewReader("")
	cli.StdinIsTTY = func() bool { return false }
	cli.ReadFile = os.ReadFile
	cli.Getenv = func(key string) string { return env[key] }
	code := cli.Run(args)
	return stdout.String(), stderr.String(), code
}

const webLinkHandshake = `{"workspace_id":"ws_1","web_url":"https://example.com/o/acme/w/main"}`

func TestLinkPrintsOneURLPerRef(t *testing.T) {
	core := newWebLinkCore(t, webLinkHandshake, 0)
	out, stderr, code := runWebLinkCLI(t, "--base-url", core.server.URL, "link", "card:launch")
	if code != 0 || out != "https://example.com/o/acme/w/main/r/card:launch\n" {
		t.Fatalf("code=%d out=%q stderr=%s", code, out, stderr)
	}
	out, _, code = runWebLinkCLI(t, "--base-url", core.server.URL, "link", "card:launch", "document:runbook", "event:ask-1")
	want := "https://example.com/o/acme/w/main/r/card:launch\nhttps://example.com/o/acme/w/main/r/document:runbook\nhttps://example.com/o/acme/w/main/r/event:ask-1\n"
	if code != 0 || out != want {
		t.Fatalf("code=%d out=%q", code, out)
	}
	if got := core.handshakes.Load(); got != 2 {
		t.Fatalf("handshakes=%d, want one per invocation", got)
	}
}

func TestLinkJSONShape(t *testing.T) {
	core := newWebLinkCore(t, webLinkHandshake, 0)
	out, _, code := runWebLinkCLI(t, "--json", "--base-url", core.server.URL, "link", "card:launch", "Doc:runbook")
	if code != 0 {
		t.Fatalf("code=%d out=%s", code, out)
	}
	payload := assertEnvelopeOK(t, out)
	if payload["command"] != "link" {
		t.Fatalf("command=%v", payload["command"])
	}
	got, _ := json.Marshal(payload["result"])
	want := `{"links":[{"ref":"card:launch","web_url":"https://example.com/o/acme/w/main/r/card:launch"},{"ref":"doc:runbook","web_url":"https://example.com/o/acme/w/main/r/doc:runbook"}]}`
	if string(got) != want {
		t.Fatalf("result=%s\nwant  %s", got, want)
	}
}

func TestLinkEscapesRefAndTrimsTrailingSlash(t *testing.T) {
	core := newWebLinkCore(t, `{"web_url":"https://example.com/o/acme/w/main/"}`, 0)
	out, _, code := runWebLinkCLI(t, "--base-url", core.server.URL, "link", "document:notes/q3 plan", "card:a?b#c")
	want := "https://example.com/o/acme/w/main/r/document:notes%2Fq3%20plan\nhttps://example.com/o/acme/w/main/r/card:a%3Fb%23c\n"
	if code != 0 || out != want {
		t.Fatalf("code=%d out=%q", code, out)
	}
}

func TestLinkMissingWebURLIsNeedsContext(t *testing.T) {
	core := newWebLinkCore(t, `{"workspace_id":"ws_1"}`, 0)
	out, _, code := runWebLinkCLI(t, "--json", "--base-url", core.server.URL, "link", "card:launch")
	if code != 10 {
		t.Fatalf("code=%d out=%s", code, out)
	}
	errPayload := asMap(assertEnvelopeError(t, out)["error"])
	message := anyString(errPayload["message"])
	if errPayload["code"] != "needs_context" || !strings.Contains(message, "ANX_WEB_BASE_URL") || !strings.Contains(message, "https://example.com/o/<org>/w/<workspace>") {
		t.Fatalf("error=%#v", errPayload)
	}
	if got := anyString(asMap(errPayload["details"])["configure_env"]); got != "ANX_WEB_BASE_URL" {
		t.Fatalf("details=%#v", errPayload["details"])
	}
	_, stderr, code := runWebLinkCLI(t, "--base-url", core.server.URL, "link", "card:launch")
	if code != 10 || !strings.Contains(stderr, "error code=needs_context") || !strings.Contains(stderr, "ANX_WEB_BASE_URL") {
		t.Fatalf("code=%d stderr=%s", code, stderr)
	}
}

func TestLinkRejectsBareAndInvalidRefsBeforeNetwork(t *testing.T) {
	core := newWebLinkCore(t, webLinkHandshake, 0)
	for _, args := range [][]string{
		{"link", "launch"},
		{"link", "card:"},
		{"link", ":launch"},
		{"link", "https://example.com/o/acme/w/main/r/card:launch"},
		{"link", "card:launch", "launch"},
		{"link", "--board", "card:launch"},
	} {
		out, _, code := runWebLinkCLI(t, append([]string{"--json", "--base-url", core.server.URL}, args...)...)
		if code != 2 {
			t.Fatalf("%v: code=%d out=%s", args, code, out)
		}
		assertEnvelopeError(t, out)
	}
	if got := core.handshakes.Load(); got != 0 {
		t.Fatalf("usage errors contacted core %d times", got)
	}
	// Preflight rejects bad refs without any workspace or host configuration.
	if _, err := preflightConfigIndependentUsage([]string{"link", "launch"}); err == nil {
		t.Fatal("preflight accepted a bare ref")
	}
	if _, err := preflightConfigIndependentUsage([]string{"link", "card:launch", "doc:runbook"}); err != nil {
		t.Fatalf("preflight rejected typed refs: %v", err)
	}
}

func TestLinkIsReadOnlyAndHasHelp(t *testing.T) {
	if got := commandSideEffectClass("link"); got != "read_only" {
		t.Fatalf("side effect=%q", got)
	}
	text, ok := helpTopicText("link")
	if !ok || !strings.Contains(text, "anx link card:") {
		t.Fatalf("help=%q ok=%v", text, ok)
	}
	if !strings.Contains(newTestApp(t).rootUsageTextAll(), "link ") {
		t.Fatal("link missing from root help")
	}
}

func TestWebURLDecoratesSingleObjectResults(t *testing.T) {
	core := newWebLinkCore(t, webLinkHandshake, 0)
	base := "https://example.com/o/acme/w/main/r/"
	for _, tc := range []struct {
		name string
		args []string
		ref  string
	}{
		{"work get", []string{"work", "get", "card:launch"}, "card:launch"},
		{"context", []string{"context", "card:launch"}, "card:launch"},
		{"cards get", []string{"cards", "get", "card:launch"}, "card:launch"},
		{"cards message", []string{"cards", "message", "card:launch", "--body", "Progress", "--actor-id", "actor_1"}, "card:card_launch"},
		{"docs get", []string{"docs", "get", "document:runbook"}, "document:runbook"},
		{"docs create", []string{"docs", "create", "--subject-ref", "topic:launch", "--title", "Runbook", "--body", "Steps", "--actor-id", "actor_1"}, "document:new-runbook"},
		{"cards create", []string{"cards", "create", "--board", "board:main", "--title", "New card", "--body", "Criteria", "--actor-id", "actor_1"}, "card:new-card"},
		{"topics create", []string{"topics", "create", "--title", "New topic", "--summary", "Why", "--actor-id", "actor_1"}, "topic:new-topic"},
		{"boards create", []string{"boards", "create", "--topic", "topic:launch", "--title", "New board", "--actor-id", "actor_1"}, "board:new-board"},
		{"ask", []string{"ask", "Ship Friday?", "--subject-ref", "card:launch", "--situation", "A launch decision is waiting.", "--impact", "The team cannot finish until we choose.", "--why", "This is the quickest safe approach.", "--recommend", "Yes", "--by", "1d", "--actor-id", "actor_1"}, "event:event_1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, stderr, code := runWebLinkCLI(t, append([]string{"--json", "--base-url", core.server.URL}, tc.args...)...)
			if code != 0 {
				t.Fatalf("code=%d out=%s stderr=%s", code, out, stderr)
			}
			result := asMap(assertEnvelopeOK(t, out)["result"])
			if got := anyString(result["web_url"]); got != base+tc.ref {
				t.Fatalf("web_url=%q want %q result=%v", got, base+tc.ref, result)
			}
			text, stderr, code := runWebLinkCLI(t, append([]string{"--base-url", core.server.URL}, tc.args...)...)
			if code != 0 {
				t.Fatalf("text code=%d stderr=%s", code, stderr)
			}
			if strings.Count(text, "\nopen url=") != 1 || !strings.Contains(text, "\nopen url="+base+tc.ref+"\n") {
				t.Fatalf("want one open line for %s:\n%s", tc.ref, text)
			}
			if strings.Contains(text, "result.web_url") || strings.Contains(text, "result.url=") || strings.Contains(text, core.server.URL) {
				t.Fatalf("web_url duplicated or API URL shown in text:\n%s", text)
			}
		})
	}
}

func TestWebURLOmittedWhenUnavailable(t *testing.T) {
	for _, tc := range []struct {
		name      string
		handshake string
		delay     time.Duration
	}{
		{"absent", `{"workspace_id":"ws_1"}`, 0},
		{"handshake failure", "", 0},
		{"invalid web_url", `{"web_url":"ftp://example.com/x"}`, 0},
		{"slow handshake", webLinkHandshake, 1500 * time.Millisecond},
	} {
		t.Run(tc.name, func(t *testing.T) {
			core := newWebLinkCore(t, tc.handshake, tc.delay)
			for _, args := range [][]string{{"work", "get", "card:launch"}, {"docs", "get", "document:runbook"}} {
				started := time.Now()
				prefix := []string{"--json", "--base-url", core.server.URL}
				if tc.delay > 0 {
					// The decoration honors a shorter command timeout.
					prefix = append(prefix, "--timeout", "1s")
				}
				out, stderr, code := runWebLinkCLI(t, append(prefix, args...)...)
				if code != 0 {
					t.Fatalf("%v: code=%d out=%s stderr=%s", args, code, out, stderr)
				}
				if elapsed := time.Since(started); elapsed > webURLDecorationTimeout+time.Second {
					t.Fatalf("%v: decoration took %s", args, elapsed)
				}
				if _, ok := asMap(assertEnvelopeOK(t, out)["result"])["web_url"]; ok {
					t.Fatalf("%v: web_url present: %s", args, out)
				}
				if tc.delay > 0 {
					continue
				}
				text, _, code := runWebLinkCLI(t, append([]string{"--base-url", core.server.URL}, args...)...)
				if code != 0 || strings.Contains(text, "open url=") {
					t.Fatalf("%v: code=%d text=%s", args, code, text)
				}
			}
		})
	}
}

func TestWebURLSkipsHandshakeForUndecoratedCommands(t *testing.T) {
	core := newWebLinkCore(t, webLinkHandshake, 0)
	out, _, code := runWebLinkCLI(t, "--json", "--base-url", core.server.URL, "work", "freshness", "card:launch")
	if code != 0 {
		t.Fatalf("code=%d out=%s", code, out)
	}
	if got := core.handshakes.Load(); got != 0 {
		t.Fatalf("handshakes=%d for an undecorated command", got)
	}
}

func TestPrimaryWebRef(t *testing.T) {
	for _, tc := range []struct {
		command string
		root    map[string]any
		want    string
	}{
		{"work get", map[string]any{"work": map[string]any{"ref": "card:a"}}, "card:a"},
		{"cards create", map[string]any{"card": map[string]any{"id": "card_1"}}, "card:card_1"},
		{"cards create", map[string]any{"dry_run": true, "card": map[string]any{"ref": "card:a"}}, ""},
		{"docs message", map[string]any{"subject_kind": "document", "subject_id": "doc_1"}, "document:doc_1"},
		{"ask", map[string]any{"ask_id": "event:e1"}, "event:e1"},
		{"cards list", map[string]any{"card": map[string]any{"ref": "card:a"}}, ""},
		{"work get", map[string]any{"work": map[string]any{}}, ""},
	} {
		if got := primaryWebRef(tc.command, tc.root); got != tc.want {
			t.Errorf("%s %v: got %q want %q", tc.command, tc.root, got, tc.want)
		}
	}
}

// Creates keep the API-base-derived JSON url for scripts, but default text shows
// only the web link when one is available and the API URL when it is not.
func TestCreateTextReplacesAPIURLWithWebLink(t *testing.T) {
	args := []string{"docs", "create", "--subject-ref", "topic:launch", "--title", "Runbook", "--body", "Steps", "--actor-id", "actor_1"}
	core := newWebLinkCore(t, webLinkHandshake, 0)
	out, _, code := runWebLinkCLI(t, append([]string{"--json", "--base-url", core.server.URL}, args...)...)
	result := asMap(assertEnvelopeOK(t, out)["result"])
	if code != 0 || anyString(result["url"]) != core.server.URL+"/docs/new-runbook" || anyString(result["web_url"]) != "https://example.com/o/acme/w/main/r/document:new-runbook" {
		t.Fatalf("code=%d result=%v", code, result)
	}
	bare := newWebLinkCore(t, `{"workspace_id":"ws_1"}`, 0)
	text, _, code := runWebLinkCLI(t, append([]string{"--base-url", bare.server.URL}, args...)...)
	if code != 0 || !strings.Contains(text, "fact result.url="+bare.server.URL+"/docs/new-runbook") || strings.Contains(text, "open url=") {
		t.Fatalf("without web_url the existing url fact must remain:\n%s", text)
	}
}

// `anx url` is the API locator scripts substitute into API calls, so it keeps
// printing it even when the workspace reports a web_url. `anx link` is the
// human-facing command.
func TestURLCommandKeepsAPILocator(t *testing.T) {
	for _, handshake := range []string{webLinkHandshake, `{"workspace_id":"ws_1"}`, ""} {
		core := newWebLinkCore(t, handshake, 0)
		for _, args := range [][]string{{"document:runbook"}, {"document", "runbook"}} {
			text, _, code := runWebLinkCLI(t, append([]string{"--base-url", core.server.URL, "url"}, args...)...)
			if code != 0 || text != core.server.URL+"/docs/runbook\n" {
				t.Fatalf("handshake=%q args=%v: code=%d text=%q", handshake, args, code, text)
			}
		}
		out, _, _ := runWebLinkCLI(t, "--json", "--base-url", core.server.URL, "url", "document:runbook")
		result := asMap(assertEnvelopeOK(t, out)["result"])
		if _, ok := result["web_url"]; ok || anyString(result["url"]) != core.server.URL+"/docs/runbook" || result["kind"] == nil || result["id"] == nil {
			t.Fatalf("handshake=%q: result=%v", handshake, result)
		}
	}
	text, _ := helpTopicText("url")
	if strings.Contains(text, "shareable") || !strings.Contains(text, "anx link") || !strings.Contains(text, "ANX_WEB_BASE_URL") {
		t.Fatalf("url help=%q", text)
	}
}

// The CLI applies the same rules as core's ANX_WEB_BASE_URL validation, so a
// nonconforming core cannot make `anx link` print credentials.
func TestNormalizeWorkspaceWebURLMatchesCore(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want string
	}{
		{"https://example.com/o/acme/w/main/", "https://example.com/o/acme/w/main"},
		{" http://127.0.0.1:5173/o/local/w/local ", "http://127.0.0.1:5173/o/local/w/local"},
		{"https://ui.example.com", "https://ui.example.com"},
	} {
		got, err := normalizeWorkspaceWebURL(tc.raw)
		if err != nil || got != tc.want {
			t.Errorf("%q: got %q err=%v, want %q", tc.raw, got, err, tc.want)
		}
	}
	for _, raw := range []string{
		"https://user:secret@example.com/o/a/w/b",
		"https://user@example.com/o/a/w/b",
		"https://example.com/o/a/w/b?token=x",
		"https://example.com/o/a/w/b?",
		"https://example.com/o/a/w/b#top",
		"ftp://example.com/o/a/w/b",
		"javascript:alert(1)",
		"/o/a/w/b",
		"https:///o/a/w/b",
	} {
		got, err := normalizeWorkspaceWebURL(raw)
		if err == nil {
			t.Errorf("%q: accepted as %q", raw, got)
			continue
		}
		if strings.Contains(err.Error(), "secret") {
			t.Errorf("%q: error echoes credentials: %v", raw, err)
		}
	}
	core := newWebLinkCore(t, `{"web_url":"https://user:secret@example.com/o/a/w/b"}`, 0)
	out, _, code := runWebLinkCLI(t, "--json", "--base-url", core.server.URL, "link", "card:launch")
	if code == 0 || strings.Contains(out, "secret") {
		t.Fatalf("credentialed web_url must be rejected without echoing it: code=%d out=%s", code, out)
	}
}
