package storage

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"filippo.io/age"
)

// seedPMFaultWorkspace creates a workspace with product rows, then rewinds the
// migration ledger so the next InitializeWorkspace runs migration 138 again.
func seedPMFaultWorkspace(t *testing.T, root string) {
	t.Helper()
	ctx := context.Background()
	ws, err := InitializeWorkspace(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	db := ws.DB()
	statements := []string{
		`CREATE TABLE IF NOT EXISTS pm_records(kind TEXT NOT NULL,id TEXT NOT NULL,workspace_id TEXT NOT NULL,actor_id TEXT NOT NULL,parent_id TEXT NOT NULL DEFAULT '',revision INTEGER NOT NULL,body BLOB NOT NULL,PRIMARY KEY(kind,id))`,
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
			t.Fatalf("seed %q: %v", statement, err)
		}
	}
	if _, err := db.Exec(`DELETE FROM schema_migrations WHERE version=138; DROP TABLE IF EXISTS pm_archive_receipts`); err != nil {
		t.Fatal(err)
	}
	if err := ws.Close(); err != nil {
		t.Fatal(err)
	}
}

func setPMFaultRecipients(t *testing.T) {
	t.Helper()
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
}

func openPMFaultDB(t *testing.T, root string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", SQLiteDSNForTest(filepath.Join(root, "state.sqlite")))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func assertPMFaultStateIntact(t *testing.T, root string) {
	t.Helper()
	db := openPMFaultDB(t, root)
	var product, presence, applied int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pm_records WHERE kind IN ('turn','conversation','binding','delivery','delivery_retry')`).Scan(&product); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM pm_presence`).Scan(&presence); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE version=138`).Scan(&applied); err != nil {
		t.Fatal(err)
	}
	if product != 5 || presence != 1 {
		t.Fatalf("product data changed after failed migration: records=%d presence=%d", product, presence)
	}
	if applied != 0 {
		t.Fatal("failed migration recorded version 138")
	}
}

func TestRemoveBuiltinPMFaultsLeaveProductData(t *testing.T) {
	cases := []struct {
		name    string
		install func(t *testing.T)
	}{
		{"dump", func(t *testing.T) {
			old := pmArchiveDumpTable
			pmArchiveDumpTable = func(context.Context, *sql.Tx, string, string) (pmArchivePart, error) {
				return pmArchivePart{}, errors.New("injected dump failure")
			}
			t.Cleanup(func() { pmArchiveDumpTable = old })
		}},
		{"write", func(t *testing.T) {
			old := pmArchiveWritePart
			pmArchiveWritePart = func(io.Writer, io.Reader) (int64, error) {
				return 0, errors.New("injected write failure")
			}
			t.Cleanup(func() { pmArchiveWritePart = old })
		}},
		{"encrypt-close", func(t *testing.T) {
			old := pmArchiveEncryptClose
			pmArchiveEncryptClose = func(io.Closer) error { return errors.New("injected encrypt close failure") }
			t.Cleanup(func() { pmArchiveEncryptClose = old })
		}},
		{"file-sync", func(t *testing.T) {
			old := pmArchiveFileSync
			pmArchiveFileSync = func(*os.File) error { return errors.New("injected file sync failure") }
			t.Cleanup(func() { pmArchiveFileSync = old })
		}},
		{"verify", func(t *testing.T) {
			old := pmArchiveVerify
			pmArchiveVerify = func(string, int64, string) error { return errors.New("injected verify failure") }
			t.Cleanup(func() { pmArchiveVerify = old })
		}},
		{"damaged-new-archive", func(t *testing.T) {
			old := pmArchiveVerify
			pmArchiveVerify = func(path string, size int64, digest string) error {
				data, err := os.ReadFile(path)
				if err != nil {
					return err
				}
				data[len(data)/2] ^= 0xff
				if err := os.WriteFile(path, data, 0o600); err != nil {
					return err
				}
				return old(path, size, digest)
			}
			t.Cleanup(func() { pmArchiveVerify = old })
		}},
		{"rename", func(t *testing.T) {
			old := pmArchiveRename
			pmArchiveRename = func(string, string) error { return errors.New("injected rename failure") }
			t.Cleanup(func() { pmArchiveRename = old })
		}},
		{"dir-sync", func(t *testing.T) {
			old := pmArchiveDirSync
			pmArchiveDirSync = func(string) error { return errors.New("injected directory sync failure") }
			t.Cleanup(func() { pmArchiveDirSync = old })
		}},
		{"receipt-insert", func(t *testing.T) {
			old := pmArchiveRecordReceipt
			pmArchiveRecordReceipt = func(context.Context, *sql.Tx, *pmArchiveReceipt) error {
				return errors.New("injected receipt insert failure")
			}
			t.Cleanup(func() { pmArchiveRecordReceipt = old })
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			seedPMFaultWorkspace(t, root)
			setPMFaultRecipients(t)
			tc.install(t)
			if _, err := InitializeWorkspace(context.Background(), root); err == nil {
				t.Fatal("expected injected failure to abort the migration")
			}
			assertPMFaultStateIntact(t, root)
		})
	}
}

func TestRemoveBuiltinPMEmptyWorkspaceNeedsNoRecipients(t *testing.T) {
	t.Setenv("ANX_BACKUP_AGE_RECIPIENTS", "")
	t.Setenv("ANX_VAULT_REC_RECIPIENT", "")
	root := t.TempDir()
	ws, err := InitializeWorkspace(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	var applied, receipts int
	if err := ws.DB().QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE version=138`).Scan(&applied); err != nil {
		t.Fatal(err)
	}
	if err := ws.DB().QueryRow(`SELECT COUNT(*) FROM pm_archive_receipts`).Scan(&receipts); err != nil {
		t.Fatal(err)
	}
	matches, err := filepath.Glob(filepath.Join(root, "state.pm-archive-138-*.tar.zst.age"))
	if err != nil {
		t.Fatal(err)
	}
	if applied != 1 || receipts != 0 || len(matches) != 0 {
		t.Fatalf("empty workspace migration: applied=%d receipts=%d archives=%d", applied, receipts, len(matches))
	}
}

func TestRemoveBuiltinPMAlreadyMigratedWorkspaceReplays(t *testing.T) {
	root := t.TempDir()
	seedPMFaultWorkspace(t, root)
	setPMFaultRecipients(t)
	ctx := context.Background()
	ws, err := InitializeWorkspace(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	if err := ws.Close(); err != nil {
		t.Fatal(err)
	}
	db := openPMFaultDB(t, root)
	if _, err := db.Exec(`DELETE FROM schema_migrations WHERE version=138`); err != nil {
		t.Fatal(err)
	}
	db.Close()

	ws, err = InitializeWorkspace(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	var applied, receipts int
	if err := ws.DB().QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE version=138`).Scan(&applied); err != nil {
		t.Fatal(err)
	}
	if err := ws.DB().QueryRow(`SELECT COUNT(*) FROM pm_archive_receipts`).Scan(&receipts); err != nil {
		t.Fatal(err)
	}
	matches, err := filepath.Glob(filepath.Join(root, "state.pm-archive-138-*.tar.zst.age"))
	if err != nil {
		t.Fatal(err)
	}
	if applied != 1 || receipts != 1 || len(matches) != 1 {
		t.Fatalf("already-migrated replay: applied=%d receipts=%d archives=%d", applied, receipts, len(matches))
	}
}
