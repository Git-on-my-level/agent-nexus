package app

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/errnorm"
)

func TestLeaseUsageBeforeHostResolution(t *testing.T) {
	for _, args := range [][]string{
		{"lease"}, {"lease", "bogus", "card:x"},
		{"lease", "show"}, {"lease", "show", "card:x", "card:y"}, {"lease", "show", "card:x", "--owner", "a"},
		{"lease", "acquire", "card:x", "--ttl", "2h"},
		{"lease", "acquire", "card:x", "--owner", "exec-a"},
		{"lease", "acquire", "card:x", "--owner", "exec-a", "--ttl", "30s"},
		{"lease", "acquire", "card:x", "--owner", "exec-a", "--ttl", "8d"},
		{"lease", "acquire", "card:x", "--owner", "exec-a", "--ttl", "2 hours"},
		{"lease", "acquire", "card:x", "--owner", "has space", "--ttl", "2h"},
		{"lease", "acquire", "card:x", "--owner", "exec-a", "--ttl", "2h", "--if-epoch", "-1"},
		{"lease", "acquire", "card:x", "--owner", "exec-a", "--owner", "exec-b", "--ttl", "2h"},
		{"lease", "renew", "card:x"},
		{"lease", "renew", "card:x", "--owner", "exec-a", "--reason", "x"},
		{"lease", "release", "card:x", "--owner", "exec-a", "--ttl", "2h"},
	} {
		a := newTestApp(t)
		a.Getenv = func(string) string { return "" }
		a.UserHomeDir = func() (string, error) { return t.TempDir(), nil }
		a.Stderr = &bytes.Buffer{}
		a.Stdout = &bytes.Buffer{}
		if code := a.Run(args); code != 2 {
			t.Fatalf("%v returned %d: %s", args, code, a.Stdout)
		}
	}
}

func TestLeaseTTLGrammar(t *testing.T) {
	for raw, want := range map[string]int64{"60s": 60, "90m": 5400, "2h": 7200, "7d": 604800} {
		if got, ok := parseLeaseTTL(raw); !ok || got != want {
			t.Fatalf("%s: %d %v", raw, got, ok)
		}
	}
	for _, raw := range []string{"", "59s", "0h", "8d", "1h30m", "-1h", "99999999999999999999d"} {
		if _, ok := parseLeaseTTL(raw); ok {
			t.Fatalf("%q accepted", raw)
		}
	}
}

func TestLeaseSideEffectClassesAndExitCodes(t *testing.T) {
	for command, want := range map[string]string{
		"lease show": "read_only", "lease acquire": "remote_coordination_write", "lease renew": "remote_coordination_write", "lease release": "remote_coordination_write",
	} {
		if got := commandSideEffectClass(command); got != want {
			t.Fatalf("%s: %s", command, got)
		}
	}
	for code, want := range map[string]int{"lease_held": 13, "lease_expired": 12, "lease_not_held": 4} {
		if got := errnorm.ExitCode(errnorm.FromHTTPFailure(409, []byte(`{"error":{"code":"`+code+`","message":"m"}}`))); got != want {
			t.Fatalf("%s exit=%d want %d", code, got, want)
		}
	}
}

func TestLeaseRequests(t *testing.T) {
	type call struct {
		method, path string
		body         map[string]any
	}
	calls := []call{}
	held := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c := call{method: r.Method, path: r.URL.EscapedPath()}
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &c.body)
		calls = append(calls, c)
		if held {
			w.WriteHeader(http.StatusConflict)
			io.WriteString(w, `{"error":{"code":"lease_held","message":"card lease is held by exec-a (epoch 1) until 2026-10-25T11:00:00Z","recoverable":false,"details":{"owner":"exec-a","epoch":1,"expires_at":"2026-10-25T11:00:00.000000000Z","state":"held"}}}`)
			return
		}
		io.WriteString(w, `{"lease":{"card_ref":"card:ramp","state":"held","owner":"exec-a","epoch":1,"expires_at":"2026-10-25T11:00:00.000000000Z","remaining_seconds":7200,"ttl_seconds":7200,"server_time":"2026-10-25T09:00:00Z"},"changed":true}`)
	}))
	defer server.Close()
	a := newTestApp(t)
	a.Stderr = &bytes.Buffer{}
	cfg := config.Resolved{BaseURL: server.URL, AccessToken: "owner", Timeout: 10 * time.Second}
	run := func(args ...string) (*commandResult, error) {
		t.Helper()
		_, res, err := a.runCommand(context.Background(), args, cfg)
		return res, err
	}
	res, err := run("lease", "acquire", "card:ramp", "--owner", "exec-a", "--ttl", "2h", "--reason", "ramp step 3", "--if-epoch", "0")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Text, "state=held") || !strings.Contains(res.Text, "owner=exec-a") || !strings.Contains(res.Text, "epoch=1") {
		t.Fatalf("text=%q", res.Text)
	}
	if _, err = run("lease", "renew", "card:ramp", "--owner", "exec-a", "--ttl=90m"); err != nil {
		t.Fatal(err)
	}
	if _, err = run("lease", "release", "card:ramp", "--owner", "exec-a"); err != nil {
		t.Fatal(err)
	}
	if _, err = run("lease", "show", "card:ramp"); err != nil {
		t.Fatal(err)
	}
	held = true
	_, err = run("lease", "acquire", "card:ramp", "--owner", "exec-b", "--ttl", "1h")
	if errnorm.ExitCode(err) != 13 || !strings.Contains(err.Error(), "exec-a") {
		t.Fatalf("held err=%v exit=%d", err, errnorm.ExitCode(err))
	}
	if calls[0].method != "POST" || calls[0].path != "/cards/card:ramp/lease/acquire" || calls[0].body["owner"] != "exec-a" || calls[0].body["ttl_seconds"] != float64(7200) || calls[0].body["reason"] != "ramp step 3" || calls[0].body["if_epoch"] != float64(0) {
		t.Fatalf("acquire: %+v", calls[0])
	}
	if calls[1].path != "/cards/card:ramp/lease/renew" || calls[1].body["ttl_seconds"] != float64(5400) || calls[1].body["reason"] != nil {
		t.Fatalf("renew: %+v", calls[1])
	}
	if calls[2].path != "/cards/card:ramp/lease/release" || calls[2].body["ttl_seconds"] != nil {
		t.Fatalf("release: %+v", calls[2])
	}
	if calls[3].method != "GET" || calls[3].path != "/cards/card:ramp/lease" {
		t.Fatalf("show: %+v", calls[3])
	}
}
