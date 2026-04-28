package app

import (
	"fmt"
	"os"
	"strings"

	"agent-nexus-cli/internal/errnorm"

	"gopkg.in/yaml.v3"
)

type humanAttentionFileFrontmatter struct {
	Title               string   `yaml:"title"`
	SubjectRef          string   `yaml:"subject_ref"`
	ThreadID            string   `yaml:"thread_id"`
	Refs                []string `yaml:"refs"`
	RequestID           string   `yaml:"request_id"`
	RequesterActorID    string   `yaml:"requester_actor_id"`
	RequesterAgentID    string   `yaml:"requester_agent_id"`
	RequesterLabel      string   `yaml:"requester_label"`
	RecommendedResponse string   `yaml:"recommended_response"`
	Proposals           []string `yaml:"proposals"`
	CoverageHint        string   `yaml:"coverage_hint"`
	Severity            string   `yaml:"severity"`
	Kind                string   `yaml:"kind"`
}

func splitHumanAttentionMarkdown(content string) (yamlSource string, body string, err error) {
	content = strings.TrimPrefix(content, "\uFEFF")
	lines := strings.Split(content, "\n")
	if len(lines) < 2 || strings.TrimSpace(lines[0]) != "---" {
		return "", "", fmt.Errorf("file must start with YAML frontmatter delimited by ---")
	}
	var yamlLines []string
	i := 1
	for i < len(lines) {
		line := strings.TrimRight(lines[i], "\r")
		if strings.TrimSpace(line) == "---" {
			break
		}
		yamlLines = append(yamlLines, line)
		i++
	}
	if i >= len(lines) {
		return "", "", fmt.Errorf("unclosed YAML frontmatter (missing closing ---)")
	}
	body = strings.TrimSpace(strings.Join(append([]string{}, lines[i+1:]...), "\n"))
	return strings.Join(yamlLines, "\n"), body, nil
}

func loadHumanAttentionFromMarkdownFile(path string, expectedKind string) (humanAttentionFileFrontmatter, string, []any, error) {
	var empty humanAttentionFileFrontmatter
	data, err := os.ReadFile(path)
	if err != nil {
		return empty, "", nil, err
	}
	yamlSrc, body, err := splitHumanAttentionMarkdown(string(data))
	if err != nil {
		return empty, "", nil, errnorm.Usage("invalid_request", err.Error())
	}
	var fm humanAttentionFileFrontmatter
	if err := yaml.Unmarshal([]byte(yamlSrc), &fm); err != nil {
		return empty, "", nil, errnorm.Usage("invalid_request", fmt.Sprintf("frontmatter YAML: %v", err))
	}
	fm.Title = strings.TrimSpace(fm.Title)
	fm.SubjectRef = strings.TrimSpace(fm.SubjectRef)
	fm.ThreadID = strings.TrimSpace(fm.ThreadID)
	fm.RequestID = strings.TrimSpace(fm.RequestID)
	fm.RequesterActorID = strings.TrimSpace(fm.RequesterActorID)
	fm.RequesterAgentID = strings.TrimSpace(fm.RequesterAgentID)
	fm.RequesterLabel = strings.TrimSpace(fm.RequesterLabel)
	fm.RecommendedResponse = strings.TrimSpace(fm.RecommendedResponse)
	fm.CoverageHint = strings.TrimSpace(fm.CoverageHint)
	fm.Severity = strings.TrimSpace(fm.Severity)
	fm.Kind = strings.TrimSpace(fm.Kind)
	for i := range fm.Refs {
		fm.Refs[i] = strings.TrimSpace(fm.Refs[i])
	}
	var proposals []string
	for _, p := range fm.Proposals {
		if s := strings.TrimSpace(p); s != "" {
			proposals = append(proposals, s)
		}
	}
	fm.Proposals = proposals

	if fm.Title == "" {
		return empty, "", nil, errnorm.Usage("invalid_request", "frontmatter title is required")
	}
	if fm.SubjectRef == "" {
		return empty, "", nil, errnorm.Usage("invalid_request", "frontmatter subject_ref is required")
	}
	if fm.RecommendedResponse == "" {
		return empty, "", nil, errnorm.Usage("invalid_request", "frontmatter recommended_response is required")
	}
	if fm.Kind != "" && !strings.EqualFold(fm.Kind, expectedKind) {
		return empty, "", nil, errnorm.Usage("invalid_request", fmt.Sprintf("frontmatter kind %q does not match anx human %s", fm.Kind, expectedKind))
	}

	responseProposals, err := buildCLIHumanAttentionResponseProposals(fm.RecommendedResponse, fm.Proposals)
	if err != nil {
		return empty, "", nil, err
	}
	return fm, body, responseProposals, nil
}
