package primitives

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

type HomeUnreadPage struct {
	Groups      []HomeUnreadGroup
	UnreadCount int
	NextCursor  string
}
type homeGroupCandidates struct{ Topics, Boards, Threads []string }

// Candidate identity/handle and backing-thread seeks use the canonical primary,
// handle and idx_{topics,boards}_thread_id indexes. Never load the group directory
// or read-state history for a whole workspace just to group one event page.
func homeGroupCandidateSQL(refs, threads []string, args *[]any) string {
	a, _ := json.Marshal(uniqueSortedStrings(refs))
	b, _ := json.Marshal(uniqueSortedStrings(threads))
	*args = append(*args, string(a), string(a), string(b))
	return ` AND (id IN (SELECT value FROM json_each(?)) OR handle IN (SELECT value FROM json_each(?)) OR thread_id IN (SELECT value FROM json_each(?)))`
}

// ListHomeUnreadPage inspects at most limit eligible, authorized events, before
// grouping and unread filtering. Counts cover this page. An empty page may carry
// a cursor; callers can advance through already-read or ungrouped history.
func (s *Store) ListHomeUnreadPage(ctx context.Context, readerID, cursor string, limit int) (HomeUnreadPage, error) {
	readerID = strings.TrimSpace(readerID)
	if readerID == "" {
		return HomeUnreadPage{}, fmt.Errorf("reader_id is required")
	}
	page, err := s.ListEventsPage(ctx, EventListFilter{Preset: "home_feed", Cursor: cursor, Limit: limit})
	if err != nil {
		return HomeUnreadPage{}, err
	}
	result := HomeUnreadPage{Groups: []HomeUnreadGroup{}, NextCursor: page.NextCursor}
	if len(page.Events) == 0 {
		return result, nil
	}
	candidates := homeGroupCandidates{}
	for _, event := range page.Events {
		if id := anyStringValue(event["thread_id"]); id != "" {
			candidates.Threads = append(candidates.Threads, id)
		}
		for _, ref := range anyStringSlice(event["refs"]) {
			kind, id, ok := splitTypedRef(ref)
			if !ok {
				continue
			}
			switch kind {
			case "topic":
				candidates.Topics = append(candidates.Topics, id)
			case "board":
				candidates.Boards = append(candidates.Boards, id)
			case "thread":
				candidates.Threads = append(candidates.Threads, id)
			}
		}
	}
	// Lifecycle checks use only this already-authorized event page, never
	// read-time JSON scans or a workspace-wide hidden-subject directory.
	lifecycleRefs := []string{}
	for _, event := range page.Events {
		lifecycleRefs = append(lifecycleRefs, anyStringSlice(event["refs"])...)
		if id := anyStringValue(event["thread_id"]); id != "" {
			lifecycleRefs = append(lifecycleRefs, "thread:"+id)
		}
	}
	hidden, err := s.HiddenSubjectRefsFor(ctx, lifecycleRefs)
	if err != nil {
		return result, err
	}
	lookup, err := s.homeGroupLookup(ctx, candidates)
	if err != nil {
		return result, err
	}
	refs := []string{}
	for ref := range lookup.groupByRef {
		refs = append(refs, ref)
	}
	refJSON, _ := json.Marshal(refs)
	rows, err := s.db.QueryContext(ctx, `SELECT group_ref,last_read_event_ts,last_read_event_id FROM home_read_cursors WHERE reader_id=? AND group_ref IN (SELECT value FROM json_each(?))`, readerID, string(refJSON))
	if err != nil {
		return result, err
	}
	cursors := map[string]homeReadCursor{}
	for rows.Next() {
		var ref, ts, id string
		if err = rows.Scan(&ref, &ts, &id); err != nil {
			rows.Close()
			return result, err
		}
		cursors[ref] = homeReadCursor{TS: ts, ID: id}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return result, err
	}
	groups := map[string]*HomeUnreadGroup{}
	for _, event := range page.Events {
		if refsHidden(anyStringSlice(event["refs"]), hidden) || hidden["thread:"+anyStringValue(event["thread_id"])] {
			continue
		}
		ref := homeGroupForEvent(event, lookup)
		if ref == "" || !homeEventAfterCursor(event, cursors[ref]) {
			continue
		}
		meta := lookup.groupByRef[ref]
		if meta == nil {
			continue
		}
		group := groups[ref]
		if group == nil {
			group = &HomeUnreadGroup{GroupRef: ref, GroupType: meta.groupType, DisplayName: meta.displayName, Priority: meta.priority, NewestEvent: event}
			groups[ref] = group
		}
		group.Events = append(group.Events, event)
		group.UnreadCount++
		result.UnreadCount++
	}
	for _, group := range groups {
		result.Groups = append(result.Groups, *group)
	}
	sort.SliceStable(result.Groups, func(i, j int) bool {
		left, right := result.Groups[i], result.Groups[j]
		if lr, rr := homeImportance(left.Events), homeImportance(right.Events); lr != rr {
			return lr > rr
		}
		lt, rt := anyStringValue(left.NewestEvent["ts"]), anyStringValue(right.NewestEvent["ts"])
		if lt == rt {
			return left.GroupRef < right.GroupRef
		}
		if homeNearTied(lt, rt) {
			if lp, rp := homePriorityRank(left.Priority), homePriorityRank(right.Priority); lp != rp {
				return lp < rp
			}
		}
		return lt > rt
	})
	return result, nil
}
