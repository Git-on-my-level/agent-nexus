//go:build integration && (darwin || linux)

package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	hostbridge "agent-nexus-cli/internal/bridge"
	"agent-nexus-cli/internal/filetransport"
	"agent-nexus-cli/internal/integrations"
)

func TestSandboxOutboxThroughHostBridgeToCoreAndMirror(t *testing.T) {
	h := newPasskeyLiveCoreHarness(t)
	// Drop the first successful note response after core commits it. Replay must
	// use the identical frozen request and core receipt, not another note.
	target, _ := url.Parse(h.baseURL)
	proxy := httputil.NewSingleHostReverseProxy(target)
	var requestMu sync.Mutex
	noteRequests := []string{}
	originalDirector := proxy.Director
	proxy.Director = func(r *http.Request) {
		originalDirector(r)
		if r.Method == "POST" && r.URL.Path == "/events" {
			raw, _ := io.ReadAll(r.Body)
			r.Body = io.NopCloser(bytes.NewReader(raw))
			requestMu.Lock()
			noteRequests = append(noteRequests, string(raw))
			requestMu.Unlock()
		}
	}
	dropped := false
	proxy.ModifyResponse = func(r *http.Response) error {
		if r.Request.Method == "POST" && r.Request.URL.Path == "/events" && r.StatusCode < 300 {
			requestMu.Lock()
			defer requestMu.Unlock()
			if !dropped {
				dropped = true
				r.Body.Close()
				return errors.New("fixture lost successful response")
			}
		}
		return nil
	}
	proxy.ErrorHandler = func(w http.ResponseWriter, _ *http.Request, _ error) {
		http.Error(w, "fixture lost receipt", http.StatusBadGateway)
	}
	server := httptest.NewServer(proxy)
	t.Cleanup(server.Close)
	h.baseURL = server.URL
	h.enrollHost(t, "writer")
	work := h.runCLIExpectOK(t, "writer", map[string]any{"title": "Sandbox file transport", "handle": "file-transport", "source": map[string]any{"authority": "nexus"}}, "work", "create", "--from-file", "-")
	ref := mustStringPath(t, work.Payload, "result.work.ref")
	project, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		_ = filepath.WalkDir(project, func(path string, d os.DirEntry, e error) error {
			if e == nil && d.IsDir() {
				return os.Chmod(path, 0700)
			}
			return e
		})
	})
	// No service install. sync enable creates a files-only registration by itself.
	h.runCLIExpectOK(t, "writer", nil, "sync", "enable", project, "--card-id", ref, "--dry-run")
	if _, e = os.Stat(filepath.Join(project, ".anx")); !os.IsNotExist(e) {
		t.Fatal("dry run wrote files", e)
	}
	enabled := h.runCLIExpectOK(t, "writer", nil, "sync", "enable", project, "--card-id", ref)
	id := mustStringPath(t, enabled.Payload, "result.directory_uuid")
	r, e := hostbridge.Load(hostbridge.Root(h.homeDir))
	if e != nil || len(r.Workspaces) != 1 || len(r.Workspaces[0].Agents) != 1 {
		t.Fatal(r, e)
	}
	agent := r.Workspaces[0].Agents[0]
	if !agent.Jobs["files"] || agent.Jobs["answers"] || agent.Jobs["wake"] {
		t.Fatal("file opt-in enabled unrelated jobs", agent)
	}
	producer := filepath.Join(t.TempDir(), "producer")
	build := exec.Command("go", "build", "-o", producer, "./integration/testdata/file-producer")
	build.Dir = filepath.Join(repoRoot(t), "cli")
	build.Env = append(integrations.GitEnvironment(os.Environ()), "CGO_ENABLED=0")
	if out, e := build.CombinedOutput(); e != nil {
		t.Fatal(e, string(out))
	}
	producer, e = filepath.EvalSymlinks(producer)
	if e != nil {
		t.Fatal(e)
	}
	// This is a fixture credential, never a real host file or credential.
	sentinel := filepath.Join(t.TempDir(), "credential-fixture")
	if e = os.WriteFile(sentinel, []byte("fixture-token-never-mounted"), 0600); e != nil {
		t.Fatal(e)
	}
	sentinel, e = filepath.EvalSymlinks(sentinel)
	if e != nil {
		t.Fatal(e)
	}
	outbox := filepath.Join(project, ".anx/outbox")
	mirror := filepath.Join(project, ".anx/INDEX.md")
	entry := filetransport.Entry{Version: 1, ID: filetransport.UUID(), Card: ref, Text: "Isolated writer delivered this note", Evidence: []string{}, Next: &filetransport.Next{At: "1d", What: "Review isolated delivery"}}
	raw := integrations.JSON(entry)
	u, _ := url.Parse(h.baseURL)
	runSandboxProducer(t, producer, outbox, sentinel, mirror, u.Host, entry.ID, raw)
	// The sandbox cannot observe credentials/network; the ordinary enrolled bridge can.
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, h.cliBin, "--json", "bridge", "run")
	cmd.Dir = project
	cmd.Env = append(integrations.GitEnvironment(os.Environ()), "HOME="+h.homeDir, "ANX_CONFIG_DIR=", "ANX_AS=", "ANX_BASE_URL=", "ANX_ACCESS_TOKEN=", "ANX_UPDATE_POLICY=off", "ANX_SKILLS_AUTO_SYNC=0", "AGENTCTL_EXECUTION_ID=", "AGENTCTL_ADAPTER=")
	logPath := filepath.Join(t.TempDir(), "bridge.log")
	log, e := os.Create(logPath)
	if e != nil {
		t.Fatal(e)
	}
	cmd.Stdout = log
	cmd.Stderr = log
	if e = cmd.Start(); e != nil {
		cancel()
		log.Close()
		t.Fatal(e)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("owned test bridge did not exit")
		}
		log.Close()
	})
	stateDir := filetransport.StateDir(filepath.Join(h.homeDir, ".config/anx"), project)
	deadline := time.Now().Add(40 * time.Second)
	for {
		b, readErr := os.ReadFile(mirror)
		cardFiles, _ := filepath.Glob(filepath.Join(project, ".anx/cards/*.md"))
		cardText := []byte{}
		if len(cardFiles) == 1 {
			cardText, _ = os.ReadFile(cardFiles[0])
		}
		s, loadErr := filetransport.Load(stateDir)
		if readErr == nil && loadErr == nil && s.Records == 1 && len(s.Pending) == 0 && strings.Contains(string(cardText), entry.Text) && strings.Contains(string(cardText), entry.Next.What) && strings.Contains(string(b), "ANX card mirror") {
			break
		}
		if time.Now().After(deadline) {
			logs, _ := os.ReadFile(logPath)
			t.Fatal("file job failed", string(logs), s, readErr, loadErr, string(cardText))
		}
		time.Sleep(100 * time.Millisecond)
	}
	requestMu.Lock()
	requests := append([]string{}, noteRequests...)
	requestMu.Unlock()
	if len(requests) < 2 {
		t.Fatal("lost receipt was not replayed", len(requests))
	}
	for _, body := range requests[1:] {
		if body != requests[0] {
			t.Fatal("uncertain replay changed frozen body")
		}
	}
	// Re-publishing the exact producer UUID must not add another event, even
	// after the original .done receipt has been created by a later tick.
	runSandboxProducer(t, producer, outbox, sentinel, mirror, u.Host, entry.ID, raw)
	deadline = time.Now().Add(15 * time.Second)
	for {
		if _, e := os.Stat(filepath.Join(outbox, entry.ID+".json")); os.IsNotExist(e) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("duplicate not inspected")
		}
		time.Sleep(100 * time.Millisecond)
	}
	events := h.runCLIExpectOK(t, "writer", nil, "debug", "events", "list", "--type", "message_posted", "--max-events", "100")
	items, ok := getPathValue(events.Payload, "result.events")
	if !ok {
		t.Fatal(events.Stdout)
	}
	count := 0
	for _, v := range items.([]any) {
		ev := v.(map[string]any)
		payload, _ := ev["payload"].(map[string]any)
		if payload["text"] == entry.Text {
			count++
			next, _ := payload["next"].(map[string]any)
			if next["what"] != entry.Next.What {
				t.Fatal(payload)
			}
		}
	}
	if count != 1 {
		t.Fatal("expected exactly one core note", events.Stdout)
	}
	if !strings.Contains(id, "-") {
		t.Fatal("missing directory UUID", id)
	}
	h.runCLIExpectOK(t, "writer", nil, "sync", "status", project)
	h.runCLIExpectOK(t, "writer", nil, "sync", "disable", project)
	if files, _ := filepath.Glob(filepath.Join(project, ".anx/cards/*.md")); len(files) != 0 {
		t.Fatal("disable retained mirror cards", files)
	}
}
func runSandboxProducer(t *testing.T, producer, outbox, sentinel, mirror, address, id string, raw []byte) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		// Deny default; only the artifact, dyld runtime, and mounted-equivalent
		// outbox are readable. No host configuration tree or network permission.
		libs := []string{"/usr/lib", "/System/Library/dyld", "/System/Volumes/Preboot/Cryptexes/OS/usr/lib", "/System/Volumes/Preboot/Cryptexes/OS/System/Library/dyld", "/private/preboot/Cryptexes/OS/usr/lib", "/private/preboot/Cryptexes/OS/System/Library/dyld"}
		var profile strings.Builder
		profile.WriteString("(version 1)(deny default)\n(allow process-exec* (literal " + strconv.Quote(producer) + "))\n(allow signal (target self))\n(allow sysctl-read)\n(deny sysctl-read (sysctl-name-prefix \"kern.proc\"))\n")
		filters := "(literal " + strconv.Quote(producer) + ")"
		for _, lib := range libs {
			filters += " (subpath " + strconv.Quote(lib) + ")"
		}
		profile.WriteString("(allow file-read* file-map-executable " + filters + ")\n(allow file-read* (literal \"/\") (literal \"/dev/null\") (literal \"/dev/urandom\") (literal \"/dev/random\"))\n")
		parents := map[string]bool{}
		for _, p := range append(append(libs, producer), outbox) {
			for p = filepath.Dir(p); p != "/"; p = filepath.Dir(p) {
				parents[p] = true
			}
		}
		for p := range parents {
			profile.WriteString("(allow file-read-metadata (literal " + strconv.Quote(p) + "))\n")
		}
		profile.WriteString("(allow file-read* file-write* (subpath " + strconv.Quote(outbox) + "))\n(deny network*)\n(deny process-fork)\n")
		cmd = exec.CommandContext(ctx, "/usr/bin/sandbox-exec", "-p", profile.String(), producer, outbox, id, address, sentinel, mirror)
	case "linux":
		bwrap, e := exec.LookPath("bwrap")
		if e != nil {
			t.Fatal("file transport sandbox E2E requires bubblewrap (installed in cli-checks):", e)
		}
		cmd = exec.CommandContext(ctx, bwrap, "--unshare-all", "--die-with-parent", "--new-session", "--clearenv", "--ro-bind", producer, "/producer", "--bind", outbox, "/outbox", "--proc", "/proc", "--dev", "/dev", "--chdir", "/outbox", "/producer", "/outbox", id, address, sentinel, mirror)
	default:
		t.Fatal("unsupported sandbox platform")
	}
	cmd.Dir = outbox
	cmd.Env = []string{"PATH=/", "LANG=C"}
	cmd.Stdin = bytes.NewReader(raw)
	output, e := cmd.CombinedOutput()
	if e != nil || !strings.Contains(string(output), "isolated: network denied") {
		t.Fatal("sandbox did not enforce producer boundary", e, string(output))
	}
	var in map[string]any
	if e = json.Unmarshal(raw, &in); e != nil {
		t.Fatal(e)
	}
	if in["id"] != id {
		t.Fatal("producer UUID changed")
	}
}
