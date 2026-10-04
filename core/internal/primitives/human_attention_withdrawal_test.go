package primitives

import (
	"context"
	"errors"
	"testing"

	"agent-nexus-core/internal/blob"
	"agent-nexus-core/internal/storage"
)

func TestHumanAttentionWithdrawalResolvesOnlyTheRequestingAgentsOpenAsk(t *testing.T) {
	ctx := context.Background()
	workspace, err := storage.InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer workspace.Close()
	store := NewStore(workspace.DB(), blob.NewFilesystemBackend(workspace.Layout().ArtifactContentDir), workspace.Layout().ArtifactContentDir)
	ask, err := store.AppendEvent(ctx, "human", map[string]any{
		"type": "human_attention_requested", "thread_id": "thread-withdrawal",
		"refs": []string{"thread:thread-withdrawal"},
		"payload": map[string]any{
			"kind": "ask", "title": "Should I continue?", "requester_actor_id": "agent-one",
			"subject_ref": "thread:thread-withdrawal", "response_proposals": []string{"Continue"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	askID := anyStringValue(ask["id"])
	withdrawal := map[string]any{
		"type": "human_attention_withdrawn", "summary": "Ask withdrawn",
		"refs":    []string{"event:" + askID},
		"payload": map[string]any{"reason": "task was superseded"},
	}
	if _, err := store.AppendHumanAttentionWithdrawal(ctx, "agent-two", askID, withdrawal); !errors.Is(err, ErrForbidden) {
		t.Fatalf("another agent could withdraw the ask: %v", err)
	}
	stored, err := store.AppendHumanAttentionWithdrawal(ctx, "agent-one", askID, withdrawal)
	if err != nil {
		t.Fatalf("withdraw own ask: %v", err)
	}
	if got := anyStringValue(stored["type"]); got != "human_attention_withdrawn" {
		t.Fatalf("withdrawal event type=%q", got)
	}
	if got := anyStringValue(asMapForWithdrawalTest(stored["payload"])["reason"]); got != "task was superseded" {
		t.Fatalf("withdrawal reason=%q", got)
	}
	if count, err := store.CountOpenHumanAttentionAsks(ctx, "agent-one"); err != nil || count != 0 {
		t.Fatalf("withdrawn ask still open: count=%d err=%v", count, err)
	}
	if _, err := store.AppendHumanAttentionWithdrawal(ctx, "agent-one", askID, withdrawal); !errors.Is(err, ErrHumanAttentionAlreadyResponded) {
		t.Fatalf("second resolution was accepted: %v", err)
	}
	_, _, err = store.AppendHumanAttentionResponse(ctx, "human", askID, "inbox:"+askID, "", "", map[string]any{
		"type": "human_attention_responded", "thread_id": "thread-withdrawal", "refs": []string{"event:" + askID},
		"payload": map[string]any{"request_event_ref": "event:" + askID, "response_text": "Continue", "outcome": "answered"},
	}, map[string]any{"requested": false})
	if !errors.Is(err, ErrHumanAttentionAlreadyResponded) {
		t.Fatalf("a human answer resolved an already withdrawn ask: %v", err)
	}
	var answered int
	if err := workspace.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM events WHERE type='human_attention_responded'`).Scan(&answered); err != nil || answered != 0 {
		t.Fatalf("withdrawal was recorded as a human answer: count=%d err=%v", answered, err)
	}
}

func asMapForWithdrawalTest(value any) map[string]any {
	if result, ok := value.(map[string]any); ok {
		return result
	}
	return map[string]any{}
}
