// Work read paths: ListWork with its bounded access-filter passes, card
// lookup by identifier, and live/report work snapshots.

package primitives

import (
	"agent-nexus-core/internal/handles"
	"agent-nexus-core/internal/schema"
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"time"
)

// WorkListFilter filters the card-backed commitment projection. Cursor is opaque.
type WorkListFilter struct {
	ProjectRef, Source, Owner, Phase, Freshness, Query, Cursor string
	Limit                                                      int
	Visible                                                    func(string, string) bool `json:"-"`
}
type WorkPage struct {
	Work          []map[string]any `json:"work"`
	NextCursor    string           `json:"next_cursor"`
	LifecyclePage bool             `json:"-"`
	ArchivedRefs  []string         `json:"archived_refs"`
}

const maxWorkAccessFilterPasses = 20

func (s *Store) ListWork(ctx context.Context, f WorkListFilter) (WorkPage, error) {
	page := WorkPage{Work: []map[string]any{}}
	if f.Limit == 0 {
		f.Limit = 50
	}
	if f.Limit < 1 || f.Limit > 200 {
		return page, workInvalid("limit must be 1..200")
	}
	codec, binding, err := s.workCursorCodec(ctx, f)
	if err != nil {
		return page, err
	}
	before := workContinuation{}
	if f.Cursor != "" {
		if codec.OpenContinuation(f.Cursor, &before) != nil || (before.Version != 1 && before.Version != 2) || before.Binding != binding || before.ExpiresAt <= time.Now().Unix() || (!before.Lifecycle && (before.ID == "" || before.UpdatedAt.IsZero())) {
			return page, ErrInvalidCursor
		}
	}
	hasMore := false
	for pass := 0; !before.Lifecycle && pass < maxWorkAccessFilterPasses; pass++ {
		batchLimit := f.Limit
		if pass > 0 && batchLimit < 50 {
			batchLimit = 50
		}
		beforeUpdated := ""
		if !before.UpdatedAt.IsZero() {
			beforeUpdated = strings.TrimSuffix(before.UpdatedAt.UTC().Format(time.RFC3339Nano), "Z")
		}
		candidates, err := s.ListReportWork(ctx, ReportWorkFilter{
			Limit:         batchLimit,
			IncludeClosed: true,
			WorkList:      &f,
			ProjectRef:    f.ProjectRef,
			BeforeUpdated: beforeUpdated,
			BeforeID:      before.ID,
		})
		if err != nil {
			return page, err
		}
		if len(candidates.Work) == 0 {
			hasMore = false
			break
		}
		rawLast := candidates.Work[len(candidates.Work)-1]
		visible := candidates.Work
		if f.Visible != nil {
			visible, err = s.FilterCardAccess(ctx, visible, f.Visible)
			if err != nil {
				return page, err
			}
		}
		needed := f.Limit - len(page.Work)
		if len(visible) > needed {
			page.Work = append(page.Work, visible[:needed]...)
			cursorCard := page.Work[len(page.Work)-1]
			before.ID = workString(cursorCard["id"])
			before.UpdatedAt, err = workTimestamp(cursorCard["updated_at"])
			if err != nil {
				return page, err
			}
			hasMore = true
			break
		}
		page.Work = append(page.Work, visible...)
		hasMore = candidates.Truncated
		before.ID = workString(rawLast["id"])
		before.UpdatedAt, err = workTimestamp(rawLast["updated_at"])
		if err != nil {
			return page, err
		}
		if len(page.Work) == f.Limit || !candidates.Truncated {
			break
		}
	}
	// Lifecycle removals follow the work page. The same opaque continuation
	// carries both phases, so existing clients collect the full inventory by
	// following next_cursor without an all-workspace read on every page.
	if !hasMore {
		before.Lifecycle = true
		page.LifecyclePage = true
		page.ArchivedRefs, before.LifecycleID, hasMore, err = s.hiddenWorkPage(ctx, before.LifecycleID, f.Limit, f.Visible)
		if err != nil {
			return page, err
		}
	}
	if hasMore {
		before.Version = 2
		before.Binding = binding
		before.ExpiresAt = time.Now().Add(24 * time.Hour).Unix()
		page.NextCursor, err = codec.SealContinuation(before)
		if err != nil {
			return page, err
		}
	}
	return page, nil
}

// getWorkCard resolves the same public typed refs/handles as HTTP, without
// loading unbounded card revision history into portfolio queries.
func (s *Store) getWorkCard(ctx context.Context, identifier string) (map[string]any, error) {
	resolved, err := s.ResolveResourceRef(ctx, ResourceRefInput{Type: "card", Ref: identifier})
	if err != nil {
		return nil, err
	}
	row, err := s.loadBoardCardByGlobalID(ctx, s.db, resolved.ID, true)
	if err != nil {
		return nil, err
	}
	return row.toMap()
}

// WorkDecisionRevision is the proposal and dispatch fence for a projected work
// record. Native fences compose metadata version and card head revision, so
// metadata and content edits both invalidate approvals. Observation and refresh
// bookkeeping do not. Successful external reads supply the source revision.
func WorkDecisionRevision(w map[string]any) string {
	source := workMap(w["source"])
	if authority := workString(source["authority"]); authority != "" && authority != "nexus" {
		if revision, _ := source["revision"].(string); revision != "" {
			return revision
		}
	}
	version, _ := workInt(w["version"])
	head, _ := workInt(w["head_revision_number"])
	return strconv.FormatInt(version, 10) + "." + strconv.FormatInt(head, 10)
}

// LiveWorkSnapshots batches PM projection enrichment, retaining scoped SQL.
func (s *Store) LiveWorkSnapshots(ctx context.Context, refs []string) (map[string]map[string]any, error) {
	return s.decisionWorkSnapshots(ctx, refs)
}

// ReportWorkSnapshots also excludes archived boards and project topics.
func (s *Store) ReportWorkSnapshots(ctx context.Context, refs []string) (map[string]map[string]any, error) {
	return s.workSnapshots(ctx, refs, false)
}

func (s *Store) workSnapshots(ctx context.Context, refs []string, cardLifecycleOnly bool) (map[string]map[string]any, error) {
	out := map[string]map[string]any{}
	if len(refs) == 0 {
		return out, nil
	}
	selectors := []map[string]string{}
	for _, ref := range refs {
		value := strings.TrimSpace(ref)
		if strings.Contains(value, ":") {
			kind, suffix, err := schema.SplitTypedRef(value)
			if err != nil || kind != "card" {
				continue
			}
			value = suffix
		}
		selectors = append(selectors, map[string]string{"ref": ref, "value": value, "handle": handles.Normalize(value)})
	}
	raw, _ := json.Marshal(selectors)
	rows, err := s.db.QueryContext(ctx, `SELECT json_extract(j.value,'$.ref'),c.id FROM json_each(?) j
 LEFT JOIN resource_handle_aliases aliases ON aliases.resource_type='card' AND aliases.alias_handle=json_extract(j.value,'$.handle')
 JOIN cards c ON c.id=COALESCE((SELECT h.id FROM cards h WHERE h.handle=json_extract(j.value,'$.handle') AND h.handle IS NOT NULL AND trim(h.handle)<>'' LIMIT 1),aliases.resource_id,(SELECT canonical.id FROM cards canonical WHERE canonical.id=json_extract(j.value,'$.value'))) WHERE EXISTS(SELECT 1 FROM ref_edges placement WHERE placement.source_type='board' AND placement.target_type='card' AND placement.edge_type='board_card' AND placement.target_id=c.id)`, string(raw))
	if err != nil {
		return nil, err
	}
	byRef := map[string]string{}
	ids := []string{}
	for rows.Next() {
		var ref, id string
		if err = rows.Scan(&ref, &id); err != nil {
			rows.Close()
			return nil, err
		}
		byRef[ref] = id
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	ids = uniqueSortedStrings(ids)
	if len(ids) == 0 {
		return out, nil
	}
	page, err := s.ListReportWork(ctx, ReportWorkFilter{CardIDs: ids, Limit: 200, IncludeClosed: true, CardLifecycleOnly: cardLifecycleOnly})
	if err != nil {
		return nil, err
	}
	byID := map[string]map[string]any{}
	for _, w := range page.Work {
		byID[workString(w["id"])] = w
	}
	for ref, id := range byRef {
		if w, ok := byID[id]; ok {
			out[ref] = w
		}
	}
	return out, nil
}
