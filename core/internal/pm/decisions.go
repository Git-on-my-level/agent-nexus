package pm

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"time"
)

func (s *Service) ProposeDecision(ctx context.Context, p Principal, in DecisionInput) (Decision, error) {
	return s.proposeDecision(ctx, p, in, "", p.ActorID, "")
}
func (s *Service) proposeDecision(ctx context.Context, p Principal, in DecisionInput, turnID, proposedBy, leaseToken string) (Decision, error) {
	if err := s.authorize(ctx, p, "pm.propose", in.WorkRef); err != nil {
		return Decision{}, err
	}
	if !validText(in.RequestKey, 256) || !validText(in.WorkRef, 512) || !validText(in.Instruction, 16000) || !validText(in.Scope, 256) || !validText(in.TargetRevision, 512) {
		return Decision{}, ErrInvalid
	}
	if !validActionPayload(in.Scope, in.Payload) {
		return Decision{}, fmt.Errorf("%w: %s", ErrInvalid, invalidActionPayloadMessage)
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
	if s.deps.DecisionWork != nil {
		work, err := s.deps.DecisionWork(ctx, p, in.WorkRef)
		if err != nil {
			return Decision{}, err
		}
		d.SourceAuthority = work.SourceAuthority
	}
	d.ProposedBy = proposedBy
	d.OriginKind = "human"
	if turnID != "" {
		d.OriginKind, d.TurnID = "pm_turn", turnID
	} else if in.Origin != nil {
		d.OriginKind = "channel"
	}
	d, inserted, err := s.store.proposeDecision(ctx, d, turnID, leaseToken)
	if err != nil {
		return Decision{}, err
	}
	if !inserted {
		d.Replayed = true
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
		return Decision{}, fmt.Errorf("%w: %s", ErrInvalid, invalidActionPayloadMessage)
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
	if in.Approve {
		if err = s.validateApprovalTarget(ctx, p, d); err != nil {
			return Decision{}, err
		}
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
		a = &Action{SourceAuthority: d.SourceAuthority, CreatedAt: &now, ID: d.ActionID, DecisionID: d.ID, WorkspaceID: d.WorkspaceID, ActorID: p.ActorID, WorkRef: d.WorkRef, Instruction: d.Instruction, Payload: d.Payload, Scope: d.Scope, TargetRevision: d.TargetRevision, AuthorizationBasis: "decision:" + d.ID + ";human:" + p.ActorID, Status: Pending, Revision: 1, Attempts: []Attempt{}}
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
		if errors.Is(err, ErrNotFound) {
			return Action{}, actionWorkReadError(a, err)
		}
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
	if d.ActorID != a.ActorID || d.AnsweredBy != d.ActorID || a.Instruction != d.Instruction || a.WorkRef != d.WorkRef || a.Scope != d.Scope || a.TargetRevision != d.TargetRevision || a.SourceAuthority != d.SourceAuthority || !reflect.DeepEqual(a.Payload, d.Payload) {
		return Action{}, ErrForbidden
	}
	if err := s.validateActionWork(ctx, p, a); err != nil {
		return Action{}, err
	}
	if a.Status != Pending {
		return a, nil
	} // Includes unknown/sending after crash: NEVER blindly resend.
	if !validActionPayload(a.Scope, a.Payload) {
		return s.failBeforeSend(ctx, a, invalidActionPayloadMessage+" This approval will not be sent; a fresh proposal with a valid payload and a new approval are needed.")
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
		return Action{}, actionWorkReadError(a, err)
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
	if err := s.validateActionWork(ctx, p, a); err != nil {
		return Action{}, err
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
func (s *Service) GetTurnContext(ctx context.Context, p Principal, turnID, query string, limit int, leaseToken string) (ContextPage, error) {
	return s.GetTurnContextPage(ctx, p, turnID, query, "", limit, leaseToken)
}
func (s *Service) GetTurnContextPage(ctx context.Context, p Principal, turnID, query, cursor string, limit int, leaseToken string) (ContextPage, error) {
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
	if err := leaseGuard(t, leaseToken); err != nil {
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
func (s *Service) ProposeForTurn(ctx context.Context, p Principal, turnID string, in DecisionInput, leaseToken string) (Decision, error) {
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
	if err := leaseGuard(t, leaseToken); err != nil {
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
	return s.proposeDecision(ctx, Principal{WorkspaceID: c.WorkspaceID, ActorID: c.ActorID, Human: true}, in, turnID, p.ActorID, leaseToken)
}

const invalidActionPayloadMessage = "Invalid work.phase payload: phase must be supported and resolution_refs are required only for done."

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
	d.WorkMissing, d.TargetCurrent, d.AlreadyAtTarget = false, false, false
	if s.deps.DecisionWork != nil {
		work, err := s.deps.DecisionWork(ctx, p, d.WorkRef)
		d.WorkMissing = errors.Is(err, ErrNotFound)
		if err != nil {
			d.CanAnswer = false
		} else {
			d.TargetCurrent = d.TargetRevision == work.Revision
			d.AlreadyAtTarget = d.Payload != nil && d.Payload.Phase != "" && d.Payload.Phase == work.Phase
		}
	}
	return d
}

// Availability is a projection of trusted routing, never stored capability truth.
func (s *Service) checkDelivery(ctx context.Context, a Action) error {
	_, err := s.deliveryPath(ctx, a)
	return err
}
func (s *Service) deliveryPath(ctx context.Context, a Action) (string, error) {
	if s.deps.Execute == nil || s.deps.CurrentRevision == nil {
		return "none", NoDeliveryPath("this action")
	}
	if s.deps.DeliveryPath != nil {
		path, err := s.deps.DeliveryPath(ctx, a)
		if err != nil {
			if path == "unknown" && errors.Is(err, ErrNotFound) {
				return path, err
			}
			return "none", err
		}
		if path == "" || path == "none" {
			return "none", NoDeliveryPath("this action")
		}
		return path, nil
	}
	if s.deps.CheckDelivery != nil {
		if err := s.deps.CheckDelivery(ctx, a); err != nil {
			return "none", err
		}
		return "configured", nil
	}
	return "none", NoDeliveryPath("this action")
}
func (s *Service) actionForReader(ctx context.Context, a Action) Action {
	path, err := s.deliveryPath(ctx, a)
	a.DeliveryPath = path
	a.Deliverable = !(a.AcknowledgedAt != nil && !hasSentAttempt(a)) && err == nil
	return a
}

type noDeliveryPathError struct{ source string }

func (e *noDeliveryPathError) Error() string {
	return fmt.Sprintf("%s: No delivery path is configured for %s yet; the approval is kept and the action stays pending", ErrUnavailable, e.source)
}
func (e *noDeliveryPathError) Unwrap() error { return ErrUnavailable }

func NoDeliveryPath(source string) error { return &noDeliveryPathError{source: source} }

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

// Replays are handled before this check. Dispatch still rechecks the revision
// because source state can change after approval (including during this read).
func (s *Service) validateApprovalTarget(ctx context.Context, p Principal, d Decision) error {
	failure := &ApprovalTargetError{ApprovedRevision: d.TargetRevision, Reason: "work_read_failed"}
	if s.deps.DecisionWork == nil {
		return failure
	}
	work, err := s.deps.DecisionWork(ctx, p, d.WorkRef)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			failure.Reason = "work_missing"
		}
		return failure
	}
	failure.CurrentRevision = &work.Revision
	if d.TargetRevision != work.Revision {
		failure.Reason = "revision_changed"
		return failure
	}
	if d.Payload != nil && d.Payload.Phase != "" && d.Payload.Phase == work.Phase {
		failure.Reason = "already_at_target"
		return failure
	}
	return nil
}

// Reconciliation checks liveness, not the approved revision or target phase:
// a successful source write is expected to have changed those values.
func (s *Service) validateActionWork(ctx context.Context, p Principal, a Action) error {
	var err error
	if s.deps.DecisionWork != nil {
		_, err = s.deps.DecisionWork(ctx, p, a.WorkRef)
	} else if s.deps.CurrentRevision != nil {
		_, err = s.deps.CurrentRevision(ctx, p, a.WorkRef)
	} else {
		err = ErrUnavailable
	}
	if err != nil {
		return actionWorkReadError(a, err)
	}
	return nil
}
func actionWorkReadError(a Action, err error) error {
	reason := "work_read_failed"
	if errors.Is(err, ErrNotFound) {
		reason = "work_missing"
	}
	return &ApprovalTargetError{ApprovedRevision: a.TargetRevision, Reason: reason}
}
