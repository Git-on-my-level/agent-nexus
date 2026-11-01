// Input types and insert machinery for adding a card to a board: insert
// preparation, blob content preloading, and the transactional insert body.

package primitives

import (
	"agent-nexus-core/internal/handles"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"agent-nexus-core/internal/blob"
)

type CardCreateReplay struct {
	Scope, Key, Hash string
	Extras           map[string]any
}

type AddBoardCardInput struct {
	Handle                      string
	Under, After, CreateSurface string
	// Initiative files a card without a parent at the top level instead of
	// the Unsorted tray.
	Initiative bool
	// CreateReplay is internal request metadata, committed with the card.
	CreateReplay *CardCreateReplay

	// WorkMetadata is the optional one-to-one commitment extension, inserted atomically.
	WorkMetadata     map[string]any
	CardID           string
	Title            string
	Body             string
	ParentThreadID   string
	DueAt            *string
	DefinitionOfDone []string
	Assignee         *string
	ColumnKey        string
	BeforeCardID     string
	AfterCardID      string
	PinnedDocumentID *string
	Resolution       *string
	ResolutionRefs   []string
	Refs             []string
	Risk             *string
	IfBoardUpdatedAt *string
}

type BoardCardMutationResult struct {
	ReplayCommitted bool
	Board           map[string]any
	Card            map[string]any
}

// maxBoardCardsBatchSize caps POST /boards/{id}/cards/batch item count.
const maxBoardCardsBatchSize = 100

type boardCardInsertPrep struct {
	Handle               string
	CardID               string
	ColumnKey            string
	Title                string
	Body                 string
	SourceThreadID       string
	BackingThreadID      string
	Refs                 []string
	RefsJSON             []byte
	DueAt                *string
	DefinitionOfDone     []string
	DefinitionOfDoneJSON []byte
	Assignee             *string
	PinnedDocumentID     *string
	RiskValue            string
	Resolution           string
	ResolutionRefs       []string
	ResolutionRefsJSON   []byte
	BeforeCardID         string
	AfterCardID          string
}

// preloadCardContent preloads the first-revision content of cards about to be
// inserted. A card without a title takes one from its thread inside the
// transaction, so its content is only known there; it is promoted there.
func (s *Store) preloadCardContent(ctx context.Context, preps ...boardCardInsertPrep) (*blobPreload, error) {
	contents := make([]blobContent, 0, len(preps))
	for _, prep := range preps {
		if strings.TrimSpace(prep.Title) == "" {
			continue
		}
		data, err := cardRevisionContent(prep.Title, prep.Body, prep.DefinitionOfDone)
		if err != nil {
			return nil, err
		}
		contents = append(contents, blobContent{hash: sha256Hex(data), data: data})
	}
	return s.preloadBlobContent(ctx, contents...)
}

func prepareBoardCardInsert(input AddBoardCardInput) (boardCardInsertPrep, error) {
	if input.Handle != "" && !handles.ValidSlug(input.Handle) {
		return boardCardInsertPrep{}, invalidBoardRequest("card.handle must be a canonical lowercase slug of 1-64 characters, excluding reserved names and UUIDs")
	}
	cardID := strings.TrimSpace(input.CardID)
	if cardID == "" {
		cardID = uuid.NewString()
	}
	if err := validateCardID(cardID); err != nil {
		return boardCardInsertPrep{}, invalidBoardRequestError(err)
	}

	columnKey := strings.TrimSpace(input.ColumnKey)
	if columnKey == "" {
		columnKey = boardDefaultColumn
	}
	if err := validateBoardColumnKey(columnKey); err != nil {
		return boardCardInsertPrep{}, invalidBoardRequestError(err)
	}
	if err := ValidateBoardPlacementAnchors(input.BeforeCardID, input.AfterCardID); err != nil {
		return boardCardInsertPrep{}, invalidBoardRequestError(err)
	}

	refs := uniqueSortedStrings(input.Refs)
	refsJSON, err := json.Marshal(refs)
	if err != nil {
		return boardCardInsertPrep{}, fmt.Errorf("marshal card refs: %w", err)
	}

	sourceThreadID := strings.TrimSpace(input.ParentThreadID)
	if sourceThreadID == "" {
		derived, derr := SoleThreadRefIDFromRefs(refs)
		if derr != nil {
			return boardCardInsertPrep{}, invalidBoardRequestError(derr)
		}
		sourceThreadID = derived
	}
	if sourceThreadID != "" {
		if err := validateThreadID(sourceThreadID); err != nil {
			return boardCardInsertPrep{}, invalidBoardRequestError(err)
		}
	}
	backingThreadID := uuid.NewString()

	title := strings.TrimSpace(input.Title)
	body := strings.TrimSpace(input.Body)
	dueAt := normalizeBoardOptionalPointer(input.DueAt)
	definitionOfDone := uniqueSortedStrings(input.DefinitionOfDone)
	definitionOfDoneJSON, err := json.Marshal(definitionOfDone)
	if err != nil {
		return boardCardInsertPrep{}, fmt.Errorf("marshal card definition_of_done: %w", err)
	}
	resolution := ""
	if input.Resolution != nil {
		resolution = normalizeIncomingCardResolution(strings.TrimSpace(*input.Resolution))
	}
	resolutionRefs := uniqueSortedStrings(input.ResolutionRefs)
	if columnKey == "done" && resolution == "" && len(resolutionRefs) > 0 {
		resolution = "done"
	}
	if err := validateCardResolution(resolution, true); err != nil {
		return boardCardInsertPrep{}, invalidBoardRequestError(err)
	}
	if columnKey == "done" {
		if err := validateCardResolution(resolution, false); err != nil {
			return boardCardInsertPrep{}, invalidBoardRequestError(err)
		}
		if len(resolutionRefs) == 0 {
			return boardCardInsertPrep{}, invalidBoardRequest("done column requires resolution_refs")
		}
		if !containsTypedRefPrefix(resolutionRefs, "artifact") && !containsTypedRefPrefix(resolutionRefs, "event") {
			return boardCardInsertPrep{}, invalidBoardRequest("resolution_refs must include at least one artifact: or event: ref for resolution done")
		}
	}
	resolutionRefsJSON, err := json.Marshal(resolutionRefs)
	if err != nil {
		return boardCardInsertPrep{}, fmt.Errorf("marshal card resolution refs: %w", err)
	}
	assignee := normalizeBoardOptionalPointer(input.Assignee)
	pinnedDocumentID := normalizeBoardOptionalPointer(input.PinnedDocumentID)
	riskValue := canonicalBoardCardRisk("")
	if input.Risk != nil {
		riskValue = canonicalBoardCardRisk(*input.Risk)
	}
	if err := rejectReservedCardAssignee(assignee); err != nil {
		return boardCardInsertPrep{}, err
	}
	if err := validateBoardCardClientText("title", title); err != nil {
		return boardCardInsertPrep{}, err
	}
	if err := validateBoardCardClientText("body", body); err != nil {
		return boardCardInsertPrep{}, err
	}
	if err := validateBoardCardClientText("resolution", resolution); err != nil {
		return boardCardInsertPrep{}, err
	}
	for i, item := range definitionOfDone {
		if err := validateBoardCardClientText(fmt.Sprintf("definition_of_done[%d]", i), item); err != nil {
			return boardCardInsertPrep{}, err
		}
	}
	if assignee != nil {
		if err := validateBoardCardClientText("assignee", *assignee); err != nil {
			return boardCardInsertPrep{}, err
		}
	}
	if dueAt != nil {
		if err := validateBoardCardClientText("due_at", *dueAt); err != nil {
			return boardCardInsertPrep{}, err
		}
	}
	if pinnedDocumentID != nil {
		if err := validateBoardCardClientText("pinned_document_id", *pinnedDocumentID); err != nil {
			return boardCardInsertPrep{}, err
		}
	}
	if err := validateBoardCardTypedRefs("refs", refs); err != nil {
		return boardCardInsertPrep{}, err
	}
	if err := validateBoardCardTypedRefs("resolution_refs", resolutionRefs); err != nil {
		return boardCardInsertPrep{}, err
	}

	return boardCardInsertPrep{
		Handle:               input.Handle,
		CardID:               cardID,
		ColumnKey:            columnKey,
		Title:                title,
		Body:                 body,
		SourceThreadID:       sourceThreadID,
		BackingThreadID:      backingThreadID,
		Refs:                 refs,
		RefsJSON:             refsJSON,
		DueAt:                dueAt,
		DefinitionOfDone:     definitionOfDone,
		DefinitionOfDoneJSON: definitionOfDoneJSON,
		Assignee:             assignee,
		PinnedDocumentID:     pinnedDocumentID,
		RiskValue:            riskValue,
		Resolution:           resolution,
		ResolutionRefs:       resolutionRefs,
		ResolutionRefsJSON:   resolutionRefsJSON,
		BeforeCardID:         input.BeforeCardID,
		AfterCardID:          input.AfterCardID,
	}, nil
}

func (s *Store) execBoardCardInsert(ctx context.Context, tx *accessTx, boardRow boardRow, actorID, boardID string, prep boardCardInsertPrep) (boardRow, boardCardRow, blob.StagedWrite, error) {
	if prep.ColumnKey == "done" {
		if err := validateResolutionRefs(ctx, tx, prep.ResolutionRefs); err != nil {
			return boardRow, boardCardRow{}, nil, err
		}
	}
	if err := ensureBoardColumnAllowed(boardRow, prep.ColumnKey); err != nil {
		return boardRow, boardCardRow{}, nil, err
	}
	cardID := prep.CardID
	columnKey := prep.ColumnKey
	sourceThreadID := prep.SourceThreadID
	title := prep.Title
	body := prep.Body
	dueAt := prep.DueAt
	assignee := prep.Assignee
	pinnedDocumentID := prep.PinnedDocumentID
	riskValue := prep.RiskValue
	resolutionStr := prep.Resolution
	refs := prep.Refs
	refsJSON := prep.RefsJSON
	definitionOfDone := prep.DefinitionOfDone
	definitionOfDoneJSON := prep.DefinitionOfDoneJSON
	resolutionRefsJSON := prep.ResolutionRefsJSON
	backingThreadID := prep.BackingThreadID

	if sourceThreadID != "" {
		if err := ensureThreadExists(ctx, tx, sourceThreadID); err != nil {
			return boardRow, boardCardRow{}, nil, err
		}
		if sourceThreadID == boardRow.ThreadID {
			return boardRow, boardCardRow{}, nil, invalidBoardRequest("board.thread_id cannot be added as a board card")
		}
		if err := ensureBoardCardParentThreadAvailable(ctx, tx, boardID, sourceThreadID, ""); err != nil {
			return boardRow, boardCardRow{}, nil, err
		}
	}
	if title == "" {
		if derivedTitle, err := loadThreadTitleForBoardCard(ctx, tx, sourceThreadID); err == nil {
			title = derivedTitle
		} else if !errors.Is(err, ErrNotFound) {
			return boardRow, boardCardRow{}, nil, err
		}
	}
	if title == "" {
		title = cardID
	}
	if title == "" {
		return boardRow, boardCardRow{}, nil, invalidBoardRequest("card.title is required")
	}
	if pinnedDocumentID != nil {
		if err := ensureDocumentExists(ctx, tx, *pinnedDocumentID); err != nil {
			return boardRow, boardCardRow{}, nil, err
		}
	}

	beforeCardID, afterCardID, err := resolveBoardPlacementAnchors(ctx, tx, boardID, prep.BeforeCardID, prep.AfterCardID)
	if err != nil {
		return boardRow, boardCardRow{}, nil, err
	}
	rank, err := s.allocateBoardCardRank(ctx, tx, boardID, columnKey, beforeCardID, afterCardID, "")
	if err != nil {
		return boardRow, boardCardRow{}, nil, err
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	provenanceJSON := inferredProvenanceJSON()
	if err := ensureCardBackingThreadTx(ctx, tx, actorID, cardID, backingThreadID, title, now); err != nil {
		return boardRow, boardCardRow{}, nil, err
	}
	revision, stagedContent, err := s.prepareCardRevisionInsert(ctx, actorID, cardID, 1, sql.NullString{}, backingThreadID, title, body, definitionOfDone, refs)
	if err != nil {
		return boardRow, boardCardRow{}, nil, err
	}
	cardHandle, err := allocateCardHandleTx(ctx, tx, prep.Handle, title, "card-"+cardID)
	if err != nil {
		return boardRow, boardCardRow{}, stagedContent, fmt.Errorf("allocate card handle: %w", err)
	}

	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO cards(
			id, handle, board_id, thread_id, title, summary, due_at, definition_of_done_json, column_key, rank, version, head_revision_id, head_revision_number,
			parent_thread_id, pinned_document_id, assignee, risk, resolution, resolution_refs_json, refs_json,
			created_at, created_by, updated_at, updated_by, provenance_json, needs_home
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		cardID,
		cardHandle,
		boardID,
		backingThreadID,
		title,
		body,
		nullableString(derefBoardString(dueAt)),
		string(definitionOfDoneJSON),
		columnKey,
		rank,
		1,
		revision.RevisionID,
		revision.RevisionNumber,
		nullableString(sourceThreadID),
		nullableString(derefBoardString(pinnedDocumentID)),
		nullableString(derefBoardString(assignee)),
		riskValue,
		nullableString(resolutionStr),
		string(resolutionRefsJSON),
		string(refsJSON),
		now,
		actorID,
		now,
		actorID,
		provenanceJSON,
		// New work waits in the Unsorted tray until it names a parent; only the
		// initiatives board (kept for layout) still creates top-level work.
		boardRow.Role != "initiatives",
	); err != nil {
		if isUniqueViolation(err) {
			return boardRow, boardCardRow{}, stagedContent, ErrConflict
		}
		return boardRow, boardCardRow{}, stagedContent, fmt.Errorf("insert card: %w", err)
	}

	if err := s.insertCardRevisionTx(ctx, tx, actorID, cardID, cardHandle, backingThreadID, revision); err != nil {
		return boardRow, boardCardRow{}, stagedContent, err
	}

	if err := upsertBoardCardRefEdge(ctx, tx, boardID, cardID, columnKey, rank); err != nil {
		return boardRow, boardCardRow{}, stagedContent, err
	}
	cardTargets := typedRefEdgeTargets(refEdgeTypeRef, refs)
	cardTargets = appendRefEdgeTarget(cardTargets, refEdgeTypeCardParentThread, "thread", sourceThreadID)
	cardTargets = appendRefEdgeTarget(cardTargets, refEdgeTypeCardPinnedDocument, "document", derefBoardString(pinnedDocumentID))
	if err := replaceRefEdges(ctx, tx, "card", cardID, cardTargets); err != nil {
		return boardRow, boardCardRow{}, stagedContent, err
	}

	boardRow, err = touchBoardRow(ctx, tx, boardRow, actorID)
	if err != nil {
		return boardRow, boardCardRow{}, stagedContent, err
	}
	cardRow, err := s.loadBoardCardByIdentifier(ctx, tx, boardID, cardID, true)
	if err != nil {
		return boardRow, boardCardRow{}, stagedContent, err
	}
	if err := registerCardTrackingTx(ctx, tx, actorID, &cardRow); err != nil {
		return boardRow, boardCardRow{}, stagedContent, err
	}
	if err := markBoardCardProjectionDirtyTx(ctx, tx, cardRow); err != nil {
		return boardRow, boardCardRow{}, stagedContent, err
	}
	return boardRow, cardRow, stagedContent, nil
}
