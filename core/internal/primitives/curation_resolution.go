package primitives

import (
	"context"
)

// Resolution is agent work. No human attention or notification path is used.
// Each successful action is replayable until its receipt and terminal state land.
func (store *Store) ResolveAgentCuration(ctx context.Context, actor string, rec CurationProposalRecord, resolution, rationale string) error {
	if store.quota.enabled() && !replayOwnsQuota(ctx, store) {
		store.quotaMu.Lock()
		defer store.quotaMu.Unlock()
		ctx = context.WithValue(ctx, replayQuotaKey{}, store)
	}
	return store.db.WriteTransaction(ctx, func(ctx context.Context, tx *accessTx) error {
		var err error
		rec, err = store.CurationProposal(ctx, rec.Key)
		if err != nil {
			return err
		}

		if rec.Status == CurationProposalDismissed || rec.Status == CurationProposalAccepted {
			return nil
		}
		event, err := store.GetEvent(ctx, rec.ProposalEventID)
		if err != nil {
			return err
		}
		thread := anyStringValue(event["thread_id"])
		applied, err := store.CurationActionApplied(ctx, thread, rec.ProposalEventID)
		if err != nil {
			return err
		}
		status := CurationProposalDismissed
		if !applied {
			trail := map[string]any{"proposal_event_id": rec.ProposalEventID, "action": rec.Action, "targets": rec.Targets, "applied_by": actor, "rationale": rationale, "result": "dismissed"}
			summary := "Dismissed curation suggestion."
			if resolution == "apply" {
				undo, text, err := store.ApplyCurationAction(ctx, actor, rec.ProposalEventID, rec)
				if err != nil {
					return err
				}
				trail["result"], trail["undo"], summary = "applied", undo, text
			}
			if _, err = store.AppendEvent(ctx, actor, map[string]any{"type": "curation_receipt", "thread_id": thread, "refs": uniqueSortedStrings(append([]string{"thread:" + thread, "event:" + rec.ProposalEventID}, rec.Targets...)), "summary": summary, "payload": map[string]any{"curation_applied": trail}, "provenance": map[string]any{"sources": []string{"inferred"}}}); err != nil {
				return err
			}
		}
		if resolution == "apply" {
			status = CurationProposalAccepted
		}
		_, err = store.ResolveCurationProposal(ctx, rec.Key, status)
		return err
	})
}
