package scopestream

import (
	"context"
	"encoding/json"
)

// Payload is a scope-pinned projection supplied by a trusted canonical mutation
// hook. No business computation may call PreparePayload to reclassify a DTO;
// A's derivation analyzer owns enforcement of that import/call boundary.
type Payload struct {
	scope string
	body  json.RawMessage
}

func PreparePayload(scope string, body json.RawMessage) (Payload, error) {
	if scope == "" || len(body) < 1 || len(body) > MaxPayloadBytes || !json.Valid(body) {
		return Payload{}, ErrProjection
	}
	return Payload{scope, append(json.RawMessage(nil), body...)}, nil
}
func (p Payload) ScopeID() string        { return p.scope }
func (p Payload) Bytes() json.RawMessage { return append(json.RawMessage(nil), p.body...) }

type Change struct {
	Stream       Stream
	RID, Version string
	Payload      Payload
}

// Writer appends at most four exact audience destinations and allocates a
// sequence for each destination in the canonical transaction. Sequences never
// share a global workspace head. Duplicate destinations are rejected first.
type Writer interface {
	ScopeID() string
	AppendChanges(context.Context, []Change) error
}

func Apply(ctx context.Context, w Writer, changes []Change) error {
	if w == nil || len(changes) < 1 || len(changes) > MaxStreamsPerScope {
		return ErrBudget
	}
	seen := map[Stream]bool{}
	for _, c := range changes {
		if string(c.Stream.Scope) != w.ScopeID() || c.Payload.scope != w.ScopeID() {
			return ErrDerivation
		}
		if seen[c.Stream] || c.Stream.Family == "" || len(c.Stream.Family) > 64 || c.Stream.Audience == "" || len(c.Stream.Audience) > 256 || c.RID == "" || c.Version == "" || len(c.RID) > 512 || len(c.Version) > 256 || len(c.Payload.body) < 1 || len(c.Payload.body) > MaxPayloadBytes || !json.Valid(c.Payload.body) {
			return ErrProjection
		}
		seen[c.Stream] = true
	}
	return w.AppendChanges(ctx, changes)
}
