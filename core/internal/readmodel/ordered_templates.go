package readmodel

import "strings"

// OrderedHydrationProposal is an unapplied A template. Its caller must retain
// the authorized snapshot/admission capability, validate all joined rows and
// keep any error sticky. RID/canonical IDs are private adapter values, never
// public FeedItem fields. No live repository executes this builder.
func OrderedHydrationProposal(snapshot Snapshot, refs []OrderedReference) (string, []any, error) {
	a, err := validateSnapshot(snapshot)
	if err != nil {
		return "", nil, err
	}
	if a != Available {
		return "", nil, ErrProjection
	}
	if len(refs) < 1 || len(refs) > MaxPageSize {
		return "", nil, ErrBudget
	}
	generations := map[string]int64{}
	for _, s := range snapshot.Scopes {
		generations[string(s.ID)] = s.Generation
	}
	values := make([]string, len(refs))
	args := make([]any, 0, 8*len(refs))
	seen := map[int64]bool{}
	for j, ref := range refs {
		if ref.Stream < 0 || ref.Stream >= len(snapshot.Streams) || !validOrderedKey(ref.Candidate.Key) || ref.Candidate.Version < 1 || seen[ref.Candidate.Key.RID] {
			return "", nil, ErrProjection
		}
		seen[ref.Candidate.Key.RID] = true
		s := snapshot.Streams[ref.Stream]
		if !boundedText(string(s.Scope), 512) || s.Family != "inbox" || !boundedText(s.Audience, 512) {
			return "", nil, ErrProjection
		}
		values[j] = "(?,?,?,?,?,?,?,?)"
		args = append(args, j, s.Scope, generations[string(s.Scope)], s.Family, s.Audience, append([]byte(nil), ref.Candidate.Key.Order...), ref.Candidate.Key.RID, ref.Candidate.Version)
	}
	return `WITH requested(ord,scope_id,generation,family,audience_key,order_key,rid,version) AS(VALUES ` + strings.Join(values, ",") + `)
SELECT q.ord,
 CASE WHEN length(CAST(k.resource_id AS BLOB))<=479 THEN k.resource_id END,
 CASE WHEN length(CAST(r.canonical_id AS BLOB))<=512 THEN r.canonical_id END,
 r.version,CASE WHEN length(CAST(p.data AS BLOB))<=16384 THEN p.data END
FROM requested q
LEFT JOIN scope_inbox_order f ON f.scope_id=q.scope_id AND f.generation=q.generation AND f.family=q.family
 AND f.audience_key=q.audience_key AND f.order_key=q.order_key AND f.rid=q.rid AND f.version=q.version
LEFT JOIN scope_feed_payloads p ON p.scope_id=f.scope_id AND p.generation=f.generation AND p.family=f.family
 AND p.audience_key=f.audience_key AND p.rid=f.rid AND p.version=f.version
LEFT JOIN scope_resource_rids k ON k.scope_id=p.scope_id AND k.rid=p.rid AND k.kind='inbox'
LEFT JOIN scope_resources r ON r.scope_id=k.scope_id AND r.kind=k.kind AND r.id=k.resource_id AND r.version=q.version
ORDER BY q.ord`, args, nil
}
