package router

import (
	"context"
	"testing"

	"organization-autorunner-core/internal/auth"
)

func TestRouteMentionSkipsSelfAuthoredMessages(t *testing.T) {
	state, err := NewStateStore("")
	if err != nil {
		t.Fatalf("NewStateStore: %v", err)
	}

	createArtifactCalls := 0
	appendEventCalls := 0
	service := NewService(
		Config{
			BaseURL:     "http://core.test",
			WorkspaceID: "ws-main",
		},
		Dependencies{
			CreateArtifact: func(context.Context, string, map[string]any, any, string) error {
				createArtifactCalls++
				return nil
			},
			AppendEvent: func(context.Context, string, map[string]any) error {
				appendEventCalls++
				return nil
			},
		},
		state,
	)
	service.cache.byHandle["m4-hermes"] = auth.AuthPrincipalSummary{
		ActorID:       "actor-m4-hermes",
		Username:      "m4-hermes",
		PrincipalKind: "agent",
		Registration: &auth.AgentRegistration{
			Handle:                     "m4-hermes",
			ActorID:                    "actor-m4-hermes",
			Status:                     "active",
			BridgeCheckinEventID:       "event-checkin-1",
			BridgeSigningPublicKeySPKI: "not-needed-for-self-skip",
			WorkspaceBindings: []auth.AgentRegistrationWorkspaceBinding{
				{WorkspaceID: "ws-main", Enabled: true},
			},
		},
	}

	ok, err := service.routeMention(
		context.Background(),
		"m4-hermes",
		map[string]any{
			"id":        "event-message-1",
			"thread_id": "thread-1",
			"actor_id":  "actor-m4-hermes",
		},
		"@m4-hermes replying to my own wake",
	)
	if err != nil {
		t.Fatalf("routeMention: %v", err)
	}
	if ok {
		t.Fatalf("expected self-authored mention not to route")
	}
	if createArtifactCalls != 0 {
		t.Fatalf("expected no wake artifact creation, got %d", createArtifactCalls)
	}
	if appendEventCalls != 0 {
		t.Fatalf("expected no wake events appended, got %d", appendEventCalls)
	}
}

func TestRouteMentionQueuesNotificationForOfflineRegisteredAgent(t *testing.T) {
	state, err := NewStateStore("")
	if err != nil {
		t.Fatalf("NewStateStore: %v", err)
	}

	createArtifactCalls := 0
	appendEventCalls := 0
	service := NewService(
		Config{
			BaseURL:     "http://core.test",
			WorkspaceID: "ws-main",
		},
		Dependencies{
			GetThread: func(context.Context, string) (map[string]any, error) {
				return map[string]any{
					"id":              "thread-1",
					"title":           "Thread One",
					"current_summary": "Current summary",
				}, nil
			},
			CreateArtifact: func(context.Context, string, map[string]any, any, string) error {
				createArtifactCalls++
				return nil
			},
			AppendEvent: func(context.Context, string, map[string]any) error {
				appendEventCalls++
				return nil
			},
		},
		state,
	)
	service.cache.byHandle["m4-hermes"] = auth.AuthPrincipalSummary{
		ActorID:       "actor-m4-hermes",
		Username:      "m4-hermes",
		PrincipalKind: "agent",
		Registration: &auth.AgentRegistration{
			Handle:  "m4-hermes",
			ActorID: "actor-m4-hermes",
			Status:  "pending",
			WorkspaceBindings: []auth.AgentRegistrationWorkspaceBinding{
				{WorkspaceID: "ws-main", Enabled: true},
			},
		},
	}

	ok, err := service.routeMention(
		context.Background(),
		"m4-hermes",
		map[string]any{
			"id":        "event-message-1",
			"thread_id": "thread-1",
			"actor_id":  "actor-human",
			"ts":        "2026-03-30T10:00:00Z",
		},
		"@m4-hermes please check this",
	)
	if err != nil {
		t.Fatalf("routeMention: %v", err)
	}
	if !ok {
		t.Fatal("expected offline registered agent mention to queue a notification")
	}
	if createArtifactCalls != 1 {
		t.Fatalf("expected one wake artifact creation, got %d", createArtifactCalls)
	}
	if appendEventCalls != 1 {
		t.Fatalf("expected one wake event appended, got %d", appendEventCalls)
	}
}

func TestRouteMentionRefreshesPrincipalCacheWhenRegistrationIsStale(t *testing.T) {
	state, err := NewStateStore("")
	if err != nil {
		t.Fatalf("NewStateStore: %v", err)
	}

	createArtifactCalls := 0
	appendEventCalls := 0
	listPrincipalCalls := 0
	service := NewService(
		Config{
			BaseURL:     "http://core.test",
			WorkspaceID: "ws-main",
		},
		Dependencies{
			ListPrincipals: func(context.Context, int) ([]auth.AuthPrincipalSummary, error) {
				listPrincipalCalls++
				return []auth.AuthPrincipalSummary{
					{
						ActorID:       "actor-m4-hermes",
						Username:      "m4-hermes",
						PrincipalKind: "agent",
						Registration: &auth.AgentRegistration{
							Handle:  "m4-hermes",
							ActorID: "actor-m4-hermes",
							Status:  "active",
							WorkspaceBindings: []auth.AgentRegistrationWorkspaceBinding{
								{WorkspaceID: "ws-main", Enabled: true},
							},
						},
					},
				}, nil
			},
			GetThread: func(context.Context, string) (map[string]any, error) {
				return map[string]any{
					"id":              "thread-1",
					"title":           "Thread One",
					"current_summary": "Current summary",
				}, nil
			},
			CreateArtifact: func(context.Context, string, map[string]any, any, string) error {
				createArtifactCalls++
				return nil
			},
			AppendEvent: func(context.Context, string, map[string]any) error {
				appendEventCalls++
				return nil
			},
		},
		state,
	)
	service.cache.byHandle["m4-hermes"] = auth.AuthPrincipalSummary{
		ActorID:       "actor-m4-hermes",
		Username:      "m4-hermes",
		PrincipalKind: "agent",
		Registration:  nil,
	}

	ok, err := service.routeMention(
		context.Background(),
		"m4-hermes",
		map[string]any{
			"id":        "event-message-1",
			"thread_id": "thread-1",
			"actor_id":  "actor-human",
			"ts":        "2026-03-31T10:00:00Z",
		},
		"@m4-hermes please check this",
	)
	if err != nil {
		t.Fatalf("routeMention: %v", err)
	}
	if !ok {
		t.Fatal("expected mention to route after refreshing stale principal cache")
	}
	if listPrincipalCalls != 1 {
		t.Fatalf("expected one forced principal refresh, got %d", listPrincipalCalls)
	}
	if createArtifactCalls != 1 {
		t.Fatalf("expected one wake artifact creation, got %d", createArtifactCalls)
	}
	if appendEventCalls != 1 {
		t.Fatalf("expected one wake event appended, got %d", appendEventCalls)
	}
}
