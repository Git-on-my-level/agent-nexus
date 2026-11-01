// Store methods that create cards on a board: single create, add, and batch,
// plus the parent-thread availability check they share.

package primitives

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"

	"agent-nexus-core/internal/blob"
)

func (s *Store) CreateBoardCard(ctx context.Context, actorID, boardID string, input AddBoardCardInput) (mutationResult BoardCardMutationResult, mutationErr error) {
	if s == nil || s.db == nil {
		return BoardCardMutationResult{}, fmt.Errorf("primitives store database is not initialized")
	}
	if s.quota.enabled() && !replayOwnsQuota(ctx, s) {
		s.quotaMu.Lock()
		defer s.quotaMu.Unlock()
		if s.quota.MaxBlobBytes > 0 {
			if err := s.ensureBlobUsageLedgerInitialized(ctx); err != nil {
				return BoardCardMutationResult{}, err
			}
		}
	}
	if strings.TrimSpace(actorID) == "" {
		return BoardCardMutationResult{}, invalidBoardRequest("actorID is required")
	}
	prep, err := prepareBoardCardInsert(input)
	if err != nil {
		return BoardCardMutationResult{}, err
	}
	preload, err := s.preloadCardContent(ctx, prep)
	if err != nil {
		return BoardCardMutationResult{}, fmt.Errorf("preload card content: %w", err)
	}
	defer preload.discardUnlessCommitted()
	tx, err := mutationTxBegin(ctx, s.db)
	if err != nil {
		return BoardCardMutationResult{}, fmt.Errorf("begin board card create transaction: %w", err)
	}
	// A statement that fails before Commit, once Rollback succeeds, changed
	// nothing. Leave that error intact so the handler can log it as a 500
	// (or 503 when SQLite is busy or locked). Only Commit is an
	// unknown outcome; content is promoted before it.
	defer func() {
		if mutationErr != nil {
			mutationErr = classifyBoardCardRequestError(mutationErr)
		}
	}()
	defer tx.Rollback()
	if replay := input.CreateReplay; replay != nil {
		var hash string
		err := tx.QueryRowContext(ctx, `SELECT request_hash FROM idempotency_replays WHERE scope=? AND actor_id=? AND request_key=?`, replay.Scope, actorID, replay.Key).Scan(&hash)
		if err == nil {
			return BoardCardMutationResult{}, ErrConflict
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return BoardCardMutationResult{}, err
		}
	}
	boardRow, err := loadBoardRow(ctx, tx, boardID)
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
	boardRow, cardRow, stagedContent, err := s.execBoardCardInsert(ctx, tx, boardRow, actorID, boardID, prep)
	if err != nil {
		if stagedContent != nil {
			_ = stagedContent.Cleanup()
		}
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return BoardCardMutationResult{}, err
	}
	if input.Under != "" {
		parentRef, err := s.groupWorkTx(ctx, tx, actorID, cardRow, input.Under, input.After, "")
		if err != nil {
			return BoardCardMutationResult{}, err
		}
		input.Under = parentRef
	} else if input.Initiative {
		if _, err := tx.ExecContext(ctx, `UPDATE cards SET needs_home=0 WHERE id=?`, cardRow.CardID); err != nil {
			return BoardCardMutationResult{}, err
		}
	} else if input.After != "" {
		return BoardCardMutationResult{}, workInvalid("after requires under")
	}
	if input.WorkMetadata != nil {
		if err := insertWorkMetadata(ctx, tx, cardRow.CardID, actorID, input.WorkMetadata); err != nil {
			_ = tx.Rollback()
			if stagedContent != nil {
				_ = stagedContent.Cleanup()
			}
			return BoardCardMutationResult{}, err
		}
	}
	// A mutation echo must not load every membership while holding the writer.
	// The full board collection is available through its paged read endpoints.
	typedRefs, err := decodeStoredJSONList(boardRow.RefsJSON, "board.refs")
	if err != nil {
		if stagedContent != nil {
			_ = stagedContent.Cleanup()
		}
		return BoardCardMutationResult{}, err
	}
	boardMap, err := boardRow.boardToMapWithRefData(typedRefs, []string{"card:" + cardRow.CardID})
	if err != nil {
		if stagedContent != nil {
			_ = stagedContent.Cleanup()
		}
		return BoardCardMutationResult{}, err
	}
	boardMap["card_refs_truncated"] = true
	cardMap, err := cardRow.toMap()
	if err != nil {
		if stagedContent != nil {
			_ = stagedContent.Cleanup()
		}
		return BoardCardMutationResult{}, err
	}
	creation := workClone(input.WorkMetadata)
	creation["under"] = input.Under
	if err := decorateCreatedCard(ctx, tx, cardMap, creation); err != nil {
		return BoardCardMutationResult{}, err
	}
	cardMap["create_surface"] = input.CreateSurface

	if replay := input.CreateReplay; replay != nil {
		response := map[string]any{"board": boardMap, "card": cardMap}
		if homes, ok := cardMap["likely_home"].([]LikelyHome); ok {
			response["likely_home"] = homes
			response["next_actions"] = GroupingNextActions(workString(cardMap["ref"]), homes, "group")
		}

		for key, value := range replay.Extras {
			if key != "board" && key != "card" {
				response[key] = value
			}
		}
		raw, err := json.Marshal(response)
		if err == nil {
			_, err = tx.ExecContext(ctx, `INSERT INTO idempotency_replays(scope,actor_id,request_key,request_hash,response_status,response_json) VALUES(?,?,?,?,201,?)`, replay.Scope, actorID, replay.Key, replay.Hash, string(raw))
		}
		if err != nil {
			if stagedContent != nil {
				_ = stagedContent.Cleanup()
			}
			return BoardCardMutationResult{}, err
		}
	}
	// Promote before committing so a committed card always has its content; a
	// failed promote rolls the card back.
	if stagedContent != nil {
		if err := stagedContent.Promote(); err != nil {
			_ = stagedContent.Cleanup()
			return BoardCardMutationResult{}, fmt.Errorf("finalize card content: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return BoardCardMutationResult{}, &MutationOutcomeUnknown{Cause: fmt.Errorf("commit board card create transaction: %w", err)}
	}
	preload.markCommitted()
	return BoardCardMutationResult{Board: boardMap, Card: cardMap, ReplayCommitted: input.CreateReplay != nil}, nil
}

func (s *Store) CreateBoardCardsBatch(ctx context.Context, actorID, boardID string, ifBoard *string, inputs []AddBoardCardInput) (mutationResults []BoardCardMutationResult, mutationErr error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("primitives store database is not initialized")
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
	if strings.TrimSpace(actorID) == "" {
		return nil, invalidBoardRequest("actorID is required")
	}
	if len(inputs) == 0 {
		return nil, invalidBoardRequest("items must include at least one card")
	}
	if len(inputs) > maxBoardCardsBatchSize {
		return nil, invalidBoardRequest(fmt.Sprintf("items exceeds maximum batch size of %d", maxBoardCardsBatchSize))
	}
	preps := make([]boardCardInsertPrep, len(inputs))
	for i := range inputs {
		in := inputs[i]
		in.IfBoardUpdatedAt = nil
		prep, err := prepareBoardCardInsert(in)
		if err != nil {
			return nil, fmt.Errorf("items[%d]: %w", i, err)
		}
		preps[i] = prep
	}
	preload, err := s.preloadCardContent(ctx, preps...)
	if err != nil {
		return nil, fmt.Errorf("preload card content: %w", err)
	}
	defer preload.discardUnlessCommitted()
	tx, err := mutationTxBegin(ctx, s.db)
	if err != nil {
		return nil, fmt.Errorf("begin board cards batch transaction: %w", err)
	}
	// Same rule as CreateBoardCard: a rolled-back statement is not an
	// unknown outcome. Only Commit is; content is promoted before it.
	defer func() {
		if mutationErr != nil {
			mutationErr = classifyBoardCardRequestError(mutationErr)
		}
	}()
	boardRow, err := loadBoardRow(ctx, tx, boardID)
	if err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return nil, err
	}
	if err := ensureBoardUpdatedAtMatches(boardRow, ifBoard); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return nil, err
	}
	results := make([]BoardCardMutationResult, 0, len(preps))
	stagedWrites := make([]blob.StagedWrite, 0, len(preps))
	for i, prep := range preps {
		var cardRow boardCardRow
		var stagedContent blob.StagedWrite
		boardRow, cardRow, stagedContent, err = s.execBoardCardInsert(ctx, tx, boardRow, actorID, boardID, prep)
		if err != nil {
			if stagedContent != nil {
				_ = stagedContent.Cleanup()
			}
			for _, staged := range stagedWrites {
				_ = staged.Cleanup()
			}
			if rbErr := tx.Rollback(); rbErr != nil {
				log.Printf("tx rollback failed: %v", rbErr)
			}
			return nil, fmt.Errorf("items[%d]: %w", i, err)
		}
		if stagedContent != nil {
			stagedWrites = append(stagedWrites, stagedContent)
		}
		if inputs[i].Under != "" {
			parentRef, groupErr := s.groupWorkTx(ctx, tx, actorID, cardRow, inputs[i].Under, inputs[i].After, "")
			if groupErr != nil {
				for _, staged := range stagedWrites {
					_ = staged.Cleanup()
				}
				_ = tx.Rollback()
				return nil, fmt.Errorf("items[%d]: %w", i, groupErr)
			}
			inputs[i].Under = parentRef
		} else if inputs[i].Initiative {
			if _, err := tx.ExecContext(ctx, `UPDATE cards SET needs_home=0 WHERE id=?`, cardRow.CardID); err != nil {
				for _, staged := range stagedWrites {
					_ = staged.Cleanup()
				}
				_ = tx.Rollback()
				return nil, fmt.Errorf("items[%d]: %w", i, err)
			}
		} else if inputs[i].After != "" {
			for _, staged := range stagedWrites {
				_ = staged.Cleanup()
			}
			_ = tx.Rollback()
			return nil, workInvalid("after requires under")
		}

		boardMap, terr := boardRowToAPI(ctx, tx, boardRow)
		if terr != nil {
			if rbErr := tx.Rollback(); rbErr != nil {
				log.Printf("tx rollback failed: %v", rbErr)
			}
			for _, staged := range stagedWrites {
				_ = staged.Cleanup()
			}
			return nil, terr
		}
		cardMap, terr := cardRow.toMap()
		if terr != nil {
			if rbErr := tx.Rollback(); rbErr != nil {
				log.Printf("tx rollback failed: %v", rbErr)
			}
			for _, staged := range stagedWrites {
				_ = staged.Cleanup()
			}
			return nil, terr
		}
		if inputs[i].Under != "" {
			cardMap["parent_ref"] = inputs[i].Under
		}

		results = append(results, BoardCardMutationResult{Board: boardMap, Card: cardMap})
	}
	// Promote before committing so committed cards always have their content;
	// a failed promote rolls the whole batch back.
	if err := promoteStagedBlobs(stagedWrites); err != nil {
		for _, pending := range stagedWrites {
			_ = pending.Cleanup()
		}
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return nil, fmt.Errorf("finalize card content: %w", err)
	}
	if err := tx.Commit(); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return nil, &MutationOutcomeUnknown{Cause: fmt.Errorf("commit board cards batch transaction: %w", err)}
	}
	preload.markCommitted()
	return results, nil
}

func (s *Store) AddBoardCard(ctx context.Context, actorID, boardID string, input AddBoardCardInput) (BoardCardMutationResult, error) {
	return s.CreateBoardCard(ctx, actorID, boardID, input)
}

func ensureBoardCardParentThreadAvailable(ctx context.Context, rower queryRower, boardID, parentThreadID, excludeCardID string) error {
	if strings.TrimSpace(parentThreadID) == "" {
		return nil
	}
	var existingCardID string
	err := rower.QueryRowContext(
		ctx,
		`SELECT c.id
		   FROM ref_edges re
		   JOIN cards c ON c.id = re.target_id
		  WHERE re.source_type = 'board'
		    AND re.edge_type = ?
		    AND re.source_id = ?
		    AND c.parent_thread_id = ?
		    AND c.archived_at IS NULL AND c.trashed_at IS NULL
		    AND c.id != ?
		  LIMIT 1`,
		refEdgeTypeBoardCard,
		boardID,
		parentThreadID,
		strings.TrimSpace(excludeCardID),
	).Scan(&existingCardID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("query board card parent thread membership: %w", err)
	}
	return ErrConflict
}
