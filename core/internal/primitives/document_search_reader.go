package primitives

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"

	"agent-nexus-core/internal/resourceaccess"
	"agent-nexus-core/internal/scopesearch"
)

var ErrSearchRepairPending = errors.New("search aggregate repair pending")

type DocumentSearchCoverage struct {
	Mode  string `json:"mode"`
	Bytes int    `json:"bytes_per_resource,omitempty"`
	Terms int    `json:"terms_per_resource,omitempty"`
}

func independentDocumentSearchEnabled() bool {
	return !strings.EqualFold(strings.TrimSpace(os.Getenv("ANX_INDEPENDENT_DOCUMENT_SEARCH")), "false")
}

type aggregateSearchCursor struct {
	Binding string
	Offset  string
}

// Both readers share one immutable authority/content snapshot. The operational
// fallback repairs at most one document before admission and refuses to serve
// if any dirty aggregate remains in the admitted snapshot.
func (s *Store) SearchDocuments(ctx context.Context, filter DocumentSearchFilter) ([]map[string]any, string, error) {
	if s == nil || s.db == nil {
		return nil, "", fmt.Errorf("store unavailable")
	}
	filter.Query = strings.TrimSpace(filter.Query)
	if filter.Query == "" || len(filter.Query) > 1024 {
		return nil, "", invalidDocumentRequest("q must contain 1 to 1024 bytes")
	}
	filter.States = NormalizeListLifecycleStates(filter.States)
	var ready bool
	if err := s.db.QueryRowContext(ctx, `SELECT c.normalization_version=2 AND d.normalization_version=2 AND c.done AND d.done AND NOT EXISTS(SELECT 1 FROM document_search_pending_heads LIMIT 1) AND NOT EXISTS(SELECT 1 FROM document_search_links_pending LIMIT 1) FROM comment_search_job c,document_search_state d WHERE c.singleton=1 AND d.singleton=1`).Scan(&ready); err != nil {
		return nil, "", err
	}
	independent := independentDocumentSearchEnabled() && ready
	if !independent {
		if _, err := s.repairAggregateSearchOne(ctx); err != nil {
			return nil, "", err
		}
	}
	// Reuse the existing epoch-admitted authority snapshot. In this immutable
	// read transaction a point denial lookup has exactly the same authority as
	// the canonical closure admitted above; no policy is copied into postings.
	ctx, close, err := s.beginDocumentSearchRead(ctx)
	if err != nil {
		return nil, "", err
	}
	defer close()
	var generation, authority int64
	var key []byte
	var dirty bool
	if err = s.db.QueryRowContext(ctx, `SELECT generation,cursor_key,(SELECT version FROM resource_access_epoch WHERE singleton=1),EXISTS(SELECT 1 FROM document_search_dirty LIMIT 1),
 (SELECT done AND normalization_version=2 FROM comment_search_job WHERE singleton=1) AND done AND normalization_version=2 AND NOT EXISTS(SELECT 1 FROM document_search_pending_heads LIMIT 1) AND NOT EXISTS(SELECT 1 FROM document_search_links_pending LIMIT 1)
 FROM document_search_state WHERE singleton=1`).Scan(&generation, &key, &authority, &dirty, &ready); err != nil {
		return nil, "", err
	}
	if independent && !ready {
		return nil, "", ErrSearchRepairPending
	}
	if !independent && dirty {
		return nil, "", ErrSearchRepairPending
	}
	codec, err := scopesearch.NewTokens(key)
	if err != nil {
		return nil, "", err
	}
	scope, scoped := accessScopeFrom(ctx)
	identity, _ := json.Marshal(struct {
		Scope       AccessScope
		Scoped      bool
		Tag, Host   string
		Knowledge   bool
		States      []string
		Independent bool
	}{scope, scoped, filter.Tag, filter.Host, filter.Knowledge, filter.States, independent})
	digest := sha256.Sum256(identity)
	principal := hex.EncodeToString(digest[:])
	if filter.Coverage != nil {
		*filter.Coverage = DocumentSearchCoverage{Mode: "aggregate"}
		if independent {
			*filter.Coverage = DocumentSearchCoverage{"independent", scopesearch.MaxTextBytes, scopesearch.MaxTerms}
		}
	}
	if !independent {
		bound := sha256.Sum256([]byte(fmt.Sprintf("%s:%s:%d:%d", principal, filter.Query, generation, authority)))
		binding := hex.EncodeToString(bound[:])
		var cursor aggregateSearchCursor
		if filter.Cursor != "" {
			if codec.OpenContinuation(filter.Cursor, &cursor) != nil || cursor.Binding != binding {
				return nil, "", ErrInvalidCursor
			}
			filter.Cursor = cursor.Offset
		}
		docs, next, err := s.searchAggregateDocuments(ctx, filter)
		if err != nil || next == "" {
			return docs, next, err
		}
		next, err = codec.SealContinuation(aggregateSearchCursor{binding, next})
		return docs, next, err
	}
	limit := documentSearchDefaultLimit
	if filter.Limit != nil && *filter.Limit > 0 {
		limit = min(*filter.Limit, scopesearch.MaxPageSize)
	}
	projection := scopesearch.Prepare("", filter.Query)
	if len(projection.Terms()) == 0 || len(projection.Terms()) > scopesearch.MaxQueryTerms {
		return nil, "", invalidDocumentRequest("q must contain 1 to 8 terms")
	}
	// A single punctuation-separated input token is a phrase, matching the
	// released parser's quoted-token behavior. Spaces between tokens mean AND.
	phrase := len(strings.Fields(filter.Query)) == 1 && len(projection.Terms()) > 1
	repo := documentSearchRepository{s: s, ctx: ctx, filter: filter, binding: scopesearch.Binding{Principal: principal, AuthorityGeneration: authority, Scopes: []scopesearch.ScopeGeneration{{Scope: "documents", Generation: generation, ScopeGeneration: 1}}}}
	page, err := scopesearch.Search(ctx, repo, codec, scopesearch.Request{Principal: principal, Scopes: []string{"documents"}, Query: filter.Query, Phrase: phrase, Limit: limit, Continuation: filter.Cursor, DeduplicateParents: true, ParentAND: !phrase && len(projection.Terms()) > 1})
	if errors.Is(err, scopesearch.ErrRestart) || errors.Is(err, scopesearch.ErrCursor) {
		return nil, "", ErrInvalidCursor
	}
	if errors.Is(err, scopesearch.ErrBudget) {
		return nil, "", invalidDocumentRequest(err.Error())
	}
	if err != nil {
		return nil, "", err
	}
	docs, err := s.documentSearchResults(ctx, page.Items)
	return docs, page.Continuation, err
}

func (s *Store) beginDocumentSearchRead(ctx context.Context) (context.Context, func(), error) {
	if scope, ok := accessScopeFrom(ctx); ok {
		if _, request := ctx.Value(denialRequestKey{}).(*denialRequestState); !request {
			ctx = WithRequestAccessScope(ctx, scope)
		}
		next, close, err := s.BeginResourceRead(ctx)
		if err != nil {
			return next, close, err
		}
		if _, pinned := next.Value(pinnedDenialKey{}).(pinnedDenial); pinned {
			return next, close, nil
		}
		close()
	}
	// Trusted callers also need content, cursor and result hydration to share
	// one SQLite snapshot. Fallback admission still retains the scoped policy.
	return s.db.PinReads(ctx, func(c context.Context, _ resourceaccess.QueryRower) (context.Context, error) { return c, nil })
}

type documentSearchRepository struct {
	s       *Store
	ctx     context.Context
	filter  DocumentSearchFilter
	binding scopesearch.Binding
}

func (r documentSearchRepository) ReadSearch(_ context.Context, principal string, selection []string, fn func(scopesearch.Snapshot) error) error {
	if principal != r.binding.Principal || len(selection) != 1 || selection[0] != "documents" {
		return scopesearch.ErrUnavailable
	}
	return fn(r)
}
func (r documentSearchRepository) Binding() scopesearch.Binding { return r.binding }

func (r documentSearchRepository) joins() (string, []any) {
	conditions := []string{LifecycleStatesOrGroup("d.archived_at", "d.trashed_at", r.filter.States)}
	args := []any{}
	c, a := documentTagFilterSQL("d.tags_json", r.filter.Tag, r.filter.Knowledge)
	conditions = append(conditions, c...)
	args = append(args, a...)
	if c, a := documentHostFilterSQL("d.hosts_json", r.filter.Host); c != "" {
		conditions = append(conditions, c)
		args = append(args, a...)
	}
	return ` LEFT JOIN document_search_resources r ON r.n=k.res_n
 LEFT JOIN document_search_heads h ON h.n=r.doc_n
 LEFT JOIN documents d ON d.id=h.document_id AND ` + strings.Join(conditions, " AND ") + `
 LEFT JOIN comment_search_heads c ON r.kind='comment' AND c.event_id=r.resource_id`, args
}
func (r documentSearchRepository) Candidates(_ context.Context, scope, term string, after *scopesearch.Key, limit int) ([]scopesearch.Candidate, error) {
	if scope != "documents" {
		return nil, scopesearch.ErrUnavailable
	}
	var docN, resN int64
	if after != nil {
		docN = math.MaxInt64 - after.Recency
		if after.RID == "~" {
			resN = math.MaxInt64
		} else {
			var err error
			resN, err = strconv.ParseInt(after.RID, 10, 64)
			if err != nil {
				return nil, scopesearch.ErrCursor
			}
		}
	}
	joins, args := r.joins()
	args = append([]any{term, docN, resN, limit}, args...)
	rows, err := r.s.db.QueryContext(r.ctx, `WITH k AS MATERIALIZED (SELECT doc_n,res_n FROM document_search_postings WHERE term=? AND (doc_n,res_n)>(?,?) ORDER BY doc_n,res_n LIMIT ?)
 SELECT k.doc_n,k.res_n,COALESCE(r.kind,'document'),COALESCE(d.id,''),
 CASE WHEN d.id IS NULL OR r.n IS NULL THEN 0 WHEN r.kind='document' THEN h.indexed_bytes ELSE COALESCE(c.indexed_bytes,0) END,
 CASE WHEN d.id IS NULL OR r.n IS NULL THEN 0 WHEN r.kind='document' THEN h.truncated ELSE COALESCE(c.truncated,0) END FROM k`+joins+` ORDER BY k.doc_n,k.res_n`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []scopesearch.Candidate{}
	for rows.Next() {
		var dn, rn int64
		c := scopesearch.Candidate{Scope: scope, Version: strconv.FormatInt(r.binding.Scopes[0].Generation, 10), Key: scopesearch.Key{Scope: scope}}
		if err = rows.Scan(&dn, &rn, &c.Key.Kind, &c.Parent, &c.IndexedBytes, &c.Truncated); err != nil {
			return nil, err
		}
		c.Key.Recency = math.MaxInt64 - dn
		c.Key.RID = fmt.Sprintf("%020d", rn)
		out = append(out, c)
	}
	return out, rows.Err()
}
func (r documentSearchRepository) Texts(_ context.Context, keys []scopesearch.Candidate) ([]string, error) {
	if len(keys) == 0 {
		return nil, nil
	}
	values := []string{}
	args := []any{}
	for i, k := range keys {
		n, err := strconv.ParseInt(k.Key.RID, 10, 64)
		if err != nil {
			return nil, err
		}
		values = append(values, "(?,?)")
		args = append(args, i, n)
	}
	joins, filterArgs := r.joins()
	args = append(args, filterArgs...)
	rows, err := r.s.db.QueryContext(r.ctx, `WITH k(ord,res_n) AS (VALUES `+strings.Join(values, ",")+`) SELECT k.ord,
 CASE WHEN d.id IS NULL OR r.n IS NULL THEN '' WHEN r.kind='document' THEN json_extract(h.projection_json,'$.text') ELSE COALESCE(json_extract(c.projection_json,'$.text'),'') END FROM k`+joins, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]string, len(keys))
	seen := 0
	for rows.Next() {
		var i int
		var text string
		if err = rows.Scan(&i, &text); err != nil {
			return nil, err
		}
		if i < 0 || i >= len(out) {
			return nil, scopesearch.ErrProjection
		}
		out[i] = text
		seen++
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if seen != len(out) {
		return nil, scopesearch.ErrProjection
	}
	return out, nil
}

// Each of at most 4P admitted candidate keys probes at most eight terms using
// (term,doc_n,res_n). EXISTS stops at its first authorized resource. Both the
// driver (before LIMIT) and every intersection operand use scoped postings.
func (r documentSearchRepository) ParentMatches(_ context.Context, keys []scopesearch.Candidate, terms []string) ([]bool, error) {
	out := make([]bool, len(keys))
	if len(keys) == 0 {
		return out, nil
	}
	values, args := []string{}, []any{}
	for i, k := range keys {
		values = append(values, "(?,?,?)")
		args = append(args, i, math.MaxInt64-k.Key.Recency, k.Parent)
	}
	checks := []string{"k.parent<>''"}
	for _, term := range terms {
		checks = append(checks, `EXISTS(SELECT 1 FROM document_search_postings p WHERE p.term=? AND p.doc_n=k.doc_n)`)
		args = append(args, term)
	}
	rows, err := r.s.db.QueryContext(r.ctx, `WITH k(ord,doc_n,parent) AS (VALUES `+strings.Join(values, ",")+`) SELECT k.ord,`+strings.Join(checks, " AND ")+` FROM k`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	seen := 0
	for rows.Next() {
		var i int
		var match bool
		if err = rows.Scan(&i, &match); err != nil {
			return nil, err
		}
		if i < 0 || i >= len(out) {
			return nil, scopesearch.ErrProjection
		}
		out[i] = match
		seen++
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if seen != len(out) {
		return nil, scopesearch.ErrProjection
	}
	return out, nil
}

func (s *Store) documentSearchResults(ctx context.Context, hits []scopesearch.Hit) ([]map[string]any, error) {
	out := []map[string]any{}
	if len(hits) == 0 {
		return out, nil
	}
	args := []any{}
	ph := []string{}
	for _, h := range hits {
		args = append(args, h.Parent)
		ph = append(ph, "?")
	}
	rows, err := s.db.QueryContext(ctx, `SELECT d.id,d.handle,d.thread_id,d.title,d.summary,d.source,d.tags_json,d.hosts_json,d.verified_at,d.slug,d.supersedes_json,
 d.refs_json,d.provenance_json,d.head_revision_id,d.head_revision_number,d.created_at,d.created_by,d.updated_at,d.updated_by,d.trashed_at,d.trashed_by,d.trash_reason,d.archived_at,d.archived_by,
 COALESCE(t.count,0),last.preview,last.source_ts,last.actor_id
 FROM documents d LEFT JOIN comment_search_threads t ON t.thread_id=d.thread_id
 LEFT JOIN comment_search_heads last ON last.event_id=(SELECT event_id FROM comment_search_heads WHERE thread_id=d.thread_id ORDER BY source_ts DESC,event_id DESC LIMIT 1)
 WHERE d.id IN (`+strings.Join(ph, ",")+`)`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	byID := map[string]map[string]any{}
	for rows.Next() {
		var row documentRow
		if err = rows.Scan(&row.ID, &row.Handle, &row.ThreadID, &row.Title, &row.Summary, &row.Source, &row.TagsJSON, &row.HostsJSON, &row.VerifiedAt, &row.Slug, &row.SupersedesJSON, &row.RefsJSON, &row.ProvenanceJSON, &row.HeadRevisionID, &row.HeadRevisionNum, &row.CreatedAt, &row.CreatedBy, &row.UpdatedAt, &row.UpdatedBy, &row.TrashedAt, &row.TrashedBy, &row.TrashReason, &row.ArchivedAt, &row.ArchivedBy, &row.ListTimelineMessageCount, &row.ListLastCommentBody, &row.ListLastCommentAt, &row.ListLastCommentBy); err != nil {
			return nil, err
		}
		m, e := row.toMap()
		if e != nil {
			return nil, e
		}
		m["search_rank"] = 1
		byID[row.ID] = m
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	for _, h := range hits {
		if d := byID[h.Parent]; d != nil {
			out = append(out, d)
		}
	}
	return out, nil
}
