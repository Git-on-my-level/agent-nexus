package app

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestRunMetaCommandsJSON(t *testing.T) {
	t.Parallel()

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cli := New()
	cli.Stdout = stdout
	cli.Stderr = stderr
	cli.Stdin = strings.NewReader("")
	cli.StdinIsTTY = func() bool { return true }
	cli.UserHomeDir = func() (string, error) { return t.TempDir(), nil }
	cli.ReadFile = func(path string) ([]byte, error) {
		return nil, &os.PathError{Op: "open", Path: path, Err: os.ErrNotExist}
	}

	exitCode := cli.Run([]string{"--json", "meta", "commands"})
	if exitCode != 0 {
		t.Fatalf("unexpected exit code: %d stderr=%s", exitCode, stderr.String())
	}

	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("decode stdout json: %v", err)
	}
	if payload["ok"] != true {
		t.Fatalf("expected ok=true payload=%#v", payload)
	}
	data, _ := payload["data"].(map[string]any)
	if data == nil {
		t.Fatalf("expected object data payload=%#v", payload)
	}
	if data["source"] != "embedded-generated-registry" {
		t.Fatalf("unexpected source payload=%#v", data)
	}
	if int(data["command_count"].(float64)) <= 0 {
		t.Fatalf("expected non-empty commands payload=%#v", data)
	}
}

func TestRunMetaCommandIncludesWhyAndExample(t *testing.T) {
	t.Parallel()

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cli := New()
	cli.Stdout = stdout
	cli.Stderr = stderr
	cli.Stdin = strings.NewReader("")
	cli.StdinIsTTY = func() bool { return true }
	cli.UserHomeDir = func() (string, error) { return t.TempDir(), nil }
	cli.ReadFile = func(path string) ([]byte, error) {
		return nil, &os.PathError{Op: "open", Path: path, Err: os.ErrNotExist}
	}

	exitCode := cli.Run([]string{"--json", "meta", "command", "threads.list"})
	if exitCode != 0 {
		t.Fatalf("unexpected exit code: %d stderr=%s stdout=%s", exitCode, stderr.String(), stdout.String())
	}

	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("decode stdout json: %v", err)
	}
	data, _ := payload["data"].(map[string]any)
	commandObj, _ := data["command"].(map[string]any)
	if strings.TrimSpace(commandObj["why"].(string)) == "" {
		t.Fatalf("expected non-empty why payload=%#v", payload)
	}
	examples, _ := commandObj["examples"].([]any)
	if len(examples) == 0 {
		t.Fatalf("expected at least one example payload=%#v", payload)
	}
}

func TestRunGeneratedHelpTopic(t *testing.T) {
	t.Parallel()

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cli := New()
	cli.Stdout = stdout
	cli.Stderr = stderr
	cli.Stdin = strings.NewReader("")
	cli.StdinIsTTY = func() bool { return true }
	cli.UserHomeDir = func() (string, error) { return t.TempDir(), nil }
	cli.ReadFile = func(path string) ([]byte, error) {
		return nil, &os.PathError{Op: "open", Path: path, Err: os.ErrNotExist}
	}

	exitCode := cli.Run([]string{"help", "threads"})
	if exitCode != 0 {
		t.Fatalf("unexpected exit code: %d stderr=%s stdout=%s", exitCode, stderr.String(), stdout.String())
	}
	output := stdout.String()
	if !strings.Contains(output, "Generated Help: threads") {
		t.Fatalf("expected generated help header output=%s", output)
	}
	if !strings.Contains(output, "threads create") {
		t.Fatalf("expected generated command listing output=%s", output)
	}
	if !strings.Contains(output, "threads patch") {
		t.Fatalf("expected patch subcommand in generated help output=%s", output)
	}
	if !strings.Contains(output, "threads timeline") {
		t.Fatalf("expected timeline subcommand in generated help output=%s", output)
	}
	if strings.Contains(output, "threads update") {
		t.Fatalf("unexpected legacy update subcommand in generated help output=%s", output)
	}
}

func TestRunEventsHelpMentionsLocalExplainAcrossEntryPoints(t *testing.T) {
	t.Parallel()

	run := func(args []string) string {
		t.Helper()
		stdout := &bytes.Buffer{}
		stderr := &bytes.Buffer{}
		cli := New()
		cli.Stdout = stdout
		cli.Stderr = stderr
		cli.Stdin = strings.NewReader("")
		cli.StdinIsTTY = func() bool { return true }
		cli.UserHomeDir = func() (string, error) { return t.TempDir(), nil }
		cli.ReadFile = func(path string) ([]byte, error) {
			return nil, &os.PathError{Op: "open", Path: path, Err: os.ErrNotExist}
		}

		exitCode := cli.Run(args)
		if exitCode != 0 {
			t.Fatalf("unexpected exit code: %d stderr=%s stdout=%s", exitCode, stderr.String(), stdout.String())
		}
		return stdout.String()
	}

	fromTopic := run([]string{"help", "events"})
	fromFlag := run([]string{"events", "--help"})

	for _, output := range []string{fromTopic, fromFlag} {
		if !strings.Contains(output, "Generated Help: events") {
			t.Fatalf("expected generated events help header output=%s", output)
		}
		if !strings.Contains(output, "events explain") {
			t.Fatalf("expected local events explain helper output=%s", output)
		}
		if !strings.Contains(output, "events validate") {
			t.Fatalf("expected local events validate helper output=%s", output)
		}
		if !strings.Contains(output, "events list") {
			t.Fatalf("expected local events list helper output=%s", output)
		}
		if !strings.Contains(output, "oar events explain <event-type>") {
			t.Fatalf("expected events explain usage hint output=%s", output)
		}
	}

	if fromTopic != fromFlag {
		t.Fatalf("expected same formatter output for help events and events --help\nhelp output:\n%s\nflag output:\n%s", fromTopic, fromFlag)
	}
}

func TestRunDocsHelpMentionsLocalValidateUpdate(t *testing.T) {
	t.Parallel()

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cli := New()
	cli.Stdout = stdout
	cli.Stderr = stderr
	cli.Stdin = strings.NewReader("")
	cli.StdinIsTTY = func() bool { return true }
	cli.UserHomeDir = func() (string, error) { return t.TempDir(), nil }
	cli.ReadFile = func(path string) ([]byte, error) {
		return nil, &os.PathError{Op: "open", Path: path, Err: os.ErrNotExist}
	}

	exitCode := cli.Run([]string{"help", "docs"})
	if exitCode != 0 {
		t.Fatalf("unexpected exit code: %d stderr=%s stdout=%s", exitCode, stderr.String(), stdout.String())
	}
	output := stdout.String()
	if !strings.Contains(output, "docs validate-update") {
		t.Fatalf("expected local docs validate-update helper output=%s", output)
	}
	if !strings.Contains(output, "docs content") {
		t.Fatalf("expected docs content helper output=%s", output)
	}
	if !strings.Contains(output, "--content-file <path>") {
		t.Fatalf("expected content-file hint output=%s", output)
	}
}

func TestRunSubcommandHelpToken(t *testing.T) {
	t.Parallel()

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cli := New()
	cli.Stdout = stdout
	cli.Stderr = stderr
	cli.Stdin = strings.NewReader("")
	cli.StdinIsTTY = func() bool { return true }
	cli.UserHomeDir = func() (string, error) { return t.TempDir(), nil }
	cli.ReadFile = func(path string) ([]byte, error) {
		return nil, &os.PathError{Op: "open", Path: path, Err: os.ErrNotExist}
	}

	exitCode := cli.Run([]string{"threads", "--help"})
	if exitCode != 0 {
		t.Fatalf("unexpected exit code: %d stderr=%s stdout=%s", exitCode, stderr.String(), stdout.String())
	}
	if !strings.Contains(stdout.String(), "Generated Help: threads") {
		t.Fatalf("expected generated threads help output=%s", stdout.String())
	}
}

func TestRunRootHelpMentionsOnboardingTopic(t *testing.T) {
	t.Parallel()

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cli := New()
	cli.Stdout = stdout
	cli.Stderr = stderr
	cli.Stdin = strings.NewReader("")
	cli.StdinIsTTY = func() bool { return true }
	cli.UserHomeDir = func() (string, error) { return t.TempDir(), nil }
	cli.ReadFile = func(path string) ([]byte, error) {
		return nil, &os.PathError{Op: "open", Path: path, Err: os.ErrNotExist}
	}

	exitCode := cli.Run([]string{"help"})
	if exitCode != 0 {
		t.Fatalf("unexpected exit code: %d stderr=%s stdout=%s", exitCode, stderr.String(), stdout.String())
	}
	if !strings.Contains(stdout.String(), "`oar help onboarding`") {
		t.Fatalf("expected onboarding hint output=%s", stdout.String())
	}
}

func TestRunOnboardingHelpTopic(t *testing.T) {
	t.Parallel()

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cli := New()
	cli.Stdout = stdout
	cli.Stderr = stderr
	cli.Stdin = strings.NewReader("")
	cli.StdinIsTTY = func() bool { return true }
	cli.UserHomeDir = func() (string, error) { return t.TempDir(), nil }
	cli.ReadFile = func(path string) ([]byte, error) {
		return nil, &os.PathError{Op: "open", Path: path, Err: os.ErrNotExist}
	}

	exitCode := cli.Run([]string{"help", "onboarding"})
	if exitCode != 0 {
		t.Fatalf("unexpected exit code: %d stderr=%s stdout=%s", exitCode, stderr.String(), stdout.String())
	}
	output := stdout.String()
	if !strings.Contains(output, "Onboarding: mental model") {
		t.Fatalf("expected onboarding header output=%s", output)
	}
	if !strings.Contains(output, "Work-order loop") {
		t.Fatalf("expected work-order section output=%s", output)
	}
	if !strings.Contains(output, "First 5 commands to run") {
		t.Fatalf("expected first-commands section output=%s", output)
	}
	if !strings.Contains(output, "cli/docs/runbook.md") {
		t.Fatalf("expected offline runbook link output=%s", output)
	}
	if !strings.Contains(output, "1. `oar` is a non-interactive CLI") {
		t.Fatalf("expected mental model sentence output=%s", output)
	}
	if !strings.Contains(output, "5. The fastest way to stay aligned") {
		t.Fatalf("expected fifth mental model sentence output=%s", output)
	}
}

func TestGeneratedCommandHelpIncludesBodySchemaAndEnums(t *testing.T) {
	t.Parallel()

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cli := New()
	cli.Stdout = stdout
	cli.Stderr = stderr
	cli.Stdin = strings.NewReader("")
	cli.StdinIsTTY = func() bool { return true }
	cli.UserHomeDir = func() (string, error) { return t.TempDir(), nil }
	cli.ReadFile = func(path string) ([]byte, error) {
		return nil, &os.PathError{Op: "open", Path: path, Err: os.ErrNotExist}
	}

	exitCode := cli.Run([]string{"help", "events", "create"})
	if exitCode != 0 {
		t.Fatalf("unexpected exit code: %d stderr=%s stdout=%s", exitCode, stderr.String(), stdout.String())
	}
	output := stdout.String()
	if !strings.Contains(output, "Body schema:") {
		t.Fatalf("expected body schema block output=%s", output)
	}
	if !strings.Contains(output, "event.type (string)") {
		t.Fatalf("expected event.type body field output=%s", output)
	}
	if !strings.Contains(output, "work_order_claimed") {
		t.Fatalf("expected enum discoverability for work_order_claimed output=%s", output)
	}
}
