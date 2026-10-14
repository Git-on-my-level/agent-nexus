package primitives

import (
	"context"
	"errors"
	"testing"
)

func TestHomeUnreadPageAdvancesAcrossReadGroups(t *testing.T) {
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	s := NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	a, err := s.CreateTopic(ctx, "writer", map[string]any{"title": "Already read", "summary": "Read-state control"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.CreateTopic(ctx, "writer", map[string]any{"title": "Still unread", "summary": "Unread control"})
	if err != nil {
		t.Fatal(err)
	}
	var newest map[string]any
	for _, topic := range []map[string]any{b.Topic, b.Topic, a.Topic} {
		newest, err = s.AppendEvent(ctx, "writer", map[string]any{"type": "message_posted", "thread_id": topic["thread_id"], "refs": []string{"topic:" + anyStringValue(topic["id"])}, "payload": map[string]any{"body": "hello"}})
		if err != nil {
			t.Fatal(err)
		}
	}
	ref := "topic:" + anyStringValue(a.Topic["id"])
	if err = s.MarkHomeReadAt(ctx, "reader", []string{ref}, map[string]EventCursor{ref: {TS: anyStringValue(newest["ts"]), ID: anyStringValue(newest["id"])}}); err != nil {
		t.Fatal(err)
	}
	first, err := s.ListHomeUnreadPage(ctx, "reader", "", 1)
	if err != nil || len(first.Groups) != 0 || first.NextCursor == "" {
		t.Fatalf("read page must advance: %+v err=%v", first, err)
	}
	second, err := s.ListHomeUnreadPage(ctx, "reader", first.NextCursor, 1)
	if err != nil || len(second.Groups) != 1 || second.UnreadCount != 1 || second.NextCursor == "" {
		t.Fatalf("second page: %+v err=%v", second, err)
	}
	third, err := s.ListHomeUnreadPage(ctx, "reader", second.NextCursor, 1)
	if err != nil || len(third.Groups) != 1 || third.UnreadCount != 1 || third.Groups[0].GroupRef != second.Groups[0].GroupRef {
		t.Fatalf("recurring group lost: %+v err=%v", third, err)
	}
	if _, err = s.ListHomeUnreadPage(ctx, "reader", "invalid cursor", 1); !errors.Is(err, ErrInvalidCursor) {
		t.Fatalf("invalid cursor err=%v", err)
	}
	if _, err = s.PatchThread(ctx, "writer", anyStringValue(b.Topic["thread_id"]), map[string]any{"pm_actor_id": "writer"}, nil); err != nil {
		t.Fatal(err)
	}
	hidden, err := s.ListHomeUnreadPage(WithRequestAccessScope(ctx, AccessScope{ActorID: "reader"}), "reader", "", 200)
	if err != nil || hidden.UnreadCount != 0 {
		t.Fatalf("private group exposed: %+v err=%v", hidden, err)
	}
}
