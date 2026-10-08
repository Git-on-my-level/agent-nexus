package primitives

import (
	"context"
	"errors"
	"sync"
	"testing"
)

func TestAskSubjectBoardPrivateDefaultAndReplacement(t *testing.T) {
	s, ws, card, _ := askDeliveryFixture(t)
	ctx := context.Background()
	board, err := s.GetBoard(ctx, anyStringValue(card["board_id"]))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.PatchThread(ctx, "requester", anyStringValue(board["thread_id"]), map[string]any{"pm_actor_id": "requester"}, nil); err != nil {
		t.Fatal(err)
	}
	topic, err := s.CreateTopic(ctx, "writer", map[string]any{"title": "Accessible topic", "summary": "Public topic"})
	if err != nil {
		t.Fatal(err)
	}
	scope := WithAccessScope(ctx, AccessScope{ActorID: "writer"})
	if _, err = s.GetBoard(scope, defaultWorkBoardID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("default board should be hidden: %v", err)
	}
	publish := func() string {
		t.Helper()
		ask, err := s.AppendTaskAttentionEvent(scope, "writer", map[string]any{"type": "human_attention_requested", "thread_id": topic.Topic["thread_id"], "refs": []string{anyStringValue(topic.Topic["ref"])}, "payload": map[string]any{"subject_ref": topic.Topic["ref"], "title": "Legacy topic question", "requester_actor_id": "writer"}})
		if err != nil {
			t.Fatal(err)
		}
		work, err := s.GetWork(scope, anyStringValue(asMapValue(ask["payload"])["subject_ref"]))
		if err != nil {
			t.Fatal(err)
		}
		var boardID string
		if err = ws.DB().QueryRow(`SELECT board_id FROM cards WHERE id=?`, work["id"]).Scan(&boardID); err != nil {
			t.Fatal(err)
		}
		if boardID == defaultWorkBoardID {
			t.Fatal("used hidden reserved board")
		}
		return boardID
	}
	first := publish()
	if again := publish(); again != first {
		t.Fatal("did not reuse subject board")
	}
	if _, err = s.PatchThread(ctx, "requester", first, map[string]any{"pm_actor_id": "requester"}, nil); err != nil {
		t.Fatal(err)
	}
	second := publish()
	if second == first {
		t.Fatal("reused newly hidden board")
	}
	if _, err = s.ArchiveBoard(scope, "writer", second); err != nil {
		t.Fatal(err)
	}
	third := publish()
	if third == second || publish() != third {
		t.Fatal("did not replace archived board and reuse replacement")
	}
}

func TestAskSubjectBoardAllocationRollsBackAndSerializes(t *testing.T) {
	s, ws, _, _ := askDeliveryFixture(t)
	ctx := context.Background()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	board, err := ensureAskSubjectBoardTx(ctx, tx, "rollback-writer")
	if err != nil {
		t.Fatal(err)
	}
	if err = tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"boards", "threads"} {
		var count int
		if err = ws.DB().QueryRow(`SELECT count(*) FROM `+table+` WHERE id=?`, board.ID).Scan(&count); err != nil || count != 0 {
			t.Fatalf("orphan %s: %d %v", table, count, err)
		}
	}
	var count int
	if err = ws.DB().QueryRow(`SELECT count(*) FROM ask_subject_boards`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("orphan pointer: %d %v", count, err)
	}
	var wg sync.WaitGroup
	ids := make(chan string, 4)
	errs := make(chan error, 4)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tx, err := s.db.BeginTx(ctx, nil)
			if err != nil {
				errs <- err
				return
			}
			defer tx.Rollback()
			board, err := ensureAskSubjectBoardTx(ctx, tx, "concurrent-writer")
			if err == nil {
				err = tx.Commit()
			}
			if err != nil {
				errs <- err
				return
			}
			ids <- board.ID
		}()
	}
	wg.Wait()
	close(ids)
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	first := ""
	for id := range ids {
		if first != "" && id != first {
			t.Fatal("concurrent allocation created extra boards")
		}
		first = id
	}
}
