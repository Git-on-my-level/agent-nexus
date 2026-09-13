package pm

import (
	"context"
	"errors"
	"fmt"
	"strings"
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
	case Pending:
		deliveryErr := s.checkDelivery(ctx, a)
		if a.Deliverable || deliveryErr == nil {
			return Action{}, fmt.Errorf("%w: this pending action has a delivery path", ErrConflict)
		}
		// Preserve the reason durably so later routing changes cannot erase it.
		if a.Receipt.Detail == "" {
			a.Receipt.Detail = strings.TrimSuffix(deliveryErr.Error(), "; the approval is kept and the action stays pending")
		}
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
		return Action{}, fmt.Errorf("%w: only failed, unresolvable unknown, or undeliverable pending actions can be acknowledged", ErrConflict)
	}
	old := a.Revision
	now := time.Now().UTC()
	a.Status, a.AcknowledgedBy, a.AcknowledgedAt = Acknowledged, p.ActorID, &now
	a.Revision++
	// Existing receipts and attempts are preserved; unsent pending actions retain their delivery limitation.
	if err = s.store.cas(ctx, "action", a.ID, old, a); errors.Is(err, ErrConflict) {
		current, readErr := s.action(ctx, p, id, "pm.read")
		if readErr == nil && current.AcknowledgedBy == p.ActorID && current.AcknowledgedAt != nil {
			return current, nil
		}
	}
	return a, err
}
