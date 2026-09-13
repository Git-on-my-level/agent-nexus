package pm

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// AcknowledgeAction records human handling, not a claim about source delivery.
func (s *Service) AcknowledgeAction(ctx context.Context, p Principal, id string) (Action, error) {
	if !p.Human {
		return Action{}, ErrForbidden
	}
	a, err := s.action(ctx, p, id, "pm.read")
	if err != nil {
		return Action{}, err
	}
	d, err := s.decision(ctx, p, a.DecisionID, "pm.approve")
	if err != nil {
		return Action{}, err
	}
	if a.ActorID != p.ActorID || d.ActorID != p.ActorID || d.ActionID != a.ID {
		return Action{}, ErrForbidden
	}
	if a.AcknowledgedAt != nil {
		return a, nil
	}
	switch a.Status {
	case Failed:
	case Unknown:
		if s.deps.Reconcile != nil {
			bounded, cancel := context.WithTimeout(ctx, s.cfg.TurnTimeout)
			receipt, readErr := s.deps.Reconcile(bounded, a)
			cancel()
			if ctx.Err() != nil {
				return Action{}, ctx.Err()
			}
			if readErr == nil && validateReceipt(receipt, true) == nil && receipt.Status != Unknown {
				return Action{}, fmt.Errorf("%w: read-back can advance this action; reconcile it first", ErrConflict)
			}
		}
	default:
		return Action{}, fmt.Errorf("%w: only failed or unresolvable unknown actions can be acknowledged", ErrConflict)
	}
	old := a.Revision
	now := time.Now().UTC()
	a.Status, a.AcknowledgedBy, a.AcknowledgedAt = Acknowledged, p.ActorID, &now
	a.Revision++
	// Receipts and attempts remain byte-for-byte equivalent as evidence.
	if err = s.store.cas(ctx, "action", a.ID, old, a); errors.Is(err, ErrConflict) {
		current, readErr := s.action(ctx, p, id, "pm.read")
		if readErr == nil && current.AcknowledgedBy == p.ActorID && current.AcknowledgedAt != nil {
			return current, nil
		}
	}
	return a, err
}
