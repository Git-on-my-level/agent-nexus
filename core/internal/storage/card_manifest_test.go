package storage

import (
	"context"
	"strings"
	"testing"

	"agent-nexus-core/internal/cardcontent"
	"agent-nexus-core/internal/resourceaccess"
)

// A single revision whose content cannot be loaded must not wedge the
// backfill: its manifest stays untouched, the good rows around it are pruned,
// and the cursor advances past it.
func TestCardManifestBackfillSkipsUnloadableRevision(t *testing.T) {
	ctx := context.Background()
	ws, err := InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ws.Close() })
	db := ws.DB()

	content := func(label string) []byte {
		return []byte(`{"title":"` + label + `","summary":"See card:abc and ship it.\n\n` + strings.Repeat("body line\n", 40) + `","definition_of_done":["ship"]}`)
	}
	type revision struct {
		artifactID, revisionID string
		content                []byte
		bad                    bool
	}
	revisions := []revision{
		{artifactID: "a-good-1", revisionID: "rev-good-1", content: content("one")},
		{artifactID: "b-bad", revisionID: "rev-bad", content: content("bad"), bad: true},
		{artifactID: "c-good-2", revisionID: "rev-good-2", content: content("two")},
	}
	manifests := make(map[string]string, len(revisions))
	for i, rev := range revisions {
		complete := string(resourceaccess.ContentReferenceAtomsJSON(string(rev.content), "structured"))
		manifests[rev.artifactID] = complete
		if _, err := db.ExecContext(ctx, `INSERT INTO artifacts(id, kind, created_at, created_by, content_type, content_hash, refs_json, metadata_json, content_refs_json)
			VALUES(?, 'card', '2026-01-01T00:00:00Z', 'owner', 'structured', ?, '[]', '{}', ?)`,
			rev.artifactID, cardcontent.Hash(rev.content), complete); err != nil {
			t.Fatalf("insert artifact %s: %v", rev.artifactID, err)
		}
		encoding, codec, base, payload := cardcontent.EncodingSnapshot, "", "", []byte(nil)
		if rev.bad {
			// An orphaned delta: its base revision does not exist, so loading
			// the content fails while the row itself is well formed.
			encoding, codec, base, payload = cardcontent.EncodingDelta, cardcontent.CodecZstd, "missing-revision", []byte("not a delta")
		} else {
			codec, payload, err = cardcontent.EncodeSnapshot(rev.content)
			if err != nil {
				t.Fatal(err)
			}
		}
		if _, err := db.ExecContext(ctx, `INSERT INTO card_revisions(revision_id, card_id, revision_number, artifact_id, created_at, created_by,
			content_encoding, content_codec, content_base_revision_id, content_hash, content_bytes, content_payload)
			VALUES(?, 'card-1', ?, ?, '2026-01-01T00:00:00Z', 'owner', ?, ?, ?, ?, ?, ?)`,
			rev.revisionID, i+1, rev.artifactID, encoding, codec, base, cardcontent.Hash(rev.content), len(rev.content), payload); err != nil {
			t.Fatalf("insert revision %s: %v", rev.revisionID, err)
		}
	}

	for {
		done, err := ws.MaintainCardManifestBatch(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if done {
			break
		}
	}

	manifest := func(artifactID string) string {
		var got string
		if err := db.QueryRowContext(ctx, `SELECT content_refs_json FROM artifacts WHERE id=?`, artifactID).Scan(&got); err != nil {
			t.Fatal(err)
		}
		return got
	}
	for _, rev := range revisions {
		got := manifest(rev.artifactID)
		if rev.bad {
			if got != manifests[rev.artifactID] {
				t.Fatalf("unloadable revision manifest changed: %d -> %d bytes", len(manifests[rev.artifactID]), len(got))
			}
			continue
		}
		want := resourceaccess.WithoutProse(manifests[rev.artifactID])
		if got != want {
			t.Fatalf("good revision %s manifest = %d bytes, want pruned %d bytes", rev.artifactID, len(got), len(want))
		}
	}

	var cursor string
	var done int
	if err := db.QueryRowContext(ctx, `SELECT cursor,done FROM `+cardManifestJobTable+` WHERE singleton=1`).Scan(&cursor, &done); err != nil {
		t.Fatal(err)
	}
	if done != 1 {
		t.Fatalf("job done = %d, want 1", done)
	}
	if cursor != "c-good-2" {
		t.Fatalf("cursor = %q, want it advanced past the unloadable row to c-good-2", cursor)
	}
}
