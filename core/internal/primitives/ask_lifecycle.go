package primitives

import (
	"agent-nexus-core/internal/loopstats"
	"agent-nexus-core/internal/workprojection"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"strings"
	"time"
)

type askBackoffState struct {
	until            time.Time
	failures         int
	askCursor        string
	passHadTransient bool
}

const askCloseQueueStepCards = 8

func permanentAskWithdrawalReason(err error, actor, askID string) (string, bool) {
	var syntaxErr *json.SyntaxError
	var typeErr *json.UnmarshalTypeError
	switch {
	case errors.Is(err, ErrNotFound), errors.Is(err, sql.ErrNoRows):
		return "not_found", true
	case errors.Is(err, ErrForbidden):
		return "forbidden", true
	case errors.Is(err, ErrInvalidResourceRef):
		return "invalid_ref", true
	case strings.TrimSpace(actor) == "" || strings.TrimSpace(askID) == "" || errors.As(err, &syntaxErr) || errors.As(err, &typeErr):
		return "invalid_request", true
	default:
		return "", false
	}
}

func (s *Store) askCloseCardBackedOff(card string, now time.Time) bool {
	s.askLifecycleMu.Lock()
	defer s.askLifecycleMu.Unlock()
	state, ok := s.askCloseBackoff[card]
	return ok && now.Before(state.until)
}

func (s *Store) nextAskCloseCardCursor(card string) string {
	s.askLifecycleMu.Lock()
	defer s.askLifecycleMu.Unlock()
	return s.askCloseBackoff[card].askCursor
}

func (s *Store) advanceAskCloseCardCursor(card, askCursor string, hadTransient bool) {
	s.askLifecycleMu.Lock()
	defer s.askLifecycleMu.Unlock()
	if s.askCloseBackoff == nil {
		s.askCloseBackoff = map[string]askBackoffState{}
	}
	state := s.askCloseBackoff[card]
	state.askCursor = askCursor
	state.passHadTransient = state.passHadTransient || hadTransient
	s.askCloseBackoff[card] = state
}

func (s *Store) askCloseCardPassHadTransient(card string) bool {
	s.askLifecycleMu.Lock()
	defer s.askLifecycleMu.Unlock()
	return s.askCloseBackoff[card].passHadTransient
}

func (s *Store) recordAskCloseBackoff(card, askCursor string, now time.Time) time.Duration {
	s.askLifecycleMu.Lock()
	defer s.askLifecycleMu.Unlock()
	if s.askCloseBackoff == nil {
		s.askCloseBackoff = map[string]askBackoffState{}
	}
	state := s.askCloseBackoff[card]
	state.failures++
	shift := state.failures - 1
	if shift > 6 {
		shift = 6
	}
	delay := time.Duration(1<<shift) * time.Second
	if delay > time.Minute {
		delay = time.Minute
	}
	state.until = now.Add(delay)
	state.askCursor = askCursor
	state.passHadTransient = false
	s.askCloseBackoff[card] = state
	return delay
}

func (s *Store) clearAskCloseBackoff(card string) {
	s.askLifecycleMu.Lock()
	defer s.askLifecycleMu.Unlock()
	delete(s.askCloseBackoff, card)
}

func (s *Store) nextAskCloseCursor() string {
	s.askLifecycleMu.Lock()
	defer s.askLifecycleMu.Unlock()
	return s.askCloseCursor
}

func (s *Store) setAskCloseCursor(cursor string) {
	s.askLifecycleMu.Lock()
	defer s.askLifecycleMu.Unlock()
	s.askCloseCursor = cursor
}

// Each maintenance step touches at most 200 indexed candidates. Closed subjects
// and expiry survive restart; responses and withdrawal compete for one claim.
func (s *Store) MaintainAskLifecycleBatch(ctx context.Context) error {
	ctx, finish := loopstats.Start(ctx, "ask_lifecycle")
	defer finish()
	type pending struct{ id, actor, reason string }
	withdrawBatch := func(card string, batch []pending) (bool, error) {
		hadTransient := false
		loopstats.Rows(ctx, len(batch))
		for _, p := range batch {
			_, err := s.appendHumanAttentionWithdrawal(ctx, p.actor, p.id, map[string]any{"payload": map[string]any{"reason": p.reason}}, p.reason == "subject_closed")
			if err == nil {
				continue
			}
			if errors.Is(err, ErrHumanAttentionAlreadyResponded) {
				if _, uerr := s.db.ExecContext(ctx, `UPDATE ask_subjects SET open=0 WHERE ask_id=?`, p.id); uerr != nil {
					return false, uerr
				}
				continue
			}
			if ctx.Err() != nil {
				return false, ctx.Err()
			}
			if reason, permanent := permanentAskWithdrawalReason(err, p.actor, p.id); permanent {
				log.Printf("ask lifecycle: skipping permanent withdrawal failure ask=%s card=%s reason=%s: %v", p.id, card, reason, err)
				if _, uerr := s.db.ExecContext(ctx, `UPDATE ask_subjects SET open=0,close_reason=? WHERE ask_id=?`, reason, p.id); uerr != nil {
					return false, uerr
				}
				continue
			}
			hadTransient = true
			if card == "" {
				delay := s.recordAskCloseBackoff(p.id, "", time.Now())
				log.Printf("ask lifecycle: backing off transient withdrawal failure ask=%s card=%s backoff=%s: %v", p.id, card, delay, err)
				if _, uerr := s.db.ExecContext(ctx, `UPDATE ask_subjects SET due_at=julianday('now')+(?/86400.0) WHERE ask_id=? AND open=1`, delay.Seconds(), p.id); uerr != nil {
					return false, uerr
				}
			} else {
				log.Printf("ask lifecycle: transient withdrawal failure ask=%s card=%s: %v", p.id, card, err)
			}
		}
		return hadTransient, nil
	}

	queryCardBatch := func(targetCard, askCursor string) ([]pending, error) {
		rows, err := s.db.QueryContext(ctx, `SELECT ask_id,requester,'subject_closed' FROM ask_subjects WHERE card_id=? AND open=1 AND ask_id>? ORDER BY ask_id LIMIT 200`, targetCard, askCursor)
		if err != nil {
			return nil, err
		}
		var batch []pending
		for rows.Next() {
			var p pending
			if err = rows.Scan(&p.id, &p.actor, &p.reason); err != nil {
				rows.Close()
				return nil, err
			}
			batch = append(batch, p)
		}
		err = rows.Err()
		rows.Close()
		return batch, err
	}

	cursor := s.nextAskCloseCursor()
	wrapped := cursor == ""
	visited := map[string]bool{}

	for step := 0; step < askCloseQueueStepCards; step++ {
		var card string
		var err error
		if cursor == "" {
			err = s.db.QueryRowContext(ctx, `SELECT card_id FROM ask_subject_close_queue ORDER BY card_id LIMIT 1`).Scan(&card)
		} else {
			err = s.db.QueryRowContext(ctx, `SELECT card_id FROM ask_subject_close_queue WHERE card_id>? ORDER BY card_id LIMIT 1`, cursor).Scan(&card)
		}
		if errors.Is(err, sql.ErrNoRows) {
			if !wrapped {
				cursor = ""
				wrapped = true
				s.setAskCloseCursor("")
				continue
			}
			s.setAskCloseCursor("")
			break
		}
		if err != nil {
			return err
		}
		if visited[card] {
			break
		}
		visited[card] = true
		cursor = card
		s.setAskCloseCursor(cursor)

		// A queued close is a hint; a valid newer observation may reopen work.
		result, e := s.db.ExecContext(ctx, `DELETE FROM ask_subject_close_queue WHERE card_id=? AND EXISTS(SELECT 1 FROM cards c LEFT JOIN work_metadata m ON m.card_id=c.id LEFT JOIN work_observations o ON o.id=m.latest_observation_id WHERE c.id=? AND NOT (`+workprojection.ClosedSQL()+`))`, card, card)
		if e != nil {
			return e
		}
		if n, e := result.RowsAffected(); e != nil {
			return e
		} else if n > 0 {
			s.clearAskCloseBackoff(card)
			continue
		}

		if s.askCloseCardBackedOff(card, time.Now()) {
			continue
		}

		askCursor := s.nextAskCloseCardCursor(card)
		batch, err := queryCardBatch(card, askCursor)
		if err != nil {
			return err
		}
		if len(batch) == 0 && askCursor != "" {
			askCursor = ""
			batch, err = queryCardBatch(card, "")
			if err != nil {
				return err
			}
		}
		hadTransient, err := withdrawBatch(card, batch)
		if err != nil {
			return err
		}
		hasMoreAfter := false
		lastAskID := ""
		if len(batch) > 0 {
			lastAskID = batch[len(batch)-1].id
		}
		if len(batch) == 200 {
			var more int
			if err = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM (SELECT 1 FROM ask_subjects WHERE card_id=? AND open=1 AND ask_id>? LIMIT 1)`, card, lastAskID).Scan(&more); err != nil {
				return err
			}
			if more > 0 {
				hasMoreAfter = true
				s.advanceAskCloseCardCursor(card, lastAskID, hadTransient)
			}
		}
		if !hasMoreAfter {
			if hadTransient || s.askCloseCardPassHadTransient(card) {
				delay := s.recordAskCloseBackoff(card, lastAskID, time.Now())
				log.Printf("ask lifecycle: backing off close-queue card=%s backoff=%s", card, delay)
			} else {
				if _, err = s.db.ExecContext(ctx, `DELETE FROM ask_subject_close_queue WHERE card_id=? AND NOT EXISTS(SELECT 1 FROM ask_subjects WHERE card_id=? AND open=1)`, card, card); err != nil {
					return err
				}
				s.clearAskCloseBackoff(card)
			}
		}
	}

	rows, err := s.db.QueryContext(ctx, `SELECT ask_id,requester,close_reason FROM ask_subjects WHERE open=1 AND due_at<=julianday('now') ORDER BY due_at,ask_id LIMIT 200`)
	if err != nil {
		return err
	}
	var expired []pending
	for rows.Next() {
		var p pending
		if err = rows.Scan(&p.id, &p.actor, &p.reason); err != nil {
			rows.Close()
			return err
		}
		expired = append(expired, p)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	_, err = withdrawBatch("", expired)
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
