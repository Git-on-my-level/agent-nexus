package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/errnorm"
	"agent-nexus-cli/internal/hostidentity"
	"agent-nexus-cli/internal/workspaceconfig"
)

func TestURLTypedRefsAndLegacyForms(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/cards/card:ship", "/cards/ship":
			fmt.Fprint(w, `{"card":{"ref":"card:ship","handle":"ship","board_ref":"board:tasks"}}`)
		case "/boards/tasks":
			fmt.Fprint(w, `{"board":{"ref":"board:tasks"}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	for _, args := range [][]string{{"card:ship"}, {"card", "card:ship"}, {"card", "ship"}, {"board:tasks"}, {"topic:launch"}, {"document:guide"}, {"artifact:report"}, {"thread:discussion"}} {

		home := t.TempDir()
		prefix := []string{"--base-url", server.URL, "url"}
		text := runCLIForTest(t, home, nil, nil, append(prefix, args...))
		suffix := map[string]string{"card": "/boards/tasks?card=ship", "board": "/boards/tasks", "topic": "/topics/launch", "document": "/docs/guide", "artifact": "/artifacts/report", "thread": "/threads/discussion"}
		kind := args[0]
		if prefix, _, ok := strings.Cut(kind, ":"); ok {
			kind = prefix
		}
		expected := server.URL + suffix[kind] + "\n"
		if text != expected {
			t.Fatalf("%v: got %q want %q", args, text, expected)
		}

		jsonArgs := append([]string{"--json"}, append(prefix, args...)...)
		var doc map[string]any
		if err := json.Unmarshal([]byte(runCLIForTest(t, home, nil, nil, jsonArgs)), &doc); err != nil {
			t.Fatal(err)
		}
		if asMap(doc["result"])["url"] != strings.TrimSpace(text) || asMap(doc["result"])["kind"] == "" {
			t.Fatalf("%v: %#v", args, doc)
		}
	}
	if _, err := preflightConfigIndependentUsage([]string{"url", "card:x", "extra", "more"}); err == nil {
		t.Fatal("malformed url must fail before auth")
	}
}

func TestNoteTextConfirmsSavedPromise(t *testing.T) {
	for _, next := range []bool{false, true} {
		t.Run(fmt.Sprint(next), func(t *testing.T) {
			rec := &noteRecorder{}
			server := noteServer(t, rec)
			defer server.Close()
			a, out := dailyTestApp(t, server.URL)
			args := []string{"--as", "worker", "note", "card:x", "Progress saved"}
			if next {
				args = append(args, "--next", "1d", "Run acceptance checks")
			}
			if exit := a.Run(args); exit != 0 {
				t.Fatalf("exit=%d %s", exit, out)
			}
			text := out.String()
			for _, want := range []string{"saved note_ref=event:", "card_ref=card:x", `note="Progress saved"`} {
				if !strings.Contains(text, want) {
					t.Fatalf("missing %s in %s", want, text)
				}
			}
			if next && (!strings.Contains(text, "next_at=") || !strings.Contains(text, `next_what="Run acceptance checks"`)) {
				t.Fatal(text)
			}
			if !next && (!strings.Contains(text, "--next") || strings.Contains(text, "suggest.3=")) {
				t.Fatal(text)
			}
			if strings.Contains(text, "Review the error") || strings.Contains(a.Stderr.(*bytes.Buffer).String(), "Review the error") {
				t.Fatal(text)
			}
		})
	}
}

func TestHandleFlags(t *testing.T) {
	a := newTestApp(t)
	body, _, err := a.parseCardCreateInput([]string{"--board", "board:tasks", "--title", "Ship", "--body", "Details", "--handle", "ship-fix"}, config.Resolved{}, "cards create")
	if err != nil || asMap(body["card"])["handle"] != "ship-fix" {
		t.Fatalf("%#v %v", body, err)
	}
	for _, bad := range []string{"", "UPPER", "two--dashes", "cards", "-edge", strings.Repeat("x", 65), "5c04521b-7629-4ef2-88ee-3dac188b5837"} {
		if _, err := preflightConfigIndependentUsage([]string{"cards", "create", "--handle", bad}); err == nil {
			t.Fatalf("preflight accepted %q", bad)
		}
		if validateCardHandle(bad) == nil {
			t.Fatalf("accepted %q", bad)
		}
		if _, err := parseWorkCommand([]string{"work", "create", "--from-file", "-", "--handle", bad}); err == nil {
			t.Fatalf("accepted work handle %q", bad)
		}
	}
	if _, err := preflightConfigIndependentUsage([]string{"cards", "create", "--handle", "Invalid", "--handle", "valid"}); err != nil {
		t.Fatal(err)
	}
	p, err := parseWorkCommand([]string{"work", "create", "--from-file", "-", "--handle", "ship-fix"})
	if err != nil || p.handle != "ship-fix" {
		t.Fatalf("%#v %v", p, err)
	}
}

func hintFixture(t *testing.T, target string, env map[string]string) (*App, config.Resolved) {
	t.Helper()
	home := t.TempDir()
	dir := filepath.Join(home, ".config", "anx")
	writeWorkspaceHost(t, dir, "b", target)
	hostDir := hostidentity.DirAt(dir, "b", target)
	raw, _ := json.Marshal(tokenCache{Token: "workspace-b-token", ExpiresAt: time.Now().Add(time.Hour)})
	if err := os.WriteFile(filepath.Join(hostDir, "token-worker.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	if err := workspaceconfig.Update(dir, func(c *workspaceconfig.Catalog) error {
		c.File.Aliases["a"] = "https://a.example.test"
		c.File.Aliases["b"] = target
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	a, _ := workspaceTestApp(t, home, home, env)
	return a, config.Resolved{BaseURL: "https://a.example.test", As: "worker", AccessToken: "DO-NOT-FORWARD", ConfigDir: dir, Sources: map[string]string{"base_url": "flag:--workspace"}}
}

func missingCard() error {
	e := errnorm.New(errnorm.KindRemote, "not_found", "card not found")
	annotateMissingRef(e, "/cards/card:ship/plan")
	return e
}

func TestWorkspaceHintsUseOnlyTargetCredentialsAndReturnAction(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("Authorization") != "Bearer workspace-b-token" || r.Method != "GET" || r.URL.Path != "/cards/card:ship" {
			t.Errorf("unsafe probe %s %s auth=%q", r.Method, r.URL.Path, r.Header.Get("Authorization"))
		}
		fmt.Fprint(w, `{"card":{"ref":"card:ship"}}`)
	}))
	defer server.Close()
	a, cfg := hintFixture(t, server.URL, nil)
	e := errnorm.Normalize(a.withWorkspaceHint(context.Background(), cfg, []string{"note", "card:ship", "Progress"}, missingCard()))
	if !strings.Contains(e.Message, `exists in workspace "b"`) || !strings.Contains(e.Message, "--workspace b") || errnorm.ExitCode(e) != 3 {
		t.Fatalf("%#v", e)
	}
	actions := deriveErrorActions("note", e)
	if len(actions) != 1 || strings.Join(actions[0].Argv, " ") != "anx --config-dir "+cfg.ConfigDir+" --workspace b --as worker note card:ship Progress" {
		t.Fatalf("%#v", actions)
	}
	if asMap(e.Details)["workspace_hint"] == nil || calls.Load() != 1 {
		t.Fatalf("%#v calls=%d", e, calls.Load())
	}
	a.Getenv = func(key string) string {
		if key == "ANX_WORKSPACE_HINTS" {
			return "0"
		}
		return ""
	}
	if got := a.withWorkspaceHint(context.Background(), cfg, nil, missingCard()); strings.Contains(got.Error(), "exists") || calls.Load() != 1 {
		t.Fatal(got)
	}
}

func TestWorkspaceHintsSkipUnauthenticatedAndExpiredAndRedirects(t *testing.T) {
	var leaked atomic.Int32
	sink := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { leaked.Add(1); fmt.Fprint(w, `{}`) }))
	defer sink.Close()
	var calls atomic.Int32
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.Redirect(w, r, sink.URL, http.StatusFound)
	}))
	defer redirect.Close()
	a, cfg := hintFixture(t, redirect.URL, nil)
	a.withWorkspaceHint(context.Background(), cfg, nil, missingCard())
	if leaked.Load() != 0 || calls.Load() != 1 {
		t.Fatalf("redirect followed: %d %d", leaked.Load(), calls.Load())
	}
	cache := filepath.Join(hostidentity.DirAt(cfg.ConfigDir, "b", redirect.URL), "token-worker.json")
	for _, mode := range []string{"expired", "unsafe", "missing"} {
		switch mode {
		case "expired":
			raw, _ := json.Marshal(tokenCache{Token: "old", ExpiresAt: time.Now().Add(-time.Hour)})
			os.WriteFile(cache, raw, 0600)
		case "unsafe":
			os.Chmod(cache, 0644)
		case "missing":
			os.Remove(cache)
		}
		a.withWorkspaceHint(context.Background(), cfg, nil, missingCard())
	}
	if calls.Load() != 1 {
		t.Fatal("unauthenticated probe")
	}
}

func TestWorkspaceHintTimeoutAndBound(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); <-r.Context().Done() }))
	defer server.Close()
	a, cfg := hintFixture(t, server.URL, nil)
	for i := 0; i < 8; i++ {
		base := fmt.Sprintf("%s/workspace-%d", server.URL, i)
		writeWorkspaceHost(t, cfg.ConfigDir, fmt.Sprintf("ws-%d", i), base)
		raw, _ := json.Marshal(tokenCache{Token: "token", ExpiresAt: time.Now().Add(time.Hour)})
		os.WriteFile(filepath.Join(hostidentity.DirAt(cfg.ConfigDir, fmt.Sprintf("ws-%d", i), base), "token-worker.json"), raw, 0600)
	}
	start := time.Now()
	got := a.withWorkspaceHint(context.Background(), cfg, nil, missingCard())
	if time.Since(start) > 2*time.Second || calls.Load() > workspaceHintLimit || strings.Contains(got.Error(), "exists") {
		t.Fatalf("elapsed=%s calls=%d err=%v", time.Since(start), calls.Load(), got)
	}
}

func TestOrientWorkspaceSelectionDetails(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".config", "anx")
	workspaceconfig.Update(dir, func(c *workspaceconfig.Catalog) error {
		c.File.Aliases["a"] = "https://a.example.test"
		c.File.Aliases["b"] = "https://b.example.test"
		c.File.Default = "a"
		c.File.Rules = map[string]string{"~/project/**": "b"}
		return nil
	})
	for _, tc := range []struct {
		cwd, env      string
		flag          *string
		alias, source string
	}{{home, "", nil, "a", "config:default"}, {filepath.Join(home, "project", "repo"), "", nil, "b", "config:directory-rule:"}, {home, "https://b.example.test", nil, "b", "env:ANX_BASE_URL"}, {home, "", stringPtr("b"), "b", "flag:--workspace"}} {
		a, _ := workspaceTestApp(t, home, tc.cwd, nil)
		cfg := config.Resolved{BaseURL: config.DefaultBaseURL, Sources: map[string]string{"base_url": "default"}}
		if tc.env != "" {
			cfg.BaseURL = tc.env
			cfg.Sources["base_url"] = "env:ANX_BASE_URL"
		}
		resolved, err := a.resolveWorkspace(cfg, tc.flag)
		if err != nil {
			t.Fatal(err)
		}
		got := a.orientWorkspace(resolved)
		if got["alias"] != tc.alias || !strings.HasPrefix(anyString(got["source"]), tc.source) || len(stringList(got["other_aliases"])) != 1 {
			t.Fatalf("%#v", got)
		}
	}
}

func stringPtr(s string) *string { return &s }

func TestCardAndWorkCreateTextIsConcise(t *testing.T) {
	var posted []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == "GET" {
			fmt.Fprint(w, `{"work":[]}`)
			return
		}
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		posted = append(posted, body)
		field := "card"
		if r.URL.Path == "/work" {
			field = "work"
		}
		fmt.Fprintf(w, `{"%s":{"ref":"card:chosen","handle":"chosen","board_ref":"board:tasks"},"board":{"cards":[{"title":"UNRELATED CARD"}]}}`, field)
	}))
	defer server.Close()
	for _, args := range [][]string{{"cards", "create", "--board", "board:tasks", "--title", "Ship", "--body", "Details", "--handle", "chosen"}, {"work", "create", "--from-file", "-", "--handle", "chosen"}} {
		input := strings.NewReader(`{"title":"Ship","handle":"overridden"}`)
		text := runCLIForTest(t, t.TempDir(), nil, input, append([]string{"--base-url", server.URL}, args...))
		if !strings.Contains(text, "created ref=card:chosen handle=chosen url=") || strings.Contains(text, "UNRELATED CARD") || strings.Contains(text, "result.board.cards") {
			t.Fatal(text)
		}
	}
	if asMap(posted[0]["card"])["handle"] != "chosen" || posted[1]["handle"] != "chosen" {
		t.Fatalf("%#v", posted)
	}
}

func TestMissingTypedRefHintsAcrossCardCommands(t *testing.T) {
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer workspace-b-token" {
			t.Error("wrong workspace credentials")
		}
		fmt.Fprint(w, `{"card":{"ref":"card:ship"}}`)
	}))
	defer other.Close()
	selected := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
		fmt.Fprint(w, `{"error":{"code":"not_found","message":"card not found"}}`)
	}))
	defer selected.Close()
	for _, args := range [][]string{{"context", "card:ship"}, {"note", "card:ship", "Progress"}, {"done", "card:ship", "--evidence", "event:proof"}, {"plan", "show", "card:ship"}, {"url", "card:ship"}, {"work", "get", "card:ship"}, {"cards", "get", "card:ship"}, {"read", "card:ship"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			a, cfg := hintFixture(t, other.URL, map[string]string{"ANX_ACCESS_TOKEN": "workspace-a-token"})
			workspaceconfig.Update(cfg.ConfigDir, func(c *workspaceconfig.Catalog) error { c.File.Aliases["a"] = selected.URL; return nil })
			out := a.Stdout.(*bytes.Buffer)
			full := append([]string{"--json", "--as", "worker", "--base-url", selected.URL}, args...)
			if exit := a.Run(full); exit != 3 {
				t.Fatalf("exit=%d %s", exit, out)
			}
			var doc map[string]any
			json.Unmarshal(out.Bytes(), &doc)
			e := asMap(doc["error"])
			if !strings.Contains(anyString(e["message"]), `exists in workspace "b"`) || len(asSlice(e["next_actions"])) == 0 {
				t.Fatalf("%s", out)
			}
		})
	}
}

func TestWorkspaceHintsIgnoreHTMLAndUnrelatedJSON(t *testing.T) {
	for _, body := range []string{"<html>Sign in</html>", `{"ok":true}`, `{"card":{}}`} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, body) }))
		a, cfg := hintFixture(t, server.URL, nil)
		got := a.withWorkspaceHint(context.Background(), cfg, nil, missingCard())
		server.Close()
		if strings.Contains(got.Error(), "exists") {
			t.Fatalf("false match for %s", body)
		}
	}
}

func TestMissingRefSelectsInnermostTypedResource(t *testing.T) {
	for _, path := range []string{"/boards/board:tasks/cards/card:ship", "/work/card:ship", "/cards/card:ship/plan"} {
		e := errnorm.New(errnorm.KindRemote, "not_found", "resource not found")
		annotateMissingRef(e, path)
		if got := anyString(asMap(e.Details)["requested_ref"]); got != "card:ship" {
			t.Fatalf("path=%s missing ref=%s", path, got)
		}
	}
}
