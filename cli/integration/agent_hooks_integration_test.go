//go:build integration

package integration

import (
	"agent-nexus-cli/internal/integrations"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Uses only a fresh local core, temporary HOME/keys and disposable Git history.
// The post-commit callback and explicit sync deliberately overlap.
func TestAgentHooksGitNotesAndPromisesAgainstCore(t *testing.T) {
	h := newPasskeyLiveCoreHarness(t)
	h.enrollHost(t, "worker")
	created := h.runCLIExpectOK(t, "worker", map[string]any{"title": "Hook capture acceptance", "handle": "hook-capture", "source": map[string]any{"authority": "nexus"}, "definition_of_done": []string{"Review evidence"}}, "work", "create", "--from-file", "-")
	ref := mustStringPath(t, created.Payload, "result.work.ref")
	root := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", root, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.test"}, args...)...)
		cmd.Env = append(os.Environ(), "HOME="+h.homeDir, "ANX_ACCESS_TOKEN=", "ANX_SKILLS_AUTO_SYNC=0", "ANX_UPDATE_POLICY=off", "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1")
		out, e := cmd.CombinedOutput()
		if e != nil {
			t.Fatal(args, e, string(out))
		}
		return strings.TrimSpace(string(out))
	}
	git("init", "-q")
	git("commit", "--allow-empty", "-m", "seed")
	dry := h.runCLIExpectOK(t, "worker", nil, "git", "enable", root, "--writer", "--install-hook", "--dry-run")
	if head, _ := getPathValue(dry.Payload, "result.head"); head == nil || head == "" {
		t.Fatal(dry.Stdout)
	}
	h.runCLIExpectOK(t, "worker", nil, "git", "enable", root, "--writer", "--install-hook")
	for _, what := range []string{"Review first capture", "Review second capture"} {
		git("commit", "--allow-empty", "-m", fmt.Sprintf("Progress\n\nAnx-Card: %s\nAnx-Note: %s\nAnx-Next: 2d %s", ref, what, what))
	}
	automatic := h.runCLIExpectOK(t, "worker", nil, "git", "status", root)
	if confirmed, _ := getPathValue(automatic.Payload, "result.confirmed"); confirmed != float64(2) {
		t.Fatal("post-commit hook did not deliver both notes", automatic.Stdout)
	}
	for n := 0; n < 3; n++ {
		h.runCLIExpectOK(t, "worker", nil, "git", "sync", root)
	}
	status := h.runCLIExpectOK(t, "worker", nil, "git", "status", root)
	if pending, _ := getPathValue(status.Payload, "result.pending"); pending != float64(0) {
		t.Fatal(status.Stdout)
	}
	// Model a crash after core committed but before the local receipt was saved.
	// The frozen request must replay against the real server without another note.
	_, gd, e := integrations.Repo(context.Background(), root)
	if e != nil {
		t.Fatal(e)
	}
	path := integrations.CapturePath(filepath.Join(h.homeDir, ".config", "anx"), gd)
	s, e := integrations.LoadCapture(path)
	if e != nil {
		t.Fatal(e)
	}
	sha := git("rev-parse", "HEAD")
	c, e := integrations.LoadCommit(path, sha)
	if e != nil {
		t.Fatal(e)
	}
	c.State = "pending"
	s.Pending = []string{sha}
	if e = integrations.SaveCommit(path, c); e != nil {
		t.Fatal(e)
	}
	if e = integrations.PrivateWrite(path, integrations.CaptureJSON(s)); e != nil {
		t.Fatal(e)
	}
	h.runCLIExpectOK(t, "worker", nil, "git", "sync", root)
	events := h.runCLIExpectOK(t, "worker", nil, "debug", "events", "list", "--type", "message_posted", "--max-events", "100")
	raw, ok := getPathValue(events.Payload, "result.events")
	if !ok {
		t.Fatal(events.Stdout)
	}
	count := 0
	for _, v := range raw.([]any) {
		e := v.(map[string]any)
		p, _ := e["payload"].(map[string]any)
		text, _ := p["text"].(string)
		if strings.Contains(text, "Commit:") {
			count++
			next, _ := p["next"].(map[string]any)
			at, _ := next["at"].(string)
			if _, err := time.Parse(time.RFC3339Nano, at); err != nil {
				t.Fatal("promise not normalized", p)
			}
		}
	}
	if count != 2 {
		t.Fatalf("want two unique commit notes, got %d: %s", count, events.Stdout)
	}
	plan := h.runCLIExpectOK(t, "worker", nil, "plan", "show", ref)
	if what := mustStringPath(t, plan.Payload, "result.plan_health.promise.what"); what != "Review second capture" {
		t.Fatal(what, plan.Stdout)
	}
	// Another author's earlier promise remains the counted promise; a later Git
	// note must not introduce global last-write-wins.
	h.runCLIExpectOK(t, "other", nil, "note", ref, "Independent checkpoint", "--next", "1d", "Other author checkpoint")
	git("commit", "--allow-empty", "-m", fmt.Sprintf("Later change\n\nAnx-Card: %s\nAnx-Next: 3d Writer checkpoint", ref))
	h.runCLIExpectOK(t, "worker", nil, "git", "sync", root)
	plan = h.runCLIExpectOK(t, "worker", nil, "plan", "show", ref)
	if what := mustStringPath(t, plan.Payload, "result.plan_health.promise.what"); what != "Other author checkpoint" {
		t.Fatal(what, plan.Stdout)
	}
	h.runCLIExpectOK(t, "worker", nil, "integrations", "enable", "context.inject", "--harness", "generic", "--scope", "user", "--card-id", ref)
	payload := map[string]any{"schema_version": 1, "cwd": root, "session_id": "acceptance-session"}
	hook := h.runCLI(t, "worker", payload, "hook", "session.start")
	var brief struct {
		Context string `json:"context"`
	}
	if e := json.Unmarshal([]byte(hook.Stdout), &brief); e != nil || !strings.Contains(brief.Context, "Hook capture acceptance") || !strings.Contains(brief.Context, "Other author checkpoint") {
		t.Log("manual context", h.runCLI(t, "worker", nil, "context", ref, "--limit", "10"))
		t.Log("manual plan", h.runCLI(t, "worker", nil, "plan", "show", ref))
		t.Fatal("context did not include actual scoped card/promise", hook.Stdout, hook.Stderr, e)
	}
	h.runCLIExpectOK(t, "worker", nil, "git", "disable", root)
	if _, e := os.Stat(filepath.Join(root, ".git", "hooks", "post-commit")); !os.IsNotExist(e) {
		t.Fatal("owned hook not removed", e)
	}
}
