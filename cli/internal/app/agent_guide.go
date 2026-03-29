package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const agentGuideSkillName = "oar-cli-onboard"

const agentGuideSkillDescription = "Use the `oar` CLI effectively: configure base URL/auth/profile, discover the available command surface, choose the right primitive or higher-level abstraction, and operate safely in human or JSON modes. Apply when running `oar`, interpreting its help/errors, or automating OAR workflows."

type guideSection struct {
	Title string
	Lines []string
}

func agentGuideIntro() string {
	return "Use this guide when you need to operate `oar` well, not just get it running. Favor stable CLI patterns over environment-specific setup."
}

func agentGuideSections() []guideSection {
	return []guideSection{
		{
			Title: "Operating posture",
			Lines: []string{
				"- Treat `oar` as the contract-aligned interface to an OAR core API.",
				"- Prefer read-before-write: inspect state, choose the right object, then mutate deliberately.",
				"- Prefer `--json` for automation, default output for quick human inspection.",
				"- Prefer profiles and env vars over repeated flags.",
				"- Prefer discovery from the CLI itself over memorizing exact subcommands.",
			},
		},
		{
			Title: "Core model",
			Lines: []string{
				"- `events`: immutable facts, observations, and updates. Use for append-only activity, audit trails, and streams.",
				"- `threads`: durable work objects and coordination state. Use for initiatives, incidents, cases, processes, relationships, and similar work units.",
				"- `inbox`: work intake and notifications. Use to see what needs attention and ack handled items.",
				"- `draft`: staged or reviewable mutations. Use when a write should be inspected before commit.",
				"- `docs`: long-lived narrative knowledge. Use for plans, notes, decisions, summaries, and shared context.",
				"- `boards`: structured coordination views. Use to group and review work across multiple objects.",
				"- `auth` and profiles: identity plus reusable config.",
				"- `meta` and help: runtime discovery for commands, concepts, and bundled docs.",
				"",
				"Heuristic:",
				"- Use `events` for facts.",
				"- Use `threads` for ongoing work and ownership.",
				"- Use `docs` for narrative or reference material.",
				"- Use `boards` for portfolio or workflow visibility.",
				"- Use `draft` when you want a checkpoint before applying change.",
				"",
				"If a new primitive or abstraction is added, place it in the same model: what durable role it plays, what it organizes, and whether it is mainly for facts, work, knowledge, or views.",
			},
		},
		{
			Title: "Higher-level concepts",
			Lines: []string{
				"- `docs` are the long-lived narrative layer. Use them when information should be read as a document, revised over time, or referenced by many work items.",
				"- `boards` are coordination views. Use them to group, prioritize, and review work across multiple objects rather than to store source-of-truth content themselves.",
				"- `threads` often back execution; `docs` explain; `boards` organize. Keep those roles distinct.",
			},
		},
		{
			Title: "Standard workflow",
			Lines: []string{
				"1. Confirm environment and identity.",
				"2. Discover current state with list/get/context commands.",
				"3. Decide which primitive matches the task.",
				"4. Make the smallest valid mutation.",
				"5. Verify via read commands, timeline, stream, or resulting state.",
				"",
				"For interrupt-driven work, a common loop is: `inbox` -> inspect related `thread` or `doc` -> apply change directly or via `draft` -> verify -> ack inbox item.",
			},
		},
		{
			Title: "Configuration",
			Lines: []string{
				"- Set the target core with `--base-url` or `OAR_BASE_URL`.",
				"- Reuse identity/config with `--agent` or `OAR_AGENT`.",
				"- Use env vars in scripts so command bodies stay portable and short.",
				"- If available, run `oar doctor` when config or connectivity is unclear.",
				"- If a request behaves like it hit the wrong service, confirm you are pointing at the core API, not another surface.",
				"",
				"Config precedence is typically: flags -> environment -> profile -> defaults.",
			},
		},
		{
			Title: "Discovery first",
			Lines: []string{
				"Do not overfit to examples in this guide. Ask the CLI what exists now:",
				"",
				"  oar help",
				"  oar help <group>",
				"  oar help <group> <command>",
				"  oar meta docs",
				"  oar meta doc <topic>",
				"  oar meta doc wake-routing",
				"",
				"Use help output as the source of truth for exact flags, request shapes, enums, and newly added primitives.",
			},
		},
		{
			Title: "Command habits",
			Lines: []string{
				"- Use list/get/context/workspace commands to orient before editing.",
				"- Use `--full-id` when an ID will be reused in later commands.",
				"- Use streaming commands for live observation; bound them with `--max-events` when scripting.",
				"- Use `draft` or proposal/apply flows when the CLI exposes them and the change benefits from reviewability.",
				"- Prefer narrow filters over broad listings when triaging large state.",
			},
		},
		{
			Title: "Automation",
			Lines: []string{
				"- Use `--json` for machine consumption.",
				"- Parse the response envelope, not formatted text.",
				"- Treat `error.code`, `error.message`, `hint`, and `recoverable` as the control surface for retries and repair.",
				"- Keep scripts idempotent where possible: read state, compare, then write only when needed.",
			},
		},
		{
			Title: "Onboarding and recovery",
			Lines: []string{
				"When starting in a new environment:",
				"",
				"1. Set base URL.",
				"2. Check onboarding state with `oar auth bootstrap status` before first registration.",
				"3. Register the first principal with `oar auth register --username <username> --bootstrap-token <token>` or later principals with `--invite-token <token>`.",
				"4. Confirm identity.",
				"5. Run a cheap read command.",
				"6. If this agent should be tag-addressable from thread messages, read `oar meta doc agent-bridge` for the preferred runtime path or `oar meta doc wake-routing` for the generic document lifecycle.",
				"",
				"When stuck:",
				"",
				"- Re-run with `--json` to inspect structured failure details.",
				"- Check help for the exact command path you are using.",
				"- Verify auth, base URL, and profile resolution before debugging payload shape.",
			},
		},
		{
			Title: "Maintenance rule",
			Lines: []string{
				"- Keep this guide focused on durable usage patterns.",
				"- Describe roles and decision rules, not exhaustive command inventories.",
				"- Prefer `oar help` and `oar meta docs` over embedding fragile schemas.",
				"- Mention examples of primitives and abstractions, but avoid implying the list is closed.",
			},
		},
	}
}

func renderGuide(title string, headingPrefix string) string {
	var b strings.Builder
	if strings.TrimSpace(title) != "" {
		b.WriteString(strings.TrimSpace(title))
		b.WriteString("\n\n")
	}
	b.WriteString(agentGuideIntro())
	for _, section := range agentGuideSections() {
		b.WriteString("\n\n")
		if headingPrefix != "" {
			b.WriteString(headingPrefix)
			b.WriteString(" ")
		}
		b.WriteString(strings.TrimSpace(section.Title))
		b.WriteString("\n\n")
		for _, line := range section.Lines {
			line = strings.TrimRight(line, " ")
			if line == "" {
				b.WriteString("\n")
				continue
			}
			b.WriteString(line)
			b.WriteString("\n")
		}
	}
	return strings.TrimSpace(b.String()) + "\n"
}

func agentGuideText() string {
	return renderGuide("Agent guide", "")
}

func init() {
	localHelperTopics = append(localHelperTopics, localHelperTopic{
		Path:        "meta skill",
		Summary:     "Render a bundled editor-specific skill file from the canonical OAR agent guide.",
		JSONShape:   "`target`, `content`, `default_file`, `written_files`, `guide_topic`, `skill_name`",
		Composition: "Pure local helper. Renders a maintained skill document from the bundled agent guide and optionally writes it to a chosen file or directory.",
		Examples: []string{
			"oar meta skill cursor",
			"oar meta skill cursor --write-dir ~/.cursor/skills/oar-cli-onboard",
			"oar meta skill --target cursor --write-file ./SKILL.md",
		},
		Flags: []localHelperFlag{
			{Name: "<target>", Description: "Skill target to render. Currently supported: `cursor`."},
			{Name: "--target <target>", Description: "Flag form of the skill target."},
			{Name: "--write-file <path>", Description: "Write the rendered skill to this exact path."},
			{Name: "--write-dir <dir>", Description: "Write the rendered skill into this directory using its default filename."},
		},
	})
}

func renderCursorSkillMarkdown() string {
	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString("name: ")
	b.WriteString(agentGuideSkillName)
	b.WriteString("\n")
	b.WriteString("description: >-\n")
	b.WriteString("  ")
	b.WriteString(agentGuideSkillDescription)
	b.WriteString("\n")
	b.WriteString("---\n\n")
	b.WriteString(renderGuide("# OAR CLI guide for agents", "##"))
	return b.String()
}

func writeRenderedFile(content string, writeFile string, writeDir string, defaultFileName string) (string, error) {
	writeFile = strings.TrimSpace(writeFile)
	writeDir = strings.TrimSpace(writeDir)
	if writeFile != "" && writeDir != "" {
		return "", fmt.Errorf("choose either --write-file or --write-dir")
	}
	if writeFile == "" && writeDir == "" {
		return "", nil
	}
	target := writeFile
	if target == "" {
		target = filepath.Join(writeDir, defaultFileName)
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return "", fmt.Errorf("create parent dir: %w", err)
	}
	if err := os.WriteFile(target, []byte(content), 0o644); err != nil {
		return "", fmt.Errorf("write file: %w", err)
	}
	return filepath.Clean(target), nil
}
