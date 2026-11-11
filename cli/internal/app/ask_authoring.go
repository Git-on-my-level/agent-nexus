package app

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"agent-nexus-cli/internal/workspaceconfig"
	"gopkg.in/yaml.v3"
)

type askEvidenceLink struct {
	Label string `json:"label" yaml:"label"`
	URL   string `json:"url" yaml:"url"`
	shape string // set when a YAML entry was not a {label, url} object
}

// UnmarshalYAML accepts the {label, url} object and records any other shape
// (a bare "Label=URL" string, a list) for lint to report with its index,
// instead of failing the whole frontmatter with a decoder type error.
// A recorded shape is a payload rule, like a --evidence flag without '=':
// preview lists it with the other violations, and --force must not submit it.
func (l *askEvidenceLink) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind == yaml.MappingNode {
		var plain struct {
			Label string `yaml:"label"`
			URL   string `yaml:"url"`
		}
		if err := node.Decode(&plain); err != nil {
			l.shape = fmt.Sprintf("an object that did not decode (%v) at line %d", err, node.Line)
			return nil
		}
		l.Label, l.URL = plain.Label, plain.URL
		return nil
	}
	if node.Kind == yaml.ScalarNode {
		l.shape = fmt.Sprintf("the string %q at line %d", node.Value, node.Line)
	} else {
		l.shape = fmt.Sprintf("a %s at line %d", map[yaml.Kind]string{yaml.SequenceNode: "list", yaml.AliasNode: "alias"}[node.Kind], node.Line)
	}
	return nil
}

const askDecisionTemplate = `---
title: "Measure the report delay before buying more capacity?"
situation: |-
  We send a daily sales report to 24 shops so managers can plan their morning orders. Reports normally arrive within 10 minutes of closing, and that was still true last week. Since Monday, the same reports have taken about 45 minutes. All 24 eventually arrived on each of the last three days, so we have evidence of delay, not missing sales data.

  The slowdown began when we moved report generation to a shared queue. We do not yet know whether reports are waiting behind other jobs or taking longer to calculate once they start. That distinction determines whether buying more capacity would help.
impact: >-
  Managers now wait an extra 35 minutes before they can use the report. Two shops have told support that they placed orders without it. If the queue is the cause, changing when reports run could restore delivery without a recurring bill. If calculation itself is slower, moving the schedule would leave the underlying problem in place.
recommendation:
  action: >-
    Measure one daily report run before buying more capacity.
  why: >-
    Record the waiting time and calculation time separately tomorrow. This should tell us which change addresses the delay, using the timing tools we already have. The tradeoff is one more morning of slow reports while we gather evidence; we cannot promise recovery tomorrow.
alternatives:
  - action: "Add capacity now for an estimated $300 a month."
    consequence: >-
      It may reduce the delay immediately, but could leave us paying more while reports still wait behind other work.
if_no_answer: >-
  We will keep the current queue and capacity, and continue checking that every report arrives. The delay remains unresolved. Please choose before tomorrow’s run if you want the measurement step included; no purchase will happen without approval.
refs: []
evidence: []
# Fictional example. Replace it with a short proposal to a busy boss with no context.
# What's happening: explain the baseline, what changed and the key numbers.
# Why it matters: show who is affected and why the choice depends on those facts.
# What I recommend: give the action, reason and tradeoff, including known cost.
# Alternatives: give real choices and their consequences, in plain sentences.
# If no answer: say what happens or stays blocked, only when known.
# Explain what the work does before naming its implementation. Write connected
# paragraphs that show why these facts lead to this decision, not terse labels.
# Use enough context to explain the choice; do not squeeze it into a word quota.
# Preserve uncertainty, conditions and numbers. Never invent facts, urgency,
# costs, approvals or a default. Preserve the original choices and conditions;
# later progress is dated context, not approval or proof that a gate passed.
# Do not turn unexplained terms into assumed mechanisms. Keep source-supplied
# choices and conditions; add no choices, deadlines or dependencies. A next-step
# promise is not an ask deadline. Use clearly conditional consequences.
# Omit unused optional fields; do not add filler.
# Keep the title near 80 characters. Answer buttons must stand alone.
# Put technical Markdown in frontmatter details; link evidence. Both are collapsed.
# When asked for a draft from supplied context, use only that context without
# research or preparation narration. Tools may only write a requested draft file
# or run an explicitly requested local preview. Do not perform the underlying
# work or search for missing facts. State material unknowns.
# Return only the document, with every field between the two --- delimiters.
# YAML evidence entries are objects: {label: "Source", url: "https://..."}.
# Label=URL is an inline flag syntax, not the YAML evidence shape.
# Every named pull request, even in details, needs its own pull-request URL;
# a checks or commit URL does not qualify. Omit unrelated reference history.
# Preview: anx ask --from-file ask.md --preview
# Publish: add subject_ref: card:<your-card> to frontmatter, then anx ask --from-file ask.md.
---
`

var askNamedPR = regexp.MustCompile(`(?i)\bPR\s*#?(\d+)\b`)
var askNamedRef = regexp.MustCompile("(?i)\\b(doc(?:ument)?|card|topic|file)\\s+[`\"]([^`\"\\n]+)[`\"]")
var askPointerOnly = regexp.MustCompile(`(?i)^\s*(?:please\s+)?(?:see|read|refer to|check)\s+[^\n]+\s*$`)

// evidenceShapeInvalid is true when a YAML evidence entry was not a
// {label, url} object. Its label and url were never filled in.
func evidenceShapeInvalid(links []askEvidenceLink) bool {
	for _, link := range links {
		if link.shape != "" {
			return true
		}
	}
	return false
}

// lintAskAuthoring returns every evidence and grounding problem at once, not
// the first: an ask used to be rejected once per problem, one per attempt.
func lintAskAuthoring(body string, refs []string, links []askEvidenceLink, forges []workspaceconfig.Forge) ([]string, []string) {
	var problems []string
	for i, link := range links {
		if link.shape != "" {
			problems = append(problems, fmt.Sprintf("evidence[%d] must be an object {label: <text>, url: <https://...>}; got %s", i, link.shape))
			continue
		}
		u, e := url.Parse(link.URL)
		if e != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || strings.TrimSpace(link.Label) == "" {
			problems = append(problems, fmt.Sprintf("evidence[%d] requires a non-empty label and an http(s) URL without credentials, as {label: <text>, url: <https://...>}", i))
		}
	}
	if askPointerOnly.MatchString(body) {
		problems = append(problems, "ask body is only a pointer; add the decision, context and effects of each option")
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
			problems = append(problems, fmt.Sprintf("PR #%s has no matching evidence URL; add {label: \"PR #%s\", url: <its pull-request URL>} to evidence", match[1], match[1]))
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
			problems = append(problems, fmt.Sprintf("%s %q has no matching evidence ref or permalink; add one", kind, match[2]))
		}
	}
	warnings := []string{}
	if strings.Contains(body, "```") {
		warnings = append(warnings, "Summarise code instead of pasting it; link a PR or commit permalink")
	}
	return warnings, problems
}
