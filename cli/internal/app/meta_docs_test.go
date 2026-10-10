package app

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"agent-nexus-cli/internal/registry"
)

func TestRunMetaDocsPrintsBundledRuntimeReference(t *testing.T) {
	t.Parallel()

	output := runHelpCommand(t, "debug", "meta", "docs")
	if !strings.Contains(output, "# ANX Runtime Help Reference") {
		t.Fatalf("expected runtime docs header output=%s", output)
	}
	if !strings.Contains(output, "## `threads`") {
		t.Fatalf("expected threads topic in runtime docs output=%s", output)
	}
	if !strings.Contains(output, "## `agent-guide`") {
		t.Fatalf("expected agent-guide topic in runtime docs output=%s", output)
	}
	if !strings.Contains(output, "## `agent-bridge`") {
		t.Fatalf("expected agent-bridge topic in runtime docs output=%s", output)
	}
	if !strings.Contains(output, "## `wake-routing`") {
		t.Fatalf("expected wake-routing topic in runtime docs output=%s", output)
	}
	if !strings.Contains(output, "## `docs revise`") {
		t.Fatalf("expected docs revise topic in runtime docs output=%s", output)
	}
	if !strings.Contains(output, "## `threads workspace`") {
		t.Fatalf("expected local helper topic in runtime docs output=%s", output)
	}
}

func TestRunMetaDocPrintsSingleTopicMarkdown(t *testing.T) {
	t.Parallel()

	output := runHelpCommand(t, "debug", "meta", "doc", "threads")
	if !strings.Contains(output, "## `threads`") {
		t.Fatalf("expected threads markdown header output=%s", output)
	}
	if !strings.Contains(output, "Generated Help: threads") {
		t.Fatalf("expected embedded threads help text output=%s", output)
	}
	if strings.Contains(output, "## `docs`") {
		t.Fatalf("expected single-topic markdown output=%s", output)
	}
}

func TestRunMetaDocPrintsLocalAuthLifecycleTopicMarkdown(t *testing.T) {
	t.Parallel()

	output := runHelpCommand(t, "debug", "meta", "doc", "auth whoami")
	if !strings.Contains(output, "## `auth whoami`") {
		t.Fatalf("expected auth whoami markdown header output=%s", output)
	}
	if !strings.Contains(output, "Local Help: auth whoami") {
		t.Fatalf("expected embedded auth whoami help text output=%s", output)
	}
	if !strings.Contains(output, "anx debug meta doc wake-routing") {
		t.Fatalf("expected wake-routing next step output=%s", output)
	}
}

func TestRunMetaDocPrintsAgentGuideMarkdown(t *testing.T) {
	t.Parallel()

	output := runHelpCommand(t, "debug", "meta", "doc", "agent-guide")
	if !strings.Contains(output, "## `agent-guide`") {
		t.Fatalf("expected agent-guide markdown header output=%s", output)
	}
	if !strings.Contains(output, "Daily loop") {
		t.Fatalf("expected daily loop section output=%s", output)
	}
	if !strings.Contains(output, "anx work start") || !strings.Contains(output, "anx await") {
		t.Fatalf("expected daily commands in agent guide output=%s", output)
	}
	if !strings.Contains(output, "Text output is compact") || !strings.Contains(output, "`--json` for scripts") {
		t.Fatalf("expected output guidance output=%s", output)
	}
	if !strings.Contains(output, "anx.card.<card-slug>") {
		t.Fatalf("expected agentctl run label guidance output=%s", output)
	}
}

func TestRunMetaDocPrintsHostIdentityAndEnvDocs(t *testing.T) {
	t.Parallel()

	profiles := runHelpCommand(t, "debug", "meta", "doc", "host", "identity")
	if !strings.Contains(profiles, "## `host identity`") || !strings.Contains(profiles, "Host identity") {
		t.Fatalf("expected host identity docs output=%s", profiles)
	}
	if !strings.Contains(profiles, "anx host enroll --plan") {
		t.Fatalf("expected precedence guidance output=%s", profiles)
	}

	env := runHelpCommand(t, "debug", "meta", "doc", "environment")
	if !strings.Contains(env, "## `env`") || !strings.Contains(env, "ANX_AS") || !strings.Contains(env, "ANX_JSON") {
		t.Fatalf("expected env docs via alias output=%s", env)
	}

	config := runHelpCommand(t, "debug", "meta", "doc", "configuration")
	if !strings.Contains(config, "## `config`") || !strings.Contains(config, "anx config show") {
		t.Fatalf("expected config docs via alias output=%s", config)
	}
}

func TestRunMetaDocsListAndSearch(t *testing.T) {
	t.Parallel()

	listOutput := runHelpCommand(t, "debug", "meta", "docs", "--list")
	if !strings.Contains(listOutput, "# ANX Runtime Help Topics") || !strings.Contains(listOutput, "`host identity`") {
		t.Fatalf("expected topic list output=%s", listOutput)
	}

	searchOutput := runHelpCommand(t, "debug", "meta", "docs", "--search", "identity")
	if !strings.Contains(searchOutput, "`host identity`") || !strings.Contains(searchOutput, "`auth whoami`") {
		t.Fatalf("expected identity search output=%s", searchOutput)
	}
	if strings.Contains(searchOutput, "## `threads`") {
		t.Fatalf("expected search index, not full docs output=%s", searchOutput)
	}
}

func TestRunMetaDocsRejectsWriteDirWithListOrSearch(t *testing.T) {
	t.Parallel()

	for _, args := range [][]string{
		{"--json", "debug", "meta", "docs", "--list", "--write-dir", t.TempDir()},
		{"--json", "debug", "meta", "docs", "--search", "identity", "--write-dir", t.TempDir()},
	} {
		stdout := runCLIForTestJSONError(t, t.TempDir(), map[string]string{}, args)
		if !strings.Contains(stdout, "use --write-dir only with full") {
			t.Fatalf("expected write-dir conflict error for %v, got %s", args, stdout)
		}
	}
}

func TestRunMetaDocPrintsAgentBridgeMarkdown(t *testing.T) {
	t.Parallel()
	output := runHelpCommand(t, "debug", "meta", "doc", "agent-bridge")
	for _, part := range []string{"one bridge per enrolled host", "anx host token", "anx bridge doctor --config ./bridge.toml", "agentctl run"} {
		if !strings.Contains(strings.ToLower(output), strings.ToLower(part)) {
			t.Fatalf("missing %q in %s", part, output)
		}
	}
}

func TestRunMetaDocPrintsWakeRoutingMarkdown(t *testing.T) {
	t.Parallel()
	output := runHelpCommand(t, "debug", "meta", "doc", "wake-routing")
	for _, part := range []string{"@<name>.<host>", "host bridge check-in", "anx runs ingest"} {
		if !strings.Contains(output, part) {
			t.Fatalf("missing %q in %s", part, output)
		}
	}
}

func TestRuntimeHelpDocMarkdownCoversCatalogTopics(t *testing.T) {
	t.Parallel()

	for _, topic := range runtimeHelpDocTopics() {
		markdown, err := RuntimeHelpDocMarkdown(topic.Path)
		if err != nil {
			t.Fatalf("render markdown for %q: %v", topic.Path, err)
		}
		if !strings.Contains(markdown, "## `"+topic.Path+"`") {
			t.Fatalf("expected markdown header for %q output=%s", topic.Path, markdown)
		}
	}
}

func TestRunsIngestExampleRequiresCallerConfiguredValues(t *testing.T) {
	t.Parallel()

	markdown, err := RuntimeHelpDocMarkdown("runs ingest")
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"ANX_CONFIG_DIR:?", "ANX_BASE_URL:?", "ANX_EVENT_FILE:?"} {
		if !strings.Contains(markdown, required) {
			t.Fatalf("runs ingest example is missing caller-provided %q:\n%s", required, markdown)
		}
	}
	for _, guessed := range []string{"anx.example.com", "/absolute/anx", "/absolute/event.json"} {
		if strings.Contains(markdown, guessed) {
			t.Fatalf("runs ingest example contains guessed value %q:\n%s", guessed, markdown)
		}
	}
}

func TestRuntimeHelpCatalogCoversGeneratedRuntimePaths(t *testing.T) {
	t.Parallel()

	meta, err := registry.LoadEmbedded()
	if err != nil {
		t.Fatalf("load embedded registry: %v", err)
	}
	for _, path := range runtimeGeneratedRegistryPaths() {
		runtimePath := strings.Join(strings.Fields(strings.TrimSpace(path)), " ")
		if runtimePath == "" {
			continue
		}
		mapped := mapRuntimePathToRegistryPath(runtimePath)
		if _, ok := commandByCLIPath(meta.Commands, mapped); !ok {
			continue
		}
		markdown, err := RuntimeHelpDocMarkdown(runtimePath)
		if err != nil {
			t.Fatalf("render runtime path %q: %v", runtimePath, err)
		}
		if !strings.Contains(markdown, "## `"+runtimePath+"`") {
			t.Fatalf("expected runtime path heading for %q output=%s", runtimePath, markdown)
		}
	}
}

func TestRuntimeHelpDocsArtifactIsCurrent(t *testing.T) {
	t.Parallel()

	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve current file path")
	}
	artifactPath := filepath.Join(filepath.Dir(currentFile), "..", "..", "docs", "generated", "runtime-help.md")
	content, err := os.ReadFile(artifactPath)
	if err != nil {
		t.Fatalf("read generated artifact: %v", err)
	}
	want, err := RuntimeHelpDocsMarkdown()
	if err != nil {
		t.Fatalf("render runtime docs markdown: %v", err)
	}
	if string(content) != want {
		t.Fatalf("runtime help artifact is stale; run `cd cli && go run ./cmd/anx-docs-gen`")
	}
}
