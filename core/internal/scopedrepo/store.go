// Package scopedrepo is the trusted, typed boundary for the staged scope model.
// It is not wired to serving or the workspace initializer in this foundation PR.
package scopedrepo

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sync"

	"agent-nexus-core/internal/scopes"
)

//go:generate go run ../../cmd/scopedrepo-gen -root .
//go:embed schema.sql
var schemaSQL string

type Store struct{ db *sql.DB }

// New is for the trusted dispatcher/migration wiring only. Never expose Store to
// business computations. The boundary test forbids serving imports until cutover.
func New(db *sql.DB) *Store { return &Store{db: db} }

// Initialize adds only empty shadow tables. The migration owner must register it
// after the actual merge head; no current workspace calls this automatically.
func (s *Store) Initialize(ctx context.Context) error {
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if _, e = tx.ExecContext(ctx, schemaSQL); e != nil {
		return e
	}
	return tx.Commit()
}
func opaque() (string, error) {
	var b [16]byte
	_, e := rand.Read(b[:])
	return hex.EncodeToString(b[:]), e
}
func role(ctx context.Context, tx *sql.Tx, principal string, id scopes.ID) (scopes.Role, error) {
	if principal == "" || id == "" {
		return "", scopes.ErrDenied
	}
	var state string
	var r scopes.Role
	e := tx.QueryRowContext(ctx, query_role, principal, id).Scan(&state, &r)
	if errors.Is(e, sql.ErrNoRows) {
		return "", scopes.ErrDenied
	}
	if e != nil {
		return "", e
	}
	if !r.CanRead() {
		return "", scopes.ErrDenied
	}
	if state != "active" {
		return "", scopes.ErrUpdating
	}
	return r, nil
}

// Directory enumerates at most limit+1 own bindings, then probes at most limit
// domains. It never joins active state before LIMIT or searches for enough active
// rows. After is an INTERNAL cursor and must be encrypted by the transport layer.
func (s *Store) Directory(ctx context.Context, principal string, after scopes.ID, limit int) (scopes.DirectoryPage, error) {
	out := scopes.DirectoryPage{}
	if principal == "" {
		return out, scopes.ErrDenied
	}
	if limit < 1 || limit > scopes.MaxScopes {
		return out, scopes.ErrBudget
	}
	tx, e := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if e != nil {
		return out, e
	}
	defer tx.Rollback()
	rows, e := tx.QueryContext(ctx, query_directory, principal, after, limit+1)
	if e != nil {
		return out, e
	}
	var ids []scopes.ID
	for rows.Next() {
		var id scopes.ID
		if e = rows.Scan(&id); e != nil {
			rows.Close()
			return out, e
		}
		ids = append(ids, id)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return out, e
	}
	if len(ids) > limit {
		out.MoreScopes = true
		ids = ids[:limit]
	}
	for _, id := range ids {
		var state string
		e = tx.QueryRowContext(ctx, query_domain, id).Scan(&state)
		if e != nil && !errors.Is(e, sql.ErrNoRows) {
			return out, e
		}
		out.Bindings = append(out.Bindings, scopes.Binding{ID: id, Available: e == nil && state == "active"})
		out.After = id
	}
	return out, tx.Commit()
}

// ValidateSelection performs <=64 exact authority probes in a single snapshot.
// It returns no durable capability: every subsequent operation reauthorizes.
func (s *Store) ValidateSelection(ctx context.Context, principal string, ids []scopes.ID) error {
	if len(ids) == 0 || len(ids) > scopes.MaxScopes {
		return scopes.ErrBudget
	}
	seen := map[scopes.ID]bool{}
	tx, e := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if e != nil {
		return e
	}
	defer tx.Rollback()
	for _, id := range ids {
		if id == "" || seen[id] {
			return scopes.ErrBudget
		}
		seen[id] = true
		if _, e = role(ctx, tx, principal, id); e != nil {
			return e
		}
	}
	return tx.Commit()
}

// RegisterForMigration associates an already-written canonical row with an opaque scoped
// identity. Canonical creation must be incorporated into this transaction before
// production use; this foundation API is for backfill, not a live create route.
func (s *Store) RegisterForMigration(ctx context.Context, principal string, id scopes.ID, kind, canonical, alias, replay string) (string, error) {
	if principal == "" || id == "" || kind == "" || canonical == "" || replay == "" || len(alias) > 256 || len(replay) > 256 || len(canonical) > 512 || len(kind) > 32 {
		return "", scopes.ErrBudget
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return "", e
	}
	defer tx.Rollback()
	r, e := role(ctx, tx, principal, id)
	if e != nil {
		return "", e
	}
	if !r.CanWrite() {
		return "", scopes.ErrDenied
	}
	b, _ := json.Marshal([]string{string(id), kind, canonical, alias})
	sum := sha256.Sum256(b)
	hash := hex.EncodeToString(sum[:])
	var oldScope, oldKind, oldHash, result string
	e = tx.QueryRowContext(ctx, query_replay, principal, replay).Scan(&oldScope, &oldKind, &oldHash, &result)
	if e == nil {
		if oldScope != string(id) || oldKind != kind || oldHash != hash {
			return "", errors.New("replay input changed")
		}
		return result, tx.Commit()
	}
	if !errors.Is(e, sql.ErrNoRows) {
		return "", e
	}
	result, e = opaque()
	if e != nil {
		return "", e
	}
	if _, e = tx.ExecContext(ctx, query_register, id, kind, result, canonical); e != nil {
		return "", e
	}
	if alias != "" {
		if _, e = tx.ExecContext(ctx, query_alias, id, kind, alias, result); e != nil {
			return "", e
		}
	}
	if _, e = tx.ExecContext(ctx, query_save_replay, principal, replay, id, kind, hash, result); e != nil {
		return "", e
	}
	return result, tx.Commit()
}

// Derived contains no plaintext and has no conversion or public constructor.
// Its token is meaningful only to the computation that produced it.
type Derived struct {
	token  string
	origin *Computation
}
type Computation struct {
	title    func(string) (Derived, error)
	constant func(string) (Derived, error)
	persist  func(scopes.ID, string, Derived) error
}

func (c *Computation) DocumentTitle(id string) (Derived, error) {
	if c == nil || c.title == nil {
		return Derived{}, scopes.ErrClosed
	}
	return c.title(id)
}
func (c *Computation) Constant(value string) (Derived, error) {
	if c == nil || c.constant == nil {
		return Derived{}, scopes.ErrClosed
	}
	return c.constant(value)
}
func (c *Computation) Persist(destination scopes.ID, key string, v Derived) error {
	if c == nil || c.persist == nil {
		return scopes.ErrClosed
	}
	return c.persist(destination, key, v)
}

// Compute pins one scope and one transaction. Capabilities expire at callback
// return, including error/panic; no DB pointer or plaintext lives on the exposed
// capability. Dispatcher isolation is additionally required for implicit flows.
func (s *Store) Compute(ctx context.Context, principal string, id scopes.ID, fn func(*Computation) error) error {
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	r, e := role(ctx, tx, principal, id)
	if e != nil {
		return e
	}
	if !r.CanWrite() {
		return scopes.ErrDenied
	}
	var mu sync.Mutex
	alive := true
	values := map[string]string{}
	ops := 0
	charge := func() error {
		ops++
		if ops > scopes.MaxComputationOps {
			return scopes.ErrBudget
		}
		return nil
	}
	c := &Computation{}
	makeValue := func(value string) (Derived, error) {
		if len(values) >= scopes.MaxDerivedValues || len(value) > scopes.MaxValueBytes {
			return Derived{}, scopes.ErrBudget
		}
		token, e := opaque()
		if e != nil {
			return Derived{}, e
		}
		values[token] = value
		return Derived{token, c}, nil
	}
	c.constant = func(value string) (Derived, error) {
		mu.Lock()
		defer mu.Unlock()
		if !alive {
			return Derived{}, scopes.ErrClosed
		}
		if e := charge(); e != nil {
			return Derived{}, e
		}
		return makeValue(value)
	}
	c.title = func(rid string) (Derived, error) {
		mu.Lock()
		defer mu.Unlock()
		if !alive {
			return Derived{}, scopes.ErrClosed
		}
		if e := charge(); e != nil {
			return Derived{}, e
		}
		var title sql.NullString
		e := tx.QueryRowContext(ctx, query_title, id, rid).Scan(&title)
		if errors.Is(e, sql.ErrNoRows) {
			return Derived{}, scopes.ErrDenied
		}
		if e != nil {
			return Derived{}, e
		}
		if !title.Valid {
			return Derived{}, scopes.ErrBudget
		}
		return makeValue(title.String)
	}
	c.persist = func(destination scopes.ID, key string, v Derived) error {
		mu.Lock()
		defer mu.Unlock()
		if !alive {
			return scopes.ErrClosed
		}
		if e := charge(); e != nil {
			return e
		}
		if destination != id || v.origin != c {
			return scopes.ErrDerivation
		}
		if key == "" || len(key) > 256 {
			return scopes.ErrBudget
		}
		value, ok := values[v.token]
		if !ok {
			return scopes.ErrDerivation
		}
		_, e := tx.ExecContext(ctx, query_projection, id, key, value)
		return e
	}
	closeCap := func() { mu.Lock(); alive = false; clear(values); mu.Unlock() }
	defer closeCap()
	if e = fn(c); e != nil {
		return e
	}
	closeCap() // Expires before commit, including concurrently retained handles.
	return tx.Commit()
}

// CanPublish checks authority only; it neither returns content nor publishes it.
// The real publish mutation must repeat these checks inside its commit transaction.
func (s *Store) CanPublish(ctx context.Context, principal string, source, destination scopes.ID) error {
	tx, e := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if e != nil {
		return e
	}
	defer tx.Rollback()
	src, e := role(ctx, tx, principal, source)
	if e != nil {
		return e
	}
	dst, e := role(ctx, tx, principal, destination)
	if e != nil {
		return e
	}
	if !src.CanPublish() || !dst.CanWrite() {
		return scopes.ErrDenied
	}
	return tx.Commit()
}
