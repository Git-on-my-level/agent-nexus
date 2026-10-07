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
			`UPDATE derived_inbox_items SET lifecycle_hidden=0,lifecycle_ready=0`,
			`UPDATE inbox_lifecycle_job SET phase=0,cursor='',owners_ready=0,done=0`,
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
		// Reopening performs no row backfill; it resumes from the durable job.
		var ready int
		if err = w.DB().QueryRow(`SELECT lifecycle_ready FROM derived_inbox_items WHERE id='legacy'`).Scan(&ready); err != nil || ready != 0 {
			t.Fatalf("startup backfilled row: ready=%d err=%v", ready, err)
		}
		for pass := 0; pass < 20; pass++ {
			done, err := w.MaintainInboxLifecycleBatch(ctx, 2)
			if err != nil {
				t.Fatal(err)
			}
			if done {
				break
			}
			if pass == 19 {
				t.Fatal("maintenance did not complete")
			}
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

func TestInboxLifecycleAppliedPreviewGetsReadinessMetadata(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	w, err := storage.InitializeWorkspace(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	// Keep67 applied while restoring the previous preview's column shape.
	rows, err := w.DB().Query(`SELECT name FROM sqlite_master WHERE type='trigger' AND name LIKE 'inbox_lifecycle_%'`)
	if err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for rows.Next() {
		var name string
		rows.Scan(&name)
		names = append(names, name)
	}
	rows.Close()
	for _, name := range names {
		if _, err = w.DB().Exec(`DROP TRIGGER ` + name); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = w.DB().Exec(`DROP TABLE inbox_lifecycle_job`); err != nil {
		t.Fatal(err)
	}
	if _, err = w.DB().Exec(`ALTER TABLE derived_inbox_items DROP COLUMN lifecycle_ready`); err != nil {
		t.Fatal(err)
	}
	w.Close()
	w, err = storage.InitializeWorkspace(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	var count int
	if err = w.DB().QueryRow(`SELECT COUNT(*) FROM pragma_table_info('derived_inbox_items') WHERE name='lifecycle_ready'`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("preview not reconciled: count=%d err=%v", count, err)
	}
	if err = w.DB().QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE version=67`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("migration history changed: count=%d err=%v", count, err)
	}
}
