package scopestream

import (
	"context"
	"encoding/json"
	"strconv"
	"unicode/utf8"

	"agent-nexus-core/internal/scopes"
)

func prefix(s string, n int) string {
	if len(s) <= n {
		return s
	}
	s = s[:n]
	for !utf8.ValidString(s) && len(s) > 0 {
		s = s[:len(s)-1]
	}
	return s
}
func projection(p *scopes.Projection) any {
	if p == nil {
		return nil
	}
	return map[string]any{"title": prefix(p.Title, 256), "text_preview": prefix(p.Text, 512), "container_id": prefix(p.ContainerID, 256), "status": prefix(p.Status, 64), "rank": prefix(p.Rank, 64), "timestamp": p.Timestamp, "deleted": p.Deleted}
}

// ApplyCanonical consumes up to four exact audience deltas from A's hook. The
// bounded payload describes the delta; full resource bytes are read separately
// under their own authority. No canonical or historical row is loaded here.
func ApplyCanonical(ctx context.Context, w Writer, deltas []scopes.Change) error {
	if len(deltas) < 1 || len(deltas) > MaxStreamsPerScope {
		return ErrBudget
	}
	changes := make([]Change, 0, len(deltas))
	for _, d := range deltas {
		if err := d.Validate(); err != nil {
			return err
		}
		if w == nil || string(d.ScopeID) != w.ScopeID() {
			return ErrDerivation
		}
		body, err := json.Marshal(map[string]any{"kind": d.Kind, "before": projection(d.Before), "after": projection(d.After)})
		if err != nil {
			return err
		}
		payload, err := PreparePayload(string(d.ScopeID), body)
		if err != nil {
			return err
		}
		changes = append(changes, Change{Stream: Stream{Scope: d.ScopeID, Family: d.Family, Audience: d.Audience}, RID: d.ResourceID, Version: strconv.FormatInt(d.CanonicalVersion, 10), Payload: payload})
	}
	return Apply(ctx, w, changes)
}
