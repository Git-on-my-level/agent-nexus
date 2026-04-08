package auth

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

var ErrUsernameTaken = errors.New("username_taken")
var ErrInvalidRequest = errors.New("invalid_request")
var ErrInvalidToken = errors.New("invalid_token")
var ErrAuthRequired = errors.New("auth_required")
var ErrAgentRevoked = errors.New("agent_revoked")
var ErrKeyMismatch = errors.New("key_mismatch")
var ErrAgentNotFound = errors.New("agent_not_found")
var ErrLastActivePrincipal = errors.New("last_active_principal")

const (
	bootstrapTokenPlaceholder = "REPLACE_WITH_SECURE_BOOTSTRAP_TOKEN"
	defaultAccessTokenTTL     = 15 * time.Minute
	defaultRefreshTokenTTL    = 30 * 24 * time.Hour
	defaultAssertionSkew      = 5 * time.Minute
	registerAgentMaxRetries   = 8
	registerAgentRetryBase    = 15 * time.Millisecond
	registerAgentRetryMax     = 250 * time.Millisecond
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

type AgentKey struct {
	KeyID     string  `json:"key_id"`
	AgentID   string  `json:"agent_id"`
	Algorithm string  `json:"algorithm"`
	PublicKey string  `json:"public_key"`
	CreatedAt string  `json:"created_at"`
	RevokedAt *string `json:"revoked_at,omitempty"`
}

type TokenBundle struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int64  `json:"expires_in"`
}

type Principal struct {
	AgentID       string
	ActorID       string
	Username      string
	PrincipalKind string
	AuthMethod    string
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

type RegisterAgentInput struct {
	Username        string
	PublicKey       string
	ExistingActorID string
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
		if token == "" || token == bootstrapTokenPlaceholder {
			store.bootstrapTokenHash = ""
			return
		}
		store.bootstrapTokenHash = hashToken(token)
	}
}

// WithAllowDevRegisterLinkedActor allows POST /auth/agents/register to accept
// existing_actor_id linking a new agent row to a pre-seeded actors row (local/dev only).
func WithAllowDevRegisterLinkedActor(allow bool) Option {
	return func(store *Store) {
		store.allowDevRegisterLinkedActor = allow
	}
}

func BuildAssertionMessage(agentID string, keyID string, signedAt string) string {
	return "oar-auth-token|" + strings.TrimSpace(agentID) + "|" + strings.TrimSpace(keyID) + "|" + strings.TrimSpace(signedAt)
}

func (s *Store) RegisterAgent(ctx context.Context, input RegisterAgentInput, claim OnboardingClaim) (Agent, AgentKey, TokenBundle, error) {
	if s == nil || s.db == nil {
		return Agent{}, AgentKey{}, TokenBundle{}, fmt.Errorf("auth store database is not initialized")
	}

	username, err := normalizeUsername(input.Username)
	if err != nil {
		return Agent{}, AgentKey{}, TokenBundle{}, fmt.Errorf("%w: %v", ErrInvalidRequest, err)
	}

	if _, err := decodeEd25519PublicKey(input.PublicKey); err != nil {
		return Agent{}, AgentKey{}, TokenBundle{}, fmt.Errorf("%w: public_key must be a base64-encoded ed25519 public key: %v", ErrInvalidRequest, err)
	}

	existingActorID := strings.TrimSpace(input.ExistingActorID)
	if existingActorID != "" && !s.allowDevRegisterLinkedActor {
		return Agent{}, AgentKey{}, TokenBundle{}, fmt.Errorf("%w: existing_actor_id is not enabled for this core instance", ErrInvalidRequest)
	}

	publicKey := strings.TrimSpace(input.PublicKey)
	var lastErr error
	for attempt := 0; attempt < registerAgentMaxRetries; attempt++ {
		agent, key, tokens, err := s.registerAgentOnce(ctx, username, publicKey, claim, existingActorID)
		if err == nil {
			return agent, key, tokens, nil
		}
		if errors.Is(err, ErrUsernameTaken) || !isSQLiteBusyError(err) {
			return Agent{}, AgentKey{}, TokenBundle{}, err
		}
		lastErr = err
		if attempt == registerAgentMaxRetries-1 {
			break
		}
		if err := waitForRegisterRetry(ctx, attempt); err != nil {
			return Agent{}, AgentKey{}, TokenBundle{}, err
		}
	}
	return Agent{}, AgentKey{}, TokenBundle{}, lastErr
}

func (s *Store) registerAgentOnce(ctx context.Context, username string, publicKey string, claim OnboardingClaim, existingActorID string) (Agent, AgentKey, TokenBundle, error) {
	now := time.Now().UTC()
	nowText := now.Format(time.RFC3339Nano)
	agentID := "agent_" + uuid.NewString()
	actorID := agentID
	if strings.TrimSpace(existingActorID) != "" {
		actorID = strings.TrimSpace(existingActorID)
	}
	keyID := "key_" + uuid.NewString()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Agent{}, AgentKey{}, TokenBundle{}, fmt.Errorf("begin register agent transaction: %w", err)
	}

	if err := s.consumeOnboardingClaimTx(ctx, tx, claim, agentID, actorID, now); err != nil {
		_ = tx.Rollback()
		return Agent{}, AgentKey{}, TokenBundle{}, err
	}

	if strings.TrimSpace(existingActorID) != "" {
		var placeholder int
		err = tx.QueryRowContext(
			ctx,
			`SELECT 1 FROM actors WHERE id = ? LIMIT 1`,
			actorID,
		).Scan(&placeholder)
		if errors.Is(err, sql.ErrNoRows) {
			_ = tx.Rollback()
			return Agent{}, AgentKey{}, TokenBundle{}, fmt.Errorf("%w: existing actor not found", ErrInvalidRequest)
		}
		if err != nil {
			_ = tx.Rollback()
			return Agent{}, AgentKey{}, TokenBundle{}, fmt.Errorf("lookup existing actor: %w", err)
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
			_ = tx.Rollback()
			return Agent{}, AgentKey{}, TokenBundle{}, fmt.Errorf("check actor link: %w", err)
		}
		if occupyingAgent != "" {
			_ = tx.Rollback()
			return Agent{}, AgentKey{}, TokenBundle{}, fmt.Errorf("%w: actor already linked to an agent", ErrInvalidRequest)
		}
	}

	agentMetadataJSON, err := principalMetadataJSON(PrincipalKindAgent, AuthMethodPublicKey, nil)
	if err != nil {
		_ = tx.Rollback()
		return Agent{}, AgentKey{}, TokenBundle{}, fmt.Errorf("encode agent metadata: %w", err)
	}
	_, err = tx.ExecContext(
		ctx,
		`INSERT INTO agents(id, username, actor_id, created_at, updated_at, revoked_at, metadata_json)
		 VALUES (?, ?, ?, ?, ?, NULL, ?)`,
		agentID,
		username,
		actorID,
		nowText,
		nowText,
		agentMetadataJSON,
	)
	if err != nil {
		_ = tx.Rollback()
		if strings.Contains(err.Error(), "UNIQUE constraint failed: agents.username") {
			return Agent{}, AgentKey{}, TokenBundle{}, ErrUsernameTaken
		}
		return Agent{}, AgentKey{}, TokenBundle{}, fmt.Errorf("insert agent: %w", err)
	}

	if strings.TrimSpace(existingActorID) == "" {
		actorMetadataValue, err := actorMetadataJSON(PrincipalKindAgent, AuthMethodPublicKey, nil)
		if err != nil {
			_ = tx.Rollback()
			return Agent{}, AgentKey{}, TokenBundle{}, fmt.Errorf("encode agent actor metadata: %w", err)
		}
		_, err = tx.ExecContext(
			ctx,
			`INSERT INTO actors(id, display_name, tags_json, created_at, metadata_json)
			 VALUES (?, ?, ?, ?, ?)`,
			actorID,
			username,
			`["agent"]`,
			nowText,
			actorMetadataValue,
		)
		if err != nil {
			_ = tx.Rollback()
			return Agent{}, AgentKey{}, TokenBundle{}, fmt.Errorf("insert mapped actor: %w", err)
		}
	}

	_, err = tx.ExecContext(
		ctx,
		`INSERT INTO agent_keys(id, agent_id, public_key, algorithm, created_at, revoked_at)
		 VALUES (?, ?, ?, 'ed25519', ?, NULL)`,
		keyID,
		agentID,
		publicKey,
		nowText,
	)
	if err != nil {
		_ = tx.Rollback()
		return Agent{}, AgentKey{}, TokenBundle{}, fmt.Errorf("insert agent key: %w", err)
	}

	if err := s.recordAuthAuditEventTx(ctx, tx, AuthAuditEventInput{
		EventType:      AuthAuditEventPrincipalRegistered,
		OccurredAt:     now.Add(time.Nanosecond),
		ActorAgentID:   agentID,
		ActorActorID:   actorID,
		SubjectAgentID: agentID,
		SubjectActorID: actorID,
		InviteID:       claim.InviteID,
		Metadata: map[string]any{
			"username":        username,
			"principal_kind":  "agent",
			"auth_method":     AuthMethodPublicKey,
			"onboarding_mode": string(claim.Mode),
		},
	}); err != nil {
		_ = tx.Rollback()
		return Agent{}, AgentKey{}, TokenBundle{}, err
	}

	tokens, _, err := s.issueTokenBundleTx(ctx, tx, agentID, now)
	if err != nil {
		_ = tx.Rollback()
		return Agent{}, AgentKey{}, TokenBundle{}, err
	}

	if err := tx.Commit(); err != nil {
		_ = tx.Rollback()
		return Agent{}, AgentKey{}, TokenBundle{}, fmt.Errorf("commit register agent transaction: %w", err)
	}

	return Agent{
			AgentID:       agentID,
			Username:      username,
			ActorID:       actorID,
			Revoked:       false,
			CreatedAt:     nowText,
			UpdatedAt:     nowText,
			PrincipalKind: ptrString("agent"),
			AuthMethod:    ptrString(AuthMethodPublicKey),
		}, AgentKey{
			KeyID:     keyID,
			AgentID:   agentID,
			Algorithm: "ed25519",
			PublicKey: publicKey,
			CreatedAt: nowText,
		}, tokens, nil
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

	tx, err := s.db.BeginTx(ctx, nil)
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
		_ = tx.Rollback()
		if errors.Is(err, sql.ErrNoRows) {
			return TokenBundle{}, ErrKeyMismatch
		}
		return TokenBundle{}, fmt.Errorf("load assertion key: %w", err)
	}

	if revokedAt.Valid {
		_ = tx.Rollback()
		return TokenBundle{}, ErrAgentRevoked
	}
	if keyRevokedAt.Valid {
		_ = tx.Rollback()
		return TokenBundle{}, ErrKeyMismatch
	}
	if strings.TrimSpace(algorithm) != "ed25519" {
		_ = tx.Rollback()
		return TokenBundle{}, ErrKeyMismatch
	}

	publicKeyBytes, err := decodeEd25519PublicKey(publicKey)
	if err != nil {
		_ = tx.Rollback()
		return TokenBundle{}, ErrKeyMismatch
	}
	signatureBytes, err := decodeBase64(signature)
	if err != nil {
		_ = tx.Rollback()
		return TokenBundle{}, ErrKeyMismatch
	}

	message := BuildAssertionMessage(agentID, keyID, signedAt)
	if !ed25519.Verify(publicKeyBytes, []byte(message), signatureBytes) {
		_ = tx.Rollback()
		return TokenBundle{}, ErrKeyMismatch
	}
	if err := s.recordAssertionUseTx(ctx, tx, message, signature, now); err != nil {
		_ = tx.Rollback()
		if errors.Is(err, ErrKeyMismatch) {
			return TokenBundle{}, ErrKeyMismatch
		}
		return TokenBundle{}, err
	}

	tokens, _, err := s.issueTokenBundleTx(ctx, tx, storedAgentID, now)
	if err != nil {
		_ = tx.Rollback()
		return TokenBundle{}, err
	}

	if err := tx.Commit(); err != nil {
		return TokenBundle{}, fmt.Errorf("commit assertion token transaction: %w", err)
	}

	return tokens, nil
}

func (s *Store) recordAssertionUseTx(ctx context.Context, tx *sql.Tx, message string, signature string, now time.Time) error {
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

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return TokenBundle{}, fmt.Errorf("begin refresh token transaction: %w", err)
	}

	var (
		sessionID    string
		agentID      string
		expiresAtRaw string
		revokedAt    sql.NullString
		replacedBy   sql.NullString
		agentRevoked sql.NullString
	)
	err = tx.QueryRowContext(
		ctx,
		`SELECT r.id, r.agent_id, r.expires_at, r.revoked_at, r.replaced_by_session_id, a.revoked_at
		 FROM auth_refresh_sessions r
		 JOIN agents a ON a.id = r.agent_id
		 WHERE r.token_hash = ?`,
		hashToken(refreshToken),
	).Scan(&sessionID, &agentID, &expiresAtRaw, &revokedAt, &replacedBy, &agentRevoked)
	if err != nil {
		_ = tx.Rollback()
		if errors.Is(err, sql.ErrNoRows) {
			return TokenBundle{}, ErrInvalidToken
		}
		return TokenBundle{}, fmt.Errorf("load refresh session: %w", err)
	}

	if revokedAt.Valid || replacedBy.Valid {
		_ = tx.Rollback()
		return TokenBundle{}, ErrInvalidToken
	}

	expiresAt, err := time.Parse(time.RFC3339Nano, expiresAtRaw)
	if err != nil {
		_ = tx.Rollback()
		return TokenBundle{}, fmt.Errorf("parse refresh token expiry: %w", err)
	}
	if time.Now().UTC().After(expiresAt) {
		_ = tx.Rollback()
		return TokenBundle{}, ErrInvalidToken
	}

	if agentRevoked.Valid {
		_ = tx.Rollback()
		return TokenBundle{}, ErrAgentRevoked
	}

	now := time.Now().UTC()
	tokens, newSessionID, err := s.issueTokenBundleTx(ctx, tx, agentID, now)
	if err != nil {
		_ = tx.Rollback()
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
		_ = tx.Rollback()
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
		agentRevoked  sql.NullString
		expiresAtRaw  string
		tokenRevoked  sql.NullString
	)
	err := s.db.QueryRowContext(
		ctx,
		fmt.Sprintf(`SELECT a.id, a.username, a.actor_id, %s, %s, a.revoked_at, t.expires_at, t.revoked_at
		 FROM auth_access_tokens t
		 JOIN agents a ON a.id = t.agent_id
		 WHERE t.token_hash = ?`, principalKindExpr("a"), authMethodExpr("a")),
		hashToken(accessToken),
	).Scan(&agentID, &username, &actorID, &principalKind, &authMethod, &agentRevoked, &expiresAtRaw, &tokenRevoked)
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
		AgentID:       agentID,
		ActorID:       actorID,
		Username:      username,
		PrincipalKind: strings.TrimSpace(principalKind),
		AuthMethod:    strings.TrimSpace(authMethod),
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
	err := s.db.QueryRowContext(
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

	return s.getPrincipalSummaryQueryRow(ctx, s.db.QueryRowContext, agentID)
}

func (s *Store) ListKeys(ctx context.Context, agentID string) ([]AgentKey, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("auth store database is not initialized")
	}

	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		return nil, ErrAgentNotFound
	}

	rows, err := s.db.QueryContext(
		ctx,
		`SELECT id, agent_id, algorithm, public_key, created_at, revoked_at
		 FROM agent_keys
		 WHERE agent_id = ?
		 ORDER BY created_at DESC, id DESC`,
		agentID,
	)
	if err != nil {
		return nil, fmt.Errorf("query agent keys: %w", err)
	}
	defer rows.Close()

	keys := make([]AgentKey, 0)
	for rows.Next() {
		var key AgentKey
		var revokedRaw sql.NullString
		if err := rows.Scan(&key.KeyID, &key.AgentID, &key.Algorithm, &key.PublicKey, &key.CreatedAt, &revokedRaw); err != nil {
			return nil, fmt.Errorf("scan agent key row: %w", err)
		}
		if revokedRaw.Valid {
			revoked := revokedRaw.String
			key.RevokedAt = &revoked
		}
		keys = append(keys, key)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate agent keys: %w", err)
	}

	return keys, nil
}

func (s *Store) UpdateUsername(ctx context.Context, agentID string, username string) (Agent, error) {
	if s == nil || s.db == nil {
		return Agent{}, fmt.Errorf("auth store database is not initialized")
	}

	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		return Agent{}, ErrAgentNotFound
	}

	normalized, err := normalizeUsername(username)
	if err != nil {
		return Agent{}, fmt.Errorf("%w: %v", ErrInvalidRequest, err)
	}

	nowText := time.Now().UTC().Format(time.RFC3339Nano)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Agent{}, fmt.Errorf("begin update username transaction: %w", err)
	}

	var authMethod string
	if err := tx.QueryRowContext(ctx, fmt.Sprintf(`SELECT %s FROM agents a WHERE a.id = ?`, authMethodExpr("a")), agentID).Scan(&authMethod); err != nil {
		_ = tx.Rollback()
		if errors.Is(err, sql.ErrNoRows) {
			return Agent{}, ErrAgentNotFound
		}
		return Agent{}, fmt.Errorf("query agent auth method before update username: %w", err)
	}
	if strings.TrimSpace(authMethod) == AuthMethodControlPlane {
		_ = tx.Rollback()
		return Agent{}, fmt.Errorf("%w: control-plane-backed principals cannot be renamed locally", ErrInvalidRequest)
	}

	result, err := tx.ExecContext(
		ctx,
		`UPDATE agents
		 SET username = ?, updated_at = ?
		 WHERE id = ?`,
		normalized,
		nowText,
		agentID,
	)
	if err != nil {
		_ = tx.Rollback()
		if strings.Contains(err.Error(), "UNIQUE constraint failed: agents.username") {
			return Agent{}, ErrUsernameTaken
		}
		return Agent{}, fmt.Errorf("update agent username: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		_ = tx.Rollback()
		return Agent{}, fmt.Errorf("read update username rows affected: %w", err)
	}
	if rowsAffected == 0 {
		_ = tx.Rollback()
		return Agent{}, ErrAgentNotFound
	}

	_, err = tx.ExecContext(
		ctx,
		`UPDATE actors
		 SET display_name = ?
		 WHERE id = (SELECT actor_id FROM agents WHERE id = ?)`,
		normalized,
		agentID,
	)
	if err != nil {
		_ = tx.Rollback()
		return Agent{}, fmt.Errorf("update mapped actor display name: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return Agent{}, fmt.Errorf("commit update username transaction: %w", err)
	}

	return s.GetAgent(ctx, agentID)
}

func (s *Store) UpdateRegistration(ctx context.Context, agentID string, registration AgentRegistration) (Agent, error) {
	if s == nil || s.db == nil {
		return Agent{}, fmt.Errorf("auth store database is not initialized")
	}

	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		return Agent{}, ErrAgentNotFound
	}

	nowText := time.Now().UTC().Format(time.RFC3339Nano)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Agent{}, fmt.Errorf("begin update registration transaction: %w", err)
	}

	var (
		username     string
		actorID      string
		authMethod   string
		metadataJSON string
	)
	if err := tx.QueryRowContext(
		ctx,
		fmt.Sprintf(`SELECT a.username, a.actor_id, %s, a.metadata_json FROM agents a WHERE a.id = ?`, authMethodExpr("a")),
		agentID,
	).Scan(&username, &actorID, &authMethod, &metadataJSON); err != nil {
		_ = tx.Rollback()
		if errors.Is(err, sql.ErrNoRows) {
			return Agent{}, ErrAgentNotFound
		}
		return Agent{}, fmt.Errorf("query agent registration target: %w", err)
	}
	if strings.TrimSpace(authMethod) == AuthMethodControlPlane {
		_ = tx.Rollback()
		return Agent{}, fmt.Errorf("%w: control-plane-backed principals cannot modify workspace-local registration state", ErrInvalidRequest)
	}

	normalized := normalizeAgentRegistration(registration)
	if normalized.Handle == "" {
		normalized.Handle = strings.TrimSpace(username)
	}
	if normalized.Handle != strings.TrimSpace(username) {
		_ = tx.Rollback()
		return Agent{}, fmt.Errorf("%w: registration.handle must match the authenticated agent username", ErrInvalidRequest)
	}
	if normalized.ActorID == "" {
		normalized.ActorID = strings.TrimSpace(actorID)
	}
	if normalized.ActorID != strings.TrimSpace(actorID) {
		_ = tx.Rollback()
		return Agent{}, fmt.Errorf("%w: registration.actor_id must match the authenticated agent actor_id", ErrInvalidRequest)
	}
	normalized.UpdatedAt = nowText

	nextMetadataJSON, err := mergeRegistrationMetadataJSON(metadataJSON, normalized)
	if err != nil {
		_ = tx.Rollback()
		return Agent{}, fmt.Errorf("merge registration metadata: %w", err)
	}
	result, err := tx.ExecContext(
		ctx,
		`UPDATE agents
		 SET metadata_json = ?, updated_at = ?
		 WHERE id = ?`,
		nextMetadataJSON,
		nowText,
		agentID,
	)
	if err != nil {
		_ = tx.Rollback()
		return Agent{}, fmt.Errorf("update agent registration: %w", err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		_ = tx.Rollback()
		return Agent{}, fmt.Errorf("read update registration rows affected: %w", err)
	}
	if rowsAffected == 0 {
		_ = tx.Rollback()
		return Agent{}, ErrAgentNotFound
	}
	if err := tx.Commit(); err != nil {
		return Agent{}, fmt.Errorf("commit update registration transaction: %w", err)
	}
	return s.GetAgent(ctx, agentID)
}

func (s *Store) RotateKey(ctx context.Context, agentID string, publicKey string) (AgentKey, error) {
	if s == nil || s.db == nil {
		return AgentKey{}, fmt.Errorf("auth store database is not initialized")
	}

	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		return AgentKey{}, ErrAgentNotFound
	}

	if _, err := decodeEd25519PublicKey(publicKey); err != nil {
		return AgentKey{}, fmt.Errorf("%w: public_key must be a base64-encoded ed25519 public key: %v", ErrInvalidRequest, err)
	}

	now := time.Now().UTC()
	nowText := now.Format(time.RFC3339Nano)
	keyID := "key_" + uuid.NewString()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return AgentKey{}, fmt.Errorf("begin rotate key transaction: %w", err)
	}

	var revokedAt sql.NullString
	var authMethod string
	err = tx.QueryRowContext(ctx, fmt.Sprintf(`SELECT revoked_at, %s FROM agents a WHERE id = ?`, authMethodExpr("a")), agentID).Scan(&revokedAt, &authMethod)
	if err != nil {
		_ = tx.Rollback()
		if errors.Is(err, sql.ErrNoRows) {
			return AgentKey{}, ErrAgentNotFound
		}
		return AgentKey{}, fmt.Errorf("query agent before key rotation: %w", err)
	}
	if revokedAt.Valid {
		_ = tx.Rollback()
		return AgentKey{}, ErrAgentRevoked
	}
	if strings.TrimSpace(authMethod) == AuthMethodControlPlane {
		_ = tx.Rollback()
		return AgentKey{}, fmt.Errorf("%w: control-plane-backed principals cannot rotate workspace-local keys", ErrInvalidRequest)
	}

	_, err = tx.ExecContext(
		ctx,
		`UPDATE agent_keys
		 SET revoked_at = ?
		 WHERE agent_id = ? AND revoked_at IS NULL`,
		nowText,
		agentID,
	)
	if err != nil {
		_ = tx.Rollback()
		return AgentKey{}, fmt.Errorf("revoke existing keys: %w", err)
	}

	_, err = tx.ExecContext(
		ctx,
		`INSERT INTO agent_keys(id, agent_id, public_key, algorithm, created_at, revoked_at)
		 VALUES (?, ?, ?, 'ed25519', ?, NULL)`,
		keyID,
		agentID,
		strings.TrimSpace(publicKey),
		nowText,
	)
	if err != nil {
		_ = tx.Rollback()
		return AgentKey{}, fmt.Errorf("insert rotated key: %w", err)
	}

	_, err = tx.ExecContext(
		ctx,
		`UPDATE agents SET updated_at = ? WHERE id = ?`,
		nowText,
		agentID,
	)
	if err != nil {
		_ = tx.Rollback()
		return AgentKey{}, fmt.Errorf("update agent timestamp during key rotation: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return AgentKey{}, fmt.Errorf("commit rotate key transaction: %w", err)
	}

	return AgentKey{
		KeyID:     keyID,
		AgentID:   agentID,
		Algorithm: "ed25519",
		PublicKey: strings.TrimSpace(publicKey),
		CreatedAt: nowText,
	}, nil
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
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return RevokeAgentResult{}, fmt.Errorf("begin revoke agent transaction: %w", err)
	}

	var (
		subjectActorID  string
		existingRevoked sql.NullString
		principal       AuthPrincipalSummary
	)
	err = tx.QueryRowContext(
		ctx,
		`SELECT actor_id, revoked_at
		 FROM agents
		 WHERE id = ?`,
		agentID,
	).Scan(&subjectActorID, &existingRevoked)
	if err != nil {
		_ = tx.Rollback()
		if errors.Is(err, sql.ErrNoRows) {
			return RevokeAgentResult{}, ErrAgentNotFound
		}
		return RevokeAgentResult{}, fmt.Errorf("query agent revoke state: %w", err)
	}
	if existingRevoked.Valid {
		principal, loadErr := s.getPrincipalSummaryTx(ctx, tx, agentID)
		_ = tx.Rollback()
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
		_ = tx.Rollback()
		return RevokeAgentResult{}, err
	}
	if principal.AuthMethod == AuthMethodControlPlane {
		_ = tx.Rollback()
		return RevokeAgentResult{}, fmt.Errorf("%w: control-plane-backed principals cannot be revoked locally", ErrInvalidRequest)
	}
	activeHumanCount, err := s.countActiveHumanPrincipalsTx(ctx, tx)
	if err != nil {
		_ = tx.Rollback()
		return RevokeAgentResult{}, err
	}
	if principal.PrincipalKind == "human" && activeHumanCount == 1 && !input.AllowHumanLockout {
		_ = tx.Rollback()
		return RevokeAgentResult{}, ErrLastActivePrincipal
	}
	usedHumanLockout := principal.PrincipalKind == "human" && activeHumanCount == 1 && input.AllowHumanLockout
	if input.AllowHumanLockout && !usedHumanLockout {
		_ = tx.Rollback()
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
		_ = tx.Rollback()
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
		_ = tx.Rollback()
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
		_ = tx.Rollback()
		return RevokeAgentResult{}, err
	}

	principal, err = s.getPrincipalSummaryTx(ctx, tx, agentID)
	if err != nil {
		_ = tx.Rollback()
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

func (s *Store) countActiveHumanPrincipalsTx(ctx context.Context, tx *sql.Tx) (int, error) {
	var count int
	if err := tx.QueryRowContext(
		ctx,
		fmt.Sprintf(`SELECT COUNT(1)
		 FROM agents a
		 WHERE a.revoked_at IS NULL
		   AND %s = 'human'
		   AND %s != '%s'`, principalKindExpr("a"), authMethodExpr("a"), AuthMethodControlPlane),
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
	if err := s.db.QueryRowContext(
		ctx,
		fmt.Sprintf(`SELECT COUNT(1)
		 FROM agents a
		 WHERE a.revoked_at IS NULL
		   AND %s = 'human'
		   AND %s != '%s'`, principalKindExpr("a"), authMethodExpr("a"), AuthMethodControlPlane),
	).Scan(&count); err != nil {
		return 0, fmt.Errorf("count active human principals: %w", err)
	}
	return count, nil
}

func (s *Store) getPrincipalSummaryTx(ctx context.Context, tx *sql.Tx, agentID string) (AuthPrincipalSummary, error) {
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

func (s *Store) issueTokenBundleTx(ctx context.Context, tx *sql.Tx, agentID string, now time.Time) (TokenBundle, string, error) {
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

func waitForRegisterRetry(ctx context.Context, attempt int) error {
	delay := registerAgentRetryBase << attempt
	if delay > registerAgentRetryMax {
		delay = registerAgentRetryMax
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return fmt.Errorf("register agent canceled while waiting to retry: %w", ctx.Err())
	case <-timer.C:
		return nil
	}
}
