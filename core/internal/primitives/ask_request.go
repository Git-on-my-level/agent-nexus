package primitives

import (
	"agent-nexus-core/internal/blob"
	"agent-nexus-core/internal/schema"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// AppendTaskAttentionEvent accepts older clients' typed subjects while storing
// only task-addressed asks. The compatibility card and ask commit together.
// AppendEvent remains available for historical import and structured requests.
func (s *Store) AppendTaskAttentionEvent(ctx context.Context, actor string, event map[string]any) (map[string]any, error) {
	body := cloneMap(event)
	payload := cloneMap(asMapValue(body["payload"]))
	body["payload"] = payload
	subject := strings.TrimSpace(anyStringValue(payload["subject_ref"]))
	if subject == "" {
		return nil, workInvalid("subject_ref is required")
	}
	prepared, err := prepareEventForInsert(actor, body)
	if err != nil {
		return nil, err
	}
	subjectType, _, err := schema.SplitTypedRef(subject)
	if err != nil {
		return nil, workInvalid("subject_ref must be a typed reference")
	}
	compatibility := subjectType != "card"
	if compatibility {
		if err := s.CheckResourceValues(ctx, body); err != nil {
			return nil, err
		}
	}
	if s.quota.enabled() {
		s.quotaMu.Lock()
		defer s.quotaMu.Unlock()
		if s.quota.MaxBlobBytes > 0 {
			if err := s.ensureBlobUsageLedgerInitialized(ctx); err != nil {
				return nil, err
			}
		}
	}
	if err = s.checkWorkspaceWriteQuota(ctx, 0, quotaWriteDelta{dbBytes: int64(len(prepared.PayloadJSON) + len(prepared.RefsJSON) + 4096)}, blobLedgerWritePlan{}); err != nil {
		return nil, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var staged blob.StagedWrite
	var projectionThreads []string
	defer func() {
		if staged != nil {
			_ = staged.Cleanup()
		}
	}()
	var existing string
	err = tx.QueryRowContext(ctx, `SELECT id FROM events WHERE id=?`, prepared.Body["id"]).Scan(&existing)
	if err == nil {
		return nil, ErrConflict
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if compatibility {
		board, err := ensureAskSubjectBoardTx(ctx, tx, actor)
		if err != nil {
			return nil, err
		}
		boardID := board.ID
		refs, err := normalizeStringSlice(body["refs"])
		if err != nil {
			return nil, err
		}
		var related []string
		if payload["related_refs"] != nil {
			related, err = normalizeStringSlice(payload["related_refs"])
		}
		if err != nil {
			return nil, err
		}
		related = uniqueSortedStrings(append(related, subject))
		evidence := uniqueSortedStrings(append(append(refs, related...), subject))
		cardEvidence := append(append([]string{}, evidence...), "event:"+anyStringValue(prepared.Body["id"]))
		if thread := anyStringValue(body["thread_id"]); thread != "" {
			cardEvidence = append(cardEvidence, "thread:"+thread)
		}
		cardEvidence = uniqueSortedStrings(cardEvidence)
		// These are evidence dependencies, not an instruction to reparent the
		// legacy thread (several asks may share it).
		prep, err := prepareBoardCardInsert(AddBoardCardInput{Title: firstNonEmpty(anyStringValue(payload["title"]), anyStringValue(body["summary"])), ColumnKey: "ready"})
		if err != nil {
			return nil, err
		}
		prep.Refs = cardEvidence
		prep.RefsJSON, _ = json.Marshal(cardEvidence)
		var card boardCardRow
		_, card, staged, err = s.execBoardCardInsert(ctx, tx, board, actor, boardID, prep)
		if err != nil {
			return nil, err
		}
		if err = insertWorkMetadata(ctx, tx, card.CardID, actor, map[string]any{"source": map[string]any{"authority": "nexus"}, "related_refs": cardEvidence}); err != nil {
			return nil, err
		}
		projectionThreads = []string{board.ThreadID, card.ThreadID.String}
		subject = "card:" + card.Handle.String
		payload["related_refs"] = related
		body["refs"] = uniqueSortedStrings(append(evidence, subject))
	} else {
		resolved, err := resolveResourceRef(ctx, tx, ResourceRefInput{Type: "card", Ref: subject})
		if err != nil {
			return nil, err
		}
		var phase, archived, trashed string
		err = tx.QueryRowContext(ctx, `SELECT `+projectedWorkStringSQL("phase", `c.column_key`)+`,COALESCE(c.archived_at,''),COALESCE(c.trashed_at,'') FROM cards c LEFT JOIN work_metadata m ON m.card_id=c.id LEFT JOIN work_observations o ON o.id=m.latest_observation_id WHERE c.id=?`, resolved.ID).Scan(&phase, &archived, &trashed)
		if err != nil {
			return nil, err
		}
		if phase == "done" || phase == "cancelled" || archived != "" || trashed != "" {
			return nil, ErrHumanAttentionAlreadyResponded
		}
		subject = resolved.CanonicalRef
	}
	payload["subject_ref"] = subject
	body["id"] = prepared.Body["id"]
	prepared, err = prepareEventForInsert(actor, body)
	if err != nil {
		return nil, err
	}
	if err = insertPreparedEvent(ctx, tx, prepared); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	if staged != nil {
		if err = staged.Promote(); err != nil {
			return nil, err
		}
	}
	if len(projectionThreads) > 0 {
		if err = s.MarkTopicProjectionsDirty(ctx, projectionThreads, time.Now().UTC()); err != nil {
			return nil, err
		}
	}
	return prepared.Body, nil
}
