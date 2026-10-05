package auth

import (
	"agent-nexus-core/internal/resourceaccess"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/google/uuid"
)

var ErrInvalidRequest = errors.New("invalid_request")
var ErrInvalidToken = errors.New("invalid_token")
var ErrAuthRequired = errors.New("auth_required")
var ErrAgentRevoked = errors.New("agent_revoked")
var ErrKeyMismatch = errors.New("key_mismatch")
var ErrAgentNotFound = errors.New("agent_not_found")
var ErrLastActivePrincipal = errors.New("last_active_principal")
var ErrAccountDisabled = errors.New("account_disabled")
var ErrAccountStatusUnreachable = errors.New("account_status_unreachable")

const (
	defaultAccessTokenTTL  = 15 * time.Minute
	defaultRefreshTokenTTL = 30 * 24 * time.Hour
	defaultAssertionSkew   = 5 * time.Minute
)

type Option func(*Store)

type Agent struct {
	AgentID       string             `json:"agent_id"`
	Username      string             `json:"username"`
	ActorID       string             `json:"actor_id"`
	Revoked       bool               `json:"revoked"`
	CreatedAt     string             `json:"created_at"`
	UpdatedAt     string             `json:"updated_at"`
	PrincipalKind *string            `json:"principal_kind,omitempty"`
	AuthMethod    *string            `json:"auth_method,omitempty"`
	Registration  *AgentRegistration `json:"registration,omitempty"`
}

type TokenBundle struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token,omitempty"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int64  `json:"expires_in"`
}

type Principal struct {
	// Scoped tokens are existing access tokens, never a separate bearer scheme.
	SeriesAdapter string
	AccessTokenID string
	AgentID       string
	ActorID       string
	Username      string
	PrincipalKind string
	AuthMethod    string
	AuthAdmin     bool
}

type RevocationMode string

const (
	RevocationModeSelf  RevocationMode = "self"
	RevocationModeAdmin RevocationMode = "admin"
)

type RevokeAgentInput struct {
	Actor              Principal
	Mode               RevocationMode
	AllowHumanLockout  bool
	HumanLockoutReason string
}

type RevokeAgentResult struct {
	Principal  AuthPrincipalSummary `json:"principal"`
	Revocation struct {
		Mode              string `json:"mode"`
		AlreadyRevoked    bool   `json:"already_revoked"`
		AllowHumanLockout bool   `json:"allow_human_lockout"`
	} `json:"revocation"`
}

type AssertionInput struct {
	AgentID   string
	KeyID     string
	SignedAt  string
	Signature string
}

type Store struct {
	db                          *sql.DB
	accessTokenTTL              time.Duration
	refreshTokenTTL             time.Duration
	maxAssertionSkew            time.Duration
	bootstrapTokenHash          string
	allowDevRegisterLinkedActor bool
	accountStatusChecker        AccountStatusChecker
}

func NewStore(db *sql.DB, options ...Option) *Store {
	store := &Store{
		db:               db,
		accessTokenTTL:   defaultAccessTokenTTL,
		refreshTokenTTL:  defaultRefreshTokenTTL,
		maxAssertionSkew: defaultAssertionSkew,
	}
	for _, option := range options {
		option(store)
	}
	return store
}

func WithAccessTokenTTL(ttl time.Duration) Option {
	return func(store *Store) {
		if ttl > 0 {
			store.accessTokenTTL = ttl
		}
	}
}

func WithRefreshTokenTTL(ttl time.Duration) Option {
	return func(store *Store) {
		if ttl > 0 {
			store.refreshTokenTTL = ttl
		}
	}
}

func WithAssertionSkew(skew time.Duration) Option {
	return func(store *Store) {
		if skew > 0 {
			store.maxAssertionSkew = skew
		}
	}
}

func WithBootstrapToken(token string) Option {
	return func(store *Store) {
		token = strings.TrimSpace(token)
		if token == "" || token == BootstrapTokenPlaceholder {
			store.bootstrapTokenHash = ""
			return
		}
		store.bootstrapTokenHash = hashToken(token)
	}
}

// WithAllowDevRegisterLinkedActor permits linking pre-seeded actor rows in local dev
// passkey registration and host-derived agent grants.
func WithAllowDevRegisterLinkedActor(allow bool) Option {
	return func(store *Store) {
		store.allowDevRegisterLinkedActor = allow
	}
}

// WithAccountStatusChecker enables HTTP account status checks during
// refresh for hosted human principals. Pass nil for OSS/local mode.
func WithAccountStatusChecker(checker AccountStatusChecker) Option {
	return func(store *Store) {
		store.accountStatusChecker = checker
	}
}

func BuildAssertionMessage(agentID string, keyID string, signedAt string) string {
	return "anx-auth-token|" + strings.TrimSpace(agentID) + "|" + strings.TrimSpace(keyID) + "|" + strings.TrimSpace(signedAt)
}

func (s *Store) ensureExistingActorReadyForAgentLinkTx(ctx context.Context, tx Transaction, actorID string) error {
	actorID = strings.TrimSpace(actorID)
	if actorID == "" {
		return fmt.Errorf("%w: existing actor not found", ErrInvalidRequest)
	}
	var placeholder int
	err := tx.QueryRowContext(
		ctx,
		`SELECT 1 FROM actors WHERE id = ? LIMIT 1`,
		actorID,
	).Scan(&placeholder)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%w: existing actor not found", ErrInvalidRequest)
	}
	if err != nil {
		return fmt.Errorf("lookup existing actor: %w", err)
	}
	var occupyingAgent string
	err = tx.QueryRowContext(
		ctx,
		`SELECT id FROM agents WHERE actor_id = ? AND revoked_at IS NULL LIMIT 1`,
		actorID,
	).Scan(&occupyingAgent)
	if errors.Is(err, sql.ErrNoRows) {
		err = nil
	}
	if err != nil {
		return fmt.Errorf("check actor link: %w", err)
	}
	if occupyingAgent != "" {
		return fmt.Errorf("%w: actor already linked to an agent", ErrInvalidRequest)
	}
	return nil
}

func (s *Store) IssueTokenFromAssertion(ctx context.Context, input AssertionInput) (TokenBundle, error) {
	if s == nil || s.db == nil {
		return TokenBundle{}, fmt.Errorf("auth store database is not initialized")
	}

	agentID := strings.TrimSpace(input.AgentID)
	keyID := strings.TrimSpace(input.KeyID)
	signedAt := strings.TrimSpace(input.SignedAt)
	signature := strings.TrimSpace(input.Signature)

	if agentID == "" || keyID == "" || signedAt == "" || signature == "" {
		return TokenBundle{}, ErrKeyMismatch
	}

	signedTime, err := time.Parse(time.RFC3339, signedAt)
	if err != nil {
		return TokenBundle{}, ErrKeyMismatch
	}
	now := time.Now().UTC()
	if now.Sub(signedTime) > s.maxAssertionSkew || signedTime.Sub(now) > s.maxAssertionSkew {
		return TokenBundle{}, ErrKeyMismatch
	}

	tx, err := resourceaccess.NewDB(s.db).BeginTx(ctx, nil)
	if err != nil {
		return TokenBundle{}, fmt.Errorf("begin assertion token transaction: %w", err)
	}

	var (
		storedAgentID string
		revokedAt     sql.NullString
		algorithm     string
		publicKey     string
		keyRevokedAt  sql.NullString
	)
	err = tx.QueryRowContext(
		ctx,
		`SELECT a.id, a.revoked_at, k.algorithm, k.public_key, k.revoked_at
		 FROM agents a
		 JOIN agent_keys k ON k.agent_id = a.id
		 WHERE a.id = ? AND k.id = ?`,
		agentID,
		keyID,
	).Scan(&storedAgentID, &revokedAt, &algorithm, &publicKey, &keyRevokedAt)
	if err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		if errors.Is(err, sql.ErrNoRows) {
			return TokenBundle{}, ErrKeyMismatch
		}
		return TokenBundle{}, fmt.Errorf("load assertion key: %w", err)
	}

	if revokedAt.Valid {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return TokenBundle{}, ErrAgentRevoked
	}
	if keyRevokedAt.Valid {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return TokenBundle{}, ErrKeyMismatch
	}
	if strings.TrimSpace(algorithm) != "ed25519" {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return TokenBundle{}, ErrKeyMismatch
	}

	publicKeyBytes, err := decodeEd25519PublicKey(publicKey)
	if err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return TokenBundle{}, ErrKeyMismatch
	}
	signatureBytes, err := decodeBase64(signature)
	if err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return TokenBundle{}, ErrKeyMismatch
	}

	message := BuildAssertionMessage(agentID, keyID, signedAt)
	if !ed25519.Verify(publicKeyBytes, []byte(message), signatureBytes) {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return TokenBundle{}, ErrKeyMismatch
	}
	if err := s.recordAssertionUseTx(ctx, tx, message, signature, now); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		if errors.Is(err, ErrKeyMismatch) {
			return TokenBundle{}, ErrKeyMismatch
		}
		return TokenBundle{}, err
	}

	tokens, _, err := s.issueTokenBundleTx(ctx, tx, storedAgentID, now)
	if err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return TokenBundle{}, err
	}

	if err := tx.Commit(); err != nil {
		return TokenBundle{}, fmt.Errorf("commit assertion token transaction: %w", err)
	}

	return tokens, nil
}

func (s *Store) recordAssertionUseTx(ctx context.Context, tx Transaction, message string, signature string, now time.Time) error {
	if _, err := tx.ExecContext(
		ctx,
		`DELETE FROM auth_used_assertions WHERE used_at < ?`,
		now.Add(-s.maxAssertionSkew).Format(time.RFC3339Nano),
	); err != nil {
		return fmt.Errorf("cleanup used assertions: %w", err)
	}

	_, err := tx.ExecContext(
		ctx,
		`INSERT INTO auth_used_assertions(assertion_hash, used_at)
		 VALUES (?, ?)`,
		hashAssertionReplay(message, signature),
		now.Format(time.RFC3339Nano),
	)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return ErrKeyMismatch
		}
		return fmt.Errorf("record used assertion: %w", err)
	}

	return nil
}

func (s *Store) IssueTokenFromRefresh(ctx context.Context, refreshToken string) (TokenBundle, error) {
	if s == nil || s.db == nil {
		return TokenBundle{}, fmt.Errorf("auth store database is not initialized")
	}

	refreshToken = strings.TrimSpace(refreshToken)
	if refreshToken == "" {
		return TokenBundle{}, ErrInvalidToken
	}

	tx, err := resourceaccess.NewDB(s.db).BeginTx(ctx, nil)
	if err != nil {
		return TokenBundle{}, fmt.Errorf("begin refresh token transaction: %w", err)
	}

	var (
		sessionID        string
		agentID          string
		expiresAtRaw     string
		revokedAt        sql.NullString
		replacedBy       sql.NullString
		agentRevoked     sql.NullString
		authMethodRaw    sql.NullString
		principalKindRaw sql.NullString
		externalSubRaw   sql.NullString
	)
	err = tx.QueryRowContext(
		ctx,
		`SELECT r.id, r.agent_id, r.expires_at, r.revoked_at, r.replaced_by_session_id, a.revoked_at,
			json_extract(a.metadata_json, '$.auth_method'),
			json_extract(a.metadata_json, '$.principal_kind'),
			json_extract(a.metadata_json, '$.external_subject')
		 FROM auth_refresh_sessions r
		 JOIN agents a ON a.id = r.agent_id
		 WHERE r.token_hash = ?`,
		hashToken(refreshToken),
	).Scan(&sessionID, &agentID, &expiresAtRaw, &revokedAt, &replacedBy, &agentRevoked, &authMethodRaw, &principalKindRaw, &externalSubRaw)
	if err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		if errors.Is(err, sql.ErrNoRows) {
			return TokenBundle{}, ErrInvalidToken
		}
		return TokenBundle{}, fmt.Errorf("load refresh session: %w", err)
	}

	if revokedAt.Valid || replacedBy.Valid {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return TokenBundle{}, ErrInvalidToken
	}

	expiresAt, err := time.Parse(time.RFC3339Nano, expiresAtRaw)
	if err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return TokenBundle{}, fmt.Errorf("parse refresh token expiry: %w", err)
	}
	if time.Now().UTC().After(expiresAt) {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return TokenBundle{}, ErrInvalidToken
	}

	if agentRevoked.Valid {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return TokenBundle{}, ErrAgentRevoked
	}

	if s.accountStatusChecker != nil {
		authMethod := ""
		if authMethodRaw.Valid {
			authMethod = authMethodRaw.String
		}
		principalKind := ""
		if principalKindRaw.Valid {
			principalKind = principalKindRaw.String
		}
		externalSubject := ""
		if externalSubRaw.Valid {
			externalSubject = externalSubRaw.String
		}
		if shouldCheckHostedHumanAccountStatus(principalKind, authMethod) && strings.TrimSpace(externalSubject) != "" {
			if _, err := s.accountStatusChecker.CheckActive(ctx, externalSubject); err != nil {
				if rbErr := tx.Rollback(); rbErr != nil {
					log.Printf("tx rollback failed: %v", rbErr)
				}
				switch {
				case errors.Is(err, ErrAccountInactive):
					return TokenBundle{}, ErrAccountDisabled
				case errors.Is(err, ErrAccountStatusUnreachable):
					return TokenBundle{}, ErrAccountStatusUnreachable
				default:
					return TokenBundle{}, err
				}
			}
		}
	}

	now := time.Now().UTC()
	tokens, newSessionID, err := s.issueTokenBundleTx(ctx, tx, agentID, now)
	if err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return TokenBundle{}, err
	}

	_, err = tx.ExecContext(
		ctx,
		`UPDATE auth_refresh_sessions
		 SET revoked_at = ?, replaced_by_session_id = ?
		 WHERE id = ?`,
		now.Format(time.RFC3339Nano),
		newSessionID,
		sessionID,
	)
	if err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return TokenBundle{}, fmt.Errorf("rotate refresh session: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return TokenBundle{}, fmt.Errorf("commit refresh token transaction: %w", err)
	}

	return tokens, nil
}

func (s *Store) AuthenticateAccessToken(ctx context.Context, accessToken string) (Principal, error) {
	if s == nil || s.db == nil {
		return Principal{}, fmt.Errorf("auth store database is not initialized")
	}

	accessToken = strings.TrimSpace(accessToken)
	if accessToken == "" {
		return Principal{}, ErrInvalidToken
	}

	var (
		agentID       string
		username      string
		actorID       string
		principalKind string
		authMethod    string
		authAdmin     bool
		agentRevoked  sql.NullString
		expiresAtRaw  string
		tokenRevoked  sql.NullString
		seriesAdapter string
		accessTokenID string
	)
	err := resourceaccess.NewDB(s.db).QueryRowContext(
		ctx,
		fmt.Sprintf(`SELECT a.id, a.username, a.actor_id, %s, %s,
		        COALESCE(json_extract(a.metadata_json, '$.auth_admin'), 0),
		        a.revoked_at, t.expires_at, t.revoked_at, COALESCE(t.series_adapter,''), t.id
		 FROM auth_access_tokens t
		 JOIN agents a ON a.id = t.agent_id
		 WHERE t.token_hash = ?`, principalKindExpr("a"), authMethodExpr("a")),
		hashToken(accessToken),
	).Scan(&agentID, &username, &actorID, &principalKind, &authMethod, &authAdmin, &agentRevoked, &expiresAtRaw, &tokenRevoked, &seriesAdapter, &accessTokenID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Principal{}, ErrInvalidToken
		}
		return Principal{}, fmt.Errorf("query access token: %w", err)
	}

	if tokenRevoked.Valid {
		return Principal{}, ErrInvalidToken
	}

	expiresAt, err := time.Parse(time.RFC3339Nano, expiresAtRaw)
	if err != nil {
		return Principal{}, fmt.Errorf("parse access token expiry: %w", err)
	}
	if time.Now().UTC().After(expiresAt) {
		return Principal{}, ErrInvalidToken
	}

	if agentRevoked.Valid {
		return Principal{}, ErrAgentRevoked
	}

	return Principal{
		SeriesAdapter: seriesAdapter,
		AccessTokenID: accessTokenID,
		AgentID:       agentID,
		ActorID:       actorID,
		Username:      username,
		PrincipalKind: strings.TrimSpace(principalKind),
		AuthMethod:    strings.TrimSpace(authMethod),
		AuthAdmin:     authAdmin && seriesAdapter == "",
	}, nil
}

func (s *Store) GetAgent(ctx context.Context, agentID string) (Agent, error) {
	if s == nil || s.db == nil {
		return Agent{}, fmt.Errorf("auth store database is not initialized")
	}

	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		return Agent{}, ErrAgentNotFound
	}

	var (
		agent              Agent
		principalKindValue string
		authMethodValue    string
		revokedRaw         sql.NullString
		metadataJSON       string
	)
	err := resourceaccess.NewDB(s.db).QueryRowContext(
		ctx,
		fmt.Sprintf(`SELECT id, username, actor_id, %s, %s, created_at, updated_at, revoked_at, metadata_json
		 FROM agents
		 WHERE id = ?`, principalKindExpr("agents"), authMethodExpr("agents")),
		agentID,
	).Scan(&agent.AgentID, &agent.Username, &agent.ActorID, &principalKindValue, &authMethodValue, &agent.CreatedAt, &agent.UpdatedAt, &revokedRaw, &metadataJSON)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Agent{}, ErrAgentNotFound
		}
		return Agent{}, fmt.Errorf("query agent: %w", err)
	}

	agent.Revoked = revokedRaw.Valid
	agent.PrincipalKind = ptrString(strings.TrimSpace(principalKindValue))
	agent.AuthMethod = ptrString(strings.TrimSpace(authMethodValue))
	registration, err := registrationFromMetadataJSON(metadataJSON)
	if err != nil {
		return Agent{}, fmt.Errorf("query agent registration: %w", err)
	}
	agent.Registration = registration

	return agent, nil
}

func (s *Store) GetPrincipalSummary(ctx context.Context, agentID string) (AuthPrincipalSummary, error) {
	if s == nil || s.db == nil {
		return AuthPrincipalSummary{}, fmt.Errorf("auth store database is not initialized")
	}

	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		return AuthPrincipalSummary{}, ErrAgentNotFound
	}

	return s.getPrincipalSummaryQueryRow(ctx, resourceaccess.NewDB(s.db).QueryRowContext, agentID)
}

func (s *Store) RevokeAgent(ctx context.Context, agentID string, input RevokeAgentInput) (RevokeAgentResult, error) {
	if s == nil || s.db == nil {
		return RevokeAgentResult{}, fmt.Errorf("auth store database is not initialized")
	}

	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		return RevokeAgentResult{}, ErrAgentNotFound
	}
	input.Actor.AgentID = strings.TrimSpace(input.Actor.AgentID)
	input.Actor.ActorID = strings.TrimSpace(input.Actor.ActorID)
	if input.Actor.AgentID == "" || input.Actor.ActorID == "" {
		return RevokeAgentResult{}, fmt.Errorf("%w: authenticated principal is required", ErrAuthRequired)
	}
	if strings.TrimSpace(string(input.Mode)) == "" {
		input.Mode = RevocationModeAdmin
	}
	input.HumanLockoutReason = strings.TrimSpace(input.HumanLockoutReason)
	if input.AllowHumanLockout && input.HumanLockoutReason == "" {
		return RevokeAgentResult{}, fmt.Errorf("%w: human_lockout_reason is required when allow_human_lockout=true", ErrInvalidRequest)
	}
	if !input.AllowHumanLockout && input.HumanLockoutReason != "" {
		return RevokeAgentResult{}, fmt.Errorf("%w: human_lockout_reason requires allow_human_lockout=true", ErrInvalidRequest)
	}

	now := time.Now().UTC()
	nowText := now.Format(time.RFC3339Nano)
	tx, err := resourceaccess.NewDB(s.db).BeginTx(ctx, nil)
	if err != nil {
		return RevokeAgentResult{}, fmt.Errorf("begin revoke agent transaction: %w", err)
	}

	defer tx.Rollback()
	if input.Mode == RevocationModeAdmin || input.AllowHumanLockout {
		if err := requireAdministrationTx(ctx, tx, input.Actor, true); err != nil {
			return RevokeAgentResult{}, err
		}
	}

	if input.Mode == RevocationModeSelf && input.Actor.AgentID != agentID {
		return RevokeAgentResult{}, ErrAuthRequired
	}

	var (
		subjectActorID  string
		existingRevoked sql.NullString
		principal       AuthPrincipalSummary
		subjectKind     string
	)
	err = tx.QueryRowContext(
		ctx,
		`SELECT actor_id, revoked_at, `+principalKindExpr("a")+`
		 FROM agents a
		 WHERE id = ?`,
		agentID,
	).Scan(&subjectActorID, &existingRevoked, &subjectKind)
	if err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		if errors.Is(err, sql.ErrNoRows) {
			return RevokeAgentResult{}, ErrAgentNotFound
		}
		return RevokeAgentResult{}, fmt.Errorf("query agent revoke state: %w", err)
	}
	if subjectKind == string(PrincipalKindHuman) {
		if err := requireAdministrationTx(ctx, tx, input.Actor, true); err != nil {
			return RevokeAgentResult{}, err
		}
	}
	if existingRevoked.Valid {
		principal, loadErr := s.getPrincipalSummaryTx(ctx, tx, agentID)
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		if loadErr != nil {
			return RevokeAgentResult{}, loadErr
		}
		result := RevokeAgentResult{Principal: principal}
		result.Revocation.Mode = string(input.Mode)
		result.Revocation.AlreadyRevoked = true
		return result, nil
	}

	principal, err = s.getPrincipalSummaryTx(ctx, tx, agentID)
	if err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return RevokeAgentResult{}, err
	}
	activeHumanCount, err := s.countActiveHumanPrincipalsTx(ctx, tx)
	if err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return RevokeAgentResult{}, err
	}
	if principal.PrincipalKind == "human" && activeHumanCount == 1 && !input.AllowHumanLockout {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return RevokeAgentResult{}, ErrLastActivePrincipal
	}
	usedHumanLockout := principal.PrincipalKind == "human" && activeHumanCount == 1 && input.AllowHumanLockout
	if input.AllowHumanLockout && !usedHumanLockout {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return RevokeAgentResult{}, fmt.Errorf("%w: allow_human_lockout is only permitted when revoking the last active human principal", ErrInvalidRequest)
	}

	_, err = tx.ExecContext(
		ctx,
		`UPDATE agents
		 SET revoked_at = ?, updated_at = ?
		 WHERE id = ?`,
		nowText,
		nowText,
		agentID,
	)
	if err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return RevokeAgentResult{}, fmt.Errorf("revoke agent: %w", err)
	}

	_, err = tx.ExecContext(
		ctx,
		`UPDATE agent_keys
		 SET revoked_at = COALESCE(revoked_at, ?)
		 WHERE agent_id = ?`,
		nowText,
		agentID,
	)
	if err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return RevokeAgentResult{}, fmt.Errorf("revoke agent keys: %w", err)
	}

	eventType := AuthAuditEventPrincipalRevoked
	if input.Mode == RevocationModeSelf {
		eventType = AuthAuditEventPrincipalSelfRevoked
	}
	if usedHumanLockout {
		eventType = AuthAuditEventPrincipalHumanLockoutRevoked
	}
	metadata := map[string]any{
		"actor_username":      strings.TrimSpace(input.Actor.Username),
		"revocation_mode":     string(input.Mode),
		"allow_human_lockout": usedHumanLockout,
		"active_human_count":  activeHumanCount,
	}
	if usedHumanLockout {
		metadata["human_lockout_reason"] = input.HumanLockoutReason
	}
	if err := s.recordAuthAuditEventTx(ctx, tx, AuthAuditEventInput{
		EventType:      eventType,
		OccurredAt:     now,
		ActorAgentID:   input.Actor.AgentID,
		ActorActorID:   input.Actor.ActorID,
		SubjectAgentID: agentID,
		SubjectActorID: subjectActorID,
		Metadata:       metadata,
	}); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return RevokeAgentResult{}, err
	}

	principal, err = s.getPrincipalSummaryTx(ctx, tx, agentID)
	if err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return RevokeAgentResult{}, err
	}

	if err := tx.Commit(); err != nil {
		return RevokeAgentResult{}, fmt.Errorf("commit revoke agent transaction: %w", err)
	}

	result := RevokeAgentResult{Principal: principal}
	result.Revocation.Mode = string(input.Mode)
	result.Revocation.AllowHumanLockout = usedHumanLockout
	return result, nil
}

func (s *Store) countActiveHumanPrincipalsTx(ctx context.Context, tx Transaction) (int, error) {
	// Global last-human invariant must count even profiles hidden by resource scope.
	ctx = resourceaccess.WithoutPolicy(ctx)
	var count int
	if err := tx.QueryRowContext(
		ctx,
		fmt.Sprintf(`SELECT COUNT(1)
		 FROM agents a
		 WHERE a.revoked_at IS NULL
		   AND %s = 'human'`, principalKindExpr("a")),
	).Scan(&count); err != nil {
		return 0, fmt.Errorf("count active human principals: %w", err)
	}
	return count, nil
}

func (s *Store) CountActiveHumanPrincipals(ctx context.Context) (int, error) {
	if s == nil || s.db == nil {
		return 0, fmt.Errorf("auth store database is not initialized")
	}
	var count int
	if err := resourceaccess.NewDB(s.db).QueryRowContext(
		ctx,
		fmt.Sprintf(`SELECT COUNT(1)
		 FROM agents a
		 WHERE a.revoked_at IS NULL
		   AND %s = 'human'`, principalKindExpr("a")),
	).Scan(&count); err != nil {
		return 0, fmt.Errorf("count active human principals: %w", err)
	}
	return count, nil
}

func (s *Store) getPrincipalSummaryTx(ctx context.Context, tx Transaction, agentID string) (AuthPrincipalSummary, error) {
	return s.getPrincipalSummaryQueryRow(ctx, tx.QueryRowContext, agentID)
}

func (s *Store) getPrincipalSummaryQueryRow(ctx context.Context, queryRow func(context.Context, string, ...any) *sql.Row, agentID string) (AuthPrincipalSummary, error) {
	var (
		item         AuthPrincipalSummary
		revokedRaw   sql.NullString
		metadataJSON string
	)
	err := queryRow(
		ctx,
		fmt.Sprintf(`SELECT
			a.id,
			a.actor_id,
			a.username,
			%s,
			%s,
			a.created_at,
			%s,
			a.updated_at,
			a.revoked_at,
			a.metadata_json
		 FROM agents a
		 WHERE a.id = ?`, principalKindExpr("a"), authMethodExpr("a"), principalLastSeenExpr("a")),
		agentID,
	).Scan(
		&item.AgentID,
		&item.ActorID,
		&item.Username,
		&item.PrincipalKind,
		&item.AuthMethod,
		&item.CreatedAt,
		&item.LastSeenAt,
		&item.UpdatedAt,
		&revokedRaw,
		&metadataJSON,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return AuthPrincipalSummary{}, ErrAgentNotFound
		}
		return AuthPrincipalSummary{}, fmt.Errorf("query auth principal summary: %w", err)
	}
	item.Revoked = revokedRaw.Valid
	if revokedRaw.Valid {
		item.RevokedAt = &revokedRaw.String
	}
	registration, err := registrationFromMetadataJSON(metadataJSON)
	if err != nil {
		return AuthPrincipalSummary{}, fmt.Errorf("query auth principal registration: %w", err)
	}
	item.Registration = registration
	return item, nil
}

func (a Agent) PrincipalKindOrDefault() string {
	if a.PrincipalKind != nil && strings.TrimSpace(*a.PrincipalKind) != "" {
		return strings.TrimSpace(*a.PrincipalKind)
	}
	return string(PrincipalKindAgent)
}

func (a Agent) AuthMethodOrDefault() string {
	if a.AuthMethod != nil && strings.TrimSpace(*a.AuthMethod) != "" {
		return strings.TrimSpace(*a.AuthMethod)
	}
	return AuthMethodPublicKey
}

func (s *Store) issueTokenBundleTx(ctx context.Context, tx Transaction, agentID string, now time.Time) (TokenBundle, string, error) {
	refreshToken, err := generateOpaqueToken(32)
	if err != nil {
		return TokenBundle{}, "", fmt.Errorf("generate refresh token: %w", err)
	}
	accessToken, err := generateOpaqueToken(32)
	if err != nil {
		return TokenBundle{}, "", fmt.Errorf("generate access token: %w", err)
	}

	refreshSessionID := "refresh_" + uuid.NewString()
	accessTokenID := "access_" + uuid.NewString()

	nowText := now.Format(time.RFC3339Nano)
	refreshExpiresText := now.Add(s.refreshTokenTTL).Format(time.RFC3339Nano)
	accessExpiresText := now.Add(s.accessTokenTTL).Format(time.RFC3339Nano)

	_, err = tx.ExecContext(
		ctx,
		`INSERT INTO auth_refresh_sessions(id, agent_id, token_hash, created_at, expires_at, revoked_at, replaced_by_session_id)
		 VALUES (?, ?, ?, ?, ?, NULL, NULL)`,
		refreshSessionID,
		agentID,
		hashToken(refreshToken),
		nowText,
		refreshExpiresText,
	)
	if err != nil {
		return TokenBundle{}, "", fmt.Errorf("insert refresh session: %w", err)
	}

	_, err = tx.ExecContext(
		ctx,
		`INSERT INTO auth_access_tokens(id, agent_id, token_hash, created_at, expires_at, revoked_at)
		 VALUES (?, ?, ?, ?, ?, NULL)`,
		accessTokenID,
		agentID,
		hashToken(accessToken),
		nowText,
		accessExpiresText,
	)
	if err != nil {
		return TokenBundle{}, "", fmt.Errorf("insert access token: %w", err)
	}

	return TokenBundle{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		TokenType:    "Bearer",
		ExpiresIn:    int64(s.accessTokenTTL.Seconds()),
	}, refreshSessionID, nil
}

func normalizeUsername(raw string) (string, error) {
	username := strings.ToLower(strings.TrimSpace(raw))
	if username == "" {
		return "", fmt.Errorf("username is required")
	}
	if len(username) < 3 || len(username) > 64 {
		return "", fmt.Errorf("username must be between 3 and 64 characters")
	}
	for _, ch := range username {
		if (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') || ch == '_' || ch == '-' || ch == '.' {
			continue
		}
		return "", fmt.Errorf("username must contain only lowercase letters, numbers, underscore, dash, or dot")
	}
	return username, nil
}

func decodeEd25519PublicKey(raw string) (ed25519.PublicKey, error) {
	decoded, err := decodeBase64(raw)
	if err != nil {
		return nil, err
	}
	if len(decoded) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("unexpected key length %d", len(decoded))
	}
	pub := make([]byte, len(decoded))
	copy(pub, decoded)
	return ed25519.PublicKey(pub), nil
}

func decodeBase64(raw string) ([]byte, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("empty value")
	}
	for _, encoding := range []*base64.Encoding{
		base64.StdEncoding,
		base64.RawStdEncoding,
		base64.URLEncoding,
		base64.RawURLEncoding,
	} {
		decoded, err := encoding.DecodeString(raw)
		if err == nil {
			return decoded, nil
		}
	}
	return nil, fmt.Errorf("invalid base64 payload")
}

func generateOpaqueToken(length int) (string, error) {
	buf := make([]byte, length)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func hashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func hashAssertionReplay(message string, signature string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(message) + "|" + strings.TrimSpace(signature)))
	return hex.EncodeToString(sum[:])
}
