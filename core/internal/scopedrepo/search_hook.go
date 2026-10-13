package scopedrepo

import (
	"context"
	_ "embed"
	"encoding/json"

	"agent-nexus-core/internal/scopes"
	"agent-nexus-core/internal/scopesearch"
)

//go:embed search_schema.sql
var searchSchemaSQL string

// InitializeSearchSchema creates an empty, disabled shadow index. No canonical
// content is read, no backfill is started, and no serving readiness is conferred.
func (s *Store) InitializeSearchSchema(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, searchSchemaSQL); err != nil {
		return err
	}
	return tx.Commit()
}

const searchBind = `INSERT INTO scope_search_bindings(scope_id,kind,rid,parent) VALUES(?,?,?,?) ON CONFLICT(scope_id,kind,rid) DO NOTHING`

// SQLite can otherwise choose the scope-only prefix of the posting primary key
// for DELETE on an unanalyzed database. Pin the full resource maintenance index.
const searchDeletePostings = `DELETE FROM scope_search_postings INDEXED BY scope_search_postings_resource WHERE scope_id=? AND kind=? AND rid=?`
const searchDeleteHead = `DELETE FROM scope_search_resources WHERE scope_id=? AND kind=? AND rid=?`
const searchInsertHead = `INSERT INTO scope_search_resources(scope_id,kind,rid,version,parent,sort_key,text,indexed_bytes,truncated) VALUES(?,?,?,?,?,?,?,?,?)`

// One fixed template, at most 128 prepared terms per invocation. Slot uniqueness
// and its CHECK bound enforce the old-posting cap without COUNT over history.
const searchInsertPostings = `INSERT INTO scope_search_postings(scope_id,term,sort_key,rid,kind,slot) SELECT ?,value,?,?,?,CAST(key AS INTEGER)+? FROM json_each(?)`
const searchAdvanceGeneration = `UPDATE scope_search_generations SET generation=generation+1 WHERE scope_id=?`
const searchCreateGeneration = `INSERT INTO scope_search_generations(scope_id,generation) VALUES(?,1)`

func registeredSearchWrite(q string) bool {
	switch q {
	case searchBind, searchDeletePostings, searchDeleteHead, searchInsertHead, searchInsertPostings, searchAdvanceGeneration, searchCreateGeneration:
		return true
	}
	return false
}

// SearchCanonicalHook consumes a trusted, bounded canonical capture. This is
// deliberately unregistered in live writers: registry checks prove identity,
// not that a caller captured the correct text or legacy audience. Complete
// source capture and old-policy certification remain required before serving.
type SearchCanonicalHook struct{}

func (SearchCanonicalHook) ApplyCanonical(ctx context.Context, tx MutationTx, m scopes.CanonicalMutation) error {
	if err := m.Validate(); err != nil {
		return err
	}
	i := m.Identity
	// Exactly one scope-visible search delta; inbox/recipient payloads and
	// ambiguous multiple deltas must never be copied into scope-wide search.
	if len(m.Changes) != 1 || (i.Kind != "document" && i.Kind != "comment") || m.Changes[0].Audience != "all" {
		return scopesearch.ErrProjection
	}
	rows, err := tx.QueryContext(ctx, readModelHookIdentity, i.ScopeID, i.Kind, i.ResourceID)
	if err != nil {
		return err
	}
	var state, canonical string
	var generation, version, rid int64
	if !rows.Next() {
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		return scopesearch.ErrProjection
	}
	err = rows.Scan(&state, &generation, &version, &canonical, &rid)
	extra := rows.Next()
	iterationErr := rows.Err()
	closeErr := rows.Close()
	if err != nil {
		return err
	}
	if iterationErr != nil {
		return iterationErr
	}
	if closeErr != nil {
		return closeErr
	}
	if extra || state != "active" || generation < 1 || version != i.CanonicalVersion || canonical != i.CanonicalID || rid != i.RID {
		return scopesearch.ErrProjection
	}
	c := m.Changes[0]
	p := c.After
	if p == nil || p.Deleted {
		p = c.Before
	}
	if p == nil {
		return scopesearch.ErrProjection
	}
	parent := p.ContainerID
	if (i.Kind == "document" && parent != "") || (i.Kind == "comment" && parent == "") {
		return scopesearch.ErrProjection
	}
	if c.Before != nil && c.Before.ContainerID != parent {
		return scopesearch.ErrDerivation
	}
	if _, err = tx.ExecContext(ctx, searchBind, i.ScopeID, i.Kind, i.ResourceID, parent); err != nil {
		return err
	}
	return scopesearch.ApplyCanonical(ctx, &searchWriter{tx: tx, scope: string(i.ScopeID)}, c, false)
}

type searchWriter struct {
	tx    MutationTx
	scope string
}

func (w *searchWriter) ScopeID() string { return w.scope }
func (w *searchWriter) ReplaceSearch(ctx context.Context, c scopesearch.Change) error {
	if c.Scope != w.scope {
		return scopesearch.ErrDerivation
	}
	if _, err := w.tx.ExecContext(ctx, searchDeletePostings, c.Scope, c.Kind, c.RID); err != nil {
		return err
	}
	if _, err := w.tx.ExecContext(ctx, searchDeleteHead, c.Scope, c.Kind, c.RID); err != nil {
		return err
	}
	if !c.Delete {
		if _, err := w.tx.ExecContext(ctx, searchInsertHead, c.Scope, c.Kind, c.RID, c.Version, c.Parent, -c.Recency, c.Content.Normalized(), c.Content.IndexedBytes(), c.Content.Truncated()); err != nil {
			return err
		}
		terms := c.Content.Terms()
		for start := 0; start < len(terms); start += 128 {
			batch, err := json.Marshal(terms[start:min(start+128, len(terms))])
			if err != nil {
				return err
			}
			if _, err = w.tx.ExecContext(ctx, searchInsertPostings, c.Scope, -c.Recency, c.RID, c.Kind, start, string(batch)); err != nil {
				return err
			}
		}
	}
	result, err := w.tx.ExecContext(ctx, searchAdvanceGeneration, c.Scope)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		_, err = w.tx.ExecContext(ctx, searchCreateGeneration, c.Scope)
		return err
	}
	if n != 1 {
		return scopesearch.ErrProjection
	}
	return nil
}

var _ CanonicalHook = SearchCanonicalHook{}
var _ scopesearch.Writer = (*searchWriter)(nil)
