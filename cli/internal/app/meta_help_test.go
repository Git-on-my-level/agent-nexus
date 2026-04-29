package app

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"agent-nexus-cli/internal/registry"
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
	if len(examples) > 0 {
		for _, raw := range examples {
			example, _ := raw.(map[string]any)
			if example == nil || strings.TrimSpace(anyString(example["command"])) == "" {
				t.Fatalf("expected example command fields when examples are present payload=%#v", payload)
			}
		}
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
	if !strings.Contains(output, "threads list") {
		t.Fatalf("expected generated command listing output=%s", output)
	}
	if !strings.Contains(output, "threads timeline") {
		t.Fatalf("expected timeline subcommand in generated help output=%s", output)
	}
	if !strings.Contains(output, "threads inspect") {
		t.Fatalf("expected local threads inspect helper in generated help output=%s", output)
	}
	if !strings.Contains(output, "Read-only backing-thread diagnostics") {
		t.Fatalf("expected backing-thread diagnostic guidance in threads group help output=%s", output)
	}
	if !strings.Contains(output, "anx topics workspace") {
		t.Fatalf("expected topics workspace preference hint in threads group help output=%s", output)
	}
	if !strings.Contains(output, "anx threads workspace") {
		t.Fatalf("expected threads workspace diagnostic hint in threads group help output=%s", output)
	}
	if !strings.Contains(output, "threads workspace") {
		t.Fatalf("expected local threads workspace helper in generated help output=%s", output)
	}
	if strings.Contains(output, "threads create") || strings.Contains(output, "threads patch") || strings.Contains(output, "threads propose-patch") || strings.Contains(output, "threads apply") {
		t.Fatalf("unexpected legacy thread write guidance in generated help output=%s", output)
	}
	if !strings.Contains(output, "Global flags can appear before or after the command path.") {
		t.Fatalf("expected global flag placement guidance output=%s", output)
	}
	if !strings.Contains(output, "anx --json threads ...") {
		t.Fatalf("expected global --json example in generated group help output=%s", output)
	}
}

func TestRunGeneratedTopicsHelpMentionsPrimaryCoordination(t *testing.T) {
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

	exitCode := cli.Run([]string{"help", "topics"})
	if exitCode != 0 {
		t.Fatalf("unexpected exit code: %d stderr=%s stdout=%s", exitCode, stderr.String(), stdout.String())
	}
	output := stdout.String()
	if !strings.Contains(output, "Primary operator coordination:") {
		t.Fatalf("expected primary coordination supplement in topics group help output=%s", output)
	}
	if !strings.Contains(output, "topics workspace") {
		t.Fatalf("expected topics workspace in topics group help output=%s", output)
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
	if !strings.Contains(authOutput, "Auth lifecycle and registration surface") {
		t.Fatalf("expected local auth help header output=%s", authOutput)
	}
	if !strings.Contains(authOutput, "auth register") || !strings.Contains(authOutput, "auth invites") || !strings.Contains(authOutput, "auth bootstrap") {
		t.Fatalf("expected auth subcommand discoverability output=%s", authOutput)
	}
	if !strings.Contains(authOutput, "auth whoami") || !strings.Contains(authOutput, "auth default") || !strings.Contains(authOutput, "auth token-status") {
		t.Fatalf("expected local auth lifecycle guidance output=%s", authOutput)
	}

	invitesOutput := run([]string{"help", "auth", "invites"})
	if !strings.Contains(invitesOutput, "Local Help: auth invites") {
		t.Fatalf("expected local auth invites help header output=%s", invitesOutput)
	}
	if !strings.Contains(invitesOutput, "auth invites create") || !strings.Contains(invitesOutput, "auth invites revoke") {
		t.Fatalf("expected auth invites subcommand discoverability output=%s", invitesOutput)
	}
	if !strings.Contains(invitesOutput, "auth invites revoke --invite-id <id>") {
		t.Fatalf("expected auth invites revoke example to use invite-id output=%s", invitesOutput)
	}
	if strings.Contains(invitesOutput, "auth invites revoke --token") {
		t.Fatalf("auth invites revoke help references removed --token flag output=%s", invitesOutput)
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

func TestRunGeneratedHelpTopicSupportsPacketsReceiptsCreatePath(t *testing.T) {
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
	if !strings.Contains(output, "Generated Help: packets receipts create") {
		t.Fatalf("expected registry topic output=%s", output)
	}
}

func TestMetaCommandShowsRequiredInputsAndConcurrencyGuidance(t *testing.T) {
	t.Parallel()

	output := runHelpCommand(t, "meta", "command", "cards.patch")
	if !strings.Contains(output, "Inputs:") {
		t.Fatalf("expected input block output=%s", output)
	}
	if !strings.Contains(output, "- path `card_id`") {
		t.Fatalf("expected required path param output=%s", output)
	}
	if !strings.Contains(output, "body `if_updated_at` (datetime)") {
		t.Fatalf("expected concurrency body field output=%s", output)
	}
}

func TestInboxListHelpMentionsViewingAsAndCategories(t *testing.T) {
	t.Parallel()

	output := runHelpCommand(t, "help", "inbox", "list")
	if !strings.Contains(output, "viewing_as") {
		t.Fatalf("expected viewing_as scoping guidance output=%s", output)
	}
	if !strings.Contains(output, "`ask`") || !strings.Contains(output, "`escalate`") {
		t.Fatalf("expected inbox kind reference output=%s", output)
	}
}

func TestConceptsCommandAndHelpTopic(t *testing.T) {
	t.Parallel()

	commandOutput := runHelpCommand(t, "concepts")
	if !strings.Contains(commandOutput, "ANX concepts guide") {
		t.Fatalf("expected concepts guide heading output=%s", commandOutput)
	}
	if !strings.Contains(commandOutput, "threads") || !strings.Contains(commandOutput, "docs") || !strings.Contains(commandOutput, "boards") {
		t.Fatalf("expected core primitives in concepts guide output=%s", commandOutput)
	}

	helpOutput := runHelpCommand(t, "help", "concepts")
	if !strings.Contains(helpOutput, "ANX concepts guide") {
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
		if !strings.Contains(output, "anx events explain <event-type>") {
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
	for _, output := range []string{eventsFromTopic, eventsFromFlag} {
		if !strings.Contains(output, "Local Help: events list") {
			t.Fatalf("expected local events list help header output=%s", output)
		}
		if !strings.Contains(output, "backing-thread timelines") || !strings.Contains(output, "--full-id") {
			t.Fatalf("expected events list local helper details output=%s", output)
		}
	}
	for _, output := range []string{threadsFromTopic, threadsFromFlag} {
		if !strings.Contains(output, "Local Help: threads inspect") {
			t.Fatalf("expected local threads inspect help header output=%s", output)
		}
		if !strings.Contains(output, "read-only thread data") || !strings.Contains(output, "inbox list") {
			t.Fatalf("expected composed-helper details output=%s", output)
		}
	}
	for _, output := range []string{threadsWorkspaceFromTopic, threadsWorkspaceFromFlag} {
		if !strings.Contains(output, "Local Help: threads workspace") {
			t.Fatalf("expected local threads workspace help header output=%s", output)
		}
		if !strings.Contains(output, "Read-only backing-thread workspace projection: context, inbox, board membership, and related-thread signals in one command.") || !strings.Contains(output, "pending_attention") || !strings.Contains(output, "--full-id") {
			t.Fatalf("expected workspace helper details output=%s", output)
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
}

func TestJSONModeTrailingHelpShowsHelpEnvelope(t *testing.T) {
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
	exit := cli.Run([]string{"--json", "--base-url", "http://127.0.0.1:8000", "inbox", "respond", "--help"})
	if exit != 0 {
		t.Fatalf("exit=%d stderr=%s", exit, stderr.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("decode stdout: %v raw=%q", err, stdout.String())
	}
	if payload["ok"] != true {
		t.Fatalf("expected ok=true: %#v", payload)
	}
	data, _ := payload["data"].(map[string]any)
	txt := anyString(data["help_text"])
	if txt == "" || !strings.Contains(txt, "inbox.respond") {
		t.Fatalf("expected help_text with inbox respond help, got %q", txt)
	}
}

func TestRootUsageAuthNotDuplicatedInGeneratedGroups(t *testing.T) {
	t.Parallel()

	text := New().rootUsageText()
	genIdx := strings.Index(text, "Generated Command Groups:")
	if genIdx < 0 {
		t.Fatalf("expected Generated Command Groups section in root usage")
	}
	generated := text[genIdx:]
	if strings.Contains(generated, "\n  auth ") {
		t.Fatalf("auth should not repeat under Generated Command Groups; output:\n%s", text)
	}
	if !strings.Contains(text, "Core Commands:") || !strings.Contains(text, "auth          Manage agent registration") {
		t.Fatalf("expected auth under Core Commands only; output:\n%s", text)
	}
}

func TestRootUsageLeadsWithTopicsBoardsDocsDomainModel(t *testing.T) {
	t.Parallel()

	text := New().rootUsageText()
	domainIdx := strings.Index(text, "Domain model:")
	coreIdx := strings.Index(text, "Core Commands:")
	if domainIdx < 0 || coreIdx < 0 || domainIdx > coreIdx {
		t.Fatalf("expected domain model before command lists; output:\n%s", text)
	}
	genIdx := strings.Index(text, "Generated Command Groups:")
	if genIdx < 0 {
		t.Fatalf("expected Generated Command Groups section; output:\n%s", text)
	}
	generated := text[genIdx:]
	topicsIdx := strings.Index(generated, "\n  topics")
	boardsIdx := strings.Index(generated, "\n  boards")
	docsIdx := strings.Index(generated, "\n  docs")
	threadsIdx := strings.Index(generated, "\n  threads")
	if topicsIdx < 0 || boardsIdx < 0 || docsIdx < 0 || threadsIdx < 0 {
		t.Fatalf("expected topics/boards/docs/threads generated rows; output:\n%s", text)
	}
	if !(topicsIdx < boardsIdx && boardsIdx < docsIdx && docsIdx < threadsIdx) {
		t.Fatalf("expected topics, boards, docs before threads; output:\n%s", text)
	}
}

func TestHelpResolvesRuntimeAliasesThreadsGetInboxAck(t *testing.T) {
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

	threadsGet := run([]string{"help", "threads", "get"})
	if !strings.Contains(threadsGet, "Generated Help: threads get") || !strings.Contains(threadsGet, "Command ID: `threads.inspect`") {
		t.Fatalf("expected threads get alias to resolve to inspect command help, output=%s", threadsGet)
	}

	inboxRespond := run([]string{"help", "inbox", "respond"})
	if !strings.Contains(inboxRespond, "Generated Help: inbox respond") || !strings.Contains(inboxRespond, "Command ID: `inbox.respond`") {
		t.Fatalf("expected inbox respond help, output=%s", inboxRespond)
	}
	if !strings.Contains(inboxRespond, "CLI flags") || !strings.Contains(inboxRespond, "--inbox-item-id") || !strings.Contains(inboxRespond, "--from-file") {
		t.Fatalf("expected inbox respond help to document CLI flags, output=%s", inboxRespond)
	}
}

func TestRunDocsHelpMentionsCanonicalRevise(t *testing.T) {
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
	if !strings.Contains(output, "docs content") {
		t.Fatalf("expected docs content helper output=%s", output)
	}
	if !strings.Contains(output, "docs revise") {
		t.Fatalf("expected docs revise helper output=%s", output)
	}
	for _, legacy := range []string{"docs update", "docs propose-update", "docs apply", "docs validate-update"} {
		if strings.Contains(output, legacy) {
			t.Fatalf("unexpected legacy docs command %q in output=%s", legacy, output)
		}
	}
	if !strings.Contains(output, "--content-file <path>") {
		t.Fatalf("expected content-file hint output=%s", output)
	}
}

func TestDocsCreateHelpUsesFileFirstLocalHelp(t *testing.T) {
	t.Parallel()

	output := runHelpCommand(t, "help", "docs", "create")
	if !strings.Contains(output, "Local Help: docs create") {
		t.Fatalf("expected local docs create help, got output=%s", output)
	}
	if !strings.Contains(output, "--topic <topic-id>") || !strings.Contains(output, "--content-file <path>") {
		t.Fatalf("expected file-first docs create flags output=%s", output)
	}
	if strings.Contains(output, "document.body_markdown") {
		t.Fatalf("unexpected stale body_markdown help output=%s", output)
	}
}

func TestRunCommitmentsHelpIsRemoved(t *testing.T) {
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
	if exitCode == 0 {
		t.Fatalf("expected removed commitments help topic to fail, stdout=%s stderr=%s", stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "unknown help topic \"commitments\"") {
		t.Fatalf("expected unknown help topic error, stdout=%s stderr=%s", stdout.String(), stderr.String())
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
	if !strings.Contains(output, "anx provenance walk") || !strings.Contains(output, "--from <typed-ref>") {
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
	if !strings.Contains(output, "docs revise") {
		t.Fatalf("expected proposal-flow guidance output=%s", output)
	}
	if strings.Contains(output, "threads propose-patch") {
		t.Fatalf("unexpected legacy thread proposal guidance output=%s", output)
	}
	if !strings.Contains(output, "anx draft list") || !strings.Contains(output, "anx draft commit") {
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
	if !strings.Contains(stdout.String(), "`anx help onboarding`") {
		t.Fatalf("expected onboarding hint output=%s", stdout.String())
	}
	if !strings.Contains(stdout.String(), "`anx meta doc agent-guide`") {
		t.Fatalf("expected agent-guide hint output=%s", stdout.String())
	}
	if !strings.Contains(stdout.String(), "`anx meta skill cursor --write-dir ~/.cursor/skills/anx-cli-onboard`") {
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
	if !strings.Contains(output, "`anx meta doc agent-guide`") {
		t.Fatalf("expected agent-guide pointer output=%s", output)
	}
	if !strings.Contains(output, "`anx meta doc wake-routing`") {
		t.Fatalf("expected wake-routing pointer output=%s", output)
	}
	if !strings.Contains(output, "First commands to run") {
		t.Fatalf("expected first-commands section output=%s", output)
	}
	if !strings.Contains(output, "anx meta skill cursor") {
		t.Fatalf("expected skill export hint output=%s", output)
	}
	if !strings.Contains(output, "1. Point the CLI at the core API") {
		t.Fatalf("expected base-url step output=%s", output)
	}
	if !strings.Contains(output, "`anx config use <agent>`") {
		t.Fatalf("expected active profile step output=%s", output)
	}
	if !strings.Contains(output, "Next step") || !strings.Contains(output, "anx meta doc agent-guide") || !strings.Contains(output, "anx meta doc wake-routing") {
		t.Fatalf("expected follow-up guidance output=%s", output)
	}
}

func TestRunMetaSkillCursorRendersBundledSkill(t *testing.T) {
	t.Parallel()

	output := runHelpCommand(t, "meta", "skill", "cursor")
	if !strings.Contains(output, "name: anx-cli-onboard") {
		t.Fatalf("expected skill frontmatter output=%s", output)
	}
	if !strings.Contains(output, "# ANX CLI guide for agents") {
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
	if !strings.Contains(output, "name: anx-cli-onboard") {
		t.Fatalf("expected rendered skill output=%s", output)
	}
	content, err := os.ReadFile(filepath.Join(writeDir, "SKILL.md"))
	if err != nil {
		t.Fatalf("read written skill: %v", err)
	}
	if !strings.Contains(string(content), "# ANX CLI guide for agents") {
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
	if !strings.Contains(output, "receipt_added") {
		t.Fatalf("expected enum discoverability for receipt_added output=%s", output)
	}
	if !strings.Contains(output, "Communication:") {
		t.Fatalf("expected authoring group heading output=%s", output)
	}
	if !strings.Contains(output, "Communication: direct communication or important non-structured information") {
		t.Fatalf("expected communication description output=%s", output)
	}
	if !strings.Contains(output, "- `human_attention_requested`") {
		t.Fatalf("expected human_attention_requested listing output=%s", output)
	}
	if !strings.Contains(output, "- `human_attention_responded`") {
		t.Fatalf("expected human_attention_responded listing output=%s", output)
	}
	if !strings.Contains(output, "`receipt_added`: prefer `anx receipts create`") {
		t.Fatalf("expected higher-level command hint output=%s", output)
	}
	if !strings.Contains(output, "`message_posted`") {
		t.Fatalf("expected message_posted discoverability note output=%s", output)
	}
	if !strings.Contains(output, "`--dry-run`") {
		t.Fatalf("expected dry-run discoverability note output=%s", output)
	}
	if !strings.Contains(output, "anx --json events create ...") {
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
	if !strings.Contains(docsGroup, "docs revise") {
		t.Fatalf("expected docs revise in docs group help output=%s", docsGroup)
	}
	for _, legacy := range []string{"docs update", "docs propose-update", "docs apply", "docs validate-update"} {
		if strings.Contains(docsGroup, legacy) {
			t.Fatalf("unexpected legacy docs command %q in output=%s", legacy, docsGroup)
		}
	}

	docsList := runHelpCommand(t, "help", "docs", "list")
	if !strings.Contains(docsList, "Generated Help: docs list") {
		t.Fatalf("expected docs list exact generated help output=%s", docsList)
	}
	if !strings.Contains(docsList, "- Command ID: `docs.list`") {
		t.Fatalf("expected docs.list command metadata output=%s", docsList)
	}

	docsRevise := runHelpCommand(t, "help", "docs", "revise")
	if !strings.Contains(docsRevise, "Local Help: docs revise") {
		t.Fatalf("expected docs revise local help output=%s", docsRevise)
	}
	if !strings.Contains(docsRevise, "--apply") || !strings.Contains(docsRevise, "--proposal-id") {
		t.Fatalf("expected docs revise apply/proposal flags output=%s", docsRevise)
	}

	artifactInspect := runHelpCommand(t, "help", "artifacts", "inspect")
	if !strings.Contains(artifactInspect, "Local Help: artifacts inspect") {
		t.Fatalf("expected artifacts inspect exact generated help output=%s", artifactInspect)
	}
	if !strings.Contains(artifactInspect, "Fetch artifact metadata and resolved content in one command") {
		t.Fatalf("expected artifacts.inspect command metadata output=%s", artifactInspect)
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
	for _, resource := range []string{"receipts", "reviews"} {
		addPath(resource + " create")
	}
	for _, path := range runtimeRegistrySecretHelpPaths {
		addPath(path)
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
	for _, resource := range []string{"receipts", "reviews"} {
		appendPath(resource + " create")
	}
	for _, path := range runtimeRegistrySecretHelpPaths {
		appendPath(path)
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

func runHelpCommandAllowExit(t *testing.T, args []string) (string, string, int) {
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
	return stdout.String(), stderr.String(), exitCode
}
