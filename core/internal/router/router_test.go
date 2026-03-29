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
			GetRegistrationContent: func(context.Context, string) (map[string]any, error) {
				return map[string]any{
					"handle":                             "m4-hermes",
					"actor_id":                           "actor-m4-hermes",
					"status":                             "active",
					"bridge_checkin_event_id":            "event-checkin-1",
					"bridge_signing_public_key_spki_b64": "not-needed-for-self-skip",
					"workspace_bindings": []map[string]any{
						{"workspace_id": "ws-main", "enabled": true},
					},
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
