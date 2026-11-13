// Package askpresentation carries additive, authored decision copy. It performs
// no rewriting and no reads; legacy event bodies remain valid.
package askpresentation

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

var keys = []string{"situation", "impact", "recommendation", "alternatives", "if_no_answer", "details", "evidence", "stakes", "presentation_revision", "presentation_rewrite"}

// Copy projects only presentation fields from an already-authorized event.
func Copy(dst, src map[string]any) {
	for _, key := range keys {
		if v, ok := src[key]; ok && v != nil {
			dst[key] = v
		}
	}
}

// Validate checks optional new fields while accepting legacy payloads and
// preserving unknown fields for forward compatibility.
func Validate(payload map[string]any) error {
	for _, key := range []string{"situation", "impact", "if_no_answer", "details"} {
		if v, ok := payload[key]; ok {
			max := 4000
			if key == "details" {
				max = 32000
			}
			if err := text(v, key, max, false); err != nil {
				return err
			}
		}
	}
	if v, ok := payload["recommendation"]; ok {
		obj, ok := v.(map[string]any)
		if !ok {
			return fmt.Errorf("recommendation must be an object with action and why")
		}
		if err := text(obj["action"], "recommendation.action", 240, true); err != nil {
			return err
		}
		if err := text(obj["why"], "recommendation.why", 4000, true); err != nil {
			return err
		}
	}
	if v, ok := payload["alternatives"]; ok {
		list, ok := v.([]any)
		if !ok || len(list) > 5 {
			return fmt.Errorf("alternatives must be an array of at most 5 entries")
		}
		for i, v := range list {
			obj, ok := v.(map[string]any)
			if !ok {
				return fmt.Errorf("alternatives[%d] must be an object", i)
			}
			if err := text(obj["action"], fmt.Sprintf("alternatives[%d].action", i), 240, true); err != nil {
				return err
			}
			if err := text(obj["consequence"], fmt.Sprintf("alternatives[%d].consequence", i), 4000, true); err != nil {
				return err
			}
		}
	}
	if v, ok := payload["stakes"]; ok && v != nil {
		// Stakes let a reader size a decision before opening it: whether it can
		// be undone and what it costs. Both are the asker's claim, shown as such.
		obj, ok := v.(map[string]any)
		if !ok {
			return fmt.Errorf("stakes must be an object with irreversible and cost")
		}
		for key, value := range obj {
			switch key {
			case "irreversible":
				if _, ok := value.(bool); !ok {
					return fmt.Errorf("stakes.irreversible must be a boolean")
				}
			case "cost":
				if err := text(value, "stakes.cost", 80, true); err != nil {
					return err
				}
			default:
				return fmt.Errorf("unknown stakes field %s", key)
			}
		}
	}
	return nil
}
func text(v any, name string, max int, required bool) error {
	s, ok := v.(string)
	if !ok || (required && strings.TrimSpace(s) == "") || utf8.RuneCountInString(s) > max {
		qualifier := ""
		if required {
			qualifier = " with non-empty text"
		}
		return fmt.Errorf("%s must be a string%s of at most %d characters", name, qualifier, max)
	}
	return nil
}
