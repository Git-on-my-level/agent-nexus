package server

import (
	"net/http"
	"sort"
	"strings"

	"agent-nexus-core/internal/markdownhygiene"
)

type markdownHygieneCollector struct {
	warnings      []map[string]any
	changedFields map[string]struct{}
}

func (c *markdownHygieneCollector) normalizeField(w http.ResponseWriter, field string, value *string) bool {
	if value == nil {
		return true
	}
	result := markdownhygiene.Normalize(*value)
	if len(result.Errors) > 0 {
		writeMarkdownHygieneError(w, field, result.Errors)
		return false
	}
	if result.Text != *value {
		*value = result.Text
		c.addChanged(field)
	}
	for _, warning := range result.Warnings {
		c.addWarning(field, warning)
	}
	return true
}

func (c *markdownHygieneCollector) normalizeMapString(w http.ResponseWriter, field string, m map[string]any, key string) bool {
	if m == nil {
		return true
	}
	raw, exists := m[key]
	if !exists {
		return true
	}
	value, ok := raw.(string)
	if !ok {
		return true
	}
	if !c.normalizeField(w, field, &value) {
		return false
	}
	m[key] = value
	return true
}

func (c *markdownHygieneCollector) addWarning(field string, warning markdownhygiene.Warning) {
	if strings.TrimSpace(warning.Code) == "" {
		return
	}
	c.warnings = append(c.warnings, map[string]any{
		"field":   field,
		"code":    warning.Code,
		"message": warning.Message,
	})
}

func (c *markdownHygieneCollector) addChanged(field string) {
	if c.changedFields == nil {
		c.changedFields = map[string]struct{}{}
	}
	c.changedFields[field] = struct{}{}
}

func (c *markdownHygieneCollector) attach(payload map[string]any) map[string]any {
	if payload == nil || len(c.warnings) == 0 {
		return payload
	}
	fields := make([]string, 0, len(c.changedFields))
	for field := range c.changedFields {
		fields = append(fields, field)
	}
	sort.Strings(fields)
	payload["markdown_hygiene"] = map[string]any{
		"warnings":       c.warnings,
		"changed_fields": fields,
	}
	return payload
}

func writeMarkdownHygieneError(w http.ResponseWriter, field string, errors []markdownhygiene.Error) {
	items := make([]map[string]any, 0, len(errors))
	for _, item := range errors {
		items = append(items, map[string]any{
			"field":   field,
			"code":    item.Code,
			"message": item.Message,
		})
	}
	writeDetailedError(w, http.StatusBadRequest, "invalid_markdown", "Markdown contains unsupported control characters or line lengths", map[string]any{
		"markdown_hygiene": map[string]any{"errors": items},
	})
}

func isMarkdownContentType(contentType string) bool {
	normalized := strings.ToLower(strings.TrimSpace(contentType))
	return normalized == "" ||
		normalized == "text" ||
		normalized == "text/plain" ||
		normalized == "markdown" ||
		normalized == "text/markdown" ||
		normalized == "text/x-markdown"
}

func normalizeAnyMarkdownContent(w http.ResponseWriter, hygiene *markdownHygieneCollector, field string, contentType string, content *any) bool {
	if content == nil || !isMarkdownContentType(contentType) {
		return true
	}
	value, ok := (*content).(string)
	if !ok {
		return true
	}
	if !hygiene.normalizeField(w, field, &value) {
		return false
	}
	*content = value
	return true
}
