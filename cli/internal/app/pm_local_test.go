package app

import (
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
	for _, needle := range []string{"What needs my decision?", "anx --agent pm work list", "anx --agent pm pm context", "pm turns propose"} {
		if !strings.Contains(prompt, needle) {
			t.Fatalf("missing %q in %s", needle, prompt)
		}
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
	refs := extractEvidenceRefs(text)
	if len(refs) != 1 || refs[0] != "card:emergency-restock" {
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
