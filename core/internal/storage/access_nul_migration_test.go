package storage_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	"agent-nexus-core/internal/blob"
	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/resourceaccess"
	"agent-nexus-core/internal/storage"
)

func TestResourceAccessMigrationRepairsNULRows(t *testing.T) {
	ctx, root := context.Background(), t.TempDir()
	ws, err := storage.InitializeWorkspace(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	s := primitives.NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	private, _, err := s.CreateDocument(ctx, "owner", map[string]any{"id": "[]", "title": "private"}, "source", "text", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.PatchThread(ctx, "owner", private["thread_id"].(string), map[string]any{"pm_actor_id": "owner"}, nil); err != nil {
		t.Fatal(err)
	}
	textDoc, _, err := s.CreateDocument(ctx, "owner", map[string]any{"title": "scalar legacy"}, "public", "text", nil)
	if err != nil {
		t.Fatal(err)
	}
	blobDoc, _, err := s.CreateDocument(ctx, "owner", map[string]any{"title": "blob legacy"}, map[string]any{"text": "public"}, "structured", nil)
	if err != nil {
		t.Fatal(err)
	}
	content := []byte(`{"text":"prose\u0000document:[]"}`)
	hash := sha256.Sum256(content)
	digest := hex.EncodeToString(hash[:])
	staged, err := blob.NewFilesystemBackend(ws.Layout().ArtifactContentDir).Write(ctx, digest, content)
	if err != nil {
		t.Fatal(err)
	}
	if err = staged.Promote(); err != nil {
		t.Fatal(err)
	}
	defer staged.Cleanup()
	if _, err = ws.DB().Exec(`UPDATE artifacts SET content_hash=?,content_refs_json='[]' WHERE id=(SELECT artifact_id FROM document_revisions WHERE document_id=? LIMIT 1)`, digest, blobDoc["id"]); err != nil {
		t.Fatal(err)
	}
	if _, err = ws.DB().Exec(`UPDATE documents SET summary=? WHERE id=?`, "LegacyNUL prose\x00document:[]", textDoc["id"]); err != nil {
		t.Fatal(err)
	}
	if _, err = ws.DB().Exec(`DELETE FROM resource_access_edges WHERE source_kind='document' AND source_id=?`, textDoc["id"]); err != nil {
		t.Fatal(err)
	}
	// Retain an actual pre-60 TEXT trigger to exercise upgrade ordering.
	for _, q := range []string{
		`DROP TRIGGER access_artifacts_update`,
		`CREATE TRIGGER access_artifacts_update AFTER UPDATE OF content_refs_json ON artifacts BEGIN SELECT anx_resource_refs(NEW.content_refs_json); END`,
		`INSERT INTO series_definitions(name,adapter,unit,kind) VALUES('legacy-nul','fixture','state','state')`,
		`INSERT INTO series_labels VALUES('legacy-nul','{}')`,
		`INSERT INTO series_points(series,labels,ts,state,received_day) VALUES('legacy-nul','{}',1,'public',0)`,
		`DELETE FROM schema_migrations WHERE version>=60`,
	} {
		if _, err = ws.DB().Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = ws.DB().Exec(`INSERT INTO resource_access_series_refs VALUES('legacy-nul','{}',?)`, "prose\x00document:[]"); err != nil {
		t.Fatal(err)
	}
	if err = ws.Close(); err != nil {
		t.Fatal(err)
	}
	ws, err = storage.InitializeWorkspace(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	s = primitives.NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	indexLegacyFixtureContent(t, ctx, s)
	for _, actor := range []string{"owner", "stranger", "unauthorized-agent"} {
		scope := primitives.WithAccessScope(ctx, primitives.AccessScope{ActorID: actor})
		for _, doc := range []map[string]any{textDoc, blobDoc} {
			if got := s.CanAccessResource(scope, "document", doc["id"].(string)); got != (actor == "owner") {
				t.Fatalf("%s legacy document access=%v", actor, got)
			}
		}
		var count int
		if err = resourceaccess.NewDB(ws.DB()).QueryRowContext(scope, `SELECT count(*) FROM series_points WHERE series='legacy-nul'`).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if (count == 1) != (actor == "owner") {
			t.Fatalf("%s legacy series count=%d", actor, count)
		}
	}
	var stored []byte
	if err = ws.DB().QueryRow(`SELECT CAST(summary AS BLOB) FROM documents WHERE id=?`, textDoc["id"]).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(stored), "\x00") {
		t.Fatal("migration changed canonical text")
	}
	var unknown int
	if err = ws.DB().QueryRow(`SELECT count(*) FROM resource_access_series_unknown`).Scan(&unknown); err != nil || unknown != 0 {
		t.Fatalf("60 marked reconstructable streams unknown: %d %v", unknown, err)
	}
}
