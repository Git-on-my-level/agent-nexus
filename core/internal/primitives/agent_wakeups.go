package primitives

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	AgentWakeupStatusRequested = "requested"
	AgentWakeupStatusClaimed   = "claimed"
	AgentWakeupStatusCompleted = "completed"
	AgentWakeupStatusFailed    = "failed"

	AgentWakeupNotificationUnread    = "unread"
	AgentWakeupNotificationRead      = "read"
	AgentWakeupNotificationDismissed = "dismissed"
)

type AgentWakeup struct {
	WakeupID           string
	Status             string
	NotificationStatus string
	TargetHandle       string
	TargetActorID      string
	WorkspaceID        string
	WorkspaceName      string
	ThreadID           string
	ThreadTitle        string
	TriggerEventID     string
	TriggerCreatedAt   string
	TriggerText        string
	Refs               []string
	BridgeInstanceID   string
	FailureReason      string
	CreatedAt          string
	ClaimedAt          string
	CompletedAt        string
	FailedAt           string
	ReadAt             string
	DismissedAt        string
	UpdatedAt          string
}

type AgentWakeupListFilter struct {
	TargetActorID        string
	ThreadID             string
	Statuses             []string
	NotificationStatuses []string
	Order                string
}

func (s *Store) UpsertAgentWakeup(ctx context.Context, wakeup AgentWakeup) (AgentWakeup, error) {
	if s == nil || s.db == nil {
		return AgentWakeup{}, fmt.Errorf("primitives store database is not initialized")
	}
	wakeup = normalizeAgentWakeup(wakeup)
	if wakeup.WakeupID == "" || wakeup.TargetActorID == "" {
		return AgentWakeup{}, fmt.Errorf("invalid agent wakeup")
	}
	refsJSON, err := json.Marshal(wakeup.Refs)
	if err != nil {
		return AgentWakeup{}, fmt.Errorf("encode wake refs: %w", err)
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO agent_wakeups (
			wakeup_id, status, notification_status, target_handle, target_actor_id,
			workspace_id, workspace_name, thread_id, thread_title, trigger_event_id,
			trigger_created_at, trigger_text, refs_json,
			bridge_instance_id, failure_reason, created_at, claimed_at, completed_at,
			failed_at, read_at, dismissed_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(wakeup_id) DO NOTHING`,
		wakeup.WakeupID, wakeup.Status, wakeup.NotificationStatus, wakeup.TargetHandle, wakeup.TargetActorID,
		wakeup.WorkspaceID, wakeup.WorkspaceName, wakeup.ThreadID, wakeup.ThreadTitle, wakeup.TriggerEventID,
		wakeup.TriggerCreatedAt, wakeup.TriggerText, string(refsJSON),
		wakeup.BridgeInstanceID, wakeup.FailureReason, wakeup.CreatedAt, nullEmpty(wakeup.ClaimedAt), nullEmpty(wakeup.CompletedAt),
		nullEmpty(wakeup.FailedAt), nullEmpty(wakeup.ReadAt), nullEmpty(wakeup.DismissedAt), wakeup.UpdatedAt,
	)
	if err != nil {
		return AgentWakeup{}, fmt.Errorf("insert agent wakeup: %w", err)
	}
	return s.GetAgentWakeup(ctx, wakeup.WakeupID)
}

func (s *Store) GetAgentWakeup(ctx context.Context, wakeupID string) (AgentWakeup, error) {
	wakeupID = strings.TrimSpace(wakeupID)
	if wakeupID == "" {
		return AgentWakeup{}, ErrNotFound
	}
	row := s.db.QueryRowContext(ctx, `SELECT wakeup_id, status, notification_status, target_handle, target_actor_id,
		workspace_id, workspace_name, thread_id, thread_title, trigger_event_id, trigger_created_at,
		trigger_text, refs_json, bridge_instance_id,
		failure_reason, created_at, claimed_at, completed_at, failed_at, read_at, dismissed_at, updated_at
		FROM agent_wakeups WHERE wakeup_id = ?`, wakeupID)
	return scanAgentWakeup(row)
}

func (s *Store) ListAgentWakeups(ctx context.Context, filter AgentWakeupListFilter) ([]AgentWakeup, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("primitives store database is not initialized")
	}
	clauses := []string{"1 = 1"}
	args := []any{}
	if actorID := strings.TrimSpace(filter.TargetActorID); actorID != "" {
		clauses = append(clauses, "target_actor_id = ?")
		args = append(args, actorID)
	}
	if threadID := strings.TrimSpace(filter.ThreadID); threadID != "" {
		clauses = append(clauses, "thread_id = ?")
		args = append(args, threadID)
	}
	if len(filter.Statuses) > 0 {
		placeholders := strings.TrimSuffix(strings.Repeat("?,", len(filter.Statuses)), ",")
		clauses = append(clauses, "status IN ("+placeholders+")")
		for _, status := range filter.Statuses {
			args = append(args, strings.TrimSpace(status))
		}
	}
	if len(filter.NotificationStatuses) > 0 {
		placeholders := strings.TrimSuffix(strings.Repeat("?,", len(filter.NotificationStatuses)), ",")
		clauses = append(clauses, "notification_status IN ("+placeholders+")")
		for _, status := range filter.NotificationStatuses {
			args = append(args, strings.TrimSpace(status))
		}
	}
	order := "DESC"
	if strings.EqualFold(strings.TrimSpace(filter.Order), "asc") {
		order = "ASC"
	}
	rows, err := s.db.QueryContext(ctx, `SELECT wakeup_id, status, notification_status, target_handle, target_actor_id,
		workspace_id, workspace_name, thread_id, thread_title, trigger_event_id, trigger_created_at,
		trigger_text, refs_json, bridge_instance_id,
		failure_reason, created_at, claimed_at, completed_at, failed_at, read_at, dismissed_at, updated_at
		FROM agent_wakeups WHERE `+strings.Join(clauses, " AND ")+` ORDER BY created_at `+order+`, wakeup_id `+order, args...)
	if err != nil {
		return nil, fmt.Errorf("list agent wakeups: %w", err)
	}
	defer rows.Close()
	out := []AgentWakeup{}
	for rows.Next() {
		wakeup, err := scanAgentWakeup(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, wakeup)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate agent wakeups: %w", err)
	}
	return out, nil
}

func (s *Store) ClaimAgentWakeup(ctx context.Context, wakeupID string, targetActorID string, bridgeInstanceID string) (AgentWakeup, error) {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	result, err := s.db.ExecContext(ctx, `UPDATE agent_wakeups
		SET status = ?, bridge_instance_id = ?, claimed_at = COALESCE(claimed_at, ?), updated_at = ?
		WHERE wakeup_id = ? AND target_actor_id = ? AND (status = ? OR (status = ? AND bridge_instance_id = ?))`,
		AgentWakeupStatusClaimed, strings.TrimSpace(bridgeInstanceID), now, now,
		strings.TrimSpace(wakeupID), strings.TrimSpace(targetActorID),
		AgentWakeupStatusRequested, AgentWakeupStatusClaimed, strings.TrimSpace(bridgeInstanceID),
	)
	if err != nil {
		return AgentWakeup{}, fmt.Errorf("claim agent wakeup: %w", err)
	}
	if rows, _ := result.RowsAffected(); rows == 0 {
		return AgentWakeup{}, ErrConflict
	}
	return s.GetAgentWakeup(ctx, wakeupID)
}

func (s *Store) CompleteAgentWakeup(ctx context.Context, wakeupID string, targetActorID string, bridgeInstanceID string) (AgentWakeup, error) {
	return s.setAgentWakeupStatus(ctx, wakeupID, targetActorID, bridgeInstanceID, AgentWakeupStatusCompleted, "")
}

func (s *Store) FailAgentWakeup(ctx context.Context, wakeupID string, targetActorID string, bridgeInstanceID string, reason string) (AgentWakeup, error) {
	return s.setAgentWakeupStatus(ctx, wakeupID, targetActorID, bridgeInstanceID, AgentWakeupStatusFailed, reason)
}

func (s *Store) MarkAgentWakeupNotification(ctx context.Context, wakeupID string, targetActorID string, status string) (AgentWakeup, error) {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	status = strings.TrimSpace(status)
	readAt := any(nil)
	dismissedAt := any(nil)
	if status == AgentWakeupNotificationRead {
		readAt = now
	}
	if status == AgentWakeupNotificationDismissed {
		dismissedAt = now
	}
	result, err := s.db.ExecContext(ctx, `UPDATE agent_wakeups
		SET notification_status = ?,
		    read_at = COALESCE(read_at, ?),
		    dismissed_at = COALESCE(dismissed_at, ?),
		    updated_at = ?
		WHERE wakeup_id = ? AND target_actor_id = ?`,
		status, readAt, dismissedAt, now, strings.TrimSpace(wakeupID), strings.TrimSpace(targetActorID),
	)
	if err != nil {
		return AgentWakeup{}, fmt.Errorf("mark agent wakeup notification: %w", err)
	}
	if rows, _ := result.RowsAffected(); rows == 0 {
		return AgentWakeup{}, ErrNotFound
	}
	return s.GetAgentWakeup(ctx, wakeupID)
}

func (s *Store) setAgentWakeupStatus(ctx context.Context, wakeupID string, targetActorID string, bridgeInstanceID string, status string, reason string) (AgentWakeup, error) {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	completedAt := any(nil)
	failedAt := any(nil)
	if status == AgentWakeupStatusCompleted {
		completedAt = now
	}
	if status == AgentWakeupStatusFailed {
		failedAt = now
	}
	result, err := s.db.ExecContext(ctx, `UPDATE agent_wakeups
		SET status = ?, failure_reason = ?, completed_at = COALESCE(completed_at, ?),
		    failed_at = COALESCE(failed_at, ?), updated_at = ?
		WHERE wakeup_id = ? AND target_actor_id = ? AND (bridge_instance_id = ? OR bridge_instance_id = '')`,
		status, strings.TrimSpace(reason), completedAt, failedAt, now,
		strings.TrimSpace(wakeupID), strings.TrimSpace(targetActorID), strings.TrimSpace(bridgeInstanceID),
	)
	if err != nil {
		return AgentWakeup{}, fmt.Errorf("update agent wakeup status: %w", err)
	}
	if rows, _ := result.RowsAffected(); rows == 0 {
		return AgentWakeup{}, ErrConflict
	}
	return s.GetAgentWakeup(ctx, wakeupID)
}

type agentWakeupScanner interface {
	Scan(dest ...any) error
}

func scanAgentWakeup(row agentWakeupScanner) (AgentWakeup, error) {
	var wakeup AgentWakeup
	var refsJSON string
	var claimedAt, completedAt, failedAt, readAt, dismissedAt sql.NullString
	err := row.Scan(
		&wakeup.WakeupID, &wakeup.Status, &wakeup.NotificationStatus, &wakeup.TargetHandle, &wakeup.TargetActorID,
		&wakeup.WorkspaceID, &wakeup.WorkspaceName, &wakeup.ThreadID, &wakeup.ThreadTitle, &wakeup.TriggerEventID,
		&wakeup.TriggerCreatedAt, &wakeup.TriggerText, &refsJSON,
		&wakeup.BridgeInstanceID, &wakeup.FailureReason, &wakeup.CreatedAt, &claimedAt, &completedAt, &failedAt,
		&readAt, &dismissedAt, &wakeup.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return AgentWakeup{}, ErrNotFound
	}
	if err != nil {
		return AgentWakeup{}, fmt.Errorf("scan agent wakeup: %w", err)
	}
	refs, err := decodeStoredJSONList(refsJSON, "agent_wakeup.refs")
	if err != nil {
		return AgentWakeup{}, err
	}
	wakeup.Refs = refs
	wakeup.ClaimedAt = claimedAt.String
	wakeup.CompletedAt = completedAt.String
	wakeup.FailedAt = failedAt.String
	wakeup.ReadAt = readAt.String
	wakeup.DismissedAt = dismissedAt.String
	return wakeup, nil
}

func normalizeAgentWakeup(wakeup AgentWakeup) AgentWakeup {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	wakeup.WakeupID = strings.TrimSpace(wakeup.WakeupID)
	wakeup.Status = strings.TrimSpace(wakeup.Status)
	if wakeup.Status == "" {
		wakeup.Status = AgentWakeupStatusRequested
	}
	wakeup.NotificationStatus = strings.TrimSpace(wakeup.NotificationStatus)
	if wakeup.NotificationStatus == "" {
		wakeup.NotificationStatus = AgentWakeupNotificationUnread
	}
	wakeup.TargetHandle = strings.TrimSpace(wakeup.TargetHandle)
	wakeup.TargetActorID = strings.TrimSpace(wakeup.TargetActorID)
	wakeup.WorkspaceID = strings.TrimSpace(wakeup.WorkspaceID)
	wakeup.WorkspaceName = strings.TrimSpace(wakeup.WorkspaceName)
	wakeup.ThreadID = strings.TrimSpace(wakeup.ThreadID)
	wakeup.ThreadTitle = strings.TrimSpace(wakeup.ThreadTitle)
	wakeup.TriggerEventID = strings.TrimSpace(wakeup.TriggerEventID)
	wakeup.TriggerCreatedAt = strings.TrimSpace(wakeup.TriggerCreatedAt)
	wakeup.TriggerText = strings.TrimSpace(wakeup.TriggerText)
	wakeup.BridgeInstanceID = strings.TrimSpace(wakeup.BridgeInstanceID)
	wakeup.FailureReason = strings.TrimSpace(wakeup.FailureReason)
	if wakeup.CreatedAt == "" {
		wakeup.CreatedAt = now
	}
	if wakeup.UpdatedAt == "" {
		wakeup.UpdatedAt = now
	}
	return wakeup
}

func nullEmpty(value string) any {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return value
}
