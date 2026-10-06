package plans

import (
	"net/url"
	"regexp"
	"strings"
)

var githubID = regexp.MustCompile(`^([A-Za-z0-9][A-Za-z0-9-]*)/([A-Za-z0-9_.-]+)#([1-9][0-9]*)$`)
var githubPath = regexp.MustCompile(`^/([A-Za-z0-9][A-Za-z0-9-]*)/([A-Za-z0-9_.-]+)/(pull|issues)/([1-9][0-9]*)/?$`)

// ParseExternalRef validates a source link without fetching it. Native identity
// is owner/repo#number for GitHub. Other aliases require structured evidence.
func ParseExternalRef(ref string) (authority, nativeID, link string, ok bool) {
	if strings.TrimSpace(ref) != ref {
		return
	}
	if m := githubID.FindStringSubmatch(strings.TrimPrefix(ref, "github:")); m != nil {
		return "github", m[1] + "/" + m[2] + "#" + m[3], "https://github.com/" + m[1] + "/" + m[2] + "/issues/" + m[3], true
	}
	if u, err := url.Parse(ref); err == nil && u.Scheme == "https" && strings.EqualFold(u.Host, "github.com") && u.User == nil {
		if m := githubPath.FindStringSubmatch(u.Path); m != nil {
			return "github", m[1] + "/" + m[2] + "#" + m[4], "https://github.com" + strings.TrimSuffix(u.Path, "/"), true
		}
	}
	return
}
func IsExternalRef(ref string) bool { _, _, _, ok := ParseExternalRef(ref); return ok }

// IsIdentifierAlias permits opaque adapter-published identifiers in plans.
// Validation establishes syntax only; evidence establishes identity and status.
func IsIdentifierAlias(ref string) bool {
	return ref != "" && strings.TrimSpace(ref) == ref && !strings.Contains(ref, "://") && !strings.ContainsAny(ref, "\r\n\t\x00")
}
