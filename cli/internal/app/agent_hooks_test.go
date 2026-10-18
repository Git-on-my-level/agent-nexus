package app

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/hostidentity"
	"agent-nexus-cli/internal/integrations"
)

type hookFixture struct {
	a                    *App
	out                  *bytes.Buffer
	home, dir, root      string
	server               *httptest.Server
	mu                   sync.Mutex
	bodies               []string
	failFirst            bool
	status               atomic.Int32
	handshakeUnsupported atomic.Bool
	prepareStatus        atomic.Int32
	prepareCalls         atomic.Int32
	malformedCard        atomic.Bool
	readGate             chan struct{}
	readStarted          chan struct{}
	calls                atomic.Int32
	identityDenied       atomic.Bool
	filePlan             atomic.Bool
	fileCardStatus       atomic.Int32
	fileSendStatus       atomic.Int32
	briefStatus          atomic.Int32
	briefDelay           atomic.Bool
	cardDelay            atomic.Int64
}

func newHookFixture(t *testing.T) *hookFixture {
	t.Helper()
	f := &hookFixture{home: t.TempDir(), root: t.TempDir()}
	f.dir = filepath.Join(f.home, ".config", "anx")
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.calls.Add(1)
		if strings.HasPrefix(r.URL.Path, "/work/") || strings.HasSuffix(r.URL.Path, "/plan") {
			f.mu.Lock()
			gate, started := f.readGate, f.readStarted
			f.mu.Unlock()
			if gate != nil {
				started <- struct{}{}
				select {
				case <-gate:
				case <-r.Context().Done():
					return
				}
			}
		}
		if r.Method == "GET" && strings.HasPrefix(r.URL.Path, "/cards/") && !strings.HasSuffix(r.URL.Path, "/plan") {
			if delay := f.cardDelay.Load(); delay > 0 {
				timer := time.NewTimer(time.Duration(delay))
				defer timer.Stop()
				select {
				case <-timer.C:
				case <-r.Context().Done():
					return
				}
			}
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/auth/token":
			var grant map[string]any
			_ = json.NewDecoder(r.Body).Decode(&grant)
			agent := anyString(grant["agent_name"])
			fmt.Fprintf(w, `{"agent":{"id":%q,"actor_id":%q,"handle":%q},"tokens":{"access_token":"fixture-token","expires_in":3600}}`, agent, "actor-"+agent, agent+".fixture")
		case r.URL.Path == "/agents/me":
			if f.identityDenied.Load() {
				w.WriteHeader(403)
				fmt.Fprint(w, `{"error":{"code":"host_revoked","message":"fixture revoked"}}`)
				return
			}
			fmt.Fprint(w, `{"agent":{"id":"worker","actor_id":"actor-worker"}}`)
		case r.URL.Path == "/brief":
			if f.briefDelay.Load() {
				<-r.Context().Done()
				return
			}
			if status := f.briefStatus.Load(); status != 0 {
				w.WriteHeader(int(status))
				fmt.Fprint(w, `{"error":{"code":"forbidden","message":"denied"}}`)
				return
			}
			if r.Header.Get("Authorization") != "Bearer fixture-token" {
				t.Error("brief used wrong credentials")
			}
			fmt.Fprint(w, `{"schema":"brief.v1","principal":{"id":"actor-worker","kind":"agent","display":"Worker"},"answered_for_you":[{"ref":"inbox:ask-answer","title":"Answered decision","answer":"Ship"}],"your_asks":[{"ref":"inbox:ask-waiting","title":"Waiting decision"}],"initiatives":[{"ref":"card:own","title":"Owned work","state":"past_promise","yours":true,"promise":{"at":"2026-01-01T00:00:00Z","what":"Verify checkpoint"}},{"ref":"card:other","title":"Other initiative","state":"no_promise"}]}`)
		case r.URL.Path == "/meta/handshake":
			if f.handshakeUnsupported.Load() {
				fmt.Fprint(w, `{"workspace_id":"ws","min_cli_version":"0.0.0"}`)
			} else {
				fmt.Fprint(w, `{"workspace_id":"ws","min_cli_version":"0.0.0","capabilities":["host_bridge_v1"]}`)
			}
		case strings.HasPrefix(r.URL.Path, "/work/"):
			if status := int(f.status.Load()); status != 0 {
				w.WriteHeader(status)
				fmt.Fprint(w, `{"error":{"code":"forbidden","message":"denied"}}`)
				return
			}
			if strings.HasSuffix(r.URL.Path, "/observations") {
				fmt.Fprint(w, `{"items":[],"next_cursor":""}`)
			} else if strings.HasSuffix(r.URL.Path, "/refresh") {
				fmt.Fprint(w, `{"refresh":{"status":"idle"}}`)
			} else {
				fmt.Fprint(w, `{"work":{"ref":"card:work","title":"Scoped fixture context","phase":"in_progress","promise":{"what":"Verify behavior"}}}`)
			}
		case strings.HasPrefix(r.URL.Path, "/cards/") && strings.HasSuffix(r.URL.Path, "/plan"):
			if f.filePlan.Load() {
				fmt.Fprint(w, `{"plan_health":{"promise":{"at":"2026-10-20T00:00:00Z","what":"Verify behavior"}},"plan":{"steps":[{"id":"review","title":"Read scoped note","ref":"card:linked"}]},"plan_state":{"steps":[{"id":"review","status":"active"}]}}`)
				return
			}
			fmt.Fprint(w, `{"plan_health":{"promise":{"at":"2026-10-20T00:00:00Z","what":"Verify behavior"}},"plan":{"steps":[]}}`)
		case strings.HasPrefix(r.URL.Path, "/cards/"):
			if r.URL.Path == "/cards/card-id" && f.fileCardStatus.Load() != 0 {
				w.WriteHeader(int(f.fileCardStatus.Load()))
				fmt.Fprint(w, `{"error":{"code":"forbidden","message":"card unavailable"}}`)
				return
			}
			if strings.HasSuffix(r.URL.Path, "card:blocked") {
				f.prepareCalls.Add(1)
				if status := f.prepareStatus.Load(); status != 0 {
					if status < 0 {
						<-r.Context().Done()
						return
					}
					w.WriteHeader(int(status))
					fmt.Fprint(w, `{"error":{"code":"unavailable","message":"blocked fixture"}}`)
					return
				}
				if f.malformedCard.Load() {
					fmt.Fprint(w, `{"card":{"id":"blocked-id"}}`)
					return
				}
			}
			if strings.HasSuffix(r.URL.Path, "card:other") || strings.HasSuffix(r.URL.Path, "/other-id") {
				fmt.Fprint(w, `{"card":{"id":"other-id","ref":"card:other","thread_id":"other-thread"}}`)
				return
			}
			fmt.Fprint(w, `{"card":{"id":"card-id","ref":"card:work","title":"Fixture","thread_id":"thread-id","column_key":"in_progress"}}`)
		case r.URL.Path == "/events" && r.Method == "GET":
			fmt.Fprint(w, `{"events":[]}`)
		case r.URL.Path == "/events" && r.Method == "POST":
			b, _ := io.ReadAll(r.Body)
			if status := f.fileSendStatus.Load(); status != 0 && strings.Contains(string(b), "thread-id") {
				w.WriteHeader(int(status))
				fmt.Fprint(w, `{"error":{"code":"forbidden","message":"card unavailable"}}`)
				return
			}
			f.mu.Lock()
			f.bodies = append(f.bodies, string(b))
			fail := f.failFirst && len(f.bodies) == 1
			f.mu.Unlock()
			if fail {
				w.WriteHeader(503)
				fmt.Fprint(w, `{"error":{"code":"unavailable","message":"unknown result"}}`)
			} else {
				fmt.Fprint(w, `{"event":{"id":"event-fixture","type":"message_posted"}}`)
			}
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(f.server.Close)
	f.a, f.out = workspaceTestApp(t, f.home, f.root, map[string]string{"ANX_CONFIG_DIR": f.dir})
	_, key, e := ed25519.GenerateKey(nil)
	if e != nil {
		t.Fatal(e)
	}
	if e = hostidentity.SaveAt(f.dir, hostidentity.Host{ID: "host-fixture", KeyID: "key", Slug: "fixture", WorkspaceID: "ws", BaseURL: f.server.URL}, key); e != nil {
		t.Fatal(e)
	}
	return f
}
func (f *hookFixture) run(t *testing.T, args ...string) map[string]any {
	t.Helper()
	f.out.Reset()
	argv := append([]string{"--json", "--base-url", f.server.URL, "--as", "worker"}, args...)
	if code := f.a.Run(argv); code != 0 {
		t.Fatalf("%v exit=%d output=%s stderr=%v", args, code, f.out, f.a.Stderr)
	}
	var doc map[string]any
	if e := json.Unmarshal(f.out.Bytes(), &doc); e != nil {
		t.Fatal(e, f.out.String())
	}
	return asMap(doc["result"])
}
func (f *hookFixture) hook(t *testing.T, harness, payload string) integrations.HookBrief {
	t.Helper()
	f.out.Reset()
	f.a.Stdin = strings.NewReader(payload)
	if code := f.a.Run([]string{"hook", "session.start", "--harness", harness}); code != 0 {
		t.Fatal(code)
	}
	if harness != "generic" {
		var out map[string]any
		if e := json.Unmarshal(f.out.Bytes(), &out); e != nil {
			t.Fatal(e, f.out.String(), f.a.Stderr)
		}
		text := anyString(out["additional_context"])
		if harness != "cursor" {
			text = anyString(asMap(out["hookSpecificOutput"])["additionalContext"])
		}
		return integrations.HookBrief{Context: text}
	}
	var b integrations.HookBrief
	if e := json.Unmarshal(f.out.Bytes(), &b); e != nil {
		t.Fatal(e, f.out.String(), f.a.Stderr)
	}
	return b
}
func fixtureGit(t *testing.T, root string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", root, "-c", "core.hooksPath=/dev/null", "-c", "user.name=Fixture", "-c", "user.email=fixture@example.test"}, args...)...)
	cmd.Env = append(integrations.GitEnvironment(os.Environ()), "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1", "GIT_AUTHOR_DATE=2026-10-17T00:00:00Z", "GIT_COMMITTER_DATE=2026-10-17T00:00:00Z")
	b, e := cmd.CombinedOutput()
	if e != nil {
		t.Fatal(args, e, string(b))
	}
	return strings.TrimSpace(string(b))
}
func TestAgentHookOptInAdaptersBindAndAuthCache(t *testing.T) {
	f := newHookFixture(t)
	payload := fmt.Sprintf(`{"schema_version":1,"cwd":%q,"session_id":"session"}`, f.root)
	if b := f.hook(t, "generic", payload); b.Context != "" || f.calls.Load() != 0 {
		t.Fatal("off hook used network", b, f.calls.Load())
	}
	for _, h := range []string{"generic", "claude-code", "codex", "cursor"} {
		f.run(t, "integrations", "enable", "context.inject", "--harness", h, "--card-id", "card:work", "--allow-stale")
		p := payload
		if h != "generic" {
			p = fmt.Sprintf(`{"cwd":%q,"session_id":"session","source":"compact","hook_event_name":"SessionStart"}`, f.root)
		}
		if h == "cursor" {
			p = fmt.Sprintf(`{"workspace_roots":[%q],"conversation_id":"session","hook_event_name":"sessionStart"}`, f.root)
		}
		b := f.hook(t, h, p)
		if !strings.Contains(b.Context, "Scoped fixture context") {
			t.Fatal(h, b, f.a.Stderr)
		}
	}
	f.run(t, "integrations", "bind", "--harness", "generic", "--session-id", "session", "--card-id", "card:work")
	b := f.hook(t, "generic", payload)
	if b.Card == nil || *b.Card != "card:work" {
		t.Fatal(b)
	}
	f.status.Store(503)
	b = f.hook(t, "generic", payload)
	if !b.Stale || !strings.Contains(b.Context, "STALE") {
		t.Fatal("no offline fallback", b, f.a.Stderr)
	}
	f.status.Store(403)
	b = f.hook(t, "generic", payload)
	if b.Context != "" {
		t.Fatal("denied cache leaked", b)
	}
	f.status.Store(503)
	b = f.hook(t, "generic", payload)
	if b.Context != "" {
		t.Fatal("revoked cache resurrected", b)
	}
	f.run(t, "integrations", "disable", "context.inject", "--harness", "generic")
	before := f.calls.Load()
	if b = f.hook(t, "generic", payload); b.Context != "" || before != f.calls.Load() {
		t.Fatal("disabled hook did work")
	}
}
func TestAgentHookDeadlineAndNoAuthDryRun(t *testing.T) {
	f := newHookFixture(t)
	before := f.calls.Load()
	f.run(t, "integrations", "enable", "context.inject", "--harness", "generic", "--dry-run")
	if f.calls.Load() != before {
		t.Fatal("dry run requested auth")
	}
	if _, e := os.Stat(integrations.HookSettingsPath(f.dir)); !os.IsNotExist(e) {
		t.Fatal("dry run wrote settings")
	}
	f.run(t, "integrations", "enable", "context.inject", "--harness", "generic", "--card-id", "card:work")
	r, w := io.Pipe()
	defer w.Close()
	f.a.Stdin = r
	f.out.Reset()
	start := time.Now()
	f.a.Run([]string{"hook", "session.start"})
	if time.Since(start) > 1800*time.Millisecond {
		t.Fatal("hook deadline exceeded")
	}
	if got := f.out.String(); !strings.Contains(got, `"context": ""`) {
		t.Fatal(got)
	}
}
func TestGitCaptureFrozenRetryPromisesAndWriterFence(t *testing.T) {
	f := newHookFixture(t)
	f.failFirst = true
	fixtureGit(t, f.root, "init", "-q")
	fixtureGit(t, f.root, "commit", "--allow-empty", "-m", "seed")
	f.run(t, "git", "enable", "--writer")
	for _, n := range []string{"first", "second"} {
		fixtureGit(t, f.root, "commit", "--allow-empty", "-m", "Progress\n\nAnx-Card: card:work\nAnx-Next: 2d "+n)
	}
	f.out.Reset()
	code := f.a.Run([]string{"--json", "git", "sync"})
	if code == 0 {
		t.Fatal("uncertain send unexpectedly succeeded")
	}
	f.mu.Lock()
	if len(f.bodies) != 1 {
		t.Fatal(f.bodies)
	}
	first := f.bodies[0]
	f.mu.Unlock()
	_, gd, e := integrations.Repo(context.Background(), f.root)
	if e != nil {
		t.Fatal(e)
	}
	path := integrations.CapturePath(f.dir, gd)
	s, e := integrations.LoadCapture(path)
	if e != nil || len(s.Pending) != 2 {
		t.Fatal(s, e)
	}
	now := time.Now().Add(time.Minute)
	f.a.now = func() time.Time { return now }
	f.run(t, "git", "sync")
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.bodies) != 3 || f.bodies[0] != f.bodies[1] {
		t.Fatal("frozen retry changed", f.bodies)
	}
	var body map[string]any
	_ = json.Unmarshal([]byte(first), &body)
	next := asMap(asMap(asMap(body["event"])["payload"])["next"])
	if next["at"] != "2026-10-19T00:00:00Z" || next["what"] != "first" {
		t.Fatal(next)
	}
	_ = json.Unmarshal([]byte(f.bodies[2]), &body)
	if asMap(asMap(asMap(body["event"])["payload"])["next"])["what"] != "second" {
		t.Fatal(body)
	}
	s, e = integrations.LoadCapture(path)
	if e != nil || len(s.Pending) != 0 || s.Confirmed != 2 {
		t.Fatal(s, e)
	}
	f.run(t, "git", "sync")
	if len(f.bodies) != 3 {
		t.Fatal("overlapping trigger duplicated write")
	}
	// Change the elected host; status/sync must never silently become this host.
	s.Binding.Host = "different-host"
	if e = integrations.AtomicWrite(path, integrations.JSON(s)); e != nil {
		t.Fatal(e)
	}
	_, e = f.a.syncCapture(context.Background(), config.Resolved{ConfigDir: f.dir, Sources: map[string]string{}}, f.root, "", "", "", true)
	if e == nil || !strings.Contains(e.Error(), "not_capture_writer") {
		t.Fatal(e)
	}
}

func TestGitCaptureLinkedWorktreesShareWriterAndReplay(t *testing.T) {
	f := newHookFixture(t)
	fixtureGit(t, f.root, "init", "-q")
	fixtureGit(t, f.root, "commit", "--allow-empty", "-m", "seed")
	f.run(t, "git", "enable", "--writer")
	linked := filepath.Join(t.TempDir(), "linked")
	fixtureGit(t, f.root, "worktree", "add", "-b", "linked", linked)
	f.run(t, "git", "enable", linked, "--writer")
	fixtureGit(t, linked, "commit", "--allow-empty", "-m", "Linked progress\n\nAnx-Card: card:work")
	sha := fixtureGit(t, linked, "rev-parse", "HEAD")
	f.run(t, "git", "sync", linked)
	// The same object appearing on another registered worktree is not republished.
	fixtureGit(t, f.root, "merge", "--ff-only", sha)
	f.run(t, "git", "sync", f.root)
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.bodies) != 1 {
		t.Fatalf("shared object published %d times", len(f.bodies))
	}
	_, primary, _ := integrations.Repo(context.Background(), f.root)
	_, secondary, _ := integrations.Repo(context.Background(), linked)
	if primary != secondary {
		t.Fatal("worktrees have separate writer namespaces")
	}
	s, e := integrations.LoadCapture(integrations.CapturePath(f.dir, primary))
	if e != nil || len(s.Cursors) != 2 || s.Confirmed != 1 {
		t.Fatal(s, e)
	}
}

func TestGitCaptureBackfillWithoutCheckpointAndExpiredReplayRetainBytes(t *testing.T) {
	f := newHookFixture(t)
	fixtureGit(t, f.root, "init", "-q")
	fixtureGit(t, f.root, "commit", "--allow-empty", "-m", "seed")
	seed := fixtureGit(t, f.root, "rev-parse", "HEAD")
	fixtureGit(t, f.root, "commit", "--allow-empty", "-m", "History\n\nAnx-Card: card:work\nAnx-Next: 2d Old promise")
	f.run(t, "git", "enable", "--writer", "--commit-url", "https://forge.example.test/repo/commit/{sha}")
	f.run(t, "git", "sync", "--since", seed)
	f.mu.Lock()
	if len(f.bodies) != 1 {
		t.Fatal(f.bodies)
	}
	var body map[string]any
	_ = json.Unmarshal([]byte(f.bodies[0]), &body)
	p := asMap(asMap(body["event"])["payload"])
	if p["next"] == nil || p["evidence_refs"] == nil {
		t.Fatal("backfill without a checkpoint lost its claims", p)
	}
	f.mu.Unlock()
	fixtureGit(t, f.root, "commit", "--allow-empty", "-m", "New\n\nAnx-Card: card:work")
	// Use a frozen local request to model a crash after an uncertain old send.
	_, gd, _ := integrations.Repo(context.Background(), f.root)
	path := integrations.CapturePath(f.dir, gd)
	s, _ := integrations.LoadCapture(path)
	sha := fixtureGit(t, f.root, "rev-parse", "HEAD")
	c, e := integrations.ParseCommit(context.Background(), f.root, sha)
	if e != nil {
		t.Fatal(e)
	}
	c.FirstAttempt = time.Now().Add(-26 * 24 * time.Hour)
	if e = integrations.SaveCommit(path, c); e != nil {
		t.Fatal(e)
	}
	s.Pending = []string{sha}
	if e = integrations.PrivateWrite(path, integrations.CaptureJSON(s)); e != nil {
		t.Fatal(e)
	}
	_, e = f.a.syncCapture(context.Background(), config.Resolved{ConfigDir: f.dir}, f.root, "", "", "", false)
	if e == nil || !strings.Contains(e.Error(), "replay_window_expired") {
		t.Fatal(e)
	}
	c, e = integrations.LoadCommit(path, sha)
	if e != nil || c.State != "failed" || c.Note != "New" {
		t.Fatal(c, e)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.bodies) != 1 {
		t.Fatal("expired request sent", f.bodies)
	}
}

func TestAgentHookReadsIndependentComponentsConcurrently(t *testing.T) {
	f := newHookFixture(t)
	f.run(t, "integrations", "enable", "context.inject", "--harness", "generic", "--card-id", "card:work")
	f.mu.Lock()
	gate, started := make(chan struct{}), make(chan struct{}, 3)
	f.readGate, f.readStarted = gate, started
	f.mu.Unlock()
	allStarted := make(chan bool, 1)
	go func() {
		timer := time.NewTimer(time.Second)
		defer timer.Stop()
		defer close(gate)
		for n := 0; n < 3; n++ {
			select {
			case <-started:
			case <-timer.C:
				allStarted <- false
				return
			}
		}
		allStarted <- true
	}()
	payload := fmt.Sprintf(`{"schema_version":1,"cwd":%q}`, f.root)
	b := f.hook(t, "generic", payload)
	if !<-allStarted || b.Context == "" {
		t.Fatal("reads did not overlap within the shared budget", b, f.a.Stderr)
	}
}

func TestGitCaptureFullQueuePreservesFailedBytesAndScan(t *testing.T) {
	f := newHookFixture(t)
	fixtureGit(t, f.root, "init", "-q")
	fixtureGit(t, f.root, "commit", "--allow-empty", "-m", "seed")
	f.run(t, "git", "enable", "--writer")
	_, gd, _ := integrations.Repo(context.Background(), f.root)
	path := integrations.CapturePath(f.dir, gd)
	s, _ := integrations.LoadCapture(path)
	for n := 1; n <= integrations.CaptureCapacity; n++ {
		sha := fmt.Sprintf("%040x", n)
		c := integrations.CommitCapture{SHA: sha, Card: "card:work", Note: "Retain original request", State: "failed", Reason: "rejected"}
		if e := integrations.SaveCommit(path, c); e != nil {
			t.Fatal(e)
		}
		s.Pending = append(s.Pending, sha)
	}
	if e := integrations.PrivateWrite(path, integrations.CaptureJSON(s)); e != nil {
		t.Fatal(e)
	}
	headBefore, _ := integrations.PrivateRead(integrations.CaptureRecordPath(path, s.Pending[0]))
	fixtureGit(t, f.root, "commit", "--allow-empty", "-m", "Additional progress\n\nAnx-Card: card:work")
	_, e := f.a.syncCapture(context.Background(), config.Resolved{ConfigDir: f.dir}, f.root, "", "", "", false)
	if e == nil || !strings.Contains(e.Error(), "capture blocked") {
		t.Fatal(e)
	}
	s, e = integrations.LoadCapture(path)
	if e != nil || len(s.Pending) != integrations.CaptureCapacity || len(s.Walk) != 1 || s.Snapshot == "" {
		t.Fatal(s, e)
	}
	headAfter, _ := integrations.PrivateRead(integrations.CaptureRecordPath(path, s.Pending[0]))
	if !bytes.Equal(headBefore, headAfter) {
		t.Fatal("failed bytes changed under backpressure")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.bodies) != 0 {
		t.Fatal("full/failed queue sent new work")
	}
}

func TestGitCaptureRepairsReceiptSavedBeforeQueueState(t *testing.T) {
	f := newHookFixture(t)
	f.failFirst = true
	fixtureGit(t, f.root, "init", "-q")
	fixtureGit(t, f.root, "commit", "--allow-empty", "-m", "seed")
	f.run(t, "git", "enable", "--writer")
	fixtureGit(t, f.root, "commit", "--allow-empty", "-m", "Progress\n\nAnx-Card: card:work\nAnx-Next: 2d Check receipt")
	f.a.Run([]string{"--json", "git", "sync"})
	_, gd, _ := integrations.Repo(context.Background(), f.root)
	path := integrations.CapturePath(f.dir, gd)
	s, e := integrations.LoadCapture(path)
	if e != nil || len(s.Pending) != 1 {
		t.Fatal(s, e)
	}
	c, e := integrations.LoadCommit(path, s.Pending[0])
	if e != nil {
		t.Fatal(e)
	}
	c.State = "confirmed"
	if e = integrations.SaveCommit(path, c); e != nil {
		t.Fatal(e)
	}
	f.run(t, "git", "sync")
	s, e = integrations.LoadCapture(path)
	if e != nil || len(s.Pending) != 0 || s.Confirmed != 1 || s.PromiseCheckpoint[c.CardID].Sequence != c.Sequence {
		t.Fatal(s, e)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.bodies) != 1 {
		t.Fatal("confirmed receipt was resent")
	}
}

func TestGitCaptureDisabledRepoCannotExceedWorktreeCapacity(t *testing.T) {
	f := newHookFixture(t)
	fixtureGit(t, f.root, "init", "-q")
	fixtureGit(t, f.root, "commit", "--allow-empty", "-m", "seed")
	f.run(t, "git", "enable", "--writer")
	f.run(t, "git", "disable")
	_, gd, _ := integrations.Repo(context.Background(), f.root)
	path := integrations.CapturePath(f.dir, gd)
	s, e := integrations.LoadCapture(path)
	if e != nil {
		t.Fatal(e)
	}
	for n := len(s.Cursors); n < 100; n++ {
		s.Cursors[fmt.Sprintf("/registered/%d", n)] = integrations.CaptureCursor{Anchor: s.Anchor}
	}
	if e = integrations.PrivateWrite(path, integrations.CaptureJSON(s)); e != nil {
		t.Fatal(e)
	}
	before, _ := integrations.PrivateRead(path)
	linked := filepath.Join(t.TempDir(), "linked")
	fixtureGit(t, f.root, "worktree", "add", "-b", "capacity", linked)
	f.out.Reset()
	code := f.a.Run([]string{"--json", "git", "enable", linked, "--writer"})
	after, _ := integrations.PrivateRead(path)
	if code == 0 || !strings.Contains(f.out.String(), "worktree capacity reached") || !bytes.Equal(before, after) {
		t.Fatal("capacity failure changed retained state", code, f.out.String())
	}
	if _, e = integrations.LoadCapture(path); e != nil {
		t.Fatal("retained state unreadable", e)
	}
}

func TestAgentHookNoCardFallsBackToOwnBrief(t *testing.T) {
	for _, harness := range []string{"generic", "claude-code"} {
		t.Run(harness, func(t *testing.T) {
			f := newHookFixture(t)
			f.run(t, "integrations", "enable", "context.inject", "--harness", harness)
			payload := fmt.Sprintf(`{"schema_version":1,"cwd":%q,"session_id":"no-card"}`, f.root)
			if harness == "claude-code" {
				payload = fmt.Sprintf(`{"cwd":%q,"session_id":"no-card","source":"resume","hook_event_name":"SessionStart"}`, f.root)
			}
			brief := f.hook(t, harness, payload)
			for _, want := range []string{"answered for you:", "Answered decision", "waiting on human:", "Waiting decision", "your initiatives:", "Verify checkpoint", "next: anx note card:own", "Reload the anx-participant skill"} {
				if !strings.Contains(brief.Context, want) {
					t.Fatalf("missing %q: %#v stderr=%v", want, brief, f.a.Stderr)
				}
			}
			if strings.Contains(brief.Context, "Other initiative") || len(brief.Context) > 6<<10 || brief.Card != nil {
				t.Fatalf("bad fallback: %#v", brief)
			}
			// No stale brief cache on authorization failure.
			f.briefStatus.Store(403)
			f.a.Stdin = strings.NewReader(payload)
			f.out.Reset()
			if exit := f.a.Run([]string{"hook", "session.start", "--harness", harness}); exit != 0 {
				t.Fatal(exit)
			}
			if strings.Contains(f.out.String(), "Answered decision") || harness == "generic" && !strings.Contains(f.out.String(), `"context": ""`) {
				t.Fatalf("denied brief leaked: %s", f.out.String())
			}
		})
	}
}

func TestAgentHookNoCardBriefDeadline(t *testing.T) {
	f := newHookFixture(t)
	f.run(t, "integrations", "enable", "context.inject", "--harness", "generic")
	f.briefDelay.Store(true)
	payload := fmt.Sprintf(`{"schema_version":1,"cwd":%q}`, f.root)
	start := time.Now()
	brief := f.hook(t, "generic", payload)
	if brief.Context != "" || time.Since(start) > 1800*time.Millisecond {
		t.Fatalf("deadline/fail-open violated: %#v", brief)
	}
}

func TestHookOrientBudgetPreservesNextAndSkillReminder(t *testing.T) {
	brief := map[string]any{"principal": map[string]any{"id": "worker", "kind": "agent"}, "answered_for_you": []any{map[string]any{"title": strings.Repeat("é", 10000), "answer": "Answer"}}, "initiatives": []any{map[string]any{"ref": "card:own", "state": "no_promise", "yours": true}}}
	text := renderHookOrient(brief)
	if len(text) > 6<<10 || !strings.Contains(text, hookSkillReminder) || !strings.HasSuffix(text, "next: anx note card:own --next <date> \"<what>\"") {
		t.Fatal(len(text), text)
	}
}
