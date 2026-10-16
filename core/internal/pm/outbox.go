package pm

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

const deliverPendingTurnsSQL = `SELECT t.id, COALESCE(json_extract(c.body,'$.origin') IS NOT NULL,0), EXISTS(SELECT 1 FROM pm_records d WHERE d.kind='delivery' AND d.workspace_id=t.workspace_id AND d.parent_id=t.id)
 FROM pm_records t LEFT JOIN pm_records c ON c.kind='conversation' AND c.id=t.parent_id
 WHERE t.kind='turn' AND t.workspace_id=? AND json_extract(t.body,'$.status')='delivered' AND COALESCE(json_extract(t.body,'$.delivery_queued'),0)=0
 ORDER BY t.rowid LIMIT ?`

// QueueTurnDeliveries atomically persists every plain-text fragment. A crash
// cannot mark a multi-part reply queued while losing the remaining fragments.
func (s *Service) QueueTurnDeliveries(ctx context.Context, turnID string) ([]Delivery, error) {
	var t Turn
	if err := s.store.get(ctx, "turn", turnID, &t); err != nil {
		return nil, err
	}
	if t.Status != Delivered {
		return nil, ErrConflict
	}
	var c Conversation
	if err := s.store.get(ctx, "conversation", t.ConversationID, &c); err != nil {
		return nil, err
	}
	if c.Origin == nil {
		return nil, ErrInvalid
	}
	b, err := s.ResolveBinding(ctx, *c.Origin)
	if err != nil || b.ActorID != c.ActorID {
		return nil, ErrForbidden
	}
	parts := splitReply(t.Response, 1800)
	deliveries := make([]Delivery, 0, len(parts))
	for i, text := range parts {
		if len(parts) > 1 {
			text = fmt.Sprintf("%s\n(%d/%d)", text, i+1, len(parts))
		}
		deliveries = append(deliveries, Delivery{ID: stableID("delivery", t.ID, fmt.Sprint(i)), WorkspaceID: c.WorkspaceID, ActorID: c.ActorID, Origin: *c.Origin, Text: text, Status: Pending, Revision: 1})
	}
	tx, err := s.store.database().BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	for i, d := range deliveries {
		raw, err := json.Marshal(d)
		if err != nil {
			return nil, err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO pm_records(kind,id,workspace_id,actor_id,parent_id,revision,body) VALUES('delivery',?,?,?,?,1,?) ON CONFLICT(kind,id) DO NOTHING`, d.ID, d.WorkspaceID, d.ActorID, t.ID, raw)
		if err != nil {
			return nil, err
		}
		var existing []byte
		if err = tx.QueryRowContext(ctx, `SELECT body FROM pm_records WHERE kind='delivery' AND id=?`, d.ID).Scan(&existing); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(existing, &deliveries[i]); err != nil {
			return nil, err
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE pm_records SET body=CAST(json_set(body,'$.delivery_queued',1) AS BLOB) WHERE kind='turn' AND id=?`, t.ID); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return deliveries, nil
}
func splitReply(text string, maxBytes int) []string {
	out := make([]string, 0)
	if strings.TrimSpace(text) == "" {
		return out
	}
	for len(text) > maxBytes {
		n := maxBytes
		for n > 0 && !utf8.RuneStart(text[n]) {
			n--
		}
		out = append(out, text[:n])
		text = text[n:]
	}
	if text != "" {
		out = append(out, text)
	}
	return out
}

// DeliverPending is a bounded drain callable from the existing core lifecycle
// or event path. It creates no watcher and consumes no channel update stream.
// Network uncertainty is retained permanently until explicit reconciliation.
func (s *Service) DeliverPending(ctx context.Context, sender Sender, limit int) ([]Delivery, error) {
	if limit < 1 || limit > 50 {
		return nil, ErrInvalid
	}
	type turnCandidate struct {
		id          string
		hasOrigin   bool
		hasDelivery bool
	}
	queuedTurns := 0
	for pass := 0; pass < 20 && queuedTurns < limit; pass++ {
		rows, err := s.store.database().QueryContext(ctx, deliverPendingTurnsSQL, s.cfg.WorkspaceID, limit)
		if err != nil {
			return nil, err
		}
		batch := make([]turnCandidate, 0, limit)
		for rows.Next() {
			var c turnCandidate
			if err = rows.Scan(&c.id, &c.hasOrigin, &c.hasDelivery); err != nil {
				rows.Close()
				return nil, err
			}
			batch = append(batch, c)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
		if len(batch) == 0 {
			break
		}
		for _, c := range batch {
			if !c.hasOrigin || c.hasDelivery {
				if _, err = s.store.database().ExecContext(ctx, `UPDATE pm_records SET body=CAST(json_set(body,'$.delivery_queued',1) AS BLOB) WHERE kind='turn' AND id=?`, c.id); err != nil {
					return nil, err
				}
				continue
			}
			ds, err := s.QueueTurnDeliveries(ctx, c.id)
			if err != nil {
				return nil, err
			}
			if len(ds) > 0 {
				queuedTurns++
			}
		}
		if len(batch) < limit {
			break
		}
	}
	rows, err := s.store.database().QueryContext(ctx, `SELECT d.id FROM pm_records d WHERE d.kind='delivery' AND d.workspace_id=?
 AND json_extract(d.body,'$.status')='pending_delivery'
 AND (json_extract(d.body,'$.next_retry_at') IS NULL OR json_extract(d.body,'$.next_retry_at') <= ?)
 AND NOT EXISTS(SELECT 1 FROM pm_records prior WHERE prior.kind='delivery' AND prior.workspace_id=d.workspace_id AND prior.parent_id=d.parent_id
 AND prior.rowid<d.rowid AND json_extract(prior.body,'$.status') IN ('sending','unknown','failed'))
 ORDER BY d.rowid LIMIT ?`, s.cfg.WorkspaceID, time.Now().UTC().Format(time.RFC3339Nano), limit)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0)
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	out := make([]Delivery, 0, len(ids))
	for _, id := range ids {
		// A preceding fragment can become unknown during this drain. Recheck it
		// before every network call, not only when selecting the batch.
		var blocked int
		err = s.store.database().QueryRowContext(ctx, `SELECT count(*) FROM pm_records d JOIN pm_records prior ON prior.kind='delivery' AND prior.workspace_id=d.workspace_id AND prior.parent_id=d.parent_id AND prior.rowid<d.rowid WHERE d.kind='delivery' AND d.id=? AND json_extract(prior.body,'$.status')!='delivered'`, id).Scan(&blocked)
		if err != nil {
			return out, err
		}
		if blocked > 0 {
			continue
		}
		d, err := s.SendDelivery(ctx, id, sender)
		if err != nil {
			return out, err
		}
		out = append(out, d)
	}
	return out, nil
}
