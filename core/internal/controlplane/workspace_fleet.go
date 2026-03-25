package controlplane

import (
	"context"
	"database/sql"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"organization-autorunner-core/internal/controlplaneauth"

	"github.com/google/uuid"
)

func (s *Service) RecordWorkspaceHeartbeat(ctx context.Context, token string, workspaceID string, payload WorkspaceHeartbeatRequest) (Workspace, error) {
	workspace, err := s.loadWorkspaceByID(ctx, workspaceID)
	if err != nil {
		return Workspace{}, err
	}
	if strings.TrimSpace(workspace.ServiceIdentityID) == "" || strings.TrimSpace(workspace.ServiceIdentityPublicKey) == "" {
		return Workspace{}, &APIError{Status: http.StatusServiceUnavailable, Code: "service_unavailable", Message: "workspace service identity is not configured"}
	}
	publicKey, err := controlplaneauth.ParseEd25519PublicKeyBase64(workspace.ServiceIdentityPublicKey)
	if err != nil {
		return Workspace{}, internalError("workspace service identity public key is invalid")
	}
	verifier, err := controlplaneauth.NewWorkspaceServiceAssertionVerifier(controlplaneauth.WorkspaceServiceAssertionVerifierConfig{
		IdentityID:  workspace.ServiceIdentityID,
		WorkspaceID: workspace.ID,
		Audience:    controlplaneauth.WorkspaceServiceAssertionAudience,
		PublicKey:   publicKey,
	})
	if err != nil {
		return Workspace{}, internalError("failed to configure workspace service assertion verifier")
	}
	if _, err := verifier.Verify(token); err != nil {
		return Workspace{}, &APIError{Status: http.StatusUnauthorized, Code: "invalid_token", Message: err.Error()}
	}
	version := strings.TrimSpace(payload.Version)
	build := strings.TrimSpace(payload.Build)
	if version == "" {
		return Workspace{}, invalidRequest("version is required")
	}
	if build == "" {
		return Workspace{}, invalidRequest("build is required")
	}
	if payload.HealthSummary == nil {
		return Workspace{}, invalidRequest("health_summary is required")
	}
	if payload.ProjectionMaintenanceSummary == nil {
		return Workspace{}, invalidRequest("projection_maintenance_summary is required")
	}
	if payload.UsageSummary == nil {
		return Workspace{}, invalidRequest("usage_summary is required")
	}

	nowText := s.now().Format(time.RFC3339Nano)
	workspace.DeployedVersion = version
	workspace.HeartbeatVersion = version
	workspace.HeartbeatBuild = build
	workspace.HeartbeatHealthSummary = payload.HealthSummary
	workspace.HeartbeatProjectionMaintenanceSummary = payload.ProjectionMaintenanceSummary
	workspace.HeartbeatUsageSummary = payload.UsageSummary
	workspace.LastHeartbeatAt = stringPtr(nowText)
	if payload.LastSuccessfulBackupAt != nil {
		value := strings.TrimSpace(*payload.LastSuccessfulBackupAt)
		if value != "" {
			workspace.LastSuccessfulBackupAt = stringPtr(value)
		}
	}
	workspace.UpdatedAt = nowText

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Workspace{}, internalError("failed to begin heartbeat transaction")
	}
	defer tx.Rollback()
	if err := s.persistWorkspaceRoutingManifest(ctx, tx, workspace); err != nil {
		return Workspace{}, err
	}
	if err := insertAuditEventTx(ctx, tx, AuditEvent{
		ID:             "audit_" + uuid.NewString(),
		EventType:      "workspace_heartbeat_recorded",
		OrganizationID: stringPtr(workspace.OrganizationID),
		WorkspaceID:    stringPtr(workspace.ID),
		TargetType:     "workspace",
		TargetID:       workspace.ID,
		OccurredAt:     nowText,
		Metadata: map[string]any{
			"version": version,
			"build":   build,
		},
	}, nil); err != nil {
		return Workspace{}, err
	}
	if err := tx.Commit(); err != nil {
		return Workspace{}, internalError("failed to commit heartbeat")
	}
	return workspace, nil
}

func (s *Service) ListWorkspaceInventory(ctx context.Context, identity RequestIdentity, organizationID string, page PageRequest) (Page[WorkspaceInventoryItem], error) {
	workspacePage, err := s.ListWorkspaces(ctx, identity, organizationID, page)
	if err != nil {
		return Page[WorkspaceInventoryItem]{}, err
	}
	items := make([]WorkspaceInventoryItem, 0, len(workspacePage.Items))
	if len(workspacePage.Items) == 0 {
		return Page[WorkspaceInventoryItem]{Items: items, NextCursor: workspacePage.NextCursor}, nil
	}

	workspaceIDs := make([]string, 0, len(workspacePage.Items))
	for _, workspace := range workspacePage.Items {
		workspaceIDs = append(workspaceIDs, workspace.ID)
	}

	placeholderParts := make([]string, 0, len(workspaceIDs))
	args := []any{organizationID}
	for _, workspaceID := range workspaceIDs {
		placeholderParts = append(placeholderParts, "?")
		args = append(args, workspaceID)
	}
	query := `SELECT id, organization_id, workspace_id, kind, status, requested_at, started_at, finished_at, failure_reason, progress_message, stdout_tail, stderr_tail, retryable, parameters_json, result_json
		FROM provisioning_jobs
		WHERE organization_id = ? AND status = 'failed' AND retryable = 1`
	if len(placeholderParts) > 0 {
		query += ` AND workspace_id IN (` + strings.Join(placeholderParts, ",") + `)`
	}
	query += ` ORDER BY requested_at DESC, id DESC`

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return Page[WorkspaceInventoryItem]{}, internalError("failed to load workspace inventory failures")
	}
	defer rows.Close()

	failedByWorkspace := map[string][]ProvisioningJob{}
	for rows.Next() {
		job, err := scanProvisioningJobRow(rows)
		if err != nil {
			return Page[WorkspaceInventoryItem]{}, internalError("failed to scan workspace inventory job")
		}
		failedByWorkspace[job.WorkspaceID] = append(failedByWorkspace[job.WorkspaceID], job)
	}
	if err := rows.Err(); err != nil {
		return Page[WorkspaceInventoryItem]{}, internalError("failed to iterate workspace inventory jobs")
	}

	for _, workspace := range workspacePage.Items {
		failedJobs := failedByWorkspace[workspace.ID]
		items = append(items, WorkspaceInventoryItem{
			Workspace:          workspace,
			OpenFailedJobs:     failedJobs,
			OpenFailedJobCount: len(failedJobs),
		})
	}

	return Page[WorkspaceInventoryItem]{Items: items, NextCursor: workspacePage.NextCursor}, nil
}

func (s *Service) RunWorkspaceBackup(ctx context.Context, identity RequestIdentity, workspaceID string, scheduleName string, retentionDays int) (Workspace, ProvisioningJob, error) {
	workspace, membership, err := s.requireWorkspaceAccess(ctx, identity, workspaceID, true)
	if err != nil {
		return Workspace{}, ProvisioningJob{}, err
	}
	if !membershipCanManage(membership.Role) {
		return Workspace{}, ProvisioningJob{}, accessDenied("workspace backup requires owner or admin access")
	}
	return s.runWorkspaceBackupJob(ctx, workspace, scheduleName, retentionDays, stringPtr(identity.Account.ID), false)
}

func (s *Service) RunWorkspaceUpgrade(ctx context.Context, identity RequestIdentity, workspaceID string, desiredVersion string) (Workspace, ProvisioningJob, error) {
	workspace, membership, err := s.requireWorkspaceAccess(ctx, identity, workspaceID, true)
	if err != nil {
		return Workspace{}, ProvisioningJob{}, err
	}
	if !membershipCanManage(membership.Role) {
		return Workspace{}, ProvisioningJob{}, accessDenied("workspace upgrade requires owner or admin access")
	}
	desiredVersion = strings.TrimSpace(desiredVersion)
	if desiredVersion == "" {
		return Workspace{}, ProvisioningJob{}, invalidRequest("desired_version is required")
	}

	nowText := s.now().Format(time.RFC3339Nano)
	previousVersion := workspace.DeployedVersion
	workspace.DesiredVersion = desiredVersion
	job := ProvisioningJob{
		ID:              "job_" + uuid.NewString(),
		OrganizationID:  workspace.OrganizationID,
		WorkspaceID:     workspace.ID,
		Kind:            "workspace_upgrade",
		Status:          "running",
		RequestedAt:     nowText,
		StartedAt:       stringPtr(nowText),
		ProgressMessage: "workspace upgrade started",
		Retryable:       true,
		Parameters: map[string]any{
			"desired_version":  desiredVersion,
			"previous_version": previousVersion,
		},
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Workspace{}, ProvisioningJob{}, internalError("failed to begin upgrade job")
	}
	defer tx.Rollback()
	if err := s.insertProvisioningJob(ctx, tx, job); err != nil {
		return Workspace{}, ProvisioningJob{}, internalError("failed to create upgrade job")
	}
	if err := insertAuditEventTx(ctx, tx, AuditEvent{
		ID:             "audit_" + uuid.NewString(),
		EventType:      "workspace_upgrade_job_recorded",
		OrganizationID: stringPtr(workspace.OrganizationID),
		WorkspaceID:    stringPtr(workspace.ID),
		TargetType:     "provisioning_job",
		TargetID:       job.ID,
		OccurredAt:     nowText,
		Metadata: map[string]any{
			"desired_version":  desiredVersion,
			"previous_version": previousVersion,
		},
	}, stringPtr(identity.Account.ID)); err != nil {
		return Workspace{}, ProvisioningJob{}, err
	}
	if err := tx.Commit(); err != nil {
		return Workspace{}, ProvisioningJob{}, internalError("failed to commit upgrade job start")
	}

	healthSummary := workspace.HeartbeatHealthSummary
	if len(healthSummary) == 0 {
		job.Status = "failed"
		job.FinishedAt = stringPtr(s.now().Format(time.RFC3339Nano))
		job.FailureReason = stringPtr("workspace heartbeat health summary is not available")
		job.ProgressMessage = "workspace upgrade failed before rollout"
		job.Result = map[string]any{
			"desired_version":  desiredVersion,
			"previous_version": previousVersion,
		}
		job.Retryable = true
	} else if workspaceHealthLooksBad(healthSummary) {
		job.Status = "failed"
		job.FinishedAt = stringPtr(s.now().Format(time.RFC3339Nano))
		job.FailureReason = stringPtr("workspace health summary is not healthy")
		job.ProgressMessage = "workspace upgrade blocked by unhealthy heartbeat"
		job.Result = map[string]any{
			"desired_version":    desiredVersion,
			"previous_version":   previousVersion,
			"pre_health_summary": healthSummary,
		}
		job.Retryable = true
	} else {
		workspace.DeployedVersion = desiredVersion
		workspace.UpdatedAt = s.now().Format(time.RFC3339Nano)
		job.Status = "succeeded"
		job.FinishedAt = stringPtr(workspace.UpdatedAt)
		job.ProgressMessage = "workspace upgrade completed"
		job.Retryable = false
		job.Result = map[string]any{
			"desired_version":     desiredVersion,
			"previous_version":    previousVersion,
			"pre_health_summary":  healthSummary,
			"post_health_summary": healthSummary,
		}
	}

	tx, err = s.db.BeginTx(ctx, nil)
	if err != nil {
		return Workspace{}, ProvisioningJob{}, internalError("failed to begin upgrade finalization")
	}
	defer tx.Rollback()
	if job.Status == "succeeded" {
		if err := s.persistWorkspaceRoutingManifest(ctx, tx, workspace); err != nil {
			return Workspace{}, ProvisioningJob{}, err
		}
	}
	if err := s.updateProvisioningJob(ctx, tx, job); err != nil {
		return Workspace{}, ProvisioningJob{}, internalError("failed to update upgrade job")
	}
	if err := insertAuditEventTx(ctx, tx, AuditEvent{
		ID:             "audit_" + uuid.NewString(),
		EventType:      "workspace_upgrade_job_finished",
		OrganizationID: stringPtr(workspace.OrganizationID),
		WorkspaceID:    stringPtr(workspace.ID),
		TargetType:     "provisioning_job",
		TargetID:       job.ID,
		OccurredAt:     job.FinishedAtValueOr(s.now().Format(time.RFC3339Nano)),
		Metadata: map[string]any{
			"status":           job.Status,
			"desired_version":  desiredVersion,
			"previous_version": previousVersion,
		},
	}, stringPtr(identity.Account.ID)); err != nil {
		return Workspace{}, ProvisioningJob{}, err
	}
	if err := tx.Commit(); err != nil {
		return Workspace{}, ProvisioningJob{}, internalError("failed to commit upgrade finalization")
	}
	return workspace, job, nil
}

func (s *Service) RunWorkspaceRestoreDrill(ctx context.Context, identity RequestIdentity, workspaceID string, backupDir string) (Workspace, ProvisioningJob, error) {
	workspace, membership, err := s.requireWorkspaceAccess(ctx, identity, workspaceID, true)
	if err != nil {
		return Workspace{}, ProvisioningJob{}, err
	}
	if !membershipCanManage(membership.Role) {
		return Workspace{}, ProvisioningJob{}, accessDenied("workspace restore drill requires owner or admin access")
	}
	backupDir = strings.TrimSpace(backupDir)
	if backupDir == "" {
		return Workspace{}, ProvisioningJob{}, invalidRequest("backup_dir is required")
	}

	drillsRoot := filepath.Join(s.workspaceRoot, "drills")
	if err := os.MkdirAll(drillsRoot, 0o755); err != nil {
		return Workspace{}, ProvisioningJob{}, internalError("failed to prepare restore drill directory")
	}
	drillRoot, err := os.MkdirTemp(drillsRoot, workspace.ID+"-")
	if err != nil {
		return Workspace{}, ProvisioningJob{}, internalError("failed to create restore drill workspace")
	}
	drillWorkspace := workspace
	drillWorkspace.WorkspaceRoot = filepath.Join(drillRoot, "workspace")
	drillWorkspace.DeploymentRoot = drillRoot
	drillWorkspace.InstanceID = workspace.InstanceID + "-drill-" + uuid.NewString()

	nowText := s.now().Format(time.RFC3339Nano)
	job := ProvisioningJob{
		ID:              "job_" + uuid.NewString(),
		OrganizationID:  workspace.OrganizationID,
		WorkspaceID:     workspace.ID,
		Kind:            "workspace_restore_drill",
		Status:          "running",
		RequestedAt:     nowText,
		StartedAt:       stringPtr(nowText),
		ProgressMessage: "workspace restore drill started",
		Retryable:       true,
		Parameters: map[string]any{
			"backup_dir":          backupDir,
			"drill_instance_root": drillRoot,
			"drill_instance_id":   drillWorkspace.InstanceID,
		},
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Workspace{}, ProvisioningJob{}, internalError("failed to begin restore drill job")
	}
	defer tx.Rollback()
	if err := s.insertProvisioningJob(ctx, tx, job); err != nil {
		return Workspace{}, ProvisioningJob{}, internalError("failed to create restore drill job")
	}
	if err := insertAuditEventTx(ctx, tx, AuditEvent{
		ID:             "audit_" + uuid.NewString(),
		EventType:      "workspace_restore_drill_job_recorded",
		OrganizationID: stringPtr(workspace.OrganizationID),
		WorkspaceID:    stringPtr(workspace.ID),
		TargetType:     "provisioning_job",
		TargetID:       job.ID,
		OccurredAt:     nowText,
		Metadata: map[string]any{
			"backup_dir": backupDir,
		},
	}, stringPtr(identity.Account.ID)); err != nil {
		return Workspace{}, ProvisioningJob{}, err
	}
	if err := tx.Commit(); err != nil {
		return Workspace{}, ProvisioningJob{}, internalError("failed to commit restore drill start")
	}

	restoreResult, restoreErr := s.runRestoreWorkspaceScriptTo(ctx, drillWorkspace, backupDir, drillRoot, drillWorkspace.InstanceID)
	verifyResult := scriptResult{}
	if restoreErr == nil {
		var verifyErr error
		verifyResult, verifyErr = s.runVerifyRestoreScript(ctx, drillWorkspace)
		if verifyErr != nil {
			restoreErr = verifyErr
		}
	}
	if restoreErr == nil {
		job.Status = "succeeded"
		job.FinishedAt = stringPtr(s.now().Format(time.RFC3339Nano))
		job.ProgressMessage = "workspace restore drill completed"
		job.StdoutTail = restoreResult.StdoutTail
		job.StderrTail = restoreResult.StderrTail
		job.Retryable = false
		job.Result = map[string]any{
			"backup_dir":          backupDir,
			"drill_instance_root": drillRoot,
			"restore_exit_code":   restoreResult.ExitCode,
			"verify_exit_code":    verifyResult.ExitCode,
		}
		if verifyResult.StdoutTail != "" || verifyResult.StderrTail != "" {
			job.Result["verification"] = "run"
			job.Result["verification_tail"] = map[string]any{
				"stdout": verifyResult.StdoutTail,
				"stderr": verifyResult.StderrTail,
			}
		} else {
			job.Result["verification"] = "skipped"
		}
	} else {
		job.Status = "failed"
		job.FinishedAt = stringPtr(s.now().Format(time.RFC3339Nano))
		job.FailureReason = stringPtr(strings.TrimSpace(restoreErr.Error()))
		job.ProgressMessage = "workspace restore drill failed"
		job.StdoutTail = restoreResult.StdoutTail
		job.StderrTail = restoreResult.StderrTail
		job.Retryable = true
		job.Result = map[string]any{
			"backup_dir":          backupDir,
			"drill_instance_root": drillRoot,
			"restore_exit_code":   restoreResult.ExitCode,
			"verify_exit_code":    verifyResult.ExitCode,
		}
	}

	tx, err = s.db.BeginTx(ctx, nil)
	if err != nil {
		return Workspace{}, ProvisioningJob{}, internalError("failed to begin restore drill finalization")
	}
	defer tx.Rollback()
	if err := s.updateProvisioningJob(ctx, tx, job); err != nil {
		return Workspace{}, ProvisioningJob{}, internalError("failed to update restore drill job")
	}
	if err := insertAuditEventTx(ctx, tx, AuditEvent{
		ID:             "audit_" + uuid.NewString(),
		EventType:      "workspace_restore_drill_job_finished",
		OrganizationID: stringPtr(workspace.OrganizationID),
		WorkspaceID:    stringPtr(workspace.ID),
		TargetType:     "provisioning_job",
		TargetID:       job.ID,
		OccurredAt:     job.FinishedAtValueOr(s.now().Format(time.RFC3339Nano)),
		Metadata: map[string]any{
			"status":     job.Status,
			"backup_dir": backupDir,
		},
	}, stringPtr(identity.Account.ID)); err != nil {
		return Workspace{}, ProvisioningJob{}, err
	}
	if err := tx.Commit(); err != nil {
		return Workspace{}, ProvisioningJob{}, internalError("failed to commit restore drill finalization")
	}
	return workspace, job, nil
}

func (s *Service) loadWorkspaceByID(ctx context.Context, workspaceID string) (Workspace, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return Workspace{}, invalidRequest("workspace_id is required")
	}
	row := s.db.QueryRowContext(
		ctx,
		`SELECT id, organization_id, slug, display_name, status, region, workspace_tier, workspace_path, base_url, public_origin, core_origin, host_id, host_label, workspace_root, listen_port, deployment_root, instance_id, service_identity_id, service_identity_public_key, desired_state, desired_version, quota_config_ref, quota_envelope_ref, deployed_version, routing_manifest_path, last_heartbeat_at, heartbeat_version, heartbeat_build, heartbeat_health_summary_json, heartbeat_projection_maintenance_summary_json, heartbeat_usage_summary_json, last_successful_backup_at, created_at, updated_at
		 FROM workspaces WHERE id = ?`,
		workspaceID,
	)
	workspace, err := scanWorkspaceRow(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return Workspace{}, notFound("workspace not found")
		}
		return Workspace{}, internalError("failed to load workspace")
	}
	return workspace, nil
}

func workspaceHealthLooksBad(summary map[string]any) bool {
	for _, key := range []string{"status", "state", "health"} {
		raw, ok := summary[key]
		if !ok {
			continue
		}
		value, ok := raw.(string)
		if !ok {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(value)) {
		case "degraded", "unhealthy", "failed", "error":
			return true
		}
	}
	return false
}

func (j ProvisioningJob) FinishedAtValueOr(fallback string) string {
	if j.FinishedAt != nil && strings.TrimSpace(*j.FinishedAt) != "" {
		return *j.FinishedAt
	}
	return fallback
}
