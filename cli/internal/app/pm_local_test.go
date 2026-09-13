package app

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/errnorm"
)

func TestSplitRunnerArgvRespectsQuotes(t *testing.T) {
	got, err := splitRunnerArgv(`omp -p --mode json --model zai/glm-5.3 --auto-approve`)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"omp", "-p", "--mode", "json", "--model", "zai/glm-5.3", "--auto-approve"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("got %v", got)
	}
}

func TestBuildPMPromptStaysSmallAndNamesTools(t *testing.T) {
	prompt := buildPMPrompt("pm", map[string]any{
		"id":       "pm_turn_1",
		"actor_id": "actor-gds-producer",
		"deadline": "2026-09-08T22:00:00Z",
		"text":     "What needs my decision?",
	}, 16000)
	if strings.Contains(prompt, "inventory") || strings.Contains(strings.ToLower(prompt), "full tracker") {
		t.Fatal("prompt stuffed tracker context")
	}
	for _, needle := range []string{"What needs my decision?", "anx --agent pm work list", "anx --agent pm pm context", "pm turns propose", "ANX_PM_LEASE_TOKEN", "--lease-token", "work_ref", "decision:", "---evidence---", "evidence_refs", "identical payload, instruction and target revision", "supersedes the earlier awaiting decision", "instead of duplicating"} {
		if !strings.Contains(prompt, needle) {
			t.Fatalf("missing %q in %s", needle, prompt)
		}
	}
	if strings.Contains(prompt, "is reused") {
		t.Fatal("prompt still claims same work_ref/scope reuse without identical content")
	}
}

func TestExpandPromptPlaceholder(t *testing.T) {
	argv := []string{"/bin/echo", "{prompt}"}
	if !runnerUsesPromptPlaceholder(argv) {
		t.Fatal("expected placeholder")
	}
	got := expandPromptPlaceholder(argv, "/tmp/turn.md")
	if strings.Join(got, " ") != "/bin/echo /tmp/turn.md" {
		t.Fatalf("got %v", got)
	}
}

func TestExtractProviderModelAndAssistantText(t *testing.T) {
	blob := `{"provider":"zai","model":"glm-5.3","messages":[{"role":"user","text":"hi"},{"role":"assistant","text":"Approve the restock card:emergency-restock"}]}`
	provider, model := extractProviderModel(blob)
	if provider != "zai" || model != "glm-5.3" {
		t.Fatalf("provider/model %s %s", provider, model)
	}
	text := extractAssistantText("", blob)
	if !strings.Contains(text, "emergency-restock") {
		t.Fatalf("assistant text %q", text)
	}
}

func TestExtractEvidenceRefsIgnoresProseAndParsesDeliberateSources(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		text string
		raw  string
		want []string
		body string
	}{
		{
			name: "prose-only mention ignored",
			text: "Proposed decision:pm_placeholder awaiting your answer. See card:emergency-restock.",
		},
		{
			name: "block parsed",
			text: "Proposed decision:pm_placeholder.\n\n---evidence---\ndecision:pm_abc123\nartifact:art_xyz\n",
			want: []string{"decision:pm_abc123", "artifact:art_xyz"},
			body: "Proposed decision:pm_placeholder.",
		},
		{
			name: "malformed lines ignored",
			text: "Ready.\n---evidence---\nnot a ref\ndecision:\n:missing\ndecision:pm_ok extra\nevent:evt_1\n",
			want: []string{"event:evt_1"},
			body: "Ready.",
		},
		{
			name: "trailing punctuation stripped on block lines",
			text: "Done.\n---evidence---\ndecision:pm_turn_9f3acc1cccd.\ncard:foo.bar.\n",
			want: []string{"decision:pm_turn_9f3acc1cccd", "card:foo.bar"},
			body: "Done.",
		},
		{
			name: "json evidence_refs array",
			raw:  `{"messages":[{"role":"assistant","text":"See decision:pm_placeholder","evidence_refs":["decision:pm_abc123","artifact:art_xyz"]}]}`,
			text: "See decision:pm_placeholder",
			want: []string{"decision:pm_abc123", "artifact:art_xyz"},
			body: "See decision:pm_placeholder",
		},
		{
			name: "json top-level evidence_refs with malformed ignored",
			raw:  `{"evidence_refs":["decision:pm_ok","nope",123,""],"text":"hello"}`,
			text: "hello",
			want: []string{"decision:pm_ok"},
			body: "hello",
		},
		{
			name: "nested tool evidence_refs ignored",
			raw:  `{"messages":[{"role":"tool","text":"ok","evidence_refs":["decision:from_tool"]},{"role":"assistant","text":"Done.","evidence_refs":["decision:from_assistant"]}],"result":{"output":{"evidence_refs":["decision:nested_tool"]}}}`,
			text: "Done.",
			want: []string{"decision:from_assistant"},
			body: "Done.",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := tc.raw
			if raw == "" {
				raw = tc.text
			}
			gotBody, refs := collectDeliberateEvidenceRefs(tc.text, raw)
			if tc.body != "" && gotBody != tc.body {
				t.Fatalf("body %q want %q", gotBody, tc.body)
			}
			if strings.Join(refs, "|") != strings.Join(tc.want, "|") {
				t.Fatalf("refs %v want %v", refs, tc.want)
			}
		})
	}
}

func TestCompleteTurnDropsUnresolvableEvidenceRefs(t *testing.T) {
	var completeBody map[string]any
	var gets []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			gets = append(gets, r.URL.Path)
		}
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/complete"):
			body, _ := io.ReadAll(r.Body)
			if err := json.Unmarshal(body, &completeBody); err != nil {
				t.Errorf("complete body: %v", err)
			}
			io.WriteString(w, `{"id":"turn-1","status":"delivered"}`)
		case r.Method == http.MethodGet && r.URL.Path == "/pm/decisions/decision:pm_ok":
			io.WriteString(w, `{"id":"pm_ok"}`)
		case r.Method == http.MethodGet && r.URL.Path == "/work/card:ok":
			io.WriteString(w, `{"work":{"ref":"card:ok"}}`)
		case r.Method == http.MethodGet && r.URL.Path == "/artifacts/artifact:art_ok":
			io.WriteString(w, `{"id":"art_ok"}`)
		case r.Method == http.MethodGet && r.URL.Path == "/topics/topic:ops-ok":
			io.WriteString(w, `{"topic":{"ref":"topic:ops-ok"}}`)
		case r.Method == http.MethodGet && r.URL.Path == "/docs/document:doc-ok":
			io.WriteString(w, `{"document":{"ref":"document:doc-ok"}}`)
		case r.Method == http.MethodGet:
			w.WriteHeader(http.StatusNotFound)
			io.WriteString(w, `{"error":{"code":"not_found","message":"PM record not found"}}`)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	stderr := &bytes.Buffer{}
	app := New()
	app.Stderr = stderr
	app.Stdout = io.Discard
	app.Getenv = func(string) string { return "" }
	app.UserHomeDir = func() (string, error) { return t.TempDir(), nil }
	cfg := config.Resolved{BaseURL: srv.URL, AccessToken: "fixture", Timeout: 5 * time.Second, Agent: "pm"}

	text := "Mention decision:pm_placeholder and card:ghost.\n\n---evidence---\ndecision:pm_ok\ndecision:pm_missing\ncard:ok\ntopic:ops-ok\ntopic:ops-missing\ndocument:doc-ok\nartifact:art_ok\n"
	if err := app.completeTurn(context.Background(), cfg, "turn-1", "lease-1", text, nil, 16000); err != nil {
		t.Fatalf("completeTurn: %v", err)
	}
	if anyString(completeBody["text"]) != "Mention decision:pm_placeholder and card:ghost." {
		t.Fatalf("stored text %v", completeBody["text"])
	}
	refs, _ := completeBody["evidence_refs"].([]any)
	joined := fmt.Sprint(refs)
	if !strings.Contains(joined, "decision:pm_ok") || !strings.Contains(joined, "card:ok") || !strings.Contains(joined, "artifact:art_ok") || !strings.Contains(joined, "topic:ops-ok") || !strings.Contains(joined, "document:doc-ok") {
		t.Fatalf("kept refs %v", refs)
	}
	if strings.Contains(joined, "decision:pm_missing") || strings.Contains(joined, "topic:ops-missing") || strings.Contains(joined, "pm_placeholder") || strings.Contains(joined, "card:ghost") {
		t.Fatalf("dropped refs leaked: %v", refs)
	}
	logs := stderr.String()
	if !strings.Contains(logs, "pm serve: dropping evidence ref decision:pm_missing:") || !strings.Contains(logs, "pm serve: dropping evidence ref topic:ops-missing:") {
		t.Fatalf("drop log %q", logs)
	}
	getJoined := strings.Join(gets, "\n")
	if !strings.Contains(getJoined, "/pm/decisions/decision:pm_ok") || !strings.Contains(getJoined, "/pm/decisions/decision:pm_missing") || !strings.Contains(getJoined, "/work/card:ok") || !strings.Contains(getJoined, "/artifacts/artifact:art_ok") || !strings.Contains(getJoined, "/topics/topic:ops-ok") || !strings.Contains(getJoined, "/docs/document:doc-ok") {
		t.Fatalf("lookups %v", gets)
	}

	stderr.Reset()
	completeBody = nil
	gets = nil
	raw := []byte(`{"messages":[{"role":"assistant","text":"JSON reply mentioning decision:pm_placeholder","evidence_refs":["decision:pm_ok","decision:pm_missing"]}]}`)
	extracted := assistantTextFromRunnerOutput(raw)
	if err := app.completeTurn(context.Background(), cfg, "turn-1", "lease-1", extracted, raw, 16000); err != nil {
		t.Fatalf("json completeTurn: %v", err)
	}
	refs, _ = completeBody["evidence_refs"].([]any)
	if fmt.Sprint(refs) != "[decision:pm_ok]" {
		t.Fatalf("json kept refs %v", refs)
	}
	if !strings.Contains(stderr.String(), "pm serve: dropping evidence ref decision:pm_missing:") {
		t.Fatalf("json drop log %q", stderr.String())
	}
}

func TestPMServeRequiresRunner(t *testing.T) {
	payload := assertEnvelopeError(t, runCLIForTest(t, t.TempDir(), map[string]string{"ANX_ACCESS_TOKEN": "fixture"}, nil, []string{"--json", "--base-url", "http://127.0.0.1:1", "pm", "serve"}))
	if code := asMap(payload["error"])["code"]; code != "invalid_request" && code != "invalid_flags" {
		t.Fatalf("code=%v payload=%v", code, payload)
	}
}

func TestPMAskRequiresQuestion(t *testing.T) {
	payload := assertEnvelopeError(t, runCLIForTest(t, t.TempDir(), map[string]string{"ANX_ACCESS_TOKEN": "fixture"}, nil, []string{"--json", "--base-url", "http://127.0.0.1:1", "pm", "ask"}))
	if code := asMap(payload["error"])["code"]; code != "invalid_request" {
		t.Fatalf("code=%v payload=%v", code, payload)
	}
}

func TestPMAskTextRendersQueuedAndInProgress(t *testing.T) {
	for _, tc := range []struct {
		name, turnBody, want, hide string
	}{
		{
			name:     "unclaimed sending",
			turnBody: `{"id":"turn-1","status":"sending","claimed":false,"deadline":"2026-09-08T22:00:00Z"}`,
			want:     "status: queued",
			hide:     "status: sending",
		},
		{
			name:     "claimed sending",
			turnBody: `{"id":"turn-2","status":"sending","claimed":true,"claimed_at":"2026-09-08T21:00:00Z","deadline":"2026-09-08T22:00:00Z"}`,
			want:     "status: in progress",
			hide:     "status: sending",
		},
		{
			name:     "delivered",
			turnBody: `{"id":"turn-3","status":"delivered","claimed":true,"response":"Ready."}`,
			want:     "status: delivered",
			hide:     "status: sending",
		},
		{
			name:     "failed",
			turnBody: `{"id":"turn-4","status":"failed","claimed":true,"failure":"deadline passed"}`,
			want:     "status: failed",
			hide:     "status: sending",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch {
				case r.Method == http.MethodPost && r.URL.Path == "/pm/conversations":
					io.WriteString(w, `{"id":"conv-1","title":"What needs my decision?"}`)
				case r.Method == http.MethodPost && r.URL.Path == "/pm/conversations/conv-1/messages":
					io.WriteString(w, tc.turnBody)
				default:
					t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			text := runCLIForTest(t, t.TempDir(), map[string]string{"ANX_ACCESS_TOKEN": "fixture"}, nil, []string{"--base-url", server.URL, "pm", "ask", "What needs my decision?"})
			if !strings.Contains(text, tc.want) {
				t.Fatalf("missing %q in %s", tc.want, text)
			}
			if strings.Contains(text, tc.hide) {
				t.Fatalf("unexpected %q in %s", tc.hide, text)
			}
			payload := assertEnvelopeOK(t, runCLIForTest(t, t.TempDir(), map[string]string{"ANX_ACCESS_TOKEN": "fixture"}, nil, []string{"--json", "--base-url", server.URL, "pm", "ask", "What needs my decision?"}))
			turn := asMap(asMap(payload["data"])["turn"])
			if strings.Contains(tc.name, "sending") && anyString(turn["status"]) != "sending" {
				t.Fatalf("JSON remapped status: %v", payload["data"])
			}
		})
	}
}

func TestPMChannelsDoctorRequiresNoPositional(t *testing.T) {
	payload := assertEnvelopeError(t, runCLIForTest(t, t.TempDir(), map[string]string{"ANX_ACCESS_TOKEN": "fixture"}, nil, []string{"--json", "--base-url", "http://127.0.0.1:1", "pm", "channels", "doctor", "extra"}))
	if code := asMap(payload["error"])["code"]; code != "invalid_args" && code != "invalid_flags" {
		t.Fatalf("code=%v payload=%v", code, payload)
	}
}

func TestPMChannelsDoctorReportsMissingSecretsWithoutSending(t *testing.T) {
	raw := runCLIForTest(t, t.TempDir(), map[string]string{"ANX_ACCESS_TOKEN": "fixture", "ANX_BASE_URL": "http://127.0.0.1:1"}, nil, []string{"--json", "--base-url", "http://127.0.0.1:1", "pm", "channels", "doctor"})
	if !strings.Contains(raw, "telegram_webhook_secret") {
		t.Fatalf("output=%s", raw)
	}
	if strings.Contains(strings.ToLower(raw), "bottok") {
		t.Fatal("doctor printed a token")
	}
}

func TestPMChannelsDoctorProbesWebhooksWithGETOnly(t *testing.T) {
	var methods []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		methods = append(methods, r.Method)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}))
	defer srv.Close()
	pub := hex.EncodeToString(make([]byte, ed25519.PublicKeySize))
	raw := runCLIForTest(t, t.TempDir(), map[string]string{
		"ANX_ACCESS_TOKEN":               "fixture",
		"ANX_PM_TELEGRAM_WEBHOOK_SECRET": strings.Repeat("s", 32),
		"ANX_PM_TELEGRAM_BOT_ID":         "99",
		"ANX_PM_TELEGRAM_BOT_TOKEN":      "tok-must-not-print",
		"ANX_PM_DISCORD_PUBLIC_KEY":      pub,
		"ANX_PM_DISCORD_APPLICATION_ID":  "app",
		"ANX_PM_DISCORD_BOT_TOKEN":       "tok-must-not-print",
	}, nil, []string{
		"--json", "--base-url", "http://127.0.0.1:1",
		"pm", "channels", "doctor",
		"--telegram-webhook-url", srv.URL + "/pm/ingress/telegram",
		"--discord-webhook-url", srv.URL + "/pm/ingress/discord",
	})
	if strings.Contains(raw, "tok-must-not-print") {
		t.Fatal("doctor printed a bot token")
	}
	if !strings.Contains(raw, "reachable (HTTP 405)") {
		t.Fatalf("expected GET reachability, output=%s", raw)
	}
	for _, method := range methods {
		if method != http.MethodGet {
			t.Fatalf("doctor sent %s", method)
		}
	}
	if len(methods) != 2 {
		t.Fatalf("probes=%v", methods)
	}
}

func TestHarnessChildEnvRestoresPasswdHomeAndProfile(t *testing.T) {
	got := harnessChildEnv(config.Resolved{
		Agent:       "pm",
		BaseURL:     "http://127.0.0.1:8000",
		ProfilePath: "/tmp/pm.json",
	}, []string{"HOME=/fake/profile-home", "PATH=/bin", "ZAI_API_KEY=already-set"})
	home := envValue(got, "HOME")
	if home == "/fake/profile-home" {
		t.Fatal("child HOME stayed on the isolated profile home; omp needs the operator home for models.yml")
	}
	if envValue(got, "ANX_PROFILE_PATH") != "/tmp/pm.json" {
		t.Fatalf("ANX_PROFILE_PATH=%q", envValue(got, "ANX_PROFILE_PATH"))
	}
	if envValue(got, "ANX_AGENT") != "pm" || envValue(got, "ANX_BASE_URL") != "http://127.0.0.1:8000" {
		t.Fatalf("agent/base %q %q", envValue(got, "ANX_AGENT"), envValue(got, "ANX_BASE_URL"))
	}
	if envValue(got, "ZAI_API_KEY") != "already-set" {
		t.Fatal("existing ZAI_API_KEY was rewritten")
	}
}

func TestHarnessChildEnvDoesNotInjectZAIAPIKey(t *testing.T) {
	got := harnessChildEnv(config.Resolved{
		Agent:       "pm",
		BaseURL:     "http://127.0.0.1:8000",
		ProfilePath: "/tmp/pm.json",
	}, []string{"PATH=/bin"})
	if envValue(got, "ZAI_API_KEY") != "" {
		t.Fatal("harness injected ZAI_API_KEY from disk")
	}
}

func envValue(env []string, key string) string {
	prefix := key + "="
	for _, item := range env {
		if strings.HasPrefix(item, prefix) {
			return strings.TrimPrefix(item, prefix)
		}
	}
	return ""
}

func TestContinuingAgentctlLaunchAndHumanFailures(t *testing.T) {
	timeoutJSON := []byte(`{"ok":false,"schema_version":1,"error":{"code":"timeout","message":"background worker did not acknowledge durable startup before the deadline and may continue","retryable":true,"exit_code":9,"details":{"execution_id":"exec-shift-siren-during-animal-zone-opera","timeout":"30s","worker_continues":true},"next_actions":[]},"warnings":[]}`)
	id, timeout, ok := continuingAgentctlLaunch(timeoutJSON)
	if !ok || id != "exec-shift-siren-during-animal-zone-opera" || timeout != "30s" {
		t.Fatalf("continuing launch %q %q %v", id, timeout, ok)
	}
	if _, _, ok := continuingAgentctlLaunch([]byte(`{"ok":false,"error":{"code":"crash","details":{"execution_id":"exec-x"}}}`)); ok {
		t.Fatal("non-retryable launch treated as continuing")
	}
	retryOnly := []byte(`{"ok":false,"error":{"retryable":true,"details":{"execution_id":"exec-retry-only"}}}`)
	id, _, ok = continuingAgentctlLaunch(retryOnly)
	if !ok || id != "exec-retry-only" {
		t.Fatalf("retryable launch %q %v", id, ok)
	}
	if got := humanTurnFailure("startup_timeout", "30s"); got != "The PM harness did not start within 30 seconds. Retry, or check the runner log." {
		t.Fatalf("startup timeout %q", got)
	}
	if got := humanTurnFailure("await_failed", ""); !strings.Contains(got, "did not finish") || strings.Contains(got, "{") {
		t.Fatalf("await %q", got)
	}
	if got := humanTurnFailure("no_assistant", ""); !strings.Contains(got, "without a reply") {
		t.Fatalf("assistant %q", got)
	}
	if got := humanTurnFailure("deadline", ""); !strings.Contains(got, "deadline passed") {
		t.Fatalf("deadline %q", got)
	}
	if !agentctlNotFound([]byte(`{"ok":false,"error":{"code":"not_found","message":"execution not found"}}`), fmt.Errorf("exit status 1")) {
		t.Fatal("expected not found")
	}
	if agentctlExecutionVisible([]byte(`{"ok":false,"error":{"code":"not_found"}}`), fmt.Errorf("exit status 1")) {
		t.Fatal("not found looked visible")
	}
	if !agentctlExecutionVisible([]byte(`{"ok":true,"id":"exec-visible"}`), nil) {
		t.Fatal("expected visible")
	}
}

func TestHandleClaimedTurnRecoversExit9StartupAck(t *testing.T) {
	execID := "exec-shift-siren-during-animal-zone-opera"
	timeoutJSON := `{"ok":false,"schema_version":1,"error":{"code":"timeout","message":"background worker did not acknowledge durable startup before the deadline and may continue","retryable":true,"exit_code":9,"details":{"execution_id":"` + execID + `","timeout":"30s","worker_continues":true},"next_actions":[]},"warnings":[]}`
	var cmds []string
	restore := stubAgentctl(t, func(_ context.Context, _ string, args []string, _ string, _ []string) ([]byte, error) {
		cmds = append(cmds, strings.Join(args, " "))
		switch {
		case len(args) > 0 && args[0] == "run":
			return []byte(timeoutJSON), fmt.Errorf("exit status 9")
		case len(args) > 0 && args[0] == "await":
			if strings.Join(args, " ") != "await "+execID+" --through-execution-deadline --ignore-attention" {
				t.Fatalf("await argv %v", args)
			}
			return []byte(`{"ok":true,"id":"` + execID + `"}`), nil
		case len(args) > 1 && args[0] == "result" && args[len(args)-1] == "--content":
			return []byte("Approve the restock."), nil
		case len(args) > 0 && args[0] == "result":
			return []byte(`{"provider":"zai","model":"glm-5.3"}`), nil
		default:
			t.Fatalf("unexpected agentctl %v", args)
			return nil, nil
		}
	})
	defer restore()
	harness, posts := pmTurnHarness(t)
	settled := harness.app.handleClaimedTurn(context.Background(), nil, harness.cfg, t.TempDir(), "agentctl", []string{"omp", "-p"}, nil, claimedTurn(), nil)
	if !settled {
		t.Fatal("expected turn to complete")
	}
	if len(posts.fail) != 0 {
		t.Fatalf("failTurn called: %v", posts.fail)
	}
	if len(posts.complete) != 1 || !strings.Contains(posts.complete[0], "Approve the restock.") {
		t.Fatalf("complete %v", posts.complete)
	}
	if strings.Contains(strings.Join(posts.fail, ""), "{") {
		t.Fatal("raw json reached failTurn")
	}
	joined := strings.Join(cmds, "\n")
	if !strings.Contains(joined, "await "+execID+" --through-execution-deadline --ignore-attention") {
		t.Fatalf("missing await: %s", joined)
	}
	if strings.Contains(joined, "status ") {
		t.Fatal("status polled on successful await")
	}
	logs := harness.stderr.String()
	if !strings.Contains(logs, "completed in") || !strings.Contains(logs, "provider=zai") {
		t.Fatalf("completion log %s", logs)
	}
	if !strings.Contains(logs, timeoutJSON) {
		t.Fatal("raw agentctl json missing from runner log")
	}
}

func TestHandleClaimedTurnPollsStatusWhenAwaitNotFound(t *testing.T) {
	execID := "exec-late-register"
	timeoutJSON := `{"ok":false,"error":{"code":"timeout","retryable":true,"details":{"execution_id":"` + execID + `","timeout":"30s","worker_continues":true}}}`
	now := time.Date(2026, 9, 12, 22, 0, 0, 0, time.UTC)
	nowFn = func() time.Time { return now }
	sleepFn = func(_ context.Context, d time.Duration) error {
		now = now.Add(d)
		return nil
	}
	t.Cleanup(func() {
		nowFn = time.Now
		sleepFn = sleepCtx
		startupAckPoll = 90 * time.Second
	})
	startupAckPoll = 90 * time.Second
	statusCalls := 0
	awaitCalls := 0
	restore := stubAgentctl(t, func(_ context.Context, _ string, args []string, _ string, _ []string) ([]byte, error) {
		switch {
		case len(args) > 0 && args[0] == "run":
			return []byte(timeoutJSON), fmt.Errorf("exit status 9")
		case len(args) > 0 && args[0] == "await":
			awaitCalls++
			if awaitCalls == 1 {
				return []byte(`{"ok":false,"error":{"code":"not_found","message":"execution not found"}}`), fmt.Errorf("exit status 1")
			}
			return []byte(`{"ok":true,"id":"` + execID + `"}`), nil
		case len(args) > 0 && args[0] == "status":
			statusCalls++
			if statusCalls < 2 {
				return []byte(`{"ok":false,"error":{"code":"not_found","message":"execution not found"}}`), fmt.Errorf("exit status 1")
			}
			return []byte(`{"ok":true,"id":"` + execID + `"}`), nil
		case len(args) > 1 && args[0] == "result" && args[len(args)-1] == "--content":
			return []byte("Done."), nil
		case len(args) > 0 && args[0] == "result":
			return []byte(`{}`), nil
		default:
			t.Fatalf("unexpected agentctl %v", args)
			return nil, nil
		}
	})
	defer restore()
	harness, posts := pmTurnHarness(t)
	if !harness.app.handleClaimedTurn(context.Background(), nil, harness.cfg, t.TempDir(), "agentctl", []string{"omp"}, nil, claimedTurn(), nil) {
		t.Fatal("expected turn to complete")
	}
	if statusCalls < 2 || awaitCalls != 2 {
		t.Fatalf("status=%d await=%d", statusCalls, awaitCalls)
	}
	if len(posts.fail) != 0 || len(posts.complete) != 1 {
		t.Fatalf("posts fail=%v complete=%v", posts.fail, posts.complete)
	}
}

func TestHandleClaimedTurnFailsPlainLanguageWhenStatusNeverAppears(t *testing.T) {
	execID := "exec-missing"
	timeoutJSON := `{"ok":false,"error":{"code":"timeout","retryable":true,"details":{"execution_id":"` + execID + `","timeout":"30s","worker_continues":true}}}`
	now := time.Date(2026, 9, 12, 22, 0, 0, 0, time.UTC)
	nowFn = func() time.Time { return now }
	sleepFn = func(_ context.Context, d time.Duration) error {
		now = now.Add(d)
		return nil
	}
	t.Cleanup(func() {
		nowFn = time.Now
		sleepFn = sleepCtx
		startupAckPoll = 90 * time.Second
	})
	restore := stubAgentctl(t, func(_ context.Context, _ string, args []string, _ string, _ []string) ([]byte, error) {
		switch {
		case len(args) > 0 && args[0] == "run":
			return []byte(timeoutJSON), fmt.Errorf("exit status 9")
		case len(args) > 0 && args[0] == "await", len(args) > 0 && args[0] == "status":
			return []byte(`{"ok":false,"error":{"code":"not_found","message":"execution not found"}}`), fmt.Errorf("exit status 1")
		default:
			t.Fatalf("unexpected agentctl %v", args)
			return nil, nil
		}
	})
	defer restore()
	harness, posts := pmTurnHarness(t)
	settled := harness.app.handleClaimedTurn(context.Background(), nil, harness.cfg, t.TempDir(), "agentctl", []string{"omp"}, nil, claimedTurn(), nil)
	if !settled {
		t.Fatal("expected turn to fail")
	}
	if len(posts.complete) != 0 || len(posts.fail) != 1 {
		t.Fatalf("posts fail=%v complete=%v", posts.fail, posts.complete)
	}
	reason := posts.fail[0]
	if strings.Contains(reason, "{") || strings.Contains(reason, "execution_id") {
		t.Fatalf("raw json in failTurn %s", reason)
	}
	if reason != "The PM harness did not start within 30 seconds. Retry, or check the runner log." {
		t.Fatalf("reason %q", reason)
	}
}

func TestHandleClaimedTurnMapsOtherFailuresToPlainSentences(t *testing.T) {
	cases := []struct {
		name string
		cmd  func(args []string) ([]byte, error)
		want string
	}{
		{
			name: "await",
			cmd: func(args []string) ([]byte, error) {
				switch args[0] {
				case "run":
					return []byte(`{"ok":true,"id":"exec-ok"}`), nil
				case "await":
					return []byte(`{"ok":false,"error":{"code":"timeout","message":"deadline"}}`), fmt.Errorf("exit status 1")
				default:
					return nil, fmt.Errorf("unexpected %v", args)
				}
			},
			want: "The PM harness did not finish before the turn deadline. Retry, or check the runner log.",
		},
		{
			name: "no assistant",
			cmd: func(args []string) ([]byte, error) {
				switch args[0] {
				case "run":
					return []byte(`{"ok":true,"id":"exec-ok"}`), nil
				case "await":
					return []byte(`{"ok":true}`), nil
				case "result":
					return []byte(""), nil
				default:
					return nil, fmt.Errorf("unexpected %v", args)
				}
			},
			want: "The PM harness finished without a reply. Retry, or check the runner log.",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			restore := stubAgentctl(t, func(_ context.Context, _ string, args []string, _ string, _ []string) ([]byte, error) {
				return tc.cmd(args)
			})
			defer restore()
			harness, posts := pmTurnHarness(t)
			_ = harness.app.handleClaimedTurn(context.Background(), nil, harness.cfg, t.TempDir(), "agentctl", []string{"omp"}, nil, claimedTurn(), nil)
			if len(posts.fail) != 1 || posts.fail[0] != tc.want {
				t.Fatalf("fail %v want %q", posts.fail, tc.want)
			}
			if strings.Contains(posts.fail[0], "{") {
				t.Fatal("raw json leaked")
			}
			if !strings.Contains(harness.stderr.String(), "failed in") {
				t.Fatalf("failure log %s", harness.stderr.String())
			}
		})
	}
}

func TestRunCmdKillsGrandchildOnCancel(t *testing.T) {
	if testing.Short() {
		t.Skip("process-group kill")
	}
	dir := t.TempDir()
	pidFile := dir + "/grandchild.pid"
	script := `#!/bin/sh
/bin/sleep 120 &
echo $! > "$1"
wait
`
	prevGrace := harnessKillGrace
	harnessKillGrace = 50 * time.Millisecond
	t.Cleanup(func() { harnessKillGrace = prevGrace })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errCh := make(chan error, 1)
	go func() {
		_, _, err := runCmd(ctx, "/bin/sh", []string{"-c", script, "sh", pidFile}, dir, nil)
		errCh <- err
	}()
	var gpid int
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		raw, err := os.ReadFile(pidFile)
		if err == nil {
			n, convErr := strconv.Atoi(strings.TrimSpace(string(raw)))
			if convErr == nil && n > 0 {
				if killErr := syscall.Kill(n, 0); killErr == nil {
					gpid = n
					break
				}
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	if gpid == 0 {
		cancel()
		<-errCh
		t.Fatal("grandchild did not start")
	}
	cancel()
	select {
	case err := <-errCh:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("runCmd err=%v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("runCmd did not return after cancel")
	}
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if err := syscall.Kill(gpid, 0); err != nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("grandchild pid %d still running", gpid)
}

func TestHandleClaimedTurnKillsGrandchildOnShutdown(t *testing.T) {
	dir := t.TempDir()
	pidFile := dir + "/grandchild.pid"
	script := "/bin/sleep 120 & echo $! > '" + pidFile + "'; wait"
	prevGrace := harnessKillGrace
	harnessKillGrace = 50 * time.Millisecond
	t.Cleanup(func() { harnessKillGrace = prevGrace })
	harness, posts := pmTurnHarness(t)
	shutdownCtx, cancelShutdown := context.WithCancel(context.Background())
	harnessRoot, stopHarness := context.WithCancel(context.Background())
	done := make(chan bool, 1)
	go func() {
		done <- harness.app.handleClaimedTurn(context.Background(), shutdownCtx, harness.cfg, dir, "", []string{"/bin/sh", "-c", script, "{prompt}"}, nil, claimedTurn(), harnessRoot)
	}()
	var gpid int
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		raw, err := os.ReadFile(pidFile)
		if err == nil {
			n, convErr := strconv.Atoi(strings.TrimSpace(string(raw)))
			if convErr == nil && n > 0 {
				if killErr := syscall.Kill(n, 0); killErr == nil {
					gpid = n
					break
				}
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	if gpid == 0 {
		cancelShutdown()
		stopHarness()
		<-done
		t.Fatal("grandchild did not start")
	}
	cancelShutdown()
	stopHarness()
	select {
	case settled := <-done:
		if settled {
			t.Fatal("shutdown should not settle the turn")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("handleClaimedTurn did not return")
	}
	if len(posts.fail) != 0 || len(posts.complete) != 0 {
		t.Fatalf("posted fail=%v complete=%v", posts.fail, posts.complete)
	}
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if err := syscall.Kill(gpid, 0); err != nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("grandchild pid %d still running", gpid)
}

func TestHandleClaimedTurnPassesLeaseTokenEnv(t *testing.T) {
	var gotEnv []string
	restore := stubAgentctl(t, func(_ context.Context, _ string, args []string, _ string, env []string) ([]byte, error) {
		if len(args) > 0 && args[0] == "run" {
			gotEnv = append([]string{}, env...)
		}
		switch {
		case len(args) > 0 && args[0] == "run":
			return []byte(`{"ok":true,"id":"exec-ok"}`), nil
		case len(args) > 0 && args[0] == "await":
			return []byte(`{"ok":true}`), nil
		case len(args) > 1 && args[0] == "result" && args[len(args)-1] == "--content":
			return []byte("Approve the restock."), nil
		case len(args) > 0 && args[0] == "result":
			return []byte(`{}`), nil
		default:
			t.Fatalf("unexpected agentctl %v", args)
			return nil, nil
		}
	})
	defer restore()
	harness, posts := pmTurnHarness(t)
	if !harness.app.handleClaimedTurn(context.Background(), nil, harness.cfg, t.TempDir(), "agentctl", []string{"omp"}, []string{"PATH=/bin"}, claimedTurn(), nil) {
		t.Fatal("expected turn to complete")
	}
	if len(posts.fail) != 0 {
		t.Fatalf("fail %v", posts.fail)
	}
	if envValue(gotEnv, "ANX_PM_LEASE_TOKEN") != "lease-1" {
		t.Fatalf("ANX_PM_LEASE_TOKEN=%q env=%v", envValue(gotEnv, "ANX_PM_LEASE_TOKEN"), gotEnv)
	}
}

func TestHandleClaimedTurnAgentctlShutdownLogsCancelHint(t *testing.T) {
	execID := "exec-outlives-runner"
	started := make(chan struct{})
	restore := stubAgentctl(t, func(ctx context.Context, _ string, args []string, _ string, _ []string) ([]byte, error) {
		switch {
		case len(args) > 0 && args[0] == "run":
			return []byte(`{"ok":true,"id":"` + execID + `"}`), nil
		case len(args) > 0 && args[0] == "await":
			select {
			case <-started:
			default:
				close(started)
			}
			<-ctx.Done()
			return nil, ctx.Err()
		default:
			t.Fatalf("unexpected agentctl %v", args)
			return nil, nil
		}
	})
	defer restore()
	harness, posts := pmTurnHarness(t)
	shutdownCtx, cancelShutdown := context.WithCancel(context.Background())
	harnessRoot, stopHarness := context.WithCancel(context.Background())
	done := make(chan bool, 1)
	go func() {
		done <- harness.app.handleClaimedTurn(context.Background(), shutdownCtx, harness.cfg, t.TempDir(), "agentctl", []string{"omp"}, nil, claimedTurn(), harnessRoot)
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("await did not start")
	}
	cancelShutdown()
	stopHarness()
	select {
	case settled := <-done:
		if settled {
			t.Fatal("shutdown should not settle the turn")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("handleClaimedTurn did not return")
	}
	if len(posts.fail) != 0 || len(posts.complete) != 0 {
		t.Fatalf("posted fail=%v complete=%v", posts.fail, posts.complete)
	}
	logs := harness.stderr.String()
	if !strings.Contains(logs, "background execution "+execID+" outlives this runner") {
		t.Fatalf("missing outlives log: %s", logs)
	}
	if !strings.Contains(logs, "agentctl cancel "+execID) {
		t.Fatalf("missing cancel hint: %s", logs)
	}
}

func TestRunCmdSeparatesStdoutAndStderr(t *testing.T) {
	stdout, stderr, err := runCmd(context.Background(), "/bin/sh", []string{"-c", "printf '%s\\n' stdout-line; printf '%s\\n' stderr-line >&2"}, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(stdout), "stderr-line") {
		t.Fatalf("stderr leaked into stdout: %q", stdout)
	}
	if !strings.Contains(string(stdout), "stdout-line") {
		t.Fatalf("stdout=%q", stdout)
	}
	if !strings.Contains(string(stderr), "stderr-line") {
		t.Fatalf("stderr=%q", stderr)
	}
	if strings.Contains(string(stderr), "stdout-line") {
		t.Fatalf("stdout leaked into stderr: %q", stderr)
	}
}

func TestHandleClaimedTurnDirectRunnerKeepsStderrOutOfAnswer(t *testing.T) {
	harness, posts := pmTurnHarness(t)
	settled := harness.app.handleClaimedTurn(context.Background(), nil, harness.cfg, t.TempDir(), "", []string{"/bin/sh", "-c", "printf '%s\\n' 'harness log line' >&2; printf '%s\\n' 'Approve the restock.'", "{prompt}"}, nil, claimedTurn(), nil)
	if !settled {
		t.Fatal("expected turn to complete")
	}
	if len(posts.fail) != 0 || len(posts.complete) != 1 {
		t.Fatalf("posts fail=%v complete=%v", posts.fail, posts.complete)
	}
	if !strings.Contains(posts.complete[0], "Approve the restock.") {
		t.Fatalf("complete %v", posts.complete)
	}
	if strings.Contains(posts.complete[0], "harness log line") {
		t.Fatalf("stderr leaked into answer: %s", posts.complete[0])
	}
	logs := harness.stderr.String()
	if !strings.Contains(logs, "harness log line") {
		t.Fatalf("stderr missing from runner log: %s", logs)
	}
	if strings.Contains(logs, "using combined output") {
		t.Fatalf("unexpected combined fallback: %s", logs)
	}
}

func TestHandleClaimedTurnDirectRunnerFailsWhenStdoutHasNoAssistant(t *testing.T) {
	harness, posts := pmTurnHarness(t)
	settled := harness.app.handleClaimedTurn(context.Background(), nil, harness.cfg, t.TempDir(), "", []string{"/bin/sh", "-c", "printf '%s\\n' 'Approve from stderr.' >&2", "{prompt}"}, nil, claimedTurn(), nil)
	if !settled {
		t.Fatal("expected no_assistant failure")
	}
	if len(posts.complete) != 0 {
		t.Fatalf("posted stderr as reply: %v", posts.complete)
	}
	if len(posts.fail) != 1 || !strings.Contains(posts.fail[0], "without a reply") {
		t.Fatalf("fail %v", posts.fail)
	}
	logs := harness.stderr.String()
	if !strings.Contains(logs, "Approve from stderr.") {
		t.Fatalf("stderr missing from runner log: %s", logs)
	}
	if strings.Contains(logs, "using combined output") {
		t.Fatalf("combined fallback still present: %s", logs)
	}
}

func TestAssistantTextFromRunnerOutputUsesStdoutOnly(t *testing.T) {
	if got := assistantTextFromRunnerOutput(nil); got != "" {
		t.Fatalf("empty stdout %q", got)
	}
	if got := assistantTextFromRunnerOutput([]byte("Approve the restock.")); got != "Approve the restock." {
		t.Fatalf("stdout %q", got)
	}
}

func TestHandleClaimedTurnFailsWhenZAIAPIKeyMissing(t *testing.T) {
	harness, posts := pmTurnHarness(t)
	settled := harness.app.handleClaimedTurn(context.Background(), nil, harness.cfg, t.TempDir(), "", []string{"/bin/sh", "-c", "printf '%s\\n' 'should not run'", "--model", "zai/glm-5.3", "{prompt}"}, []string{"PATH=/bin"}, claimedTurn(), nil)
	if !settled {
		t.Fatal("expected missing ZAI_API_KEY failure")
	}
	if len(posts.complete) != 0 {
		t.Fatalf("complete %v", posts.complete)
	}
	if len(posts.fail) != 1 || !strings.Contains(posts.fail[0], "ZAI_API_KEY") {
		t.Fatalf("fail %v", posts.fail)
	}
	if strings.Contains(posts.fail[0], "should not run") {
		t.Fatalf("posted runner output as failure: %v", posts.fail)
	}
}

func TestHandleClaimedTurnRunsWhenZAIAPIKeyExported(t *testing.T) {
	harness, posts := pmTurnHarness(t)
	settled := harness.app.handleClaimedTurn(context.Background(), nil, harness.cfg, t.TempDir(), "", []string{"/bin/sh", "-c", "printf '%s\\n' 'Approve the restock.'", "--model", "zai/glm-5.3", "{prompt}"}, []string{"PATH=/bin", "ZAI_API_KEY=from-operator"}, claimedTurn(), nil)
	if !settled {
		t.Fatal("expected turn to complete")
	}
	if len(posts.fail) != 0 || len(posts.complete) != 1 {
		t.Fatalf("posts fail=%v complete=%v", posts.fail, posts.complete)
	}
	if !strings.Contains(posts.complete[0], "Approve the restock.") {
		t.Fatalf("complete %v", posts.complete)
	}
}

func TestHandleClaimedTurnAgentctlKeepsStderrOutOfAnswer(t *testing.T) {
	restore := stubAgentctlStreams(t, func(_ context.Context, _ string, args []string, _ string, _ []string) ([]byte, []byte, error) {
		switch {
		case len(args) > 0 && args[0] == "run":
			return []byte(`{"ok":true,"id":"exec-ok"}`), []byte("launch log\n"), nil
		case len(args) > 0 && args[0] == "await":
			return []byte(`{"ok":true}`), []byte("await log\n"), nil
		case len(args) > 1 && args[0] == "result" && args[len(args)-1] == "--content":
			return []byte("Approve the restock."), []byte("harness log line\n"), nil
		case len(args) > 0 && args[0] == "result":
			return []byte(`{"provider":"zai","model":"glm-5.3"}`), nil, nil
		default:
			t.Fatalf("unexpected agentctl %v", args)
			return nil, nil, nil
		}
	})
	defer restore()
	harness, posts := pmTurnHarness(t)
	if !harness.app.handleClaimedTurn(context.Background(), nil, harness.cfg, t.TempDir(), "agentctl", []string{"omp", "-p"}, nil, claimedTurn(), nil) {
		t.Fatal("expected turn to complete")
	}
	if len(posts.fail) != 0 || len(posts.complete) != 1 {
		t.Fatalf("posts fail=%v complete=%v", posts.fail, posts.complete)
	}
	if !strings.Contains(posts.complete[0], "Approve the restock.") {
		t.Fatalf("complete %v", posts.complete)
	}
	if strings.Contains(posts.complete[0], "harness log line") {
		t.Fatalf("stderr leaked into answer: %s", posts.complete[0])
	}
	logs := harness.stderr.String()
	if !strings.Contains(logs, "harness log line") {
		t.Fatalf("stderr missing from runner log: %s", logs)
	}
}

func stubAgentctl(t *testing.T, fn func(ctx context.Context, name string, args []string, dir string, env []string) ([]byte, error)) func() {
	t.Helper()
	return stubAgentctlStreams(t, func(ctx context.Context, name string, args []string, dir string, env []string) ([]byte, []byte, error) {
		out, err := fn(ctx, name, args, dir, env)
		return out, nil, err
	})
}

func stubAgentctlStreams(t *testing.T, fn func(ctx context.Context, name string, args []string, dir string, env []string) ([]byte, []byte, error)) func() {
	t.Helper()
	prev := runCmd
	runCmd = fn
	return func() { runCmd = prev }
}

type pmTurnPosts struct {
	complete []string
	fail     []string
}

type pmTurnTest struct {
	app    *App
	cfg    config.Resolved
	stderr *bytes.Buffer
}

func pmTurnHarness(t *testing.T) (pmTurnTest, *pmTurnPosts) {
	t.Helper()
	posts := &pmTurnPosts{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		switch {
		case strings.HasSuffix(r.URL.Path, "/complete"):
			posts.complete = append(posts.complete, string(body))
			io.WriteString(w, `{"id":"turn-1","status":"delivered"}`)
		case strings.HasSuffix(r.URL.Path, "/fail"):
			var payload map[string]any
			_ = json.Unmarshal(body, &payload)
			posts.fail = append(posts.fail, anyString(payload["reason"]))
			io.WriteString(w, `{"id":"turn-1","status":"failed"}`)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	stderr := &bytes.Buffer{}
	app := New()
	app.Stderr = stderr
	app.Stdout = io.Discard
	app.Getenv = func(string) string { return "" }
	app.UserHomeDir = func() (string, error) { return t.TempDir(), nil }
	return pmTurnTest{app: app, cfg: config.Resolved{BaseURL: srv.URL, AccessToken: "fixture", Timeout: 5 * time.Second, Agent: "pm"}, stderr: stderr}, posts
}

func claimedTurn() map[string]any {
	return map[string]any{
		"id":          "turn-1",
		"lease_token": "lease-1",
		"lease_owner": "runner-1",
		"deadline":    time.Now().Add(2 * time.Minute).UTC().Format(time.RFC3339Nano),
		"text":        "What needs my decision?",
	}
}

func stubTerminalSleep(t *testing.T) {
	t.Helper()
	prev := sleepFn
	sleepFn = func(ctx context.Context, _ time.Duration) error {
		if ctx != nil && ctx.Err() != nil {
			return ctx.Err()
		}
		return nil
	}
	t.Cleanup(func() { sleepFn = prev })
}

func TestTruncateToMaxBytesKeepsUTF8RuneBoundaries(t *testing.T) {
	multi := "héllo世界" // é is 2 bytes; 世 and 界 are 3 bytes each
	if got := truncateToMaxBytes(multi, len(multi)); got != multi {
		t.Fatalf("full budget: got %q", got)
	}
	if got := truncateToMaxBytes("é", 1); got != "" {
		t.Fatalf("cut mid-rune should be empty, got %q bytes=%v", got, []byte(got))
	}
	world := "世界"
	if got := truncateToMaxBytes(world, 3); got != "世" {
		t.Fatalf("first CJK rune: got %q", got)
	}
	if got := truncateToMaxBytes(world, 4); got != "世" {
		t.Fatalf("mid second rune must not split UTF-8, got %q bytes=%v", got, []byte(got))
	}
	if got := truncateToMaxBytes("abc", 2); got != "ab" {
		t.Fatalf("ascii: got %q", got)
	}
	invalidMid := "abcdefghij\xffklmnopqrstuvwxyz"
	got := truncateToMaxBytes(invalidMid, 20)
	if !strings.Contains(got, "\xff") {
		t.Fatalf("invalid byte in the middle should be kept, got %q bytes=%v", got, []byte(got))
	}
	if len(got) != 20 {
		t.Fatalf("budget 20 with ASCII tail: got %d bytes %q", len(got), got)
	}
}

func TestClaimErrorRetryable(t *testing.T) {
	if claimErrorRetryable(errnorm.FromHTTPFailure(http.StatusUnauthorized, nil)) {
		t.Fatal("401 should not retry")
	}
	if claimErrorRetryable(errnorm.FromHTTPFailure(http.StatusForbidden, nil)) {
		t.Fatal("403 should not retry")
	}
	if !claimErrorRetryable(errnorm.FromHTTPFailure(http.StatusServiceUnavailable, nil)) {
		t.Fatal("503 should retry")
	}
	if !claimErrorRetryable(errnorm.Network("request_failed", "connection refused")) {
		t.Fatal("network should retry")
	}
}

func TestHarnessCmdFailureDistinguishesStartExitAndDeadline(t *testing.T) {
	if got := harnessCmdFailure(context.DeadlineExceeded); got != humanTurnFailure("await_failed", "") {
		t.Fatalf("deadline %q", got)
	}
	if got := harnessCmdFailure(os.ErrDeadlineExceeded); got != humanTurnFailure("await_failed", "") {
		t.Fatalf("wait delay %q", got)
	}
	cmd := exec.Command("/bin/sh", "-c", "exit 7")
	err := cmd.Run()
	want := "The PM harness exited with an error (exit 7). Retry, or check the runner log."
	if got := harnessCmdFailure(err); got != want {
		t.Fatalf("exit 7: %q", got)
	}
	if got := harnessCmdFailure(exec.ErrNotFound); got != "The PM harness failed to start. Retry, or check the runner log." {
		t.Fatalf("exec error %q", got)
	}
}

func TestHandleClaimedTurnDirectRunnerMapsStartExitAndDeadline(t *testing.T) {
	t.Run("exec error", func(t *testing.T) {
		harness, posts := pmTurnHarness(t)
		settled := harness.app.handleClaimedTurn(context.Background(), nil, harness.cfg, t.TempDir(), "", []string{"/no/such/pm-harness", "{prompt}"}, nil, claimedTurn(), nil)
		if !settled {
			t.Fatal("expected start failure")
		}
		if len(posts.fail) != 1 || posts.fail[0] != "The PM harness failed to start. Retry, or check the runner log." {
			t.Fatalf("fail %v", posts.fail)
		}
		if strings.Contains(harness.stderr.String(), "agentctl:") {
			t.Fatalf("direct path logged agentctl prefix: %s", harness.stderr.String())
		}
	})
	t.Run("exit 1 with stdout", func(t *testing.T) {
		harness, posts := pmTurnHarness(t)
		settled := harness.app.handleClaimedTurn(context.Background(), nil, harness.cfg, t.TempDir(), "", []string{"/bin/sh", "-c", "printf '%s\\n' 'harness wrote stdout'; exit 1", "{prompt}"}, nil, claimedTurn(), nil)
		if !settled {
			t.Fatal("expected exit failure")
		}
		want := "The PM harness exited with an error (exit 1). Retry, or check the runner log."
		if len(posts.fail) != 1 || posts.fail[0] != want {
			t.Fatalf("fail %v", posts.fail)
		}
		logs := harness.stderr.String()
		if strings.Contains(logs, "agentctl:") {
			t.Fatalf("direct path logged agentctl prefix: %s", logs)
		}
		if !strings.Contains(logs, "runner:") || !strings.Contains(logs, "harness wrote stdout") {
			t.Fatalf("expected runner log of stdout, got %s", logs)
		}
	})
	t.Run("deadline", func(t *testing.T) {
		restore := stubAgentctl(t, func(ctx context.Context, _ string, _ []string, _ string, _ []string) ([]byte, error) {
			return nil, context.DeadlineExceeded
		})
		defer restore()
		harness, posts := pmTurnHarness(t)
		settled := harness.app.handleClaimedTurn(context.Background(), nil, harness.cfg, t.TempDir(), "", []string{"/bin/true", "{prompt}"}, nil, claimedTurn(), nil)
		if !settled {
			t.Fatal("expected deadline failure")
		}
		if len(posts.fail) != 1 || posts.fail[0] != humanTurnFailure("await_failed", "") {
			t.Fatalf("fail %v", posts.fail)
		}
	})
}

func TestHandleClaimedTurnDirectRunnerCompletesWhenGrandchildHoldsStdout(t *testing.T) {
	prevDelay := harnessWaitDelay
	harnessWaitDelay = 400 * time.Millisecond
	t.Cleanup(func() { harnessWaitDelay = prevDelay })
	harness, posts := pmTurnHarness(t)
	harness.cfg.Timeout = 20 * time.Second
	started := time.Now()
	settled := harness.app.handleClaimedTurn(context.Background(), nil, harness.cfg, t.TempDir(), "", []string{"/bin/sh", "-c", "(sleep 20 &); printf '%s\\n' 'Launch is ready'; exit 0", "{prompt}"}, nil, claimedTurn(), nil)
	elapsed := time.Since(started)
	if !settled {
		t.Fatal("expected completed turn")
	}
	if elapsed > 8*time.Second {
		t.Fatalf("grandchild holding stdout blocked Wait for %s", elapsed)
	}
	if len(posts.fail) != 0 {
		t.Fatalf("fail %v", posts.fail)
	}
	if len(posts.complete) != 1 || !strings.Contains(posts.complete[0], "Launch is ready") {
		t.Fatalf("complete %v", posts.complete)
	}
}

func TestHandleClaimedTurnDirectRunnerNeverExitsMapsDeadline(t *testing.T) {
	harness, posts := pmTurnHarness(t)
	harness.cfg.Timeout = 20 * time.Second
	turn := claimedTurn()
	turn["deadline"] = time.Now().Add(2 * time.Second).UTC().Format(time.RFC3339Nano)
	settled := harness.app.handleClaimedTurn(context.Background(), nil, harness.cfg, t.TempDir(), "", []string{"/bin/sh", "-c", "sleep 60", "{prompt}"}, nil, turn, nil)
	if !settled {
		t.Fatal("expected deadline failure")
	}
	if len(posts.fail) != 1 || posts.fail[0] != humanTurnFailure("await_failed", "") {
		t.Fatalf("fail %v", posts.fail)
	}
	if strings.Contains(strings.Join(posts.fail, "\n"), "before the harness started") {
		t.Fatalf("wall timeout mapped to start failure: %v", posts.fail)
	}
}

func TestPMServeExitsAfterThreeForbiddenClaims(t *testing.T) {
	var claims int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/pm/turns/claim" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
			return
		}
		claims++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		io.WriteString(w, `{"error":{"code":"forbidden","message":"PM permission denied"}}`)
	}))
	t.Cleanup(srv.Close)
	sleepFn = func(context.Context, time.Duration) error { return nil }
	t.Cleanup(func() { sleepFn = sleepCtx })
	stderr := &bytes.Buffer{}
	app := New()
	app.Stderr = stderr
	app.Stdout = io.Discard
	app.Getenv = func(string) string { return "" }
	app.UserHomeDir = func() (string, error) { return t.TempDir(), nil }
	cfg := config.Resolved{BaseURL: srv.URL, AccessToken: "fixture", Timeout: 5 * time.Second, Agent: "maya"}
	_, err := app.runPMServe(context.Background(), []string{"--runner", "/bin/true {prompt}", "--poll-interval", "200ms", "--work-dir", t.TempDir()}, cfg)
	if err == nil || !strings.Contains(err.Error(), "Claim failed with a forbidden error 3 times. Exiting.") {
		t.Fatalf("err=%v", err)
	}
	if claims != 3 {
		t.Fatalf("claims=%d", claims)
	}
	logs := stderr.String()
	wantWhy := "claim forbidden: pm.respond requires the configured PM actor (ANX_PM_AGENT_ACTOR_ID); this profile is maya"
	if !strings.Contains(logs, wantWhy) {
		t.Fatalf("missing first forbidden explanation: %s", logs)
	}
	if strings.Count(logs, wantWhy) != 1 {
		t.Fatalf("expected the forbidden explanation once, got %s", logs)
	}
	if !strings.Contains(logs, "claim failed (forbidden); retrying in 1s, 1 of 3") {
		t.Fatalf("missing first backoff log: %s", logs)
	}
	if strings.Count(logs, "claim failed (forbidden); retrying in") != 2 {
		t.Fatalf("expected a backoff log for each retry, got %s", logs)
	}
	if strings.Contains(logs, "1 of 10") || strings.Contains(logs, "non-retryable error 10") {
		t.Fatalf("forbidden claims used the long budget: %s", logs)
	}
	if !strings.Contains(logs, "Claim failed with a forbidden error 3 times. Exiting.") {
		t.Fatalf("missing final exit sentence: %s", logs)
	}
}

func TestPMServeBacksOffAndExitsOnNonRetryableClaim(t *testing.T) {
	var claims int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/pm/turns/claim" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
			return
		}
		claims++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		io.WriteString(w, `{"error":{"code":"auth_required","message":"missing bearer"}}`)
	}))
	t.Cleanup(srv.Close)
	sleepFn = func(context.Context, time.Duration) error { return nil }
	t.Cleanup(func() { sleepFn = sleepCtx })
	stderr := &bytes.Buffer{}
	app := New()
	app.Stderr = stderr
	app.Stdout = io.Discard
	app.Getenv = func(string) string { return "" }
	app.UserHomeDir = func() (string, error) { return t.TempDir(), nil }
	cfg := config.Resolved{BaseURL: srv.URL, AccessToken: "fixture", Timeout: 5 * time.Second, Agent: "pm"}
	_, err := app.runPMServe(context.Background(), []string{"--runner", "/bin/true {prompt}", "--poll-interval", "200ms", "--work-dir", t.TempDir()}, cfg)
	if err == nil || !strings.Contains(err.Error(), "Claim failed with a non-retryable error 10 times. Exiting.") {
		t.Fatalf("err=%v", err)
	}
	if claims != 10 {
		t.Fatalf("claims=%d", claims)
	}
	logs := stderr.String()
	if strings.Contains(logs, "claim forbidden:") {
		t.Fatalf("401 used the forbidden path: %s", logs)
	}
	if !strings.Contains(logs, "claim failed (auth required); retrying in 1s, 1 of 10") {
		t.Fatalf("missing first backoff log: %s", logs)
	}
	if strings.Count(logs, "claim failed (auth required); retrying in") != 9 {
		t.Fatalf("expected a backoff log for each retry, got %s", logs)
	}
	if !strings.Contains(logs, "Claim failed with a non-retryable error 10 times. Exiting.") {
		t.Fatalf("missing final exit sentence: %s", logs)
	}
}

func TestPMServeKeepsPollingTransientClaimErrors(t *testing.T) {
	claimed := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-claimed:
		default:
			close(claimed)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		io.WriteString(w, `{"error":{"code":"unavailable","message":"try again"}}`)
	}))
	t.Cleanup(srv.Close)
	sleepFn = func(context.Context, time.Duration) error { return nil }
	t.Cleanup(func() { sleepFn = sleepCtx })
	ctx, cancel := context.WithCancel(context.Background())
	stderr := &lockedBuffer{}
	app := New()
	app.Stderr = stderr
	app.Stdout = io.Discard
	app.Getenv = func(string) string { return "" }
	app.UserHomeDir = func() (string, error) { return t.TempDir(), nil }
	done := make(chan error, 1)
	go func() {
		_, err := app.runPMServe(ctx, []string{"--runner", "/bin/true {prompt}", "--poll-interval", "200ms", "--work-dir", t.TempDir()}, config.Resolved{BaseURL: srv.URL, AccessToken: "fixture", Timeout: 5 * time.Second, Agent: "pm"})
		done <- err
	}()
	select {
	case <-claimed:
	case <-time.After(2 * time.Second):
		t.Fatal("transient claim did not run")
	}
	// The response has been sent; the runner logs the failure just after.
	deadline := time.Now().Add(2 * time.Second)
	for !strings.Contains(stderr.String(), "claim failed:") && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	err := <-done
	if err != nil {
		t.Fatalf("clean stop should return nil, got %v", err)
	}
	if !strings.Contains(stderr.String(), "claim failed:") {
		t.Fatalf("expected transient claim logs, got %s", stderr.String())
	}
	if strings.Contains(stderr.String(), "non-retryable") {
		t.Fatalf("transient errors exited: %s", stderr.String())
	}
}

func TestPMServeReleasesHeldTurnOnShutdown(t *testing.T) {
	var (
		mu       sync.Mutex
		released []map[string]any
		claimed  bool
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/pm/turns/claim":
			mu.Lock()
			already := claimed
			claimed = true
			mu.Unlock()
			if already {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			io.WriteString(w, `{"id":"turn-held","lease_token":"lease-held","deadline":"`+time.Now().Add(2*time.Minute).UTC().Format(time.RFC3339Nano)+`","text":"hi"}`)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/release"):
			body, _ := io.ReadAll(r.Body)
			payload := requirePMReleaseContractBody(t, body)
			if anyString(payload["runner_id"]) == "" || anyString(payload["lease_token"]) == "" {
				w.WriteHeader(http.StatusBadRequest)
				io.WriteString(w, `{"error":{"code":"invalid_request","message":"invalid PM request"}}`)
				return
			}
			if payload["lease_token"] != "lease-held" {
				t.Errorf("lease_token=%v", payload["lease_token"])
			}
			mu.Lock()
			released = append(released, payload)
			mu.Unlock()
			io.WriteString(w, `{"id":"turn-held","status":"sending"}`)
		case strings.HasSuffix(r.URL.Path, "/complete"), strings.HasSuffix(r.URL.Path, "/fail"):
			t.Errorf("shutdown should release, not complete/fail: %s", r.URL.Path)
			http.NotFound(w, r)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	started := make(chan struct{})
	restore := stubAgentctl(t, func(ctx context.Context, _ string, _ []string, _ string, _ []string) ([]byte, error) {
		select {
		case <-started:
		default:
			close(started)
		}
		<-ctx.Done()
		return nil, ctx.Err()
	})
	defer restore()
	prevGrace := pmServeShutdownGrace
	pmServeShutdownGrace = 30 * time.Millisecond
	t.Cleanup(func() { pmServeShutdownGrace = prevGrace })
	stderr := &bytes.Buffer{}
	app := New()
	app.Stderr = stderr
	app.Stdout = io.Discard
	app.Getenv = func(string) string { return "" }
	app.UserHomeDir = func() (string, error) { return t.TempDir(), nil }
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct {
		result *commandResult
		err    error
	}, 1)
	go func() {
		result, err := app.runPMServe(ctx, []string{"--runner", "/bin/true {prompt}", "--poll-interval", "200ms", "--work-dir", t.TempDir()}, config.Resolved{BaseURL: srv.URL, AccessToken: "fixture", Timeout: 5 * time.Second, Agent: "pm"})
		done <- struct {
			result *commandResult
			err    error
		}{result, err}
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("harness did not start")
	}
	cancel()
	select {
	case got := <-done:
		if got.err != nil {
			t.Fatalf("clean stop should return nil, got %v", got.err)
		}
		if got.result == nil || got.result.Text != "pm serve stopped; released 1 turn(s)" {
			t.Fatalf("result=%#v", got.result)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("pm serve did not stop")
	}
	mu.Lock()
	got := append([]map[string]any{}, released...)
	mu.Unlock()
	if len(got) != 1 {
		t.Fatalf("release %v", got)
	}
	if anyString(got[0]["runner_id"]) == "" || anyString(got[0]["lease_token"]) != "lease-held" {
		t.Fatalf("release body %v", got[0])
	}
	if !strings.Contains(stderr.String(), "released turn turn-held on shutdown") {
		t.Fatalf("expected release log, got %s", stderr.String())
	}
}

func requirePMReleaseContractBody(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Errorf("release body is not JSON: %v %s", err, body)
		return map[string]any{}
	}
	if anyString(payload["runner_id"]) == "" || anyString(payload["lease_token"]) == "" {
		t.Errorf("release body missing runner_id or lease_token: %s", body)
	}
	return payload
}

func TestReleaseTurnPostsRunnerIDAndLeaseToken(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/pm/turns/turn-1/release" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
			return
		}
		body, _ := io.ReadAll(r.Body)
		got = requirePMReleaseContractBody(t, body)
		if anyString(got["runner_id"]) == "" || anyString(got["lease_token"]) == "" {
			w.WriteHeader(http.StatusBadRequest)
			io.WriteString(w, `{"error":{"code":"invalid_request","message":"invalid PM request"}}`)
			return
		}
		io.WriteString(w, `{"id":"turn-1","status":"sending","claimed":false}`)
	}))
	t.Cleanup(srv.Close)
	app := New()
	app.Stderr = io.Discard
	app.Stdout = io.Discard
	app.Getenv = func(string) string { return "" }
	app.UserHomeDir = func() (string, error) { return t.TempDir(), nil }
	err := app.releaseTurn(context.Background(), config.Resolved{BaseURL: srv.URL, AccessToken: "fixture", Timeout: 5 * time.Second}, "runner-abc", "turn-1", "lease-xyz")
	if err != nil {
		t.Fatal(err)
	}
	if anyString(got["runner_id"]) != "runner-abc" || anyString(got["lease_token"]) != "lease-xyz" {
		t.Fatalf("body=%v", got)
	}
}

func TestHandleClaimedTurnDoesNotFailWhenShutdownBegun(t *testing.T) {
	harness, posts := pmTurnHarness(t)
	shutdownCtx, cancel := context.WithCancel(context.Background())
	cancel()
	settled := harness.app.handleClaimedTurn(context.Background(), shutdownCtx, harness.cfg, t.TempDir(), "", []string{"/bin/sh", "-c", "exit 1", "{prompt}"}, nil, claimedTurn(), nil)
	if settled {
		t.Fatal("shutdown death should not settle the turn")
	}
	if len(posts.fail) != 0 || len(posts.complete) != 0 {
		t.Fatalf("posted fail=%v complete=%v", posts.fail, posts.complete)
	}
}

func TestPMServeCleanStopReturnsNil(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/pm/turns/claim" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)
	sleepFn = func(ctx context.Context, _ time.Duration) error { return ctx.Err() }
	t.Cleanup(func() { sleepFn = sleepCtx })
	stderr := &bytes.Buffer{}
	app := New()
	app.Stderr = stderr
	app.Stdout = io.Discard
	app.Getenv = func(string) string { return "" }
	app.UserHomeDir = func() (string, error) { return t.TempDir(), nil }
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err := app.runPMServe(ctx, []string{"--runner", "/bin/true {prompt}", "--poll-interval", "200ms", "--work-dir", t.TempDir()}, config.Resolved{BaseURL: srv.URL, AccessToken: "fixture", Timeout: 5 * time.Second, Agent: "pm"})
	if err != nil {
		t.Fatalf("clean stop should return nil, got %v", err)
	}
	if result == nil || result.Text != "pm serve stopped; released 0 turn(s)" {
		t.Fatalf("result=%#v", result)
	}
}

func TestPMServeSignalDuringRunReleasesInsteadOfFailing(t *testing.T) {
	exitErr := exec.Command("/bin/sh", "-c", "exit 1").Run()
	if exitErr == nil {
		t.Fatal("expected exit error")
	}
	for _, sig := range []os.Signal{os.Interrupt, syscall.SIGTERM} {
		t.Run(sig.String(), func(t *testing.T) {
			var (
				mu       sync.Mutex
				released []map[string]any
				claimed  bool
			)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch {
				case r.Method == http.MethodPost && r.URL.Path == "/pm/turns/claim":
					mu.Lock()
					already := claimed
					claimed = true
					mu.Unlock()
					if already {
						w.WriteHeader(http.StatusNoContent)
						return
					}
					io.WriteString(w, `{"id":"turn-held","lease_token":"lease-held","deadline":"`+time.Now().Add(2*time.Minute).UTC().Format(time.RFC3339Nano)+`","text":"hi"}`)
				case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/release"):
					body, _ := io.ReadAll(r.Body)
					payload := requirePMReleaseContractBody(t, body)
					if anyString(payload["runner_id"]) == "" || anyString(payload["lease_token"]) == "" {
						w.WriteHeader(http.StatusBadRequest)
						io.WriteString(w, `{"error":{"code":"invalid_request","message":"invalid PM request"}}`)
						return
					}
					mu.Lock()
					released = append(released, payload)
					mu.Unlock()
					io.WriteString(w, `{"id":"turn-held","status":"sending","claimed":false}`)
				case strings.HasSuffix(r.URL.Path, "/complete"), strings.HasSuffix(r.URL.Path, "/fail"):
					t.Errorf("shutdown should release, not complete/fail: %s", r.URL.Path)
					http.NotFound(w, r)
				default:
					t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
					http.NotFound(w, r)
				}
			}))
			t.Cleanup(srv.Close)
			started := make(chan struct{})
			restore := stubAgentctl(t, func(ctx context.Context, _ string, _ []string, _ string, _ []string) ([]byte, error) {
				select {
				case <-started:
				default:
					close(started)
				}
				<-ctx.Done()
				return nil, exitErr
			})
			defer restore()
			prevGrace := pmServeShutdownGrace
			pmServeShutdownGrace = time.Second
			t.Cleanup(func() { pmServeShutdownGrace = prevGrace })
			stderr := &lockedBuffer{}
			app := New()
			app.Stderr = stderr
			app.Stdout = io.Discard
			app.Getenv = func(string) string { return "" }
			app.UserHomeDir = func() (string, error) { return t.TempDir(), nil }
			done := make(chan struct {
				result *commandResult
				err    error
			}, 1)
			go func() {
				result, err := app.runPMServe(context.Background(), []string{"--runner", "/bin/true {prompt}", "--poll-interval", "200ms", "--work-dir", t.TempDir()}, config.Resolved{BaseURL: srv.URL, AccessToken: "fixture", Timeout: 5 * time.Second, Agent: "pm"})
				done <- struct {
					result *commandResult
					err    error
				}{result, err}
			}()
			select {
			case <-started:
			case <-time.After(2 * time.Second):
				t.Fatal("harness did not start")
			}
			if s, ok := sig.(syscall.Signal); ok {
				if err := syscall.Kill(os.Getpid(), s); err != nil {
					t.Fatal(err)
				}
			}
			select {
			case got := <-done:
				if got.err != nil {
					t.Fatalf("signal stop should return nil, got %v", got.err)
				}
				if got.result == nil || got.result.Text != "pm serve stopped; released 1 turn(s)" {
					t.Fatalf("result=%#v", got.result)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("pm serve did not stop")
			}
			mu.Lock()
			got := append([]map[string]any{}, released...)
			mu.Unlock()
			if len(got) != 1 || anyString(got[0]["runner_id"]) == "" || anyString(got[0]["lease_token"]) != "lease-held" {
				t.Fatalf("release %v", got)
			}
			logs := stderr.String()
			if !strings.Contains(logs, "released turn turn-held on shutdown") {
				t.Fatalf("expected release log, got %s", logs)
			}
			if strings.Contains(logs, "failed in") || strings.Contains(logs, "The PM harness exited") {
				t.Fatalf("harness death failed the turn: %s", logs)
			}
		})
	}
}

// lockedBuffer is a bytes.Buffer safe to read while the runner goroutine writes.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func TestClipReplyForTurnAddsTruncationMarker(t *testing.T) {
	text := strings.Repeat("a", 80)
	got, dropped := clipReplyForTurn(text, 60)
	if dropped <= 0 {
		t.Fatalf("expected dropped bytes, got %d", dropped)
	}
	if !strings.Contains(got, "[reply truncated by anx pm serve at 60 bytes;") || !strings.Contains(got, "bytes were dropped]") {
		t.Fatalf("missing marker: %q", got)
	}
	if len(got) > 60 {
		t.Fatalf("clipped reply exceeded limit: %d", len(got))
	}
	if clip, n := clipReplyForTurn("short", 60); clip != "short" || n != 0 {
		t.Fatalf("short text should pass through: %q %d", clip, n)
	}
}

func TestCompleteTurnTruncatesWithMarkerAndKeepsEvidence(t *testing.T) {
	var completeBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/complete"):
			body, _ := io.ReadAll(r.Body)
			if err := json.Unmarshal(body, &completeBody); err != nil {
				t.Errorf("complete body: %v", err)
			}
			io.WriteString(w, `{"id":"turn-1","status":"delivered"}`)
		case r.Method == http.MethodGet && r.URL.Path == "/pm/decisions/decision:pm_ok":
			io.WriteString(w, `{"id":"pm_ok"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	stderr := &bytes.Buffer{}
	app := New()
	app.Stderr = stderr
	app.Stdout = io.Discard
	app.Getenv = func(string) string { return "" }
	app.UserHomeDir = func() (string, error) { return t.TempDir(), nil }
	cfg := config.Resolved{BaseURL: srv.URL, AccessToken: "fixture", Timeout: 5 * time.Second, Agent: "pm"}
	body := strings.Repeat("Filler anal", 40) + " DO NOT SHIP THE RELEASE"
	text := body + "\n\n---evidence---\ndecision:pm_ok\n"
	if err := app.completeTurn(context.Background(), cfg, "turn-1", "lease-1", text, nil, 80); err != nil {
		t.Fatalf("completeTurn: %v", err)
	}
	stored := anyString(completeBody["text"])
	if !strings.Contains(stored, "[reply truncated by anx pm serve at 80 bytes;") || !strings.Contains(stored, "bytes were dropped]") {
		t.Fatalf("stored text missing marker: %q", stored)
	}
	if len(stored) > 80 {
		t.Fatalf("stored text %d exceeded 80", len(stored))
	}
	if strings.Contains(stored, "DO NOT SHIP THE RELEASE") {
		t.Fatalf("expected conclusion to be cut, got %q", stored)
	}
	refs, _ := completeBody["evidence_refs"].([]any)
	if fmt.Sprint(refs) != "[decision:pm_ok]" {
		t.Fatalf("evidence dropped: %v", refs)
	}
	if !strings.Contains(stderr.String(), "reply truncated by anx pm serve at 80 bytes") {
		t.Fatalf("missing truncation warning: %s", stderr.String())
	}
}

func TestHandleClaimedTurnRetriesCompleteThenSucceeds(t *testing.T) {
	stubTerminalSleep(t)
	completeCalls := 0
	var lastBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/complete"):
			completeCalls++
			body, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(body, &lastBody)
			if completeCalls == 1 {
				w.WriteHeader(http.StatusServiceUnavailable)
				io.WriteString(w, `{"error":{"code":"unavailable","message":"temporary"}}`)
				return
			}
			io.WriteString(w, `{"id":"turn-1","status":"delivered"}`)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	stderr := &bytes.Buffer{}
	app := New()
	app.Stderr = stderr
	app.Stdout = io.Discard
	app.Getenv = func(string) string { return "" }
	app.UserHomeDir = func() (string, error) { return t.TempDir(), nil }
	cfg := config.Resolved{BaseURL: srv.URL, AccessToken: "fixture", Timeout: 5 * time.Second, Agent: "pm"}
	settled := app.handleClaimedTurn(context.Background(), nil, cfg, t.TempDir(), "", []string{"/bin/sh", "-c", "printf '%s\\n' 'Approve the restock.'", "{prompt}"}, nil, claimedTurn(), nil)
	if !settled {
		t.Fatal("expected settled turn")
	}
	if completeCalls != 2 {
		t.Fatalf("complete calls=%d", completeCalls)
	}
	if anyString(lastBody["text"]) != "Approve the restock." {
		t.Fatalf("reply %v", lastBody["text"])
	}
	logs := stderr.String()
	if !strings.Contains(logs, "complete retry 1") || !strings.Contains(logs, "temporary") {
		t.Fatalf("expected one retry log, got %s", logs)
	}
	if strings.Count(logs, "complete retry") != 1 {
		t.Fatalf("retry log count: %s", logs)
	}
}

func TestHandleClaimedTurnPersistentComplete503FailsAndReleases(t *testing.T) {
	stubTerminalSleep(t)
	completeCalls := 0
	var failReason string
	released := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/complete"):
			completeCalls++
			w.WriteHeader(http.StatusServiceUnavailable)
			io.WriteString(w, `{"error":{"code":"unavailable","message":"core down"}}`)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/fail"):
			var payload map[string]any
			body, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(body, &payload)
			failReason = anyString(payload["reason"])
			io.WriteString(w, `{"id":"turn-1","status":"failed"}`)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/release"):
			released++
			io.WriteString(w, `{"id":"turn-1","status":"sending"}`)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	stderr := &bytes.Buffer{}
	app := New()
	app.Stderr = stderr
	app.Stdout = io.Discard
	app.Getenv = func(string) string { return "" }
	app.UserHomeDir = func() (string, error) { return t.TempDir(), nil }
	cfg := config.Resolved{BaseURL: srv.URL, AccessToken: "fixture", Timeout: 5 * time.Second, Agent: "pm"}
	settled := app.handleClaimedTurn(context.Background(), nil, cfg, t.TempDir(), "", []string{"/bin/sh", "-c", "printf '%s\\n' 'Approve the restock.'", "{prompt}"}, nil, claimedTurn(), nil)
	if !settled {
		t.Fatal("expected settled after fail")
	}
	if completeCalls != 1+len(terminalRetryDelays) {
		t.Fatalf("complete calls=%d want %d", completeCalls, 1+len(terminalRetryDelays))
	}
	want := fmt.Sprintf("complete failed after %d attempts:", completeCalls)
	if !strings.Contains(failReason, want) || !strings.Contains(failReason, "core down") {
		t.Fatalf("fail reason %q", failReason)
	}
	if released != 0 {
		t.Fatalf("successful fail should not also release, released=%d", released)
	}
	if !strings.Contains(stderr.String(), "complete retry") {
		t.Fatalf("missing retry logs: %s", stderr.String())
	}
}

func TestHandleClaimedTurnFailAlso503ReleasesLease(t *testing.T) {
	stubTerminalSleep(t)
	released := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/complete"):
			w.WriteHeader(http.StatusServiceUnavailable)
			io.WriteString(w, `{"error":{"code":"unavailable","message":"complete 503"}}`)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/fail"):
			w.WriteHeader(http.StatusServiceUnavailable)
			io.WriteString(w, `{"error":{"code":"unavailable","message":"fail 503"}}`)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/release"):
			released++
			io.WriteString(w, `{"id":"turn-1","status":"sending","claimed":false}`)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	stderr := &bytes.Buffer{}
	app := New()
	app.Stderr = stderr
	app.Stdout = io.Discard
	app.Getenv = func(string) string { return "" }
	app.UserHomeDir = func() (string, error) { return t.TempDir(), nil }
	cfg := config.Resolved{BaseURL: srv.URL, AccessToken: "fixture", Timeout: 5 * time.Second, Agent: "pm"}
	settled := app.handleClaimedTurn(context.Background(), nil, cfg, t.TempDir(), "", []string{"/bin/sh", "-c", "printf '%s\\n' 'Approve the restock.'", "{prompt}"}, nil, claimedTurn(), nil)
	if !settled {
		t.Fatal("expected move-on after undeliverable fail")
	}
	if released != 1 {
		t.Fatalf("released=%d", released)
	}
	logs := stderr.String()
	if !strings.Contains(logs, "fail request failed") || !strings.Contains(logs, "releasing lease") {
		t.Fatalf("expected fail+release logs: %s", logs)
	}
}

func TestHandleClaimedTurnLeaseMismatchAlreadyCompletedDoesNotRerun(t *testing.T) {
	stubTerminalSleep(t)
	runs := 0
	restore := stubAgentctlStreams(t, func(ctx context.Context, name string, args []string, dir string, env []string) ([]byte, []byte, error) {
		runs++
		return []byte("Approve the restock."), nil, nil
	})
	defer restore()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/complete"):
			w.WriteHeader(http.StatusConflict)
			io.WriteString(w, `{"error":{"code":"lease_mismatch","message":"the turn is already delivered and no retry is needed"}}`)
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/pm/turns/"):
			io.WriteString(w, `{"id":"turn-1","status":"delivered","response":"Approve the restock."}`)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	stderr := &bytes.Buffer{}
	app := New()
	app.Stderr = stderr
	app.Stdout = io.Discard
	app.Getenv = func(string) string { return "" }
	app.UserHomeDir = func() (string, error) { return t.TempDir(), nil }
	cfg := config.Resolved{BaseURL: srv.URL, AccessToken: "fixture", Timeout: 5 * time.Second, Agent: "pm"}
	settled := app.handleClaimedTurn(context.Background(), nil, cfg, t.TempDir(), "", []string{"/bin/sh", "-c", "true", "{prompt}"}, nil, claimedTurn(), nil)
	if !settled {
		t.Fatal("expected settled")
	}
	if runs != 1 {
		t.Fatalf("harness runs=%d", runs)
	}
	if !strings.Contains(stderr.String(), "turn already delivered; nothing to do") {
		t.Fatalf("logs %s", stderr.String())
	}
}

func TestHandleClaimedTurnLeaseMismatchPendingReclaimsAndReruns(t *testing.T) {
	stubTerminalSleep(t)
	runs := 0
	completeCalls := 0
	claimed := 0
	restore := stubAgentctlStreams(t, func(ctx context.Context, name string, args []string, dir string, env []string) ([]byte, []byte, error) {
		runs++
		return []byte("Approve the restock."), nil, nil
	})
	defer restore()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/complete"):
			completeCalls++
			if completeCalls == 1 {
				w.WriteHeader(http.StatusConflict)
				io.WriteString(w, `{"error":{"code":"lease_mismatch","message":"lease token does not match"}}`)
				return
			}
			io.WriteString(w, `{"id":"turn-1","status":"delivered"}`)
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/pm/turns/"):
			io.WriteString(w, `{"id":"turn-1","status":"sending","claimed":false}`)
		case r.Method == http.MethodPost && r.URL.Path == "/pm/turns/claim":
			claimed++
			io.WriteString(w, `{"id":"turn-1","status":"sending","claimed":true,"lease_token":"lease-2","lease_owner":"runner-1"}`)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	stderr := &bytes.Buffer{}
	app := New()
	app.Stderr = stderr
	app.Stdout = io.Discard
	app.Getenv = func(string) string { return "" }
	app.UserHomeDir = func() (string, error) { return t.TempDir(), nil }
	cfg := config.Resolved{BaseURL: srv.URL, AccessToken: "fixture", Timeout: 5 * time.Second, Agent: "pm"}
	settled := app.handleClaimedTurn(context.Background(), nil, cfg, t.TempDir(), "", []string{"/bin/echo", "{prompt}"}, nil, claimedTurn(), nil)
	if !settled {
		t.Fatal("expected settled after re-run")
	}
	if runs != 2 {
		t.Fatalf("harness runs=%d want 2", runs)
	}
	if claimed != 1 {
		t.Fatalf("claim calls=%d", claimed)
	}
	if completeCalls != 2 {
		t.Fatalf("complete calls=%d", completeCalls)
	}
	if !strings.Contains(stderr.String(), "re-claimed after lease loss; re-running harness") {
		t.Fatalf("logs %s", stderr.String())
	}
}
