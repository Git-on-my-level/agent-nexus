package app

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/profile"
	"agent-nexus-cli/internal/workspaceconfig"
)

func TestHostIdentityErrorsOfferNextActions(t *testing.T) {
	for _, tc := range []struct {
		name    string
		args    []string
		code    string
		command string
	}{
		{name: "unresolved", args: []string{"--json", "auth", "whoami"}, code: "identity_unresolved", command: "--as"},
		{name: "unenrolled", args: []string{"--json", "--as", "codex", "auth", "whoami"}, code: "host_not_enrolled", command: "enroll"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := New()
			home := t.TempDir()
			a.UserHomeDir = func() (string, error) { return home, nil }
			a.Getenv = func(key string) string {
				if key == "HOME" {
					return home
				}
				return ""
			}
			var stdout bytes.Buffer
			a.Stdout = &stdout
			if code := a.Run(tc.args); code == 0 {
				t.Fatalf("expected failure: %s", stdout.String())
			}
			var payload map[string]any
			if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
				t.Fatal(err)
			}
			failure := asMap(payload["error"])
			if anyString(failure["code"]) != tc.code {
				t.Fatalf("unexpected error: %#v", failure)
			}
			if !strings.Contains(fmt.Sprint(failure["next_actions"]), tc.command) {
				t.Fatalf("missing next action: %#v", failure)
			}
		})
	}
}

func TestIdentityResolutionOrder(t *testing.T) {
	cases := []struct {
		name             string
		env              map[string]string
		as, want, source string
		fails            bool
	}{
		{name: "flag", env: map[string]string{"ANX_AS": "env", "AGENTCTL_ADAPTER": "claude-code", "AGENTCTL_EXECUTION_ID": "exec-one", "CLAUDECODE": "1"}, as: "reviewer", want: "reviewer", source: "flag:--as"},
		{name: "env", env: map[string]string{"AGENTCTL_ADAPTER": "codex", "AGENTCTL_EXECUTION_ID": "exec-one"}, as: "release-bot", want: "release-bot", source: "env:ANX_AS"},
		{name: "agentctl", env: map[string]string{"AGENTCTL_ADAPTER": "claude-code", "AGENTCTL_EXECUTION_ID": "exec-one", "CLAUDECODE": "1"}, want: "claude", source: "agentctl"},
		{name: "claude", env: map[string]string{"CLAUDECODE": "1"}, want: "claude", source: "harness:claude"},
		{name: "codex", env: map[string]string{"CODEX_THREAD_ID": "thread-one"}, want: "codex", source: "harness:codex"},
		{name: "cursor", env: map[string]string{"CURSOR_AGENT_COMPLETED_PATH": "/tmp/out"}, want: "cursor", source: "harness:cursor"},
		{name: "omp", env: map[string]string{"AGENT": "1"}, want: "omp", source: "harness:omp"},
		{name: "missing", env: map[string]string{}, fails: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := New()
			// This table covers the direct fallback independently of locally installed providers.
			a.runtimeIdentity = nil
			a.hasOMPAncestor = func() bool { return true }
			a.Getenv = func(k string) string { return tc.env[k] }
			cfg := config.Resolved{As: tc.as, IdentitySource: tc.source}
			got, source, err := a.identityName(cfg)
			if tc.fails {
				if err == nil {
					t.Fatal("expected identity error")
				}
				return
			}
			if err != nil || got != tc.want || source != tc.source {
				t.Fatalf("%q %q %v", got, source, err)
			}
		})
	}
}

func TestHostEnrollmentAdoptsOrExcludesLocalProfile(t *testing.T) {
	for _, exclude := range []bool{false, true} {
		t.Run(fmt.Sprint("exclude=", exclude), func(t *testing.T) {
			home := t.TempDir()
			pub, key, err := ed25519.GenerateKey(nil)
			if err != nil {
				t.Fatal(err)
			}
			keyPath := profile.KeyPath(home, "reviewer")
			if err := profile.SavePrivateKey(keyPath, key); err != nil {
				t.Fatal(err)
			}
			var posted map[string]any
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/meta/handshake":
					fmt.Fprint(w, `{"workspace_id":"ws_test","workspace_slug":"personal"}`)
				case "/auth/hosts/enrollments/headless":
					if err := json.NewDecoder(r.Body).Decode(&posted); err != nil {
						t.Error(err)
					}
					w.WriteHeader(201)
					fmt.Fprint(w, `{"host":{"id":"host-1","key_id":"key-1","slug":"testhost"}}`)
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			path := profile.ProfilePath(home, "reviewer")
			if err := profile.Save(path, profile.Profile{Agent: "reviewer", AgentID: "agent-1", KeyID: "key-old", BaseURL: server.URL, PrivateKeyPath: keyPath}); err != nil {
				t.Fatal(err)
			}
			a := New()
			a.UserHomeDir = func() (string, error) { return home, nil }
			cfg := config.Resolved{BaseURL: server.URL, Timeout: 10_000_000_000}
			args := []string{"--name", "testhost", "--token", "headless"}
			if exclude {
				args = append(args, "--exclude", "reviewer")
			}
			plan, err := a.hostEnroll(context.Background(), append(append([]string{}, args...), "--plan"), cfg)
			if err != nil {
				t.Fatal(err)
			}
			adoptedPlan := stringList(asMap(plan.Data)["adopt"])
			if (len(adoptedPlan) == 0) != exclude {
				t.Fatalf("bad adoption plan: %#v", plan.Data)
			}
			configDir := filepath.Join(home, ".config", "anx")
			if err := workspaceconfig.Update(configDir, func(c *workspaceconfig.Catalog) error { c.File.Default = "https://existing.example"; return nil }); err != nil {
				t.Fatal(err)
			}
			enrolled, err := a.hostEnroll(context.Background(), args, cfg)
			if err != nil {
				t.Fatal(err)
			}
			prefs, err := workspaceconfig.Load(configDir)
			if err != nil {
				t.Fatal(err)
			}
			if prefs.File.Default != "https://existing.example" || prefs.File.Aliases["personal"] != server.URL {
				t.Fatalf("enrollment changed selection: %#v", prefs.File)
			}
			actions := deriveNextActions("host enroll", nil, enrolled.Data)
			if len(actions) != 2 || strings.Join(actions[0].Argv, " ") != "anx config use personal" {
				t.Fatalf("enrollment repairs=%#v", actions)
			}
			proofs := asSlice(posted["adoptions"])
			if (len(proofs) == 0) != exclude {
				t.Fatalf("bad proofs: %#v", posted)
			}
			if !exclude {
				proof := asMap(proofs[0])
				sig, _ := base64.StdEncoding.DecodeString(anyString(proof["signature"]))
				msg := "anx-host-adopt|" + anyString(posted["request_nonce"]) + "|" + anyString(posted["public_key"]) + "|agent-1|reviewer"
				if !ed25519.Verify(pub, []byte(msg), sig) {
					t.Fatal("adoption proof not signed by local profile key")
				}
			}
			_, statErr := os.Stat(path)
			if exclude && statErr != nil || !exclude && !os.IsNotExist(statErr) {
				t.Fatalf("profile deletion mismatch: %v", statErr)
			}
			hostKey := filepath.Join(home, ".config", "anx", "hosts", "ws_test", "host.ed25519")
			st, err := os.Stat(hostKey)
			if err != nil || st.Mode().Perm() != 0600 {
				t.Fatalf("host key permissions: %v %v", st, err)
			}
		})
	}
}
