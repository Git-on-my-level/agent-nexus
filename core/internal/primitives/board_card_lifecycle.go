// Card lifecycle mutations on a board: remove, archive, restore, purge, and
// trash, with the shared lifecycle version advance.

package primitives

import (
	"agent-nexus-core/internal/resourceaccess"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"agent-nexus-core/internal/actors"
)

type RemoveBoardCardInput struct {
	Reason                string
	IfLifecycle           *EventLifecycleBase
	IfBoardUpdatedAt      *string
	IfWorkVersion         *int64
	IfLatestObservationID *string
}

type BoardCardRemovalResult struct {
	Board           map[string]any
	Card            map[string]any
	RemovedCardID   string
	RemovedThreadID string
}

func (s *Store) RemoveBoardCard(ctx context.Context, actorID, boardID, identifier string, input RemoveBoardCardInput) (BoardCardRemovalResult, error) {
	card, err := s.ArchiveBoardCard(ctx, actorID, boardID, identifier, input)
	if err != nil {
		return BoardCardRemovalResult{}, err
	}
	return BoardCardRemovalResult{
		Board:           card.Board,
		Card:            card.Card,
		RemovedCardID:   strings.TrimSpace(anyStringValue(card.Card["id"])),
		RemovedThreadID: parentThreadIDFromCardRefs(card.Card),
	}, nil
}

// Lifecycle transitions advance the existing work fence even when the visibility
// tuple later returns to its original value. Preserve source-owned annotations.
func advanceCardLifecycleVersion(ctx context.Context, tx *resourceaccess.Tx, cardID, actorID, now string) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO work_metadata(card_id,authority,metadata_json,version,updated_at,updated_by) VALUES(?,'nexus','{"source":{"authority":"nexus"}}',1,?,?) ON CONFLICT(card_id) DO UPDATE SET version=work_metadata.version+1,updated_at=excluded.updated_at,updated_by=excluded.updated_by`, cardID, now, actorID)
	return err
}

func (s *Store) ArchiveBoardCard(ctx context.Context, actorID, boardID, identifier string, input RemoveBoardCardInput) (mutationResult BoardCardMutationResult, mutationErr error) {
	if s == nil || s.db == nil {
		return BoardCardMutationResult{}, fmt.Errorf("primitives store database is not initialized")
	}
	if strings.TrimSpace(actorID) == "" {
		return BoardCardMutationResult{}, invalidBoardRequest("actorID is required")
	}

	tx, err := mutationTxBegin(ctx, s.db)
	if err != nil {
		return BoardCardMutationResult{}, fmt.Errorf("begin board card archive transaction: %w", err)
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
	boardID = cardRow.BoardID
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
	if input.IfWorkVersion != nil {
		var currentVersion int64
		versionErr := tx.QueryRowContext(ctx, `SELECT version FROM work_metadata WHERE card_id = ?`, cardRow.CardID).Scan(&currentVersion)
		if versionErr != nil || currentVersion != *input.IfWorkVersion {
			if rbErr := tx.Rollback(); rbErr != nil {
				log.Printf("tx rollback failed: %v", rbErr)
			}
			return BoardCardMutationResult{}, ErrConflict
		}
	}
	if input.IfLatestObservationID != nil {
		if strings.TrimSpace(*input.IfLatestObservationID) == "" && input.IfLifecycle == nil {
			_ = tx.Rollback()
			return BoardCardMutationResult{}, invalidBoardRequest("if_latest_observation_id must be nonempty")
		}
		var latest string
		err := tx.QueryRowContext(ctx, `SELECT COALESCE(latest_observation_id,'') FROM work_metadata WHERE card_id = ?`, cardRow.CardID).Scan(&latest)
		if err != nil || latest != *input.IfLatestObservationID {
			_ = tx.Rollback()
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return BoardCardMutationResult{}, fmt.Errorf("read archive observation fence: %w", err)
			}
			return BoardCardMutationResult{}, ErrConflict
		}
	}
	if input.IfLifecycle != nil {
		archived, trashed := nullableStringPtr(cardRow.ArchivedAt), nullableStringPtr(cardRow.TrashedAt)
		if !equalLifecycleString(archived, input.IfLifecycle.ArchivedAt) || !equalLifecycleString(trashed, input.IfLifecycle.TrashedAt) {
			_ = tx.Rollback()
			return BoardCardMutationResult{}, ErrConflict
		}
	}
	if cardRow.TrashedAt.Valid && strings.TrimSpace(cardRow.TrashedAt.String) != "" {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return BoardCardMutationResult{}, ErrAlreadyTrashed
	}
	if cardRow.ArchivedAt.Valid && strings.TrimSpace(cardRow.ArchivedAt.String) != "" {
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
	archiveQuery := `UPDATE cards SET archived_at = ?, archived_by = ? WHERE id = ?`
	archiveArgs := []any{now, actorID, cardRow.CardID}
	if input.IfWorkVersion != nil {
		archiveQuery += ` AND EXISTS (SELECT 1 FROM work_metadata WHERE card_id = ? AND version = ?)`
		archiveArgs = append(archiveArgs, cardRow.CardID, *input.IfWorkVersion)
	}
	if input.IfLatestObservationID != nil {
		// Keep the observation fence on the mutation itself. Successful polls
		// can replace this ID without changing either the board or work version.
		archiveQuery += ` AND EXISTS (SELECT 1 FROM work_metadata WHERE card_id = ? AND COALESCE(latest_observation_id,'') = ?)`
		archiveArgs = append(archiveArgs, cardRow.CardID, *input.IfLatestObservationID)
	}
	archiveResult, err := tx.ExecContext(ctx, archiveQuery, archiveArgs...)
	if err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return BoardCardMutationResult{}, fmt.Errorf("archive board card: %w", err)
	}
	if input.IfWorkVersion != nil || input.IfLatestObservationID != nil {
		rows, rowsErr := archiveResult.RowsAffected()
		if rowsErr != nil || rows != 1 {
			if rbErr := tx.Rollback(); rbErr != nil {
				log.Printf("tx rollback failed: %v", rbErr)
			}
			if rowsErr != nil {
				return BoardCardMutationResult{}, rowsErr
			}
			return BoardCardMutationResult{}, ErrConflict
		}
	}
	// Children of archived work wait in the Unsorted tray for a new home.
	if err := releaseWorkTreeChildrenTx(ctx, tx, cardRow.CardID, actorID, now); err != nil {
		_ = tx.Rollback()
		return BoardCardMutationResult{}, err
	}
	if err := advanceCardLifecycleVersion(ctx, tx, cardRow.CardID, actorID, now); err != nil {
		_ = tx.Rollback()
		return BoardCardMutationResult{}, err
	}
	boardRow, err = touchBoardRow(ctx, tx, boardRow, actorID)
	if err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return BoardCardMutationResult{}, err
	}
	cardRow.ArchivedAt = sql.NullString{String: now, Valid: true}
	cardRow.ArchivedBy = sql.NullString{String: actorID, Valid: true}

	if err := markBoardCardProjectionDirtyTx(ctx, tx, cardRow); err != nil {
		_ = tx.Rollback()
		return BoardCardMutationResult{}, err
	}
	if reason := strings.TrimSpace(input.Reason); reason != "" {
		card, err := cardRow.toMap()
		if err != nil {
			return BoardCardMutationResult{}, err
		}
		if err = insertWorkEvent(ctx, tx, actorID, card, "card_archived", "Card archived", map[string]any{"reason": reason}); err != nil {
			return BoardCardMutationResult{}, err
		}
	}

	if err := tx.Commit(); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return BoardCardMutationResult{}, fmt.Errorf("commit board card archive transaction: %w", err)
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

func (s *Store) RestoreArchivedBoardCard(ctx context.Context, actorID, boardID, identifier string, input RemoveBoardCardInput) (BoardCardMutationResult, error) {
	if s == nil || s.db == nil {
		return BoardCardMutationResult{}, fmt.Errorf("primitives store database is not initialized")
	}
	if strings.TrimSpace(actorID) == "" {
		return BoardCardMutationResult{}, invalidBoardRequest("actorID is required")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return BoardCardMutationResult{}, fmt.Errorf("begin board card restore transaction: %w", err)
	}

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
	boardID = cardRow.BoardID
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
	trashed := cardRow.TrashedAt.Valid && strings.TrimSpace(cardRow.TrashedAt.String) != ""
	archived := cardRow.ArchivedAt.Valid && strings.TrimSpace(cardRow.ArchivedAt.String) != ""
	if !trashed && !archived {
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

	if trashed {
		if _, err := tx.ExecContext(ctx, `UPDATE cards SET trashed_at = NULL, trashed_by = NULL, trash_reason = NULL WHERE id = ?`, cardRow.CardID); err != nil {
			if rbErr := tx.Rollback(); rbErr != nil {
				log.Printf("tx rollback failed: %v", rbErr)
			}
			return BoardCardMutationResult{}, fmt.Errorf("restore trashed board card: %w", err)
		}
		cardRow.TrashedAt = sql.NullString{}
		cardRow.TrashedBy = sql.NullString{}
		cardRow.TrashReason = sql.NullString{}
	} else {
		if _, err := tx.ExecContext(ctx, `UPDATE cards SET archived_at = NULL, archived_by = NULL WHERE id = ?`, cardRow.CardID); err != nil {
			if rbErr := tx.Rollback(); rbErr != nil {
				log.Printf("tx rollback failed: %v", rbErr)
			}
			return BoardCardMutationResult{}, fmt.Errorf("restore board card: %w", err)
		}
		cardRow.ArchivedAt = sql.NullString{}
		cardRow.ArchivedBy = sql.NullString{}
	}

	if err := advanceCardLifecycleVersion(ctx, tx, cardRow.CardID, actorID, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		_ = tx.Rollback()
		return BoardCardMutationResult{}, err
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
		return BoardCardMutationResult{}, fmt.Errorf("commit board card restore transaction: %w", err)
	}

	boardMap, err := boardRowToAPI(ctx, s.db, boardRow)
	if err != nil {
		return BoardCardMutationResult{}, err
	}
	cardRow, err = s.loadBoardCardByIdentifier(ctx, s.db, boardID, cardRow.CardID, false)
	if err != nil {
		return BoardCardMutationResult{}, err
	}
	cardMap, err := cardRow.toMap()
	if err != nil {
		return BoardCardMutationResult{}, err
	}
	return BoardCardMutationResult{Board: boardMap, Card: cardMap}, nil
}

// PurgeArchivedBoardCard permanently removes a card that was soft-deleted (archived).
func (s *Store) PurgeArchivedBoardCard(ctx context.Context, boardID, identifier string) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("primitives store database is not initialized")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin purge card transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var cardRow boardCardRow
	if strings.TrimSpace(boardID) != "" {
		cardRow, err = s.loadBoardCardByIdentifier(ctx, tx, boardID, identifier, true)
	} else {
		cardRow, err = s.loadBoardCardByGlobalID(ctx, tx, identifier, true)
	}
	if err != nil {
		return err
	}
	archived := cardRow.ArchivedAt.Valid && strings.TrimSpace(cardRow.ArchivedAt.String) != ""
	trashed := cardRow.TrashedAt.Valid && strings.TrimSpace(cardRow.TrashedAt.String) != ""
	if !archived && !trashed {
		return ErrNotArchived
	}
	cardID := strings.TrimSpace(cardRow.CardID)

	boardRow, err := loadBoardRow(ctx, tx, cardRow.BoardID)
	if err != nil {
		return err
	}

	if err := preservePurgedAccess(ctx, tx, "card", cardID); err != nil {
		return err
	}

	revisionRows, err := tx.QueryContext(ctx,
		`SELECT cr.revision_id, cr.artifact_id, a.content_hash
		   FROM card_revisions cr
		   JOIN artifacts a ON a.id = cr.artifact_id
		  WHERE cr.card_id = ?`,
		cardID,
	)
	if err != nil {
		return fmt.Errorf("query card revision artifacts for purge: %w", err)
	}
	var revisionIDs, artifactIDs, contentHashes []string
	for revisionRows.Next() {
		var revisionID, artifactID, contentHash string
		if err := revisionRows.Scan(&revisionID, &artifactID, &contentHash); err != nil {
			_ = revisionRows.Close()
			return fmt.Errorf("scan card revision artifact for purge: %w", err)
		}
		revisionIDs = append(revisionIDs, strings.TrimSpace(revisionID))
		artifactIDs = append(artifactIDs, strings.TrimSpace(artifactID))
		contentHashes = append(contentHashes, strings.TrimSpace(contentHash))
	}
	if err := revisionRows.Close(); err != nil {
		return fmt.Errorf("close card revision artifact rows: %w", err)
	}
	if err := revisionRows.Err(); err != nil {
		return fmt.Errorf("iterate card revision artifacts for purge: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM ref_edges WHERE source_type = ? AND source_id = ?`, "card", cardID); err != nil {
		return fmt.Errorf("delete card source ref edges: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM ref_edges WHERE target_type = ? AND target_id = ?`, "card", cardID); err != nil {
		return fmt.Errorf("delete card target ref edges: %w", err)
	}
	for _, revisionID := range revisionIDs {
		if revisionID == "" {
			continue
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM ref_edges WHERE (source_type = ? AND source_id = ?) OR (target_type = ? AND target_id = ?)`, "card_revision", revisionID, "card_revision", revisionID); err != nil {
			return fmt.Errorf("delete card revision ref edges: %w", err)
		}
	}
	for _, artifactID := range artifactIDs {
		if artifactID == "" {
			continue
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM ref_edges WHERE (source_type = ? AND source_id = ?) OR (target_type = ? AND target_id = ?)`, "artifact", artifactID, "artifact", artifactID); err != nil {
			return fmt.Errorf("delete card artifact ref edges: %w", err)
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM card_revisions WHERE card_id = ?`, cardID); err != nil {
		return fmt.Errorf("delete card revisions: %w", err)
	}
	for _, artifactID := range artifactIDs {
		if artifactID == "" {
			continue
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM artifacts WHERE id = ?`, artifactID); err != nil {
			return fmt.Errorf("delete card revision artifact: %w", err)
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM work_participants WHERE card_id = ?`, cardID); err != nil {
		return fmt.Errorf("delete card participation: %w", err)
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM cards WHERE id = ? AND (archived_at IS NOT NULL OR trashed_at IS NOT NULL)`, cardID)
	if err != nil {
		return fmt.Errorf("delete card: %w", err)
	}
	// Never leave a dangling parent edge; any remaining children (including
	// trashed or archived ones) wait in the Unsorted tray.
	if _, err = tx.ExecContext(ctx, `UPDATE cards SET parent_card_id=NULL,needs_home=1 WHERE parent_card_id=?`, cardID); err != nil {
		return fmt.Errorf("release purged card children: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected card purge: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}

	boardRow, err = touchBoardRow(ctx, tx, boardRow, actors.SystemActorID)
	if err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit purge card transaction: %w", err)
	}
	return s.collectUnreferencedBlobs(ctx, contentHashes)
}

// TrashBoardCard records an operational soft-delete and clears archive columns (distinct soft-delete lanes).
func (s *Store) TrashBoardCard(ctx context.Context, actorID, boardID, identifier, reason string, input RemoveBoardCardInput) (mutationResult BoardCardMutationResult, mutationErr error) {
	if s == nil || s.db == nil {
		return BoardCardMutationResult{}, fmt.Errorf("primitives store database is not initialized")
	}
	if strings.TrimSpace(actorID) == "" {
		return BoardCardMutationResult{}, invalidBoardRequest("actorID is required")
	}

	tx, err := mutationTxBegin(ctx, s.db)
	if err != nil {
		return BoardCardMutationResult{}, fmt.Errorf("begin board card trash transaction: %w", err)
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
	boardID = cardRow.BoardID
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
	if cardRow.TrashedAt.Valid && strings.TrimSpace(cardRow.TrashedAt.String) != "" {
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
		cardMap["_mutation_applied"] = false
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return BoardCardMutationResult{Board: boardMap, Card: cardMap}, nil
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	reason = strings.TrimSpace(reason)
	if _, err := tx.ExecContext(ctx,
		`UPDATE cards SET trashed_at = ?, trashed_by = ?, trash_reason = ?, archived_at = NULL, archived_by = NULL WHERE id = ?`,
		now, actorID, reason, cardRow.CardID,
	); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return BoardCardMutationResult{}, fmt.Errorf("trash board card: %w", err)
	}
	// Children of trashed work wait in the Unsorted tray for a new home.
	if err := releaseWorkTreeChildrenTx(ctx, tx, cardRow.CardID, actorID, now); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return BoardCardMutationResult{}, fmt.Errorf("unparent trashed card children: %w", err)
	}
	cardRow.TrashedAt = sql.NullString{String: now, Valid: true}
	cardRow.TrashedBy = sql.NullString{String: actorID, Valid: true}
	if reason != "" {
		cardRow.TrashReason = sql.NullString{String: reason, Valid: true}
	} else {
		cardRow.TrashReason = sql.NullString{}
	}
	cardRow.ArchivedAt = sql.NullString{}
	cardRow.ArchivedBy = sql.NullString{}

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
		return BoardCardMutationResult{}, fmt.Errorf("commit board card trash transaction: %w", err)
	}

	boardMap, err := boardRowToAPI(ctx, s.db, boardRow)
	if err != nil {
		return BoardCardMutationResult{}, err
	}
	cardMap, err := cardRow.toMap()
	if err != nil {
		return BoardCardMutationResult{}, err
	}
	cardMap["_mutation_applied"] = true
	return BoardCardMutationResult{Board: boardMap, Card: cardMap}, nil
}
