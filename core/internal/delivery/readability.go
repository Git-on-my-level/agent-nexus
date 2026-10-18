package delivery

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sort"
	"time"
)

// Per-principal attention counts are materialized, not computed per
// heartbeat. principal_ask_readability holds one row per notified principal
// and open ask: whether that principal can read the ask, as of the access
// epoch it was checked at. The escalation step refreshes it under fixed
// budgets; the heartbeat only reads it with indexed, capped SQL and never
// evaluates access.
//
// Freshness: a new ask reaches every notified principal on the next step
// (ask_readability_queue, filled by a trigger). A new principal is covered by
// a bounded walk of the open-ask ledger. Rows checked before the current
// access epoch are rechecked oldest first, readabilityCheckBudget per step,
// so a count can lag an access change by a bounded number of steps. Content
// is never served from this table: escalation and claims check access live.
const (
	readabilityCheckBudget = 1000
	readabilityQueueBatch  = 10
	readabilityWalkPage    = 200
	readabilityWalkBudget  = 2000
)

// notified is one principal who receives notifications, with the channels
// that reach them.
type notified struct {
	id       string
	channels []string
}

func accessEpoch(ctx context.Context, db *sql.DB) (int64, error) {
	var v int64
	err := db.QueryRowContext(ctx, `SELECT version FROM resource_access_epoch WHERE singleton=1`).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		// Legacy fixtures without access tables have one epoch forever.
		var n int
		if qerr := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='resource_access_epoch'`).Scan(&n); qerr == nil && n == 0 {
			return 0, nil
		}
		return 0, err
	}
	return v, nil
}

// RefreshReadability advances the materialized readability under fixed
// budgets: drop principals who no longer receive notifications, expand newly
// opened asks to every notified principal, walk the ledger for principals
// not yet covered, then evaluate at most readabilityCheckBudget pairs whose
// check predates the current access epoch (unchecked pairs first).
func (s Store) RefreshReadability(ctx context.Context, now time.Time, canRead func(ctx context.Context, principalID, askEventID string) bool) error {
	if canRead == nil || s.DB == nil {
		return nil
	}
	principals, err := s.notifiedPrincipals(ctx)
	if err != nil {
		return err
	}
	ids := make([]string, 0, len(principals))
	want := map[string]bool{}
	for _, p := range principals {
		ids = append(ids, p.id)
		want[p.id] = true
	}
	covered, err := queryStrings(ctx, s.DB, `SELECT principal_id FROM principal_readability_coverage ORDER BY principal_id LIMIT ?`, recipientCap*4)
	if err != nil {
		return err
	}
	for _, id := range covered {
		if want[id] {
			continue
		}
		// One primary-key prefix per departed principal.
		if _, err := s.DB.ExecContext(ctx, `DELETE FROM principal_ask_readability WHERE principal_id=?`, id); err != nil {
			return err
		}
		if _, err := s.DB.ExecContext(ctx, `DELETE FROM principal_readability_coverage WHERE principal_id=?`, id); err != nil {
			return err
		}
	}
	for _, id := range ids {
		if _, err := s.DB.ExecContext(ctx, `INSERT INTO principal_readability_coverage(principal_id) VALUES(?) ON CONFLICT DO NOTHING`, id); err != nil {
			return err
		}
	}
	// Expanding one ask writes one row per principal; spend the same row
	// budget as the ledger walk.
	batch := readabilityWalkBudget / (len(ids) + 1)
	if batch < readabilityQueueBatch {
		batch = readabilityQueueBatch
	}
	queued, err := queryStrings(ctx, s.DB, `SELECT request_event_id FROM ask_readability_queue ORDER BY request_event_id LIMIT ?`, batch)
	if err != nil {
		return err
	}
	encodedIDs, err := json.Marshal(ids)
	if err != nil {
		return err
	}
	for _, ask := range queued {
		// One statement per ask: a row for every notified principal.
		if _, err := s.DB.ExecContext(ctx, `INSERT INTO principal_ask_readability(principal_id, request_event_id, decide_by, escalated)
			SELECT p.value, d.request_event_id, d.decide_by, d.escalated_at IS NOT NULL FROM json_each(?) p JOIN ask_deadlines d ON d.request_event_id=? WHERE true ON CONFLICT DO NOTHING`, string(encodedIDs), ask); err != nil {
			return err
		}
		if _, err := s.DB.ExecContext(ctx, `DELETE FROM ask_readability_queue WHERE request_event_id=?`, ask); err != nil {
			return err
		}
	}
	if err := s.walkCoverage(ctx); err != nil {
		return err
	}
	epoch, err := accessEpoch(ctx, s.DB)
	if err != nil {
		return err
	}
	type pair struct{ principal, ask string }
	rows, err := s.DB.QueryContext(ctx, `SELECT principal_id, request_event_id FROM principal_ask_readability INDEXED BY idx_principal_ask_readability_refresh
		WHERE checked_epoch<? ORDER BY checked_epoch, checked_at LIMIT ?`, epoch, readabilityCheckBudget)
	if err != nil {
		return err
	}
	pairs := []pair{}
	for rows.Next() {
		var p pair
		if err := rows.Scan(&p.principal, &p.ask); err != nil {
			rows.Close()
			return err
		}
		pairs = append(pairs, p)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	checked := stamp(now)
	for _, p := range pairs {
		readable := boolInt(canRead(ctx, p.principal, p.ask))
		if _, err := s.DB.ExecContext(ctx, `UPDATE principal_ask_readability SET readable=?, checked_epoch=?, checked_at=? WHERE principal_id=? AND request_event_id=?`,
			readable, epoch, checked, p.principal, p.ask); err != nil {
			return err
		}
	}
	return nil
}

// walkCoverage pages the open-ask ledger for principals not yet covered,
// at most readabilityWalkBudget rows per step across all of them.
func (s Store) walkCoverage(ctx context.Context) error {
	rows, err := s.DB.QueryContext(ctx, `SELECT principal_id, cursor FROM principal_readability_coverage WHERE complete=0 ORDER BY principal_id LIMIT ?`, recipientCap)
	if err != nil {
		return err
	}
	type cursor struct{ principal, after string }
	pending := []cursor{}
	for rows.Next() {
		var c cursor
		if err := rows.Scan(&c.principal, &c.after); err != nil {
			rows.Close()
			return err
		}
		pending = append(pending, c)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	walked := 0
	for _, c := range pending {
		if walked >= readabilityWalkBudget {
			break
		}
		// One page of the ledger by primary key, inserted in one statement.
		if _, err := s.DB.ExecContext(ctx, `INSERT INTO principal_ask_readability(principal_id, request_event_id, decide_by, escalated)
			SELECT ?, request_event_id, decide_by, escalated_at IS NOT NULL FROM (SELECT request_event_id, decide_by, escalated_at FROM ask_deadlines WHERE request_event_id>? ORDER BY request_event_id LIMIT ?) WHERE true ON CONFLICT DO NOTHING`, c.principal, c.after, readabilityWalkPage); err != nil {
			return err
		}
		var n int
		var last sql.NullString
		if err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*), MAX(request_event_id) FROM (SELECT request_event_id FROM ask_deadlines WHERE request_event_id>? ORDER BY request_event_id LIMIT ?)`, c.after, readabilityWalkPage).Scan(&n, &last); err != nil {
			return err
		}
		walked += n
		next, complete := c.after, 0
		if last.Valid {
			next = last.String
		}
		if n < readabilityWalkPage {
			complete = 1
		}
		if _, err := s.DB.ExecContext(ctx, `UPDATE principal_readability_coverage SET cursor=?, complete=? WHERE principal_id=?`, next, complete, c.principal); err != nil {
			return err
		}
	}
	return nil
}

// principalCounts reads one principal's materialized counts, uncapped: open
// asks it can read, how many are overdue, the earliest unescalated deadline,
// and whether every open ask has been checked. Each figure is an index-only
// aggregate over that principal's readable rows (idx_..._due ranges for the
// counts, one idx_..._earliest seek for the minimum), never a row fetch.
func (s Store) principalCounts(ctx context.Context, principalID string, now time.Time) (due, overdue int, earliest any, complete bool, err error) {
	var earliestText sql.NullString
	err = s.DB.QueryRowContext(ctx, `SELECT
			(SELECT COUNT(*) FROM principal_ask_readability INDEXED BY idx_principal_ask_readability_due WHERE principal_id=?1 AND readable=1),
			(SELECT COUNT(*) FROM principal_ask_readability INDEXED BY idx_principal_ask_readability_due WHERE principal_id=?1 AND readable=1 AND decide_by<=?2),
			(SELECT MIN(decide_by) FROM principal_ask_readability INDEXED BY idx_principal_ask_readability_earliest WHERE principal_id=?1 AND readable=1 AND escalated=0 AND decide_by IS NOT NULL)`,
		principalID, stamp(now)).Scan(&due, &overdue, &earliestText)
	if err != nil {
		return 0, 0, nil, false, err
	}
	if earliestText.Valid && earliestText.String != "" {
		earliest = earliestText.String
	}
	var coverageComplete, unchecked int
	if err = s.DB.QueryRowContext(ctx, `SELECT COALESCE((SELECT complete FROM principal_readability_coverage WHERE principal_id=?),0),
			EXISTS(SELECT 1 FROM principal_ask_readability INDEXED BY idx_principal_ask_readability_refresh WHERE checked_epoch<0 AND principal_id=?)`,
		principalID, principalID).Scan(&coverageComplete, &unchecked); err != nil {
		return 0, 0, nil, false, err
	}
	var queued int
	if err = s.DB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM ask_readability_queue)`).Scan(&queued); err != nil {
		return 0, 0, nil, false, err
	}
	return due, overdue, earliest, coverageComplete == 1 && unchecked == 0 && queued == 0, nil
}

func queryStrings(ctx context.Context, db *sql.DB, query string, args ...any) ([]string, error) {
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func sortedNotified(seen map[string]map[string]bool) []notified {
	out := make([]notified, 0, len(seen))
	for id, channels := range seen {
		n := notified{id: id}
		for c := range channels {
			n.channels = append(n.channels, c)
		}
		sort.Strings(n.channels)
		out = append(out, n)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].id < out[j].id })
	if len(out) > recipientCap {
		out = out[:recipientCap]
	}
	return out
}
