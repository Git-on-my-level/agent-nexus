package readmodel_test

import (
	"agent-nexus-core/internal/readmodel"
	"fmt"
	"strings"
	"unicode/utf8"

	"agent-nexus-core/internal/scopes"
)

// Diagnostic-only SQL shapes retained for the pre-integration request-cost and
// overlap counterexamples. The runtime uses scopedrepo's reviewed builders.

func BatchAuthorityProposal(principal string, ids []scopes.ID) (string, []any, error) {
	if !proposalText(principal, 512) || len(ids) < 1 || len(ids) > readmodel.MaxScopes {
		return "", nil, readmodel.ErrBudget
	}
	values := make([]string, len(ids))
	args := make([]any, 0, 2*len(ids)+1)
	seen := map[scopes.ID]bool{}
	for i, id := range ids {
		if !proposalText(string(id), 512) || seen[id] {
			return "", nil, readmodel.ErrProjection
		}
		seen[id] = true
		values[i] = "(?,?)"
		args = append(args, i, id)
	}
	args = append(args, principal)
	return `WITH requested(ord,scope_id) AS (VALUES ` + strings.Join(values, ",") + `)
 SELECT r.ord,d.state,d.generation,m.role,m.generation,
 g.projection_version,g.audience_version,g.lifecycle_version,g.legacy_auth_version,g.legacy_auth_epoch
 FROM requested r LEFT JOIN scope_domains d ON d.id=r.scope_id
 LEFT JOIN scope_memberships m ON m.scope_id=r.scope_id AND m.principal=?
 LEFT JOIN scope_feed_generations g ON g.scope_id=r.scope_id AND g.generation=d.generation
 ORDER BY r.ord`, args, nil
}

type AuthorizedStream struct {
	Stream     Stream
	Generation int64
	After      *Key
}

func validateAuthorizedStreams(streams []AuthorizedStream) error {
	s := make([]Stream, len(streams))
	for i, stream := range streams {
		if !proposalText(string(stream.Stream.Scope), 512) || !proposalText(stream.Stream.Family, 128) || !proposalText(stream.Stream.Audience, 512) || stream.Generation < 1 || stream.After != nil && stream.After.RID < 1 {
			return readmodel.ErrProjection
		}
		s[i] = stream.Stream
	}
	return scopes.ValidateStreams(s)
}

func BatchBindingProposal(principal string, streams []AuthorizedStream) (string, []any, error) {
	if !proposalText(principal, 512) || len(streams) < 1 {
		return "", nil, readmodel.ErrBudget
	}
	if err := validateAuthorizedStreams(streams); err != nil {
		return "", nil, err
	}
	values := make([]string, len(streams))
	args := make([]any, 0, 5*len(streams)+1)
	for i, s := range streams {
		values[i] = "(?,?,?,?,?)"
		args = append(args, i, s.Stream.Scope, s.Generation, s.Stream.Family, s.Stream.Audience)
	}
	args = append(args, principal)
	return `WITH requested(ord,scope_id,generation,family,audience_key) AS (VALUES ` + strings.Join(values, ",") + `)
 SELECT r.ord,b.membership_generation,b.binding_generation
 FROM requested r LEFT JOIN scope_feed_bindings b ON b.scope_id=r.scope_id AND b.generation=r.generation
 AND b.family=r.family AND b.audience_key=r.audience_key AND b.principal=? ORDER BY r.ord`, args, nil
}

// BatchCandidatesProposal returns a global P+1 page after exact-prefix seeks.
// Each branch is capped at P+1 before the bounded merge. SQLite may examine
// S*(P+1) tuples internally, but returns at most P+1; no workspace sort occurs.
// The ordinal uses validated integer formatting, and every identity is bound.
// This is NOT equivalent to Read's per-stream overlap admission guard: far-away
// duplicate identities can escape global lookahead and recur on later pages.
// Adoption therefore requires separately persisted proof that selected streams
// have disjoint resource identities for this generation. It cannot establish
// that proof itself; retain Read's existing guard until A supplies certification.
func BatchCandidatesProposal(streams []AuthorizedStream, size int) (string, []any, error) {
	if size < 1 || size > readmodel.MaxPageSize || len(streams) < 1 {
		return "", nil, readmodel.ErrBudget
	}
	if err := validateAuthorizedStreams(streams); err != nil {
		return "", nil, err
	}
	parts := make([]string, len(streams))
	args := make([]any, 0, 7*len(streams)+1)
	for i, s := range streams {
		query := readmodel.SeekFeedStart
		args = append(args, s.Stream.Scope, s.Generation, s.Stream.Family, s.Stream.Audience)
		if s.After != nil {
			query = readmodel.SeekFeedAfter
			args = append(args, s.After.Sort, s.After.RID)
		}
		args = append(args, size+1)
		parts[i] = fmt.Sprintf("SELECT %d AS stream,sort_key,rid,version FROM (%s)", i, query)
	}
	args = append(args, size+1)
	return "SELECT stream,sort_key,rid,version FROM (" + strings.Join(parts, " UNION ALL ") + ") ORDER BY sort_key,rid,stream LIMIT ?", args, nil
}

// AggregateBucketsProposal groups the existing sparse exact-probe relation so
// counters return <=4 rows. Integer SUM overflow fails, never casts to REAL.
func AggregateBucketsProposal(streams []AuthorizedStream, buckets []string) (string, []any, error) {
	if len(streams) < 1 || len(buckets) < 1 || len(buckets) > readmodel.MaxBuckets {
		return "", nil, readmodel.ErrBudget
	}
	if err := validateAuthorizedStreams(streams); err != nil {
		return "", nil, err
	}
	seen := map[string]bool{}
	for _, b := range buckets {
		if !proposalText(b, 128) || seen[b] {
			return "", nil, readmodel.ErrProjection
		}
		seen[b] = true
	}
	var values []string
	var args []any
	for _, s := range streams {
		for _, b := range buckets {
			values = append(values, "(?,?,?,?,?)")
			args = append(args, s.Stream.Scope, s.Generation, s.Stream.Family, s.Stream.Audience, b)
		}
	}
	return `WITH requested(scope_id,generation,family,audience_key,bucket) AS (VALUES ` + strings.Join(values, ",") + `)
 SELECT r.bucket,SUM(COALESCE(c.value,0)) FROM requested r
 LEFT JOIN scope_counters c ON c.scope_id=r.scope_id AND c.generation=r.generation
 AND c.family=r.family AND c.audience_key=r.audience_key AND c.bucket=r.bucket
 GROUP BY r.bucket`, args, nil
}

type Stream = readmodel.Stream
type Key = readmodel.Key

func proposalText(s string, max int) bool {
	return s != "" && len(s) <= max && utf8.ValidString(s) && !strings.ContainsRune(s, 0)
}
