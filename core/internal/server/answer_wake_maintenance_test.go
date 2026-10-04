package server

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"agent-nexus-core/internal/blob"
	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/storage"
)

func TestAnswerWakeMaintainerDebouncesAnswersIntoOneWake(t *testing.T) {
	ctx := context.Background()
	workspace, store := newAnswerWakeTestStore(t, ctx)
	defer workspace.Close()

	answers := make([]map[string]any, 0, 3)
	for i := 1; i <= 3; i++ {
		task := appendAnswerWakeTestAsk(t, ctx, store, "actor-one", fmt.Sprintf("one-%d", i))
		answers = append(answers, appendAnswerWakeTestResponse(t, ctx, store, "actor-one", "worker.one", task))
	}
	maintainer := NewAnswerWakeMaintainer(AnswerWakeMaintainerConfig{
		PrimitiveStore: store, WorkspaceID: "ws_test", QuietWindow: time.Minute,
		FlushWhenNoOpenAsks: false,
	})
	lastAnswered := answerWakeTestTimestamp(t, answers[len(answers)-1])
	if err := maintainer.Step(ctx, lastAnswered.Add(59*time.Second)); err != nil {
		t.Fatalf("step before quiet window: %v", err)
	}
	if wakeups, err := store.ListAgentWakeups(ctx, primitives.AgentWakeupListFilter{TargetActorID: "actor-one"}); err != nil || len(wakeups) != 0 {
		t.Fatalf("wake before quiet window: wakeups=%#v err=%v", wakeups, err)
	}
	if err := maintainer.Step(ctx, lastAnswered.Add(time.Minute)); err != nil {
		t.Fatalf("step after quiet window: %v", err)
	}
	wakeups, err := store.ListAgentWakeups(ctx, primitives.AgentWakeupListFilter{TargetActorID: "actor-one"})
	if err != nil || len(wakeups) != 1 {
		t.Fatalf("expected one batched wake, got %#v err=%v", wakeups, err)
	}
	wakeup := wakeups[0]
	if !strings.Contains(wakeup.TriggerText, "3 answers") {
		t.Fatalf("wake does not describe the answer batch: %#v", wakeup)
	}
	for _, response := range answers {
		if !containsAnswerWakeRef(wakeup.Refs, "event:"+fmt.Sprint(response["id"])) {
			t.Fatalf("wake omitted answer event %v: %#v", response["id"], wakeup.Refs)
		}
	}
	updated, err := store.MarkAgentWakeupNotification(ctx, wakeup.WakeupID, "actor-one", primitives.AgentWakeupNotificationRead)
	if err != nil || updated.NotificationStatus != primitives.AgentWakeupNotificationRead {
		t.Fatalf("mark batch read: wakeup=%#v err=%v", updated, err)
	}
	unread, err := store.ListAgentWakeups(ctx, primitives.AgentWakeupListFilter{TargetActorID: "actor-one", NotificationStatuses: []string{primitives.AgentWakeupNotificationUnread}})
	if err != nil || len(unread) != 0 {
		t.Fatalf("read batch still appears unread: wakeups=%#v err=%v", unread, err)
	}
}

func TestAnswerWakeMaintainerSplitsQuietWindowsAndTargetsEachAgent(t *testing.T) {
	t.Run("split quiet windows produce two wakes", func(t *testing.T) {
		ctx := context.Background()
		workspace, store := newAnswerWakeTestStore(t, ctx)
		defer workspace.Close()
		maintainer := NewAnswerWakeMaintainer(AnswerWakeMaintainerConfig{
			PrimitiveStore: store, WorkspaceID: "ws_test", QuietWindow: time.Minute,
			FlushWhenNoOpenAsks: false,
		})
		firstAsk := appendAnswerWakeTestAsk(t, ctx, store, "actor-one", "split-1")
		firstAnswer := appendAnswerWakeTestResponse(t, ctx, store, "actor-one", "worker.one", firstAsk)
		if err := maintainer.Step(ctx, answerWakeTestTimestamp(t, firstAnswer).Add(time.Minute)); err != nil {
			t.Fatalf("dispatch first window: %v", err)
		}
		secondAsk := appendAnswerWakeTestAsk(t, ctx, store, "actor-one", "split-2")
		secondAnswer := appendAnswerWakeTestResponse(t, ctx, store, "actor-one", "worker.one", secondAsk)
		if err := maintainer.Step(ctx, answerWakeTestTimestamp(t, secondAnswer).Add(time.Minute)); err != nil {
			t.Fatalf("dispatch second window: %v", err)
		}
		wakeups, err := store.ListAgentWakeups(ctx, primitives.AgentWakeupListFilter{TargetActorID: "actor-one"})
		if err != nil || len(wakeups) != 2 {
			t.Fatalf("expected two wakes for separate windows, got %#v err=%v", wakeups, err)
		}
	})

	t.Run("each agent gets its own wake", func(t *testing.T) {
		ctx := context.Background()
		workspace, store := newAnswerWakeTestStore(t, ctx)
		defer workspace.Close()
		firstAsk := appendAnswerWakeTestAsk(t, ctx, store, "actor-one", "agent-one")
		firstAnswer := appendAnswerWakeTestResponse(t, ctx, store, "actor-one", "worker.one", firstAsk)
		secondAsk := appendAnswerWakeTestAsk(t, ctx, store, "actor-two", "agent-two")
		secondAnswer := appendAnswerWakeTestResponse(t, ctx, store, "actor-two", "worker.two", secondAsk)
		maintainer := NewAnswerWakeMaintainer(AnswerWakeMaintainerConfig{
			PrimitiveStore: store, WorkspaceID: "ws_test", QuietWindow: time.Minute,
			FlushWhenNoOpenAsks: false,
		})
		flushAt := answerWakeTestTimestamp(t, firstAnswer).Add(time.Minute)
		if at := answerWakeTestTimestamp(t, secondAnswer).Add(time.Minute); at.After(flushAt) {
			flushAt = at
		}
		if err := maintainer.Step(ctx, flushAt); err != nil {
			t.Fatalf("dispatch agent batches: %v", err)
		}
		for actorID, handle := range map[string]string{"actor-one": "worker.one", "actor-two": "worker.two"} {
			wakeups, err := store.ListAgentWakeups(ctx, primitives.AgentWakeupListFilter{TargetActorID: actorID})
			if err != nil || len(wakeups) != 1 || wakeups[0].TargetHandle != handle {
				t.Fatalf("agent %s received wrong wake batch: %#v err=%v", actorID, wakeups, err)
			}
		}
	})
}

func TestAnswerWakeMaintainerFlushesWhenNoAsksRemain(t *testing.T) {
	ctx := context.Background()
	workspace, store := newAnswerWakeTestStore(t, ctx)
	defer workspace.Close()
	ask := appendAnswerWakeTestAsk(t, ctx, store, "actor-one", "no-open-asks")
	answer := appendAnswerWakeTestResponse(t, ctx, store, "actor-one", "worker.one", ask)
	maintainer := NewAnswerWakeMaintainer(AnswerWakeMaintainerConfig{
		PrimitiveStore: store, WorkspaceID: "ws_test", QuietWindow: time.Minute,
		FlushWhenNoOpenAsks: true,
	})
	if err := maintainer.Step(ctx, answerWakeTestTimestamp(t, answer)); err != nil {
		t.Fatalf("flush completed asks: %v", err)
	}
	wakeups, err := store.ListAgentWakeups(ctx, primitives.AgentWakeupListFilter{TargetActorID: "actor-one"})
	if err != nil || len(wakeups) != 1 {
		t.Fatalf("expected immediate wake after final ask, got %#v err=%v", wakeups, err)
	}
}

func newAnswerWakeTestStore(t *testing.T, ctx context.Context) (*storage.Workspace, *primitives.Store) {
	t.Helper()
	workspace, err := storage.InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatalf("initialize workspace: %v", err)
	}
	store := primitives.NewStore(workspace.DB(), blob.NewFilesystemBackend(workspace.Layout().ArtifactContentDir), workspace.Layout().ArtifactContentDir)
	return workspace, store
}

func appendAnswerWakeTestAsk(t *testing.T, ctx context.Context, store *primitives.Store, targetActorID, suffix string) map[string]any {
	t.Helper()
	threadID := "thread-" + suffix
	event, err := store.AppendEvent(ctx, "actor-human", map[string]any{
		"type": "human_attention_requested", "thread_id": threadID, "refs": []string{"thread:" + threadID},
		"payload": map[string]any{
			"kind": "ask", "title": "Need a decision", "requester_actor_id": targetActorID,
			"subject_ref": "thread:" + threadID, "response_proposals": []string{"Proceed"},
		},
	})
	if err != nil {
		t.Fatalf("append ask: %v", err)
	}
	return event
}

func appendAnswerWakeTestResponse(t *testing.T, ctx context.Context, store *primitives.Store, targetActorID, targetHandle string, ask map[string]any) map[string]any {
	t.Helper()
	askID := fmt.Sprint(ask["id"])
	threadID := fmt.Sprint(ask["thread_id"])
	event, _, err := store.AppendHumanAttentionResponse(ctx, "actor-human", askID, "inbox:"+askID, "", "", map[string]any{
		"type": "human_attention_responded", "thread_id": threadID, "refs": []string{"event:" + askID, "thread:" + threadID},
		"payload": map[string]any{
			"request_event_ref": "event:" + askID, "requester_actor_id": targetActorID,
			"responding_actor_id": "actor-human", "outcome": "answered", "response_text": "Proceed",
			"subject_ref": "thread:" + threadID,
		},
	}, map[string]any{
		"requested": true, "target_actor_id": targetActorID, "target_handle": targetHandle,
		"workspace_id": "ws_test", "thread_id": threadID, "subject_ref": "thread:" + threadID,
	})
	if err != nil {
		t.Fatalf("append answer: %v", err)
	}
	return asMapForAnswerWakeTest(event["event"])
}

func answerWakeTestTimestamp(t *testing.T, answer map[string]any) time.Time {
	t.Helper()
	value, err := time.Parse(time.RFC3339Nano, fmt.Sprint(answer["ts"]))
	if err != nil {
		t.Fatalf("parse answer timestamp: %v", err)
	}
	return value
}

func asMapForAnswerWakeTest(value any) map[string]any {
	if result, ok := value.(map[string]any); ok {
		return result
	}
	return map[string]any{}
}

func containsAnswerWakeRef(refs []string, want string) bool {
	for _, ref := range refs {
		if ref == want {
			return true
		}
	}
	return false
}
