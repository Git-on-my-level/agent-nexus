package server

import (
	"agent-nexus-core/internal/pm"
	"agent-nexus-core/internal/primitives"
	"context"
	"errors"
)

// A deterministic event identity makes reconciliation possible after a lost
// receipt. Only the human-approved action reaches this executor.
func executeCardNote(ctx context.Context, store *primitives.Store, a pm.Action) (pm.Receipt, error) {
	if a.Payload == nil || a.Payload.Note == "" {
		return pm.Receipt{}, pm.ErrInvalid
	}
	w, err := store.GetWork(ctx, a.WorkRef)
	if err != nil {
		return pm.Receipt{}, err
	}
	if anyString(workSourceMap(w)["authority"]) != "nexus" {
		return pm.Receipt{}, pm.ErrUnavailable
	}
	if primitives.WorkDecisionRevision(w) != a.TargetRevision {
		return pm.Receipt{}, pm.ErrStale
	}
	err = store.AppendApprovedCardNote(ctx, a.ActorID, a.WorkRef, a.TargetRevision, a.ID, a.Payload.Note)
	if err != nil {
		if errors.Is(err, primitives.ErrApprovedCardNoteStale) {
			return pm.Receipt{}, pm.ErrStale
		}
		var unknown *primitives.MutationOutcomeUnknown
		return pm.Receipt{}, &pm.NativeExecutionError{Cause: err, WriteStarted: errors.As(err, &unknown)}
	}

	receipt, err := readBackCardNote(ctx, store, a)
	if err != nil {
		return receipt, &pm.NativeExecutionError{Cause: err, WriteStarted: true}
	}
	return receipt, nil
}
func readBackCardNote(ctx context.Context, store *primitives.Store, a pm.Action) (pm.Receipt, error) {
	if a.Payload == nil {
		return pm.Receipt{}, pm.ErrInvalid
	}
	e, err := store.GetEvent(ctx, "pm_note_"+a.ID)
	if err != nil {
		return pm.Receipt{}, err
	}
	w, err := store.GetWork(ctx, a.WorkRef)
	if err != nil {
		return pm.Receipt{}, err
	}
	payload, _ := e["payload"].(map[string]any)
	if e["type"] != "message_posted" || e["actor_id"] != a.ActorID || e["thread_id"] != w["thread_id"] || payload["text"] != a.Payload.Note {
		return pm.Receipt{Status: pm.Failed, Detail: "Card note does not match approved text"}, nil
	}
	return pm.Receipt{NativeReadBack: true, Status: pm.Verified, ExternalID: a.ID, EvidenceRefs: []string{anyString(e["ref"])}, IndependentlyVerified: true, Detail: "Read back approved card note"}, nil
}
