//go:build integration

package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAuthAdminAgentHeadlessFleetCLI(t *testing.T) {
	h := newLiveCoreHarness(t)
	h.enrollHost(t, "fleet")
	who := h.runCLIExpectOK(t, "fleet", nil, "auth", "whoami")
	agentID := mustStringPath(t, who.Payload, "result.agent.agent_id")
	h.humanTokens["human-admin"] = h.adminToken
	denied := h.runCLI(t, "fleet", nil, "host", "tokens", "create", "--label", "fleet", "--expires-in", "1h")
	if denied.ExitCode != 5 || mustStringPath(t, denied.Payload, "error.code") != "auth_admin_required" {
		t.Fatalf("expected denied capability: %s", denied.Stdout)
	}
	h.runCLIExpectOK(t, "human-admin", nil, "auth", "admins", "grant", agentID)
	h.runCLIExpectOK(t, "fleet", nil, "auth", "admins", "list")
	h.runCLIExpectOK(t, "fleet", nil, "host", "enrollments", "list")
	grant := h.runCLIExpectOK(t, "fleet", nil, "host", "tokens", "create", "--label", "host-b", "--expires-in", "10m")
	secret := mustStringPath(t, grant.Payload, "result.token")
	if strings.Count(grant.Stdout, secret) != 1 || strings.Contains(grant.Stderr, secret) {
		t.Fatal("create secret repeated or logged")
	}
	listed := h.runCLIExpectOK(t, "fleet", nil, "host", "tokens", "list")
	if strings.Contains(listed.Stdout, secret) {
		t.Fatal("token list exposed secret")
	}
	// Simulate transfer to another machine's stdin; secret never enters argv or env.
	secondHome := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, h.cliBin, "--json", "--base-url", h.baseURL, "host", "enroll", "--name", "host-b", "--token-stdin")
	cmd.Env = append(os.Environ(), "HOME="+secondHome, "XDG_CONFIG_HOME="+filepath.Join(secondHome, ".config"), "ANX_ACCESS_TOKEN=", "ANX_CONFIG_DIR=", "AGENTCTL_EXECUTION_ID=", "AGENTCTL_ADAPTER=", "AGENTCTL_HOST_ID=")
	cmd.Stdin = strings.NewReader(secret + "\n")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("stdin enrollment failed: %v %s %s", err, stdout.String(), stderr.String())
	}
	if strings.Contains(stdout.String(), secret) || strings.Contains(stderr.String(), secret) {
		t.Fatal("enrollment exposed secret")
	}
	var enrolled map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &enrolled); err != nil {
		t.Fatal(err)
	}
	hostB := mustStringPath(t, enrolled, "result.host.id")
	self := h.runCLI(t, "fleet", nil, "host", "revoke", "integration-host")
	if self.ExitCode != 5 || mustStringPath(t, self.Payload, "error.code") != "host_self_revoke" {
		t.Fatalf("self-host guard failed: %s", self.Stdout)
	}
	h.runCLIExpectOK(t, "fleet", nil, "host", "revoke", hostB)
	h.runCLIExpectOK(t, "human-admin", nil, "auth", "admins", "revoke", agentID)
	denied = h.runCLI(t, "fleet", nil, "host", "tokens", "create", "--label", "after-revoke", "--expires-in", "1h")
	if denied.ExitCode != 5 {
		t.Fatalf("revocation did not take effect: %s", denied.Stdout)
	}
	h.runCLIExpectOK(t, "fleet", nil, "host", "list")
	logs, err := os.ReadFile(h.logPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(logs), secret) {
		t.Fatal("core logged enrollment token")
	}
}
