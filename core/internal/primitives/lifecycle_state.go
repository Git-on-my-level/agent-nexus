package primitives

import (
	"database/sql"
	"strings"
)

// canonicalLifecycleState matches document.state: trashed > archived > active.
func canonicalLifecycleState(archivedAt, trashedAt sql.NullString) string {
	if trashedAt.Valid && strings.TrimSpace(trashedAt.String) != "" {
		return "trashed"
	}
	if archivedAt.Valid && strings.TrimSpace(archivedAt.String) != "" {
		return "archived"
	}
	return "active"
}

// LifecycleStateFromTimestampStrings derives state from optional RFC3339 timestamp strings.
func LifecycleStateFromTimestampStrings(archivedAt, trashedAt string) string {
	if strings.TrimSpace(trashedAt) != "" {
		return "trashed"
	}
	if strings.TrimSpace(archivedAt) != "" {
		return "archived"
	}
	return "active"
}
