package primitives

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
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
	Snapshot  bool
	CreatedAt string
	WakeupID  string
	Seq       int64
	TailFrom  int64
}

// ReceiptStreamPage is one bounded read. Cursor advances over hidden rows;
// Wakeups contains only rows the caller can currently see.
type ReceiptStreamPage struct {
	Wakeups []AgentWakeup
	Cursor  ReceiptStreamCursor `json:"-"`
	HasMore bool
}

// ReceiptStreamCursor accepts a client resume id only when accept reports that
// the loaded row is the caller's current visible receipt. Empty ids replay the
// snapshot. Every other id starts at the head of the update log.
func (s *Store) ReceiptStreamCursor(ctx context.Context, threadID string, lastEventID string, accept func(AgentWakeup) bool) (ReceiptStreamCursor, error) {
	if s == nil || s.db == nil {
		return ReceiptStreamCursor{}, errors.New("primitives store database is not initialized")
	}
	head, err := s.receiptStreamHead(ctx, threadID)
	if err != nil {
		return ReceiptStreamCursor{}, err
	}
	wakeupID, ok := receiptStreamWakeupID(lastEventID)
	if !ok {
		if strings.TrimSpace(lastEventID) == "" {
			return ReceiptStreamCursor{Snapshot: true, TailFrom: head}, nil
		}
		return ReceiptStreamCursor{Seq: head, TailFrom: head}, nil
	}
	wakeup, found, err := s.visibleReceiptWakeup(ctx, threadID, wakeupID)
	if err != nil {
		return ReceiptStreamCursor{}, err
	}
	if !found || accept == nil || !accept(wakeup) {
		return ReceiptStreamCursor{Seq: head, TailFrom: head}, nil
	}
	return ReceiptStreamCursor{Snapshot: true, CreatedAt: wakeup.CreatedAt, WakeupID: wakeup.WakeupID, TailFrom: head}, nil
}

// ListReceiptStreamPage reads at most ReceiptStreamPageSize positions. Snapshot
// pages follow created-at order. After the snapshot, pages follow the
// append-only update log so a later change to an older receipt is not missed.
func (s *Store) ListReceiptStreamPage(ctx context.Context, threadID string, cursor ReceiptStreamCursor) (ReceiptStreamPage, error) {
	if s == nil || s.db == nil {
		return ReceiptStreamPage{}, errors.New("primitives store database is not initialized")
	}
	if cursor.Snapshot {
		return s.listReceiptSnapshotPage(ctx, threadID, cursor)
	}
	return s.listReceiptTailPage(ctx, threadID, cursor)
}

func (s *Store) receiptStreamHead(ctx context.Context, threadID string) (int64, error) {
	var head int64
	err := s.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(seq),0) FROM agent_wakeup_stream WHERE thread_id=?`, threadID).Scan(&head)
	return head, err
}

func (s *Store) visibleReceiptWakeup(ctx context.Context, threadID, wakeupID string) (AgentWakeup, bool, error) {
	wakeup, err := scanAgentWakeup(s.db.QueryRowContext(ctx, `SELECT `+agentWakeupSelectList+` FROM agent_wakeups
		WHERE wakeup_id=? AND thread_id=?
		AND NOT EXISTS (
			SELECT 1 FROM events
			WHERE events.id=agent_wakeups.trigger_event_id AND COALESCE(events.trashed_at,'')<>''
		)`, wakeupID, threadID))
	if errors.Is(err, ErrNotFound) {
		return AgentWakeup{}, false, nil
	}
	if err != nil {
		return AgentWakeup{}, false, err
	}
	return wakeup, true, nil
}

func (s *Store) listReceiptSnapshotPage(ctx context.Context, threadID string, cursor ReceiptStreamCursor) (ReceiptStreamPage, error) {
	limit := ReceiptStreamPageSize + 1
	rows, err := s.db.QueryContext(ctx, `WITH stream_same AS MATERIALIZED (
			SELECT wakeup_id, created_at FROM agent_wakeup_snapshot_positions
			WHERE thread_id=? AND created_at=? AND wakeup_id>?
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
	keys, err := json.Marshal(ids)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+agentWakeupSelectList+` FROM agent_wakeups
		WHERE thread_id=? AND wakeup_id IN (SELECT value FROM json_each(?))
		AND NOT EXISTS (
			SELECT 1 FROM events
			WHERE events.id=agent_wakeups.trigger_event_id AND COALESCE(events.trashed_at,'')<>''
		)`, threadID, string(keys))
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
		if !ok {
			continue
		}
		out = append(out, wakeup)
	}
	return out, nil
}

func receiptStreamWakeupID(lastEventID string) (string, bool) {
	rest, ok := strings.CutPrefix(strings.TrimSpace(lastEventID), "receipt:")
	if !ok {
		return "", false
	}
	wakeupID, digest, ok := strings.Cut(rest, "@")
	if !ok || strings.TrimSpace(wakeupID) == "" || strings.TrimSpace(digest) == "" {
		return "", false
	}
	return wakeupID, true
}
