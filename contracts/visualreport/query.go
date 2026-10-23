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
	"time"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	extensionast "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/text"
)

const MaxRows = 2000

type Query struct {
	Collection          string   `json:"collection,omitempty"`
	StatusKeys          []string `json:"-"`
	SubjectRefs         []string `json:"subject_refs,omitempty"`
	Board               string   `json:"board,omitempty"`
	Card                string   `json:"card,omitempty"`
	Health              string   `json:"health,omitempty"`
	MinImpact           string   `json:"min_impact,omitempty"`
	BoardRefs           []string `json:"board_refs,omitempty"`
	ProjectRef          string   `json:"project_ref,omitempty"`
	CardRef             string   `json:"card_ref,omitempty"`
	Limit               int      `json:"limit,omitempty"`
	Sort                string   `json:"sort,omitempty"`
	Label               string   `json:"label,omitempty"`
	Role                string   `json:"role,omitempty"`
	Status              string   `json:"status,omitempty"`
	GroupBy             string   `json:"group_by,omitempty"`
	IncludeAnswered     bool     `json:"include_answered,omitempty"`
	AnsweredOnly        bool     `json:"answered_only,omitempty"`
	AnsweredWithinHours int      `json:"answered_within_hours,omitempty"`
}

type Panel struct {
	Role              string          `json:"role"`
	ID                string          `json:"id"`
	Type              string          `json:"type"`
	Author            string          `json:"author"`
	AuthoredAt        string          `json:"authored_at,omitempty"`
	ReviewBy          string          `json:"review_by,omitempty"`
	ReviewByDefaulted bool            `json:"review_by_defaulted,omitempty"`
	StaticData        map[string]any  `json:"-"`
	Query             Query           `json:"data"`
	Source            *SeriesSource   `json:"source,omitempty"`
	Fallback          *SeriesFallback `json:"fallback,omitempty"`
}

var identifier = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,79}$`)
var boardRef = regexp.MustCompile(`^board:[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
var projectRef = regexp.MustCompile(`^topic:[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
var cardRef = regexp.MustCompile(`^card:[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

func PanelRole(kind, role string) string {
	if role != "" {
		return role
	}
	switch kind {
	case "live-activity":
		return "secondary"
	case "explanation", "callout", "artifact-preview", "comparison":
		return "context"
	default:
		return "primary"
	}
}

func IsLive(kind string) bool {
	switch kind {
	case "live-collection", "live-loops", "live-loop", "live-initiatives", "live-asks", "live-work-mix", "live-activity", "live-fleet-health", "live-cards", "live-timeline":
		return true
	}
	return false
}

func Parse(content any) ([]Panel, error) {
	panels, err := ParseAll(content)
	out := []Panel{}
	for _, panel := range panels {
		if IsLive(panel.Type) || panel.Source != nil {
			out = append(out, panel)
		}
	}
	return out, err
}

// ParseAll includes authored panels so readers receive resolved provenance for
// the complete report, rather than inferring freshness from missing results.
func ParseAll(content any) ([]Panel, error) {
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
		Panels      []json.RawMessage `json:"panels"`
		GeneratedAt string            `json:"generated_at"`
	}
	if err := json.Unmarshal(raw, &report); err != nil {
		return nil, err
	}
	out := []Panel{}
	seen := map[string]bool{}
	for _, rawPanel := range report.Panels {
		var p struct {
			Role       string          `json:"role"`
			Query      json.RawMessage `json:"query"`
			Author     string          `json:"author"`
			AuthoredAt string          `json:"authored_at"`
			ReviewBy   string          `json:"review_by"`
			ID         string          `json:"id"`
			Type       string          `json:"type"`
			Data       json.RawMessage `json:"data"`
			Source     *SeriesSource   `json:"source"`
			Fallback   *SeriesFallback `json:"fallback"`
		}
		if json.Unmarshal(rawPanel, &p) != nil || !identifier.MatchString(p.ID) || seen[p.ID] {
			return nil, fmt.Errorf("panel ids must be valid and unique")
		}
		seen[p.ID] = true
		if len(p.Query) > 0 {
			p.Data = p.Query
		}
		p.Role = PanelRole(p.Type, p.Role)
		if p.Source != nil {
			out = append(out, Panel{Role: p.Role, ID: p.ID, Type: p.Type, Source: p.Source, Fallback: p.Fallback})
			continue
		}
		if !IsLive(p.Type) {
			authored := p.AuthoredAt
			if authored == "" {
				authored = report.GeneratedAt
			}
			base, _ := time.Parse(time.RFC3339Nano, authored)
			deadline := base.Add(7 * 24 * time.Hour)
			if p.ReviewBy != "" {
				deadline, _ = ReviewDeadline(p.ReviewBy, base)
			}
			var data map[string]any
			_ = json.Unmarshal(p.Data, &data)
			out = append(out, Panel{Role: p.Role, ID: p.ID, Type: p.Type, Author: p.Author, AuthoredAt: authored, ReviewBy: deadline.UTC().Format(time.RFC3339Nano), ReviewByDefaulted: p.ReviewBy == "", StaticData: data})
			continue
		}
		query, err := ParseQuery(p.Type, p.Data)
		if err != nil {
			return nil, fmt.Errorf("panel %s: %w", p.ID, err)
		}
		out = append(out, Panel{Role: p.Role, ID: p.ID, Type: p.Type, Query: query})
	}
	return out, nil
}

func ParseQuery(kind string, raw []byte) (Query, error) {
	q := Query{Limit: 10, Sort: "priority", GroupBy: "phase", AnsweredWithinHours: 168, MinImpact: "low"}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || fields == nil {
		return q, fmt.Errorf("data must be a query object")
	}
	allowed := map[string]bool{}
	switch kind {
	case "live-collection":
		q.Limit = 24
		q.GroupBy = "project"
		allowed = map[string]bool{"collection": true, "project_ref": true, "status": true, "group_by": true, "limit": true}
	case "live-loops":
		q.Limit = 24
		allowed = map[string]bool{"board": true, "health": true, "limit": true}
	case "live-loop":
		allowed = map[string]bool{"card": true}
	case "live-cards":
		allowed = map[string]bool{"board_refs": true, "project_ref": true, "card_ref": true, "label": true, "role": true, "status": true, "limit": true, "sort": true}
	case "live-initiatives":
		allowed = map[string]bool{"board_refs": true, "project_ref": true, "card_ref": true, "limit": true, "sort": true}
	case "live-asks":
		allowed = map[string]bool{"limit": true, "include_answered": true, "answered_only": true, "answered_within_hours": true, "card_ref": true}
	case "live-work-mix":
		allowed = map[string]bool{"board_refs": true, "project_ref": true, "card_ref": true, "group_by": true}
	case "live-activity":
		allowed = map[string]bool{"limit": true, "min_impact": true, "subject_refs": true, "collection": true}
	case "live-fleet-health":
		allowed = map[string]bool{}
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
	if kind == "live-collection" {
		if raw, ok := fields["status"]; ok {
			if json.Unmarshal(raw, &q.StatusKeys) != nil || q.StatusKeys == nil || len(q.StatusKeys) > 8 {
				return q, fmt.Errorf("status must be an array of at most 8 state keys")
			}
			delete(fields, "status")
		}
	}
	normalized, _ := json.Marshal(fields)
	if err := json.Unmarshal(normalized, &q); err != nil {
		return q, fmt.Errorf("invalid query field type")
	}
	if kind == "live-collection" {
		states := map[string][]string{"initiatives": {"waiting_on_you", "at_risk", "stalled", "on_track"}, "loops": {"failing", "stopped_reporting", "flat", "improving", "unknown"}, "agents": {"stuck", "offline", "idle", "working"}}
		allowed, ok := states[q.Collection]
		if !ok || q.Limit > 48 || (q.GroupBy != "project" && q.GroupBy != "none") {
			return q, fmt.Errorf("invalid collection query")
		}
		seen := map[string]bool{}
		for _, state := range q.StatusKeys {
			valid := false
			for _, s := range allowed {
				if state == s {
					valid = true
				}
			}
			if !valid || seen[state] {
				return q, fmt.Errorf("invalid or duplicate collection state")
			}
			seen[state] = true
		}
	}
	if kind == "live-activity" {
		if q.Collection != "" && q.Collection != "loops" && q.Collection != "initiatives" {
			return q, fmt.Errorf("invalid activity collection")
		}
		if len(q.SubjectRefs) > 100 {
			return q, fmt.Errorf("too many subject refs")
		}
		seen := map[string]bool{}
		for _, ref := range q.SubjectRefs {
			if len(ref) > 2048 || !strings.Contains(ref, ":") || strings.TrimSpace(ref) != ref || seen[ref] {
				return q, fmt.Errorf("invalid or duplicate subject ref")
			}
			seen[ref] = true
		}
	}
	if q.Limit < 1 || q.Limit > 100 || len(q.BoardRefs) > 16 || q.AnsweredWithinHours < 1 || q.AnsweredWithinHours > 720 {
		return q, fmt.Errorf("query exceeds limits")
	}
	if kind == "live-loops" {
		if q.Limit > 48 {
			return q, fmt.Errorf("loop limit exceeds 48")
		}
		if _, ok := fields["board"]; ok && !boardRef.MatchString(q.Board) {
			return q, fmt.Errorf("board must be a board ref")
		}
		if _, ok := fields["health"]; ok {
			for _, h := range strings.Split(q.Health, ",") {
				if h != "healthy" && h != "stalled" && h != "failing" && h != "overdue" && h != "unknown" {
					return q, fmt.Errorf("invalid loop health")
				}
			}
		}
	}
	if kind == "live-loop" && !cardRef.MatchString(q.Card) {
		return q, fmt.Errorf("card is required")
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
	for key, value := range map[string]string{"label": q.Label, "role": q.Role, "status": q.Status} {
		if _, present := fields[key]; present && (strings.TrimSpace(value) == "" || len(value) > 128) {
			return q, fmt.Errorf("invalid card filter")
		}
	}
	if q.MinImpact != "low" && q.MinImpact != "normal" && q.MinImpact != "high" {
		return q, fmt.Errorf("min_impact must be high, normal, or low")
	}
	if q.AnsweredOnly {
		q.IncludeAnswered = true
	}
	if q.Sort != "priority" && q.Sort != "updated" && q.Sort != "title" {
		return q, fmt.Errorf("sort must be priority, updated, or title")
	}
	if kind != "live-collection" && q.GroupBy != "phase" && q.GroupBy != "board" {
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
