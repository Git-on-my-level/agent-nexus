package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"agent-nexus-cli/internal/registry"
)

type runtimeHelpCatalogCommand struct {
	RuntimePath  string
	RegistryPath string
	Command      registry.Command
}

type runtimeHelpCatalog struct {
	GeneratedCommands   []runtimeHelpCatalogCommand
	SupportedCommandIDs map[string]struct{}
	LocalHelpers        []localHelperTopic
}

type runtimeHelpDocTopic struct {
	Path    string
	Kind    string
	Summary string
}

var (
	runtimeHelpCatalogOnce  sync.Once
	runtimeHelpCatalogCache runtimeHelpCatalog
)

var runtimeHelpManualDocTopics = []runtimeHelpDocTopic{
	{Path: "onboarding", Kind: "manual", Summary: "Offline quick-start mental model and first command flow."},
	{Path: "concepts", Kind: "manual", Summary: "Quick guide to the core ANX primitives and when to use each."},
	{Path: "agent-guide", Kind: "manual", Summary: "Prescriptive agent guide for choosing ANX primitives, operating safely, and automating the CLI well."},
	{Path: "agent-bridge", Kind: "manual", Summary: "Install, configure, and operate the preferred per-agent `anx-agent-bridge` runtime (local adapter + check-in); workspace wake routing still lives in `anx-core`."},
	{Path: "wake-routing", Kind: "manual", Summary: "How `@handle` wake routing works, including self-registration, verification, and troubleshooting."},
	{Path: "draft", Kind: "manual", Summary: "Local draft staging, listing, commit, and discard workflow."},
	{Path: "provenance", Kind: "manual", Summary: "Deterministic provenance walk reference and examples."},
	{Path: "auth whoami", Kind: "manual", Summary: "Validate the active profile, print resolved identity metadata, and point agents at wake-registration next steps."},
	{Path: "auth list", Kind: "manual", Summary: "List local CLI profiles and the active profile."},
	{Path: "auth default", Kind: "manual", Summary: "Persist the default CLI profile used when no explicit agent is selected."},
	{Path: "config use", Kind: "manual", Summary: "Set the active CLI profile used when --agent and ANX_AGENT are omitted."},
	{Path: "config show", Kind: "manual", Summary: "Print effective CLI settings and per-field sources (tokens redacted)."},
	{Path: "config unset", Kind: "manual", Summary: "Clear the persisted default profile marker (~/.config/anx/default-profile)."},
	{Path: "auth update-username", Kind: "manual", Summary: "Rename the authenticated agent and sync the local profile."},
	{Path: "auth rotate", Kind: "manual", Summary: "Rotate the active agent key and refresh stored credentials."},
	{Path: "auth revoke", Kind: "manual", Summary: "Revoke the active agent and mark the local profile revoked. Use explicit human-lockout flags only for break-glass recovery."},
	{Path: "auth token-status", Kind: "manual", Summary: "Inspect whether the local profile still has refreshable token material."},
}

func runtimeHelpCatalogSnapshot() runtimeHelpCatalog {
	runtimeHelpCatalogOnce.Do(func() {
		runtimeHelpCatalogCache = buildRuntimeHelpCatalog()
	})
	return runtimeHelpCatalogCache
}

func buildRuntimeHelpCatalog() runtimeHelpCatalog {
	meta, err := registry.LoadEmbedded()
	if err != nil {
		return runtimeHelpCatalog{
			SupportedCommandIDs: map[string]struct{}{},
			LocalHelpers:        append([]localHelperTopic{}, localHelperTopics...),
		}
	}

	commandsByCLIPath := make(map[string]registry.Command, len(meta.Commands))
	for _, cmd := range meta.Commands {
		path := strings.TrimSpace(cmd.CLIPath)
		if path == "" {
			continue
		}
		commandsByCLIPath[path] = cmd
	}

	supported := make(map[string]struct{})
	generated := make([]runtimeHelpCatalogCommand, 0, len(runtimeGeneratedRegistryPaths()))
	seenRuntimePath := make(map[string]struct{})
	for _, runtimePath := range runtimeGeneratedRegistryPaths() {
		runtimePath = strings.Join(strings.Fields(runtimePath), " ")
		if runtimePath == "" {
			continue
		}
		if _, exists := seenRuntimePath[runtimePath]; exists {
			continue
		}
		seenRuntimePath[runtimePath] = struct{}{}

		registryPath := mapRuntimePathToRegistryPath(runtimePath)
		cmd, ok := commandsByCLIPath[registryPath]
		if !ok {
			continue
		}
		commandID := strings.TrimSpace(cmd.CommandID)
		if commandID == "" {
			continue
		}
		supported[commandID] = struct{}{}
		generated = append(generated, runtimeHelpCatalogCommand{
			RuntimePath:  runtimePath,
			RegistryPath: registryPath,
			Command:      cmd,
		})
	}

	return runtimeHelpCatalog{
		GeneratedCommands:   generated,
		SupportedCommandIDs: supported,
		LocalHelpers:        append([]localHelperTopic{}, localHelperTopics...),
	}
}

func runtimeHelpDocTopics() []runtimeHelpDocTopic {
	catalog := runtimeHelpCatalogSnapshot()
	topics := make([]runtimeHelpDocTopic, 0, len(runtimeHelpManualDocTopics)+len(runtimeGeneratedTopics)+len(catalog.GeneratedCommands)+len(catalog.LocalHelpers))
	indexByPath := map[string]int{}
	generatedByRuntimePath := make(map[string]runtimeHelpCatalogCommand, len(catalog.GeneratedCommands))
	for _, item := range catalog.GeneratedCommands {
		generatedByRuntimePath[strings.Join(strings.Fields(strings.TrimSpace(item.RuntimePath)), " ")] = item
	}
	addTopic := func(topic runtimeHelpDocTopic) {
		path := strings.Join(strings.Fields(strings.TrimSpace(topic.Path)), " ")
		if path == "" {
			return
		}
		topic.Path = path
		if idx, exists := indexByPath[path]; exists {
			topics[idx] = topic
			return
		}
		indexByPath[path] = len(topics)
		topics = append(topics, topic)
	}
	for _, topic := range runtimeHelpManualDocTopics {
		addTopic(topic)
	}
	for _, topic := range runtimeGeneratedTopics {
		addTopic(runtimeHelpDocTopic{
			Path:    strings.TrimSpace(topic.Path),
			Kind:    "group",
			Summary: strings.TrimSpace(topic.Description),
		})
	}
	meta, _ := registry.LoadEmbedded()
	for _, runtimePath := range runtimeGeneratedRegistryPaths() {
		runtimePath = strings.Join(strings.Fields(strings.TrimSpace(runtimePath)), " ")
		if runtimePath == "" {
			continue
		}
		if _, isLocalHelper := localHelperTopicByPath(runtimePath); isLocalHelper {
			continue
		}
		if item, ok := generatedByRuntimePath[runtimePath]; ok {
			summary := strings.TrimSpace(item.Command.Summary)
			if summary == "" {
				summary = strings.TrimSpace(item.Command.Why)
			}
			addTopic(runtimeHelpDocTopic{
				Path:    runtimePath,
				Kind:    "command",
				Summary: summary,
			})
			continue
		}
		if meta.CommandCount == 0 {
			continue
		}
		if len(runtimeCommandsForTopic(meta, runtimePath)) == 0 {
			continue
		}
		addTopic(runtimeHelpDocTopic{
			Path:    runtimePath,
			Kind:    "group",
			Summary: "Nested generated help topic.",
		})
	}
	for _, helper := range catalog.LocalHelpers {
		addTopic(runtimeHelpDocTopic{
			Path:    strings.TrimSpace(helper.Path),
			Kind:    "local-helper",
			Summary: strings.TrimSpace(helper.Summary),
		})
	}
	return topics
}

func runtimeHelpDocTopicByPath(path string) (runtimeHelpDocTopic, bool) {
	path = strings.Join(strings.Fields(strings.TrimSpace(path)), " ")
	for _, topic := range runtimeHelpDocTopics() {
		if strings.Join(strings.Fields(strings.TrimSpace(topic.Path)), " ") == path {
			return topic, true
		}
	}
	return runtimeHelpDocTopic{}, false
}

func RuntimeHelpDocMarkdown(topic string) (string, error) {
	docTopic, ok := runtimeHelpDocTopicByPath(topic)
	if !ok {
		return "", fmt.Errorf("unknown runtime help topic %q", strings.TrimSpace(topic))
	}
	return renderRuntimeHelpDocTopicMarkdown(docTopic)
}

func RuntimeHelpDocsMarkdown() (string, error) {
	topics := runtimeHelpDocTopics()
	var b strings.Builder
	b.WriteString("# ANX Runtime Help Reference\n\n")
	b.WriteString("This reference is bundled with the CLI. Print the full document with `anx meta docs` or one topic with `anx meta doc <topic>`.\n\n")
	b.WriteString("## Topics\n\n")
	for _, topic := range topics {
		b.WriteString(fmt.Sprintf("- `%s` (%s): %s\n", topic.Path, topic.Kind, topic.Summary))
	}
	for _, topic := range topics {
		section, err := renderRuntimeHelpDocTopicMarkdown(topic)
		if err != nil {
			return "", err
		}
		b.WriteString("\n\n")
		b.WriteString(section)
	}
	return strings.TrimSpace(b.String()) + "\n", nil
}

func WriteRuntimeHelpDocs(dir string) (string, error) {
	content, err := RuntimeHelpDocsMarkdown()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create docs dir: %w", err)
	}
	path := filepath.Join(dir, "runtime-help.md")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return "", fmt.Errorf("write runtime help docs: %w", err)
	}
	return path, nil
}

func renderRuntimeHelpDocTopicMarkdown(topic runtimeHelpDocTopic) (string, error) {
	helpText, ok := helpTopicText(topic.Path)
	if !ok {
		return "", fmt.Errorf("no help text for topic %q", topic.Path)
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("## `%s`\n\n", topic.Path))
	if topic.Summary != "" {
		b.WriteString(topic.Summary)
		b.WriteString("\n\n")
	}
	b.WriteString("```text\n")
	b.WriteString(strings.TrimSpace(helpText))
	b.WriteString("\n```")
	return b.String(), nil
}
