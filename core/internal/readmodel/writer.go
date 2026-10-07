package readmodel

import (
	"context"
	"encoding/json"
	"strings"
	"unicode/utf8"

	"agent-nexus-core/internal/scopes"
)

// Executor is an injected, transaction-bound trusted template adapter. Exec
// returns affected rows. It must retain source authority, make errors sticky,
// and roll back the caller's transaction on any failure. No factory is exposed.
type Executor interface {
	Exec(context.Context, string, ...any) (int64, error)
}

type Rows interface {
	Next() bool
	Scan(...any) error
	Err() error
	Close() error
}
type QueryTx interface {
	Executor
	Query(context.Context, string, ...any) (Rows, error)
}

const DeletePayload = `DELETE FROM scope_feed_payloads WHERE scope_id=? AND generation=?
 AND family=? AND audience_key=? AND rid=? AND version=?`
const InsertPayload = `INSERT INTO scope_feed_payloads
 (scope_id,generation,family,audience_key,rid,version,data) VALUES(?,?,?,?,?,?,?)`

func exact(ctx context.Context, tx Executor, query string, args ...any) error {
	n, err := tx.Exec(ctx, query, args...)
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrProjection
	}
	return nil
}

func boundedText(s string, max int) bool {
	return s != "" && len(s) <= max && utf8.ValidString(s) && !strings.ContainsRune(s, 0)
}

// ApplyProjection persists feed keys, audience-specific payloads and exact
// counters in the existing source/worker transaction. It never commits. Payloads
// must cover exactly the new destinations; old payload deletes are versioned.
// At most 16 delta statements plus eight payload statements are issued.
func ApplyProjection(ctx context.Context, tx Executor, old, next *Projection, payloads map[Stream]json.RawMessage, rebuilding bool) error {
	d, err := planDelta(old, next, rebuilding)
	if err != nil {
		return err
	}
	if err = validateProjectionPayloads(old, next, payloads); err != nil {
		return err
	}
	for _, f := range d.Feeds {
		p, e := f.Projection, f.Entry
		args := []any{p.ScopeID, p.Generation, e.Family, e.Audience, e.Sort, p.RID, p.Version}
		q := InsertFeed
		if f.Delete {
			q = DeleteFeed
		}
		if err = exact(ctx, tx, q, args...); err != nil {
			return err
		}
		args = []any{p.ScopeID, p.Generation, e.Family, e.Audience, p.RID, p.Version}
		q = DeletePayload
		if !f.Delete {
			q = InsertPayload
			args = append(args, string(payloads[Stream{Scope: p.ScopeID, Family: e.Family, Audience: e.Audience}]))
		}
		if err = exact(ctx, tx, q, args...); err != nil {
			return err
		}
	}
	for _, c := range d.Counters {
		k := c.Key
		q := IncrementCounter
		args := []any{k.ScopeID, k.Generation, k.Family, k.Audience, k.Bucket, c.Delta}
		if c.Delta < 0 {
			q = DecrementCounter
			args = []any{c.Delta, k.ScopeID, k.Generation, k.Family, k.Audience, k.Bucket}
		}
		if err = exact(ctx, tx, q, args...); err != nil {
			return err
		}
	}
	return nil
}

func validateProjectionPayloads(old, next *Projection, payloads map[Stream]json.RawMessage) error {
	n := 0
	if next != nil {
		n = len(next.Entries)
	}
	if len(payloads) != n {
		return ErrProjection
	}
	// Validate all destinations and bytes before the first statement.
	for _, p := range []*Projection{old, next} {
		if p == nil {
			continue
		}
		if !boundedText(string(p.ScopeID), 512) {
			return ErrProjection
		}
		for _, e := range p.Entries {
			if !boundedText(e.Family, 128) || !boundedText(e.Audience, 512) {
				return ErrProjection
			}
			for _, b := range e.Buckets {
				if !boundedText(b, 128) {
					return ErrProjection
				}
			}
			if p == next {
				data, ok := payloads[Stream{Scope: p.ScopeID, Family: e.Family, Audience: e.Audience}]
				if !ok || len(data) > MaxItemBytes || !json.Valid(data) {
					return ErrProjection
				}
			}
		}
	}
	return nil
}

// Capture is a trusted canonical projector, selected by A, never author ingress.
// Before/After come from adjacent canonical rows in the source transaction.
type Capture func(scopes.Change, scopes.Projection) (Entry, json.RawMessage, error)

func CaptureCanonical(m scopes.CanonicalMutation, generation int64, capture Capture) (*Projection, *Projection, map[Stream]json.RawMessage, error) {
	if err := m.Validate(); err != nil {
		return nil, nil, nil, err
	}
	if generation < 1 || capture == nil {
		return nil, nil, nil, ErrProjection
	}
	i := m.Identity
	old := &Projection{ScopeID: i.ScopeID, Generation: generation, RID: i.RID, Version: m.PreviousVersion}
	next := &Projection{ScopeID: i.ScopeID, Generation: generation, RID: i.RID, Version: i.CanonicalVersion}
	payloads := map[Stream]json.RawMessage{}
	for _, c := range m.Changes {
		for index, source := range []*scopes.Projection{c.Before, c.After} {
			if source == nil || source.Deleted {
				continue
			}
			e, data, err := capture(c, *source)
			if err != nil {
				return nil, nil, nil, err
			}
			if e.Family != c.Family || e.Audience != c.Audience {
				return nil, nil, nil, ErrProjection
			}
			if index == 0 {
				old.Entries = append(old.Entries, e)
			} else {
				next.Entries = append(next.Entries, e)
				payloads[Stream{Scope: i.ScopeID, Family: e.Family, Audience: e.Audience}] = append(json.RawMessage(nil), data...)
			}
		}
	}
	if len(old.Entries) == 0 {
		old = nil
	}
	if len(next.Entries) == 0 {
		next = nil
	}
	if _, err := PlanDelta(old, next); err != nil {
		return nil, nil, nil, err
	}
	return old, next, payloads, nil
}
