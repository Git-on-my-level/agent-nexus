package primitives_test

import (
	"context"
	"errors"
	"organization-autorunner-core/internal/blob"
	"reflect"
	"strings"
	"testing"
	"time"

	"organization-autorunner-core/internal/primitives"
	"organization-autorunner-core/internal/storage"
)

func TestBoardStoreCreateUpdateAndListSummaries(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	workspace, err := storage.InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatalf("initialize workspace: %v", err)
	}
	defer workspace.Close()

	store := primitives.NewStore(workspace.DB(), blob.NewFilesystemBackend(workspace.Layout().ArtifactContentDir), workspace.Layout().ArtifactContentDir)

	primaryThreadID := createBoardTestThread(t, ctx, store, "Primary board thread")
	cardThreadA := createBoardTestThread(t, ctx, store, "Card thread A")
	cardThreadB := createBoardTestThread(t, ctx, store, "Card thread B")
	primaryDocumentID := createBoardTestDocument(t, ctx, store, primaryThreadID, "Primary board doc")

	board, err := store.CreateBoard(ctx, "actor-1", map[string]any{
		"title":         "Operations Board",
		"labels":        []string{"ops", "infra"},
		"owners":        []string{"actor-1"},
		"document_refs": []string{"document:" + primaryDocumentID},
		"pinned_refs":   []string{"thread:" + primaryThreadID},
	})
	if err != nil {
		t.Fatalf("create board: %v", err)
	}

	boardID := board["id"].(string)
	if board["status"] != "active" {
		t.Fatalf("expected default board status active, got %#v", board["status"])
	}
	columnSchema, ok := board["column_schema"].([]map[string]any)
	if !ok || len(columnSchema) != 6 {
		t.Fatalf("expected default six-column schema, got %#v", board["column_schema"])
	}

	if _, err := store.AddBoardCard(ctx, "actor-2", boardID, primitives.AddBoardCardInput{
		ThreadID:  cardThreadA,
		ColumnKey: "backlog",
		DueAt:     pointerString(time.Now().UTC().Add(24 * time.Hour).Format(time.RFC3339)),
	}); err != nil {
		t.Fatalf("add backlog card: %v", err)
	}
	if _, err := store.AddBoardCard(ctx, "actor-2", boardID, primitives.AddBoardCardInput{
		ThreadID:       cardThreadB,
		ColumnKey:      "ready",
		BeforeThreadID: "",
	}); err != nil {
		t.Fatalf("add ready card: %v", err)
	}

	boardThreadID := board["thread_id"].(string)
	putBoardTestProjection(t, ctx, store, boardThreadID, "2099-01-01T00:00:00Z", 2, 1)
	putBoardTestProjection(t, ctx, store, cardThreadA, "2099-01-02T00:00:00Z", 3, 2)
	putBoardTestProjection(t, ctx, store, cardThreadB, "2099-01-03T00:00:00Z", 1, 0)

	listed, _, err := store.ListBoards(ctx, primitives.BoardListFilter{
		Status: "active",
		Label:  "ops",
		Owner:  "actor-1",
	})
	if err != nil {
		t.Fatalf("list boards: %v", err)
	}
	if len(listed) != 1 {
		t.Fatalf("expected 1 listed board, got %d", len(listed))
	}

	summary := listed[0].Summary
	if got := summary["card_count"]; got != 2 {
		t.Fatalf("unexpected card count: %#v", got)
	}
	cardsByColumn, ok := summary["cards_by_column"].(map[string]int)
	if !ok {
		t.Fatalf("expected cards_by_column map[string]int, got %#v", summary["cards_by_column"])
	}
	if cardsByColumn["backlog"] != 1 || cardsByColumn["ready"] != 1 || cardsByColumn["done"] != 0 {
		t.Fatalf("unexpected cards by column: %#v", cardsByColumn)
	}
	if got := summary["at_risk_card_count"]; got != 1 {
		t.Fatalf("unexpected at_risk_card_count: %#v", got)
	}
	if got := summary["document_count"]; got != 3 {
		t.Fatalf("unexpected document count: %#v", got)
	}
	if got := summary["latest_activity_at"]; got != "2099-01-03T00:00:00Z" {
		t.Fatalf("unexpected latest activity: %#v", got)
	}
	if got := summary["has_document_refs"]; got != true {
		t.Fatalf("unexpected has_document_refs: %#v", got)
	}

	initialUpdatedAt := listed[0].Board["updated_at"].(string)
	updated, err := store.UpdateBoard(ctx, "actor-3", boardID, map[string]any{
		"title":  "Operations Board Updated",
		"status": "paused",
		"labels": []string{"ops", "platform"},
		"owners": []string{"actor-3"},
		"refs":   []string{"thread:" + cardThreadA},
	}, &initialUpdatedAt)
	if err != nil {
		t.Fatalf("update board: %v", err)
	}
	if updated["title"] != "Operations Board Updated" || updated["status"] != "paused" {
		t.Fatalf("unexpected updated board: %#v", updated)
	}
	if _, exists := updated["document_refs"]; exists {
		t.Fatalf("expected document refs cleared, got %#v", updated["document_refs"])
	}

	loaded, err := store.GetBoard(ctx, boardID)
	if err != nil {
		t.Fatalf("get board: %v", err)
	}
	if loaded["updated_by"] != "actor-3" {
		t.Fatalf("expected updated_by actor-3, got %#v", loaded["updated_by"])
	}
}

func TestBoardStoreCreateRejectsBoardIDsWithPathSeparators(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	workspace, err := storage.InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatalf("initialize workspace: %v", err)
	}
	defer workspace.Close()

	store := primitives.NewStore(workspace.DB(), blob.NewFilesystemBackend(workspace.Layout().ArtifactContentDir), workspace.Layout().ArtifactContentDir)
	primaryThreadID := createBoardTestThread(t, ctx, store, "Primary board thread")

	_, err = store.CreateBoard(ctx, "actor-1", map[string]any{
		"id":        "board/with-slash",
		"title":     "Invalid Board ID",
		"thread_id": primaryThreadID,
	})
	if !errors.Is(err, primitives.ErrInvalidBoardRequest) {
		t.Fatalf("expected ErrInvalidBoardRequest, got %v", err)
	}
	if err == nil || !strings.Contains(err.Error(), "board.id contains invalid path characters") {
		t.Fatalf("expected invalid path character error, got %v", err)
	}
}

func TestBoardStoreCardOrderingAndMutations(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	workspace, err := storage.InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatalf("initialize workspace: %v", err)
	}
	defer workspace.Close()

	store := primitives.NewStore(workspace.DB(), blob.NewFilesystemBackend(workspace.Layout().ArtifactContentDir), workspace.Layout().ArtifactContentDir)

	primaryThreadID := createBoardTestThread(t, ctx, store, "Primary board thread")
	cardThreadA := createBoardTestThread(t, ctx, store, "Card thread A")
	cardThreadB := createBoardTestThread(t, ctx, store, "Card thread B")
	pinnedDocumentID := createBoardTestDocument(t, ctx, store, cardThreadA, "Pinned card doc")

	board, err := store.CreateBoard(ctx, "actor-1", map[string]any{
		"title":     "Execution Board",
		"thread_id": primaryThreadID,
	})
	if err != nil {
		t.Fatalf("create board: %v", err)
	}
	boardID := board["id"].(string)

	addedA, err := store.AddBoardCard(ctx, "actor-2", boardID, primitives.AddBoardCardInput{
		ThreadID:  cardThreadA,
		ColumnKey: "backlog",
	})
	if err != nil {
		t.Fatalf("add board card A: %v", err)
	}
	sleepBoardTick()

	addedB, err := store.AddBoardCard(ctx, "actor-2", boardID, primitives.AddBoardCardInput{
		ThreadID:       cardThreadB,
		ColumnKey:      "backlog",
		BeforeThreadID: cardThreadA,
	})
	if err != nil {
		t.Fatalf("add board card B: %v", err)
	}
	if addedA.Card["rank"] == addedB.Card["rank"] || addedA.Card["rank"] == "" || addedB.Card["rank"] == "" {
		t.Fatalf("expected opaque server-generated ranks, got A=%#v B=%#v", addedA.Card["rank"], addedB.Card["rank"])
	}

	cards, err := store.ListBoardCards(ctx, boardID)
	if err != nil {
		t.Fatalf("list board cards after add: %v", err)
	}
	if got := boardCardThreadIDs(cards); !reflect.DeepEqual(got, []string{cardThreadB, cardThreadA}) {
		t.Fatalf("unexpected order after add: %#v", got)
	}

	addUpdatedAt := addedB.Board["updated_at"].(string)
	sleepBoardTick()
	movedWithinColumn, err := store.MoveBoardCard(ctx, "actor-3", boardID, cardThreadB, primitives.MoveBoardCardInput{
		ColumnKey:        "backlog",
		AfterThreadID:    cardThreadA,
		IfBoardUpdatedAt: &addUpdatedAt,
	})
	if err != nil {
		t.Fatalf("move within column: %v", err)
	}
	cards, err = store.ListBoardCards(ctx, boardID)
	if err != nil {
		t.Fatalf("list board cards after within-column move: %v", err)
	}
	if got := boardCardThreadIDs(cards); !reflect.DeepEqual(got, []string{cardThreadA, cardThreadB}) {
		t.Fatalf("unexpected order after within-column move: %#v", got)
	}
	if movedWithinColumn.Board["updated_by"] != "actor-3" {
		t.Fatalf("expected board updated_by actor-3 after move, got %#v", movedWithinColumn.Board["updated_by"])
	}

	moveUpdatedAt := movedWithinColumn.Board["updated_at"].(string)
	sleepBoardTick()
	movedAcrossColumns, err := store.MoveBoardCard(ctx, "actor-4", boardID, cardThreadA, primitives.MoveBoardCardInput{
		ColumnKey:        "blocked",
		IfBoardUpdatedAt: &moveUpdatedAt,
	})
	if err != nil {
		t.Fatalf("move across columns: %v", err)
	}
	if movedAcrossColumns.Card["column_key"] != "blocked" {
		t.Fatalf("expected blocked column after cross-column move, got %#v", movedAcrossColumns.Card["column_key"])
	}

	updateUpdatedAt := movedAcrossColumns.Board["updated_at"].(string)
	sleepBoardTick()
	updatedCard, err := store.UpdateBoardCard(ctx, "actor-5", boardID, cardThreadA, primitives.UpdateBoardCardInput{
		PinnedDocumentID: &pinnedDocumentID,
		IfBoardUpdatedAt: &updateUpdatedAt,
	})
	if err != nil {
		t.Fatalf("update board card: %v", err)
	}
	if updatedCard.Card["pinned_document_id"] != pinnedDocumentID {
		t.Fatalf("expected pinned document to be set, got %#v", updatedCard.Card["pinned_document_id"])
	}
	if updatedCard.Board["updated_by"] != "actor-5" {
		t.Fatalf("expected board updated_by actor-5 after card update, got %#v", updatedCard.Board["updated_by"])
	}
	statusDone := "done"
	if _, err := store.UpdateBoardCard(ctx, "actor-stale", boardID, cardThreadA, primitives.UpdateBoardCardInput{
		Status:           &statusDone,
		IfBoardUpdatedAt: &updateUpdatedAt,
	}); !errors.Is(err, primitives.ErrConflict) {
		t.Fatalf("expected stale card update ErrConflict, got %v", err)
	}

	removeUpdatedAt := updatedCard.Board["updated_at"].(string)
	sleepBoardTick()
	removed, err := store.RemoveBoardCard(ctx, "actor-6", boardID, cardThreadB, primitives.RemoveBoardCardInput{
		IfBoardUpdatedAt: &removeUpdatedAt,
	})
	if err != nil {
		t.Fatalf("remove board card: %v", err)
	}
	if removed.RemovedThreadID != cardThreadB {
		t.Fatalf("unexpected removed thread id: %#v", removed.RemovedThreadID)
	}
	if removed.Board["updated_by"] != "actor-6" {
		t.Fatalf("expected board updated_by actor-6 after remove, got %#v", removed.Board["updated_by"])
	}

	cards, err = store.ListBoardCards(ctx, boardID)
	if err != nil {
		t.Fatalf("list board cards after remove: %v", err)
	}
	if got := boardCardThreadIDs(cards); !reflect.DeepEqual(got, []string{cardThreadA}) {
		t.Fatalf("unexpected cards after remove: %#v", got)
	}
	if cards[0]["column_key"] != "blocked" {
		t.Fatalf("expected remaining card in blocked column, got %#v", cards[0]["column_key"])
	}
}

func TestBoardStoreMoveCardResolutionTransitions(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	workspace, err := storage.InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatalf("initialize workspace: %v", err)
	}
	defer workspace.Close()

	store := primitives.NewStore(workspace.DB(), blob.NewFilesystemBackend(workspace.Layout().ArtifactContentDir), workspace.Layout().ArtifactContentDir)

	primaryThreadID := createBoardTestThread(t, ctx, store, "Move primary board thread")
	cardThreadA := createBoardTestThread(t, ctx, store, "Move card thread A")
	cardThreadB := createBoardTestThread(t, ctx, store, "Move card thread B")

	board, err := store.CreateBoard(ctx, "actor-1", map[string]any{
		"title":     "Resolution Board",
		"thread_id": primaryThreadID,
	})
	if err != nil {
		t.Fatalf("create board: %v", err)
	}
	boardID := board["id"].(string)

	addedA, err := store.AddBoardCard(ctx, "actor-2", boardID, primitives.AddBoardCardInput{
		ThreadID:  cardThreadA,
		ColumnKey: "review",
	})
	if err != nil {
		t.Fatalf("add board card A: %v", err)
	}
	firstMoveBoardUpdatedAt := addedA.Board["updated_at"].(string)

	_, err = store.MoveBoardCard(ctx, "actor-3", boardID, cardThreadA, primitives.MoveBoardCardInput{
		ColumnKey:        "done",
		IfBoardUpdatedAt: &firstMoveBoardUpdatedAt,
	})
	if !errors.Is(err, primitives.ErrInvalidBoardRequest) {
		t.Fatalf("expected done move without resolution ErrInvalidBoardRequest, got %v", err)
	}

	_, err = store.MoveBoardCard(ctx, "actor-3", boardID, cardThreadA, primitives.MoveBoardCardInput{
		ColumnKey:        "done",
		Resolution:       stringPtr("done"),
		IfBoardUpdatedAt: &firstMoveBoardUpdatedAt,
	})
	if !errors.Is(err, primitives.ErrInvalidBoardRequest) {
		t.Fatalf("expected terminal move without evidence ErrInvalidBoardRequest, got %v", err)
	}

	evidenceRefs := []string{"event:card-completion-1"}
	movedDone, err := store.MoveBoardCard(ctx, "actor-3", boardID, cardThreadA, primitives.MoveBoardCardInput{
		ColumnKey:        "done",
		Resolution:       stringPtr("done"),
		ResolutionRefs:   &evidenceRefs,
		IfBoardUpdatedAt: &firstMoveBoardUpdatedAt,
	})
	if err != nil {
		t.Fatalf("move done card with evidence: %v", err)
	}
	res, _ := movedDone.Card["resolution"].(string)
	if movedDone.Card["column_key"] != "done" || res != "done" {
		t.Fatalf("unexpected terminal move result: %#v", movedDone.Card)
	}
	if got := movedDone.Card["resolution_refs"]; !reflect.DeepEqual(got, []any{"event:card-completion-1"}) && !reflect.DeepEqual(got, []string{"event:card-completion-1"}) {
		t.Fatalf("unexpected resolution refs after done move: %#v", got)
	}
	afterDoneBoardUpdatedAt := movedDone.Board["updated_at"].(string)

	addedB, err := store.AddBoardCard(ctx, "actor-2", boardID, primitives.AddBoardCardInput{
		ThreadID:         cardThreadB,
		ColumnKey:        "ready",
		IfBoardUpdatedAt: &afterDoneBoardUpdatedAt,
	})
	if err != nil {
		t.Fatalf("add board card B: %v", err)
	}
	afterAddBoardUpdatedAt := addedB.Board["updated_at"].(string)

	cancelRefs := []string{"event:card-canceled-1"}
	movedCanceled, err := store.MoveBoardCard(ctx, "actor-3", boardID, cardThreadB, primitives.MoveBoardCardInput{
		ColumnKey:        "done",
		Resolution:       stringPtr("canceled"),
		ResolutionRefs:   &cancelRefs,
		IfBoardUpdatedAt: &afterAddBoardUpdatedAt,
	})
	if err != nil {
		t.Fatalf("move canceled card with evidence: %v", err)
	}
	if movedCanceled.Card["resolution"] != "canceled" {
		t.Fatalf("unexpected canceled resolution result: %#v", movedCanceled.Card)
	}
}

func TestBoardStoreArchiveBoardCardByGlobalID(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	workspace, err := storage.InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatalf("initialize workspace: %v", err)
	}
	defer workspace.Close()

	store := primitives.NewStore(workspace.DB(), blob.NewFilesystemBackend(workspace.Layout().ArtifactContentDir), workspace.Layout().ArtifactContentDir)

	primaryThreadID := createBoardTestThread(t, ctx, store, "Primary board thread")
	cardThreadID := createBoardTestThread(t, ctx, store, "Card thread")

	board, err := store.CreateBoard(ctx, "actor-1", map[string]any{
		"title":     "Archive Board",
		"thread_id": primaryThreadID,
	})
	if err != nil {
		t.Fatalf("create board: %v", err)
	}
	boardID := board["id"].(string)

	added, err := store.AddBoardCard(ctx, "actor-2", boardID, primitives.AddBoardCardInput{
		ThreadID:  cardThreadID,
		ColumnKey: "review",
	})
	if err != nil {
		t.Fatalf("add board card: %v", err)
	}
	cardID := added.Card["id"].(string)
	updatedAt := added.Board["updated_at"].(string)

	sleepBoardTick()
	archived, err := store.ArchiveBoardCard(ctx, "actor-3", "", cardID, primitives.RemoveBoardCardInput{
		IfBoardUpdatedAt: &updatedAt,
	})
	if err != nil {
		t.Fatalf("archive board card by global id: %v", err)
	}
	if archived.Card["id"] != cardID {
		t.Fatalf("expected archived card id %q, got %#v", cardID, archived.Card["id"])
	}
	if archived.Card["board_id"] != boardID {
		t.Fatalf("expected archived board id %q, got %#v", boardID, archived.Card["board_id"])
	}
	archivedAt, _ := archived.Card["archived_at"].(string)
	if strings.TrimSpace(archivedAt) == "" {
		t.Fatalf("expected archived_at on archived card, got %#v", archived.Card["archived_at"])
	}
	if archived.Board["updated_by"] != "actor-3" {
		t.Fatalf("expected board updated_by actor-3 after archive, got %#v", archived.Board["updated_by"])
	}

	cards, err := store.ListBoardCards(ctx, boardID)
	if err != nil {
		t.Fatalf("list board cards after archive: %v", err)
	}
	if len(cards) != 0 {
		t.Fatalf("expected archived card to disappear from active list, got %#v", cards)
	}
}

func TestBoardStoreRejectsArchivedCardMutations(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	workspace, err := storage.InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatalf("initialize workspace: %v", err)
	}
	defer workspace.Close()

	store := primitives.NewStore(workspace.DB(), blob.NewFilesystemBackend(workspace.Layout().ArtifactContentDir), workspace.Layout().ArtifactContentDir)

	primaryThreadID := createBoardTestThread(t, ctx, store, "Primary board thread")
	cardThreadID := createBoardTestThread(t, ctx, store, "Card thread")

	board, err := store.CreateBoard(ctx, "actor-1", map[string]any{
		"title":     "Archived Mutation Board",
		"thread_id": primaryThreadID,
	})
	if err != nil {
		t.Fatalf("create board: %v", err)
	}
	boardID := board["id"].(string)

	added, err := store.AddBoardCard(ctx, "actor-2", boardID, primitives.AddBoardCardInput{
		ThreadID:  cardThreadID,
		ColumnKey: "ready",
	})
	if err != nil {
		t.Fatalf("add board card: %v", err)
	}
	cardID := added.Card["id"].(string)
	updatedAt := added.Board["updated_at"].(string)

	sleepBoardTick()
	archived, err := store.ArchiveBoardCard(ctx, "actor-3", "", cardID, primitives.RemoveBoardCardInput{
		IfBoardUpdatedAt: &updatedAt,
	})
	if err != nil {
		t.Fatalf("archive board card: %v", err)
	}
	archivedUpdatedAt := archived.Board["updated_at"].(string)
	statusDone := "done"

	if _, err := store.UpdateBoardCard(ctx, "actor-4", boardID, cardThreadID, primitives.UpdateBoardCardInput{
		Status:           &statusDone,
		IfBoardUpdatedAt: &archivedUpdatedAt,
	}); !errors.Is(err, primitives.ErrInvalidBoardRequest) {
		t.Fatalf("expected archived card update ErrInvalidBoardRequest, got %v", err)
	}

	if _, err := store.MoveBoardCard(ctx, "actor-4", boardID, cardThreadID, primitives.MoveBoardCardInput{
		ColumnKey:        "done",
		IfBoardUpdatedAt: &archivedUpdatedAt,
	}); !errors.Is(err, primitives.ErrInvalidBoardRequest) {
		t.Fatalf("expected archived card move ErrInvalidBoardRequest, got %v", err)
	}
}

func TestBoardStoreRejectsMixedPlacementAnchorTypes(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	workspace, err := storage.InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatalf("initialize workspace: %v", err)
	}
	defer workspace.Close()

	store := primitives.NewStore(workspace.DB(), blob.NewFilesystemBackend(workspace.Layout().ArtifactContentDir), workspace.Layout().ArtifactContentDir)

	primaryThreadID := createBoardTestThread(t, ctx, store, "Primary board thread")
	cardThreadA := createBoardTestThread(t, ctx, store, "Card thread A")
	cardThreadB := createBoardTestThread(t, ctx, store, "Card thread B")
	cardThreadC := createBoardTestThread(t, ctx, store, "Card thread C")

	board, err := store.CreateBoard(ctx, "actor-1", map[string]any{
		"title":     "Anchor Board",
		"thread_id": primaryThreadID,
	})
	if err != nil {
		t.Fatalf("create board: %v", err)
	}
	boardID := board["id"].(string)

	addedA, err := store.AddBoardCard(ctx, "actor-2", boardID, primitives.AddBoardCardInput{
		ThreadID:  cardThreadA,
		ColumnKey: "backlog",
	})
	if err != nil {
		t.Fatalf("add board card A: %v", err)
	}
	addedB, err := store.AddBoardCard(ctx, "actor-2", boardID, primitives.AddBoardCardInput{
		ThreadID:  cardThreadB,
		ColumnKey: "backlog",
	})
	if err != nil {
		t.Fatalf("add board card B: %v", err)
	}
	updatedAt := addedB.Board["updated_at"].(string)

	if _, err := store.CreateBoardCard(ctx, "actor-3", boardID, primitives.AddBoardCardInput{
		Title:            "Card C",
		ParentThreadID:   cardThreadC,
		ColumnKey:        "backlog",
		BeforeCardID:     addedA.Card["id"].(string),
		AfterThreadID:    cardThreadB,
		IfBoardUpdatedAt: &updatedAt,
	}); !errors.Is(err, primitives.ErrInvalidBoardRequest) {
		t.Fatalf("expected mixed-anchor create ErrInvalidBoardRequest, got %v", err)
	}

	if _, err := store.MoveBoardCard(ctx, "actor-3", boardID, cardThreadA, primitives.MoveBoardCardInput{
		ColumnKey:        "backlog",
		BeforeCardID:     addedB.Card["id"].(string),
		AfterThreadID:    cardThreadB,
		IfBoardUpdatedAt: &updatedAt,
	}); !errors.Is(err, primitives.ErrInvalidBoardRequest) {
		t.Fatalf("expected mixed-anchor move ErrInvalidBoardRequest, got %v", err)
	}
}

func TestBoardStoreMembershipValidationAndLookup(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	workspace, err := storage.InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatalf("initialize workspace: %v", err)
	}
	defer workspace.Close()

	store := primitives.NewStore(workspace.DB(), blob.NewFilesystemBackend(workspace.Layout().ArtifactContentDir), workspace.Layout().ArtifactContentDir)

	primaryThreadA := createBoardTestThread(t, ctx, store, "Board A primary thread")
	primaryThreadB := createBoardTestThread(t, ctx, store, "Board B primary thread")
	memberThread := createBoardTestThread(t, ctx, store, "Shared member thread")

	boardA, err := store.CreateBoard(ctx, "actor-1", map[string]any{
		"title":     "Board A",
		"thread_id": primaryThreadA,
	})
	if err != nil {
		t.Fatalf("create board A: %v", err)
	}
	boardB, err := store.CreateBoard(ctx, "actor-1", map[string]any{
		"title":     "Board B",
		"thread_id": primaryThreadB,
	})
	if err != nil {
		t.Fatalf("create board B: %v", err)
	}

	boardAID := boardA["id"].(string)
	boardBID := boardB["id"].(string)

	if _, err := store.AddBoardCard(ctx, "actor-2", boardAID, primitives.AddBoardCardInput{
		ThreadID: memberThread,
	}); err != nil {
		t.Fatalf("add member thread to board A: %v", err)
	}
	if _, err := store.AddBoardCard(ctx, "actor-2", boardBID, primitives.AddBoardCardInput{
		ThreadID:  memberThread,
		ColumnKey: "review",
	}); err != nil {
		t.Fatalf("add member thread to board B: %v", err)
	}

	if _, err := store.AddBoardCard(ctx, "actor-3", boardAID, primitives.AddBoardCardInput{
		ThreadID: memberThread,
	}); !errors.Is(err, primitives.ErrConflict) {
		t.Fatalf("expected duplicate membership ErrConflict, got %v", err)
	}

	if _, err := store.AddBoardCard(ctx, "actor-3", boardAID, primitives.AddBoardCardInput{
		ThreadID: boardA["thread_id"].(string),
	}); !errors.Is(err, primitives.ErrInvalidBoardRequest) {
		t.Fatalf("expected board thread rejection, got %v", err)
	}

	memberships, err := store.ListBoardMembershipsByThread(ctx, memberThread)
	if err != nil {
		t.Fatalf("list board memberships by thread: %v", err)
	}
	if len(memberships) != 2 {
		t.Fatalf("expected 2 memberships, got %d", len(memberships))
	}

	gotBoardIDs := []string{
		memberships[0].Board["id"].(string),
		memberships[1].Board["id"].(string),
	}
	wantBoardIDs := []string{boardAID, boardBID}
	if !sameStringSet(gotBoardIDs, wantBoardIDs) {
		t.Fatalf("unexpected membership board ids: got %#v want %#v", gotBoardIDs, wantBoardIDs)
	}

	columnsByBoard := map[string]string{}
	threadIDsByBoard := map[string]string{}
	for _, membership := range memberships {
		columnsByBoard[membership.Board["id"].(string)] = membership.Card["column_key"].(string)
		threadIDsByBoard[membership.Board["id"].(string)] = membership.Card["parent_thread"].(string)
	}
	if columnsByBoard[boardAID] != "backlog" || columnsByBoard[boardBID] != "review" {
		t.Fatalf("unexpected membership card columns: %#v", columnsByBoard)
	}
	if threadIDsByBoard[boardAID] != memberThread || threadIDsByBoard[boardBID] != memberThread {
		t.Fatalf("unexpected membership card thread ids: %#v", threadIDsByBoard)
	}
}

func TestBoardCardRiskPersistAndUpdate(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	workspace, err := storage.InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatalf("initialize workspace: %v", err)
	}
	defer workspace.Close()

	store := primitives.NewStore(workspace.DB(), blob.NewFilesystemBackend(workspace.Layout().ArtifactContentDir), workspace.Layout().ArtifactContentDir)

	primaryThread := createBoardTestThread(t, ctx, store, "Risk board primary")
	cardThread := createBoardTestThread(t, ctx, store, "Risk card thread")
	board, err := store.CreateBoard(ctx, "actor-1", map[string]any{
		"title":     "Risk board",
		"thread_id": primaryThread,
	})
	if err != nil {
		t.Fatalf("create board: %v", err)
	}
	boardID := board["id"].(string)

	high := "high"
	added, err := store.AddBoardCard(ctx, "actor-2", boardID, primitives.AddBoardCardInput{
		ThreadID:  cardThread,
		ColumnKey: "backlog",
		Risk:      &high,
	})
	if err != nil {
		t.Fatalf("add card: %v", err)
	}
	if got := added.Card["risk"]; got != "high" {
		t.Fatalf("expected risk high on create, got %#v", got)
	}

	cardID := added.Card["id"].(string)
	medium := "medium"
	updated, err := store.UpdateBoardCard(ctx, "actor-2", boardID, cardID, primitives.UpdateBoardCardInput{
		Risk: &medium,
	})
	if err != nil {
		t.Fatalf("update risk: %v", err)
	}
	if got := updated.Card["risk"]; got != "medium" {
		t.Fatalf("expected risk medium after update, got %#v", got)
	}

	low := "low"
	updated2, err := store.UpdateBoardCard(ctx, "actor-2", boardID, cardID, primitives.UpdateBoardCardInput{
		Risk: &low,
	})
	if err != nil {
		t.Fatalf("update risk low: %v", err)
	}
	if got := updated2.Card["risk"]; got != "low" {
		t.Fatalf("expected risk low, got %#v", got)
	}

	addedDefault, err := store.AddBoardCard(ctx, "actor-2", boardID, primitives.AddBoardCardInput{
		ThreadID:  createBoardTestThread(t, ctx, store, "Risk default thread"),
		ColumnKey: "ready",
	})
	if err != nil {
		t.Fatalf("add default-risk card: %v", err)
	}
	if got := addedDefault.Card["risk"]; got != "low" {
		t.Fatalf("expected default risk low, got %#v", got)
	}
}

func createBoardTestThread(t *testing.T, ctx context.Context, store *primitives.Store, title string) string {
	t.Helper()

	result, err := store.CreateThread(ctx, "actor-1", map[string]any{
		"title":           title,
		"type":            "incident",
		"status":          "active",
		"priority":        "p1",
		"tags":            []string{},
		"cadence":         "reactive",
		"current_summary": title,
		"next_actions":    []string{},
		"key_artifacts":   []string{},
		"provenance":      map[string]any{"sources": []string{"inferred"}},
	})
	if err != nil {
		t.Fatalf("create board test thread %q: %v", title, err)
	}

	threadID, _ := result.Thread["id"].(string)
	if threadID == "" {
		t.Fatalf("expected thread id for %q", title)
	}
	return threadID
}

func createBoardTestDocument(t *testing.T, ctx context.Context, store *primitives.Store, threadID string, title string) string {
	t.Helper()

	document, _, err := store.CreateDocument(ctx, "actor-1", map[string]any{
		"thread_id": threadID,
		"title":     title,
	}, "# "+title, "text", nil)
	if err != nil {
		t.Fatalf("create board test document %q: %v", title, err)
	}

	documentID, _ := document["id"].(string)
	if documentID == "" {
		t.Fatalf("expected document id for %q", title)
	}
	return documentID
}

func putBoardTestProjection(t *testing.T, ctx context.Context, store *primitives.Store, threadID, lastActivityAt string, openCardCount, documentCount int) {
	t.Helper()

	if err := store.PutDerivedTopicProjection(ctx, primitives.DerivedTopicProjection{
		ThreadID:       threadID,
		LastActivityAt: lastActivityAt,
		OpenCardCount:  openCardCount,
		DocumentCount:  documentCount,
	}); err != nil {
		t.Fatalf("put derived projection for %s: %v", threadID, err)
	}
}

func boardCardThreadIDs(cards []map[string]any) []string {
	out := make([]string, 0, len(cards))
	for _, card := range cards {
		threadID, _ := card["parent_thread"].(string)
		out = append(out, threadID)
	}
	return out
}

func pointerString(value string) *string {
	return &value
}

func sameStringSet(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	counts := map[string]int{}
	for _, value := range left {
		counts[value]++
	}
	for _, value := range right {
		counts[value]--
		if counts[value] < 0 {
			return false
		}
	}
	for _, count := range counts {
		if count != 0 {
			return false
		}
	}
	return true
}

func sleepBoardTick() {
	time.Sleep(2 * time.Millisecond)
}

func stringPtr(value string) *string {
	return &value
}
