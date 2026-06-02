package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const agentGuideSkillName = "anx-opinionated-onboarding"

const agentGuideSkillDescription = "Use Agent Nexus as the default durable knowledge and coordination tracker: orient with `anx`, choose topics/cards/docs/boards correctly, collaborate through domain messages and asks, and keep workspace state current while doing work."

type guideSection struct {
	Title string
	Lines []string
}

func agentGuideIntro() string {
	return "Use this guide when Agent Nexus (`anx`) is available. Treat ANX as the default durable tracker for work, knowledge, asks, and collaboration."
}

func agentGuideSections() []guideSection {
	return []guideSection{
		{
			Title: "Operating posture",
			Lines: []string{
				"- Treat `anx` as the contract-aligned interface to an ANX core API and as the default durable workspace memory.",
				"- Proactively look for useful tracking opportunities: create or update Topics for work subjects, Cards for concrete tasks, Docs for durable knowledge, and domain messages for collaboration updates.",
				"- Prefer updating existing objects over creating duplicates. Search/list first, then create only when the current workspace does not already have the right Topic, Card, Doc, or Board.",
				"- Prefer read-before-write: inspect state, choose the right object, then mutate deliberately.",
				"- Prefer **default (non-JSON) output** for normal agent readbacks and orientation: concise text for direct consumption, usually fewer tokens than JSON envelopes.",
				"- Use **`--json`** or **`ANX_JSON=true`** when the consumer is code, a shell script, CI, `jq`, or anything that parses the stable JSON envelope (including rich `error.details`).",
				"- Prefer profiles and env vars over repeated flags.",
				"- Prefer discovery from the CLI itself over memorizing exact subcommands.",
			},
		},
		{
			Title: "Default tracking loop",
			Lines: []string{
				"1. Orient with `anx workspace summary`, then inspect relevant Topics, Boards, Cards, Docs, Inbox items, and notifications.",
				"2. Attach the current work to the best existing Topic/Card/Doc, or create the smallest missing durable object.",
				"3. Track concrete execution as Cards on Boards when status, owner, priority, review, or completion should remain visible.",
				"4. Preserve reusable context, decisions, investigation notes, handoff notes, and runbooks in Docs.",
				"5. Collaborate through `topics message/reply`, `docs message/reply`, and `cards message/reply` instead of raw `events create` for ordinary conversation.",
				"6. Surface human attention with `anx human ask|review|escalate` when blocked, high consequence, or low confidence.",
				"7. Close the loop by moving/resolving Cards and leaving evidence on the Topic/Card/Doc where future agents and humans will look.",
			},
		},
		{
			Title: "Core model",
			Lines: []string{
				"- `events`: immutable facts, messages, human-attention lifecycle facts, and audit updates. Use `human_attention_requested` and `human_attention_responded` for operator asks, reviews, and escalations.",
				"- `topics`: the primary durable work subjects. Use them as the main organizational root for initiatives, incidents, cases, processes, relationships, and similar work.",
				"- `cards`: the primary work items. Use `anx cards ...` for card creation, list/get, messages, assignment, workflow movement, revisions, resolution, reopen, and lifecycle.",
				"- `threads`: backing timelines and packet-routing infrastructure. Use them for read-only diagnostics, low-level inspection, and wake/tooling flows rather than normal coordination.",
				"- `inbox`: work intake and notifications. Use to see what needs attention and ack handled items.",
				"- `draft`: staged or reviewable mutations. Use when a write should be inspected before commit.",
				"- `docs`: long-lived narrative knowledge. Use for plans, notes, decisions, summaries, and shared context.",
				"- `boards`: structured coordination views. Use to group and review work across multiple cards; use `anx cards list --board <board-ref>` to read one board's cards.",
				"- `auth` and profiles: identity plus reusable config.",
				"- `meta` and help: runtime discovery for commands, concepts, and bundled docs.",
				"",
				"Heuristic:",
				"- Use `events` for facts.",
				"- Use `topics` for ongoing work, ownership, current conversation, and operator coordination.",
				"- Use `cards` for concrete tracked execution, assignment, workflow status, and delivery evidence.",
				"- Use `docs` for long-term narrative knowledge, decisions, plans, runbooks, and context that should be revised over time.",
				"- Use `boards` for portfolio or workflow visibility, not as the namespace for individual card workflow.",
				"- Use `threads` only when you need backing-timeline diagnostics or tooling-specific inspection.",
				"- Use `draft` for reviewable JSON writes, risky or broad mutations, or when acting on behalf of a human and you want an inspectable checkpoint before commit. Direct domain verbs are fine for narrow, verified changes.",
				"",
				"If a new primitive or abstraction is added, place it in the same model: what durable role it plays, what it organizes, and whether it is mainly for facts, work, knowledge, or views.",
			},
		},
		{
			Title: "Higher-level concepts",
			Lines: []string{
				"- `docs` are the long-lived narrative layer. Use them when information should be read as a document, revised over time, or referenced by many work items.",
				"- `boards` are coordination views. Use them to group, prioritize, and review work across multiple objects rather than to store source-of-truth content themselves.",
				"- `threads` back topics, cards, boards, and documents; `docs` explain; `boards` organize. Keep those roles distinct.",
				"- Before you revise a long-lived `doc` on an operator’s behalf, run `anx docs messages doc:<handle>` to read document discussion on that document’s backing thread (and use `--json` when a script or agent is consuming the output).",
			},
		},
		{
			Title: "Asks and collaboration",
			Lines: []string{
				"- Ask a human with `anx human ask` when you need a decision, approval, missing context, credential, policy call, or product judgment before continuing.",
				"- Use `anx human review` when the human should inspect a proposed change, document, plan, or result before it becomes authoritative.",
				"- Use `anx human escalate` for high-risk, time-sensitive, security, data-loss, privacy, billing, or irreversible-impact situations.",
				"- Include a recommended response and useful alternatives. The first response proposal should be the action you recommend.",
				"- Ask another agent by posting a Topic/Card/Doc message that mentions `@handle` when the target is taggable. Use Cards when the ask is a trackable task, Docs when the ask is about durable knowledge, and Topics when the ask belongs to the broader work conversation.",
				"- If your runtime can be programmatically prompted or resumed by a bridge, ask the human whether they want bridge setup after registration. If your runtime cannot be externally woken, explain that bridge setup is not useful for you and skip it.",
			},
		},
		{
			Title: "Standard workflow",
			Lines: []string{
				"1. Confirm environment and identity.",
				"2. Discover current state with list/get/context commands; in a new workspace, start with `anx workspace summary`.",
				"3. Decide which primitive matches the task.",
				"4. Make the smallest valid mutation.",
				"5. Verify via read commands, timeline, stream, or resulting state.",
				"",
				"For interrupt-driven work, a common loop is: `inbox` -> inspect the related `topic`, `card`, or `doc` -> apply change directly or via `draft` -> verify -> ack inbox item. When leaving a domain update, use `anx topics message topic:<handle> --body-file update.md`, `anx docs message doc:<handle> --body-file update.md`, or `anx cards message card:<handle> --body-file update.md`; reach for raw `events create` only for contract-level writes or unusual integrations.",
			},
		},
		{
			Title: "Configuration",
			Lines: []string{
				"- On a durable workstation, set the active profile once with `anx config use <profile>` (equivalent to `anx auth default <profile>`). Later commands can omit repeated `--base-url` / `--agent`; inspect merged settings with `anx config show` (tokens redacted).",
				"- Override per command with `--base-url` or `ANX_BASE_URL` and `--agent` or `ANX_AGENT` when needed.",
				"- Prefer `ANX_BASE_URL` and `ANX_AGENT` in scripts, CI, or environments without a persistent `~/.config/anx`.",
				"- Config precedence is command flags > environment variables > profile/default marker/autodiscovery > built-in defaults. Read `anx meta doc profiles` and `anx meta doc env` for details.",
				"- If available, run `anx doctor` when config or connectivity is unclear.",
				"- If a request behaves like it hit the wrong service, confirm you are pointing at the core API, not another surface.",
			},
		},
		{
			Title: "Discovery first",
			Lines: []string{
				"Do not overfit to examples in this guide. Ask the CLI what exists now:",
				"",
				"  anx help",
				"  anx help <group>",
				"  anx help <group> <command>",
				"  anx meta docs",
				"  anx meta doc <topic>",
				"  anx meta doc wake-routing",
				"",
				"Use help output as the source of truth for exact flags, request shapes, enums, and newly added primitives.",
			},
		},
		{
			Title: "Command habits",
			Lines: []string{
				"- Use list/get/context/workspace commands to orient before editing.",
				"- Default text and JSON list payloads lead with public typed refs and handles, for example `card:<handle>`. The CLI passes typed refs and bare handles through to core for resolution.",
				"- Prefer default text for reading and `--json` for scripts that need stable `ref` and `handle` fields. Internal `id` fields may still appear for debugging or compatibility, but they are not the normal copy/paste identity.",
				"- Use streaming commands for live observation; bound them with `--max-events` when scripting.",
				"- Use `draft` or proposal/apply flows when the CLI exposes them and the change benefits from reviewability; prefer direct domain verbs for small, already-verified writes.",
				"- Prefer narrow filters over broad listings when triaging large state.",
			},
		},
		{
			Title: "Programmatic output (`--json`)",
			Lines: []string{
				"- Use `--json` or `ANX_JSON=true` when you are parsing output in code, scripts, CI, or `jq` (not for default agent readbacks).",
				"- Parse the response envelope; do not assume the same shape for default text output.",
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
				"2. Check onboarding state with `anx auth bootstrap status` before first registration.",
				"3. Register the first principal with `anx auth register --username <username> --bootstrap-token <token>`. For later principals, obtain an invite with `anx auth invites create --kind agent`, then register with `anx auth register --username <username> --invite-token <token>`.",
				"4. Confirm identity.",
				"5. Run a cheap read command.",
				"6. Install this opinionated skill into your agent environment with `anx install skill --path <path>` when the environment supports local agent instructions.",
				"7. If this agent can be programmatically prompted or resumed and should be tag-addressable from thread messages, ask the human whether to set up the bridge. If yes, read `anx meta doc agent-bridge` for the preferred runtime path or `anx meta doc wake-routing` for the generic lifecycle.",
				"",
				"When stuck:",
				"",
				"- Re-run with `--json` when structured failure fields (`error.details`, etc.) would help.",
				"- Check help for the exact command path you are using.",
				"- Verify auth, base URL, and profile resolution before debugging payload shape.",
			},
		},
		{
			Title: "Maintenance rule",
			Lines: []string{
				"- Keep this guide focused on durable usage patterns.",
				"- Describe roles and decision rules, not exhaustive command inventories.",
				"- Prefer `anx help` and `anx meta docs` over embedding fragile schemas.",
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
		Summary:     "Render the bundled opinionated ANX agent skill.",
		JSONShape:   "`target`, `content`, `default_file`, `written_files`, `guide_topic`, `skill_name`",
		Composition: "Pure local helper. Renders the maintained opinionated ANX skill and optionally writes it to a chosen file or directory.",
		Examples: []string{
			"anx meta skill anx",
			"anx meta skill anx --write-file ./SKILL.md",
			"anx meta skill --target cursor --write-file ./SKILL.md",
		},
		Flags: []localHelperFlag{
			{Name: "<target>", Description: "Skill target to render. Use `anx`; `cursor` is accepted as a compatibility alias."},
			{Name: "--target <target>", Description: "Flag form of the skill target."},
			{Name: "--write-file <path>", Description: "Write the rendered skill to this exact path."},
			{Name: "--write-dir <dir>", Description: "Write the rendered skill into this directory using its default filename."},
		},
	}, localHelperTopic{
		Path:        "install skill",
		Summary:     "Install the bundled opinionated ANX agent skill to a specific file path.",
		JSONShape:   "`path`, `content`, `written_files`, `guide_topic`, `skill_name`",
		Composition: "Pure local helper. Writes the maintained opinionated ANX skill to the requested path.",
		Examples: []string{
			"anx install skill --path ./SKILL.md",
			"anx install skill ./SKILL.md",
		},
		Flags: []localHelperFlag{
			{Name: "<path>", Description: "Destination file path."},
			{Name: "--path <path>", Description: "Destination file path."},
			{Name: "--write-file <path>", Description: "Compatibility spelling for --path."},
			{Name: "--force", Description: "Overwrite an existing destination file."},
		},
	})
}

func renderOpinionatedANXSkillMarkdown() string {
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
	b.WriteString(renderGuide("# Opinionated ANX onboarding for agents", "##"))
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
