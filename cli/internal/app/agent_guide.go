package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const agentGuideSkillName = "anx-opinionated-onboarding"

const agentGuideSkillDescription = "Use the Agent Nexus daily loop to orient, start work, report progress, ask an operator, await an answer, and finish with evidence."

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
			"- Inside `agentctl run`, ANX uses the adapter context. Otherwise set `ANX_AS=<name>` or pass `--as <name>`; check the resolved handle and host in `anx orient`.",
		}},
		{Title: "Daily loop", Lines: []string{
			"1. Run `anx orient` to see your identity, assigned work, asks and answers, notifications, stale work, and next commands.",
			"2. Run `anx work start card:<slug>` to add yourself as assignee and mark the card in progress. Subsequent work verbs use that current card.",
			"3. Run `anx work note \"What changed\"` after meaningful progress.",
			"4. When blocked, run `anx work block \"Why\" --ask --recommend \"Preferred answer\"` or `anx ask \"Question\" --recommend \"Preferred answer\" [--alt \"Alternative\"]`. Use `anx review` for review and `anx escalate` for urgent intervention.",
			"5. Run `anx await <ask-id>` when an answer gates the next step. It prints one terminal result with outcome. Exit 8 means timeout; exit 9 means rejected.",
			"6. Run `anx work done --evidence <url|event:ref|artifact:ref>` to resolve the current card and clear presence.",
		}},
		{Title: "Runs and output", Lines: []string{
			"- Label agentctl work `anx.card.<card-slug>` so the run links to the card. A completed run does not complete the card.",
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
