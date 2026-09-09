package pm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/router"
)

// NexusBridge sends actual agent-wake/v1 artifacts to the existing durable
// queue. It does not launch an alternative agent, watcher or provider registry.
// Ready must validate current registration/check-in and a deployment-approved
// PM runtime with read-only context credentials and bounded execution. Prompt
// instructions alone are NOT an execution sandbox or a permission boundary.
type NexusBridge struct {
	Store   *primitives.Store
	ActorID string
	Ready   func(context.Context, string, string) error
}

func (b NexusBridge) EnsureThread(ctx context.Context, p Principal, id, workRef string) (string, error) {
	if b.Store == nil {
		return "", ErrUnavailable
	}
	threadID := stableID("thread", id)
	existing, err := b.Store.GetThread(ctx, threadID)
	if err == nil {
		if existing["pm_actor_id"] != p.ActorID {
			return "", ErrForbidden
		}
		return threadID, nil
	}
	if !errors.Is(err, primitives.ErrNotFound) {
		return "", err
	}
	thread := map[string]any{"id": threadID, "title": "Project manager", "pm_actor_id": p.ActorID, "pm_conversation_id": id}
	if workRef != "" {
		thread["subject_ref"] = workRef
	}
	_, err = b.Store.CreateThread(ctx, p.ActorID, thread)
	if err != nil && !errors.Is(err, primitives.ErrConflict) {
		return "", err
	}
	return threadID, nil
}
func (b NexusBridge) Dispatch(ctx context.Context, req DispatchRequest) error {
	if b.Store == nil || b.ActorID == "" || b.Ready == nil {
		return ErrUnavailable
	}
	p := req.Packet
	if err := b.Ready(ctx, p.ActorID, p.WorkspaceID); err != nil {
		return ErrUnavailable
	}
	// Persist a real trigger event so context_fetch and reply refs resolve.
	_, err := b.Store.AppendEvent(ctx, req.Turn.ActorID, map[string]any{"id": p.TriggerEventID, "type": "message_posted", "thread_id": p.ThreadID, "summary": "PM discussion", "refs": []string{"thread:" + p.ThreadID}, "payload": map[string]any{"text": req.Turn.Text, "pm_turn_id": req.Turn.ID}})
	if err != nil && !errors.Is(err, primitives.ErrConflict) {
		return err
	}
	content := p.ToContent()
	content["pm_execution"] = map[string]any{"turn_id": req.Turn.ID, "deadline": req.Turn.Deadline, "max_output_bytes": req.MaxOutputBytes, "timeout_seconds": int(req.Timeout.Seconds()), "requesting_actor_id": req.Turn.ActorID, "context_url": p.AnxBaseURL + "/pm/turns/" + req.Turn.ID + "/context"}
	artifact := map[string]any{"id": p.WakeupID, "kind": router.WakeArtifactKind, "summary": "Contextual PM turn", "refs": router.WakeArtifactRefs(p.ThreadID, p.TriggerEventID, p.SubjectRef), "target_handle": p.Handle, "target_actor_id": p.ActorID, "workspace_id": p.WorkspaceID, "thread_id": p.ThreadID}
	_, err = b.Store.CreateArtifact(ctx, b.ActorID, artifact, content, "structured")
	if err != nil && !errors.Is(err, primitives.ErrConflict) {
		return err
	}
	_, err = b.Store.UpsertAgentWakeup(ctx, primitives.AgentWakeup{WakeupID: p.WakeupID, Status: primitives.AgentWakeupStatusRequested, TargetHandle: p.Handle, TargetActorID: p.ActorID, WorkspaceID: p.WorkspaceID, WorkspaceName: p.WorkspaceName, ThreadID: p.ThreadID, ThreadTitle: p.ThreadTitle, TriggerEventID: p.TriggerEventID, TriggerCreatedAt: p.TriggerCreatedAt, TriggerText: p.TriggerText, Refs: append(router.WakeArtifactRefs(p.ThreadID, p.TriggerEventID, p.SubjectRef), "artifact:"+p.WakeupID)})
	return err
}

// SyncBridgeReply uses an existing durable event, not process exit or a source
// task status, as the assistant response. Call on a message event notification
// or an explicit refresh; no new watcher or channel consumer is needed.
func (s *Service) SyncBridgeReply(ctx context.Context, store *primitives.Store, turnID, eventID string) (Turn, error) {
	if store == nil {
		return Turn{}, ErrUnavailable
	}
	var t Turn
	if err := s.store.get(ctx, "turn", turnID, &t); err != nil {
		return Turn{}, err
	}
	var c Conversation
	if err := s.store.get(ctx, "conversation", t.ConversationID, &c); err != nil {
		return Turn{}, err
	}
	event, err := store.GetEvent(ctx, eventID)
	if err != nil {
		return Turn{}, err
	}
	if event["actor_id"] != t.AgentActorID || event["thread_id"] != c.ThreadID || event["type"] != "message_posted" {
		return Turn{}, ErrForbidden
	}
	payload, _ := event["payload"].(map[string]any)
	if payload["wakeup_id"] != t.WakeupID {
		return Turn{}, ErrForbidden
	}
	text, _ := payload["text"].(string)
	return s.CompleteTurn(ctx, Principal{WorkspaceID: t.WorkspaceID, ActorID: t.AgentActorID}, t.ID, text, []string{"event:" + eventID})
}

// RuntimePolicy is embedded in existing wake packet context_inline so the
// existing bridge's WakePacket round-trip preserves execution limits.
func runtimePolicy(t Turn, maxOutput int) string {
	b, _ := json.Marshal(map[string]any{"pm_turn_id": t.ID, "deadline": t.Deadline, "max_output_bytes": maxOutput, "context_path": fmt.Sprintf("/pm/turns/%s/context", t.ID)})
	return string(b)
}
