package pm

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"agent-nexus-core/internal/resourceaccess"
)

type proposalReplay struct{ Scope, Actor, Key, Hash string }

func newProposalReplay(p Principal, in DecisionInput) (proposalReplay, error) {
	if !validText(in.ReplayKey, 256) || strings.ContainsRune(in.ReplayKey, 0) {
		return proposalReplay{}, ErrInvalid
	}
	raw, err := json.Marshal(in)
	if err != nil {
		return proposalReplay{}, err
	}
	hash := sha256.Sum256(raw)
	return proposalReplay{Scope: "pm.decisions.create:" + p.WorkspaceID, Actor: p.ActorID, Key: in.ReplayKey, Hash: hex.EncodeToString(hash[:])}, nil
}

func (s *Store) readProposalReplay(ctx context.Context, r proposalReplay) (Decision, bool, error) {
	var hash, raw string
	var status int
	err := s.database().QueryRowContext(ctx, `SELECT request_hash,response_status,response_json FROM idempotency_replays WHERE scope=? AND actor_id=? AND request_key=?`, r.Scope, r.Actor, r.Key).Scan(&hash, &status, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return Decision{}, false, nil
	}
	if err != nil {
		return Decision{}, false, err
	}
	if hash != r.Hash {
		return Decision{}, false, ErrConflict
	}
	d, err := decodeProposalReceipt([]byte(raw), status)
	return d, err == nil, err
}

func decodeProposalReceipt(raw []byte, status int) (Decision, error) {
	var out struct {
		Decision
		WorkMissing     bool  `json:"work_missing"`
		TargetCurrent   *bool `json:"target_current"`
		AlreadyAtTarget *bool `json:"already_at_target"`
		Replayed        bool  `json:"replayed"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return Decision{}, err
	}
	if out.ID == "" || (status != http.StatusCreated && status != http.StatusOK) {
		return Decision{}, ErrInvalid
	}
	d := out.Decision
	d.WorkMissing, d.TargetCurrent, d.AlreadyAtTarget, d.Replayed = out.WorkMissing, out.TargetCurrent, out.AlreadyAtTarget, out.Replayed
	d.ProposalReplayJSON, d.ProposalReplayStatus = raw, status
	return d, nil
}

// Anticipate legacy deduplication before projection/auth preflight. If a different
// candidate wins under the write lock, fail closed instead of attaching an HTTP
// projection prepared for another identity or source authority.
func (s *Store) proposalCandidate(ctx context.Context, d Decision) (Decision, error) {
	var prior Decision
	err := s.get(ctx, "decision", d.ID, &prior)
	if err == nil {
		return prior, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return Decision{}, err
	}
	var raw []byte
	err = s.database().QueryRowContext(ctx, proposalCandidateSQL, d.WorkspaceID, d.ActorID, d.WorkRef, d.Scope).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return d, nil
	}
	if err != nil {
		return Decision{}, err
	}
	if err = json.Unmarshal(raw, &prior); err != nil {
		return Decision{}, err
	}
	if sameDecisionIntent(prior, d) {
		return prior, nil
	}
	return d, nil
}

func (s *Service) proposeDecisionWithReplay(ctx context.Context, p Principal, in DecisionInput) (Decision, error) {
	if in.ActorID != "" && in.ActorID != p.ActorID {
		return Decision{}, ErrForbidden
	}
	if err := s.authorizePermission(ctx, p, "pm.propose", in.WorkRef); err != nil {
		return Decision{}, err
	}
	r, err := newProposalReplay(p, in)
	if err != nil {
		return Decision{}, err
	}
	// Fresh visibility checks precede even a stored receipt. Authentication and
	// origin authorization never run inside the scoped writer.
	if in.Payload != nil {
		for _, ref := range in.Payload.ResolutionRefs {
			if err := s.authorizePermission(ctx, p, "pm.read", ref); err != nil {
				return Decision{}, err
			}
		}
	}
	if in.Origin != nil {
		binding, err := s.ResolveBinding(ctx, *in.Origin)
		if err != nil || binding.ActorID != p.ActorID || binding.WorkspaceID != p.WorkspaceID {
			return Decision{}, ErrForbidden
		}
	}
	if prior, found, err := s.store.readProposalReplay(ctx, r); err != nil || found {
		return prior, err
	}
	if err := s.RequireOnboarded(ctx); err != nil {
		return Decision{}, err
	}
	if !validText(in.RequestKey, 256) || !validText(in.WorkRef, 512) || !validText(in.Instruction, 16000) || !validText(in.Scope, 256) || !validText(in.TargetRevision, 512) {
		return Decision{}, ErrInvalid
	}
	if err := validateAction(in.Scope, in.Instruction, in.Payload); err != nil {
		return Decision{}, err
	}
	if err := s.validateResolution(ctx, p, in.Scope, in.Payload); err != nil {
		return Decision{}, err
	}
	d := Decision{ID: stableID("decision", p.WorkspaceID, p.ActorID, in.RequestKey), WorkspaceID: p.WorkspaceID, ActorID: p.ActorID, WorkRef: in.WorkRef, Instruction: in.Instruction, Payload: in.Payload, Scope: in.Scope, TargetRevision: in.TargetRevision, Status: AwaitingAnswer, Revision: 1, Origin: in.Origin, ProposedBy: p.ActorID, OriginKind: "human", CreatedAt: time.Now().UTC()}
	if s.deps.DecisionWork != nil {
		work, err := s.deps.DecisionWork(ctx, p, in.WorkRef)
		if err != nil {
			return Decision{}, err
		}
		d.SourceAuthority = work.SourceAuthority
	}
	candidate, err := s.store.proposalCandidate(ctx, d)
	if err != nil {
		return Decision{}, err
	}
	projected := s.decisionForReader(ctx, p, candidate)
	canApprove := p.Human && s.authorize(ctx, p, "pm.approve", candidate.WorkRef) == nil
	path, pathErr := s.deliveryPath(ctx, Action{SourceAuthority: candidate.SourceAuthority, DecisionID: candidate.ID, WorkspaceID: candidate.WorkspaceID, ActorID: candidate.ActorID, WorkRef: candidate.WorkRef, Instruction: candidate.Instruction, Scope: candidate.Scope, TargetRevision: candidate.TargetRevision, Payload: candidate.Payload})
	resolution := make([]ResolutionRef, 0)
	if candidate.Payload != nil {
		for _, ref := range candidate.Payload.ResolutionRefs {
			kind, _, _ := strings.Cut(ref, ":")
			resolved := ResolutionRef{Ref: ref, Kind: kind}
			if s.deps.ResolveResolution != nil {
				if live, err := s.deps.ResolveResolution(ctx, p, ref); err == nil {
					resolved = live
				}
			}
			resolution = append(resolution, resolved)
		}
	}
	var out Decision
	err = s.store.database().WriteTransaction(ctx, func(ctx context.Context, tx *resourceaccess.Tx) error {
		if _, err := tx.ExecContext(ctx, `UPDATE idempotency_replays SET request_hash=request_hash WHERE scope=? AND actor_id=? AND request_key=?`, r.Scope, r.Actor, r.Key); err != nil {
			return err
		}
		if prior, found, err := s.store.readProposalReplay(ctx, r); err != nil || found {
			out = prior
			return err
		}
		final, inserted, err := s.store.proposeDecision(ctx, d, "", "")
		if err != nil {
			return err
		}
		if final.ID != candidate.ID || final.SourceAuthority != candidate.SourceAuthority {
			return ErrConflict
		}
		if inserted && final.Origin != nil {
			// Recheck the durable binding under the write lock without invoking the
			// raw-auth authorizer a second time on the one-connection pool.
			var binding Binding
			if err := s.store.get(ctx, "binding", bindingID(*final.Origin), &binding); err != nil {
				return err
			}
			if !binding.Enabled || binding.WorkspaceID != p.WorkspaceID || binding.ActorID != p.ActorID || binding.Origin != *final.Origin {
				return ErrForbidden
			}
			delivery := newDecisionCardDelivery(final, "")
			if _, err := s.store.insert(ctx, "delivery", delivery.ID, delivery.WorkspaceID, delivery.ActorID, final.ID, delivery); err != nil {
				return err
			}
		}
		final.Replayed = !inserted
		final.CanAnswer = canApprove && final.ActorID == p.ActorID && final.Status == AwaitingAnswer
		final.WorkMissing, final.TargetCurrent, final.AlreadyAtTarget = projected.WorkMissing, projected.TargetCurrent, projected.AlreadyAtTarget
		status := http.StatusCreated
		if !inserted {
			status = http.StatusOK
		}
		raw, err := json.Marshal(decisionResponseSnapshot(final, path, pathErr, resolution))
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO idempotency_replays(scope,actor_id,request_key,request_hash,response_status,response_json) VALUES(?,?,?,?,?,?)`, r.Scope, r.Actor, r.Key, r.Hash, status, string(raw)); err != nil {
			return err
		}
		out, err = decodeProposalReceipt(raw, status)
		return err
	})
	return out, err
}
