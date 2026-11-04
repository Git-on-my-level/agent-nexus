package storage

import (
	"context"
	"database/sql"
	"testing"

	"agent-nexus-core/internal/cardcontent"
)

// Two revisions that name one artifact must not share a migration chunk: the
// first one's artifact write changes what the second one's read sees.
func TestCardRevisionContentChunkSplitsSharedArtifacts(t *testing.T) {
	ctx := context.Background()
	db := mainHistoryDatabase(t, t.TempDir(), 129)
	defer db.Close()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cardRevisionContentMigrationChunk(ctx, tx); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	const cardDoD = `["ship"]`
	meta := func(summary string) string {
		return `{"title":"T","summary":"` + summary + `","definition_of_done":["ship"]}`
	}
	content := func(summary string) []byte {
		c, _, err := LegacyCardRevisionContent(sql.NullString{String: meta(summary), Valid: true}, "Card", "card summary", cardDoD)
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	first, second := content("one"), content("two")
	for _, stmt := range []struct {
		q    string
		args []any
	}{
		{`INSERT INTO cards(id, title, definition_of_done_json, column_key, created_at, created_by, updated_at, updated_by, summary) VALUES ('c', 'Card', ?, 'ready', '2026-01-01T00:00:00Z', 'owner', '2026-01-01T00:00:00Z', 'owner', 'card summary')`, []any{cardDoD}},
		{`INSERT INTO artifacts(id, handle, kind, thread_id, created_at, created_by, content_type, content_hash, refs_json, metadata_json) VALUES ('a1', 'a1', 'card', 't', '2026-01-01T00:00:00Z', 'owner', 'structured', ?, '[]', ?)`, []any{cardcontent.Hash(first), meta("one")}},
		{`INSERT INTO artifacts(id, handle, kind, thread_id, created_at, created_by, content_type, content_hash, refs_json, metadata_json) VALUES ('a2', 'a2', 'card', 't', '2026-01-01T00:00:00Z', 'owner', 'structured', ?, '[]', ?)`, []any{cardcontent.Hash(second), meta("two")}},
		{`INSERT INTO card_revisions(revision_id, card_id, revision_number, prev_revision_id, artifact_id, thread_id, refs_json, revision_hash, created_at, created_by) VALUES ('r1', 'c', 1, NULL, 'a1', 't', '[]', 'h', '2026-01-01T00:00:00Z', 'owner')`, nil},
		{`INSERT INTO card_revisions(revision_id, card_id, revision_number, prev_revision_id, artifact_id, thread_id, refs_json, revision_hash, created_at, created_by) VALUES ('r2', 'c', 2, 'r1', 'a2', 't', '[]', 'h', '2026-01-01T00:00:00Z', 'owner')`, nil},
		{`INSERT INTO card_revisions(revision_id, card_id, revision_number, prev_revision_id, artifact_id, thread_id, refs_json, revision_hash, created_at, created_by) VALUES ('r3', 'c', 3, 'r2', 'a2', 't', '[]', 'h', '2026-01-01T00:00:00Z', 'owner')`, nil},
	} {
		if _, err := db.ExecContext(ctx, stmt.q, stmt.args...); err != nil {
			t.Fatalf("%s: %v", stmt.q, err)
		}
	}

	tx, err = db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	work, err := loadCardRevisionContentWork(ctx, tx)
	if err != nil {
		t.Fatal(err)
	}
	tx.Rollback()
	if len(work) != 2 || work[0].revisionID != "r1" || work[1].revisionID != "r2" {
		t.Fatalf("first chunk = %+v, want r1 and r2 only", work)
	}

	for i := 0; i < 10; i++ {
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		done, err := cardRevisionContentMigrationChunk(ctx, tx)
		if err != nil {
			tx.Rollback()
			t.Fatal(err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
		if done {
			break
		}
	}
	var pending int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM card_revisions WHERE content_encoding IS NULL`).Scan(&pending); err != nil {
		t.Fatal(err)
	}
	if pending != 0 {
		t.Fatalf("%d revisions left without stored content", pending)
	}
	for _, id := range []string{"r1", "r2", "r3"} {
		if _, err := cardcontent.LoadRevisionContent(ctx, db, id); err != nil {
			t.Fatalf("revision %s: %v", id, err)
		}
	}
}
