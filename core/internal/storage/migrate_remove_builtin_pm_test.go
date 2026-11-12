package storage_test

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"agent-nexus-core/internal/backupage"
	"agent-nexus-core/internal/storage"

	"filippo.io/age"
	"github.com/klauspost/compress/zstd"
	_ "modernc.org/sqlite"
)

func TestRemoveBuiltinPMArchivesThenDropsProductData(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	ws, err := storage.InitializeWorkspace(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	seedLegacyPMWorkspace(t, ws.DB())
	resetPMArchiveMigration(t, ws.DB())
	if err := ws.Close(); err != nil {
		t.Fatal(err)
	}

	ops, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	rec, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("ANX_BACKUP_AGE_RECIPIENTS", ops.Recipient().String())
	t.Setenv("ANX_VAULT_REC_RECIPIENT", rec.Recipient().String())

	ws, err = storage.InitializeWorkspace(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()

	assertMigrationApplied(t, ws.DB(), 138)
	for _, kind := range []string{"decision", "action", "cursor"} {
		var count int
		if err = ws.DB().QueryRow(`SELECT COUNT(*) FROM pm_records WHERE kind=?`, kind).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Fatalf("expected kept %s row, got %d", kind, count)
		}
	}
	var productCount int
	if err = ws.DB().QueryRow(`SELECT COUNT(*) FROM pm_records WHERE kind IN ('turn','conversation','binding','delivery','delivery_retry')`).Scan(&productCount); err != nil {
		t.Fatal(err)
	}
	if productCount != 0 {
		t.Fatalf("expected product kinds removed, got %d", productCount)
	}
	for _, table := range []string{"pm_presence", "pm_registration", "pm_onboarding_backfill"} {
		if tableExists(t, ws.DB(), table) {
			t.Fatalf("expected %s dropped", table)
		}
	}
	for _, index := range []string{"pm_records_turn_outbox", "pm_records_health_expired", "pm_records_health_success"} {
		if indexExists(t, ws.DB(), index) {
			t.Fatalf("expected product index %s dropped", index)
		}
	}
	if !indexExists(t, ws.DB(), "pm_records_status") {
		t.Fatal("expected Decide index to survive")
	}

	var receipt struct {
		path, sha, fingerprint string
		size                   int64
		created                string
	}
	if err = ws.DB().QueryRow(`SELECT archive_path,sha256,size_bytes,recipients_fingerprint,created_at FROM pm_archive_receipts`).
		Scan(&receipt.path, &receipt.sha, &receipt.size, &receipt.fingerprint, &receipt.created); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(receipt.path)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.size <= 0 || info.Size() != receipt.size {
		t.Fatalf("unexpected archive size %d/%d", receipt.size, info.Size())
	}
	if len(receipt.sha) != 64 || receipt.fingerprint == "" || receipt.created == "" {
		t.Fatal("receipt is incomplete")
	}
	ciphertext, err := os.ReadFile(receipt.path)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(ciphertext)
	if receipt.sha != hex.EncodeToString(digest[:]) {
		t.Fatal("receipt sha256 does not match the archive")
	}

	assertPMWatcherResolved(t, ws.DB(), true)
	if got := threadArchived(t, ws.DB(), "PM health"); !got {
		t.Fatal("expected PM health thread archived")
	}
	if got := threadArchived(t, ws.DB(), "Keep this thread"); got {
		t.Fatal("unrelated thread was archived")
	}
	if got := openAskCount(t, ws.DB(), "Other watcher"); got != 1 {
		t.Fatalf("unrelated ask changed: %d", got)
	}

	plaintext := decryptArchive(t, ciphertext, rec)
	if !bytes.Contains(plaintext, []byte(`"schema_version": 137`)) {
		t.Fatal("manifest does not record schema version 137")
	}
	if !bytes.Contains(plaintext, []byte(`"turn"`)) || !bytes.Contains(plaintext, []byte(`"decision"`)) {
		t.Fatal("archive does not contain both product and kept rows")
	}
	archiveNames := tarNames(t, plaintext)
	for _, name := range []string{"manifest.json", "pm_records.jsonl", "pm_presence.jsonl"} {
		if !archiveNames[name] {
			t.Fatalf("archive missing %s", name)
		}
	}

	// A second application after a simulated ledger reset is a no-op success and
	// must not write a second archive.
	if _, err = ws.DB().Exec(`DELETE FROM schema_migrations WHERE version=138`); err != nil {
		t.Fatal(err)
	}
	if err = ws.Close(); err != nil {
		t.Fatal(err)
	}
	ws, err = storage.InitializeWorkspace(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	assertMigrationApplied(t, ws.DB(), 138)
	var receipts, archives, withdrawals int
	if err = ws.DB().QueryRow(`SELECT COUNT(*) FROM pm_archive_receipts`).Scan(&receipts); err != nil {
		t.Fatal(err)
	}
	matches, err := filepath.Glob(filepath.Join(root, "state.pm-archive-138-*.tar.zst.age"))
	if err != nil {
		t.Fatal(err)
	}
	archives = len(matches)
	if err = ws.DB().QueryRow(`SELECT COUNT(*) FROM events WHERE id LIKE 'pm-archive-138-withdraw-%'`).Scan(&withdrawals); err != nil {
		t.Fatal(err)
	}
	if receipts != 1 || archives != 1 {
		t.Fatalf("idempotent apply wrote another archive: receipts=%d archives=%d", receipts, archives)
	}
	if withdrawals != 1 {
		t.Fatalf("idempotent apply wrote another withdrawal: %d", withdrawals)
	}
}

func TestRemoveBuiltinPMWithdrawsEveryOpenWatcherAsk(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	ws, err := storage.InitializeWorkspace(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	db := ws.DB()
	insertAsk(t, db, "ask-pm-a", "thread-pm-a", `{"payload":{"title":"Restore the workspace PM?","watcher_health_kind":"pm","requester_actor_id":"owner","subject_ref":"decide"},"title":"Restore the workspace PM?"}`)
	insertAsk(t, db, "ask-pm-b", "", `{"payload":{"title":"PM watcher needs attention","watcher_health_kind":"pm","requester_actor_id":"owner","subject_ref":"card:work"},"title":"PM watcher needs attention"}`)
	insertAsk(t, db, "ask-other", "thread-keep", `{"payload":{"title":"Other watcher","requester_actor_id":"owner","subject_ref":"card:other"},"title":"Other watcher"}`)
	for _, ask := range []struct{ id, card, requester, thread string }{
		{"ask-pm-a", "decide", "owner", "thread-pm-a"},
		{"ask-pm-b", "card:work", "owner", ""},
		{"ask-other", "card:other", "owner", "thread-keep"},
	} {
		if _, err := db.Exec(`INSERT OR IGNORE INTO ask_subjects(ask_id,card_id,requester,thread_id,open) VALUES(?,?,?,?,1)`, ask.id, ask.card, ask.requester, ask.thread); err != nil {
			t.Fatal(err)
		}
	}
	resetPMArchiveMigration(t, db)
	if err := ws.Close(); err != nil {
		t.Fatal(err)
	}

	ws, err = storage.InitializeWorkspace(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	db = ws.DB()
	assertMigrationApplied(t, db, 138)

	rows, err := db.Query(`SELECT id,handle FROM events WHERE type='human_attention_withdrawn' AND id LIKE 'pm-archive-138-withdraw-%' ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	handles := map[string]string{}
	for rows.Next() {
		var id, handle string
		if err := rows.Scan(&id, &handle); err != nil {
			t.Fatal(err)
		}
		if handle == "" {
			t.Fatalf("withdrawal event %s has an empty handle", id)
		}
		handles[id] = handle
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(handles) != 2 {
		t.Fatalf("expected two withdrawal events, got %d (%v)", len(handles), handles)
	}
	seenHandles := map[string]string{}
	for id, handle := range handles {
		if other, ok := seenHandles[handle]; ok {
			t.Fatalf("withdrawal events %s and %s share handle %q", id, other, handle)
		}
		seenHandles[handle] = id
	}

	for _, askID := range []string{"ask-pm-a", "ask-pm-b"} {
		var resolutionEvent string
		if err := db.QueryRow(`SELECT resolution_event_id FROM human_attention_request_resolutions WHERE request_event_id=?`, askID).Scan(&resolutionEvent); err != nil {
			t.Fatalf("PM ask %s was not resolved: %v", askID, err)
		}
		var eventExists int
		if err := db.QueryRow(`SELECT COUNT(*) FROM events WHERE id=?`, resolutionEvent).Scan(&eventExists); err != nil {
			t.Fatal(err)
		}
		if eventExists != 1 {
			t.Fatalf("PM ask %s resolution points at missing event %q", askID, resolutionEvent)
		}
		var open int
		if err := db.QueryRow(`SELECT open FROM ask_subjects WHERE ask_id=?`, askID).Scan(&open); err != nil {
			t.Fatal(err)
		}
		if open != 0 {
			t.Fatalf("PM ask %s still open", askID)
		}
	}
	if got := openAskCount(t, db, "Restore the workspace PM?"); got != 0 {
		t.Fatalf("first PM ask still open: %d", got)
	}
	if got := openAskCount(t, db, "PM watcher needs attention"); got != 0 {
		t.Fatalf("second PM ask still open: %d", got)
	}
	if got := openAskCount(t, db, "Other watcher"); got != 1 {
		t.Fatalf("unrelated ask changed: %d", got)
	}
	var otherOpen int
	if err := db.QueryRow(`SELECT open FROM ask_subjects WHERE ask_id='ask-other'`).Scan(&otherOpen); err != nil {
		t.Fatal(err)
	}
	if otherOpen != 1 {
		t.Fatalf("unrelated ask_subjects row closed: %d", otherOpen)
	}
}

func TestRemoveBuiltinPMFailsClosedWithoutRecipients(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	ws, err := storage.InitializeWorkspace(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	seedLegacyPMWorkspace(t, ws.DB())
	resetPMArchiveMigration(t, ws.DB())
	if err := ws.Close(); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ANX_BACKUP_AGE_RECIPIENTS", "")
	t.Setenv("ANX_VAULT_REC_RECIPIENT", "")

	if _, err := storage.InitializeWorkspace(ctx, root); err == nil {
		t.Fatal("expected missing recipients to fail closed")
	}

	db := openWorkspaceDB(t, root)
	defer db.Close()
	assertLegacyPMDataIntact(t, db)
	var receipts int
	if err = db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='pm_archive_receipts'`).Scan(&receipts); err != nil {
		t.Fatal(err)
	}
	if receipts != 0 {
		t.Fatal("failed migration left a receipt table")
	}
	if got := openAskCount(t, db, "Restore the workspace PM?"); got != 1 {
		t.Fatalf("failed migration changed the PM watcher ask: %d", got)
	}
}

func TestRemoveBuiltinPMFailsClosedWithInvalidRecipients(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	ws, err := storage.InitializeWorkspace(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	seedLegacyPMWorkspace(t, ws.DB())
	resetPMArchiveMigration(t, ws.DB())
	if err := ws.Close(); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ANX_BACKUP_AGE_RECIPIENTS", "not-a-public-age-key")
	t.Setenv("ANX_VAULT_REC_RECIPIENT", "also-not-a-public-age-key")

	if _, err := storage.InitializeWorkspace(ctx, root); err == nil {
		t.Fatal("expected malformed recipients to fail closed")
	}
	db := openWorkspaceDB(t, root)
	defer db.Close()
	assertLegacyPMDataIntact(t, db)
}

func TestRemoveBuiltinPMDoesNotDeleteCurrentRowsUnderStaleIntactReceipt(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	ws, err := storage.InitializeWorkspace(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	seedLegacyPMWorkspace(t, ws.DB())
	resetPMArchiveMigration(t, ws.DB())
	if err := ws.Close(); err != nil {
		t.Fatal(err)
	}
	ops, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	rec, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("ANX_BACKUP_AGE_RECIPIENTS", ops.Recipient().String())
	t.Setenv("ANX_VAULT_REC_RECIPIENT", rec.Recipient().String())

	ws, err = storage.InitializeWorkspace(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	var oldPath string
	if err = ws.DB().QueryRow(`SELECT archive_path FROM pm_archive_receipts`).Scan(&oldPath); err != nil {
		t.Fatal(err)
	}
	oldArchive, err := os.ReadFile(oldPath)
	if err != nil {
		t.Fatal(err)
	}
	// Put current product rows back, including a distinctive row that the old
	// archive cannot contain, and rewind only the ledger. The intact receipt is
	// stale: it does not cover the rows the migration is about to delete.
	reseedPMProductData(t, ws.DB())
	if _, err = ws.DB().Exec(`INSERT OR REPLACE INTO pm_records VALUES('turn','turn-current-2','ws_main','pm-agent','',1,'{"status":"delivered","marker":"distinctive-current-row"}')`); err != nil {
		t.Fatal(err)
	}
	if _, err = ws.DB().Exec(`DELETE FROM schema_migrations WHERE version=138`); err != nil {
		t.Fatal(err)
	}
	if err = ws.Close(); err != nil {
		t.Fatal(err)
	}

	ws, err = storage.InitializeWorkspace(ctx, root)
	if err != nil {
		t.Fatalf("stale intact receipt should be replaced, not trusted: %v", err)
	}
	defer ws.Close()
	assertMigrationApplied(t, ws.DB(), 138)

	var newPath string
	if err = ws.DB().QueryRow(`SELECT archive_path FROM pm_archive_receipts`).Scan(&newPath); err != nil {
		t.Fatal(err)
	}
	if newPath == oldPath {
		t.Fatal("stale receipt was reused instead of replaced")
	}
	unchanged, err := os.ReadFile(oldPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(oldArchive, unchanged) {
		t.Fatal("old archive was modified while writing the replacement")
	}
	ciphertext, err := os.ReadFile(newPath)
	if err != nil {
		t.Fatal(err)
	}
	plaintext := decryptArchive(t, ciphertext, rec)
	if !bytes.Contains(plaintext, []byte("distinctive-current-row")) {
		t.Fatal("replacement archive does not cover the current product rows")
	}
	var productCount int
	if err = ws.DB().QueryRow(`SELECT COUNT(*) FROM pm_records WHERE kind IN ('turn','conversation','binding','delivery','delivery_retry')`).Scan(&productCount); err != nil {
		t.Fatal(err)
	}
	if productCount != 0 {
		t.Fatalf("expected product kinds removed after replacement archive, got %d", productCount)
	}
}

func TestRemoveBuiltinPMReplacesDamagedStaleReceiptWithNewArchive(t *testing.T) {
	cases := []struct {
		name   string
		damage func(t *testing.T, path string)
	}{
		{"corrupted", func(t *testing.T, path string) {
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			data[len(data)/2] ^= 0xff
			if err := os.WriteFile(path, data, 0o600); err != nil {
				t.Fatal(err)
			}
		}},
		{"truncated", func(t *testing.T, path string) {
			info, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Truncate(path, info.Size()/2); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			root := t.TempDir()
			ws, err := storage.InitializeWorkspace(ctx, root)
			if err != nil {
				t.Fatal(err)
			}
			seedLegacyPMWorkspace(t, ws.DB())
			resetPMArchiveMigration(t, ws.DB())
			if err := ws.Close(); err != nil {
				t.Fatal(err)
			}
			ops, err := age.GenerateX25519Identity()
			if err != nil {
				t.Fatal(err)
			}
			rec, err := age.GenerateX25519Identity()
			if err != nil {
				t.Fatal(err)
			}
			t.Setenv("ANX_BACKUP_AGE_RECIPIENTS", ops.Recipient().String())
			t.Setenv("ANX_VAULT_REC_RECIPIENT", rec.Recipient().String())

			ws, err = storage.InitializeWorkspace(ctx, root)
			if err != nil {
				t.Fatal(err)
			}
			var archivePath string
			if err = ws.DB().QueryRow(`SELECT archive_path FROM pm_archive_receipts`).Scan(&archivePath); err != nil {
				t.Fatal(err)
			}
			// Put product rows back and rewind only the ledger, leaving the
			// receipt in place to simulate a resumed migration.
			reseedPMProductData(t, ws.DB())
			if _, err = ws.DB().Exec(`DELETE FROM schema_migrations WHERE version=138`); err != nil {
				t.Fatal(err)
			}
			if err = ws.Close(); err != nil {
				t.Fatal(err)
			}
			tc.damage(t, archivePath)

			// The damaged receipt describes an old archive. Current rows exist,
			// so the migration must write and verify a fresh archive instead of
			// trusting the stale receipt.
			ws, err = storage.InitializeWorkspace(ctx, root)
			if err != nil {
				t.Fatalf("damaged stale receipt should be replaced: %v", err)
			}
			defer ws.Close()
			assertMigrationApplied(t, ws.DB(), 138)
			var newPath string
			if err = ws.DB().QueryRow(`SELECT archive_path FROM pm_archive_receipts`).Scan(&newPath); err != nil {
				t.Fatal(err)
			}
			if newPath == archivePath {
				t.Fatal("damaged stale receipt was reused instead of replaced")
			}
			ciphertext, err := os.ReadFile(newPath)
			if err != nil {
				t.Fatal(err)
			}
			plaintext := decryptArchive(t, ciphertext, rec)
			if !bytes.Contains(plaintext, []byte(`"turn"`)) {
				t.Fatal("replacement archive does not contain current product rows")
			}
		})
	}
}

func TestRemoveBuiltinPMClearsMaterializedInboxForWithdrawnAsks(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	ws, err := storage.InitializeWorkspace(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	db := ws.DB()
	seedLegacyPMWorkspace(t, db)
	for _, row := range []struct{ id, thread, source, body string }{
		{"inbox-pm", "thread-pm-health", "ask-pm", `{"kind":"ask","body":"PM watcher","subject_ref":"decide","related_refs":["event:ask-pm"]}`},
		{"inbox-other", "thread-keep", "ask-other", `{"kind":"ask","body":"Other watcher","subject_ref":"card:other","related_refs":["event:ask-other"]}`},
	} {
		if _, err := db.Exec(`INSERT INTO derived_inbox_items(id,thread_id,category,trigger_at,generated_at,data_json,source_event_id) VALUES(?,?,'ask','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z',?,?)`, row.id, row.thread, row.body, row.source); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`INSERT INTO topic_projection_refresh_status(thread_id,desired_generation,materialized_generation,updated_at) VALUES('thread-pm-health',1,1,'2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	resetPMArchiveMigration(t, db)
	if err := ws.Close(); err != nil {
		t.Fatal(err)
	}
	ops, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	rec, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("ANX_BACKUP_AGE_RECIPIENTS", ops.Recipient().String())
	t.Setenv("ANX_VAULT_REC_RECIPIENT", rec.Recipient().String())

	ws, err = storage.InitializeWorkspace(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	db = ws.DB()
	assertMigrationApplied(t, db, 138)

	var pmInbox, otherInbox int
	if err = db.QueryRow(`SELECT COUNT(*) FROM derived_inbox_items WHERE id='inbox-pm'`).Scan(&pmInbox); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow(`SELECT COUNT(*) FROM derived_inbox_items WHERE id='inbox-other'`).Scan(&otherInbox); err != nil {
		t.Fatal(err)
	}
	if pmInbox != 0 {
		t.Fatal("withdrawn PM ask still has a materialized inbox row")
	}
	if otherInbox != 1 {
		t.Fatal("unrelated inbox row was removed")
	}
	var dirty int
	if err = db.QueryRow(`SELECT COUNT(*) FROM derived_topic_dirty_queue WHERE thread_id='thread-pm-health'`).Scan(&dirty); err != nil {
		t.Fatal(err)
	}
	if dirty != 1 {
		t.Fatal("PM ask thread was not queued for topic projection refresh")
	}
	var desired, materialized int
	if err = db.QueryRow(`SELECT desired_generation,materialized_generation FROM topic_projection_refresh_status WHERE thread_id='thread-pm-health'`).Scan(&desired, &materialized); err != nil {
		t.Fatal(err)
	}
	if desired <= materialized {
		t.Fatalf("PM ask thread refresh was not bumped: desired=%d materialized=%d", desired, materialized)
	}
	if got := openAskCount(t, db, "Other watcher"); got != 1 {
		t.Fatalf("unrelated ask changed: %d", got)
	}
}

func TestRemoveBuiltinPMRejectsWithdrawalEventForDifferentRequest(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	ws, err := storage.InitializeWorkspace(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	db := ws.DB()
	seedLegacyPMWorkspace(t, db)
	eventID := "pm-archive-138-withdraw-ask-pm"
	if _, err := db.Exec(`INSERT INTO events(id,type,ts,actor_id,thread_id,refs_json,payload_json,created_at) VALUES(?,'human_attention_withdrawn','2026-01-01T00:00:00Z','system','thread-pm-health','[]',?,CURRENT_TIMESTAMP)`,
		eventID, `{"payload":{"request_event_id":"some-other-ask"}}`); err != nil {
		t.Fatal(err)
	}
	resetPMArchiveMigration(t, db)
	if err := ws.Close(); err != nil {
		t.Fatal(err)
	}
	ops, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	rec, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("ANX_BACKUP_AGE_RECIPIENTS", ops.Recipient().String())
	t.Setenv("ANX_VAULT_REC_RECIPIENT", rec.Recipient().String())

	if _, err := storage.InitializeWorkspace(ctx, root); err == nil {
		t.Fatal("expected mismatched withdrawal event to abort the migration")
	}
	db = openWorkspaceDB(t, root)
	defer db.Close()
	assertLegacyPMDataIntact(t, db)
	assertMigrationAbsent(t, db, 138)
	if got := openAskCount(t, db, "Restore the workspace PM?"); got != 1 {
		t.Fatalf("mismatched withdrawal closed the PM ask: %d", got)
	}
	var resolutions int
	if err := db.QueryRow(`SELECT COUNT(*) FROM human_attention_request_resolutions WHERE request_event_id='ask-pm'`).Scan(&resolutions); err != nil {
		t.Fatal(err)
	}
	if resolutions != 0 {
		t.Fatal("mismatched withdrawal wrote a resolution")
	}
}

func TestRemoveBuiltinPMRejectsResolutionEventConflict(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	ws, err := storage.InitializeWorkspace(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	db := ws.DB()
	seedLegacyPMWorkspace(t, db)
	eventID := "pm-archive-138-withdraw-ask-pm"
	if _, err := db.Exec(`INSERT INTO human_attention_request_resolutions(request_event_id,resolution_event_id,resolution_type,actor_id,created_at) VALUES('some-other-request',?,'withdrawn','system','2026-01-01T00:00:00Z')`, eventID); err != nil {
		t.Fatal(err)
	}
	resetPMArchiveMigration(t, db)
	if err := ws.Close(); err != nil {
		t.Fatal(err)
	}
	ops, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	rec, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("ANX_BACKUP_AGE_RECIPIENTS", ops.Recipient().String())
	t.Setenv("ANX_VAULT_REC_RECIPIENT", rec.Recipient().String())

	if _, err := storage.InitializeWorkspace(ctx, root); err == nil {
		t.Fatal("expected resolution event conflict to abort the migration")
	}
	db = openWorkspaceDB(t, root)
	defer db.Close()
	assertLegacyPMDataIntact(t, db)
	assertMigrationAbsent(t, db, 138)
	if got := openAskCount(t, db, "Restore the workspace PM?"); got != 1 {
		t.Fatalf("resolution conflict closed the PM ask: %d", got)
	}
	var resolutions int
	if err := db.QueryRow(`SELECT COUNT(*) FROM human_attention_request_resolutions WHERE request_event_id='ask-pm'`).Scan(&resolutions); err != nil {
		t.Fatal(err)
	}
	if resolutions != 0 {
		t.Fatal("resolution conflict wrote a resolution for the PM ask")
	}
}

func reseedPMProductData(t *testing.T, db *sql.DB) {
	t.Helper()
	statements := []string{
		`CREATE TABLE IF NOT EXISTS pm_presence(workspace_id TEXT NOT NULL,actor_id TEXT NOT NULL,last_seen_at TEXT NOT NULL,signal TEXT NOT NULL,first_seen_at TEXT NOT NULL DEFAULT '',PRIMARY KEY(workspace_id,actor_id))`,
		`INSERT OR REPLACE INTO pm_records VALUES('turn','turn-1','ws_main','pm-agent','',1,'{"status":"delivered"}')`,
		`INSERT OR REPLACE INTO pm_records VALUES('conversation','conversation-1','ws_main','pm-agent','',1,'{}')`,
		`INSERT OR REPLACE INTO pm_records VALUES('binding','binding-1','ws_main','owner','',1,'{}')`,
		`INSERT OR REPLACE INTO pm_records VALUES('delivery','delivery-1','ws_main','pm-agent','',1,'{}')`,
		`INSERT OR REPLACE INTO pm_records VALUES('delivery_retry','delivery-retry-1','ws_main','pm-agent','',1,'{}')`,
		`INSERT OR REPLACE INTO pm_presence VALUES('ws_main','pm-agent','2026-01-01T00:00:00Z','connect','2026-01-01T00:00:00Z')`,
	}
	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			t.Fatalf("reseed %q: %v", statement, err)
		}
	}
}

func assertMigrationAbsent(t *testing.T, db *sql.DB, version int) {
	t.Helper()
	var applied int
	if err := db.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE version=?`, version).Scan(&applied); err != nil {
		t.Fatal(err)
	}
	if applied != 0 {
		t.Fatalf("migration %d recorded after failed resume", version)
	}
}

func resetPMArchiveMigration(t *testing.T, db *sql.DB) {
	t.Helper()
	if _, err := db.Exec(`DELETE FROM schema_migrations WHERE version=138; DROP TABLE IF EXISTS pm_archive_receipts`); err != nil {
		t.Fatal(err)
	}
}

func seedLegacyPMWorkspace(t *testing.T, db *sql.DB) {
	t.Helper()
	statements := []string{
		`CREATE TABLE IF NOT EXISTS pm_records(kind TEXT NOT NULL,id TEXT NOT NULL,workspace_id TEXT NOT NULL,actor_id TEXT NOT NULL,parent_id TEXT NOT NULL DEFAULT '',revision INTEGER NOT NULL,body BLOB NOT NULL,PRIMARY KEY(kind,id))`,
		`CREATE INDEX IF NOT EXISTS pm_records_status ON pm_records(kind,workspace_id,json_extract(body,'$.status'))`,
		`CREATE INDEX IF NOT EXISTS pm_records_turn_outbox ON pm_records(kind,workspace_id,json_extract(body,'$.status')) WHERE kind='turn'`,
		`CREATE INDEX IF NOT EXISTS pm_records_health_expired ON pm_records(kind,workspace_id,json_extract(body,'$.agent_actor_id'))`,
		`CREATE INDEX IF NOT EXISTS pm_records_health_success ON pm_records(kind,workspace_id,json_extract(body,'$.agent_actor_id'))`,
		`CREATE TABLE IF NOT EXISTS pm_onboarding_backfill(workspace_id TEXT PRIMARY KEY,actor_id TEXT NOT NULL,first_seen_at TEXT,last_seen_at TEXT)`,
		`CREATE TABLE IF NOT EXISTS pm_registration(workspace_id TEXT PRIMARY KEY,actor_id TEXT NOT NULL,runner TEXT NOT NULL,host TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS pm_presence(workspace_id TEXT NOT NULL,actor_id TEXT NOT NULL,last_seen_at TEXT NOT NULL,signal TEXT NOT NULL,first_seen_at TEXT NOT NULL DEFAULT '',PRIMARY KEY(workspace_id,actor_id))`,
		`INSERT INTO pm_records VALUES('decision','decision-1','ws_main','owner','',1,'{"status":"awaiting_answer","work_ref":"card:work"}')`,
		`INSERT INTO pm_records VALUES('action','action-1','ws_main','owner','decision-1',1,'{"decision_id":"decision-1"}')`,
		`INSERT INTO pm_records VALUES('cursor','cursor-1','ws_main','owner','',1,'{"scope":"decisions"}')`,
		`INSERT INTO pm_records VALUES('turn','turn-1','ws_main','pm-agent','',1,'{"status":"delivered"}')`,
		`INSERT INTO pm_records VALUES('conversation','conversation-1','ws_main','pm-agent','',1,'{}')`,
		`INSERT INTO pm_records VALUES('binding','binding-1','ws_main','owner','',1,'{}')`,
		`INSERT INTO pm_records VALUES('delivery','delivery-1','ws_main','pm-agent','',1,'{}')`,
		`INSERT INTO pm_records VALUES('delivery_retry','delivery-retry-1','ws_main','pm-agent','',1,'{}')`,
		`INSERT INTO pm_presence VALUES('ws_main','pm-agent','2026-01-01T00:00:00Z','connect','2026-01-01T00:00:00Z')`,
		`INSERT INTO pm_registration VALUES('ws_main','pm-agent','runner','host')`,
		`INSERT INTO pm_onboarding_backfill VALUES('ws_main','pm-agent','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z')`,
		`INSERT INTO threads(id,updated_at,updated_by,body_json) VALUES('thread-pm-health','2026-01-01T00:00:00Z','system','{"title":"PM health"}')`,
		`INSERT INTO threads(id,updated_at,updated_by,body_json) VALUES('thread-keep','2026-01-01T00:00:00Z','owner','{"title":"Keep this thread"}')`,
	}
	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			t.Fatalf("seed %q: %v", statement, err)
		}
	}
	insertAsk(t, db, "ask-pm", "thread-pm-health", `{"payload":{"title":"Restore the workspace PM?","watcher_health_kind":"pm","requester_actor_id":"owner","subject_ref":"decide"},"title":"Restore the workspace PM?"}`)
	insertAsk(t, db, "ask-other", "thread-keep", `{"payload":{"title":"Other watcher","requester_actor_id":"owner","subject_ref":"card:other"},"title":"Other watcher"}`)
}

func insertAsk(t *testing.T, db *sql.DB, id, thread, payload string) {
	t.Helper()
	ts := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := db.Exec(`INSERT INTO events(id,type,ts,actor_id,thread_id,refs_json,payload_json,created_at) VALUES(?,'human_attention_requested',?,'system',?,?,?,CURRENT_TIMESTAMP)`,
		id, ts, thread, `["thread:`+thread+`"]`, payload); err != nil {
		t.Fatal(err)
	}
}

func assertLegacyPMDataIntact(t *testing.T, db *sql.DB) {
	t.Helper()
	var product, presence int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pm_records WHERE kind IN ('turn','conversation','binding','delivery','delivery_retry')`).Scan(&product); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM pm_presence`).Scan(&presence); err != nil {
		t.Fatal(err)
	}
	if product != 5 || presence != 1 {
		t.Fatalf("product data changed after failed migration: records=%d presence=%d", product, presence)
	}
	for _, kind := range []string{"decision", "action", "cursor"} {
		var count int
		if err := db.QueryRow(`SELECT COUNT(*) FROM pm_records WHERE kind=?`, kind).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 1 {
			t.Fatalf("kept %s row changed: %d", kind, count)
		}
	}
}

func openWorkspaceDB(t *testing.T, root string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", storage.SQLiteDSNForTest(filepath.Join(root, "state.sqlite")))
	if err != nil {
		t.Fatal(err)
	}
	return db
}

func assertMigrationApplied(t *testing.T, db *sql.DB, version int) {
	t.Helper()
	var applied int
	if err := db.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE version=?`, version).Scan(&applied); err != nil {
		t.Fatal(err)
	}
	if applied != 1 {
		t.Fatalf("migration %d not recorded", version)
	}
}

func tableExists(t *testing.T, db *sql.DB, name string) bool {
	t.Helper()
	var exists int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`, name).Scan(&exists); err != nil {
		t.Fatal(err)
	}
	return exists > 0
}

func indexExists(t *testing.T, db *sql.DB, name string) bool {
	t.Helper()
	var exists int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name=?`, name).Scan(&exists); err != nil {
		t.Fatal(err)
	}
	return exists > 0
}

func threadArchived(t *testing.T, db *sql.DB, title string) bool {
	t.Helper()
	var archived int
	if err := db.QueryRow(`SELECT COUNT(*) FROM threads WHERE json_extract(body_json,'$.title')=? AND COALESCE(archived_at,'')<>''`, title).Scan(&archived); err != nil {
		t.Fatal(err)
	}
	return archived > 0
}

func openAskCount(t *testing.T, db *sql.DB, title string) int {
	t.Helper()
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM events ask WHERE ask.type='human_attention_requested' AND trim(COALESCE(json_extract(ask.payload_json,'$.payload.title'),''))=? AND NOT EXISTS (SELECT 1 FROM human_attention_request_resolutions r WHERE r.request_event_id=ask.id)`, title).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func assertPMWatcherResolved(t *testing.T, db *sql.DB, wantResolved bool) {
	t.Helper()
	var resolved int
	if err := db.QueryRow(`SELECT COUNT(*) FROM human_attention_request_resolutions WHERE request_event_id='ask-pm'`).Scan(&resolved); err != nil {
		t.Fatal(err)
	}
	if (resolved == 1) != wantResolved {
		t.Fatalf("PM watcher resolution=%d, want resolved=%v", resolved, wantResolved)
	}
}

func decryptArchive(t *testing.T, ciphertext []byte, identity age.Identity) []byte {
	t.Helper()
	reader, err := age.Decrypt(bytes.NewReader(ciphertext), identity)
	if err != nil {
		t.Fatal(err)
	}
	zstdReader, err := zstd.NewReader(reader)
	if err != nil {
		t.Fatal(err)
	}
	defer zstdReader.Close()
	plaintext, err := io.ReadAll(zstdReader)
	if err != nil {
		t.Fatal(err)
	}
	return plaintext
}

func tarNames(t *testing.T, archive []byte) map[string]bool {
	t.Helper()
	names := map[string]bool{}
	reader := tar.NewReader(bytes.NewReader(archive))
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		names[header.Name] = true
	}
	return names
}

// Keep the helper package import exercised through its public surface, and make
// sure the test recipients are validated the same way the migration does.
func TestRemoveBuiltinPMRecipientParsingMatchesHelper(t *testing.T) {
	ops, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	rec, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := backupage.ParseRecipients(ops.Recipient().String(), rec.Recipient().String())
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(parsed.Fingerprint()) == "" {
		t.Fatal("expected a recipient fingerprint")
	}
	if _, err := json.Marshal(parsed.Fingerprint()); err != nil {
		t.Fatal(err)
	}
}
