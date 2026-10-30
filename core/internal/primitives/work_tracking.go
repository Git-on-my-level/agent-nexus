package primitives

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"agent-nexus-core/internal/plans"
)

const DefaultWorkPromise = 12 * time.Hour
const AutoCardLeaseTTL = 24 * time.Hour

// The lead is a generic workspace principal selected by the workspace runtime.
// It grants responsibility, never additional resource access.
func (s *Store) SetTrackingLead(lead func() string) { s.trackingLead = lead }

func registerCardTrackingTx(ctx context.Context, tx *accessTx, actor string, card *boardCardRow) error {
	now := time.Now().UTC()
	created := plans.ParseDue(card.CreatedAt)
	if created.IsZero() {
		created = now
	}
	if strings.TrimSpace(card.Assignee.String) == "" && actor != "" {
		owner := "actor:" + stripActor(actor)
		if _, err := tx.ExecContext(ctx, `UPDATE cards SET assignee=? WHERE id=?`, owner, card.CardID); err != nil {
			return err
		}
		card.Assignee = sql.NullString{String: owner, Valid: true}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO work_tracking(card_id,default_at,updated_at) VALUES(?,?,?)`, card.CardID, cardLeaseStamp(created.Add(DefaultWorkPromise)), cardLeaseStamp(now)); err != nil {
		return err
	}
	var agent bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM agents WHERE actor_id=? AND revoked_at IS NULL AND COALESCE(json_extract(metadata_json,'$.principal_kind'),'agent')='agent')`, stripActor(actor)).Scan(&agent); err != nil {
		return err
	}
	if !agent || plans.HealthDone(card.ColumnKey) || card.ArchivedAt.Valid || card.TrashedAt.Valid {
		return nil
	}
	stamp := cardLeaseStamp(now)
	owner := "actor:" + stripActor(actor)
	expires := cardLeaseStamp(now.Add(AutoCardLeaseTTL))
	if _, err := tx.ExecContext(ctx, `INSERT INTO card_leases(card_id,owner,epoch,holder_actor_id,ttl_seconds,acquired_at,renewed_at,expires_at,updated_at) VALUES(?,?,1,?,?,?,?,?,?)`, card.CardID, owner, stripActor(actor), int64(AutoCardLeaseTTL/time.Second), stamp, stamp, expires, stamp); err != nil {
		return err
	}
	_, err := insertCardLeaseEvent(ctx, tx, actor, *card, "card_lease_acquired", "Creating agent acquired the work lease", map[string]any{"owner": owner, "epoch": 1, "ttl_seconds": int64(AutoCardLeaseTTL / time.Second), "expires_at": expires, "source": "create"})
	return err
}

// Principal labels on leases remain backward compatible. Only actor:/agent:
// references are assignments; arbitrary launcher/session labels are not actors.
func trackingActor(ctx context.Context, tx *accessTx, ref string) (string, error) {
	ref = strings.TrimSpace(ref)
	if strings.HasPrefix(ref, "actor:") {
		return stripActor(ref), nil
	}
	if strings.HasPrefix(ref, "agent:") {
		var actor string
		err := tx.QueryRowContext(ctx, `SELECT actor_id FROM agents WHERE id=? AND revoked_at IS NULL`, strings.TrimPrefix(ref, "agent:")).Scan(&actor)
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil
		}
		return actor, err
	}
	return "", nil
}

func trackingActorCanRead(ctx context.Context, tx *accessTx, actor, cardID string) bool {
	if actor == "" {
		return false
	}
	var id string
	// This is a narrower principal check within the same transaction, not an
	// unscoped loader and not a grant to the fallback owner.
	err := tx.QueryRowContext(WithAccessScope(ctx, AccessScope{ActorID: actor}), `SELECT id FROM cards WHERE id=?`, cardID).Scan(&id)
	return err == nil
}

func (s *Store) trackingFallbackTx(ctx context.Context, tx *accessTx, card boardCardRow, previous string) (string, error) {
	var parentOwner string
	err := tx.QueryRowContext(ctx, `SELECT COALESCE(p.assignee,'') FROM cards c LEFT JOIN cards p ON p.id=`+workParentSQL("c")+` WHERE c.id=?`, card.CardID).Scan(&parentOwner)
	if err != nil {
		return "", err
	}
	candidates := []string{firstCardLeaseAssignee(parentOwner)}
	if s.trackingLead != nil {
		if lead := stripActor(strings.TrimSpace(s.trackingLead())); lead != "" {
			candidates = append(candidates, "actor:"+lead)
		}
	}
	// An unconfigured or inaccessible lead cannot silently erase ownership.
	// Retain the existing responsible actor as a last resort and digest it.
	candidates = append(candidates, firstCardLeaseAssignee(card.Assignee.String), "actor:"+stripActor(card.CreatedBy))
	for _, ref := range candidates {
		actor, err := trackingActor(ctx, tx, ref)
		if err != nil {
			return "", err
		}
		if actor != "" && trackingActorCanRead(ctx, tx, actor, card.CardID) {
			return "actor:" + actor, nil
		}
	}
	// Retention is safer than inventing an authority or disclosing a private
	// card to a configured lead who cannot read it.
	if owner := firstCardLeaseAssignee(card.Assignee.String); owner != "" {
		return owner, nil
	}
	return "actor:" + stripActor(card.CreatedBy), nil
}

func (s *Store) fallbackCardTrackingTx(ctx context.Context, tx *accessTx, actor string, card *boardCardRow, previous, reason string, now time.Time) (bool, error) {
	if card.ArchivedAt.Valid || card.TrashedAt.Valid || plans.HealthDone(card.ColumnKey) {
		return false, nil
	}
	owner, err := s.trackingFallbackTx(ctx, tx, *card, previous)
	if err != nil {
		return false, err
	}
	stamp := cardLeaseStamp(now)
	if _, err = tx.ExecContext(ctx, `UPDATE cards SET assignee=?,updated_at=?,updated_by=? WHERE id=?`, owner, stamp, actor, card.CardID); err != nil {
		return false, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO work_tracking(card_id,orphaned_at,fallback_owner,next_digest_at,updated_at) VALUES(?,?,?,?,?) ON CONFLICT(card_id) DO UPDATE SET orphaned_at=excluded.orphaned_at,fallback_owner=excluded.fallback_owner,next_digest_at=excluded.next_digest_at,updated_at=excluded.updated_at`, card.CardID, stamp, owner, cardLeaseStamp(nextTrackingDigest(now)), stamp); err != nil {
		return false, err
	}
	card.Assignee = sql.NullString{String: owner, Valid: true}
	_, err = insertCardLeaseEvent(ctx, tx, actor, *card, "message_posted", "Automatic ownership fallback: "+reason+"; responsibility assigned to "+owner+". Included in the daily digest until a new owner acquires or promises work.", map[string]any{"automatic": true, "source": "work_tracking", "previous_owner": previous, "fallback_owner": owner, "reason": reason})
	return true, err
}

func (input cardHealthInput) trackingDigest(h plans.Health, now time.Time) plans.Health {
	if h.State == plans.PromiseDone {
		return h
	}
	// One owner field: a card awaits an owner only when it has none. A lapsed
	// claim (lease) is an internal detail; after an automatic fallback the card
	// has an owner and says so.
	orphaned := input.Owner == ""
	fellBack := !orphaned && input.OrphanedAt != ""
	if orphaned || fellBack || (h.Promise != nil && h.Promise.Source == "default" && input.ReminderEventID != "") {
		h.State = plans.PromiseDigest
		switch {
		case orphaned:
			h.Reason = "Awaiting an owner; included in the daily digest."
		case fellBack:
			h.Reason = "Ownership fell back to " + input.Owner + " after the previous claim lapsed; included in the daily digest until it is confirmed."
		default:
			h.Reason = "Default promise elapsed; reminder recorded, then daily digest only."
		}
		h.PastPromiseForSeconds = nil
	}
	return h
}
