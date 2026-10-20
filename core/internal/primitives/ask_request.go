package primitives

import (
	"agent-nexus-core/internal/blob"
	"agent-nexus-core/internal/schema"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// AppendTaskAttentionEvent accepts older clients' typed subjects while storing
// only task-addressed asks. The compatibility card and ask commit together.
// AppendEvent remains available for historical import and structured requests.
func (s *Store) AppendTaskAttentionEvent(ctx context.Context, actor string, event map[string]any) (map[string]any, error) {
	body := cloneMap(event)
	if strings.TrimSpace(anyStringValue(asMapValue(body["payload"])["subject_ref"])) == "" {
		return nil, workInvalid("subject_ref is required")
	}
	prepared, err := prepareEventForInsert(actor, body)
	if err != nil {
		return nil, err
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
	if err := s.checkWorkspaceWriteQuota(ctx, 0, quotaWriteDelta{dbBytes: int64(len(prepared.PayloadJSON) + len(prepared.RefsJSON) + 4096)}, blobLedgerWritePlan{}); err != nil {
		return nil, err
	}
	preload, err := s.preloadAskCardContent(ctx, body)
	if err != nil {
		return nil, fmt.Errorf("preload ask card content: %w", err)
	}
	defer preload.discardUnlessCommitted()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	stored, staged, _, err := s.appendTaskAttentionEventTx(ctx, tx, actor, body)
	if err != nil {
		return nil, err
	}
	defer func() {
		if staged != nil {
			_ = staged.Cleanup()
		}
	}()
	// Promote before committing so a committed ask always has its content.
	if staged != nil {
		if err = staged.Promote(); err != nil {
			return nil, err
		}
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	preload.markCommitted()
	return stored, nil
}

// askCompatibilityCardInput is the card an ask about a non-card subject
// creates. Its content is preloaded before the filing transaction.
func askCompatibilityCardInput(payload, body map[string]any) AddBoardCardInput {
	return AddBoardCardInput{Title: firstNonEmpty(anyStringValue(payload["title"]), anyStringValue(body["summary"])), ColumnKey: "ready"}
}

// preloadAskCardContent preloads the compatibility cards the asks in bodies
// will create.
func (s *Store) preloadAskCardContent(ctx context.Context, bodies ...map[string]any) (*blobPreload, error) {
	preps := make([]boardCardInsertPrep, 0, len(bodies))
	for _, body := range bodies {
		payload := asMapValue(body["payload"])
		subjectType, _, err := schema.SplitTypedRef(strings.TrimSpace(anyStringValue(payload["subject_ref"])))
		if err != nil || subjectType == "card" {
			continue
		}
		prep, err := prepareBoardCardInsert(askCompatibilityCardInput(payload, body))
		if err != nil {
			continue
		}
		preps = append(preps, prep)
	}
	return s.preloadCardContent(ctx, preps...)
}

// appendTaskAttentionEventTx validates and inserts one task-addressed ask
// inside an open transaction. Batch filing reuses it so N asks commit or roll
// back together. On success the caller owns commit and the returned staged
// write (promote before commit, or cleanup); on error this helper discards it.
func (s *Store) appendTaskAttentionEventTx(ctx context.Context, tx *accessTx, actor string, body map[string]any) (map[string]any, blob.StagedWrite, []string, error) {
	payload := cloneMap(asMapValue(body["payload"]))
	body["payload"] = payload
	subject := strings.TrimSpace(anyStringValue(payload["subject_ref"]))
	if subject == "" {
		return nil, nil, nil, workInvalid("subject_ref is required")
	}
	prepared, err := prepareEventForInsert(actor, body)
	if err != nil {
		return nil, nil, nil, err
	}
	subjectType, _, err := schema.SplitTypedRef(subject)
	if err != nil {
		return nil, nil, nil, workInvalid("subject_ref must be a typed reference")
	}
	compatibility := subjectType != "card"
	if compatibility {
		if err := s.CheckResourceValues(ctx, body); err != nil {
			return nil, nil, nil, err
		}
	}
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
		return nil, nil, nil, ErrConflict
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, nil, nil, err
	}
	if compatibility {
		board, err := ensureAskSubjectBoardTx(ctx, tx, actor)
		if err != nil {
			return nil, nil, nil, err
		}
		boardID := board.ID
		refs, err := normalizeStringSlice(body["refs"])
		if err != nil {
			return nil, nil, nil, err
		}
		var related []string
		if payload["related_refs"] != nil {
			related, err = normalizeStringSlice(payload["related_refs"])
		}
		if err != nil {
			return nil, nil, nil, err
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
		prep, err := prepareBoardCardInsert(askCompatibilityCardInput(payload, body))
		if err != nil {
			return nil, nil, nil, err
		}
		prep.Refs = cardEvidence
		prep.RefsJSON, _ = json.Marshal(cardEvidence)
		var card boardCardRow
		_, card, staged, err = s.execBoardCardInsert(ctx, tx, board, actor, boardID, prep)
		if err != nil {
			return nil, nil, nil, err
		}
		if err = insertWorkMetadata(ctx, tx, card.CardID, actor, map[string]any{"source": map[string]any{"authority": "nexus"}, "related_refs": cardEvidence}); err != nil {
			return nil, nil, nil, err
		}
		projectionThreads = []string{board.ThreadID, card.ThreadID.String}
		subject = "card:" + card.Handle.String
		payload["related_refs"] = related
		body["refs"] = uniqueSortedStrings(append(evidence, subject))
	} else {
		resolved, err := resolveResourceRef(ctx, tx, ResourceRefInput{Type: "card", Ref: subject})
		if err != nil {
			return nil, nil, nil, err
		}
		var phase, archived, trashed string
		err = tx.QueryRowContext(ctx, `SELECT `+projectedWorkStringSQL("phase", `c.column_key`)+`,COALESCE(c.archived_at,''),COALESCE(c.trashed_at,'') FROM cards c LEFT JOIN work_metadata m ON m.card_id=c.id LEFT JOIN work_observations o ON o.id=m.latest_observation_id WHERE c.id=?`, resolved.ID).Scan(&phase, &archived, &trashed)
		if err != nil {
			return nil, nil, nil, err
		}
		if phase == "done" || phase == "cancelled" || archived != "" || trashed != "" {
			return nil, nil, nil, ErrHumanAttentionAlreadyResponded
		}
		subject = resolved.CanonicalRef
	}
	payload["subject_ref"] = subject
	body["id"] = prepared.Body["id"]
	prepared, err = prepareEventForInsert(actor, body)
	if err != nil {
		return nil, nil, nil, err
	}
	if err = insertPreparedEvent(ctx, tx, prepared); err != nil {
		return nil, nil, nil, err
	}
	if err = markTopicProjectionsDirtyTx(ctx, tx, projectionThreads, time.Now().UTC()); err != nil {
		return nil, nil, nil, err
	}
	out := staged
	staged = nil
	return prepared.Body, out, projectionThreads, nil
}
