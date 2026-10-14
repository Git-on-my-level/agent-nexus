package primitives

import (
	"context"
	"fmt"
	"testing"
)

func TestTopicWorkspaceCanonicalBoardDocumentAndParentMembership(t *testing.T) {
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	s := NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	topic, err := s.CreateTopic(ctx, "owner", map[string]any{"title": "Parent membership", "summary": "Canonical association fixture"})
	if err != nil {
		t.Fatal(err)
	}
	board, err := s.CreateBoard(ctx, "owner", map[string]any{"title": "Parent-only board"})
	if err != nil {
		t.Fatal(err)
	}
	card, err := s.CreateWork(ctx, "owner", anyStringValue(board["id"]), map[string]any{"title": "Parent-only card"})
	if err != nil {
		t.Fatal(err)
	}
	// Imported legacy parent membership uses empty lifecycle scalars. No
	// topic/board ref edge exists to rescue an incorrect membership lookup.
	if _, err := ws.DB().Exec(`UPDATE cards SET parent_thread_id=?,archived_at='',trashed_at='' WHERE id=?`, topic.Topic["thread_id"], card["id"]); err != nil {
		t.Fatal(err)
	}
	doc, _, err := s.CreateDocument(ctx, "owner", map[string]any{"title": "Canonical-only document"}, "context", "text", nil)
	if err != nil {
		t.Fatal(err)
	}
	// Keep boards.refs_json empty: canonical associations are authoritative
	// independently of a stale legacy JSON copy.
	if _, err := ws.DB().Exec(`INSERT INTO ref_edges(id,source_type,source_id,target_type,target_id,edge_type,created_at,metadata_json) VALUES('canonical-board-document','board',?,'document',?,'ref','now','{}')`, board["id"], doc["id"]); err != nil {
		t.Fatal(err)
	}
	page, err := s.TopicWorkspaceResources(WithRequestAccessScope(ctx, AccessScope{ActorID: "reader"}), anyStringValue(topic.Topic["id"]), anyStringValue(topic.Topic["thread_id"]))
	if err != nil {
		t.Fatal(err)
	}
	containsID := func(rows []map[string]any, id any) bool {
		for _, row := range rows {
			if row["id"] == id {
				return true
			}
		}
		return false
	}
	if !containsID(page.Boards, board["id"]) || !containsID(page.Cards, card["id"]) || !containsID(page.Documents, doc["id"]) || !containsID(page.Threads, doc["thread_id"]) {
		t.Fatalf("parent or canonical document association omitted: %#v", page)
	}
	if page.Truncated {
		t.Fatal("small complete fixture was marked truncated")
	}
}

func TestTopicWorkspaceRetainsPrimaryThreadPastThreadCap(t *testing.T) {
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	s := NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	topic, err := s.CreateTopic(ctx, "owner", map[string]any{"title": "Thread cap", "summary": "Primary retention", "thread_id": "zz-primary-retained"})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 101; i++ {
		id := fmt.Sprintf("aa-linked-%03d", i)
		if _, err := s.CreateThread(ctx, "owner", map[string]any{"id": id, "title": id, "type": "thread", "state": "active"}); err != nil {
			t.Fatal(err)
		}
		if _, err := ws.DB().Exec(`INSERT INTO ref_edges(id,source_type,source_id,target_type,target_id,edge_type,created_at,metadata_json) VALUES(?,'topic',?,'thread',?,'ref','now','{}')`, "edge-"+id, topic.Topic["id"], id); err != nil {
			t.Fatal(err)
		}
	}
	page, err := s.TopicWorkspaceResources(WithRequestAccessScope(ctx, AccessScope{ActorID: "reader"}), anyStringValue(topic.Topic["id"]), "zz-primary-retained")
	if err != nil {
		t.Fatal(err)
	}
	if !page.Truncated || len(page.Threads) > 100 {
		t.Fatalf("thread bound/truncation lost: threads=%d truncated=%v", len(page.Threads), page.Truncated)
	}
	for _, thread := range page.Threads {
		if thread["id"] == "zz-primary-retained" {
			return
		}
	}
	t.Fatal("primary thread was omitted because its ID sorts beyond the cap")
}
