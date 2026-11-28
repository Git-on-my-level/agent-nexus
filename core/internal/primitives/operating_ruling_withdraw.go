package primitives

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
)

// MaxOperatingRulingWithdrawals bounds one withdrawal request.
const MaxOperatingRulingWithdrawals = 200

// ErrRulingNotYours rejects an agent withdrawing a proposal it did not make.
var ErrRulingNotYours = errors.New("only the proposer or a human may withdraw this proposal")

// WithdrawOperatingRulings removes unconfirmed proposals together with their
// names, so a merged ruling can carry those names. Nothing was ever decided on
// a proposal, so no history is lost. Only proposals can be withdrawn: an
// active or retired ruling changes through a human revoke or a superseding
// ruling. An agent withdraws only its own proposals; a human may withdraw any.
//
// The set is withdrawn atomically. A proposal outside the set that still
// relates to one inside it fails the request, so no remaining row points at a
// missing ruling.
func (s *Store) WithdrawOperatingRulings(ctx context.Context, actor string, human bool, ids []string) ([]OperatingRuling, error) {
	if len(ids) == 0 || len(ids) > MaxOperatingRulingWithdrawals {
		return nil, workInvalid(fmt.Sprintf("withdraw 1 to %d proposals at a time", MaxOperatingRulingWithdrawals))
	}
	set := make(map[string]bool, len(ids))
	for _, id := range ids {
		if !ValidOperatingRulingID(id) || set[id] {
			return nil, workInvalid("withdraw distinct ruling date IDs")
		}
		set[id] = true
	}
	sorted := append([]string(nil), ids...)
	sort.Strings(sorted)
	raw, _ := json.Marshal(sorted)
	var out []OperatingRuling
	err := s.db.WriteTransaction(ctx, func(ctx context.Context, tx *accessTx) error {
		if _, err := tx.ExecContext(ctx, `UPDATE standing_ruling_revision SET version=version WHERE singleton=1`); err != nil {
			return err
		}
		out = out[:0]
		for _, id := range sorted {
			r, err := scanOperatingRuling(tx.QueryRowContext(ctx, "SELECT "+operatingRulingColumns+" FROM standing_rulings WHERE ruling_id=?", id))
			if err != nil {
				return fmt.Errorf("%w: %s", err, id)
			}
			if r.Status != "proposed" {
				return fmt.Errorf("%w: %s is %s; only a proposal can be withdrawn", ErrConflict, id, r.Status)
			}
			if !human && r.CreatedBy != actor {
				return fmt.Errorf("%w: %s", ErrRulingNotYours, id)
			}
			out = append(out, r)
		}
		var referrer, target string
		err := tx.QueryRowContext(ctx, `SELECT s.ruling_id, w.value FROM standing_rulings s, json_each(?) w
		 WHERE s.ruling_id IS NOT NULL AND s.ruling_id NOT IN (SELECT value FROM json_each(?))
		 AND (s.superseded_by=w.value OR s.superseded_by_hint=w.value
		  OR EXISTS (SELECT 1 FROM json_each(s.relations_json) rel WHERE json_extract(rel.value,'$.ruling_id')=w.value))
		 ORDER BY s.ruling_id LIMIT 1`, string(raw), string(raw)).Scan(&referrer, &target)
		if err == nil {
			return fmt.Errorf("%w: %s still refers to %s; withdraw both together", ErrConflict, referrer, target)
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM standing_ruling_names WHERE ruling_id IN (SELECT value FROM json_each(?))`, string(raw)); err != nil {
			return err
		}
		// The delete trigger bumps the projection revision.
		_, err = tx.ExecContext(ctx, `DELETE FROM standing_rulings WHERE ruling_id IN (SELECT value FROM json_each(?))`, string(raw))
		return err
	})
	if err != nil {
		return nil, err
	}
	for i := range out {
		out[i].Status = "withdrawn"
	}
	return out, nil
}
