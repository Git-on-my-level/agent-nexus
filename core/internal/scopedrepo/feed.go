package scopedrepo

import (
	"context"
	"crypto/sha256"
	"database/sql"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"agent-nexus-core/internal/scopes"
)

//go:embed feed_schema.sql
var feedSchemaSQL string

const feedCertificateVersion = 1
const feedMaxItemBytes = 16 * 1024

var ErrFeedProjection = errors.New("invalid feed projection")

type FeedAvailability string

const (
	FeedAvailable FeedAvailability = "available"
	FeedUpdating  FeedAvailability = "scope_updating"
)

type FeedScope struct {
	ID           scopes.ID
	Generation   int64
	Availability FeedAvailability
	Ready        bool
}
type FeedSnapshot struct {
	Binding               string
	Scopes                []FeedScope
	Streams               []scopes.Stream
	MoreScopes            bool
	DirectoryContinuation string
	AsOf                  time.Time
}
type FeedKey struct{ Sort, RID int64 }
type FeedCandidate struct {
	Key     FeedKey
	Version int64
}
type FeedReference struct {
	Stream    int
	Candidate FeedCandidate
}
type FeedItem struct {
	Ref  string          `json:"ref"`
	Data json.RawMessage `json:"data"`
}

// FeedReader is a render-only, transaction-bound capability. The enclosing
// ReadFeed context controls every query. It exposes neither SQL nor a factory.
type FeedReader interface {
	Snapshot() (FeedSnapshot, error)
	Candidates(stream int, after *FeedKey, limit int) ([]FeedCandidate, error)
	Hydrate([]FeedReference) ([]FeedItem, error)
	Buckets([]string) (map[string]int64, error)
}

// InitializeFeedSchema installs only empty, unregistered shadow tables once.
// It intentionally fails for existing tables instead of upgrading or certifying.
func (s *Store) InitializeFeedSchema(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, feedSchemaSQL); err != nil {
		return err
	}
	return tx.Commit()
}

// Fixed reviewed templates: all scope/audience restrictions precede LIMIT.

type feedAuthority struct {
	ID                   scopes.ID
	State                string
	Generation           int64
	Role                 scopes.Role
	MembershipGeneration int64
	Certificates         [4]int64
	LegacyEpoch          int64
}
type feedStreamAuthority struct {
	Stream            scopes.Stream
	Generation        int64
	BindingGeneration int64
}

func feedText(s string, max int) bool {
	return s != "" && len(s) <= max && utf8.ValidString(s) && !strings.ContainsRune(s, 0)
}

// ReadFeed authorizes every scope and every exact audience before the callback.
// Readiness is persisted generation state, never a request-supplied assertion.
func (s *Store) ReadFeed(ctx context.Context, request scopes.RequestSelection, streams []scopes.Stream, fn func(FeedReader) error) error {
	if fn == nil || len(request.ScopeIDs) < 1 || len(request.ScopeIDs) > scopes.MaxScopes || !feedText(request.Principal, 512) {
		return scopes.ErrBudget
	}
	if err := scopes.ValidateStreams(streams); err != nil {
		return err
	}
	ids := append([]scopes.ID(nil), request.ScopeIDs...)
	selected := make(map[scopes.ID]int, len(ids))
	for i, id := range ids {
		if _, exists := selected[id]; exists || !feedText(string(id), 512) {
			return scopes.ErrBudget
		}
		selected[id] = i
	}
	streams = append([]scopes.Stream(nil), streams...)
	for _, stream := range streams {
		if _, ok := selected[stream.Scope]; !ok {
			return scopes.ErrDenied
		}
		if !feedText(stream.Family, 128) || !feedText(stream.Audience, 512) {
			return scopes.ErrBudget
		}
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	authorities := make([]feedAuthority, len(ids))
	// Finish all membership checks before consulting projection/binding state.
	for i, id := range ids {
		a := &authorities[i]
		a.ID = id
		err = tx.QueryRowContext(ctx, query_feed_authority, request.Principal, id).Scan(&a.State, &a.Generation, &a.Role, &a.MembershipGeneration)
		if errors.Is(err, sql.ErrNoRows) {
			return scopes.ErrDenied
		}
		if err != nil {
			return err
		}
		if !a.Role.CanRead() || a.State == "inaccessible" {
			return scopes.ErrDenied
		}
		if a.Generation < 1 || a.MembershipGeneration < 1 || (a.State != "active" && a.State != "transitioning") {
			return ErrFeedProjection
		}
	}
	var legacyEpoch int64
	if err = tx.QueryRowContext(ctx, query_feed_legacy_epoch).Scan(&legacyEpoch); err != nil {
		return err
	}
	if legacyEpoch < 0 {
		return ErrFeedProjection
	}
	r := &feedReader{ctx: ctx, tx: tx, alive: true, ready: true, admitted: make(map[FeedReference]bool), queried: make([]bool, len(streams))}
	r.snapshot = FeedSnapshot{Streams: streams, AsOf: time.Now().UTC()}
	for i := range authorities {
		a := &authorities[i]
		c := &a.Certificates
		err = tx.QueryRowContext(ctx, query_feed_generation, a.ID, a.Generation).Scan(&c[0], &c[1], &c[2], &c[3], &a.LegacyEpoch)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		ready := err == nil && a.State == "active" && a.LegacyEpoch == legacyEpoch
		for _, version := range c {
			ready = ready && version == feedCertificateVersion
		}
		availability := FeedAvailable
		if a.State == "transitioning" {
			availability = FeedUpdating
		}
		r.snapshot.Scopes = append(r.snapshot.Scopes, FeedScope{a.ID, a.Generation, availability, ready})
		r.ready = r.ready && ready
	}
	bindings := make([]feedStreamAuthority, len(streams))
	for i, stream := range streams {
		a := authorities[selected[stream.Scope]]
		var membership, generation int64
		err = tx.QueryRowContext(ctx, query_feed_binding, request.Principal, stream.Scope, a.Generation, stream.Family, stream.Audience).Scan(&membership, &generation)
		if errors.Is(err, sql.ErrNoRows) {
			return scopes.ErrDenied
		}
		if err != nil {
			return err
		}
		if membership != a.MembershipGeneration || generation < 1 {
			return scopes.ErrDenied
		}
		bindings[i] = feedStreamAuthority{stream, a.Generation, generation}
	}
	// Ordered selection is part of the binding because cursor heads use indexes.
	encoded, err := json.Marshal(struct {
		Format      int
		Principal   string
		LegacyEpoch int64
		Scopes      []feedAuthority
		Streams     []feedStreamAuthority
	}{feedCertificateVersion, request.Principal, legacyEpoch, authorities, bindings})
	if err != nil {
		return err
	}
	digest := sha256.Sum256(encoded)
	r.snapshot.Binding = hex.EncodeToString(digest[:])
	r.streams = bindings
	defer r.close()
	if err = fn(r); err != nil {
		return err
	}
	if err = r.close(); err != nil {
		return err
	}
	return tx.Commit()
}

type feedReader struct {
	mu                sync.Mutex
	ctx               context.Context
	tx                *sql.Tx
	alive, ready      bool
	failure           error
	snapshot          FeedSnapshot
	streams           []feedStreamAuthority
	admitted          map[FeedReference]bool
	queried           []bool
	snapshots         int
	hydrated, counted bool
}

func (r *feedReader) close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.alive = false
	clear(r.admitted)
	return r.failure
}
func (r *feedReader) fail(err error) error {
	if err != nil && r.failure == nil {
		r.failure = err
	}
	return err
}
func (r *feedReader) check(data bool) error {
	if !r.alive {
		return scopes.ErrClosed
	}
	if r.failure != nil {
		return r.failure
	}
	if err := r.ctx.Err(); err != nil {
		return r.fail(err)
	}
	if data && !r.ready {
		return r.fail(scopes.ErrUpdating)
	}
	return nil
}
func (r *feedReader) Snapshot() (FeedSnapshot, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.check(false); err != nil {
		return FeedSnapshot{}, err
	}
	r.snapshots++
	if r.snapshots > 8 {
		return FeedSnapshot{}, r.fail(scopes.ErrBudget)
	}
	out := r.snapshot
	out.Scopes = append([]FeedScope(nil), out.Scopes...)
	out.Streams = append([]scopes.Stream(nil), out.Streams...)
	return out, nil
}
func (r *feedReader) Candidates(stream int, after *FeedKey, limit int) ([]FeedCandidate, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.check(true); err != nil {
		return nil, err
	}
	if stream < 0 || stream >= len(r.streams) || limit < 1 || limit > scopes.MaxPage+1 || r.queried[stream] {
		return nil, r.fail(scopes.ErrBudget)
	}
	if after != nil && after.RID < 1 {
		return nil, r.fail(ErrFeedProjection)
	}
	r.queried[stream] = true
	b := r.streams[stream]
	args := []any{b.Stream.Scope, b.Generation, b.Stream.Family, b.Stream.Audience}
	query := query_feed_start
	if after != nil {
		query = query_feed_after
		args = append(args, after.Sort, after.RID)
	}
	args = append(args, limit)
	rows, err := r.tx.QueryContext(r.ctx, query, args...)
	if err != nil {
		return nil, r.fail(err)
	}
	defer rows.Close()
	out := make([]FeedCandidate, 0, limit)
	for rows.Next() {
		var c FeedCandidate
		if err = rows.Scan(&c.Key.Sort, &c.Key.RID, &c.Version); err != nil {
			return nil, r.fail(err)
		}
		if c.Key.RID < 1 || c.Version < 1 || len(out) >= limit {
			return nil, r.fail(ErrFeedProjection)
		}
		out = append(out, c)
		r.admitted[FeedReference{stream, c}] = true
	}
	if err = rows.Err(); err != nil {
		return nil, r.fail(err)
	}
	return out, nil
}

// Hydration is one VALUES batch, with exact feed+payload+identity probes after
// admission. The CASE guards bound SQLite-to-Go bytes even for corrupt rows.

func (r *feedReader) Hydrate(refs []FeedReference) ([]FeedItem, error) {
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
		return []FeedItem{}, nil
	}
	seen := make(map[int64]bool, len(refs))
	values := make([]string, 0, len(refs))
	args := make([]any, 0, 8*len(refs))
	for i, ref := range refs {
		if !r.admitted[ref] || seen[ref.Candidate.Key.RID] {
			return nil, r.fail(ErrFeedProjection)
		}
		seen[ref.Candidate.Key.RID] = true
		b := r.streams[ref.Stream]
		values = append(values, "(?,?,?,?,?,?,?,?)")
		args = append(args, i, b.Stream.Scope, b.Generation, b.Stream.Family, b.Stream.Audience, ref.Candidate.Key.Sort, ref.Candidate.Key.RID, ref.Candidate.Version)
	}
	query := `WITH requested(ord,scope_id,generation,family,audience_key,sort_key,rid,version) AS (VALUES ` + strings.Join(values, ",") + query_feed_hydrate_suffix
	rows, err := r.tx.QueryContext(r.ctx, query, args...)
	if err != nil {
		return nil, r.fail(err)
	}
	defer rows.Close()
	out := make([]FeedItem, len(refs))
	returned := make([]bool, len(refs))
	n := 0
	for rows.Next() {
		var ordinal int
		var kind, id, data sql.NullString
		var version sql.NullInt64
		if err = rows.Scan(&ordinal, &kind, &id, &data, &version); err != nil {
			return nil, r.fail(err)
		}
		if ordinal < 0 || ordinal >= len(refs) || returned[ordinal] || !kind.Valid || !id.Valid || !data.Valid || !version.Valid || version.Int64 != refs[ordinal].Candidate.Version || !feedText(kind.String, 32) || !feedText(id.String, 479) || len(data.String) > feedMaxItemBytes || !json.Valid([]byte(data.String)) {
			return nil, r.fail(ErrFeedProjection)
		}
		returned[ordinal] = true
		n++
		out[ordinal] = FeedItem{kind.String + ":" + id.String, json.RawMessage(data.String)}
	}
	if err = rows.Err(); err != nil {
		return nil, r.fail(err)
	}
	if n != len(refs) {
		return nil, r.fail(ErrFeedProjection)
	}
	return out, nil
}

func (r *feedReader) Buckets(buckets []string) (map[string]int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.check(true); err != nil {
		return nil, err
	}
	if r.counted || len(buckets) < 1 || len(buckets) > 4 {
		return nil, r.fail(scopes.ErrBudget)
	}
	r.counted = true
	out := make(map[string]int64, len(buckets))
	for _, bucket := range buckets {
		if _, duplicate := out[bucket]; duplicate || !feedText(bucket, 128) {
			return nil, r.fail(scopes.ErrBudget)
		}
		out[bucket] = 0
	}
	if len(r.streams) == 0 {
		return out, nil
	}
	values := make([]string, 0, len(r.streams)*len(buckets))
	args := make([]any, 0, 5*len(r.streams)*len(buckets))
	for _, b := range r.streams {
		for _, bucket := range buckets {
			values = append(values, "(?,?,?,?,?)")
			args = append(args, b.Stream.Scope, b.Generation, b.Stream.Family, b.Stream.Audience, bucket)
		}
	}
	query := `WITH requested(scope_id,generation,family,audience_key,bucket) AS (VALUES ` + strings.Join(values, ",") + query_feed_buckets_suffix
	rows, err := r.tx.QueryContext(r.ctx, query, args...)
	if err != nil {
		return nil, r.fail(err)
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		var bucket string
		var value sql.NullInt64
		if err = rows.Scan(&bucket, &value); err != nil {
			return nil, r.fail(err)
		}
		n++
		current, ok := out[bucket]
		if !ok || n > len(values) || value.Int64 < 0 || value.Int64 > math.MaxInt64-current {
			return nil, r.fail(ErrFeedProjection)
		}
		out[bucket] = current + value.Int64
	}
	if err = rows.Err(); err != nil {
		return nil, r.fail(err)
	}
	if n != len(values) {
		return nil, r.fail(ErrFeedProjection)
	}
	return out, nil
}
