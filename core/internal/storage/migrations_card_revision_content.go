// Migration 130: card revision content moves into card_revisions as
// keyframe/delta records (see internal/cardcontent) and leaves the revision
// artifact's metadata.
//
// Before this migration each revision kept its title, summary and definition
// of done twice: in artifacts.metadata_json, which the history read used, and
// in a content-addressed blob. History reads materialized every summary.
//
// The migration is resumable and idempotent. Each chunk encodes at most
// cardContentMigrationBatch pending revisions in revision order, verifies that
// every record decodes to the exact bytes it was built from, and commits. A
// crash between chunks resumes at the first revision still pending.
//
// Content is rebuilt with the history read's own rules: metadata fields when
// present, and the card's columns for the legacy rows (migration 19) whose
// metadata predates a field. When the rebuilt bytes hash to the artifact's
// content_hash (every revision written by the normal write path), they are the
// blob's exact bytes; the artifact then takes its content reference atoms from
// those bytes and its metadata drops the duplicated summary and definition of
// done. A legacy row whose bytes cannot be verified keeps its metadata and its
// fail-closed access manifest unchanged, so no access decision can loosen.

package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"agent-nexus-core/internal/cardcontent"
	"agent-nexus-core/internal/resourceaccess"
)

const cardContentMigrationBatch = 64

var cardRevisionContentColumns = []struct{ name, ddl string }{
	{"content_encoding", `ALTER TABLE card_revisions ADD COLUMN content_encoding TEXT`},
	{"content_codec", `ALTER TABLE card_revisions ADD COLUMN content_codec TEXT`},
	{"content_base_revision_id", `ALTER TABLE card_revisions ADD COLUMN content_base_revision_id TEXT`},
	{"content_hash", `ALTER TABLE card_revisions ADD COLUMN content_hash TEXT`},
	{"content_bytes", `ALTER TABLE card_revisions ADD COLUMN content_bytes INTEGER`},
	{"summary_bytes", `ALTER TABLE card_revisions ADD COLUMN summary_bytes INTEGER`},
	// Kept last so metadata-only history pages read only the row's leading
	// columns and never the payload's overflow pages.
	{"content_payload", `ALTER TABLE card_revisions ADD COLUMN content_payload BLOB`},
}

func cardRevisionContentSchemaInstalled(ctx context.Context, tx *sql.Tx) (bool, error) {
	for _, col := range cardRevisionContentColumns {
		has, err := sqliteTableHasColumn(ctx, tx, "card_revisions", col.name)
		if err != nil || !has {
			return false, err
		}
	}
	return true, nil
}

func cardRevisionContentMigrationChunk(ctx context.Context, tx *sql.Tx) (bool, error) {
	installed, err := cardRevisionContentSchemaInstalled(ctx, tx)
	if err != nil {
		return false, err
	}
	if !installed {
		for _, col := range cardRevisionContentColumns {
			has, err := sqliteTableHasColumn(ctx, tx, "card_revisions", col.name)
			if err != nil {
				return false, err
			}
			if !has {
				if _, err := tx.ExecContext(ctx, col.ddl); err != nil {
					return false, fmt.Errorf("add card_revisions.%s: %w", col.name, err)
				}
			}
		}
		for _, stmt := range []string{
			// Content-addressed dedupe within one card.
			`CREATE INDEX IF NOT EXISTS idx_card_revisions_card_content_hash ON card_revisions(card_id, content_hash)`,
			// The latest snapshot of a card, read before each write.
			`CREATE INDEX IF NOT EXISTS idx_card_revisions_card_snapshots ON card_revisions(card_id, revision_number) WHERE content_encoding = 'snapshot'`,
			// Migration work queue. It stays (empty) afterwards: writers always
			// store content, and it keeps the startup verification O(1).
			`CREATE INDEX IF NOT EXISTS idx_card_revisions_content_pending ON card_revisions(card_id, revision_number) WHERE content_encoding IS NULL`,
		} {
			if _, err := tx.ExecContext(ctx, stmt); err != nil {
				return false, fmt.Errorf("index card revision content: %w", err)
			}
		}
		return false, nil
	}

	rows, err := tx.QueryContext(ctx, `SELECT cr.revision_id, cr.card_id, cr.revision_number
		  FROM card_revisions cr INDEXED BY idx_card_revisions_content_pending
		 WHERE cr.content_encoding IS NULL
		 ORDER BY cr.card_id, cr.revision_number
		 LIMIT ?`, cardContentMigrationBatch)
	if err != nil {
		return false, err
	}
	type pending struct {
		revisionID, cardID string
		number             int
	}
	var batch []pending
	for rows.Next() {
		var p pending
		if err := rows.Scan(&p.revisionID, &p.cardID, &p.number); err != nil {
			rows.Close()
			return false, err
		}
		batch = append(batch, p)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return false, err
	}
	if len(batch) == 0 {
		// The usage triggers on card_revisions predate the content columns
		// until startup reinstalls them, so mark the cached usage totals
		// stale for the bytes this migration added.
		var tracked bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM sqlite_schema WHERE type = 'table' AND name = 'background_usage_revision')`).Scan(&tracked); err != nil {
			return false, err
		}
		if tracked {
			if _, err := tx.ExecContext(ctx, `UPDATE background_usage_revision SET revision = revision + 1 WHERE singleton = 1`); err != nil {
				return false, err
			}
		}
		return true, nil
	}
	for _, p := range batch {
		if err := migrateCardRevisionContent(ctx, tx, p.revisionID, p.cardID, p.number); err != nil {
			return false, fmt.Errorf("card revision %s: %w", p.revisionID, err)
		}
	}
	return false, nil
}

// LegacyCardRevisionContent rebuilds the canonical content bytes of a revision
// from the values the pre-130 history read returned: metadata fields when
// present and the card's columns otherwise. Exported for the round-trip test.
func LegacyCardRevisionContent(metadataJSON sql.NullString, cardTitle, cardSummary, cardDefinitionOfDoneJSON string) ([]byte, string, error) {
	fields := map[string]json.RawMessage{}
	if metadataJSON.Valid && strings.TrimSpace(metadataJSON.String) != "" {
		if err := json.Unmarshal([]byte(metadataJSON.String), &fields); err != nil {
			return nil, "", fmt.Errorf("decode revision metadata: %w", err)
		}
	}
	text := func(key, fallback string) string {
		var s string
		if raw, ok := fields[key]; ok && json.Unmarshal(raw, &s) == nil && string(raw) != "null" {
			return s
		}
		return fallback
	}
	title := text("title", cardTitle)
	summary := text("summary", cardSummary)
	var definitionOfDone []string
	if raw, ok := fields["definition_of_done"]; ok {
		// A recorded JSON null is a revision without acceptance lines.
		if err := json.Unmarshal(raw, &definitionOfDone); err != nil {
			return nil, "", fmt.Errorf("decode revision definition_of_done: %w", err)
		}
	} else {
		definitionOfDone = []string{}
		if strings.TrimSpace(cardDefinitionOfDoneJSON) != "" {
			if err := json.Unmarshal([]byte(cardDefinitionOfDoneJSON), &definitionOfDone); err != nil {
				return nil, "", fmt.Errorf("decode card definition_of_done: %w", err)
			}
		}
	}
	// Same encoder as the write path (primitives.cardRevisionContent).
	content, err := json.Marshal(map[string]any{
		"title":              title,
		"summary":            summary,
		"definition_of_done": definitionOfDone,
	})
	return content, summary, err
}

func migrateCardRevisionContent(ctx context.Context, tx *sql.Tx, revisionID, cardID string, number int) error {
	var (
		artifactID, artifactHash, metadata sql.NullString
		contentRefs                        sql.NullString
		cardTitle, cardSummary, cardDoD    sql.NullString
	)
	if err := tx.QueryRowContext(ctx, `SELECT a.id, a.content_hash, a.metadata_json, a.content_refs_json, c.title, c.summary, c.definition_of_done_json
		  FROM card_revisions cr
		  LEFT JOIN cards c ON c.id = cr.card_id
		  LEFT JOIN artifacts a ON a.id = cr.artifact_id
		 WHERE cr.revision_id = ?`, revisionID).Scan(&artifactID, &artifactHash, &metadata, &contentRefs, &cardTitle, &cardSummary, &cardDoD); err != nil {
		return err
	}
	content, summary, err := LegacyCardRevisionContent(metadata, cardTitle.String, cardSummary.String, cardDoD.String)
	if err != nil {
		return err
	}
	record, err := cardcontent.PlanForCard(ctx, tx, cardID, number, content)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE card_revisions
		    SET content_encoding = ?, content_codec = ?, content_base_revision_id = ?, content_hash = ?, content_bytes = ?, summary_bytes = ?, content_payload = ?
		  WHERE revision_id = ? AND content_encoding IS NULL`,
		record.Encoding, nullIfEmpty(record.Codec), nullIfEmpty(record.BaseRevisionID), record.ContentHash, record.ContentBytes, len(summary), record.Payload, revisionID); err != nil {
		return err
	}
	// Verify the stored row, not just the planned record.
	got, err := cardcontent.LoadRevisionContent(ctx, tx, revisionID)
	if err != nil {
		return err
	}
	if string(got) != string(content) {
		return fmt.Errorf("stored content does not round-trip")
	}
	if !artifactID.Valid || artifactHash.String != record.ContentHash {
		return nil
	}
	// Verified: these are the blob's bytes. Index them exactly as the write
	// path does, then drop the duplicated fields from metadata.
	atoms := resourceaccess.ContentReferenceAtomsJSON(string(content), "structured")
	if contentRefs.Valid && contentRefs.String != string(atoms) {
		// Written by another rule; leave this artifact's access inputs alone.
		return nil
	}
	_, err = tx.ExecContext(ctx, `UPDATE artifacts
		    SET content_refs_json = ?, metadata_json = json_remove(metadata_json, '$.summary', '$.definition_of_done')
		  WHERE id = ?`, string(atoms), artifactID.String)
	return err
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func verifyCardRevisionContentMigration(ctx context.Context, tx *sql.Tx) error {
	installed, err := cardRevisionContentSchemaInstalled(ctx, tx)
	if err != nil {
		return err
	}
	if !installed {
		return fmt.Errorf("migration 130 card revision content columns missing")
	}
	var pending bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM card_revisions INDEXED BY idx_card_revisions_content_pending WHERE content_encoding IS NULL)`).Scan(&pending); err != nil {
		return err
	}
	if pending {
		return fmt.Errorf("migration 130 left card revisions without stored content")
	}
	return nil
}
