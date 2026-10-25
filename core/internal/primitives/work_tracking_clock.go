package primitives

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	"agent-nexus-core/internal/delivery"
	"agent-nexus-core/internal/plans"
)

type TrackingDigestCard struct {
	ID     string `json:"-"`
	Ref    string `json:"ref"`
	Title  string `json:"title"`
	Reason string `json:"reason"`
}

type TrackingNotice struct {
	Key, Kind, Owner, EventID, ThreadID, At string
	Cards                                   []TrackingDigestCard
}

func nextTrackingDigest(now time.Time) time.Time {
	next := time.Date(now.Year(), now.Month(), now.Day(), 9, 0, 0, 0, time.UTC)
	if !next.After(now) {
		next = next.Add(24 * time.Hour)
	}
	return next
}

// MaintainWorkTracking never scans history or a workspace inventory on a tick:
// it admits 100 legacy identities once, then at most 50 due default promises.
func (s *Store) MaintainWorkTracking(ctx context.Context, now time.Time) error {
	if err := s.backfillWorkTracking(ctx, now); err != nil {
		return err
	}
	if err := s.routeUnownedWork(ctx, now); err != nil {
		return err
	}
	ids, err := queryStrings(ctx, s.db, `SELECT card_id FROM work_tracking INDEXED BY idx_work_tracking_default WHERE default_at<>'' AND reminder_event_id='' AND default_at<? ORDER BY default_at,card_id LIMIT 50`, cardLeaseStamp(now))
	if err != nil {
		return err
	}
	for _, id := range ids {
		if err := s.remindDefaultPromise(ctx, id, now); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) routeUnownedWork(ctx context.Context, now time.Time) error {
	ids, err := queryStrings(ctx, s.db, `SELECT card_id FROM work_tracking INDEXED BY idx_work_tracking_needs_owner WHERE needs_owner=1 ORDER BY card_id LIMIT 50`)
	if err != nil {
		return err
	}
	for _, id := range ids {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		card, err := s.loadBoardCardByGlobalID(ctx, tx, id, true)
		if err != nil {
			tx.Rollback()
			return err
		}
		owner, due := "", ""
		if !plans.HealthDone(card.ColumnKey) && !card.ArchivedAt.Valid && !card.TrashedAt.Valid && firstCardLeaseAssignee(card.Assignee.String) == "" {
			owner, err = s.trackingFallbackTx(ctx, tx, card, "")
			if err != nil {
				tx.Rollback()
				return err
			}
			due = cardLeaseStamp(nextTrackingDigest(now))
		}
		_, err = tx.ExecContext(ctx, `UPDATE work_tracking SET needs_owner=0,fallback_owner=?,next_digest_at=?,updated_at=? WHERE card_id=? AND needs_owner=1`, owner, due, cardLeaseStamp(now), id)
		if err != nil {
			tx.Rollback()
			return err
		}
		if err = tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) backfillWorkTracking(ctx context.Context, now time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var after string
	var complete bool
	if err := tx.QueryRowContext(ctx, `SELECT after_card_id,complete FROM work_tracking_cursor WHERE singleton=1`).Scan(&after, &complete); err != nil {
		return err
	}
	if complete {
		return nil
	}
	ids, err := queryStrings(ctx, tx, `SELECT id FROM cards WHERE id>? ORDER BY id LIMIT 100`, after)
	if err != nil {
		return err
	}
	for _, id := range ids {
		card, err := s.loadBoardCardByGlobalID(ctx, tx, id, true)
		if errors.Is(err, ErrNotFound) {
			continue
		}
		if err != nil {
			return err
		}
		owner, due := "", ""
		if !plans.HealthDone(card.ColumnKey) && !card.ArchivedAt.Valid && !card.TrashedAt.Valid && firstCardLeaseAssignee(card.Assignee.String) == "" {
			owner, err = s.trackingFallbackTx(ctx, tx, card, "")
			if err != nil {
				return err
			}
			due = cardLeaseStamp(nextTrackingDigest(now))
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO work_tracking(card_id,fallback_owner,next_digest_at,updated_at) VALUES(?,?,?,?) ON CONFLICT DO NOTHING`, id, owner, due, cardLeaseStamp(now)); err != nil {
			return err
		}
	}
	if len(ids) > 0 {
		after = ids[len(ids)-1]
	}
	if _, err = tx.ExecContext(ctx, `UPDATE work_tracking_cursor SET after_card_id=?,complete=? WHERE singleton=1`, after, len(ids) < 100); err != nil {
		return err
	}
	return tx.Commit()
}

func trackingFoldTx(ctx context.Context, tx *accessTx, id string) (plans.PromiseFold, error) {
	inputs, err := briefPromiseInputs(ctx, tx, []string{id}, func(ids []string) (map[string][]plans.PromiseNote, error) { return promiseNotesFor(ctx, tx, ids) })
	if err != nil {
		return plans.PromiseFold{}, err
	}
	if len(inputs) != 1 {
		return plans.PromiseFold{}, ErrNotFound
	}
	return plans.FoldPromise(inputs[0].in), nil
}

func (s *Store) remindDefaultPromise(ctx context.Context, id string, now time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var due, event, orphaned string
	err = tx.QueryRowContext(ctx, `SELECT default_at,reminder_event_id,orphaned_at FROM work_tracking WHERE card_id=?`, id).Scan(&due, &event, &orphaned)
	if err != nil {
		return err
	}
	if due == "" || event != "" || !plans.ParseDue(due).Before(now) {
		return nil
	}
	card, err := s.loadBoardCardByGlobalID(ctx, tx, id, true)
	if errors.Is(err, ErrNotFound) {
		_, err = tx.ExecContext(ctx, `DELETE FROM work_tracking WHERE card_id=?`, id)
		if err != nil {
			return err
		}
		return tx.Commit()
	}
	if err != nil {
		return err
	}
	var cadence bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM work_metadata WHERE card_id=? AND json_type(metadata_json,'$.cadence')='object')`, id).Scan(&cadence); err != nil {
		return err
	}
	fold, err := trackingFoldTx(ctx, tx, id)
	if err != nil {
		return err
	}
	p := fold.Promise
	owner := firstCardLeaseAssignee(card.Assignee.String)
	lease, err := loadCardLeaseRow(ctx, tx, id)
	if err != nil {
		return err
	}
	ended := lease != nil && lease.owner == owner && (lease.releasedAt != "" || lease.expiresAt <= cardLeaseStamp(now))
	if cadence || fold.Done || card.ArchivedAt.Valid || card.TrashedAt.Valid || p == nil || p.Source != "default" || orphaned != "" || ended || owner == "" {
		_, err = tx.ExecContext(ctx, `UPDATE work_tracking SET default_at='',updated_at=? WHERE card_id=?`, cardLeaseStamp(now), id)
		if err != nil {
			return err
		}
		return tx.Commit()
	}
	event, err = insertCardLeaseEvent(ctx, tx, "system", card, "message_posted", "Default 12-hour promise elapsed. Please record progress or hand off; subsequent reminders are included only in the daily digest.", map[string]any{"automatic": true, "source": "default_promise", "promise_at": due, "owner": owner})
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE work_tracking SET reminder_event_id=?,fallback_owner=?,next_digest_at=?,updated_at=? WHERE card_id=? AND reminder_event_id=''`, event, owner, cardLeaseStamp(nextTrackingDigest(now)), cardLeaseStamp(now), id)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) PendingTrackingReminders(ctx context.Context) ([]TrackingNotice, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT t.card_id,t.reminder_event_id,t.fallback_owner,t.updated_at,COALESCE(c.thread_id,''),COALESCE(NULLIF(c.handle,''),c.id),c.title FROM work_tracking t INDEXED BY idx_work_tracking_reminders JOIN cards c ON c.id=t.card_id WHERE t.reminder_event_id<>'' AND t.reminder_sent_at='' ORDER BY t.card_id LIMIT 50`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TrackingNotice{}
	for rows.Next() {
		var n TrackingNotice
		var id, ref, title string
		if err = rows.Scan(&id, &n.EventID, &n.Owner, &n.At, &n.ThreadID, &ref, &title); err != nil {
			return nil, err
		}
		n.Key = "default-promise:" + id
		n.Kind = "default_promise_lapsed"
		n.Cards = []TrackingDigestCard{{ID: id, Ref: "card:" + ref, Title: title, Reason: n.Kind}}
		out = append(out, n)
	}
	return out, rows.Err()
}

func (s *Store) AckTrackingReminder(ctx context.Context, n TrackingNotice, now time.Time) error {
	if len(n.Cards) != 1 {
		return workInvalid("one reminder card required")
	}
	_, err := s.db.ExecContext(ctx, `UPDATE work_tracking SET reminder_sent_at=? WHERE card_id=? AND reminder_event_id=? AND reminder_sent_at=''`, cardLeaseStamp(now), n.Cards[0].ID, n.EventID)
	return err
}

func (s *Store) PendingTrackingDigests(ctx context.Context, now time.Time) ([]TrackingNotice, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT t.card_id,t.fallback_owner,COALESCE(NULLIF(c.handle,''),c.id),c.title,COALESCE(c.thread_id,''),CASE WHEN t.orphaned_at<>'' OR COALESCE(c.assignee,'')='' THEN 'awaiting_owner' ELSE 'default_promise_lapsed' END FROM work_tracking t INDEXED BY idx_work_tracking_digest JOIN cards c ON c.id=t.card_id WHERE t.next_digest_at<>'' AND t.next_digest_at<=? AND c.archived_at IS NULL AND c.trashed_at IS NULL AND c.column_key NOT IN ('done','cancelled') ORDER BY t.next_digest_at,t.fallback_owner,t.card_id LIMIT 50`, cardLeaseStamp(now))
	if err != nil {
		return nil, err
	}
	byOwner := map[string]*TrackingNotice{}
	for rows.Next() {
		var card TrackingDigestCard
		var owner, ref, thread string
		if err = rows.Scan(&card.ID, &owner, &ref, &card.Title, &thread, &card.Reason); err != nil {
			rows.Close()
			return nil, err
		}
		card.Ref = "card:" + ref
		if byOwner[owner] == nil {
			byOwner[owner] = &TrackingNotice{Key: "work-tracking-digest:" + now.UTC().Format("2006-01-02") + ":" + owner, Kind: "work_tracking_digest", Owner: owner, ThreadID: thread, At: cardLeaseStamp(now), Cards: []TrackingDigestCard{}}
		}
		byOwner[owner].Cards = append(byOwner[owner].Cards, card)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return nil, err
	}
	owners := []string{}
	for owner := range byOwner {
		owners = append(owners, owner)
	}
	sort.Strings(owners)
	out := []TrackingNotice{}
	for _, owner := range owners {
		out = append(out, *byOwner[owner])
	}
	return out, nil
}

func (s *Store) AckTrackingDigest(ctx context.Context, n TrackingNotice, now time.Time) error {
	ids := []string{}
	for _, card := range n.Cards {
		ids = append(ids, card.ID)
	}
	raw, _ := json.Marshal(ids)
	_, err := s.db.ExecContext(ctx, `UPDATE work_tracking SET next_digest_at=?,updated_at=? WHERE card_id IN(SELECT value FROM json_each(?)) AND fallback_owner=? AND next_digest_at<>'' AND next_digest_at<=?`, cardLeaseStamp(nextTrackingDigest(now)), cardLeaseStamp(now), string(raw), n.Owner, n.At)
	return err
}

// Validate a pending snapshot immediately before enqueue. Stale ownership or
// a newer explicit promise removes it; permissions are checked as the target.
func (s *Store) ValidateTrackingNotice(ctx context.Context, n TrackingNotice) (TrackingNotice, error) {
	return validateTrackingNotice(ctx, s.db, n)
}

func validateTrackingNotice(ctx context.Context, q briefQueryer, n TrackingNotice) (TrackingNotice, error) {
	actor := stripActor(n.Owner)
	if !strings.HasPrefix(n.Owner, "actor:") {
		n.Cards = nil
		n.ThreadID = ""
		return n, nil
	}
	ctx = WithAccessScope(ctx, AccessScope{ActorID: actor})
	ids := []string{}
	for _, card := range n.Cards {
		ids = append(ids, card.ID)
	}
	raw, _ := json.Marshal(ids)
	rows, err := q.QueryContext(ctx, `SELECT t.card_id,c.thread_id FROM work_tracking t JOIN cards c ON c.id=t.card_id WHERE t.card_id IN(SELECT value FROM json_each(?)) AND t.fallback_owner=? AND c.archived_at IS NULL AND c.trashed_at IS NULL AND c.column_key NOT IN('done','cancelled') AND ((?='default_promise_lapsed' AND t.reminder_event_id=? AND t.reminder_sent_at='' AND t.default_at<>'') OR (?='work_tracking_digest' AND t.next_digest_at<>''))`, string(raw), n.Owner, n.Kind, n.EventID, n.Kind)
	if err != nil {
		return n, err
	}
	valid := map[string]string{}
	for rows.Next() {
		var id, thread string
		if err = rows.Scan(&id, &thread); err != nil {
			rows.Close()
			return n, err
		}
		valid[id] = thread
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return n, err
	}
	kept := []TrackingDigestCard{}
	for _, card := range n.Cards {
		if _, ok := valid[card.ID]; ok {
			kept = append(kept, card)
		}
	}
	n.Cards = kept
	n.ThreadID = ""
	if len(kept) > 0 {
		n.ThreadID = valid[kept[0].ID]
	}
	return n, nil
}

// The brief gets a bounded, principal-owned digest preview. It is not a new
// alert on every read and does not advance the delivery clock.
func (s *Store) TrackingDigestPreview(ctx context.Context, actor string) ([]map[string]any, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT COALESCE(NULLIF(c.handle,''),c.id),c.title,CASE WHEN t.orphaned_at<>'' OR COALESCE(c.assignee,'')='' THEN 'awaiting_owner' ELSE 'default_promise_lapsed' END FROM (SELECT card_id FROM work_tracking_digest_positions WHERE fallback_owner=? ORDER BY next_digest_at,card_id LIMIT 200) positions CROSS JOIN work_tracking t ON t.card_id=positions.card_id CROSS JOIN cards c ON c.id=t.card_id WHERE t.next_digest_at<>'' AND c.archived_at IS NULL AND c.trashed_at IS NULL AND c.column_key NOT IN('done','cancelled') ORDER BY t.next_digest_at,t.card_id LIMIT 50`, "actor:"+stripActor(actor))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var ref, title, reason string
		if err = rows.Scan(&ref, &title, &reason); err != nil {
			return nil, err
		}
		out = append(out, map[string]any{"ref": "card:" + ref, "title": title, "reason": reason})
	}
	return out, rows.Err()
}

func refreshTrackingAfterEventTx(ctx context.Context, tx *accessTx, event preparedEvent) error {
	if event.Type != "message_posted" {
		return nil
	}
	payload := workMap(event.Body["payload"])
	if payload["next"] == nil && len(anyStringSlice(payload["evidence_refs"])) == 0 {
		return nil
	}
	// brief_notes is trigger-maintained in the same insertion transaction and
	// records resolved evidence, not merely an authored evidence-looking URL.
	_, err := tx.ExecContext(ctx, `UPDATE work_tracking SET default_at='',reminder_sent_at=CASE WHEN reminder_event_id<>'' AND reminder_sent_at='' THEN 'canceled' ELSE reminder_sent_at END,next_digest_at=CASE WHEN orphaned_at='' THEN '' ELSE next_digest_at END WHERE card_id IN(SELECT card_id FROM brief_notes WHERE event_id=? AND(next_at IS NOT NULL OR evidence=1))`, workString(event.Body["id"]))
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE work_tracking SET orphaned_at='',fallback_owner='',next_digest_at='' WHERE card_id IN(SELECT n.card_id FROM brief_notes n JOIN cards c ON c.id=n.card_id WHERE n.event_id=? AND n.next_at IS NOT NULL AND replace(COALESCE(c.assignee,''),'actor:','')=n.author)`, workString(event.Body["id"]))
	return err
}

// Ownership recheck and wake insertion share the writer transaction. A new
// assignee/explicit promise that committed first cannot receive a stale wake.
func (s *Store) UpsertTrackingWakeup(ctx context.Context, n TrackingNotice, w AgentWakeup) (bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	valid, err := validateTrackingNotice(ctx, tx, n)
	if err != nil {
		return false, err
	}
	if len(valid.Cards) != len(n.Cards) || len(valid.Cards) == 0 {
		return false, nil
	}
	if err = upsertAgentWakeupTx(ctx, tx, w); err != nil {
		return false, err
	}
	if err = tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

func (s *Store) EnqueueTrackingDelivery(ctx context.Context, n TrackingNotice, request delivery.Request, now time.Time) (bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	valid, err := validateTrackingNotice(ctx, tx, n)
	if err != nil {
		return false, err
	}
	if len(valid.Cards) != len(n.Cards) || len(valid.Cards) == 0 {
		return false, nil
	}
	if _, _, err = delivery.EnqueueTx(ctx, tx, request, now); err != nil {
		return false, err
	}
	if err = tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}
