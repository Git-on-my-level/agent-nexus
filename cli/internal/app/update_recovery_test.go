package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/errnorm"
)

func recoveryTransactionFixture(t *testing.T) (*App, config.Resolved, string, updateTransaction) {
	t.Helper()
	a, cfg, path := managedUpdateFixture(t)
	var old updateInstallRecord
	if err := readUpdateJSON(installRecordPath(path), &old); err != nil {
		t.Fatal(err)
	}
	backup := filepath.Join(filepath.Dir(path), ".anx-rollback-recovery-test")
	if err := os.WriteFile(backup, []byte("original"), 0755); err != nil {
		t.Fatal(err)
	}
	tx := updateTransaction{SchemaVersion: 1, Phase: "prepared", BackupPath: backup, OldSHA256: old.SHA256, OldRecord: old, OldRecordExists: true,
		NewRecord: updateInstallRecord{ManagedBy: "anx", Version: "v0.12.11", SHA256: sha256HexForTest([]byte("candidate")), InstalledAt: "2026-10-05T00:00:00Z"}}
	if err := writeUpdateJSON(updateTransactionPath(path), tx); err != nil {
		t.Fatal(err)
	}
	// A crash after replacement but before committing the new receipt.
	if err := os.WriteFile(path, []byte("candidate"), 0755); err != nil {
		t.Fatal(err)
	}
	return a, cfg, path, tx
}

func assertRecoveryPreservesFiles(t *testing.T, paths []string, attempt func()) {
	t.Helper()
	type snapshot struct {
		data   []byte
		exists bool
	}
	before := map[string]snapshot{}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			t.Fatal(err)
		}
		before[path] = snapshot{data, err == nil}
	}
	attempt()
	for path, saved := range before {
		data, err := os.ReadFile(path)
		if err != nil && !errors.Is(err, os.ErrNotExist) || (err == nil) != saved.exists || !bytes.Equal(data, saved.data) {
			t.Fatalf("rejected recovery changed evidence %s: %v", path, err)
		}
	}
}

func assertScheduledRecoveryRejected(t *testing.T, a *App, cfg config.Resolved) {
	t.Helper()
	if _, err := a.runUpdate(context.Background(), []string{"now", "--scheduled"}, cfg); err == nil || errnorm.Normalize(err).Code != "update_recovery_failed" {
		t.Fatalf("scheduled recovery did not fail safely: %v", err)
	}
	dir, err := a.updateDirectory(cfg)
	if err != nil {
		t.Fatal(err)
	}
	state, err := readUpdateState(dir)
	if err != nil || state.Rollback != "failed" || state.FailureStage != "crash_recovery" {
		t.Fatalf("recovery failure not recorded: %+v %v", state, err)
	}
	status, err := a.runUpdateStatus(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if visible := asMap(status.Data)["state"].(updateState); visible.Rollback != "failed" || visible.FailureCode != "update_recovery_failed" {
		t.Fatalf("offline status concealed failed recovery: %+v", visible)
	}
}

func TestUpdateRecoveryPreservesForeignBinaryAfterCrash(t *testing.T) {
	for _, phase := range []string{"prepared", "committed"} {
		t.Run(phase, func(t *testing.T) {
			a, cfg, path, tx := recoveryTransactionFixture(t)
			tx.Phase = phase
			if err := writeUpdateJSON(updateTransactionPath(path), tx); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte("unrelated development bytes"), 0755); err != nil {
				t.Fatal(err)
			}
			assertRecoveryPreservesFiles(t, []string{path, tx.BackupPath, installRecordPath(path), updateTransactionPath(path)}, func() {
				assertScheduledRecoveryRejected(t, a, cfg)
			})
		})
	}
}

func TestUpdateRecoveryRejectsIncompleteOrInconsistentJournal(t *testing.T) {
	tests := map[string]func(map[string]any){
		"minimal reproduction": func(tx map[string]any) {
			for key := range tx {
				if key != "schema_version" && key != "phase" {
					delete(tx, key)
				}
			}
		},
		"empty digests and receipt": func(tx map[string]any) {
			tx["old_sha256"], tx["backup_path"], tx["old_record_exists"] = "", "", false
			tx["old_record"], tx["new_record"] = map[string]any{}, map[string]any{}
		},
		"empty candidate digest":              func(tx map[string]any) { tx["new_record"].(map[string]any)["sha256"] = "" },
		"invalid original digest":             func(tx map[string]any) { tx["old_sha256"] = "garbage" },
		"empty backup":                        func(tx map[string]any) { tx["backup_path"] = "" },
		"relative backup":                     func(tx map[string]any) { tx["backup_path"] = ".anx-rollback-relative" },
		"original receipt mismatch":           func(tx map[string]any) { tx["old_record"].(map[string]any)["sha256"] = strings.Repeat("0", 64) },
		"first install with original receipt": func(tx map[string]any) { tx["old_sha256"], tx["backup_path"] = "", "" },
		"unexpected original receipt":         func(tx map[string]any) { tx["old_record_exists"] = false },
		"invalid candidate owner":             func(tx map[string]any) { tx["new_record"].(map[string]any)["managed_by"] = "foreign" },
		"invalid candidate version":           func(tx map[string]any) { tx["new_record"].(map[string]any)["version"] = "v1/../../x" },
		"invalid timestamp":                   func(tx map[string]any) { tx["new_record"].(map[string]any)["installed_at"] = "yesterday" },
		"invalid phase":                       func(tx map[string]any) { tx["phase"] = "unknown" },
		"invalid schema":                      func(tx map[string]any) { tx["schema_version"] = 2 },
		"unexpected field":                    func(tx map[string]any) { tx["old_sh256"] = tx["old_sha256"] },
	}
	for _, key := range []string{"schema_version", "phase", "backup_path", "old_sha256", "old_record", "old_record_exists", "new_record"} {
		tests["missing "+key] = func(tx map[string]any) { delete(tx, key) }
		tests["null "+key] = func(tx map[string]any) { tx[key] = nil }
	}
	for _, receipt := range []string{"old_record", "new_record"} {
		for _, key := range []string{"managed_by", "version", "sha256", "installed_at"} {
			tests[receipt+" missing "+key] = func(tx map[string]any) { delete(tx[receipt].(map[string]any), key) }
			tests[receipt+" null "+key] = func(tx map[string]any) { tx[receipt].(map[string]any)[key] = nil }
		}
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			a, cfg, path, tx := recoveryTransactionFixture(t)
			// Preserve a healthy original, including the minimal-record deletion reproduction.
			if err := os.WriteFile(path, []byte("original"), 0755); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(updateTransactionPath(path))
			if err != nil {
				t.Fatal(err)
			}
			var fields map[string]any
			if err := json.Unmarshal(data, &fields); err != nil {
				t.Fatal(err)
			}
			mutate(fields)
			if err := writeUpdateJSON(updateTransactionPath(path), fields); err != nil {
				t.Fatal(err)
			}
			assertRecoveryPreservesFiles(t, []string{path, tx.BackupPath, installRecordPath(path), updateTransactionPath(path)}, func() {
				assertScheduledRecoveryRejected(t, a, cfg)
			})
		})
	}
}

func TestUpdateRecoveryRequiresCandidateIdentityForFirstInstall(t *testing.T) {
	for _, contents := range []string{"candidate", "", "foreign"} {
		t.Run("binary="+contents, func(t *testing.T) {
			_, _, path, tx := recoveryTransactionFixture(t)
			if err := os.Remove(tx.BackupPath); err != nil {
				t.Fatal(err)
			}
			tx.OldSHA256, tx.BackupPath, tx.OldRecordExists, tx.OldRecord = "", "", false, updateInstallRecord{}
			if err := writeUpdateJSON(updateTransactionPath(path), tx); err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(installRecordPath(path)); err != nil {
				t.Fatal(err)
			}
			if contents == "" {
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
			} else if err := os.WriteFile(path, []byte(contents), 0755); err != nil {
				t.Fatal(err)
			}
			lock, err := lockUpdateInstall(path)
			if err != nil {
				t.Fatal(err)
			}
			defer lock.Close()
			if contents == "foreign" {
				assertRecoveryPreservesFiles(t, []string{path, installRecordPath(path), updateTransactionPath(path)}, func() {
					if outcome, _, err := recoverUpdateTransaction(path); err == nil || outcome != "failed" {
						t.Fatalf("foreign first-install target accepted: %s %v", outcome, err)
					}
				})
				return
			}
			if outcome, _, err := recoverUpdateTransaction(path); err != nil || outcome != "succeeded" {
				t.Fatalf("valid first-install recovery failed: %s %v", outcome, err)
			}
			for _, file := range []string{path, installRecordPath(path), updateTransactionPath(path)} {
				if _, err := os.Stat(file); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("first-install rollback did not restore absence: %s %v", file, err)
				}
			}
		})
	}
}

func TestUpdateRecoveryPreservesChangedOrSymlinkedBackup(t *testing.T) {
	for _, symlink := range []bool{false, true} {
		t.Run(map[bool]string{false: "changed", true: "symlink"}[symlink], func(t *testing.T) {
			a, cfg, path, tx := recoveryTransactionFixture(t)
			if symlink {
				if err := os.Remove(tx.BackupPath); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(path, tx.BackupPath); err != nil {
					t.Fatal(err)
				}
			} else if err := os.WriteFile(tx.BackupPath, []byte("foreign backup"), 0755); err != nil {
				t.Fatal(err)
			}
			assertRecoveryPreservesFiles(t, []string{path, tx.BackupPath, installRecordPath(path), updateTransactionPath(path)}, func() {
				assertScheduledRecoveryRejected(t, a, cfg)
			})
		})
	}
}

func TestUpdateReplacementPreservesTargetChangedAfterJournalPreparation(t *testing.T) {
	_, _, path, tx := recoveryTransactionFixture(t)
	if err := os.Remove(updateTransactionPath(path)); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(tx.BackupPath); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("original"), 0755); err != nil {
		t.Fatal(err)
	}
	// Candidate probing occurs during staging, before the durable journal. A
	// source build may change the target while the candidate is being prepared.
	updateProbeBinary = func(context.Context, string, string) error {
		return os.WriteFile(path, []byte("foreign bytes arriving during staging"), 0755)
	}
	if outcome, backup, err := replaceManagedExecutable(context.Background(), path, []byte("candidate"), 0755, "v0.12.11", tx.OldRecord); err == nil || outcome != "failed" || backup == "" {
		t.Fatalf("staging race did not preserve evidence: %s %s %v", outcome, backup, err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "foreign bytes arriving during staging" {
		t.Fatal("replacement overwrote foreign target", err)
	}
	retained, err := readUpdateTransaction(path)
	if err != nil {
		t.Fatal("staging race lost journal", err)
	}
	if data, err := os.ReadFile(retained.BackupPath); err != nil || string(data) != "original" {
		t.Fatal("staging race lost backup", err)
	}
}

func TestUpdateRecoveryAcceptsRecordedOriginalCandidateOrAbsence(t *testing.T) {
	for _, managed := range []bool{true, false} {
		for _, contents := range []string{"original", "candidate", ""} {
			t.Run(map[bool]string{true: "managed", false: "unmanaged"}[managed]+"/"+contents, func(t *testing.T) {
				_, _, path, tx := recoveryTransactionFixture(t)
				if !managed {
					tx.OldRecordExists, tx.OldRecord = false, updateInstallRecord{}
					if err := os.Remove(installRecordPath(path)); err != nil {
						t.Fatal(err)
					}
				}
				if contents == "" {
					if err := os.Remove(path); err != nil {
						t.Fatal(err)
					}
				} else if err := os.WriteFile(path, []byte(contents), 0755); err != nil {
					t.Fatal(err)
				}
				if contents == "original" {
					// Interrupted rollback already consumed its backup.
					if err := os.Remove(tx.BackupPath); err != nil {
						t.Fatal(err)
					}
				}
				if err := writeUpdateJSON(updateTransactionPath(path), tx); err != nil {
					t.Fatal(err)
				}
				lock, err := lockUpdateInstall(path)
				if err != nil {
					t.Fatal(err)
				}
				defer lock.Close()
				if outcome, _, err := recoverUpdateTransaction(path); err != nil || outcome != "succeeded" {
					t.Fatalf("valid rollback rejected: %s %v", outcome, err)
				}
				if data, err := os.ReadFile(path); err != nil || string(data) != "original" {
					t.Fatal("original not restored", err)
				}
				if managed {
					var restored updateInstallRecord
					if err := readUpdateJSON(installRecordPath(path), &restored); err != nil || restored != tx.OldRecord {
						t.Fatal("original receipt not restored", err)
					}
				} else if _, err := os.Stat(installRecordPath(path)); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("unmanaged original gained a receipt", err)
				}
			})
		}
	}
}

func TestUpdateRecoveryCompletesCommittedCleanup(t *testing.T) {
	for _, backupPresent := range []bool{true, false} {
		t.Run(map[bool]string{true: "backup", false: "already cleaned backup"}[backupPresent], func(t *testing.T) {
			_, _, path, tx := recoveryTransactionFixture(t)
			tx.Phase = "committed"
			if !backupPresent {
				if err := os.Remove(tx.BackupPath); err != nil {
					t.Fatal(err)
				}
			}
			if err := writeUpdateJSON(updateTransactionPath(path), tx); err != nil {
				t.Fatal(err)
			}
			lock, err := lockUpdateInstall(path)
			if err != nil {
				t.Fatal(err)
			}
			defer lock.Close()
			if outcome, _, err := recoverUpdateTransaction(path); err != nil || outcome != "committed" {
				t.Fatalf("valid commit cleanup rejected: %s %v", outcome, err)
			}
			var receipt updateInstallRecord
			if err := readUpdateJSON(installRecordPath(path), &receipt); err != nil || receipt != tx.NewRecord {
				t.Fatal("committed receipt not completed", err)
			}
			if data, err := os.ReadFile(path); err != nil || string(data) != "candidate" {
				t.Fatal("committed binary changed", err)
			}
			for _, file := range []string{tx.BackupPath, updateTransactionPath(path)} {
				if _, err := os.Stat(file); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("commit cleanup incomplete", err)
				}
			}
		})
	}
}
