package resourceaccess

import (
	"database/sql/driver"
	"encoding/json"
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"modernc.org/sqlite"
)

// Scan explicit references in both structured content and prose/Markdown. The
// complete string is retained too, for bare IDs, aliases and external URLs.
var embeddedRef = regexp.MustCompile(`(?i)\b(thread|board|card|topic|document|doc|event|artifact|card_revision|document_revision|wakeup|plan|inbox|run)[\s\p{Z}\x{85}\x{0B}]*:[\s\p{Z}\x{85}\x{0B}]*[^\s\p{Z}\x{85}\x{0B}<>()\[\]{}"'` + "`" + `,;!?]+`)

// Legacy document IDs admit punctuation and internal whitespace, so prose has
// no unambiguous closing delimiter. Keep one text candidate instead of emitting
// every possible ID prefix. Denied identities resolve these candidates at read
// time, including resources created after the text was written.
const textReferencePrefix = "$anx-ref-text$"

var embeddedRefStart = regexp.MustCompile(`(?i)(?:^|[^[:alnum:]])(thread|board|card|topic|document|doc|event|artifact|card_revision|document_revision|wakeup|plan|inbox|run)[\s\p{Z}\x{85}\x{0B}]*:[\s\p{Z}\x{85}\x{0B}]*`)

func textHasReference(atom, target string) bool {
	if !strings.HasPrefix(atom, textReferencePrefix) {
		return false
	}
	kind, id, ok := strings.Cut(target, ":")
	if !ok || id == "" {
		return false
	}
	text := strings.TrimPrefix(atom, textReferencePrefix)
	for _, loc := range embeddedRefStart.FindAllStringSubmatchIndex(text, -1) {
		found := text[loc[2]:loc[3]]
		if strings.EqualFold(found, "doc") {
			found = "document"
		}
		if strings.EqualFold(found, kind) && len(text)-loc[1] >= len(id) && strings.EqualFold(text[loc[1]:loc[1]+len(id)], id) {
			end := loc[1] + len(id)
			if end == len(text) {
				return true
			}
			next, _ := utf8.DecodeRuneInString(text[end:])
			// Keep canonical handle prefixes distinct (private vs private-2),
			// while allowing prose/Markdown boundaries after the complete ID.
			markupUnderscore := next == '_' && loc[2] > 0 && text[loc[2]-1] == '_'
			if unicode.IsSpace(next) || ((unicode.IsPunct(next) || unicode.IsSymbol(next)) && next != '-' && next != '_') || markupUnderscore {
				return true
			}
		}
	}
	return false
}

// The LIKE range selects only prose candidates from the indexed atom ledger.
func TextReferenceMatchSQL(atom, target string) string {
	return "(" + atom + " LIKE '" + textReferencePrefix + "%' AND anx_resource_text_has_ref(" + atom + "," + target + "))"
}

var embeddedURL = regexp.MustCompile(`(?i)https?://[^\s\p{Z}\x{85}\x{0B}<>()\[\]{}"'` + "`" + `]+`)

func ReferenceAtoms(value string) []string { return referenceAtoms(value, false) }

// Structured values contribute their string atoms, never the serialization of
// a container. Scalar fields separately retain even JSON-shaped legacy IDs.
func referenceAtoms(value string, structured bool) []string {
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
			if strings.HasPrefix(v, textReferencePrefix) {
				return // An already-indexed candidate remains idempotent on backfill.
			}
			if embeddedRefStart.MatchString(v) {
				seen[textReferencePrefix+v] = true
			}
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
	if !structured {
		add(value)
	}
	var decoded any
	if json.Unmarshal([]byte(value), &decoded) == nil {
		switch decoded.(type) {
		case map[string]any, []any, string:
			walk(decoded)
		default:
			// Scalar TEXT IDs may also happen to be valid JSON numbers/bools/null.
			if !structured {
				add(value)
			}
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

func ContentReferenceAtomsJSON(value, contentType string) string {
	contentType = strings.ToLower(strings.TrimSpace(strings.SplitN(contentType, ";", 2)[0]))
	structured := contentType == "structured" || contentType == "application/json" || strings.HasSuffix(contentType, "+json")
	b, _ := json.Marshal(referenceAtoms(value, structured))
	return string(b)
}

// ReferenceSQLAtoms preserves the storage field's JSON/scalar distinction.
func ReferenceSQLAtoms(column string, structured bool) string {
	name := "anx_resource_refs"
	if structured {
		name = "anx_resource_json_refs"
	}
	return name + "(" + column + ")"
}

func init() {
	sqlite.MustRegisterDeterministicScalarFunction("anx_resource_text_has_ref", 2, func(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
		atom, _ := args[0].(string)
		target, _ := args[1].(string)
		if textHasReference(atom, target) {
			return int64(1), nil
		}
		return int64(0), nil
	})
	for _, structured := range []bool{false, true} {
		name := "anx_resource_refs"
		if structured {
			name = "anx_resource_json_refs"
		}
		sqlite.MustRegisterDeterministicScalarFunction(name, 1, func(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
			var value string
			switch v := args[0].(type) {
			case string:
				value = v
			case []byte:
				value = string(v)
			}
			b, _ := json.Marshal(referenceAtoms(value, structured))
			return string(b), nil
		})
	}
}
