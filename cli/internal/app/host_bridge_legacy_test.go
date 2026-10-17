package app

import (
	"strings"
	"testing"
)

func TestLegacyBridgeUsageBeforeEnrollment(t *testing.T) {
	for _, args := range [][]string{
		{"--as", "worker", "bridge", "run", "--command-timeout", "0"},
		{"--as", "worker", "bridge", "run", "--command-timeout", "invalid"},
		{"--as", "worker", "bridge", "run", "--max-attempts", "21"},
		{"bridge", "run", "--command-timeout", "30m"},
	} {
		home := t.TempDir()
		a, out := workspaceTestApp(t, home, home, nil)
		if code := a.Run(args); code != 2 || strings.Contains(out.String(), "host_not_enrolled") {
			t.Fatalf("legacy usage reached enrollment: %v code=%d %s", args, code, out)
		}
	}
	home := t.TempDir()
	a, out := workspaceTestApp(t, home, home, nil)
	if code := a.Run([]string{"bridge", "run", "--help"}); code != 0 {
		t.Fatal(code, out)
	}
	for _, text := range []string{"--command-timeout", "--max-attempts", "rollback"} {
		if !strings.Contains(out.String(), text) {
			t.Fatal("missing rollback help", text, out)
		}
	}
}
