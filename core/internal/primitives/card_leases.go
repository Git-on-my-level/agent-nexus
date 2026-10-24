package primitives

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

// Card work leases. A lease says which owner (an execution, session or lane
// id) is working a card until expires_at. Every change runs inside one write
// transaction and updates the card's single card_leases row with a
// compare-and-swap on its previous (owner, epoch, expires_at, released_at)
// tuple, so two acquirers can never both win. The epoch increments whenever
// a new tenure starts and is the fencing token callers pass downstream.
//
// This is optimistic concurrency on the server, sufficient for work claims.
// It is not a partition-safe authority lock: a holder that cannot reach the
// server cannot learn it lost the lease until it next renews.

const (
	CardLeaseMinTTL = time.Minute
	CardLeaseMaxTTL = 7 * 24 * time.Hour

	CardLeaseStateNone     = "none"
	CardLeaseStateHeld     = "held"
	CardLeaseStateExpired  = "expired"
	CardLeaseStateReleased = "released"

	CardLeaseErrHeld    = "lease_held"
	CardLeaseErrExpired = "lease_expired"
	CardLeaseErrNotHeld = "lease_not_held"

	cardLeaseReasonMaxRunes = 200
	cardLeaseExpireBatch    = 50
)

var cardLeaseOwnerPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:@/+-]{0,127}$`)

// cardLeaseNow is a seam so tests can move the clock.
var cardLeaseNow = func() time.Time { return time.Now().UTC() }

// Fixed-width stamps keep lexical order equal to time order for the due index.
func cardLeaseStamp(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05.000000000Z") }

// CardLease is the computed view of a card's lease.
type CardLease struct {
	CardID           string
	CardRef          string
	State            string
	Owner            string
	Epoch            int64
	HolderActorID    string
	TTLSeconds       int64
	AcquiredAt       string
	RenewedAt        string
	ExpiresAt        string
	ReleasedAt       string
	RemainingSeconds int64
	ServerTime       string
}

// Map renders the contract CardLease shape.
func (l CardLease) Map() map[string]any {
	out := map[string]any{"card_ref": l.CardRef, "state": l.State, "epoch": l.Epoch, "server_time": l.ServerTime}
	if l.State == CardLeaseStateNone {
		return out
	}
	out["owner"] = l.Owner
	out["holder_actor_id"] = l.HolderActorID
	out["ttl_seconds"] = l.TTLSeconds
	out["acquired_at"] = l.AcquiredAt
	out["renewed_at"] = l.RenewedAt
	out["expires_at"] = l.ExpiresAt
	if l.ReleasedAt != "" {
		out["released_at"] = l.ReleasedAt
	}
	if l.State == CardLeaseStateHeld {
		out["remaining_seconds"] = l.RemainingSeconds
	}
	return out
}

// CardLeaseResult is the outcome of a lease mutation.
type CardLeaseResult struct {
	Lease   CardLease
	Changed bool
	EventID string
}

// CardLeaseError is a deterministic lease refusal. Lease carries the current
// lease so the caller can name the holder and its expiry.
type CardLeaseError struct {
	Code  string
	Lease CardLease
}

func (e *CardLeaseError) Error() string {
	switch e.Code {
	case CardLeaseErrHeld:
		return fmt.Sprintf("card lease is held by %s (epoch %d) until %s", e.Lease.Owner, e.Lease.Epoch, e.Lease.ExpiresAt)
	case CardLeaseErrExpired:
		return fmt.Sprintf("card lease for %s expired at %s; acquire it again for a new epoch", e.Lease.Owner, e.Lease.ExpiresAt)
	default:
		return "card lease is not held"
	}
}

type CardLeaseAcquireInput struct {
	Owner   string
	TTL     time.Duration
	Reason  string
	IfEpoch *int64
}

type CardLeaseRenewInput struct {
	Owner string
	TTL   time.Duration // zero keeps the lease's current TTL
}

type CardLeaseReleaseInput struct {
	Owner  string
	Reason string
}

type cardLeaseRow struct {
	owner, holder                                         string
	epoch, ttl                                            int64
	acquiredAt, renewedAt, expiresAt, releasedAt, expired string
}

func validateCardLeaseOwner(owner string) error {
	if !cardLeaseOwnerPattern.MatchString(owner) {
		return workInvalid("owner must be 1 to 128 characters of letters, digits and ._:@/+- starting with a letter or digit")
	}
	return nil
}

func validateCardLeaseTTL(ttl time.Duration, optional bool) error {
	if optional && ttl == 0 {
		return nil
	}
	if ttl < CardLeaseMinTTL || ttl > CardLeaseMaxTTL {
		return workInvalid("ttl must be between 60s and 7d")
	}
	return nil
}

func validateCardLeaseReason(reason string) error {
	if utf8.RuneCountInString(reason) > cardLeaseReasonMaxRunes || strings.ContainsAny(reason, "\x00\r\n") {
		return workInvalid("reason must be one line of at most 200 characters")
	}
	return nil
}

func cardLeaseRef(card boardCardRow) string {
	if card.Handle.Valid && strings.TrimSpace(card.Handle.String) != "" {
		return "card:" + card.Handle.String
	}
	return "card:" + card.CardID
}

func cardLeaseView(card boardCardRow, row *cardLeaseRow, now time.Time) CardLease {
	l := CardLease{CardID: card.CardID, CardRef: cardLeaseRef(card), State: CardLeaseStateNone, ServerTime: now.UTC().Format(time.RFC3339Nano)}
	if row == nil {
		return l
	}
	l.Owner, l.Epoch, l.HolderActorID, l.TTLSeconds = row.owner, row.epoch, row.holder, row.ttl
	l.AcquiredAt, l.RenewedAt, l.ExpiresAt, l.ReleasedAt = row.acquiredAt, row.renewedAt, row.expiresAt, row.releasedAt
	switch {
	case row.releasedAt != "":
		l.State = CardLeaseStateReleased
	case row.expiresAt <= cardLeaseStamp(now):
		l.State = CardLeaseStateExpired
	default:
		l.State = CardLeaseStateHeld
		if at, err := time.Parse(time.RFC3339Nano, row.expiresAt); err == nil {
			l.RemainingSeconds = int64(at.Sub(now) / time.Second)
		}
	}
	return l
}

func loadCardLeaseRow(ctx context.Context, q queryRower, cardID string) (*cardLeaseRow, error) {
	var r cardLeaseRow
	err := q.QueryRowContext(ctx, `SELECT owner, holder_actor_id, epoch, ttl_seconds, acquired_at, renewed_at, expires_at, released_at, expired_event_id FROM card_leases WHERE card_id=?`, cardID).
		Scan(&r.owner, &r.holder, &r.epoch, &r.ttl, &r.acquiredAt, &r.renewedAt, &r.expiresAt, &r.releasedAt, &r.expired)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// GetCardLease reads a card's lease under the caller's access scope.
func (s *Store) GetCardLease(ctx context.Context, cardID string) (CardLease, error) {
	card, err := s.loadBoardCardByGlobalID(ctx, s.db, cardID, true)
	if err != nil {
		return CardLease{}, err
	}
	row, err := loadCardLeaseRow(ctx, s.db, card.CardID)
	if err != nil {
		return CardLease{}, err
	}
	return cardLeaseView(card, row, cardLeaseNow()), nil
}

type cardLeaseChange struct {
	next      *cardLeaseRow
	eventType string
	summary   string
	payload   map[string]any
}

// mutateCardLease loads the card and its lease inside one write transaction,
// lets decide pick the change, and applies it with a compare-and-swap on the
// previous lease tuple plus the card timeline event, atomically.
func (s *Store) mutateCardLease(ctx context.Context, actor, cardID string, decide func(card boardCardRow, cur *cardLeaseRow, now time.Time) (*cardLeaseChange, error)) (result CardLeaseResult, resultErr error) {
	if strings.TrimSpace(actor) == "" {
		return result, workInvalid("actor required")
	}
	tx, err := mutationTxBegin(ctx, s.db)
	if err != nil {
		return result, err
	}
	defer func() {
		var leaseErr *CardLeaseError
		if resultErr != nil && !errors.As(resultErr, &leaseErr) {
			resultErr = mutationOutcomeAfterBegin(resultErr)
		}
	}()
	defer tx.Rollback()
	card, err := s.loadBoardCardByGlobalID(ctx, tx, cardID, true)
	if err != nil {
		return result, err
	}
	cur, err := loadCardLeaseRow(ctx, tx, card.CardID)
	if err != nil {
		return result, err
	}
	now := cardLeaseNow()
	change, err := decide(card, cur, now)
	if err != nil {
		return result, err
	}
	if change == nil {
		result.Lease = cardLeaseView(card, cur, now)
		return result, nil
	}
	n := change.next
	stamp := cardLeaseStamp(now)
	var res sql.Result
	if cur == nil {
		res, err = tx.ExecContext(ctx, `INSERT INTO card_leases(card_id, owner, epoch, holder_actor_id, ttl_seconds, acquired_at, renewed_at, expires_at, released_at, expired_event_id, updated_at)
			VALUES(?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(card_id) DO NOTHING`,
			card.CardID, n.owner, n.epoch, n.holder, n.ttl, n.acquiredAt, n.renewedAt, n.expiresAt, n.releasedAt, n.expired, stamp)
	} else {
		res, err = tx.ExecContext(ctx, `UPDATE card_leases SET owner=?, epoch=?, holder_actor_id=?, ttl_seconds=?, acquired_at=?, renewed_at=?, expires_at=?, released_at=?, expired_event_id=?, updated_at=?
			WHERE card_id=? AND owner=? AND epoch=? AND expires_at=? AND released_at=?`,
			n.owner, n.epoch, n.holder, n.ttl, n.acquiredAt, n.renewedAt, n.expiresAt, n.releasedAt, n.expired, stamp,
			card.CardID, cur.owner, cur.epoch, cur.expiresAt, cur.releasedAt)
	}
	if err != nil {
		return result, err
	}
	if affected, err := res.RowsAffected(); err != nil {
		return result, err
	} else if affected != 1 {
		return result, ErrConflict
	}
	eventID, err := insertCardLeaseEvent(ctx, tx, actor, card, change.eventType, change.summary, change.payload)
	if err != nil {
		return result, err
	}
	if err := tx.Commit(); err != nil {
		return result, &MutationOutcomeUnknown{Cause: err}
	}
	result.Lease = cardLeaseView(card, n, now)
	result.Changed = true
	result.EventID = eventID
	return result, nil
}

// insertCardLeaseEvent appends the lease audit event to the card timeline in
// the lease transaction and returns its event id.
func insertCardLeaseEvent(ctx context.Context, tx *accessTx, actor string, card boardCardRow, eventType, summary string, payload map[string]any) (string, error) {
	payload["card_id"] = card.CardID
	payload["board_id"] = card.BoardID
	threadID := card.ThreadID.String
	refs := []string{"card:" + card.CardID, "board:" + card.BoardID}
	if threadID != "" {
		refs = append(refs, "thread:"+threadID)
	}
	prepared, err := prepareEventForInsert(actor, map[string]any{"type": eventType, "thread_id": threadID, "summary": summary, "refs": refs, "payload": payload})
	if err != nil {
		return "", err
	}
	if err := insertPreparedEvent(ctx, tx, prepared); err != nil {
		return "", err
	}
	return workString(prepared.Body["id"]), nil
}

func cardLeasePayload(n *cardLeaseRow) map[string]any {
	return map[string]any{"owner": n.owner, "epoch": n.epoch, "ttl_seconds": n.ttl, "expires_at": n.expiresAt}
}

// AcquireCardLease takes the lease for in.Owner. An unexpired lease held by
// another owner refuses with lease_held. The same owner re-acquiring an
// unexpired lease extends it without a new epoch; every other acquisition
// starts a new tenure with epoch+1.
func (s *Store) AcquireCardLease(ctx context.Context, actor, cardID string, in CardLeaseAcquireInput) (CardLeaseResult, error) {
	in.Owner, in.Reason = strings.TrimSpace(in.Owner), strings.TrimSpace(in.Reason)
	if err := validateCardLeaseOwner(in.Owner); err != nil {
		return CardLeaseResult{}, err
	}
	if err := validateCardLeaseTTL(in.TTL, false); err != nil {
		return CardLeaseResult{}, err
	}
	if err := validateCardLeaseReason(in.Reason); err != nil {
		return CardLeaseResult{}, err
	}
	return s.mutateCardLease(ctx, actor, cardID, func(card boardCardRow, cur *cardLeaseRow, now time.Time) (*cardLeaseChange, error) {
		if card.ArchivedAt.Valid || card.TrashedAt.Valid {
			return nil, workInvalid("an archived or trashed card cannot be leased")
		}
		view := cardLeaseView(card, cur, now)
		if in.IfEpoch != nil && *in.IfEpoch != view.Epoch {
			return nil, ErrConflict
		}
		stamp := cardLeaseStamp(now)
		expires := cardLeaseStamp(now.Add(in.TTL))
		ttl := int64(in.TTL / time.Second)
		if view.State == CardLeaseStateHeld {
			if cur.owner != in.Owner {
				return nil, &CardLeaseError{Code: CardLeaseErrHeld, Lease: view}
			}
			next := *cur
			next.holder, next.ttl, next.renewedAt, next.expiresAt = actor, ttl, stamp, expires
			payload := cardLeasePayload(&next)
			if in.Reason != "" {
				payload["reason"] = in.Reason
			}
			return &cardLeaseChange{next: &next, eventType: "card_lease_renewed", summary: "Lease renewed by " + in.Owner, payload: payload}, nil
		}
		next := &cardLeaseRow{owner: in.Owner, holder: actor, epoch: view.Epoch + 1, ttl: ttl, acquiredAt: stamp, renewedAt: stamp, expiresAt: expires}
		payload := cardLeasePayload(next)
		if in.Reason != "" {
			payload["reason"] = in.Reason
		}
		if cur != nil {
			payload["previous_owner"] = cur.owner
			payload["previous_state"] = view.State
			payload["previous_epoch"] = cur.epoch
		}
		return &cardLeaseChange{next: next, eventType: "card_lease_acquired", summary: "Lease acquired by " + in.Owner, payload: payload}, nil
	})
}

// RenewCardLease extends a lease the owner still holds. It never revives an
// expired lease: the owner must acquire again and take a new epoch, so a
// stalled holder always observes that its tenure ended.
func (s *Store) RenewCardLease(ctx context.Context, actor, cardID string, in CardLeaseRenewInput) (CardLeaseResult, error) {
	in.Owner = strings.TrimSpace(in.Owner)
	if err := validateCardLeaseOwner(in.Owner); err != nil {
		return CardLeaseResult{}, err
	}
	if err := validateCardLeaseTTL(in.TTL, true); err != nil {
		return CardLeaseResult{}, err
	}
	return s.mutateCardLease(ctx, actor, cardID, func(card boardCardRow, cur *cardLeaseRow, now time.Time) (*cardLeaseChange, error) {
		view := cardLeaseView(card, cur, now)
		switch {
		case view.State == CardLeaseStateHeld && cur.owner != in.Owner:
			return nil, &CardLeaseError{Code: CardLeaseErrHeld, Lease: view}
		case view.State == CardLeaseStateExpired && cur.owner == in.Owner:
			return nil, &CardLeaseError{Code: CardLeaseErrExpired, Lease: view}
		case view.State != CardLeaseStateHeld:
			return nil, &CardLeaseError{Code: CardLeaseErrNotHeld, Lease: view}
		}
		ttl := cur.ttl
		if in.TTL > 0 {
			ttl = int64(in.TTL / time.Second)
		}
		next := *cur
		next.holder, next.ttl, next.renewedAt, next.expiresAt = actor, ttl, cardLeaseStamp(now), cardLeaseStamp(now.Add(time.Duration(ttl)*time.Second))
		return &cardLeaseChange{next: &next, eventType: "card_lease_renewed", summary: "Lease renewed by " + in.Owner, payload: cardLeasePayload(&next)}, nil
	})
}

// ReleaseCardLease gives up the owner's lease. Releasing a free lease is a
// no-op; releasing another owner's unexpired lease refuses with lease_held.
// The owner may release its own expired lease to close it explicitly.
func (s *Store) ReleaseCardLease(ctx context.Context, actor, cardID string, in CardLeaseReleaseInput) (CardLeaseResult, error) {
	in.Owner, in.Reason = strings.TrimSpace(in.Owner), strings.TrimSpace(in.Reason)
	if err := validateCardLeaseOwner(in.Owner); err != nil {
		return CardLeaseResult{}, err
	}
	if err := validateCardLeaseReason(in.Reason); err != nil {
		return CardLeaseResult{}, err
	}
	return s.mutateCardLease(ctx, actor, cardID, func(card boardCardRow, cur *cardLeaseRow, now time.Time) (*cardLeaseChange, error) {
		view := cardLeaseView(card, cur, now)
		switch view.State {
		case CardLeaseStateNone, CardLeaseStateReleased:
			return nil, nil
		case CardLeaseStateHeld:
			if cur.owner != in.Owner {
				return nil, &CardLeaseError{Code: CardLeaseErrHeld, Lease: view}
			}
		case CardLeaseStateExpired:
			if cur.owner != in.Owner {
				return nil, nil // Nobody holds it; the expired owner is not ours to close.
			}
		}
		next := *cur
		next.holder, next.releasedAt = actor, cardLeaseStamp(now)
		payload := map[string]any{"owner": next.owner, "epoch": next.epoch, "state_before": view.State}
		if in.Reason != "" {
			payload["reason"] = in.Reason
		}
		return &cardLeaseChange{next: &next, eventType: "card_lease_released", summary: "Lease released by " + in.Owner, payload: payload}, nil
	})
}

func firstCardLeaseAssignee(raw string) string {
	if refs := boardCardAssigneeRefs(raw); len(refs) > 0 {
		return refs[0]
	}
	return ""
}

// ExpiredCardLease is one expiry the clock recorded. Assignee is the card's
// first assignee as a typed ref (actor: or agent:), or empty.
type ExpiredCardLease struct {
	CardID   string
	CardRef  string
	ThreadID string
	Assignee string
	Owner    string
	Epoch    int64
	Expires  string
	EventID  string
}

// ExpireDueCardLeases records card_lease_expired once per epoch for leases
// whose expires_at passed without renewal or release. It is the lease
// clock's step: a bounded seek on idx_card_leases_due, then one write
// transaction per lease that re-checks the row before recording. The
// context must carry no access scope (trusted internal operation).
func (s *Store) ExpireDueCardLeases(ctx context.Context, actor string, limit int) ([]ExpiredCardLease, error) {
	if limit <= 0 || limit > cardLeaseExpireBatch {
		limit = cardLeaseExpireBatch
	}
	now := cardLeaseNow()
	rows, err := s.db.QueryContext(ctx, `SELECT card_id FROM card_leases INDEXED BY idx_card_leases_due WHERE released_at='' AND expired_event_id='' AND expires_at<=? ORDER BY expires_at, card_id LIMIT ?`, cardLeaseStamp(now), limit)
	if err != nil {
		return nil, err
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := []ExpiredCardLease{}
	for _, id := range ids {
		expired, ok, err := s.expireCardLease(ctx, actor, id, now)
		if err != nil {
			return out, err
		}
		if ok {
			out = append(out, expired)
		}
	}
	return out, nil
}

func (s *Store) expireCardLease(ctx context.Context, actor, cardID string, now time.Time) (ExpiredCardLease, bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ExpiredCardLease{}, false, err
	}
	defer tx.Rollback()
	cur, err := loadCardLeaseRow(ctx, tx, cardID)
	if err != nil || cur == nil || cur.releasedAt != "" || cur.expired != "" || cur.expiresAt > cardLeaseStamp(now) {
		return ExpiredCardLease{}, false, err
	}
	card, err := s.loadBoardCardByGlobalID(ctx, tx, cardID, true)
	if errors.Is(err, ErrNotFound) {
		// The card left the board graph; close the row without an event.
		_, err = tx.ExecContext(ctx, `UPDATE card_leases SET expired_event_id='-' WHERE card_id=? AND epoch=?`, cardID, cur.epoch)
		if err == nil {
			err = tx.Commit()
		}
		return ExpiredCardLease{}, false, err
	}
	if err != nil {
		return ExpiredCardLease{}, false, err
	}
	payload := map[string]any{"owner": cur.owner, "epoch": cur.epoch, "ttl_seconds": cur.ttl, "expires_at": cur.expiresAt, "renewed_at": cur.renewedAt}
	eventID, err := insertCardLeaseEvent(ctx, tx, actor, card, "card_lease_expired", "Lease held by "+cur.owner+" expired", payload)
	if err != nil {
		return ExpiredCardLease{}, false, err
	}
	if eventID == "" {
		eventID = "-"
	}
	res, err := tx.ExecContext(ctx, `UPDATE card_leases SET expired_event_id=? WHERE card_id=? AND epoch=? AND owner=? AND expires_at=? AND released_at='' AND expired_event_id=''`, eventID, cardID, cur.epoch, cur.owner, cur.expiresAt)
	if err != nil {
		return ExpiredCardLease{}, false, err
	}
	if n, err := res.RowsAffected(); err != nil || n != 1 {
		return ExpiredCardLease{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return ExpiredCardLease{}, false, err
	}
	return ExpiredCardLease{
		CardID: card.CardID, CardRef: cardLeaseRef(card), ThreadID: card.ThreadID.String, Assignee: firstCardLeaseAssignee(card.Assignee.String),
		Owner: cur.owner, Epoch: cur.epoch, Expires: cur.expiresAt, EventID: eventID,
	}, true, nil
}
