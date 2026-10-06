package resourceaccess

import (
	"encoding/json"
	"errors"
	"strings"
)

var ErrNULText = errors.New("text and JSON strings must not contain NUL")

// ValidateContent uses the declared media type. Binary bytes may happen to be
// JSON; they remain opaque to text validation, but never to reference indexing.
func ValidateContent(content any, contentType string) error {
	kind := strings.ToLower(strings.TrimSpace(strings.SplitN(contentType, ";", 2)[0]))
	if kind == "binary" || (strings.Contains(kind, "/") && !strings.HasPrefix(kind, "text/") && kind != "application/json" && !strings.HasSuffix(kind, "+json")) {
		return nil
	}
	if err := ValidateText(content); err != nil {
		return err
	}
	if b, ok := content.([]byte); ok {
		return ValidateText(string(b))
	}
	return nil
}

// ValidateText follows the same nested strings and encoded JSON containers as
// reference extraction. Binary values remain opaque unless they encode JSON.
func ValidateText(value any) error {
	switch v := value.(type) {
	case nil, bool, float64, json.Number, ReferenceManifest:
		return nil
	case string:
		if strings.ContainsRune(v, 0) {
			return ErrNULText
		}
		var nested any
		if json.Unmarshal([]byte(v), &nested) == nil {
			switch nested.(type) {
			case map[string]any, []any:
				return ValidateText(nested)
			}
		}
	case []byte:
		var nested any
		if json.Unmarshal(v, &nested) == nil {
			return ValidateText(nested)
		}
	case []any:
		for _, item := range v {
			if err := ValidateText(item); err != nil {
				return err
			}
		}
	case map[string]any:
		for key, item := range v {
			if err := ValidateText(key); err != nil {
				return err
			}
			if err := ValidateText(item); err != nil {
				return err
			}
		}
	default:
		// HTTP request structs, RawMessage and typed collections use their JSON
		// representation, including fields the current server does not interpret.
		encoded, err := json.Marshal(value)
		if err != nil {
			return err
		}
		var nested any
		if err = json.Unmarshal(encoded, &nested); err != nil {
			return err
		}
		return ValidateText(nested)
	}
	return nil
}

// SQL JSON columns can arrive as encoded strings/bytes, including scalar JSON
// strings. Decode those before validating; typed manifests remain distinct.
func ValidateSQLValues(args []any) error {
	for _, arg := range args {
		var raw []byte
		switch v := arg.(type) {
		case string:
			raw = []byte(v)
		case []byte:
			raw = v
		}
		var nested any
		if len(raw) > 0 && json.Unmarshal(raw, &nested) == nil {
			arg = nested
		}
		if err := ValidateText(arg); err != nil {
			return err
		}
	}
	return nil
}
