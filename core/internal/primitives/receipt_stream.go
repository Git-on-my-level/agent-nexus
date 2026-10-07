package primitives

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

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
	Replay           bool
	ReplayAgain      bool
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
	ThreadDenied  bool `json:"-"`
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
	base := ReceiptStreamCursor{Seq: head, TailFrom: head, VisibilityEpoch: visibility, VisibilityKnown: true}
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
		if cursor.Replay {
			// Drain new updates before every replay page. Unrelated changes cannot
			// starve newly appended visible receipts.
			tail, tailErr := s.listReceiptTailPage(ctx, threadID, cursor)
			if tailErr != nil {
				return ReceiptStreamPage{}, tailErr
			}
			if tail.Cursor.Seq != cursor.Seq {
				seq := tail.Cursor.Seq
				tail.Cursor = cursor
				tail.Cursor.Seq = seq
				tail.HasMore = true
				tail.AccessChanged = changed
				if changed {
					tail.ThreadDenied, err = s.receiptThreadDenied(ctx, threadID)
					if err != nil {
						return ReceiptStreamPage{}, err
					}
				}
				return tail, nil
			}
		}
		page, err = s.listReceiptSnapshotPage(ctx, threadID, cursor)
	} else {
		page, err = s.listReceiptTailPage(ctx, threadID, cursor)
	}
	if err != nil {
		return ReceiptStreamPage{}, err
	}
	page.AccessChanged = changed
	if changed {
		page.ThreadDenied, err = s.receiptThreadDenied(ctx, threadID)
		if err != nil {
			return ReceiptStreamPage{}, err
		}
	}
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
	changed := cursor.VisibilityKnown && visibility != cursor.VisibilityEpoch
	if changed && cursor.ReplayVisibility {
		if cursor.Snapshot {
			cursor.Replay = true
			cursor.ReplayAgain = true
		} else {
			cursor.Snapshot = true
			cursor.Replay = true
			cursor.CreatedAt = ""
			cursor.WakeupID = ""
			cursor.IncludeCurrent = false
		}
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
	if cursor.ReplayAgain {
		page.Cursor.Snapshot = true
		page.Cursor.Replay = true
		page.Cursor.ReplayAgain = false
		page.Cursor.CreatedAt = ""
		page.Cursor.WakeupID = ""
		page.HasMore = true
		return page, nil
	}
	if !cursor.Replay {
		page.Cursor.Seq = cursor.TailFrom
	}
	page.Cursor.Replay = false
	var next int64
	err = s.db.QueryRowContext(ctx, `SELECT seq FROM agent_wakeup_stream WHERE thread_id=? AND seq>? ORDER BY seq LIMIT 1`, threadID, page.Cursor.Seq).Scan(&next)
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

// Only traversal/ownership metadata is read before the scoped payload query.
// New isolated wakeup leaves can inherit cached parent denials without rebuilding
// the workspace closure. The final read validates the full authorization epoch.
func (s *Store) loadVisibleReceipts(ctx context.Context, threadID string, ids []string) ([]AgentWakeup, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	keys, _ := json.Marshal(ids)
	rows, err := s.db.QueryContext(ctx, `SELECT p.wakeup_id,p.thread_id,p.trigger_event_id,
  COALESCE((SELECT json_group_array(x.target_key) FROM resource_access_edges e
   CROSS JOIN resource_access_exact_edges x ON x.edge_id=e.rowid WHERE e.source_kind='wakeup' AND e.source_id=p.wakeup_id),'[]'),
  COALESCE((SELECT json_group_array(json_array(i.kind,i.resource_id)) FROM resource_access_edges e
   CROSS JOIN resource_access_mentions m ON m.edge_id=e.rowid
   JOIN resource_access_identities i ON i.identity_id=m.identity_id WHERE e.source_kind='wakeup' AND e.source_id=p.wakeup_id),'[]'),
  (SELECT version FROM resource_access_epoch WHERE singleton=1)
  FROM json_each(?) wanted JOIN agent_wakeup_snapshot_positions p ON p.wakeup_id=wanted.value`, string(keys))
	if err != nil {
		return nil, err
	}
	type metadata struct {
		id, thread, trigger string
		atoms               []string
		mentions            [][2]string
	}
	var metas []metadata
	var epoch int64
	for rows.Next() {
		var m metadata
		var atoms, mentions string
		if err = rows.Scan(&m.id, &m.thread, &m.trigger, &atoms, &mentions, &epoch); err != nil {
			rows.Close()
			return nil, err
		}
		if err = json.Unmarshal([]byte(atoms), &m.atoms); err != nil {
			rows.Close()
			return nil, err
		}
		if err = json.Unmarshal([]byte(mentions), &m.mentions); err != nil {
			rows.Close()
			return nil, err
		}
		metas = append(metas, m)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	snap, err := s.receiptDenial(ctx)
	if err != nil {
		return nil, err
	}
	denied := [][2]string{}
	for _, m := range metas {
		hidden := m.thread != threadID || snap.deniesWakeup(m.id, m.thread, m.trigger)
		for _, atom := range m.atoms {
			hidden = hidden || snap.deniesAtom(atom)
		}
		for _, ref := range m.mentions {
			hidden = hidden || snap.denies(ref[0], ref[1])
		}
		if hidden {
			denied = append(denied, [2]string{"wakeup", m.id})
		}
	}
	data, _ := json.Marshal(denied)
	readCtx := ctx
	if scope, scoped := accessScopeFrom(ctx); scoped {
		readCtx = WithRequestAccessScope(ctx, scope)
		state, _ := readCtx.Value(denialRequestKey{}).(*denialRequestState)
		state.snapshot = &denialSnapshot{epoch: epoch, rows: string(data)}
	}
	// Scope shadows agent_wakeups and validates resource_access_epoch in this
	// statement, falling back to the canonical graph on ANY intervening write.
	rows, err = s.db.QueryContext(readCtx, `SELECT `+agentWakeupSelectList+` FROM json_each(?) wanted
  CROSS JOIN agent_wakeups ON agent_wakeups.wakeup_id=wanted.value
  WHERE agent_wakeups.thread_id=? AND NOT EXISTS (SELECT 1 FROM receipt_trigger_positions e
   WHERE e.id=agent_wakeups.trigger_event_id AND COALESCE(e.trashed_at,'')<>'')`, string(keys), threadID)
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
	out := []AgentWakeup{}
	for _, id := range ids {
		if wakeup, ok := byID[id]; ok {
			out = append(out, wakeup)
		}
	}
	return out, nil
}

func (s *Store) receiptDenial(ctx context.Context) (*denialSnapshot, error) {
	scope, scoped := accessScopeFrom(ctx)
	if !scoped {
		return nil, nil
	}
	capture := withRequestAccessEpoch(ctx, scope, "receipt_access_epoch")
	policy, _ := resourceaccess.PolicyFrom(capture)
	original := policy.ReadOnDB
	policy.ReadOnDB = func(c context.Context, db resourceaccess.QueryRower, query string, args []any) (string, []any) {
		state, _ := c.Value(denialRequestKey{}).(*denialRequestState)
		state.Lock()
		if state.snapshot == nil {
			raw, _ := db.(*sql.DB)
			cached := cachedReadDenialForEpoch(raw, scope, "receipt_access_epoch")
			if cached != nil {
				var epoch int64
				if db.QueryRowContext(c, `SELECT version FROM receipt_access_epoch WHERE singleton=1`).Scan(&epoch) == nil && epoch == cached.epoch {
					state.snapshot = cached
				}
			}
		}
		state.Unlock()
		if denialSnapshotFrom(c) == nil {
			original(c, db, query, args)
		}
		if snap := denialSnapshotFrom(c); snap != nil {
			// This capture probe selects the impossible thread key, so no denial
			// binding from the complete closure is needed on warm reads.
			empty := &denialSnapshot{epoch: snap.epoch, epochTable: "receipt_access_epoch", rows: "[]"}
			return scopeReadWithSnapshot(c, query, empty), append([]any{"[]"}, args...)
		}
		return original(c, db, query, args)
	}
	capture = resourceaccess.WithPolicy(capture, policy)
	var n int
	if err := s.db.QueryRowContext(capture, `SELECT count(*) FROM threads WHERE id=''`).Scan(&n); err != nil {
		return nil, err
	}
	snap := denialSnapshotFrom(capture)
	if snap == nil {
		return nil, errors.New("receipt access snapshot unavailable")
	}
	return snap, nil
}

func (s *Store) receiptThreadDenied(ctx context.Context, threadID string) (bool, error) {
	scope, scoped := accessScopeFrom(ctx)
	if !scoped {
		return false, nil
	}
	snap, err := s.receiptDenial(ctx)
	if err != nil {
		return false, err
	}
	rows, ok := snap.targetRows("thread", []string{threadID})
	if !ok {
		return false, errors.New("receipt thread access snapshot unavailable")
	}
	readCtx := WithRequestAccessScope(ctx, scope)
	state, _ := readCtx.Value(denialRequestKey{}).(*denialRequestState)
	state.snapshot = &denialSnapshot{epoch: snap.epoch, epochTable: "receipt_access_epoch", rows: rows}
	var visible bool
	err = s.db.QueryRowContext(readCtx, `SELECT EXISTS(SELECT 1 FROM threads WHERE id=?)`, threadID).Scan(&visible)
	return !visible, err
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
