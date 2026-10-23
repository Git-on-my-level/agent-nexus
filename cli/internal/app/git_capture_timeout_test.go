package app

import (
	"context"
	"testing"
	"time"

	hostbridge "agent-nexus-cli/internal/bridge"
	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/integrations"
)

func TestGitCaptureRequestTimeouts(t *testing.T) {
	for _, mode := range []string{"sync-default", "sync-explicit", "sync-over-five-seconds", "hook", "bridge"} {
		t.Run(mode, func(t *testing.T) {
			f := newHookFixture(t)
			fixtureGit(t, f.root, "init", "-q")
			fixtureGit(t, f.root, "commit", "--allow-empty", "-m", "seed")
			f.run(t, "git", "enable", "--writer")
			fixtureGit(t, f.root, "commit", "--allow-empty", "-m", "Progress\n\nAnx-Card: card:work")
			// Longer than both captureAuth's former 1s and bridgeCfg's 2s.
			f.cardDelay.Store(int64(2200 * time.Millisecond))
			switch mode {
			case "sync-default":
				f.run(t, "git", "sync")
			case "sync-explicit":
				f.run(t, "--timeout", "4s", "git", "sync")
			case "sync-over-five-seconds":
				// Each card read exceeds the former whole-sync deadline, but
				// remains within the caller's per-request timeout.
				f.cardDelay.Store(int64(5500 * time.Millisecond))
				f.run(t, "--timeout", "8s", "git", "sync")
			case "hook":
				start := time.Now()
				// Even an explicit long timeout cannot slow the post-commit hook.
				if code := f.a.Run([]string{"--timeout", "4s", "hook", "git.commit", "--harness", "git"}); code != 0 {
					t.Fatal(code)
				}
				if elapsed := time.Since(start); elapsed > 2*time.Second {
					t.Fatalf("hook took %v", elapsed)
				}
			case "bridge":
				cfg := bridgeJobConfig(hostbridge.Workspace{BaseURL: f.server.URL, ConfigDir: f.dir}, hostbridge.Agent{Name: "worker", ActorID: "actor-worker"}, "git", 4*time.Second)
				if n, err := f.a.bridgeGitTick(context.Background(), cfg, false); err != nil || n != 1 {
					t.Fatal(n, err)
				}
			}
			_, gd, err := integrations.Repo(context.Background(), f.root)
			if err != nil {
				t.Fatal(err)
			}
			state, err := integrations.LoadCapture(integrations.CapturePath(f.dir, gd))
			if err != nil {
				t.Fatal(err)
			}
			f.mu.Lock()
			sends := len(f.bodies)
			f.mu.Unlock()
			if mode == "hook" {
				if sends != 0 || len(state.Pending) > 1 {
					t.Fatalf("slow hook state=%#v sends=%d", state, sends)
				}
				if len(state.Pending) == 1 {
					record, err := integrations.LoadCommit(integrations.CapturePath(f.dir, gd), state.Pending[0])
					if err != nil {
						t.Fatal(err)
					}
					if record.State != "deferred" || record.Reason != "prepare_uncertain" {
						t.Fatalf("%#v", record)
					}
				}
				// The hook budget may end during Git collection on a busy host,
				// before a pending record exists. Either way, explicit sync must
				// recover and deliver the original commit exactly once.
				// Advance this App's clock past the deferred entry's retry backoff.
				f.a.now = func() time.Time { return time.Now().Add(time.Minute) }
				f.run(t, "git", "sync")
				recovered, err := integrations.LoadCapture(integrations.CapturePath(f.dir, gd))
				if err != nil {
					t.Fatal(err)
				}
				f.mu.Lock()
				sends = len(f.bodies)
				f.mu.Unlock()
				if recovered.Confirmed != 1 || len(recovered.Pending) != 0 || sends != 1 {
					t.Fatalf("hook did not recover: state=%#v sends=%d", recovered, sends)
				}
			} else if sends != 1 || len(state.Pending) != 0 || state.Confirmed != 1 {
				t.Fatalf("sync state=%#v sends=%d", state, sends)
			}
		})
	}
}

func TestGitCaptureHonorsShortExplicitTimeout(t *testing.T) {
	f := newHookFixture(t)
	fixtureGit(t, f.root, "init", "-q")
	fixtureGit(t, f.root, "commit", "--allow-empty", "-m", "seed")
	f.run(t, "git", "enable", "--writer")
	fixtureGit(t, f.root, "commit", "--allow-empty", "-m", "Progress\n\nAnx-Card: card:work")
	f.cardDelay.Store(int64(time.Second))
	if code := f.a.Run([]string{"--json", "--timeout", "100ms", "git", "sync"}); code == 0 {
		t.Fatal("explicit timeout ignored")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.bodies) != 0 {
		t.Fatal("timed out prepare sent a note")
	}
}

func TestBridgeGitConfigPreservesNormalTimeout(t *testing.T) {
	for _, timeout := range []time.Duration{0, 4 * time.Second, config.DefaultTimeout} {
		cfg := bridgeJobConfig(hostbridge.Workspace{}, hostbridge.Agent{}, "git", timeout)
		want := timeout
		if want <= 0 {
			want = config.DefaultTimeout
		}
		if cfg.Timeout != want {
			t.Fatalf("got %v want %v", cfg.Timeout, want)
		}
	}
	if got := bridgeJobConfig(hostbridge.Workspace{}, hostbridge.Agent{}, "spool", time.Minute).Timeout; got != 2*time.Second {
		t.Fatalf("other job timeout changed: %v", got)
	}
}
