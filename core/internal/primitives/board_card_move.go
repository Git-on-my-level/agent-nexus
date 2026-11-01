// Moving a card between board columns: placement anchor resolution and
// validation, and move-driven resolution changes.

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

type MoveBoardCardInput struct {
	BoardRef string
	Reason   string
	// IfWorkVersion binds PM authorization to the canonical work revision.
	IfWorkVersion    *int64
	ColumnKey        string
	BeforeCardID     string
	AfterCardID      string
	Resolution       *string
	ResolutionRefs   *[]string
	IfBoardUpdatedAt *string
}

func (s *Store) MoveBoardCard(ctx context.Context, actorID, boardID, identifier string, input MoveBoardCardInput) (mutationResult BoardCardMutationResult, mutationErr error) {
	if s == nil || s.db == nil {
		return BoardCardMutationResult{}, fmt.Errorf("primitives store database is not initialized")
	}
	if strings.TrimSpace(actorID) == "" {
		return BoardCardMutationResult{}, invalidBoardRequest("actorID is required")
	}
	if len([]rune(input.Reason)) > 2000 {
		return BoardCardMutationResult{}, invalidBoardRequest("reason must be at most 2000 characters")
	}

	columnKey := strings.TrimSpace(input.ColumnKey)
	if columnKey == "" {
		return BoardCardMutationResult{}, invalidBoardRequest("column_key is required")
	}
	if err := validateBoardColumnKey(columnKey); err != nil {
		return BoardCardMutationResult{}, invalidBoardRequestError(err)
	}
	if err := ValidateBoardPlacementAnchors(input.BeforeCardID, input.AfterCardID); err != nil {
		return BoardCardMutationResult{}, invalidBoardRequestError(err)
	}

	tx, err := mutationTxBegin(ctx, s.db)
	if err != nil {
		return BoardCardMutationResult{}, fmt.Errorf("begin board card move transaction: %w", err)
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
	var boardRow boardRow
	if strings.TrimSpace(boardID) == "" {
		cardRow, err = s.loadBoardCardByGlobalID(ctx, tx, identifier, true)
		if err != nil {
			if rbErr := tx.Rollback(); rbErr != nil {
				log.Printf("tx rollback failed: %v", rbErr)
			}
			return BoardCardMutationResult{}, err
		}
		boardID = cardRow.BoardID
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
		cardRow, err = s.loadBoardCardByIdentifier(ctx, tx, boardID, identifier, true)
	}
	if err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return BoardCardMutationResult{}, err
	}
	// Every board move advances the work revision, including ordinary UI moves.
	if _, err = tx.ExecContext(ctx, `INSERT INTO work_metadata(card_id,authority,metadata_json,version,updated_at,updated_by) VALUES(?,'nexus','{"source":{"authority":"nexus"}}',0,?,?) ON CONFLICT(card_id) DO NOTHING`, cardRow.CardID, time.Now().UTC().Format(time.RFC3339Nano), actorID); err != nil {
		_ = tx.Rollback()
		return BoardCardMutationResult{}, err
	}
	var expected any
	if input.IfWorkVersion != nil {
		expected = *input.IfWorkVersion
	}
	result, err := tx.ExecContext(ctx, `UPDATE work_metadata SET version=version+1,updated_at=?,updated_by=? WHERE card_id=? AND (? IS NULL OR version=?)`, time.Now().UTC().Format(time.RFC3339Nano), actorID, cardRow.CardID, expected, expected)
	if err != nil {
		_ = tx.Rollback()
		return BoardCardMutationResult{}, err
	}
	changed, err := result.RowsAffected()
	if err != nil || changed != 1 {
		_ = tx.Rollback()
		return BoardCardMutationResult{}, ErrConflict
	}
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
	if strings.TrimSpace(input.BoardRef) != "" {
		resolved, e := resolveResourceRef(ctx, tx, ResourceRefInput{Type: "board", Ref: input.BoardRef})
		if e != nil {
			_ = tx.Rollback()
			return BoardCardMutationResult{}, e
		}
		destination, e := loadBoardRow(ctx, tx, resolved.ID)
		if e != nil {
			_ = tx.Rollback()
			return BoardCardMutationResult{}, e
		}
		if destination.ID != cardRow.BoardID {
			cardRow, e = s.moveCardBoardTx(ctx, tx, actorID, cardRow, destination)
			if e != nil {
				_ = tx.Rollback()
				return BoardCardMutationResult{}, e
			}
		}
		boardID = destination.ID
		boardRow = destination
	}

	beforeCardID, afterCardID, err := resolveBoardPlacementAnchors(ctx, tx, boardID, input.BeforeCardID, input.AfterCardID)
	if err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return BoardCardMutationResult{}, err
	}
	if err := validateBoardAnchors(ctx, tx, boardID, columnKey, beforeCardID, afterCardID, cardRow.CardID); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return BoardCardMutationResult{}, err
	}

	rank, err := s.allocateBoardCardRank(ctx, tx, boardID, columnKey, beforeCardID, afterCardID, cardRow.CardID)
	if err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return BoardCardMutationResult{}, err
	}

	nextResolution, nextResolutionRefsJSON, updateCard, err := resolveBoardCardMoveResolution(cardRow, columnKey, input)
	if err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return BoardCardMutationResult{}, err
	}

	if columnKey == "done" {
		var refs []string
		if err := json.Unmarshal([]byte(nextResolutionRefsJSON), &refs); err != nil {
			_ = tx.Rollback()
			return BoardCardMutationResult{}, err
		}
		if err := validateResolutionRefs(ctx, tx, refs); err != nil {
			_ = tx.Rollback()
			return BoardCardMutationResult{}, err
		}
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	if err := upsertBoardCardRefEdge(ctx, tx, boardID, cardRow.CardID, columnKey, rank); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return BoardCardMutationResult{}, err
	}

	if updateCard {
		if _, err := tx.ExecContext(
			ctx,
			`UPDATE cards
			    SET board_id = ?, thread_id = ?, column_key = ?, rank = ?, resolution = ?, resolution_refs_json = ?,
			        updated_at = ?, updated_by = ?
			  WHERE id = ?`,
			boardID,
			firstNonEmpty(cardRow.ThreadID.String, cardRow.ParentThreadID.String),
			columnKey,
			rank,
			nullableString(nextResolution),
			nextResolutionRefsJSON,
			now,
			actorID,
			cardRow.CardID,
		); err != nil {
			if rbErr := tx.Rollback(); rbErr != nil {
				log.Printf("tx rollback failed: %v", rbErr)
			}
			return BoardCardMutationResult{}, fmt.Errorf("update board card resolution: %w", err)
		}
		cardRow, err = s.loadBoardCardByIdentifier(ctx, tx, boardID, cardRow.CardID, true)
		if err != nil {
			if rbErr := tx.Rollback(); rbErr != nil {
				log.Printf("tx rollback failed: %v", rbErr)
			}
			return BoardCardMutationResult{}, err
		}
	} else {
		cardRow.ColumnKey = columnKey
		cardRow.Rank = rank
	}

	boardRow, err = touchBoardRow(ctx, tx, boardRow, actorID)
	if err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return BoardCardMutationResult{}, err
	}

	if err := markBoardCardProjectionDirtyTx(ctx, tx, cardRow); err != nil {
		_ = tx.Rollback()
		return BoardCardMutationResult{}, err
	}
	if err := tx.Commit(); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return BoardCardMutationResult{}, &MutationOutcomeUnknown{Cause: fmt.Errorf("commit board card move transaction: %w", err)}
	}

	boardMap, err := boardRowToAPI(ctx, s.db, boardRow)
	if err != nil {
		return BoardCardMutationResult{}, &MutationOutcomeUnknown{Cause: err}
	}
	cardMap, err := cardRow.toMap()
	if err != nil {
		return BoardCardMutationResult{}, &MutationOutcomeUnknown{Cause: err}
	}
	return BoardCardMutationResult{Board: boardMap, Card: cardMap}, nil
}

func validateBoardAnchors(ctx context.Context, tx *accessTx, boardID, targetColumn, beforeCardID, afterCardID, movingCardID string) error {
	anchorCardID := beforeCardID
	if anchorCardID == "" {
		anchorCardID = afterCardID
	}
	anchorCardID = strings.TrimSpace(anchorCardID)
	if anchorCardID == "" {
		return nil
	}
	if anchorCardID == strings.TrimSpace(movingCardID) {
		return invalidBoardRequest("placement anchor cannot reference the moving card")
	}
	anchor, err := loadBoardCardRow(ctx, tx, boardID, anchorCardID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return invalidBoardRequest("placement anchor must reference a card already on the board")
		}
		return err
	}
	if anchor.ColumnKey != targetColumn {
		return invalidBoardRequest("placement anchor must reference a card in the target column")
	}
	return nil
}

func resolveBoardPlacementAnchors(ctx context.Context, rower queryRower, boardID, beforeCardID, afterCardID string) (string, string, error) {
	before, err := resolveBoardPlacementAnchor(ctx, rower, beforeCardID)
	if err != nil {
		return "", "", err
	}
	after, err := resolveBoardPlacementAnchor(ctx, rower, afterCardID)
	if err != nil {
		return "", "", err
	}
	return before, after, nil
}

func resolveBoardPlacementAnchor(ctx context.Context, rower queryRower, raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	resolved, err := resolveResourceRef(ctx, rower, ResourceRefInput{Type: "card", Ref: raw})
	if err != nil {
		if errors.Is(err, ErrNotFound) || errors.Is(err, ErrInvalidResourceRef) {
			return "", invalidBoardRequest("placement anchor must reference a card already on the board")
		}
		return "", err
	}
	id := strings.TrimSpace(resolved.ID)
	if id == "" {
		return "", invalidBoardRequest("placement anchor must reference a card already on the board")
	}
	return id, nil
}

func resolveBoardCardMoveResolution(cardRow boardCardRow, columnKey string, input MoveBoardCardInput) (string, string, bool, error) {
	columnKey = strings.TrimSpace(columnKey)
	currentResolution := canonicalizeStoredCardResolution(cardRow.Resolution.String)
	effectiveResolution := input.Resolution
	if effectiveResolution == nil && columnKey == "done" && input.ResolutionRefs != nil {
		rr := uniqueSortedStrings(*input.ResolutionRefs)
		if len(rr) > 0 {
			done := "done"
			effectiveResolution = &done
		}
	}
	if effectiveResolution == nil {
		if columnKey != "done" {
			resolutionRefs, err := decodeStoredJSONList(cardRow.ResolutionRefsJSON, "card.resolution_refs")
			if err != nil {
				return "", "", false, err
			}
			hasRefs := len(resolutionRefs) > 0
			if currentResolution != "" || hasRefs {
				emptyRefs, err := json.Marshal([]string{})
				if err != nil {
					return "", "", false, fmt.Errorf("marshal cleared resolution refs: %w", err)
				}
				return "", string(emptyRefs), true, nil
			}
			return "", "", false, nil
		}
		if strings.TrimSpace(cardRow.ColumnKey) == "done" && currentResolution != "" {
			return currentResolution, cardRow.ResolutionRefsJSON, false, nil
		}
		if currentResolution == "" {
			return "", "", false, invalidBoardRequest("resolution is required when column_key is done")
		}
		return "", "", false, invalidBoardRequest("resolution is required when column_key is done")
	}

	nextResolution := normalizeIncomingCardResolution(strings.TrimSpace(*effectiveResolution))
	if err := validateCardResolution(nextResolution, false); err != nil {
		return "", "", false, invalidBoardRequestError(err)
	}
	if columnKey != "done" {
		return "", "", false, invalidBoardRequest("resolution requires column_key done")
	}
	if input.ResolutionRefs == nil {
		return "", "", false, invalidBoardRequest("resolution_refs are required when resolution is set")
	}

	resolutionRefs := uniqueSortedStrings(*input.ResolutionRefs)
	if len(resolutionRefs) == 0 {
		return "", "", false, invalidBoardRequest("resolution_refs are required when resolution is set")
	}
	for _, ref := range resolutionRefs {
		if _, _, ok := normalizeTypedRef(ref); !ok {
			return "", "", false, invalidBoardRequest(fmt.Sprintf("invalid typed ref %q", strings.TrimSpace(ref)))
		}
	}
	switch nextResolution {
	case "done":
		if !containsTypedRefPrefix(resolutionRefs, "artifact") && !containsTypedRefPrefix(resolutionRefs, "event") {
			return "", "", false, invalidBoardRequest("resolution_refs must include at least one artifact: or event: ref for resolution done")
		}
	default:
		return "", "", false, invalidBoardRequest("resolution must be done")
	}
	resolutionRefsJSON, err := json.Marshal(resolutionRefs)
	if err != nil {
		return "", "", false, fmt.Errorf("marshal card resolution refs: %w", err)
	}
	return nextResolution, string(resolutionRefsJSON), true, nil
}

// ValidateBoardPlacementAnchors enforces placement anchor rules shared by HTTP handlers and the store.
func ValidateBoardPlacementAnchors(beforeCardID, afterCardID string) error {
	beforeCardID = strings.TrimSpace(beforeCardID)
	afterCardID = strings.TrimSpace(afterCardID)
	if beforeCardID != "" && afterCardID != "" {
		return fmt.Errorf("before and after anchors are mutually exclusive")
	}
	return nil
}
