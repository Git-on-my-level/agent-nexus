package primitives

import (
	"agent-nexus-core/internal/storage"
	"context"
	"strings"
	"testing"
	"time"
)

func TestInboxReadPublicSubject(t *testing.T) {
	ctx := context.Background()
	ws, err := storage.InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	s := NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	thread, err := s.CreateThread(ctx, "actor", map[string]any{"title": "Public"})
	if err != nil {
		t.Fatal(err)
	}
	id := thread.Thread["id"].(string)
	if err = s.ReplaceDerivedInboxItems(ctx, id, []DerivedInboxItem{{ID: "ask", ThreadID: id, Category: "ask", TriggerAt: time.Now().UTC().Format(time.RFC3339Nano), Data: map[string]any{"subject_ref": "thread:" + id, "kind": "ask"}}}); err != nil {
		t.Fatal(err)
	}
	// Unrelated private resources must never suppress this public thread ask.
	board, err := s.CreateBoard(ctx, "owner", map[string]any{"title": "Private board"})
	if err != nil {
		t.Fatal(err)
	}
	card, err := s.CreateWork(ctx, "owner", board["id"].(string), map[string]any{"title": "Private card"})
	if err != nil {
		t.Fatal(err)
	}
	for _, tid := range []string{board["thread_id"].(string), card["thread_id"].(string)} {
		if _, err = s.PatchThread(ctx, "owner", tid, map[string]any{"pm_actor_id": "owner"}, nil); err != nil {
			t.Fatal(err)
		}
	}
	scoped := WithAccessScope(ctx, AccessScope{ActorID: "reader"})
	limit := 1
	items, count, err := s.ReadInbox(scoped, InboxReadOptions{AsksOnly: true, Limit: &limit})
	if err != nil || count != 1 || len(items) != 1 {
		t.Fatalf("public ask: count=%d items=%v err=%v", count, items, err)
	}
}

func TestInboxReadRetainsArchivedLegacyRequestsWithSubjectAuthorization(t *testing.T) {
	ctx := context.Background()
	ws, err := storage.InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	s := NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	thread, err := s.CreateThread(ctx, "owner", map[string]any{"title": "Public requests"})
	if err != nil {
		t.Fatal(err)
	}
	threadID := thread.Thread["id"].(string)
	items := []DerivedInboxItem{}
	for _, private := range []bool{false, true} {
		board, err := s.CreateBoard(ctx, "owner", map[string]any{"title": "Archived context"})
		if err != nil {
			t.Fatal(err)
		}
		if private {
			if _, err := s.PatchThread(ctx, "owner", board["thread_id"].(string), map[string]any{"pm_actor_id": "owner"}, nil); err != nil {
				t.Fatal(err)
			}
		}
		card, err := s.CreateWork(ctx, "owner", board["id"].(string), map[string]any{"title": "Subject"})
		if err != nil {
			t.Fatal(err)
		}
		for _, kind := range []string{"ask", "review", "escalate", "agent_wake"} {
			id := kind + "-public"
			if private {
				id = kind + "-private"
			}
			// Legacy rows have no source_event_id; the request's own lifecycle
			// still governs retention, while the archived subject governs access.
			items = append(items, DerivedInboxItem{ID: id, ThreadID: threadID, Category: kind,
				TriggerAt: time.Now().UTC().Format(time.RFC3339Nano),
				Data:      map[string]any{"kind": kind, "subject_ref": card["ref"], "related_refs": []any{card["ref"]}},
			})
		}
		if _, err := s.ArchiveBoard(ctx, "owner", board["id"].(string)); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.ReplaceDerivedInboxItems(ctx, threadID, items); err != nil {
		t.Fatal(err)
	}
	for _, actor := range []string{"reader", "owner"} {
		scoped := WithAccessScope(ctx, AccessScope{ActorID: actor})
		got, count, err := s.ReadInbox(scoped, InboxReadOptions{})
		want := 3
		if actor == "owner" {
			want = 6
		}
		if err != nil || len(got) != want || count != want {
			t.Fatalf("%s: count=%d items=%v err=%v", actor, count, got, err)
		}
		for _, item := range got {
			if item.Category == "agent_wake" || (actor == "reader" && strings.HasSuffix(item.ID, "-private")) {
				t.Fatalf("%s received hidden item: %+v", actor, item)
			}
		}
		for _, limit := range []int{0, 1} {
			asks, count, err := s.ReadInbox(scoped, InboxReadOptions{AsksOnly: true, Limit: &limit})
			wantAsks := want / 3
			if err != nil || count != wantAsks || len(asks) != limit {
				t.Fatalf("%s limit=%d: count=%d items=%v err=%v", actor, limit, count, asks, err)
			}
		}
	}
}

func TestInboxNotificationLifecycleChecksEveryReferenceInput(t *testing.T) {
	ctx := context.Background()
	ws, err := storage.InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	s := NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	thread, err := s.CreateThread(ctx, "owner", map[string]any{"title": "Public notifications"})
	if err != nil {
		t.Fatal(err)
	}
	threadID := thread.Thread["id"].(string)
	board, err := s.CreateBoard(ctx, "owner", map[string]any{"title": "Archived context"})
	if err != nil {
		t.Fatal(err)
	}
	card, err := s.CreateWork(ctx, "owner", board["id"].(string), map[string]any{"title": "Archived subject"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.ArchiveBoard(ctx, "owner", board["id"].(string)); err != nil {
		t.Fatal(err)
	}
	archivedThread, err := s.CreateThread(ctx, "owner", map[string]any{"title": "Archived thread"})
	if err != nil {
		t.Fatal(err)
	}
	archivedThreadID := archivedThread.Thread["id"].(string)
	if _, err = s.ArchiveThread(ctx, "owner", archivedThreadID); err != nil {
		t.Fatal(err)
	}
	newItem := func(id string) DerivedInboxItem {
		return DerivedInboxItem{ID: id, ThreadID: threadID, Category: "agent_wake", TriggerAt: time.Now().UTC().Format(time.RFC3339Nano), Data: map[string]any{"kind": "agent_wake", "subject_ref": "thread:" + threadID}}
	}
	items := []DerivedInboxItem{newItem("public")}
	for _, input := range []string{"subject", "source_card", "related", "legacy"} {
		item := newItem(input)
		switch input {
		case "subject":
			item.Data["subject_ref"] = " \t" + card["ref"].(string) + "\t "
		case "source_card":
			item.SourceCardID = card["id"].(string)
		case "related":
			item.Data["related_refs"] = []string{card["ref"].(string)}
		case "legacy":
			item.Data["refs"] = []string{card["ref"].(string)}
		}
		items = append(items, item)
	}
	request := newItem("retained-request")
	request.Category, request.Data["kind"], request.Data["subject_ref"] = "ask", "ask", card["ref"]
	items = append(items, request)
	if err = s.ReplaceDerivedInboxItems(ctx, threadID, items); err != nil {
		t.Fatal(err)
	}
	archived := newItem("archived-thread")
	archived.ThreadID = archivedThreadID
	// The public subject makes the backing thread the sole hidden input.
	if err = s.ReplaceDerivedInboxItems(ctx, archivedThreadID, []DerivedInboxItem{archived}); err != nil {
		t.Fatal(err)
	}
	reader := WithAccessScope(ctx, AccessScope{ActorID: "reader"})
	got, count, err := s.ReadInbox(reader, InboxReadOptions{})
	if err != nil || count != 2 || len(got) != 2 {
		t.Fatalf("count=%d items=%v err=%v", count, got, err)
	}
	for _, item := range got {
		if item.ID != "public" && item.ID != "retained-request" {
			t.Fatalf("notification with archived input returned: %+v", item)
		}
	}
}
