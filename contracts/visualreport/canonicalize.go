package visualreport

import (
	"bytes"
	"encoding/json"
	"strings"
)

// Canonicalize makes a visual-report body visible to Validate. It trims and
// lowercases the kind, and it unwraps YAML front matter whose kind is a visual
// report. Documents that are not visual reports are returned unchanged.
func Canonicalize(content []byte) []byte {
	payload := content
	frontKind := ""
	if body, kind, ok := visualReportFrontMatter(content); ok {
		payload = body
		frontKind = kind
	}
	if rewritten, ok := canonicalizeReportJSON(payload); ok {
		return rewritten
	}
	if isVisualReportKind(frontKind) {
		return []byte(`{"kind":"` + Kind + `"}`)
	}
	return content
}

func isVisualReportKind(kind string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(kind)), Kind)
}

func canonicalVisualReportKind(kind string) string {
	trimmed := strings.TrimSpace(kind)
	if len(trimmed) < len(Kind) {
		return Kind
	}
	return Kind + trimmed[len(Kind):]
}

func canonicalizeReportJSON(content []byte) ([]byte, bool) {
	var root map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(content), &root); err != nil {
		return nil, false
	}
	kind, ok := root["kind"].(string)
	if !ok || !isVisualReportKind(kind) {
		return nil, false
	}
	root["kind"] = canonicalVisualReportKind(kind)
	encoded, err := json.Marshal(root)
	if err != nil {
		return nil, false
	}
	return encoded, true
}

func visualReportFrontMatter(content []byte) ([]byte, string, bool) {
	s := string(content)
	nl := "\n"
	switch {
	case strings.HasPrefix(s, "---\r\n"):
		nl = "\r\n"
		s = s[len("---\r\n"):]
	case strings.HasPrefix(s, "---\n"):
		s = s[len("---\n"):]
	default:
		return nil, "", false
	}
	marker := nl + "---"
	idx := strings.Index(s, marker)
	if idx < 0 {
		return nil, "", false
	}
	header := s[:idx]
	after := s[idx+len(marker):]
	if strings.HasPrefix(after, nl) {
		after = after[len(nl):]
	} else if after != "" {
		return nil, "", false
	}
	kind := ""
	for _, line := range strings.Split(header, "\n") {
		line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if len(line) >= len("kind:") && strings.EqualFold(line[:len("kind:")], "kind:") {
			kind = strings.Trim(strings.TrimSpace(line[len("kind:"):]), `"'`)
		}
	}
	return []byte(after), kind, true
}
