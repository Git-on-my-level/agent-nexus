package observation

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestSSHBuildsFixedCommandAndRejectsUnapprovedTargets(t *testing.T) {
	cfg := SSHConfig{WorkspaceID: "w", ConnectionID: "c", Host: "reader@example.test", Repository: "/srv/approved repo", KnownHostsFile: "/etc/ssh/ssh_known_hosts", Timeout: time.Second, MaxBytes: 4096}
	reader, err := NewSSHGitReader(cfg)
	if err != nil {
		t.Fatal(err)
	}
	target := Target{WorkspaceID: "w", ConnectionID: "c", Source: "ssh_git", Kind: "repository", NativeID: "repo", Host: cfg.Host, Path: cfg.Repository}
	args, err := reader.arguments(target)
	if err != nil {
		t.Fatal(err)
	}
	text := strings.Join(args, " ")
	for _, required := range []string{"StrictHostKeyChecking=yes", "BatchMode=yes", "ClearAllForwardings=yes", "ForwardAgent=no", "-F /dev/null", "'/srv/approved repo'", "core.fsmonitor=false"} {
		if !strings.Contains(text, required) {
			t.Errorf("missing %s: %s", required, text)
		}
	}
	target.Path = "/srv/other"
	if _, err := reader.Read(context.Background(), target); err == nil {
		t.Fatal("unapproved repository accepted")
	}
	cfg.Host = "-oProxyCommand=fixture"
	if _, err := NewSSHGitReader(cfg); err == nil {
		t.Fatal("SSH option accepted as host")
	}
	cfg.Host = "reader@example.test"
	cfg.Repository = "/srv/repo\nfixture"
	if _, err := NewSSHGitReader(cfg); err == nil {
		t.Fatal("control character path accepted")
	}
}
func TestSSHOutputIsGitEvidenceNotDeploymentProof(t *testing.T) {
	target := Target{WorkspaceID: "w", ConnectionID: "c", Source: "ssh_git", Kind: "repository", NativeID: "repo", Host: "reader@example.test", Path: "/srv/repo"}
	raw := strings.Repeat("a", 40) + "\n2026-09-07T00:00:00+00:00\n M fixture.txt\n"
	report, err := parseGitOutput(target, []byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if report.NativeStatus != "" || report.Facts["working_tree_dirty"] != true || report.SourceRevision != strings.Repeat("a", 40) || report.Knowledge != "reported" {
		t.Fatalf("wrong git evidence: %+v", report)
	}
	if _, err := parseGitOutput(target, []byte("not a revision")); err == nil {
		t.Fatal("malformed git evidence accepted")
	}
}
