package app

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

type askEvidenceLink struct {
	Label string `json:"label" yaml:"label"`
	URL   string `json:"url" yaml:"url"`
}

const askAuthoringTemplate = `---
title: Decision in one line
recommended_response: Proceed
proposals: [Wait]
refs: []
evidence: []
---
## Decision
State the decision in one line.
## Context
Explain why now and what is affected in 2–5 sentences.
## What each option does
Proceed: explain the effect.
Wait: explain the effect.
## Evidence
Use typed document:, card:, topic: refs and labelled external URLs.
Summarise code; never paste it. Link a PR or a commit-pinned file permalink with lines.
`

var askNamedPR = regexp.MustCompile(`(?i)\bPR\s*#?(\d+)\b`)
var askNamedRef = regexp.MustCompile("(?i)\\b(doc(?:ument)?|card|topic|file)\\s+[`\"]([^`\"\\n]+)[`\"]")
var askPointerOnly = regexp.MustCompile(`(?i)^\s*(?:please\s+)?(?:see|read|refer to|check)\s+[^\n]+\s*$`)

func lintAskAuthoring(body string, refs []string, links []askEvidenceLink) ([]string, error) {
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
	for _, match := range askNamedPR.FindAllStringSubmatch(body, -1) {
		if !regexp.MustCompile(`/pull/` + regexp.QuoteMeta(match[1]) + `(?:[^0-9]|$)`).MatchString(evidence) {
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
				if regexp.MustCompile(`/blob/[0-9a-fA-F]{40}/`).MatchString(link.URL) && strings.Contains(link.URL, match[2]) && strings.Contains(link.URL, "#L") {
					found = true
				}
			}
		}
		if !found {
			return nil, fmt.Errorf("%s %q has no matching evidence ref or permalink; add one", kind, match[2])
		}
	}
	warnings := []string{}
	for _, section := range []string{"Decision", "Context", "What each option does", "Evidence"} {
		if !strings.Contains(strings.ToLower(body), strings.ToLower(section)) {
			warnings = append(warnings, "Missing "+section+" section; use anx ask --help for the template")
		}
	}
	if strings.Contains(body, "```") {
		warnings = append(warnings, "Summarise code instead of pasting it; link a PR or commit permalink")
	}
	return warnings, nil
}
