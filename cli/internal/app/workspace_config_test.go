package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"agent-nexus-cli/internal/hostidentity"
	"agent-nexus-cli/internal/workspaceconfig"
)

func workspaceTestApp(t *testing.T, home, cwd string, env map[string]string) (*App, *bytes.Buffer) {
	t.Helper()
	stdout := &bytes.Buffer{}
	a := newTestApp(t)
	a.Stdout = stdout
	a.Stderr = &bytes.Buffer{}
	a.UserHomeDir = func() (string, error) { return home, nil }
	a.Getwd = func() (string, error) { return cwd, nil }
	a.Getenv = func(key string) string {
		if key == "HOME" {
			return home
		}
		return env[key]
	}
	return a, stdout
}

// Enrollment records are enough for selection; no private key is needed.
func writeWorkspaceHost(t *testing.T, configDir, id, base string) {
	t.Helper()
	dir := hostidentity.DirAt(configDir, id, base)
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(hostidentity.Host{ID: "host-" + id, WorkspaceID: id, BaseURL: base})
	if err := os.WriteFile(filepath.Join(dir, "host.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestWorkspaceResolutionOrder(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".config", "anx")
	cwd := filepath.Join(home, "work", "demo", "repo")
	for _, tc := range []struct {
		name                string
		hosts               int
		defaultURL          string
		rules               map[string]string
		env                 map[string]string
		flags               []string
		wantURL, wantSource string
	}{
		{name: "zero hosts local dev", wantURL: "http://127.0.0.1:8000", wantSource: "default"},
		{name: "single enrolled", hosts: 1, wantURL: "https://a.example", wantSource: "bridge:auto-single"},
		{name: "default beats single", hosts: 1, defaultURL: "b", wantURL: "https://b.example", wantSource: "config:default"},
		{name: "default resolves multi", hosts: 2, defaultURL: "a", wantURL: "https://a.example", wantSource: "config:default"},
		{name: "directory beats default", hosts: 2, defaultURL: "a", rules: map[string]string{filepath.Join(home, "work", "**"): "b"}, wantURL: "https://b.example", wantSource: "config:directory-rule:" + filepath.Join(home, "work", "**")},
		{name: "specific directory beats broad", hosts: 2, defaultURL: "a", rules: map[string]string{filepath.Join(home, "work", "**"): "a", "~/work/demo/**": "b"}, wantURL: "https://b.example", wantSource: "config:directory-rule:~/work/demo/**"},
		{name: "environment beats directory", hosts: 2, defaultURL: "a", rules: map[string]string{"~/work/**": "a"}, env: map[string]string{"ANX_BASE_URL": "https://env.example"}, wantURL: "https://env.example", wantSource: "env:ANX_BASE_URL"},
		{name: "URL flag beats environment", hosts: 2, defaultURL: "a", rules: map[string]string{"~/work/**": "a"}, env: map[string]string{"ANX_BASE_URL": "https://env.example"}, flags: []string{"--base-url", "https://flag.example"}, wantURL: "https://flag.example", wantSource: "flag:--base-url"},
		{name: "workspace flag beats environment", hosts: 2, defaultURL: "a", rules: map[string]string{"~/work/**": "a"}, env: map[string]string{"ANX_BASE_URL": "https://env.example"}, flags: []string{"--workspace", "b"}, wantURL: "https://b.example", wantSource: "flag:--workspace"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			os.RemoveAll(dir)
			for i, base := range []string{"https://a.example", "https://b.example"} {
				if i < tc.hosts {
					writeWorkspaceHost(t, dir, []string{"ws_a", "ws_b"}[i], base)
				}
			}
			if err := workspaceconfig.Update(dir, func(c *workspaceconfig.Catalog) error {
				c.File.Aliases["a"] = "https://a.example"
				c.File.Aliases["b"] = "https://b.example"
				c.File.Default = tc.defaultURL
				c.File.Rules = tc.rules
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			a, stdout := workspaceTestApp(t, home, cwd, tc.env)
			args := append([]string{"--json"}, tc.flags...)
			args = append(args, "config", "show")
			if exit := a.Run(args); exit != 0 {
				t.Fatalf("exit=%d stdout=%s", exit, stdout)
			}
			var payload map[string]any
			if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
				t.Fatal(err)
			}
			result := asMap(payload["result"])
			if result["base_url"] != tc.wantURL || asMap(result["sources"])["base_url"] != tc.wantSource {
				t.Fatalf("result=%#v", result)
			}
		})
	}
}

func TestWorkspaceConfigCommandsAreGlobalAndRepairAmbiguity(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(t.TempDir(), "custom-config")
	cwd := filepath.Join(home, "outside-repos", "demo")
	writeWorkspaceHost(t, dir, "ws_a", "https://a.example")
	writeWorkspaceHost(t, dir, "ws_b", "https://b.example")
	a, stdout := workspaceTestApp(t, home, cwd, map[string]string{"ANX_CONFIG_DIR": dir})
	run := func(args ...string) map[string]any {
		t.Helper()
		stdout.Reset()
		if exit := a.Run(append([]string{"--json"}, args...)); exit != 0 {
			t.Fatalf("%v: exit=%d stdout=%s", args, exit, stdout)
		}
		var payload map[string]any
		if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
			t.Fatal(err)
		}
		return asMap(payload["result"])
	}
	listed := run("config", "workspaces")
	if listed["base_url"] != "" || listed["source"] != "unresolved:ambiguous" || !strings.Contains(anyString(listed["selection_error"]), "config use") {
		t.Fatalf("list=%#v", listed)
	}
	if _, err := os.Stat(filepath.Join(dir, workspaceconfig.Filename)); !os.IsNotExist(err) {
		t.Fatalf("read wrote preferences: %v", err)
	}
	run("config", "use", "a")
	run("config", "map", "~/outside-repos/**", "b")
	if result := run("config", "show"); result["base_url"] != "https://b.example" {
		t.Fatalf("rule=%#v", result)
	}
	if result := run("config", "workspaces"); result["matching_rule"] != filepath.Join(home, "outside-repos", "**") {
		t.Fatalf("list=%#v", result)
	}
	run("config", "unmap", "~/outside-repos/**")
	run("config", "unmap", "~/outside-repos/**")
	if result := run("config", "show"); result["base_url"] != "https://a.example" {
		t.Fatalf("default=%#v", result)
	}
	run("config", "use", "https://custom.example/ws/main")
	if result := run("config", "show"); result["base_url"] != "https://custom.example/ws/main" {
		t.Fatalf("url default=%#v", result)
	}
	if result := run("config", "workspaces", "--workspace=b"); result["base_url"] != "https://b.example" || result["source"] != "flag:--workspace" {
		t.Fatalf("explicit list=%#v", result)
	}
	info, err := os.Stat(filepath.Join(dir, workspaceconfig.Filename))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("preferences permissions=%v %v", info, err)
	}
	stdout.Reset()
	if exit := a.Run([]string{"--json", "config", "show", "--workspace=b"}); exit != 0 || !strings.Contains(stdout.String(), "flag:--workspace") {
		t.Fatalf("trailing workspace: %s", stdout)
	}
}

func TestWorkspaceUsageErrorsBeatAmbiguity(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".config", "anx")
	writeWorkspaceHost(t, dir, "ws_a", "https://a.example")
	writeWorkspaceHost(t, dir, "ws_b", "https://b.example")
	for _, args := range [][]string{
		{"config", "map", "~/work/**"}, {"config", "show", "extra"}, {"config", "use", "--bad"},
		{"--workspace", "a", "--base-url", "https://a.example", "doctor"}, {"--workspace=", "doctor"},
	} {
		a, stdout := workspaceTestApp(t, home, home, nil)
		if exit := a.Run(append([]string{"--json"}, args...)); exit != 2 || strings.Contains(stdout.String(), "workspace_ambiguous") {
			t.Fatalf("%v: %s", args, stdout)
		}
	}
	for _, args := range [][]string{{"config", "use", "unknown"}, {"--workspace", "unknown", "config", "show"}, {"config", "map", "relative/**", "a"}} {
		a, stdout := workspaceTestApp(t, home, home, nil)
		if exit := a.Run(append([]string{"--json"}, args...)); exit != 2 {
			t.Fatalf("%v: %s", args, stdout)
		}
	}
}

func TestExplicitConfigDirRepairsAndTextOutput(t *testing.T) {
	home, dir := t.TempDir(), t.TempDir()
	writeWorkspaceHost(t, dir, "ws_a", "https://a.example")
	writeWorkspaceHost(t, dir, "ws_b", "https://b.example")
	a, stdout := workspaceTestApp(t, home, home, nil)
	args := []string{"--json", "--config-dir", dir, "doctor"}
	if exit := a.Run(args); exit != 2 {
		t.Fatalf("exit=%d stdout=%s", exit, stdout)
	}
	var payload map[string]any
	json.Unmarshal(stdout.Bytes(), &payload)
	for _, raw := range asSlice(asMap(payload["error"])["next_actions"]) {
		argv := stringList(asMap(raw)["argv"])
		if len(argv) < 3 || argv[1] != "--config-dir" || argv[2] != dir {
			t.Fatalf("repair lost config-dir: %v", argv)
		}
	}
	stdout.Reset()
	if exit := a.Run([]string{"--config-dir", dir, "config", "workspaces"}); exit != 0 || !strings.Contains(stdout.String(), "https://a.example") || !strings.Contains(stdout.String(), "https://b.example") {
		t.Fatalf("text list=%s", stdout)
	}
	stdout.Reset()
	if exit := a.Run([]string{"--config-dir", dir, "config", "use", "a"}); exit != 0 {
		t.Fatalf("text use=%s", stdout)
	}
	stdout.Reset()
	if exit := a.Run([]string{"--config-dir", dir, "config", "show"}); exit != 0 || !strings.Contains(stdout.String(), "config:default") {
		t.Fatalf("text show=%s", stdout)
	}
}

func TestExplicitURLBypassesCorruptPreferences(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".config", "anx")
	os.MkdirAll(dir, 0700)
	os.WriteFile(filepath.Join(dir, workspaceconfig.Filename), []byte("{"), 0600)
	a, stdout := workspaceTestApp(t, home, home, nil)
	if exit := a.Run([]string{"--json", "--base-url", "https://explicit.example", "config", "show"}); exit != 0 {
		t.Fatalf("explicit URL=%s", stdout)
	}
	stdout.Reset()
	if exit := a.Run([]string{"--json", "doctor"}); exit == 0 || !strings.Contains(stdout.String(), "workspace_config_invalid") {
		t.Fatalf("corrupt file ignored: %s", stdout)
	}
}

// Intercept the default transport so this test cannot leak its fixture bearer
// to localhost, an enrolled endpoint, or any other network destination.
type workspaceRecordingTransport struct{ requests []*http.Request }

func (r *workspaceRecordingTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	r.requests = append(r.requests, request)
	return nil, fmt.Errorf("request intercepted by workspace regression test")
}

func TestHelpFlagValuesNeverBypassWorkspaceResolution(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".config", "anx")
	writeWorkspaceHost(t, dir, "ws_a", "https://a.example")
	writeWorkspaceHost(t, dir, "ws_b", "https://b.example")
	transport := &workspaceRecordingTransport{}
	original := http.DefaultTransport
	http.DefaultTransport = transport
	t.Cleanup(func() { http.DefaultTransport = original })
	for _, commandArgs := range [][]string{
		{"topics", "message", "topic:test", "--body", "--help"},
		{"topics", "message", "topic:test", "--body", "-h"},
		{"topics", "message", "topic:test", "--body", "help"},
		{"topics", "message", "topic:test", "--body=--help"},
		{"import", "apply", "--plan", "--help", "--execute"},
		{"import", "apply", "--plan", "fixture.json", "--execute=false", "--execute=true"},
	} {
		t.Run(strings.Join(commandArgs, " "), func(t *testing.T) {
			a, stdout := workspaceTestApp(t, home, home, map[string]string{"ANX_ACCESS_TOKEN": "fixture"})
			args := append([]string{"--json"}, commandArgs...)
			if exit := a.Run(args); exit != 2 {
				t.Fatalf("exit=%d output=%s", exit, stdout)
			}
			var payload map[string]any
			if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
				t.Fatal(err)
			}
			if asMap(payload["error"])["code"] != "workspace_ambiguous" {
				t.Fatalf("output=%s", stdout)
			}
			if len(transport.requests) != 0 {
				t.Fatalf("ambiguous command sent %d requests", len(transport.requests))
			}
		})
	}
	// The exact reported argv must honor an explicit alias even with a help-
	// shaped body value. The transport stops the request before any real I/O.
	for _, body := range []string{"--help", "-h", "help"} {
		transport.requests = nil
		a, stdout := workspaceTestApp(t, home, home, map[string]string{"ANX_ACCESS_TOKEN": "fixture"})
		if exit := a.Run([]string{"--json", "--workspace", "b", "topics", "message", "topic:test", "--body", body}); exit != 6 {
			t.Fatalf("alias exit=%d output=%s", exit, stdout)
		}
		if len(transport.requests) == 0 {
			t.Fatalf("body %q was treated as help", body)
		}
		for _, request := range transport.requests {
			if request.URL.Scheme != "https" || request.URL.Host != "b.example" || request.Header.Get("Authorization") != "Bearer fixture" {
				t.Fatalf("wrong workspace: %s", request.URL)
			}
		}
	}
}

func TestParsedCommandHelpNeverDispatchesNetworkCommand(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".config", "anx")
	writeWorkspaceHost(t, dir, "ws_a", "https://a.example")
	writeWorkspaceHost(t, dir, "ws_b", "https://b.example")
	transport := &workspaceRecordingTransport{}
	original := http.DefaultTransport
	http.DefaultTransport = transport
	t.Cleanup(func() { http.DefaultTransport = original })
	for _, args := range [][]string{
		{"topics", "message", "--help"},
		{"topics", "message", "topic:test", "--body", "literal", "--help"},
		{"topics", "message", "topic:test", "--body", "--help", "-h"},
		{"doctor", "--help"}, {"debug", "meta", "--help"},
	} {
		a, stdout := workspaceTestApp(t, home, home, map[string]string{"ANX_ACCESS_TOKEN": "fixture"})
		if exit := a.Run(append([]string{"--json"}, args...)); exit != 0 {
			t.Fatalf("%v: exit=%d output=%s", args, exit, stdout)
		}
		if !strings.Contains(stdout.String(), "help_text") {
			t.Fatalf("help=%s", stdout)
		}
	}
	// A command with no published help topic also stays local: it must return
	// a usage error instead of falling through to its network handler.
	a, stdout := workspaceTestApp(t, home, home, map[string]string{"ANX_ACCESS_TOKEN": "fixture"})
	if exit := a.Run([]string{"--json", "debug", "meta", "handshake", "--help"}); exit != 2 || !strings.Contains(stdout.String(), "unknown help topic") {
		t.Fatalf("unknown help: exit=%d output=%s", exit, stdout)
	}
	if len(transport.requests) != 0 {
		t.Fatalf("help dispatched %d requests", len(transport.requests))
	}
}
