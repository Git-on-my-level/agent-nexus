package errnorm

import "fmt"

// WithWorkspace adds CLI-local routing context at the shared error boundary.
// Callers supply only the alias, URL host (never the full URL), and routing
// source. Keep the original failure, metadata and exit code intact.
func WithWorkspace(err error, alias, host, source string) *Error {
	normalized := Normalize(err)
	if normalized == nil || normalized.Kind != KindRemote {
		return normalized
	}
	details, _ := normalized.Details.(map[string]any)
	if details == nil {
		details = map[string]any{}
	}
	if details["workspace_source"] == nil {
		details["workspace_original_message"] = normalized.Message
		normalized.Message = fmt.Sprintf("%s; searched workspace %q (%s), selected by %s", normalized.Message, alias, host, source)
	}
	details["workspace"] = alias
	details["workspace_host"] = host
	details["workspace_source"] = source
	details["next_action"] = "Retry with --workspace <alias>."
	normalized.Hint = "Verify the ref belongs to the searched workspace; retry with --workspace <alias>."
	normalized.Details = details
	return normalized
}
