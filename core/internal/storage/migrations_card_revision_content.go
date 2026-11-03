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

	work, err := loadCardRevisionContentWork(ctx, tx)
	if err != nil {
		return false, err
	}
	if len(work) == 0 {
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
	if err := migrateCardRevisionContentChunk(ctx, tx, work); err != nil {
		return false, err
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

// cardRevisionContentWork is one pending revision of a chunk with the values
// its planning and verification need.
type cardRevisionContentWork struct {
	revisionID, cardID string
	number             int
	artifactID         sql.NullString
	artifactHash       sql.NullString
	contentRefs        sql.NullString
	content            []byte
	hash               string
	summary            string
	record             cardcontent.Record
}

// loadCardRevisionContentWork reads one chunk of pending revisions in revision
// order, with their artifact and card values, and rebuilds each content. A
// revision whose artifact an earlier revision of the chunk already writes ends
// the chunk: that write changes the artifact the later read would see, so the
// later revision waits for the next chunk, as it did when rows were read one
// at a time.
func loadCardRevisionContentWork(ctx context.Context, tx *sql.Tx) ([]cardRevisionContentWork, error) {
	rows, err := tx.QueryContext(ctx, `SELECT cr.revision_id, cr.card_id, cr.revision_number, a.id, a.content_hash, a.metadata_json, a.content_refs_json, c.title, c.summary, c.definition_of_done_json
		  FROM card_revisions cr INDEXED BY idx_card_revisions_content_pending
		  LEFT JOIN cards c ON c.id = cr.card_id
		  LEFT JOIN artifacts a ON a.id = cr.artifact_id
		 WHERE cr.content_encoding IS NULL
		 ORDER BY cr.card_id, cr.revision_number
		 LIMIT ?`, cardContentMigrationBatch)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var work []cardRevisionContentWork
	artifacts := map[string]bool{}
	for rows.Next() {
		var (
			w                               cardRevisionContentWork
			metadata                        sql.NullString
			cardTitle, cardSummary, cardDoD sql.NullString
		)
		if err := rows.Scan(&w.revisionID, &w.cardID, &w.number, &w.artifactID, &w.artifactHash, &metadata, &w.contentRefs, &cardTitle, &cardSummary, &cardDoD); err != nil {
			return nil, err
		}
		if w.artifactID.Valid {
			if artifacts[w.artifactID.String] {
				break
			}
			artifacts[w.artifactID.String] = true
		}
		content, summary, err := LegacyCardRevisionContent(metadata, cardTitle.String, cardSummary.String, cardDoD.String)
		if err != nil {
			return nil, fmt.Errorf("card revision %s: %w", w.revisionID, err)
		}
		w.content, w.summary = content, summary
		w.hash = cardcontent.Hash(content)
		work = append(work, w)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return work, nil
}

// cardRevisionRef is an earlier stored revision of a card, as planning needs it.
type cardRevisionRef struct {
	revisionID   string
	number       int
	content      []byte // snapshots only
	payloadBytes int    // snapshots only
}

type cardContentHash struct{ cardID, hash string }

// migrateCardRevisionContentChunk plans every revision of the chunk in order
// against the rows the chunk sees, then writes them with one statement per
// table. Planning reads exactly what PlanForCard reads (a card's earlier rows
// with the same content hash, and its latest earlier snapshot), with the rows
// that earlier revisions of this chunk plan standing in for their stored rows.
//
// The batch is what keeps this fast: the statement-level cost of an UPDATE on
// card_revisions and artifacts (preparing the statement and compiling the row
// triggers that fire on it) is paid once per chunk instead of once per
// revision. Each row still fires its triggers, so the side effects are the same.
func migrateCardRevisionContentChunk(ctx context.Context, tx *sql.Tx, work []cardRevisionContentWork) error {
	cardIDs, hashes := []string{}, []string{}
	seenCard, seenHash := map[string]bool{}, map[string]bool{}
	for _, w := range work {
		if !seenCard[w.cardID] {
			seenCard[w.cardID] = true
			cardIDs = append(cardIDs, w.cardID)
		}
		if !seenHash[w.hash] {
			seenHash[w.hash] = true
			hashes = append(hashes, w.hash)
		}
	}
	cardsJSON, err := json.Marshal(cardIDs)
	if err != nil {
		return err
	}
	hashesJSON, err := json.Marshal(hashes)
	if err != nil {
		return err
	}

	// Earlier stored rows of the chunk's cards with the chunk's hashes, for the
	// duplicate lookup. Refs are never duplicate targets.
	dupRefs := map[cardContentHash][]cardRevisionRef{}
	dup, err := tx.QueryContext(ctx, `SELECT card_id, content_hash, revision_id, revision_number FROM card_revisions
		 WHERE card_id IN (SELECT value FROM json_each(?)) AND content_hash IN (SELECT value FROM json_each(?)) AND content_encoding IN ('snapshot','delta')`,
		string(cardsJSON), string(hashesJSON))
	if err != nil {
		return fmt.Errorf("find duplicate card revision content: %w", err)
	}
	for dup.Next() {
		var (
			cardID, hash string
			ref          cardRevisionRef
		)
		if err := dup.Scan(&cardID, &hash, &ref.revisionID, &ref.number); err != nil {
			dup.Close()
			return err
		}
		key := cardContentHash{cardID, hash}
		dupRefs[key] = append(dupRefs[key], ref)
	}
	if err := dup.Err(); err != nil {
		dup.Close()
		return err
	}
	dup.Close()

	// Earlier stored snapshots of the chunk's cards, for the keyframe lookup.
	snapshotRefs := map[string][]cardRevisionRef{}
	snaps, err := tx.QueryContext(ctx, `SELECT card_id, revision_id, revision_number, COALESCE(length(content_payload), 0) FROM card_revisions
		 WHERE card_id IN (SELECT value FROM json_each(?)) AND content_encoding = 'snapshot'`, string(cardsJSON))
	if err != nil {
		return fmt.Errorf("find card content keyframes: %w", err)
	}
	for snaps.Next() {
		var (
			cardID string
			ref    cardRevisionRef
		)
		if err := snaps.Scan(&cardID, &ref.revisionID, &ref.number, &ref.payloadBytes); err != nil {
			snaps.Close()
			return err
		}
		snapshotRefs[cardID] = append(snapshotRefs[cardID], ref)
	}
	if err := snaps.Err(); err != nil {
		snaps.Close()
		return err
	}
	snaps.Close()

	// The keyframe a pending revision can use is the latest stored snapshot
	// before it. Only those are decoded, and only when the chunk needs one.
	keyframeIDs := []string{}
	seenKeyframe := map[string]bool{}
	for _, w := range work {
		var latest *cardRevisionRef
		for i := range snapshotRefs[w.cardID] {
			ref := &snapshotRefs[w.cardID][i]
			if ref.number < w.number && (latest == nil || ref.number > latest.number) {
				latest = ref
			}
		}
		if latest != nil && !seenKeyframe[latest.revisionID] {
			seenKeyframe[latest.revisionID] = true
			keyframeIDs = append(keyframeIDs, latest.revisionID)
		}
	}
	if len(keyframeIDs) > 0 {
		keyframes, err := cardcontent.LoadRevisionContents(ctx, tx, keyframeIDs)
		if err != nil {
			return fmt.Errorf("load card content keyframes: %w", err)
		}
		for card := range snapshotRefs {
			for i := range snapshotRefs[card] {
				if content, ok := keyframes[snapshotRefs[card][i].revisionID]; ok {
					snapshotRefs[card][i].content = content
				}
			}
		}
	}

	// Plan in revision order. Each planned row joins the lookups for the
	// revisions after it, exactly as the stored row would once written.
	for i := range work {
		w := &work[i]
		var duplicateOf string
		best := 0
		for _, ref := range dupRefs[cardContentHash{w.cardID, w.hash}] {
			if ref.number < w.number && (duplicateOf == "" || ref.number > best) {
				duplicateOf, best = ref.revisionID, ref.number
			}
		}
		in := cardcontent.PlanInput{Content: w.content, RevisionNumber: w.number, DuplicateOf: duplicateOf}
		if duplicateOf == "" {
			var latest *cardRevisionRef
			for i := range snapshotRefs[w.cardID] {
				ref := &snapshotRefs[w.cardID][i]
				if ref.number < w.number && (latest == nil || ref.number > latest.number) {
					latest = ref
				}
			}
			if latest != nil && w.number-latest.number < cardcontent.KeyframeInterval {
				if latest.content == nil {
					return fmt.Errorf("card revision %s: keyframe %s content not loaded", w.revisionID, latest.revisionID)
				}
				in.Keyframe = &cardcontent.Keyframe{RevisionID: latest.revisionID, RevisionNumber: latest.number, Content: latest.content, PayloadBytes: latest.payloadBytes}
			}
		}
		record, err := cardcontent.Plan(in)
		if err != nil {
			return fmt.Errorf("card revision %s: %w", w.revisionID, err)
		}
		w.record = record
		if record.Encoding != cardcontent.EncodingRef {
			key := cardContentHash{w.cardID, w.hash}
			dupRefs[key] = append(dupRefs[key], cardRevisionRef{revisionID: w.revisionID, number: w.number})
		}
		if record.Encoding == cardcontent.EncodingSnapshot {
			snapshotRefs[w.cardID] = append(snapshotRefs[w.cardID], cardRevisionRef{revisionID: w.revisionID, number: w.number, content: w.content, payloadBytes: len(record.Payload)})
		}
	}

	// One statement writes every planned row. Rows that matched the guard are
	// the ones this migration may still own.
	const rowColumns = 8
	args := make([]any, 0, len(work)*rowColumns)
	for _, w := range work {
		args = append(args, w.revisionID, w.record.Encoding, nullIfEmpty(w.record.Codec), nullIfEmpty(w.record.BaseRevisionID), w.record.ContentHash, w.record.ContentBytes, len(w.summary), w.record.Payload)
	}
	if _, err := tx.ExecContext(ctx, `WITH v(revision_id, content_encoding, content_codec, content_base_revision_id, content_hash, content_bytes, summary_bytes, content_payload) AS (VALUES `+valuesRows(len(work), rowColumns)+`)
		UPDATE card_revisions
		    SET content_encoding = v.content_encoding, content_codec = v.content_codec, content_base_revision_id = v.content_base_revision_id, content_hash = v.content_hash, content_bytes = v.content_bytes, summary_bytes = v.summary_bytes, content_payload = v.content_payload
		   FROM v
		  WHERE card_revisions.revision_id = v.revision_id AND card_revisions.content_encoding IS NULL`, args...); err != nil {
		return fmt.Errorf("write card revision content: %w", err)
	}

	// Verify the stored rows, not just the planned records.
	ids := make([]string, 0, len(work))
	for _, w := range work {
		ids = append(ids, w.revisionID)
	}
	got, err := cardcontent.LoadRevisionContents(ctx, tx, ids)
	if err != nil {
		return err
	}
	var verified []cardRevisionContentWork
	for _, w := range work {
		if string(got[w.revisionID]) != string(w.content) {
			return fmt.Errorf("card revision %s: stored content does not round-trip", w.revisionID)
		}
		if !w.artifactID.Valid || w.artifactHash.String != w.record.ContentHash {
			continue
		}
		atoms := resourceaccess.ContentReferenceAtomsJSON(string(w.content), "structured")
		if w.contentRefs.Valid && w.contentRefs.String != string(atoms) {
			// Written by another rule; leave this artifact's access inputs alone.
			continue
		}
		verified = append(verified, w)
	}
	if len(verified) == 0 {
		return nil
	}
	// Verified: these are the blobs' bytes. Index them exactly as the write
	// path does, then drop the duplicated fields from metadata.
	args = args[:0]
	for _, w := range verified {
		args = append(args, w.artifactID.String, string(resourceaccess.ContentReferenceAtomsJSON(string(w.content), "structured")))
	}
	if _, err := tx.ExecContext(ctx, `WITH v(artifact_id, content_refs_json) AS (VALUES `+valuesRows(len(verified), 2)+`)
		UPDATE artifacts
		    SET content_refs_json = v.content_refs_json, metadata_json = json_remove(artifacts.metadata_json, '$.summary', '$.definition_of_done')
		   FROM v
		  WHERE artifacts.id = v.artifact_id`, args...); err != nil {
		return fmt.Errorf("index card revision artifacts: %w", err)
	}
	return nil
}

// valuesRows renders n tuples of cols placeholders for a VALUES list.
func valuesRows(n, cols int) string {
	tuple := "(" + strings.TrimSuffix(strings.Repeat("?,", cols), ",") + ")"
	var b strings.Builder
	for i := 0; i < n; i++ {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(tuple)
	}
	return b.String()
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
