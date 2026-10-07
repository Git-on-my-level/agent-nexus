package server

// This durable SQL adapter exercises the production executors against A's real
// shadow authority schema. It is a reference for A's owned typed repository
// integration, not permission to enable a partly migrated production reader.
import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"agent-nexus-core/internal/scopedrepo"
	"agent-nexus-core/internal/scopes"
	"agent-nexus-core/internal/scopesearch"
	"agent-nexus-core/internal/scopestream"
	"agent-nexus-core/internal/storage"
	"modernc.org/sqlite"
)

var scopeCExamined atomic.Int64

func init() {
	sqlite.MustRegisterScalarFunction("scope_c_examined", 1, func(_ *sqlite.FunctionContext, _ []driver.Value) (driver.Value, error) {
		scopeCExamined.Add(1)
		return int64(1), nil
	})
}

const scopeCShadowDDL = `
CREATE TABLE scope_principal_generations(principal TEXT PRIMARY KEY,generation INTEGER NOT NULL CHECK(generation>0)) WITHOUT ROWID;
CREATE TABLE scope_search_generations(scope_id TEXT PRIMARY KEY,generation INTEGER NOT NULL CHECK(generation>0),FOREIGN KEY(scope_id) REFERENCES scope_domains(id)) WITHOUT ROWID;
CREATE TABLE scope_search_resources(scope_id TEXT,kind TEXT CHECK(kind IN ('document','comment')),rid TEXT,version TEXT,parent TEXT,sort_key INTEGER,text TEXT NOT NULL,indexed_bytes INTEGER CHECK(indexed_bytes BETWEEN 0 AND 65536),truncated INTEGER,PRIMARY KEY(scope_id,kind,rid),FOREIGN KEY(scope_id) REFERENCES scope_domains(id)) WITHOUT ROWID;
CREATE TABLE scope_search_postings(scope_id TEXT,term TEXT,sort_key INTEGER,rid TEXT,kind TEXT,PRIMARY KEY(scope_id,term,sort_key,rid,kind),FOREIGN KEY(scope_id,kind,rid) REFERENCES scope_search_resources(scope_id,kind,rid)) WITHOUT ROWID;
CREATE INDEX scope_search_postings_resource ON scope_search_postings(scope_id,kind,rid,term);
CREATE TABLE scope_stream_bindings(principal TEXT,scope_id TEXT,audience_key TEXT,generation INTEGER NOT NULL,PRIMARY KEY(principal,scope_id,audience_key)) WITHOUT ROWID;
CREATE TABLE scope_stream_sequences(scope_id TEXT,family TEXT,audience_key TEXT,head INTEGER DEFAULT 0,retention_generation INTEGER DEFAULT 1,compacted_through INTEGER DEFAULT 0,PRIMARY KEY(scope_id,family,audience_key)) WITHOUT ROWID;
CREATE TABLE scope_changes(scope_id TEXT,family TEXT,audience_key TEXT,seq INTEGER,rid TEXT,version TEXT,payload TEXT,payload_bytes INTEGER CHECK(payload_bytes BETWEEN 1 AND 16384),PRIMARY KEY(scope_id,family,audience_key,seq)) WITHOUT ROWID;
`

type scopeCRepository struct{ db *sql.DB }
type scopeCSearchSnapshot struct {
	tx      *sql.Tx
	binding scopesearch.Binding
}
type scopeCStreamSnapshot struct {
	tx      *sql.Tx
	binding scopestream.Binding
}
type scopeCWriter struct {
	tx    scopedrepo.MutationTx
	scope string
}

func (s *scopeCSearchSnapshot) selected(id string) bool {
	for _, b := range s.binding.Scopes {
		if b.Scope == id {
			return true
		}
	}
	return false
}
func (s *scopeCStreamSnapshot) selected(stream scopestream.Stream) bool {
	for _, b := range s.binding.Streams {
		if b.Stream == stream {
			return true
		}
	}
	return false
}

func newScopeCRepository(t *testing.T) *scopeCRepository {
	t.Helper()
	requireIntegrationTest(t)
	ws, err := storage.InitializeWorkspace(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ws.Close() })
	if err = scopedrepo.New(ws.DB()).Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err = ws.DB().Exec(scopeCShadowDDL); err != nil {
		t.Fatal(err)
	}
	return &scopeCRepository{ws.DB()}
}
func (c *scopeCRepository) grant(t *testing.T, scope, principal, state string) {
	t.Helper()
	scopeCExec(t, c.db, `INSERT INTO scope_domains VALUES(?,?,1)`, scope, state)
	scopeCExec(t, c.db, `INSERT INTO scope_memberships VALUES(?,?,'owner',1)`, principal, scope)
	scopeCExec(t, c.db, `INSERT OR IGNORE INTO scope_principal_generations VALUES(?,1)`, principal)
	scopeCExec(t, c.db, `INSERT INTO scope_search_generations VALUES(?,1)`, scope)
}
func scopeCAuthorize(ctx context.Context, tx *sql.Tx, principal, scope string) (generation, member int64, err error) {
	var state, role string
	err = tx.QueryRowContext(ctx, `SELECT d.state,d.generation,m.role,m.generation FROM scope_memberships m JOIN scope_domains d ON d.id=m.scope_id WHERE m.principal=? AND m.scope_id=?`, principal, scope).Scan(&state, &generation, &role, &member)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, 0, scopes.ErrDenied
	}
	if err != nil {
		return
	}
	if !scopes.Role(role).CanRead() {
		err = scopes.ErrDenied
		return
	}
	if state != "active" {
		err = scopes.ErrUpdating
	}
	return
}
func (c *scopeCRepository) ReadSearch(ctx context.Context, principal string, ids []string, fn func(scopesearch.Snapshot) error) error {
	tx, err := c.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	b := scopesearch.Binding{Principal: principal}
	if err = tx.QueryRowContext(ctx, `SELECT generation FROM scope_principal_generations WHERE principal=?`, principal).Scan(&b.AuthorityGeneration); err != nil {
		return scopes.ErrDenied
	}
	for _, id := range ids {
		scopeGeneration, _, err := scopeCAuthorize(ctx, tx, principal, id)
		if err != nil {
			return err
		}
		var generation int64
		if err = tx.QueryRowContext(ctx, `SELECT generation FROM scope_search_generations WHERE scope_id=?`, id).Scan(&generation); err != nil {
			return err
		}
		b.Scopes = append(b.Scopes, scopesearch.ScopeGeneration{Scope: id, Generation: generation, ScopeGeneration: scopeGeneration})
	}
	if err = fn(&scopeCSearchSnapshot{tx, b}); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *scopeCSearchSnapshot) Binding() scopesearch.Binding { return s.binding }
func (s *scopeCSearchSnapshot) Candidates(ctx context.Context, scope, term string, after *scopesearch.Key, limit int) ([]scopesearch.Candidate, error) {
	if !s.selected(scope) {
		return nil, scopes.ErrDenied
	}
	q := `SELECT p.sort_key,p.rid,p.kind,r.version,r.parent,r.indexed_bytes,r.truncated FROM scope_search_postings p JOIN scope_search_resources r ON r.scope_id=p.scope_id AND r.kind=p.kind AND r.rid=p.rid WHERE p.scope_id=? AND p.term=?`
	args := []any{scope, term}
	if after != nil {
		q += ` AND (p.sort_key,p.rid,p.kind,p.scope_id)>(?,?,?,?)`
		args = append(args, -after.Recency, after.RID, after.Kind, after.Scope)
	}
	q += ` AND scope_c_examined(p.rid) ORDER BY p.sort_key,p.rid,p.kind LIMIT ?`
	args = append(args, limit)
	rows, err := s.tx.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []scopesearch.Candidate
	for rows.Next() {
		c := scopesearch.Candidate{Scope: scope}
		c.Key.Scope = scope
		var order int64
		if err = rows.Scan(&order, &c.Key.RID, &c.Key.Kind, &c.Version, &c.Parent, &c.IndexedBytes, &c.Truncated); err != nil {
			return nil, err
		}
		c.Key.Recency = -order
		out = append(out, c)
	}
	return out, rows.Err()
}
func (s *scopeCSearchSnapshot) Texts(ctx context.Context, keys []scopesearch.Candidate) ([]string, error) {
	if len(keys) == 0 {
		return nil, nil
	}
	values := make([]string, len(keys))
	args := make([]any, 0, len(keys)*4)
	for i, k := range keys {
		if !s.selected(k.Scope) {
			return nil, scopes.ErrDenied
		}
		values[i] = "(?,?,?,?)"
		args = append(args, i, k.Scope, k.Key.Kind, k.Key.RID)
	}
	rows, err := s.tx.QueryContext(ctx, `WITH keys(ord,scope_id,kind,rid) AS (VALUES `+strings.Join(values, ",")+`) SELECT k.ord,r.text FROM keys k JOIN scope_search_resources r ON r.scope_id=k.scope_id AND r.kind=k.kind AND r.rid=k.rid`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]string, len(keys))
	seen := make([]bool, len(keys))
	for rows.Next() {
		var text string
		var ordinal int
		if err = rows.Scan(&ordinal, &text); err != nil {
			return nil, err
		}
		if ordinal < 0 || ordinal >= len(keys) || seen[ordinal] {
			return nil, scopesearch.ErrProjection
		}
		out[ordinal] = text
		seen[ordinal] = true
	}
	for _, found := range seen {
		if !found {
			return nil, scopesearch.ErrProjection
		}
	}
	return out, rows.Err()
}
func (w *scopeCWriter) ScopeID() string { return w.scope }
func (w *scopeCWriter) ReplaceSearch(ctx context.Context, c scopesearch.Change) error {
	if c.Scope != w.scope || (!c.Delete && c.Content.ScopeID() != w.scope) {
		return scopes.ErrDerivation
	}
	if _, err := w.tx.ExecContext(ctx, `DELETE FROM scope_search_postings WHERE scope_id=? AND kind=? AND rid=?`, c.Scope, c.Kind, c.RID); err != nil {
		return err
	}
	if _, err := w.tx.ExecContext(ctx, `DELETE FROM scope_search_resources WHERE scope_id=? AND kind=? AND rid=?`, c.Scope, c.Kind, c.RID); err != nil {
		return err
	}
	if !c.Delete {
		if _, err := w.tx.ExecContext(ctx, `INSERT INTO scope_search_resources VALUES(?,?,?,?,?,?,?,?,?)`, c.Scope, c.Kind, c.RID, c.Version, c.Parent, -c.Recency, c.Content.Normalized(), c.Content.IndexedBytes(), c.Content.Truncated()); err != nil {
			return err
		}
		// Bounded VALUES batches fit A's shared 256-call hook budget even
		// when a resource has all 4,096 postings. Production templates and
		// schema constraints remain an A-owned proposal.
		terms := c.Content.Terms()
		for start := 0; start < len(terms); start += 128 {
			end := min(start+128, len(terms))
			values := make([]string, 0, end-start)
			args := make([]any, 0, 5*(end-start))
			for _, term := range terms[start:end] {
				values = append(values, "(?,?,?,?,?)")
				args = append(args, c.Scope, term, -c.Recency, c.RID, c.Kind)
			}
			if _, err := w.tx.ExecContext(ctx, `INSERT INTO scope_search_postings VALUES `+strings.Join(values, ","), args...); err != nil {
				return err
			}
		}
	}
	_, err := w.tx.ExecContext(ctx, `UPDATE scope_search_generations SET generation=generation+1 WHERE scope_id=?`, c.Scope)
	return err
}
func (c *scopeCRepository) searchWrite(t *testing.T, scope, kind, rid, text string, recency int64) {
	t.Helper()
	tx, err := c.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	err = scopesearch.Apply(context.Background(), &scopeCWriter{tx, scope}, scopesearch.Change{Scope: scope, Kind: kind, RID: rid, Version: "v1", Recency: recency, Content: scopesearch.Prepare(scope, text)})
	if err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
}
func (c *scopeCRepository) ReadStreams(ctx context.Context, principal string, streams []scopestream.Stream, fn func(scopestream.Snapshot) error) error {
	tx, err := c.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	b := scopestream.Binding{Principal: principal}
	if err = tx.QueryRowContext(ctx, `SELECT generation FROM scope_principal_generations WHERE principal=?`, principal).Scan(&b.AuthorityGeneration); err != nil {
		return scopes.ErrDenied
	}
	for _, s := range streams {
		if s.Family != "inbox" && s.Family != "events" && s.Family != "receipts" && s.Family != "work" {
			return scopes.ErrDenied
		}
		generation, member, err := scopeCAuthorize(ctx, tx, principal, string(s.Scope))
		if err != nil {
			return err
		}
		audience := int64(1)
		if s.Audience != "all" && s.Audience != "person:"+principal {
			if err = tx.QueryRowContext(ctx, `SELECT generation FROM scope_stream_bindings WHERE principal=? AND scope_id=? AND audience_key=?`, principal, s.Scope, s.Audience).Scan(&audience); err != nil {
				return scopes.ErrDenied
			}
		}
		g := scopestream.StreamGeneration{Stream: s, ScopeGeneration: generation, BindingGeneration: member, AudienceGeneration: audience, RetentionGeneration: 1}
		err = tx.QueryRowContext(ctx, `SELECT retention_generation,compacted_through FROM scope_stream_sequences WHERE scope_id=? AND family=? AND audience_key=?`, s.Scope, s.Family, s.Audience).Scan(&g.RetentionGeneration, &g.AfterCompacted)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		b.Streams = append(b.Streams, g)
	}
	if err = fn(&scopeCStreamSnapshot{tx, b}); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *scopeCStreamSnapshot) Binding() scopestream.Binding { return s.binding }
func (s *scopeCStreamSnapshot) Heads(ctx context.Context, stream scopestream.Stream, after int64, limit int) ([]scopestream.Head, error) {
	if !s.selected(stream) {
		return nil, scopes.ErrDenied
	}
	rows, err := s.tx.QueryContext(ctx, `SELECT seq,rid,version,payload_bytes FROM scope_changes WHERE scope_id=? AND family=? AND audience_key=? AND seq>? AND scope_c_examined(seq) ORDER BY seq LIMIT ?`, stream.Scope, stream.Family, stream.Audience, after, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []scopestream.Head
	for rows.Next() {
		var head scopestream.Head
		if err = rows.Scan(&head.Sequence, &head.RID, &head.Version, &head.PayloadBytes); err != nil {
			return nil, err
		}
		out = append(out, head)
	}
	return out, rows.Err()
}
func (s *scopeCStreamSnapshot) Records(ctx context.Context, keys []scopestream.Key) ([]scopestream.Record, error) {
	if len(keys) == 0 {
		return nil, nil
	}
	values := make([]string, len(keys))
	args := make([]any, 0, len(keys)*5)
	for i, k := range keys {
		if !s.selected(k.Stream) {
			return nil, scopes.ErrDenied
		}
		values[i] = "(?,?,?,?,?)"
		args = append(args, i, k.Stream.Scope, k.Stream.Family, k.Stream.Audience, k.Head.Sequence)
	}
	rows, err := s.tx.QueryContext(ctx, `WITH keys(ord,scope_id,family,audience_key,seq) AS (VALUES `+strings.Join(values, ",")+`) SELECT k.ord,c.rid,c.version,c.payload,c.payload_bytes FROM keys k JOIN scope_changes c ON c.scope_id=k.scope_id AND c.family=k.family AND c.audience_key=k.audience_key AND c.seq=k.seq`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]scopestream.Record, len(keys))
	seen := make([]bool, len(keys))
	for rows.Next() {
		var ordinal, size int
		var rid, version, payload string
		if err = rows.Scan(&ordinal, &rid, &version, &payload, &size); err != nil {
			return nil, err
		}
		if ordinal < 0 || ordinal >= len(keys) || seen[ordinal] || rid != keys[ordinal].Head.RID || version != keys[ordinal].Head.Version || size != keys[ordinal].Head.PayloadBytes {
			return nil, scopestream.ErrProjection
		}
		out[ordinal] = scopestream.Record{Key: keys[ordinal], Payload: json.RawMessage(payload)}
		seen[ordinal] = true
	}
	for _, found := range seen {
		if !found {
			return nil, scopestream.ErrProjection
		}
	}
	return out, rows.Err()
}
func (w *scopeCWriter) AppendChanges(ctx context.Context, changes []scopestream.Change) error {
	for _, c := range changes {
		if string(c.Stream.Scope) != w.scope || c.Payload.ScopeID() != w.scope {
			return scopes.ErrDerivation
		}
	}
	for _, c := range changes {
		var seq int64
		rows, err := w.tx.QueryContext(ctx, `INSERT INTO scope_stream_sequences(scope_id,family,audience_key,head) VALUES(?,?,?,1) ON CONFLICT(scope_id,family,audience_key) DO UPDATE SET head=head+1 RETURNING head`, c.Stream.Scope, c.Stream.Family, c.Stream.Audience)
		if err != nil {
			return err
		}
		if !rows.Next() {
			err = rows.Err()
			rows.Close()
			if err != nil {
				return err
			}
			return scopestream.ErrProjection
		}
		err = rows.Scan(&seq)
		closeErr := rows.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
		payload := c.Payload.Bytes()
		if _, err := w.tx.ExecContext(ctx, `INSERT INTO scope_changes VALUES(?,?,?,?,?,?,?,?)`, c.Stream.Scope, c.Stream.Family, c.Stream.Audience, seq, c.RID, c.Version, string(payload), len(payload)); err != nil {
			return err
		}
	}
	return nil
}
func (c *scopeCRepository) streamWrite(t *testing.T, s scopestream.Stream, rid, text string) {
	t.Helper()
	payload, err := json.Marshal(map[string]string{"text": text})
	if err != nil {
		t.Fatal(err)
	}
	pinned, err := scopestream.PreparePayload(string(s.Scope), payload)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := c.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	err = scopestream.Apply(context.Background(), &scopeCWriter{tx, string(s.Scope)}, []scopestream.Change{{Stream: s, RID: rid, Version: "v1", Payload: pinned}})
	if err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

func scopeCCount(t *testing.T, db *sql.DB, query string, args ...any) int {
	t.Helper()
	var n int
	if err := db.QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}
func scopeCExec(t *testing.T, db *sql.DB, q string, args ...any) {
	t.Helper()
	if _, err := db.Exec(q, args...); err != nil {
		t.Fatal(err)
	}
}
func scopeCPlan(t *testing.T, db *sql.DB, q string, args ...any) string {
	t.Helper()
	rows, err := db.Query("EXPLAIN QUERY PLAN "+q, args...)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id, parent, unused int
		var detail string
		if err = rows.Scan(&id, &parent, &unused, &detail); err != nil {
			t.Fatal(err)
		}
		out = append(out, detail)
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	plan := strings.Join(out, "\n")
	if strings.Contains(plan, "SCAN scope_changes") || strings.Contains(plan, "SCAN scope_search_postings") || strings.Contains(plan, "AUTOMATIC") || strings.Contains(plan, "TEMP B-TREE") {
		t.Fatalf("unbounded read plan: %s", plan)
	}
	return plan
}

func TestScopeSearchAndStreamIndexPlans(t *testing.T) {
	c := newScopeCRepository(t)
	t.Log(scopeCPlan(t, c.db, `SELECT rid FROM scope_changes WHERE scope_id=? AND family=? AND audience_key=? AND seq>? ORDER BY seq LIMIT ?`, "s", "inbox", "person:p", 0, 2))
	t.Log(scopeCPlan(t, c.db, `SELECT rid,kind FROM scope_search_postings WHERE scope_id=? AND term=? AND (sort_key,rid,kind,scope_id)>(?,?,?,?) ORDER BY sort_key,rid,kind LIMIT ?`, "s", "needle", -1, "r", "document", "s", 4))
	t.Log(scopeCPlan(t, c.db, `SELECT term FROM scope_search_postings WHERE scope_id=? AND kind=? AND rid=?`, "s", "comment", "c"))
	t.Log(scopeCPlan(t, c.db, `SELECT p.sort_key,p.rid,p.kind,r.version,r.parent,r.indexed_bytes,r.truncated FROM scope_search_postings p JOIN scope_search_resources r ON r.scope_id=p.scope_id AND r.kind=p.kind AND r.rid=p.rid WHERE p.scope_id=? AND p.term=? AND (p.sort_key,p.rid,p.kind,p.scope_id)>(?,?,?,?) ORDER BY p.sort_key,p.rid,p.kind LIMIT ?`, "s", "needle", -1, "r", "document", "s", 4))
	t.Log(scopeCPlan(t, c.db, `WITH keys(ord,scope_id,kind,rid) AS (VALUES (?,?,?,?)) SELECT k.ord,r.text FROM keys k JOIN scope_search_resources r ON r.scope_id=k.scope_id AND r.kind=k.kind AND r.rid=k.rid`, 0, "s", "document", "r"))
	t.Log(scopeCPlan(t, c.db, `WITH keys(ord,scope_id,family,audience_key,seq) AS (VALUES (?,?,?,?,?)) SELECT k.ord,c.rid,c.version,c.payload,c.payload_bytes FROM keys k JOIN scope_changes c ON c.scope_id=k.scope_id AND c.family=k.family AND c.audience_key=k.audience_key AND c.seq=k.seq`, 0, "s", "inbox", "all", 1))
}

func TestScopeSearchAndStreamSnapshotRejectsAnotherScopeEvenForOwner(t *testing.T) {
	c := newScopeCRepository(t)
	c.grant(t, "public", "owner", "active")
	c.grant(t, "private", "owner", "active")
	c.searchWrite(t, "private", "document", "secret", "private sentinel", 1)
	privateStream := scopestream.Stream{Scope: "private", Family: "events", Audience: "all"}
	c.streamWrite(t, privateStream, "secret", "private sentinel")
	scopeCExamined.Store(0)
	err := c.ReadSearch(context.Background(), "owner", []string{"public"}, func(s scopesearch.Snapshot) error {
		if _, err := s.Candidates(context.Background(), "private", "private", nil, 1); !errors.Is(err, scopes.ErrDenied) {
			t.Fatalf("cross-scope candidate query: %v", err)
		}
		if _, err := s.Texts(context.Background(), []scopesearch.Candidate{{Scope: "private", Key: scopesearch.Key{Kind: "document", RID: "secret"}}}); !errors.Is(err, scopes.ErrDenied) {
			t.Fatalf("cross-scope body hydration: %v", err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	err = c.ReadStreams(context.Background(), "owner", []scopestream.Stream{{Scope: "public", Family: "events", Audience: "all"}}, func(s scopestream.Snapshot) error {
		if _, err := s.Heads(context.Background(), privateStream, 0, 1); !errors.Is(err, scopes.ErrDenied) {
			t.Fatalf("cross-scope stream query: %v", err)
		}
		if _, err := s.Records(context.Background(), []scopestream.Key{{Stream: privateStream, Head: scopestream.Head{Sequence: 1}}}); !errors.Is(err, scopes.ErrDenied) {
			t.Fatalf("cross-scope payload hydration: %v", err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if scopeCExamined.Load() != 0 {
		t.Fatal("denied scope was examined before binding check")
	}
}

var _ scopesearch.Repository = (*scopeCRepository)(nil)
var _ scopestream.Repository = (*scopeCRepository)(nil)
var _ scopesearch.Writer = (*scopeCWriter)(nil)
var _ scopestream.Writer = (*scopeCWriter)(nil)
