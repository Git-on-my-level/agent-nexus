package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

var ErrOnboardingRequired = errors.New("onboarding_required")
var ErrBootstrapRequired = errors.New("bootstrap_required")
var ErrInviteRequired = errors.New("invite_required")
var ErrInviteNotFound = errors.New("invite_not_found")
var ErrInviteKindMismatch = errors.New("invite_kind_mismatch")

type PrincipalKind string

const (
	PrincipalKindHuman PrincipalKind = "human"
	PrincipalKindAgent PrincipalKind = "agent"
	PrincipalKindAny   PrincipalKind = "any"
)

type OnboardingMode string

const (
	OnboardingModeBootstrap OnboardingMode = "bootstrap"
	OnboardingModeInvite    OnboardingMode = "invite"
)

type OnboardingClaim struct {
	Mode          OnboardingMode
	PrincipalKind PrincipalKind
	TokenHash     string
	InviteID      string
	InviteKind    string
}

type Invite struct {
	ID                string  `json:"id"`
	Kind              string  `json:"kind"`
	CreatedByAgentID  string  `json:"created_by_agent_id"`
	CreatedByActorID  string  `json:"created_by_actor_id"`
	CreatedAt         string  `json:"created_at"`
	ExpiresAt         *string `json:"expires_at,omitempty"`
	ConsumedAt        *string `json:"consumed_at,omitempty"`
	ConsumedByAgentID *string `json:"consumed_by_agent_id,omitempty"`
	ConsumedByActorID *string `json:"consumed_by_actor_id,omitempty"`
	RevokedAt         *string `json:"revoked_at,omitempty"`
	RevokedByAgentID  *string `json:"revoked_by_agent_id,omitempty"`
	RevokedByActorID  *string `json:"revoked_by_actor_id,omitempty"`
}

type CreateInviteInput struct {
	Kind      string
	ExpiresAt *time.Time
}

func NormalizePrincipalKind(raw string, allowAny bool) (PrincipalKind, error) {
	switch PrincipalKind(strings.ToLower(strings.TrimSpace(raw))) {
	case PrincipalKindHuman:
		return PrincipalKindHuman, nil
	case PrincipalKindAgent:
		return PrincipalKindAgent, nil
	case PrincipalKindAny:
		if allowAny {
			return PrincipalKindAny, nil
		}
	}
	return "", fmt.Errorf("kind must be %s, %s%s", PrincipalKindHuman, PrincipalKindAgent, func() string {
		if allowAny {
			return ", or " + string(PrincipalKindAny)
		}
		return ""
	}())
}

func (s *Store) BootstrapRegistrationAvailable(ctx context.Context) (bool, error) {
	if s == nil || s.db == nil {
		return false, fmt.Errorf("auth store database is not initialized")
	}
	return s.bootstrapRegistrationAvailable(ctx)
}

func (s *Store) bootstrapRegistrationAvailable(ctx context.Context) (bool, error) {
	if strings.TrimSpace(s.bootstrapTokenHash) == "" {
		return false, nil
	}

	var consumedAt sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT consumed_at FROM auth_bootstrap_state WHERE id = 1`).Scan(&consumedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return true, nil
		}
		return false, fmt.Errorf("query bootstrap state: %w", err)
	}
	return !consumedAt.Valid, nil
}

func (s *Store) ResolveOnboardingClaim(ctx context.Context, bootstrapToken string, inviteToken string, principalKind PrincipalKind) (OnboardingClaim, error) {
	if s == nil || s.db == nil {
		return OnboardingClaim{}, fmt.Errorf("auth store database is not initialized")
	}
	if _, err := NormalizePrincipalKind(string(principalKind), false); err != nil {
		return OnboardingClaim{}, fmt.Errorf("%w: %v", ErrInvalidRequest, err)
	}

	bootstrapToken = strings.TrimSpace(bootstrapToken)
	inviteToken = strings.TrimSpace(inviteToken)
	if bootstrapToken != "" && inviteToken != "" {
		return OnboardingClaim{}, fmt.Errorf("%w: provide only one of bootstrap_token or invite_token", ErrInvalidRequest)
	}

	bootstrapAvailable, err := s.bootstrapRegistrationAvailable(ctx)
	if err != nil {
		return OnboardingClaim{}, err
	}
	if bootstrapAvailable {
		if bootstrapToken == "" {
			return OnboardingClaim{}, ErrBootstrapRequired
		}
		if hashToken(bootstrapToken) != s.bootstrapTokenHash {
			return OnboardingClaim{}, ErrInvalidToken
		}
		return OnboardingClaim{
			Mode:          OnboardingModeBootstrap,
			PrincipalKind: principalKind,
			TokenHash:     s.bootstrapTokenHash,
		}, nil
	}

	if inviteToken == "" {
		if bootstrapToken != "" {
			return OnboardingClaim{}, ErrInvalidToken
		}
		return OnboardingClaim{}, ErrInviteRequired
	}

	return s.resolveInviteClaim(ctx, inviteToken, principalKind)
}

func (s *Store) CreateInvite(ctx context.Context, createdBy Principal, input CreateInviteInput) (Invite, string, error) {
	if s == nil || s.db == nil {
		return Invite{}, "", fmt.Errorf("auth store database is not initialized")
	}

	kind, err := NormalizePrincipalKind(input.Kind, true)
	if err != nil {
		return Invite{}, "", fmt.Errorf("%w: %v", ErrInvalidRequest, err)
	}
	createdBy.AgentID = strings.TrimSpace(createdBy.AgentID)
	createdBy.ActorID = strings.TrimSpace(createdBy.ActorID)
	if createdBy.AgentID == "" || createdBy.ActorID == "" {
		return Invite{}, "", fmt.Errorf("%w: authenticated principal is required", ErrAuthRequired)
	}

	var expiresAtText *string
	if input.ExpiresAt != nil {
		expiresAt := input.ExpiresAt.UTC()
		if !expiresAt.After(time.Now().UTC()) {
			return Invite{}, "", fmt.Errorf("%w: expires_at must be in the future", ErrInvalidRequest)
		}
		value := expiresAt.Format(time.RFC3339Nano)
		expiresAtText = &value
	}

	now := time.Now().UTC()
	nowText := now.Format(time.RFC3339Nano)
	inviteID := "invite_" + uuid.NewString()
	tokenBody, err := generateOpaqueToken(24)
	if err != nil {
		return Invite{}, "", fmt.Errorf("generate invite token: %w", err)
	}
	token := "oinv_" + tokenBody

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Invite{}, "", fmt.Errorf("begin create invite transaction: %w", err)
	}

	_, err = tx.ExecContext(
		ctx,
		`INSERT INTO auth_invites(
			id,
			token_hash,
			kind,
			created_by_agent_id,
			created_by_actor_id,
			note,
			created_at,
			expires_at,
			consumed_at,
			consumed_by_agent_id,
			consumed_by_actor_id,
			revoked_at,
			revoked_by_agent_id,
			revoked_by_actor_id
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, NULL, NULL, NULL, NULL, NULL, NULL)`,
		inviteID,
		hashToken(token),
		string(kind),
		createdBy.AgentID,
		createdBy.ActorID,
		"",
		nowText,
		expiresAtText,
	)
	if err != nil {
		_ = tx.Rollback()
		return Invite{}, "", fmt.Errorf("insert auth invite: %w", err)
	}

	metadata := map[string]any{"kind": string(kind)}
	if expiresAtText != nil {
		metadata["expires_at"] = *expiresAtText
	}
	if err := s.recordAuthAuditEventTx(ctx, tx, AuthAuditEventInput{
		EventType:    AuthAuditEventInviteCreated,
		OccurredAt:   now,
		ActorAgentID: createdBy.AgentID,
		ActorActorID: createdBy.ActorID,
		InviteID:     inviteID,
		Metadata:     metadata,
	}); err != nil {
		_ = tx.Rollback()
		return Invite{}, "", err
	}

	if err := tx.Commit(); err != nil {
		_ = tx.Rollback()
		return Invite{}, "", fmt.Errorf("commit create invite transaction: %w", err)
	}

	return Invite{
		ID:               inviteID,
		Kind:             string(kind),
		CreatedByAgentID: createdBy.AgentID,
		CreatedByActorID: createdBy.ActorID,
		CreatedAt:        nowText,
		ExpiresAt:        expiresAtText,
	}, token, nil
}

func (s *Store) ListInvites(ctx context.Context) ([]Invite, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("auth store database is not initialized")
	}

	rows, err := s.db.QueryContext(
		ctx,
		`SELECT
			id,
			kind,
			created_by_agent_id,
			created_by_actor_id,
			note,
			created_at,
			expires_at,
			consumed_at,
			consumed_by_agent_id,
			consumed_by_actor_id,
			revoked_at,
			revoked_by_agent_id,
			revoked_by_actor_id
		 FROM auth_invites
		 ORDER BY created_at DESC, id DESC`,
	)
	if err != nil {
		return nil, fmt.Errorf("query auth invites: %w", err)
	}
	defer rows.Close()

	invites := make([]Invite, 0)
	for rows.Next() {
		invite, err := scanInvite(rows)
		if err != nil {
			return nil, err
		}
		invites = append(invites, invite)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate auth invites: %w", err)
	}

	return invites, nil
}

func (s *Store) RevokeInvite(ctx context.Context, inviteID string, revokedBy Principal) (Invite, error) {
	if s == nil || s.db == nil {
		return Invite{}, fmt.Errorf("auth store database is not initialized")
	}

	inviteID = strings.TrimSpace(inviteID)
	if inviteID == "" {
		return Invite{}, fmt.Errorf("%w: invite_id is required", ErrInvalidRequest)
	}
	revokedBy.AgentID = strings.TrimSpace(revokedBy.AgentID)
	revokedBy.ActorID = strings.TrimSpace(revokedBy.ActorID)
	if revokedBy.AgentID == "" || revokedBy.ActorID == "" {
		return Invite{}, fmt.Errorf("%w: authenticated principal is required", ErrAuthRequired)
	}

	now := time.Now().UTC()
	nowText := now.Format(time.RFC3339Nano)

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Invite{}, fmt.Errorf("begin revoke invite transaction: %w", err)
	}

	result, err := tx.ExecContext(
		ctx,
		`UPDATE auth_invites
		 SET revoked_at = ?,
		     revoked_by_agent_id = ?,
		     revoked_by_actor_id = ?
		 WHERE id = ?
		   AND revoked_at IS NULL`,
		nowText,
		revokedBy.AgentID,
		revokedBy.ActorID,
		inviteID,
	)
	if err != nil {
		_ = tx.Rollback()
		return Invite{}, fmt.Errorf("revoke auth invite: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		_ = tx.Rollback()
		return Invite{}, fmt.Errorf("read revoke invite rows affected: %w", err)
	}

	invite, err := getInviteByIDWithQuerier(ctx, tx, inviteID)
	if err != nil {
		_ = tx.Rollback()
		if errors.Is(err, ErrInviteNotFound) {
			return Invite{}, ErrInviteNotFound
		}
		return Invite{}, err
	}

	if rowsAffected == 0 {
		_ = tx.Rollback()
		return invite, nil
	}

	if err := s.recordAuthAuditEventTx(ctx, tx, AuthAuditEventInput{
		EventType:    AuthAuditEventInviteRevoked,
		OccurredAt:   now,
		ActorAgentID: revokedBy.AgentID,
		ActorActorID: revokedBy.ActorID,
		InviteID:     inviteID,
		Metadata: map[string]any{
			"kind": invite.Kind,
		},
	}); err != nil {
		_ = tx.Rollback()
		return Invite{}, err
	}

	if err := tx.Commit(); err != nil {
		_ = tx.Rollback()
		return Invite{}, fmt.Errorf("commit revoke invite transaction: %w", err)
	}

	return invite, nil
}

func (s *Store) getInviteByID(ctx context.Context, inviteID string) (Invite, error) {
	return getInviteByIDWithQuerier(ctx, s.db, inviteID)
}

type inviteQueryer interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func getInviteByIDWithQuerier(ctx context.Context, queryer inviteQueryer, inviteID string) (Invite, error) {
	inviteID = strings.TrimSpace(inviteID)
	if inviteID == "" {
		return Invite{}, ErrInviteNotFound
	}

	row := queryer.QueryRowContext(
		ctx,
		`SELECT
			id,
			kind,
			created_by_agent_id,
			created_by_actor_id,
			note,
			created_at,
			expires_at,
			consumed_at,
			consumed_by_agent_id,
			consumed_by_actor_id,
			revoked_at,
			revoked_by_agent_id,
			revoked_by_actor_id
		 FROM auth_invites
		 WHERE id = ?`,
		inviteID,
	)
	invite, err := scanInvite(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Invite{}, ErrInviteNotFound
		}
		return Invite{}, err
	}
	return invite, nil
}

func (s *Store) resolveInviteClaim(ctx context.Context, inviteToken string, principalKind PrincipalKind) (OnboardingClaim, error) {
	var (
		inviteID   string
		kind       string
		expiresAt  sql.NullString
		consumedAt sql.NullString
		revokedAt  sql.NullString
	)
	err := s.db.QueryRowContext(
		ctx,
		`SELECT id, kind, expires_at, consumed_at, revoked_at
		 FROM auth_invites
		 WHERE token_hash = ?`,
		hashToken(inviteToken),
	).Scan(&inviteID, &kind, &expiresAt, &consumedAt, &revokedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return OnboardingClaim{}, ErrInvalidToken
		}
		return OnboardingClaim{}, fmt.Errorf("query auth invite: %w", err)
	}

	if revokedAt.Valid || consumedAt.Valid {
		return OnboardingClaim{}, ErrInvalidToken
	}
	if expiresAt.Valid {
		expiry, err := time.Parse(time.RFC3339Nano, expiresAt.String)
		if err != nil {
			return OnboardingClaim{}, fmt.Errorf("parse invite expiry: %w", err)
		}
		if !expiry.After(time.Now().UTC()) {
			return OnboardingClaim{}, ErrInvalidToken
		}
	}

	if PrincipalKind(kind) != PrincipalKindAny && PrincipalKind(kind) != principalKind {
		return OnboardingClaim{}, ErrInviteKindMismatch
	}

	return OnboardingClaim{
		Mode:          OnboardingModeInvite,
		PrincipalKind: principalKind,
		TokenHash:     hashToken(inviteToken),
		InviteID:      inviteID,
		InviteKind:    kind,
	}, nil
}

func (s *Store) consumeOnboardingClaimTx(ctx context.Context, tx *sql.Tx, claim OnboardingClaim, agentID string, actorID string, now time.Time) error {
	switch claim.Mode {
	case OnboardingModeBootstrap:
		if strings.TrimSpace(s.bootstrapTokenHash) == "" || claim.TokenHash != s.bootstrapTokenHash {
			return ErrInvalidToken
		}
		_, err := tx.ExecContext(
			ctx,
			`INSERT INTO auth_bootstrap_state(
				id,
				consumed_token_hash,
				consumed_at,
				consumed_by_agent_id,
				consumed_by_actor_id
			) VALUES (1, ?, ?, ?, ?)`,
			claim.TokenHash,
			now.Format(time.RFC3339Nano),
			agentID,
			actorID,
		)
		if err != nil {
			if strings.Contains(strings.ToLower(err.Error()), "unique") {
				return ErrInvalidToken
			}
			return fmt.Errorf("consume bootstrap token: %w", err)
		}
		if err := s.recordAuthAuditEventTx(ctx, tx, AuthAuditEventInput{
			EventType:      AuthAuditEventBootstrapConsumed,
			OccurredAt:     now,
			ActorAgentID:   agentID,
			ActorActorID:   actorID,
			SubjectAgentID: agentID,
			SubjectActorID: actorID,
			Metadata: map[string]any{
				"principal_kind": string(claim.PrincipalKind),
			},
		}); err != nil {
			return err
		}
		return nil
	case OnboardingModeInvite:
		result, err := tx.ExecContext(
			ctx,
			`UPDATE auth_invites
			 SET consumed_at = ?,
			     consumed_by_agent_id = ?,
			     consumed_by_actor_id = ?
			 WHERE id = ?
			   AND token_hash = ?
			   AND consumed_at IS NULL
			   AND revoked_at IS NULL
			   AND (expires_at IS NULL OR expires_at > ?)
			   AND (kind = ? OR kind = ?)`,
			now.Format(time.RFC3339Nano),
			agentID,
			actorID,
			claim.InviteID,
			claim.TokenHash,
			now.Format(time.RFC3339Nano),
			string(claim.PrincipalKind),
			string(PrincipalKindAny),
		)
		if err != nil {
			return fmt.Errorf("consume invite token: %w", err)
		}
		rowsAffected, err := result.RowsAffected()
		if err != nil {
			return fmt.Errorf("read invite consume rows affected: %w", err)
		}
		if rowsAffected == 0 {
			return ErrInvalidToken
		}
		metadata := map[string]any{
			"principal_kind": string(claim.PrincipalKind),
		}
		if strings.TrimSpace(claim.InviteKind) != "" {
			metadata["invite_kind"] = claim.InviteKind
		}
		if err := s.recordAuthAuditEventTx(ctx, tx, AuthAuditEventInput{
			EventType:      AuthAuditEventInviteConsumed,
			OccurredAt:     now,
			ActorAgentID:   agentID,
			ActorActorID:   actorID,
			SubjectAgentID: agentID,
			SubjectActorID: actorID,
			InviteID:       claim.InviteID,
			Metadata:       metadata,
		}); err != nil {
			return err
		}
		return nil
	default:
		return fmt.Errorf("%w: unsupported onboarding mode", ErrInvalidRequest)
	}
}

type inviteScanner interface {
	Scan(dest ...any) error
}

func scanInvite(scanner inviteScanner) (Invite, error) {
	var (
		invite            Invite
		ignoredNote       string
		expiresAt         sql.NullString
		consumedAt        sql.NullString
		consumedByAgentID sql.NullString
		consumedByActorID sql.NullString
		revokedAt         sql.NullString
		revokedByAgentID  sql.NullString
		revokedByActorID  sql.NullString
	)
	if err := scanner.Scan(
		&invite.ID,
		&invite.Kind,
		&invite.CreatedByAgentID,
		&invite.CreatedByActorID,
		&ignoredNote,
		&invite.CreatedAt,
		&expiresAt,
		&consumedAt,
		&consumedByAgentID,
		&consumedByActorID,
		&revokedAt,
		&revokedByAgentID,
		&revokedByActorID,
	); err != nil {
		return Invite{}, err
	}
	if expiresAt.Valid {
		invite.ExpiresAt = &expiresAt.String
	}
	if consumedAt.Valid {
		invite.ConsumedAt = &consumedAt.String
	}
	if consumedByAgentID.Valid {
		invite.ConsumedByAgentID = &consumedByAgentID.String
	}
	if consumedByActorID.Valid {
		invite.ConsumedByActorID = &consumedByActorID.String
	}
	if revokedAt.Valid {
		invite.RevokedAt = &revokedAt.String
	}
	if revokedByAgentID.Valid {
		invite.RevokedByAgentID = &revokedByAgentID.String
	}
	if revokedByActorID.Valid {
		invite.RevokedByActorID = &revokedByActorID.String
	}
	return invite, nil
}
