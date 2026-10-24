package resourceaccess

import (
	"crypto/sha256"
	"database/sql/driver"
	"encoding"
	"encoding/hex"
	"encoding/json"
	"hash"
	"strings"
	"unicode"
	"unicode/utf8"

	"modernc.org/sqlite"
)

const proseChunkRunes = 16

func proseFold(r rune) rune {
	best := r
	for next := unicode.SimpleFold(r); next != r; next = unicode.SimpleFold(next) {
		if next < best {
			best = next
		}
	}
	return best
}
func proseKind(kind string) string {
	kind = strings.ToLower(kind)
	if kind == "doc" {
		return "document"
	}
	return kind
}
func proseWrite(h hash.Hash, r rune) {
	var b [utf8.UTFMax]byte
	n := utf8.EncodeRune(b[:], proseFold(r))
	_, _ = h.Write(b[:n])
}
func proseHash(kind string) hash.Hash {
	h := sha256.New()
	for _, r := range proseKind(kind) + ":" {
		proseWrite(h, r)
	}
	return h
}
func proseKey(ref string) string {
	kind, id, ok := strings.Cut(ref, ":")
	if !ok {
		return ""
	}
	h := proseHash(kind)
	for _, r := range id {
		proseWrite(h, r)
	}
	return hex.EncodeToString(h.Sum(nil))
}
func proseRuneSize(s string) int { _, n := utf8.DecodeRuneInString(s); return n }
func prosePrefixes(ref string) []string {
	kind, id, ok := strings.Cut(ref, ":")
	out := []string{}
	if !ok {
		return out
	}
	h := proseHash(kind)
	n := 0
	for pos, r := range id {
		proseWrite(h, r)
		n++
		if n%proseChunkRunes == 0 && pos+proseRuneSize(id[pos:]) < len(id) {
			out = append(out, hex.EncodeToString(h.Sum(nil)))
		}
	}
	return out
}

type proseProbe struct {
	Offset int      `json:"offset"`
	State  []byte   `json:"state"`
	Prefix string   `json:"prefix"`
	Keys   []string `json:"keys"`
}

func nextProseProbe(text string, p proseProbe, h hash.Hash) proseProbe {
	p.Keys = []string{}
	p.Prefix = ""
	for n := 0; n < proseChunkRunes && p.Offset < len(text); n++ {
		r, size := utf8.DecodeRuneInString(text[p.Offset:])
		p.Offset += size
		proseWrite(h, r)
		p.Keys = append(p.Keys, hex.EncodeToString(h.Sum(nil)))
	}
	if len(p.Keys) == proseChunkRunes && p.Offset < len(text) {
		p.Prefix = p.Keys[len(p.Keys)-1]
	}
	p.State, _ = h.(encoding.BinaryMarshaler).MarshalBinary()
	return p
}
func proseStarts(atom string) []proseProbe {
	out := []proseProbe{}
	if !strings.HasPrefix(atom, textReferencePrefix) {
		return out
	}
	text := strings.TrimPrefix(atom, textReferencePrefix)
	for _, loc := range embeddedRefStart.FindAllStringSubmatchIndex(text, -1) {
		out = append(out, nextProseProbe(text, proseProbe{Offset: loc[1]}, proseHash(text[loc[2]:loc[3]])))
	}
	return out
}

func ProseKeySQL(value string) string { return "anx_resource_prose_key(CAST(" + value + " AS BLOB))" }
func ProsePrefixesSQL(value string) string {
	return "anx_resource_prose_prefixes(CAST(" + value + " AS BLOB))"
}
func ProseStartsSQL(value string) string {
	return "anx_resource_prose_starts(CAST(" + value + " AS BLOB))"
}
func ProseNextSQL(value, state string) string {
	return "anx_resource_prose_next(CAST(" + value + " AS BLOB),CAST(" + state + " AS BLOB))"
}
func init() {
	for _, mode := range []string{"key", "prefixes", "starts"} {
		mode := mode
		sqlite.MustRegisterDeterministicScalarFunction("anx_resource_prose_"+mode, 1, func(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
			ref, err := referenceBytes(args[0])
			if err != nil {
				return nil, err
			}
			if mode == "key" {
				return proseKey(ref), nil
			}
			var v any
			if mode == "prefixes" {
				v = prosePrefixes(ref)
			} else {
				v = proseStarts(ref)
			}
			b, err := json.Marshal(v)
			return string(b), err
		})
	}
	sqlite.MustRegisterDeterministicScalarFunction("anx_resource_prose_next", 2, func(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
		atom, err := referenceBytes(args[0])
		if err != nil {
			return nil, err
		}
		state, err := referenceBytes(args[1])
		if err != nil {
			return nil, err
		}
		var p proseProbe
		if err := json.Unmarshal([]byte(state), &p); err != nil {
			return nil, err
		}
		h := sha256.New()
		if err := h.(encoding.BinaryUnmarshaler).UnmarshalBinary(p.State); err != nil {
			return nil, err
		}
		b, err := json.Marshal(nextProseProbe(strings.TrimPrefix(atom, textReferencePrefix), p, h))
		return string(b), err
	})
}
