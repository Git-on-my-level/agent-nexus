package app

import (
	hostbridge "agent-nexus-cli/internal/bridge"
	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/hostidentity"
	"agent-nexus-cli/internal/integrations"
	"context"
	"crypto/ed25519"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestHostBridgeRuntimeCommandsUsePinnedIdentityAndCallbacks(t *testing.T) {
	if testing.Short() {
		t.Skip("real process integration")
	}
	root := t.TempDir()
	receipt := filepath.Join(root, "identity")
	script := filepath.Join(root, "runtime")
	body := "#!/bin/sh\n[ -z \"${GIT_DIR+x}\" ] || exit 21\n[ -z \"${AGENTCTL_EXECUTION_ID+x}\" ] || exit 22\nprintf '%s|%s|%s' \"$ANX_AS\" \"$ANX_BASE_URL\" \"$ANX_CONFIG_DIR\" > " + integrations.ShellQuote(receipt) + "\ncat >/dev/null\n"
	if e := os.WriteFile(script, []byte(body), 0700); e != nil {
		t.Fatal(e)
	}
	t.Setenv("GIT_DIR", "/sentinel")
	t.Setenv("AGENTCTL_EXECUTION_ID", "wrong-parent")
	t.Setenv("ANX_BASE_URL", "wrong-workspace")
	a := New()
	cfg := config.Resolved{As: "worker", BaseURL: "http://fixture.test", ConfigDir: root}
	if e := a.launchBridgeRuntime(context.Background(), cfg, hostbridge.Runtime{Argv: []string{script}, CWD: root, Env: map[string]string{"GIT_DIR": "/sentinel"}}, "prompt", nil); e != nil {
		t.Fatal(e)
	}
	raw, e := os.ReadFile(receipt)
	if e != nil || string(raw) != "worker|http://fixture.test|"+root {
		t.Fatal(string(raw), e)
	}
	agentctl := filepath.Join(root, "agentctl")
	log := filepath.Join(root, "argv")
	source := "#!/bin/sh\nprintf '%s\\n' \"$@\" >> " + integrations.ShellQuote(log) + "\nif [ \"$1\" = id ]; then printf '{\"result\":{\"id\":\"execution-fixture\"}}'; fi\ncat >/dev/null\n"
	if e = os.WriteFile(agentctl, []byte(source), 0700); e != nil {
		t.Fatal(e)
	}
	if e = a.launchBridgeRuntime(context.Background(), cfg, hostbridge.Runtime{Argv: []string{script}, CWD: root, Agentctl: agentctl, Adapter: "generic"}, "prompt", map[string]any{"subject_ref": "card:work"}); e != nil {
		t.Fatal(e)
	}
	raw, e = os.ReadFile(log)
	if e != nil {
		t.Fatal(e)
	}
	for _, part := range []string{"subscribe\ncreate\n", "--target\n", "--config-dir\n", root, "--base-url\n", "runs\n--arg\ningest\n", "run\n--execution-id\nexecution-fixture", "anx.card.work", "--prompt-delivery\nstdin"} {
		if !strings.Contains(string(raw), part) {
			t.Fatal("missing callback/runtime wiring", part, string(raw))
		}
	}
}
func TestHostBridgeRuntimeReceiptReplaysAcknowledgementOnly(t *testing.T) {
	if testing.Short() {
		t.Skip("real process integration")
	}
	root := t.TempDir()
	home := t.TempDir()
	var completions atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/artifacts/"):
			fmt.Fprint(w, `{"subject_ref":"card:work","trigger":{"text":"wake"}}`)
		case r.URL.Path == "/agent-wakeups/claim":
			fmt.Fprint(w, `{}`)
		case r.URL.Path == "/agent-wakeups/complete":
			if completions.Add(1) == 1 {
				w.WriteHeader(503)
			}
			fmt.Fprint(w, `{}`)
		default:
			t.Errorf("unexpected %s", r.URL.Path)
		}
	}))
	defer server.Close()
	a, _ := workspaceTestApp(t, home, home, map[string]string{"ANX_CONFIG_DIR": root})
	_, key, e := ed25519.GenerateKey(nil)
	if e != nil {
		t.Fatal(e)
	}
	if e = hostidentity.SaveAt(root, hostidentity.Host{ID: "host", KeyID: "key", Slug: "fixture", WorkspaceID: "ws", BaseURL: server.URL}, key); e != nil {
		t.Fatal(e)
	}
	dir := filepath.Join(root, "answer-resumes", hostbridge.Namespace(server.URL, "actor"))
	if e = os.MkdirAll(dir, 0700); e != nil {
		t.Fatal(e)
	}
	calls := filepath.Join(root, "calls")
	command := filepath.Join(root, "command")
	if e = os.WriteFile(command, []byte("#!/bin/sh\nprintf x >> "+integrations.ShellQuote(calls)+"\ncat >/dev/null\n"), 0700); e != nil {
		t.Fatal(e)
	}
	cfg := config.Resolved{BaseURL: server.URL, As: "worker", ActorID: "actor", ConfigDir: root, AccessToken: "fixture", Timeout: time.Second, Sources: map[string]string{"base_url": "fixture"}}
	runtime := hostbridge.Runtime{Argv: []string{command}, CWD: root}
	item := map[string]any{"wakeup_id": "wake-fixture", "target_actor_id": "actor", "delivery_status": "requested"}
	if e = a.deliverRuntimeWake(context.Background(), cfg, dir, runtime, item, nil, time.Second); e == nil {
		t.Fatal("unknown completion treated as confirmed")
	}
	if e = a.deliverRuntimeWake(context.Background(), cfg, dir, runtime, item, nil, time.Second); e != nil {
		t.Fatal(e)
	}
	raw, e := os.ReadFile(calls)
	if e != nil || string(raw) != "x" || completions.Load() != 2 {
		t.Fatal(string(raw), e, completions.Load())
	}
}
func TestHostBridgeGitJobOnlyDrainsExplicitWriter(t *testing.T) {
	f := newHookFixture(t)
	fixtureGit(t, f.root, "init", "-q")
	fixtureGit(t, f.root, "commit", "--allow-empty", "-m", "seed")
	f.run(t, "git", "enable", "--writer")
	fixtureGit(t, f.root, "commit", "--allow-empty", "-m", "Progress\n\nAnx-Card: card:work")
	cfg := config.Resolved{BaseURL: f.server.URL, As: "worker", ActorID: "actor-worker", ConfigDir: f.dir, Timeout: time.Second, Sources: map[string]string{"base_url": "fixture"}}
	foreign := cfg
	foreign.ActorID = "actor-other"
	foreign.As = "other"
	if n, e := f.a.bridgeGitTick(context.Background(), foreign, false); e != nil || n != 0 {
		t.Fatal(n, e)
	}
	if n, e := f.a.bridgeGitTick(context.Background(), cfg, false); e != nil || n != 1 {
		t.Fatal(n, e)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.bodies) != 1 {
		t.Fatal(f.bodies)
	}
}

func TestHostBridgeRuntimeUnknownOutcomePausesLane(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "runtime-receipts", "fixture.json")
	if e := integrations.PrivateWrite(path, integrations.JSON(runtimeWakeReceipt{State: "executing", Instance: "old-claim", Wake: "wake"})); e != nil {
		t.Fatal(e)
	}
	if e := checkRuntimeReceipts(dir); e == nil {
		t.Fatal("unknown runtime effect could replay")
	}
	if e := integrations.PrivateWrite(path, integrations.JSON(runtimeWakeReceipt{State: "delivered", Instance: "old-claim", Wake: "wake"})); e != nil {
		t.Fatal(e)
	}
	if e := checkRuntimeReceipts(dir); e != nil {
		t.Fatal("confirmed outcome must allow acknowledgement", e)
	}
}
func TestHostBridgeControlOutputBound(t *testing.T) {
	var out bridgeControlOutput
	if _, e := out.Write(make([]byte, 64<<10)); e != nil {
		t.Fatal(e)
	}
	if _, e := out.Write([]byte("overflow")); e == nil || out.Len() != 64<<10 {
		t.Fatal("unbounded runtime reply", out.Len(), e)
	}
}

func TestHostBridgeDisabledWakeDoesNotExecuteRetainedRuntime(t *testing.T) {
	root, home := t.TempDir(), t.TempDir()
	var unexpected atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/stream/agent-wakeups" {
			fmt.Fprint(w, "event: notification_receipt\ndata: {\"receipt\":{\"wakeup_id\":\"ordinary-wake\",\"target_actor_id\":\"actor\",\"delivery_status\":\"requested\"}}\n\n")
			return
		}
		unexpected.Add(1)
		http.NotFound(w, r)
	}))
	defer server.Close()
	a, _ := workspaceTestApp(t, home, home, map[string]string{"ANX_CONFIG_DIR": root})
	_, key, e := ed25519.GenerateKey(nil)
	if e != nil {
		t.Fatal(e)
	}
	if e = hostidentity.SaveAt(root, hostidentity.Host{ID: "host", KeyID: "key", Slug: "fixture", WorkspaceID: "ws", BaseURL: server.URL}, key); e != nil {
		t.Fatal(e)
	}
	dir := filepath.Join(root, "answer-resumes", hostbridge.Namespace(server.URL, "actor"))
	if e = os.MkdirAll(dir, 0700); e != nil {
		t.Fatal(e)
	}
	cfg := config.Resolved{BaseURL: server.URL, As: "worker", ActorID: "actor", ConfigDir: root, AccessToken: "fixture", Timeout: time.Second, Sources: map[string]string{"base_url": "fixture"}}
	agent := hostbridge.Agent{Jobs: map[string]bool{"answers": true, "wake": false}, RegistryDirs: []string{dir}, Policy: hostbridge.Policy{MaxAttempts: 5, Timeout: time.Second}, Runtime: &hostbridge.Runtime{Argv: []string{"/must-not-execute"}, CWD: root}}
	_ = a.consumeHostWakes(context.Background(), cfg, agent, nil)
	if unexpected.Load() != 0 {
		t.Fatal("disabled runtime fetched packet or claimed wake", unexpected.Load())
	}
}
