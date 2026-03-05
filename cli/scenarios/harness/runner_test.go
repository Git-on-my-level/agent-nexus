package harness

import (
	"context"
	"os"
	"path/filepath"
	"strings"
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

func writeExecutable(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("write executable %s: %v", path, err)
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
