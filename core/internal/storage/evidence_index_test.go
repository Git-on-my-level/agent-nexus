package storage_test

import (
	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/storage"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestEvidenceIndexMigrationBackfillsAndObservationUpdates(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	root := t.TempDir()
	ws, err := storage.InitializeWorkspace(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	s := primitives.NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	card, err := s.CreateWork(ctx, "actor", "", map[string]any{"title": "Legacy", "source": map[string]any{"authority": "tracker", "connection_id": "one", "native_id": "opaque", "identifier_aliases": []string{"LEGACY-42"}}, "source_refs": []any{map[string]any{"authority": "other", "connection_id": "two", "native_id": "evidence", "identifier_aliases": []string{"RELATED-42"}, "status": "done"}}})
	if err != nil {
		t.Fatal(err)
	}
	bad, err := s.CreateWork(ctx, "actor", "", map[string]any{"title": "Legacy extension", "source": map[string]any{"authority": "tracker", "connection_id": "one", "native_id": "bad-alias-extension"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = ws.DB().Exec(`UPDATE work_metadata SET metadata_json=json_set(metadata_json,'$.source.identifier_aliases','old-extension') WHERE card_id=?`, bad["id"]); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{`DROP TRIGGER work_evidence_metadata_insert`, `DROP TRIGGER work_evidence_metadata_update`, `DROP TRIGGER work_evidence_metadata_delete`, `DROP VIEW work_evidence_keys`, `DROP VIEW work_evidence_entries`, `DROP TABLE work_evidence_index`, `DELETE FROM schema_migrations WHERE version=65`} {
		if _, err = ws.DB().Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	ws.Close()
	ws, err = storage.InitializeWorkspace(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	s = primitives.NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	refs := []string{"LEGACY-42", "RELATED-42", "OBSERVED-42"}
	got, err := s.ResolveRefs(ctx, refs, nil, time.Now(), 0)
	if err != nil || !got[0].Resolvable || !got[1].Resolvable || got[2].Resolvable {
		t.Fatalf("backfill=%+v %v", got, err)
	}
	_, err = s.SubmitWorkObservation(ctx, "actor", card["id"].(string), map[string]any{"idempotency_key": "observe", "reader_id": "tracker", "reader_revision": "v1", "observed_at": time.Now().UTC().Format(time.RFC3339Nano), "status": "reported", "facts": map[string]any{"title": "Observed", "phase": "done", "identifier_aliases": []string{"OBSERVED-42"}}, "evidence": []any{map[string]any{"url": "https://source.test/42"}}})
	if err != nil {
		t.Fatal(err)
	}
	got, err = s.ResolveRefs(ctx, refs, nil, time.Now(), 0)
	if err != nil || !got[2].Resolvable || got[2].Status != "done" || got[2].Title != "Observed" {
		t.Fatalf("observation=%+v %v", got, err)
	}
}

// Main can contain legacy extension arrays beyond the new write caps. Migration
// 65 bounds their projection while preserving canonical evidence bytes.
func TestEvidenceIndex65BoundsLegacyAliasBackfill(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	root := t.TempDir()
	ws, err := storage.InitializeWorkspace(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	s := primitives.NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	card, err := s.CreateWork(ctx, "actor", "", map[string]any{"title": "Legacy amplification"})
	if err != nil {
		t.Fatal(err)
	}
	aliases := []string{}
	for i := 0; i < 60; i++ {
		aliases = append(aliases, fmt.Sprintf("legacy-%d", i))
	}
	evidence := map[string]any{"authority": "generic", "connection_id": "connection", "native_id": "item", "aliases": aliases, "status": "done", "title": strings.Repeat("payload", 1000)}
	raw, _ := json.Marshal([]any{evidence})
	if _, err = ws.DB().Exec(`UPDATE work_metadata SET metadata_json=json_set(metadata_json,'$.source_refs',json(?)) WHERE card_id=?`, string(raw), card["id"]); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{`DROP TRIGGER work_evidence_metadata_insert`, `DROP TRIGGER work_evidence_metadata_update`, `DROP TRIGGER work_evidence_metadata_delete`, `DROP VIEW work_evidence_keys`, `DROP VIEW work_evidence_entries`, `DROP TABLE work_evidence_index`, `DROP TABLE work_evidence_records`, `DELETE FROM schema_migrations WHERE version=65`} {
		if _, err = ws.DB().Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	ws.Close()
	for open := 0; open < 2; open++ {
		ws, err = storage.InitializeWorkspace(ctx, root)
		if err != nil {
			t.Fatal(err)
		}
		var records, keys, total int
		if err = ws.DB().QueryRow(`SELECT count(*),sum(length(evidence_json)) FROM work_evidence_records WHERE card_id=?`, card["id"]).Scan(&records, &total); err != nil {
			t.Fatal(err)
		}
		if err = ws.DB().QueryRow(`SELECT count(*) FROM work_evidence_index WHERE card_id=?`, card["id"]).Scan(&keys); err != nil {
			t.Fatal(err)
		}
		if records != 1 || keys != 52 || total > 9000 {
			t.Fatalf("legacy amplification: records=%d keys=%d bytes=%d", records, keys, total)
		}
		s = primitives.NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
		got, err := s.ResolveRefs(ctx, []string{"legacy-0", "legacy-59"}, nil, time.Now(), 0)
		if err != nil || !got[0].Resolvable || got[1].Resolvable {
			t.Fatalf("legacy bounds: %+v %v", got, err)
		}
		// A v0.12.10-style metadata UPDATE still maintains the rebuilt index.
		if _, err = ws.DB().Exec(`UPDATE work_metadata SET metadata_json=json_set(metadata_json,'$.next_action','legacy client patch') WHERE card_id=?`, card["id"]); err != nil {
			t.Fatal(err)
		}
		ws.Close()
	}
}
