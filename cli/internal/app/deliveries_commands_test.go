package app

import (
	"agent-nexus-cli/internal/config"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestDeliveriesUsageBeforeHostResolution(t *testing.T) {
	for _, args := range [][]string{
		{"deliveries"}, {"deliveries", "bogus"}, {"deliveries", "claim"},
		{"deliveries", "claim", "--channel", "chat", "--limit", "0"},
		{"deliveries", "receipt", "--claim-token", "t", "--outcome", "delivered"},
		{"deliveries", "receipt", "d1", "--claim-token", "t", "--outcome", "maybe"},
		{"deliveries", "receipt", "d1", "--outcome", "delivered"},
		{"deliveries", "list", "--channel", "chat"},
		{"notifications", "settings"}, {"notifications", "settings", "set"}, {"notifications", "settings", "get", "--body-file", "x"},
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

func TestDeliveriesSideEffectClasses(t *testing.T) {
	for command, want := range map[string]string{
		"deliveries list": "read_only", "deliveries claim": "remote_coordination_write", "deliveries receipt": "remote_coordination_write",
		"notifications settings get": "read_only", "notifications settings set": "remote_coordination_write",
	} {
		if got := commandSideEffectClass(command); got != want {
			t.Fatalf("%s: %s", command, got)
		}
	}
}

func TestDeliveriesAndSettingsRequests(t *testing.T) {
	type call struct {
		method, path string
		body         map[string]any
	}
	calls := []call{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c := call{method: r.Method, path: r.URL.RequestURI()}
		if r.Body != nil {
			raw, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(raw, &c.body)
		}
		calls = append(calls, c)
		switch r.URL.Path {
		case "/deliveries":
			io.WriteString(w, `{"deliveries":[{"delivery":{"delivery_id":"d1","channel":"chat","kind":"push","status":"queued","attempt":0},"receipts":[]}]}`)
		case "/deliveries/claim":
			io.WriteString(w, `{"deliveries":[{"delivery_id":"d1","channel":"chat","kind":"push","claim_token":"tok","address":"a"}]}`)
		case "/deliveries/d1/receipt":
			io.WriteString(w, `{"delivery":{"delivery_id":"d1","status":"failed","attempt":1}}`)
		case "/notification-settings":
			io.WriteString(w, `{"channels":[{"channel":"chat","address":"a","push_enabled":true,"digest_enabled":false}],"available_channels":["chat"]}`)
		default:
			t.Errorf("unexpected %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	a := newTestApp(t)
	a.Stderr = &bytes.Buffer{}
	a.ReadFile = func(string) ([]byte, error) {
		return []byte(`{"channels":[{"channel":"chat","address":"a","push_enabled":true,"digest_enabled":false}]}`), nil
	}
	cfg := config.Resolved{BaseURL: server.URL, AccessToken: "owner", Timeout: 10 * time.Second}
	run := func(args ...string) *commandResult {
		t.Helper()
		_, res, err := a.runCommand(context.Background(), args, cfg)
		if err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		return res
	}
	run("deliveries", "list", "--limit", "5")
	run("deliveries", "claim", "--channel", "chat", "--limit", "3", "--lease-seconds", "45")
	res := run("deliveries", "receipt", "d1", "--claim-token", "tok", "--outcome", "failed", "--error", "boom", "--retryable", "true")
	run("notifications", "settings", "get")
	run("notifications", "settings", "set", "--body-file", "s.json")
	if res.Text == "" {
		t.Fatal("empty text")
	}
	if calls[0].path != "/deliveries?limit=5" || calls[0].method != "GET" {
		t.Fatalf("list: %+v", calls[0])
	}
	if calls[1].body["channel"] != "chat" || calls[1].body["limit"] != float64(3) || calls[1].body["lease_seconds"] != float64(45) {
		t.Fatalf("claim: %+v", calls[1])
	}
	if calls[2].path != "/deliveries/d1/receipt" || calls[2].body["claim_token"] != "tok" || calls[2].body["outcome"] != "failed" || calls[2].body["retryable"] != true || calls[2].body["error"] != "boom" {
		t.Fatalf("receipt: %+v", calls[2])
	}
	if calls[3].method != "GET" || calls[3].path != "/notification-settings" {
		t.Fatalf("settings get: %+v", calls[3])
	}
	if calls[4].method != "PUT" || len(asSlice(calls[4].body["channels"])) != 1 {
		t.Fatalf("settings set: %+v", calls[4])
	}
}
