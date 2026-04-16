package auth

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
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
	if first.Revocation.Mode != string(RevocationModeAdmin) || first.Revocation.AllowHumanLockout {
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
		if allow, ok := event.Metadata["allow_human_lockout"].(bool); !ok || allow {
			t.Fatalf("unexpected allow_human_lockout metadata: %#v", event)
		}
	}
	if matches != 1 {
		t.Fatalf("expected exactly one principal_revoked audit event, got %d in %#v", matches, events)
	}
}

func TestRevokeAgentBlocksLastActiveHumanButNotMachine(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	workspace, err := storage.InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatalf("initialize workspace: %v", err)
	}
	defer workspace.Close()

	store := NewStore(workspace.DB())
	human := insertAuthHumanForRevokeTest(t, ctx, workspace.DB(), "agent-human", "actor-human", "human.user")
	machine := insertAuthAgentForRevokeTest(t, ctx, workspace.DB(), "agent-machine", "actor-machine", "machine.user")

	_, err = store.RevokeAgent(ctx, human.AgentID, RevokeAgentInput{
		Actor: machine,
		Mode:  RevocationModeAdmin,
	})
	if err == nil || err != ErrLastActivePrincipal {
		t.Fatalf("expected ErrLastActivePrincipal, got %v", err)
	}

	summary, err := store.GetPrincipalSummary(ctx, human.AgentID)
	if err != nil {
		t.Fatalf("load principal summary after blocked revoke: %v", err)
	}
	if summary.Revoked {
		t.Fatalf("principal should remain active after blocked revoke: %#v", summary)
	}

	result, err := store.RevokeAgent(ctx, machine.AgentID, RevokeAgentInput{
		Actor: human,
		Mode:  RevocationModeAdmin,
	})
	if err != nil {
		t.Fatalf("revoke machine while one human remains: %v", err)
	}
	if !result.Principal.Revoked || result.Revocation.AllowHumanLockout {
		t.Fatalf("unexpected machine revoke result: %#v", result)
	}
}

func TestRevokeAgentRequiresExplicitHumanLockoutReason(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	workspace, err := storage.InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatalf("initialize workspace: %v", err)
	}
	defer workspace.Close()

	store := NewStore(workspace.DB())
	admin := insertAuthHumanForRevokeTest(t, ctx, workspace.DB(), "agent-admin", "actor-admin", "admin.user")

	_, err = store.RevokeAgent(ctx, admin.AgentID, RevokeAgentInput{
		Actor: admin,
		Mode:  RevocationModeSelf,
	})
	if err == nil || err != ErrLastActivePrincipal {
		t.Fatalf("expected ErrLastActivePrincipal, got %v", err)
	}

	result, err := store.RevokeAgent(ctx, admin.AgentID, RevokeAgentInput{
		Actor:              admin,
		Mode:               RevocationModeSelf,
		AllowHumanLockout:  true,
		HumanLockoutReason: "break-glass recovery",
	})
	if err != nil {
		t.Fatalf("force revoke last active human principal: %v", err)
	}
	if !result.Principal.Revoked || !result.Revocation.AllowHumanLockout {
		t.Fatalf("unexpected forced revoke result: %#v", result)
	}

	events, _, err := store.ListAuditEvents(ctx, AuthAuditListFilter{})
	if err != nil {
		t.Fatalf("list audit events: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected one audit event, got %#v", events)
	}
	if events[0].EventType != AuthAuditEventPrincipalHumanLockoutRevoked {
		t.Fatalf("unexpected audit event type: %#v", events[0])
	}
	if got := stringValueForRevokeTest(events[0].Metadata["revocation_mode"]); got != string(RevocationModeSelf) {
		t.Fatalf("unexpected revocation mode metadata: %#v", events[0])
	}
	if allow, ok := events[0].Metadata["allow_human_lockout"].(bool); !ok || !allow {
		t.Fatalf("expected allow_human_lockout metadata=true, got %#v", events[0])
	}
	if reason := stringValueForRevokeTest(events[0].Metadata["human_lockout_reason"]); reason != "break-glass recovery" {
		t.Fatalf("unexpected human_lockout_reason metadata: %#v", events[0])
	}
}

func TestRevokeAgentAllowsNonFinalHumanWithMachinePresent(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	workspace, err := storage.InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatalf("initialize workspace: %v", err)
	}
	defer workspace.Close()

	store := NewStore(workspace.DB())
	admin := insertAuthAgentForRevokeTest(t, ctx, workspace.DB(), "agent-admin", "actor-admin", "admin.user")
	humanA := insertAuthHumanForRevokeTest(t, ctx, workspace.DB(), "agent-human-a", "actor-human-a", "human-a.user")
	_ = insertAuthHumanForRevokeTest(t, ctx, workspace.DB(), "agent-human-b", "actor-human-b", "human-b.user")
	_ = insertAuthAgentForRevokeTest(t, ctx, workspace.DB(), "agent-machine", "actor-machine", "machine.user")

	result, err := store.RevokeAgent(ctx, humanA.AgentID, RevokeAgentInput{
		Actor: admin,
		Mode:  RevocationModeAdmin,
	})
	if err != nil {
		t.Fatalf("revoke one of multiple human principals: %v", err)
	}
	if !result.Principal.Revoked || result.Revocation.AllowHumanLockout {
		t.Fatalf("unexpected revoke result: %#v", result)
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

func insertAuthHumanForRevokeTest(t *testing.T, ctx context.Context, db *sql.DB, agentID string, actorID string, username string) Principal {
	t.Helper()

	now := time.Date(2026, 3, 21, 10, 0, 0, 0, time.UTC).Format(time.RFC3339Nano)
	publicKeyB64, _ := generateKeyPairForRevokeTest(t)
	publicKey, err := base64.StdEncoding.DecodeString(publicKeyB64)
	if err != nil {
		t.Fatalf("decode public key: %v", err)
	}
	if _, err := db.ExecContext(
		ctx,
		`INSERT INTO actors(id, display_name, tags_json, created_at, metadata_json)
		 VALUES (?, ?, ?, ?, '{}')`,
		actorID,
		username,
		`["agent","human","passkey"]`,
		now,
	); err != nil {
		t.Fatalf("insert human actor: %v", err)
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
		t.Fatalf("insert human agent: %v", err)
	}
	if _, err := db.ExecContext(
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
		"credential-"+agentID,
		agentID,
		[]byte("user-"+agentID),
		publicKey,
		"none",
		"",
		0,
		0,
		0,
		[]byte{},
		"",
		now,
	); err != nil {
		t.Fatalf("insert human passkey credential: %v", err)
	}
	return Principal{
		AgentID:  agentID,
		ActorID:  actorID,
		Username: username,
	}
}

func generateKeyPairForRevokeTest(t *testing.T) (string, ed25519.PrivateKey) {
	t.Helper()
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate ed25519 key pair: %v", err)
	}
	return base64.StdEncoding.EncodeToString(publicKey), privateKey
}

func stringValueForRevokeTest(raw any) string {
	text, _ := raw.(string)
	return text
}
