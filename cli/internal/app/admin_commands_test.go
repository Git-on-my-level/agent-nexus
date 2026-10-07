package app

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAdminCLICommands(t *testing.T) {
	for _, tc := range []struct {
		args         []string
		method, path string
	}{
		{[]string{"auth", "admins", "grant", "fleet.host-a"}, "POST", "/auth/admins/fleet.host-a/grant"},
		{[]string{"auth", "admins", "revoke", "agent-1"}, "POST", "/auth/admins/agent-1/revoke"},
		{[]string{"auth", "admins", "list"}, "GET", "/auth/admins"},
		{[]string{"auth", "admins", "list", "--limit", "2", "--cursor", "next"}, "GET", "/auth/admins"},
		{[]string{"host", "tokens", "create", "--label", "fleet", "--expires-in", "10m"}, "POST", "/auth/hosts/enrollment-tokens"},
		{[]string{"host", "tokens", "list"}, "GET", "/auth/hosts/enrollment-tokens"},
		{[]string{"host", "tokens", "revoke", "htok-1"}, "POST", "/auth/hosts/enrollment-tokens/htok-1/revoke"},
		{[]string{"host", "revoke", "host-b"}, "DELETE", "/hosts/host-b"},
		{[]string{"host", "enrollments", "list"}, "GET", "/auth/hosts/enrollments/pending"},
		{[]string{"host", "enrollments", "approve", "ABCD-EFGH"}, "POST", "/auth/hosts/enrollments/henr-1/approve"},
		{[]string{"host", "enrollments", "deny", "ABCD-EFGH"}, "POST", "/auth/hosts/enrollments/henr-1/deny"},
	} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			seen := false
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer admin-test" {
					t.Error("missing authenticated principal")
				}
				w.Header().Set("Content-Type", "application/json")
				if r.URL.Path == "/auth/hosts/enrollments/pending" {
					if tc.path == r.URL.Path {
						seen = true
					}
					status := "pending"
					if strings.HasSuffix(tc.path, "/deny") {
						status = "approved"
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"enrollments": []any{map[string]any{"id": "henr-1", "user_code": "ABCD-EFGH", "status": status}}})
					return
				}
				if r.Method != tc.method || r.URL.Path != tc.path {
					t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
				}
				seen = true
				if len(tc.args) > 3 && tc.args[1] == "admins" && tc.args[2] == "list" {
					if r.URL.Query().Get("limit") != "2" || r.URL.Query().Get("cursor") != "next" {
						t.Error("pagination flags were not sent")
					}
				}
				if tc.args[1] == "tokens" && tc.args[2] == "create" {
					var body map[string]any
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Error(err)
					}
					if body["expires_in_seconds"] != float64(600) || body["label"] != "fleet" {
						t.Errorf("unexpected body %#v", body)
					}
					_, _ = w.Write([]byte(`{"token":"one-time-secret","enrollment_token":{"id":"htok-1"}}`))
					return
				}
				_, _ = w.Write([]byte(`{"ok":true}`))
			}))
			defer server.Close()
			a := newTestApp(t)
			home := t.TempDir()
			a.UserHomeDir = func() (string, error) { return home, nil }
			a.Getenv = func(k string) string {
				if k == "ANX_ACCESS_TOKEN" {
					return "admin-test"
				}
				return ""
			}
			var out, errout bytes.Buffer
			a.Stdout = &out
			a.Stderr = &errout
			argv := append([]string{"--json", "--base-url", server.URL}, tc.args...)
			if exit := a.Run(argv); exit != 0 {
				t.Fatalf("exit=%d output=%s stderr=%s", exit, out.String(), errout.String())
			}
			if !seen {
				t.Fatal("command did not reach endpoint")
			}
			if len(tc.args) > 2 && tc.args[1] == "admins" && tc.args[2] == "grant" && !strings.Contains(errout.String(), "every process that can read its host's shared key") {
				t.Fatal("grant omitted shared host credential trust warning")
			}
			var envelope map[string]any
			if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
				t.Fatal(err)
			}
			if envelope["ok"] != true || envelope["schema_version"] != float64(2) {
				t.Fatal(envelope)
			}
			if strings.Count(out.String(), "one-time-secret") > 1 || strings.Contains(errout.String(), "one-time-secret") {
				t.Fatal("secret repeated or logged")
			}
		})
	}
}

func TestAdminCommandUsageBeforeIdentity(t *testing.T) {
	for _, args := range [][]string{
		{"auth", "admins", "list", "--limit", "0"}, {"auth", "admins", "list", "--limit", "abc"}, {"auth", "admins", "list", "extra"}, {"host", "tokens", "create", "--typo"}, {"host", "enrollments", "bad"}, {"auth", "admins", "bad"},
		{"host", "enrollments", "list", "--typo"}, {"host", "revoke", "host-b", "--typo"}, {"host", "enroll", "--token-stdinn"},
	} {
		_, err := preflightConfigIndependentUsage(args)
		if err == nil {
			t.Errorf("invalid usage accepted: %v", args)
		}
	}
	for _, cmd := range []string{"host enrollments list", "host tokens list", "auth admins list"} {
		if commandSideEffectClass(cmd) != "read_only" {
			t.Errorf("wrong effect for %s", cmd)
		}
	}
	for _, cmd := range []string{"host revoke", "host tokens create", "host tokens revoke", "host enrollments approve", "host enrollments deny", "auth admins grant", "auth admins revoke"} {
		if commandSideEffectClass(cmd) != "remote_coordination_write" {
			t.Errorf("wrong effect for %s", cmd)
		}
	}
}

func TestHostEnrollmentStdinHelp(t *testing.T) {
	text, ok := helpTopicText("host enroll")
	if !ok || !strings.Contains(text, "--token-stdin") {
		t.Fatalf("missing stdin enrollment help: %s", text)
	}
}
