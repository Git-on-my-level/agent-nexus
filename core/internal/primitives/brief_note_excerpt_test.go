package primitives_test

import (
	"context"
	"testing"

	"agent-nexus-core/internal/primitives"
)

func TestBriefNoteExcerptReadsNoteText(t *testing.T) {
	t.Parallel()
	ws, err := initializeTestWorkspace(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ws.Close() })
	s := primitives.NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	ctx := context.Background()
	board, err := s.CreateBoard(ctx, "actor-1", map[string]any{"title": "Portfolio"})
	if err != nil {
		t.Fatal(err)
	}
	card := registerWork(t, s, board["id"].(string))
	thread := card["thread_id"].(string)
	for _, payload := range []map[string]any{{"text": "from text"}, {"body": "from body"}, {"text": "", "body": "body after empty text"}, {}} {
		if _, err := s.AppendEvent(ctx, "agent-1", map[string]any{"type": "message_posted", "thread_id": thread, "summary": "from summary", "refs": []string{}, "provenance": map[string]any{"sources": []string{"actor_statement:test"}}, "payload": payload}); err != nil {
			t.Fatal(err)
		}
	}
	rows, err := ws.DB().QueryContext(ctx, `SELECT excerpt FROM brief_notes WHERE card_id=? ORDER BY ts, rowid`, card["id"])
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var excerpt string
		if err := rows.Scan(&excerpt); err != nil {
			t.Fatal(err)
		}
		got = append(got, excerpt)
	}
	want := []string{"from text", "from body", "body after empty text", "from summary"}
	if len(got) != len(want) {
		t.Fatalf("excerpts=%q", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("excerpts=%q want %q", got, want)
		}
	}
}
