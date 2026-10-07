package scopedrepo

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"agent-nexus-core/internal/scopes"
)

// Versions belong to the reviewed implementation, never to the request.
const feedPolicyVersion = 1
const feedProjectorVersion = 1

// BatchFeedReader admits one global P+1 page only after a persisted complete
// selection proof. It has no SQL, proof writer, factory or per-stream escape.
// Cursor decoding/signing and HTTP fallback belong to the future dispatcher;
// it must validate Snapshot.Binding before passing continuation keys here.
type BatchFeedReader interface {
	Snapshot() (FeedSnapshot, error)
	Candidates(after []*FeedKey, size int) ([]FeedReference, error)
	Hydrate([]FeedReference) ([]FeedItem, error)
	Buckets([]string) (map[string]int64, error)
}

type batchFeedReader struct {
	*feedReader
	candidateRead bool
}

// ReadBatchFeed is a disabled bridge boundary, NOT production certification.
// All authorities and exact audience bindings are checked before the proof;
// absent/stale/malformed proof returns ErrUpdating without calling fn. No API
// accepts a caller-authored certificate. No production code can mint one yet.
func (s *Store) ReadBatchFeed(ctx context.Context, request scopes.RequestSelection, streams []scopes.Stream, fn func(BatchFeedReader) error) error {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = s.readBatchFeedTx(ctx, tx, request, streams, fn); err != nil {
		return err
	}
	return tx.Commit()
}

// The inbox dispatcher uses this private entry point so directory discovery,
// authority, proof, candidates and counters consume one SQLite snapshot.
func (s *Store) readBatchFeedTx(ctx context.Context, tx *sql.Tx, request scopes.RequestSelection, streams []scopes.Stream, fn func(BatchFeedReader) error) error {
	if fn == nil || len(request.ScopeIDs) < 1 || len(request.ScopeIDs) > scopes.MaxScopes || !feedText(request.Principal, 512) {
		return scopes.ErrBudget
	}
	if err := scopes.ValidateStreams(streams); err != nil {
		return err
	}
	ids := append([]scopes.ID(nil), request.ScopeIDs...)
	selected := make(map[scopes.ID]int, len(ids))
	for i, id := range ids {
		if _, ok := selected[id]; ok || !feedText(string(id), 512) {
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
	authorities, err := batchAuthorities(ctx, tx, request.Principal, ids)
	if err != nil {
		return err
	}
	var epoch int64
	if err = tx.QueryRowContext(ctx, query_feed_legacy_epoch).Scan(&epoch); err != nil {
		return err
	}
	if epoch < 0 {
		return ErrFeedProjection
	}
	bindings, err := batchBindings(ctx, tx, request.Principal, streams, authorities, selected)
	if err != nil {
		return err
	}
	r := &feedReader{ctx: ctx, tx: tx, alive: true, ready: true, admitted: make(map[FeedReference]bool), queried: make([]bool, len(streams)), streams: bindings}
	defer r.close()
	r.snapshot = FeedSnapshot{Streams: streams, AsOf: time.Now().UTC()}
	for _, a := range authorities {
		ready := a.State == "active" && a.LegacyEpoch == epoch
		for _, v := range a.Certificates {
			ready = ready && v == feedCertificateVersion
		}
		availability := FeedAvailable
		if a.State == "transitioning" {
			availability = FeedUpdating
		}
		r.snapshot.Scopes = append(r.snapshot.Scopes, FeedScope{a.ID, a.Generation, availability, ready})
		r.ready = r.ready && ready
	}
	// Ordered selection, principal, membership/role, all generations and legacy
	// authority are bound independently of source-revision proof invalidation.
	encoded, err := json.Marshal(struct {
		Format      int
		Principal   string
		LegacyEpoch int64
		Scopes      []feedAuthority
		Streams     []feedStreamAuthority
	}{feedCertificateVersion, request.Principal, epoch, authorities, bindings})
	if err != nil {
		return err
	}
	digest := sha256.Sum256(encoded)
	authorityBinding := hex.EncodeToString(digest[:])
	var revision int64
	err = tx.QueryRowContext(ctx, query_feed_batch_proof, authorityBinding, feedPolicyVersion, feedProjectorVersion).Scan(&revision)
	if errors.Is(err, sql.ErrNoRows) {
		return scopes.ErrUpdating
	}
	if err != nil {
		return err
	}
	if !r.ready {
		return scopes.ErrUpdating
	}
	// A cursor from an earlier certified source snapshot must not survive ABA.
	digest = sha256.Sum256([]byte(fmt.Sprintf("%s:%d:%d:%d", authorityBinding, revision, feedPolicyVersion, feedProjectorVersion)))
	r.snapshot.Binding = hex.EncodeToString(digest[:])
	if err = fn(&batchFeedReader{feedReader: r}); err != nil {
		return err
	}
	if err = r.close(); err != nil {
		return err
	}
	return nil
}

func batchAuthorities(ctx context.Context, tx *sql.Tx, principal string, ids []scopes.ID) ([]feedAuthority, error) {
	values := make([]string, len(ids))
	args := make([]any, 0, 2*len(ids)+1)
	for i, id := range ids {
		values[i] = "(?,?)"
		args = append(args, i, id)
	}
	args = append(args, principal)
	rows, err := tx.QueryContext(ctx, `WITH requested(ord,scope_id) AS (VALUES `+strings.Join(values, ",")+query_feed_batch_authority_suffix, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]feedAuthority, len(ids))
	seen := make([]bool, len(ids))
	n := 0
	denied := false
	for rows.Next() {
		var ord int
		var state, role sql.NullString
		var generation, membership, epoch sql.NullInt64
		var certificates [4]sql.NullInt64
		if err = rows.Scan(&ord, &state, &generation, &role, &membership, &certificates[0], &certificates[1], &certificates[2], &certificates[3], &epoch); err != nil {
			return nil, err
		}
		if ord < 0 || ord >= len(ids) || seen[ord] {
			return nil, ErrFeedProjection
		}
		seen[ord] = true
		n++
		if !state.Valid || !role.Valid || !scopes.Role(role.String).CanRead() || state.String == "inaccessible" {
			denied = true
			continue
		}
		if generation.Int64 < 1 || membership.Int64 < 1 || (state.String != "active" && state.String != "transitioning") {
			return nil, ErrFeedProjection
		}
		a := feedAuthority{ID: ids[ord], State: state.String, Generation: generation.Int64, Role: scopes.Role(role.String), MembershipGeneration: membership.Int64, LegacyEpoch: -1}
		if epoch.Valid {
			a.LegacyEpoch = epoch.Int64
		}
		for i, v := range certificates {
			a.Certificates[i] = v.Int64
		}
		out[ord] = a
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if n != len(ids) {
		return nil, ErrFeedProjection
	}
	if denied {
		return nil, scopes.ErrDenied
	}
	return out, nil
}

func batchBindings(ctx context.Context, tx *sql.Tx, principal string, streams []scopes.Stream, authorities []feedAuthority, selected map[scopes.ID]int) ([]feedStreamAuthority, error) {
	out := make([]feedStreamAuthority, len(streams))
	if len(streams) == 0 {
		return out, nil
	}
	values := make([]string, len(streams))
	args := make([]any, 0, 5*len(streams)+1)
	for i, stream := range streams {
		values[i] = "(?,?,?,?,?)"
		args = append(args, i, stream.Scope, authorities[selected[stream.Scope]].Generation, stream.Family, stream.Audience)
	}
	args = append(args, principal)
	rows, err := tx.QueryContext(ctx, `WITH requested(ord,scope_id,generation,family,audience_key) AS (VALUES `+strings.Join(values, ",")+query_feed_batch_binding_suffix, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	seen := make([]bool, len(streams))
	n := 0
	denied := false
	for rows.Next() {
		var ord int
		var membership, generation sql.NullInt64
		if err = rows.Scan(&ord, &membership, &generation); err != nil {
			return nil, err
		}
		if ord < 0 || ord >= len(streams) || seen[ord] {
			return nil, ErrFeedProjection
		}
		seen[ord] = true
		n++
		a := authorities[selected[streams[ord].Scope]]
		if !membership.Valid || membership.Int64 != a.MembershipGeneration || !generation.Valid || generation.Int64 < 1 {
			denied = true
			continue
		}
		out[ord] = feedStreamAuthority{streams[ord], a.Generation, generation.Int64}
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if n != len(streams) {
		return nil, ErrFeedProjection
	}
	if denied {
		return nil, scopes.ErrDenied
	}
	return out, nil
}

// Each branch reads at most P+1 indexed tuples. The merge examines at most
// S*(P+1) tuples (<=25,856), independent of unrelated workspace rows, and returns
// <=P+1. Cross-page disjointness comes from the proof, not this local duplicate
// guard. All stream heads must come from the same authenticated cursor.
func (r *batchFeedReader) Candidates(after []*FeedKey, size int) ([]FeedReference, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.check(true); err != nil {
		return nil, err
	}
	if r.candidateRead || size < 1 || size > scopes.MaxPage || len(after) != len(r.streams) {
		return nil, r.fail(scopes.ErrBudget)
	}
	for _, queried := range r.queried {
		if queried {
			return nil, r.fail(scopes.ErrBudget)
		}
	}
	for _, key := range after {
		if key != nil && key.RID < 1 {
			return nil, r.fail(ErrFeedProjection)
		}
	}
	r.candidateRead = true
	for i := range r.queried {
		r.queried[i] = true
	}
	if len(r.streams) == 0 {
		return []FeedReference{}, nil
	}
	query, args := batchCandidateQuery(r.streams, after, size)
	rows, err := r.tx.QueryContext(r.ctx, query, args...)
	if err != nil {
		return nil, r.fail(err)
	}
	defer rows.Close()
	out := make([]FeedReference, 0, size+1)
	seen := make(map[int64]bool, size+1)
	for rows.Next() {
		var ref FeedReference
		if err = rows.Scan(&ref.Stream, &ref.Candidate.Key.Sort, &ref.Candidate.Key.RID, &ref.Candidate.Version); err != nil {
			return nil, r.fail(err)
		}
		if ref.Stream < 0 || ref.Stream >= len(r.streams) || ref.Candidate.Key.RID < 1 || ref.Candidate.Version < 1 || seen[ref.Candidate.Key.RID] || len(out) >= size+1 {
			return nil, r.fail(ErrFeedProjection)
		}
		seen[ref.Candidate.Key.RID] = true
		out = append(out, ref)
		r.admitted[ref] = true
	}
	if err = rows.Err(); err != nil {
		return nil, r.fail(err)
	}
	return out, nil
}

func batchCandidateQuery(streams []feedStreamAuthority, after []*FeedKey, size int) (string, []any) {
	parts := make([]string, len(streams))
	args := make([]any, 0, 7*len(streams)+1)
	for i, b := range streams {
		query := query_feed_start
		args = append(args, b.Stream.Scope, b.Generation, b.Stream.Family, b.Stream.Audience)
		if after[i] != nil {
			query = query_feed_after
			args = append(args, after[i].Sort, after[i].RID)
		}
		args = append(args, size+1)
		parts[i] = fmt.Sprintf("SELECT %d AS stream,sort_key,rid,version FROM (%s)", i, query)
	}
	args = append(args, size+1)
	return "SELECT stream,sort_key,rid,version FROM (" + strings.Join(parts, " UNION ALL ") + ") ORDER BY sort_key,rid,stream LIMIT ?", args
}

func (r *batchFeedReader) Buckets(buckets []string) (map[string]int64, error) {
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
	for _, b := range buckets {
		if _, ok := out[b]; ok || !feedText(b, 128) {
			return nil, r.fail(scopes.ErrBudget)
		}
		out[b] = 0
	}
	if len(r.streams) == 0 {
		return out, nil
	}
	values := make([]string, 0, len(r.streams)*len(buckets))
	args := make([]any, 0, 5*len(r.streams)*len(buckets))
	for _, s := range r.streams {
		for _, b := range buckets {
			values = append(values, "(?,?,?,?,?)")
			args = append(args, s.Stream.Scope, s.Generation, s.Stream.Family, s.Stream.Audience, b)
		}
	}
	rows, err := r.tx.QueryContext(r.ctx, `WITH requested(scope_id,generation,family,audience_key,bucket) AS (VALUES `+strings.Join(values, ",")+query_feed_batch_buckets_suffix, args...)
	if err != nil {
		return nil, r.fail(errors.Join(ErrFeedProjection, err))
	}
	defer rows.Close()
	seen := make(map[string]bool, len(buckets))
	for rows.Next() {
		var bucket string
		var value, valid int64
		if err = rows.Scan(&bucket, &value, &valid); err != nil {
			return nil, r.fail(ErrFeedProjection)
		}
		if _, ok := out[bucket]; !ok || seen[bucket] || value < 0 || valid != 1 {
			return nil, r.fail(ErrFeedProjection)
		}
		seen[bucket] = true
		out[bucket] = value
	}
	// SQLite integer SUM overflow must fail, never silently promote to REAL.
	if err = rows.Err(); err != nil {
		return nil, r.fail(ErrFeedProjection)
	}
	if len(seen) != len(buckets) {
		return nil, r.fail(ErrFeedProjection)
	}
	return out, nil
}
