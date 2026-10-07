package primitives

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"sync"

	"agent-nexus-core/internal/resourceaccess"
)

const ReceiptStreamPageSize = 200
const ReceiptStreamChunkCandidateBudget = 2000

const agentWakeupSelectList = `wakeup_id, status, notification_status, target_handle, target_actor_id,
	workspace_id, workspace_name, thread_id, thread_title, trigger_event_id, trigger_created_at,
	trigger_text, refs_json, bridge_instance_id,
	failure_reason, created_at, claimed_at, completed_at, failed_at, read_at, dismissed_at, updated_at`

// ReceiptStreamCursor is connection-local traversal state. It can sit on a
// hidden row and must not be serialized to a client.
type ReceiptStreamCursor struct {
	Snapshot         bool
	IncludeCurrent   bool
	CreatedAt        string
	WakeupID         string
	Seq              int64
	TailFrom         int64
	ReplayVisibility bool
	VisibilityEpoch  int64
	VisibilityKnown  bool
	AcceptedWakeupID string
	AcceptedDigest   string
}

// ReceiptStreamPage is one bounded read. Cursor advances over hidden rows;
// Wakeups contains only rows the caller can currently see.
type ReceiptStreamPage struct {
	Wakeups       []AgentWakeup
	Cursor        ReceiptStreamCursor `json:"-"`
	HasMore       bool
	AccessChanged bool `json:"-"`
}

// ReceiptStreamCursor replays the snapshot for an empty id. A visible receipt
// resumes at that receipt: a matching digest continues after it, and any other
// digest scans forward from it so an offline update is delivered. Private,
// unknown, and trashed ids are indistinguishable and start at the log head.
func (s *Store) ReceiptStreamCursor(ctx context.Context, threadID string, lastEventID string, accept func(AgentWakeup) bool) (ReceiptStreamCursor, error) {
	if s == nil || s.db == nil {
		return ReceiptStreamCursor{}, errors.New("primitives store database is not initialized")
	}
	head, err := s.receiptStreamHead(ctx, threadID)
	if err != nil {
		return ReceiptStreamCursor{}, err
	}
	visibility, err := s.receiptVisibilityEpoch(ctx)
	if err != nil {
		return ReceiptStreamCursor{}, err
	}
	base := ReceiptStreamCursor{TailFrom: head, VisibilityEpoch: visibility, VisibilityKnown: true}
	wakeupID, digest, ok := receiptStreamWakeupID(lastEventID)
	if !ok {
		if strings.TrimSpace(lastEventID) == "" {
			base.Snapshot = true
			base.ReplayVisibility = true
			return base, nil
		}
		base.Seq = head
		return base, nil
	}
	wakeup, found, err := s.visibleReceiptWakeup(ctx, threadID, wakeupID)
	if err != nil {
		return ReceiptStreamCursor{}, err
	}
	if !found {
		base.Seq = head
		return base, nil
	}
	base.Snapshot = true
	base.ReplayVisibility = true
	base.CreatedAt = wakeup.CreatedAt
	base.WakeupID = wakeup.WakeupID
	if accept != nil && accept(wakeup) {
		base.AcceptedWakeupID = wakeup.WakeupID
		base.AcceptedDigest = digest
		return base, nil
	}
	// The payload changed while disconnected. Include this row so the current
	// state is delivered, then continue forward. Never skip to the head.
	base.IncludeCurrent = true
	return base, nil
}

// ListReceiptStreamPage reads at most ReceiptStreamPageSize positions. Snapshot
// pages follow created-at order. After the snapshot, pages follow the
// append-only update log so a later change to an older receipt is not missed.
func (s *Store) ListReceiptStreamPage(ctx context.Context, threadID string, cursor ReceiptStreamCursor) (ReceiptStreamPage, error) {
	if s == nil || s.db == nil {
		return ReceiptStreamPage{}, errors.New("primitives store database is not initialized")
	}
	cursor, changed, err := s.advanceReceiptVisibility(ctx, threadID, cursor)
	if err != nil {
		return ReceiptStreamPage{}, err
	}
	var page ReceiptStreamPage
	if cursor.Snapshot {
		page, err = s.listReceiptSnapshotPage(ctx, threadID, cursor)
	} else {
		page, err = s.listReceiptTailPage(ctx, threadID, cursor)
	}
	if err != nil {
		return ReceiptStreamPage{}, err
	}
	page.AccessChanged = changed
	page.Cursor.ReplayVisibility = cursor.ReplayVisibility
	page.Cursor.VisibilityEpoch = cursor.VisibilityEpoch
	page.Cursor.VisibilityKnown = true
	return page, nil
}

func (s *Store) advanceReceiptVisibility(ctx context.Context, threadID string, cursor ReceiptStreamCursor) (ReceiptStreamCursor, bool, error) {
	visibility, err := s.receiptVisibilityEpoch(ctx)
	if err != nil {
		return cursor, false, err
	}
	changed := cursor.VisibilityKnown && cursor.ReplayVisibility && visibility != cursor.VisibilityEpoch
	if changed {
		head, err := s.receiptStreamHead(ctx, threadID)
		if err != nil {
			return cursor, false, err
		}
		cursor = ReceiptStreamCursor{Snapshot: true, TailFrom: head, ReplayVisibility: true}
	}
	cursor.VisibilityEpoch = visibility
	cursor.VisibilityKnown = true
	return cursor, changed, nil
}

func (s *Store) receiptVisibilityEpoch(ctx context.Context) (int64, error) {
	var version int64
	err := s.db.QueryRowContext(ctx, `SELECT version FROM receipt_visibility_epoch WHERE singleton=1`).Scan(&version)
	return version, err
}

func (s *Store) receiptStreamHead(ctx context.Context, threadID string) (int64, error) {
	var head int64
	err := s.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(seq),0) FROM agent_wakeup_stream WHERE thread_id=?`, threadID).Scan(&head)
	return head, err
}

func (s *Store) visibleReceiptWakeup(ctx context.Context, threadID, wakeupID string) (AgentWakeup, bool, error) {
	wakeups, err := s.loadVisibleReceipts(ctx, threadID, []string{wakeupID})
	if err != nil {
		return AgentWakeup{}, false, err
	}
	if len(wakeups) != 1 || wakeups[0].WakeupID != wakeupID {
		return AgentWakeup{}, false, nil
	}
	return wakeups[0], true, nil
}

func (s *Store) listReceiptSnapshotPage(ctx context.Context, threadID string, cursor ReceiptStreamCursor) (ReceiptStreamPage, error) {
	limit := ReceiptStreamPageSize + 1
	sameID := "wakeup_id>?"
	if cursor.IncludeCurrent {
		sameID = "wakeup_id>=?"
	}
	rows, err := s.db.QueryContext(ctx, `WITH stream_same AS MATERIALIZED (
			SELECT wakeup_id, created_at FROM agent_wakeup_snapshot_positions
			WHERE thread_id=? AND created_at=? AND `+sameID+`
			ORDER BY wakeup_id LIMIT ?
		), stream_later AS MATERIALIZED (
			SELECT wakeup_id, created_at FROM agent_wakeup_snapshot_positions
			WHERE thread_id=? AND created_at>?
			ORDER BY created_at, wakeup_id LIMIT ?
		), stream_candidates AS MATERIALIZED (
			SELECT * FROM stream_same UNION ALL SELECT * FROM stream_later
			ORDER BY created_at, wakeup_id LIMIT ?
		) SELECT wakeup_id, created_at FROM stream_candidates ORDER BY created_at, wakeup_id`,
		threadID, cursor.CreatedAt, cursor.WakeupID, limit,
		threadID, cursor.CreatedAt, limit,
		limit)
	if err != nil {
		return ReceiptStreamPage{}, err
	}
	page := ReceiptStreamPage{Cursor: cursor, Wakeups: []AgentWakeup{}}
	page.Cursor.IncludeCurrent = false
	ids := make([]string, 0, ReceiptStreamPageSize)
	for rows.Next() {
		var wakeupID, createdAt string
		if err = rows.Scan(&wakeupID, &createdAt); err != nil {
			rows.Close()
			return ReceiptStreamPage{}, err
		}
		if len(ids) == ReceiptStreamPageSize {
			page.HasMore = true
			continue
		}
		ids = append(ids, wakeupID)
		page.Cursor.CreatedAt = createdAt
		page.Cursor.WakeupID = wakeupID
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return ReceiptStreamPage{}, err
	}
	if len(ids) > 0 {
		page.Wakeups, err = s.loadVisibleReceipts(ctx, threadID, ids)
		if err != nil {
			return ReceiptStreamPage{}, err
		}
	}
	if page.HasMore {
		return page, nil
	}
	// Snapshot positions are consumed. Changes committed after the cursor was
	// opened remain on the update log and are not folded into this page.
	page.Cursor.Snapshot = false
	page.Cursor.Seq = cursor.TailFrom
	var next int64
	err = s.db.QueryRowContext(ctx, `SELECT seq FROM agent_wakeup_stream WHERE thread_id=? AND seq>? ORDER BY seq LIMIT 1`, threadID, cursor.TailFrom).Scan(&next)
	if errors.Is(err, sql.ErrNoRows) {
		return page, nil
	}
	if err != nil {
		return ReceiptStreamPage{}, err
	}
	page.HasMore = true
	return page, nil
}

func (s *Store) listReceiptTailPage(ctx context.Context, threadID string, cursor ReceiptStreamCursor) (ReceiptStreamPage, error) {
	limit := ReceiptStreamPageSize + 1
	rows, err := s.db.QueryContext(ctx, `SELECT seq, wakeup_id FROM agent_wakeup_stream
		WHERE thread_id=? AND seq>? ORDER BY seq LIMIT ?`, threadID, cursor.Seq, limit)
	if err != nil {
		return ReceiptStreamPage{}, err
	}
	page := ReceiptStreamPage{Cursor: cursor, Wakeups: []AgentWakeup{}}
	ids := make([]string, 0, ReceiptStreamPageSize)
	seen := map[string]struct{}{}
	examined := 0
	for rows.Next() {
		var seq int64
		var wakeupID string
		if err = rows.Scan(&seq, &wakeupID); err != nil {
			rows.Close()
			return ReceiptStreamPage{}, err
		}
		if examined == ReceiptStreamPageSize {
			page.HasMore = true
			continue
		}
		examined++
		page.Cursor.Seq = seq
		if _, ok := seen[wakeupID]; ok {
			continue
		}
		seen[wakeupID] = struct{}{}
		ids = append(ids, wakeupID)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return ReceiptStreamPage{}, err
	}
	if len(ids) == 0 {
		return page, nil
	}
	page.Wakeups, err = s.loadVisibleReceipts(ctx, threadID, ids)
	if err != nil {
		return ReceiptStreamPage{}, err
	}
	return page, nil
}

func (s *Store) loadVisibleReceipts(ctx context.Context, threadID string, ids []string) ([]AgentWakeup, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	keys, err := json.Marshal(ids)
	if err != nil {
		return nil, err
	}
	plain := resourceaccess.WithoutPolicy(ctx)
	metaRows, err := s.db.QueryContext(plain, `SELECT agent_wakeups.wakeup_id, agent_wakeups.thread_id, agent_wakeups.trigger_event_id,
		agent_wakeups.refs_json, agent_wakeups.trigger_text, agent_wakeups.thread_title, agent_wakeups.failure_reason
		FROM json_each(?) AS wanted
		JOIN agent_wakeups ON agent_wakeups.wakeup_id = wanted.value`, string(keys))
	if err != nil {
		return nil, err
	}
	type receiptIdentity struct {
		threadID, triggerID string
		atoms               []string
	}
	identity := map[string]receiptIdentity{}
	triggerIDs := []string{}
	for metaRows.Next() {
		var wakeupID, wakeupThread, triggerID, refs, triggerText, threadTitle, failure string
		if err = metaRows.Scan(&wakeupID, &wakeupThread, &triggerID, &refs, &triggerText, &threadTitle, &failure); err != nil {
			metaRows.Close()
			return nil, err
		}
		identity[wakeupID] = receiptIdentity{
			threadID: wakeupThread, triggerID: triggerID,
			atoms: receiptReferenceAtoms(refs, triggerText, threadTitle, failure),
		}
		if triggerID != "" {
			triggerIDs = append(triggerIDs, triggerID)
		}
	}
	err = metaRows.Err()
	metaRows.Close()
	if err != nil {
		return nil, err
	}
	trashed, err := s.trashedReceiptTriggers(plain, triggerIDs)
	if err != nil {
		return nil, err
	}
	snap, err := s.receiptDenial(ctx)
	if err != nil {
		return nil, err
	}
	denied := make([][2]string, 0)
	for _, id := range ids {
		meta, ok := identity[id]
		if !ok || meta.threadID != threadID || trashed[meta.triggerID] || snap.deniesWakeup(id, meta.threadID, meta.triggerID) || receiptAtomsDenied(snap, meta.atoms) {
			denied = append(denied, [2]string{"wakeup", id})
		}
	}
	deniedJSON, err := json.Marshal(denied)
	if err != nil {
		return nil, err
	}
	// Seek each candidate by primary key. Denial and trash are a page-sized
	// set, not a scan of the thread's history.
	rows, err := s.db.QueryContext(plain, `SELECT `+agentWakeupSelectList+` FROM json_each(?) AS wanted
		JOIN agent_wakeups ON agent_wakeups.wakeup_id = wanted.value
		WHERE NOT EXISTS (
			SELECT 1 FROM json_each(?) AS denied
			WHERE json_extract(denied.value,'$[0]')='wakeup' AND json_extract(denied.value,'$[1]')=agent_wakeups.wakeup_id
		)`, string(keys), string(deniedJSON))
	if err != nil {
		return nil, err
	}
	byID := map[string]AgentWakeup{}
	for rows.Next() {
		wakeup, scanErr := scanAgentWakeup(rows)
		if scanErr != nil {
			rows.Close()
			return nil, scanErr
		}
		byID[wakeup.WakeupID] = wakeup
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	out := make([]AgentWakeup, 0, len(byID))
	for _, id := range ids {
		wakeup, ok := byID[id]
		if !ok || wakeup.ThreadID != threadID {
			continue
		}
		out = append(out, wakeup)
	}
	return out, nil
}

func receiptReferenceAtoms(fields ...string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, field := range fields {
		for _, atom := range resourceaccess.ReferenceAtoms(field) {
			if seen[atom] || strings.HasPrefix(atom, "$") {
				continue
			}
			seen[atom] = true
			out = append(out, atom)
		}
	}
	return out
}

func receiptAtomsDenied(snap *denialSnapshot, atoms []string) bool {
	for _, atom := range atoms {
		kind, id, ok := strings.Cut(atom, ":")
		if ok && snap.denies(kind, id) {
			return true
		}
	}
	return false
}

func (s *Store) trashedReceiptTriggers(ctx context.Context, ids []string) (map[string]bool, error) {
	trashed := map[string]bool{}
	if len(ids) == 0 {
		return trashed, nil
	}
	keys, err := json.Marshal(ids)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT events.id FROM json_each(?) AS wanted
		JOIN events ON events.id = wanted.value
		WHERE COALESCE(events.trashed_at,'')<>''`, string(keys))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		trashed[id] = true
	}
	return trashed, rows.Err()
}

type receiptDenialCacheKey struct {
	db    *accessDB
	scope AccessScope
}

type receiptDenialSlot struct {
	visibility int64
	snapshot   *denialSnapshot
}

var receiptDenials = struct {
	sync.Mutex
	slots map[receiptDenialCacheKey]receiptDenialSlot
}{slots: map[receiptDenialCacheKey]receiptDenialSlot{}}

// receiptDenial reuses one closure for the whole visibility epoch. Wakeup
// inserts and updates move the resource-access epoch without changing which
// existing receipts the caller can see, so those writes must not rebuild it.
func (s *Store) receiptDenial(ctx context.Context) (*denialSnapshot, error) {
	scope, scoped := accessScopeFrom(ctx)
	if !scoped {
		return nil, nil
	}
	visibility, err := s.receiptVisibilityEpoch(ctx)
	if err != nil {
		return nil, err
	}
	key := receiptDenialCacheKey{db: s.db, scope: scope}
	receiptDenials.Lock()
	slot, ok := receiptDenials.slots[key]
	receiptDenials.Unlock()
	if ok && slot.visibility == visibility && slot.snapshot != nil {
		return slot.snapshot, nil
	}
	captureCtx := WithRequestAccessScope(ctx, scope)
	var n int
	if err = s.db.QueryRowContext(captureCtx, `SELECT count(*) FROM threads WHERE id=''`).Scan(&n); err != nil {
		return nil, err
	}
	snap := denialSnapshotFrom(captureCtx)
	if snap == nil {
		return nil, errors.New("receipt access snapshot unavailable")
	}
	receiptDenials.Lock()
	if len(receiptDenials.slots) >= 32 {
		for old := range receiptDenials.slots {
			delete(receiptDenials.slots, old)
			break
		}
	}
	receiptDenials.slots[key] = receiptDenialSlot{visibility: visibility, snapshot: snap}
	receiptDenials.Unlock()
	return snap, nil
}

func receiptStreamWakeupID(lastEventID string) (string, string, bool) {
	rest, ok := strings.CutPrefix(strings.TrimSpace(lastEventID), "receipt:")
	if !ok {
		return "", "", false
	}
	wakeupID, digest, ok := strings.Cut(rest, "@")
	if !ok || strings.TrimSpace(wakeupID) == "" || strings.TrimSpace(digest) == "" {
		return "", "", false
	}
	return wakeupID, digest, true
}
