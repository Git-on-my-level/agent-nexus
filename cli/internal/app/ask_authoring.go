package app

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"agent-nexus-cli/internal/workspaceconfig"
)

type askEvidenceLink struct {
	Label string `json:"label" yaml:"label"`
	URL   string `json:"url" yaml:"url"`
}

const askDecisionTemplate = `---
title: "Approve the guard that keeps deleted users' data erased?"
situation: "After a user erases their data, five background jobs can still recreate their monthly usage totals."
impact: "Data we promised to delete comes back, creating privacy and compliance risk."
recommendation:
  action: "Make the background jobs check whether the user was deleted."
  why: "Reuse the guard other jobs already use. Cost: about 3 extra database reads per usage write."
alternatives:
  - action: "Build a different design."
    consequence: "More retry logic and slower to build."
if_no_answer: "The database cost change stays blocked until we decide; please answer by Friday."
refs: []
evidence: []
# Write for a busy boss with no context. Replace this neutral example with
# one short proposal. Keep the title near 80 characters and situation under 60 words.
# Options must be plain sentences a non-engineer understands.
#
# Before: Approve PR123 existing-fence reuse on every aggregate creator?
# Recovered c10041: ordinary/once/admin writers recreate monthly_usage_totals.
# Reuse external_write_fence and require an existing root, reading authority first.
# Options: Approve existing-fence reuse plus root check / Keep draft blocked.
#
# After: Deleted users' usage data can reappear. After erasure, five background
# jobs can recreate monthly totals, bringing back data we promised to delete.
# I recommend the same user-deleted guard the other jobs already use. Cost: about
# 3 extra reads per write. Alternative: more retry logic, slower to build.
# If no answer: the database cost change stays blocked until Friday.
#
# Put technical Markdown in frontmatter details; link evidence. Both are collapsed.
# Preview: anx ask --from-file ask.md --preview
# Publish: add subject_ref: card:<your-card> to frontmatter, then anx ask --from-file ask.md.
---
`

const askAuthoringTemplate = askDecisionTemplate

var askNamedPR = regexp.MustCompile(`(?i)\bPR\s*#?(\d+)\b`)
var askNamedRef = regexp.MustCompile("(?i)\\b(doc(?:ument)?|card|topic|file)\\s+[`\"]([^`\"\\n]+)[`\"]")
var askPointerOnly = regexp.MustCompile(`(?i)^\s*(?:please\s+)?(?:see|read|refer to|check)\s+[^\n]+\s*$`)

func lintAskAuthoring(body string, refs []string, links []askEvidenceLink, forges []workspaceconfig.Forge) ([]string, error) {
	for _, link := range links {
		u, e := url.Parse(link.URL)
		if e != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || strings.TrimSpace(link.Label) == "" {
			return nil, fmt.Errorf("evidence requires a label and an http(s) URL without credentials")
		}
	}
	if askPointerOnly.MatchString(body) {
		return nil, fmt.Errorf("ask body is only a pointer; add the decision, context and effects of each option")
	}
	evidence := strings.Join(refs, " ") + " " + body
	for _, link := range links {
		evidence += " " + link.URL
	}
	evidenceLinks := evidenceURLs(evidence)
	for _, match := range askNamedPR.FindAllStringSubmatch(body, -1) {
		satisfied := false
		for _, u := range evidenceLinks {
			kind := forgeKindFor(forges, u)
			if kind == "" {
				continue
			}
			if number, ok := forgePRNumber(kind, u.Path); ok && number == match[1] {
				satisfied = true
				break
			}
		}
		if !satisfied {
			return nil, fmt.Errorf("PR #%s has no matching evidence URL; add its labelled pull-request URL", match[1])
		}
	}
	for _, match := range askNamedRef.FindAllStringSubmatch(body, -1) {
		kind := strings.ToLower(match[1])
		if kind == "doc" {
			kind = "document"
		}
		needle := kind + ":" + match[2]
		found := strings.Contains(evidence, needle)
		if kind == "file" {
			for _, link := range links {
				u, e := url.Parse(link.URL)
				if e != nil {
					continue
				}
				forgeKind := forgeKindFor(forges, u)
				if forgeKind != "" && forgeCommitPinnedFile(forgeKind, u) && strings.Contains(link.URL, match[2]) {
					found = true
				}
			}
		}
		if !found {
			return nil, fmt.Errorf("%s %q has no matching evidence ref or permalink; add one", kind, match[2])
		}
	}
	warnings := []string{}
	if strings.Contains(body, "```") {
		warnings = append(warnings, "Summarise code instead of pasting it; link a PR or commit permalink")
	}
	return warnings, nil
}
