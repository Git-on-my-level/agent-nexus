package app

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestRunMetaDocsPrintsBundledRuntimeReference(t *testing.T) {
	t.Parallel()

	output := runHelpCommand(t, "meta", "docs")
	if !strings.Contains(output, "# OAR Runtime Help Reference") {
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
	if !strings.Contains(output, "## `docs tombstone`") {
		t.Fatalf("expected docs tombstone topic in runtime docs output=%s", output)
	}
	if !strings.Contains(output, "## `threads workspace`") {
		t.Fatalf("expected local helper topic in runtime docs output=%s", output)
	}
}

func TestRunMetaDocPrintsSingleTopicMarkdown(t *testing.T) {
	t.Parallel()

	output := runHelpCommand(t, "meta", "doc", "threads")
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

	output := runHelpCommand(t, "meta", "doc", "auth whoami")
	if !strings.Contains(output, "## `auth whoami`") {
		t.Fatalf("expected auth whoami markdown header output=%s", output)
	}
	if !strings.Contains(output, "Local Help: auth whoami") {
		t.Fatalf("expected embedded auth whoami help text output=%s", output)
	}
	if !strings.Contains(output, "oar meta doc wake-routing") {
		t.Fatalf("expected wake-routing next step output=%s", output)
	}
}

func TestRunMetaDocPrintsAgentGuideMarkdown(t *testing.T) {
	t.Parallel()

	output := runHelpCommand(t, "meta", "doc", "agent-guide")
	if !strings.Contains(output, "## `agent-guide`") {
		t.Fatalf("expected agent-guide markdown header output=%s", output)
	}
	if !strings.Contains(output, "Operating posture") {
		t.Fatalf("expected operating posture section output=%s", output)
	}
	if !strings.Contains(output, "`boards`") || !strings.Contains(output, "`docs`") {
		t.Fatalf("expected higher-level abstractions in agent guide output=%s", output)
	}
}

func TestRunMetaDocPrintsAgentBridgeMarkdown(t *testing.T) {
	t.Parallel()

	output := runHelpCommand(t, "meta", "doc", "agent-bridge")
	if !strings.Contains(output, "## `agent-bridge`") {
		t.Fatalf("expected agent-bridge markdown header output=%s", output)
	}
	if !strings.Contains(output, "oar-agent-bridge --version") {
		t.Fatalf("expected install verification guidance output=%s", output)
	}
	if !strings.Contains(output, "oar bridge init-config") || !strings.Contains(output, "oar bridge doctor --config ./agent.toml") {
		t.Fatalf("expected first-run bootstrap guidance output=%s", output)
	}
}

func TestRunMetaDocPrintsWakeRoutingMarkdown(t *testing.T) {
	t.Parallel()

	output := runHelpCommand(t, "meta", "doc", "wake-routing")
	if !strings.Contains(output, "## `wake-routing`") {
		t.Fatalf("expected wake-routing markdown header output=%s", output)
	}
	if !strings.Contains(output, "Use this when you want humans or agents to wake other agents") {
		t.Fatalf("expected wake-routing overview output=%s", output)
	}
	if !strings.Contains(output, "`agentreg.<handle>`") {
		t.Fatalf("expected registration doc guidance output=%s", output)
	}
	if !strings.Contains(output, "oar docs create --from-file wake-registration.json --json") {
		t.Fatalf("expected docs create registration example output=%s", output)
	}
	if !strings.Contains(output, "oar docs update --document-id agentreg.<handle> --from-file wake-registration-update.json --json") {
		t.Fatalf("expected docs update registration example output=%s", output)
	}
	if !strings.Contains(output, "agent-registration/v1") {
		t.Fatalf("expected registration schema version output=%s", output)
	}
	if !strings.Contains(output, "oar-agent-bridge registration apply --config <agent.toml>") {
		t.Fatalf("expected bridge registration shortcut output=%s", output)
	}
	if !strings.Contains(output, "[oar] workspace_id") || !strings.Contains(output, "ws_main") {
		t.Fatalf("expected workspace-id discovery guidance output=%s", output)
	}
	if !strings.Contains(output, "docs create` returns `conflict`") {
		t.Fatalf("expected conflict recovery guidance output=%s", output)
	}
	if !strings.Contains(output, "server actor id as `<actor-id>`") {
		t.Fatalf("expected actor-id sourcing guidance output=%s", output)
	}
	if !strings.Contains(output, "Do not hand-edit `status = \"active\"`") {
		t.Fatalf("expected bridge readiness lifecycle warning output=%s", output)
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

func TestRuntimeHelpCatalogCoversGeneratedRuntimePaths(t *testing.T) {
	t.Parallel()

	for _, path := range runtimeGeneratedRegistryPaths() {
		runtimePath := strings.Join(strings.Fields(strings.TrimSpace(path)), " ")
		if runtimePath == "" {
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
		t.Fatalf("runtime help artifact is stale; run `cd cli && go run ./cmd/oar-docs-gen`")
	}
}
