// Migrations 27-28: document knowledge columns and the document FTS index.

package storage

import (
	"context"
	"database/sql"
	"fmt"
)

func applyMigration27DocumentKnowledge(ctx context.Context, tx *sql.Tx) error {
	ok, err := sqliteTableExists(ctx, tx, "documents")
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	for _, col := range []struct {
		name string
		ddl  string
	}{
		{name: "source", ddl: `ALTER TABLE documents ADD COLUMN source TEXT NOT NULL DEFAULT ''`},
		{name: "tags_json", ddl: `ALTER TABLE documents ADD COLUMN tags_json TEXT NOT NULL DEFAULT '[]'`},
		{name: "search_text", ddl: `ALTER TABLE documents ADD COLUMN search_text TEXT NOT NULL DEFAULT ''`},
	} {
		has, err := sqliteTableHasColumn(ctx, tx, "documents", col.name)
		if err != nil {
			return fmt.Errorf("migration 27 pragma documents.%s: %w", col.name, err)
		}
		if has {
			continue
		}
		if _, err := tx.ExecContext(ctx, col.ddl); err != nil {
			return fmt.Errorf("migration 27 add documents.%s: %w", col.name, err)
		}
	}
	return nil
}

func applyMigration28DocumentFTS(ctx context.Context, tx *sql.Tx) error {
	ok, err := sqliteTableExists(ctx, tx, "documents")
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	for _, col := range []struct {
		name string
		ddl  string
	}{
		{name: "hosts_json", ddl: `ALTER TABLE documents ADD COLUMN hosts_json TEXT NOT NULL DEFAULT '[]'`},
		{name: "verified_at", ddl: `ALTER TABLE documents ADD COLUMN verified_at TEXT NOT NULL DEFAULT ''`},
	} {
		has, err := sqliteTableHasColumn(ctx, tx, "documents", col.name)
		if err != nil {
			return fmt.Errorf("migration 28 pragma documents.%s: %w", col.name, err)
		}
		if has {
			continue
		}
		if _, err := tx.ExecContext(ctx, col.ddl); err != nil {
			return fmt.Errorf("migration 28 add documents.%s: %w", col.name, err)
		}
	}
	if _, err := tx.ExecContext(ctx, `CREATE VIRTUAL TABLE IF NOT EXISTS document_fts USING fts5(
		document_id UNINDEXED,
		title,
		body,
		summary,
		source,
		tags,
		comments,
		tokenize = 'unicode61'
	)`); err != nil {
		return fmt.Errorf("migration 28 create document_fts: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM document_fts`); err != nil {
		return fmt.Errorf("migration 28 clear document_fts: %w", err)
	}

	colExpr := func(column, fallback string) (string, error) {
		has, err := sqliteTableHasColumn(ctx, tx, "documents", column)
		if err != nil {
			return "", err
		}
		if !has {
			return fallback, nil
		}
		return "COALESCE(d." + column + ", " + fallback + ")", nil
	}
	titleExpr, err := colExpr("title", "''")
	if err != nil {
		return err
	}
	bodyExpr, err := colExpr("search_text", "''")
	if err != nil {
		return err
	}
	summaryExpr, err := colExpr("summary", "''")
	if err != nil {
		return err
	}
	sourceExpr, err := colExpr("source", "''")
	if err != nil {
		return err
	}
	hasTags, err := sqliteTableHasColumn(ctx, tx, "documents", "tags_json")
	if err != nil {
		return err
	}
	tagsExpr := "''"
	if hasTags {
		tagsExpr = `COALESCE((
			SELECT GROUP_CONCAT(json_each.value, ' ')
			FROM json_each(CASE WHEN json_valid(d.tags_json) THEN d.tags_json ELSE '[]' END)
		), '')`
	}
	commentsExpr := "''"
	hasThreadID, err := sqliteTableHasColumn(ctx, tx, "documents", "thread_id")
	if err != nil {
		return err
	}
	eventsOK, err := sqliteTableExists(ctx, tx, "events")
	if err != nil {
		return err
	}
	if hasThreadID && eventsOK {
		commentsExpr = `COALESCE((
			SELECT GROUP_CONCAT(
				COALESCE(
					json_extract(e.payload_json, '$.payload.text'),
					json_extract(e.payload_json, '$.summary'),
					''
				),
				' '
			)
			FROM events e
			WHERE e.thread_id = d.thread_id
			  AND e.type = 'message_posted'
			  AND COALESCE(trim(e.trashed_at), '') = ''
		), '')`
	}
	insertSQL := `INSERT INTO document_fts(document_id, title, body, summary, source, tags, comments)
		SELECT
			d.id,
			` + titleExpr + `,
			` + bodyExpr + `,
			` + summaryExpr + `,
			` + sourceExpr + `,
			` + tagsExpr + `,
			` + commentsExpr + `
		FROM documents d`
	if _, err := tx.ExecContext(ctx, insertSQL); err != nil {
		return fmt.Errorf("migration 28 backfill document_fts: %w", err)
	}
	return nil
}
