package app

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"organization-autorunner-cli/internal/registry"
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
	if !strings.Contains(output, "threads propose-patch") {
		t.Fatalf("expected explicit proposal subcommand in generated help output=%s", output)
	}
	if !strings.Contains(output, "threads timeline") {
		t.Fatalf("expected timeline subcommand in generated help output=%s", output)
	}
	if !strings.Contains(output, "threads inspect") {
		t.Fatalf("expected local threads inspect helper in generated help output=%s", output)
	}
	if !strings.Contains(output, "Canonical coordination read path:") {
		t.Fatalf("expected canonical coordination guidance in threads group help output=%s", output)
	}
	if !strings.Contains(output, "oar threads workspace") {
		t.Fatalf("expected canonical threads workspace command hint in threads group help output=%s", output)
	}
	if !strings.Contains(output, "threads recommendations") {
		t.Fatalf("expected local threads recommendations helper in generated help output=%s", output)
	}
	if !strings.Contains(output, "threads workspace") {
		t.Fatalf("expected local threads workspace helper in generated help output=%s", output)
	}
	if !strings.Contains(output, "threads apply") {
		t.Fatalf("expected threads apply workflow guidance in generated help output=%s", output)
	}
	if strings.Contains(output, "threads update") {
		t.Fatalf("unexpected legacy update subcommand in generated help output=%s", output)
	}
	if !strings.Contains(output, "Global flags can appear before or after the command path.") {
		t.Fatalf("expected global flag placement guidance output=%s", output)
	}
	if !strings.Contains(output, "oar --json threads ...") {
		t.Fatalf("expected global --json example in generated group help output=%s", output)
	}
}

func TestRunGeneratedAuthHelpTopics(t *testing.T) {
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

	authOutput := run([]string{"help", "auth"})
	if !strings.Contains(authOutput, "Generated Help: auth") {
		t.Fatalf("expected generated auth help header output=%s", authOutput)
	}
	if !strings.Contains(authOutput, "auth register") || !strings.Contains(authOutput, "auth invites") || !strings.Contains(authOutput, "auth bootstrap") {
		t.Fatalf("expected auth subcommand discoverability output=%s", authOutput)
	}
	if !strings.Contains(authOutput, "auth whoami") || !strings.Contains(authOutput, "auth default") || !strings.Contains(authOutput, "auth token-status") {
		t.Fatalf("expected local auth lifecycle guidance output=%s", authOutput)
	}

	invitesOutput := run([]string{"help", "auth", "invites"})
	if !strings.Contains(invitesOutput, "Generated Help: auth invites") {
		t.Fatalf("expected generated auth invites help header output=%s", invitesOutput)
	}
	if !strings.Contains(invitesOutput, "auth invites create") || !strings.Contains(invitesOutput, "auth invites revoke") {
		t.Fatalf("expected auth invites subcommand discoverability output=%s", invitesOutput)
	}
}

func TestRunLocalAuthLifecycleHelpTopics(t *testing.T) {
	t.Parallel()

	for _, topic := range []string{"auth whoami", "auth list", "auth default", "auth update-username", "auth rotate", "auth revoke", "auth token-status"} {
		output := runHelpCommand(t, append([]string{"help"}, strings.Fields(topic)...)...)
		if !strings.Contains(output, "Local Help: "+topic) {
			t.Fatalf("expected local auth help header for %q output=%s", topic, output)
		}
	}
}

func TestRunGeneratedHelpTopicSupportsCompatibilityAliasPath(t *testing.T) {
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

	exitCode := cli.Run([]string{"help", "packets", "receipts", "create"})
	if exitCode != 0 {
		t.Fatalf("unexpected exit code: %d stderr=%s stdout=%s", exitCode, stderr.String(), stdout.String())
	}
	output := stdout.String()
	if !strings.Contains(output, "Generated Help: receipts create") {
		t.Fatalf("expected compatibility alias help to resolve to canonical topic output=%s", output)
	}
	if !strings.Contains(output, "- CLI path: `receipts create`") {
		t.Fatalf("expected canonical CLI path in help output=%s", output)
	}
}

func TestMetaCommandShowsRequiredInputsAndConcurrencyGuidance(t *testing.T) {
	t.Parallel()

	output := runHelpCommand(t, "meta", "command", "boards.cards.remove")
	if !strings.Contains(output, "Inputs:") {
		t.Fatalf("expected input block output=%s", output)
	}
	if !strings.Contains(output, "- path `board_id`") || !strings.Contains(output, "- path `thread_id`") {
		t.Fatalf("expected required path params output=%s", output)
	}
	if !strings.Contains(output, "body `if_board_updated_at` (datetime)") {
		t.Fatalf("expected required concurrency body field output=%s", output)
	}
	if !strings.Contains(output, "oar boards get --board-id <board-id>") || !strings.Contains(output, "oar boards workspace --board-id <board-id>") {
		t.Fatalf("expected concurrency token sourcing guidance output=%s", output)
	}
}

func TestInboxListHelpMentionsViewingAsAndCategories(t *testing.T) {
	t.Parallel()

	output := runHelpCommand(t, "help", "inbox", "list")
	if !strings.Contains(output, "viewing_as") {
		t.Fatalf("expected viewing_as scoping guidance output=%s", output)
	}
	if !strings.Contains(output, "`decision_needed`") || !strings.Contains(output, "`commitment_risk`") {
		t.Fatalf("expected inbox category reference output=%s", output)
	}
}

func TestConceptsCommandAndHelpTopic(t *testing.T) {
	t.Parallel()

	commandOutput := runHelpCommand(t, "concepts")
	if !strings.Contains(commandOutput, "OAR concepts guide") {
		t.Fatalf("expected concepts guide heading output=%s", commandOutput)
	}
	if !strings.Contains(commandOutput, "threads") || !strings.Contains(commandOutput, "docs") || !strings.Contains(commandOutput, "boards") {
		t.Fatalf("expected core primitives in concepts guide output=%s", commandOutput)
	}

	helpOutput := runHelpCommand(t, "help", "concepts")
	if !strings.Contains(helpOutput, "OAR concepts guide") {
		t.Fatalf("expected help concepts to reuse concepts guide output=%s", helpOutput)
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

func TestRunLocalHelperHelpTopicsResolveAcrossEntryPoints(t *testing.T) {
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

	eventsFromTopic := run([]string{"help", "events", "list"})
	eventsFromFlag := run([]string{"events", "list", "--help"})
	threadsFromTopic := run([]string{"help", "threads", "inspect"})
	threadsFromFlag := run([]string{"threads", "inspect", "--help"})
	threadsWorkspaceFromTopic := run([]string{"help", "threads", "workspace"})
	threadsWorkspaceFromFlag := run([]string{"threads", "workspace", "--help"})
	threadsRecommendationsFromTopic := run([]string{"help", "threads", "recommendations"})
	threadsRecommendationsFromFlag := run([]string{"threads", "recommendations", "--help"})

	for _, output := range []string{eventsFromTopic, eventsFromFlag} {
		if !strings.Contains(output, "Local Help: events list") {
			t.Fatalf("expected local events list help header output=%s", output)
		}
		if !strings.Contains(output, "threads timeline") || !strings.Contains(output, "--full-id") {
			t.Fatalf("expected events list local helper details output=%s", output)
		}
	}
	for _, output := range []string{threadsFromTopic, threadsFromFlag} {
		if !strings.Contains(output, "Local Help: threads inspect") {
			t.Fatalf("expected local threads inspect help header output=%s", output)
		}
		if !strings.Contains(output, "threads context") || !strings.Contains(output, "inbox list") {
			t.Fatalf("expected composed-helper details output=%s", output)
		}
	}
	for _, output := range []string{threadsWorkspaceFromTopic, threadsWorkspaceFromFlag} {
		if !strings.Contains(output, "Local Help: threads workspace") {
			t.Fatalf("expected local threads workspace help header output=%s", output)
		}
		if !strings.Contains(output, "related-thread") || !strings.Contains(output, "inbox list") || !strings.Contains(output, "--include-related-event-content") {
			t.Fatalf("expected workspace helper details output=%s", output)
		}
	}
	for _, output := range []string{threadsRecommendationsFromTopic, threadsRecommendationsFromFlag} {
		if !strings.Contains(output, "Local Help: threads recommendations") {
			t.Fatalf("expected local threads recommendations help header output=%s", output)
		}
		if !strings.Contains(output, "--full-summary") || !strings.Contains(output, "inbox list") || !strings.Contains(output, "--include-related-event-content") {
			t.Fatalf("expected recommendations helper details output=%s", output)
		}
	}
	if eventsFromTopic != eventsFromFlag {
		t.Fatalf("expected same events list help via topic and --help\nhelp output:\n%s\nflag output:\n%s", eventsFromTopic, eventsFromFlag)
	}
	if threadsFromTopic != threadsFromFlag {
		t.Fatalf("expected same threads inspect help via topic and --help\nhelp output:\n%s\nflag output:\n%s", threadsFromTopic, threadsFromFlag)
	}
	if threadsWorkspaceFromTopic != threadsWorkspaceFromFlag {
		t.Fatalf("expected same threads workspace help via topic and --help\nhelp output:\n%s\nflag output:\n%s", threadsWorkspaceFromTopic, threadsWorkspaceFromFlag)
	}
	if threadsRecommendationsFromTopic != threadsRecommendationsFromFlag {
		t.Fatalf("expected same threads recommendations help via topic and --help\nhelp output:\n%s\nflag output:\n%s", threadsRecommendationsFromTopic, threadsRecommendationsFromFlag)
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
	if !strings.Contains(output, "docs update") || !strings.Contains(output, "docs propose-update") || !strings.Contains(output, "docs apply") {
		t.Fatalf("expected docs direct/proposal/apply helpers output=%s", output)
	}
	if !strings.Contains(output, "--content-file <path>") {
		t.Fatalf("expected content-file hint output=%s", output)
	}
}

func TestRunCommitmentsHelpMentionsApplyWorkflow(t *testing.T) {
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

	exitCode := cli.Run([]string{"help", "commitments"})
	if exitCode != 0 {
		t.Fatalf("unexpected exit code: %d stderr=%s stdout=%s", exitCode, stderr.String(), stdout.String())
	}
	output := stdout.String()
	if !strings.Contains(output, "commitments patch") || !strings.Contains(output, "commitments propose-patch") || !strings.Contains(output, "commitments apply") {
		t.Fatalf("expected commitments direct/proposal/apply workflow output=%s", output)
	}
}

func TestRunProvenanceHelpTopic(t *testing.T) {
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

	exitCode := cli.Run([]string{"help", "provenance"})
	if exitCode != 0 {
		t.Fatalf("unexpected exit code: %d stderr=%s stdout=%s", exitCode, stderr.String(), stdout.String())
	}
	output := stdout.String()
	if !strings.Contains(output, "oar provenance walk") || !strings.Contains(output, "--from <typed-ref>") {
		t.Fatalf("expected provenance help text, got: %s", output)
	}
	if !strings.Contains(output, "Why does this object exist?") {
		t.Fatalf("expected provenance investigation framing, got: %s", output)
	}
	if !strings.Contains(output, "Prefer shallow depths like 1-3") {
		t.Fatalf("expected provenance heuristics, got: %s", output)
	}
}

func TestRunDraftHelpTopic(t *testing.T) {
	t.Parallel()

	output := runHelpCommand(t, "help", "draft")
	if !strings.Contains(output, "Draft staging") {
		t.Fatalf("expected draft header output=%s", output)
	}
	if !strings.Contains(output, "threads propose-patch") || !strings.Contains(output, "docs propose-update") {
		t.Fatalf("expected proposal-flow guidance output=%s", output)
	}
	if !strings.Contains(output, "oar draft list") || !strings.Contains(output, "oar draft commit") {
		t.Fatalf("expected draft workflow guidance output=%s", output)
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
	if !strings.Contains(stdout.String(), "`oar meta doc agent-guide`") {
		t.Fatalf("expected agent-guide hint output=%s", stdout.String())
	}
	if !strings.Contains(stdout.String(), "`oar meta skill cursor --write-dir ~/.cursor/skills/oar-cli-onboard`") {
		t.Fatalf("expected skill export hint output=%s", stdout.String())
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
	if !strings.Contains(output, "Onboarding: first steps") {
		t.Fatalf("expected onboarding header output=%s", output)
	}
	if !strings.Contains(output, "`oar meta doc agent-guide`") {
		t.Fatalf("expected agent-guide pointer output=%s", output)
	}
	if !strings.Contains(output, "`oar meta doc wake-routing`") {
		t.Fatalf("expected wake-routing pointer output=%s", output)
	}
	if !strings.Contains(output, "First commands to run") {
		t.Fatalf("expected first-commands section output=%s", output)
	}
	if !strings.Contains(output, "oar meta skill cursor") {
		t.Fatalf("expected skill export hint output=%s", output)
	}
	if !strings.Contains(output, "1. Point the CLI at the core API") {
		t.Fatalf("expected base-url step output=%s", output)
	}
	if !strings.Contains(output, "Next step") || !strings.Contains(output, "oar meta doc agent-guide") || !strings.Contains(output, "oar meta doc wake-routing") {
		t.Fatalf("expected follow-up guidance output=%s", output)
	}
}

func TestRunMetaSkillCursorRendersBundledSkill(t *testing.T) {
	t.Parallel()

	output := runHelpCommand(t, "meta", "skill", "cursor")
	if !strings.Contains(output, "name: oar-cli-onboard") {
		t.Fatalf("expected skill frontmatter output=%s", output)
	}
	if !strings.Contains(output, "# OAR CLI guide for agents") {
		t.Fatalf("expected skill title output=%s", output)
	}
	if !strings.Contains(output, "## Core model") {
		t.Fatalf("expected core model section output=%s", output)
	}
	if !strings.Contains(output, "`boards`") || !strings.Contains(output, "`docs`") {
		t.Fatalf("expected higher-level abstractions in skill output=%s", output)
	}
}

func TestRunMetaSkillCursorWritesSkillFile(t *testing.T) {
	t.Parallel()

	writeDir := t.TempDir()
	output := runHelpCommand(t, "meta", "skill", "cursor", "--write-dir", writeDir)
	if !strings.Contains(output, "name: oar-cli-onboard") {
		t.Fatalf("expected rendered skill output=%s", output)
	}
	content, err := os.ReadFile(filepath.Join(writeDir, "SKILL.md"))
	if err != nil {
		t.Fatalf("read written skill: %v", err)
	}
	if !strings.Contains(string(content), "# OAR CLI guide for agents") {
		t.Fatalf("expected written skill title content=%s", string(content))
	}
	if !strings.Contains(string(content), "## Maintenance rule") {
		t.Fatalf("expected written maintenance section content=%s", string(content))
	}
	if !strings.Contains(output, "auth bootstrap status") {
		t.Fatalf("expected bootstrap status onboarding guidance output=%s", output)
	}
	if !strings.Contains(output, "auth register --username <username> --bootstrap-token <token>") {
		t.Fatalf("expected token-gated onboarding guidance output=%s", output)
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
	if !strings.Contains(output, "Inputs:") {
		t.Fatalf("expected input block output=%s", output)
	}
	if !strings.Contains(output, "body `event.type` (string)") {
		t.Fatalf("expected event.type body field output=%s", output)
	}
	if !strings.Contains(output, "work_order_claimed") {
		t.Fatalf("expected enum discoverability for work_order_claimed output=%s", output)
	}
	if !strings.Contains(output, "Communication:") {
		t.Fatalf("expected authoring group heading output=%s", output)
	}
	if !strings.Contains(output, "Communication: direct communication or important non-structured information") {
		t.Fatalf("expected communication description output=%s", output)
	}
	if !strings.Contains(output, "- `decision_needed`") {
		t.Fatalf("expected decision_needed listing output=%s", output)
	}
	if !strings.Contains(output, "- `intervention_needed`") {
		t.Fatalf("expected intervention_needed listing output=%s", output)
	}
	if !strings.Contains(output, "`work_order_created`: prefer `oar work-orders create`") {
		t.Fatalf("expected higher-level command hint output=%s", output)
	}
	if !strings.Contains(output, "`actor_statement`") {
		t.Fatalf("expected actor_statement discoverability note output=%s", output)
	}
	if !strings.Contains(output, "`--dry-run`") {
		t.Fatalf("expected dry-run discoverability note output=%s", output)
	}
	if !strings.Contains(output, "oar --json events create ...") {
		t.Fatalf("expected global --json example in generated command help output=%s", output)
	}
}

func TestRuntimeSupportedCommandIDsMatchGeneratedHelpSpecSurface(t *testing.T) {
	t.Parallel()

	meta, err := registry.LoadEmbedded()
	if err != nil {
		t.Fatalf("load embedded registry: %v", err)
	}

	got := sortedCommandIDs(runtimeSupportedCommandIDs())
	want := sortedCommandIDs(expectedRuntimeSupportedCommandIDs(meta))
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("unexpected runtime-supported command ids\n got: %v\nwant: %v", got, want)
	}
}

func TestGeneratedHelpResolvesAllRegistryBackedRuntimePaths(t *testing.T) {
	t.Parallel()

	meta, err := registry.LoadEmbedded()
	if err != nil {
		t.Fatalf("load embedded registry: %v", err)
	}

	commandsByCLIPath := make(map[string]registry.Command, len(meta.Commands))
	for _, cmd := range meta.Commands {
		path := strings.TrimSpace(cmd.CLIPath)
		if path == "" {
			continue
		}
		commandsByCLIPath[path] = cmd
	}

	resolved := 0
	for _, runtimePath := range expectedGeneratedHelpRuntimePaths() {
		mapped := mapRuntimePathToRegistryPath(runtimePath)
		cmd, ok := commandsByCLIPath[mapped]
		if !ok {
			continue
		}
		resolved++

		output := runHelpCommand(t, append([]string{"help"}, strings.Fields(runtimePath)...)...)
		header := "Generated Help: " + runtimePath
		if _, ok := localHelperTopicByPath(runtimePath); ok {
			header = "Local Help: " + runtimePath
		}
		if !strings.Contains(output, header) {
			t.Fatalf("expected help header %q for command %q mapped to %q output=%s", header, cmd.CommandID, mapped, output)
		}
	}
	if resolved == 0 {
		t.Fatal("expected at least one registry-backed runtime path")
	}
}

func TestRunGeneratedHelpResolvesDerivedDocsAndArtifactCommands(t *testing.T) {
	t.Parallel()

	docsGroup := runHelpCommand(t, "help", "docs")
	if !strings.Contains(docsGroup, "docs list") {
		t.Fatalf("expected docs list in docs group help output=%s", docsGroup)
	}
	if !strings.Contains(docsGroup, "docs tombstone") {
		t.Fatalf("expected docs tombstone in docs group help output=%s", docsGroup)
	}

	docsList := runHelpCommand(t, "help", "docs", "list")
	if !strings.Contains(docsList, "Generated Help: docs list") {
		t.Fatalf("expected docs list exact generated help output=%s", docsList)
	}
	if !strings.Contains(docsList, "- Command ID: `docs.list`") {
		t.Fatalf("expected docs.list command metadata output=%s", docsList)
	}

	docsTombstone := runHelpCommand(t, "help", "docs", "tombstone")
	if !strings.Contains(docsTombstone, "Generated Help: docs tombstone") {
		t.Fatalf("expected docs tombstone exact generated help output=%s", docsTombstone)
	}
	if !strings.Contains(docsTombstone, "- Command ID: `docs.tombstone`") {
		t.Fatalf("expected docs.tombstone command metadata output=%s", docsTombstone)
	}

	artifactTombstone := runHelpCommand(t, "help", "artifacts", "tombstone")
	if !strings.Contains(artifactTombstone, "Generated Help: artifacts tombstone") {
		t.Fatalf("expected artifacts tombstone exact generated help output=%s", artifactTombstone)
	}
	if !strings.Contains(artifactTombstone, "- Command ID: `artifacts.tombstone`") {
		t.Fatalf("expected artifacts.tombstone command metadata output=%s", artifactTombstone)
	}
}

func runHelpCommand(t *testing.T, args ...string) string {
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

func expectedRuntimeSupportedCommandIDs(meta registry.MetaRegistry) map[string]struct{} {
	commandsByCLIPath := make(map[string]registry.Command, len(meta.Commands))
	for _, cmd := range meta.Commands {
		path := strings.TrimSpace(cmd.CLIPath)
		if path == "" {
			continue
		}
		commandsByCLIPath[path] = cmd
	}

	expected := make(map[string]struct{})
	addPath := func(path string) {
		mapped := mapRuntimePathToRegistryPath(path)
		cmd, ok := commandsByCLIPath[mapped]
		if !ok {
			return
		}
		commandID := strings.TrimSpace(cmd.CommandID)
		if commandID == "" {
			return
		}
		expected[commandID] = struct{}{}
	}

	for _, spec := range runtimeGeneratedHelpSpecs() {
		command := strings.TrimSpace(spec.command)
		if command == "" {
			continue
		}
		for _, subcommand := range spec.valid {
			path := strings.Join(strings.Fields(command+" "+strings.TrimSpace(subcommand)), " ")
			if path == "" {
				continue
			}
			addPath(path)
		}
	}
	for _, resource := range []string{"work-orders", "receipts", "reviews"} {
		addPath(resource + " create")
	}

	return expected
}

func expectedGeneratedHelpRuntimePaths() []string {
	paths := make([]string, 0, 40)
	appendPath := func(path string) {
		path = strings.Join(strings.Fields(path), " ")
		if path == "" {
			return
		}
		paths = append(paths, path)
	}

	for _, spec := range runtimeGeneratedHelpSpecs() {
		command := strings.TrimSpace(spec.command)
		if command == "" {
			continue
		}
		for _, subcommand := range spec.valid {
			appendPath(command + " " + strings.TrimSpace(subcommand))
		}
	}
	for _, resource := range []string{"work-orders", "receipts", "reviews"} {
		appendPath(resource + " create")
	}

	return paths
}

func sortedCommandIDs(set map[string]struct{}) []string {
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
