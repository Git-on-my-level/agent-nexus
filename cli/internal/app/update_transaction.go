package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
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

var updateReceiptVersion = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+(?:[-+][A-Za-z0-9.-]+)?$`)

func validUpdateDigest(digest string) bool {
	b, err := hex.DecodeString(digest)
	return err == nil && len(b) == sha256.Size && strings.ToLower(digest) == digest
}

func validateUpdateReceipt(record updateInstallRecord) error {
	_, err := time.Parse(time.RFC3339, record.InstalledAt)
	if record.ManagedBy != "anx" || !updateReceiptVersion.MatchString(record.Version) || !validUpdateDigest(record.SHA256) || err != nil {
		return fmt.Errorf("invalid update transaction receipt")
	}
	return nil
}

// Explicitly require fields: decoding into a struct alone treats missing/null
// values as zero values, which must never authorize first-install rollback.
func requireUpdateJSONFields(data json.RawMessage, keys ...string) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil || len(fields) != len(keys) {
		return fmt.Errorf("incomplete update transaction object")
	}
	for _, key := range keys {
		if value, ok := fields[key]; !ok || string(value) == "null" {
			return fmt.Errorf("missing update transaction field %s", key)
		}
	}
	return nil
}

func validateUpdateTransaction(path string, tx updateTransaction) error {
	if tx.SchemaVersion != 1 || tx.Phase != "prepared" && tx.Phase != "committed" {
		return fmt.Errorf("invalid update transaction")
	}
	if err := validateUpdateReceipt(tx.NewRecord); err != nil {
		return err
	}
	if tx.OldRecordExists {
		if err := validateUpdateReceipt(tx.OldRecord); err != nil {
			return err
		}
		if tx.OldRecord.SHA256 != tx.OldSHA256 {
			return fmt.Errorf("original receipt differs from transaction digest")
		}
	} else if tx.OldRecord != (updateInstallRecord{}) {
		return fmt.Errorf("unexpected original receipt in update transaction")
	}
	if tx.OldSHA256 == "" {
		if tx.BackupPath != "" || tx.OldRecordExists {
			return fmt.Errorf("inconsistent first-install transaction")
		}
	} else {
		if !validUpdateDigest(tx.OldSHA256) || !filepath.IsAbs(tx.BackupPath) || filepath.Clean(tx.BackupPath) != tx.BackupPath {
			return fmt.Errorf("invalid original digest or rollback path")
		}
		// macOS /var and /private/var (and other symlinked ancestors) can
		// name the same installation directory across processes.
		backupDir, backupErr := filepath.EvalSymlinks(filepath.Dir(tx.BackupPath))
		installDir, installErr := filepath.EvalSymlinks(filepath.Dir(path))
		if backupErr != nil || installErr != nil || backupDir != installDir || filepath.Base(tx.BackupPath) == filepath.Base(path) || !strings.HasPrefix(filepath.Base(tx.BackupPath), ".anx-rollback-") {
			return fmt.Errorf("invalid update transaction backup")
		}
	}
	return nil
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
	if err := validateUpdateTransaction(path, tx); err != nil {
		return err
	}
	if _, err := inspectUpdateTransactionFiles(path, tx); err != nil {
		return err
	}
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
	data, err := os.ReadFile(updateTransactionPath(path))
	if err != nil {
		return tx, err
	}
	if err := requireUpdateJSONFields(data, "schema_version", "phase", "backup_path", "old_sha256", "old_record", "old_record_exists", "new_record"); err != nil {
		return tx, err
	}
	if err := json.Unmarshal(data, &tx); err != nil {
		return tx, err
	}
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(data, &fields)
	if err := requireUpdateJSONFields(fields["new_record"], "managed_by", "version", "sha256", "installed_at"); err != nil {
		return tx, err
	}
	var oldFields map[string]json.RawMessage
	if err := json.Unmarshal(fields["old_record"], &oldFields); err != nil {
		return tx, err
	}
	// Python writes {} for an absent receipt; Go writes an empty typed record.
	if tx.OldRecordExists || len(oldFields) != 0 {
		if err := requireUpdateJSONFields(fields["old_record"], "managed_by", "version", "sha256", "installed_at"); err != nil {
			return tx, err
		}
	}
	return tx, validateUpdateTransaction(path, tx)
}

// Absence is distinct from unreadable/nonregular files. Never follow a final
// symlink when deciding whether a transaction owns bytes it could destroy.
func updateRecoveryDigest(path string) (string, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("transaction path is not a regular file: %s", path)
	}
	f, err := filelock.OpenNoFollow(path, os.O_RDONLY, 0)
	if err != nil {
		return "", err
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(info, opened) {
		return "", fmt.Errorf("transaction file changed while opening: %s", path)
	}
	sum := sha256.New()
	if _, err := io.Copy(sum, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(sum.Sum(nil)), nil
}

func inspectUpdateTransactionFiles(path string, tx updateTransaction) (string, error) {
	digest, err := updateRecoveryDigest(path)
	if err != nil {
		return "", err
	}
	if digest != "" && digest != tx.OldSHA256 && digest != tx.NewRecord.SHA256 {
		return "", fmt.Errorf("binary differs from both transaction digests; preserving recovery evidence")
	}
	if tx.Phase == "committed" && digest != tx.NewRecord.SHA256 {
		return "", fmt.Errorf("committed binary differs from transaction")
	}
	if tx.BackupPath != "" {
		backupDigest, err := updateRecoveryDigest(tx.BackupPath)
		if err != nil || backupDigest != "" && backupDigest != tx.OldSHA256 {
			return "", fmt.Errorf("rollback backup unreadable or changed")
		}
		if tx.Phase == "prepared" && digest != tx.OldSHA256 && backupDigest == "" {
			return "", fmt.Errorf("rollback backup missing")
		}
	}
	return digest, nil
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
	digest, err := inspectUpdateTransactionFiles(path, tx)
	if err != nil {
		return "failed", tx.BackupPath, err
	}
	if tx.Phase == "committed" {
		if err := writeUpdateJSON(installRecordPath(path), tx.NewRecord); err != nil {
			return "failed", tx.BackupPath, err
		}
		return "committed", "", removeUpdateTransaction(path, tx)
	}
	if tx.OldSHA256 == "" {
		// Only the recorded candidate can be removed after a first install.
		if digest != "" {
			if err := os.Remove(path); err != nil {
				return "failed", tx.BackupPath, err
			}
		}
	} else {
		if digest != tx.OldSHA256 {
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
	if err := validateUpdateReceipt(oldRecord); err != nil {
		return "not_needed", "", err
	}
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
	if err := validateUpdateTransaction(path, tx); err != nil {
		return "not_needed", backup, err
	}
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
	if _, err := inspectUpdateTransactionFiles(path, tx); err != nil {
		return rollback(err)
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
