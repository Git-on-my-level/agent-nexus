package app

import (
	"crypto/ed25519"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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
	for _, needle := range []string{"What needs my decision?", "anx --agent pm work list", "anx --agent pm pm context", "pm turns propose", "work_ref", "decision:"} {
		if !strings.Contains(prompt, needle) {
			t.Fatalf("missing %q in %s", needle, prompt)
		}
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
