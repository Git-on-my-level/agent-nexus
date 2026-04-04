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

func TestRouteMentionWakePacketIncludesSubjectFromThread(t *testing.T) {
	state, err := NewStateStore("")
	if err != nil {
		t.Fatalf("NewStateStore: %v", err)
	}

	var capturedContent map[string]any
	var lastEvent map[string]any
	service := NewService(
		Config{
			BaseURL:     "http://core.test",
			WorkspaceID: "ws-main",
		},
		Dependencies{
			GetThread: func(context.Context, string) (map[string]any, error) {
				return map[string]any{
					"id":              "thread-subj",
					"title":           "Topic thread",
					"current_summary": "Summary",
					"subject_ref":     "topic:top-9",
				}, nil
			},
			CreateArtifact: func(_ context.Context, _ string, _ map[string]any, content any, _ string) error {
				m, ok := content.(map[string]any)
				if !ok {
					t.Fatalf("artifact content type %T", content)
				}
				capturedContent = m
				return nil
			},
			AppendEvent: func(_ context.Context, _ string, event map[string]any) error {
				lastEvent = event
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
			"id":        "event-message-subj",
			"thread_id": "thread-subj",
			"actor_id":  "actor-human",
			"ts":        "2026-03-30T11:00:00Z",
		},
		"@m4-hermes with subject",
	)
	if err != nil {
		t.Fatalf("routeMention: %v", err)
	}
	if !ok {
		t.Fatal("expected route ok")
	}
	if capturedContent["version"] != WakePacketVersion {
		t.Fatalf("packet version: got %#v", capturedContent["version"])
	}
	if capturedContent["subject_ref"] != "topic:top-9" {
		t.Fatalf("subject_ref: got %#v", capturedContent["subject_ref"])
	}
	rs, _ := capturedContent["resolved_subject"].(map[string]any)
	if rs == nil || rs["kind"] != "topic" {
		t.Fatalf("resolved_subject: %#v", capturedContent["resolved_subject"])
	}
	refs, _ := capturedContent["reply_refs"].([]string)
	if len(refs) < 2 || refs[0] != "thread:thread-subj" || refs[1] != "topic:top-9" {
		t.Fatalf("reply_refs: %#v", refs)
	}
	payload, _ := lastEvent["payload"].(map[string]any)
	if payload["subject_ref"] != "topic:top-9" {
		t.Fatalf("wake request payload subject_ref: %#v", payload["subject_ref"])
	}
	evRefs, _ := lastEvent["refs"].([]string)
	if len(evRefs) < 3 || evRefs[0] != "thread:thread-subj" || evRefs[1] != "topic:top-9" {
		t.Fatalf("wake request refs: %#v", evRefs)
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
