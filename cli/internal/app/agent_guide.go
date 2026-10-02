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

const agentGuideSkillVersion = "anx.participant.v2"

type guideSection struct {
	Title string
	Lines []string
}

func agentGuideIntro() string {
	return "Use Agent Nexus (`anx`) to keep your current task and its evidence visible to the workspace."
}

func agentGuideSections() []guideSection {
	return []guideSection{
		{Title: "Setup and identity", Lines: []string{
			"- Enroll a host once per workspace and machine with `anx host enroll`. A human approves the enrollment. Other agents on that host use the same host enrollment.",
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
		{Title: "Daily loop", Lines: []string{
			"1. Run `anx orient` to see your identity, assigned work, asks and answers, notifications, stale work, and next commands.",
			"2. Read `anx work context card:<slug>` and register participation when doing substantive work. Use `anx work start card:<slug>` only when explicitly taking ownership of a Nexus-native task: it adds an assignee and marks in progress.",
			"3. Post `anx cards message card:<slug> --body \"What changed and why\"` after meaningful progress. Include evidence, decisions, blockers, uncertainty and next steps; avoid raw chat copies and repeated unchanged updates. Always name the task explicitly: participation does not change legacy current-card selection.",
			"4. Report a blocker with `anx cards message card:<slug> --body \"Blocked: reason and next step\"` and, when a decision is needed, `anx ask \"Question\" --subject-ref card:<slug> --recommend \"Preferred answer\"`. These do not move source-owned task status. Use `anx work block \"Why\" card:<slug> --ask --recommend \"Preferred answer\"` only when deliberately changing an authorized Nexus-native task to blocked.",
			"5. Run `anx await <ask-id>` when an answer gates the next step. It prints one terminal result with outcome. Exit 8 means timeout; exit 9 means rejected.",
			"6. Verify acceptance criteria before changing task completion. For an authorized Nexus-native task, `anx work done card:<slug> --evidence <url|event:ref|artifact:ref>` resolves that explicit task and clears legacy presence. Report source-owned completion as attributed evidence for its authorized source workflow. Closing a session or finishing a run never completes a task.",
		}},
		{Title: "Runs and output", Lines: []string{
			"- Label agentctl work `anx.card.<card-slug>` so the run links to the card. A completed run does not complete the card.",
			"- Prefer fresh context from durable task evidence. Use previous sessions only as supported provenance/recovery clues for unfinished or unreflected work; do not assume a session can be resumed.",
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
