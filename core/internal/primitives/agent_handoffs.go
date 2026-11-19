package primitives

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

const agentHandoffPrefix = "handoff-"

func IsAgentHandoffID(id string) bool {
	if !strings.HasPrefix(id, agentHandoffPrefix) || len(id) != len(agentHandoffPrefix)+sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(strings.TrimPrefix(id, agentHandoffPrefix))
	return err == nil
}

type AgentHandoff struct {
	ID               string
	SenderActorID    string
	RecipientActorID string
	WorkspaceID      string
	Refs             []string
	ExpiresAt        string
	QueuedAt         string
	DeliveryStatus   string
	DeliveredAt      string
	AcknowledgedAt   string
	BlockedRefIndex  int // One-based. Zero means the recipient verified every ref.
}

// AgentHandoffID hashes the canonical, length-delimited request. It never uses
// a host identity, and repeats remain identifiable after the deadline.
func AgentHandoffID(workspaceID, senderID, recipientID, expiresAt string, refs []string) string {
	semantic, _ := json.Marshal([]any{workspaceID, senderID, recipientID, expiresAt, refs})
	sum := sha256.Sum256(semantic)
	return agentHandoffPrefix + hex.EncodeToString(sum[:])
}

func (s *Store) CreateAgentHandoff(ctx context.Context, handoff AgentHandoff, targetHandle string) (AgentHandoff, error) {
	refs, err := json.Marshal(handoff.Refs)
	if err != nil {
		return AgentHandoff{}, err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	// The existing wake and delivery triggers publish the row and its queue
	// position atomically. A handoff has no source thread or human inbox event.
	_, err = s.db.ExecContext(ctx, `INSERT INTO agent_wakeups
		(wakeup_id, status, target_actor_id, target_handle, workspace_id, refs_json,
		 created_at, updated_at, handoff_sender_actor_id, handoff_expires_at)
		VALUES (?, 'requested', ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(wakeup_id) DO NOTHING`, handoff.ID, handoff.RecipientActorID,
		targetHandle, handoff.WorkspaceID, string(refs), now, now, handoff.SenderActorID, handoff.ExpiresAt)
	if err != nil {
		return AgentHandoff{}, err
	}
	return s.GetAgentHandoff(ctx, handoff.WorkspaceID, handoff.SenderActorID, handoff.ID)
}

func (s *Store) GetAgentHandoff(ctx context.Context, workspaceID, actorID, id string) (AgentHandoff, error) {
	var handoff AgentHandoff
	var refs string
	err := s.db.QueryRowContext(ctx, `SELECT wakeup_id, workspace_id, handoff_sender_actor_id, target_actor_id,
		refs_json, handoff_expires_at, created_at, status, COALESCE(completed_at, ''),
		handoff_acknowledged_at, handoff_blocked_ref_index FROM agent_wakeups
		WHERE wakeup_id=? AND workspace_id=? AND handoff_sender_actor_id<>''
		AND (handoff_sender_actor_id=? OR target_actor_id=?)`,
		id, workspaceID, actorID, actorID).Scan(&handoff.ID, &handoff.WorkspaceID,
		&handoff.SenderActorID, &handoff.RecipientActorID, &refs, &handoff.ExpiresAt, &handoff.QueuedAt,
		&handoff.DeliveryStatus, &handoff.DeliveredAt, &handoff.AcknowledgedAt, &handoff.BlockedRefIndex)
	if errors.Is(err, sql.ErrNoRows) {
		return AgentHandoff{}, ErrNotFound
	}
	if err != nil {
		return AgentHandoff{}, err
	}
	handoff.Refs, err = decodeStoredJSONList(refs, "handoff.refs")
	return handoff, err
}

func (s *Store) AcknowledgeAgentHandoff(ctx context.Context, handoff AgentHandoff, actorID string, blockedRefIndex int) (AgentHandoff, error) {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := s.db.ExecContext(ctx, `UPDATE agent_wakeups
		SET handoff_acknowledged_at=?, handoff_blocked_ref_index=?, updated_at=?
		WHERE wakeup_id=? AND workspace_id=? AND target_actor_id=? AND handoff_sender_actor_id<>''
		  AND status IN ('claimed','completed') AND handoff_acknowledged_at=''
		  AND unixepoch(handoff_expires_at)>unixepoch('now')`, now, blockedRefIndex, now,
		handoff.ID, handoff.WorkspaceID, actorID)
	if err != nil {
		return AgentHandoff{}, err
	}
	recorded, err := s.GetAgentHandoff(ctx, handoff.WorkspaceID, actorID, handoff.ID)
	if err != nil {
		return AgentHandoff{}, err
	}
	if recorded.RecipientActorID != actorID {
		return AgentHandoff{}, ErrNotFound
	}
	if recorded.AcknowledgedAt == "" || recorded.BlockedRefIndex != blockedRefIndex {
		return AgentHandoff{}, ErrConflict
	}
	return recorded, nil
}
