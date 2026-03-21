package controlplane

type Account struct {
	ID          string  `json:"id"`
	Email       string  `json:"email"`
	DisplayName string  `json:"display_name"`
	Status      string  `json:"status"`
	CreatedAt   string  `json:"created_at"`
	LastLoginAt *string `json:"last_login_at,omitempty"`
}

type AccountHint struct {
	Email       string `json:"email"`
	DisplayName string `json:"display_name,omitempty"`
}

type Session struct {
	ID          string `json:"id"`
	AccountID   string `json:"account_id"`
	IssuedAt    string `json:"issued_at"`
	ExpiresAt   string `json:"expires_at"`
	AccessToken string `json:"access_token"`
}

type Organization struct {
	ID          string `json:"id"`
	Slug        string `json:"slug"`
	DisplayName string `json:"display_name"`
	PlanTier    string `json:"plan_tier"`
	Status      string `json:"status"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

type Membership struct {
	ID             string `json:"id"`
	OrganizationID string `json:"organization_id"`
	AccountID      string `json:"account_id"`
	Role           string `json:"role"`
	Status         string `json:"status"`
	CreatedAt      string `json:"created_at"`
}

type OrganizationInvite struct {
	ID             string  `json:"id"`
	OrganizationID string  `json:"organization_id"`
	Email          string  `json:"email"`
	Role           string  `json:"role"`
	Status         string  `json:"status"`
	CreatedAt      string  `json:"created_at"`
	ExpiresAt      string  `json:"expires_at"`
	AcceptedAt     *string `json:"accepted_at,omitempty"`
	RevokedAt      *string `json:"revoked_at,omitempty"`
}

type Workspace struct {
	ID                                    string         `json:"id"`
	OrganizationID                        string         `json:"organization_id"`
	Slug                                  string         `json:"slug"`
	DisplayName                           string         `json:"display_name"`
	Status                                string         `json:"status"`
	Region                                string         `json:"region"`
	WorkspaceTier                         string         `json:"workspace_tier"`
	WorkspacePath                         string         `json:"workspace_path"`
	BaseURL                               string         `json:"base_url"`
	PublicOrigin                          string         `json:"public_origin"`
	CoreOrigin                            string         `json:"core_origin"`
	DeploymentRoot                        string         `json:"deployment_root"`
	InstanceID                            string         `json:"instance_id"`
	ServiceIdentityID                     string         `json:"service_identity_id,omitempty"`
	ServiceIdentityPublicKey              string         `json:"service_identity_public_key,omitempty"`
	DesiredState                          string         `json:"desired_state"`
	DesiredVersion                        string         `json:"desired_version"`
	QuotaConfigRef                        string         `json:"quota_config_ref"`
	QuotaEnvelopeRef                      string         `json:"quota_envelope_ref"`
	DeployedVersion                       string         `json:"deployed_version"`
	RoutingManifestPath                   string         `json:"routing_manifest_path"`
	LastHeartbeatAt                       *string        `json:"last_heartbeat_at,omitempty"`
	HeartbeatVersion                      string         `json:"heartbeat_version,omitempty"`
	HeartbeatBuild                        string         `json:"heartbeat_build,omitempty"`
	HeartbeatHealthSummary                map[string]any `json:"heartbeat_health_summary,omitempty"`
	HeartbeatProjectionMaintenanceSummary map[string]any `json:"heartbeat_projection_maintenance_summary,omitempty"`
	HeartbeatUsageSummary                 map[string]any `json:"heartbeat_usage_summary,omitempty"`
	LastSuccessfulBackupAt                *string        `json:"last_successful_backup_at,omitempty"`
	CreatedAt                             string         `json:"created_at"`
	UpdatedAt                             string         `json:"updated_at"`
}

type ProvisioningJob struct {
	ID              string         `json:"id"`
	OrganizationID  string         `json:"organization_id"`
	WorkspaceID     string         `json:"workspace_id"`
	Kind            string         `json:"kind"`
	Status          string         `json:"status"`
	RequestedAt     string         `json:"requested_at"`
	StartedAt       *string        `json:"started_at,omitempty"`
	FinishedAt      *string        `json:"finished_at,omitempty"`
	FailureReason   *string        `json:"failure_reason,omitempty"`
	ProgressMessage string         `json:"progress_message,omitempty"`
	StdoutTail      string         `json:"stdout_tail,omitempty"`
	StderrTail      string         `json:"stderr_tail,omitempty"`
	Retryable       bool           `json:"retryable,omitempty"`
	Parameters      map[string]any `json:"parameters,omitempty"`
	Result          map[string]any `json:"result,omitempty"`
}

type WorkspaceRoutingManifest struct {
	WorkspaceID         string `json:"workspace_id"`
	OrganizationID      string `json:"organization_id"`
	Slug                string `json:"slug"`
	WorkspacePath       string `json:"workspace_path"`
	PublicOrigin        string `json:"public_origin"`
	BaseURL             string `json:"base_url"`
	CoreOrigin          string `json:"core_origin"`
	DeploymentRoot      string `json:"deployment_root"`
	InstanceID          string `json:"instance_id"`
	CurrentState        string `json:"current_state"`
	DesiredState        string `json:"desired_state"`
	CurrentVersion      string `json:"current_version"`
	DesiredVersion      string `json:"desired_version"`
	DeployedVersion     string `json:"deployed_version,omitempty"`
	QuotaConfigRef      string `json:"quota_config_ref"`
	QuotaEnvelopeRef    string `json:"quota_envelope_ref"`
	RoutingManifestPath string `json:"routing_manifest_path"`
	GeneratedAt         string `json:"generated_at"`
}

type WorkspaceLaunchSession struct {
	LaunchID      string  `json:"launch_id"`
	WorkspaceID   string  `json:"workspace_id"`
	WorkspacePath string  `json:"workspace_path"`
	WorkspaceURL  string  `json:"workspace_url"`
	ReturnPath    *string `json:"return_path,omitempty"`
	ExchangeToken string  `json:"exchange_token"`
	ExpiresAt     string  `json:"expires_at"`
}

type WorkspaceGrant struct {
	Kind        string `json:"kind"`
	BearerToken string `json:"bearer_token"`
	ExpiresAt   string `json:"expires_at"`
	Scope       string `json:"scope"`
}

type UsagePlan struct {
	ID                string `json:"id"`
	DisplayName       string `json:"display_name"`
	WorkspaceLimit    int    `json:"workspace_limit"`
	HumanSeatLimit    int    `json:"human_seat_limit"`
	IncludedStorageGB int    `json:"included_storage_gb"`
}

type UsageMeter struct {
	WorkspaceCount     int `json:"workspace_count"`
	HumanSeatCount     int `json:"human_seat_count"`
	StorageGB          int `json:"storage_gb"`
	MonthlyLaunchCount int `json:"monthly_launch_count"`
}

type UsageQuota struct {
	WorkspacesRemaining int `json:"workspaces_remaining"`
	HumanSeatsRemaining int `json:"human_seats_remaining"`
	StorageGBRemaining  int `json:"storage_gb_remaining"`
}

type UsageSummary struct {
	OrganizationID string     `json:"organization_id"`
	Plan           UsagePlan  `json:"plan"`
	Usage          UsageMeter `json:"usage"`
	Quota          UsageQuota `json:"quota"`
}

type WorkspaceUsageSummary struct {
	Usage       WorkspaceUsage `json:"usage"`
	Quota       WorkspaceQuota `json:"quota"`
	GeneratedAt string         `json:"generated_at"`
}

type WorkspaceUsage struct {
	BlobBytes   int64 `json:"blob_bytes"`
	BlobObjects int64 `json:"blob_objects"`
	Artifacts   int64 `json:"artifact_count"`
	Documents   int64 `json:"document_count"`
	Revisions   int64 `json:"document_revision_count"`
}

type WorkspaceQuota struct {
	MaxBlobBytes         int64 `json:"max_blob_bytes"`
	MaxArtifacts         int64 `json:"max_artifacts"`
	MaxDocuments         int64 `json:"max_documents"`
	MaxDocumentRevisions int64 `json:"max_document_revisions"`
	MaxUploadBytes       int64 `json:"max_upload_bytes"`
}

type WorkspaceHeartbeatRequest struct {
	Version                      string         `json:"version"`
	Build                        string         `json:"build"`
	HealthSummary                map[string]any `json:"health_summary"`
	ProjectionMaintenanceSummary map[string]any `json:"projection_maintenance_summary"`
	UsageSummary                 map[string]any `json:"usage_summary"`
	LastSuccessfulBackupAt       *string        `json:"last_successful_backup_at,omitempty"`
}

type WorkspaceInventoryItem struct {
	Workspace          Workspace         `json:"workspace"`
	OpenFailedJobs     []ProvisioningJob `json:"open_failed_jobs"`
	OpenFailedJobCount int               `json:"open_failed_job_count"`
}

type WorkspaceInventoryResponse struct {
	OrganizationID string                   `json:"organization_id"`
	Summary        UsageSummary             `json:"summary"`
	Workspaces     []WorkspaceInventoryItem `json:"workspaces"`
	NextCursor     string                   `json:"next_cursor,omitempty"`
}

type AuditEvent struct {
	ID             string         `json:"id"`
	EventType      string         `json:"event_type"`
	ActorAccountID *string        `json:"actor_account_id,omitempty"`
	OrganizationID *string        `json:"organization_id,omitempty"`
	WorkspaceID    *string        `json:"workspace_id,omitempty"`
	TargetType     string         `json:"target_type"`
	TargetID       string         `json:"target_id"`
	OccurredAt     string         `json:"occurred_at"`
	Metadata       map[string]any `json:"metadata,omitempty"`
}

type Page[T any] struct {
	Items      []T
	NextCursor string
}
