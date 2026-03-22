package controlplane

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	defaultWorkspaceBackupScheduleName   = "nightly"
	defaultWorkspaceBackupInterval       = 24 * time.Hour
	defaultWorkspaceBackupRetentionDays  = 30
	workspaceBackupRetryBackoff          = time.Hour
	workspaceBackupScheduleBatchSize     = 100
	workspaceBackupMaintenanceBatchLimit = 100
)

type workspaceBackupScheduleRow struct {
	WorkspaceID       string
	OrganizationID    string
	ScheduleName      string
	Enabled           bool
	IntervalSeconds   int
	RetentionDays     int
	NextRunAt         string
	LastRunAt         *string
	LastStatus        string
	LastFailureReason *string
	LastJobID         *string
	CreatedAt         string
	UpdatedAt         string
}

type workspaceBackupRunRow struct {
	ID                 string
	OrganizationID     string
	WorkspaceID        string
	ProvisioningJobID  string
	ScheduleName       string
	BackupDir          string
	RetentionDays      int
	Status             string
	RequestedAt        string
	StartedAt          *string
	FinishedAt         *string
	FailureReason      *string
	RetentionExpiresAt *string
	PrunedAt           *string
	PruneFailureReason *string
	CreatedAt          string
	UpdatedAt          string
}

func (s *Service) RunBackupMaintenancePass(ctx context.Context) error {
	if err := s.ensureWorkspaceBackupSchedules(ctx); err != nil {
		return err
	}

	for {
		now := s.now().UTC()
		dueSchedules, err := s.listDueWorkspaceBackupSchedules(ctx, now.Format(time.RFC3339Nano), workspaceBackupMaintenanceBatchLimit)
		if err != nil {
			return err
		}
		if len(dueSchedules) == 0 {
			break
		}

		for _, schedule := range dueSchedules {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			workspace, err := s.loadWorkspaceByID(ctx, schedule.WorkspaceID)
			if err != nil {
				return err
			}
			if workspace.Status != "ready" {
				continue
			}
			if _, _, err := s.runWorkspaceBackupJob(ctx, workspace, schedule.ScheduleName, schedule.RetentionDays, nil, true); err != nil {
				if apiErr, ok := err.(*APIError); ok && apiErr.Status == http.StatusConflict {
					if skipErr := s.deferWorkspaceBackupSchedule(ctx, schedule, apiErr.Message); skipErr != nil {
						return skipErr
					}
					continue
				}
				return err
			}
		}
	}

	return s.pruneExpiredWorkspaceBackupRuns(ctx)
}

func (s *Service) ensureWorkspaceBackupSchedules(ctx context.Context) error {
	now := s.now().UTC()
	nowText := now.Format(time.RFC3339Nano)
	nextRunAt := now.Add(defaultWorkspaceBackupInterval).Format(time.RFC3339Nano)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return internalError("failed to begin workspace backup schedule seeding")
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(
		ctx,
		`INSERT OR IGNORE INTO workspace_backup_schedules(
			workspace_id, organization_id, schedule_name, enabled, interval_seconds, retention_days,
			next_run_at, last_run_at, last_status, last_failure_reason, last_job_id, created_at, updated_at
		)
		SELECT id, organization_id, ?, 1, ?, ?, ?, NULL, '', NULL, '', ?, ?
		FROM workspaces`,
		defaultWorkspaceBackupScheduleName,
		int(defaultWorkspaceBackupInterval/time.Second),
		defaultWorkspaceBackupRetentionDays,
		nextRunAt,
		nowText,
		nowText,
	); err != nil {
		return internalError("failed to seed workspace backup schedules")
	}
	if err := tx.Commit(); err != nil {
		return internalError("failed to commit workspace backup schedule seeding")
	}
	return nil
}

func (s *Service) deferWorkspaceBackupSchedule(ctx context.Context, schedule workspaceBackupScheduleRow, reason string) error {
	now := s.now().UTC()
	nowText := now.Format(time.RFC3339Nano)
	nextRunAt := now.Add(workspaceBackupRetryBackoff).Format(time.RFC3339Nano)
	_, err := s.db.ExecContext(
		ctx,
		`UPDATE workspace_backup_schedules
		 SET next_run_at = ?, last_run_at = ?, last_status = ?, last_failure_reason = ?, updated_at = ?
		 WHERE workspace_id = ?`,
		nextRunAt,
		nowText,
		"skipped",
		strings.TrimSpace(reason),
		nowText,
		schedule.WorkspaceID,
	)
	return err
}

func (s *Service) insertWorkspaceBackupScheduleTx(ctx context.Context, tx *sql.Tx, workspace Workspace, createdAt string) error {
	nextRunAt := s.now().UTC().Add(defaultWorkspaceBackupInterval).Format(time.RFC3339Nano)
	_, err := tx.ExecContext(
		ctx,
		`INSERT INTO workspace_backup_schedules(
			workspace_id, organization_id, schedule_name, enabled, interval_seconds, retention_days,
			next_run_at, last_run_at, last_status, last_failure_reason, last_job_id, created_at, updated_at
		) VALUES (?, ?, ?, 1, ?, ?, ?, NULL, '', NULL, '', ?, ?)`,
		workspace.ID,
		workspace.OrganizationID,
		defaultWorkspaceBackupScheduleName,
		int(defaultWorkspaceBackupInterval/time.Second),
		defaultWorkspaceBackupRetentionDays,
		nextRunAt,
		createdAt,
		createdAt,
	)
	return err
}

func (s *Service) listDueWorkspaceBackupSchedules(ctx context.Context, nowText string, limit int) ([]workspaceBackupScheduleRow, error) {
	if limit <= 0 {
		limit = workspaceBackupScheduleBatchSize
	}
	rows, err := s.db.QueryContext(
		ctx,
		`SELECT s.workspace_id, s.organization_id, s.schedule_name, s.enabled, s.interval_seconds, s.retention_days, s.next_run_at,
			s.last_run_at, s.last_status, s.last_failure_reason, s.last_job_id, s.created_at, s.updated_at
		 FROM workspace_backup_schedules s
		 JOIN workspaces w ON w.id = s.workspace_id
		 WHERE s.enabled = 1 AND w.status = 'ready' AND s.next_run_at <= ?
		 ORDER BY s.next_run_at ASC, s.workspace_id ASC
		 LIMIT ?`,
		nowText,
		limit,
	)
	if err != nil {
		return nil, internalError("failed to list due workspace backup schedules")
	}
	defer rows.Close()

	var schedules []workspaceBackupScheduleRow
	for rows.Next() {
		schedule, err := scanWorkspaceBackupScheduleRow(rows)
		if err != nil {
			return nil, internalError("failed to scan workspace backup schedule")
		}
		schedules = append(schedules, schedule)
	}
	if err := rows.Err(); err != nil {
		return nil, internalError("failed to iterate workspace backup schedules")
	}
	return schedules, nil
}

func scanWorkspaceBackupScheduleRow(scanner rowScanner) (workspaceBackupScheduleRow, error) {
	var (
		schedule          workspaceBackupScheduleRow
		enabled           sql.NullInt64
		intervalSeconds   sql.NullInt64
		retentionDays     sql.NullInt64
		lastRunAt         sql.NullString
		lastStatus        sql.NullString
		lastFailureReason sql.NullString
		lastJobID         sql.NullString
	)
	if err := scanner.Scan(
		&schedule.WorkspaceID,
		&schedule.OrganizationID,
		&schedule.ScheduleName,
		&enabled,
		&intervalSeconds,
		&retentionDays,
		&schedule.NextRunAt,
		&lastRunAt,
		&lastStatus,
		&lastFailureReason,
		&lastJobID,
		&schedule.CreatedAt,
		&schedule.UpdatedAt,
	); err != nil {
		return workspaceBackupScheduleRow{}, err
	}
	schedule.Enabled = !enabled.Valid || enabled.Int64 != 0
	if intervalSeconds.Valid {
		schedule.IntervalSeconds = int(intervalSeconds.Int64)
	}
	if retentionDays.Valid {
		schedule.RetentionDays = int(retentionDays.Int64)
	}
	schedule.LastRunAt = nullableString(lastRunAt)
	schedule.LastStatus = stringFromNullString(lastStatus)
	schedule.LastFailureReason = nullableString(lastFailureReason)
	schedule.LastJobID = nullableString(lastJobID)
	return schedule, nil
}

func (s *Service) runWorkspaceBackupJob(ctx context.Context, workspace Workspace, scheduleName string, retentionDays int, actorAccountID *string, updateSchedule bool) (Workspace, ProvisioningJob, error) {
	scheduleName = normalizeBackupScheduleName(scheduleName)
	if retentionDays < 0 {
		return Workspace{}, ProvisioningJob{}, invalidRequest("retention_days must be zero or positive")
	}
	if retentionDays == 0 {
		retentionDays = defaultWorkspaceBackupRetentionDays
	}
	if workspace.Status == "archived" {
		return Workspace{}, ProvisioningJob{}, &APIError{Status: http.StatusConflict, Code: "workspace_not_ready", Message: "archived workspaces cannot be backed up"}
	}
	running, err := s.workspaceHasRunningBackupJob(ctx, workspace.ID)
	if err != nil {
		return Workspace{}, ProvisioningJob{}, err
	}
	if running {
		return Workspace{}, ProvisioningJob{}, &APIError{Status: http.StatusConflict, Code: "backup_in_progress", Message: "workspace backup is already running"}
	}

	now := s.now().UTC()
	nowText := now.Format(time.RFC3339Nano)
	outputDir := filepath.Join(workspace.DeploymentRoot, "backups", fmt.Sprintf("%s-%s", workspace.Slug, now.Format("20060102T150405Z")))
	job := ProvisioningJob{
		ID:              "job_" + uuid.NewString(),
		OrganizationID:  workspace.OrganizationID,
		WorkspaceID:     workspace.ID,
		Kind:            "workspace_backup",
		Status:          "running",
		RequestedAt:     nowText,
		StartedAt:       stringPtr(nowText),
		ProgressMessage: "workspace backup started",
		Retryable:       true,
		Parameters: map[string]any{
			"backup_dir":     outputDir,
			"schedule_name":  scheduleName,
			"retention_days": retentionDays,
			"instance_root":  workspace.DeploymentRoot,
		},
	}
	run := workspaceBackupRunRow{
		ID:                "backup_run_" + uuid.NewString(),
		OrganizationID:    workspace.OrganizationID,
		WorkspaceID:       workspace.ID,
		ProvisioningJobID: job.ID,
		ScheduleName:      scheduleName,
		BackupDir:         outputDir,
		RetentionDays:     retentionDays,
		Status:            "running",
		RequestedAt:       nowText,
		StartedAt:         stringPtr(nowText),
		CreatedAt:         nowText,
		UpdatedAt:         nowText,
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Workspace{}, ProvisioningJob{}, internalError("failed to begin backup job")
	}
	defer tx.Rollback()
	if err := s.insertProvisioningJob(ctx, tx, job); err != nil {
		if isSQLiteConstraint(err) {
			return Workspace{}, ProvisioningJob{}, &APIError{Status: http.StatusConflict, Code: "backup_in_progress", Message: "workspace backup is already running"}
		}
		return Workspace{}, ProvisioningJob{}, internalError("failed to create backup job")
	}
	if err := s.insertWorkspaceBackupRunTx(ctx, tx, run); err != nil {
		return Workspace{}, ProvisioningJob{}, internalError("failed to create backup history record")
	}
	if err := insertAuditEventTx(ctx, tx, AuditEvent{
		ID:             "audit_" + uuid.NewString(),
		EventType:      "workspace_backup_job_recorded",
		OrganizationID: stringPtr(workspace.OrganizationID),
		WorkspaceID:    stringPtr(workspace.ID),
		TargetType:     "provisioning_job",
		TargetID:       job.ID,
		OccurredAt:     nowText,
		Metadata: map[string]any{
			"kind":           job.Kind,
			"schedule_name":  scheduleName,
			"retention_days": retentionDays,
		},
	}, actorAccountID); err != nil {
		return Workspace{}, ProvisioningJob{}, err
	}
	if err := tx.Commit(); err != nil {
		return Workspace{}, ProvisioningJob{}, internalError("failed to commit backup job start")
	}

	backupResult, backupErr := s.runBackupWorkspaceScript(ctx, workspace, outputDir)
	finishedAt := s.now().UTC()
	finishedText := finishedAt.Format(time.RFC3339Nano)
	if backupErr == nil {
		workspace.LastSuccessfulBackupAt = stringPtr(finishedText)
		workspace.UpdatedAt = finishedText
		job.Status = "succeeded"
		job.FinishedAt = stringPtr(finishedText)
		job.ProgressMessage = "workspace backup completed"
		job.StdoutTail = backupResult.StdoutTail
		job.StderrTail = backupResult.StderrTail
		job.Retryable = false
		job.Result = map[string]any{
			"backup_dir":     outputDir,
			"retention_days": retentionDays,
			"exit_code":      backupResult.ExitCode,
		}
		run.Status = "succeeded"
		run.FinishedAt = stringPtr(finishedText)
		run.RetentionExpiresAt = stringPtr(finishedAt.Add(time.Duration(retentionDays) * 24 * time.Hour).Format(time.RFC3339Nano))
	} else {
		job.Status = "failed"
		job.FinishedAt = stringPtr(finishedText)
		job.FailureReason = stringPtr(strings.TrimSpace(backupErr.Error()))
		job.ProgressMessage = "workspace backup failed"
		job.StdoutTail = backupResult.StdoutTail
		job.StderrTail = backupResult.StderrTail
		job.Retryable = true
		job.Result = map[string]any{
			"backup_dir":     outputDir,
			"retention_days": retentionDays,
			"exit_code":      backupResult.ExitCode,
		}
		run.Status = "failed"
		run.FinishedAt = stringPtr(finishedText)
		run.FailureReason = stringPtr(strings.TrimSpace(backupErr.Error()))
	}
	run.UpdatedAt = finishedText

	tx, err = s.db.BeginTx(ctx, nil)
	if err != nil {
		return Workspace{}, ProvisioningJob{}, internalError("failed to begin backup job finalization")
	}
	defer tx.Rollback()
	if backupErr == nil {
		if err := s.persistWorkspaceRoutingManifest(ctx, tx, workspace); err != nil {
			return Workspace{}, ProvisioningJob{}, err
		}
	}
	if err := s.updateProvisioningJob(ctx, tx, job); err != nil {
		return Workspace{}, ProvisioningJob{}, internalError("failed to update backup job")
	}
	if err := s.updateWorkspaceBackupRunTx(ctx, tx, run); err != nil {
		return Workspace{}, ProvisioningJob{}, internalError("failed to update backup history record")
	}
	if updateSchedule {
		schedule := workspaceBackupScheduleRow{
			WorkspaceID:     workspace.ID,
			OrganizationID:  workspace.OrganizationID,
			ScheduleName:    scheduleName,
			IntervalSeconds: int(defaultWorkspaceBackupInterval / time.Second),
			RetentionDays:   retentionDays,
		}
		if err := s.updateWorkspaceBackupScheduleTx(ctx, tx, schedule, job, backupErr == nil, finishedText); err != nil {
			return Workspace{}, ProvisioningJob{}, internalError("failed to update backup schedule")
		}
	}
	if err := insertAuditEventTx(ctx, tx, AuditEvent{
		ID:             "audit_" + uuid.NewString(),
		EventType:      "workspace_backup_job_finished",
		OrganizationID: stringPtr(workspace.OrganizationID),
		WorkspaceID:    stringPtr(workspace.ID),
		TargetType:     "provisioning_job",
		TargetID:       job.ID,
		OccurredAt:     finishedText,
		Metadata: map[string]any{
			"status":         job.Status,
			"kind":           job.Kind,
			"schedule_name":  scheduleName,
			"retention_days": retentionDays,
		},
	}, actorAccountID); err != nil {
		return Workspace{}, ProvisioningJob{}, err
	}
	if err := tx.Commit(); err != nil {
		return Workspace{}, ProvisioningJob{}, internalError("failed to commit backup finalization")
	}
	return workspace, job, nil
}

func (s *Service) updateWorkspaceBackupScheduleTx(ctx context.Context, tx *sql.Tx, schedule workspaceBackupScheduleRow, job ProvisioningJob, succeeded bool, finishedAt string) error {
	nextRunAt := s.now().UTC().Add(workspaceBackupRetryBackoff)
	if succeeded {
		interval := time.Duration(schedule.IntervalSeconds) * time.Second
		if interval <= 0 {
			interval = defaultWorkspaceBackupInterval
		}
		nextRunAt = s.now().UTC().Add(interval)
	}
	var failureReason any = nil
	if job.FailureReason != nil && strings.TrimSpace(*job.FailureReason) != "" {
		failureReason = strings.TrimSpace(*job.FailureReason)
	}
	_, err := tx.ExecContext(
		ctx,
		`UPDATE workspace_backup_schedules
		 SET next_run_at = ?, last_run_at = ?, last_status = ?, last_failure_reason = ?, last_job_id = ?, updated_at = ?
		 WHERE workspace_id = ?`,
		nextRunAt.Format(time.RFC3339Nano),
		finishedAt,
		job.Status,
		failureReason,
		job.ID,
		finishedAt,
		schedule.WorkspaceID,
	)
	return err
}

func (s *Service) insertWorkspaceBackupRunTx(ctx context.Context, tx *sql.Tx, run workspaceBackupRunRow) error {
	_, err := tx.ExecContext(
		ctx,
		`INSERT INTO workspace_backup_runs(
			id, organization_id, workspace_id, provisioning_job_id, schedule_name, backup_dir, retention_days,
			status, requested_at, started_at, finished_at, failure_reason, retention_expires_at, pruned_at,
			prune_failure_reason, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		run.ID,
		run.OrganizationID,
		run.WorkspaceID,
		run.ProvisioningJobID,
		run.ScheduleName,
		run.BackupDir,
		run.RetentionDays,
		run.Status,
		run.RequestedAt,
		nullStringValue(run.StartedAt),
		nullStringValue(run.FinishedAt),
		nullStringValue(run.FailureReason),
		nullStringValue(run.RetentionExpiresAt),
		nullStringValue(run.PrunedAt),
		nullStringValue(run.PruneFailureReason),
		run.CreatedAt,
		run.UpdatedAt,
	)
	return err
}

func (s *Service) updateWorkspaceBackupRunTx(ctx context.Context, tx *sql.Tx, run workspaceBackupRunRow) error {
	_, err := tx.ExecContext(
		ctx,
		`UPDATE workspace_backup_runs
		 SET status = ?, started_at = ?, finished_at = ?, failure_reason = ?, retention_expires_at = ?, pruned_at = ?, prune_failure_reason = ?, updated_at = ?
		 WHERE id = ?`,
		run.Status,
		nullStringValue(run.StartedAt),
		nullStringValue(run.FinishedAt),
		nullStringValue(run.FailureReason),
		nullStringValue(run.RetentionExpiresAt),
		nullStringValue(run.PrunedAt),
		nullStringValue(run.PruneFailureReason),
		run.UpdatedAt,
		run.ID,
	)
	return err
}

func (s *Service) workspaceHasRunningBackupJob(ctx context.Context, workspaceID string) (bool, error) {
	row := s.db.QueryRowContext(ctx, `SELECT 1 FROM provisioning_jobs WHERE workspace_id = ? AND kind = 'workspace_backup' AND status = 'running' LIMIT 1`, workspaceID)
	var one int
	if err := row.Scan(&one); err != nil {
		if err == sql.ErrNoRows {
			return false, nil
		}
		return false, internalError("failed to check workspace backup job state")
	}
	return true, nil
}

func (s *Service) pruneExpiredWorkspaceBackupRuns(ctx context.Context) error {
	nowText := s.now().UTC().Format(time.RFC3339Nano)
	rows, err := s.db.QueryContext(
		ctx,
		`SELECT id, organization_id, workspace_id, provisioning_job_id, schedule_name, backup_dir, retention_days,
			status, requested_at, started_at, finished_at, failure_reason, retention_expires_at, pruned_at,
			prune_failure_reason, created_at, updated_at
		 FROM workspace_backup_runs
		 WHERE status = 'succeeded' AND pruned_at IS NULL AND retention_expires_at <= ?
		 ORDER BY retention_expires_at ASC, requested_at ASC, id ASC
		 LIMIT ?`,
		nowText,
		workspaceBackupMaintenanceBatchLimit,
	)
	if err != nil {
		return internalError("failed to list expired workspace backup runs")
	}
	defer rows.Close()

	var runs []workspaceBackupRunRow
	for rows.Next() {
		run, err := scanWorkspaceBackupRunRow(rows)
		if err != nil {
			return internalError("failed to scan workspace backup run")
		}
		runs = append(runs, run)
	}
	if err := rows.Err(); err != nil {
		return internalError("failed to iterate workspace backup runs")
	}

	for _, run := range runs {
		if err := os.RemoveAll(run.BackupDir); err != nil {
			tx, txErr := s.db.BeginTx(ctx, nil)
			if txErr != nil {
				return internalError("failed to begin backup prune failure update")
			}
			if _, execErr := tx.ExecContext(
				ctx,
				`UPDATE workspace_backup_runs
				 SET prune_failure_reason = ?, updated_at = ?
				 WHERE id = ?`,
				strings.TrimSpace(err.Error()),
				nowText,
				run.ID,
			); execErr != nil {
				_ = tx.Rollback()
				return internalError("failed to record backup prune failure")
			}
			if err := tx.Commit(); err != nil {
				return internalError("failed to commit backup prune failure")
			}
			continue
		}

		tx, txErr := s.db.BeginTx(ctx, nil)
		if txErr != nil {
			return internalError("failed to begin backup prune update")
		}
		if _, execErr := tx.ExecContext(
			ctx,
			`UPDATE workspace_backup_runs
			 SET pruned_at = ?, prune_failure_reason = NULL, updated_at = ?
			 WHERE id = ?`,
			nowText,
			nowText,
			run.ID,
		); execErr != nil {
			_ = tx.Rollback()
			return internalError("failed to update backup prune state")
		}
		if err := tx.Commit(); err != nil {
			return internalError("failed to commit backup prune state")
		}
	}

	return nil
}

func scanWorkspaceBackupRunRow(scanner rowScanner) (workspaceBackupRunRow, error) {
	var (
		run                workspaceBackupRunRow
		startedAt          sql.NullString
		finishedAt         sql.NullString
		failureReason      sql.NullString
		retentionExpiresAt sql.NullString
		prunedAt           sql.NullString
		pruneFailureReason sql.NullString
	)
	if err := scanner.Scan(
		&run.ID,
		&run.OrganizationID,
		&run.WorkspaceID,
		&run.ProvisioningJobID,
		&run.ScheduleName,
		&run.BackupDir,
		&run.RetentionDays,
		&run.Status,
		&run.RequestedAt,
		&startedAt,
		&finishedAt,
		&failureReason,
		&retentionExpiresAt,
		&prunedAt,
		&pruneFailureReason,
		&run.CreatedAt,
		&run.UpdatedAt,
	); err != nil {
		return workspaceBackupRunRow{}, err
	}
	run.StartedAt = nullableString(startedAt)
	run.FinishedAt = nullableString(finishedAt)
	run.FailureReason = nullableString(failureReason)
	run.RetentionExpiresAt = nullableString(retentionExpiresAt)
	run.PrunedAt = nullableString(prunedAt)
	run.PruneFailureReason = nullableString(pruneFailureReason)
	return run, nil
}

func normalizeBackupScheduleName(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "manual"
	}
	return raw
}
