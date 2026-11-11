package storage

import (
	"archive/tar"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"agent-nexus-core/internal/backupage"
	"agent-nexus-core/internal/handles"

	"github.com/klauspost/compress/zstd"
)

const (
	pmArchiveSchemaVersion = 137
	pmArchiveReceiptsTable = "pm_archive_receipts"
	ageHeader              = "age-encryption.org/v1\n"
)

var pmProductKinds = []string{"turn", "conversation", "binding", "delivery", "delivery_retry"}

// These indexes serve only product record kinds. Decide indexes on pm_records
// are deliberately kept.
var pmProductIndexes = []string{
	"pm_records_turn_outbox",
	"pm_records_health_expired",
	"pm_records_health_success",
}

type pmArchiveReceipt struct {
	WorkspaceID           string
	ArchivePath           string
	SHA256                string
	SizeBytes             int64
	RecipientsFingerprint string
	SchemaVersion         int
	CreatedAt             string
}

// migrateRemoveBuiltinPM archives and then removes the built-in PM product
// data. The archive is written and verified before any product row is deleted.
// A missing archive configuration fails closed whenever product data exists.
func migrateRemoveBuiltinPM(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS `+pmArchiveReceiptsTable+` (
		workspace_id TEXT PRIMARY KEY,
		archive_path TEXT NOT NULL,
		sha256 TEXT NOT NULL,
		size_bytes INTEGER NOT NULL,
		recipients_fingerprint TEXT NOT NULL,
		schema_version INTEGER NOT NULL,
		created_at TEXT NOT NULL
	)`); err != nil {
		return fmt.Errorf("create PM archive receipt table: %w", err)
	}

	databasePath, err := sqliteDatabasePath(ctx, tx)
	if err != nil {
		return err
	}
	if databasePath == "" {
		return errors.New("PM archive requires a file-backed workspace database")
	}

	productRows, workspaceID, err := pmProductRowCounts(ctx, tx)
	if err != nil {
		return err
	}
	receipt, err := loadPMArchiveReceipt(ctx, tx)
	if err != nil {
		return err
	}

	switch {
	case productRows > 0 && receipt == nil:
		recipients, err := backupage.ParseRecipients(os.Getenv("ANX_BACKUP_AGE_RECIPIENTS"), os.Getenv("ANX_VAULT_REC_RECIPIENT"))
		if err != nil {
			return fmt.Errorf("PM archive recipients are not configured: %w", err)
		}
		written, err := writePMArchive(ctx, tx, databasePath, workspaceID, recipients)
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO `+pmArchiveReceiptsTable+`(workspace_id,archive_path,sha256,size_bytes,recipients_fingerprint,schema_version,created_at) VALUES(?,?,?,?,?,?,?)`,
			written.WorkspaceID, written.ArchivePath, written.SHA256, written.SizeBytes, written.RecipientsFingerprint, written.SchemaVersion, written.CreatedAt); err != nil {
			return fmt.Errorf("record PM archive receipt: %w", err)
		}
	case receipt != nil:
		info, statErr := os.Stat(receipt.ArchivePath)
		if statErr != nil {
			return fmt.Errorf("PM archive receipt exists but its archive is missing: %w", statErr)
		}
		if !info.Mode().IsRegular() || info.Size() <= 0 {
			return errors.New("PM archive receipt exists but its archive is empty or not a regular file")
		}
	}
	// If productRows == 0 and there is no receipt, the product tables hold no
	// data. Dropping empty product-only tables loses nothing and keeps fresh or
	// already-cleaned workspaces bootable without backup credentials.

	if err := dropPMProductData(ctx, tx); err != nil {
		return err
	}
	return resolvePMWatcherArtifacts(ctx, tx)
}

func sqliteDatabasePath(ctx context.Context, tx *sql.Tx) (string, error) {
	rows, err := tx.QueryContext(ctx, `PRAGMA database_list`)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	for rows.Next() {
		var seq int
		var name, file string
		if err := rows.Scan(&seq, &name, &file); err != nil {
			return "", err
		}
		if name == "main" {
			return file, nil
		}
	}
	return "", rows.Err()
}

func loadPMArchiveReceipt(ctx context.Context, tx *sql.Tx) (*pmArchiveReceipt, error) {
	row := tx.QueryRowContext(ctx, `SELECT workspace_id,archive_path,sha256,size_bytes,recipients_fingerprint,schema_version,created_at FROM `+pmArchiveReceiptsTable+` ORDER BY created_at DESC LIMIT 1`)
	receipt := &pmArchiveReceipt{}
	err := row.Scan(&receipt.WorkspaceID, &receipt.ArchivePath, &receipt.SHA256, &receipt.SizeBytes, &receipt.RecipientsFingerprint, &receipt.SchemaVersion, &receipt.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return receipt, nil
}

// pmProductRowCounts returns the number of rows the removal would delete or
// drop. A zero count means there is no product data to protect with an archive.
func pmProductRowCounts(ctx context.Context, tx *sql.Tx) (int64, string, error) {
	var total int64
	workspaceID := ""
	exists, err := sqliteTableExists(ctx, tx, "pm_records")
	if err != nil {
		return 0, "", err
	}
	if exists {
		query := `SELECT COUNT(*),COALESCE(MAX(workspace_id),'') FROM pm_records WHERE kind IN (` + sqlPlaceholders(len(pmProductKinds)) + `)`
		args := make([]any, len(pmProductKinds))
		for i, kind := range pmProductKinds {
			args[i] = kind
		}
		var n int64
		var ws string
		if err := tx.QueryRowContext(ctx, query, args...).Scan(&n, &ws); err != nil {
			return 0, "", err
		}
		total += n
		if ws != "" {
			workspaceID = ws
		}
	}
	for _, table := range []string{"pm_presence", "pm_registration", "pm_onboarding_backfill"} {
		exists, err := sqliteTableExists(ctx, tx, table)
		if err != nil {
			return 0, "", err
		}
		if !exists {
			continue
		}
		var n int64
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+table).Scan(&n); err != nil {
			return 0, "", err
		}
		total += n
		if workspaceID == "" {
			var ws string
			if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(workspace_id),'') FROM `+table).Scan(&ws); err != nil {
				return 0, "", err
			}
			workspaceID = ws
		}
	}
	return total, workspaceID, nil
}

func sqlPlaceholders(n int) string {
	if n <= 0 {
		return ""
	}
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}

func dropPMProductData(ctx context.Context, tx *sql.Tx) error {
	if exists, err := sqliteTableExists(ctx, tx, "pm_records"); err != nil {
		return err
	} else if exists {
		args := make([]any, len(pmProductKinds))
		for i, kind := range pmProductKinds {
			args[i] = kind
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM pm_records WHERE kind IN (`+sqlPlaceholders(len(pmProductKinds))+`)`, args...); err != nil {
			return fmt.Errorf("delete PM product records: %w", err)
		}
	}
	for _, index := range pmProductIndexes {
		if _, err := tx.ExecContext(ctx, `DROP INDEX IF EXISTS `+index); err != nil {
			return fmt.Errorf("drop PM product index %s: %w", index, err)
		}
	}
	for _, table := range []string{"pm_presence", "pm_registration", "pm_onboarding_backfill"} {
		if _, err := tx.ExecContext(ctx, `DROP TABLE IF EXISTS `+table); err != nil {
			return fmt.Errorf("drop PM product table %s: %w", table, err)
		}
	}
	return nil
}

type pmArchivePart struct {
	Name    string   `json:"name"`
	Kind    string   `json:"kind"`
	Columns []string `json:"columns,omitempty"`
	Rows    int64    `json:"rows"`
	Bytes   int64    `json:"bytes"`
	SHA256  string   `json:"sha256"`
	SQL     string   `json:"sql,omitempty"`
}

type pmArchiveManifest struct {
	SchemaVersion int             `json:"schema_version"`
	WorkspaceID   string          `json:"workspace_id"`
	CreatedAt     string          `json:"created_at"`
	Tables        []pmArchivePart `json:"tables"`
	Views         []pmArchivePart `json:"views"`
}

func writePMArchive(ctx context.Context, tx *sql.Tx, databasePath, workspaceID string, recipients *backupage.Recipients) (*pmArchiveReceipt, error) {
	root := filepath.Dir(databasePath)
	staging, err := os.MkdirTemp(root, ".pm-archive-138-staging-*")
	if err != nil {
		return nil, fmt.Errorf("create PM archive staging directory: %w", err)
	}
	defer os.RemoveAll(staging)

	manifest := pmArchiveManifest{
		SchemaVersion: pmArchiveSchemaVersion,
		WorkspaceID:   workspaceID,
		CreatedAt:     time.Now().UTC().Format(time.RFC3339Nano),
	}
	tableNames, err := pmSchemaObjects(ctx, tx, "table")
	if err != nil {
		return nil, err
	}
	for _, table := range tableNames {
		if table == pmArchiveReceiptsTable {
			continue
		}
		part, err := dumpPMTable(ctx, tx, staging, table)
		if err != nil {
			return nil, err
		}
		manifest.Tables = append(manifest.Tables, part)
	}
	viewNames, err := pmSchemaObjects(ctx, tx, "view")
	if err != nil {
		return nil, err
	}
	for _, view := range viewNames {
		var definition string
		if err := tx.QueryRowContext(ctx, `SELECT COALESCE(sql,'') FROM sqlite_master WHERE type='view' AND name=?`, view).Scan(&definition); err != nil {
			return nil, err
		}
		manifest.Views = append(manifest.Views, pmArchivePart{Name: view, Kind: "view", SQL: definition})
	}
	manifestJSON, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(staging, "manifest.json"), manifestJSON, 0o600); err != nil {
		return nil, fmt.Errorf("write PM archive manifest: %w", err)
	}

	utc := time.Now().UTC().Format("20060102T150405Z")
	finalPath := filepath.Join(root, "state.pm-archive-138-"+utc+".tar.zst.age")
	tempFile, err := os.CreateTemp(root, ".state.pm-archive-138-*.tmp")
	if err != nil {
		return nil, fmt.Errorf("create PM archive temporary file: %w", err)
	}
	tempPath := tempFile.Name()
	removeTemp := true
	defer func() {
		_ = tempFile.Close()
		if removeTemp {
			_ = os.Remove(tempPath)
		}
	}()

	hasher := sha256.New()
	cipherSink := io.MultiWriter(tempFile, hasher)
	ageWriter, err := recipients.Encrypt(cipherSink)
	if err != nil {
		return nil, fmt.Errorf("initialize PM archive encryption: %w", err)
	}
	zstdWriter, err := zstd.NewWriter(ageWriter, zstd.WithEncoderLevel(zstd.SpeedDefault), zstd.WithEncoderConcurrency(1))
	if err != nil {
		_ = ageWriter.Close()
		return nil, fmt.Errorf("initialize PM archive compression: %w", err)
	}
	tarWriter := tar.NewWriter(zstdWriter)

	parts := append([]string{"manifest.json"}, partFileNames(manifest)...)
	for _, name := range parts {
		filePath := filepath.Join(staging, name)
		info, err := os.Stat(filePath)
		if err != nil {
			return nil, err
		}
		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return nil, err
		}
		header.Name = name
		header.Mode = 0o600
		if err := tarWriter.WriteHeader(header); err != nil {
			return nil, fmt.Errorf("write PM archive tar header: %w", err)
		}
		source, err := os.Open(filePath)
		if err != nil {
			return nil, err
		}
		_, copyErr := io.Copy(tarWriter, source)
		closeErr := source.Close()
		if copyErr != nil {
			return nil, fmt.Errorf("write PM archive part %s: %w", name, copyErr)
		}
		if closeErr != nil {
			return nil, closeErr
		}
	}
	if err := tarWriter.Close(); err != nil {
		return nil, fmt.Errorf("finish PM archive tar: %w", err)
	}
	if err := zstdWriter.Close(); err != nil {
		return nil, fmt.Errorf("finish PM archive compression: %w", err)
	}
	if err := ageWriter.Close(); err != nil {
		return nil, fmt.Errorf("finish PM archive encryption: %w", err)
	}
	if err := tempFile.Sync(); err != nil {
		return nil, fmt.Errorf("sync PM archive: %w", err)
	}
	if err := tempFile.Close(); err != nil {
		return nil, fmt.Errorf("close PM archive: %w", err)
	}

	cipherDigest := hex.EncodeToString(hasher.Sum(nil))
	info, err := os.Stat(tempPath)
	if err != nil {
		return nil, err
	}
	if err := verifyPMArchive(tempPath, info.Size(), cipherDigest); err != nil {
		return nil, err
	}
	if err := os.Rename(tempPath, finalPath); err != nil {
		return nil, fmt.Errorf("publish PM archive: %w", err)
	}
	removeTemp = false
	if dir, err := os.Open(root); err == nil {
		_ = dir.Sync()
		_ = dir.Close()
	}
	return &pmArchiveReceipt{
		WorkspaceID:           workspaceID,
		ArchivePath:           finalPath,
		SHA256:                cipherDigest,
		SizeBytes:             info.Size(),
		RecipientsFingerprint: recipients.Fingerprint(),
		SchemaVersion:         pmArchiveSchemaVersion,
		CreatedAt:             manifest.CreatedAt,
	}, nil
}

func partFileNames(manifest pmArchiveManifest) []string {
	names := make([]string, 0, len(manifest.Tables))
	for _, part := range manifest.Tables {
		names = append(names, part.Name)
	}
	return names
}

func verifyPMArchive(path string, size int64, expectedDigest string) error {
	if size <= 0 {
		return errors.New("PM archive verification failed: archive is empty")
	}
	if expectedDigest == "" {
		return errors.New("PM archive verification failed: missing ciphertext digest")
	}
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open PM archive for verification: %w", err)
	}
	defer file.Close()
	header := make([]byte, len(ageHeader))
	if _, err := io.ReadFull(file, header); err != nil {
		return fmt.Errorf("read PM archive header: %w", err)
	}
	if string(header) != ageHeader {
		return errors.New("PM archive verification failed: missing age header")
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("rewind PM archive for verification: %w", err)
	}
	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return fmt.Errorf("hash PM archive: %w", err)
	}
	if actual := hex.EncodeToString(hasher.Sum(nil)); actual != expectedDigest {
		return errors.New("PM archive verification failed: ciphertext digest mismatch")
	}
	return nil
}

func pmSchemaObjects(ctx context.Context, tx *sql.Tx, kind string) ([]string, error) {
	rows, err := tx.QueryContext(ctx, `SELECT name FROM sqlite_master WHERE type=? AND name LIKE 'pm\_%' ESCAPE '\' ORDER BY name`, kind)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		names = append(names, name)
	}
	return names, rows.Err()
}

func dumpPMTable(ctx context.Context, tx *sql.Tx, staging, table string) (pmArchivePart, error) {
	rows, err := tx.QueryContext(ctx, `SELECT * FROM "`+strings.ReplaceAll(table, `"`, `""`)+`"`)
	if err != nil {
		return pmArchivePart{}, fmt.Errorf("read PM archive table %s: %w", table, err)
	}
	defer rows.Close()
	columns, err := rows.Columns()
	if err != nil {
		return pmArchivePart{}, err
	}
	var ddl string
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(sql,'') FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&ddl); err != nil {
		return pmArchivePart{}, err
	}
	fileName := table + ".jsonl"
	file, err := os.OpenFile(filepath.Join(staging, fileName), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return pmArchivePart{}, err
	}
	defer file.Close()
	hasher := sha256.New()
	writer := io.MultiWriter(file, hasher)
	encoder := json.NewEncoder(writer)
	var count, size int64
	for rows.Next() {
		values := make([]any, len(columns))
		destinations := make([]any, len(columns))
		for i := range values {
			destinations[i] = &values[i]
		}
		if err := rows.Scan(destinations...); err != nil {
			return pmArchivePart{}, err
		}
		record := make(map[string]any, len(columns))
		for i, column := range columns {
			switch value := values[i].(type) {
			case []byte:
				record[column] = string(value)
			default:
				record[column] = value
			}
		}
		if err := encoder.Encode(record); err != nil {
			return pmArchivePart{}, err
		}
		encoded, err := json.Marshal(record)
		if err != nil {
			return pmArchivePart{}, err
		}
		count++
		size += int64(len(encoded) + 1)
	}
	if err := rows.Err(); err != nil {
		return pmArchivePart{}, err
	}
	if err := file.Sync(); err != nil {
		return pmArchivePart{}, err
	}
	if err := file.Close(); err != nil {
		return pmArchivePart{}, err
	}
	return pmArchivePart{
		Name:    fileName,
		Kind:    "table",
		Columns: columns,
		Rows:    count,
		Bytes:   size,
		SHA256:  hex.EncodeToString(hasher.Sum(nil)),
		SQL:     ddl,
	}, nil
}

// resolvePMWatcherArtifacts withdraws open PM watcher asks and archives the
// "PM health" threads created by the retired product. Matching is by the
// identifiers the watcher writer used, and it is idempotent.
func resolvePMWatcherArtifacts(ctx context.Context, tx *sql.Tx) error {
	if err := withdrawPMWatcherAsks(ctx, tx); err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if exists, err := sqliteTableExists(ctx, tx, "threads"); err != nil {
		return err
	} else if exists {
		if _, err := tx.ExecContext(ctx, `UPDATE threads SET archived_at=?,archived_by='system',trash_reason='PM product removed by migration 138' WHERE json_extract(body_json,'$.title')='PM health' AND COALESCE(archived_at,'')='' AND COALESCE(trashed_at,'')=''`, now); err != nil {
			return fmt.Errorf("archive PM health threads: %w", err)
		}
	}
	if exists, err := sqliteTableExists(ctx, tx, "watcher_health"); err != nil {
		return err
	} else if exists {
		if _, err := tx.ExecContext(ctx, `UPDATE watcher_health SET unhealthy_since='',level='',recipient='',ask_id='',raise_count=0,next_raise_at='',thread_id='' WHERE alarm_key='pm'`); err != nil {
			return fmt.Errorf("clear PM watcher health alarm: %w", err)
		}
	}
	return nil
}

func withdrawPMWatcherAsks(ctx context.Context, tx *sql.Tx) error {
	exists, err := sqliteTableExists(ctx, tx, "events")
	if err != nil || !exists {
		return err
	}
	exists, err = sqliteTableExists(ctx, tx, "human_attention_request_resolutions")
	if err != nil || !exists {
		return err
	}
	rows, err := tx.QueryContext(ctx, `SELECT ask.id,COALESCE(ask.thread_id,''),COALESCE(json_extract(ask.payload_json,'$.payload.requester_actor_id'),ask.actor_id),COALESCE(json_extract(ask.payload_json,'$.payload.subject_ref'),''),COALESCE(ask.refs_json,'[]')
		FROM events AS ask
		WHERE ask.type='human_attention_requested'
		  AND (trim(COALESCE(json_extract(ask.payload_json,'$.payload.watcher_health_kind'),''))='pm'
		       OR trim(COALESCE(json_extract(ask.payload_json,'$.payload.title'),''))='Restore the workspace PM?')
		  AND NOT EXISTS (SELECT 1 FROM human_attention_request_resolutions AS resolution WHERE resolution.request_event_id=ask.id)`)
	if err != nil {
		return fmt.Errorf("find PM watcher asks: %w", err)
	}
	type pmAsk struct {
		id, thread, requester, subject, refsJSON string
	}
	var asks []pmAsk
	for rows.Next() {
		var ask pmAsk
		if err := rows.Scan(&ask.id, &ask.thread, &ask.requester, &ask.subject, &ask.refsJSON); err != nil {
			rows.Close()
			return err
		}
		asks = append(asks, ask)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, ask := range asks {
		var refs []string
		if err := json.Unmarshal([]byte(ask.refsJSON), &refs); err != nil {
			refs = nil
		}
		refs = appendUniqueString(refs, "event:"+ask.id)
		if ask.thread != "" {
			refs = appendUniqueString(refs, "thread:"+ask.thread)
		}
		if ask.subject != "" {
			refs = appendUniqueString(refs, ask.subject)
		}
		eventID := "pm-archive-138-withdraw-" + ask.id
		now := time.Now().UTC().Format(time.RFC3339Nano)
		body := map[string]any{
			"id":        eventID,
			"type":      "human_attention_withdrawn",
			"ts":        now,
			"actor_id":  "system",
			"thread_id": ask.thread,
			"refs":      refs,
			"payload": map[string]any{
				"request_event_id":      ask.id,
				"request_event_ref":     "event:" + ask.id,
				"requester_actor_id":    ask.requester,
				"withdrawn_by_actor_id": "system",
				"reason":                "The built-in PM product was removed; this watcher card is no longer actionable.",
			},
		}
		refsJSON, err := json.Marshal(refs)
		if err != nil {
			return err
		}
		payloadJSON, err := json.Marshal(body)
		if err != nil {
			return err
		}
		handle := handles.Candidate("human_attention_withdrawn", "event-"+eventID)
		if len(handle) > 31 {
			handle = strings.TrimRight(handle[:31], "-")
		}
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO events(id,handle,type,ts,actor_id,thread_id,refs_json,payload_json,created_at) VALUES(?,?,?,?,?,?,?,?,CURRENT_TIMESTAMP)`,
			eventID, handle, "human_attention_withdrawn", now, "system", ask.thread, string(refsJSON), string(payloadJSON)); err != nil {
			return fmt.Errorf("write PM watcher withdrawal: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO human_attention_request_resolutions(request_event_id,resolution_event_id,resolution_type,actor_id,created_at) VALUES(?,?,'withdrawn','system',?)`,
			ask.id, eventID, now); err != nil {
			return fmt.Errorf("resolve PM watcher ask: %w", err)
		}
		if exists, err := sqliteTableExists(ctx, tx, "ask_subjects"); err != nil {
			return err
		} else if exists {
			if _, err := tx.ExecContext(ctx, `UPDATE ask_subjects SET open=0 WHERE ask_id=?`, ask.id); err != nil {
				return err
			}
		}
	}
	return nil
}

func appendUniqueString(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}
