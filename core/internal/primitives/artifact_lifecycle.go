// Artifact lifecycle mutations: trash, archive, unarchive, restore, and
// purge of trashed artifacts.

package primitives

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

func (s *Store) TrashArtifact(ctx context.Context, actorID string, artifactID string, reason string) (map[string]any, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("primitives store database is not initialized")
	}
	actorID = strings.TrimSpace(actorID)
	if actorID == "" {
		return nil, fmt.Errorf("actor_id is required")
	}
	artifactID = strings.TrimSpace(artifactID)
	if artifactID == "" {
		return nil, fmt.Errorf("artifact_id is required")
	}

	if owner, err := s.loadArtifactOwner(ctx, s.db, artifactID); err != nil {
		return nil, err
	} else if owner.Kind != "" {
		switch owner.Kind {
		case "document":
			if _, _, err := s.TrashDocument(ctx, actorID, owner.ResourceID, reason); err != nil {
				return nil, err
			}
			return s.GetArtifact(ctx, artifactID)
		case "card":
			if _, err := s.TrashBoardCard(ctx, actorID, "", owner.ResourceID, reason, RemoveBoardCardInput{}); err != nil {
				return nil, err
			}
			return s.GetArtifact(ctx, artifactID)
		default:
			return nil, ErrOwnedArtifactLifecycle
		}
	}

	var metadataJSON string
	var trashedAt sql.NullString
	var trashedBy sql.NullString
	var trashReason sql.NullString
	err := s.db.QueryRowContext(
		ctx,
		`SELECT metadata_json, trashed_at, trashed_by, trash_reason FROM artifacts WHERE id = ?`,
		artifactID,
	).Scan(&metadataJSON, &trashedAt, &trashedBy, &trashReason)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("query artifact for trash: %w", err)
	}

	metadata, err := decodeArtifactMetadataJSON(metadataJSON)
	if err != nil {
		return nil, err
	}
	if trashedAt.Valid && strings.TrimSpace(trashedAt.String) != "" {
		lifecycleFieldsFromSQLColumns(sql.NullString{}, sql.NullString{}, trashedAt, trashedBy, trashReason).apply(metadata)
		return metadata, nil
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	applyTrashedLifecycle(metadata, now, actorID, reason)

	updatedMetadataJSON, err := json.Marshal(metadata)
	if err != nil {
		return nil, fmt.Errorf("encode trashed artifact metadata: %w", err)
	}

	_, err = s.db.ExecContext(ctx,
		`UPDATE artifacts SET trashed_at = ?, trashed_by = ?, trash_reason = ?, archived_at = NULL, archived_by = NULL, metadata_json = ? WHERE id = ?`,
		now, actorID, strings.TrimSpace(reason), string(updatedMetadataJSON), artifactID,
	)
	if err != nil {
		return nil, fmt.Errorf("trash artifact: %w", err)
	}

	return metadata, nil
}

func (s *Store) ArchiveArtifact(ctx context.Context, actorID, artifactID string) (map[string]any, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("primitives store database is not initialized")
	}
	actorID = strings.TrimSpace(actorID)
	if actorID == "" {
		return nil, fmt.Errorf("actor_id is required")
	}
	artifactID = strings.TrimSpace(artifactID)
	if artifactID == "" {
		return nil, fmt.Errorf("artifact_id is required")
	}

	if owner, err := s.loadArtifactOwner(ctx, s.db, artifactID); err != nil {
		return nil, err
	} else if owner.Kind != "" {
		return nil, ErrOwnedArtifactLifecycle
	}

	var metadataJSON string
	var trashedAt sql.NullString
	var archivedAt sql.NullString
	var archivedBy sql.NullString
	err := s.db.QueryRowContext(
		ctx,
		`SELECT metadata_json, trashed_at, archived_at, archived_by FROM artifacts WHERE id = ?`,
		artifactID,
	).Scan(&metadataJSON, &trashedAt, &archivedAt, &archivedBy)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("query artifact for archive: %w", err)
	}

	if trashedAt.Valid && strings.TrimSpace(trashedAt.String) != "" {
		return nil, ErrAlreadyTrashed
	}

	metadata, err := decodeArtifactMetadataJSON(metadataJSON)
	if err != nil {
		return nil, err
	}

	if archivedAt.Valid && strings.TrimSpace(archivedAt.String) != "" {
		lifecycleFieldsFromSQLColumns(archivedAt, archivedBy, sql.NullString{}, sql.NullString{}, sql.NullString{}).apply(metadata)
		return metadata, nil
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	applyArchivedLifecycle(metadata, now, actorID)

	updatedMetadataJSON, err := json.Marshal(metadata)
	if err != nil {
		return nil, fmt.Errorf("encode archived artifact metadata: %w", err)
	}

	_, err = s.db.ExecContext(ctx,
		`UPDATE artifacts SET archived_at = ?, archived_by = ?, metadata_json = ? WHERE id = ?`,
		now, actorID, string(updatedMetadataJSON), artifactID,
	)
	if err != nil {
		return nil, fmt.Errorf("archive artifact: %w", err)
	}

	return metadata, nil
}

func (s *Store) UnarchiveArtifact(ctx context.Context, actorID, artifactID string) (map[string]any, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("primitives store database is not initialized")
	}
	actorID = strings.TrimSpace(actorID)
	if actorID == "" {
		return nil, fmt.Errorf("actor_id is required")
	}
	artifactID = strings.TrimSpace(artifactID)
	if artifactID == "" {
		return nil, fmt.Errorf("artifact_id is required")
	}

	if owner, err := s.loadArtifactOwner(ctx, s.db, artifactID); err != nil {
		return nil, err
	} else if owner.Kind != "" {
		return nil, ErrOwnedArtifactLifecycle
	}

	var metadataJSON string
	var trashedDiscard sql.NullString
	var archivedAt sql.NullString
	var archivedByDiscard sql.NullString
	err := s.db.QueryRowContext(
		ctx,
		`SELECT metadata_json, trashed_at, archived_at, archived_by FROM artifacts WHERE id = ?`,
		artifactID,
	).Scan(&metadataJSON, &trashedDiscard, &archivedAt, &archivedByDiscard)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("query artifact for unarchive: %w", err)
	}

	if !archivedAt.Valid || strings.TrimSpace(archivedAt.String) == "" {
		return nil, ErrNotArchived
	}

	metadata, err := decodeArtifactMetadataJSON(metadataJSON)
	if err != nil {
		return nil, err
	}
	clearArchivedLifecycle(metadata)

	updatedMetadataJSON, err := json.Marshal(metadata)
	if err != nil {
		return nil, fmt.Errorf("encode unarchived artifact metadata: %w", err)
	}

	_, err = s.db.ExecContext(ctx,
		`UPDATE artifacts SET archived_at = NULL, archived_by = NULL, metadata_json = ? WHERE id = ?`,
		string(updatedMetadataJSON), artifactID,
	)
	if err != nil {
		return nil, fmt.Errorf("unarchive artifact: %w", err)
	}

	return metadata, nil
}

func (s *Store) RestoreArtifact(ctx context.Context, actorID, artifactID string) (map[string]any, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("primitives store database is not initialized")
	}
	actorID = strings.TrimSpace(actorID)
	if actorID == "" {
		return nil, fmt.Errorf("actor_id is required")
	}
	artifactID = strings.TrimSpace(artifactID)
	if artifactID == "" {
		return nil, fmt.Errorf("artifact_id is required")
	}

	if owner, err := s.loadArtifactOwner(ctx, s.db, artifactID); err != nil {
		return nil, err
	} else if owner.Kind != "" {
		switch owner.Kind {
		case "document":
			if _, _, err := s.RestoreDocument(ctx, actorID, owner.ResourceID, ""); err != nil {
				return nil, err
			}
			return s.GetArtifact(ctx, artifactID)
		case "card":
			if _, err := s.RestoreArchivedBoardCard(ctx, actorID, "", owner.ResourceID, RemoveBoardCardInput{}); err != nil {
				return nil, err
			}
			return s.GetArtifact(ctx, artifactID)
		default:
			return nil, ErrOwnedArtifactLifecycle
		}
	}

	var metadataJSON string
	var trashedAt sql.NullString
	var trashedBy sql.NullString
	var trashReason sql.NullString
	err := s.db.QueryRowContext(
		ctx,
		`SELECT metadata_json, trashed_at, trashed_by, trash_reason FROM artifacts WHERE id = ?`,
		artifactID,
	).Scan(&metadataJSON, &trashedAt, &trashedBy, &trashReason)
	_ = trashedBy
	_ = trashReason
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("query artifact for restore: %w", err)
	}

	if !trashedAt.Valid || strings.TrimSpace(trashedAt.String) == "" {
		return nil, ErrNotTrashed
	}

	metadata, err := decodeArtifactMetadataJSON(metadataJSON)
	if err != nil {
		return nil, err
	}
	clearTrashedLifecycle(metadata, "", "")

	updatedMetadataJSON, err := json.Marshal(metadata)
	if err != nil {
		return nil, fmt.Errorf("encode restored artifact metadata: %w", err)
	}

	_, err = s.db.ExecContext(ctx,
		`UPDATE artifacts SET trashed_at = NULL, trashed_by = NULL, trash_reason = NULL, metadata_json = ? WHERE id = ?`,
		string(updatedMetadataJSON), artifactID,
	)
	if err != nil {
		return nil, fmt.Errorf("restore artifact: %w", err)
	}

	return metadata, nil
}

func (s *Store) PurgeTrashedArtifact(ctx context.Context, artifactID string) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("primitives store database is not initialized")
	}
	artifactID = strings.TrimSpace(artifactID)
	if artifactID == "" {
		return fmt.Errorf("artifact_id is required")
	}

	if owner, err := s.loadArtifactOwner(ctx, s.db, artifactID); err != nil {
		return err
	} else if owner.Kind != "" {
		switch owner.Kind {
		case "document":
			return s.PurgeDocument(ctx, owner.ResourceID)
		case "card":
			return s.PurgeArchivedBoardCard(ctx, "", owner.ResourceID)
		default:
			return ErrOwnedArtifactLifecycle
		}
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin purge transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var contentHash string
	err = tx.QueryRowContext(ctx,
		`SELECT content_hash FROM artifacts WHERE id = ? AND trashed_at IS NOT NULL`,
		artifactID,
	).Scan(&contentHash)
	if errors.Is(err, sql.ErrNoRows) {
		var one int
		err2 := tx.QueryRowContext(ctx, `SELECT 1 FROM artifacts WHERE id = ?`, artifactID).Scan(&one)
		if errors.Is(err2, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err2 != nil {
			return fmt.Errorf("check artifact existence: %w", err2)
		}
		return ErrNotTrashed
	}
	if err != nil {
		return fmt.Errorf("select trashed artifact: %w", err)
	}

	if err := preservePurgedAccess(ctx, tx, "artifact", artifactID); err != nil {
		return err
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM artifacts WHERE id = ?`, artifactID); err != nil {
		return fmt.Errorf("delete artifact: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit purge transaction: %w", err)
	}

	return s.collectUnreferencedBlobs(ctx, []string{contentHash})
}
