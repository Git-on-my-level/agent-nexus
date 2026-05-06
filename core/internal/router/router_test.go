package router

import (
	"context"
	"strings"
	"testing"

	"agent-nexus-core/internal/auth"
	"agent-nexus-core/internal/primitives"
)

func TestRouteMentionSkipsSelfAuthoredMessages(t *testing.T) {
	state, err := NewStateStore("")
	if err != nil {
		t.Fatalf("NewStateStore: %v", err)
	}

	createArtifactCalls := 0
	wakeupCalls := 0
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
			UpsertAgentWakeup: func(context.Context, primitives.AgentWakeup) error {
				wakeupCalls++
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
			BridgeSigningPublicKeySPKI: "not-needed-for-self-skip",
			BridgeProofSignatureB64:    "not-needed-for-self-skip",
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
	if wakeupCalls != 0 {
		t.Fatalf("expected no wakeups queued, got %d", wakeupCalls)
	}
}

func TestRouteMentionQueuesNotificationForOfflineRegisteredAgent(t *testing.T) {
	state, err := NewStateStore("")
	if err != nil {
		t.Fatalf("NewStateStore: %v", err)
	}

	createArtifactCalls := 0
	wakeupCalls := 0
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
			UpsertAgentWakeup: func(context.Context, primitives.AgentWakeup) error {
				wakeupCalls++
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
	if wakeupCalls != 1 {
		t.Fatalf("expected one wake queued, got %d", wakeupCalls)
	}
}

func TestRouteMentionWakePacketIncludesSubjectFromThread(t *testing.T) {
	state, err := NewStateStore("")
	if err != nil {
		t.Fatalf("NewStateStore: %v", err)
	}

	var capturedContent map[string]any
	var capturedWakeup primitives.AgentWakeup
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
			UpsertAgentWakeup: func(_ context.Context, wakeup primitives.AgentWakeup) error {
				capturedWakeup = wakeup
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
	if capturedWakeup.TriggerEventID != "event-message-subj" {
		t.Fatalf("wake trigger event id: %#v", capturedWakeup.TriggerEventID)
	}
	if len(capturedWakeup.Refs) < 3 || capturedWakeup.Refs[0] != "thread:thread-subj" || capturedWakeup.Refs[1] != "topic:top-9" {
		t.Fatalf("wake refs: %#v", capturedWakeup.Refs)
	}
	cf, _ := capturedContent["context_fetch"].(map[string]any)
	if cf == nil || cf["preferred"] != "topics.workspace" {
		t.Fatalf("context_fetch.preferred: %#v", capturedContent["context_fetch"])
	}
	cli, _ := cf["cli"].([]string)
	if len(cli) < 1 || !strings.Contains(cli[0], "topics workspace topic:top-9") {
		t.Fatalf("context_fetch.cli: %#v", cli)
	}
	api, _ := cf["api"].(map[string]any)
	if api == nil || api["topic_workspace"] != "http://core.test/topics/top-9/workspace" {
		t.Fatalf("context_fetch.api.topic_workspace: %#v", api)
	}
}

func TestRouteMentionRefreshesPrincipalCacheWhenRegistrationIsStale(t *testing.T) {
	state, err := NewStateStore("")
	if err != nil {
		t.Fatalf("NewStateStore: %v", err)
	}

	createArtifactCalls := 0
	wakeupCalls := 0
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
			UpsertAgentWakeup: func(context.Context, primitives.AgentWakeup) error {
				wakeupCalls++
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
	if wakeupCalls != 1 {
		t.Fatalf("expected one wake queued, got %d", wakeupCalls)
	}
}
