package primitives

import (
	"agent-nexus-core/internal/inboxmodel"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"reflect"
	"strings"
	"sync/atomic"
	"time"
)

type inboxQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}
type scopeInboxSample struct {
	scope   AccessScope
	filter  *DerivedInboxListFilter
	options *InboxReadOptions
}
type scopeInboxRuntime struct {
	requests, served, notBuilt, budget, readError, unsupported atomic.Uint64
	shadowCompared, shadowMismatch, shadowError, shadowDropped atomic.Uint64
	samples                                                    chan scopeInboxSample
}

// WithScopedInboxReader enables the default-off, workspace-local serving bridge.
// The index grants no authority: every payload loads through the legacy scoped
// relation and retains the existing lifecycle, audience and enrichment checks.
func WithScopedInboxReader(enabled bool) Option {
	return func(s *Store) {
		s.scopeInbox = nil
		if enabled {
			s.scopeInbox = &scopeInboxRuntime{samples: make(chan scopeInboxSample, 8)}
		}
	}
}

type ScopeInboxDiagnostics struct {
	Requests, Served, Fallbacks, NotBuilt, CandidateBudget, ReadError, Unsupported uint64
	ShadowCompared, ShadowMismatch, ShadowError, ShadowDropped                     uint64
}

func (s *Store) ScopeInboxDiagnostics() ScopeInboxDiagnostics {
	if s.scopeInbox == nil {
		return ScopeInboxDiagnostics{}
	}
	r := s.scopeInbox
	d := ScopeInboxDiagnostics{Requests: r.requests.Load(), Served: r.served.Load(), NotBuilt: r.notBuilt.Load(), CandidateBudget: r.budget.Load(), ReadError: r.readError.Load(), Unsupported: r.unsupported.Load(), ShadowCompared: r.shadowCompared.Load(), ShadowMismatch: r.shadowMismatch.Load(), ShadowError: r.shadowError.Load(), ShadowDropped: r.shadowDropped.Load()}
	d.Fallbacks = d.NotBuilt + d.CandidateBudget + d.ReadError + d.Unsupported
	return d
}

var errScopeInboxNotBuilt = errors.New("scoped inbox not built")
var errScopeInboxBudget = errors.New("scoped inbox candidate budget")
var errScopeInboxUnsupported = errors.New("scoped inbox unsupported selector")

// CROSS JOIN fixes the bounded candidate set as the outer loop. Hydration is
// then canonical primary-key probes, never a workspace ordering-index scan.
func inboxCandidateSource(ids []string, args *[]any) string {
	if ids == nil {
		return "derived_inbox_items i"
	}
	raw, _ := json.Marshal(ids)
	*args = append(*args, string(raw))
	return `(SELECT value AS inbox_candidate_id FROM json_each(?)) _scope_candidates CROSS JOIN derived_inbox_items i ON i.id=_scope_candidates.inbox_candidate_id`
}

// Internal traversal metadata is never returned as a payload or grant. Readiness,
// positions and scoped canonical hydration all share one read transaction.
func scopeInboxCandidates(ctx context.Context, db inboxQueryer, filter DerivedInboxListFilter, budget int) ([]string, bool, error) {
	var done bool
	if err := db.QueryRowContext(ctx, `SELECT done FROM scope_inbox_live_job WHERE singleton=1`).Scan(&done); err != nil {
		return nil, false, err
	}
	if !done {
		return nil, false, errScopeInboxNotBuilt
	}
	where := "1=1"
	args := []any{}
	if strings.TrimSpace(filter.ThreadID) != "" {
		where = "scope_id=?"
		args = append(args, strings.TrimSpace(filter.ThreadID))
	}
	base := `SELECT id,category_rank,trigger_at FROM scope_inbox_live_positions WHERE ` + where
	order := ` ORDER BY category_rank,trigger_at DESC,id ASC`
	query := base + order + ` LIMIT ?`
	if filter.BeforeID != "" {
		spans := []struct {
			q string
			a []any
		}{
			{`category_rank=? AND trigger_at=? AND id>?`, []any{filter.BeforeCategory, filter.BeforeTrigger, filter.BeforeID}},
			{`category_rank=? AND trigger_at<?`, []any{filter.BeforeCategory, filter.BeforeTrigger}},
			{`category_rank>?`, []any{filter.BeforeCategory}},
		}
		parts := []string{}
		bound := []any{}
		for _, span := range spans {
			parts = append(parts, `SELECT * FROM (`+base+` AND `+span.q+order+` LIMIT ?)`)
			bound = append(bound, args...)
			bound = append(bound, span.a...)
			bound = append(bound, budget+1)
		}
		query = `SELECT * FROM (` + strings.Join(parts, ` UNION ALL `) + `)` + order + ` LIMIT ?`
		args = bound
	}
	args = append(args, budget+1)
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id, trigger string
		var rank int
		if err = rows.Scan(&id, &rank, &trigger); err != nil {
			return nil, false, err
		}
		ids = append(ids, id)
	}
	if err = rows.Err(); err != nil {
		return nil, false, err
	}
	more := len(ids) > budget
	if more {
		ids = ids[:budget]
	}
	return ids, more, nil
}
func (s *Store) scopeInboxList(ctx context.Context, db inboxQueryer, filter DerivedInboxListFilter) ([]DerivedInboxItem, error) {
	if filter.Limit < 1 || filter.Limit > 200 {
		return nil, errScopeInboxUnsupported
	}
	budget := 4 * (filter.Limit + 1)
	if budget > inboxmodel.MaxCandidates {
		budget = inboxmodel.MaxCandidates
	}
	ids, more, err := scopeInboxCandidates(ctx, db, filter, budget)
	if err != nil {
		return nil, err
	}
	filter.BeforeID = "" // The bounded index seek already applied the continuation.
	items, err := s.listDerivedInboxItemsLegacy(ctx, filter, db, ids)
	if err != nil {
		return nil, err
	}
	// A dense hidden/inactive prefix must not silently shorten a page or lose
	// its continuation. Retry the complete legacy selector for this request.
	if more && len(items) < filter.Limit+1 {
		return nil, errScopeInboxBudget
	}
	return items, nil
}
func (s *Store) scopeInboxRead(ctx context.Context, db inboxQueryer, options InboxReadOptions) ([]DerivedInboxItem, int, error) {
	// Exact totals require complete bounded enumeration; large inboxes retain
	// the original aggregate rather than publishing a partial count.
	ids, more, err := scopeInboxCandidates(ctx, db, DerivedInboxListFilter{ThreadID: options.ThreadID}, inboxmodel.MaxCandidates)
	if err != nil {
		return nil, 0, err
	}
	if more {
		return nil, 0, errScopeInboxBudget
	}
	return s.readInboxLegacy(ctx, options, db, ids)
}
func (s *Store) recordScopeInbox(err error) {
	r := s.scopeInbox
	switch {
	case err == nil:
		r.served.Add(1)
	case errors.Is(err, errScopeInboxNotBuilt):
		r.notBuilt.Add(1)
	case errors.Is(err, errScopeInboxBudget):
		r.budget.Add(1)
	case errors.Is(err, errScopeInboxUnsupported):
		r.unsupported.Add(1)
	default:
		r.readError.Add(1)
	}
}
func (s *Store) sampleScopeInbox(ctx context.Context, n uint64, filter *DerivedInboxListFilter, options *InboxReadOptions) {
	if n%100 != 0 {
		return
	}
	scope, ok := accessScopeFrom(ctx)
	if !ok {
		return
	}
	// Queue identity/selectors only, never the request context or denial cache.
	sample := scopeInboxSample{scope: scope, filter: filter, options: options}
	select {
	case s.scopeInbox.samples <- sample:
	default:
		s.scopeInbox.shadowDropped.Add(1)
	}
}
func (s *Store) ListDerivedInboxItems(ctx context.Context, filter DerivedInboxListFilter) ([]DerivedInboxItem, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("primitives store database is not initialized")
	}
	if s.scopeInbox == nil {
		return s.listDerivedInboxItemsLegacy(ctx, filter, s.db, nil)
	}
	n := s.scopeInbox.requests.Add(1)
	var items []DerivedInboxItem
	tx, err := s.db.BeginReadSnapshot(ctx, "SELECT id FROM derived_inbox_items")
	if err == nil {
		items, err = s.scopeInboxList(ctx, tx, filter)
		if err == nil {
			err = tx.Commit()
		}
		_ = tx.Rollback()
	}
	s.recordScopeInbox(err)
	copyFilter := filter
	s.sampleScopeInbox(ctx, n, &copyFilter, nil)
	if err != nil {
		return s.listDerivedInboxItemsLegacy(ctx, filter, s.db, nil)
	}
	return items, nil
}
func (s *Store) ReadInbox(ctx context.Context, options InboxReadOptions) ([]DerivedInboxItem, int, error) {
	if s.scopeInbox == nil {
		return s.readInboxLegacy(ctx, options, s.db, nil)
	}
	n := s.scopeInbox.requests.Add(1)
	var items []DerivedInboxItem
	var count int
	tx, err := s.db.BeginReadSnapshot(ctx, "SELECT id FROM derived_inbox_items")
	if err == nil {
		items, count, err = s.scopeInboxRead(ctx, tx, options)
		if err == nil {
			err = tx.Commit()
		}
		_ = tx.Rollback()
	}
	s.recordScopeInbox(err)
	copyOptions := options
	if options.Limit != nil {
		limit := *options.Limit
		copyOptions.Limit = &limit
	}
	s.sampleScopeInbox(ctx, n, nil, &copyOptions)
	if err != nil {
		return s.readInboxLegacy(ctx, options, s.db, nil)
	}
	return items, count, nil
}
func scopeInboxIDs(items []DerivedInboxItem) []string {
	ids := make([]string, len(items))
	for i, item := range items {
		ids[i] = item.ID
	}
	return ids
}

// Compare both selectors under one fresh principal and snapshot, off-request.
// Only counts and reason codes are logged; IDs/payloads/principals stay private.
func (s *Store) compareScopeInbox(ctx context.Context, sample scopeInboxSample) (string, error) {
	ctx = WithRequestAccessScope(ctx, sample.scope)
	tx, err := s.db.BeginReadSnapshot(ctx, "SELECT id FROM derived_inbox_items")
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	var proposed, legacy []DerivedInboxItem
	var proposedCount, legacyCount int
	if sample.filter != nil {
		proposed, err = s.scopeInboxList(ctx, tx, *sample.filter)
		if err == nil {
			legacy, err = s.listDerivedInboxItemsLegacy(ctx, *sample.filter, tx, nil)
		}
		proposedCount = len(proposed)
		legacyCount = len(legacy)
	} else if sample.options != nil {
		proposed, proposedCount, err = s.scopeInboxRead(ctx, tx, *sample.options)
		if err == nil {
			legacy, legacyCount, err = s.readInboxLegacy(ctx, *sample.options, tx, nil)
		}
	} else {
		return "", errScopeInboxUnsupported
	}
	if err != nil {
		return "", err
	}
	reason := ""
	if proposedCount != legacyCount {
		reason = "count_mismatch"
	} else if !reflect.DeepEqual(scopeInboxIDs(proposed), scopeInboxIDs(legacy)) {
		reason = "id_order_mismatch"
	}
	if reason != "" {
		log.Printf("scoped inbox shadow mismatch: reason=%s new_count=%d legacy_count=%d", reason, proposedCount, legacyCount)
	}
	return reason, tx.Commit()
}

// One comparison per second at most, 1/100 reads sampled, queue <=8, deadline
// <=2s. The main server owns cancellation and joins this worker before closing DB.
func (s *Store) RunScopeInboxShadow(ctx context.Context) {
	if s.scopeInbox == nil {
		return
	}
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	reports := time.NewTicker(time.Minute)
	defer reports.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-reports.C:
			log.Printf("scoped inbox diagnostics: %+v", s.ScopeInboxDiagnostics())
		case <-ticker.C:
			select {
			case sample := <-s.scopeInbox.samples:
				slice, cancel := context.WithTimeout(ctx, 2*time.Second)
				reason, err := s.compareScopeInbox(slice, sample)
				cancel()
				if err != nil {
					s.scopeInbox.shadowError.Add(1)
					log.Printf("scoped inbox shadow skipped: reason=%s", scopeInboxErrorReason(err))
				} else {
					s.scopeInbox.shadowCompared.Add(1)
					if reason != "" {
						s.scopeInbox.shadowMismatch.Add(1)
					}
				}
			default:
			}
		}
	}
}
func scopeInboxErrorReason(err error) string {
	switch {
	case errors.Is(err, errScopeInboxNotBuilt):
		return "not_built"
	case errors.Is(err, errScopeInboxBudget):
		return "candidate_budget"
	case errors.Is(err, errScopeInboxUnsupported):
		return "unsupported"
	case errors.Is(err, context.DeadlineExceeded):
		return "deadline"
	case errors.Is(err, context.Canceled):
		return "cancelled"
	default:
		return "read_error"
	}
}
