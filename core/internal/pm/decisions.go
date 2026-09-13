package pm

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"time"
)

func (s *Service) ProposeDecision(ctx context.Context, p Principal, in DecisionInput) (Decision, error) {
	return s.proposeDecision(ctx, p, in, "", p.ActorID)
}
func (s *Service) proposeDecision(ctx context.Context, p Principal, in DecisionInput, turnID, proposedBy string) (Decision, error) {
	if err := s.authorize(ctx, p, "pm.propose", in.WorkRef); err != nil {
		return Decision{}, err
	}
	if !validText(in.RequestKey, 256) || !validText(in.WorkRef, 512) || !validText(in.Instruction, 16000) || !validText(in.Scope, 256) || !validText(in.TargetRevision, 512) {
		return Decision{}, ErrInvalid
	}
	if err := s.validateResolution(ctx, p, in.Scope, in.Payload); err != nil {
		return Decision{}, err
	}
	// Public callers cannot inject an origin to redirect an approval notification.
	if in.Origin != nil {
		binding, err := s.ResolveBinding(ctx, *in.Origin)
		if err != nil || binding.ActorID != p.ActorID || binding.WorkspaceID != p.WorkspaceID {
			return Decision{}, ErrForbidden
		}
	}
	d := Decision{ID: stableID("decision", p.WorkspaceID, p.ActorID, in.RequestKey), WorkspaceID: p.WorkspaceID, ActorID: p.ActorID, WorkRef: in.WorkRef, Instruction: in.Instruction, Payload: in.Payload, Scope: in.Scope, TargetRevision: in.TargetRevision, Status: AwaitingAnswer, Revision: 1, Origin: in.Origin, CreatedAt: time.Now().UTC()}
	d.ProposedBy = proposedBy
	d.OriginKind = "human"
	if turnID != "" {
		d.OriginKind, d.TurnID = "pm_turn", turnID
	} else if in.Origin != nil {
		d.OriginKind = "channel"
	}
	d, inserted, err := s.store.proposeDecision(ctx, d, turnID)
	if err != nil {
		return Decision{}, err
	}
	if !inserted {
		return s.decisionForReader(ctx, p, d), nil
	}
	if d.Origin != nil {
		if _, err = s.QueueDecisionCard(ctx, d, ""); err != nil {
			return d, err
		}
	}
	return s.decisionForReader(ctx, p, d), nil
}
func (s *Service) decision(ctx context.Context, p Principal, id, permission string) (Decision, error) {
	var d Decision
	if err := s.store.get(ctx, "decision", id, &d); err != nil {
		return d, err
	}
	if d.WorkspaceID != p.WorkspaceID || (permission == "pm.approve" && d.ActorID != p.ActorID) {
		return Decision{}, ErrForbidden
	}
	if err := s.authorize(ctx, p, permission, d.WorkRef); err != nil {
		return Decision{}, err
	}
	return s.decisionForReader(ctx, p, d), nil
}
func (s *Service) ListDecisions(ctx context.Context, p Principal) ([]Decision, error) {
	if err := s.authorize(ctx, p, "pm.read", ""); err != nil {
		return nil, err
	}
	ds, err := listRecords[Decision](ctx, s.store, "decision", p.WorkspaceID, "", "")
	if err != nil {
		return nil, err
	}
	out := make([]Decision, 0, len(ds))
	for _, d := range ds {
		if s.authorize(ctx, p, "pm.read", d.WorkRef) == nil {
			out = append(out, s.decisionForReader(ctx, p, d))
		}
	}
	return out, nil
}
func (s *Service) AnswerDecision(ctx context.Context, p Principal, id string, in AnswerInput) (Decision, error) {
	if !p.Human {
		return Decision{}, ErrForbidden
	}
	d, err := s.decision(ctx, p, id, "pm.approve")
	if err != nil {
		return d, err
	}
	if d.Status == Superseded && d.SupersededBy != "" {
		return Decision{}, &SupersededDecisionError{SupersededBy: d.SupersededBy}
	}
	if in.Approve && !validActionPayload(d.Scope, d.Payload) {
		return Decision{}, ErrInvalid
	}
	if d.Revision == in.Revision+1 && d.AnsweredBy == p.ActorID && d.Answer == in.Text && ((in.Approve && d.Status == Answered) || (!in.Approve && d.Status == Declined)) {
		if in.Approve {
			if err = s.authorize(ctx, p, "pm.action."+d.Scope, d.WorkRef); err != nil {
				return Decision{}, err
			}
		}
		return s.decisionForReader(ctx, p, d), nil
	}
	if d.Revision != in.Revision || d.Status != AwaitingAnswer {
		if d.Status == Superseded {
			return Decision{}, &SupersededDecisionError{SupersededBy: d.SupersededBy}
		}
		return Decision{}, ErrConflict
	}
	if !validText(in.Text, 16000) {
		return Decision{}, ErrInvalid
	}
	d.Answer = in.Text
	d.AnsweredBy = p.ActorID
	d.Revision++
	d.Status = Declined
	var a *Action
	if in.Approve {
		if err = s.authorize(ctx, p, "pm.action."+d.Scope, d.WorkRef); err != nil {
			return Decision{}, err
		}
		d.Status = Answered
		d.ActionID = stableID("action", d.ID)
		now := time.Now().UTC()
		a = &Action{CreatedAt: &now, ID: d.ActionID, DecisionID: d.ID, WorkspaceID: d.WorkspaceID, ActorID: p.ActorID, WorkRef: d.WorkRef, Instruction: d.Instruction, Payload: d.Payload, Scope: d.Scope, TargetRevision: d.TargetRevision, AuthorizationBasis: "decision:" + d.ID + ";human:" + p.ActorID, Status: Pending, Revision: 1, Attempts: []Attempt{}}
	}
	d.CanAnswer = false
	if err = s.store.answer(ctx, d, a, in.Revision); err != nil {
		return Decision{}, err
	}
	return s.decisionForReader(ctx, p, d), nil
}
func (s *Service) action(ctx context.Context, p Principal, id, permission string) (Action, error) {
	var a Action
	if err := s.store.get(ctx, "action", id, &a); err != nil {
		return a, err
	}
	if a.WorkspaceID != p.WorkspaceID {
		return Action{}, ErrForbidden
	}
	if err := s.authorize(ctx, p, permission, a.WorkRef); err != nil {
		return Action{}, err
	}
	return s.actionForReader(ctx, a), nil
}
func (s *Service) ListActions(ctx context.Context, p Principal) ([]Action, error) {
	if err := s.authorize(ctx, p, "pm.read", ""); err != nil {
		return nil, err
	}
	as, err := listRecords[Action](ctx, s.store, "action", p.WorkspaceID, "", "")
	if err != nil {
		return nil, err
	}
	out := make([]Action, 0, len(as))
	for _, a := range as {
		if s.authorize(ctx, p, "pm.read", a.WorkRef) == nil {
			out = append(out, s.actionForReader(ctx, a))
		}
	}
	return out, nil
}
func (s *Service) DispatchDecision(ctx context.Context, p Principal, id string) (Action, error) {
	if !p.Human {
		return Action{}, ErrForbidden
	}
	d, err := s.decision(ctx, p, id, "pm.approve")
	if err != nil {
		return Action{}, err
	}
	if d.Status == Superseded {
		return Action{}, &SupersededDecisionError{SupersededBy: d.SupersededBy}
	}
	if d.ActionID == "" || d.Status != Answered {
		return Action{}, ErrConflict
	}
	a, err := s.action(ctx, p, d.ActionID, "pm.action."+d.Scope)
	if err != nil {
		return Action{}, err
	}
	if d.ActorID != a.ActorID || d.AnsweredBy != d.ActorID || a.Instruction != d.Instruction || a.WorkRef != d.WorkRef || a.Scope != d.Scope || a.TargetRevision != d.TargetRevision || !reflect.DeepEqual(a.Payload, d.Payload) {
		return Action{}, ErrForbidden
	}
	if a.Status != Pending {
		return a, nil
	} // Includes unknown/sending after crash: NEVER blindly resend.
	if !validActionPayload(a.Scope, a.Payload) {
		return s.failBeforeSend(ctx, a, "Invalid work.phase payload: phase must be supported and resolution_refs are required only for done. This approval will not be sent; a fresh proposal with a valid payload and a new approval are needed.")
	}
	if err = s.validateResolution(ctx, p, a.Scope, a.Payload); err != nil {
		return s.failBeforeSend(ctx, a, err.Error())
	}
	if err = s.checkDelivery(ctx, a); err != nil {
		return Action{}, err
	}
	// Revalidate the actual approving identity as well as the dispatch caller.
	approver := Principal{WorkspaceID: a.WorkspaceID, ActorID: a.ActorID, Human: true}
	if err = s.authorize(ctx, approver, "pm.approve", d.WorkRef); err != nil {
		return Action{}, err
	}
	if err = s.authorize(ctx, approver, "pm.action."+a.Scope, a.WorkRef); err != nil {
		return Action{}, err
	}
	revision, err := s.deps.CurrentRevision(ctx, approver, a.WorkRef)
	if err != nil {
		return Action{}, err
	}
	if revision != a.TargetRevision {
		// The approval remains answered, but its delivery is now terminal. Keep
		// the failed preflight as durable evidence so clients do not offer retry
		// against the same stale authorization. No source handoff was made.
		a, err = s.failBeforeSend(ctx, a, fmt.Sprintf("Approved source revision has changed (approved at %s, source now %s). This approval will not be sent; a fresh proposal and approval are needed.", a.TargetRevision, revision))
		if err != nil {
			return Action{}, err
		}
		return a, ErrStale
	}
	old := a.Revision
	a.Status = Sending
	a.Revision++
	sentAt := time.Now().UTC()
	a.Attempts = append(a.Attempts, Attempt{StartedAt: sentAt, SentAt: &sentAt, Status: Sending})
	if err = s.store.cas(ctx, "action", a.ID, old, a); err != nil {
		return Action{}, err
	}
	bounded, cancel := context.WithTimeout(ctx, s.cfg.TurnTimeout)
	defer cancel()
	receipt, execErr := s.deps.Execute(bounded, a)
	var nativeErr *NativeExecutionError
	native := errors.As(execErr, &nativeErr)
	verifiedReadBack := receipt.NativeReadBack
	receipt.NativeReadBack = false
	if native {
		receipt = Receipt{Status: Failed, Detail: nativeErr.Error()}
		if !nativeErr.WriteStarted {
			a.Attempts[len(a.Attempts)-1].SentAt = nil
		} else {
			readCtx, readCancel := context.WithTimeout(context.WithoutCancel(ctx), s.cfg.TurnTimeout)
			receipt = Receipt{Status: Unknown, Detail: "Nexus write outcome could not be read back: " + nativeErr.Error()}
			if s.deps.Reconcile != nil {
				read, readErr := s.deps.Reconcile(readCtx, a)
				if readErr == nil && validateReceipt(read, true) == nil {
					read.NativeReadBack = false
					receipt = read
					verifiedReadBack = true
					if receipt.Status == Failed {
						receipt.Detail = nativeErr.Error() + "; " + receipt.Detail
					}
				} else if readErr != nil {
					receipt.Detail += "; " + readErr.Error()
				}
			}
			readCancel()
		}
	} else if errors.Is(execErr, ErrStale) {
		receipt = Receipt{Status: Failed, Detail: ErrStale.Error()}
	} else if execErr != nil {
		receipt = Receipt{Status: Unknown, Detail: "Source handoff outcome is unknown; reconcile to establish whether it was delivered. This approval will not be sent again; if another send is needed, a fresh proposal and a new approval are required."}
	}
	if err = validateReceipt(receipt, verifiedReadBack); err != nil {
		receipt = Receipt{Status: Unknown, Detail: "Source returned an invalid receipt; reconciliation required"}
	}
	old = a.Revision
	a.Receipt = receipt
	a.Status = receipt.Status
	a.Revision++
	now := time.Now().UTC()
	a.Attempts[len(a.Attempts)-1].FinishedAt = &now
	a.Attempts[len(a.Attempts)-1].Status = a.Status
	a.Attempts[len(a.Attempts)-1].Receipt = receipt
	if err = s.store.cas(context.WithoutCancel(ctx), "action", a.ID, old, a); err != nil {
		return Action{}, err
	}
	if errors.Is(execErr, ErrStale) || errors.Is(execErr, ErrUnavailable) {
		return a, execErr
	}
	return a, nil
}
func (s *Service) failBeforeSend(ctx context.Context, a Action, detail string) (Action, error) {
	old := a.Revision
	now := time.Now().UTC()
	a.Status = Failed
	a.Receipt = Receipt{Status: Failed, Detail: detail}
	a.Revision++
	a.Attempts = append(a.Attempts, Attempt{StartedAt: now, FinishedAt: &now, Status: Failed, Receipt: a.Receipt})
	if err := s.store.cas(context.WithoutCancel(ctx), "action", a.ID, old, a); err != nil {
		return Action{}, err
	}
	return a, nil
}

func validateReceipt(r Receipt, reconcile bool) error {
	switch r.Status {
	case Unknown, Failed:
		return nil
	case Delivered, Acknowledged, Reported:
		if r.ExternalID == "" {
			return ErrInvalid
		}
	case Verified:
		if !reconcile || !r.IndependentlyVerified || len(r.EvidenceRefs) == 0 || r.ExternalID == "" {
			return ErrInvalid
		}
	default:
		return ErrInvalid
	}
	return nil
}
func (s *Service) ReconcileAction(ctx context.Context, p Principal, id string) (Action, error) {
	a, err := s.action(ctx, p, id, "pm.read")
	if err != nil {
		return a, err
	}
	// Human handling is a display state, not evidence of source acknowledgement.
	sourceStatus := a.Status
	if a.Status == Acknowledged && a.AcknowledgedAt != nil {
		if !hasSentAttempt(a) {
			return Action{}, ErrNothingDelivered
		}
		sourceStatus = a.Receipt.Status
	}
	if sourceStatus == Pending || (sourceStatus == Failed && !hasSentAttempt(a)) {
		return Action{}, ErrNothingDelivered
	}
	if a.Status == Verified {
		return a, nil
	}
	if s.deps.Reconcile == nil {
		return Action{}, ErrUnavailable
	}
	bounded, cancel := context.WithTimeout(ctx, s.cfg.TurnTimeout)
	defer cancel()
	r, err := s.deps.Reconcile(bounded, a)
	if err != nil {
		return Action{}, err
	}
	r.NativeReadBack = false
	if err = validateReceipt(r, true); err != nil {
		return Action{}, err
	}
	// A read-back must not regress an acknowledged/applied result to delivery.
	rank := map[Status]int{Unknown: 0, Sending: 0, Failed: 1, Delivered: 2, Acknowledged: 3, Reported: 4, Verified: 5}
	a.ReconciliationConflict = rank[r.Status] < rank[sourceStatus]
	old := a.Revision
	a.Receipt = r
	if !a.ReconciliationConflict && !(a.Status == Acknowledged && a.AcknowledgedAt != nil && r.Status == Unknown) {
		a.Status = r.Status
	}
	a.Revision++
	err = s.store.cas(ctx, "action", a.ID, old, a)
	return a, err
}

// GetTurnContext lets the selected PM bridge actor query evidence on behalf of
// the request's actor, with current permissions, never ambient PM privileges.
func (s *Service) GetTurnContext(ctx context.Context, p Principal, turnID, query string, limit int) (ContextPage, error) {
	return s.GetTurnContextPage(ctx, p, turnID, query, "", limit)
}
func (s *Service) GetTurnContextPage(ctx context.Context, p Principal, turnID, query, cursor string, limit int) (ContextPage, error) {
	if err := s.authorize(ctx, p, "pm.respond", ""); err != nil {
		return ContextPage{}, err
	}
	var t Turn
	if err := s.store.get(ctx, "turn", turnID, &t); err != nil {
		return ContextPage{}, err
	}
	if p.WorkspaceID != t.WorkspaceID || p.ActorID != t.AgentActorID {
		return ContextPage{}, ErrForbidden
	}
	if err := s.requireOpenTurn(ctx, t); err != nil {
		return ContextPage{}, err
	}
	if err := requireLease(t); err != nil {
		return ContextPage{}, err
	}
	var c Conversation
	if err := s.store.get(ctx, "conversation", t.ConversationID, &c); err != nil {
		return ContextPage{}, err
	}
	return s.QueryContextPage(ctx, Principal{WorkspaceID: c.WorkspaceID, ActorID: c.ActorID}, c.WorkRef, query, cursor, limit)
}

// ProposeForTurn records a proposal under the requesting actor so that it is
// visible in that actor's decision list. It confers no approval authority.
func (s *Service) ProposeForTurn(ctx context.Context, p Principal, turnID string, in DecisionInput) (Decision, error) {
	if err := s.authorize(ctx, p, "pm.respond", ""); err != nil {
		return Decision{}, err
	}
	var t Turn
	if err := s.store.get(ctx, "turn", turnID, &t); err != nil {
		return Decision{}, err
	}
	if p.WorkspaceID != t.WorkspaceID || p.ActorID != t.AgentActorID {
		return Decision{}, ErrForbidden
	}
	if err := s.requireOpenTurn(ctx, t); err != nil {
		return Decision{}, err
	}
	if err := requireLease(t); err != nil {
		return Decision{}, err
	}
	var c Conversation
	if err := s.store.get(ctx, "conversation", t.ConversationID, &c); err != nil {
		return Decision{}, err
	}
	if c.WorkRef != "" && in.WorkRef != c.WorkRef {
		return Decision{}, ErrForbidden
	}
	in.Origin = c.Origin
	return s.proposeDecision(ctx, Principal{WorkspaceID: c.WorkspaceID, ActorID: c.ActorID, Human: true}, in, turnID, p.ActorID)
}

// Prose never supplies mutation parameters, including for legacy decisions.
func validActionPayload(scope string, p *ActionPayload) bool {
	if scope != "work.phase" {
		return true
	}
	if p == nil {
		return false
	}
	switch p.Phase {
	case "backlog", "ready", "in_progress", "blocked", "review":
		return len(p.ResolutionRefs) == 0
	case "done":
		return len(p.ResolutionRefs) > 0
	}
	return false
}

// Reading decisions is workspace-visible; answering never inherits that scope.
func (s *Service) decisionForReader(ctx context.Context, p Principal, d Decision) Decision {
	// Older rejections used superseded without a replacement. Project them as
	// declined without rewriting the durable answer or its revision.
	if d.Status == Superseded && d.SupersededBy == "" {
		d.Status = Declined
	}
	d.CanAnswer = p.Human && d.ActorID == p.ActorID && d.Status == AwaitingAnswer && s.authorize(ctx, p, "pm.approve", d.WorkRef) == nil
	return d
}

// Availability is a projection of trusted routing, never stored capability truth.
func (s *Service) checkDelivery(ctx context.Context, a Action) error {
	if s.deps.Execute == nil || s.deps.CurrentRevision == nil || s.deps.CheckDelivery == nil {
		return NoDeliveryPath("this action")
	}
	return s.deps.CheckDelivery(ctx, a)
}
func (s *Service) actionForReader(ctx context.Context, a Action) Action {
	a.Deliverable = !(a.AcknowledgedAt != nil && !hasSentAttempt(a)) && s.checkDelivery(ctx, a) == nil
	return a
}
func NoDeliveryPath(source string) error {
	return fmt.Errorf("%w: No delivery path is configured for %s yet; the approval is kept and the action stays pending", ErrUnavailable, source)
}

// Missing sent_at on legacy failed attempts cannot establish a source handoff.
// Other legacy delivery states already record an attempted handoff.
func hasSentAttempt(a Action) bool {
	for _, attempt := range a.Attempts {
		if attempt.SentAt != nil {
			return true
		}
		switch attempt.Status {
		case Sending, Unknown, Delivered, Acknowledged, Reported, Verified:
			return true
		}
	}
	return false
}
