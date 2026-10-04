// Queries define bounded, read-only live report queries. Report strings
// are data, never code, fetch URLs, or SQL.
package visualreport

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	extensionast "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/text"
)

const MaxRows = 2000

type Query struct {
	BoardRefs           []string `json:"board_refs,omitempty"`
	ProjectRef          string   `json:"project_ref,omitempty"`
	CardRef             string   `json:"card_ref,omitempty"`
	Limit               int      `json:"limit,omitempty"`
	Sort                string   `json:"sort,omitempty"`
	GroupBy             string   `json:"group_by,omitempty"`
	IncludeAnswered     bool     `json:"include_answered,omitempty"`
	AnsweredOnly        bool     `json:"answered_only,omitempty"`
	AnsweredWithinHours int      `json:"answered_within_hours,omitempty"`
}

type Panel struct {
	ID       string          `json:"id"`
	Type     string          `json:"type"`
	Query    Query           `json:"data"`
	Source   *SeriesSource   `json:"source,omitempty"`
	Fallback *SeriesFallback `json:"fallback,omitempty"`
}

var identifier = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,79}$`)
var boardRef = regexp.MustCompile(`^board:[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
var projectRef = regexp.MustCompile(`^topic:[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
var cardRef = regexp.MustCompile(`^card:[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

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
			ID       string          `json:"id"`
			Type     string          `json:"type"`
			Data     json.RawMessage `json:"data"`
			Source   *SeriesSource   `json:"source"`
			Fallback *SeriesFallback `json:"fallback"`
		}
		if json.Unmarshal(rawPanel, &p) != nil || !identifier.MatchString(p.ID) || seen[p.ID] {
			return nil, fmt.Errorf("panel ids must be valid and unique")
		}
		seen[p.ID] = true
		if p.Source != nil {
			out = append(out, Panel{ID: p.ID, Type: p.Type, Source: p.Source, Fallback: p.Fallback})
			continue
		}
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
		allowed = map[string]bool{"board_refs": true, "project_ref": true, "card_ref": true, "limit": true, "sort": true}
	case "live-asks":
		allowed = map[string]bool{"limit": true, "include_answered": true, "answered_only": true, "answered_within_hours": true, "card_ref": true}
	case "live-work-mix":
		allowed = map[string]bool{"board_refs": true, "project_ref": true, "card_ref": true, "group_by": true}
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
	if _, present := fields["card_ref"]; present && !cardRef.MatchString(q.CardRef) {
		return q, fmt.Errorf("card_ref must be a card ref")
	}
	if q.AnsweredOnly {
		q.IncludeAnswered = true
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

var needsLine = regexp.MustCompile(`(?i)^Needs\s+[^:]+:`)

var summaryMarkdown = goldmark.New(goldmark.WithExtensions(extension.GFM))

func lineStarts(source []byte) []int {
	starts := []int{0}
	for offset, b := range source {
		if b == '\n' {
			starts = append(starts, offset+1)
		}
	}
	return starts
}

func sourceLine(starts []int, offset int) int {
	line := sort.Search(len(starts), func(i int) bool { return starts[i] > offset }) - 1
	if line < 0 {
		return 0
	}
	return line
}

func markVisibleBlockLines(node ast.Node, starts []int, visible []bool) {
	if node.Type() != ast.TypeBlock || node.Lines() == nil {
		return
	}
	if _, fenced := node.(*ast.FencedCodeBlock); fenced {
		return
	}
	if _, indented := node.(*ast.CodeBlock); indented {
		return
	}
	// Containers span their descendants, including code. Only mark leaf block
	// text so a list or quote cannot make a nested code block visible again.
	for child := node.FirstChild(); child != nil; child = child.NextSibling() {
		if child.Type() == ast.TypeBlock {
			return
		}
	}
	for i := 0; i < node.Lines().Len(); i++ {
		segment := node.Lines().At(i)
		first := sourceLine(starts, segment.Start)
		lastOffset := segment.Stop - 1
		if lastOffset < segment.Start {
			lastOffset = segment.Start
		}
		last := sourceLine(starts, lastOffset)
		for line := first; line <= last && line < len(visible); line++ {
			visible[line] = true
		}
	}
}

func markTaskLine(node ast.Node, starts []int, taskLines []bool) {
	for parent := node.Parent(); parent != nil; parent = parent.Parent() {
		if parent.Type() != ast.TypeBlock || parent.Lines() == nil || parent.Lines().Len() == 0 {
			continue
		}
		line := sourceLine(starts, parent.Lines().At(0).Start)
		if line < len(taskLines) {
			taskLines[line] = true
		}
		return
	}
}

// Ignore code examples; they are not commitments. Goldmark's GFM AST defines
// task items and code boundaries so counting follows CommonMark containers.
func Summary(markdown string) (string, Progress, []string) {
	source := []byte(markdown)
	doc := summaryMarkdown.Parser().Parse(text.NewReader(source))
	starts := lineStarts(source)
	visible := make([]bool, len(starts))
	taskLines := make([]bool, len(starts))
	first := ""
	progress := Progress{}
	needs := []string{}
	_ = ast.Walk(doc, func(node ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		if checkbox, ok := node.(*extensionast.TaskCheckBox); ok {
			progress.Total++
			if checkbox.IsChecked {
				progress.Done++
			}
			markTaskLine(node, starts, taskLines)
		}
		markVisibleBlockLines(node, starts, visible)
		return ast.WalkContinue, nil
	})

	// Goldmark marks only visible, non-code leaf-block lines above. Apply that
	// boundary before trimming indentation so indented-code examples cannot
	// become either the summary or a Needs request.
	for i, raw := range strings.Split(markdown, "\n") {
		if i >= len(visible) || !visible[i] || taskLines[i] {
			continue
		}
		line := strings.TrimSpace(raw)
		if line == "" {
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
