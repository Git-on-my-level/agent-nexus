package pm

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"agent-nexus-core/internal/blob"
	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/storage"
)

func TestRealNexusWakeArtifactSessionAndReplyRoundTrip(t *testing.T) {
	ctx := context.Background()
	workspace, err := storage.InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer workspace.Close()
	ps := primitives.NewStore(workspace.DB(), blob.NewFilesystemBackend(workspace.Layout().ArtifactContentDir), workspace.Layout().ArtifactContentDir)
	st, err := NewStore(workspace.DB())
	if err != nil {
		t.Fatal(err)
	}
	bridge := NexusBridge{Store: ps, ActorID: "pm-service", Ready: func(context.Context, string, string) error { return nil }}
	cfg := Config{WorkspaceID: "ws", AgentActorID: "pm-agent", AgentHandle: "pm", BaseURL: "http://localhost:8000", TurnTimeout: time.Minute}
	svc, err := NewService(st, cfg, Dependencies{Authorize: func(context.Context, Principal, string, string) error { return nil }, EnsureThread: bridge.EnsureThread, Dispatch: bridge.Dispatch})
	if err != nil {
		t.Fatal(err)
	}
	p := Principal{WorkspaceID: "ws", ActorID: "human", Human: true}
	c, err := svc.CreateConversation(ctx, p, CreateConversation{RequestKey: "conversation", Title: "Review work"})
	if err != nil {
		t.Fatal(err)
	}
	turn, err := svc.PostMessage(ctx, p, c.ID, MessageInput{RequestKey: "turn", Text: "What changed?"})
	if err != nil {
		t.Fatal(err)
	}
	if turn.Status != Sending {
		t.Fatalf("dispatch %+v", turn)
	}
	wake, err := ps.GetAgentWakeup(ctx, turn.WakeupID)
	if err != nil {
		t.Fatal(err)
	}
	if wake.TargetActorID != "pm-agent" || wake.ThreadID != c.ThreadID {
		t.Fatalf("wrong bridge target %+v", wake)
	}
	content, _, err := ps.GetArtifactContent(ctx, turn.WakeupID)
	if err != nil {
		t.Fatal(err)
	}
	var packet map[string]any
	if err = json.Unmarshal(content, &packet); err != nil {
		t.Fatal(err)
	}
	if packet["session_key"] != "anx:ws:"+c.ThreadID+":pm" || packet["version"] != "agent-wake/v1" {
		t.Fatalf("incompatible bridge packet %v", packet)
	}
	if _, err = ps.GetEvent(ctx, turn.ID); err != nil {
		t.Fatal(err)
	}
	event := map[string]any{"id": "reply-1", "type": "message_posted", "thread_id": c.ThreadID, "summary": "PM result", "refs": []string{"thread:" + c.ThreadID, "artifact:" + turn.WakeupID}, "payload": map[string]any{"text": "A source reports progress; independent verification remains open.", "wakeup_id": turn.WakeupID}}
	if _, err = ps.AppendEvent(ctx, "wrong-agent", event); err != nil {
		t.Fatal(err)
	}
	if _, err = svc.SyncBridgeReply(ctx, ps, turn.ID, "reply-1"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("wrong agent reply accepted %v", err)
	}
	event["id"] = "reply-2"
	if _, err = ps.AppendEvent(ctx, "pm-agent", event); err != nil {
		t.Fatal(err)
	}
	completed, err := svc.SyncBridgeReply(ctx, ps, turn.ID, "reply-2")
	if err != nil {
		t.Fatal(err)
	}
	if completed.Status != Delivered || completed.Response == "" || len(completed.EvidenceRefs) != 1 {
		t.Fatalf("reply %+v", completed)
	}
	// Queue completion is not an action verification or a work completion.
	actions, err := svc.ListActions(ctx, p)
	if err != nil || len(actions) != 0 {
		t.Fatalf("reply created action %v %v", actions, err)
	}
}

func TestNexusBridgeRequiresLiveReadiness(t *testing.T) {
	err := (NexusBridge{}).Dispatch(context.Background(), DispatchRequest{})
	if !errors.Is(err, ErrUnavailable) {
		t.Fatal(err)
	}
}
