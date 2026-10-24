package primitives

import (
	"context"
	"encoding/json"
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
	return context.WithValue(ctx, eventPageTargetsKey{}, eventPageTargets{kind: "event", ids: ids})
}

func eventPageRefScope(ctx context.Context, kind string, ids []string) context.Context {
	if _, ok := ctx.Value(eventPageTargetsKey{}).(eventPageTargets); !ok {
		return ctx
	}
	return context.WithValue(ctx, eventPageTargetsKey{}, eventPageTargets{kind: kind, ids: ids})
}
