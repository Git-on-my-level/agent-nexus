package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
)

const (
	AuthMethodPublicKey    = "public_key"
	AuthMethodPasskey      = "passkey"
	AuthMethodControlPlane = "control_plane"

	controlPlaneShadowOnboardingMode = "control_plane_shadow"
)

var nonUsernameCharsPattern = regexp.MustCompile(`[^a-z0-9._-]+`)

type EnsureControlPlanePrincipalInput struct {
	Issuer         string
	Subject        string
	WorkspaceID    string
	OrganizationID string
	Email          string
	DisplayName    string
	LaunchID       string
}

func principalKindExpr(agentAlias string) string {
	return fmt.Sprintf(`COALESCE(
		NULLIF(json_extract(%s.metadata_json, '$.principal_kind'), ''),
		CASE
			WHEN EXISTS(SELECT 1 FROM passkey_credentials pc WHERE pc.agent_id = %s.id LIMIT 1) THEN 'human'
			ELSE 'agent'
		END
	)`, agentAlias, agentAlias)
}

func authMethodExpr(agentAlias string) string {
	return fmt.Sprintf(`COALESCE(
		NULLIF(json_extract(%s.metadata_json, '$.auth_method'), ''),
		CASE
			WHEN EXISTS(SELECT 1 FROM passkey_credentials pc WHERE pc.agent_id = %s.id LIMIT 1) THEN 'passkey'
			ELSE 'public_key'
		END
	)`, agentAlias, agentAlias)
}

func principalMetadataJSON(kind PrincipalKind, authMethod string, extra map[string]any) (string, error) {
	payload := map[string]any{
		"principal_kind": string(kind),
		"auth_method":    strings.TrimSpace(authMethod),
	}
	for key, value := range extra {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		payload[key] = value
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

func actorMetadataJSON(kind PrincipalKind, authMethod string, extra map[string]any) (string, error) {
	payload := map[string]any{
		"principal_kind": string(kind),
		"auth_method":    strings.TrimSpace(authMethod),
	}
	for key, value := range extra {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		payload[key] = value
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

func controlPlanePrincipalIDs(issuer string, subject string, email string, displayName string) (string, string, string) {
	hash := sha256.Sum256([]byte(strings.TrimSpace(issuer) + "\n" + strings.TrimSpace(subject)))
	suffix := hex.EncodeToString(hash[:])[:20]
	base := sanitizeUsernameComponent(email)
	if base == "" {
		base = sanitizeUsernameComponent(displayName)
	}
	if base == "" {
		base = "human"
	}
	if len(base) > 24 {
		base = base[:24]
	}
	username := "cp." + base + "." + suffix
	if len(username) > 64 {
		username = username[:64]
	}
	agentID := "agent_cp_" + suffix
	return agentID, agentID, username
}

func sanitizeUsernameComponent(raw string) string {
	raw = strings.ToLower(strings.TrimSpace(raw))
	if raw == "" {
		return ""
	}
	if parts := strings.SplitN(raw, "@", 2); len(parts) > 0 {
		raw = parts[0]
	}
	raw = nonUsernameCharsPattern.ReplaceAllString(raw, ".")
	raw = strings.Trim(raw, "._-")
	for strings.Contains(raw, "..") {
		raw = strings.ReplaceAll(raw, "..", ".")
	}
	return raw
}

func controlPlaneActorDisplayName(displayName string, email string, username string) string {
	switch {
	case strings.TrimSpace(displayName) != "":
		return strings.TrimSpace(displayName)
	case strings.TrimSpace(email) != "":
		return strings.TrimSpace(email)
	default:
		return username
	}
}

func (s *Store) EnsureControlPlanePrincipal(ctx context.Context, input EnsureControlPlanePrincipalInput) (Principal, error) {
	if s == nil || s.db == nil {
		return Principal{}, fmt.Errorf("auth store database is not initialized")
	}

	issuer := strings.TrimSpace(input.Issuer)
	subject := strings.TrimSpace(input.Subject)
	workspaceID := strings.TrimSpace(input.WorkspaceID)
	if issuer == "" || subject == "" || workspaceID == "" {
		return Principal{}, fmt.Errorf("%w: issuer, subject, and workspace_id are required", ErrInvalidRequest)
	}

	agentID, actorID, username := controlPlanePrincipalIDs(issuer, subject, input.Email, input.DisplayName)
	now := time.Now().UTC()
	nowText := now.Format(time.RFC3339Nano)
	displayName := controlPlaneActorDisplayName(input.DisplayName, input.Email, username)
	tagsJSON := `["human","control-plane"]`
	metadataExtra := map[string]any{
		"control_plane_issuer":          issuer,
		"control_plane_subject":         subject,
		"control_plane_workspace_id":    workspaceID,
		"control_plane_organization_id": strings.TrimSpace(input.OrganizationID),
		"control_plane_email":           strings.TrimSpace(input.Email),
		"control_plane_display_name":    strings.TrimSpace(input.DisplayName),
		"control_plane_launch_id":       strings.TrimSpace(input.LaunchID),
		"control_plane_shadow":          true,
	}
	agentMetadataJSON, err := principalMetadataJSON(PrincipalKindHuman, AuthMethodControlPlane, metadataExtra)
	if err != nil {
		return Principal{}, fmt.Errorf("encode control-plane principal metadata: %w", err)
	}
	actorMetadataValue, err := actorMetadataJSON(PrincipalKindHuman, AuthMethodControlPlane, metadataExtra)
	if err != nil {
		return Principal{}, fmt.Errorf("encode control-plane actor metadata: %w", err)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Principal{}, fmt.Errorf("begin control-plane principal transaction: %w", err)
	}
	defer tx.Rollback()

	insertResult, err := tx.ExecContext(
		ctx,
		`INSERT OR IGNORE INTO agents(id, username, actor_id, created_at, updated_at, revoked_at, metadata_json)
		 VALUES (?, ?, ?, ?, ?, NULL, ?)`,
		agentID,
		username,
		actorID,
		nowText,
		nowText,
		agentMetadataJSON,
	)
	if err != nil {
		return Principal{}, fmt.Errorf("insert control-plane principal: %w", err)
	}

	inserted := false
	if rowsAffected, rowsErr := insertResult.RowsAffected(); rowsErr == nil && rowsAffected > 0 {
		inserted = true
	}
	if !inserted {
		if _, err := tx.ExecContext(
			ctx,
			`UPDATE agents
			 SET updated_at = ?, metadata_json = ?
			 WHERE id = ?`,
			nowText,
			agentMetadataJSON,
			agentID,
		); err != nil {
			return Principal{}, fmt.Errorf("update control-plane principal metadata: %w", err)
		}
	}

	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO actors(id, display_name, tags_json, created_at, metadata_json)
		 VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET display_name = excluded.display_name, tags_json = excluded.tags_json, metadata_json = excluded.metadata_json`,
		actorID,
		displayName,
		tagsJSON,
		nowText,
		actorMetadataValue,
	); err != nil {
		return Principal{}, fmt.Errorf("upsert control-plane actor: %w", err)
	}

	if inserted {
		if err := s.recordAuthAuditEventTx(ctx, tx, AuthAuditEventInput{
			EventType:      AuthAuditEventPrincipalRegistered,
			OccurredAt:     now,
			ActorAgentID:   agentID,
			ActorActorID:   actorID,
			SubjectAgentID: agentID,
			SubjectActorID: actorID,
			Metadata: map[string]any{
				"username":        username,
				"principal_kind":  string(PrincipalKindHuman),
				"auth_method":     AuthMethodControlPlane,
				"onboarding_mode": controlPlaneShadowOnboardingMode,
				"issuer":          issuer,
				"workspace_id":    workspaceID,
				"organization_id": strings.TrimSpace(input.OrganizationID),
				"email":           strings.TrimSpace(input.Email),
				"display_name":    strings.TrimSpace(input.DisplayName),
				"launch_id":       strings.TrimSpace(input.LaunchID),
			},
		}); err != nil {
			return Principal{}, err
		}
	}

	principalSummary, err := s.getPrincipalSummaryQueryRow(ctx, tx.QueryRowContext, agentID)
	if err != nil {
		return Principal{}, err
	}
	if err := tx.Commit(); err != nil {
		return Principal{}, fmt.Errorf("commit control-plane principal transaction: %w", err)
	}

	return Principal{
		AgentID:       principalSummary.AgentID,
		ActorID:       principalSummary.ActorID,
		Username:      principalSummary.Username,
		PrincipalKind: principalSummary.PrincipalKind,
		AuthMethod:    principalSummary.AuthMethod,
	}, nil
}
