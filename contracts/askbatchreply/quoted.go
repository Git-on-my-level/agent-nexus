package askbatchreply

import (
	"regexp"
	"strings"
)

// Lines that start the quoted history or signature of an email reply. Anything
// from the first such line on is not part of the reply.
var (
	quoteAttribution = regexp.MustCompile(`(?i)^(on\s.+wrote:|le\s.+a écrit\s?:|am\s.+schrieb.*:|el\s.+escribió:)$`)
	quoteHeader      = regexp.MustCompile(`(?i)^(-{2,}\s*original message\s*-{2,}|-{2,}\s*forwarded message\s*-{2,}|_{5,}|from:\s.+|sent from my .+|get outlook for .+)$`)
)

// StripQuoted returns only what the sender wrote at the top of an email reply:
// it drops the quoted history ("> " lines and everything after an "On ...
// wrote:" attribution or an "Original Message" / "From:" header) and a "-- "
// signature. A two-line attribution that a client wrapped ("On Tue, Nov 11,
// ... <x@y>\nwrote:") is recognised too.
func StripQuoted(body string) string {
	lines := strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n")
	kept := make([]string, 0, len(lines))
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, ">") || line == "-- " || trimmed == "--" ||
			quoteAttribution.MatchString(trimmed) || quoteHeader.MatchString(trimmed) {
			break
		}
		if i+1 < len(lines) && strings.HasPrefix(strings.ToLower(trimmed), "on ") &&
			quoteAttribution.MatchString(trimmed+" "+strings.TrimSpace(lines[i+1])) {
			break
		}
		kept = append(kept, line)
	}
	return strings.TrimSpace(strings.Join(kept, "\n"))
}
