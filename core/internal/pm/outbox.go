package pm

import (
	"context"
	"encoding/json"
	"fmt"
	"unicode/utf8"
)

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
	tx, err := s.store.db.BeginTx(ctx, nil)
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
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return deliveries, nil
}
func splitReply(text string, maxBytes int) []string {
	out := make([]string, 0)
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
	rows, err := s.store.db.QueryContext(ctx, `SELECT t.id FROM pm_records t JOIN pm_records c ON c.kind='conversation' AND c.id=t.parent_id
 WHERE t.kind='turn' AND t.workspace_id=? AND json_extract(t.body,'$.status')='delivered'
 AND json_extract(c.body,'$.origin') IS NOT NULL AND NOT EXISTS(SELECT 1 FROM pm_records d WHERE d.kind='delivery' AND d.parent_id=t.id)
 ORDER BY t.rowid LIMIT ?`, s.cfg.WorkspaceID, limit)
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
	for _, id := range ids {
		if _, err = s.QueueTurnDeliveries(ctx, id); err != nil {
			return nil, err
		}
	}
	rows, err = s.store.db.QueryContext(ctx, `SELECT d.id FROM pm_records d WHERE d.kind='delivery' AND d.workspace_id=?
 AND json_extract(d.body,'$.status')='pending_delivery'
 AND NOT EXISTS(SELECT 1 FROM pm_records prior WHERE prior.kind='delivery' AND prior.parent_id=d.parent_id
 AND prior.rowid<d.rowid AND json_extract(prior.body,'$.status') IN ('sending','unknown','failed'))
 ORDER BY d.rowid LIMIT ?`, s.cfg.WorkspaceID, limit)
	if err != nil {
		return nil, err
	}
	ids = ids[:0]
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
		err = s.store.db.QueryRowContext(ctx, `SELECT count(*) FROM pm_records d JOIN pm_records prior ON prior.kind='delivery' AND prior.parent_id=d.parent_id AND prior.rowid<d.rowid WHERE d.kind='delivery' AND d.id=? AND json_extract(prior.body,'$.status')!='delivered'`, id).Scan(&blocked)
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
