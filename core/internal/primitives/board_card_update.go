// Store.UpdateBoardCard, its input type, and the mutability and projection
// invalidation helpers shared by the card mutations.

package primitives

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"
)

type UpdateBoardCardInput struct {
	Title            *string
	Body             *string
	ParentThreadID   *string
	DueAt            *string
	DefinitionOfDone *[]string
	Assignee         *string
	PinnedDocumentID *string
	Resolution       *string
	ResolutionRefs   *[]string
	Refs             *[]string
	Risk             *string
	IfBoardUpdatedAt *string
}

func (s *Store) UpdateBoardCard(ctx context.Context, actorID, boardID, identifier string, input UpdateBoardCardInput) (mutationResult BoardCardMutationResult, mutationErr error) {
	if s == nil || s.db == nil {
		return BoardCardMutationResult{}, fmt.Errorf("primitives store database is not initialized")
	}
	if strings.TrimSpace(actorID) == "" {
		return BoardCardMutationResult{}, invalidBoardRequest("actorID is required")
	}

	tx, err := mutationTxBegin(ctx, s.db)
	if err != nil {
		return BoardCardMutationResult{}, fmt.Errorf("begin board card update transaction: %w", err)
	}
	// The transaction now holds the workspace writer: from here any
	// failure leaves the outcome unknown unless it is a deterministic
	// request rejection.
	defer func() {
		if mutationErr != nil {
			mutationErr = mutationOutcomeAfterBegin(mutationErr)
		}
	}()

	var cardRow boardCardRow
	if strings.TrimSpace(boardID) != "" {
		cardRow, err = s.loadBoardCardByIdentifier(ctx, tx, boardID, identifier, true)
	} else {
		cardRow, err = s.loadBoardCardByGlobalID(ctx, tx, identifier, true)
	}
	if err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return BoardCardMutationResult{}, err
	}
	var boardRow boardRow
	if err := ensureNativeWorkMutation(ctx, tx, cardRow.CardID); err != nil {
		_ = tx.Rollback()
		return BoardCardMutationResult{}, err
	}
	if err := ensureBoardCardMutable(cardRow); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return BoardCardMutationResult{}, err
	}
	if input.Title != nil || input.Body != nil || input.DefinitionOfDone != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return BoardCardMutationResult{}, invalidBoardRequest("card content must be changed with POST /cards/{card_id}/revisions")
	}
	if strings.TrimSpace(boardID) == "" {
		if err := ensureUpdatedAtMatches(cardRow.UpdatedAt, input.IfBoardUpdatedAt); err != nil {
			if rbErr := tx.Rollback(); rbErr != nil {
				log.Printf("tx rollback failed: %v", rbErr)
			}
			return BoardCardMutationResult{}, err
		}
		boardID = cardRow.BoardID
		if strings.TrimSpace(boardID) != "" {
			boardRow, err = loadBoardRow(ctx, tx, boardID)
			if err != nil {
				if rbErr := tx.Rollback(); rbErr != nil {
					log.Printf("tx rollback failed: %v", rbErr)
				}
				return BoardCardMutationResult{}, err
			}
		}
	} else {
		boardRow, err = loadBoardRow(ctx, tx, boardID)
		if err != nil {
			if rbErr := tx.Rollback(); rbErr != nil {
				log.Printf("tx rollback failed: %v", rbErr)
			}
			return BoardCardMutationResult{}, err
		}
		if err := ensureBoardUpdatedAtMatches(boardRow, input.IfBoardUpdatedAt); err != nil {
			if rbErr := tx.Rollback(); rbErr != nil {
				log.Printf("tx rollback failed: %v", rbErr)
			}
			return BoardCardMutationResult{}, err
		}
	}

	nextTitle := cardRow.Title
	nextBody := cardRow.Body
	nextParentThread := strings.TrimSpace(cardRow.ParentThreadID.String)
	if input.ParentThreadID != nil {
		nextParentThread = strings.TrimSpace(*input.ParentThreadID)
	}
	nextThreadID := strings.TrimSpace(firstNonEmpty(cardRow.ThreadID.String, nextParentThread))
	nextDueAt := strings.TrimSpace(cardRow.DueAt.String)
	if input.DueAt != nil {
		nextDueAt = strings.TrimSpace(*input.DueAt)
	}
	nextDefinitionOfDoneJSON := cardRow.DefinitionOfDoneJSON
	nextAssignee := strings.TrimSpace(cardRow.Assignee.String)
	if input.Assignee != nil {
		nextAssignee = strings.TrimSpace(*input.Assignee)
		if err := rejectReservedCardAssignee(&nextAssignee); err != nil {
			if rbErr := tx.Rollback(); rbErr != nil {
				log.Printf("tx rollback failed: %v", rbErr)
			}
			return BoardCardMutationResult{}, err
		}
	}
	nextPinnedDocumentID := strings.TrimSpace(cardRow.PinnedDocumentID.String)
	if input.PinnedDocumentID != nil {
		nextPinnedDocumentID = strings.TrimSpace(*input.PinnedDocumentID)
	}
	nextResolution := canonicalizeStoredCardResolution(cardRow.Resolution.String)
	if input.Resolution != nil {
		nextResolution = normalizeIncomingCardResolution(strings.TrimSpace(*input.Resolution))
	}
	if err := validateCardResolution(nextResolution, true); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return BoardCardMutationResult{}, invalidBoardRequestError(err)
	}
	nextResolutionRefsJSON := cardRow.ResolutionRefsJSON
	if input.ResolutionRefs != nil {
		resolutionRefs := uniqueSortedStrings(*input.ResolutionRefs)
		resolutionRefsBytes, marshalErr := json.Marshal(resolutionRefs)
		if marshalErr != nil {
			if rbErr := tx.Rollback(); rbErr != nil {
				log.Printf("tx rollback failed: %v", rbErr)
			}
			return BoardCardMutationResult{}, fmt.Errorf("marshal card resolution refs: %w", marshalErr)
		}
		nextResolutionRefsJSON = string(resolutionRefsBytes)
	}
	nextRefsJSON := cardRow.RefsJSON
	if input.Refs != nil {
		refs := uniqueSortedStrings(*input.Refs)
		refsBytes, marshalErr := json.Marshal(refs)
		if marshalErr != nil {
			if rbErr := tx.Rollback(); rbErr != nil {
				log.Printf("tx rollback failed: %v", rbErr)
			}
			return BoardCardMutationResult{}, fmt.Errorf("marshal card refs: %w", marshalErr)
		}
		nextRefsJSON = string(refsBytes)
	}
	nextRisk := canonicalBoardCardRisk(cardRow.Risk)
	if input.Risk != nil {
		nextRisk = canonicalBoardCardRisk(*input.Risk)
	}

	if strings.TrimSpace(cardRow.ColumnKey) == "done" && nextResolution == "" {
		var refs []string
		if err := json.Unmarshal([]byte(nextResolutionRefsJSON), &refs); err == nil && len(uniqueSortedStrings(refs)) > 0 {
			nextResolution = "done"
		}
	}
	columnKey := strings.TrimSpace(cardRow.ColumnKey)
	if columnKey == "done" {
		if err := validateCardResolution(nextResolution, false); err != nil {
			if rbErr := tx.Rollback(); rbErr != nil {
				log.Printf("tx rollback failed: %v", rbErr)
			}
			return BoardCardMutationResult{}, invalidBoardRequestError(err)
		}
		var refs []string
		if err := json.Unmarshal([]byte(nextResolutionRefsJSON), &refs); err != nil {
			if rbErr := tx.Rollback(); rbErr != nil {
				log.Printf("tx rollback failed: %v", rbErr)
			}
			return BoardCardMutationResult{}, fmt.Errorf("unmarshal resolution refs: %w", err)
		}
		refs = uniqueSortedStrings(refs)
		if len(refs) == 0 {
			if rbErr := tx.Rollback(); rbErr != nil {
				log.Printf("tx rollback failed: %v", rbErr)
			}
			return BoardCardMutationResult{}, invalidBoardRequest("done column requires resolution_refs")
		}
		if err := validateResolutionRefs(ctx, tx, refs); err != nil {
			_ = tx.Rollback()
			return BoardCardMutationResult{}, err
		}
		if !containsTypedRefPrefix(refs, "artifact") && !containsTypedRefPrefix(refs, "event") {
			if rbErr := tx.Rollback(); rbErr != nil {
				log.Printf("tx rollback failed: %v", rbErr)
			}
			return BoardCardMutationResult{}, invalidBoardRequest("resolution_refs must include at least one artifact: or event: ref for resolution done")
		}
	} else if nextResolution != "" {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return BoardCardMutationResult{}, invalidBoardRequest("resolution must be null when column_key is not done")
	}

	if nextParentThread != "" {
		if err := ensureThreadExists(ctx, tx, nextParentThread); err != nil {
			if rbErr := tx.Rollback(); rbErr != nil {
				log.Printf("tx rollback failed: %v", rbErr)
			}
			return BoardCardMutationResult{}, err
		}
		if nextParentThread == boardRow.ThreadID {
			if rbErr := tx.Rollback(); rbErr != nil {
				log.Printf("tx rollback failed: %v", rbErr)
			}
			return BoardCardMutationResult{}, invalidBoardRequest("board.thread_id cannot be added as a board card")
		}
		if err := ensureBoardCardParentThreadAvailable(ctx, tx, boardID, nextParentThread, cardRow.CardID); err != nil {
			if rbErr := tx.Rollback(); rbErr != nil {
				log.Printf("tx rollback failed: %v", rbErr)
			}
			return BoardCardMutationResult{}, err
		}
	}
	if nextPinnedDocumentID != "" {
		if err := ensureDocumentExists(ctx, tx, nextPinnedDocumentID); err != nil {
			if rbErr := tx.Rollback(); rbErr != nil {
				log.Printf("tx rollback failed: %v", rbErr)
			}
			return BoardCardMutationResult{}, err
		}
	}
	if nextTitle == cardRow.Title &&
		nextBody == cardRow.Body &&
		nextParentThread == strings.TrimSpace(cardRow.ParentThreadID.String) &&
		nextAssignee == strings.TrimSpace(cardRow.Assignee.String) &&
		nextPinnedDocumentID == strings.TrimSpace(cardRow.PinnedDocumentID.String) &&
		nextThreadID == strings.TrimSpace(firstNonEmpty(cardRow.ThreadID.String, cardRow.ParentThreadID.String)) &&
		nextDueAt == strings.TrimSpace(cardRow.DueAt.String) &&
		nextDefinitionOfDoneJSON == cardRow.DefinitionOfDoneJSON &&
		nextResolution == strings.TrimSpace(cardRow.Resolution.String) &&
		nextResolutionRefsJSON == cardRow.ResolutionRefsJSON &&
		nextRefsJSON == cardRow.RefsJSON &&
		nextRisk == canonicalBoardCardRisk(cardRow.Risk) {
		boardMap, mapErr := boardRowToAPI(ctx, tx, boardRow)
		if mapErr != nil {
			if rbErr := tx.Rollback(); rbErr != nil {
				log.Printf("tx rollback failed: %v", rbErr)
			}
			return BoardCardMutationResult{}, mapErr
		}
		cardMap, mapErr := cardRow.toMap()
		if mapErr != nil {
			if rbErr := tx.Rollback(); rbErr != nil {
				log.Printf("tx rollback failed: %v", rbErr)
			}
			return BoardCardMutationResult{}, mapErr
		}
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return BoardCardMutationResult{Board: boardMap, Card: cardMap}, nil
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := tx.ExecContext(
		ctx,
		`UPDATE cards
		    SET board_id = ?, thread_id = ?, title = ?, summary = ?, due_at = ?, definition_of_done_json = ?, column_key = ?, rank = ?,
		        parent_thread_id = ?, pinned_document_id = ?, assignee = ?, risk = ?, resolution = ?, resolution_refs_json = ?, refs_json = ?,
		        updated_at = ?, updated_by = ?
		  WHERE id = ?`,
		boardID,
		nextThreadID,
		nextTitle,
		nextBody,
		nullableString(nextDueAt),
		nextDefinitionOfDoneJSON,
		cardRow.ColumnKey,
		cardRow.Rank,
		nullableString(nextParentThread),
		nullableString(nextPinnedDocumentID),
		nullableString(nextAssignee),
		nextRisk,
		nullableString(nextResolution),
		nextResolutionRefsJSON,
		nextRefsJSON,
		now,
		actorID,
		cardRow.CardID,
	); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return BoardCardMutationResult{}, fmt.Errorf("update board card: %w", err)
	}
	var refsForEdges []string
	if err := json.Unmarshal([]byte(nextRefsJSON), &refsForEdges); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return BoardCardMutationResult{}, fmt.Errorf("unmarshal card refs for ref_edges: %w", err)
	}
	refsForEdges = uniqueSortedStrings(refsForEdges)
	cardTargets := typedRefEdgeTargets(refEdgeTypeRef, refsForEdges)
	cardTargets = appendRefEdgeTarget(cardTargets, refEdgeTypeCardParentThread, "thread", nextParentThread)
	cardTargets = appendRefEdgeTarget(cardTargets, refEdgeTypeCardPinnedDocument, "document", nextPinnedDocumentID)
	if err := replaceRefEdges(ctx, tx, "card", cardRow.CardID, cardTargets); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return BoardCardMutationResult{}, err
	}
	if err := upsertBoardCardRefEdge(ctx, tx, boardID, cardRow.CardID, cardRow.ColumnKey, cardRow.Rank); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return BoardCardMutationResult{}, err
	}

	boardRow, err = touchBoardRow(ctx, tx, boardRow, actorID)
	if err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return BoardCardMutationResult{}, err
	}
	previousCardRow := cardRow
	cardRow, err = s.loadBoardCardByIdentifier(ctx, tx, boardID, cardRow.CardID, true)
	if err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return BoardCardMutationResult{}, err
	}

	if err := markBoardCardProjectionDirtyTx(ctx, tx, previousCardRow, cardRow); err != nil {
		_ = tx.Rollback()
		return BoardCardMutationResult{}, err
	}
	if err := tx.Commit(); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return BoardCardMutationResult{}, fmt.Errorf("commit board card update transaction: %w", err)
	}

	boardMap, err := boardRowToAPI(ctx, s.db, boardRow)
	if err != nil {
		return BoardCardMutationResult{}, err
	}
	cardMap, err := cardRow.toMap()
	if err != nil {
		return BoardCardMutationResult{}, err
	}
	return BoardCardMutationResult{Board: boardMap, Card: cardMap}, nil
}

func ensureBoardCardMutable(card boardCardRow) error {
	if card.TrashedAt.Valid && strings.TrimSpace(card.TrashedAt.String) != "" {
		return invalidBoardRequest("card is trashed")
	}
	if card.ArchivedAt.Valid && strings.TrimSpace(card.ArchivedAt.String) != "" {
		return invalidBoardRequest("card is archived")
	}
	return nil
}

// Invalidates only the card's own and explicitly associated threads, never all workspace threads.
func markBoardCardProjectionDirtyTx(ctx context.Context, tx *accessTx, cards ...boardCardRow) error {
	var threadIDs []string
	for _, card := range cards {
		threadIDs = append(threadIDs, card.ThreadID.String, card.ParentThreadID.String)
		refs, err := decodeStoredJSONList(card.RefsJSON, "card.refs")
		if err != nil {
			return err
		}
		for _, ref := range refs {
			if kind, _, ok := splitTypedRef(ref); ok && kind == "thread" {
				resolved, err := resolveResourceRef(ctx, tx, ResourceRefInput{Type: kind, Ref: ref})
				if err != nil {
					if errors.Is(err, ErrNotFound) {
						continue
					}
					return err
				}
				threadIDs = append(threadIDs, resolved.ID)
			}
		}
	}
	return markTopicProjectionsDirtyTx(ctx, tx, threadIDs, time.Now().UTC())
}
