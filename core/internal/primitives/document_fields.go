// Document field and content helpers: decoding and normalizing stored values,
// optional field extraction, and visual-report content validation.

package primitives

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"agent-nexus-visualreport"
)

func asIntValue(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int32:
		return int(typed)
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	default:
		return 0
	}
}

func validateDocumentID(id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("document_id is required")
	}
	if filepath.IsAbs(id) {
		return fmt.Errorf("document_id must not be absolute")
	}
	if id == "." || id == ".." {
		return fmt.Errorf("document_id must not be . or ..")
	}
	if strings.Contains(id, "/") || strings.Contains(id, `\\`) {
		return fmt.Errorf("document_id must not contain path separators")
	}
	return nil
}

func normalizeDocumentContentType(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	switch raw {
	case "text", "structured", "binary":
		return raw, nil
	default:
		return "", fmt.Errorf("content_type must be one of: text, structured, binary")
	}
}

func optionalStringField(values map[string]any, key string) (string, error) {
	raw, exists := values[key]
	if !exists || raw == nil {
		return "", nil
	}
	text := strings.TrimSpace(anyStringValue(raw))
	if text == "" {
		return "", nil
	}
	return text, nil
}

func optionalStringListField(values map[string]any, key string) ([]string, error) {
	raw, exists := values[key]
	if !exists || raw == nil {
		return []string{}, nil
	}
	parsed, err := normalizeStringSlice(raw)
	if err != nil {
		return nil, fmt.Errorf("document.%s must be a list of strings", key)
	}
	return parsed, nil
}

func nullableMapValue(raw string) any {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	return raw
}

func setDocumentContentValue(out map[string]any, content []byte, contentType string) {
	switch strings.TrimSpace(contentType) {
	case "structured":
		var parsed any
		if err := json.Unmarshal(content, &parsed); err != nil {
			out["content"] = string(content)
			return
		}
		out["content"] = parsed
	case "binary":
		out["content_base64"] = base64.StdEncoding.EncodeToString(content)
	default:
		out["content"] = string(content)
	}
}

func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func sortStringsStable(values []string) {
	if len(values) < 2 {
		return
	}
	for i := 1; i < len(values); i++ {
		for j := i; j > 0 && values[j-1] > values[j]; j-- {
			values[j], values[j-1] = values[j-1], values[j]
		}
	}
}

// VisualReportValidationError is an invalid document write whose content is a
// recognized anx.visual-report that failed the shared validator.
type VisualReportValidationError struct {
	Errors []string
}

func (e *VisualReportValidationError) Error() string {
	message := "visual report validation failed"
	if e != nil && len(e.Errors) > 0 {
		message += ": " + strings.Join(e.Errors, "; ")
	}
	return message
}

func (e *VisualReportValidationError) Unwrap() error { return ErrInvalidDocumentRequest }

func rejectInvalidVisualReport(content []byte) error {
	// Validate the bytes that will be stored. This is the same recognizer
	// GET /docs/{id}/report and `anx report validate` use, including the size limit.
	result := visualreport.ValidateWrite(content, time.Now().UTC())
	if !result.Recognized || result.Valid {
		return nil
	}
	return &VisualReportValidationError{Errors: append([]string(nil), result.Errors...)}
}

func (s *Store) rejectInvalidVisualReportRevision(ctx context.Context, documentID, revisionID string, content []byte) error {
	now := time.Now().UTC()
	result := visualreport.ValidateWrite(content, now)
	if !result.Recognized || result.Valid {
		return nil
	}
	// Only an otherwise valid report with an expired deadline needs its prior
	// content. Keep ordinary writes and malformed reports on the existing path.
	if visualreport.Validate(content).Valid {
		previous, err := s.loadDocumentRevision(ctx, documentID, revisionID, true)
		if err != nil {
			return err
		}
		previousContent, err := encodeContent(previous["content"], anyStringValue(previous["content_type"]))
		if err != nil {
			return err
		}
		result = visualreport.ValidateRevision(content, previousContent, now)
	}
	if result.Valid {
		return nil
	}
	return &VisualReportValidationError{Errors: append([]string(nil), result.Errors...)}
}
