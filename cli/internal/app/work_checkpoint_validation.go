package app

import (
	"encoding/json"
	"fmt"
	"unicode/utf8"

	"agent-nexus-cli/internal/errnorm"
)

// validateWorkCheckpointRequest checks the stable request wrapper before the
// CLI sends a write. The server remains authoritative for the full content
// schema and conditional fields such as a local artifact's host identifier.
func validateWorkCheckpointRequest(body map[string]any) error {
	version, ok := body["if_version"].(json.Number)
	if !ok {
		return errnorm.Usage("invalid_request", "checkpoint --from-file requires if_version as a nonnegative integer from the latest briefing (0 when no checkpoint exists)")
	}
	parsedVersion, err := version.Int64()
	if err != nil || parsedVersion < 0 {
		return errnorm.Usage("invalid_request", "if_version must be a nonnegative integer from the latest briefing (0 when no checkpoint exists)")
	}
	content, ok := body["checkpoint"].(map[string]any)
	if !ok {
		return errnorm.Usage("invalid_request", "checkpoint --from-file requires a checkpoint object")
	}
	for _, field := range []string{"summary", "next_action"} {
		value, ok := content[field].(string)
		if !ok || utf8.RuneCountInString(value) == 0 || utf8.RuneCountInString(value) > 4096 {
			return errnorm.Usage("invalid_request", fmt.Sprintf("checkpoint.%s must be a nonempty string of at most 4096 characters", field))
		}
	}
	return nil
}
