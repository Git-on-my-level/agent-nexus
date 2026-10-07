package resourceaccess

import (
	"crypto/sha256"
	"database/sql/driver"
	"encoding/hex"
	"encoding/json"
	"modernc.org/sqlite"
	"strings"
	"unicode"
	"unicode/utf8"
)

// A bucket narrows write-time matching to identities with the same initial
// identifier token (up to eight runes), or initial punctuation rune. It never
// decides ownership: textHasReference verifies the complete identity afterward.
// Both paths use Unicode simple folding, as strings.EqualFold does.
func mentionBucket(id string) string {
	var out strings.Builder
	for n, r := range id {
		if n > 0 && !referenceIdentifierRune(r) {
			break
		}
		best := r
		for next := unicode.SimpleFold(r); next != r; next = unicode.SimpleFold(next) {
			if next < best {
				best = next
			}
		}
		out.WriteRune(best)
		if !referenceIdentifierRune(r) || utf8.RuneCountInString(out.String()) == 8 {
			break
		}
	}
	return out.String()
}
func mentionBuckets(atom string) []string {
	if !strings.HasPrefix(atom, textReferencePrefix) {
		return []string{}
	}
	text := strings.TrimPrefix(atom, textReferencePrefix)
	seen := map[string]bool{}
	for _, loc := range embeddedRefStart.FindAllStringSubmatchIndex(text, -1) {
		kind := strings.ToLower(text[loc[2]:loc[3]])
		if kind == "doc" {
			kind = "document"
		}
		// Conservative candidates are harmless: the complete matcher checks both
		// boundaries, including paired Markdown underscores and malformed bytes.
		bucket := mentionBucket(text[loc[1]:])
		// At most eight prefixes. Short IDs followed by paired Markdown
		// underscores share a bucket; the exact matcher rejects false prefixes.
		for end := range bucket {
			if end > 0 {
				seen[kind+":"+bucket[:end]] = true
			}
		}
		if bucket != "" {
			seen[kind+":"+bucket] = true
		}
	}
	buckets := make([]string, 0, len(seen))
	for b := range seen {
		buckets = append(buckets, b)
	}
	return buckets
}

// Exact ledger lookups use a fixed-size digest of SQLite NOCASE spelling.
// SQLite NOCASE folds ASCII only; non-ASCII bytes must stay unchanged.
func AtomKey(value string) string {
	b := []byte(value)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + ('a' - 'A')
		}
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
func AtomKeySQL(value string) string { return "anx_resource_atom_key(CAST(" + value + " AS BLOB))" }

// Projection probes bucket both the kind and identity with the same Unicode
// simple folding as the final matcher. Truncated kinds only add candidates.
func MentionRefBucketSQL(value string) string {
	return "anx_resource_mention_ref_bucket(CAST(" + value + " AS BLOB))"
}

func init() {
	sqlite.MustRegisterDeterministicScalarFunction("anx_resource_mention_ref_bucket", 1, func(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
		ref, err := referenceBytes(args[0])
		if err != nil {
			return nil, err
		}
		kind, id, _ := strings.Cut(ref, ":")
		return mentionBucket(kind) + ":" + mentionBucket(id), nil
	})
	sqlite.MustRegisterDeterministicScalarFunction("anx_resource_atom_key", 1, func(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
		value, err := referenceBytes(args[0])
		if err != nil {
			return nil, err
		}
		return AtomKey(value), nil
	})
	sqlite.MustRegisterDeterministicScalarFunction("anx_resource_mention_bucket", 1, func(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
		id, err := referenceBytes(args[0])
		if err != nil {
			return nil, err
		}
		return mentionBucket(id), nil
	})
	sqlite.MustRegisterDeterministicScalarFunction("anx_resource_mention_buckets", 1, func(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
		atom, err := referenceBytes(args[0])
		if err != nil {
			return nil, err
		}
		b, _ := json.Marshal(mentionBuckets(atom))
		return string(b), nil
	})
}
