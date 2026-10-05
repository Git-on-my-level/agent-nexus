package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"agent-nexus-cli/skills"
)

const agentGuideSkillName = "anx-participant"

const agentGuideSkillDescription = "Participate in Agent Nexus work with source authority, scoped session identity, meaningful updates, and evidence-backed completion."

const agentGuideSkillVersion = "anx.participant.v12"

type guideSection struct {
	Title string
	Lines []string
}

func agentGuideIntro() string {
	return "Every ANX reader is a CEO by default: lead with outcomes, decisions and evidence. Use Agent Nexus (`anx`) to keep meaningful work visible to the workspace."
}

func agentGuideSections() []guideSection {
	return []guideSection{
		{Title: "Setup and identity", Lines: []string{
			"- Enroll a host once per workspace and machine with `anx host enroll`. A human or explicitly granted auth-admin agent approves the enrollment. Other agents on that host use the same host enrollment.",
			"- Granting an agent on host X trusts every process that can read X's shared host key and request that agent name. Protect that key as an administration credential. Human invitations and human identity creation remain human-only.",
			"- For fleet hosts, use an explicitly granted auth-admin agent: `anx --json host tokens create --label host-b --expires-in 1h | jq -er '.result.token' | ssh host-b 'anx host enroll --name host-b --token-stdin'`. Set the workspace base URL on both hosts, disable shell tracing, and never log the secret. Only a human can use `anx auth admins grant|revoke <principal>`; revocation applies on the next request. Agents cannot revoke their own host.",
			"- Run `anx config workspaces` when unsure which workspace applies. Set a user-global default with `anx config use <alias>` or map a directory with `anx config map \"~/work/project/**\" <alias>`. Use `--workspace <alias>` for an explicit invocation; never hardcode `--base-url` in agent prompts. Preferences live outside git repositories.",
			"- Set `ANX_AS=<name>` or pass `--as <name>` to select an explicit stable principal. Optional `agentctl identity` evidence can suggest a harness name; check the resolved handle and host in `anx orient`.",
			"- `anx host discover` inspects optional local runtime evidence without uploading it. An installed harness is not proof of a live conversation, history access, or resume support. Generic registration does not require agentctl.",
		}},
		{Title: "Participation and source authority", Lines: []string{
			"- Keep the stable agent principal, provider/host-scoped native session, and each run attempt distinct. Register an already authenticated session with `anx sessions register --from-file session.json`; inspect exact fields with `anx help sessions register`.",
			"- Use `anx work participants register card:<slug> --from-file participation.json` for nonlocking participation. Preserve sequence and identical payload on retry; increment sequence only for a new observation. This never assigns, moves, locks, or completes the task.",
			"- Associate an existing project only from clear configured repository/source/task evidence. Ask when the project is new or ambiguous; skip trivial activity. Reading alone is not a reason to create a task or report progress.",
			"- Native Nexus tasks and externally authoritative tasks can coexist. Keep source assignees and workflow fields intact; use an authorized source workflow for source-owned changes. A mandatory project owner is not required.",
			"- Share only task-scoped facts and references. Do not upload raw transcripts, secrets, unrelated session history, or local paths as globally reachable links. A session reference grants no history access.",
		}},
		{Title: "Executive workspace", Lines: []string{
			"- A card represents a human-level initiative or outcome that may span many executor tasks and outlive them. Keep issue, PR and run detail in its source; link that detail as evidence. Before `anx work create`, run `anx work list --project-ref <ref>` and update a matching card; never mirror a tracker 1:1.",
			"- For source ingestion through an external adapter, route linked source evidence to existing initiatives. Review the adapter preview before publishing workspace mapping rules with `anx docs revise <mapping-doc> --apply --body-file mapping.json`. Keep unmatched items visible for deliberate triage. Linked evidence complements the initiative plan; only deliberate milestones become plan steps.",
			"- Keep one plan per initiative card. Add steps rather than writing progress prose; link steps to real refs. Never pick a view: the graph determines it. Read `anx plan show card:<slug>` for computed progress and health. Keep about 15 or fewer open cards per workspace and very few open asks; consolidate when approaching that budget.",
			"- For a human-facing dashboard, start with `anx report templates` and `anx report init --template <name> [--topic <topic-ref>] [--card <card-ref>]`; add narrative without pasting live numbers. Run `anx report preview <file>` and inspect its panel summary and PNG before sharing. Publish with `anx report publish <file> --topic <topic-ref> [--title <title>] [--doc <doc-ref>]`; it validates the report, writes a text document, and verifies the saved revision. `--doc` is an exact ref; use `--replace` only when intentionally replacing a non-report document.",
			"- Ask only for a decision that belongs to the human (direction, money, risk or an irreversible choice). Recommend one answer, give at most 2–3 alternatives, and batch related decisions into one ask. Do not also block the card or set its `next_actor` to the human for that same question; that duplicates the Inbox item. Keep `next_actor` on the agent and advance after the answer with its response event as evidence, for example `anx work done <card> --evidence event:<response_event_id>`.",
			"- Write for a busy executive: lead with the outcome and what needs them, then add detail. Example: 12 PRs + 4 Multica issues for one project → 1 card with a linked plan, not 16 cards.",
		}},
		{Title: "Initiative plans", Lines: []string{
			"Use `anx plan step add card:<slug> --step-id build --title \"Build\" --ref <ref-or-url>` to add a linked step. Branch with `anx plan step add card:<slug> --title \"QA\" --after build`. Stable ids belong to the plan; agents never select a view.",
			"Use `anx plan set card:<slug> --from-file plan.json` with {steps:[...]} for a full plan, or `step update <card> <step-id>` and `step rm <card> <step-id>` for edits. Writes compare the token from a fresh read; reconcile conflicts explicitly. Known refs override fallback status. Resolve chips in one request with `anx refs resolve <ref>...`.",
		}},
		{Title: "Daily loop", Lines: []string{
			"1. Run `anx orient` to see your identity, assigned work, asks and answers, notifications, stale work, and next commands.",
			"2. Read `anx work context card:<slug>` and register participation when doing substantive work. Use `anx work start card:<slug>` only when explicitly taking ownership of a Nexus-native task: it adds an assignee and marks in progress.",
			"3. Post `anx cards message card:<slug> --body \"What changed and why\"` after meaningful progress. Include evidence, decisions, blockers, uncertainty and next steps; avoid raw chat copies and repeated unchanged updates. Always name the task explicitly: participation does not change legacy current-card selection.",
			"4. Report execution blockers on the card. For a consequential human decision, create one recommended ask with `anx ask \"Question\" --subject-ref card:<slug> --recommend \"Preferred answer\"`; keep `next_actor` on the agent and do not also block the card for that question. Withdraw an ask that is no longer needed with `anx ask withdraw <event:ask-id> --reason \"<short reason>\"`. Use `anx work block` only for an authorized Nexus-native task blocked by an execution issue, not as a duplicate of a human ask.",
			"5. Run `anx await <ask-id>` when one answer gates the next step. For a batch, use `anx await --answers`; `anx orient` and `anx inbox list --status answered` also show replies. Exit 8 means timeout; exit 9 means an individual answer was rejected.",
			"6. Hermes, Claude Code, and Codex harnesses consume the same workspace-local agent notification: on wake, read `anx inbox list --unread` or `anx orient`, then mark each processed answer with `anx inbox read event:<ask-id>`. `inbox read` marks that answer only, including before wake delivery; `anx notifications read --wakeup-id <id>` separately marks the wake notification read.",
			"7. Verify acceptance criteria before changing task completion. For an authorized Nexus-native task, `anx work done card:<slug> --evidence <url|event:ref|artifact:ref>` resolves that explicit task and clears legacy presence. Report source-owned completion as attributed evidence for its authorized source workflow. Closing a session or finishing a run never completes a task.",
		}},
		{Title: "Runs and output", Lines: []string{
			"- Label agentctl work `anx.card.<card-slug>` so the run links to the card. A completed run does not complete the card.",
			"- Prefer fresh context from durable task evidence. Use previous sessions only as supported provenance/recovery clues for unfinished or unreflected work; do not assume a session can be resumed.",
			"- Prefer native live dashboard queries, then declared host-local pushed series; pasted numbers need an as-of timestamp. Declare an adapter before pushing (`anx adapters declare --body-file adapter.json`).",
			"- If designated as PM, remain an ordinary agent: summarize and propose with provenance, ask the user about consequential unresolved ambiguity, and preserve human approval gates. Designation grants no source-write or private-history authority.",
			"- Text output is compact. Use `--json` for scripts; follow `next_actions` rather than guessing refs.",
			"- Use `anx help <command>` for flags and `anx debug meta doc agent-guide` for this guide.",
		}},
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
		Summary:     "Render the bundled participant or PM skill.",
		JSONShape:   "`target`, `content`, `default_file`, `written_files`, `guide_topic`, `skill_name`",
		Composition: "Pure local helper. Renders a canonical portable participant or PM skill and optionally writes an unmanaged export.",
		Examples: []string{
			"anx meta skill anx",
			"anx meta skill anx --write-file ./SKILL.md",
			"anx meta skill --target cursor --write-file ./SKILL.md",
		},
		Flags: []localHelperFlag{
			{Name: "<target>", Description: "Skill target to render. Use `participant` or `pm`; `anx` and legacy `cursor` export the participant skill."},
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
	skill, err := skills.Get("participant")
	if err != nil {
		panic(err)
	} // Embedded catalog consistency is a build/test invariant.
	return skill.Content
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
