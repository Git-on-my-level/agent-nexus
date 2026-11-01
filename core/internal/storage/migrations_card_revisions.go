// Migration 19: creating card_revisions and backfilling them from cards and
// card_versions.

package storage

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

func applyMigration19CardRevisions(ctx context.Context, tx *sql.Tx) error {
	ok, err := sqliteTableExists(ctx, tx, "cards")
	if err != nil || !ok {
		return err
	}
	for _, col := range []struct {
		name string
		sql  string
	}{
		{"head_revision_id", `ALTER TABLE cards ADD COLUMN head_revision_id TEXT;`},
		{"head_revision_number", `ALTER TABLE cards ADD COLUMN head_revision_number INTEGER NOT NULL DEFAULT 1;`},
	} {
		has, err := sqliteTableHasColumn(ctx, tx, "cards", col.name)
		if err != nil {
			return fmt.Errorf("migration 19 pragma cards.%s: %w", col.name, err)
		}
		if !has {
			if _, err := tx.ExecContext(ctx, col.sql); err != nil {
				return fmt.Errorf("migration 19 add cards.%s: %w", col.name, err)
			}
		}
	}
	if _, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS card_revisions (
		revision_id TEXT PRIMARY KEY,
		card_id TEXT NOT NULL,
		revision_number INTEGER NOT NULL,
		prev_revision_id TEXT,
		artifact_id TEXT NOT NULL,
		thread_id TEXT,
		refs_json TEXT NOT NULL DEFAULT '[]',
		revision_hash TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL,
		created_by TEXT NOT NULL,
		UNIQUE(card_id, revision_number)
	);`); err != nil {
		return fmt.Errorf("migration 19 create card_revisions: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_card_revisions_card_id_revision_number ON card_revisions (card_id, revision_number);`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_card_revisions_card_id_revision_id ON card_revisions (card_id, revision_id);`); err != nil {
		return err
	}
	var existing int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM card_revisions`).Scan(&existing); err != nil {
		return fmt.Errorf("migration 19 count card_revisions: %w", err)
	}
	if existing > 0 {
		if _, err := tx.ExecContext(ctx, `DROP TABLE IF EXISTS card_versions;`); err != nil {
			return fmt.Errorf("migration 19 drop card_versions: %w", err)
		}
		return nil
	}
	hasCardVersions, err := sqliteTableExists(ctx, tx, "card_versions")
	if err != nil {
		return err
	}
	if hasCardVersions {
		if err := migration19BackfillFromCardVersions(ctx, tx); err != nil {
			return err
		}
	} else if err := migration19BackfillFromCards(ctx, tx); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DROP TABLE IF EXISTS card_versions;`); err != nil {
		return fmt.Errorf("migration 19 drop card_versions: %w", err)
	}
	return nil
}

func migration19BackfillFromCards(ctx context.Context, tx *sql.Tx) error {
	rows, err := tx.QueryContext(ctx, `SELECT id, thread_id, title, summary, definition_of_done_json, refs_json, created_at, created_by FROM cards`)
	if err != nil {
		return fmt.Errorf("migration 19 select cards: %w", err)
	}
	defer rows.Close()
	type cardRow struct {
		id, title, summary, dod, refs, createdAt, createdBy string
		thread                                              sql.NullString
	}
	var cards []cardRow
	for rows.Next() {
		var r cardRow
		if err := rows.Scan(&r.id, &r.thread, &r.title, &r.summary, &r.dod, &r.refs, &r.createdAt, &r.createdBy); err != nil {
			return fmt.Errorf("migration 19 scan card: %w", err)
		}
		cards = append(cards, r)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, r := range cards {
		if err := migration19InsertCardRevision(ctx, tx, r.id, 1, "", r.thread.String, r.title, r.summary, r.dod, r.refs, r.createdAt, r.createdBy); err != nil {
			return err
		}
	}
	return nil
}

func migration19BackfillFromCardVersions(ctx context.Context, tx *sql.Tx) error {
	rows, err := tx.QueryContext(ctx, `SELECT card_id, version, thread_id, title, summary, definition_of_done_json, refs_json, created_at, created_by FROM card_versions ORDER BY card_id, version`)
	if err != nil {
		return fmt.Errorf("migration 19 select card_versions: %w", err)
	}
	defer rows.Close()
	type versionRow struct {
		cardID, title, summary, dod, refs, createdAt, createdBy string
		version                                                 int
		thread                                                  sql.NullString
	}
	prev := map[string]string{}
	for rows.Next() {
		var r versionRow
		if err := rows.Scan(&r.cardID, &r.version, &r.thread, &r.title, &r.summary, &r.dod, &r.refs, &r.createdAt, &r.createdBy); err != nil {
			return fmt.Errorf("migration 19 scan card_version: %w", err)
		}
		if err := migration19InsertCardRevision(ctx, tx, r.cardID, r.version, prev[r.cardID], r.thread.String, r.title, r.summary, r.dod, r.refs, r.createdAt, r.createdBy); err != nil {
			return err
		}
		prev[r.cardID] = migration19RevisionID(r.cardID, r.version)
	}
	return rows.Err()
}

func migration19RevisionID(cardID string, revisionNumber int) string {
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte(fmt.Sprintf("card-revision:%s:%d", strings.TrimSpace(cardID), revisionNumber))).String()
}

func migration19InsertCardRevision(ctx context.Context, tx *sql.Tx, cardID string, revisionNumber int, prevRevisionID, threadID, title, summary, dodJSON, refsJSON, createdAt, createdBy string) error {
	revisionID := migration19RevisionID(cardID, revisionNumber)
	content := map[string]any{"title": strings.TrimSpace(title), "summary": strings.TrimSpace(summary), "definition_of_done": json.RawMessage(dodJSON)}
	contentBytes, _ := json.Marshal(content)
	sum := sha256.Sum256(contentBytes)
	contentHash := hex.EncodeToString(sum[:])
	metadata := map[string]any{
		"id":              revisionID,
		"kind":            "card",
		"created_at":      createdAt,
		"created_by":      createdBy,
		"content_type":    "structured",
		"content_hash":    contentHash,
		"refs":            json.RawMessage(refsJSON),
		"card_id":         cardID,
		"revision_id":     revisionID,
		"revision_number": revisionNumber,
		"title":           strings.TrimSpace(title),
		"summary":         strings.TrimSpace(summary),
	}
	if strings.TrimSpace(prevRevisionID) != "" {
		metadata["prev_revision_id"] = strings.TrimSpace(prevRevisionID)
	}
	metadataJSON, err := json.Marshal(metadata)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO artifacts(id, kind, thread_id, created_at, created_by, content_type, content_hash, refs_json, metadata_json)
		VALUES (?, 'card', ?, ?, ?, 'structured', ?, ?, ?)`, revisionID, nullableMigrationString(threadID), createdAt, createdBy, contentHash, refsJSON, string(metadataJSON)); err != nil {
		return fmt.Errorf("migration 19 insert artifact: %w", err)
	}
	revisionHash := hex.EncodeToString(sha256.New().Sum([]byte(contentHash + cardID + fmt.Sprint(revisionNumber))))
	if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO card_revisions(revision_id, card_id, revision_number, prev_revision_id, artifact_id, thread_id, refs_json, revision_hash, created_at, created_by)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, revisionID, cardID, revisionNumber, nullableMigrationString(prevRevisionID), revisionID, nullableMigrationString(threadID), refsJSON, revisionHash, createdAt, createdBy); err != nil {
		return fmt.Errorf("migration 19 insert card_revision: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE cards SET head_revision_id = ?, head_revision_number = ? WHERE id = ? AND COALESCE(head_revision_number, 0) <= ?`, revisionID, revisionNumber, cardID, revisionNumber); err != nil {
		return fmt.Errorf("migration 19 update card head: %w", err)
	}
	return nil
}
