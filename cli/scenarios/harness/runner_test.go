package harness

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestInterpolateString(t *testing.T) {
	t.Parallel()

	captures := map[string]map[string]any{
		"run":         {"id": "run-123"},
		"coordinator": {"thread_id": "thread-1"},
	}

	resolved, err := interpolateString("thread={{coordinator.thread_id}} run={{run.id}}", captures)
	if err != nil {
		t.Fatalf("interpolateString returned error: %v", err)
	}
	if resolved != "thread=thread-1 run=run-123" {
		t.Fatalf("unexpected interpolation result: %q", resolved)
	}
}

func TestGetPathValue(t *testing.T) {
	t.Parallel()

	payload := map[string]any{
		"data": map[string]any{
			"body": map[string]any{
				"thread": map[string]any{
					"thread_id": "thread-xyz",
				},
			},
		},
	}

	value, ok := getPathValue(payload, "data.body.thread.thread_id")
	if !ok {
		t.Fatalf("expected path lookup success")
	}
	if value != "thread-xyz" {
		t.Fatalf("unexpected path value: %#v", value)
	}
}

func TestRunLLMModeWithFakeDeterministicDriver(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	oarLogPath := filepath.Join(tmp, "fake-oar.log")
	oarPath := filepath.Join(tmp, "fake-oar.sh")
	driverPath := filepath.Join(tmp, "fake-driver.py")
	scenarioPath := filepath.Join(tmp, "scenario.json")

	writeExecutable(t, oarPath, strings.ReplaceAll(`#!/usr/bin/env bash
set -euo pipefail
echo "$*" >> "__LOG_PATH__"
cat >/dev/null || true
printf '{"ok":true,"data":{"body":{"command":"%s"}}}\n' "$*"
`, "__LOG_PATH__", oarLogPath))

	writeExecutable(t, driverPath, `#!/usr/bin/env python3
import json, sys
req = json.load(sys.stdin)
turn = int(req.get("turn", 1))
if turn == 1:
    print(json.dumps({"action": "run", "name": "llm list threads", "args": ["threads", "list", "--status", "active"]}))
else:
    print(json.dumps({"action": "stop", "reason": "done"}))
`)

	scenarioJSON := `{
  "name": "llm-fake-test",
  "base_url": "http://127.0.0.1:8000",
  "agents": [
    {
      "name": "coordinator",
      "username_prefix": "coord",
      "llm": {
        "objective": "List active threads once then stop.",
        "profile_path": "",
        "max_turns": 3
      },
      "deterministic_steps": []
    }
  ],
  "assertions": []
}`
	if err := os.WriteFile(scenarioPath, []byte(scenarioJSON), 0o644); err != nil {
		t.Fatalf("write scenario file: %v", err)
	}

	report, err := Run(context.Background(), Config{
		ScenarioPath:     scenarioPath,
		OARBinary:        oarPath,
		Mode:             ModeLLM,
		LLMDriverBin:     driverPath,
		BaseURLOverride:  "http://127.0.0.1:8000",
		WorkingDirectory: tmp,
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if report.Failed {
		t.Fatalf("expected successful report, got failed report: %#v", report)
	}
	if len(report.Agents) != 1 {
		t.Fatalf("expected one agent report, got %d", len(report.Agents))
	}
	steps := report.Agents[0].Steps
	if len(steps) != 3 {
		t.Fatalf("expected 3 steps (register + llm run + stop), got %d", len(steps))
	}
	if steps[0].Name != "auth register" {
		t.Fatalf("unexpected first step: %#v", steps[0].Name)
	}
	if steps[1].Name != "llm list threads" {
		t.Fatalf("unexpected llm step name: %#v", steps[1].Name)
	}
	if got := strings.Join(steps[1].Args, " "); !strings.Contains(got, "threads list --status active") {
		t.Fatalf("unexpected llm step args: %q", got)
	}
	if !strings.Contains(strings.ToLower(steps[2].Name), "llm stop") {
		t.Fatalf("unexpected stop step name: %#v", steps[2].Name)
	}
	if !steps[2].Succeeded {
		t.Fatalf("expected stop step to succeed: %#v", steps[2])
	}

	logBytes, err := os.ReadFile(oarLogPath)
	if err != nil {
		t.Fatalf("read fake oar log: %v", err)
	}
	logLines := strings.Split(strings.TrimSpace(string(logBytes)), "\n")
	if len(logLines) != 2 {
		t.Fatalf("expected fake oar to be called twice, got %d lines: %q", len(logLines), string(logBytes))
	}
}

func TestRunLLMModeWithBuiltInOpenAICompatibleDriver(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	oarLogPath := filepath.Join(tmp, "fake-oar.log")
	oarPath := filepath.Join(tmp, "fake-oar.sh")
	scenarioPath := filepath.Join(tmp, "scenario.json")

	writeExecutable(t, oarPath, strings.ReplaceAll(`#!/usr/bin/env bash
set -euo pipefail
echo "$*" >> "__LOG_PATH__"
cat >/dev/null || true
printf '{"ok":true,"data":{"body":{"command":"%s"}}}\n' "$*"
`, "__LOG_PATH__", oarLogPath))

	var llmTurns atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("unexpected method: %s", r.Method)
		}
		if got := r.URL.Path; got != "/v4/chat/completions" {
			t.Fatalf("unexpected path: %s", got)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Fatalf("unexpected auth header: %q", got)
		}

		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode llm request: %v", err)
		}
		if got := strings.TrimSpace(anyString(req["model"])); got != "glm-4.7-flashx" {
			t.Fatalf("unexpected model: %q", got)
		}

		turn := llmTurns.Add(1)
		w.Header().Set("Content-Type", "application/json")
		if turn == 1 {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"choices": []any{
					map[string]any{
						"message": map[string]any{
							"content": `{"action":"run","name":"llm list threads","args":["threads","list","--status","active"]}`,
						},
					},
				},
			})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{
				map[string]any{
					"message": map[string]any{
						"content": `{"action":"stop","reason":"done"}`,
					},
				},
			},
		})
	}))
	defer server.Close()

	scenarioJSON := `{
  "name": "llm-openai-test",
  "base_url": "http://127.0.0.1:8000",
  "agents": [
    {
      "name": "coordinator",
      "username_prefix": "coord",
      "llm": {
        "objective": "List active threads once then stop.",
        "profile_path": "",
        "max_turns": 3
      },
      "deterministic_steps": []
    }
  ],
  "assertions": []
}`
	if err := os.WriteFile(scenarioPath, []byte(scenarioJSON), 0o644); err != nil {
		t.Fatalf("write scenario file: %v", err)
	}

	report, err := Run(context.Background(), Config{
		ScenarioPath:     scenarioPath,
		OARBinary:        oarPath,
		Mode:             ModeLLM,
		BaseURLOverride:  "http://127.0.0.1:8000",
		LLMAPIBase:       server.URL + "/v4",
		LLMAPIKey:        "test-key",
		LLMModel:         "glm-4.7-flashx",
		LLMTemperature:   0.0,
		LLMMaxTokens:     128,
		WorkingDirectory: tmp,
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if report.Failed {
		t.Fatalf("expected successful report, got failed report: %#v", report)
	}
	if len(report.Agents) != 1 {
		t.Fatalf("expected one agent report, got %d", len(report.Agents))
	}
	steps := report.Agents[0].Steps
	if len(steps) != 3 {
		t.Fatalf("expected 3 steps (register + llm run + stop), got %d", len(steps))
	}
	if steps[1].Name != "llm list threads" {
		t.Fatalf("unexpected llm step name: %#v", steps[1].Name)
	}
	if !strings.Contains(strings.ToLower(steps[2].Name), "llm stop") {
		t.Fatalf("unexpected stop step name: %#v", steps[2].Name)
	}

	logBytes, err := os.ReadFile(oarLogPath)
	if err != nil {
		t.Fatalf("read fake oar log: %v", err)
	}
	logLines := strings.Split(strings.TrimSpace(string(logBytes)), "\n")
	if len(logLines) != 2 {
		t.Fatalf("expected fake oar to be called twice, got %d lines: %q", len(logLines), string(logBytes))
	}

	if got := llmTurns.Load(); got != 2 {
		t.Fatalf("expected two llm turns, got %d", got)
	}
}

func TestRunLLMModeFeedbackActionIsCaptured(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	oarPath := filepath.Join(tmp, "fake-oar.sh")
	driverPath := filepath.Join(tmp, "fake-driver.py")
	scenarioPath := filepath.Join(tmp, "scenario.json")

	writeExecutable(t, oarPath, `#!/usr/bin/env bash
set -euo pipefail
cat >/dev/null || true
cat <<'JSON'
{"ok":true,"command":"ok"}
JSON
`)

	writeExecutable(t, driverPath, `#!/usr/bin/env python3
import json, sys
req = json.load(sys.stdin)
turn = int(req.get("turn", 1))
if turn == 1:
    print(json.dumps({"action":"feedback","reason":"CLI help was unclear","stdin":{"severity":"medium","surface":"threads"}}))
elif turn == 2:
    print(json.dumps({"action":"run","name":"list threads","args":["threads","list"]}))
else:
    print(json.dumps({"action":"stop","reason":"done"}))
`)

	scenarioJSON := `{
  "name": "llm-feedback-test",
  "base_url": "http://127.0.0.1:8000",
  "agents": [
    {
      "name": "coordinator",
      "username_prefix": "coord",
      "llm": {
        "objective": "Emit feedback then run one command.",
        "profile_path": "",
        "max_turns": 4
      },
      "deterministic_steps": []
    }
  ],
  "assertions": []
}`
	if err := os.WriteFile(scenarioPath, []byte(scenarioJSON), 0o644); err != nil {
		t.Fatalf("write scenario file: %v", err)
	}

	report, err := Run(context.Background(), Config{
		ScenarioPath:     scenarioPath,
		OARBinary:        oarPath,
		Mode:             ModeLLM,
		LLMDriverBin:     driverPath,
		BaseURLOverride:  "http://127.0.0.1:8000",
		WorkingDirectory: tmp,
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if report.Failed {
		t.Fatalf("expected successful report, got failed report: %#v", report)
	}
	if len(report.Feedback) != 1 {
		t.Fatalf("expected one feedback entry, got %d", len(report.Feedback))
	}
	if got := report.Feedback[0].Summary; got != "CLI help was unclear" {
		t.Fatalf("unexpected feedback summary: %q", got)
	}
	if got := report.Feedback[0].Source; got != "agent_feedback" {
		t.Fatalf("unexpected feedback source: %q", got)
	}
	if got := anyString(report.Feedback[0].Details["severity"]); got != "medium" {
		t.Fatalf("unexpected feedback severity: %q", got)
	}
	if len(report.Agents) != 1 {
		t.Fatalf("expected one agent report, got %d", len(report.Agents))
	}
	if got := len(report.Agents[0].Steps); got != 4 {
		t.Fatalf("expected 4 steps (register + feedback + run + stop), got %d", got)
	}
	if !strings.Contains(strings.ToLower(report.Agents[0].Steps[1].Name), "feedback") {
		t.Fatalf("expected feedback step, got %q", report.Agents[0].Steps[1].Name)
	}
}

func TestRunLLMModeMinSuccessfulRunsEnforced(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	oarPath := filepath.Join(tmp, "fake-oar.sh")
	driverPath := filepath.Join(tmp, "fake-driver.py")
	scenarioPath := filepath.Join(tmp, "scenario.json")

	writeExecutable(t, oarPath, `#!/usr/bin/env bash
set -euo pipefail
cat >/dev/null || true
cat <<'JSON'
{"ok":true,"command":"ok"}
JSON
`)

	writeExecutable(t, driverPath, `#!/usr/bin/env python3
import json
print(json.dumps({"action":"stop","reason":"no-op"}))
`)

	scenarioJSON := `{
  "name": "llm-min-success-test",
  "base_url": "http://127.0.0.1:8000",
  "agents": [
    {
      "name": "worker",
      "llm": {
        "objective": "Stop immediately",
        "profile_path": "",
        "max_turns": 2,
        "min_successful_runs": 1
      },
      "deterministic_steps": []
    }
  ],
  "assertions": []
}`
	if err := os.WriteFile(scenarioPath, []byte(scenarioJSON), 0o644); err != nil {
		t.Fatalf("write scenario file: %v", err)
	}

	report, err := Run(context.Background(), Config{
		ScenarioPath:     scenarioPath,
		OARBinary:        oarPath,
		Mode:             ModeLLM,
		LLMDriverBin:     driverPath,
		BaseURLOverride:  "http://127.0.0.1:8000",
		WorkingDirectory: tmp,
	})
	if err == nil {
		t.Fatalf("expected Run to fail for unmet min_successful_runs")
	}
	if !report.Failed {
		t.Fatalf("expected failed report")
	}
	if !strings.Contains(report.FailureReason, "min 1") {
		t.Fatalf("unexpected failure reason: %s", report.FailureReason)
	}
}

func TestRunLLMModeMinSuccessfulRunsIgnoresFinalFeedback(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	oarPath := filepath.Join(tmp, "fake-oar.sh")
	driverPath := filepath.Join(tmp, "fake-driver.py")
	scenarioPath := filepath.Join(tmp, "scenario.json")

	writeExecutable(t, oarPath, `#!/usr/bin/env bash
set -euo pipefail
cat >/dev/null || true
cat <<'JSON'
{"ok":true,"command":"ok"}
JSON
`)

	writeExecutable(t, driverPath, `#!/usr/bin/env python3
import json, sys
req = json.load(sys.stdin)
if req.get("request_kind") == "final_feedback":
    print(json.dumps({"action":"feedback","reason":"reflection"}))
else:
    print(json.dumps({"action":"stop","reason":"no-op"}))
`)

	scenarioJSON := `{
  "name": "llm-min-success-final-feedback-test",
  "base_url": "http://127.0.0.1:8000",
  "agents": [
    {
      "name": "worker",
      "llm": {
        "objective": "Stop immediately",
        "profile_path": "",
        "max_turns": 2,
        "min_successful_runs": 1,
        "collect_final_feedback": true
      },
      "deterministic_steps": []
    }
  ],
  "assertions": []
}`
	if err := os.WriteFile(scenarioPath, []byte(scenarioJSON), 0o644); err != nil {
		t.Fatalf("write scenario file: %v", err)
	}

	report, err := Run(context.Background(), Config{
		ScenarioPath:     scenarioPath,
		OARBinary:        oarPath,
		Mode:             ModeLLM,
		LLMDriverBin:     driverPath,
		BaseURLOverride:  "http://127.0.0.1:8000",
		WorkingDirectory: tmp,
	})
	if err == nil {
		t.Fatalf("expected Run to fail for unmet min_successful_runs")
	}
	if !report.Failed {
		t.Fatalf("expected failed report")
	}
	if got := len(report.FinalFeedback); got != 1 {
		t.Fatalf("expected one final feedback entry, got %d", got)
	}
}

func TestSanitizeRunArgs(t *testing.T) {
	t.Parallel()

	input := []string{"oar", "--json", "--base-url", "http://127.0.0.1:8000", "--agent", "a1", "--", "threads", "list", "--status", "active"}
	got := sanitizeRunArgs(input)
	want := []string{"threads", "list", "--status", "active"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("sanitizeRunArgs mismatch: got=%v want=%v", got, want)
	}
}

func TestSanitizeRunArgsMapsSingularRootCommands(t *testing.T) {
	t.Parallel()

	cases := []struct {
		in   []string
		want []string
	}{
		{in: []string{"oar", "thread", "list"}, want: []string{"threads", "list"}},
		{in: []string{"event", "get", "--event-id", "e1"}, want: []string{"events", "get", "--event-id", "e1"}},
		{in: []string{"artifact", "get", "--artifact-id", "a1"}, want: []string{"artifacts", "get", "--artifact-id", "a1"}},
	}
	for _, tc := range cases {
		got := sanitizeRunArgs(tc.in)
		if strings.Join(got, " ") != strings.Join(tc.want, " ") {
			t.Fatalf("sanitizeRunArgs mismatch: in=%v got=%v want=%v", tc.in, got, tc.want)
		}
	}
}

func TestSanitizeRunArgsKeepsCommandAfterMalformedGlobalFlag(t *testing.T) {
	t.Parallel()

	input := []string{"--agent", "threads", "list"}
	got := sanitizeRunArgs(input)
	want := []string{"threads", "list"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("sanitizeRunArgs mismatch: got=%v want=%v", got, want)
	}
}

func TestValidateDriverActionPrefixesRootFromName(t *testing.T) {
	t.Parallel()

	action := DriverAction{
		Action: "run",
		Name:   "threads",
		Args:   []string{"list"},
	}
	if err := validateDriverAction(&action); err != nil {
		t.Fatalf("expected validateDriverAction success, got %v", err)
	}
	want := []string{"threads", "list"}
	if strings.Join(action.Args, " ") != strings.Join(want, " ") {
		t.Fatalf("unexpected args after validation: got=%v want=%v", action.Args, want)
	}
}

func TestValidateDriverActionPrefixesCanonicalRootFromName(t *testing.T) {
	t.Parallel()

	action := DriverAction{
		Action: "run",
		Name:   "thread get",
		Args:   []string{"get", "--thread-id", "t1"},
	}
	if err := validateDriverAction(&action); err != nil {
		t.Fatalf("expected validateDriverAction success, got %v", err)
	}
	want := []string{"threads", "get", "--thread-id", "t1"}
	if strings.Join(action.Args, " ") != strings.Join(want, " ") {
		t.Fatalf("unexpected args after validation: got=%v want=%v", action.Args, want)
	}
}

func TestValidateDriverActionRejectsFollowStream(t *testing.T) {
	t.Parallel()

	action := DriverAction{
		Action: "run",
		Args:   []string{"events", "stream", "--follow"},
	}
	err := validateDriverAction(&action)
	if err == nil {
		t.Fatalf("expected validateDriverAction to reject --follow stream")
	}
	if !strings.Contains(err.Error(), "--follow") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateDriverActionRejectsUnboundedStream(t *testing.T) {
	t.Parallel()

	action := DriverAction{
		Action: "run",
		Args:   []string{"inbox", "stream"},
	}
	err := validateDriverAction(&action)
	if err == nil {
		t.Fatalf("expected validateDriverAction to reject unbounded stream")
	}
	if !strings.Contains(err.Error(), "--max-events") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateDriverActionAllowsBoundedStream(t *testing.T) {
	t.Parallel()

	action := DriverAction{
		Action: "run",
		Args:   []string{"events", "stream", "--max-events", "10"},
	}
	if err := validateDriverAction(&action); err != nil {
		t.Fatalf("expected bounded stream to validate, got %v", err)
	}
}

func TestRunLLMModeFailedRunCreatesFeedbackEntry(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	oarPath := filepath.Join(tmp, "fake-oar.sh")
	driverPath := filepath.Join(tmp, "fake-driver.py")
	scenarioPath := filepath.Join(tmp, "scenario.json")

	writeExecutable(t, oarPath, `#!/usr/bin/env bash
set -euo pipefail
if [[ " $* " == *" threads list "* ]]; then
  cat >/dev/null || true
  cat <<'JSON'
{"ok":false,"error":{"code":"unknown_subcommand","message":"bad command"}}
JSON
  exit 1
fi
cat >/dev/null || true
cat <<'JSON'
{"ok":true,"command":"ok"}
JSON
`)

	writeExecutable(t, driverPath, `#!/usr/bin/env python3
import json, sys
req = json.load(sys.stdin)
turn = int(req.get("turn", 1))
if turn == 1:
    print(json.dumps({"action":"run","name":"bad command","args":["threads","list"]}))
else:
    print(json.dumps({"action":"stop","reason":"done"}))
`)

	scenarioJSON := `{
  "name": "llm-failure-feedback-test",
  "base_url": "http://127.0.0.1:8000",
  "agents": [
    {
      "name": "coordinator",
      "llm": {
        "objective": "Try one bad command then stop",
        "profile_path": "",
        "max_turns": 2
      },
      "deterministic_steps": []
    }
  ],
  "assertions": []
}`
	if err := os.WriteFile(scenarioPath, []byte(scenarioJSON), 0o644); err != nil {
		t.Fatalf("write scenario file: %v", err)
	}

	report, err := Run(context.Background(), Config{
		ScenarioPath:     scenarioPath,
		OARBinary:        oarPath,
		Mode:             ModeLLM,
		LLMDriverBin:     driverPath,
		BaseURLOverride:  "http://127.0.0.1:8000",
		WorkingDirectory: tmp,
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if report.Failed {
		t.Fatalf("expected successful report, got failed report: %#v", report)
	}
	if len(report.Feedback) == 0 {
		t.Fatalf("expected feedback entry for failed llm run step")
	}
	if got := strings.ToLower(report.Feedback[0].Summary); !strings.Contains(got, "command failed") {
		t.Fatalf("unexpected feedback summary: %q", report.Feedback[0].Summary)
	}
	if got := report.Feedback[0].Source; got != "command_failure" {
		t.Fatalf("unexpected feedback source: %q", got)
	}
}

func TestRunLLMModeCollectsFinalFeedbackSeparately(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	oarPath := filepath.Join(tmp, "fake-oar.sh")
	driverPath := filepath.Join(tmp, "fake-driver.py")
	scenarioPath := filepath.Join(tmp, "scenario.json")

	writeExecutable(t, oarPath, `#!/usr/bin/env bash
set -euo pipefail
cat >/dev/null || true
cat <<'JSON'
{"ok":true,"command":"ok"}
JSON
`)

	writeExecutable(t, driverPath, `#!/usr/bin/env python3
import json, sys
req = json.load(sys.stdin)
kind = req.get("request_kind", "next_action")
turn = int(req.get("turn", 1))
if kind == "final_feedback":
    print(json.dumps({
        "action":"feedback",
        "reason":"Final reflection",
        "stdin":{"progress":"inspected one thread","improvement":"surface next likely command"}
    }))
elif turn == 1:
    print(json.dumps({"action":"run","name":"list threads","args":["threads","list","--status","active"]}))
else:
    print(json.dumps({"action":"stop","reason":"done"}))
`)

	scenarioJSON := `{
  "name": "llm-final-feedback-test",
  "base_url": "http://127.0.0.1:8000",
  "agents": [
    {
      "name": "coordinator",
      "username_prefix": "coord",
      "llm": {
        "objective": "Run one command then stop.",
        "profile_path": "",
        "max_turns": 3,
        "collect_final_feedback": true
      },
      "deterministic_steps": []
    }
  ],
  "assertions": []
}`
	if err := os.WriteFile(scenarioPath, []byte(scenarioJSON), 0o644); err != nil {
		t.Fatalf("write scenario file: %v", err)
	}

	report, err := Run(context.Background(), Config{
		ScenarioPath:     scenarioPath,
		OARBinary:        oarPath,
		Mode:             ModeLLM,
		LLMDriverBin:     driverPath,
		BaseURLOverride:  "http://127.0.0.1:8000",
		WorkingDirectory: tmp,
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if report.Failed {
		t.Fatalf("expected successful report, got failed report: %#v", report)
	}
	if len(report.Feedback) != 0 {
		t.Fatalf("expected no regular feedback entries, got %d", len(report.Feedback))
	}
	if len(report.FinalFeedback) != 1 {
		t.Fatalf("expected one final feedback entry, got %d", len(report.FinalFeedback))
	}
	if got := report.FinalFeedback[0].Source; got != "final_reflection" {
		t.Fatalf("unexpected final feedback source: %q", got)
	}
	if got := report.FinalFeedback[0].Summary; got != "Final reflection" {
		t.Fatalf("unexpected final feedback summary: %q", got)
	}
	if got := anyString(report.FinalFeedback[0].Details["improvement"]); got != "surface next likely command" {
		t.Fatalf("unexpected final feedback improvement: %q", got)
	}
	if got := len(report.Agents[0].Steps); got != 4 {
		t.Fatalf("expected 4 steps (register + run + stop + final feedback), got %d", got)
	}
	if got := strings.ToLower(report.Agents[0].Steps[3].Name); got != "llm final feedback" {
		t.Fatalf("unexpected final feedback step name: %q", report.Agents[0].Steps[3].Name)
	}
}

func writeExecutable(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("write executable %s: %v", path, err)
	}
}

func anyString(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	default:
		return ""
	}
}

func TestRunDeterministicExpectErrorSatisfied(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	oarPath := filepath.Join(tmp, "fake-oar.sh")
	scenarioPath := filepath.Join(tmp, "scenario.json")

	writeExecutable(t, oarPath, `#!/usr/bin/env bash
set -euo pipefail
if [[ " $* " == *" docs update "* ]]; then
  cat >/dev/null || true
  cat <<'JSON'
{"ok":false,"command":"docs update","error":{"code":"conflict","message":"document has been updated; refresh and retry","details":{"status":409}}}
JSON
  exit 1
fi
cat >/dev/null || true
cat <<'JSON'
{"ok":true,"command":"ok"}
JSON
`)

	scenarioJSON := `{
  "name": "expect-error-success",
  "base_url": "http://127.0.0.1:8000",
  "agents": [
    {
      "name": "reviewer",
      "deterministic_steps": [
        {
          "name": "stale update",
          "args": ["docs", "update", "--document-id", "doc-1"],
          "stdin": {"if_base_revision":"rev-1","content":"next"},
          "expect_error": {
            "code": "conflict",
            "status": 409,
            "message_contains": "updated"
          }
        }
      ]
    }
  ],
  "assertions": []
}`
	if err := os.WriteFile(scenarioPath, []byte(scenarioJSON), 0o644); err != nil {
		t.Fatalf("write scenario file: %v", err)
	}

	report, err := Run(context.Background(), Config{
		ScenarioPath:     scenarioPath,
		OARBinary:        oarPath,
		Mode:             ModeDeterministic,
		BaseURLOverride:  "http://127.0.0.1:8000",
		WorkingDirectory: tmp,
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if report.Failed {
		t.Fatalf("expected successful report, got failed report: %#v", report)
	}
	if len(report.Agents) != 1 {
		t.Fatalf("expected one agent, got %d", len(report.Agents))
	}
	if got := len(report.Agents[0].Steps); got != 2 {
		t.Fatalf("expected 2 steps (register + stale update), got %d", got)
	}
	if report.Agents[0].Steps[1].Succeeded {
		t.Fatalf("expected stale update command result to be unsuccessful")
	}
}

func TestRunDeterministicExpectErrorMismatchFailsRun(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	oarPath := filepath.Join(tmp, "fake-oar.sh")
	scenarioPath := filepath.Join(tmp, "scenario.json")

	writeExecutable(t, oarPath, `#!/usr/bin/env bash
set -euo pipefail
if [[ " $* " == *" docs update "* ]]; then
  cat >/dev/null || true
  cat <<'JSON'
{"ok":false,"command":"docs update","error":{"code":"invalid_request","message":"bad input","details":{"status":400}}}
JSON
  exit 1
fi
cat >/dev/null || true
cat <<'JSON'
{"ok":true,"command":"ok"}
JSON
`)

	scenarioJSON := `{
  "name": "expect-error-mismatch",
  "base_url": "http://127.0.0.1:8000",
  "agents": [
    {
      "name": "reviewer",
      "deterministic_steps": [
        {
          "name": "stale update",
          "args": ["docs", "update", "--document-id", "doc-1"],
          "stdin": {"if_base_revision":"rev-1","content":"next"},
          "expect_error": {
            "code": "conflict",
            "status": 409
          }
        }
      ]
    }
  ],
  "assertions": []
}`
	if err := os.WriteFile(scenarioPath, []byte(scenarioJSON), 0o644); err != nil {
		t.Fatalf("write scenario file: %v", err)
	}

	report, err := Run(context.Background(), Config{
		ScenarioPath:     scenarioPath,
		OARBinary:        oarPath,
		Mode:             ModeDeterministic,
		BaseURLOverride:  "http://127.0.0.1:8000",
		WorkingDirectory: tmp,
	})
	if err == nil {
		t.Fatalf("expected Run to fail for expect_error mismatch")
	}
	if !report.Failed {
		t.Fatalf("expected failed report")
	}
	if !strings.Contains(report.FailureReason, `expected error code "conflict"`) {
		t.Fatalf("unexpected failure reason: %s", report.FailureReason)
	}
}

func TestValidateScenarioRejectsInvalidReviewCompletedRefs(t *testing.T) {
	t.Parallel()

	scenario := Scenario{
		Name: "invalid-review-completed",
		Agents: []AgentSpec{
			{
				Name: "reviewer",
				DeterministicSteps: []Step{
					{
						Name: "invalid review completed",
						Args: []string{"events", "create"},
						Stdin: map[string]any{
							"event": map[string]any{
								"type": "review_completed",
								"refs": []any{"thread:t1", "document:d1", "event:e1"},
							},
						},
					},
				},
			},
		},
	}

	err := validateScenario(scenario)
	if err == nil {
		t.Fatalf("expected validation error for invalid review_completed refs")
	}
	if !strings.Contains(err.Error(), "review_completed requires at least 3 artifact:* refs") {
		t.Fatalf("unexpected validation error: %v", err)
	}
}
