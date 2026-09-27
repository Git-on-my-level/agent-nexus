package app

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/hostidentity"
)

func runCLIForTest(t *testing.T, home string, env map[string]string, stdin io.Reader, args []string) string {
	t.Helper()
	envCopy := map[string]string{}
	envCopy["HOME"] = home
	for k, v := range env {
		envCopy[k] = v
	}
	name := ""
	baseURL := envCopy["ANX_BASE_URL"]
	for i := 0; i+1 < len(args); i++ {
		switch args[i] {
		case "--as":
			name = args[i+1]
		case "--base-url":
			baseURL = args[i+1]
		}
	}
	fixtureDir := filepath.Join(home, ".test-derived-agents")
	if name == "" {
		entries, _ := os.ReadDir(fixtureDir)
		if len(entries) == 1 {
			name = strings.TrimSuffix(entries[0].Name(), ".json")
			envCopy["ANX_AS"] = name
		}
	}
	if name != "" {
		fixture := map[string]any{}
		if raw, err := os.ReadFile(filepath.Join(fixtureDir, name+".json")); err == nil {
			_ = json.Unmarshal(raw, &fixture)
		}
		if baseURL == "" {
			baseURL, _ = fixture["base_url"].(string)
		}
		if baseURL == "" {
			baseURL = config.DefaultBaseURL
		}
		if envCopy["ANX_BASE_URL"] == "" {
			envCopy["ANX_BASE_URL"] = baseURL
		}
		_, key, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		cfgDir := filepath.Join(home, ".config", "anx")
		host := hostidentity.Host{ID: "host-test", KeyID: "key-test", Slug: "host", WorkspaceID: "ws_test", BaseURL: baseURL}
		dir := hostidentity.DirAt(cfgDir, host.WorkspaceID, baseURL)
		if _, err := os.Stat(filepath.Join(dir, "host.json")); os.IsNotExist(err) {
			if err := hostidentity.SaveAt(cfgDir, host, key); err != nil {
				t.Fatal(err)
			}
		}
		agentID, _ := fixture["agent_id"].(string)
		if agentID == "" {
			agentID = name
		}
		actorID, _ := fixture["actor_id"].(string)
		handle, _ := fixture["username"].(string)
		if handle == "" {
			handle = name + ".host"
		}
		token, _ := fixture["access_token"].(string)
		if token == "" {
			token = "test-token"
		}
		cache := tokenCache{Token: token, ExpiresAt: time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC), Agent: map[string]any{"id": agentID, "actor_id": actorID, "handle": handle}}
		raw, _ := json.Marshal(cache)
		if err := os.WriteFile(filepath.Join(dir, "token-"+name+".json"), raw, 0600); err != nil {
			t.Fatal(err)
		}
	} else if envCopy["ANX_ACCESS_TOKEN"] == "" {
		envCopy["ANX_ACCESS_TOKEN"] = "test-token"
	}
	if stdin == nil {
		stdin = strings.NewReader("")
	}
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cli := New()
	cli.Stdout = stdout
	cli.Stderr = stderr
	cli.Stdin = stdin
	cli.StdinIsTTY = func() bool { return stdin == nil }
	cli.UserHomeDir = func() (string, error) { return home, nil }
	cli.ReadFile = os.ReadFile
	cli.Getenv = func(key string) string { return envCopy[key] }

	exitCode := cli.Run(args)
	if exitCode != 0 {
		if stdout.Len() == 0 {
			t.Fatalf("cli run failed: exit=%d stderr=%s", exitCode, stderr.String())
		}
	}
	return stdout.String()
}

func assertEnvelopeOK(t *testing.T, raw string) map[string]any {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatalf("decode envelope json: %v raw=%s", err, raw)
	}
	if payload["ok"] != true {
		t.Fatalf("expected ok=true payload=%#v", payload)
	}
	return payload
}

func assertEnvelopeError(t *testing.T, raw string) map[string]any {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatalf("decode envelope json: %v raw=%s", err, raw)
	}
	if payload["ok"] != false {
		t.Fatalf("expected ok=false payload=%#v", payload)
	}
	return payload
}

func anyStr(raw any) string { value, _ := raw.(string); return value }
