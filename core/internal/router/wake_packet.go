package router

import "strings"

type WakePacket struct {
	WakeupID             string
	Handle               string
	ActorID              string
	WorkspaceID          string
	WorkspaceName        string
	ThreadID             string
	ThreadTitle          string
	TriggerEventID       string
	TriggerCreatedAt     string
	TriggerAuthorActorID string
	TriggerText          string
	CurrentSummary       string
	SessionKey           string
	OARBaseURL           string
	ThreadContextURL     string
	ThreadWorkspaceURL   string
	TriggerEventURL      string
	CLIThreadInspect     string
	CLIThreadWorkspace   string
	Version              string
}

func (p WakePacket) ToContent() map[string]any {
	version := p.Version
	if version == "" {
		version = "agent-wake-packet/v1"
	}
	return map[string]any{
		"version":   version,
		"wakeup_id": p.WakeupID,
		"target": map[string]any{
			"handle":   p.Handle,
			"actor_id": p.ActorID,
		},
		"workspace": map[string]any{
			"id":   p.WorkspaceID,
			"name": p.WorkspaceName,
		},
		"thread": map[string]any{
			"id":    p.ThreadID,
			"title": p.ThreadTitle,
		},
		"trigger": map[string]any{
			"kind":             "mention",
			"message_event_id": p.TriggerEventID,
			"created_at":       p.TriggerCreatedAt,
			"author_actor_id":  p.TriggerAuthorActorID,
			"text":             p.TriggerText,
		},
		"context_inline": map[string]any{
			"current_summary": p.CurrentSummary,
		},
		"session_key": p.SessionKey,
		"context_fetch": map[string]any{
			"preferred": "threads.workspace",
			"cli":       []string{p.CLIThreadWorkspace, p.CLIThreadInspect},
			"api": map[string]any{
				"thread":        strings.TrimRight(p.OARBaseURL, "/") + "/threads/" + p.ThreadID,
				"context":       p.ThreadContextURL,
				"workspace":     p.ThreadWorkspaceURL,
				"trigger_event": p.TriggerEventURL,
			},
		},
		"reply_refs": []string{
			"thread:" + p.ThreadID,
			"event:" + p.TriggerEventID,
			"artifact:" + p.WakeupID,
		},
	}
}
