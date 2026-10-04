// Queries define bounded, read-only live report queries. Report strings
// are data, never code, fetch URLs, or SQL.
package visualreport

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
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
	result := Validate(raw)
	if !result.Valid {
		return nil, fmt.Errorf("invalid visual report: %s", strings.Join(result.Errors, "; "))
	}
	var report struct {
		Panels []json.RawMessage `json:"panels"`
	}
	if err := json.Unmarshal(raw, &report); err != nil {
		return nil, err
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
	// JSON numbers use the browser's IEEE-754 semantics. Integral decimal and
	// exponent spellings are accepted; fractions and out-of-range values are not.
	for _, key := range []string{"limit", "answered_within_hours"} {
		if value, present := fields[key]; present {
			var number float64
			max := float64(100)
			if key == "answered_within_hours" {
				max = 720
			}
			if json.Unmarshal(value, &number) != nil || number < 1 || number > max || number != math.Trunc(number) {
				return q, fmt.Errorf("invalid integer query field")
			}
			fields[key], _ = json.Marshal(int(number))
		}
	}
	normalized, _ := json.Marshal(fields)
	if err := json.Unmarshal(normalized, &q); err != nil {
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
var listItemMarker = regexp.MustCompile(`^(?:[*+-]|[0-9]{1,9}[.)])[ \t]+`)

type listContainer struct {
	contentIndent int
}

// listItemIndent returns the absolute column where a list item's content
// begins. CommonMark permits up to three spaces before a marker and uses one
// to four columns of padding after it.
func listItemIndent(raw string, parentIndent int) (int, bool) {
	indent := len(raw) - len(strings.TrimLeft(raw, " "))
	if indent < parentIndent || indent-parentIndent > 3 {
		return 0, false
	}
	match := listItemMarker.FindStringIndex(raw[indent:])
	if match == nil {
		return 0, false
	}
	markerEnd := indent + match[1]
	markerStart := indent
	for markerStart < len(raw) && raw[markerStart] == ' ' {
		markerStart++
	}
	markerWidth := 1
	if raw[markerStart] >= '0' && raw[markerStart] <= '9' {
		markerWidth = 0
		for i := markerStart; i < markerEnd && raw[i] >= '0' && raw[i] <= '9'; i++ {
			markerWidth++
		}
		markerWidth++ // ordered-list delimiter
	}
	padding := markerEnd - indent - markerWidth
	if padding > 4 {
		padding = 1
	}
	return indent + markerWidth + padding, true
}

// Ignore fenced examples; they are not commitments. Preserve plain text for the UI.
func Summary(markdown string) (string, Progress, []string) {
	first := ""
	progress := Progress{}
	needs := []string{}
	var fence byte
	fenceLength := 0
	containers := []listContainer{}
	for _, raw := range strings.Split(markdown, "\n") {
		line := strings.TrimSpace(raw)
		indent := len(raw) - len(strings.TrimLeft(raw, " "))
		if fence == 0 {
			for len(containers) > 0 && indent < containers[len(containers)-1].contentIndent {
				containers = containers[:len(containers)-1]
			}
			parentIndent := 0
			if len(containers) > 0 {
				parentIndent = containers[len(containers)-1].contentIndent
			}
			if contentIndent, ok := listItemIndent(raw, parentIndent); ok {
				containers = append(containers, listContainer{contentIndent: contentIndent})
			}
		}
		// CommonMark fence indentation is measured after removing list item
		// container indentation; a four-space raw indent may therefore be zero.
		effectiveIndent := indent
		markerLine := strings.TrimLeft(raw, " ")
		if len(containers) > 0 && indent >= containers[len(containers)-1].contentIndent {
			effectiveIndent -= containers[len(containers)-1].contentIndent
			markerLine = strings.TrimLeft(raw[containers[len(containers)-1].contentIndent:], " ")
		}
		// CommonMark permits at most three spaces of indentation; closers use
		// the opening character and length and cannot have an info string.
		if effectiveIndent <= 3 && len(markerLine) >= 3 && (markerLine[0] == '`' || markerLine[0] == '~') {
			marker := markerLine[0]
			length := 0
			for length < len(markerLine) && markerLine[length] == marker {
				length++
			}
			tail := markerLine[length:]
			if fence == 0 && length >= 3 && (marker != '`' || !strings.Contains(tail, "`")) {
				fence = marker
				fenceLength = length
				continue
			}
			if fence == marker && length >= fenceLength && strings.Trim(tail, " \t\r") == "" {
				fence = 0
				continue
			}
		}
		if fence != 0 || line == "" {
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
