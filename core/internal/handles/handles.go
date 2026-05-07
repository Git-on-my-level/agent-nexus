package handles

import (
	"crypto/rand"
	"encoding/binary"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

const MaxLength = 64

var uuidLikeRE = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

var reserved = map[string]struct{}{
	"api": {}, "attachments": {}, "cards": {}, "documents": {}, "events": {}, "new": {},
	"search": {}, "settings": {}, "threads": {}, "topics": {}, "trash": {}, "uuid": {},
}

var fallbackAdjectives = []string{"amber", "brisk", "clear", "daring", "eager", "fresh", "gentle", "honest"}
var fallbackNouns = []string{"anchor", "beacon", "canvas", "delta", "ember", "focus", "harbor", "signal"}

func Normalize(raw string) string {
	raw = strings.TrimSpace(strings.ToLower(raw))
	var b strings.Builder
	lastDash := false
	for _, r := range raw {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		case unicode.IsSpace(r) || r == '-' || r == '_' || r == '/' || r == '.':
			if b.Len() > 0 && !lastDash {
				b.WriteByte('-')
				lastDash = true
			}
		default:
			// Non-ASCII and punctuation are delimiters. We intentionally avoid
			// transliteration so handle generation stays dependency-free and stable.
			if b.Len() > 0 && !lastDash {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	out := strings.Trim(b.String(), "-")
	if len(out) > MaxLength {
		out = strings.Trim(out[:MaxLength], "-")
	}
	if IsReserved(out) || IsUUIDLike(out) {
		return ""
	}
	return out
}

func IsReserved(handle string) bool {
	_, ok := reserved[strings.TrimSpace(strings.ToLower(handle))]
	return ok
}

func IsUUIDLike(value string) bool {
	return uuidLikeRE.MatchString(strings.TrimSpace(value))
}

func Fallback(seed string) string {
	seed = strings.TrimSpace(seed)
	var n uint64
	if seed != "" {
		for _, r := range seed {
			n = n*131 + uint64(r)
		}
	} else {
		var buf [8]byte
		if _, err := rand.Read(buf[:]); err == nil {
			n = binary.BigEndian.Uint64(buf[:])
		}
	}
	adj := fallbackAdjectives[int(n%uint64(len(fallbackAdjectives)))]
	n = n / uint64(len(fallbackAdjectives))
	noun := fallbackNouns[int(n%uint64(len(fallbackNouns)))]
	n = n / uint64(len(fallbackNouns))
	return adj + "-" + noun + "-" + strconv.FormatUint(n%10000, 10)
}

func Candidate(raw, fallbackSeed string) string {
	if h := Normalize(raw); h != "" {
		return h
	}
	return Fallback(fallbackSeed)
}
