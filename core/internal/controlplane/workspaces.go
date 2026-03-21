package controlplane

import (
	"context"
	"database/sql"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
)

func (s *Service) ListWorkspaces(ctx context.Context, identity RequestIdentity, organizationID string, page PageRequest) (Page[Workspace], error) {
	limit := normalizePageLimit(page.Limit)
	sortAt, sortID, err := decodeCursor(page.Cursor)
	if err != nil {
		return Page[Workspace]{}, invalidRequest("cursor is invalid")
	}

	query := `SELECT w.id, w.organization_id, w.slug, w.display_name, w.status, w.region, w.workspace_tier, w.workspace_path, w.base_url, w.created_at, w.updated_at
		FROM workspaces w
		JOIN organization_memberships m ON m.organization_id = w.organization_id
		WHERE m.account_id = ? AND m.status = 'active'`
	args := []any{identity.Account.ID}
	if strings.TrimSpace(organizationID) != "" {
		query += ` AND w.organization_id = ?`
		args = append(args, organizationID)
	}
	if sortAt != "" {
		query += ` AND (w.created_at > ? OR (w.created_at = ? AND w.id > ?))`
		args = append(args, sortAt, sortAt, sortID)
	}
	query += ` ORDER BY w.created_at ASC, w.id ASC LIMIT ?`
	args = append(args, limit+1)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return Page[Workspace]{}, internalError("failed to list workspaces")
	}
	defer rows.Close()

	var workspaces []Workspace
	for rows.Next() {
		var workspace Workspace
		if err := rows.Scan(&workspace.ID, &workspace.OrganizationID, &workspace.Slug, &workspace.DisplayName, &workspace.Status, &workspace.Region, &workspace.WorkspaceTier, &workspace.WorkspacePath, &workspace.BaseURL, &workspace.CreatedAt, &workspace.UpdatedAt); err != nil {
			return Page[Workspace]{}, internalError("failed to scan workspace")
		}
		workspaces = append(workspaces, workspace)
	}
	if err := rows.Err(); err != nil {
		return Page[Workspace]{}, internalError("failed to iterate workspaces")
	}
	return pageFromItems(workspaces, limit, func(workspace Workspace) (string, string) {
		return workspace.CreatedAt, workspace.ID
	}), nil
}

func (s *Service) CreateWorkspace(ctx context.Context, identity RequestIdentity, organizationID string, slug string, displayName string, region string, workspaceTier string) (Workspace, ProvisioningJob, error) {
	organization, membership, err := s.requireOrganizationAccess(ctx, identity, organizationID, false)
	if err != nil {
		return Workspace{}, ProvisioningJob{}, err
	}
	if !membershipCanManage(membership.Role) {
		return Workspace{}, ProvisioningJob{}, accessDenied("workspace creation requires owner or admin access")
	}
	slug, err = normalizeSlug(slug)
	if err != nil {
		return Workspace{}, ProvisioningJob{}, err
	}
	displayName, err = normalizeDisplayName(displayName)
	if err != nil {
		return Workspace{}, ProvisioningJob{}, err
	}
	region = strings.TrimSpace(region)
	if region == "" {
		return Workspace{}, ProvisioningJob{}, invalidRequest("region is required")
	}
	if err := validateWorkspaceTier(workspaceTier); err != nil {
		return Workspace{}, ProvisioningJob{}, err
	}
	usageSummary, err := s.GetUsageSummary(ctx, identity, organizationID)
	if err != nil {
		return Workspace{}, ProvisioningJob{}, err
	}
	if usageSummary.Quota.WorkspacesRemaining <= 0 {
		return Workspace{}, ProvisioningJob{}, &APIError{Status: http.StatusUnprocessableEntity, Code: "quota_exceeded", Message: "workspace quota has been reached for this organization"}
	}

	nowText := s.now().Format(time.RFC3339Nano)
	workspacePath := "/" + slug
	workspace := Workspace{
		ID:             "ws_" + uuid.NewString(),
		OrganizationID: organizationID,
		Slug:           slug,
		DisplayName:    displayName,
		Status:         "ready",
		Region:         region,
		WorkspaceTier:  workspaceTier,
		WorkspacePath:  workspacePath,
		BaseURL:        formatTemplateURL(s.workspaceURLTemplate, strings.TrimPrefix(workspacePath, "/")),
		CreatedAt:      nowText,
		UpdatedAt:      nowText,
	}
	job := ProvisioningJob{
		ID:             "job_" + uuid.NewString(),
		OrganizationID: organizationID,
		WorkspaceID:    workspace.ID,
		Kind:           "workspace_create",
		Status:         "succeeded",
		RequestedAt:    nowText,
		StartedAt:      stringPtr(nowText),
		FinishedAt:     stringPtr(nowText),
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Workspace{}, ProvisioningJob{}, internalError("failed to begin create workspace transaction")
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO workspaces(id, organization_id, slug, display_name, status, region, workspace_tier, workspace_path, base_url, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		workspace.ID,
		workspace.OrganizationID,
		workspace.Slug,
		workspace.DisplayName,
		workspace.Status,
		workspace.Region,
		workspace.WorkspaceTier,
		workspace.WorkspacePath,
		workspace.BaseURL,
		workspace.CreatedAt,
		workspace.UpdatedAt,
	); err != nil {
		if isSQLiteConstraint(err) {
			return Workspace{}, ProvisioningJob{}, conflict("slug_conflict", "workspace slug is already in use for this organization")
		}
		return Workspace{}, ProvisioningJob{}, internalError("failed to create workspace")
	}
	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO provisioning_jobs(id, organization_id, workspace_id, kind, status, requested_at, started_at, finished_at, failure_reason)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, NULL)`,
		job.ID,
		job.OrganizationID,
		job.WorkspaceID,
		job.Kind,
		job.Status,
		job.RequestedAt,
		*job.StartedAt,
		*job.FinishedAt,
	); err != nil {
		return Workspace{}, ProvisioningJob{}, internalError("failed to create provisioning job")
	}
	if err := insertAuditEventTx(ctx, tx, AuditEvent{
		ID:             "audit_" + uuid.NewString(),
		EventType:      "workspace_created",
		OrganizationID: stringPtr(organization.ID),
		WorkspaceID:    stringPtr(workspace.ID),
		TargetType:     "workspace",
		TargetID:       workspace.ID,
		OccurredAt:     nowText,
		Metadata: map[string]any{
			"slug":           workspace.Slug,
			"region":         workspace.Region,
			"workspace_tier": workspace.WorkspaceTier,
		},
	}, stringPtr(identity.Account.ID)); err != nil {
		return Workspace{}, ProvisioningJob{}, err
	}
	if err := insertAuditEventTx(ctx, tx, AuditEvent{
		ID:             "audit_" + uuid.NewString(),
		EventType:      "provisioning_job_recorded",
		OrganizationID: stringPtr(organization.ID),
		WorkspaceID:    stringPtr(workspace.ID),
		TargetType:     "provisioning_job",
		TargetID:       job.ID,
		OccurredAt:     nowText,
		Metadata: map[string]any{
			"status": job.Status,
			"kind":   job.Kind,
		},
	}, stringPtr(identity.Account.ID)); err != nil {
		return Workspace{}, ProvisioningJob{}, err
	}

	if err := tx.Commit(); err != nil {
		return Workspace{}, ProvisioningJob{}, internalError("failed to commit workspace creation")
	}
	return workspace, job, nil
}

func (s *Service) GetWorkspace(ctx context.Context, identity RequestIdentity, workspaceID string) (Workspace, error) {
	workspace, _, err := s.requireWorkspaceAccess(ctx, identity, workspaceID, false)
	return workspace, err
}

func (s *Service) GetProvisioningJob(ctx context.Context, identity RequestIdentity, jobID string) (ProvisioningJob, error) {
	jobID = strings.TrimSpace(jobID)
	if jobID == "" {
		return ProvisioningJob{}, invalidRequest("job_id is required")
	}

	var job ProvisioningJob
	var startedAt sql.NullString
	var finishedAt sql.NullString
	var failureReason sql.NullString
	err := s.db.QueryRowContext(
		ctx,
		`SELECT id, organization_id, workspace_id, kind, status, requested_at, started_at, finished_at, failure_reason
		 FROM provisioning_jobs WHERE id = ?`,
		jobID,
	).Scan(&job.ID, &job.OrganizationID, &job.WorkspaceID, &job.Kind, &job.Status, &job.RequestedAt, &startedAt, &finishedAt, &failureReason)
	if err != nil {
		if err == sql.ErrNoRows {
			return ProvisioningJob{}, notFound("provisioning job not found")
		}
		return ProvisioningJob{}, internalError("failed to load provisioning job")
	}
	job.StartedAt = nullableString(startedAt)
	job.FinishedAt = nullableString(finishedAt)
	job.FailureReason = nullableString(failureReason)

	if _, _, err := s.requireOrganizationAccess(ctx, identity, job.OrganizationID, false); err != nil {
		return ProvisioningJob{}, err
	}
	return job, nil
}

func (s *Service) ListProvisioningJobs(ctx context.Context, identity RequestIdentity, filter JobsFilter) (Page[ProvisioningJob], error) {
	limit := normalizePageLimit(filter.Page.Limit)
	sortAt, sortID, err := decodeCursor(filter.Page.Cursor)
	if err != nil {
		return Page[ProvisioningJob]{}, invalidRequest("cursor is invalid")
	}

	query := `SELECT j.id, j.organization_id, j.workspace_id, j.kind, j.status, j.requested_at, j.started_at, j.finished_at, j.failure_reason
		FROM provisioning_jobs j
		JOIN organization_memberships m ON m.organization_id = j.organization_id
		WHERE m.account_id = ? AND m.status = 'active'`
	args := []any{identity.Account.ID}
	if strings.TrimSpace(filter.OrganizationID) != "" {
		query += ` AND j.organization_id = ?`
		args = append(args, filter.OrganizationID)
	}
	if strings.TrimSpace(filter.WorkspaceID) != "" {
		query += ` AND j.workspace_id = ?`
		args = append(args, filter.WorkspaceID)
	}
	if sortAt != "" {
		query += ` AND (j.requested_at > ? OR (j.requested_at = ? AND j.id > ?))`
		args = append(args, sortAt, sortAt, sortID)
	}
	query += ` ORDER BY j.requested_at ASC, j.id ASC LIMIT ?`
	args = append(args, limit+1)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return Page[ProvisioningJob]{}, internalError("failed to list provisioning jobs")
	}
	defer rows.Close()

	var jobs []ProvisioningJob
	for rows.Next() {
		var (
			job           ProvisioningJob
			startedAt     sql.NullString
			finishedAt    sql.NullString
			failureReason sql.NullString
		)
		if err := rows.Scan(&job.ID, &job.OrganizationID, &job.WorkspaceID, &job.Kind, &job.Status, &job.RequestedAt, &startedAt, &finishedAt, &failureReason); err != nil {
			return Page[ProvisioningJob]{}, internalError("failed to scan provisioning job")
		}
		job.StartedAt = nullableString(startedAt)
		job.FinishedAt = nullableString(finishedAt)
		job.FailureReason = nullableString(failureReason)
		jobs = append(jobs, job)
	}
	if err := rows.Err(); err != nil {
		return Page[ProvisioningJob]{}, internalError("failed to iterate provisioning jobs")
	}
	return pageFromItems(jobs, limit, func(job ProvisioningJob) (string, string) {
		return job.RequestedAt, job.ID
	}), nil
}

func (s *Service) CreateWorkspaceLaunchSession(ctx context.Context, identity RequestIdentity, workspaceID string, returnPath *string) (WorkspaceLaunchSession, error) {
	workspace, membership, err := s.requireWorkspaceAccess(ctx, identity, workspaceID, false)
	if err != nil {
		return WorkspaceLaunchSession{}, err
	}
	if membership.Status != "active" {
		return WorkspaceLaunchSession{}, accessDenied("workspace membership is disabled")
	}
	if workspace.Status != "ready" {
		return WorkspaceLaunchSession{}, &APIError{Status: http.StatusConflict, Code: "workspace_not_ready", Message: "workspace is not ready for launch"}
	}

	if returnPath != nil {
		trimmed := strings.TrimSpace(*returnPath)
		if trimmed != "" && !strings.HasPrefix(trimmed, "/") {
			return WorkspaceLaunchSession{}, invalidRequest("return_path must start with /")
		}
		returnPath = &trimmed
	}

	exchangeToken, err := randomBase64URL(32)
	if err != nil {
		return WorkspaceLaunchSession{}, internalError("failed to generate launch token")
	}
	now := s.now()
	launch := WorkspaceLaunchSession{
		LaunchID:      "launch_" + uuid.NewString(),
		WorkspaceID:   workspace.ID,
		WorkspacePath: workspace.WorkspacePath,
		WorkspaceURL:  workspace.BaseURL,
		ExchangeToken: exchangeToken,
		ExpiresAt:     now.Add(s.launchTTL).Format(time.RFC3339Nano),
	}
	if returnPath != nil && *returnPath != "" {
		launch.ReturnPath = returnPath
	}
	if _, err := s.db.ExecContext(
		ctx,
		`INSERT INTO launch_sessions(id, workspace_id, account_id, return_path, token_hash, created_at, expires_at, consumed_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, NULL)`,
		launch.LaunchID,
		workspace.ID,
		identity.Account.ID,
		nullStringValue(returnPath),
		hashToken(exchangeToken),
		now.Format(time.RFC3339Nano),
		launch.ExpiresAt,
	); err != nil {
		return WorkspaceLaunchSession{}, internalError("failed to persist launch session")
	}
	if err := insertAuditEvent(ctx, s.db, AuditEvent{
		ID:             "audit_" + uuid.NewString(),
		EventType:      "workspace_launch_session_created",
		OrganizationID: stringPtr(workspace.OrganizationID),
		WorkspaceID:    stringPtr(workspace.ID),
		TargetType:     "launch_session",
		TargetID:       launch.LaunchID,
		OccurredAt:     now.Format(time.RFC3339Nano),
	}, stringPtr(identity.Account.ID)); err != nil {
		return WorkspaceLaunchSession{}, err
	}
	return launch, nil
}

func (s *Service) ExchangeWorkspaceSession(ctx context.Context, workspaceID string, exchangeToken string) (Workspace, WorkspaceGrant, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	exchangeToken = strings.TrimSpace(exchangeToken)
	if workspaceID == "" {
		return Workspace{}, WorkspaceGrant{}, invalidRequest("workspace_id is required")
	}
	if exchangeToken == "" {
		return Workspace{}, WorkspaceGrant{}, invalidRequest("exchange_token is required")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Workspace{}, WorkspaceGrant{}, internalError("failed to begin session exchange")
	}
	defer tx.Rollback()

	var (
		launchID       string
		returnPath     sql.NullString
		organizationID string
		workspace      Workspace
		expiresAt      string
		consumedAt     sql.NullString
	)
	err = tx.QueryRowContext(
		ctx,
		`SELECT
			l.id,
			l.return_path,
			l.expires_at,
			l.consumed_at,
			w.id,
			w.organization_id,
			w.slug,
			w.display_name,
			w.status,
			w.region,
			w.workspace_tier,
			w.workspace_path,
			w.base_url,
			w.created_at,
			w.updated_at
		 FROM launch_sessions l
		 JOIN workspaces w ON w.id = l.workspace_id
		 WHERE l.workspace_id = ? AND l.token_hash = ?`,
		workspaceID,
		hashToken(exchangeToken),
	).Scan(
		&launchID,
		&returnPath,
		&expiresAt,
		&consumedAt,
		&workspace.ID,
		&organizationID,
		&workspace.Slug,
		&workspace.DisplayName,
		&workspace.Status,
		&workspace.Region,
		&workspace.WorkspaceTier,
		&workspace.WorkspacePath,
		&workspace.BaseURL,
		&workspace.CreatedAt,
		&workspace.UpdatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return Workspace{}, WorkspaceGrant{}, &APIError{Status: http.StatusNotFound, Code: "not_found", Message: "launch session not found"}
		}
		return Workspace{}, WorkspaceGrant{}, internalError("failed to load launch session")
	}
	workspace.OrganizationID = organizationID
	if workspace.Status != "ready" {
		return Workspace{}, WorkspaceGrant{}, &APIError{Status: http.StatusConflict, Code: "workspace_not_ready", Message: "workspace is not ready for launch"}
	}
	if consumedAt.Valid {
		return Workspace{}, WorkspaceGrant{}, &APIError{Status: http.StatusConflict, Code: "exchange_invalid", Message: "exchange token has already been used"}
	}
	if isExpired(expiresAt, s.now()) {
		return Workspace{}, WorkspaceGrant{}, &APIError{Status: http.StatusConflict, Code: "exchange_expired", Message: "exchange token has expired"}
	}

	now := s.now()
	nowText := now.Format(time.RFC3339Nano)
	if _, err := tx.ExecContext(ctx, `UPDATE launch_sessions SET consumed_at = ? WHERE id = ?`, nowText, launchID); err != nil {
		return Workspace{}, WorkspaceGrant{}, internalError("failed to consume launch session")
	}
	grantToken, err := randomBase64URL(32)
	if err != nil {
		return Workspace{}, WorkspaceGrant{}, internalError("failed to generate workspace grant")
	}
	grant := WorkspaceGrant{
		Kind:        "human-session",
		BearerToken: grantToken,
		ExpiresAt:   now.Add(s.launchTTL).Format(time.RFC3339Nano),
		Scope:       "workspace:" + workspace.ID,
	}
	if err := insertAuditEventTx(ctx, tx, AuditEvent{
		ID:             "audit_" + uuid.NewString(),
		EventType:      "workspace_session_exchanged",
		OrganizationID: stringPtr(workspace.OrganizationID),
		WorkspaceID:    stringPtr(workspace.ID),
		TargetType:     "launch_session",
		TargetID:       launchID,
		OccurredAt:     nowText,
		Metadata: map[string]any{
			"return_path": nullStringValue(nullableString(returnPath)),
		},
	}, nil); err != nil {
		return Workspace{}, WorkspaceGrant{}, err
	}
	if err := tx.Commit(); err != nil {
		return Workspace{}, WorkspaceGrant{}, internalError("failed to commit session exchange")
	}
	return workspace, grant, nil
}

func (s *Service) requireWorkspaceAccess(ctx context.Context, identity RequestIdentity, workspaceID string, includeSuspended bool) (Workspace, Membership, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return Workspace{}, Membership{}, invalidRequest("workspace_id is required")
	}

	row := s.db.QueryRowContext(
		ctx,
		`SELECT
			w.id, w.organization_id, w.slug, w.display_name, w.status, w.region, w.workspace_tier, w.workspace_path, w.base_url, w.created_at, w.updated_at,
			m.id, m.account_id, m.role, m.status, m.created_at
		 FROM workspaces w
		 JOIN organization_memberships m ON m.organization_id = w.organization_id
		 WHERE w.id = ? AND m.account_id = ?`,
		workspaceID,
		identity.Account.ID,
	)
	var (
		workspace  Workspace
		membership Membership
	)
	if err := row.Scan(
		&workspace.ID,
		&workspace.OrganizationID,
		&workspace.Slug,
		&workspace.DisplayName,
		&workspace.Status,
		&workspace.Region,
		&workspace.WorkspaceTier,
		&workspace.WorkspacePath,
		&workspace.BaseURL,
		&workspace.CreatedAt,
		&workspace.UpdatedAt,
		&membership.ID,
		&membership.AccountID,
		&membership.Role,
		&membership.Status,
		&membership.CreatedAt,
	); err != nil {
		if err == sql.ErrNoRows {
			return Workspace{}, Membership{}, notFound("workspace not found")
		}
		return Workspace{}, Membership{}, internalError("failed to load workspace access")
	}
	membership.OrganizationID = workspace.OrganizationID
	if membership.Status != "active" {
		return Workspace{}, Membership{}, accessDenied("workspace membership is disabled")
	}
	if !includeSuspended && workspace.Status == "archived" {
		return Workspace{}, Membership{}, accessDenied("workspace is archived")
	}
	return workspace, membership, nil
}

func validateWorkspaceTier(tier string) error {
	switch strings.TrimSpace(tier) {
	case "standard", "plus", "dedicated":
		return nil
	default:
		return invalidRequest("workspace_tier must be standard, plus, or dedicated")
	}
}

func nullStringValue(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}
