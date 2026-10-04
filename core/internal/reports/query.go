// Package reports defines bounded, read-only live report queries. Report strings
// are data, never code, fetch URLs, or SQL.
package reports

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

const MaxRows = 2000

type Query struct {
	BoardRefs           []string `json:"board_refs,omitempty"`
	ProjectRef          string   `json:"project_ref,omitempty"`
	Limit               int      `json:"limit,omitempty"`
	Sort                string   `json:"sort,omitempty"`
	GroupBy             string   `json:"group_by,omitempty"`
	IncludeAnswered     bool     `json:"include_answered,omitempty"`
	AnsweredWithinHours int      `json:"answered_within_hours,omitempty"`
}

type Panel struct {
	ID    string `json:"id"`
	Type  string `json:"type"`
	Query Query  `json:"data"`
}

var identifier = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,79}$`)
var boardRef = regexp.MustCompile(`^board:[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
var projectRef = regexp.MustCompile(`^topic:[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

func IsLive(kind string) bool {
	switch kind {
	case "live-initiatives", "live-asks", "live-work-mix", "live-activity":
		return true
	}
	return false
}

func Parse(content any) ([]Panel, error) {
	var raw []byte
	if text, ok := content.(string); ok {
		raw = []byte(text)
	} else {
		var err error
		raw, err = json.Marshal(content)
		if err != nil {
			return nil, err
		}
	}
	if len(raw) > 128*1024 {
		return nil, fmt.Errorf("report exceeds 128 KiB")
	}
	var report struct {
		Kind    string            `json:"kind"`
		Version int               `json:"schema_version"`
		Panels  []json.RawMessage `json:"panels"`
	}
	if json.Unmarshal(raw, &report) != nil || report.Kind != "anx.visual-report" || report.Version != 1 || len(report.Panels) < 1 || len(report.Panels) > 32 {
		return nil, fmt.Errorf("expected a version 1 visual report with 1..32 panels")
	}
	out := []Panel{}
	seen := map[string]bool{}
	for _, rawPanel := range report.Panels {
		var p struct {
			ID   string          `json:"id"`
			Type string          `json:"type"`
			Data json.RawMessage `json:"data"`
		}
		if json.Unmarshal(rawPanel, &p) != nil || !identifier.MatchString(p.ID) || seen[p.ID] {
			return nil, fmt.Errorf("panel ids must be valid and unique")
		}
		seen[p.ID] = true
		if !IsLive(p.Type) {
			continue
		}
		query, err := ParseQuery(p.Type, p.Data)
		if err != nil {
			return nil, fmt.Errorf("panel %s: %w", p.ID, err)
		}
		out = append(out, Panel{ID: p.ID, Type: p.Type, Query: query})
	}
	return out, nil
}

func ParseQuery(kind string, raw []byte) (Query, error) {
	q := Query{Limit: 10, Sort: "priority", GroupBy: "phase", AnsweredWithinHours: 168}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || fields == nil {
		return q, fmt.Errorf("data must be a query object")
	}
	allowed := map[string]bool{}
	switch kind {
	case "live-initiatives":
		allowed = map[string]bool{"board_refs": true, "project_ref": true, "limit": true, "sort": true}
	case "live-asks":
		allowed = map[string]bool{"limit": true, "include_answered": true, "answered_within_hours": true}
	case "live-work-mix":
		allowed = map[string]bool{"board_refs": true, "project_ref": true, "group_by": true}
	case "live-activity":
		allowed = map[string]bool{"limit": true}
	default:
		return q, fmt.Errorf("unsupported live type")
	}
	for key, value := range fields {
		if !allowed[key] || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return q, fmt.Errorf("unsupported or null query field")
		}
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&q); err != nil {
		return q, fmt.Errorf("invalid query field type")
	}
	if q.Limit < 1 || q.Limit > 100 || len(q.BoardRefs) > 16 || q.AnsweredWithinHours < 1 || q.AnsweredWithinHours > 720 {
		return q, fmt.Errorf("query exceeds limits")
	}
	seen := map[string]bool{}
	for _, ref := range q.BoardRefs {
		if !boardRef.MatchString(ref) || seen[ref] {
			return q, fmt.Errorf("board_refs must contain unique board refs")
		}
		seen[ref] = true
	}
	if _, present := fields["project_ref"]; present && !projectRef.MatchString(q.ProjectRef) {
		return q, fmt.Errorf("project_ref must be a topic ref")
	}
	if q.Sort != "priority" && q.Sort != "updated" && q.Sort != "title" {
		return q, fmt.Errorf("sort must be priority, updated, or title")
	}
	if q.GroupBy != "phase" && q.GroupBy != "board" {
		return q, fmt.Errorf("group_by must be phase or board")
	}
	return q, nil
}

type Progress struct {
	Done  int `json:"done"`
	Total int `json:"total"`
}

var checkbox = regexp.MustCompile(`^\s*-\s+\[([ xX])\](?:\s|$)`)
var needsLine = regexp.MustCompile(`(?i)^Needs\s+[^:]+:`)

// Ignore fenced examples; they are not commitments. Preserve plain text for the UI.
func Summary(markdown string) (string, Progress, []string) {
	first := ""
	progress := Progress{}
	needs := []string{}
	fence := ""
	for _, raw := range strings.Split(markdown, "\n") {
		line := strings.TrimSpace(raw)
		if strings.HasPrefix(line, "```") || strings.HasPrefix(line, "~~~") {
			marker := line[:3]
			if fence == "" {
				fence = marker
			} else if fence == marker {
				fence = ""
			}
			continue
		}
		if fence != "" || line == "" {
			continue
		}
		if match := checkbox.FindStringSubmatch(raw); match != nil {
			progress.Total++
			if strings.EqualFold(match[1], "x") {
				progress.Done++
			}
			continue
		}
		if needsLine.MatchString(line) {
			needs = append(needs, line)
			continue
		}
		if first == "" {
			first = strings.TrimSpace(strings.TrimLeft(line, "#"))
		}
	}
	return first, progress, needs
}
