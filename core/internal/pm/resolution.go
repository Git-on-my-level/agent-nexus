package pm

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

type ResolutionRef struct {
	Ref            string `json:"ref"`
	Kind           string `json:"kind"`
	TitleOrSummary string `json:"title_or_summary"`
	Exists         bool   `json:"exists"`
}

func (s *Service) validateResolution(ctx context.Context, p Principal, scope string, payload *ActionPayload) error {
	if scope != "work.phase" || payload == nil || payload.Phase != "done" {
		return nil
	}
	if len(payload.ResolutionRefs) == 0 {
		return fmt.Errorf("%w: done requires resolution_refs", ErrInvalid)
	}
	hasEvidence := false
	for _, ref := range payload.ResolutionRefs {
		if s.deps.ResolveResolution == nil {
			return ErrUnavailable
		}
		resolved, err := s.deps.ResolveResolution(ctx, p, ref)
		if err != nil {
			return err
		}
		if !resolved.Exists {
			return fmt.Errorf("%w: resolution_refs: missing or trashed resolution ref %q", ErrInvalid, ref)
		}
		if resolved.Kind == "artifact" || resolved.Kind == "event" {
			hasEvidence = true
		}
	}
	if !hasEvidence {
		return fmt.Errorf("%w: resolution_refs must include at least one artifact: or event: ref", ErrInvalid)
	}
	return nil
}

// Project only at the HTTP boundary: derived fields never enter persistence,
// approval equality, idempotency checks, or the source executor's parameters.
func (s *Service) decisionResponse(ctx context.Context, p Principal, d Decision) any {
	path, err := s.deliveryPath(ctx, Action{SourceAuthority: d.SourceAuthority, DecisionID: d.ID, WorkspaceID: d.WorkspaceID, ActorID: d.ActorID, WorkRef: d.WorkRef, Instruction: d.Instruction, Scope: d.Scope, TargetRevision: d.TargetRevision, Payload: d.Payload})
	type response struct {
		Decision
		WorkMissing            bool   `json:"work_missing"`
		TargetCurrent          bool   `json:"target_current"`
		AlreadyAtTarget        bool   `json:"already_at_target"`
		Replayed               bool   `json:"replayed,omitempty"`
		ReplayedTerminalStatus Status `json:"replayed_terminal_status,omitempty"`
		Deliverable            *bool  `json:"deliverable"`
		DeliveryPath           string `json:"delivery_path"`
	}
	out := response{Decision: d, WorkMissing: d.WorkMissing, TargetCurrent: d.TargetCurrent, AlreadyAtTarget: d.AlreadyAtTarget, Replayed: d.Replayed, DeliveryPath: path}
	if path != "unknown" {
		available := err == nil
		out.Deliverable = &available
	}
	if d.Replayed && d.Status != AwaitingAnswer {
		out.ReplayedTerminalStatus = d.Status
	}
	if d.Payload == nil {
		return out
	}
	type payloadResponse struct {
		*ActionPayload
		Resolution []ResolutionRef `json:"resolution,omitempty"`
	}
	payload := payloadResponse{ActionPayload: d.Payload, Resolution: make([]ResolutionRef, 0, len(d.Payload.ResolutionRefs))}
	for _, ref := range d.Payload.ResolutionRefs {
		kind, _, _ := strings.Cut(ref, ":")
		resolved := ResolutionRef{Ref: ref, Kind: kind}
		if s.deps.ResolveResolution != nil {
			if live, err := s.deps.ResolveResolution(ctx, p, ref); err == nil {
				resolved = live
			}
		}
		payload.Resolution = append(payload.Resolution, resolved)
	}
	return struct {
		response
		Payload payloadResponse `json:"payload"`
	}{out, payload}
}

// Legacy purged work may have no recoverable source authority. Null availability
// is intentionally different from false (a known unconfigured executor). Keep
// the internal bool fail-closed, while wire consumers can distinguish the two.
func (a Action) MarshalJSON() ([]byte, error) {
	type action Action
	if a.DeliveryPath != "unknown" {
		return json.Marshal(action(a))
	}
	return json.Marshal(struct {
		action
		Deliverable *bool `json:"deliverable"`
	}{action: action(a)})
}
