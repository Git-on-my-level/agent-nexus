package storage_test

import (
	"context"
	"testing"

	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/storage"
)

func TestInboxLifecycleMigrationBackfillsAndReplays(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	w, err := storage.InitializeWorkspace(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	s := primitives.NewTestStore(w.DB(), w.Layout().ArtifactContentDir)
	thread, err := s.CreateThread(ctx, "owner", map[string]any{"title": "Legacy"})
	if err != nil {
		t.Fatal(err)
	}
	id := thread.Thread["id"].(string)
	if err = s.ReplaceDerivedInboxItems(ctx, id, []primitives.DerivedInboxItem{{ID: "legacy", ThreadID: id, Category: "agent_wake", TriggerAt: "2026-10-07T00:00:00Z", Data: map[string]any{"kind": "agent_wake", "body": "Keep", "refs": []any{"thread:" + id}}}}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ArchiveThread(ctx, "owner", id); err != nil {
		t.Fatal(err)
	}
	for replay := 0; replay < 2; replay++ {
		for _, query := range []string{
			`UPDATE derived_inbox_items SET lifecycle_hidden=0`,
			`DELETE FROM inbox_lifecycle_refs`,
			`DELETE FROM inbox_hidden_subject_refs`,
			`DELETE FROM schema_migrations WHERE version=67`,
		} {
			if _, err = w.DB().Exec(query); err != nil {
				t.Fatal(err)
			}
		}
		if err = w.Close(); err != nil {
			t.Fatal(err)
		}
		w, err = storage.InitializeWorkspace(ctx, root)
		if err != nil {
			t.Fatal(err)
		}
		var hidden int
		var body string
		if err = w.DB().QueryRow(`SELECT lifecycle_hidden,json_extract(data_json,'$.body') FROM derived_inbox_items WHERE id='legacy'`).Scan(&hidden, &body); err != nil || hidden != 1 || body != "Keep" {
			t.Fatalf("replay %d: hidden=%d body=%q err=%v", replay, hidden, body, err)
		}
		if _, err = w.DB().Exec(`UPDATE threads SET archived_at=NULL WHERE id=?`, id); err != nil {
			t.Fatal(err)
		}
		if err = w.DB().QueryRow(`SELECT lifecycle_hidden FROM derived_inbox_items WHERE id='legacy'`).Scan(&hidden); err != nil || hidden != 0 {
			t.Fatalf("restored after replay %d: hidden=%d err=%v", replay, hidden, err)
		}
		if _, err = w.DB().Exec(`UPDATE threads SET archived_at='now' WHERE id=?`, id); err != nil {
			t.Fatal(err)
		}
	}
	w.Close()
}
