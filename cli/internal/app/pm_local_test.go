package app

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"agent-nexus-cli/internal/config"
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
	for _, needle := range []string{"What needs my decision?", "anx --agent pm work list", "anx --agent pm pm context", "pm turns propose", "work_ref", "decision:", "identical payload, instruction and target revision", "supersedes the earlier awaiting decision", "instead of duplicating"} {
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
	refs := extractEvidenceRefs(text + " decision:pm_abc123")
	if len(refs) != 2 {
		t.Fatalf("refs %v", refs)
	}
	decisionRefs := extractEvidenceRefs("Proposed decision:dec-42 awaiting your answer")
	if len(decisionRefs) != 1 || decisionRefs[0] != "decision:dec-42" {
		t.Fatalf("decision refs %v", decisionRefs)
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
	err := harness.app.handleClaimedTurn(context.Background(), harness.cfg, t.TempDir(), "agentctl", []string{"omp", "-p"}, nil, claimedTurn())
	if err != nil {
		t.Fatal(err)
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
	if err := harness.app.handleClaimedTurn(context.Background(), harness.cfg, t.TempDir(), "agentctl", []string{"omp"}, nil, claimedTurn()); err != nil {
		t.Fatal(err)
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
	err := harness.app.handleClaimedTurn(context.Background(), harness.cfg, t.TempDir(), "agentctl", []string{"omp"}, nil, claimedTurn())
	if err == nil {
		t.Fatal("expected failure")
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
			_ = harness.app.handleClaimedTurn(context.Background(), harness.cfg, t.TempDir(), "agentctl", []string{"omp"}, nil, claimedTurn())
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

func stubAgentctl(t *testing.T, fn func(ctx context.Context, name string, args []string, dir string, env []string) ([]byte, error)) func() {
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
		"deadline":    time.Now().Add(2 * time.Minute).UTC().Format(time.RFC3339Nano),
		"text":        "What needs my decision?",
	}
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
}
