//go:build integration

package integration

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

type binarySet struct {
	cliPath  string
	corePath string
	err      error
}

var (
	buildOnce sync.Once
	binaries  binarySet
	// coreBindMu serializes ephemeral port reservation + oar-core bind + readyz.
	// Parallel tests otherwise risk reusing the same port between listener close
	// and core Listen (TOCTOU), which can mis-route CLI calls or fail binds.
	coreBindMu sync.Mutex
)

type liveCoreHarness struct {
	t              *testing.T
	baseURL        string
	cliBin         string
	homeDir        string
	logPath        string
	logFile        *os.File
	server         *exec.Cmd
	workspace      string
	bootstrapToken string
}

type cliResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
	Payload  map[string]any
}

func TestThreadEventHandoffScenario(t *testing.T) {
	t.Parallel()

	h := newLiveCoreHarness(t)
	runID := runToken()

	h.registerAgentBootstrap(t, "coordinator", "coordinator."+runID)
	workerInvite := h.createInviteToken(t, "coordinator")
	h.registerAgentInvite(t, "worker", "worker."+runID, workerInvite)

	topic := h.runCLIExpectOK(t, "coordinator", map[string]any{
		"topic": map[string]any{
			"title":         "Harness Incident " + runID,
			"type":          "incident",
			"status":        "active",
			"summary":       "Topic created by CLI integration coverage.",
			"owner_refs":    []any{},
			"document_refs": []any{},
			"board_refs":    []any{},
			"related_refs":  []any{},
			"provenance":    map[string]any{"sources": []any{"inferred"}},
		},
	}, "topics", "create")

	threadID := mustStringPath(t, topic.Payload, "data.body.topic.thread_id")

	h.runCLIExpectOK(t, "coordinator", map[string]any{
		"event": map[string]any{
			"type":      "decision_needed",
			"thread_id": threadID,
			"refs":      []string{"thread:" + threadID},
			"summary":   "Need worker acknowledgement for integration run " + runID,
			"payload": map[string]any{
				"run_id": runID,
				"source": "integration_test",
			},
			"provenance": map[string]any{
				"sources": []string{"inferred"},
			},
		},
	}, "events", "create")

	workerThread := h.runCLIExpectOK(t, "worker", nil, "threads", "get", "--thread-id", threadID)
	if !strings.Contains(workerThread.Stdout, "Harness Incident") || !strings.Contains(workerThread.Stdout, runID) {
		t.Fatalf("expected worker thread read to include run title, got: %s", workerThread.Stdout)
	}

	ack := h.runCLIExpectOK(t, "worker", map[string]any{
		"event": map[string]any{
			"type":      "actor_statement",
			"thread_id": threadID,
			"refs":      []string{"thread:" + threadID},
			"summary":   "Worker acknowledged harness thread " + threadID,
			"payload": map[string]any{
				"run_id": runID,
				"source": "integration_test",
			},
			"provenance": map[string]any{
				"sources": []string{"inferred"},
			},
		},
	}, "events", "create")

	workerEventID := mustStringPath(t, ack.Payload, "data.body.event.id")
	coordEvent := h.runCLIExpectOK(t, "coordinator", nil, "events", "get", "--event-id", workerEventID)
	if !strings.Contains(coordEvent.Stdout, "Worker acknowledged harness thread") {
		t.Fatalf("expected coordinator event read to include worker acknowledgement, got: %s", coordEvent.Stdout)
	}

	if got := mustStringPath(t, workerThread.Payload, "data.body.thread.thread_id"); got != threadID {
		t.Fatalf("thread id mismatch: got %q want %q", got, threadID)
	}
}

func TestDocumentLifecycleConflictScenario(t *testing.T) {
	t.Parallel()

	h := newLiveCoreHarness(t)
	runID := runToken()

	h.registerAgentBootstrap(t, "coordinator", "coordinator."+runID)
	workerInvite := h.createInviteToken(t, "coordinator")
	h.registerAgentInvite(t, "worker", "worker."+runID, workerInvite)
	reviewerInvite := h.createInviteToken(t, "coordinator")
	h.registerAgentInvite(t, "reviewer", "reviewer."+runID, reviewerInvite)

	topic := h.runCLIExpectOK(t, "coordinator", map[string]any{
		"topic": map[string]any{
			"title":         "Harness Docs Lifecycle " + runID,
			"type":          "incident",
			"status":        "active",
			"summary":       "Coordinator started docs lifecycle integration run " + runID + ".",
			"owner_refs":    []any{},
			"document_refs": []any{},
			"board_refs":    []any{},
			"related_refs":  []any{},
			"provenance":    map[string]any{"sources": []any{"inferred"}},
		},
	}, "topics", "create")
	threadID := mustStringPath(t, topic.Payload, "data.body.topic.thread_id")
	topicID := mustStringPath(t, topic.Payload, "data.body.topic.id")

	doc := h.runCLIExpectOK(t, "coordinator", map[string]any{
		"document": map[string]any{
			"id": "policy-" + runID,
		},
		"refs":         []string{"thread:" + threadID},
		"content":      "Baseline run " + runID + " policy draft (v1).",
		"content_type": "text",
		"provenance": map[string]any{
			"sources": []string{"inferred"},
		},
	}, "docs", "create")
	documentID := mustStringPath(t, doc.Payload, "data.body.document.id")
	initialRevisionID := mustStringPath(t, doc.Payload, "data.body.revision.revision_id")

	decision := h.runCLIExpectOK(t, "coordinator", map[string]any{
		"event": map[string]any{
			"type":      "decision_needed",
			"thread_id": threadID,
			"refs":      []string{"thread:" + threadID, "document:" + documentID},
			"summary":   "Coordinator requested draft + review for run " + runID,
			"payload": map[string]any{
				"run_id":              runID,
				"document_id":         documentID,
				"initial_revision_id": initialRevisionID,
			},
			"provenance": map[string]any{
				"sources": []string{"inferred"},
			},
		},
	}, "events", "create")
	decisionEventID := mustStringPath(t, decision.Payload, "data.body.event.id")

	h.runCLIExpectOK(t, "coordinator", map[string]any{
		"patch": map[string]any{
			"summary": "Worker owns draft " + documentID + " revisioning for run " + runID + ". Next: worker drafts updated revision; reviewer validates and records review outcome.",
		},
	}, "topics", "patch", "--topic-id", topicID)

	h.runCLIExpectOK(t, "worker", nil, "threads", "context", "--thread-id", threadID)
	h.runCLIExpectOK(t, "worker", nil, "events", "get", "--event-id", decisionEventID)

	ack := h.runCLIExpectOK(t, "worker", map[string]any{
		"event": map[string]any{
			"type":      "actor_statement",
			"thread_id": threadID,
			"refs":      []string{"thread:" + threadID, "event:" + decisionEventID},
			"summary":   "Worker accepted run " + runID + " draft task",
			"payload": map[string]any{
				"run_id":      runID,
				"document_id": documentID,
			},
			"provenance": map[string]any{
				"sources": []string{"inferred"},
			},
		},
	}, "events", "create")
	ackEventID := mustStringPath(t, ack.Payload, "data.body.event.id")

	workerUpdate := h.runCLIExpectOK(t, "worker", map[string]any{
		"if_base_revision": initialRevisionID,
		"content":          "Worker-produced revision for run " + runID + " (v2).",
		"content_type":     "text",
		"refs":             []string{"thread:" + threadID, "event:" + decisionEventID},
	}, "docs", "update", "--document-id", documentID)
	workerRevisionID := mustStringPath(t, workerUpdate.Payload, "data.body.revision.revision_id")

	completion := h.runCLIExpectOK(t, "worker", map[string]any{
		"event": map[string]any{
			"type":      "message_posted",
			"thread_id": threadID,
			"refs":      []string{"thread:" + threadID, "document:" + documentID, "event:" + ackEventID},
			"summary":   "Worker posted draft revision for run " + runID,
			"payload": map[string]any{
				"run_id":      runID,
				"document_id": documentID,
				"revision_id": workerRevisionID,
			},
			"provenance": map[string]any{
				"sources": []string{"inferred"},
			},
		},
	}, "events", "create")
	completionEventID := mustStringPath(t, completion.Payload, "data.body.event.id")

	h.runCLIExpectOK(t, "reviewer", nil, "threads", "context", "--thread-id", threadID)
	preConflict := h.runCLIExpectOK(t, "reviewer", nil, "docs", "get", "--document-id", documentID)
	if got := mustStringPath(t, preConflict.Payload, "data.body.revision.revision_id"); got != workerRevisionID {
		t.Fatalf("expected worker revision to be head before stale update, got %q want %q", got, workerRevisionID)
	}

	stale := h.runCLI(t, "reviewer", map[string]any{
		"if_base_revision": initialRevisionID,
		"content":          "Reviewer stale overwrite attempt for run " + runID + ".",
		"content_type":     "text",
		"refs":             []string{"thread:" + threadID, "event:" + completionEventID},
	}, "docs", "update", "--document-id", documentID)
	if stale.ExitCode == 0 {
		t.Fatalf("expected stale update to fail, got success payload=%s", stale.Stdout)
	}
	if got := mustStringPath(t, stale.Payload, "error.code"); got != "conflict" {
		t.Fatalf("unexpected stale update error code: got %q want conflict", got)
	}
	if got := mustIntPath(t, stale.Payload, "error.details.status"); got != 409 {
		t.Fatalf("unexpected stale update status: got %d want 409", got)
	}
	if msg := mustStringPath(t, stale.Payload, "error.message"); !strings.Contains(msg, "document has been updated") {
		t.Fatalf("unexpected stale update error message: %q", msg)
	}

	postConflict := h.runCLIExpectOK(t, "reviewer", nil, "docs", "get", "--document-id", documentID)
	if got := mustStringPath(t, postConflict.Payload, "data.body.revision.revision_id"); got != workerRevisionID {
		t.Fatalf("stale overwrite advanced revision head: got %q want %q", got, workerRevisionID)
	}

	review := h.runCLIExpectOK(t, "reviewer", map[string]any{
		"event": map[string]any{
			"type":      "actor_statement",
			"thread_id": threadID,
			"refs":      []string{"thread:" + threadID, "document:" + documentID, "event:" + completionEventID},
			"summary":   "Reviewer completed review for run " + runID + " (conflict-safe check passed).",
			"payload": map[string]any{
				"run_id":               runID,
				"document_id":          documentID,
				"observed_revision_id": workerRevisionID,
			},
			"provenance": map[string]any{
				"sources": []string{"inferred"},
			},
		},
	}, "events", "create")
	reviewEventID := mustStringPath(t, review.Payload, "data.body.event.id")

	h.runCLIExpectOK(t, "reviewer", map[string]any{
		"patch": map[string]any{
			"summary": "Reviewer reviewed run " + runID + " and recorded outcome event. Coordinator may close the thread.",
		},
	}, "topics", "patch", "--topic-id", topicID)

	coordThread := h.runCLIExpectOK(t, "coordinator", nil, "threads", "get", "--thread-id", threadID)
	if !strings.Contains(coordThread.Stdout, "Harness Docs Lifecycle") || !strings.Contains(coordThread.Stdout, runID) {
		t.Fatalf("expected thread read to include run title, got: %s", coordThread.Stdout)
	}

	coordCompletion := h.runCLIExpectOK(t, "coordinator", nil, "events", "get", "--event-id", completionEventID)
	if !strings.Contains(coordCompletion.Stdout, "message_posted") || !strings.Contains(coordCompletion.Stdout, runID) {
		t.Fatalf("expected completion event output to include message_posted and run id, got: %s", coordCompletion.Stdout)
	}

	coordDoc := h.runCLIExpectOK(t, "coordinator", nil, "docs", "get", "--document-id", documentID)
	if got := mustStringPath(t, coordDoc.Payload, "data.body.revision.revision_id"); got != workerRevisionID {
		t.Fatalf("coordinator saw unexpected doc head: got %q want %q", got, workerRevisionID)
	}

	history := h.runCLIExpectOK(t, "reviewer", nil, "docs", "history", "--document-id", documentID)
	if !strings.Contains(history.Stdout, initialRevisionID) || !strings.Contains(history.Stdout, workerRevisionID) {
		t.Fatalf("expected document history to contain baseline and worker revisions, got: %s", history.Stdout)
	}

	coordReview := h.runCLIExpectOK(t, "coordinator", nil, "events", "get", "--event-id", reviewEventID)
	if !strings.Contains(coordReview.Stdout, "actor_statement") || !strings.Contains(coordReview.Stdout, "conflict-safe") || !strings.Contains(coordReview.Stdout, runID) {
		t.Fatalf("expected review event output to include review details, got: %s", coordReview.Stdout)
	}
}

func TestProvenanceWalkScenario(t *testing.T) {
	t.Parallel()

	h := newLiveCoreHarness(t)
	runID := runToken()

	h.registerAgentBootstrap(t, "investigator", "investigator."+runID)

	artifact := h.runCLIExpectOK(t, "investigator", map[string]any{
		"artifact": map[string]any{
			"kind":    "evidence",
			"refs":    []string{"url:https://example.com/provenance/" + runID},
			"summary": "Provenance seed artifact " + runID,
		},
		"content": map[string]any{
			"run_id": runID,
			"type":   "provenance-seed",
		},
		"content_type": "structured",
	}, "artifacts", "create")
	artifactID := mustStringPath(t, artifact.Payload, "data.body.artifact.id")

	topic := h.runCLIExpectOK(t, "investigator", map[string]any{
		"topic": map[string]any{
			"title":         "Provenance Walk Harness " + runID,
			"type":          "incident",
			"status":        "active",
			"summary":       "Validate event -> thread + event -> artifact traversal via refs.",
			"owner_refs":    []any{},
			"document_refs": []any{},
			"board_refs":    []any{},
			"related_refs":  []any{"artifact:" + artifactID},
			"provenance":    map[string]any{"sources": []any{"artifact:" + artifactID}},
		},
	}, "topics", "create")
	threadID := mustStringPath(t, topic.Payload, "data.body.topic.thread_id")

	event := h.runCLIExpectOK(t, "investigator", map[string]any{
		"event": map[string]any{
			"type":      "decision_needed",
			"thread_id": threadID,
			"refs":      []string{"thread:" + threadID, "artifact:" + artifactID},
			"summary":   "Trace thread provenance for run " + runID,
			"payload": map[string]any{
				"run_id": runID,
			},
			"provenance": map[string]any{
				"sources": []string{"inferred"},
			},
		},
	}, "events", "create")
	eventID := mustStringPath(t, event.Payload, "data.body.event.id")

	walk := h.runCLIExpectOK(t, "investigator", nil,
		"provenance", "walk",
		"--from", "event:"+eventID,
		"--depth", "2",
	)
	if got := mustStringPath(t, walk.Payload, "command"); got != "provenance walk" {
		t.Fatalf("unexpected command name: got %q want provenance walk", got)
	}
	if got := mustStringPath(t, walk.Payload, "data.from"); got != "event:"+eventID {
		t.Fatalf("unexpected walk root: got %q want %q", got, "event:"+eventID)
	}

	data, _ := walk.Payload["data"].(map[string]any)
	if data == nil {
		t.Fatalf("missing data payload: %#v", walk.Payload)
	}
	nodes, _ := data["nodes"].([]any)
	if !integrationContainsNodeRef(nodes, "event:"+eventID) {
		t.Fatalf("expected event node in walk output, payload=%#v", walk.Payload)
	}
	if !integrationContainsNodeRef(nodes, "thread:"+threadID) {
		t.Fatalf("expected thread node in walk output, payload=%#v", walk.Payload)
	}
	if !integrationContainsNodeRef(nodes, "artifact:"+artifactID) {
		t.Fatalf("expected artifact node in walk output, payload=%#v", walk.Payload)
	}

	edges, _ := data["edges"].([]any)
	if !integrationHasEdge(edges, "event:"+eventID, "thread:"+threadID, "refs") {
		t.Fatalf("expected event->thread edge, payload=%#v", walk.Payload)
	}
	if !integrationHasEdge(edges, "event:"+eventID, "artifact:"+artifactID, "refs") {
		t.Fatalf("expected event->artifact refs edge, payload=%#v", walk.Payload)
	}
}

func newLiveCoreHarness(t *testing.T) *liveCoreHarness {
	t.Helper()

	root := repoRoot(t)
	cliBin, coreBin := buildBinaries(t)
	tempDir := t.TempDir()
	workspace := filepath.Join(tempDir, "workspace")
	// Empty workspace: copying `core/.oar-workspace` would bring a `state.sqlite` whose
	// auth/bootstrap rows are keyed to a different operator token than our per-run
	// OAR_BOOTSTRAP_TOKEN, causing `invalid_token` on register.
	if mkErr := os.MkdirAll(workspace, 0o755); mkErr != nil {
		t.Fatalf("create workspace %s: %v", workspace, mkErr)
	}

	homeDir := filepath.Join(tempDir, "home")
	if err := os.MkdirAll(homeDir, 0o755); err != nil {
		t.Fatalf("create home dir: %v", err)
	}

	logPath := filepath.Join(tempDir, "core.log")
	logFile, err := os.Create(logPath)
	if err != nil {
		t.Fatalf("create core log file: %v", err)
	}

	bootstrapToken := "it-bs-" + runToken()
	coreBindMu.Lock()
	defer coreBindMu.Unlock()
	port := allocatePort(t)
	baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)
	cmd := exec.Command(coreBin,
		"--listen-addr", fmt.Sprintf("127.0.0.1:%d", port),
		"--workspace-root", workspace,
		"--schema-path", filepath.Join(root, "contracts", "oar-schema.yaml"),
	)
	// Background projection maintenance contends with API SQLite transactions; on
	// slow CI busy_timeout(20s) can expire and surface as 500s on mutating requests.
	// These scenarios do not rely on derived topic projections, so use manual mode.
	cmd.Env = append(os.Environ(),
		"OAR_BOOTSTRAP_TOKEN="+bootstrapToken,
		"OAR_PROJECTION_MODE=manual",
	)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		t.Fatalf("start oar-core: %v", err)
	}

	h := &liveCoreHarness{
		t:              t,
		baseURL:        baseURL,
		cliBin:         cliBin,
		homeDir:        homeDir,
		logPath:        logPath,
		logFile:        logFile,
		server:         cmd,
		workspace:      workspace,
		bootstrapToken: bootstrapToken,
	}

	t.Cleanup(func() {
		if h.server.Process != nil {
			_ = h.server.Process.Signal(syscall.SIGTERM)
			done := make(chan struct{})
			go func() {
				_, _ = h.server.Process.Wait()
				close(done)
			}()
			select {
			case <-done:
			case <-time.After(2 * time.Second):
				_ = h.server.Process.Kill()
			}
		}
		_ = h.logFile.Close()
	})

	waitForHealthy(t, baseURL, logPath)
	return h
}

func (h *liveCoreHarness) registerAgentBootstrap(t *testing.T, agent string, username string) {
	t.Helper()
	h.runCLIExpectOK(t, agent, nil, "auth", "register", "--username", username, "--bootstrap-token", h.bootstrapToken)
}

func (h *liveCoreHarness) registerAgentInvite(t *testing.T, agent string, username string, inviteToken string) {
	t.Helper()
	h.runCLIExpectOK(t, agent, nil, "auth", "register", "--username", username, "--invite-token", inviteToken)
}

func (h *liveCoreHarness) createInviteToken(t *testing.T, issuerAgent string) string {
	t.Helper()
	res := h.runCLIExpectOK(t, issuerAgent, nil, "auth", "invites", "create", "--kind", "agent")
	return mustStringPath(t, res.Payload, "data.token")
}

func (h *liveCoreHarness) runCLIExpectOK(t *testing.T, agent string, stdin any, args ...string) cliResult {
	t.Helper()
	res := h.runCLI(t, agent, stdin, args...)
	if res.ExitCode != 0 {
		t.Fatalf("command failed unexpectedly (exit=%d): %s\nstderr: %s", res.ExitCode, res.Stdout, res.Stderr)
	}
	if ok, _ := res.Payload["ok"].(bool); !ok {
		t.Fatalf("expected ok=true payload, got: %s", res.Stdout)
	}
	return res
}

func (h *liveCoreHarness) runCLI(t *testing.T, agent string, stdin any, args ...string) cliResult {
	t.Helper()

	allArgs := make([]string, 0, len(args)+6)
	allArgs = append(allArgs, "--json", "--base-url", h.baseURL, "--agent", agent)
	allArgs = append(allArgs, args...)

	cmd := exec.Command(h.cliBin, allArgs...)
	cmd.Env = append(os.Environ(),
		"HOME="+h.homeDir,
		"XDG_CONFIG_HOME="+filepath.Join(h.homeDir, ".config"),
	)

	var stdinReader io.Reader
	if stdin != nil {
		raw, err := json.Marshal(stdin)
		if err != nil {
			t.Fatalf("marshal stdin payload: %v", err)
		}
		stdinReader = bytes.NewReader(raw)
	}
	if stdinReader != nil {
		cmd.Stdin = stdinReader
	}

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	runErr := cmd.Run()
	exitCode := 0
	if runErr != nil {
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = 1
		}
	}

	res := cliResult{
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		ExitCode: exitCode,
		Payload:  mustParseJSON(t, stdout.String()),
	}
	return res
}

func buildBinaries(t *testing.T) (string, string) {
	t.Helper()

	buildOnce.Do(func() {
		root := repoRoot(t)
		tempDir, err := os.MkdirTemp("", "oar-cli-integration-bin-*")
		if err != nil {
			binaries.err = fmt.Errorf("create build temp dir: %w", err)
			return
		}

		cliPath := filepath.Join(tempDir, "oar")
		corePath := filepath.Join(tempDir, "oar-core")

		if err := buildGoBinary(filepath.Join(root, "cli"), "./cmd/oar", cliPath); err != nil {
			binaries.err = err
			return
		}
		if err := buildGoBinary(filepath.Join(root, "core"), "./cmd/oar-core", corePath); err != nil {
			binaries.err = err
			return
		}

		binaries.cliPath = cliPath
		binaries.corePath = corePath
	})

	if binaries.err != nil {
		t.Fatalf("build integration binaries: %v", binaries.err)
	}
	return binaries.cliPath, binaries.corePath
}

func buildGoBinary(dir string, pkg string, out string) error {
	cmd := exec.Command("go", "build", "-o", out, pkg)
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("build %s: %w: %s", pkg, err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve caller path")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

func allocatePort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("allocate port: %v", err)
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port
}

func waitForHealthy(t *testing.T, baseURL string, logPath string) {
	t.Helper()
	client := &http.Client{Timeout: 500 * time.Millisecond}
	deadline := time.Now().Add(10 * time.Second)
	healthURL := baseURL + "/readyz"
	for time.Now().Before(deadline) {
		resp, err := client.Get(healthURL)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				return
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	raw, _ := os.ReadFile(logPath)
	t.Fatalf("oar-core did not become healthy at %s\nlog:\n%s", healthURL, string(raw))
}

func mustParseJSON(t *testing.T, raw string) map[string]any {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &payload); err != nil {
		t.Fatalf("decode JSON output: %v raw=%s", err, raw)
	}
	return payload
}

func mustStringPath(t *testing.T, payload map[string]any, path string) string {
	t.Helper()
	value, ok := getPathValue(payload, path)
	if !ok {
		t.Fatalf("json path %q not found in payload %#v", path, payload)
	}
	text := strings.TrimSpace(fmt.Sprint(value))
	if text == "" {
		t.Fatalf("json path %q was empty", path)
	}
	return text
}

func mustIntPath(t *testing.T, payload map[string]any, path string) int {
	t.Helper()
	value, ok := getPathValue(payload, path)
	if !ok {
		t.Fatalf("json path %q not found in payload %#v", path, payload)
	}
	switch typed := value.(type) {
	case int:
		return typed
	case int32:
		return int(typed)
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	default:
		t.Fatalf("json path %q was not numeric: %#v", path, value)
		return 0
	}
}

func getPathValue(payload map[string]any, path string) (any, bool) {
	if v, ok := descendJSONPath(payload, path); ok {
		return v, true
	}
	// CLI --json envelopes flatten API `body` into `data` (see flattenEnvelopeData).
	if strings.HasPrefix(path, "data.body.") {
		alt := "data." + strings.TrimPrefix(path, "data.body.")
		if v, ok := descendJSONPath(payload, alt); ok {
			return v, true
		}
	}
	return nil, false
}

func descendJSONPath(payload map[string]any, path string) (any, bool) {
	current := any(payload)
	for _, segment := range strings.Split(path, ".") {
		object, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		value, exists := object[segment]
		if !exists {
			return nil, false
		}
		current = value
	}
	return current, true
}

func integrationContainsNodeRef(nodes []any, ref string) bool {
	target := strings.TrimSpace(ref)
	for _, raw := range nodes {
		node, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if strings.TrimSpace(fmt.Sprint(node["ref"])) == target {
			return true
		}
	}
	return false
}

func integrationHasEdge(edges []any, from string, to string, relation string) bool {
	expectedFrom := strings.TrimSpace(from)
	expectedTo := strings.TrimSpace(to)
	expectedRelation := strings.TrimSpace(relation)
	for _, raw := range edges {
		edge, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if strings.TrimSpace(fmt.Sprint(edge["from"])) != expectedFrom {
			continue
		}
		if strings.TrimSpace(fmt.Sprint(edge["to"])) != expectedTo {
			continue
		}
		if strings.TrimSpace(fmt.Sprint(edge["relation"])) != expectedRelation {
			continue
		}
		return true
	}
	return false
}

func runToken() string {
	return time.Now().UTC().Format("20060102T150405.000000000")
}
