package primitives

import (
	"context"
	"database/sql"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"agent-nexus-core/internal/blob"
	"agent-nexus-core/internal/cardcontent"
	"agent-nexus-core/internal/resourceaccess"
	"agent-nexus-core/internal/storage"
)

// legacyRevision is one revision as the pre-130 schema stored it.
type legacyRevision struct {
	revisionID, artifactID, cardID string
	content                        []byte // the blob bytes, or the bytes the history read implied
	hasBlob                        bool
	verifiable                     bool // content hashes to the artifact's content_hash
	metadata                       string
}

// rewindToLegacyCardRevisions turns every card revision of the workspace into
// its pre-130 form: content columns and indexes gone, title, summary and
// definition of done in the artifact metadata, content in a blob. Rows listed
// in migration19 take the shape migration 19 wrote instead (no definition of
// done in metadata, no blob, fail-closed access atoms); when stale is set the
// card's columns no longer reproduce the recorded hash.
func rewindToLegacyCardRevisions(t *testing.T, ctx context.Context, db *sql.DB, backend blob.Backend, migration19 map[string]bool) []legacyRevision {
	t.Helper()
	rows, err := db.QueryContext(ctx, `SELECT cr.revision_id, cr.artifact_id, cr.card_id FROM card_revisions cr ORDER BY cr.card_id, cr.revision_number`)
	if err != nil {
		t.Fatal(err)
	}
	var out []legacyRevision
	for rows.Next() {
		var r legacyRevision
		if err := rows.Scan(&r.revisionID, &r.artifactID, &r.cardID); err != nil {
			t.Fatal(err)
		}
		out = append(out, r)
	}
	rows.Close()
	for i := range out {
		r := &out[i]
		content, err := cardcontent.LoadRevisionContent(ctx, db, r.revisionID)
		if err != nil {
			t.Fatal(err)
		}
		var fields struct {
			Title            string          `json:"title"`
			Summary          string          `json:"summary"`
			DefinitionOfDone json.RawMessage `json:"definition_of_done"`
		}
		if err := json.Unmarshal(content, &fields); err != nil {
			t.Fatal(err)
		}
		r.content = content
		if migration19[r.revisionID] {
			// Migration 19: metadata carries title and summary only; the
			// definition of done comes from the card's current columns.
			var cardDoD string
			if err := db.QueryRowContext(ctx, `SELECT definition_of_done_json FROM cards WHERE id = ?`, r.cardID).Scan(&cardDoD); err != nil {
				t.Fatal(err)
			}
			implied, _, err := storage.LegacyCardRevisionContent(sql.NullString{String: `{"title":` + string(mustJSON(t, fields.Title)) + `,"summary":` + string(mustJSON(t, fields.Summary)) + `}`, Valid: true}, "", "", cardDoD)
			if err != nil {
				t.Fatal(err)
			}
			r.verifiable = string(implied) == string(content)
			r.content = implied
			if _, err := db.ExecContext(ctx, `UPDATE artifacts SET content_refs_json = NULL,
				metadata_json = json_set(json_remove(metadata_json, '$.definition_of_done'), '$.title', ?, '$.summary', ?)
				WHERE id = ?`, fields.Title, fields.Summary, r.artifactID); err != nil {
				t.Fatal(err)
			}
		} else {
			r.verifiable, r.hasBlob = true, true
			staged, err := backend.Write(ctx, cardcontent.Hash(content), content)
			if err != nil {
				t.Fatal(err)
			}
			if err := staged.Promote(); err != nil {
				t.Fatal(err)
			}
			if _, err := db.ExecContext(ctx, `UPDATE artifacts
				SET metadata_json = json_set(metadata_json, '$.title', ?, '$.summary', ?, '$.definition_of_done', json(?))
				WHERE id = ?`, fields.Title, fields.Summary, string(fields.DefinitionOfDone), r.artifactID); err != nil {
				t.Fatal(err)
			}
		}
		if err := db.QueryRowContext(ctx, `SELECT metadata_json FROM artifacts WHERE id = ?`, r.artifactID).Scan(&r.metadata); err != nil {
			t.Fatal(err)
		}
	}
	for _, stmt := range []string{
		`DROP INDEX idx_card_revisions_card_content_hash`,
		`DROP INDEX idx_card_revisions_card_snapshots`,
		`DROP INDEX idx_card_revisions_content_pending`,
		`ALTER TABLE card_revisions DROP COLUMN content_payload`,
		`ALTER TABLE card_revisions DROP COLUMN summary_bytes`,
		`ALTER TABLE card_revisions DROP COLUMN content_bytes`,
		`ALTER TABLE card_revisions DROP COLUMN content_hash`,
		`ALTER TABLE card_revisions DROP COLUMN content_base_revision_id`,
		`ALTER TABLE card_revisions DROP COLUMN content_codec`,
		`ALTER TABLE card_revisions DROP COLUMN content_encoding`,
		`DELETE FROM schema_migrations WHERE version = 130`,
	} {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
	return out
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

type cardRevisionStorageState struct {
	encoding, codec, base, hash string
	bytes, summaryBytes         int
	payload                     []byte
	metadata                    string
	contentRefs                 sql.NullString
}

func snapshotCardRevisionStorage(t *testing.T, ctx context.Context, db *sql.DB) map[string]cardRevisionStorageState {
	t.Helper()
	rows, err := db.QueryContext(ctx, `SELECT cr.revision_id, cr.content_encoding, COALESCE(cr.content_codec,''), COALESCE(cr.content_base_revision_id,''), cr.content_hash, cr.content_bytes, cr.summary_bytes, COALESCE(cr.content_payload, x''), a.metadata_json, a.content_refs_json
		FROM card_revisions cr JOIN artifacts a ON a.id = cr.artifact_id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := map[string]cardRevisionStorageState{}
	for rows.Next() {
		var id string
		var s cardRevisionStorageState
		if err := rows.Scan(&id, &s.encoding, &s.codec, &s.base, &s.hash, &s.bytes, &s.summaryBytes, &s.payload, &s.metadata, &s.contentRefs); err != nil {
			t.Fatal(err)
		}
		out[id] = s
	}
	return out
}

func reopenWorkspace(t *testing.T, ctx context.Context, ws *storage.Workspace, root string) *storage.Workspace {
	t.Helper()
	if err := ws.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := storage.InitializeWorkspace(ctx, root)
	if err != nil {
		t.Fatalf("reopen workspace (runs migration 130): %v", err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	return reopened
}

// TestCardRevisionContentMigrationRoundTrip rewinds a workspace to the pre-130
// layout, reopens it so migration 130 runs over more than one chunk, and checks
// that every revision's stored content reconstructs byte for byte, that access
// inputs only change for verified rows, and that a second run changes nothing.
func TestCardRevisionContentMigrationRoundTrip(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	root := t.TempDir()
	ws, err := initializeTestWorkspace(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	backend := blob.NewFilesystemBackend(ws.Layout().ArtifactContentDir)
	store := NewStore(ws.DB(), backend, ws.Layout().ArtifactContentDir)
	board, err := store.CreateBoard(ctx, "actor-1", map[string]any{"title": "Ops"})
	if err != nil {
		t.Fatal(err)
	}
	boardID := board["id"].(string)

	// A long card: deltas, keyframes, reverts and definition-of-done changes
	// across more than one migration chunk.
	long, err := store.AddBoardCard(ctx, "actor-1", boardID, AddBoardCardInput{Title: "Evidence", Body: evidenceText(1, 120), DefinitionOfDone: []string{"tests pass"}})
	if err != nil {
		t.Fatal(err)
	}
	longID := long.Card["id"].(string)
	for i := 2; i <= 90; i++ {
		in := CreateCardRevisionInput{}
		summary := evidenceText(i, 120)
		if i%15 == 0 {
			summary = evidenceText(i-5, 120)
		}
		in.Summary = &summary
		switch i {
		case 20:
			dod := []string{"docs updated", "tests pass"}
			in.DefinitionOfDone = &dod
		case 40:
			dod := []string{}
			in.DefinitionOfDone = &dod
		case 41:
			title := "Evidence <&> é"
			in.Title = &title
		}
		if _, _, err := store.CreateCardRevision(ctx, "actor-1", longID, in); err != nil {
			t.Fatal(err)
		}
	}
	// A card whose first revisions take migration 19's shape: the first one
	// still matches the card's columns, the second does not.
	short, err := store.AddBoardCard(ctx, "actor-1", boardID, AddBoardCardInput{Title: "Imported", Body: "see document:launch-plan", DefinitionOfDone: []string{"ship"}})
	if err != nil {
		t.Fatal(err)
	}
	shortID := short.Card["id"].(string)
	stale := "see document:launch-plan, revised"
	staleDoD := []string{"ship", "announce"}
	if _, _, err := store.CreateCardRevision(ctx, "actor-1", shortID, CreateCardRevisionInput{Summary: &stale, DefinitionOfDone: &staleDoD}); err != nil {
		t.Fatal(err)
	}
	backToShip := []string{"ship"}
	final := "final"
	if _, _, err := store.CreateCardRevision(ctx, "actor-1", shortID, CreateCardRevisionInput{Summary: &final, DefinitionOfDone: &backToShip}); err != nil {
		t.Fatal(err)
	}
	migration19 := map[string]bool{}
	for _, r := range storedCardRevisions(t, ctx, store, shortID)[:2] {
		migration19[r.id] = true
	}

	legacy := rewindToLegacyCardRevisions(t, ctx, ws.DB(), backend, migration19)
	if len(legacy) <= 64 {
		t.Fatalf("fixture must span several migration chunks, has %d revisions", len(legacy))
	}
	var verifiable, unverifiable int
	for _, r := range legacy {
		if r.verifiable {
			verifiable++
		} else {
			unverifiable++
		}
	}
	if unverifiable != 1 || verifiable != len(legacy)-1 {
		t.Fatalf("fixture verifiable=%d unverifiable=%d", verifiable, unverifiable)
	}

	ws = reopenWorkspace(t, ctx, ws, root)
	store = NewStore(ws.DB(), backend, ws.Layout().ArtifactContentDir)
	for _, r := range legacy {
		got, err := cardcontent.LoadRevisionContent(ctx, ws.DB(), r.revisionID)
		if err != nil {
			t.Fatalf("revision %s: %v", r.revisionID, err)
		}
		if string(got) != string(r.content) {
			t.Fatalf("revision %s does not reconstruct:\n got %s\nwant %s", r.revisionID, got, r.content)
		}
		if r.hasBlob {
			blobBytes, err := backend.Read(ctx, cardcontent.Hash(r.content))
			if err != nil {
				t.Fatal(err)
			}
			if string(blobBytes) != string(got) {
				t.Fatalf("revision %s differs from its legacy blob", r.revisionID)
			}
		}
		var metadata string
		var contentRefs sql.NullString
		if err := ws.DB().QueryRowContext(ctx, `SELECT metadata_json, content_refs_json FROM artifacts WHERE id = ?`, r.artifactID).Scan(&metadata, &contentRefs); err != nil {
			t.Fatal(err)
		}
		if r.verifiable {
			if strings.Contains(metadata, `"summary"`) || strings.Contains(metadata, `"definition_of_done"`) {
				t.Fatalf("verified revision %s keeps duplicated content in metadata: %s", r.revisionID, metadata)
			}
			if want := string(resourceaccess.ContentReferenceAtomsJSON(string(got), "structured")); !contentRefs.Valid || contentRefs.String != want {
				t.Fatalf("verified revision %s access atoms = %v, want %s", r.revisionID, contentRefs, want)
			}
			body, _, err := store.GetArtifactContent(ctx, r.artifactID)
			if err != nil || string(body) != string(got) {
				t.Fatalf("artifact content of %s = %q err=%v", r.revisionID, body, err)
			}
		} else if metadata != r.metadata || contentRefs.Valid {
			t.Fatalf("unverified revision %s changed its access inputs: metadata=%s refs=%v", r.revisionID, metadata, contentRefs)
		}

		// The API serves the reconstructed values.
		var want struct {
			Title            string   `json:"title"`
			Summary          string   `json:"summary"`
			DefinitionOfDone []string `json:"definition_of_done"`
		}
		if err := json.Unmarshal(r.content, &want); err != nil {
			t.Fatal(err)
		}
		rev, err := store.GetCardRevision(ctx, r.cardID, r.revisionID)
		if err != nil {
			t.Fatal(err)
		}
		gotDoD, _ := rev["definition_of_done"].([]string)
		if rev["title"] != want.Title || rev["summary"] != want.Summary || len(gotDoD) != len(want.DefinitionOfDone) || (len(gotDoD) > 0 && !reflect.DeepEqual(gotDoD, want.DefinitionOfDone)) {
			t.Fatalf("revision %s API = %v, want %+v", r.revisionID, rev, want)
		}
	}
	encodings := map[string]int{}
	before := snapshotCardRevisionStorage(t, ctx, ws.DB())
	for _, s := range before {
		encodings[s.encoding]++
	}
	if encodings[cardcontent.EncodingSnapshot] == 0 || encodings[cardcontent.EncodingDelta] == 0 || encodings[cardcontent.EncodingRef] == 0 {
		t.Fatalf("migration must produce snapshots, deltas and refs: %v", encodings)
	}

	// Idempotent: running the migration again over migrated rows is a no-op.
	if _, err := ws.DB().ExecContext(ctx, `DELETE FROM schema_migrations WHERE version = 130`); err != nil {
		t.Fatal(err)
	}
	ws = reopenWorkspace(t, ctx, ws, root)
	if after := snapshotCardRevisionStorage(t, ctx, ws.DB()); !reflect.DeepEqual(before, after) {
		t.Fatal("re-running migration 130 changed stored card revisions")
	}
}
