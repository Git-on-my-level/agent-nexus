package app

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/hostidentity"
	"agent-nexus-cli/internal/integrations"
	"agent-nexus-cli/internal/workspaceconfig"
)

const suggestedIntegration = "claude-code-suggested-tasks"

func integrationTestApp(t *testing.T) (*App, *bytes.Buffer, string) {
	t.Helper()
	home := t.TempDir()
	a, out := workspaceTestApp(t, home, home, nil)
	return a, out, home
}
func integrationResult(t *testing.T, a *App, out *bytes.Buffer, args ...string) map[string]any {
	t.Helper()
	out.Reset()
	if code := a.Run(append([]string{"--json", "integrations"}, args...)); code != 0 {
		t.Fatalf("exit %d: %s / %v", code, out, a.Stderr)
	}
	var doc map[string]any
	if err := json.Unmarshal(out.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	return asMap(doc["result"])
}
func TestIntegrationsEnableDisableDryRun(t *testing.T) {
	a, out, home := integrationTestApp(t)
	first := integrationResult(t, a, out, "list")
	if !strings.Contains(string(integrations.JSON(first)), `"disabled"`) {
		t.Fatal(first)
	}
	plan := integrationResult(t, a, out, "enable", suggestedIntegration, "--board", "tasks", "--dry-run")
	if plan["changed"] != true {
		t.Fatal(plan)
	}
	entries, _ := os.ReadDir(home)
	if len(entries) != 0 {
		t.Fatal("dry run or list wrote files", entries)
	}
	enabled := integrationResult(t, a, out, "enable", suggestedIntegration, "--board", "tasks")
	if enabled["changed"] != true {
		t.Fatal(enabled)
	}
	again := integrationResult(t, a, out, "enable", suggestedIntegration, "--board", "tasks")
	if again["changed"] != false {
		t.Fatal(again)
	}
	status := integrationResult(t, a, out, "status", suggestedIntegration)
	if status["state"] != "enabled" || status["hook_matches"] != true {
		t.Fatal(status)
	}
	configPath := enabled["config_path"].(string)
	c, err := integrations.LoadConfig(configPath)
	if err != nil {
		t.Fatal(err)
	}
	c.Executable = filepath.Join(home, "deleted-anx")
	if err = integrations.AtomicWrite(configPath, integrations.JSON(c)); err != nil {
		t.Fatal(err)
	}
	status = integrationResult(t, a, out, "status", suggestedIntegration)
	if status["state"] != "drifted" {
		t.Fatal(status)
	}
	disabled := integrationResult(t, a, out, "disable", suggestedIntegration, "--dry-run")
	if disabled["changed"] != true {
		t.Fatal(disabled)
	}
	if _, err = os.Stat(configPath); err != nil {
		t.Fatal("disable dry-run removed config")
	}
	disabled = integrationResult(t, a, out, "disable", suggestedIntegration)
	if disabled["changed"] != true {
		t.Fatal(disabled)
	}
	disabled = integrationResult(t, a, out, "disable", suggestedIntegration)
	if disabled["changed"] != false {
		t.Fatal(disabled)
	}
}
func TestIntegrationsInvalidSettingsRefuse(t *testing.T) {
	a, out, home := integrationTestApp(t)
	path := filepath.Join(home, ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("not json"), 0600); err != nil {
		t.Fatal(err)
	}
	if code := a.Run([]string{"--json", "integrations", "enable", suggestedIntegration, "--board", "tasks"}); code == 0 || !strings.Contains(out.String(), "integration_config_invalid") {
		t.Fatal(code, out.String())
	}
	if _, err := os.Stat(path + ".anx-lock"); !os.IsNotExist(err) {
		t.Fatal("created settings lock for invalid JSON", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".config", "anx")); !os.IsNotExist(err) {
		t.Fatal("created config state for invalid JSON", err)
	}
	b, _ := os.ReadFile(path)
	if string(b) != "not json" {
		t.Fatal("settings changed")
	}
	matches, _ := filepath.Glob(filepath.Join(home, ".config", "anx", "integrations", suggestedIntegration, "*.json"))
	if len(matches) != 0 {
		t.Fatal("config written on invalid settings")
	}
}
func sampleHook(cwd string) string {
	b, _ := json.Marshal(map[string]any{"session_id": "session", "cwd": cwd, "hook_event_name": "PostToolUse", "tool_name": "mcp__ccd_session__spawn_task", "tool_use_id": "tool-1", "tool_input": map[string]any{"title": "Follow up", "tldr": "Summary", "prompt": "Full original prompt"}, "tool_response": []any{map[string]any{"type": "text", "text": "task_id: task_1234abcd"}}})
	return string(b)
}
func TestIntegrationsRunSpoolsAndRoutesCWD(t *testing.T) {
	a, out, home := integrationTestApp(t)
	dir := filepath.Join(home, ".config", "anx")
	cwd := filepath.Join(home, "project")
	base := "http://127.0.0.1:1"
	if err := workspaceconfig.Update(dir, func(c *workspaceconfig.Catalog) error {
		c.File.Aliases["project"] = base
		c.File.Rules = map[string]string{filepath.Join(cwd, "**"): "project"}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// Enabling in a different directory registers only its own workspace board.
	integrationResult(t, a, out, "enable", suggestedIntegration, "--board", "tasks")
	a.Stdin = strings.NewReader(sampleHook(filepath.Join(cwd, "subdir")))
	out.Reset()
	if code := a.Run([]string{"integrations", "run", suggestedIntegration}); code != 0 || out.Len() != 0 {
		t.Fatal(code, out.String())
	}
	r, _ := integrations.Lookup(suggestedIntegration)
	_, _, _, id, err := a.integrationLocation(config.Resolved{}, r, "user")
	if err != nil {
		t.Fatal(err)
	}
	queuePath := integrations.QueuePath(dir, suggestedIntegration, id)
	q, err := integrations.LoadQueue(queuePath)
	if err != nil || len(q.Pending) != 1 || q.Pending[0].Reason != "no_board" {
		t.Fatal(q, err)
	}
	// Register that workspace, then replay under the original event's cwd.
	a.Getwd = func() (string, error) { return filepath.Join(cwd, "subdir"), nil }
	integrationResult(t, a, out, "enable", suggestedIntegration, "--board", "follow-ups")
	a.Getwd = func() (string, error) { return home, nil }
	a.Stdin = strings.NewReader(sampleHook(filepath.Join(cwd, "subdir")))
	out.Reset()
	if code := a.Run([]string{"--json", "integrations", "run", suggestedIntegration}); code != 0 || out.Len() != 0 {
		t.Fatal(code, out.String())
	}
	q, err = integrations.LoadQueue(queuePath)
	if err != nil || len(q.Pending) != 1 || q.Pending[0].Delivery == nil || q.Pending[0].Delivery.BaseURL != base {
		t.Fatal(q, err)
	}
	if strings.Count(a.Stderr.(*bytes.Buffer).String(), "\n") > 2 {
		t.Fatal("hook too noisy")
	}
}
func TestIntegrationsUsageBeforeConfigAndHelp(t *testing.T) {
	for _, args := range [][]string{{"integrations", "bogus"}, {"integrations", "enable", suggestedIntegration, "--oops"}, {"integrations", "enable"}, {"integrations", "status", suggestedIntegration, "--scope", "wrong"}} {
		a, out, _ := integrationTestApp(t)
		a.Getenv = func(string) string { return "" }
		if code := a.Run(append([]string{"--json"}, args...)); code != 2 {
			t.Fatal(args, code, out.String())
		}
	}
	a, out, _ := integrationTestApp(t)
	for _, args := range [][]string{{"integrations", "--help"}, {"integrations", "enable", "--help"}, {"help", "integrations"}} {
		out.Reset()
		if code := a.Run(args); code != 0 || !strings.Contains(out.String(), "Disabled until explicitly enabled") {
			t.Fatal(args, code, out.String())
		}
	}
	for command, want := range map[string]string{"integrations list": "read_only", "integrations status": "read_only", "integrations enable": "local_operational_write", "integrations disable": "local_operational_write", "integrations run": "remote_coordination_write"} {
		if got := commandSideEffectClass(command); got != want {
			t.Fatal(command, got)
		}
	}
}
func TestIntegrationsRunAlwaysZeroAndBounded(t *testing.T) {
	a, out, _ := integrationTestApp(t)
	integrationResult(t, a, out, "enable", suggestedIntegration)
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	defer writer.Close()
	a.Stdin = reader
	out.Reset()
	start := time.Now()
	if code := a.Run([]string{"integrations", "run", suggestedIntegration}); code != 0 || out.Len() != 0 || time.Since(start) > 4*time.Second {
		t.Fatal(code, out.String(), time.Since(start))
	}
	// End the blocked worker before test cleanup to avoid touching removed paths.
	writer.Close()
}
func TestIntegrationsProjectLocalIndependent(t *testing.T) {
	a, out, home := integrationTestApp(t)
	for _, scope := range []string{"project", "local"} {
		integrationResult(t, a, out, "enable", suggestedIntegration, "--scope", scope)
		s := integrationResult(t, a, out, "status", suggestedIntegration, "--scope", scope)
		if s["state"] != "enabled" {
			t.Fatal(s)
		}
	}
	if _, err := os.Stat(filepath.Join(home, ".claude", "settings.local.json")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := a.runIntegrations(context.Background(), []string{"disable", suggestedIntegration, "--scope", "local"}, config.Resolved{}); err != nil {
		t.Fatal(err)
	}
	s := integrationResult(t, a, out, "status", suggestedIntegration, "--scope", "project")
	if s["state"] != "enabled" {
		t.Fatal(s)
	}
}

func TestIntegrationsHostAuthAndUnknownOutcomeReplay(t *testing.T) {
	a, out, home := integrationTestApp(t)
	var mu sync.Mutex
	var bodies []string
	var agentName string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/auth/token":
			var grant map[string]any
			if err := json.NewDecoder(r.Body).Decode(&grant); err != nil {
				t.Error(err)
			}
			mu.Lock()
			agentName = anyString(grant["agent_name"])
			mu.Unlock()
			fmt.Fprint(w, `{"agent":{"id":"agent-1","actor_id":"actor-1","handle":"claude-code.host"},"tokens":{"access_token":"derived-token","expires_in":3600}}`)
		case "/cards":
			if r.Header.Get("Authorization") != "Bearer derived-token" {
				t.Error("missing derived host auth")
			}
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if !strings.Contains(anyString(body["request_key"]), "anx-integration-") {
				t.Error("missing replay key")
			}
			mu.Lock()
			bodies = append(bodies, string(integrations.JSON(body)))
			count := len(bodies)
			mu.Unlock()
			if count == 1 {
				w.WriteHeader(http.StatusServiceUnavailable)
				fmt.Fprint(w, `{"error":{"code":"unavailable","message":"unknown outcome"}}`)
			} else {
				w.WriteHeader(http.StatusCreated)
				fmt.Fprint(w, `{"card":{"ref":"card:follow-up"},"board":{"ref":"board:tasks"}}`)
			}
		default:
			t.Errorf("unexpected HTTP read %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	_, key, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = hostidentity.Save(home, hostidentity.Host{ID: "host-1", KeyID: "key-1", WorkspaceID: "ws_test", BaseURL: server.URL}, key); err != nil {
		t.Fatal(err)
	}
	enabled := integrationResult(t, a, out, "enable", suggestedIntegration, "--board", "tasks", "--as", "mirror-agent")
	// The complete card bytes and identity must remain fixed across a config change.
	for i := 0; i < 2; i++ {
		a.Stdin = strings.NewReader(sampleHook(home))
		out.Reset()
		if code := a.Run([]string{"integrations", "run", suggestedIntegration}); code != 0 || out.Len() != 0 {
			t.Fatal(code, out.String())
		}
		if i == 0 {
			integrationResult(t, a, out, "enable", suggestedIntegration, "--board", "new-board", "--as", "another-agent")
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(bodies) != 2 || bodies[0] != bodies[1] || agentName != "mirror-agent" {
		t.Fatal("replay identity/body changed", agentName, bodies)
	}
	c, err := integrations.LoadConfig(enabled["config_path"].(string))
	if err != nil {
		t.Fatal(err)
	}
	q, err := integrations.LoadQueue(integrations.QueuePath(filepath.Join(home, ".config", "anx"), suggestedIntegration, c.ID))
	if err != nil || len(q.Pending) != 0 || len(q.Ledger) != 1 {
		t.Fatal(q, err)
	}
}

func TestIntegrationsPinnedWorkspace(t *testing.T) {
	a, out, home := integrationTestApp(t)
	dir := filepath.Join(home, ".config", "anx")
	cwd := filepath.Join(home, "other", "repo")
	pinned := "http://127.0.0.1:1"
	if err := workspaceconfig.Update(dir, func(c *workspaceconfig.Catalog) error {
		c.File.Aliases["pinned"] = pinned
		c.File.Aliases["other"] = "http://127.0.0.1:2"
		c.File.Rules = map[string]string{filepath.Join(home, "other", "**"): "other"}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	enabled := integrationResult(t, a, out, "enable", suggestedIntegration, "--workspace", "pinned", "--board", "tasks", "--column", "ready", "--as", "mirror")
	c, err := integrations.LoadConfig(enabled["config_path"].(string))
	if err != nil || c.Workspace != "pinned" {
		t.Fatal(c, err)
	}
	a.Stdin = strings.NewReader(sampleHook(cwd))
	out.Reset()
	if code := a.Run([]string{"integrations", "run", suggestedIntegration}); code != 0 || out.Len() != 0 {
		t.Fatal(code, out.String())
	}
	q, err := integrations.LoadQueue(integrations.QueuePath(dir, suggestedIntegration, c.ID))
	if err != nil || len(q.Pending) != 1 || q.Pending[0].Delivery == nil {
		t.Fatal(q, err)
	}
	d := q.Pending[0].Delivery
	if d.BaseURL != pinned || d.Agent != "mirror" || asMap(d.Body["card"])["column_key"] != "ready" {
		t.Fatal(d)
	}
}

func TestIntegrationsRunCannotEnterSettingsMutationHandler(t *testing.T) {
	a, out, _ := integrationTestApp(t)
	integrationResult(t, a, out, "enable", suggestedIntegration)
	if _, _, err := a.runIntegrations(context.Background(), []string{"run", suggestedIntegration}, config.Resolved{}); err == nil {
		t.Fatal("hook entered the settings mutation handler")
	}
	if state := integrationResult(t, a, out, "status", suggestedIntegration); state["state"] != "enabled" {
		t.Fatal(state)
	}
}

func TestIntegrationsDoNotReplayUnderReenrolledHost(t *testing.T) {
	a, out, home := integrationTestApp(t)
	var mu sync.Mutex
	creates := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/auth/token" {
			fmt.Fprint(w, `{"agent":{"id":"agent-1","actor_id":"actor-1"},"tokens":{"access_token":"token","expires_in":3600}}`)
			return
		}
		if r.URL.Path == "/cards" {
			mu.Lock()
			creates++
			mu.Unlock()
			w.WriteHeader(503)
			fmt.Fprint(w, `{"error":{"code":"unavailable","message":"unknown outcome"}}`)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()
	_, key, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = hostidentity.Save(home, hostidentity.Host{ID: "host-original", KeyID: "key", WorkspaceID: "ws_test", BaseURL: server.URL}, key); err != nil {
		t.Fatal(err)
	}
	enabled := integrationResult(t, a, out, "enable", suggestedIntegration, "--board", "tasks")
	for i := 0; i < 2; i++ {
		a.Stdin = strings.NewReader(sampleHook(home))
		out.Reset()
		if code := a.Run([]string{"integrations", "run", suggestedIntegration}); code != 0 || out.Len() != 0 {
			t.Fatal(code, out.String())
		}
		if i == 0 {
			dir := filepath.Join(home, ".config", "anx")
			host, ok, err := hostidentity.LoadAt(dir, server.URL)
			if err != nil || !ok {
				t.Fatal(err)
			}
			host.ID = "host-reenrolled"
			if err = integrations.AtomicWrite(filepath.Join(filepath.Dir(host.PrivateKeyPath), "host.json"), integrations.JSON(host)); err != nil {
				t.Fatal(err)
			}
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if creates != 1 {
		t.Fatal("replayed under another host", creates)
	}
	c, err := integrations.LoadConfig(enabled["config_path"].(string))
	if err != nil {
		t.Fatal(err)
	}
	q, err := integrations.LoadQueue(integrations.QueuePath(filepath.Join(home, ".config", "anx"), suggestedIntegration, c.ID))
	if err != nil || len(q.Pending) != 1 || q.Pending[0].Reason != "identity_changed" {
		t.Fatal(q, err)
	}
}

func TestIntegrationsMayRecoverAuthBeforeFirstCardAttempt(t *testing.T) {
	a, out, home := integrationTestApp(t)
	var mu sync.Mutex
	grants, creates := 0, 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/auth/token" {
			mu.Lock()
			grants++
			n := grants
			mu.Unlock()
			if n == 1 {
				w.WriteHeader(401)
				fmt.Fprint(w, `{"error":{"code":"invalid_token","message":"enroll again"}}`)
				return
			}
			fmt.Fprint(w, `{"agent":{"id":"agent-new","actor_id":"actor-new"},"tokens":{"access_token":"token","expires_in":3600}}`)
			return
		}
		if r.URL.Path == "/cards" {
			mu.Lock()
			creates++
			mu.Unlock()
			w.WriteHeader(201)
			fmt.Fprint(w, `{"card":{"ref":"card:task"}}`)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()
	_, key, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = hostidentity.Save(home, hostidentity.Host{ID: "host-old", KeyID: "key", WorkspaceID: "ws_test", BaseURL: server.URL}, key); err != nil {
		t.Fatal(err)
	}
	integrationResult(t, a, out, "enable", suggestedIntegration, "--board", "tasks")
	for i := 0; i < 2; i++ {
		a.Stdin = strings.NewReader(sampleHook(home))
		out.Reset()
		if code := a.Run([]string{"integrations", "run", suggestedIntegration}); code != 0 || out.Len() != 0 {
			t.Fatal(code, out.String())
		}
		if i == 0 {
			host, _, err := hostidentity.Load(home, server.URL)
			if err != nil {
				t.Fatal(err)
			}
			host.ID = "host-new"
			if err = integrations.AtomicWrite(filepath.Join(filepath.Dir(host.PrivateKeyPath), "host.json"), integrations.JSON(host)); err != nil {
				t.Fatal(err)
			}
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if creates != 1 {
		t.Fatal("auth recovery failed", creates)
	}
}
