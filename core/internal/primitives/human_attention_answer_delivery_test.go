package primitives

import (
	"context"
	"fmt"
	"testing"
	"time"

	"agent-nexus-core/internal/blob"
	"agent-nexus-core/internal/storage"
)

func TestAnswerWakeDeliveryRollsBackClaimIfWakeInsertFails(t *testing.T) {
	ctx := context.Background()
	workspace, err := storage.InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer workspace.Close()
	store := NewStore(workspace.DB(), blob.NewFilesystemBackend(workspace.Layout().ArtifactContentDir), workspace.Layout().ArtifactContentDir)
	appendDeliveryTestAnswer(t, ctx, store, "agent-one", "rollback")
	batches, err := store.ListHumanAttentionAnswerWakeBatches(ctx)
	if err != nil || len(batches) != 1 {
		t.Fatalf("load pending generation: batches=%#v err=%v", batches, err)
	}

	// An invalid wakeup fails after the transaction has claimed/deleted the
	// generation. The rollback must restore it as if the process crashed there.
	_, err = store.DeliverHumanAttentionAnswerWakeBatch(ctx, batches[0], AgentWakeup{TargetActorID: "agent-one"})
	if err == nil {
		t.Fatal("expected invalid wakeup insert to fail")
	}
	remaining, err := store.ListHumanAttentionAnswerWakeBatches(ctx)
	if err != nil || len(remaining) != 1 || remaining[0].BatchID != batches[0].BatchID {
		t.Fatalf("failed delivery lost its pending generation: batches=%#v err=%v", remaining, err)
	}
	wakeups, err := store.ListAgentWakeups(ctx, AgentWakeupListFilter{TargetActorID: "agent-one"})
	if err != nil || len(wakeups) != 0 {
		t.Fatalf("failed delivery left a partial wake: wakeups=%#v err=%v", wakeups, err)
	}
}

func TestAnswerWakeClaimSerializesAnAnswerIntoTheNextGeneration(t *testing.T) {
	ctx := context.Background()
	workspace, err := storage.InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer workspace.Close()
	store := NewStore(workspace.DB(), blob.NewFilesystemBackend(workspace.Layout().ArtifactContentDir), workspace.Layout().ArtifactContentDir)
	firstAnswer := appendDeliveryTestAnswer(t, ctx, store, "agent-one", "first")
	batches, err := store.ListHumanAttentionAnswerWakeBatches(ctx)
	if err != nil || len(batches) != 1 {
		t.Fatalf("load first generation: batches=%#v err=%v", batches, err)
	}
	batch := batches[0]
	firstAnswerID := fmt.Sprint(firstAnswer["id"])
	wakeup := deliveryTestWakeup(batch)

	tx, err := workspace.DB().BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := claimHumanAttentionAnswerWakeBatchTx(ctx, tx, batch, wakeup)
	if err != nil || !claimed {
		_ = tx.Rollback()
		t.Fatalf("claim first generation: claimed=%v err=%v", claimed, err)
	}

	started := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		close(started)
		_, err := makeDeliveryTestAnswer(ctx, store, "agent-one", "concurrent")
		done <- err
	}()
	<-started
	select {
	case err := <-done:
		_ = tx.Rollback()
		t.Fatalf("concurrent answer passed an uncommitted generation claim: %v", err)
	case <-time.After(25 * time.Millisecond):
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatalf("append concurrent answer after wake commit: %v", err)
	}

	wakeups, err := store.ListAgentWakeups(ctx, AgentWakeupListFilter{TargetActorID: "agent-one"})
	if err != nil || len(wakeups) != 1 {
		t.Fatalf("expected claimed generation to have one wake: wakeups=%#v err=%v", wakeups, err)
	}
	if !containsStoredRef(wakeups[0].Refs, "event:"+firstAnswerID) {
		t.Fatalf("first wake omitted its response: %#v", wakeups[0].Refs)
	}
	batches, err = store.ListHumanAttentionAnswerWakeBatches(ctx)
	if err != nil || len(batches) != 1 || len(batches[0].AnswerEventIDs) != 1 {
		t.Fatalf("concurrent answer did not form a fresh generation: batches=%#v err=%v", batches, err)
	}
	if containsStoredRef(wakeups[0].Refs, "event:"+batches[0].AnswerEventIDs[0]) {
		t.Fatalf("new-generation response was included in the committed wake: wake=%#v batch=%#v", wakeups[0], batches[0])
	}
}

func appendDeliveryTestAnswer(t *testing.T, ctx context.Context, store *Store, targetActorID, suffix string) map[string]any {
	t.Helper()
	event, err := makeDeliveryTestAnswer(ctx, store, targetActorID, suffix)
	if err != nil {
		t.Fatalf("append answer for %s: %v", suffix, err)
	}
	return event
}

func makeDeliveryTestAnswer(ctx context.Context, store *Store, targetActorID, suffix string) (map[string]any, error) {
	threadID := "thread-" + suffix
	ask, err := store.AppendEvent(ctx, "actor-human", map[string]any{
		"type": "human_attention_requested", "thread_id": threadID, "refs": []string{"thread:" + threadID},
		"payload": map[string]any{
			"kind": "ask", "title": "Need a decision", "requester_actor_id": targetActorID,
			"subject_ref": "thread:" + threadID, "response_proposals": []string{"Proceed"},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("append ask: %w", err)
	}
	response, _, err := store.AppendHumanAttentionResponse(ctx, "actor-human", fmt.Sprint(ask["id"]), "inbox:"+fmt.Sprint(ask["id"]), "", "", map[string]any{
		"type": "human_attention_responded", "thread_id": threadID, "refs": []string{"event:" + fmt.Sprint(ask["id"]), "thread:" + threadID},
		"payload": map[string]any{
			"request_event_ref": "event:" + fmt.Sprint(ask["id"]), "requester_actor_id": targetActorID,
			"responding_actor_id": "actor-human", "outcome": "answered", "response_text": "Proceed",
			"subject_ref": "thread:" + threadID,
		},
	}, map[string]any{
		"requested": true, "target_actor_id": targetActorID, "target_handle": "agent.one",
		"workspace_id": "ws_test", "thread_id": threadID, "subject_ref": "thread:" + threadID,
		"quiet_window_ns": int64(time.Minute),
	})
	if err != nil {
		return nil, fmt.Errorf("append response: %w", err)
	}
	event, ok := response["event"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("response missing event: %#v", response)
	}
	return event, nil
}

func deliveryTestWakeup(batch HumanAttentionAnswerWakeBatch) AgentWakeup {
	refs := append([]string(nil), batch.Refs...)
	return AgentWakeup{
		WakeupID: "wake-" + batch.BatchID, Status: AgentWakeupStatusRequested,
		TargetActorID: batch.TargetActorID, TargetHandle: batch.TargetHandle,
		WorkspaceID: batch.WorkspaceID, ThreadID: batch.ThreadID,
		TriggerEventID: batch.TriggerEventID, TriggerCreatedAt: batch.TriggerCreatedAt,
		TriggerText: fmt.Sprintf("%d answers are ready", batch.AnswerCount), Refs: refs,
	}
}

func containsStoredRef(refs []string, wanted string) bool {
	for _, ref := range refs {
		if ref == wanted {
			return true
		}
	}
	return false
}
