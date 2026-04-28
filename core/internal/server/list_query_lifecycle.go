package server

import (
	"errors"
	"net/url"
	"strings"

	"agent-nexus-core/internal/primitives"
)

var errInvalidLifecycleQueryState = errors.New("state must be one of: active, archived, trashed")

// ParseListLifecycleStates reads repeated `state` query values. Omitting `state`
// defaults to active-only. Returns canonical ordered states (OR semantics downstream).
func ParseListLifecycleStates(query url.Values) ([]string, error) {
	raw := query["state"]
	if len(raw) == 0 {
		return []string{"active"}, nil
	}
	seen := make(map[string]struct{})
	var tokens []string
	for _, r := range raw {
		s := strings.TrimSpace(r)
		if s == "" {
			continue
		}
		switch s {
		case "active", "archived", "trashed":
			if _, ok := seen[s]; ok {
				continue
			}
			seen[s] = struct{}{}
			tokens = append(tokens, s)
		default:
			return nil, errInvalidLifecycleQueryState
		}
	}
	if len(tokens) == 0 {
		return []string{"active"}, nil
	}
	return primitives.NormalizeListLifecycleStates(tokens), nil
}
