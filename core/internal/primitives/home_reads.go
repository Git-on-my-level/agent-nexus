// Home unread feed: ListHomeUnread and MarkHomeRead* with their read cursors
// and topic/board grouping.

package primitives

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"agent-nexus-core/internal/schema"
)

type HomeUnreadGroup struct {
	GroupRef    string
	GroupType   string
	DisplayName string
	Priority    string
	UnreadCount int
	NewestEvent map[string]any
	Events      []map[string]any
}

var HomeFeedEventTypes = []string{
	"message_posted",
	"card_created",
	"card_updated",
	"card_moved",
	"card_resolved",
	"card_archived",
	"card_trashed",
	"topic_updated",
	"topic_archived",
	"topic_restored",
	"topic_trashed",
	"human_attention_requested",
	"human_attention_responded",
	"human_attention_withdrawn",
	"human_attention_retired",
	"human_attention_reopened",
	"human_attention_retirement_audited",
	"document_created",
	"document_revision_created",
	"document_revised",
}

func (s *Store) ListHomeUnread(ctx context.Context, readerID string) ([]HomeUnreadGroup, int, error) {
	if s == nil || s.db == nil {
		return nil, 0, fmt.Errorf("primitives store database is not initialized")
	}
	readerID = strings.TrimSpace(readerID)
	if readerID == "" {
		return nil, 0, fmt.Errorf("reader_id is required")
	}

	lookup, err := s.homeGroupLookup(ctx)
	if err != nil {
		return nil, 0, err
	}
	hidden, err := s.HiddenSubjectRefs(ctx)
	if err != nil {
		return nil, 0, err
	}
	cursorByGroup, err := s.homeReadCursors(ctx, readerID)
	if err != nil {
		return nil, 0, err
	}
	sinceBoundOK, sinceTS, sinceExclusiveID := homeUnreadSinceLowerBound(lookup.groupByRef, cursorByGroup)

	// NOTE: homeUnreadSinceLowerBound returns false when any group lacks a cursor.
	// This is correct — groups without cursors need all their events scanned (all are
	// unread), so no since-bound pruning is safe until every active group has been
	// marked read at least once. The optimization activates after the first "Mark all
	// read" for the current set of active groups.

	groupByRef := map[string]*HomeUnreadGroup{}
	for cursor := ""; ; {
		f := EventListFilter{Preset: "home_feed", Limit: 200, Cursor: cursor}
		if sinceBoundOK && cursor == "" {
			f.Since = sinceTS
			f.SinceExclusiveID = sinceExclusiveID
		}
		eventsPage, err := s.ListEventsPage(ctx, f)
		if err != nil {
			return nil, 0, err
		}
		for _, event := range eventsPage.Events {
			if refsHidden(anyStringSlice(event["refs"]), hidden) || hidden["thread:"+anyStringValue(event["thread_id"])] {
				continue
			}
			groupRef := homeGroupForEvent(event, lookup)
			if groupRef == "" {
				continue
			}
			if !homeEventAfterCursor(event, cursorByGroup[groupRef]) {
				continue
			}
			meta := lookup.groupByRef[groupRef]
			if meta == nil {
				continue
			}
			group := groupByRef[groupRef]
			if group == nil {
				group = &HomeUnreadGroup{
					GroupRef:    groupRef,
					GroupType:   meta.groupType,
					DisplayName: meta.displayName,
					Priority:    meta.priority,
					NewestEvent: event,
				}
				groupByRef[groupRef] = group
			}
			group.Events = append(group.Events, event)
			group.UnreadCount++
		}
		if eventsPage.NextCursor == "" {
			break
		}
		cursor = eventsPage.NextCursor
	}

	groups := make([]HomeUnreadGroup, 0, len(groupByRef))
	total := 0
	for _, group := range groupByRef {
		total += group.UnreadCount
		groups = append(groups, *group)
	}
	sort.SliceStable(groups, func(i, j int) bool {
		left := groups[i]
		right := groups[j]
		if lr, rr := homeImportance(left.Events), homeImportance(right.Events); lr != rr {
			return lr > rr
		}
		leftTS := anyStringValue(left.NewestEvent["ts"])
		rightTS := anyStringValue(right.NewestEvent["ts"])
		if leftTS == rightTS {
			return left.GroupRef < right.GroupRef
		}
		if homeNearTied(leftTS, rightTS) {
			lp := homePriorityRank(left.Priority)
			rp := homePriorityRank(right.Priority)
			if lp != rp {
				return lp < rp
			}
		}
		return leftTS > rightTS
	})
	return groups, total, nil
}

func (s *Store) MarkHomeRead(ctx context.Context, readerID string, groupRefs []string) error {
	return s.MarkHomeReadAt(ctx, readerID, groupRefs, nil)
}

func (s *Store) MarkHomeReadAt(ctx context.Context, readerID string, groupRefs []string, expected map[string]EventCursor) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("primitives store database is not initialized")
	}
	readerID = strings.TrimSpace(readerID)
	if readerID == "" {
		return fmt.Errorf("reader_id is required")
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	lookup, _ := s.homeGroupLookup(ctx)
	for _, groupRef := range dedupeStrings(groupRefs) {
		groupRef = strings.TrimSpace(groupRef)
		if groupRef == "" {
			continue
		}
		inputGroupRef := groupRef
		if lookup != nil {
			if canonical := lookup.aliasToGroupRef[groupRef]; canonical != "" {
				groupRef = canonical
			}
		}
		cursor := expected[groupRef]
		if strings.TrimSpace(cursor.TS) == "" && inputGroupRef != groupRef {
			cursor = expected[inputGroupRef]
		}
		if strings.TrimSpace(cursor.TS) == "" || strings.TrimSpace(cursor.ID) == "" {
			event, err := s.newestHomeEventForGroup(ctx, groupRef)
			if err != nil {
				return err
			}
			if event == nil {
				continue
			}
			cursor = EventCursor{TS: anyStringValue(event["ts"]), ID: anyStringValue(event["id"])}
		}
		if _, err := s.db.ExecContext(ctx, `INSERT INTO home_read_cursors(
				reader_id, group_ref, last_read_event_ts, last_read_event_id, updated_at
			) VALUES (?, ?, ?, ?, ?)
			ON CONFLICT(reader_id, group_ref) DO UPDATE SET
				last_read_event_ts = excluded.last_read_event_ts,
				last_read_event_id = excluded.last_read_event_id,
				updated_at = excluded.updated_at
			WHERE excluded.last_read_event_ts > home_read_cursors.last_read_event_ts
				OR (
					excluded.last_read_event_ts = home_read_cursors.last_read_event_ts
					AND excluded.last_read_event_id > home_read_cursors.last_read_event_id
				)`,
			readerID, groupRef, strings.TrimSpace(cursor.TS), strings.TrimSpace(cursor.ID), now); err != nil {
			return fmt.Errorf("upsert home read cursor: %w", err)
		}
	}
	return nil
}

func (s *Store) newestHomeEventForGroup(ctx context.Context, groupRef string) (map[string]any, error) {
	prefix, id, _ := schema.SplitTypedRef(groupRef)
	switch prefix {
	case "topic":
		events, err := s.ListEvents(ctx, EventListFilter{Preset: "home_feed", TopicID: id, Limit: 1})
		if err != nil {
			return nil, err
		}
		if len(events) == 0 {
			return nil, nil
		}
		return events[0], nil
	case "thread":
		events, err := s.ListEvents(ctx, EventListFilter{Preset: "home_feed", ThreadID: id, Limit: 1})
		if err != nil {
			return nil, err
		}
		if len(events) == 0 {
			return nil, nil
		}
		return events[0], nil
	default:
		return s.newestHomeEventForRef(ctx, groupRef)
	}
}

func (s *Store) newestHomeEventForRef(ctx context.Context, ref string) (map[string]any, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, COALESCE(handle, ''), type, ts, actor_id, thread_id, refs_json, payload_json,
			archived_at, archived_by, trashed_at, trashed_by, trash_reason
		FROM events
		WHERE COALESCE(trashed_at, '') = ''
			AND type IN (`+homeFeedTypePlaceholders()+`)
			AND EXISTS (SELECT 1 FROM json_each(events.refs_json) WHERE json_each.value = ?)
		ORDER BY ts DESC, id DESC LIMIT 1`,
		appendHomeFeedTypeArgs([]any{ref})...)
	if err != nil {
		return nil, fmt.Errorf("query newest home event for ref %s: %w", ref, err)
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, rows.Err()
	}
	var (
		eventID     string
		eventHandle string
		typeValue   string
		ts          string
		actorID     string
		thread      sql.NullString
		refsJSON    string
		payloadJSON string
		archivedAt  sql.NullString
		archivedBy  sql.NullString
		trashedAt   sql.NullString
		trashedBy   sql.NullString
		trashReason sql.NullString
	)
	if err := rows.Scan(&eventID, &eventHandle, &typeValue, &ts, &actorID, &thread, &refsJSON, &payloadJSON,
		&archivedAt, &archivedBy, &trashedAt, &trashedBy, &trashReason); err != nil {
		return nil, fmt.Errorf("scan newest home event for ref %s: %w", ref, err)
	}
	body, err := decodeEventBodyFromRow(ctx, s.db, eventID, eventHandle, typeValue, ts, actorID, thread, refsJSON, payloadJSON)
	if err != nil {
		return nil, err
	}
	overlayEventLifecycleFromSQLColumns(body, archivedAt, archivedBy, trashedAt, trashedBy, trashReason)
	return body, rows.Err()
}

func homeFeedTypePlaceholders() string {
	placeholders := make([]string, len(HomeFeedEventTypes))
	for i := range HomeFeedEventTypes {
		placeholders[i] = "?"
	}
	return strings.Join(placeholders, ",")
}

func appendHomeFeedTypeArgs(args []any) []any {
	for _, t := range HomeFeedEventTypes {
		args = append(args, t)
	}
	return args
}

type homeReadCursor struct {
	TS string
	ID string
}

func (s *Store) homeReadCursors(ctx context.Context, readerID string) (map[string]homeReadCursor, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT group_ref, last_read_event_ts, last_read_event_id FROM home_read_cursors WHERE reader_id = ?`, readerID)
	if err != nil {
		return nil, fmt.Errorf("query home read cursors: %w", err)
	}
	defer rows.Close()
	out := map[string]homeReadCursor{}
	for rows.Next() {
		var groupRef, ts, id string
		if err := rows.Scan(&groupRef, &ts, &id); err != nil {
			return nil, fmt.Errorf("scan home read cursor: %w", err)
		}
		out[groupRef] = homeReadCursor{TS: ts, ID: id}
	}
	return out, rows.Err()
}

type homeGroupMeta struct {
	groupRef    string
	groupType   string
	displayName string
	priority    string
}

type homeGroupLookup struct {
	groupByRef       map[string]*homeGroupMeta
	aliasToGroupRef  map[string]string
	threadToGroupRef map[string]string
}

func (s *Store) homeGroupLookup(ctx context.Context, candidates ...homeGroupCandidates) (*homeGroupLookup, error) {
	lookup := &homeGroupLookup{
		groupByRef:       map[string]*homeGroupMeta{},
		aliasToGroupRef:  map[string]string{},
		threadToGroupRef: map[string]string{},
	}

	if err := s.loadTopicGroups(ctx, lookup, candidates...); err != nil {
		return nil, err
	}
	if err := s.loadBoardGroups(ctx, lookup, candidates...); err != nil {
		return nil, err
	}
	return lookup, nil
}

func (s *Store) loadTopicGroups(ctx context.Context, lookup *homeGroupLookup, candidates ...homeGroupCandidates) error {
	query := `SELECT id, COALESCE(handle, ''), title, summary, thread_id, updated_at, extensions_json, archived_at, trashed_at FROM topics WHERE COALESCE(trashed_at, '') = '' AND COALESCE(archived_at, '') = ''`
	args := []any{}
	if len(candidates) > 0 {
		query += homeGroupCandidateSQL(candidates[0].Topics, candidates[0].Threads, &args)
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("query home topics: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id, handle, updatedAt, extensionsJSON string
		var title, summary, threadID sql.NullString
		var discardArchivedAt, discardTrashedAt sql.NullString
		if err := rows.Scan(&id, &handle, &title, &summary, &threadID, &updatedAt, &extensionsJSON, &discardArchivedAt, &discardTrashedAt); err != nil {
			return fmt.Errorf("scan home topic: %w", err)
		}
		var extensions map[string]any
		_ = json.Unmarshal([]byte(extensionsJSON), &extensions)
		if extensions == nil {
			extensions = map[string]any{}
		}
		priority := firstNonEmptyString(anyStringValue(extensions["priority"]), anyStringValue(extensions["filter_priority"]))
		publicHandle := firstNonEmptyString(strings.TrimSpace(handle), id)
		groupRef := "topic:" + publicHandle
		meta := &homeGroupMeta{
			groupRef:    groupRef,
			groupType:   "topic",
			displayName: firstNonEmptyString(title.String, id),
			priority:    priority,
		}
		lookup.groupByRef[groupRef] = meta
		lookup.aliasToGroupRef["topic:"+id] = groupRef
		if threadID.Valid && strings.TrimSpace(threadID.String) != "" {
			lookup.threadToGroupRef[threadID.String] = groupRef
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate home topics: %w", err)
	}
	return nil
}

func (s *Store) loadBoardGroups(ctx context.Context, lookup *homeGroupLookup, candidates ...homeGroupCandidates) error {
	query := `SELECT id, COALESCE(handle, ''), title, thread_id FROM boards WHERE COALESCE(trashed_at, '') = '' AND COALESCE(archived_at, '') = ''`
	args := []any{}
	if len(candidates) > 0 {
		query += homeGroupCandidateSQL(candidates[0].Boards, candidates[0].Threads, &args)
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("query home boards: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id, handle, title, threadID string
		if err := rows.Scan(&id, &handle, &title, &threadID); err != nil {
			return fmt.Errorf("scan home board: %w", err)
		}
		publicHandle := firstNonEmptyString(strings.TrimSpace(handle), id)
		groupRef := "board:" + publicHandle
		meta := &homeGroupMeta{
			groupRef:    groupRef,
			groupType:   "board",
			displayName: firstNonEmptyString(strings.TrimSpace(title), id),
			priority:    "",
		}
		lookup.groupByRef[groupRef] = meta
		lookup.aliasToGroupRef["board:"+id] = groupRef
		threadID = strings.TrimSpace(threadID)
		if threadID != "" {
			if _, exists := lookup.threadToGroupRef[threadID]; !exists {
				lookup.threadToGroupRef[threadID] = groupRef
			}
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate home boards: %w", err)
	}
	return nil
}

func homeCardEventPrefersBoardGroup(eventType string) bool {
	switch strings.TrimSpace(strings.ToLower(eventType)) {
	case "card_created", "card_updated", "card_moved", "card_resolved", "card_archived", "card_trashed":
		return true
	default:
		return false
	}
}

func homeBoardGroupRefFromRefs(refs []string, lookup *homeGroupLookup) string {
	for _, ref := range refs {
		if strings.HasPrefix(ref, "board:") {
			boardRef := "board:" + strings.TrimSpace(strings.TrimPrefix(ref, "board:"))
			if _, ok := lookup.groupByRef[boardRef]; ok {
				return boardRef
			}
			if canonical := lookup.aliasToGroupRef[boardRef]; canonical != "" {
				return canonical
			}
		}
	}
	return ""
}

func homeGroupForEvent(event map[string]any, lookup *homeGroupLookup) string {
	eventType, _ := event["type"].(string)
	if homeCardEventPrefersBoardGroup(eventType) {
		if boardRef := homeBoardGroupRefFromRefs(anyStringSlice(event["refs"]), lookup); boardRef != "" {
			return boardRef
		}
	}
	for _, ref := range anyStringSlice(event["refs"]) {
		if strings.HasPrefix(ref, "topic:") {
			topicRef := "topic:" + strings.TrimSpace(strings.TrimPrefix(ref, "topic:"))
			if _, ok := lookup.groupByRef[topicRef]; ok {
				return topicRef
			}
			if canonical := lookup.aliasToGroupRef[topicRef]; canonical != "" {
				return canonical
			}
		}
	}
	if threadID := strings.TrimSpace(anyStringValue(event["thread_id"])); threadID != "" {
		if groupRef, ok := lookup.threadToGroupRef[threadID]; ok {
			return groupRef
		}
	}
	for _, ref := range anyStringSlice(event["refs"]) {
		if strings.HasPrefix(ref, "thread:") {
			threadID := strings.TrimSpace(strings.TrimPrefix(ref, "thread:"))
			if groupRef, ok := lookup.threadToGroupRef[threadID]; ok {
				return groupRef
			}
		}
	}
	if boardRef := homeBoardGroupRefFromRefs(anyStringSlice(event["refs"]), lookup); boardRef != "" {
		return boardRef
	}
	return ""
}

func homeEventAfterCursor(event map[string]any, cursor homeReadCursor) bool {
	if strings.TrimSpace(cursor.TS) == "" {
		return true
	}
	ts := anyStringValue(event["ts"])
	id := anyStringValue(event["id"])
	return ts > cursor.TS || (ts == cursor.TS && id > cursor.ID)
}

// homeUnreadSinceLowerBound returns a global lower bound on events that can still be home-unread
// when every topic has a read cursor: strictly after the lexicographically smallest (ts, id) cursor.
func homeUnreadSinceLowerBound(groupByRef map[string]*homeGroupMeta, cursorByGroup map[string]homeReadCursor) (ok bool, ts string, exclusiveID string) {
	if len(groupByRef) == 0 {
		return false, "", ""
	}
	for groupRef := range groupByRef {
		c := cursorByGroup[groupRef]
		if strings.TrimSpace(c.TS) == "" || strings.TrimSpace(c.ID) == "" {
			return false, "", ""
		}
		if ts == "" {
			ts, exclusiveID = c.TS, c.ID
			continue
		}
		if c.TS < ts || (c.TS == ts && c.ID < exclusiveID) {
			ts, exclusiveID = c.TS, c.ID
		}
	}
	return true, ts, exclusiveID
}

func homeNearTied(left, right string) bool {
	l, lerr := time.Parse(time.RFC3339Nano, left)
	r, rerr := time.Parse(time.RFC3339Nano, right)
	if lerr != nil || rerr != nil {
		return false
	}
	if l.After(r) {
		return l.Sub(r) <= 5*time.Minute
	}
	return r.Sub(l) <= 5*time.Minute
}

func homePriorityRank(priority string) int {
	switch strings.ToUpper(strings.TrimSpace(priority)) {
	case "P0":
		return 0
	case "P1":
		return 1
	case "P2":
		return 2
	default:
		return 3
	}
}
