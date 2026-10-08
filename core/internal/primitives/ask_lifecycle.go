package primitives

import (
	"agent-nexus-core/internal/workprojection"
	"context"
	"database/sql"
	"errors"
	"time"
)

// Each maintenance step touches at most 200 indexed candidates. Closed subjects
// and expiry survive restart; responses and withdrawal compete for one claim.
func (s *Store) MaintainAskLifecycleBatch(ctx context.Context) error {
	type pending struct{ id, actor, reason string }
	var card string
	err := s.db.QueryRowContext(ctx, `SELECT card_id FROM ask_subject_close_queue ORDER BY card_id LIMIT 1`).Scan(&card)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if card != "" {
		// A queued close is a hint; a valid newer observation may reopen work.
		result, e := s.db.ExecContext(ctx, `DELETE FROM ask_subject_close_queue WHERE card_id=? AND NOT EXISTS(SELECT 1 FROM cards c LEFT JOIN work_metadata m ON m.card_id=c.id LEFT JOIN work_observations o ON o.id=m.latest_observation_id WHERE c.id=? AND `+workprojection.ClosedSQL()+`)`, card, card)
		if e != nil {
			return e
		}
		if n, e := result.RowsAffected(); e != nil {
			return e
		} else if n > 0 {
			return nil
		}
	}
	query := `SELECT ask_id,requester,close_reason FROM ask_subjects WHERE open=1 AND due_at<=julianday('now') ORDER BY due_at,ask_id LIMIT 200`
	args := []any{}
	if card != "" {
		query = `SELECT ask_id,requester,'subject_closed' FROM ask_subjects WHERE card_id=? AND open=1 ORDER BY ask_id LIMIT 200`
		args = append(args, card)
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return err
	}
	var batch []pending
	for rows.Next() {
		var p pending
		if err = rows.Scan(&p.id, &p.actor, &p.reason); err != nil {
			rows.Close()
			return err
		}
		batch = append(batch, p)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, p := range batch {
		_, err = s.appendHumanAttentionWithdrawal(ctx, p.actor, p.id, map[string]any{"payload": map[string]any{"reason": p.reason}}, p.reason == "subject_closed")
		if err != nil && !errors.Is(err, ErrHumanAttentionAlreadyResponded) {
			return err
		}
	}
	if card != "" && len(batch) < 200 {
		_, err = s.db.ExecContext(ctx, `DELETE FROM ask_subject_close_queue WHERE card_id=? AND NOT EXISTS(SELECT 1 FROM ask_subjects WHERE card_id=? AND open=1)`, card, card)
	}
	return err
}
func (s *Store) RunAskLifecycle(ctx context.Context, report func(error)) {
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			if err := s.MaintainAskLifecycleBatch(ctx); err != nil && report != nil {
				report(err)
			}
		}
	}
}
