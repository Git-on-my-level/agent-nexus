package primitives

import (
	"context"
	"errors"
	"sync"
	"testing"
)

// The stage boundary makes the request-ended busy classification retry-safe:
// a cancellation before BEGIN IMMEDIATE acquires the writer stays raw (nothing
// can have committed), and a cancellation after the transaction holds the
// writer must surface as outcome_unknown because the process can no longer
// prove the change did not apply.
func testString(value any) string {
	s, _ := value.(string)
	return s
}

func TestBoardCardMoveOutcomeStageClassification(t *testing.T) {
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	s := NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	board, err := s.CreateBoard(ctx, "actor-1", map[string]any{"title": "Stage board"})
	if err != nil {
		t.Fatal(err)
	}
	card, err := s.CreateWork(ctx, "actor-1", testString(board["id"]), map[string]any{"title": "Stage card", "phase": "blocked"})
	if err != nil {
		t.Fatal(err)
	}
	cardID := testString(card["id"])
	moveErr := func(moveCtx context.Context) error {
		_, err := s.MoveBoardCard(moveCtx, "actor-1", "", cardID, MoveBoardCardInput{ColumnKey: "in_progress"})
		return err
	}

	t.Run("cancel before begin stays raw", func(t *testing.T) {
		moveCtx, cancel := context.WithCancel(ctx)
		cancel()
		err := moveErr(moveCtx)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("want context.Canceled, got %v", err)
		}
		var outcome *MutationOutcomeUnknown
		if errors.As(err, &outcome) {
			t.Fatalf("pre-begin cancel must stay raw, got outcome_unknown: %v", err)
		}
	})

	t.Run("cancel after begin is outcome_unknown", func(t *testing.T) {
		orig := mutationTxBegin
		t.Cleanup(func() { mutationTxBegin = orig })
		var once sync.Once
		moveCtx, cancel := context.WithCancel(ctx)
		defer cancel()
		mutationTxBegin = func(ctx context.Context, db *accessDB) (*accessTx, error) {
			tx, err := db.BeginTx(ctx, nil)
			if err == nil {
				once.Do(cancel)
			}
			return tx, err
		}
		err := moveErr(moveCtx)
		var outcome *MutationOutcomeUnknown
		if !errors.As(err, &outcome) {
			t.Fatalf("post-begin cancel: want outcome_unknown, got %v", err)
		}
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("outcome_unknown must keep the cause chain, got %v", err)
		}
	})

	t.Run("failed attempts leave the card unchanged", func(t *testing.T) {
		after, err := s.GetWork(ctx, testString(card["ref"]))
		if err != nil {
			t.Fatal(err)
		}
		if after["phase"] != card["phase"] || after["version"] != card["version"] {
			t.Fatalf("canceled moves changed state: before=%v after=%v", card, after)
		}
	})

	// Deterministic rejections stay typed even when they are detected
	// post-begin: archiving a trashed card must keep answering the
	// contract-documented already_trashed, not outcome_unknown.
	t.Run("archiving a trashed card stays already_trashed", func(t *testing.T) {
		if _, err := s.TrashBoardCard(ctx, "actor-1", "", cardID, "cleanup", RemoveBoardCardInput{}); err != nil {
			t.Fatal(err)
		}
		_, err := s.ArchiveBoardCard(ctx, "actor-1", "", cardID, RemoveBoardCardInput{})
		if !errors.Is(err, ErrAlreadyTrashed) {
			t.Fatalf("want ErrAlreadyTrashed, got %v", err)
		}
		var outcome *MutationOutcomeUnknown
		if errors.As(err, &outcome) {
			t.Fatalf("deterministic rejection wrapped as outcome_unknown: %v", err)
		}
	})
}
