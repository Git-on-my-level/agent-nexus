// Generic required-field and format checks (strings, string lists, RFC3339
// timestamps, provenance, typed refs) used by draft body validation.

package app

import (
	"fmt"
	"strings"
	"time"
)

func requiredObjectField(body map[string]any, key string, path string, out *[]string) (map[string]any, bool) {
	raw, exists := body[key]
	if !exists || raw == nil {
		*out = append(*out, path+" is required")
		return nil, false
	}
	obj, ok := raw.(map[string]any)
	if !ok {
		*out = append(*out, path+" must be an object")
		return nil, false
	}
	return obj, true
}

func requiredStringField(body map[string]any, key string, path string, nonEmpty bool, out *[]string) {
	raw, exists := body[key]
	if !exists {
		*out = append(*out, path+" is required")
		return
	}
	text, ok := raw.(string)
	if !ok {
		*out = append(*out, path+" must be a string")
		return
	}
	if nonEmpty && strings.TrimSpace(text) == "" {
		*out = append(*out, path+" must be non-empty")
	}
}

func requiredStringListField(body map[string]any, key string, path string, minItems int, out *[]string) ([]string, bool) {
	raw, exists := body[key]
	if !exists {
		*out = append(*out, path+" is required")
		return nil, false
	}
	values, ok := asStringList(raw)
	if !ok {
		*out = append(*out, path+" must be a list of strings")
		return nil, false
	}
	if len(values) < minItems {
		*out = append(*out, fmt.Sprintf("%s must include at least %d item", path, minItems))
		return nil, false
	}
	return values, true
}

func validateOptionalNonEmptyString(body map[string]any, key string, path string, out *[]string) {
	raw, exists := body[key]
	if !exists {
		return
	}
	validateNonEmptyString(raw, path, out)
}

func validateNonEmptyString(raw any, path string, out *[]string) {
	text, ok := raw.(string)
	if !ok {
		*out = append(*out, path+" must be a string")
		return
	}
	if strings.TrimSpace(text) == "" {
		*out = append(*out, path+" must be non-empty")
	}
}

func validateOptionalRFC3339(body map[string]any, key string, path string, out *[]string) {
	raw, exists := body[key]
	if !exists || raw == nil {
		return
	}
	text, ok := raw.(string)
	if !ok {
		*out = append(*out, path+" must be an RFC3339 datetime string")
		return
	}
	if _, err := time.Parse(time.RFC3339, text); err != nil {
		*out = append(*out, path+" must be an RFC3339 datetime string")
	}
}

func validateRequiredRFC3339(body map[string]any, key string, path string, out *[]string) {
	raw, exists := body[key]
	if !exists || raw == nil {
		*out = append(*out, path+" is required")
		return
	}
	text, ok := raw.(string)
	if !ok {
		*out = append(*out, path+" must be an RFC3339 datetime string")
		return
	}
	if strings.TrimSpace(text) == "" {
		*out = append(*out, path+" must be non-empty")
		return
	}
	if _, err := time.Parse(time.RFC3339, text); err != nil {
		*out = append(*out, path+" must be an RFC3339 datetime string")
	}
}

func validateProvenance(provenance map[string]any, path string, out *[]string) {
	sources, ok := requiredStringListField(provenance, "sources", path+".sources", 0, out)
	if ok {
		for idx, source := range sources {
			if strings.TrimSpace(source) == "" {
				*out = append(*out, fmt.Sprintf("%s[%d] must be non-empty", path+".sources", idx))
			}
		}
	}
	if rawNotes, exists := provenance["notes"]; exists {
		if _, ok := rawNotes.(string); !ok {
			*out = append(*out, path+".notes must be a string")
		}
	}
	if rawByField, exists := provenance["by_field"]; exists {
		asMap, ok := rawByField.(map[string]any)
		if !ok {
			*out = append(*out, path+".by_field must be a map of string to list<string>")
			return
		}
		for key, rawValues := range asMap {
			if strings.TrimSpace(key) == "" {
				*out = append(*out, path+".by_field field keys must be non-empty")
				continue
			}
			values, ok := asStringList(rawValues)
			if !ok {
				*out = append(*out, fmt.Sprintf("%s.by_field.%s must be a list of strings", path, key))
				continue
			}
			for idx, value := range values {
				if strings.TrimSpace(value) == "" {
					*out = append(*out, fmt.Sprintf("%s.by_field.%s[%d] must be non-empty", path, key, idx))
				}
			}
		}
	}
}

func asStringList(raw any) ([]string, bool) {
	switch values := raw.(type) {
	case []string:
		out := make([]string, 0, len(values))
		for _, value := range values {
			out = append(out, value)
		}
		return out, true
	case []any:
		out := make([]string, 0, len(values))
		for _, rawValue := range values {
			value, ok := rawValue.(string)
			if !ok {
				return nil, false
			}
			out = append(out, value)
		}
		return out, true
	default:
		return nil, false
	}
}

func validateTypedRefs(refs []string, path string, out *[]string) {
	for idx, ref := range refs {
		if err := validateTypedRef(ref); err != nil {
			*out = append(*out, fmt.Sprintf("%s[%d]: %s", path, idx, err.Error()))
		}
	}
}

func validateTypedRef(ref string) error {
	ref = strings.TrimSpace(ref)
	idx := strings.Index(ref, ":")
	if idx <= 0 || idx >= len(ref)-1 {
		return fmt.Errorf("typed ref %q must be in \"<prefix>:<value>\" form", ref)
	}
	prefix := strings.TrimSpace(ref[:idx])
	value := strings.TrimSpace(ref[idx+1:])
	if prefix == "" || value == "" {
		return fmt.Errorf("typed ref %q must be in \"<prefix>:<value>\" form", ref)
	}
	return nil
}

func containsRef(refs []string, expected string) bool {
	for _, ref := range refs {
		if strings.TrimSpace(ref) == expected {
			return true
		}
	}
	return false
}
