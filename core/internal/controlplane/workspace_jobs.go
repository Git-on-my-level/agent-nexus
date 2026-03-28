package controlplane

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	hostedInstanceVersion = "hosted-instance/v1"
	jobTailLimitBytes     = 32 * 1024
)

type rowScanner interface {
	Scan(dest ...any) error
}

type scriptResult struct {
	StdoutTail string
	StderrTail string
	ExitCode   int
}

type tailWriter struct {
	limit int
	buf   []byte
}

func newTailWriter(limit int) *tailWriter {
	return &tailWriter{limit: limit}
}

func (w *tailWriter) Write(p []byte) (int, error) {
	if w == nil {
		return len(p), nil
	}
	if w.limit <= 0 {
		return len(p), nil
	}
	if len(p) >= w.limit {
		w.buf = append(w.buf[:0], p[len(p)-w.limit:]...)
		return len(p), nil
	}
	if len(w.buf)+len(p) > w.limit {
		overflow := len(w.buf) + len(p) - w.limit
		if overflow < len(w.buf) {
			w.buf = append(w.buf[:0], w.buf[overflow:]...)
		} else {
			w.buf = w.buf[:0]
		}
	}
	w.buf = append(w.buf, p...)
	return len(p), nil
}

func (w *tailWriter) String() string {
	if w == nil {
		return ""
	}
	return string(w.buf)
}

func scanWorkspaceRow(scanner rowScanner) (Workspace, error) {
	var (
		workspace                             Workspace
		lastHeartbeatAt                       sql.NullString
		lastSuccessfulBackupAt                sql.NullString
		heartbeatHealthSummary                sql.NullString
		heartbeatProjectionMaintenanceSummary sql.NullString
		heartbeatUsageSummary                 sql.NullString
	)
	if err := scanner.Scan(
		&workspace.ID,
		&workspace.OrganizationID,
		&workspace.Slug,
		&workspace.DisplayName,
		&workspace.Status,
		&workspace.Region,
		&workspace.WorkspaceTier,
		&workspace.WorkspacePath,
		&workspace.BaseURL,
		&workspace.PublicOrigin,
		&workspace.CoreOrigin,
		&workspace.HostID,
		&workspace.HostLabel,
		&workspace.WorkspaceRoot,
		&workspace.ListenPort,
		&workspace.DeploymentRoot,
		&workspace.InstanceID,
		&workspace.ServiceIdentityID,
		&workspace.ServiceIdentityPublicKey,
		&workspace.DesiredState,
		&workspace.DesiredVersion,
		&workspace.QuotaConfigRef,
		&workspace.QuotaEnvelopeRef,
		&workspace.DeployedVersion,
		&workspace.RoutingManifestPath,
		&lastHeartbeatAt,
		&workspace.HeartbeatVersion,
		&workspace.HeartbeatBuild,
		&heartbeatHealthSummary,
		&heartbeatProjectionMaintenanceSummary,
		&heartbeatUsageSummary,
		&lastSuccessfulBackupAt,
		&workspace.CreatedAt,
		&workspace.UpdatedAt,
	); err != nil {
		return Workspace{}, err
	}
	workspace.LastHeartbeatAt = nullableString(lastHeartbeatAt)
	workspace.LastSuccessfulBackupAt = nullableString(lastSuccessfulBackupAt)
	workspace.HeartbeatHealthSummary = decodeJSONMap(heartbeatHealthSummary)
	workspace.HeartbeatProjectionMaintenanceSummary = decodeJSONMap(heartbeatProjectionMaintenanceSummary)
	workspace.HeartbeatUsageSummary = decodeJSONMap(heartbeatUsageSummary)
	return workspace, nil
}

func scanProvisioningJobRow(scanner rowScanner) (ProvisioningJob, error) {
	var (
		job           ProvisioningJob
		startedAt     sql.NullString
		finishedAt    sql.NullString
		failureReason sql.NullString
		progress      sql.NullString
		stdoutTail    sql.NullString
		stderrTail    sql.NullString
		parameters    sql.NullString
		result        sql.NullString
		retryable     sql.NullInt64
	)
	if err := scanner.Scan(
		&job.ID,
		&job.OrganizationID,
		&job.WorkspaceID,
		&job.Kind,
		&job.Status,
		&job.RequestedAt,
		&startedAt,
		&finishedAt,
		&failureReason,
		&progress,
		&stdoutTail,
		&stderrTail,
		&retryable,
		&parameters,
		&result,
	); err != nil {
		return ProvisioningJob{}, err
	}
	job.StartedAt = nullableString(startedAt)
	job.FinishedAt = nullableString(finishedAt)
	job.FailureReason = nullableString(failureReason)
	job.ProgressMessage = stringFromNullString(progress)
	job.StdoutTail = stringFromNullString(stdoutTail)
	job.StderrTail = stringFromNullString(stderrTail)
	job.Retryable = !retryable.Valid || retryable.Int64 != 0
	job.Parameters = decodeJSONMap(parameters)
	job.Result = decodeJSONMap(result)
	return job, nil
}

func stringFromNullString(value sql.NullString) string {
	if !value.Valid {
		return ""
	}
	return value.String
}

func decodeJSONMap(value sql.NullString) map[string]any {
	if !value.Valid {
		return nil
	}
	trimmed := strings.TrimSpace(value.String)
	if trimmed == "" || trimmed == "{}" {
		return nil
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(trimmed), &decoded); err != nil {
		return map[string]any{"raw": trimmed}
	}
	return decoded
}

func encodeJSONValue(value any) string {
	if value == nil {
		return "{}"
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return "{}"
	}
	if len(raw) == 0 {
		return "{}"
	}
	return string(raw)
}

func detectHostedScriptsDir() string {
	candidates := []string{}
	if wd, err := os.Getwd(); err == nil {
		candidates = append(candidates, wd)
	}
	if exe, err := os.Executable(); err == nil {
		candidates = append(candidates, filepath.Dir(exe))
	}
	for _, start := range candidates {
		for dir := filepath.Clean(start); dir != string(filepath.Separator); dir = filepath.Dir(dir) {
			scriptsDir := filepath.Join(dir, "scripts", "hosted")
			if fileExists(filepath.Join(scriptsDir, "provision-workspace.sh")) {
				return scriptsDir
			}
		}
	}
	return ""
}

func detectSchemaPath() string {
	candidates := []string{}
	if wd, err := os.Getwd(); err == nil {
		candidates = append(candidates, wd)
	}
	if exe, err := os.Executable(); err == nil {
		candidates = append(candidates, filepath.Dir(exe))
	}
	for _, start := range candidates {
		for dir := filepath.Clean(start); dir != string(filepath.Separator); dir = filepath.Dir(dir) {
			schemaPath := filepath.Join(dir, "contracts", "oar-schema.yaml")
			if fileExists(schemaPath) {
				return schemaPath
			}
		}
	}
	return ""
}

func detectCoreBinaryPath() string {
	candidates := []string{}
	if wd, err := os.Getwd(); err == nil {
		candidates = append(candidates, wd)
	}
	if exe, err := os.Executable(); err == nil {
		candidates = append(candidates, filepath.Dir(exe))
	}
	for _, start := range candidates {
		for dir := filepath.Clean(start); dir != string(filepath.Separator); dir = filepath.Dir(dir) {
			binPath := filepath.Join(dir, "core", ".bin", "oar-core")
			if fileExists(binPath) {
				return binPath
			}
		}
	}
	return ""
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func (s *Service) workspaceDeploymentRoot(workspace Workspace) string {
	if strings.TrimSpace(workspace.DeploymentRoot) != "" {
		return workspace.DeploymentRoot
	}
	placement := s.workspacePlacement(workspace)
	if strings.TrimSpace(placement.WorkspaceRoot) == "" {
		return ""
	}
	return filepath.Dir(placement.WorkspaceRoot)
}

func (s *Service) workspacePublicOrigin(workspace Workspace) string {
	if origin := normalizeWorkspaceOrigin(workspace.PublicOrigin); origin != "" {
		return origin
	}
	return normalizeWorkspaceOrigin(workspace.BaseURL)
}

func (s *Service) workspaceCoreOrigin(workspace Workspace) string {
	if strings.TrimSpace(workspace.CoreOrigin) != "" {
		return normalizeWorkspaceOrigin(workspace.CoreOrigin)
	}
	if listenPort := s.workspacePlacement(workspace).ListenPort; listenPort > 0 {
		return fmt.Sprintf("http://127.0.0.1:%d", listenPort)
	}
	return normalizeWorkspaceOrigin(s.workspacePublicOrigin(workspace))
}

func normalizeWorkspaceOrigin(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return strings.TrimRight(raw, "/")
	}
	parsed.Path = ""
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return strings.TrimRight(parsed.String(), "/")
}

func (s *Service) workspaceRoutingManifestPath(workspace Workspace) string {
	if strings.TrimSpace(workspace.RoutingManifestPath) != "" {
		return workspace.RoutingManifestPath
	}
	return filepath.Join(s.workspaceDeploymentRoot(workspace), "metadata", "routing-manifest.json")
}

func (s *Service) workspaceRoutingManifest(workspace Workspace) WorkspaceRoutingManifest {
	publicOrigin := s.workspacePublicOrigin(workspace)
	placement := s.workspacePlacement(workspace)
	return WorkspaceRoutingManifest{
		WorkspaceID:         workspace.ID,
		OrganizationID:      workspace.OrganizationID,
		Slug:                workspace.Slug,
		WorkspacePath:       workspace.WorkspacePath,
		PublicOrigin:        publicOrigin,
		BaseURL:             workspace.BaseURL,
		CoreOrigin:          s.workspaceCoreOrigin(workspace),
		HostID:              placement.HostID,
		HostLabel:           placement.HostLabel,
		WorkspaceRoot:       placement.WorkspaceRoot,
		ListenPort:          placement.ListenPort,
		DeploymentRoot:      s.workspaceDeploymentRoot(workspace),
		InstanceID:          workspace.InstanceID,
		CurrentState:        workspace.Status,
		DesiredState:        workspace.DesiredState,
		CurrentVersion:      workspace.DeployedVersion,
		DesiredVersion:      workspace.DesiredVersion,
		DeployedVersion:     workspace.DeployedVersion,
		QuotaConfigRef:      workspace.QuotaConfigRef,
		QuotaEnvelopeRef:    workspace.QuotaEnvelopeRef,
		RoutingManifestPath: s.workspaceRoutingManifestPath(workspace),
		GeneratedAt:         s.now().Format(time.RFC3339Nano),
	}
}

func ensureWorkspaceDeploymentDirs(workspace Workspace) error {
	root := strings.TrimSpace(workspace.DeploymentRoot)
	if root == "" {
		return internalError("workspace deployment root is not configured")
	}
	for _, dir := range []string{
		filepath.Join(root, "workspace", "artifacts", "content"),
		filepath.Join(root, "workspace", "logs"),
		filepath.Join(root, "workspace", "tmp"),
		filepath.Join(root, "config"),
		filepath.Join(root, "metadata"),
		filepath.Join(root, "backups"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return internalError(fmt.Sprintf("failed to create workspace deployment directory %q", dir))
		}
	}
	return nil
}

func (s *Service) persistWorkspaceRoutingManifest(ctx context.Context, tx *sql.Tx, workspace Workspace) error {
	manifest := s.workspaceRoutingManifest(workspace)
	rawManifest, err := json.Marshal(manifest)
	if err != nil {
		return internalError("failed to marshal routing manifest")
	}
	routingManifestPath := manifest.RoutingManifestPath
	if err := os.MkdirAll(filepath.Dir(routingManifestPath), 0o755); err != nil {
		return internalError("failed to create routing manifest directory")
	}
	if err := os.WriteFile(routingManifestPath, rawManifest, 0o644); err != nil {
		return internalError("failed to write routing manifest")
	}
	placement := s.workspacePlacement(workspace)
	workspace.HostID = placement.HostID
	workspace.HostLabel = placement.HostLabel
	workspace.WorkspaceRoot = placement.WorkspaceRoot
	workspace.ListenPort = placement.ListenPort
	workspace.DeploymentRoot = manifest.DeploymentRoot
	workspace.RoutingManifestPath = routingManifestPath
	workspace.UpdatedAt = manifest.GeneratedAt
	return s.updateWorkspaceRow(ctx, tx, workspace, rawManifest)
}

func (s *Service) updateWorkspaceRow(ctx context.Context, tx *sql.Tx, workspace Workspace, rawManifest []byte) error {
	_, err := tx.ExecContext(
		ctx,
		`UPDATE workspaces
		 SET status = ?, region = ?, workspace_tier = ?, workspace_path = ?, base_url = ?, public_origin = ?, core_origin = ?, host_id = ?, host_label = ?, workspace_root = ?, listen_port = ?, deployment_root = ?, instance_id = ?, service_identity_id = ?, service_identity_public_key = ?, desired_state = ?, desired_version = ?, quota_config_ref = ?, quota_envelope_ref = ?, deployed_version = ?, routing_manifest_path = ?, last_heartbeat_at = ?, heartbeat_version = ?, heartbeat_build = ?, heartbeat_health_summary_json = ?, heartbeat_projection_maintenance_summary_json = ?, heartbeat_usage_summary_json = ?, last_successful_backup_at = ?, routing_manifest_json = ?, updated_at = ?
		 WHERE id = ?`,
		workspace.Status,
		workspace.Region,
		workspace.WorkspaceTier,
		workspace.WorkspacePath,
		workspace.BaseURL,
		workspace.PublicOrigin,
		workspace.CoreOrigin,
		workspace.HostID,
		workspace.HostLabel,
		workspace.WorkspaceRoot,
		workspace.ListenPort,
		workspace.DeploymentRoot,
		workspace.InstanceID,
		workspace.ServiceIdentityID,
		workspace.ServiceIdentityPublicKey,
		workspace.DesiredState,
		workspace.DesiredVersion,
		workspace.QuotaConfigRef,
		workspace.QuotaEnvelopeRef,
		workspace.DeployedVersion,
		workspace.RoutingManifestPath,
		nullStringValue(workspace.LastHeartbeatAt),
		workspace.HeartbeatVersion,
		workspace.HeartbeatBuild,
		encodeJSONValue(workspace.HeartbeatHealthSummary),
		encodeJSONValue(workspace.HeartbeatProjectionMaintenanceSummary),
		encodeJSONValue(workspace.HeartbeatUsageSummary),
		nullStringValue(workspace.LastSuccessfulBackupAt),
		string(rawManifest),
		workspace.UpdatedAt,
		workspace.ID,
	)
	return err
}

func (s *Service) insertProvisioningJob(ctx context.Context, tx *sql.Tx, job ProvisioningJob) error {
	_, err := tx.ExecContext(
		ctx,
		`INSERT INTO provisioning_jobs(
			id, organization_id, workspace_id, kind, status, requested_at, started_at, finished_at, failure_reason,
			progress_message, stdout_tail, stderr_tail, retryable, parameters_json, result_json
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		job.ID,
		job.OrganizationID,
		job.WorkspaceID,
		job.Kind,
		job.Status,
		job.RequestedAt,
		nullStringValue(job.StartedAt),
		nullStringValue(job.FinishedAt),
		nullStringValue(job.FailureReason),
		job.ProgressMessage,
		job.StdoutTail,
		job.StderrTail,
		boolToInt(job.Retryable),
		encodeJSONValue(job.Parameters),
		encodeJSONValue(job.Result),
	)
	return err
}

func (s *Service) updateProvisioningJob(ctx context.Context, tx *sql.Tx, job ProvisioningJob) error {
	_, err := tx.ExecContext(
		ctx,
		`UPDATE provisioning_jobs
		 SET status = ?, started_at = ?, finished_at = ?, failure_reason = ?, progress_message = ?, stdout_tail = ?, stderr_tail = ?, retryable = ?, parameters_json = ?, result_json = ?
		 WHERE id = ?`,
		job.Status,
		nullStringValue(job.StartedAt),
		nullStringValue(job.FinishedAt),
		nullStringValue(job.FailureReason),
		job.ProgressMessage,
		job.StdoutTail,
		job.StderrTail,
		boolToInt(job.Retryable),
		encodeJSONValue(job.Parameters),
		encodeJSONValue(job.Result),
		job.ID,
	)
	return err
}

func (s *Service) runHostedScript(ctx context.Context, scriptName string, args ...string) (scriptResult, error) {
	scriptsDir := strings.TrimSpace(s.hostedScriptsDir)
	if scriptsDir == "" || !dirExists(scriptsDir) {
		return scriptResult{}, internalError("hosted scripts directory is not configured")
	}
	scriptPath := filepath.Join(scriptsDir, scriptName)
	if !fileExists(scriptPath) {
		return scriptResult{}, internalError(fmt.Sprintf("hosted script not found: %s", scriptName))
	}

	stdoutTail := newTailWriter(jobTailLimitBytes)
	stderrTail := newTailWriter(jobTailLimitBytes)
	cmd := exec.CommandContext(ctx, scriptPath, args...)
	cmd.Stdout = io.MultiWriter(stdoutTail)
	cmd.Stderr = io.MultiWriter(stderrTail)
	cmd.Env = os.Environ()

	err := cmd.Run()
	result := scriptResult{
		StdoutTail: stdoutTail.String(),
		StderrTail: stderrTail.String(),
	}
	if err == nil {
		return result, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		result.ExitCode = exitErr.ExitCode()
		return result, err
	}
	return result, err
}

func (s *Service) runProvisionWorkspaceScript(ctx context.Context, workspace Workspace, quota WorkspaceQuota) (scriptResult, error) {
	args := []string{
		"--instance", workspace.InstanceID,
		"--instance-root", s.workspaceDeploymentRoot(workspace),
		"--public-origin", s.workspacePublicOrigin(workspace),
		"--listen-port", fmt.Sprintf("%d", s.workspacePlacement(workspace).ListenPort),
		"--web-ui-port", fmt.Sprintf("%d", s.workspaceWebUIPort(workspace)),
		"--core-instance-id", workspace.InstanceID,
		"--max-blob-bytes", fmt.Sprintf("%d", quota.MaxBlobBytes),
		"--max-artifacts", fmt.Sprintf("%d", quota.MaxArtifacts),
		"--max-documents", fmt.Sprintf("%d", quota.MaxDocuments),
		"--max-document-revisions", fmt.Sprintf("%d", quota.MaxDocumentRevisions),
		"--max-upload-bytes", fmt.Sprintf("%d", quota.MaxUploadBytes),
	}
	return s.runHostedScript(ctx, "provision-workspace.sh", args...)
}

func (s *Service) runRestoreWorkspaceScript(ctx context.Context, workspace Workspace, backupDir string) (scriptResult, error) {
	return s.runRestoreWorkspaceScriptTo(ctx, workspace, backupDir, s.workspaceDeploymentRoot(workspace), workspace.InstanceID)
}

func (s *Service) runRestoreWorkspaceScriptTo(ctx context.Context, workspace Workspace, backupDir string, targetInstanceRoot string, instanceName string) (scriptResult, error) {
	args := []string{
		"--backup-dir", backupDir,
		"--target-instance-root", targetInstanceRoot,
		"--instance", instanceName,
		"--public-origin", s.workspacePublicOrigin(workspace),
		"--listen-port", fmt.Sprintf("%d", s.workspacePlacement(workspace).ListenPort),
		"--web-ui-port", fmt.Sprintf("%d", s.workspaceWebUIPort(workspace)),
		"--core-instance-id", instanceName,
		"--force",
	}
	return s.runHostedScript(ctx, "restore-workspace.sh", args...)
}

func (s *Service) runBackupWorkspaceScript(ctx context.Context, workspace Workspace, outputDir string) (scriptResult, error) {
	args := []string{
		"--instance-root", s.workspaceDeploymentRoot(workspace),
		"--output-dir", outputDir,
	}
	return s.runHostedScript(ctx, "backup-workspace.sh", args...)
}

func (s *Service) runVerifyRestoreScript(ctx context.Context, workspace Workspace) (scriptResult, error) {
	coreBinary := strings.TrimSpace(s.verifyCoreBinaryPath)
	if coreBinary == "" {
		coreBinary = detectCoreBinaryPath()
	}
	if coreBinary == "" {
		return scriptResult{}, nil
	}
	schemaPath := strings.TrimSpace(s.verifySchemaPath)
	if schemaPath == "" {
		schemaPath = detectSchemaPath()
	}
	if schemaPath == "" {
		return scriptResult{}, nil
	}
	args := []string{
		"--instance-root", s.workspaceDeploymentRoot(workspace),
		"--core-bin", coreBinary,
		"--schema-path", schemaPath,
	}
	return s.runHostedScript(ctx, "verify-restore.sh", args...)
}

func (s *Service) applyWorkspaceQuotaConfigForOrganization(ctx context.Context, organizationID string, planTier string) error {
	rows, err := s.db.QueryContext(
		ctx,
		`SELECT id, organization_id, slug, display_name, status, region, workspace_tier, workspace_path, base_url, public_origin, core_origin, host_id, host_label, workspace_root, listen_port, deployment_root, instance_id, service_identity_id, service_identity_public_key, desired_state, desired_version, quota_config_ref, quota_envelope_ref, deployed_version, routing_manifest_path, last_heartbeat_at, heartbeat_version, heartbeat_build, heartbeat_health_summary_json, heartbeat_projection_maintenance_summary_json, heartbeat_usage_summary_json, last_successful_backup_at, created_at, updated_at
		FROM workspaces
		WHERE organization_id = ?`,
		organizationID,
	)
	if err != nil {
		return internalError("failed to list workspaces for quota refresh")
	}
	defer rows.Close()

	quota := workspaceQuotaForPlanTier(planTier)
	for rows.Next() {
		workspace, err := scanWorkspaceRow(rows)
		if err != nil {
			return internalError("failed to scan workspace for quota refresh")
		}
		if _, err := s.runProvisionWorkspaceScript(ctx, workspace, quota); err != nil {
			return internalError(fmt.Sprintf("failed to refresh workspace quota config for %s", workspace.ID))
		}
	}
	if err := rows.Err(); err != nil {
		return internalError("failed to iterate workspaces for quota refresh")
	}
	return nil
}

func workspaceOperationRetryable(kind string, err error) bool {
	if err == nil {
		return true
	}
	switch kind {
	case "workspace_suspend", "workspace_resume", "workspace_decommission":
		return false
	default:
		return true
	}
}

func boolToInt(value bool) int64 {
	if value {
		return 1
	}
	return 0
}
