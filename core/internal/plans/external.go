package plans

import "strings"

// IsIdentifierAlias permits opaque adapter-published identifiers in plans.
// Validation establishes syntax only; evidence establishes identity and status.
func IsIdentifierAlias(ref string) bool {
	return ref != "" && strings.TrimSpace(ref) == ref && !strings.Contains(ref, "://") && !strings.ContainsAny(ref, "\r\n\t\x00")
}
