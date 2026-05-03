package markdownhygiene

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

const MaxLineBytes = 10 * 1024

type Warning struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type Result struct {
	Text     string    `json:"text"`
	Warnings []Warning `json:"warnings,omitempty"`
	Errors   []Error   `json:"errors,omitempty"`
}

var (
	atxHeadingRE        = regexp.MustCompile(`^([ \t]{0,3})(#{1,6})([^#\s].*)$`)
	arrowRE             = regexp.MustCompile(`\s*→\s*`)
	rawURLRE            = regexp.MustCompile(`(?i)\b(?:https?|ftp)://[^\s<>()]+`)
	tableSeparatorCell  = regexp.MustCompile(`^\s*:?-{3,}:?\s*$`)
	tableSeparatorPipes = regexp.MustCompile(`^\s*\|?\s*:?-{3,}:?\s*(\|\s*:?-{3,}:?\s*)+\|?\s*$`)
)

func Normalize(input string) Result {
	var result Result
	if input == "" {
		return Result{Text: input}
	}
	if errors := validateInput(input); len(errors) > 0 {
		result.Text = input
		result.Errors = errors
		return result
	}

	lines := splitLines(input)
	inFence := false
	fenceChar := byte(0)
	fenceLen := 0
	codeLine := make([]bool, len(lines))
	for i, line := range lines {
		trimmedLeft := strings.TrimLeft(line, " \t")
		if markerChar, markerLen, ok := fenceMarker(trimmedLeft); ok {
			codeLine[i] = true
			if !inFence {
				inFence = true
				fenceChar = markerChar
				fenceLen = markerLen
			} else if markerChar == fenceChar && markerLen >= fenceLen {
				inFence = false
				fenceChar = 0
				fenceLen = 0
			}
			continue
		}
		if inFence || isIndentedCodeLine(line) {
			codeLine[i] = true
		}
	}
	if inFence {
		result.Warnings = append(result.Warnings, Warning{Code: "unclosed_fence", Message: "markdown contains an unclosed code fence"})
	}

	changedHeading := false
	changedArrow := false
	changedTrailing := false
	for i, line := range lines {
		if codeLine[i] {
			continue
		}
		trimmedRight := strings.TrimRightFunc(line, unicode.IsSpace)
		if trimmedRight != line {
			line = trimmedRight
			changedTrailing = true
		}
		if fixed, ok := fixATXHeading(line); ok {
			line = fixed
			changedHeading = true
		}
		if fixed, ok := normalizeArrowsOutsideProtectedSpans(line); ok {
			line = fixed
			changedArrow = true
		}
		lines[i] = line
	}

	changedTable := normalizeTableBlocks(lines, codeLine)
	lines, changedHeadingGap := ensureHeadingBodyBlankLine(lines, codeLine)
	lines, changedBlankLines := collapseBlankLines(lines)

	if changedHeading {
		result.Warnings = append(result.Warnings, Warning{Code: "heading_spacing", Message: "added missing space after markdown heading marker"})
	}
	if changedHeadingGap {
		result.Warnings = append(result.Warnings, Warning{Code: "heading_blank_line", Message: "added blank line after markdown heading"})
	}
	if changedTable {
		result.Warnings = append(result.Warnings, Warning{Code: "table_pipe_padding", Message: "normalized spacing in GFM table rows"})
	}
	if changedArrow {
		result.Warnings = append(result.Warnings, Warning{Code: "arrow_spacing", Message: "normalized arrow spacing outside code"})
	}
	if changedBlankLines {
		result.Warnings = append(result.Warnings, Warning{Code: "blank_lines", Message: "collapsed repeated blank lines"})
	}
	if changedTrailing {
		result.Warnings = append(result.Warnings, Warning{Code: "trailing_whitespace", Message: "trimmed trailing whitespace"})
	}

	result.Text = strings.Join(lines, "\n")
	return result
}

func validateInput(input string) []Error {
	var errors []Error
	lineBytes := 0
	for _, r := range input {
		if r == '\n' {
			lineBytes = 0
			continue
		}
		lineBytes += len(string(r))
		if lineBytes > MaxLineBytes {
			errors = append(errors, Error{Code: "line_too_long", Message: fmt.Sprintf("markdown line exceeds %d bytes", MaxLineBytes)})
			break
		}
		if r == '\t' || r == '\r' {
			continue
		}
		if r < 0x20 {
			errors = append(errors, Error{Code: "control_character", Message: "markdown contains null bytes or disallowed control characters"})
			break
		}
	}
	return errors
}

func splitLines(input string) []string {
	input = strings.ReplaceAll(input, "\r\n", "\n")
	input = strings.ReplaceAll(input, "\r", "\n")
	return strings.Split(input, "\n")
}

func fenceMarker(trimmedLeft string) (byte, int, bool) {
	if trimmedLeft == "" {
		return 0, 0, false
	}
	marker := trimmedLeft[0]
	if marker != '`' && marker != '~' {
		return 0, 0, false
	}
	count := 0
	for count < len(trimmedLeft) && trimmedLeft[count] == marker {
		count++
	}
	if count < 3 {
		return 0, 0, false
	}
	return marker, count, true
}

func isIndentedCodeLine(line string) bool {
	return strings.HasPrefix(line, "\t") || strings.HasPrefix(line, "    ")
}

func fixATXHeading(line string) (string, bool) {
	m := atxHeadingRE.FindStringSubmatch(line)
	if m == nil {
		return line, false
	}
	return m[1] + m[2] + " " + strings.TrimLeft(m[3], " \t"), true
}

func isHeading(line string) bool {
	trimmed := strings.TrimLeft(line, " \t")
	if !strings.HasPrefix(trimmed, "#") {
		return false
	}
	count := 0
	for _, r := range trimmed {
		if r != '#' {
			break
		}
		count++
	}
	return count >= 1 && count <= 6 && len(trimmed) > count && unicode.IsSpace(rune(trimmed[count]))
}

func ensureHeadingBodyBlankLine(lines []string, codeLine []bool) ([]string, bool) {
	out := make([]string, 0, len(lines))
	changed := false
	for i, line := range lines {
		out = append(out, line)
		if i >= len(lines)-1 || codeLine[i] || !isHeading(line) {
			continue
		}
		next := strings.TrimSpace(lines[i+1])
		if next == "" || isHeading(lines[i+1]) || codeLine[i+1] {
			continue
		}
		out = append(out, "")
		changed = true
	}
	return out, changed
}

func collapseBlankLines(lines []string) ([]string, bool) {
	out := make([]string, 0, len(lines))
	blankRun := 0
	changed := false
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			blankRun++
			if blankRun > 1 {
				changed = true
				continue
			}
			out = append(out, "")
			continue
		}
		blankRun = 0
		out = append(out, line)
	}
	return out, changed
}

func normalizeArrowsOutsideProtectedSpans(line string) (string, bool) {
	segments := splitInlineCodeSegments(line)
	changed := false
	for i := range segments {
		if segments[i].protected {
			continue
		}
		updated := normalizeArrowSegmentSkippingURLs(segments[i].text)
		if updated != segments[i].text {
			segments[i].text = updated
			changed = true
		}
	}
	if !changed {
		return line, false
	}
	var b strings.Builder
	for _, segment := range segments {
		b.WriteString(segment.text)
	}
	return b.String(), true
}

type inlineSegment struct {
	text      string
	protected bool
}

func splitInlineCodeSegments(line string) []inlineSegment {
	var segments []inlineSegment
	start := 0
	codeRunLen := 0
	for i := 0; i < len(line); i++ {
		if line[i] != '`' {
			continue
		}
		j := i
		for j < len(line) && line[j] == '`' {
			j++
		}
		runLen := j - i
		inCode := codeRunLen > 0
		if start < i {
			segments = append(segments, inlineSegment{text: line[start:i], protected: inCode})
		}
		segments = append(segments, inlineSegment{text: line[i:j], protected: true})
		if !inCode {
			codeRunLen = runLen
		} else if runLen == codeRunLen {
			codeRunLen = 0
		}
		i = j - 1
		start = j
	}
	if start < len(line) {
		segments = append(segments, inlineSegment{text: line[start:], protected: codeRunLen > 0})
	}
	if len(segments) == 0 {
		return []inlineSegment{{text: line}}
	}
	return segments
}

func normalizeArrowSegmentSkippingURLs(s string) string {
	matches := rawURLRE.FindAllStringIndex(s, -1)
	if len(matches) == 0 {
		return arrowRE.ReplaceAllString(s, " → ")
	}
	var b strings.Builder
	last := 0
	for _, match := range matches {
		b.WriteString(arrowRE.ReplaceAllString(s[last:match[0]], " → "))
		b.WriteString(s[match[0]:match[1]])
		last = match[1]
	}
	b.WriteString(arrowRE.ReplaceAllString(s[last:], " → "))
	return b.String()
}

func normalizeTableBlocks(lines []string, codeLine []bool) bool {
	changed := false
	for i := 0; i < len(lines); i++ {
		if codeLine[i] || !isTableSeparator(lines[i]) {
			continue
		}
		start := i
		for start > 0 && !codeLine[start-1] && strings.Contains(lines[start-1], "|") && strings.TrimSpace(lines[start-1]) != "" {
			start--
		}
		end := i
		for end+1 < len(lines) && !codeLine[end+1] && strings.Contains(lines[end+1], "|") && strings.TrimSpace(lines[end+1]) != "" {
			end++
		}
		if start == end {
			continue
		}
		for j := start; j <= end; j++ {
			fixed := padTableRow(lines[j])
			if fixed != lines[j] {
				lines[j] = fixed
				changed = true
			}
		}
		i = end
	}
	return changed
}

func isTableSeparator(line string) bool {
	if !tableSeparatorPipes.MatchString(line) {
		return false
	}
	cells := strings.Split(strings.Trim(line, " \t|"), "|")
	if len(cells) < 2 {
		return false
	}
	for _, cell := range cells {
		if !tableSeparatorCell.MatchString(cell) {
			return false
		}
	}
	return true
}

func padTableRow(line string) string {
	trimmed := strings.TrimSpace(line)
	if !strings.Contains(trimmed, "|") {
		return line
	}
	hasLead := strings.HasPrefix(trimmed, "|")
	hasTrail := strings.HasSuffix(trimmed, "|")
	core := strings.Trim(trimmed, "|")
	rawCells := strings.Split(core, "|")
	cells := make([]string, len(rawCells))
	for i, cell := range rawCells {
		cells[i] = strings.TrimSpace(cell)
	}
	row := strings.Join(cells, " | ")
	if hasLead {
		row = "| " + row
	}
	if hasTrail {
		row += " |"
	}
	return row
}
