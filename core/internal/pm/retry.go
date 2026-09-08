package pm

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

type deliveryRetry struct {
	DeliveryID          string    `json:"delivery_id"`
	ActorID             string    `json:"actor_id"`
	ReviewedRevision    int       `json:"reviewed_revision"`
	NonDeliveryEvidence Receipt   `json:"non_delivery_evidence"`
	CreatedAt           time.Time `json:"created_at"`
}

// RetryFailedDelivery is an explicit human operator recovery operation. The
// original unknown send must first be reconciled to independently established
// non-delivery. It records a separate immutable retry approval before I/O.
// Replaying the retry approval can never reopen a later delivered attempt.
func (s *Service) RetryFailedDelivery(ctx context.Context, p Principal, id string, revision int, requestKey string) (Delivery, error) {
	if !p.Human {
		return Delivery{}, ErrForbidden
	}
	if err := s.authorize(ctx, p, "pm.delivery.retry", ""); err != nil {
		return Delivery{}, err
	}
	if !validText(requestKey, 256) {
		return Delivery{}, ErrInvalid
	}
	var d Delivery
	if err := s.store.get(ctx, "delivery", id, &d); err != nil {
		return Delivery{}, err
	}
	if d.WorkspaceID != p.WorkspaceID {
		return Delivery{}, ErrForbidden
	}
	b, err := s.ResolveBinding(ctx, d.Origin)
	if err != nil || b.ActorID != d.ActorID {
		return Delivery{}, ErrForbidden
	}
	key := stableID("delivery-retry", id, requestKey)
	var prior deliveryRetry
	if err = s.store.get(ctx, "delivery_retry", key, &prior); err == nil {
		if prior.ActorID != p.ActorID || prior.ReviewedRevision != revision {
			return Delivery{}, ErrConflict
		}
		return d, nil
	} else if !errors.Is(err, ErrNotFound) {
		return Delivery{}, err
	}
	if d.Status != Failed || d.Revision != revision {
		return Delivery{}, ErrConflict
	}
	if !d.Receipt.IndependentlyVerified || len(d.Receipt.EvidenceRefs) == 0 || d.Receipt.ExternalID == "" {
		return Delivery{}, ErrInvalid
	}
	if len(d.Attempts) >= 3 {
		return Delivery{}, ErrBusy
	}
	approval := deliveryRetry{DeliveryID: id, ActorID: p.ActorID, ReviewedRevision: revision, NonDeliveryEvidence: d.Receipt, CreatedAt: time.Now().UTC()}
	d.Status = Pending
	d.Revision++
	tx, err := s.store.db.BeginTx(ctx, nil)
	if err != nil {
		return Delivery{}, err
	}
	defer tx.Rollback()
	raw, err := json.Marshal(d)
	if err != nil {
		return Delivery{}, err
	}
	result, err := tx.ExecContext(ctx, `UPDATE pm_records SET revision=revision+1,body=? WHERE kind='delivery' AND id=? AND revision=?`, raw, id, revision)
	if err != nil {
		return Delivery{}, err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return Delivery{}, err
	}
	if n != 1 {
		return Delivery{}, ErrConflict
	}
	raw, err = json.Marshal(approval)
	if err != nil {
		return Delivery{}, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO pm_records(kind,id,workspace_id,actor_id,parent_id,revision,body) VALUES('delivery_retry',?,?,?,?,1,?)`, key, d.WorkspaceID, p.ActorID, id, raw)
	if err != nil {
		return Delivery{}, err
	}
	if err = tx.Commit(); err != nil {
		return Delivery{}, err
	}
	return d, nil
}
