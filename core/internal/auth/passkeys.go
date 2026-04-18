package auth

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"regexp"
	"strings"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/google/uuid"
)

var ErrPasskeyNotFound = errors.New("passkey_not_found")

// ErrAmbiguousPasskeyPrincipal is returned when multiple passkey-backed principals
// match a dev-only lookup that requires a unique selection (for example sole-principal sign-in).
var ErrAmbiguousPasskeyPrincipal = errors.New("ambiguous_passkey_principal")

// DevSyntheticPasskeyCredential returns a placeholder WebAuthn credential used when
// registration bypasses the browser ceremony in development. It cannot satisfy real assertion verification.
func DevSyntheticPasskeyCredential() (webauthn.Credential, error) {
	id := make([]byte, 32)
	if _, err := rand.Read(id); err != nil {
		return webauthn.Credential{}, err
	}
	return webauthn.Credential{
		ID:              id,
		PublicKey:       []byte{0x01},
		AttestationType: "none",
		Authenticator: webauthn.Authenticator{
			SignCount: 0,
			// Non-empty slice so SQLite driver binds a BLOB (nil/empty AAGUID would store NULL).
			AAGUID: make([]byte, 16),
		},
	}, nil
}

type RegisterPasskeyAgentInput struct {
	DisplayName string
	UserHandle  []byte
	Credential  *webauthn.Credential
	// ExistingActorID, when set, links the new passkey agent to a pre-seeded actor row
	// when the explicit local passkey bypass capability and ANX_DEV_REGISTER_LINKED_ACTORS
	// are both enabled for this core instance.
	ExistingActorID string
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

	existingActorID := strings.TrimSpace(input.ExistingActorID)
	if existingActorID != "" && !s.allowDevRegisterLinkedActor {
		return Agent{}, TokenBundle{}, fmt.Errorf("%w: existing_actor_id is not enabled for this core instance", ErrInvalidRequest)
	}

	now := time.Now().UTC()
	nowText := now.Format(time.RFC3339Nano)
	agentID := "agent_" + uuid.NewString()
	actorID := agentID
	if existingActorID != "" {
		actorID = existingActorID
	}
	username, err := generatePasskeyUsername(displayName)
	if err != nil {
		return Agent{}, TokenBundle{}, fmt.Errorf("%w: %v", ErrInvalidRequest, err)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Agent{}, TokenBundle{}, fmt.Errorf("begin register passkey transaction: %w", err)
	}

	if err := s.consumeOnboardingClaimTx(ctx, tx, claim, agentID, actorID, now); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return Agent{}, TokenBundle{}, err
	}

	if existingActorID != "" {
		if err := s.ensureExistingActorReadyForAgentLinkTx(ctx, tx, actorID); err != nil {
			if rbErr := tx.Rollback(); rbErr != nil {
				log.Printf("tx rollback failed: %v", rbErr)
			}
			return Agent{}, TokenBundle{}, err
		}
	}

	agentMetadataJSON, err := principalMetadataJSON(PrincipalKindHuman, AuthMethodPasskey, nil)
	if err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return Agent{}, TokenBundle{}, fmt.Errorf("encode passkey agent metadata: %w", err)
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
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return Agent{}, TokenBundle{}, fmt.Errorf("insert passkey agent: %w", err)
	}

	actorMetadataValue, err := actorMetadataJSON(PrincipalKindHuman, AuthMethodPasskey, nil)
	if err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return Agent{}, TokenBundle{}, fmt.Errorf("encode passkey actor metadata: %w", err)
	}
	if existingActorID == "" {
		_, err = tx.ExecContext(
			ctx,
			`INSERT INTO actors(id, display_name, tags_json, created_at, metadata_json)
			 VALUES (?, ?, ?, ?, ?)`,
			actorID,
			displayName,
			`["agent","human","passkey"]`,
			nowText,
			actorMetadataValue,
		)
		if err != nil {
			if rbErr := tx.Rollback(); rbErr != nil {
				log.Printf("tx rollback failed: %v", rbErr)
			}
			return Agent{}, TokenBundle{}, fmt.Errorf("insert passkey actor: %w", err)
		}
	} else {
		_, err = tx.ExecContext(
			ctx,
			`UPDATE actors SET metadata_json = ? WHERE id = ?`,
			actorMetadataValue,
			actorID,
		)
		if err != nil {
			if rbErr := tx.Rollback(); rbErr != nil {
				log.Printf("tx rollback failed: %v", rbErr)
			}
			return Agent{}, TokenBundle{}, fmt.Errorf("update linked actor metadata for passkey: %w", err)
		}
	}

	if err := insertPasskeyCredentialTx(ctx, tx, agentID, input.UserHandle, *input.Credential, nowText); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		if errors.Is(err, ErrInvalidRequest) {
			return Agent{}, TokenBundle{}, err
		}
		return Agent{}, TokenBundle{}, fmt.Errorf("insert passkey credential: %w", err)
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
			"principal_kind":  "human",
			"auth_method":     AuthMethodPasskey,
			"onboarding_mode": string(claim.Mode),
		},
	}); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return Agent{}, TokenBundle{}, err
	}

	tokens, _, err := s.issueTokenBundleTx(ctx, tx, agentID, now)
	if err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return Agent{}, TokenBundle{}, err
	}

	if err := tx.Commit(); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
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
		AuthMethod:    ptrString(AuthMethodPasskey),
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
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		if errors.Is(err, sql.ErrNoRows) {
			return TokenBundle{}, ErrAgentNotFound
		}
		return TokenBundle{}, fmt.Errorf("load passkey agent: %w", err)
	}
	if revokedAt.Valid {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return TokenBundle{}, ErrAgentRevoked
	}

	if err := updatePasskeyCredentialTx(ctx, tx, agentID, credential); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		if errors.Is(err, ErrPasskeyNotFound) {
			return TokenBundle{}, ErrPasskeyNotFound
		}
		return TokenBundle{}, fmt.Errorf("update passkey credential: %w", err)
	}

	tokens, _, err := s.issueTokenBundleTx(ctx, tx, agentID, time.Now().UTC())
	if err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
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

// GetPasskeyIdentityByDisplayName returns the passkey identity for the unique principal whose
// actor display name matches (trimmed). It returns ErrAmbiguousPasskeyPrincipal if more than one principal matches.
func (s *Store) GetPasskeyIdentityByDisplayName(ctx context.Context, displayName string) (PasskeyIdentity, error) {
	displayName = strings.TrimSpace(displayName)
	if displayName == "" {
		return PasskeyIdentity{}, ErrPasskeyNotFound
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT DISTINCT a.id
		FROM agents a
		INNER JOIN passkey_credentials pc ON pc.agent_id = a.id
		INNER JOIN actors ac ON ac.id = a.actor_id
		WHERE a.revoked_at IS NULL AND ac.display_name = ?`, displayName)
	if err != nil {
		return PasskeyIdentity{}, fmt.Errorf("query passkey principals by display name: %w", err)
	}
	defer rows.Close()

	var agentIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return PasskeyIdentity{}, fmt.Errorf("scan agent id: %w", err)
		}
		agentIDs = append(agentIDs, id)
	}
	if err := rows.Err(); err != nil {
		return PasskeyIdentity{}, err
	}
	switch len(agentIDs) {
	case 0:
		return PasskeyIdentity{}, ErrPasskeyNotFound
	case 1:
		return s.getPasskeyIdentity(ctx, `a.id = ?`, agentIDs[0])
	default:
		return PasskeyIdentity{}, ErrAmbiguousPasskeyPrincipal
	}
}

// GetSolePasskeyIdentity returns the identity when exactly one non-revoked passkey principal exists.
func (s *Store) GetSolePasskeyIdentity(ctx context.Context) (PasskeyIdentity, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT DISTINCT a.id
		FROM agents a
		INNER JOIN passkey_credentials pc ON pc.agent_id = a.id
		WHERE a.revoked_at IS NULL`)
	if err != nil {
		return PasskeyIdentity{}, fmt.Errorf("query passkey principals: %w", err)
	}
	defer rows.Close()

	var agentIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return PasskeyIdentity{}, fmt.Errorf("scan agent id: %w", err)
		}
		agentIDs = append(agentIDs, id)
	}
	if err := rows.Err(); err != nil {
		return PasskeyIdentity{}, err
	}
	switch len(agentIDs) {
	case 0:
		return PasskeyIdentity{}, ErrPasskeyNotFound
	case 1:
		return s.getPasskeyIdentity(ctx, `a.id = ?`, agentIDs[0])
	default:
		return PasskeyIdentity{}, ErrAmbiguousPasskeyPrincipal
	}
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
