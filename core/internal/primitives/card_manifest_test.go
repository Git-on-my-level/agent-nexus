package primitives

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"agent-nexus-core/internal/cardcontent"
	"agent-nexus-core/internal/resourceaccess"
)

type cardManifestFixture struct {
	ctx              context.Context
	ws               workspaceDB
	store            *Store
	cardID           string
	revisionID       string
	artifactID       string
	contentHash      string
	content          []byte
	completeManifest string
	storedManifest   string
}

// workspaceDB is the small part of storage.Workspace the manifest tests need.
type workspaceDB interface {
	DB() *sql.DB
	MaintainCardManifestBatch(context.Context) (bool, error)
}

func newCardManifestFixture(t *testing.T, summary string) cardManifestFixture {
	t.Helper()
	ctx, ws, store, boardID := newCardRevisionTestStore(t)
	added, err := store.AddBoardCard(ctx, "actor-1", boardID, AddBoardCardInput{Title: "Manifest", Body: summary})
	if err != nil {
		t.Fatal(err)
	}
	cardID := added.Card["id"].(string)
	var revisionID, artifactID, contentHash string
	var manifest sql.NullString
	if err := ws.DB().QueryRowContext(CanonicalMaintenanceContext(ctx), `SELECT cr.revision_id, cr.artifact_id, a.content_hash, a.content_refs_json
		  FROM card_revisions cr JOIN artifacts a ON a.id=cr.artifact_id WHERE cr.card_id=?`, cardID).Scan(&revisionID, &artifactID, &contentHash, &manifest); err != nil {
		t.Fatal(err)
	}
	contents, err := cardcontent.LoadRevisionContents(ctx, ws.DB(), []string{revisionID})
	if err != nil {
		t.Fatal(err)
	}
	f := cardManifestFixture{ctx: ctx, ws: ws, store: store, cardID: cardID, revisionID: revisionID, artifactID: artifactID, contentHash: contentHash, content: contents[revisionID], storedManifest: manifest.String}
	f.completeManifest = string(resourceaccess.ContentReferenceAtomsJSON(string(f.content), "structured"))
	return f
}

func (f cardManifestFixture) edges(t *testing.T) map[string]bool {
	t.Helper()
	rows, err := f.ws.DB().QueryContext(CanonicalMaintenanceContext(f.ctx), `SELECT target_ref FROM resource_access_edges WHERE source_kind='artifact' AND source_id=?`, f.artifactID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var target string
		if err := rows.Scan(&target); err != nil {
			t.Fatal(err)
		}
		out[target] = true
	}
	return out
}

func (f cardManifestFixture) manifest(t *testing.T) string {
	t.Helper()
	var manifest string
	if err := f.ws.DB().QueryRowContext(CanonicalMaintenanceContext(f.ctx), `SELECT content_refs_json FROM artifacts WHERE id=?`, f.artifactID).Scan(&manifest); err != nil {
		t.Fatal(err)
	}
	return manifest
}

func (f cardManifestFixture) assertContentUnchanged(t *testing.T) {
	t.Helper()
	contents, err := cardcontent.LoadRevisionContents(f.ctx, f.ws.DB(), []string{f.revisionID})
	if err != nil {
		t.Fatal(err)
	}
	if got := contents[f.revisionID]; string(got) != string(f.content) {
		t.Fatalf("card revision content changed: %d -> %d bytes", len(f.content), len(got))
	}
	if cardcontent.Hash(f.content) != f.contentHash {
		t.Fatal("fixture content hash does not match the stored artifact")
	}
}

func TestCardRevisionWritePrunesProseButKeepsRawEdges(t *testing.T) {
	summary := strings.TrimSpace("Ship card:abc and document:legacy id with spaces. See https://example.test/x.\n\n" + strings.Repeat("detail line for the release plan\n", 200))
	f := newCardManifestFixture(t, summary)

	if strings.Contains(f.storedManifest, summary) || strings.Contains(f.storedManifest, "$anx-ref-text$") {
		t.Fatalf("stored manifest still contains the prose body: %d bytes", len(f.storedManifest))
	}
	for _, keep := range []string{"card:abc", "document:legacy", "https://example.test/x"} {
		if !strings.Contains(f.storedManifest, keep) {
			t.Errorf("stored manifest lost reference %q", keep)
		}
	}
	edges := f.edges(t)
	if len(edges) == 0 {
		t.Fatal("write path created no raw access edges")
	}
	for _, want := range []string{summary, "$anx-ref-text$" + summary, "card:abc", "document:legacy"} {
		if !edges[want] {
			t.Errorf("raw access edges lost %q", want)
		}
	}
	f.assertContentUnchanged(t)
}

func TestCardRevisionProseEdgeStillResolvesFutureIdentity(t *testing.T) {
	summary := "See document:legacy id with spaces for context.\n\n" + strings.Repeat("more prose\n", 50)
	f := newCardManifestFixture(t, summary)
	if _, err := f.ws.DB().ExecContext(CanonicalMaintenanceContext(f.ctx), `INSERT INTO resource_access_identities(origin,origin_id,kind,resource_id,ref,bucket)
		VALUES('test','future-doc','document','legacy id with spaces','legacy id with spaces','document:'||anx_resource_mention_bucket(CAST('legacy id with spaces' AS BLOB)))`); err != nil {
		t.Fatal(err)
	}
	var identityID int64
	if err := f.ws.DB().QueryRowContext(CanonicalMaintenanceContext(f.ctx), `SELECT identity_id FROM resource_access_identities WHERE origin='test' AND origin_id='future-doc'`).Scan(&identityID); err != nil {
		t.Fatal(err)
	}
	var mentions int
	if err := f.ws.DB().QueryRowContext(CanonicalMaintenanceContext(f.ctx), `SELECT COUNT(*) FROM resource_access_mentions WHERE source_kind='artifact' AND source_id=? AND identity_id=?`, f.artifactID, identityID).Scan(&mentions); err != nil {
		t.Fatal(err)
	}
	if mentions == 0 {
		t.Fatal("pruned manifest lost the resolved prose reference to a future identity")
	}
}

func TestCardRevisionManifestGenuineUpdateRebuildsEdges(t *testing.T) {
	summary := strings.TrimSpace("See card:abc and ship it.\n\n" + strings.Repeat("body\n", 100))
	f := newCardManifestFixture(t, summary)
	before := f.edges(t)
	if !before[summary] {
		t.Fatal("fixture raw edges missing the full prose target")
	}
	if _, err := f.ws.DB().ExecContext(CanonicalMaintenanceContext(f.ctx), `UPDATE artifacts SET content_refs_json='["card:replacement"]' WHERE id=?`, f.artifactID); err != nil {
		t.Fatal(err)
	}
	after := f.edges(t)
	if !after["card:replacement"] || after[summary] || after["$anx-ref-text$"+summary] {
		t.Fatalf("a genuine manifest update did not rebuild the raw edges: %v", after)
	}
	f.assertContentUnchanged(t)
}

func TestCardRevisionManifestBackfillResumesAndSkipsChangedManifests(t *testing.T) {
	summary := strings.TrimSpace("See card:abc and ship it.\n\n" + strings.Repeat("body\n", 100))
	f := newCardManifestFixture(t, summary)
	before := f.edges(t)
	// Put the artifact back into the pre-142 shape, as a legacy workspace has it.
	if _, err := f.ws.DB().ExecContext(CanonicalMaintenanceContext(f.ctx), `UPDATE artifacts SET content_refs_json=? WHERE id=?`, f.completeManifest, f.artifactID); err != nil {
		t.Fatal(err)
	}
	if got := f.manifest(t); got != f.completeManifest {
		t.Fatal("fixture did not restore the complete manifest")
	}
	// A resumed cursor past this artifact must not prune it.
	if _, err := f.ws.DB().ExecContext(CanonicalMaintenanceContext(f.ctx), `UPDATE resource_access_card_manifest_job SET cursor=?,done=0 WHERE singleton=1`, f.artifactID); err != nil {
		t.Fatal(err)
	}
	runCardManifestMaintenanceForTest(t, f.ws)
	if got := f.manifest(t); got != f.completeManifest {
		t.Fatal("backfill pruned an artifact before its resumed cursor")
	}
	// Rewinding the cursor prunes it, without changing edges or content.
	if _, err := f.ws.DB().ExecContext(CanonicalMaintenanceContext(f.ctx), `UPDATE resource_access_card_manifest_job SET cursor='',done=0 WHERE singleton=1`); err != nil {
		t.Fatal(err)
	}
	runCardManifestMaintenanceForTest(t, f.ws)
	if got := f.manifest(t); strings.Contains(got, summary) || got == f.completeManifest {
		t.Fatalf("backfill did not prune the manifest: %d bytes", len(got))
	}
	if after := f.edges(t); !sameTargets(before, after) {
		t.Fatalf("backfill changed raw access edges: before=%d after=%d", len(before), len(after))
	}
	f.assertContentUnchanged(t)

	// A manifest changed by another rule is left alone.
	if _, err := f.ws.DB().ExecContext(CanonicalMaintenanceContext(f.ctx), `UPDATE artifacts SET content_refs_json='["card:custom"]' WHERE id=?`, f.artifactID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.ws.DB().ExecContext(CanonicalMaintenanceContext(f.ctx), `UPDATE resource_access_card_manifest_job SET cursor='',done=0 WHERE singleton=1`); err != nil {
		t.Fatal(err)
	}
	runCardManifestMaintenanceForTest(t, f.ws)
	if got := f.manifest(t); got != `["card:custom"]` {
		t.Fatalf("backfill overwrote a manifest another rule owns: %s", got)
	}
}

func runCardManifestMaintenanceForTest(t *testing.T, ws workspaceDB) {
	t.Helper()
	for {
		done, err := ws.MaintainCardManifestBatch(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if done {
			return
		}
	}
}

func sameTargets(a, b map[string]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for target := range a {
		if !b[target] {
			return false
		}
	}
	return true
}
