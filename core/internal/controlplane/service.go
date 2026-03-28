package controlplane

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	cpstorage "organization-autorunner-core/internal/controlplane/storage"
	"organization-autorunner-core/internal/controlplaneauth"

	"github.com/google/uuid"
)

const (
	defaultSessionTTL   = 12 * time.Hour
	defaultCeremonyTTL  = 5 * time.Minute
	defaultLaunchTTL    = 10 * time.Minute
	defaultInviteTTL    = 7 * 24 * time.Hour
	defaultPageSize     = 50
	maxPageSize         = 200
	defaultWorkspaceURL = "http://127.0.0.1:8000/%s"
	defaultInviteURL    = "http://127.0.0.1:8100/invites/%s"
)

var (
	slugPattern            = regexp.MustCompile(`^[a-z0-9]+(?:[-][a-z0-9]+)*$`)
	emailSpacePattern      = regexp.MustCompile(`\s+`)
	displayNamePattern     = regexp.MustCompile(`\s+`)
	reservedWorkspaceSlugs = map[string]struct{}{
		"actors":      {},
		"api":         {},
		"artifacts":   {},
		"auth":        {},
		"boards":      {},
		"commitments": {},
		"control":     {},
		"dashboard":   {},
		"docs":        {},
		"events":      {},
		"inbox":       {},
		"invites":     {},
		"login":       {},
		"meta":        {},
		"receipts":    {},
		"reviews":     {},
		"snapshots":   {},
		"threads":     {},
		"version":     {},
	}
)

type Config struct {
	PublicBaseURL        string
	SessionTTL           time.Duration
	CeremonyTTL          time.Duration
	LaunchTTL            time.Duration
	InviteTTL            time.Duration
	WorkspaceURLTemplate string
	InviteURLTemplate    string
	PackedHosts          []PackedHost
	WorkspaceGrantSigner *controlplaneauth.WorkspaceHumanGrantSigner
	HostedScriptsDir     string
	VerifyCoreBinaryPath string
	VerifySchemaPath     string
	Stripe               StripeConfig
	Now                  func() time.Time
}

type Service struct {
	db                   *sql.DB
	workspaceRoot        string
	sessionTTL           time.Duration
	ceremonyTTL          time.Duration
	launchTTL            time.Duration
	inviteTTL            time.Duration
	workspaceURLTemplate string
	inviteURLTemplate    string
	packedHosts          []PackedHost
	workspaceGrantSigner *controlplaneauth.WorkspaceHumanGrantSigner
	hostedScriptsDir     string
	verifyCoreBinaryPath string
	verifySchemaPath     string
	stripe               StripeConfig
	now                  func() time.Time
}

type RequestIdentity struct {
	Account Account
	Session Session
}

type PageRequest struct {
	Limit  int
	Cursor string
}

type AuditFilter struct {
	OrganizationID string
	WorkspaceID    string
	AccountID      string
	Page           PageRequest
}

type JobsFilter struct {
	OrganizationID string
	WorkspaceID    string
	Page           PageRequest
}

func NewService(workspace *cpstorage.Workspace, config Config) *Service {
	sessionTTL := config.SessionTTL
	if sessionTTL <= 0 {
		sessionTTL = defaultSessionTTL
	}
	ceremonyTTL := config.CeremonyTTL
	if ceremonyTTL <= 0 {
		ceremonyTTL = defaultCeremonyTTL
	}
	launchTTL := config.LaunchTTL
	if launchTTL <= 0 {
		launchTTL = defaultLaunchTTL
	}
	inviteTTL := config.InviteTTL
	if inviteTTL <= 0 {
		inviteTTL = defaultInviteTTL
	}
	nowFn := config.Now
	if nowFn == nil {
		nowFn = func() time.Time { return time.Now().UTC() }
	}
	publicBaseURL, err := NormalizePublicBaseURL(config.PublicBaseURL)
	if err != nil {
		panic(fmt.Sprintf("invalid control-plane public base URL: %v", err))
	}
	workspaceURLTemplate := strings.TrimSpace(config.WorkspaceURLTemplate)
	if workspaceURLTemplate == "" {
		if publicBaseURL != "" {
			workspaceURLTemplate, err = WorkspaceURLTemplateFromPublicBase(publicBaseURL)
			if err != nil {
				panic(fmt.Sprintf("invalid control-plane public base URL: %v", err))
			}
		} else {
			workspaceURLTemplate = defaultWorkspaceURL
		}
	}
	inviteURLTemplate := strings.TrimSpace(config.InviteURLTemplate)
	if inviteURLTemplate == "" {
		if publicBaseURL != "" {
			inviteURLTemplate, err = InviteURLTemplateFromPublicBase(publicBaseURL)
			if err != nil {
				panic(fmt.Sprintf("invalid control-plane public base URL: %v", err))
			}
		} else {
			inviteURLTemplate = defaultInviteURL
		}
	}
	hostedScriptsDir := strings.TrimSpace(config.HostedScriptsDir)
	if hostedScriptsDir == "" {
		hostedScriptsDir = detectHostedScriptsDir()
	}
	verifyCoreBinaryPath := strings.TrimSpace(config.VerifyCoreBinaryPath)
	verifySchemaPath := strings.TrimSpace(config.VerifySchemaPath)
	if verifySchemaPath == "" {
		verifySchemaPath = detectSchemaPath()
	}
	packedHosts := config.PackedHosts
	if len(packedHosts) == 0 {
		packedHosts = []PackedHost{defaultPackedHost(workspace.Layout().RootDir)}
	} else {
		normalized := make([]PackedHost, 0, len(packedHosts))
		for _, host := range packedHosts {
			normalized = append(normalized, normalizePackedHost(host, workspace.Layout().RootDir))
		}
		packedHosts = normalized
	}
	return &Service{
		db:                   workspace.DB(),
		workspaceRoot:        workspace.Layout().RootDir,
		sessionTTL:           sessionTTL,
		ceremonyTTL:          ceremonyTTL,
		launchTTL:            launchTTL,
		inviteTTL:            inviteTTL,
		workspaceURLTemplate: workspaceURLTemplate,
		inviteURLTemplate:    inviteURLTemplate,
		packedHosts:          packedHosts,
		workspaceGrantSigner: config.WorkspaceGrantSigner,
		hostedScriptsDir:     hostedScriptsDir,
		verifyCoreBinaryPath: verifyCoreBinaryPath,
		verifySchemaPath:     verifySchemaPath,
		stripe:               normalizeStripeConfig(config.Stripe),
		now:                  nowFn,
	}
}

func (s *Service) StartPasskeyRegistration(ctx context.Context, email string, displayName string, rpID string, origin string) (string, map[string]any, Account, error) {
	email, err := normalizeEmail(email)
	if err != nil {
		return "", nil, Account{}, err
	}
	displayName, err = normalizeDisplayName(displayName)
	if err != nil {
		return "", nil, Account{}, err
	}
	rpID, origin, err = normalizeWebAuthnInputs(rpID, origin)
	if err != nil {
		return "", nil, Account{}, err
	}

	now := s.now()
	nowText := now.Format(time.RFC3339Nano)
	expiresAt := now.Add(s.ceremonyTTL).Format(time.RFC3339Nano)
	sessionID := "cpreg_" + uuid.NewString()
	userHandle, err := randomBytes(32)
	if err != nil {
		return "", nil, Account{}, internalError("failed to generate passkey user handle")
	}
	challenge, err := randomBase64URL(32)
	if err != nil {
		return "", nil, Account{}, internalError("failed to generate passkey challenge")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", nil, Account{}, internalError("failed to start registration transaction")
	}
	defer tx.Rollback()

	account, registered, loadErr := loadAccountByEmailTx(ctx, tx, email)
	switch {
	case loadErr == nil && registered:
		return "", nil, Account{}, &APIError{Status: http.StatusConflict, Code: "account_exists", Message: "account already exists"}
	case loadErr == nil:
		if _, execErr := tx.ExecContext(
			ctx,
			`UPDATE accounts SET display_name = ?, status = ? WHERE id = ?`,
			displayName,
			"active",
			account.ID,
		); execErr != nil {
			return "", nil, Account{}, internalError("failed to update existing provisional account")
		}
		account.DisplayName = displayName
		account.Status = "active"
	case loadErr == sql.ErrNoRows:
		account = Account{
			ID:          "acct_" + uuid.NewString(),
			Email:       email,
			DisplayName: displayName,
			Status:      "active",
			CreatedAt:   nowText,
		}
		if _, execErr := tx.ExecContext(
			ctx,
			`INSERT INTO accounts(id, email, display_name, status, created_at, last_login_at, passkey_registered_at)
			 VALUES (?, ?, ?, ?, ?, NULL, NULL)`,
			account.ID,
			account.Email,
			account.DisplayName,
			account.Status,
			account.CreatedAt,
		); execErr != nil {
			return "", nil, Account{}, internalError("failed to create provisional account")
		}
	default:
		return "", nil, Account{}, internalError("failed to load account")
	}

	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO passkey_ceremonies(id, kind, account_id, email, display_name, user_handle, challenge, rp_id, origin, credential_id_hint, expires_at, created_at, consumed_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, '', ?, ?, NULL)`,
		sessionID,
		"registration",
		account.ID,
		email,
		displayName,
		userHandle,
		challenge,
		rpID,
		origin,
		expiresAt,
		nowText,
	); err != nil {
		return "", nil, Account{}, internalError("failed to store passkey ceremony")
	}

	if err := tx.Commit(); err != nil {
		return "", nil, Account{}, internalError("failed to commit registration ceremony")
	}

	return sessionID, map[string]any{
		"challenge": challenge,
		"rp": map[string]any{
			"id":   rpID,
			"name": "Organization Autorunner",
		},
		"user": map[string]any{
			"id":          base64.RawURLEncoding.EncodeToString(userHandle),
			"name":        email,
			"displayName": displayName,
		},
		"pubKeyCredParams": []map[string]any{
			{"type": "public-key", "alg": -7},
		},
		"timeout":     300000,
		"attestation": "none",
		"authenticatorSelection": map[string]any{
			"residentKey":      "required",
			"userVerification": "preferred",
		},
	}, account, nil
}

func (s *Service) FinishPasskeyRegistration(ctx context.Context, sessionID string, credential map[string]any, rpID string, origin string, inviteToken string) (Account, Session, error) {
	rpID, origin, err := normalizeWebAuthnInputs(rpID, origin)
	if err != nil {
		return Account{}, Session{}, err
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return Account{}, Session{}, invalidRequest("registration_session_id is required")
	}
	if len(credential) == 0 {
		return Account{}, Session{}, invalidRequest("credential is required")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Account{}, Session{}, internalError("failed to begin registration completion")
	}
	defer tx.Rollback()

	ceremony, err := loadCeremonyTx(ctx, tx, sessionID)
	if err != nil {
		if err == sql.ErrNoRows {
			return Account{}, Session{}, &APIError{Status: http.StatusUnauthorized, Code: "session_expired", Message: "registration session is invalid or expired"}
		}
		return Account{}, Session{}, internalError("failed to load registration session")
	}
	if ceremony.Kind != "registration" || ceremony.ConsumedAt != nil || isExpired(ceremony.ExpiresAt, s.now()) {
		return Account{}, Session{}, &APIError{Status: http.StatusUnauthorized, Code: "session_expired", Message: "registration session is invalid or expired"}
	}
	if ceremony.RPID != rpID || ceremony.Origin != origin {
		return Account{}, Session{}, &APIError{Status: http.StatusUnauthorized, Code: "credential_invalid", Message: "credential origin does not match the registration session"}
	}

	credentialID, userHandle, rawJSON, err := validateCredential(credential, "webauthn.create", ceremony.Challenge, origin)
	if err != nil {
		return Account{}, Session{}, err
	}
	if len(userHandle) > 0 && !equalBytes(userHandle, ceremony.UserHandle) {
		return Account{}, Session{}, &APIError{Status: http.StatusUnauthorized, Code: "credential_invalid", Message: "credential user handle does not match the registration session"}
	}

	account, _, err := loadAccountByIDTx(ctx, tx, ceremony.AccountID)
	if err != nil {
		if err == sql.ErrNoRows {
			return Account{}, Session{}, &APIError{Status: http.StatusUnauthorized, Code: "session_expired", Message: "registration session account no longer exists"}
		}
		return Account{}, Session{}, internalError("failed to load registration account")
	}

	registered, err := accountHasPasskeyTx(ctx, tx, account.ID)
	if err != nil {
		return Account{}, Session{}, internalError("failed to check existing passkey")
	}
	if registered {
		return Account{}, Session{}, &APIError{Status: http.StatusConflict, Code: "account_exists", Message: "account already has a registered passkey"}
	}

	now := s.now()
	nowText := now.Format(time.RFC3339Nano)
	if err := consumePasskeyCeremonyTx(
		ctx,
		tx,
		ceremony.ID,
		nowText,
		"failed to consume registration ceremony",
		"registration session is invalid or expired",
	); err != nil {
		return Account{}, Session{}, err
	}
	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO passkey_credentials(
			credential_id, account_id, user_handle, public_key, attestation_type, transport, sign_count,
			backup_eligible, backup_state, aaguid, attachment, credential_json, created_at, last_used_at
		 ) VALUES (?, ?, ?, X'', ?, '', 0, 0, 0, X'', '', ?, ?, NULL)`,
		credentialID,
		account.ID,
		ceremony.UserHandle,
		"webauthn-json",
		rawJSON,
		nowText,
	); err != nil {
		if isSQLiteConstraint(err) {
			return Account{}, Session{}, &APIError{Status: http.StatusUnauthorized, Code: "credential_invalid", Message: "passkey credential is already registered"}
		}
		return Account{}, Session{}, internalError("failed to persist passkey credential")
	}

	if _, err := tx.ExecContext(
		ctx,
		`UPDATE accounts
		 SET display_name = ?, passkey_registered_at = ?, last_login_at = ?
		 WHERE id = ?`,
		ceremony.DisplayName,
		nowText,
		nowText,
		account.ID,
	); err != nil {
		return Account{}, Session{}, internalError("failed to activate account")
	}
	account.DisplayName = ceremony.DisplayName
	account.LastLoginAt = stringPtr(nowText)

	if err := acceptInviteTokenTx(ctx, tx, account, inviteToken, now); err != nil {
		return Account{}, Session{}, err
	}

	session, err := issueSessionTx(ctx, tx, account.ID, now, s.sessionTTL)
	if err != nil {
		return Account{}, Session{}, err
	}
	if err := insertAuditEventTx(ctx, tx, AuditEvent{
		ID:         "audit_" + uuid.NewString(),
		EventType:  "account_registered",
		TargetType: "account",
		TargetID:   account.ID,
		OccurredAt: nowText,
		Metadata: map[string]any{
			"email": credentialSafeEmail(account.Email),
		},
	}, stringPtr(account.ID)); err != nil {
		return Account{}, Session{}, err
	}
	if err := insertAuditEventTx(ctx, tx, AuditEvent{
		ID:         "audit_" + uuid.NewString(),
		EventType:  "account_session_issued",
		TargetType: "session",
		TargetID:   session.ID,
		OccurredAt: nowText,
	}, stringPtr(account.ID)); err != nil {
		return Account{}, Session{}, err
	}

	if err := tx.Commit(); err != nil {
		return Account{}, Session{}, internalError("failed to commit registration")
	}
	return account, session, nil
}

func (s *Service) StartAccountSession(ctx context.Context, email string, rpID string, origin string) (string, map[string]any, AccountHint, error) {
	email, err := normalizeEmail(email)
	if err != nil {
		return "", nil, AccountHint{}, err
	}
	rpID, origin, err = normalizeWebAuthnInputs(rpID, origin)
	if err != nil {
		return "", nil, AccountHint{}, err
	}

	account, registered, err := loadAccountByEmail(ctx, s.db, email)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", nil, AccountHint{}, &APIError{Status: http.StatusNotFound, Code: "not_found", Message: "account not found"}
		}
		return "", nil, AccountHint{}, internalError("failed to load account")
	}
	if account.Status != "active" {
		return "", nil, AccountHint{}, &APIError{Status: http.StatusBadRequest, Code: "account_disabled", Message: "account is disabled"}
	}
	if !registered {
		return "", nil, AccountHint{}, &APIError{Status: http.StatusNotFound, Code: "not_found", Message: "account not found"}
	}
	credentialIDs, userHandle, err := loadCredentialIDsForAccount(ctx, s.db, account.ID)
	if err != nil {
		return "", nil, AccountHint{}, internalError("failed to load passkey credentials")
	}
	if len(credentialIDs) == 0 {
		return "", nil, AccountHint{}, &APIError{Status: http.StatusNotFound, Code: "not_found", Message: "account not found"}
	}

	challenge, err := randomBase64URL(32)
	if err != nil {
		return "", nil, AccountHint{}, internalError("failed to generate login challenge")
	}
	sessionID := "cplogin_" + uuid.NewString()
	now := s.now()
	if _, err := s.db.ExecContext(
		ctx,
		`INSERT INTO passkey_ceremonies(id, kind, account_id, email, display_name, user_handle, challenge, rp_id, origin, credential_id_hint, expires_at, created_at, consumed_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, '', ?, ?, NULL)`,
		sessionID,
		"login",
		account.ID,
		account.Email,
		account.DisplayName,
		userHandle,
		challenge,
		rpID,
		origin,
		now.Add(s.ceremonyTTL).Format(time.RFC3339Nano),
		now.Format(time.RFC3339Nano),
	); err != nil {
		return "", nil, AccountHint{}, internalError("failed to store login ceremony")
	}

	allowCredentials := make([]map[string]any, 0, len(credentialIDs))
	for _, credentialID := range credentialIDs {
		allowCredentials = append(allowCredentials, map[string]any{
			"id":   credentialID,
			"type": "public-key",
		})
	}

	return sessionID, map[string]any{
		"challenge":        challenge,
		"rpId":             rpID,
		"timeout":          300000,
		"allowCredentials": allowCredentials,
		"userVerification": "preferred",
	}, AccountHint{Email: account.Email, DisplayName: account.DisplayName}, nil
}

func (s *Service) FinishAccountSession(ctx context.Context, sessionID string, credential map[string]any, rpID string, origin string, inviteToken string) (Account, Session, error) {
	rpID, origin, err := normalizeWebAuthnInputs(rpID, origin)
	if err != nil {
		return Account{}, Session{}, err
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return Account{}, Session{}, invalidRequest("session_id is required")
	}
	if len(credential) == 0 {
		return Account{}, Session{}, invalidRequest("credential is required")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Account{}, Session{}, internalError("failed to begin login completion")
	}
	defer tx.Rollback()

	ceremony, err := loadCeremonyTx(ctx, tx, sessionID)
	if err != nil {
		if err == sql.ErrNoRows {
			return Account{}, Session{}, &APIError{Status: http.StatusUnauthorized, Code: "session_expired", Message: "session is invalid or expired"}
		}
		return Account{}, Session{}, internalError("failed to load login ceremony")
	}
	if ceremony.Kind != "login" || ceremony.ConsumedAt != nil || isExpired(ceremony.ExpiresAt, s.now()) {
		return Account{}, Session{}, &APIError{Status: http.StatusUnauthorized, Code: "session_expired", Message: "session is invalid or expired"}
	}
	if ceremony.RPID != rpID || ceremony.Origin != origin {
		return Account{}, Session{}, &APIError{Status: http.StatusUnauthorized, Code: "credential_invalid", Message: "credential origin does not match the sign-in session"}
	}

	credentialID, userHandle, _, err := validateCredential(credential, "webauthn.get", ceremony.Challenge, origin)
	if err != nil {
		return Account{}, Session{}, err
	}
	if len(userHandle) > 0 && !equalBytes(userHandle, ceremony.UserHandle) {
		return Account{}, Session{}, &APIError{Status: http.StatusUnauthorized, Code: "credential_invalid", Message: "credential user handle does not match the stored account"}
	}

	account, _, err := loadAccountByIDTx(ctx, tx, ceremony.AccountID)
	if err != nil {
		if err == sql.ErrNoRows {
			return Account{}, Session{}, &APIError{Status: http.StatusUnauthorized, Code: "invalid_token", Message: "account no longer exists"}
		}
		return Account{}, Session{}, internalError("failed to load session account")
	}
	if account.Status != "active" {
		return Account{}, Session{}, &APIError{Status: http.StatusUnauthorized, Code: "invalid_token", Message: "account is disabled"}
	}

	storedUserHandle, err := loadCredentialUserHandleTx(ctx, tx, ceremony.AccountID, credentialID)
	if err != nil {
		if err == sql.ErrNoRows {
			return Account{}, Session{}, &APIError{Status: http.StatusUnauthorized, Code: "credential_invalid", Message: "passkey credential is not registered for this account"}
		}
		return Account{}, Session{}, internalError("failed to validate passkey credential")
	}
	if !equalBytes(storedUserHandle, ceremony.UserHandle) {
		return Account{}, Session{}, &APIError{Status: http.StatusUnauthorized, Code: "credential_invalid", Message: "passkey credential does not match the stored account"}
	}

	now := s.now()
	nowText := now.Format(time.RFC3339Nano)
	if err := consumePasskeyCeremonyTx(
		ctx,
		tx,
		ceremony.ID,
		nowText,
		"failed to consume login ceremony",
		"session is invalid or expired",
	); err != nil {
		return Account{}, Session{}, err
	}
	if _, err := tx.ExecContext(
		ctx,
		`UPDATE passkey_credentials SET last_used_at = ? WHERE account_id = ? AND credential_id = ?`,
		nowText,
		account.ID,
		credentialID,
	); err != nil {
		return Account{}, Session{}, internalError("failed to update passkey usage")
	}
	if _, err := tx.ExecContext(ctx, `UPDATE accounts SET last_login_at = ? WHERE id = ?`, nowText, account.ID); err != nil {
		return Account{}, Session{}, internalError("failed to update last login")
	}
	account.LastLoginAt = stringPtr(nowText)

	if err := acceptInviteTokenTx(ctx, tx, account, inviteToken, now); err != nil {
		return Account{}, Session{}, err
	}

	session, err := issueSessionTx(ctx, tx, account.ID, now, s.sessionTTL)
	if err != nil {
		return Account{}, Session{}, err
	}
	if err := insertAuditEventTx(ctx, tx, AuditEvent{
		ID:         "audit_" + uuid.NewString(),
		EventType:  "account_login",
		TargetType: "account",
		TargetID:   account.ID,
		OccurredAt: nowText,
	}, stringPtr(account.ID)); err != nil {
		return Account{}, Session{}, err
	}
	if err := insertAuditEventTx(ctx, tx, AuditEvent{
		ID:         "audit_" + uuid.NewString(),
		EventType:  "account_session_issued",
		TargetType: "session",
		TargetID:   session.ID,
		OccurredAt: nowText,
	}, stringPtr(account.ID)); err != nil {
		return Account{}, Session{}, err
	}

	if err := tx.Commit(); err != nil {
		return Account{}, Session{}, internalError("failed to commit login")
	}
	return account, session, nil
}

func (s *Service) AuthenticateAccessToken(ctx context.Context, rawToken string) (RequestIdentity, error) {
	rawToken = strings.TrimSpace(rawToken)
	if rawToken == "" {
		return RequestIdentity{}, &APIError{Status: http.StatusUnauthorized, Code: "auth_required", Message: "authorization token is required"}
	}

	nowText := s.now().Format(time.RFC3339Nano)
	row := s.db.QueryRowContext(
		ctx,
		`SELECT
			s.id,
			s.account_id,
			s.issued_at,
			s.expires_at,
			a.email,
			a.display_name,
			a.status,
			a.created_at,
			a.last_login_at
		 FROM account_sessions s
		 JOIN accounts a ON a.id = s.account_id
		 WHERE s.token_hash = ? AND s.revoked_at IS NULL AND s.expires_at > ?`,
		hashToken(rawToken),
		nowText,
	)

	var (
		sessionID     string
		accountID     string
		issuedAt      string
		expiresAt     string
		email         string
		displayName   string
		status        string
		createdAt     string
		lastLoginAtNS sql.NullString
	)
	if err := row.Scan(&sessionID, &accountID, &issuedAt, &expiresAt, &email, &displayName, &status, &createdAt, &lastLoginAtNS); err != nil {
		if err == sql.ErrNoRows {
			return RequestIdentity{}, &APIError{Status: http.StatusUnauthorized, Code: "invalid_token", Message: "authorization token is invalid or expired"}
		}
		return RequestIdentity{}, internalError("failed to authenticate session")
	}
	lastLoginAt := nullableString(lastLoginAtNS)
	return RequestIdentity{
		Account: Account{
			ID:          accountID,
			Email:       email,
			DisplayName: displayName,
			Status:      status,
			CreatedAt:   createdAt,
			LastLoginAt: lastLoginAt,
		},
		Session: Session{
			ID:        sessionID,
			AccountID: accountID,
			IssuedAt:  issuedAt,
			ExpiresAt: expiresAt,
		},
	}, nil
}

func (s *Service) RevokeCurrentSession(ctx context.Context, identity RequestIdentity) error {
	nowText := s.now().Format(time.RFC3339Nano)
	result, err := s.db.ExecContext(
		ctx,
		`UPDATE account_sessions SET revoked_at = ? WHERE id = ? AND revoked_at IS NULL`,
		nowText,
		identity.Session.ID,
	)
	if err != nil {
		return internalError("failed to revoke session")
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return internalError("failed to confirm session revocation")
	}
	if rowsAffected == 0 {
		return &APIError{Status: http.StatusUnauthorized, Code: "invalid_token", Message: "authorization token is invalid or expired"}
	}
	if err := insertAuditEvent(ctx, s.db, AuditEvent{
		ID:         "audit_" + uuid.NewString(),
		EventType:  "account_session_revoked",
		TargetType: "session",
		TargetID:   identity.Session.ID,
		OccurredAt: nowText,
	}, stringPtr(identity.Account.ID)); err != nil {
		return err
	}
	return nil
}

func (s *Service) ListAccounts(ctx context.Context, _ RequestIdentity, page PageRequest) (Page[Account], error) {
	limit := normalizePageLimit(page.Limit)
	sortAt, sortID, err := decodeCursor(page.Cursor)
	if err != nil {
		return Page[Account]{}, invalidRequest("cursor is invalid")
	}

	query := `SELECT id, email, display_name, status, created_at, last_login_at
		FROM accounts
		WHERE passkey_registered_at IS NOT NULL`
	args := []any{}
	if sortAt != "" {
		query += ` AND (created_at > ? OR (created_at = ? AND id > ?))`
		args = append(args, sortAt, sortAt, sortID)
	}
	query += ` ORDER BY created_at ASC, id ASC LIMIT ?`
	args = append(args, limit+1)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return Page[Account]{}, internalError("failed to list accounts")
	}
	defer rows.Close()

	accounts := make([]Account, 0, limit+1)
	for rows.Next() {
		var (
			account     Account
			lastLoginAt sql.NullString
		)
		if err := rows.Scan(&account.ID, &account.Email, &account.DisplayName, &account.Status, &account.CreatedAt, &lastLoginAt); err != nil {
			return Page[Account]{}, internalError("failed to scan account")
		}
		account.LastLoginAt = nullableString(lastLoginAt)
		accounts = append(accounts, account)
	}
	if err := rows.Err(); err != nil {
		return Page[Account]{}, internalError("failed to iterate accounts")
	}
	return pageFromItems(accounts, limit, func(account Account) (string, string) {
		return account.CreatedAt, account.ID
	}), nil
}

type storedCeremony struct {
	ID          string
	Kind        string
	AccountID   string
	Email       string
	DisplayName string
	UserHandle  []byte
	Challenge   string
	RPID        string
	Origin      string
	ExpiresAt   string
	ConsumedAt  *string
}

func loadCeremonyTx(ctx context.Context, tx *sql.Tx, id string) (storedCeremony, error) {
	row := tx.QueryRowContext(
		ctx,
		`SELECT id, kind, COALESCE(account_id, ''), email, display_name, user_handle, challenge, rp_id, origin, expires_at, consumed_at
		 FROM passkey_ceremonies WHERE id = ?`,
		id,
	)
	var (
		ceremony   storedCeremony
		consumedAt sql.NullString
	)
	if err := row.Scan(&ceremony.ID, &ceremony.Kind, &ceremony.AccountID, &ceremony.Email, &ceremony.DisplayName, &ceremony.UserHandle, &ceremony.Challenge, &ceremony.RPID, &ceremony.Origin, &ceremony.ExpiresAt, &consumedAt); err != nil {
		return storedCeremony{}, err
	}
	ceremony.ConsumedAt = nullableString(consumedAt)
	return ceremony, nil
}

func consumePasskeyCeremonyTx(ctx context.Context, tx *sql.Tx, ceremonyID string, consumedAt string, internalMessage string, expiredMessage string) error {
	for attempt := 0; attempt < 5; attempt++ {
		result, err := tx.ExecContext(
			ctx,
			`UPDATE passkey_ceremonies
			 SET consumed_at = ?
			 WHERE id = ?
			   AND consumed_at IS NULL`,
			consumedAt,
			ceremonyID,
		)
		if err != nil {
			if isSQLiteBusyError(err) {
				if attempt < 4 {
					time.Sleep(10 * time.Millisecond)
					continue
				}
				return &APIError{Status: http.StatusUnauthorized, Code: "session_expired", Message: expiredMessage}
			}
			return internalError(internalMessage)
		}
		rowsAffected, err := result.RowsAffected()
		if err != nil {
			return internalError(internalMessage)
		}
		if rowsAffected == 0 {
			return &APIError{Status: http.StatusUnauthorized, Code: "session_expired", Message: expiredMessage}
		}
		return nil
	}
	return &APIError{Status: http.StatusUnauthorized, Code: "session_expired", Message: expiredMessage}
}

func issueSessionTx(ctx context.Context, tx *sql.Tx, accountID string, now time.Time, ttl time.Duration) (Session, error) {
	rawToken, err := randomBase64URL(32)
	if err != nil {
		return Session{}, internalError("failed to generate session token")
	}
	session := Session{
		ID:          "sess_" + uuid.NewString(),
		AccountID:   accountID,
		IssuedAt:    now.Format(time.RFC3339Nano),
		ExpiresAt:   now.Add(ttl).Format(time.RFC3339Nano),
		AccessToken: rawToken,
	}
	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO account_sessions(id, account_id, token_hash, issued_at, expires_at, revoked_at)
		 VALUES (?, ?, ?, ?, ?, NULL)`,
		session.ID,
		session.AccountID,
		hashToken(rawToken),
		session.IssuedAt,
		session.ExpiresAt,
	); err != nil {
		return Session{}, internalError("failed to persist session")
	}
	return session, nil
}

func acceptInviteTokenTx(ctx context.Context, tx *sql.Tx, account Account, inviteToken string, now time.Time) error {
	inviteToken = strings.TrimSpace(inviteToken)
	if inviteToken == "" {
		return nil
	}

	type inviteRef struct {
		ID             string
		OrganizationID string
		Email          string
		Role           string
	}

	var invite inviteRef
	err := tx.QueryRowContext(
		ctx,
		`SELECT id, organization_id, email, role
		 FROM organization_invites
		 WHERE token_hash = ?
		   AND status = 'pending'
		   AND expires_at > ?`,
		hashToken(inviteToken),
		now.Format(time.RFC3339Nano),
	).Scan(&invite.ID, &invite.OrganizationID, &invite.Email, &invite.Role)
	if err != nil {
		if err == sql.ErrNoRows {
			return &APIError{Status: http.StatusUnauthorized, Code: "invalid_token", Message: "invite token is invalid or expired"}
		}
		return internalError("failed to load pending invite")
	}

	if invite.Email != account.Email {
		return &APIError{Status: http.StatusUnauthorized, Code: "invalid_token", Message: "invite token is invalid or expired"}
	}

	nowText := now.Format(time.RFC3339Nano)
	result, err := tx.ExecContext(
		ctx,
		`UPDATE organization_invites
		 SET status = 'accepted', accepted_at = ?, accepted_by_account_id = ?
		 WHERE id = ? AND status = 'pending'`,
		nowText,
		account.ID,
		invite.ID,
	)
	if err != nil {
		return internalError("failed to accept organization invite")
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return internalError("failed to confirm organization invite acceptance")
	}
	if rowsAffected == 0 {
		return &APIError{Status: http.StatusUnauthorized, Code: "invalid_token", Message: "invite token is invalid or expired"}
	}

	if err := upsertMembershipTx(ctx, tx, Membership{
		ID:             "mem_" + uuid.NewString(),
		OrganizationID: invite.OrganizationID,
		AccountID:      account.ID,
		Role:           invite.Role,
		Status:         "active",
		CreatedAt:      nowText,
	}); err != nil {
		return err
	}
	if err := insertAuditEventTx(ctx, tx, AuditEvent{
		ID:             "audit_" + uuid.NewString(),
		EventType:      "organization_invite_accepted",
		OrganizationID: stringPtr(invite.OrganizationID),
		TargetType:     "organization_invite",
		TargetID:       invite.ID,
		OccurredAt:     nowText,
		Metadata: map[string]any{
			"account_id": account.ID,
		},
	}, stringPtr(account.ID)); err != nil {
		return err
	}
	return nil
}

func insertAuditEvent(ctx context.Context, db *sql.DB, event AuditEvent, actorAccountID *string) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return internalError("failed to begin audit transaction")
	}
	defer tx.Rollback()
	if err := insertAuditEventTx(ctx, tx, event, actorAccountID); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return internalError("failed to commit audit event")
	}
	return nil
}

func insertAuditEventTx(ctx context.Context, tx *sql.Tx, event AuditEvent, actorAccountID *string) error {
	if strings.TrimSpace(event.ID) == "" {
		event.ID = "audit_" + uuid.NewString()
	}
	metadataJSON, err := json.Marshal(nonNilMap(event.Metadata))
	if err != nil {
		return internalError("failed to encode audit metadata")
	}
	var organizationID any
	if event.OrganizationID != nil {
		organizationID = *event.OrganizationID
	}
	var workspaceID any
	if event.WorkspaceID != nil {
		workspaceID = *event.WorkspaceID
	}
	var actor any
	if actorAccountID != nil {
		actor = *actorAccountID
	}
	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO audit_events(id, event_type, actor_account_id, organization_id, workspace_id, target_type, target_id, metadata_json, occurred_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		event.ID,
		event.EventType,
		actor,
		organizationID,
		workspaceID,
		event.TargetType,
		event.TargetID,
		string(metadataJSON),
		event.OccurredAt,
	); err != nil {
		return internalError("failed to insert audit event")
	}
	return nil
}

func validateCredential(credential map[string]any, expectedType string, expectedChallenge string, expectedOrigin string) (string, []byte, string, error) {
	rawJSON, err := json.Marshal(credential)
	if err != nil {
		return "", nil, "", invalidRequest("credential is invalid")
	}
	rawID := strings.TrimSpace(stringValue(credential["rawId"]))
	if rawID == "" {
		rawID = strings.TrimSpace(stringValue(credential["id"]))
	}
	if rawID == "" {
		return "", nil, "", &APIError{Status: http.StatusUnauthorized, Code: "credential_invalid", Message: "credential id is required"}
	}

	response, _ := credential["response"].(map[string]any)
	if response == nil {
		return "", nil, "", &APIError{Status: http.StatusUnauthorized, Code: "credential_invalid", Message: "credential response is required"}
	}
	clientDataRaw := strings.TrimSpace(stringValue(response["clientDataJSON"]))
	if clientDataRaw == "" {
		return "", nil, "", &APIError{Status: http.StatusUnauthorized, Code: "credential_invalid", Message: "client data is required"}
	}
	clientDataJSON, err := base64.RawURLEncoding.DecodeString(clientDataRaw)
	if err != nil {
		clientDataJSON, err = base64.StdEncoding.DecodeString(clientDataRaw)
		if err != nil {
			return "", nil, "", &APIError{Status: http.StatusUnauthorized, Code: "credential_invalid", Message: "client data is invalid"}
		}
	}
	var clientData struct {
		Challenge string `json:"challenge"`
		Origin    string `json:"origin"`
		Type      string `json:"type"`
	}
	if err := json.Unmarshal(clientDataJSON, &clientData); err != nil {
		return "", nil, "", &APIError{Status: http.StatusUnauthorized, Code: "credential_invalid", Message: "client data is invalid"}
	}
	if strings.TrimSpace(clientData.Challenge) != expectedChallenge {
		return "", nil, "", &APIError{Status: http.StatusUnauthorized, Code: "credential_invalid", Message: "credential challenge does not match the session"}
	}
	if strings.TrimSpace(clientData.Origin) != expectedOrigin {
		return "", nil, "", &APIError{Status: http.StatusUnauthorized, Code: "credential_invalid", Message: "credential origin does not match the session"}
	}
	if strings.TrimSpace(clientData.Type) != expectedType {
		return "", nil, "", &APIError{Status: http.StatusUnauthorized, Code: "credential_invalid", Message: "credential type does not match the session"}
	}

	var userHandle []byte
	if rawUserHandle := strings.TrimSpace(stringValue(response["userHandle"])); rawUserHandle != "" {
		userHandle, err = decodeBase64Either(rawUserHandle)
		if err != nil {
			return "", nil, "", &APIError{Status: http.StatusUnauthorized, Code: "credential_invalid", Message: "credential user handle is invalid"}
		}
	}
	return rawID, userHandle, string(rawJSON), nil
}

func normalizeEmail(raw string) (string, error) {
	email := strings.ToLower(strings.TrimSpace(emailSpacePattern.ReplaceAllString(raw, "")))
	if email == "" || !strings.Contains(email, "@") || strings.HasPrefix(email, "@") || strings.HasSuffix(email, "@") {
		return "", invalidRequest("email must be a non-empty email address")
	}
	return email, nil
}

func normalizeDisplayName(raw string) (string, error) {
	displayName := strings.TrimSpace(displayNamePattern.ReplaceAllString(raw, " "))
	if displayName == "" {
		return "", invalidRequest("display_name is required")
	}
	if len(displayName) > 120 {
		return "", invalidRequest("display_name must be 120 characters or fewer")
	}
	return displayName, nil
}

func normalizeSlug(raw string) (string, error) {
	slug := strings.ToLower(strings.TrimSpace(raw))
	if !slugPattern.MatchString(slug) {
		return "", invalidRequest("slug must contain lowercase letters, numbers, and single dashes")
	}
	return slug, nil
}

func validateReservedWorkspaceSlug(slug string) error {
	if _, reserved := reservedWorkspaceSlugs[slug]; reserved {
		return invalidRequest("workspace slug is reserved for control-plane routes")
	}
	return nil
}

func normalizeWebAuthnInputs(rpID string, origin string) (string, string, error) {
	rpID = strings.ToLower(strings.TrimSpace(rpID))
	if rpID == "" {
		return "", "", &APIError{Status: http.StatusServiceUnavailable, Code: "invalid_request", Message: "WebAuthn RP ID is required"}
	}
	origin = strings.TrimSpace(origin)
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", "", &APIError{Status: http.StatusServiceUnavailable, Code: "invalid_request", Message: "WebAuthn origin is invalid"}
	}
	normalizedOrigin := strings.ToLower(parsed.Scheme) + "://" + strings.ToLower(parsed.Host)
	host := strings.ToLower(parsed.Hostname())
	if host != rpID && !strings.HasSuffix(host, "."+rpID) {
		return "", "", &APIError{Status: http.StatusServiceUnavailable, Code: "invalid_request", Message: "WebAuthn RP ID must match the request origin"}
	}
	return rpID, normalizedOrigin, nil
}

func pageFromItems[T any](items []T, limit int, sortValues func(T) (string, string)) Page[T] {
	page := Page[T]{Items: items}
	if len(items) <= limit {
		return page
	}
	page.Items = append([]T(nil), items[:limit]...)
	sortAt, sortID := sortValues(items[limit-1])
	page.NextCursor = encodeCursor(sortAt, sortID)
	return page
}

func encodeCursor(sortAt string, id string) string {
	if sortAt == "" || id == "" {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString([]byte(sortAt + "\n" + id))
}

func decodeCursor(raw string) (string, string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", "", nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return "", "", err
	}
	parts := strings.SplitN(string(decoded), "\n", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("invalid cursor")
	}
	return parts[0], parts[1], nil
}

func normalizePageLimit(raw int) int {
	switch {
	case raw <= 0:
		return defaultPageSize
	case raw > maxPageSize:
		return maxPageSize
	default:
		return raw
	}
}

func hashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func randomBytes(size int) ([]byte, error) {
	value := make([]byte, size)
	if _, err := rand.Read(value); err != nil {
		return nil, err
	}
	return value, nil
}

func randomBase64URL(size int) (string, error) {
	value, err := randomBytes(size)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}

func loadAccountByEmail(ctx context.Context, db *sql.DB, email string) (Account, bool, error) {
	return loadAccountByEmailTx(ctx, nilTxShim{db: db}, email)
}

type queryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

type nilTxShim struct {
	db *sql.DB
}

func (s nilTxShim) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	return s.db.QueryRowContext(ctx, query, args...)
}

func loadAccountByEmailTx(ctx context.Context, q queryer, email string) (Account, bool, error) {
	if q == nil {
		return Account{}, false, sql.ErrNoRows
	}
	var (
		account             Account
		lastLoginAt         sql.NullString
		passkeyRegisteredAt sql.NullString
	)
	err := q.QueryRowContext(
		ctx,
		`SELECT id, email, display_name, status, created_at, last_login_at, passkey_registered_at
		 FROM accounts WHERE email = ?`,
		email,
	).Scan(&account.ID, &account.Email, &account.DisplayName, &account.Status, &account.CreatedAt, &lastLoginAt, &passkeyRegisteredAt)
	if err != nil {
		return Account{}, false, err
	}
	account.LastLoginAt = nullableString(lastLoginAt)
	return account, passkeyRegisteredAt.Valid, nil
}

func loadAccountByIDTx(ctx context.Context, tx *sql.Tx, accountID string) (Account, bool, error) {
	var (
		account             Account
		lastLoginAt         sql.NullString
		passkeyRegisteredAt sql.NullString
	)
	err := tx.QueryRowContext(
		ctx,
		`SELECT id, email, display_name, status, created_at, last_login_at, passkey_registered_at
		 FROM accounts WHERE id = ?`,
		accountID,
	).Scan(&account.ID, &account.Email, &account.DisplayName, &account.Status, &account.CreatedAt, &lastLoginAt, &passkeyRegisteredAt)
	if err != nil {
		return Account{}, false, err
	}
	account.LastLoginAt = nullableString(lastLoginAt)
	return account, passkeyRegisteredAt.Valid, nil
}

func accountHasPasskeyTx(ctx context.Context, tx *sql.Tx, accountID string) (bool, error) {
	var exists bool
	err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM passkey_credentials WHERE account_id = ? LIMIT 1)`, accountID).Scan(&exists)
	return exists, err
}

func loadCredentialIDsForAccount(ctx context.Context, db *sql.DB, accountID string) ([]string, []byte, error) {
	rows, err := db.QueryContext(
		ctx,
		`SELECT credential_id, user_handle
		 FROM passkey_credentials
		 WHERE account_id = ?
		 ORDER BY created_at ASC, credential_id ASC`,
		accountID,
	)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	var (
		credentialIDs []string
		userHandle    []byte
	)
	for rows.Next() {
		var credentialID string
		var handle []byte
		if err := rows.Scan(&credentialID, &handle); err != nil {
			return nil, nil, err
		}
		credentialIDs = append(credentialIDs, credentialID)
		if len(userHandle) == 0 {
			userHandle = append([]byte(nil), handle...)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	return credentialIDs, userHandle, nil
}

func loadCredentialUserHandleTx(ctx context.Context, tx *sql.Tx, accountID string, credentialID string) ([]byte, error) {
	var userHandle []byte
	err := tx.QueryRowContext(
		ctx,
		`SELECT user_handle FROM passkey_credentials WHERE account_id = ? AND credential_id = ?`,
		accountID,
		credentialID,
	).Scan(&userHandle)
	return userHandle, err
}

func upsertMembershipTx(ctx context.Context, tx *sql.Tx, membership Membership) error {
	_, err := tx.ExecContext(
		ctx,
		`INSERT INTO organization_memberships(id, organization_id, account_id, role, status, created_at)
		 VALUES (?, ?, ?, ?, ?, ?)
		 ON CONFLICT(organization_id, account_id) DO UPDATE SET role = excluded.role, status = excluded.status`,
		membership.ID,
		membership.OrganizationID,
		membership.AccountID,
		membership.Role,
		membership.Status,
		membership.CreatedAt,
	)
	if err != nil {
		return internalError("failed to upsert organization membership")
	}
	return nil
}

func invalidRequest(message string) error {
	return &APIError{Status: http.StatusBadRequest, Code: "invalid_request", Message: message}
}

func accessDenied(message string) error {
	return &APIError{Status: http.StatusForbidden, Code: "access_denied", Message: message}
}

func notFound(message string) error {
	return &APIError{Status: http.StatusNotFound, Code: "not_found", Message: message}
}

func conflict(code string, message string) error {
	return &APIError{Status: http.StatusConflict, Code: code, Message: message}
}

func internalError(message string) error {
	return &APIError{Status: http.StatusInternalServerError, Code: "internal_error", Message: message}
}

func stringValue(raw any) string {
	value, _ := raw.(string)
	return value
}

func nullableString(raw sql.NullString) *string {
	if !raw.Valid {
		return nil
	}
	value := raw.String
	return &value
}

func stringPtr(raw string) *string {
	value := raw
	return &value
}

func nonNilMap(raw map[string]any) map[string]any {
	if raw == nil {
		return map[string]any{}
	}
	return raw
}

func credentialSafeEmail(email string) string {
	parts := strings.SplitN(email, "@", 2)
	if len(parts) != 2 || parts[0] == "" {
		return email
	}
	return parts[0][:1] + "***@" + parts[1]
}

func isSQLiteConstraint(err error) bool {
	return strings.Contains(strings.ToLower(err.Error()), "constraint")
}

func isWorkspaceSlugConstraint(err error) bool {
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "workspaces.organization_id, workspaces.slug")
}

func isWorkspaceListenPortConstraint(err error) bool {
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "workspaces.host_id, workspaces.listen_port") ||
		strings.Contains(message, "idx_workspaces_host_listen_port_unique")
}

func isSQLiteBusyError(err error) bool {
	if err == nil {
		return false
	}
	lowered := strings.ToLower(err.Error())
	return strings.Contains(lowered, "database is locked") ||
		strings.Contains(lowered, "database table is locked") ||
		strings.Contains(lowered, "sqlite_busy") ||
		strings.Contains(lowered, "cannot start a transaction within a transaction")
}

func equalBytes(left []byte, right []byte) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func isExpired(ts string, now time.Time) bool {
	expiresAt, err := time.Parse(time.RFC3339Nano, ts)
	if err != nil {
		return true
	}
	return !expiresAt.After(now)
}

func decodeBase64Either(raw string) ([]byte, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err == nil {
		return decoded, nil
	}
	return base64.StdEncoding.DecodeString(raw)
}
