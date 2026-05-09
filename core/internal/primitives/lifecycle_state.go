package primitives

import (
	"database/sql"
	"strings"
)

const (
	LifecycleStateActive   = "active"
	LifecycleStateArchived = "archived"
	LifecycleStateTrashed  = "trashed"
)

var canonicalLifecycleStates = []string{LifecycleStateActive, LifecycleStateArchived, LifecycleStateTrashed}

// CanonicalLifecycleStates returns lifecycle states in contract order.
func CanonicalLifecycleStates() []string {
	return append([]string(nil), canonicalLifecycleStates...)
}

// canonicalLifecycleState matches document.state: trashed > archived > active.
func canonicalLifecycleState(archivedAt, trashedAt sql.NullString) string {
	if trashedAt.Valid && strings.TrimSpace(trashedAt.String) != "" {
		return LifecycleStateTrashed
	}
	if archivedAt.Valid && strings.TrimSpace(archivedAt.String) != "" {
		return LifecycleStateArchived
	}
	return LifecycleStateActive
}

// LifecycleStateFromTimestampStrings derives state from optional RFC3339 timestamp strings.
func LifecycleStateFromTimestampStrings(archivedAt, trashedAt string) string {
	if strings.TrimSpace(trashedAt) != "" {
		return LifecycleStateTrashed
	}
	if strings.TrimSpace(archivedAt) != "" {
		return LifecycleStateArchived
	}
	return LifecycleStateActive
}
