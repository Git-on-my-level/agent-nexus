package controlplane

import (
	"context"
	"database/sql"
	"encoding/json"
)

func (s *Service) ListAuditEvents(ctx context.Context, identity RequestIdentity, filter AuditFilter) (Page[AuditEvent], error) {
	limit := normalizePageLimit(filter.Page.Limit)
	sortAt, sortID, err := decodeCursor(filter.Page.Cursor)
	if err != nil {
		return Page[AuditEvent]{}, invalidRequest("cursor is invalid")
	}

	query := `SELECT a.id, a.event_type, a.actor_account_id, a.organization_id, a.workspace_id, a.target_type, a.target_id, a.metadata_json, a.occurred_at
		FROM audit_events a
		WHERE (
			a.actor_account_id = ?
			OR a.organization_id IN (
				SELECT organization_id FROM organization_memberships
				WHERE account_id = ? AND status = 'active'
			)
		)`
	args := []any{identity.Account.ID, identity.Account.ID}
	if filter.OrganizationID != "" {
		if _, _, err := s.requireOrganizationAccess(ctx, identity, filter.OrganizationID, true); err != nil {
			return Page[AuditEvent]{}, err
		}
		query += ` AND a.organization_id = ?`
		args = append(args, filter.OrganizationID)
	}
	if filter.WorkspaceID != "" {
		if _, _, err := s.requireWorkspaceAccess(ctx, identity, filter.WorkspaceID, true); err != nil {
			return Page[AuditEvent]{}, err
		}
		query += ` AND a.workspace_id = ?`
		args = append(args, filter.WorkspaceID)
	}
	if filter.AccountID != "" {
		query += ` AND a.actor_account_id = ?`
		args = append(args, filter.AccountID)
	}
	if sortAt != "" {
		query += ` AND (a.occurred_at > ? OR (a.occurred_at = ? AND a.id > ?))`
		args = append(args, sortAt, sortAt, sortID)
	}
	query += ` ORDER BY a.occurred_at ASC, a.id ASC LIMIT ?`
	args = append(args, limit+1)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return Page[AuditEvent]{}, internalError("failed to list audit events")
	}
	defer rows.Close()

	var events []AuditEvent
	for rows.Next() {
		event, err := scanAuditEvent(rows)
		if err != nil {
			return Page[AuditEvent]{}, err
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return Page[AuditEvent]{}, internalError("failed to iterate audit events")
	}
	return pageFromItems(events, limit, func(event AuditEvent) (string, string) {
		return event.OccurredAt, event.ID
	}), nil
}

type auditScanner interface {
	Scan(dest ...any) error
}

func scanAuditEvent(scanner auditScanner) (AuditEvent, error) {
	var (
		event            AuditEvent
		actorAccountID   sql.NullString
		organizationID   sql.NullString
		workspaceID      sql.NullString
		metadataJSONText string
	)
	if err := scanner.Scan(&event.ID, &event.EventType, &actorAccountID, &organizationID, &workspaceID, &event.TargetType, &event.TargetID, &metadataJSONText, &event.OccurredAt); err != nil {
		return AuditEvent{}, internalError("failed to scan audit event")
	}
	event.ActorAccountID = nullableString(actorAccountID)
	event.OrganizationID = nullableString(organizationID)
	event.WorkspaceID = nullableString(workspaceID)
	if metadataJSONText != "" {
		event.Metadata = map[string]any{}
		if err := json.Unmarshal([]byte(metadataJSONText), &event.Metadata); err != nil {
			return AuditEvent{}, internalError("failed to decode audit metadata")
		}
	}
	return event, nil
}
