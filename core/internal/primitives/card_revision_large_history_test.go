package primitives

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"agent-nexus-core/internal/cardcontent"
	"github.com/google/uuid"
)

// The card from issue #510: 758 revisions, each with a summary of about 1 MB
// (accepted before the summary limit existed). ANX_TIMEOUT was 45s there.
const (
	largeHistoryRevisions = 758
	largeHistoryLines     = 16000 // evidenceText lines; about 0.9 MB
	largeHistoryDeadline  = 45 * time.Second
)

// appendStoredCardRevisions adds revisions from+1..to to cardID directly in
// SQL, encoding each one's content exactly as the write path does. It skips
// the card-row update the API makes per revision, which is what keeps a
// fixture of this size buildable in a unit test.
func appendStoredCardRevisions(t *testing.T, ctx context.Context, db *sql.DB, cardID, title string, from, to int, summary func(int) string) {
	t.Helper()
	var prev string
	if err := db.QueryRowContext(ctx, `SELECT revision_id FROM card_revisions WHERE card_id = ? AND revision_number = ?`, cardID, from).Scan(&prev); err != nil {
		t.Fatal(err)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	insertArtifact, err := tx.PrepareContext(ctx, `INSERT INTO artifacts(id, kind, created_at, created_by, content_type, content_hash, refs_json, metadata_json, content_refs_json)
		VALUES (?, 'card', ?, 'actor-1', 'structured', ?, ?, ?, '[]')`)
	if err != nil {
		t.Fatal(err)
	}
	defer insertArtifact.Close()
	insertRevision, err := tx.PrepareContext(ctx, `INSERT INTO card_revisions(revision_id, card_id, revision_number, prev_revision_id, artifact_id, refs_json, revision_hash, created_at, created_by,
			content_encoding, content_codec, content_base_revision_id, content_hash, content_bytes, summary_bytes, content_payload)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'actor-1', ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		t.Fatal(err)
	}
	defer insertRevision.Close()
	for n := from + 1; n <= to; n++ {
		text := summary(n)
		content, err := cardRevisionContent(title, text, nil)
		if err != nil {
			t.Fatal(err)
		}
		record, err := cardcontent.PlanForCard(ctx, tx, cardID, n, content)
		if err != nil {
			t.Fatal(err)
		}
		id := uuid.NewString()
		now := time.Now().UTC().Format(time.RFC3339Nano)
		meta, _ := json.Marshal(map[string]any{"kind": "card", "card_id": cardID, "revision_id": id, "revision_number": n, "title": title})
		if _, err := insertArtifact.ExecContext(ctx, id, now, record.ContentHash, `["card:`+cardID+`"]`, string(meta)); err != nil {
			t.Fatal(err)
		}
		if _, err := insertRevision.ExecContext(ctx,
			id, cardID, n, prev, id, `["card:`+cardID+`"]`, record.ContentHash, now,
			record.Encoding, nullableString(record.Codec), nullableString(record.BaseRevisionID), record.ContentHash, record.ContentBytes, len(text), record.Payload); err != nil {
			t.Fatal(err)
		}
		prev = id
	}
	last := summary(to)
	if _, err := tx.ExecContext(ctx, `UPDATE cards SET summary = ?, head_revision_id = ?, head_revision_number = ? WHERE id = ?`, last, prev, to, cardID); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

// appendRepeatedCardRevisions adds revisions from+1..to, where revision n
// repeats the content of revision ((n-1) mod period)+1, as the content-addressed
// refs the write path stores for repeated content. It inserts them with one
// statement per table: per-row inserts into these trigger-heavy tables cost
// about 50ms each, which would dominate the test.
func appendRepeatedCardRevisions(t *testing.T, ctx context.Context, db *sql.DB, cardID string, period, from, to int) {
	t.Helper()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	const series = `WITH RECURSIVE n(i) AS (SELECT ?2 + 1 UNION ALL SELECT i + 1 FROM n WHERE i < ?3),
		rev AS (SELECT n.i, printf('%s-r%d', ?1, n.i) AS id, src.revision_id AS src_id, src.content_hash, src.content_bytes, src.summary_bytes
		          FROM n JOIN card_revisions src ON src.card_id = ?1 AND src.revision_number = ((n.i - 1) % ?4) + 1)`
	if _, err := tx.ExecContext(ctx, series+`
		INSERT INTO artifacts(id, kind, created_at, created_by, content_type, content_hash, refs_json, metadata_json, content_refs_json)
		SELECT id, 'card', strftime('%Y-%m-%dT%H:%M:%fZ','now'), 'actor-1', 'structured', content_hash, json_array('card:' || ?1),
		       json_object('kind', 'card', 'card_id', ?1, 'revision_id', id, 'revision_number', i), '[]'
		  FROM rev`, cardID, from, to, period); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, series+`
		INSERT INTO card_revisions(revision_id, card_id, revision_number, prev_revision_id, artifact_id, refs_json, revision_hash, created_at, created_by,
			content_encoding, content_base_revision_id, content_hash, content_bytes, summary_bytes)
		SELECT id, ?1, i,
		       CASE WHEN i = ?2 + 1 THEN (SELECT revision_id FROM card_revisions WHERE card_id = ?1 AND revision_number = ?2) ELSE printf('%s-r%d', ?1, i - 1) END,
		       id, json_array('card:' || ?1), content_hash, strftime('%Y-%m-%dT%H:%M:%fZ','now'), 'actor-1',
		       'ref', src_id, content_hash, content_bytes, summary_bytes
		  FROM rev ORDER BY i`, cardID, from, to, period); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE cards SET head_revision_id = ?, head_revision_number = ? WHERE id = ?`, fmt.Sprintf("%s-r%d", cardID, to), to, cardID); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

// TestLargeCardHistoryReadsStayBounded is the regression test for issue #510.
// On the previous layout, reading this card materialized every revision's
// summary (about 680 MB of JSON) and missed the 45s deadline.
func TestLargeCardHistoryReadsStayBounded(t *testing.T) {
	t.Parallel()
	ctx, ws, store, boardID := newCardRevisionTestStore(t)
	store.cardSummaryLimit = 2 << 20 // a legacy card, accepted before the limit
	added, err := store.AddBoardCard(ctx, "actor-1", boardID, AddBoardCardInput{Title: "Release foundation", Body: evidenceText(1, largeHistoryLines)})
	if err != nil {
		t.Fatal(err)
	}
	store.cardSummaryLimit = 0
	cardID := added.Card["id"].(string)
	// 40 distinct ~1 MB summaries (past one keyframe interval) go through the
	// write path's encoder; the remaining revisions repeat them, as a sync job
	// that rewrites its evidence dump does, and are stored as refs.
	const distinct = 40
	texts := make([]string, distinct)
	summary := func(n int) string {
		i := (n - 1) % distinct
		if texts[i] == "" {
			texts[i] = evidenceText(i+1, largeHistoryLines)
		}
		return texts[i]
	}

	build := time.Now()
	appendStoredCardRevisions(t, ctx, ws.DB(), cardID, "Release foundation", 1, distinct, summary)
	appendRepeatedCardRevisions(t, ctx, ws.DB(), cardID, distinct, distinct, largeHistoryRevisions)
	if _, err := ws.DB().ExecContext(ctx, `UPDATE cards SET summary = ? WHERE id = ?`, summary(largeHistoryRevisions), cardID); err != nil {
		t.Fatal(err)
	}
	var payloadBytes, contentBytes int64
	if err := ws.DB().QueryRowContext(ctx, `SELECT SUM(length(content_payload)), SUM(content_bytes) FROM card_revisions WHERE card_id = ?`, cardID).Scan(&payloadBytes, &contentBytes); err != nil {
		t.Fatal(err)
	}
	t.Logf("built %d revisions in %v: %d content bytes stored as %d payload bytes (%.1fx)", largeHistoryRevisions, time.Since(build).Round(time.Millisecond), contentBytes, payloadBytes, float64(contentBytes)/float64(payloadBytes))
	if payloadBytes*20 > contentBytes {
		t.Fatalf("history storage %d bytes for %d content bytes; expected keyframes and deltas to save at least 20x", payloadBytes, contentBytes)
	}
	if _, err := cardcontent.LoadRevisionContent(ctx, ws.DB(), fmt.Sprintf("%s-r%d", cardID, largeHistoryRevisions)); err != nil {
		t.Fatalf("fixture head revision does not load: %v", err)
	}

	timed := func(name string, budget time.Duration, maxJSON int, read func() (any, error)) any {
		t.Helper()
		start := time.Now()
		out, err := read()
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		body, err := json.Marshal(out)
		if err != nil {
			t.Fatal(err)
		}
		elapsed := time.Since(start)
		t.Logf("%s: %v, %d response bytes", name, elapsed.Round(time.Microsecond), len(body))
		if elapsed > budget {
			t.Fatalf("%s took %v, budget %v (deadline %v)", name, elapsed, budget, largeHistoryDeadline)
		}
		if len(body) > maxJSON {
			t.Fatalf("%s returned %d bytes, want at most %d", name, len(body), maxJSON)
		}
		return out
	}
	// Budgets are a fraction of the deadline, loose enough for a loaded CI
	// runner; the measured values are logged above.
	const budget = largeHistoryDeadline / 9
	current := timed("cards get", budget, 2<<20, func() (any, error) { return store.GetBoardCard(ctx, "", cardID) })
	if card := current.(map[string]any); card["history"] != nil || card["summary"] != summary(largeHistoryRevisions) {
		t.Fatalf("cards get must return the current row only")
	}
	page := timed("cards history (first page)", budget, 64<<10, func() (any, error) { return store.ListCardRevisions(ctx, cardID, 0, "") }).(CardRevisionPage)
	if len(page.Revisions) != DefaultCardRevisionPageSize || page.NextCursor == "" {
		t.Fatalf("history page = %d revisions, cursor %q", len(page.Revisions), page.NextCursor)
	}
	timed("cards history (max page)", budget, 256<<10, func() (any, error) {
		return store.ListCardRevisions(ctx, cardID, MaxCardRevisionPageSize, page.NextCursor)
	})
	var oldest, newest string
	if err := ws.DB().QueryRowContext(ctx, `SELECT (SELECT revision_id FROM card_revisions WHERE card_id = ?1 AND revision_number = 400), (SELECT head_revision_id FROM cards WHERE id = ?1)`, cardID).Scan(&oldest, &newest); err != nil {
		t.Fatal(err)
	}
	rev := timed("cards revision get", budget, 2<<20, func() (any, error) { return store.GetCardRevision(ctx, cardID, oldest) }).(map[string]any)
	if rev["summary"] != summary(400) {
		t.Fatal("revision 400 does not reconstruct")
	}
	diff := timed("cards revision diff", budget, 300<<10, func() (any, error) { return store.DiffCardRevisions(ctx, cardID, newest, "") }).(map[string]any)
	if s := diff["summary"].(map[string]any); s["changed"] != true || s["added_lines"] == 0 {
		t.Fatalf("diff of the head revision = %v", s)
	}
	reviseStart := time.Now()
	next := summary(largeHistoryRevisions + 1)
	if _, _, err := store.CreateCardRevision(ctx, "actor-1", cardID, CreateCardRevisionInput{Summary: &next}); err == nil {
		t.Fatal("a new revision over the summary limit must fail")
	} else if want := fmt.Sprintf("%d-byte", MaxCardSummaryBytes); !strings.Contains(err.Error(), want) {
		t.Fatalf("limit error must name the limit: %v", err)
	}
	t.Logf("oversized revise rejected in %v", time.Since(reviseStart).Round(time.Microsecond))
	short := "Evidence moved to source_refs."
	if _, _, err := store.CreateCardRevision(ctx, "actor-1", cardID, CreateCardRevisionInput{Summary: &short}); err != nil {
		t.Fatalf("a revision under the limit must succeed on a card with large history: %v", err)
	}
}
