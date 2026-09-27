package app

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"

	"agent-nexus-cli/internal/profile"
)

func runCLIForTest(t *testing.T, home string, env map[string]string, stdin io.Reader, args []string) string {
	t.Helper()
	envCopy := map[string]string{}
	for k, v := range env {
		envCopy[k] = v
	}
	for i := 0; i+1 < len(args); i++ {
		if args[i] == "--as" {
			path := profile.ProfilePath(home, args[i+1])
			if _, err := os.Stat(path); err == nil {
				envCopy["ANX_PROFILE_PATH"] = path
			}
			break
		}
	}
	if envCopy["ANX_PROFILE_PATH"] == "" {
		names, _ := profile.ListAgents(home)
		if len(names) == 1 {
			envCopy["ANX_PROFILE_PATH"] = profile.ProfilePath(home, names[0])
		}
	}
	if envCopy["ANX_ACCESS_TOKEN"] == "" && envCopy["ANX_PROFILE_PATH"] == "" {
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
