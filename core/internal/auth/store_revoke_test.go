package auth

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"organization-autorunner-core/internal/storage"
)

func TestRevokeAgentIsIdempotentAndAudited(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	workspace, err := storage.InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatalf("initialize workspace: %v", err)
	}
	defer workspace.Close()

	store := NewStore(workspace.DB())
	admin := insertAuthAgentForRevokeTest(t, ctx, workspace.DB(), "agent-admin", "actor-admin", "admin.user")
	member := insertAuthAgentForRevokeTest(t, ctx, workspace.DB(), "agent-member", "actor-member", "member.user")

	first, err := store.RevokeAgent(ctx, member.AgentID, RevokeAgentInput{
		Actor: admin,
		Mode:  RevocationModeAdmin,
	})
	if err != nil {
		t.Fatalf("first revoke: %v", err)
	}
	if !first.Principal.Revoked || first.Revocation.AlreadyRevoked {
		t.Fatalf("unexpected first revoke result: %#v", first)
	}
	if first.Revocation.Mode != string(RevocationModeAdmin) || first.Revocation.ForceLastActive {
		t.Fatalf("unexpected first revoke metadata: %#v", first)
	}

	second, err := store.RevokeAgent(ctx, member.AgentID, RevokeAgentInput{
		Actor: admin,
		Mode:  RevocationModeAdmin,
	})
	if err != nil {
		t.Fatalf("second revoke: %v", err)
	}
	if !second.Principal.Revoked || !second.Revocation.AlreadyRevoked {
		t.Fatalf("expected idempotent revoke result, got %#v", second)
	}

	events, _, err := store.ListAuditEvents(ctx, AuthAuditListFilter{})
	if err != nil {
		t.Fatalf("list audit events: %v", err)
	}
	matches := 0
	for _, event := range events {
		if event.EventType != AuthAuditEventPrincipalRevoked {
			continue
		}
		if event.SubjectAgentID == nil || *event.SubjectAgentID != member.AgentID {
			continue
		}
		matches++
		if event.ActorAgentID == nil || *event.ActorAgentID != admin.AgentID {
			t.Fatalf("unexpected actor in audit event: %#v", event)
		}
		if got := stringValueForRevokeTest(event.Metadata["revocation_mode"]); got != string(RevocationModeAdmin) {
			t.Fatalf("unexpected revocation mode metadata: %#v", event)
		}
		if force, ok := event.Metadata["force_last_active"].(bool); !ok || force {
			t.Fatalf("unexpected force_last_active metadata: %#v", event)
		}
	}
	if matches != 1 {
		t.Fatalf("expected exactly one principal_revoked audit event, got %d in %#v", matches, events)
	}
}

func TestRevokeAgentRequiresForceForLastActivePrincipal(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	workspace, err := storage.InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatalf("initialize workspace: %v", err)
	}
	defer workspace.Close()

	store := NewStore(workspace.DB())
	admin := insertAuthAgentForRevokeTest(t, ctx, workspace.DB(), "agent-admin", "actor-admin", "admin.user")

	_, err = store.RevokeAgent(ctx, admin.AgentID, RevokeAgentInput{
		Actor: admin,
		Mode:  RevocationModeSelf,
	})
	if err == nil || err != ErrLastActivePrincipal {
		t.Fatalf("expected ErrLastActivePrincipal, got %v", err)
	}

	summary, err := store.GetPrincipalSummary(ctx, admin.AgentID)
	if err != nil {
		t.Fatalf("load principal summary after blocked revoke: %v", err)
	}
	if summary.Revoked {
		t.Fatalf("principal should remain active after blocked revoke: %#v", summary)
	}

	result, err := store.RevokeAgent(ctx, admin.AgentID, RevokeAgentInput{
		Actor:           admin,
		Mode:            RevocationModeSelf,
		ForceLastActive: true,
	})
	if err != nil {
		t.Fatalf("force revoke last active principal: %v", err)
	}
	if !result.Principal.Revoked || !result.Revocation.ForceLastActive {
		t.Fatalf("unexpected forced revoke result: %#v", result)
	}

	events, _, err := store.ListAuditEvents(ctx, AuthAuditListFilter{})
	if err != nil {
		t.Fatalf("list audit events: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected one audit event, got %#v", events)
	}
	if events[0].EventType != AuthAuditEventPrincipalSelfRevoked {
		t.Fatalf("unexpected audit event type: %#v", events[0])
	}
	if got := stringValueForRevokeTest(events[0].Metadata["revocation_mode"]); got != string(RevocationModeSelf) {
		t.Fatalf("unexpected revocation mode metadata: %#v", events[0])
	}
	if force, ok := events[0].Metadata["force_last_active"].(bool); !ok || !force {
		t.Fatalf("expected force_last_active metadata=true, got %#v", events[0])
	}
}

func insertAuthAgentForRevokeTest(t *testing.T, ctx context.Context, db *sql.DB, agentID string, actorID string, username string) Principal {
	t.Helper()

	now := time.Date(2026, 3, 21, 10, 0, 0, 0, time.UTC).Format(time.RFC3339Nano)
	if _, err := db.ExecContext(
		ctx,
		`INSERT INTO actors(id, display_name, tags_json, created_at, metadata_json)
		 VALUES (?, ?, ?, ?, '{}')`,
		actorID,
		username,
		`["agent"]`,
		now,
	); err != nil {
		t.Fatalf("insert actor: %v", err)
	}
	if _, err := db.ExecContext(
		ctx,
		`INSERT INTO agents(id, username, actor_id, created_at, updated_at, revoked_at, metadata_json)
		 VALUES (?, ?, ?, ?, ?, NULL, '{}')`,
		agentID,
		username,
		actorID,
		now,
		now,
	); err != nil {
		t.Fatalf("insert agent: %v", err)
	}
	return Principal{
		AgentID:  agentID,
		ActorID:  actorID,
		Username: username,
	}
}

func stringValueForRevokeTest(raw any) string {
	text, _ := raw.(string)
	return text
}
