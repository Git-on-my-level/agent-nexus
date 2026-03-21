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
	ID             string `json:"id"`
	OrganizationID string `json:"organization_id"`
	Slug           string `json:"slug"`
	DisplayName    string `json:"display_name"`
	Status         string `json:"status"`
	Region         string `json:"region"`
	WorkspaceTier  string `json:"workspace_tier"`
	WorkspacePath  string `json:"workspace_path"`
	BaseURL        string `json:"base_url"`
	CreatedAt      string `json:"created_at"`
	UpdatedAt      string `json:"updated_at"`
}

type ProvisioningJob struct {
	ID             string  `json:"id"`
	OrganizationID string  `json:"organization_id"`
	WorkspaceID    string  `json:"workspace_id"`
	Kind           string  `json:"kind"`
	Status         string  `json:"status"`
	RequestedAt    string  `json:"requested_at"`
	StartedAt      *string `json:"started_at,omitempty"`
	FinishedAt     *string `json:"finished_at,omitempty"`
	FailureReason  *string `json:"failure_reason,omitempty"`
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
