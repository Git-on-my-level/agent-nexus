package app

import (
	"net/url"
	"regexp"
	"strings"

	"agent-nexus-cli/internal/workspaceconfig"
)

// github.com is the only built-in forge and stays built in on any explicit
// port; every other host must be registered for the workspace with
// `anx config forge add` and matches one exact normalized origin.
var builtinForgeHosts = map[string]string{"github.com": "github"}

// urlOrigin normalizes a URL to its canonical origin, scheme included. An
// omitted port is the scheme's default, so http://forge.example:443 and
// https://forge.example are different origins, and an http:// URL can never
// match the canonical HTTPS origin a registration normalizes to.
func urlOrigin(u *url.URL) string {
	host := strings.ToLower(u.Hostname())
	if host == "" {
		return ""
	}
	port := u.Port()
	if port == "" {
		if u.Scheme == "http" {
			port = "80"
		} else {
			port = "443"
		}
	}
	return u.Scheme + "://" + host + ":" + port
}

// forgeKindFor reports the forge dialect registered for a URL's exact origin,
// or "" when the origin is not a registered forge for this workspace. A
// registration without a port is the HTTPS default origin (443); a URL on any
// other port is a different origin and never matches.
func forgeKindFor(forges []workspaceconfig.Forge, u *url.URL) string {
	if u == nil {
		return ""
	}
	if kind, ok := builtinForgeHosts[strings.ToLower(u.Hostname())]; ok {
		return kind
	}
	origin := urlOrigin(u)
	for _, forge := range forges {
		if entry, ok := workspaceconfig.CanonicalForgeOrigin(forge.Host); ok && entry == origin {
			return strings.ToLower(forge.Kind)
		}
	}
	return ""
}

func digitsOnly(s string) bool {
	if s == "" || len(s) > 10 {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// prViewSegments are pull-request view tabs that still identify the same PR
// number, so a copied files/commits tab URL satisfies the named-PR lint.
var prViewSegments = map[string]bool{"files": true, "commits": true, "diff": true, "conversations": true, "checks": true, "overview": true}

// forgePRNumber reports the pull-request number when a URL path matches the
// registered kind's PR form: /pull/N (github), /pulls/N (forgejo, gitea),
// /-/merge_requests/N (gitlab, nested groups allowed), /pull-requests/N
// (bitbucket cloud and server). The number must be the last real segment.
func forgePRNumber(kind, rawPath string) (string, bool) {
	path := strings.Trim(rawPath, "/")
	if path == "" {
		return "", false
	}
	if kind == "gitlab" {
		if i := strings.LastIndex(path, "/-/"); i > 0 {
			segments := strings.Split(path[i+3:], "/")
			if n := len(segments); n > 1 && prViewSegments[segments[n-1]] {
				segments = segments[:n-1]
			}
			if len(segments) == 2 && segments[0] == "merge_requests" && digitsOnly(segments[1]) {
				return segments[1], true
			}
		}
		return "", false
	}
	segments := strings.Split(path, "/")
	if n := len(segments); n > 1 && prViewSegments[segments[n-1]] {
		segments = segments[:n-1]
	}
	marker := map[string]string{"github": "pull", "forgejo": "pulls", "gitea": "pulls", "bitbucket": "pull-requests"}[kind]
	number := ""
	switch {
	case len(segments) == 4 && segments[2] == marker:
		number = segments[3]
	case kind == "bitbucket" && len(segments) == 6 && segments[0] == "projects" && segments[2] == "repos" && segments[4] == "pull-requests":
		// Bitbucket Server nests projects above repositories.
		number = segments[5]
	default:
		return "", false
	}
	if !digitsOnly(number) {
		return "", false
	}
	return number, true
}

func shaSegment(segment string) bool {
	if len(segment) != 40 && len(segment) != 64 {
		return false
	}
	for _, c := range segment {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F') {
			return false
		}
	}
	return true
}

// forgeCommitPinnedFile reports whether a URL is the kind's commit-pinned file
// permalink with a line anchor: /blob/<sha>/…#L (github), /src/commit/<sha>/…#L
// (forgejo, gitea), /-/blob/<sha>/…#L (gitlab), /src/<sha>/…#lines-N
// (bitbucket). A branch name never pins a permalink.
func forgeCommitPinnedFile(kind string, u *url.URL) bool {
	if u == nil {
		return false
	}
	path := strings.Trim(u.Path, "/")
	if kind == "gitlab" {
		if i := strings.LastIndex(path, "/-/"); i > 0 {
			segments := strings.Split(path[i+3:], "/")
			if len(segments) >= 3 && segments[0] == "blob" && shaSegment(segments[1]) {
				return lineAnchor(u.Fragment, "L")
			}
		}
		return false
	}
	segments := strings.Split(path, "/")
	if len(segments) < 4 {
		return false
	}
	switch kind {
	case "github":
		if segments[2] == "blob" && shaSegment(segments[3]) {
			return lineAnchor(u.Fragment, "L")
		}
	case "forgejo", "gitea":
		if len(segments) >= 5 && segments[2] == "src" && segments[3] == "commit" && shaSegment(segments[4]) {
			return lineAnchor(u.Fragment, "L")
		}
	case "bitbucket":
		if segments[2] == "src" && shaSegment(segments[3]) {
			return lineAnchor(u.Fragment, "lines-")
		}
		// Bitbucket Server nests projects above repositories.
		if len(segments) >= 6 && segments[0] == "projects" && segments[2] == "repos" && segments[4] == "src" && shaSegment(segments[5]) {
			return lineAnchor(u.Fragment, "lines-")
		}
	}
	return false
}

// lineAnchor requires the fragment to be the anchor plus a line number, so
// "#L" alone never pins anything. Copied line ranges keep working: GitHub's
// "#L10-L20" repeats the anchor on the right side, Bitbucket ranges use
// "#lines-10-19" or "#lines-10:19".
func lineAnchor(fragment, anchor string) bool {
	value := strings.TrimPrefix(fragment, anchor)
	if len(value) == len(fragment) || value == "" {
		return false
	}
	if anchor == "L" {
		value = strings.Replace(value, "-L", "-", 1)
	}
	for _, side := range strings.Split(value, "-") {
		if at := strings.IndexByte(side, ':'); at >= 0 {
			side = side[:at]
		}
		if !digitsOnly(side) {
			return false
		}
	}
	return true
}

var evidenceURLPattern = regexp.MustCompile(`https?://[^\s<>"'` + "`" + `\]\\]+`)

// evidenceURLs extracts and parses every http(s) URL in the evidence text so
// per-forge matching works on parsed hosts and paths, not raw substrings.
func evidenceURLs(evidence string) []*url.URL {
	out := []*url.URL{}
	for _, candidate := range evidenceURLPattern.FindAllString(evidence, -1) {
		candidate = strings.TrimRight(candidate, ".,;:!?")
		// Markdown and prose wrap URLs in parentheses: drop a trailing ")"
		// that has no matching opener instead of parsing it into the path.
		if strings.HasSuffix(candidate, ")") && strings.Count(candidate, "(") < strings.Count(candidate, ")") {
			candidate = strings.TrimRight(candidate, ")")
		}
		u, err := url.Parse(candidate)
		if err != nil || u.Host == "" {
			continue
		}
		out = append(out, u)
	}
	return out
}
