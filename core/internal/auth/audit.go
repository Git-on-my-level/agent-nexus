package auth

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"organization-autorunner-core/internal/authaudit"

	"github.com/google/uuid"
)

var ErrInvalidCursor = errors.New("invalid_cursor")

const (
	AuthAuditEventBootstrapConsumed            = "bootstrap_consumed"
	AuthAuditEventInviteCreated                = "invite_created"
	AuthAuditEventInviteRevoked                = "invite_revoked"
	AuthAuditEventInviteConsumed               = "invite_consumed"
	AuthAuditEventPrincipalRegistered          = "principal_registered"
	AuthAuditEventPrincipalHumanLockoutRevoked = "principal_human_lockout_revoked"
	AuthAuditEventPrincipalRevoked             = "principal_revoked"
	AuthAuditEventPrincipalSelfRevoked         = "principal_self_revoked"
)

type AuthPrincipalListFilter struct {
	Limit  *int
	Cursor string
}

type AuthAuditListFilter struct {
	Limit  *int
	Cursor string
}

type AuthPrincipalSummary struct {
	AgentID       string  `json:"agent_id"`
	ActorID       string  `json:"actor_id"`
	Username      string  `json:"username"`
	PrincipalKind string  `json:"principal_kind"`
	AuthMethod    string  `json:"auth_method"`
	CreatedAt     string  `json:"created_at"`
	UpdatedAt     string  `json:"updated_at"`
	Revoked       bool    `json:"revoked"`
	RevokedAt     *string `json:"revoked_at,omitempty"`
}

type AuthAuditEvent struct {
	EventID        string         `json:"event_id"`
	EventType      string         `json:"event_type"`
	OccurredAt     string         `json:"occurred_at"`
	ActorAgentID   *string        `json:"actor_agent_id,omitempty"`
	ActorActorID   *string        `json:"actor_actor_id,omitempty"`
	SubjectAgentID *string        `json:"subject_agent_id,omitempty"`
	SubjectActorID *string        `json:"subject_actor_id,omitempty"`
	InviteID       *string        `json:"invite_id,omitempty"`
	Metadata       map[string]any `json:"metadata"`
}

type AuthAuditEventInput struct {
	EventType      string
	OccurredAt     time.Time
	ActorAgentID   string
	ActorActorID   string
	SubjectAgentID string
	SubjectActorID string
	InviteID       string
	Metadata       map[string]any
}

func (s *Store) ListPrincipals(ctx context.Context, filter AuthPrincipalListFilter) ([]AuthPrincipalSummary, string, error) {
	if s == nil || s.db == nil {
		return nil, "", fmt.Errorf("auth store database is not initialized")
	}

	offset, err := decodeAuthPrincipalCursor(strings.TrimSpace(filter.Cursor))
	if err != nil {
		return nil, "", fmt.Errorf("%w: %v", ErrInvalidCursor, err)
	}

	query := `SELECT
		a.id,
		a.actor_id,
		a.username,
		` + principalKindExpr("a") + `,
		` + authMethodExpr("a") + `,
		a.created_at,
		a.updated_at,
		a.revoked_at
	 FROM agents a
	 ORDER BY a.created_at DESC, a.id DESC`
	args := make([]any, 0, 2)

	if filter.Limit != nil && *filter.Limit > 0 {
		query += ` LIMIT ?`
		args = append(args, *filter.Limit+1)
		if offset > 0 {
			query += ` OFFSET ?`
			args = append(args, offset)
		}
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, "", fmt.Errorf("query auth principals: %w", err)
	}
	defer rows.Close()

	principals := make([]AuthPrincipalSummary, 0)
	for rows.Next() {
		var (
			item       AuthPrincipalSummary
			revokedRaw sql.NullString
		)
		if err := rows.Scan(
			&item.AgentID,
			&item.ActorID,
			&item.Username,
			&item.PrincipalKind,
			&item.AuthMethod,
			&item.CreatedAt,
			&item.UpdatedAt,
			&revokedRaw,
		); err != nil {
			return nil, "", fmt.Errorf("scan auth principal row: %w", err)
		}
		item.Revoked = revokedRaw.Valid
		if revokedRaw.Valid {
			item.RevokedAt = &revokedRaw.String
		}
		principals = append(principals, item)
	}
	if err := rows.Err(); err != nil {
		return nil, "", fmt.Errorf("iterate auth principals: %w", err)
	}

	var nextCursor string
	if filter.Limit != nil && len(principals) > *filter.Limit {
		principals = principals[:*filter.Limit]
		nextCursor = encodeAuthPrincipalCursor(offset + *filter.Limit)
	}

	return principals, nextCursor, nil
}

func (s *Store) ListAuditEvents(ctx context.Context, filter AuthAuditListFilter) ([]AuthAuditEvent, string, error) {
	if s == nil || s.db == nil {
		return nil, "", fmt.Errorf("auth store database is not initialized")
	}

	keysetCursor, err := decodeAuthAuditCursor(strings.TrimSpace(filter.Cursor))
	if err != nil {
		return nil, "", fmt.Errorf("%w: %v", ErrInvalidCursor, err)
	}

	query := `SELECT
		id,
		event_type,
		occurred_at,
		occurred_at_sort_key,
		actor_agent_id,
		actor_actor_id,
		subject_agent_id,
		subject_actor_id,
		invite_id,
		metadata_json
	 FROM auth_audit_events
	`
	args := make([]any, 0, 4)

	if keysetCursor != nil {
		query += ` WHERE (occurred_at_sort_key < ? OR (occurred_at_sort_key = ? AND id < ?))`
		args = append(args, keysetCursor.SortKey, keysetCursor.SortKey, keysetCursor.EventID)
	}

	query += ` ORDER BY occurred_at_sort_key DESC, id DESC`

	if filter.Limit != nil && *filter.Limit > 0 {
		query += ` LIMIT ?`
		args = append(args, *filter.Limit+1)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, "", fmt.Errorf("query auth audit events: %w", err)
	}
	defer rows.Close()

	type auditEventRow struct {
		event   AuthAuditEvent
		sortKey string
	}

	rowsOut := make([]auditEventRow, 0)
	for rows.Next() {
		var (
			event        AuthAuditEvent
			sortKey      string
			actorAgent   sql.NullString
			actorActor   sql.NullString
			subjectAgent sql.NullString
			subjectActor sql.NullString
			inviteID     sql.NullString
			metadataJSON string
		)
		if err := rows.Scan(
			&event.EventID,
			&event.EventType,
			&event.OccurredAt,
			&sortKey,
			&actorAgent,
			&actorActor,
			&subjectAgent,
			&subjectActor,
			&inviteID,
			&metadataJSON,
		); err != nil {
			return nil, "", fmt.Errorf("scan auth audit row: %w", err)
		}
		event.ActorAgentID = nullStringPtr(actorAgent)
		event.ActorActorID = nullStringPtr(actorActor)
		event.SubjectAgentID = nullStringPtr(subjectAgent)
		event.SubjectActorID = nullStringPtr(subjectActor)
		event.InviteID = nullStringPtr(inviteID)
		if err := json.Unmarshal([]byte(metadataJSON), &event.Metadata); err != nil {
			return nil, "", fmt.Errorf("decode auth audit metadata: %w", err)
		}
		if event.Metadata == nil {
			event.Metadata = map[string]any{}
		}
		rowsOut = append(rowsOut, auditEventRow{event: event, sortKey: sortKey})
	}
	if err := rows.Err(); err != nil {
		return nil, "", fmt.Errorf("iterate auth audit rows: %w", err)
	}

	var nextCursor string
	if filter.Limit != nil && len(rowsOut) > *filter.Limit {
		rowsOut = rowsOut[:*filter.Limit]
		last := rowsOut[len(rowsOut)-1]
		nextCursor = encodeAuthAuditCursor(last.sortKey, last.event.EventID)
	}

	events := make([]AuthAuditEvent, 0, len(rowsOut))
	for _, row := range rowsOut {
		events = append(events, row.event)
	}

	return events, nextCursor, nil
}

func (s *Store) recordAuthAuditEventTx(ctx context.Context, tx *sql.Tx, input AuthAuditEventInput) error {
	if tx == nil {
		return fmt.Errorf("auth audit transaction is required")
	}

	eventType := strings.TrimSpace(input.EventType)
	if eventType == "" {
		return fmt.Errorf("%w: auth audit event_type is required", ErrInvalidRequest)
	}

	occurredAt := input.OccurredAt.UTC()
	if occurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	}

	metadata := input.Metadata
	if metadata == nil {
		metadata = map[string]any{}
	}
	metadataJSON, err := json.Marshal(metadata)
	if err != nil {
		return fmt.Errorf("marshal auth audit metadata: %w", err)
	}

	_, err = tx.ExecContext(
		ctx,
		`INSERT INTO auth_audit_events(
			id,
			event_type,
			occurred_at,
			occurred_at_sort_key,
			actor_agent_id,
			actor_actor_id,
			subject_agent_id,
			subject_actor_id,
			invite_id,
			metadata_json
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"authevt_"+uuid.NewString(),
		eventType,
		occurredAt.Format(time.RFC3339Nano),
		authaudit.FormatOccurredAtSortKey(occurredAt),
		nullIfEmpty(input.ActorAgentID),
		nullIfEmpty(input.ActorActorID),
		nullIfEmpty(input.SubjectAgentID),
		nullIfEmpty(input.SubjectActorID),
		nullIfEmpty(input.InviteID),
		string(metadataJSON),
	)
	if err != nil {
		return fmt.Errorf("insert auth audit event: %w", err)
	}
	return nil
}

func nullStringPtr(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	return &value.String
}

func nullIfEmpty(value string) any {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return value
}
