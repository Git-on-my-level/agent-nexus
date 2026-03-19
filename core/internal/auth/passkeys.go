package auth

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/google/uuid"
)

var ErrPasskeyNotFound = errors.New("passkey_not_found")

type RegisterPasskeyAgentInput struct {
	DisplayName string
	UserHandle  []byte
	Credential  *webauthn.Credential
}

type PasskeyIdentity struct {
	Agent       Agent
	DisplayName string
	UserHandle  []byte
	Credentials []webauthn.Credential
}

func (s *Store) RegisterPasskeyAgent(ctx context.Context, input RegisterPasskeyAgentInput, claim OnboardingClaim) (Agent, TokenBundle, error) {
	if s == nil || s.db == nil {
		return Agent{}, TokenBundle{}, fmt.Errorf("auth store database is not initialized")
	}

	displayName, err := NormalizePasskeyDisplayName(input.DisplayName)
	if err != nil {
		return Agent{}, TokenBundle{}, fmt.Errorf("%w: %v", ErrInvalidRequest, err)
	}
	if len(input.UserHandle) == 0 {
		return Agent{}, TokenBundle{}, fmt.Errorf("%w: user handle is required", ErrInvalidRequest)
	}
	if len(input.UserHandle) > 64 {
		return Agent{}, TokenBundle{}, fmt.Errorf("%w: user handle must be 64 bytes or fewer", ErrInvalidRequest)
	}
	if input.Credential == nil {
		return Agent{}, TokenBundle{}, fmt.Errorf("%w: passkey credential is required", ErrInvalidRequest)
	}

	now := time.Now().UTC()
	nowText := now.Format(time.RFC3339Nano)
	agentID := "agent_" + uuid.NewString()
	actorID := agentID
	username, err := generatePasskeyUsername(displayName)
	if err != nil {
		return Agent{}, TokenBundle{}, fmt.Errorf("%w: %v", ErrInvalidRequest, err)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Agent{}, TokenBundle{}, fmt.Errorf("begin register passkey transaction: %w", err)
	}

	if err := s.consumeOnboardingClaimTx(ctx, tx, claim, agentID, actorID, now); err != nil {
		_ = tx.Rollback()
		return Agent{}, TokenBundle{}, err
	}

	_, err = tx.ExecContext(
		ctx,
		`INSERT INTO agents(id, username, actor_id, created_at, updated_at, revoked_at, metadata_json)
		 VALUES (?, ?, ?, ?, ?, NULL, '{}')`,
		agentID,
		username,
		actorID,
		nowText,
		nowText,
	)
	if err != nil {
		_ = tx.Rollback()
		return Agent{}, TokenBundle{}, fmt.Errorf("insert passkey agent: %w", err)
	}

	_, err = tx.ExecContext(
		ctx,
		`INSERT INTO actors(id, display_name, tags_json, created_at, metadata_json)
		 VALUES (?, ?, ?, ?, '{}')`,
		actorID,
		displayName,
		`["agent","human","passkey"]`,
		nowText,
	)
	if err != nil {
		_ = tx.Rollback()
		return Agent{}, TokenBundle{}, fmt.Errorf("insert passkey actor: %w", err)
	}

	if err := insertPasskeyCredentialTx(ctx, tx, agentID, input.UserHandle, *input.Credential, nowText); err != nil {
		_ = tx.Rollback()
		if errors.Is(err, ErrInvalidRequest) {
			return Agent{}, TokenBundle{}, err
		}
		return Agent{}, TokenBundle{}, fmt.Errorf("insert passkey credential: %w", err)
	}

	tokens, _, err := s.issueTokenBundleTx(ctx, tx, agentID, now)
	if err != nil {
		_ = tx.Rollback()
		return Agent{}, TokenBundle{}, err
	}

	if err := tx.Commit(); err != nil {
		_ = tx.Rollback()
		return Agent{}, TokenBundle{}, fmt.Errorf("commit register passkey transaction: %w", err)
	}

	return Agent{
		AgentID:       agentID,
		Username:      username,
		ActorID:       actorID,
		Revoked:       false,
		CreatedAt:     nowText,
		UpdatedAt:     nowText,
		PrincipalKind: ptrString("human"),
		AuthMethod:    ptrString("passkey"),
	}, tokens, nil
}

func (s *Store) IssueTokenForPasskey(ctx context.Context, agentID string, credential webauthn.Credential) (TokenBundle, error) {
	if s == nil || s.db == nil {
		return TokenBundle{}, fmt.Errorf("auth store database is not initialized")
	}

	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		return TokenBundle{}, ErrAgentNotFound
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return TokenBundle{}, fmt.Errorf("begin passkey token transaction: %w", err)
	}

	var revokedAt sql.NullString
	err = tx.QueryRowContext(ctx, `SELECT revoked_at FROM agents WHERE id = ?`, agentID).Scan(&revokedAt)
	if err != nil {
		_ = tx.Rollback()
		if errors.Is(err, sql.ErrNoRows) {
			return TokenBundle{}, ErrAgentNotFound
		}
		return TokenBundle{}, fmt.Errorf("load passkey agent: %w", err)
	}
	if revokedAt.Valid {
		_ = tx.Rollback()
		return TokenBundle{}, ErrAgentRevoked
	}

	if err := updatePasskeyCredentialTx(ctx, tx, agentID, credential); err != nil {
		_ = tx.Rollback()
		if errors.Is(err, ErrPasskeyNotFound) {
			return TokenBundle{}, ErrPasskeyNotFound
		}
		return TokenBundle{}, fmt.Errorf("update passkey credential: %w", err)
	}

	tokens, _, err := s.issueTokenBundleTx(ctx, tx, agentID, time.Now().UTC())
	if err != nil {
		_ = tx.Rollback()
		return TokenBundle{}, err
	}

	if err := tx.Commit(); err != nil {
		return TokenBundle{}, fmt.Errorf("commit passkey token transaction: %w", err)
	}

	return tokens, nil
}

func (s *Store) GetPasskeyIdentityByUsername(ctx context.Context, username string) (PasskeyIdentity, error) {
	normalized, err := normalizeUsername(username)
	if err != nil {
		return PasskeyIdentity{}, ErrPasskeyNotFound
	}
	return s.getPasskeyIdentity(ctx, `a.username = ?`, normalized)
}

func (s *Store) GetPasskeyIdentityByUserHandle(ctx context.Context, userHandle []byte) (PasskeyIdentity, error) {
	if len(userHandle) == 0 {
		return PasskeyIdentity{}, ErrPasskeyNotFound
	}
	return s.getPasskeyIdentity(ctx, `pc.user_handle = ?`, userHandle)
}

func (s *Store) getPasskeyIdentity(ctx context.Context, whereClause string, value any) (PasskeyIdentity, error) {
	if s == nil || s.db == nil {
		return PasskeyIdentity{}, fmt.Errorf("auth store database is not initialized")
	}

	rows, err := s.db.QueryContext(
		ctx,
		fmt.Sprintf(
			`SELECT
			 a.id,
			 a.username,
			 a.actor_id,
			 a.created_at,
			 a.updated_at,
			 a.revoked_at,
			 ac.display_name,
			 pc.user_handle,
			 pc.credential_id,
			 pc.public_key,
			 pc.attestation_type,
			 pc.transport,
			 pc.sign_count,
			 pc.backup_eligible,
			 pc.backup_state,
			 pc.aaguid,
			 pc.attachment
			 FROM passkey_credentials pc
			 JOIN agents a ON a.id = pc.agent_id
			 JOIN actors ac ON ac.id = a.actor_id
			 WHERE %s
			 ORDER BY pc.created_at ASC, pc.credential_id ASC`,
			whereClause,
		),
		value,
	)
	if err != nil {
		return PasskeyIdentity{}, fmt.Errorf("query passkey identity: %w", err)
	}
	defer rows.Close()

	var (
		identity PasskeyIdentity
		found    bool
	)
	for rows.Next() {
		var (
			revokedAt        sql.NullString
			displayName      string
			userHandle       []byte
			credentialIDText string
			publicKey        []byte
			attestationType  string
			transportText    string
			signCount        int64
			backupEligible   bool
			backupState      bool
			aaguid           []byte
			attachment       string
		)
		agent := Agent{}
		if err := rows.Scan(
			&agent.AgentID,
			&agent.Username,
			&agent.ActorID,
			&agent.CreatedAt,
			&agent.UpdatedAt,
			&revokedAt,
			&displayName,
			&userHandle,
			&credentialIDText,
			&publicKey,
			&attestationType,
			&transportText,
			&signCount,
			&backupEligible,
			&backupState,
			&aaguid,
			&attachment,
		); err != nil {
			return PasskeyIdentity{}, fmt.Errorf("scan passkey identity row: %w", err)
		}
		agent.Revoked = revokedAt.Valid
		if !found {
			identity.Agent = agent
			identity.DisplayName = displayName
			identity.UserHandle = append([]byte(nil), userHandle...)
			found = true
		}
		credentialID, err := decodePasskeyID(credentialIDText)
		if err != nil {
			return PasskeyIdentity{}, fmt.Errorf("decode stored passkey credential id: %w", err)
		}
		credential := webauthn.Credential{
			ID:              credentialID,
			PublicKey:       append([]byte(nil), publicKey...),
			AttestationType: attestationType,
			Transport:       parsePasskeyTransports(transportText),
			Flags: webauthn.CredentialFlags{
				BackupEligible: backupEligible,
				BackupState:    backupState,
			},
			Authenticator: webauthn.Authenticator{
				AAGUID:     append([]byte(nil), aaguid...),
				SignCount:  uint32(maxInt64(signCount, 0)),
				Attachment: protocol.AuthenticatorAttachment(strings.TrimSpace(attachment)),
			},
		}
		identity.Credentials = append(identity.Credentials, credential)
	}
	if err := rows.Err(); err != nil {
		return PasskeyIdentity{}, fmt.Errorf("iterate passkey identity rows: %w", err)
	}
	if !found {
		return PasskeyIdentity{}, ErrPasskeyNotFound
	}

	return identity, nil
}

func insertPasskeyCredentialTx(ctx context.Context, tx *sql.Tx, agentID string, userHandle []byte, credential webauthn.Credential, createdAt string) error {
	credentialID := encodePasskeyID(credential.ID)
	if credentialID == "" {
		return fmt.Errorf("%w: credential id is required", ErrInvalidRequest)
	}
	if len(credential.PublicKey) == 0 {
		return fmt.Errorf("%w: credential public key is required", ErrInvalidRequest)
	}

	_, err := tx.ExecContext(
		ctx,
		`INSERT INTO passkey_credentials(
			credential_id,
			agent_id,
			user_handle,
			public_key,
			attestation_type,
			transport,
			sign_count,
			backup_eligible,
			backup_state,
			aaguid,
			attachment,
			created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		credentialID,
		agentID,
		append([]byte(nil), userHandle...),
		append([]byte(nil), credential.PublicKey...),
		strings.TrimSpace(credential.AttestationType),
		formatPasskeyTransports(credential.Transport),
		int64(credential.Authenticator.SignCount),
		credential.Flags.BackupEligible,
		credential.Flags.BackupState,
		append([]byte(nil), credential.Authenticator.AAGUID...),
		strings.TrimSpace(string(credential.Authenticator.Attachment)),
		createdAt,
	)
	if err != nil {
		return err
	}
	return nil
}

func updatePasskeyCredentialTx(ctx context.Context, tx *sql.Tx, agentID string, credential webauthn.Credential) error {
	result, err := tx.ExecContext(
		ctx,
		`UPDATE passkey_credentials
		 SET
		   sign_count = ?,
		   backup_eligible = ?,
		   backup_state = ?,
		   aaguid = ?,
		   attachment = ?,
		   transport = ?
		 WHERE agent_id = ? AND credential_id = ?`,
		int64(credential.Authenticator.SignCount),
		credential.Flags.BackupEligible,
		credential.Flags.BackupState,
		append([]byte(nil), credential.Authenticator.AAGUID...),
		strings.TrimSpace(string(credential.Authenticator.Attachment)),
		formatPasskeyTransports(credential.Transport),
		agentID,
		encodePasskeyID(credential.ID),
	)
	if err != nil {
		return err
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read passkey credential rows affected: %w", err)
	}
	if rowsAffected == 0 {
		return ErrPasskeyNotFound
	}
	return nil
}

func NormalizePasskeyDisplayName(raw string) (string, error) {
	displayName := strings.TrimSpace(raw)
	if displayName == "" {
		return "", fmt.Errorf("display_name is required")
	}
	if len(displayName) > 120 {
		return "", fmt.Errorf("display_name must be 120 characters or fewer")
	}
	return displayName, nil
}

var nonAlphaNumeric = regexp.MustCompile(`[^a-z0-9]+`)

func generatePasskeyUsername(displayName string) (string, error) {
	base := strings.ToLower(strings.TrimSpace(displayName))
	base = nonAlphaNumeric.ReplaceAllString(base, "-")
	base = strings.Trim(base, "-")
	base = strings.ReplaceAll(base, "-", ".")
	base = strings.Trim(base, ".")
	if len(base) > 24 {
		base = strings.Trim(base[:24], ".")
	}
	if len(base) < 3 {
		base = "user"
	}
	suffix := make([]byte, 4)
	if _, err := rand.Read(suffix); err != nil {
		return "", fmt.Errorf("generate passkey username suffix: %w", err)
	}
	username, err := normalizeUsername(
		fmt.Sprintf("passkey.%s.%s", base, hex.EncodeToString(suffix)),
	)
	if err != nil {
		return "", err
	}
	return username, nil
}

func encodePasskeyID(raw []byte) string {
	if len(raw) == 0 {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}

func decodePasskeyID(raw string) ([]byte, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("empty passkey id")
	}
	return base64.RawURLEncoding.DecodeString(raw)
}

func formatPasskeyTransports(transports []protocol.AuthenticatorTransport) string {
	if len(transports) == 0 {
		return ""
	}
	parts := make([]string, 0, len(transports))
	for _, transport := range transports {
		value := strings.TrimSpace(string(transport))
		if value == "" {
			continue
		}
		parts = append(parts, value)
	}
	return strings.Join(parts, ",")
}

func parsePasskeyTransports(raw string) []protocol.AuthenticatorTransport {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	transports := make([]protocol.AuthenticatorTransport, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		transports = append(transports, protocol.AuthenticatorTransport(part))
	}
	return transports
}

func maxInt64(value int64, minimum int64) int64 {
	if value < minimum {
		return minimum
	}
	return value
}

func ptrString(s string) *string {
	return &s
}
