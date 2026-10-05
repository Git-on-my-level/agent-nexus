package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"agent-nexus-cli/internal/errnorm"
	"agent-nexus-cli/internal/filelock"
)

type updateTransaction struct {
	SchemaVersion   int                 `json:"schema_version"`
	Phase           string              `json:"phase"`
	BackupPath      string              `json:"backup_path"`
	OldSHA256       string              `json:"old_sha256"`
	OldRecord       updateInstallRecord `json:"old_record"`
	OldRecordExists bool                `json:"old_record_exists"`
	NewRecord       updateInstallRecord `json:"new_record"`
}

func updateTransactionPath(path string) string { return path + ".anx-transaction.json" }
func lockUpdateInstall(path string) (*os.File, error) {
	f, err := filelock.OpenNoFollow(path+".anx-update.lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		f.Close()
		return nil, fmt.Errorf("invalid update lock")
	}
	if err := filelock.TryLock(f); err != nil {
		f.Close()
		return nil, errnorm.Wrap(errnorm.KindLocal, "update_locked", "another installer or updater owns the process lock", err)
	}
	return f, nil // Close releases only this owner's kernel lock; inode stays.
}
func syncUpdateDirectory(dir string) error {
	f, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}
func syncUpdateFile(path string) error {
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}
func durableUpdateRename(from, to string) error {
	if err := updateRename(from, to); err != nil {
		return err
	}
	if err := syncUpdateDirectory(filepath.Dir(to)); err != nil {
		return err
	}
	if filepath.Dir(from) != filepath.Dir(to) {
		return syncUpdateDirectory(filepath.Dir(from))
	}
	return nil
}
func removeUpdateTransaction(path string, tx updateTransaction) error {
	if tx.BackupPath != "" {
		if err := os.Remove(tx.BackupPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	if err := os.Remove(updateTransactionPath(path)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return syncUpdateDirectory(filepath.Dir(path))
}
func readUpdateTransaction(path string) (updateTransaction, error) {
	var tx updateTransaction
	err := readUpdateJSON(updateTransactionPath(path), &tx)
	if err != nil {
		return tx, err
	}
	if tx.SchemaVersion != 1 || tx.Phase != "prepared" && tx.Phase != "committed" {
		return tx, fmt.Errorf("invalid update transaction")
	}
	if tx.BackupPath != "" {
		// macOS /var and /private/var (and other symlinked ancestors) can
		// name the same installation directory across processes.
		backupDir, backupErr := filepath.EvalSymlinks(filepath.Dir(tx.BackupPath))
		installDir, installErr := filepath.EvalSymlinks(filepath.Dir(path))
		if backupErr != nil || installErr != nil || backupDir != installDir || !strings.HasPrefix(filepath.Base(tx.BackupPath), ".anx-rollback-") {
			return tx, fmt.Errorf("invalid update transaction backup")
		}
	}
	return tx, nil
}

// Called only under the shared per-install kernel lock. Rollback is idempotent
// even if recovery itself dies between restoring the binary and its receipt.
func recoverUpdateTransaction(path string) (string, string, error) {
	tx, err := readUpdateTransaction(path)
	if errors.Is(err, os.ErrNotExist) {
		return "not_needed", "", nil
	}
	if err != nil {
		return "failed", tx.BackupPath, err
	}
	if tx.Phase == "committed" {
		digest, err := binaryDigest(path)
		if err != nil || digest != tx.NewRecord.SHA256 {
			return "failed", tx.BackupPath, fmt.Errorf("committed binary differs from transaction")
		}
		if err := writeUpdateJSON(installRecordPath(path), tx.NewRecord); err != nil {
			return "failed", tx.BackupPath, err
		}
		return "committed", "", removeUpdateTransaction(path, tx)
	}
	if tx.OldSHA256 == "" {
		// Interrupted first install: restore absence, rather than adopting bytes.
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return "failed", tx.BackupPath, err
		}
	} else {
		digest, _ := binaryDigest(path)
		if digest != tx.OldSHA256 {
			oldDigest, err := binaryDigest(tx.BackupPath)
			if err != nil || oldDigest != tx.OldSHA256 {
				return "failed", tx.BackupPath, fmt.Errorf("rollback backup missing or changed")
			}
			if err := durableUpdateRename(tx.BackupPath, path); err != nil {
				return "failed", tx.BackupPath, err
			}
		}
	}
	if tx.OldRecordExists {
		if err := writeUpdateJSON(installRecordPath(path), tx.OldRecord); err != nil {
			return "failed", tx.BackupPath, err
		}
	} else if err := os.Remove(installRecordPath(path)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return "failed", tx.BackupPath, err
	}
	if err := syncUpdateDirectory(filepath.Dir(path)); err != nil {
		return "failed", tx.BackupPath, err
	}
	return "succeeded", "", removeUpdateTransaction(path, tx)
}

// Used by the updater with the process lock held. The installer uses the same
// journal and fsync ordering, including recovery before ownership inspection.
func replaceManagedExecutable(ctx context.Context, path string, binary []byte, mode os.FileMode, version string, oldRecord updateInstallRecord) (string, string, error) {
	old, err := os.ReadFile(path)
	if err != nil {
		return "not_needed", "", err
	}
	oldSum := sha256.Sum256(old)
	if hex.EncodeToString(oldSum[:]) != oldRecord.SHA256 {
		return "not_needed", "", errnorm.Local("unmanaged_install", "binary changed before replacement")
	}
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}
	if mode == 0 {
		mode = 0755
	}
	tmp, err := os.MkdirTemp(filepath.Dir(path), ".anx-update-")
	if err != nil {
		return "not_needed", "", err
	}
	defer os.RemoveAll(tmp)
	candidate := filepath.Join(tmp, filepath.Base(path))
	if err := updateWriteFile(candidate, binary, mode); err != nil {
		return "not_needed", "", err
	}
	if err := updateChmod(candidate, mode); err != nil {
		return "not_needed", "", err
	}
	if err := syncUpdateFile(candidate); err != nil {
		return "not_needed", "", err
	}
	if err := syncUpdateDirectory(tmp); err != nil {
		return "not_needed", "", err
	}
	if err := updateProbeBinary(ctx, candidate, version); err != nil {
		return "not_needed", "", errnorm.Wrap(errnorm.KindLocal, "update_probe_failed", "candidate failed version/architecture probe", err)
	}
	backupFile, err := os.CreateTemp(filepath.Dir(path), ".anx-rollback-")
	if err != nil {
		return "not_needed", "", err
	}
	backup := backupFile.Name()
	if _, err = backupFile.Write(old); err == nil {
		err = backupFile.Chmod(mode)
	}
	if err == nil {
		err = backupFile.Sync()
	}
	closeErr := backupFile.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		os.Remove(backup)
		return "not_needed", "", err
	}
	if err := syncUpdateDirectory(filepath.Dir(path)); err != nil {
		return "not_needed", backup, err
	}
	sum := sha256.Sum256(binary)
	record := updateInstallRecord{ManagedBy: "anx", Version: version, SHA256: hex.EncodeToString(sum[:]), InstalledAt: time.Now().UTC().Format(time.RFC3339)}
	tx := updateTransaction{SchemaVersion: 1, Phase: "prepared", BackupPath: backup, OldSHA256: oldRecord.SHA256, OldRecord: oldRecord, OldRecordExists: true, NewRecord: record}
	if err := writeUpdateJSON(updateTransactionPath(path), tx); err != nil {
		return "not_needed", backup, err
	}
	rollback := func(cause error) (string, string, error) {
		outcome, saved, recoveryErr := recoverUpdateTransaction(path)
		if recoveryErr != nil {
			return "failed", saved, errnorm.Wrap(errnorm.KindLocal, "update_rollback_failed", "transaction retained for recovery", recoveryErr)
		}
		return outcome, saved, cause
	}
	if err := durableUpdateRename(candidate, path); err != nil {
		return rollback(errnorm.Wrap(errnorm.KindLocal, "update_replace_failed", "replacement failed", err))
	}
	if err := updateProbeBinary(ctx, path, version); err != nil {
		return rollback(errnorm.Wrap(errnorm.KindLocal, "update_probe_failed", "installed binary failed probe", err))
	}
	if err := writeUpdateJSON(installRecordPath(path), record); err != nil {
		return rollback(err)
	}
	tx.Phase = "committed"
	if err := writeUpdateJSON(updateTransactionPath(path), tx); err != nil {
		return rollback(err)
	}
	if err := removeUpdateTransaction(path, tx); err != nil {
		return "not_needed", backup, err
	}
	return "not_needed", "", nil
}
