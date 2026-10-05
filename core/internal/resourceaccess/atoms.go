package resourceaccess

import (
	"database/sql/driver"
	"encoding/json"
	"regexp"
	"sort"
	"strings"

	"modernc.org/sqlite"
)

// Scan explicit references in both structured content and prose/Markdown. The
// complete string is retained too, for bare IDs, aliases and external URLs.
var embeddedRef = regexp.MustCompile(`(?i)\b(thread|board|card|topic|document|doc|event|artifact|card_revision|document_revision|wakeup|plan|inbox|run)[\s\p{Z}\x{85}\x{0B}]*:[\s\p{Z}\x{85}\x{0B}]*[^\s\p{Z}\x{85}\x{0B}<>()\[\]{}"'` + "`" + `,;!?]+`)

var embeddedURL = regexp.MustCompile(`(?i)https?://[^\s\p{Z}\x{85}\x{0B}<>()\[\]{}"'` + "`" + `]+`)

func ReferenceAtoms(value string) []string {
	// Malformed text must not suppress otherwise readable references. Replacement
	// separates valid text runs without joining bytes into a different identifier.
	value = strings.ToValidUTF8(value, " ")
	seen := map[string]bool{}
	add := func(s string) {
		s = strings.TrimSpace(s)
		if kind, ref, ok := strings.Cut(s, ":"); ok {
			kind = strings.ToLower(strings.TrimSpace(kind))
			switch kind {
			case "doc":
				kind = "document"
				fallthrough
			case "thread", "board", "card", "topic", "document", "event", "artifact", "card_revision", "document_revision", "wakeup", "plan", "inbox", "run":
				s = kind + ":" + strings.TrimSpace(ref)
			}
		}
		// Explicit legacy document IDs have no length bound. Never silently
		// drop an atom based on size: that would create an authorization bypass.
		if s != "" {
			seen[s] = true
		}
	}
	var walk func(any)
	walk = func(v any) {
		switch v := v.(type) {
		case string:
			add(v)
			// Generic JSON envelopes (notably series label/state arrays) can contain
			// encoded structured values. Preserve their bare IDs as well as typed refs.
			var nested any
			if json.Unmarshal([]byte(v), &nested) == nil {
				switch nested.(type) {
				case map[string]any, []any:
					walk(nested)
				}
			}
			for _, ref := range embeddedRef.FindAllString(v, -1) {
				add(ref)
				add(strings.TrimRight(ref, "."))
			}
			for _, ref := range embeddedURL.FindAllString(v, -1) {
				add(ref)
				add(strings.TrimRight(ref, ".,;!?"))
			}
		case []any:
			for _, item := range v {
				walk(item)
			}
		case map[string]any:
			for key, item := range v {
				// Snapshots and maps can use resource IDs as keys.
				walk(key)
				walk(item)
			}
		}
	}
	// A SQL TEXT value can be an explicit legacy ID even when its bytes are
	// also valid JSON (including {}, [], or a quoted string). Retain both views.
	add(value)
	var decoded any
	if json.Unmarshal([]byte(value), &decoded) == nil {
		switch decoded.(type) {
		case map[string]any, []any, string:
			walk(decoded)
		default:
			// Scalar TEXT IDs may also happen to be valid JSON numbers/bools/null.
			add(value)
		}
	} else {
		walk(value)
	}
	refs := make([]string, 0, len(seen))
	for ref := range seen {
		refs = append(refs, ref)
	}
	sort.Strings(refs)
	return refs
}

func ReferenceAtomsJSON(value string) string {
	b, _ := json.Marshal(ReferenceAtoms(value))
	return string(b)
}

func init() {
	sqlite.MustRegisterDeterministicScalarFunction("anx_resource_refs", 1, func(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
		var value string
		switch v := args[0].(type) {
		case string:
			value = v
		case []byte:
			value = string(v)
		}
		return ReferenceAtomsJSON(value), nil
	})
}
