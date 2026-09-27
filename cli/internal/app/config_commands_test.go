package app

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

func runCLIForTestJSONError(t *testing.T, home string, env map[string]string, args []string) string {
	t.Helper()
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cli := New()
	cli.Stdout = stdout
	cli.Stderr = stderr
	cli.Stdin = strings.NewReader("")
	cli.StdinIsTTY = func() bool { return false }
	cli.UserHomeDir = func() (string, error) { return home, nil }
	cli.ReadFile = os.ReadFile
	cli.Getenv = func(key string) string { return env[key] }
	exit := cli.Run(args)
	if exit == 0 {
		t.Fatalf("expected non-zero exit, got 0 stdout=%s stderr=%s", stdout.String(), stderr.String())
	}
	return stdout.String()
}
