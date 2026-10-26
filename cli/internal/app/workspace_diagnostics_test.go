package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/errnorm"
	"agent-nexus-cli/internal/workspaceconfig"
)

func TestNotFoundNamesEveryWorkspaceRoutingSource(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `{"error":{"code":"not_found","message":"card not found"}}`)
	}))
	defer server.Close()
	host, _ := url.Parse(server.URL)
	for _, tc := range []struct {
		name, source string
		env          map[string]string
		flags        []string
		mapped       bool
	}{
		{name: "flag", source: "flag:--workspace", flags: []string{"--workspace", "selected"}, env: map[string]string{"ANX_WORKSPACE": "unknown"}},
		{name: "environment", source: "env:ANX_WORKSPACE", env: map[string]string{"ANX_WORKSPACE": "selected", "ANX_BASE_URL": "https://unused.example.test"}, mapped: true},
		{name: "directory mapping", source: "config:directory-rule:~/project/**", mapped: true},
		{name: "user default", source: "config:default"},
		{name: "URL flag", source: "flag:--base-url", flags: []string{"--base-url", server.URL}, env: map[string]string{"ANX_WORKSPACE": "unknown"}},
		{name: "URL environment", source: "env:ANX_BASE_URL", env: map[string]string{"ANX_BASE_URL": server.URL}},
	} {
		for _, jsonMode := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/json=%t", tc.name, jsonMode), func(t *testing.T) {
				home := t.TempDir()
				dir := filepath.Join(home, ".config", "anx")
				if err := workspaceconfig.Update(dir, func(c *workspaceconfig.Catalog) error {
					c.File.Aliases["selected"] = server.URL
					c.File.Default = "selected"
					if tc.mapped {
						c.File.Rules["~/project/**"] = "selected"
					}
					return nil
				}); err != nil {
					t.Fatal(err)
				}
				env := map[string]string{"ANX_ACCESS_TOKEN": "NEVER-PRINT-TOKEN", "ANX_WORKSPACE_HINTS": "0"}
				for k, v := range tc.env {
					env[k] = v
				}
				a, out := workspaceTestApp(t, home, filepath.Join(home, "project", "repo"), env)
				args := append([]string{}, tc.flags...)
				if jsonMode {
					args = append(args, "--json")
				}
				args = append(args, "--as", "worker", "cards", "get", "card:missing")
				if exit := a.Run(args); exit != 3 {
					t.Fatalf("exit=%d out=%s err=%s", exit, out, a.Stderr)
				}
				text := a.Stderr.(*bytes.Buffer).String()
				if jsonMode {
					var doc map[string]any
					if err := json.Unmarshal(out.Bytes(), &doc); err != nil {
						t.Fatal(err)
					}
					e := asMap(doc["error"])
					details := asMap(e["details"])
					if details["workspace"] != "selected" || details["workspace_source"] != tc.source || details["workspace_host"] != host.Host || details["next_action"] == nil || len(asSlice(e["next_actions"])) == 0 {
						t.Fatalf("%s", out)
					}
					text = anyString(e["message"]) + " " + anyString(details["hint"])
				}
				for _, want := range []string{"selected", host.Host, tc.source, "--workspace <alias>"} {
					if !strings.Contains(text, want) {
						t.Fatalf("missing %q in %s", want, text)
					}
				}
				if strings.Contains(out.String()+fmt.Sprint(a.Stderr), "NEVER-PRINT-TOKEN") {
					t.Fatal("token leaked")
				}
			})
		}
	}
}

func TestWorkspaceDiagnosticsNeverProbeEvenWithCachedCredentials(t *testing.T) {
	var calls atomic.Int32
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		fmt.Fprint(w, `{"card":{"ref":"card:ship"}}`)
	}))
	defer other.Close()
	a, cfg := hintFixture(t, other.URL, nil)
	for _, err := range []error{missingCard(), errnorm.FromHTTPFailure(404, nil), errnorm.FromHTTPFailure(404, []byte(`{"error":{"code":"card_not_found","message":"missing"}}`))} {
		e := errnorm.Normalize(a.withWorkspaceHint(cfg, err))
		if !strings.Contains(e.Message, `searched workspace "a"`) || strings.Contains(e.Message, "exists in") || asMap(e.Details)["workspace_source"] != "flag:--workspace" {
			t.Fatalf("%#v", e)
		}
	}
	if calls.Load() != 0 {
		t.Fatal("probed other workspace", calls.Load())
	}
	for _, err := range []error{errnorm.Local("draft_not_found", "local draft missing"), errnorm.New(errnorm.KindRemote, "forbidden", "denied")} {
		if got := a.withWorkspaceHint(cfg, err); got != err || asMap(errnorm.Normalize(got).Details)["workspace_source"] != nil {
			t.Fatalf("changed unrelated error: %#v", got)
		}
	}
}

func TestWorkspaceDiagnosticsRedactURLSecrets(t *testing.T) {
	a, _ := workspaceTestApp(t, t.TempDir(), t.TempDir(), nil)
	cfg := config.Resolved{BaseURL: "https://user:SECRET-PASSWORD@core.example.test:8443/api?token=SECRET-QUERY#SECRET-FRAGMENT", Sources: map[string]string{"base_url": "flag:--base-url"}}
	workspace := a.orientWorkspace(cfg)
	e := a.withWorkspaceHint(cfg, missingCard())
	data, _ := json.Marshal([]any{workspace, errnorm.Normalize(e)})
	if strings.Contains(string(data), "SECRET") || workspace["alias"] != "core.example.test:8443" || workspace["base_url"] != "https://core.example.test:8443/api" {
		t.Fatalf("unsafe diagnostics: %s", data)
	}
}

func TestOrientAndWhoamiTextShowWorkspaceRoute(t *testing.T) {
	brief, err := os.ReadFile(filepath.Join("..", "..", "..", "contracts", "fixtures", "brief", "brief.v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/brief":
			w.Write(brief)
		case "/agents/me":
			fmt.Fprint(w, `{"agent":{"id":"worker","actor_id":"own"}}`)
		default:
			fmt.Fprint(w, `{}`)
		}
	}))
	defer server.Close()
	for _, command := range [][]string{{"orient"}, {"auth", "whoami"}} {
		t.Run(strings.Join(command, " "), func(t *testing.T) {
			home := t.TempDir()
			if err := workspaceconfig.Update(filepath.Join(home, ".config", "anx"), func(c *workspaceconfig.Catalog) error {
				c.File.Aliases["chosen"] = server.URL
				c.File.Rules["~/project/**"] = "chosen"
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			a, out := workspaceTestApp(t, home, filepath.Join(home, "project", "repo"), map[string]string{"ANX_ACCESS_TOKEN": "SECRET-TOKEN"})
			if exit := a.Run(command); exit != 0 {
				t.Fatalf("exit=%d %s %s", exit, out, a.Stderr)
			}
			host, _ := url.Parse(server.URL)
			for _, want := range []string{"chosen", host.Host, "config:directory-rule:~/project/**"} {
				if !strings.Contains(out.String(), want) {
					t.Fatalf("missing %q: %s", want, out)
				}
			}
			if strings.Contains(out.String()+fmt.Sprint(a.Stderr), "SECRET-TOKEN") {
				t.Fatal("token leaked")
			}
		})
	}
}

func TestWorkspaceDiagnosticsPreserveDocumentRepairQueries(t *testing.T) {
	for _, tc := range []struct {
		message string
		want    string
	}{
		{message: "document missing", want: "anx docs list"},
		{message: `document "guide" missing`, want: "anx docs search guide"},
	} {
		err := errnorm.WithWorkspace(errnorm.New(errnorm.KindRemote, "not_found", tc.message), "selected", "core.example.test", "config:default")
		actions := deriveErrorActions("docs get", err)
		if len(actions) != 2 || strings.Join(actions[0].Argv, " ") != tc.want || strings.Join(actions[1].Argv, " ") != "anx config workspaces" {
			t.Fatalf("message=%q actions=%#v", tc.message, actions)
		}
	}
}
