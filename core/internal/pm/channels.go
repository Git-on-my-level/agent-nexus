package pm

import (
	"context"
	"errors"
	"time"
)

func validOrigin(o Origin) bool {
	return (o.Transport == "telegram" || o.Transport == "discord") && validText(o.TenantID, 128) && validText(o.ChannelID, 128) && validText(o.ExternalUserID, 128) && len(o.ThreadID) <= 128
}
func bindingID(o Origin) string {
	return stableID("binding", o.Transport, o.TenantID, o.ChannelID, o.ThreadID, o.ExternalUserID)
}

// BindChannel is an explicit administrative mapping to an existing principal.
// Authorize must reject unknown target principals (pm.bind.target permission).
func (s *Service) BindChannel(ctx context.Context, admin Principal, b Binding) (Binding, error) {
	if !admin.Human {
		return Binding{}, ErrForbidden
	}
	if err := s.authorize(ctx, admin, "pm.bind", ""); err != nil {
		return Binding{}, err
	}
	if !validOrigin(b.Origin) || b.WorkspaceID != s.cfg.WorkspaceID || b.ActorID == "" {
		return Binding{}, ErrInvalid
	}
	target := Principal{WorkspaceID: b.WorkspaceID, ActorID: b.ActorID, Human: b.CanApprove}
	if err := s.authorize(ctx, target, "pm.bind.target", ""); err != nil {
		return Binding{}, err
	}
	b.ID = bindingID(b.Origin)
	var prior Binding
	err := s.store.get(ctx, "binding", b.ID, &prior)
	if err == nil {
		if b.Revision != prior.Revision {
			return Binding{}, ErrConflict
		}
		b.Revision++
		err = s.store.cas(ctx, "binding", b.ID, prior.Revision, b)
		return b, err
	}
	if !errors.Is(err, ErrNotFound) {
		return Binding{}, err
	}
	if b.Revision != 0 {
		return Binding{}, ErrConflict
	}
	b.Revision = 1
	inserted, err := s.store.insert(ctx, "binding", b.ID, b.WorkspaceID, b.ActorID, "", b)
	if err == nil && !inserted {
		err = ErrConflict
	}
	return b, err
}
func (s *Service) ResolveBinding(ctx context.Context, o Origin) (Binding, error) {
	if !validOrigin(o) {
		return Binding{}, ErrForbidden
	}
	var b Binding
	if err := s.store.get(ctx, "binding", bindingID(o), &b); err != nil {
		return Binding{}, ErrForbidden
	}
	if !b.Enabled || b.WorkspaceID != s.cfg.WorkspaceID || b.Origin != o {
		return Binding{}, ErrForbidden
	}
	if err := s.authorize(ctx, Principal{WorkspaceID: b.WorkspaceID, ActorID: b.ActorID, Human: b.CanApprove}, "pm.read", ""); err != nil {
		return Binding{}, err
	}
	return b, nil
}

// ReceiveChannel is only called after transport authentication. No client-
// supplied workspace or actor is accepted; both come from the current binding.
func (s *Service) ReceiveChannel(ctx context.Context, o Origin, eventID, text string) (Turn, error) {
	b, err := s.ResolveBinding(ctx, o)
	if err != nil {
		return Turn{}, err
	}
	if !validText(eventID, 256) {
		return Turn{}, ErrInvalid
	}
	p := Principal{WorkspaceID: b.WorkspaceID, ActorID: b.ActorID, Human: b.CanApprove}
	c, err := s.createConversation(ctx, p, CreateConversation{RequestKey: bindingID(o), Title: "PM · " + o.Transport}, &o)
	if err != nil {
		return Turn{}, err
	}
	return s.PostMessage(ctx, p, c.ID, MessageInput{RequestKey: eventID, Text: text})
}
func (s *Service) AnswerFromChannel(ctx context.Context, o Origin, decisionID string, in AnswerInput) (Decision, error) {
	b, err := s.ResolveBinding(ctx, o)
	if err != nil {
		return Decision{}, err
	}
	if !b.CanApprove {
		return Decision{}, ErrForbidden
	}
	d, err := s.decision(ctx, Principal{WorkspaceID: b.WorkspaceID, ActorID: b.ActorID}, decisionID, "pm.read")
	if err != nil {
		return Decision{}, err
	}
	// Explicit origin binding prevents decisions copied to another chat/user from
	// becoming bearer approval tokens.
	if d.Origin == nil || *d.Origin != o {
		return Decision{}, ErrForbidden
	}
	return s.AnswerDecision(ctx, Principal{WorkspaceID: b.WorkspaceID, ActorID: b.ActorID, Human: true}, decisionID, in)
}

type Sender interface {
	Send(context.Context, Delivery) (Receipt, error)
}

func (s *Service) QueueTurnDelivery(ctx context.Context, turnID string) (Delivery, error) {
	var t Turn
	if err := s.store.get(ctx, "turn", turnID, &t); err != nil {
		return Delivery{}, err
	}
	if t.Status != Delivered {
		return Delivery{}, ErrConflict
	}
	var c Conversation
	if err := s.store.get(ctx, "conversation", t.ConversationID, &c); err != nil {
		return Delivery{}, err
	}
	if c.Origin == nil {
		return Delivery{}, ErrInvalid
	}
	b, err := s.ResolveBinding(ctx, *c.Origin)
	if err != nil || b.ActorID != c.ActorID {
		return Delivery{}, ErrForbidden
	}
	d := Delivery{ID: stableID("delivery", t.ID), WorkspaceID: c.WorkspaceID, ActorID: c.ActorID, Origin: *c.Origin, Text: t.Response, Status: Pending, Revision: 1}
	inserted, err := s.store.insert(ctx, "delivery", d.ID, d.WorkspaceID, d.ActorID, t.ID, d)
	if err != nil {
		return Delivery{}, err
	}
	if !inserted {
		err = s.store.get(ctx, "delivery", d.ID, &d)
	}
	return d, err
}
func (s *Service) SendDelivery(ctx context.Context, id string, sender Sender) (Delivery, error) {
	if sender == nil {
		return Delivery{}, ErrUnavailable
	}
	var d Delivery
	if err := s.store.get(ctx, "delivery", id, &d); err != nil {
		return d, err
	}
	b, err := s.ResolveBinding(ctx, d.Origin)
	if err != nil || b.ActorID != d.ActorID || b.WorkspaceID != d.WorkspaceID {
		return Delivery{}, ErrForbidden
	}
	if d.Status != Pending {
		return d, nil
	}
	old := d.Revision
	d.Status = Sending
	d.Revision++
	if err = s.store.cas(ctx, "delivery", id, old, d); err != nil {
		return Delivery{}, err
	}
	bounded, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	receipt, sendErr := sender.Send(bounded, d)
	if sendErr != nil || receipt.Status != Delivered || receipt.ExternalID == "" {
		receipt = Receipt{Status: Unknown, Detail: "Transport outcome unknown; inspect remote history before retry"}
	}
	old = d.Revision
	d.Status = receipt.Status
	d.Receipt = receipt
	d.Revision++
	if err = s.store.cas(context.WithoutCancel(ctx), "delivery", id, old, d); err != nil {
		return Delivery{}, err
	}
	return d, nil
}

// ReconcileDelivery records externally inspected evidence. It cannot reopen a
// send automatically; creating a new delivery requires a separate explicit intent.
func (s *Service) ReconcileDelivery(ctx context.Context, p Principal, id string, r Receipt) (Delivery, error) {
	if !p.Human {
		return Delivery{}, ErrForbidden
	}
	if err := s.authorize(ctx, p, "pm.delivery.reconcile", ""); err != nil {
		return Delivery{}, err
	}
	var d Delivery
	if err := s.store.get(ctx, "delivery", id, &d); err != nil {
		return d, err
	}
	if d.WorkspaceID != p.WorkspaceID {
		return Delivery{}, ErrForbidden
	}
	if d.Status != Sending && d.Status != Unknown {
		return Delivery{}, ErrConflict
	}
	if r.Status != Delivered && r.Status != Failed {
		return Delivery{}, ErrInvalid
	}
	if len(r.EvidenceRefs) == 0 || r.ExternalID == "" {
		return Delivery{}, ErrInvalid
	}
	old := d.Revision
	d.Status = r.Status
	d.Receipt = r
	d.Revision++
	err := s.store.cas(ctx, "delivery", id, old, d)
	return d, err
}
