package primitives

import (
	"context"
	"database/sql"
	"encoding/json"

	"agent-nexus-core/internal/resourceaccess"
)

type eventPageTargetsKey struct{}
type eventPageTargets struct {
	kind string
	ids  []string
}

func (s *denialSnapshot) targetRows(kind string, ids []string) (string, bool) {
	s.prepareTargetIndex()
	if s.targetIndex == nil {
		return "", false
	}
	rows := [][2]string{}
	for _, id := range ids {
		if _, denied := s.targetIndex[denialTarget{kind, id}]; denied {
			rows = append(rows, [2]string{kind, id})
		}
	}
	data, err := json.Marshal(rows)
	return string(data), err == nil
}

// Restricted to the event pager and its reference hydration. Every lookup has
// an explicit bounded set of primary keys. Inherited parent denials already
// deny their event/revision keys in the canonical closure. No general reader
// may reuse this context for resources outside the selected keys.
func withEventPageAccessScope(ctx context.Context, ids []string) context.Context {
	scope, ok := accessScopeFrom(ctx)
	if !ok {
		return ctx
	}
	ctx = WithRequestAccessScope(ctx, scope)
	p, _ := resourceaccess.PolicyFrom(ctx)
	original := p.ReadOnDB
	p.ReadOnDB = func(c context.Context, db resourceaccess.QueryRower, query string, args []any) (string, []any) {
		state, _ := c.Value(denialRequestKey{}).(*denialRequestState)
		state.Lock()
		if state.snapshot == nil {
			raw, _ := db.(*sql.DB)
			cached := cachedReadDenial(raw, scope)
			if cached != nil {
				var epoch int64
				// A warm capture reads only the epoch, never copies the complete
				// cached JSON through SQLite. The consuming SQL validates again.
				if db.QueryRowContext(c, `SELECT version FROM resource_access_epoch WHERE singleton=1`).Scan(&epoch) == nil && epoch == cached.epoch {
					state.snapshot = cached
				}
			}
		}
		state.Unlock()
		if denialSnapshotFrom(c) == nil {
			// Retain the canonical capture and bounded shared cache on misses.
			// This prepares SQL but does not execute the resource read twice.
			original(c, db, query, args)
		}
		snapshot := denialSnapshotFrom(c)
		targets, selected := c.Value(eventPageTargetsKey{}).(eventPageTargets)
		if snapshot == nil || !selected || !resourceaccess.AnonymousSQLParameters(query) {
			return original(c, db, query, args)
		}
		rows, ok := snapshot.targetRows(targets.kind, targets.ids)
		if !ok {
			return original(c, db, query, args)
		}
		pageSnapshot := &denialSnapshot{epoch: snapshot.epoch, rows: rows}
		bound := make([]any, 0, len(args)+1)
		bound = append(bound, rows)
		bound = append(bound, args...)
		return scopeReadWithSnapshot(c, query, pageSnapshot), bound
	}
	ctx = resourceaccess.WithPolicy(ctx, p)
	return context.WithValue(ctx, eventPageTargetsKey{}, eventPageTargets{kind: "event", ids: ids})
}

func eventPageRefScope(ctx context.Context, kind string, ids []string) context.Context {
	if _, ok := ctx.Value(eventPageTargetsKey{}).(eventPageTargets); !ok {
		return ctx
	}
	return context.WithValue(ctx, eventPageTargetsKey{}, eventPageTargets{kind: kind, ids: ids})
}
