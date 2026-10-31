package integrations

import (
	"bytes"
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

func testGit(t *testing.T, root string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", root, "-c", "core.hooksPath=/dev/null", "-c", "user.name=Fixture", "-c", "user.email=fixture@example.test"}, args...)...)
	cmd.Env = append(GitEnvironment(os.Environ()), "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1", "GIT_COMMITTER_DATE=2026-10-17T00:00:00Z", "GIT_AUTHOR_DATE=2026-10-17T00:00:00Z")
	b, e := cmd.CombinedOutput()
	if e != nil {
		t.Fatalf("git %v: %v %s", args, e, b)
	}
	return strings.TrimSpace(string(b))
}
func TestHookWireAndResolution(t *testing.T) {
	for _, h := range []string{"generic", "claude-code", "codex", "cursor"} {
		t.Run(h, func(t *testing.T) {
			p := map[string]any{"schema_version": 1, "cwd": "/repo", "session_id": "local", "hook_event_name": "SessionStart", "source": "compact", "transcript_path": "MUST NOT READ"}
			if h == "cursor" {
				p["hook_event_name"] = "sessionStart"
				delete(p, "cwd")
				p["workspace_roots"] = []string{"/repo"}
			}
			raw, _ := json.Marshal(p)
			e, err := ParseHook(h, "session.start", raw)
			if err != nil || e.CWD != "/repo" {
				t.Fatal(e, err)
			}
			b := HookBrief{Version: 1, Context: "ANX card context"}
			out := string(RenderHook(h, b))
			key := "additionalContext"
			if h == "cursor" {
				key = "additional_context"
			}
			if h == "generic" {
				key = "context"
			}
			if !strings.Contains(out, key) || strings.Contains(out, "transcript") {
				t.Fatal(out)
			}
		})
	}
	if _, e := ParseHook("cursor", "session.start", []byte(`{"hook_event_name":"sessionStart","workspace_roots":["/a","/b"]}`)); e == nil {
		t.Fatal("ambiguous roots accepted")
	}
	s := HookSettings{Bindings: []HookBinding{{Workspace: "ws", Actor: "actor", Session: SessionKey("generic", "s"), Card: "card:bound"}}, Rules: []HookRule{{Workspace: "ws", Actor: "actor", Directory: "/repo", Card: "card:parent"}, {Workspace: "ws", Actor: "actor", Directory: "/repo/sub", Card: "card:child"}}}
	i := HookInstallation{Harness: "generic", Card: "card:default"}
	e := HookEvent{CWD: "/repo/sub", Session: "s", Card: "card:explicit"}
	for _, want := range []string{"card:explicit", "card:bound", "card:default", "card:child"} {
		got, err := ResolveHookCard(s, i, e, "ws", "actor")
		if err != nil || got != want {
			t.Fatal(got, want, err)
		}
		switch want {
		case "card:explicit":
			e.Card = ""
		case "card:bound":
			e.Session = ""
		case "card:default":
			i.Card = ""
		}
	}
	if got, _ := ResolveHookCard(s, i, e, "other", "actor"); got != "" {
		t.Fatal("cross-workspace target", got)
	}
}
func TestHookAdapterPreservesSettings(t *testing.T) {
	for _, h := range []string{"claude-code", "codex", "cursor"} {
		t.Run(h, func(t *testing.T) {
			raw := []byte(`{"unrelated":{"large":9007199254740993},"hooks":{"Other":[]}}`)
			command := "'/bin/anx' --config-dir '/private' hook session.start --installation abc"
			next, changed, e := MergeHook(raw, h, "context.inject", command, "", true)
			if e != nil || !changed {
				t.Fatal(e)
			}
			again, changed, e := MergeHook(next, h, "context.inject", command, "", true)
			if e != nil || changed || string(again) != string(next) {
				t.Fatal("not idempotent", e)
			}
			removed, _, e := MergeHook(next, h, "context.inject", command, "", false)
			if e != nil || strings.Contains(string(removed), command) || !strings.Contains(string(removed), "9007199254740993") {
				t.Fatal(string(removed), e)
			}
			if _, _, e = MergeHook([]byte(`{"hooks":{},"hooks":{}}`), h, "context.inject", command, "", true); e == nil {
				t.Fatal("duplicate key accepted")
			}
		})
	}
}
func TestGitTrailerParsingAndBoundedWalk(t *testing.T) {
	root := t.TempDir()
	testGit(t, root, "init", "-q")
	testGit(t, root, "commit", "--allow-empty", "-m", "seed")
	anchor := testGit(t, root, "rev-parse", "HEAD")
	ctx := context.Background()
	cases := []struct{ message, reason string }{
		{"subject\n\nAnx-Card: card:one\nAnx-Note: progress\nAnx-Next: 2d Review behavior", ""},
		{"body contains Anx-Card: card:ignored in prose", "skipped"},
		{"s\n\nAnx-Card: card:one\nAnx-Card: card:two", "duplicate_trailer"},
		{"s\n\nAnx-Card: card:one\nAnx-Status: done", "unsupported_trailer"},
		{"s\n\nAnx-Card: card:one\nAnx-Next: 2d", "invalid_next"},
		{"s\n\nAnx-Card: card:one\nAnx-Note: first\n continuation", "multiline_trailer"},
		{"s\n\nAnx-Card: bare", "invalid_card"},
	}
	for _, tc := range cases {
		testGit(t, root, "commit", "--allow-empty", "-m", tc.message)
		sha := testGit(t, root, "rev-parse", "HEAD")
		c, e := ParseCommit(ctx, root, sha)
		if tc.reason == "" {
			if e != nil || c.Next != "2d" || c.At.IsZero() {
				t.Fatal(c, e)
			}
		} else if tc.reason == "skipped" {
			if e != nil || c.State != "skipped" {
				t.Fatal(c, e)
			}
		} else if e == nil || !strings.Contains(e.Error(), tc.reason) {
			t.Fatal(tc.reason, c, e)
		}
	}
	s := CaptureState{Root: root, Anchor: anchor}
	if e := Collect(ctx, &s); e != nil {
		t.Fatal(e)
	}
	if !s.Collected || len(s.Walk) != len(cases) {
		t.Fatal(s)
	}
	// Rewritten/disconnected histories must not silently advance a cursor.
	testGit(t, root, "checkout", "--orphan", "rewrite")
	testGit(t, root, "commit", "--allow-empty", "-m", "new root")
	s = CaptureState{Root: root, Anchor: anchor}
	if e := Collect(ctx, &s); e == nil || !strings.Contains(e.Error(), "divergent_history") {
		t.Fatal(e)
	}
}
func TestPrivateCaptureNeverEvictsOrFollowsSymlink(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "private.json")
	if e := os.WriteFile(path, []byte("existing"), 0644); e != nil {
		t.Fatal(e)
	}
	if e := PrivateWrite(path, []byte("secret")); e == nil {
		t.Fatal("permissive file accepted")
	}
	b, _ := os.ReadFile(path)
	if string(b) != "existing" {
		t.Fatal(string(b))
	}
	link := filepath.Join(dir, "link")
	if e := os.Symlink(path, link); e != nil {
		t.Fatal(e)
	}
	if _, e := PrivateRead(link); e == nil {
		t.Fatal("symlink read")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	unlock, e := Lock(context.Background(), filepath.Join(dir, "held"))
	if e != nil {
		t.Fatal(e)
	}
	defer unlock()
	if _, e = Lock(ctx, filepath.Join(dir, "held")); e == nil {
		t.Fatal("deadline not honored")
	}
}

func TestGitFrozenWalkPagesAndMergeOrder(t *testing.T) {
	root := t.TempDir()
	testGit(t, root, "init", "-q")
	testGit(t, root, "commit", "--allow-empty", "-m", "seed")
	anchor := testGit(t, root, "rev-parse", "HEAD")
	for n := 0; n < WalkPage+1; n++ {
		testGit(t, root, "commit", "--allow-empty", "-m", "progress")
	}
	s := CaptureState{Root: root, Anchor: anchor}
	if e := Collect(context.Background(), &s); e != nil {
		t.Fatal(e)
	}
	frozen := s.Snapshot
	if s.Collected || len(s.Walk) != WalkPage {
		t.Fatal(s)
	}
	testGit(t, root, "commit", "--allow-empty", "-m", "later")
	if e := Collect(context.Background(), &s); e != nil {
		t.Fatal(e)
	}
	if !s.Collected || len(s.Walk) != WalkPage+1 || s.Snapshot != frozen || s.Walk[0] != frozen {
		t.Fatal(s)
	}
	seen := map[string]bool{}
	for _, sha := range s.Walk {
		if seen[sha] {
			t.Fatal("duplicate walk position")
		}
		seen[sha] = true
	}
	// A merge has both parents before its own position when consumed oldest-first.
	testGit(t, root, "checkout", "-b", "side", anchor)
	testGit(t, root, "commit", "--allow-empty", "-m", "side")
	side := testGit(t, root, "rev-parse", "HEAD")
	testGit(t, root, "checkout", "-b", "merge", frozen)
	testGit(t, root, "merge", "--no-ff", "-m", "merge", side)
	s = CaptureState{Root: root, Anchor: frozen}
	if e := Collect(context.Background(), &s); e != nil {
		t.Fatal(e)
	}
	if len(s.Walk) != 2 || s.Walk[1] != side {
		t.Fatal(s.Walk)
	}
}

func TestGitHookEnvironmentCannotSelectAnotherRepository(t *testing.T) {
	foreign, target := t.TempDir(), t.TempDir()
	testGit(t, foreign, "init", "-q")
	testGit(t, foreign, "commit", "--allow-empty", "-m", "seed")
	configPath := filepath.Join(foreign, ".git", "config")
	configBefore, e := os.ReadFile(configPath)
	if e != nil {
		t.Fatal(e)
	}
	if bare := testGit(t, foreign, "config", "--local", "--get", "core.bare"); bare != "false" {
		t.Fatal(bare)
	}
	t.Cleanup(func() {
		after, e := os.ReadFile(configPath)
		if e != nil || !bytes.Equal(configBefore, after) {
			t.Errorf("sentinel config changed: %v", e)
		}
		if bare := testGit(t, foreign, "config", "--local", "--get", "core.bare"); bare != "false" {
			t.Errorf("sentinel core.bare=%s", bare)
		}
	})
	before := testGit(t, foreign, "rev-parse", "HEAD")
	t.Setenv("GIT_DIR", filepath.Join(foreign, ".git"))
	t.Setenv("GIT_COMMON_DIR", filepath.Join(foreign, ".git"))
	t.Setenv("GIT_WORK_TREE", foreign)
	t.Setenv("GIT_INDEX_FILE", filepath.Join(foreign, ".git", "index"))
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "core.bare")
	t.Setenv("GIT_CONFIG_VALUE_0", "true")
	testGit(t, target, "init", "-q")
	testGit(t, target, "commit", "--allow-empty", "-m", "Target\n\nAnx-Card: card:target")
	root, gd, e := Repo(context.Background(), target)
	want, _ := filepath.EvalSymlinks(target)
	if e != nil || root != want || gd != filepath.Join(want, ".git") {
		t.Fatal(root, gd, e)
	}
	sha, e := ResolveCommit(context.Background(), target, "HEAD")
	if e != nil {
		t.Fatal(e)
	}
	c, e := ParseCommit(context.Background(), target, sha)
	if e != nil || c.Card != "card:target" {
		t.Fatal(c, e)
	}
	if got := testGit(t, foreign, "rev-parse", "HEAD"); got != before {
		t.Fatal("foreign repo was modified", before, got)
	}
}

func TestCaptureLoadsLegacyCheckpointWithoutLosingPendingWork(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	sha := strings.Repeat("a", 40)
	state := map[string]any{"schema_version": 1, "binding": map[string]any{"repo_uuid": "repo"}, "pending": []string{sha}, "promise_checkpoint": map[string]any{"card:old": 3}}
	if e := PrivateWrite(path, JSON(state)); e != nil {
		t.Fatal(e)
	}
	s, e := LoadCapture(path)
	if e != nil || !s.LegacyCheckpoint || s.PromiseCheckpoint["card:old"].Sequence != 3 || len(s.Pending) != 1 || s.Pending[0] != sha {
		t.Fatal(s, e)
	}
	if e = PrivateWrite(path, CaptureJSON(s)); e != nil {
		t.Fatal(e)
	}
	s, e = LoadCapture(path)
	if e != nil || !s.LegacyCheckpoint || len(s.Pending) != 1 {
		t.Fatal(s, e)
	}
}

func TestDrainSelectedLeavesOtherWorkspaceRequestsUntouched(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "queue.json")
	now := time.Now()
	other := Pending{Event: Event{Key: "other", Timestamp: now}, Delivery: &Delivery{Key: "other", BaseURL: "http://other.test", Agent: "other", Body: map[string]any{"title": "retain me"}}}
	own := Pending{Event: Event{Key: "own", Timestamp: now}, Delivery: &Delivery{Key: "own", BaseURL: "http://own.test", Agent: "worker", Body: map[string]any{"title": "send me"}}}
	q := Queue{Pending: []Pending{other, own}, Ledger: []Receipt{}}
	if e := AtomicWrite(path, JSON(q)); e != nil {
		t.Fatal(e)
	}
	sent := []string{}
	e := DrainSelected(ctx, path, func(Event) (Delivery, error) { t.Fatal("frozen request was prepared again"); return Delivery{}, nil }, func(_ context.Context, d Delivery) error { sent = append(sent, d.Key); return nil }, func(p Pending) bool { return p.Delivery.BaseURL == "http://own.test" })
	if e != nil {
		t.Fatal(e)
	}
	q, e = LoadQueue(path)
	if e != nil || len(sent) != 1 || sent[0] != "own" || len(q.Pending) != 1 || q.Pending[0].Delivery.Key != "other" || q.Pending[0].Delivery.Body["title"] != "retain me" {
		t.Fatal(q, e, sent)
	}
}

func TestDrainSelectedPreservesExpiredForeignWorkAndDefersExpiredOwnedWork(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fixture.spool.json")
	old := time.Now().Add(-31 * 24 * time.Hour)
	q := Queue{Pending: []Pending{{Event: Event{Key: "foreign", Timestamp: old}, Reason: "foreign-reason"}, {Event: Event{Key: "owned", Timestamp: old}, Reason: "pending"}}, Ledger: []Receipt{}}
	if e := AtomicWrite(path, JSON(q)); e != nil {
		t.Fatal(e)
	}
	called := false
	e := DrainSelected(context.Background(), path, func(Event) (Delivery, error) { called = true; return Delivery{}, nil }, func(context.Context, Delivery) error { called = true; return nil }, func(p Pending) bool { return p.Event.Key == "owned" })
	if e == nil || called {
		t.Fatal("expired background work replayed", called, e)
	}
	got, e := LoadQueue(path)
	if e != nil || len(got.Pending) != 2 || got.Pending[0].Reason != "foreign-reason" || got.Pending[1].Reason != "replay_window_expired" || got.Dropped != 0 {
		t.Fatal(got, e)
	}
}

func TestDrainSelectedStopsBeforeReceiptEviction(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fixture.spool.json")
	q := Queue{Pending: []Pending{}, Ledger: make([]Receipt, MaxLedger-1)}
	for i := range q.Ledger {
		q.Ledger[i] = Receipt{Key: fmt.Sprint(i), At: time.Now()}
	}
	for _, key := range []string{"one", "two", "three"} {
		q.Pending = append(q.Pending, Pending{Event: Event{Key: key, Timestamp: time.Now()}, Delivery: &Delivery{Key: key}})
	}
	if e := AtomicWrite(path, JSON(q)); e != nil {
		t.Fatal(e)
	}
	calls := 0
	_ = DrainSelected(context.Background(), path, func(Event) (Delivery, error) { t.Fatal("already frozen"); return Delivery{}, nil }, func(context.Context, Delivery) error { calls++; return nil }, func(Pending) bool { return true })
	got, e := LoadQueue(path)
	if e != nil || calls != 1 || len(got.Ledger) != MaxLedger || got.Ledger[0].Key != "0" || len(got.Pending) != 2 {
		t.Fatal(calls, got, e)
	}
}

// The harness must outwait the hook's own budget, or it kills a hook about to answer.
func TestMergeHookGivesContextLongerThanCapture(t *testing.T) {
	for _, tc := range []struct {
		harness, recipe string
		want            string
	}{
		{"claude-code", "context.inject", `"timeout": 10`},
		{"claude-code", "git.capture", `"timeout": 2`},
		{"cursor", "context.inject", `"timeout": 10`},
		{"cursor", "git.capture", `"timeout": 2`},
	} {
		got, _, e := MergeHook([]byte(`{}`), tc.harness, tc.recipe, "anx hook", "", true)
		if e != nil || !bytes.Contains(got, []byte(tc.want)) {
			t.Fatalf("%s %s: %v %s", tc.harness, tc.recipe, e, got)
		}
	}
}
