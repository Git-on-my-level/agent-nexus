package app

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"agent-nexus-cli/internal/integrations"
)

func prepareFailureFixture(t *testing.T) (*hookFixture, string, string) {
	t.Helper()
	f := newHookFixture(t)
	fixtureGit(t, f.root, "init", "-q")
	fixtureGit(t, f.root, "commit", "--allow-empty", "-m", "seed")
	f.run(t, "git", "enable", "--writer")
	fixtureGit(t, f.root, "commit", "--allow-empty", "-m", "Blocked progress\n\nAnx-Card: card:blocked\nAnx-Next: 2d Blocked promise")
	sha := fixtureGit(t, f.root, "rev-parse", "HEAD")
	fixtureGit(t, f.root, "commit", "--allow-empty", "-m", "Later progress\n\nAnx-Card: card:work\nAnx-Next: 3d Later promise")
	_, gd, e := integrations.Repo(context.Background(), f.root)
	if e != nil {
		t.Fatal(e)
	}
	return f, integrations.CapturePath(f.dir, gd), sha
}

func failPrepare(t *testing.T, f *hookFixture, path, sha, wantState string) integrations.CommitCapture {
	t.Helper()
	f.out.Reset()
	if code := f.a.Run([]string{"--json", "git", "sync"}); code == 0 {
		t.Fatal("prepare unexpectedly succeeded", f.out.String())
	}
	c, e := integrations.LoadCommit(path, sha)
	if e != nil || c.State != wantState || c.Attempts != 1 || c.FirstAttempt.IsZero() || !c.RetryAt.After(c.FirstAttempt) || c.Delivery != nil || !strings.HasPrefix(c.Reason, "prepare_") || len(c.Source) == 0 {
		t.Fatal(c, e)
	}
	s, e := integrations.LoadCapture(path)
	if e != nil || len(s.Pending) != 2 || s.Pending[0] != sha {
		t.Fatal(s, e)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.bodies) != 0 {
		t.Fatal("later promise overtook failed preparation", f.bodies)
	}
	return c
}

func TestGitCapturePrepareFailuresAreInspectableAndSkippable(t *testing.T) {
	for _, tc := range []struct {
		name      string
		status    int32
		malformed bool
		state     string
	}{
		{"forbidden", 403, false, "deferred"}, {"missing", 404, false, "deferred"},
		{"invalid", 422, false, "failed"}, {"unavailable", 503, false, "deferred"},
		{"timeout", -1, false, "deferred"}, {"local-validation", 0, true, "failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, path, sha := prepareFailureFixture(t)
			f.prepareStatus.Store(tc.status)
			f.malformedCard.Store(tc.malformed)
			before := failPrepare(t, f, path, sha, tc.state)
			status := f.run(t, "git", "status")
			if asMap(status["head"])["state"] != tc.state {
				t.Fatal(status)
			}
			f.run(t, "git", "sync", "--skip", sha)
			c, e := integrations.LoadCommit(path, sha)
			if e != nil || c.State != "skipped" || !bytes.Equal(c.Source, before.Source) || !c.FirstAttempt.Equal(before.FirstAttempt) {
				t.Fatal(c, e)
			}
			s, e := integrations.LoadCapture(path)
			if e != nil || len(s.Pending) != 0 || s.Confirmed != 1 {
				t.Fatal(s, e)
			}
			f.mu.Lock()
			if len(f.bodies) != 1 || !strings.Contains(f.bodies[0], "Later promise") {
				t.Fatal(f.bodies)
			}
			f.mu.Unlock()
			f.run(t, "git", "disable")
			f.run(t, "git", "enable", "--writer")
		})
	}
}

func TestGitCaptureDeferredPrepareRetriesWithBackoffAndExpires(t *testing.T) {
	for _, expire := range []bool{false, true} {
		t.Run(fmt.Sprint("expire=", expire), func(t *testing.T) {
			f, path, sha := prepareFailureFixture(t)
			now := time.Now().UTC()
			f.a.now = func() time.Time { return now }
			f.prepareStatus.Store(503)
			first := failPrepare(t, f, path, sha, "deferred")
			// Recreate the app and reread durable state, then prove an early tick does no I/O.
			restarted := *f.a
			f.a = &restarted
			calls := f.prepareCalls.Load()
			f.run(t, "git", "sync")
			if f.prepareCalls.Load() != calls {
				t.Fatal("prepare ignored retry_at")
			}
			if expire {
				now = first.FirstAttempt.Add(25 * 24 * time.Hour)
				f.out.Reset()
				if code := f.a.Run([]string{"--json", "git", "sync"}); code == 0 {
					t.Fatal("expired prepare succeeded")
				}
				c, e := integrations.LoadCommit(path, sha)
				if e != nil || c.State != "failed" || c.Reason != "replay_window_expired" || c.Attempts != 1 || f.prepareCalls.Load() != calls {
					t.Fatal(c, e)
				}
				f.run(t, "git", "sync", "--skip", sha)
			} else {
				now = first.RetryAt.Add(time.Millisecond)
				f.prepareStatus.Store(0)
				f.run(t, "git", "sync")
				c, e := integrations.LoadCommit(path, sha)
				if e != nil || c.State != "confirmed" || c.Attempts != 2 || !c.FirstAttempt.Equal(first.FirstAttempt) {
					t.Fatal(c, e)
				}
			}
			s, e := integrations.LoadCapture(path)
			if e != nil || len(s.Pending) != 0 {
				t.Fatal(s, e)
			}
		})
	}
}

func TestGitCaptureBackfillUsesCanonicalPerCardCheckpoint(t *testing.T) {
	f := newHookFixture(t)
	fixtureGit(t, f.root, "init", "-q")
	fixtureGit(t, f.root, "commit", "--allow-empty", "-m", "seed")
	seed := fixtureGit(t, f.root, "rev-parse", "HEAD")
	fixtureGit(t, f.root, "commit", "--allow-empty", "-m", "Older\n\nAnx-Card: card:alias\nAnx-Next: 1d Older promise")
	older := fixtureGit(t, f.root, "rev-parse", "HEAD")
	fixtureGit(t, f.root, "commit", "--allow-empty", "-m", "Other\n\nAnx-Card: card:other\nAnx-Next: 2d Other card promise")
	other := fixtureGit(t, f.root, "rev-parse", "HEAD")
	f.run(t, "git", "enable", "--writer", "--commit-url", "https://forge.example.test/repo/commit/{sha}")
	fixtureGit(t, f.root, "commit", "--allow-empty", "-m", "Checkpoint\n\nAnx-Card: card:work\nAnx-Next: 3d Confirmed checkpoint")
	checkpoint := fixtureGit(t, f.root, "rev-parse", "HEAD")
	f.run(t, "git", "sync")
	fixtureGit(t, f.root, "commit", "--allow-empty", "-m", "Newer\n\nAnx-Card: card:alias\nAnx-Next: 4d Newer promise")
	newer := fixtureGit(t, f.root, "rev-parse", "HEAD")
	f.run(t, "git", "sync", "--since", seed)
	_, gd, _ := integrations.Repo(context.Background(), f.root)
	path := integrations.CapturePath(f.dir, gd)
	old, e := integrations.LoadCommit(path, older)
	if e != nil || !old.Historical || old.Next != "" || old.CardID != "card-id" || old.Delivery == nil {
		t.Fatal(old, e)
	}
	oldPayload := asMap(asMap(old.Delivery.Body["event"])["payload"])
	if oldPayload["next"] != nil || oldPayload["evidence_refs"] != nil || !strings.Contains(anyString(oldPayload["text"]), older) {
		t.Fatal(oldPayload)
	}
	for _, sha := range []string{other, newer} {
		c, e := integrations.LoadCommit(path, sha)
		if e != nil || c.Historical || c.Next == "" || c.Delivery == nil {
			t.Fatal(c, e)
		}
		p := asMap(asMap(c.Delivery.Body["event"])["payload"])
		if p["next"] == nil || p["evidence_refs"] == nil {
			t.Fatal("newer/unrelated card claims lost", p)
		}
	}
	s, e := integrations.LoadCapture(path)
	if e != nil || s.PromiseCheckpoint["card-id"].SHA != newer || s.PromiseCheckpoint["other-id"].SHA != other || s.PromiseCheckpoint["card-id"].SHA == checkpoint {
		t.Fatal(s, e)
	}
}

func TestGitCaptureLegacyCheckpointBackfillFailsInspectably(t *testing.T) {
	f := newHookFixture(t)
	fixtureGit(t, f.root, "init", "-q")
	fixtureGit(t, f.root, "commit", "--allow-empty", "-m", "seed")
	seed := fixtureGit(t, f.root, "rev-parse", "HEAD")
	f.run(t, "git", "enable", "--writer")
	_, gd, _ := integrations.Repo(context.Background(), f.root)
	path := integrations.CapturePath(f.dir, gd)
	s, e := integrations.LoadCapture(path)
	if e != nil {
		t.Fatal(e)
	}
	s.PromiseCheckpoint["card:work"] = integrations.CaptureCheckpoint{Sequence: 3}
	if e = integrations.PrivateWrite(path, integrations.CaptureJSON(s)); e != nil {
		t.Fatal(e)
	}
	fixtureGit(t, f.root, "commit", "--allow-empty", "-m", "Backfill\n\nAnx-Card: card:work\nAnx-Next: 1d Promise")
	sha := fixtureGit(t, f.root, "rev-parse", "HEAD")
	if code := f.a.Run([]string{"--json", "git", "sync", "--since", seed}); code == 0 {
		t.Fatal("backfill guessed the order of a SHA-less checkpoint")
	}
	c, e := integrations.LoadCommit(path, sha)
	if e != nil || c.State != "failed" || c.Reason != "prepare_checkpoint_reconciliation_required" || c.FirstAttempt.IsZero() || c.RetryAt.IsZero() || c.Delivery != nil {
		t.Fatal(c, e)
	}
	f.run(t, "git", "sync", "--skip", sha)
	s, e = integrations.LoadCapture(path)
	if e != nil || len(s.Pending) != 0 {
		t.Fatal(s, e)
	}
}

func TestGitCaptureMismatchHookNamesWriterSafely(t *testing.T) {
	f := newHookFixture(t)
	fixtureGit(t, f.root, "init", "-q")
	fixtureGit(t, f.root, "commit", "--allow-empty", "-m", "seed")
	f.run(t, "git", "enable", "--writer")
	// A transient git failure here would yield an empty gitdir, the capture
	// would land under the wrong digest, and the hook would then no-op on the
	// missing capture: an empty-stderr failure with no diagnosis.
	_, gd, e := integrations.Repo(context.Background(), f.root)
	if e != nil {
		t.Fatal(e)
	}
	path := integrations.CapturePath(f.dir, gd)
	s, e := integrations.LoadCapture(path)
	if e != nil {
		t.Fatal(e)
	}
	s.Binding.Host = "elected-host\n\x1b[31m"
	s.Binding.Actor = "elected-actor"
	if e := integrations.PrivateWrite(path, integrations.CaptureJSON(s)); e != nil {
		t.Fatal(e)
	}
	stderr := &bytes.Buffer{}
	f.a.Stderr = stderr
	f.out.Reset()
	if code := f.a.Run([]string{"hook", "git.commit", "--harness", "git"}); code != 0 {
		t.Fatal(code)
	}
	got := stderr.String()
	if !strings.Contains(got, `host="elected-host\n\x1b[31m"`) || !strings.Contains(got, `actor="elected-actor"`) || strings.Contains(got, "\x1b") || strings.Count(got, "\n") != 1 || f.out.Len() != 0 {
		t.Fatal(got, f.out.String())
	}
}

func TestGitCaptureHookEnvironmentPreservesSentinelConfig(t *testing.T) {
	f := newHookFixture(t)
	sentinel := t.TempDir()
	fixtureGit(t, sentinel, "init", "-q")
	fixtureGit(t, sentinel, "commit", "--allow-empty", "-m", "sentinel")
	configPath := filepath.Join(sentinel, ".git", "config")
	before, e := os.ReadFile(configPath)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		after, e := os.ReadFile(configPath)
		if e != nil || !bytes.Equal(before, after) {
			t.Errorf("sentinel config changed: %v", e)
		}
		if bare := fixtureGit(t, sentinel, "config", "--local", "--get", "core.bare"); bare != "false" {
			t.Errorf("sentinel core.bare=%s", bare)
		}
	})
	t.Setenv("GIT_DIR", filepath.Join(sentinel, ".git"))
	t.Setenv("GIT_COMMON_DIR", filepath.Join(sentinel, ".git"))
	for _, name := range []string{"GIT_WORK_TREE", "GIT_INDEX_FILE"} {
		t.Setenv(name, "")
		if e := os.Unsetenv(name); e != nil {
			t.Fatal(e)
		}
	}
	// The init happens after poisoning, reproducing the original reinitialization hazard.
	fixtureGit(t, f.root, "init", "-q")
	fixtureGit(t, f.root, "commit", "--allow-empty", "-m", "seed")
	f.run(t, "git", "enable", "--writer")
	fixtureGit(t, f.root, "commit", "--allow-empty", "-m", "Progress\n\nAnx-Card: card:work")
	f.run(t, "git", "sync")
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.bodies) != 1 {
		t.Fatal(f.bodies)
	}
}

func TestGitPostCommitScriptUnsetsHookRepositoryVariables(t *testing.T) {
	root := t.TempDir()
	probe := filepath.Join(root, "probe")
	receipt := filepath.Join(root, "receipt")
	// The probe deliberately does not scrub its environment. Only the generated shell may do so.
	code := "#!/bin/sh\nfor name in GIT_DIR GIT_WORK_TREE GIT_INDEX_FILE GIT_COMMON_DIR; do\n eval 'test -z \"${'\"$name\"'+x}\"' || exit 19\ndone\nprintf ok > " + integrations.ShellQuote(receipt) + "\n"
	if e := os.WriteFile(probe, []byte(code), 0700); e != nil {
		t.Fatal(e)
	}
	path := filepath.Join(root, "post-commit")
	if e := os.WriteFile(path, []byte(gitPostCommitScript(probe, root, integrations.CaptureBinding{Agent: "worker", Workspace: "http://example.test"})), 0700); e != nil {
		t.Fatal(e)
	}
	cmd := exec.Command("/bin/sh", path)
	cmd.Env = append(integrations.GitEnvironment(os.Environ()), "GIT_DIR=/sentinel/git", "GIT_COMMON_DIR=/sentinel/common", "GIT_WORK_TREE=/sentinel/work", "GIT_INDEX_FILE=/sentinel/index")
	if out, e := cmd.CombinedOutput(); e != nil {
		t.Fatal(e, string(out))
	}
	if got, e := os.ReadFile(receipt); e != nil || string(got) != "ok" {
		t.Fatal("hook did not clear variables before invoking probe", string(got), e)
	}
}
