package scopedrepo

import (
	"bytes"
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"agent-nexus-core/internal/scopes"
)

//go:embed inbox_schema.sql
var inboxOrderSchemaSQL string

const feedMaxOrderKeyBytes = 770

// These private tuples are repository capabilities, never transport objects.
type OrderedFeedKey struct {
	Order []byte `json:"-"`
	RID   int64  `json:"-"`
}
type OrderedFeedCandidate struct {
	Key     OrderedFeedKey
	Version int64
}
type OrderedFeedReference struct {
	Stream    int
	Candidate OrderedFeedCandidate
}
type OrderedFeedItem struct {
	Identity scopes.ResourceIdentity `json:"-"`
	Data     json.RawMessage         `json:"-"`
}

type OrderedBatchFeedReader interface {
	Snapshot() (FeedSnapshot, error)
	Candidates(*OrderedFeedKey, int) ([]OrderedFeedReference, error)
	Hydrate([]OrderedFeedReference) ([]OrderedFeedItem, error)
	Buckets([]string) (map[string]int64, error)
}

type orderedAdmission struct {
	stream       int
	order        string
	rid, version int64
}
type orderedBatchFeedReader struct {
	*batchFeedReader
	orderedRead     bool
	orderedAdmitted map[orderedAdmission]bool
}

func (r *orderedBatchFeedReader) orderedState(err error) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if e := r.check(true); e != nil {
		return e
	}
	return r.fail(err)
}

// InitializeInboxOrderSchema installs an empty disabled comparator table. It
// requires the feed schema and deliberately cannot upgrade an existing table.
func (s *Store) InitializeInboxOrderSchema(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, inboxOrderSchemaSQL); err != nil {
		return err
	}
	return tx.Commit()
}

// ReadOrderedBatchFeed reuses the persisted batch proof admission, with no
// legacy graph. That disabled proof currently lacks complete canonical source
// and directory coverage; it MUST NOT be used to enable production serving.
func (s *Store) ReadOrderedBatchFeed(ctx context.Context, request scopes.RequestSelection, streams []scopes.Stream, fn func(OrderedBatchFeedReader) error) error {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = s.readOrderedBatchFeedTx(ctx, tx, request, streams, fn); err != nil {
		return err
	}
	return tx.Commit()
}

// The closed dispatcher discovers its directory and admits the ordered page
// inside this same transaction, without exposing a transaction to the kernel.
func (s *Store) readOrderedBatchFeedTx(ctx context.Context, tx *sql.Tx, request scopes.RequestSelection, streams []scopes.Stream, fn func(OrderedBatchFeedReader) error) error {
	if fn == nil {
		return scopes.ErrBudget
	}
	for _, stream := range streams {
		if stream.Family != "inbox" {
			return ErrFeedProjection
		}
	}
	return s.readBatchFeedTx(ctx, tx, request, streams, func(base BatchFeedReader) error {
		r := &orderedBatchFeedReader{batchFeedReader: base.(*batchFeedReader), orderedAdmitted: make(map[orderedAdmission]bool)}
		defer clear(r.orderedAdmitted)
		return fn(r)
	})
}

func validOrderedFeedKey(k OrderedFeedKey) bool {
	return len(k.Order) > 0 && len(k.Order) <= feedMaxOrderKeyBytes && k.RID > 0
}
func copyOrderedFeedKey(k OrderedFeedKey) OrderedFeedKey {
	return OrderedFeedKey{append([]byte(nil), k.Order...), k.RID}
}
func orderedFeedBefore(a, b OrderedFeedKey) bool {
	c := bytes.Compare(a.Order, b.Order)
	return c < 0 || c == 0 && a.RID < b.RID
}
func admissionFor(r OrderedFeedReference) orderedAdmission {
	return orderedAdmission{r.Stream, string(r.Candidate.Key.Order), r.Candidate.Key.RID, r.Candidate.Version}
}

const orderedFeedStart = `SELECT order_key,rid,version FROM scope_inbox_order
 WHERE scope_id=? AND generation=? AND family=? AND audience_key=? ORDER BY order_key,rid LIMIT ?`
const orderedFeedAfter = `SELECT order_key,rid,version FROM scope_inbox_order
 WHERE scope_id=? AND generation=? AND family=? AND audience_key=? AND (order_key,rid)>(?,?)
 ORDER BY order_key,rid LIMIT ?`

func orderedBatchCandidateQuery(streams []feedStreamAuthority, after *OrderedFeedKey, size int) (string, []any) {
	parts := make([]string, len(streams))
	args := make([]any, 0, 7*len(streams)+1)
	for i, b := range streams {
		query := orderedFeedStart
		args = append(args, b.Stream.Scope, b.Generation, b.Stream.Family, b.Stream.Audience)
		if after != nil {
			query = orderedFeedAfter
			args = append(args, append([]byte(nil), after.Order...), after.RID)
		}
		args = append(args, size+1)
		parts[i] = fmt.Sprintf("SELECT %d AS stream,order_key,rid,version FROM (%s)", i, query)
	}
	args = append(args, size+1)
	// Branches examine <=S(P+1); only the global P+1 rows cross SQLite-to-Go.
	return "SELECT stream,CASE WHEN typeof(order_key)='blob' AND length(order_key) BETWEEN 1 AND 770 THEN order_key END,rid,version,typeof(rid),typeof(version) FROM (" + strings.Join(parts, " UNION ALL ") + ") ORDER BY order_key,rid,stream LIMIT ?", args
}

func (r *orderedBatchFeedReader) Candidates(after *OrderedFeedKey, size int) ([]OrderedFeedReference, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.check(true); err != nil {
		return nil, err
	}
	if r.orderedRead || size < 1 || size > scopes.MaxPage {
		return nil, r.fail(scopes.ErrBudget)
	}
	if after != nil && !validOrderedFeedKey(*after) {
		return nil, r.fail(ErrFeedProjection)
	}
	r.orderedRead = true
	if len(r.streams) == 0 {
		return []OrderedFeedReference{}, nil
	}
	query, args := orderedBatchCandidateQuery(r.streams, after, size)
	rows, err := r.tx.QueryContext(r.ctx, query, args...)
	if err != nil {
		return nil, r.fail(err)
	}
	defer rows.Close()
	out := make([]OrderedFeedReference, 0, size+1)
	seen := make(map[int64]bool, size+1)
	for rows.Next() {
		var ref OrderedFeedReference
		var ridType, versionType string
		if err = rows.Scan(&ref.Stream, &ref.Candidate.Key.Order, &ref.Candidate.Key.RID, &ref.Candidate.Version, &ridType, &versionType); err != nil {
			return nil, r.fail(err)
		}
		k := ref.Candidate.Key
		if ref.Stream < 0 || ref.Stream >= len(r.streams) || !validOrderedFeedKey(k) || ridType != "integer" || versionType != "integer" || ref.Candidate.Version < 1 || seen[k.RID] || len(out) >= size+1 || after != nil && !orderedFeedBefore(*after, k) || len(out) > 0 && !orderedFeedBefore(out[len(out)-1].Candidate.Key, k) {
			return nil, r.fail(ErrFeedProjection)
		}
		seen[k.RID] = true
		ref.Candidate.Key = copyOrderedFeedKey(k)
		r.orderedAdmitted[admissionFor(ref)] = true
		out = append(out, ref)
	}
	if err = rows.Err(); err != nil {
		return nil, r.fail(err)
	}
	return out, nil
}

const orderedFeedHydrateSuffix = `)
 SELECT q.ord,
 CASE WHEN typeof(k.resource_id)='text' AND length(CAST(k.resource_id AS BLOB)) BETWEEN 1 AND 479 THEN k.resource_id END,
 CASE WHEN typeof(r.canonical_id)='text' AND length(CAST(r.canonical_id AS BLOB)) BETWEEN 1 AND 512 THEN r.canonical_id END,
 CASE WHEN typeof(r.version)='integer' THEN r.version END,
 CASE WHEN typeof(p.data)='text' AND length(CAST(p.data AS BLOB)) BETWEEN 1 AND 16384 THEN p.data END
 FROM requested q
 LEFT JOIN scope_inbox_order f ON f.scope_id=q.scope_id AND f.generation=q.generation AND f.family=q.family
 AND f.audience_key=q.audience_key AND f.order_key=q.order_key AND f.rid=q.rid AND f.version=q.version
 LEFT JOIN scope_feed_payloads p ON p.scope_id=f.scope_id AND p.generation=f.generation AND p.family=f.family
 AND p.audience_key=f.audience_key AND p.rid=f.rid AND p.version=f.version
 LEFT JOIN scope_resource_rids k ON k.scope_id=p.scope_id AND k.rid=p.rid AND k.kind='inbox'
 LEFT JOIN scope_resources r ON r.scope_id=k.scope_id AND r.kind=k.kind AND r.id=k.resource_id AND r.version=q.version
 ORDER BY q.ord`

func (r *orderedBatchFeedReader) Hydrate(refs []OrderedFeedReference) ([]OrderedFeedItem, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.check(true); err != nil {
		return nil, err
	}
	if r.hydrated || len(refs) > scopes.MaxPage {
		return nil, r.fail(scopes.ErrBudget)
	}
	r.hydrated = true
	if len(refs) == 0 {
		return []OrderedFeedItem{}, nil
	}
	seen := make(map[int64]bool, len(refs))
	values := make([]string, len(refs))
	args := make([]any, 0, 8*len(refs))
	for i, ref := range refs {
		if !r.orderedAdmitted[admissionFor(ref)] || seen[ref.Candidate.Key.RID] {
			return nil, r.fail(ErrFeedProjection)
		}
		seen[ref.Candidate.Key.RID] = true
		b := r.streams[ref.Stream]
		values[i] = "(?,?,?,?,?,?,?,?)"
		args = append(args, i, b.Stream.Scope, b.Generation, b.Stream.Family, b.Stream.Audience, append([]byte(nil), ref.Candidate.Key.Order...), ref.Candidate.Key.RID, ref.Candidate.Version)
	}
	query := `WITH requested(ord,scope_id,generation,family,audience_key,order_key,rid,version) AS (VALUES ` + strings.Join(values, ",") + orderedFeedHydrateSuffix
	rows, err := r.tx.QueryContext(r.ctx, query, args...)
	if err != nil {
		return nil, r.fail(err)
	}
	defer rows.Close()
	out := make([]OrderedFeedItem, len(refs))
	n := 0
	for rows.Next() {
		var ordinal int
		var resource, canonical, data *string
		var version *int64
		if err = rows.Scan(&ordinal, &resource, &canonical, &version, &data); err != nil {
			return nil, r.fail(err)
		}
		if ordinal != n || ordinal >= len(refs) || resource == nil || canonical == nil || version == nil || data == nil || *version != refs[ordinal].Candidate.Version || !feedText(*resource, 479) || !feedText(*canonical, 512) || len(*data) > feedMaxItemBytes || !utf8.ValidString(*data) || !json.Valid([]byte(*data)) {
			return nil, r.fail(ErrFeedProjection)
		}
		ref := refs[ordinal]
		out[ordinal] = OrderedFeedItem{scopes.ResourceIdentity{ScopeID: r.streams[ref.Stream].Stream.Scope, Kind: "inbox", ResourceID: *resource, CanonicalID: *canonical, RID: ref.Candidate.Key.RID, CanonicalVersion: *version}, json.RawMessage(*data)}
		n++
	}
	if err = rows.Err(); err != nil {
		return nil, r.fail(err)
	}
	if n != len(refs) {
		return nil, r.fail(ErrFeedProjection)
	}
	return out, nil
}
