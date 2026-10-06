package primitives

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"agent-nexus-core/internal/auth"
	"github.com/google/uuid"
)

var ErrInvalidAccessDecision = errors.New("access requests require approved or rejected")

type AccessRequest struct {
	ID              string `json:"id"`
	PrincipalID     string `json:"principal_id"`
	ActorID         string `json:"actor_id"`
	Username        string `json:"username"`
	Grant           string `json:"grant"`
	Reason          string `json:"reason"`
	Status          string `json:"status"`
	CreatedAt       string `json:"created_at"`
	DecidedAt       string `json:"decided_at,omitempty"`
	DecidedBy       string `json:"decided_by,omitempty"`
	RequestEventRef string `json:"request_event_ref"`
	InboxItemID     string `json:"inbox_item_id"`
}

const accessRequestColumns = `id,principal_id,actor_id,username,grant_name,reason,status,created_at,decided_at,decided_by,request_event_id,inbox_item_id`

func scanAccessRequest(row interface{ Scan(...any) error }) (AccessRequest, error) {
	var out AccessRequest
	var eventID string
	err := row.Scan(&out.ID, &out.PrincipalID, &out.ActorID, &out.Username, &out.Grant, &out.Reason, &out.Status, &out.CreatedAt, &out.DecidedAt, &out.DecidedBy, &eventID, &out.InboxItemID)
	if errors.Is(err, sql.ErrNoRows) {
		return out, ErrNotFound
	}
	out.RequestEventRef = "event:" + eventID
	return out, err
}

func (s *Store) GetAccessRequest(ctx context.Context, id string) (AccessRequest, error) {
	return scanAccessRequest(s.db.QueryRowContext(ctx, `SELECT `+accessRequestColumns+` FROM access_requests WHERE id=?`, id))
}

func (s *Store) ListPendingAccessRequests(ctx context.Context) ([]AccessRequest, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+accessRequestColumns+` FROM access_requests WHERE status='pending' ORDER BY created_at,id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AccessRequest{}
	for rows.Next() {
		item, err := scanAccessRequest(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) AccessSummary(ctx context.Context) (map[string]any, error) {
	var requests, enrollments int
	err := s.db.QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM access_requests WHERE status='pending'),(SELECT COUNT(*) FROM host_enrollments WHERE status IN ('pending','approved') AND expires_at>?)`, time.Now().UTC().Format(time.RFC3339Nano)).Scan(&requests, &enrollments)
	return map[string]any{"pending_count": requests + enrollments, "pending_access_request_count": requests, "pending_host_enrollment_count": enrollments}, err
}

// CreateAccessRequest commits the durable request, its shared thread, and the
// canonical attention event together. A retry cannot leave duplicate asks.
func (s *Store) CreateAccessRequest(ctx context.Context, actor auth.Principal, grant, reason string) (AccessRequest, error) {
	grant, reason = strings.TrimSpace(grant), strings.TrimSpace(reason)
	if grant != "auth-admin" || reason == "" || len([]rune(reason)) > 4000 {
		return AccessRequest{}, auth.ErrInvalidRequest
	}
	if s.quota.enabled() {
		s.quotaMu.Lock()
		defer s.quotaMu.Unlock()
		// Quota reads use the database pool, so run them before opening a write
		// transaction. Existing requests consume no additional space on replay.
		var exists int
		if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM access_requests WHERE principal_id=? AND grant_name=?`, actor.AgentID, grant).Scan(&exists); err != nil {
			return AccessRequest{}, err
		}
		if exists == 0 {
			if err := s.checkWorkspaceWriteQuota(ctx, 0, quotaWriteDelta{dbBytes: int64(2*len(reason) + len(actor.Username) + 8192)}, blobLedgerWritePlan{}); err != nil {
				return AccessRequest{}, err
			}
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return AccessRequest{}, err
	}
	defer tx.Rollback()
	if err := auth.RequireAgentTx(ctx, tx, actor); err != nil {
		return AccessRequest{}, err
	}
	existing, err := scanAccessRequest(tx.QueryRowContext(ctx, `SELECT `+accessRequestColumns+` FROM access_requests WHERE principal_id=? AND grant_name=?`, actor.AgentID, grant))
	if err == nil {
		return existing, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return AccessRequest{}, err
	}
	id, threadID, eventID := uuid.NewString(), uuid.NewString(), uuid.NewString()
	inboxID := fmt.Sprintf("inbox:review:%s:%s:%s", threadID, id, eventID)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	event := map[string]any{"id": eventID, "type": "human_attention_requested", "thread_id": threadID, "refs": []string{"thread:" + threadID}, "summary": "Access requested: " + grant, "payload": map[string]any{
		"kind": "review", "request_id": id, "title": "Grant " + grant + " to " + actor.Username, "body": reason, "subject_ref": "thread:" + threadID, "related_refs": []string{"thread:" + threadID}, "requester_actor_id": actor.ActorID, "requester_agent_id": actor.AgentID, "requester_label": actor.Username, "response_proposals": []string{"Approve " + grant, "Deny request"},
	}, "provenance": map[string]any{"sources": []string{"inferred"}}}
	prepared, err := prepareEventForInsert(actor.ActorID, event)
	if err != nil {
		return AccessRequest{}, err
	}
	handle, err := uniqueHandleTx(ctx, tx, "thread", "access-request-"+id, "access-request-"+id)
	if err != nil {
		return AccessRequest{}, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO threads(id,handle,kind,thread_id,updated_at,updated_by,body_json,provenance_json) VALUES(?,?,'thread',?,?,?,?,'{"sources":["inferred"]}')`, threadID, handle, threadID, now, actor.ActorID, `{"title":"Access request","type":"incident","status":"active"}`)
	if err != nil {
		return AccessRequest{}, err
	}
	if err := insertPreparedEvent(ctx, tx, prepared); err != nil {
		return AccessRequest{}, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO access_requests(id,principal_id,actor_id,username,grant_name,reason,created_at,request_event_id,inbox_item_id) VALUES(?,?,?,?,?,?,?,?,?)`, id, actor.AgentID, actor.ActorID, actor.Username, grant, reason, now, eventID, inboxID)
	if err != nil {
		return AccessRequest{}, err
	}
	if err := tx.Commit(); err != nil {
		return AccessRequest{}, err
	}
	return s.GetAccessRequest(ctx, id)
}

// applyAccessDecisionTx resolves by the server-owned source event, never by
// caller-supplied attention payload metadata. It participates in response commit.
func (s *Store) applyAccessDecisionTx(ctx context.Context, tx *accessTx, sourceEventID, actorID, outcome, at string) error {
	request, err := scanAccessRequest(tx.QueryRowContext(ctx, `SELECT `+accessRequestColumns+` FROM access_requests WHERE request_event_id=?`, sourceEventID))
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if outcome != "approved" && outcome != "rejected" {
		return ErrInvalidAccessDecision
	}
	if request.Status != "pending" {
		return ErrHumanAttentionAlreadyResponded
	}
	var actor auth.Principal
	actor.ActorID = actorID
	actor.PrincipalKind = string(auth.PrincipalKindHuman)
	if err := tx.QueryRowContext(ctx, `SELECT id FROM agents WHERE actor_id=? AND revoked_at IS NULL ORDER BY id LIMIT 1`, actorID).Scan(&actor.AgentID); err != nil {
		return auth.ErrHumanRequired
	}
	if err := auth.RequireHumanTx(ctx, tx, actor); err != nil {
		return err
	}
	status := "denied"
	if outcome == "approved" {
		if request.Grant != "auth-admin" {
			return auth.ErrInvalidRequest
		}
		if _, err := auth.ApplyAuthAdminTx(ctx, tx, request.PrincipalID, true, actor); err != nil {
			return err
		}
		status = "approved"
	}
	_, err = tx.ExecContext(ctx, `UPDATE access_requests SET status=?,decided_at=?,decided_by=? WHERE id=? AND status='pending'`, status, at, actorID, request.ID)
	return err
}

func (s *Store) AccessRequestForEvent(ctx context.Context, eventID string) (AccessRequest, error) {
	return scanAccessRequest(s.db.QueryRowContext(ctx, `SELECT `+accessRequestColumns+` FROM access_requests WHERE request_event_id=?`, eventID))
}
