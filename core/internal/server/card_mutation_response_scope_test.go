package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"agent-nexus-core/internal/auth"
	"agent-nexus-core/internal/primitives"
)

// The hook runs after the real store mutation commits and before the handler
// builds its response. It models a competing transaction without timing sleeps.
type mutationResponseRaceStore struct {
	*primitives.Store
	afterCommit func(primitives.BoardCardMutationResult) error
	hookNoop    bool
	calls       int
	afterEvents bool
	pending     primitives.BoardCardMutationResult
}

func (s *mutationResponseRaceStore) after(result primitives.BoardCardMutationResult, err error) error {
	if err != nil {
		return err
	}
	if s.afterEvents {
		s.pending = result
		return nil
	}
	s.calls++
	return s.afterCommit(result)
}

func (s *mutationResponseRaceStore) AppendEvent(ctx context.Context, actor string, event map[string]any) (map[string]any, error) {
	stored, err := s.Store.AppendEvent(ctx, actor, event)
	// Cross-board moves require their audit events. Revoke visibility after
	// the final event commits, before the response projection starts.
	if err == nil && s.afterEvents && event["type"] == "card_updated" {
		s.afterEvents = false
		err = s.after(s.pending, nil)
	}
	return stored, err
}

func (s *mutationResponseRaceStore) GetBoard(ctx context.Context, id string) (map[string]any, error) {
	board, err := s.Store.GetBoard(ctx, id)
	if err == nil && s.hookNoop {
		err = s.after(primitives.BoardCardMutationResult{Board: board}, nil)
	}
	return board, err
}

func (s *mutationResponseRaceStore) UpdateBoardCard(ctx context.Context, actor, board, card string, input primitives.UpdateBoardCardInput) (primitives.BoardCardMutationResult, error) {
	result, err := s.Store.UpdateBoardCard(ctx, actor, board, card, input)
	return result, s.after(result, err)
}

func (s *mutationResponseRaceStore) MoveBoardCard(ctx context.Context, actor, board, card string, input primitives.MoveBoardCardInput) (primitives.BoardCardMutationResult, error) {
	result, err := s.Store.MoveBoardCard(ctx, actor, board, card, input)
	return result, s.after(result, err)
}

func (s *mutationResponseRaceStore) ArchiveBoardCard(ctx context.Context, actor, board, card string, input primitives.RemoveBoardCardInput) (primitives.BoardCardMutationResult, error) {
	result, err := s.Store.ArchiveBoardCard(ctx, actor, board, card, input)
	return result, s.after(result, err)
}

func (s *mutationResponseRaceStore) RestoreArchivedBoardCard(ctx context.Context, actor, board, card string, input primitives.RemoveBoardCardInput) (primitives.BoardCardMutationResult, error) {
	result, err := s.Store.RestoreArchivedBoardCard(ctx, actor, board, card, input)
	return result, s.after(result, err)
}

func (s *mutationResponseRaceStore) TrashBoardCard(ctx context.Context, actor, board, card, reason string, input primitives.RemoveBoardCardInput) (primitives.BoardCardMutationResult, error) {
	result, err := s.Store.TrashBoardCard(ctx, actor, board, card, reason, input)
	return result, s.after(result, err)
}
func (s *mutationResponseRaceStore) RemoveBoardCard(ctx context.Context, actor, board, card string, input primitives.RemoveBoardCardInput) (primitives.BoardCardRemovalResult, error) {
	result, err := s.Store.RemoveBoardCard(ctx, actor, board, card, input)
	return result, s.after(primitives.BoardCardMutationResult{Board: result.Board, Card: result.Card}, err)
}
func (s *mutationResponseRaceStore) CreateCardRevision(ctx context.Context, actor, card string, input primitives.CreateCardRevisionInput) (primitives.BoardCardMutationResult, map[string]any, error) {
	result, revision, err := s.Store.CreateCardRevision(ctx, actor, card, input)
	return result, revision, s.after(result, err)
}

func (s *mutationResponseRaceStore) CreateBoardCard(ctx context.Context, actor, board string, input primitives.AddBoardCardInput) (primitives.BoardCardMutationResult, error) {
	result, err := s.Store.CreateBoardCard(ctx, actor, board, input)
	return result, s.after(result, err)
}

func (s *mutationResponseRaceStore) CreateBoardCardsBatch(ctx context.Context, actor, board string, token *string, inputs []primitives.AddBoardCardInput) ([]primitives.BoardCardMutationResult, error) {
	results, err := s.Store.CreateBoardCardsBatch(ctx, actor, board, token, inputs)
	if err == nil {
		err = s.after(results[len(results)-1], nil)
	}
	return results, err
}

func assertIdentityOnlyMutationResponse(t *testing.T, out *httptest.ResponseRecorder, wantKeys []string) {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(out.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body) != len(wantKeys) {
		t.Fatalf("unexpected fallback fields: %s", out.Body.String())
	}
	for _, key := range wantKeys {
		record, ok := body[key].(map[string]any)
		if !ok || record["id"] == nil {
			t.Fatalf("missing %s identity: %s", key, out.Body.String())
		}
		for field := range record {
			if field != "id" && field != "ref" && field != "version" && field != "revision_number" {
				t.Errorf("fallback leaked %s.%s in whole response: %s", key, field, out.Body.String())
			}
		}
	}
	for _, secret := range []string{"ResponseCardSecret", "ResponseBoardSecret", "ResponseSummarySecret", "ResponseRevisionSecret", "ResponseRevisionSummarySecret"} {
		if strings.Contains(out.Body.String(), secret) {
			t.Errorf("whole response leaked %s: %s", secret, out.Body.String())
		}
	}
}

func TestCardMutationWholeResponseAfterAccessLoss(t *testing.T) {
	requireIntegrationTest(t)
	env := newAuthIntegrationEnv(t, authIntegrationOptions{})
	reader := seedNotificationTestAgent(t, env, "reader.echohost")
	s := env.primitiveStore.(*primitives.Store)
	ctx := context.Background()
	names := []string{"patch", "revise", "move", "move-cross-board", "archive", "board-archive", "trash", "board-trash", "restore", "board-patch", "board-patch-noop", "remove", "board-move", "board-lost-card-visible"}
	for i, name := range names {
		t.Run(name, func(t *testing.T) {
			board, err := s.CreateBoard(ctx, reader.ActorID, map[string]any{"handle": fmt.Sprintf("echo-board-%d", i), "title": "ResponseBoardSecret", "summary": "ResponseSummarySecret"})
			if err != nil {
				t.Fatal(err)
			}
			work, err := s.CreateWork(ctx, reader.ActorID, anyString(board["id"]), map[string]any{"handle": fmt.Sprintf("echo-card-%d", i), "title": "ResponseCardSecret", "summary": "ResponseSummarySecret"})
			if err != nil {
				t.Fatal(err)
			}
			id, boardID := anyString(work["id"]), anyString(board["id"])
			if name == "restore" {
				if _, err = s.ArchiveBoardCard(ctx, reader.ActorID, "", id, primitives.RemoveBoardCardInput{}); err != nil {
					t.Fatal(err)
				}
			}
			board, err = s.GetBoard(ctx, boardID)
			if err != nil {
				t.Fatal(err)
			}
			card, err := s.GetBoardCard(ctx, "", id)
			if err != nil {
				t.Fatal(err)
			}
			body := map[string]any{"actor_id": reader.ActorID}
			method, path, status := http.MethodPost, "/cards/"+id+"/"+name, http.StatusOK
			wantKeys := []string{"card", "board"}
			switch name {
			case "patch":
				method, path = http.MethodPatch, "/cards/"+id
				body["if_updated_at"], body["patch"] = card["updated_at"], map[string]any{"due_at": "2026-11-15T00:00:00Z"}
				wantKeys = []string{"card"}
			case "revise":
				path, status = "/cards/"+id+"/revisions", http.StatusCreated
				body["revision"] = map[string]any{"title": "ResponseRevisionSecret", "summary": "ResponseRevisionSummarySecret"}
				wantKeys = []string{"card", "board", "revision"}
			case "move", "move-cross-board", "board-move", "board-lost-card-visible":
				path = "/cards/" + id + "/move"
				body["column_key"], body["if_board_updated_at"] = "ready", board["updated_at"]
				if name == "move-cross-board" {
					dest, err := s.CreateBoard(ctx, reader.ActorID, map[string]any{"title": "ResponseBoardSecret"})
					if err != nil {
						t.Fatal(err)
					}
					body["board_ref"] = dest["ref"]
				}
			case "trash", "board-trash":
				if name == "board-trash" {
					path = "/boards/" + boardID + "/cards/" + id + "/trash"
				}
				body["reason"] = "ResponseSummarySecret"
			case "board-patch", "board-patch-noop":
				method, path = http.MethodPatch, "/boards/"+boardID+"/cards/"+id
				body["if_board_updated_at"] = board["updated_at"]
				body["patch"] = map[string]any{}
				if name == "board-patch" {
					body["patch"] = map[string]any{"due_at": "2026-11-15T00:00:00Z"}
				}
			case "board-archive":
				path = "/boards/" + boardID + "/cards/" + id + "/archive"
			case "remove":
				method, path = http.MethodDelete, "/boards/"+boardID+"/cards/"+id
				body["if_board_updated_at"] = board["updated_at"]
			}
			spy := &mutationResponseRaceStore{Store: s, hookNoop: name == "board-patch-noop", afterEvents: name == "move-cross-board"}
			spy.afterCommit = func(result primitives.BoardCardMutationResult) error {
				// The revision case reproduces the exact reported concurrent
				// move. Lifecycle cases revoke the board without moving an
				// archived/trashed card, which the store correctly forbids.
				target := result.Board
				if name == "revise" || name == "board-lost-card-visible" {
					dest, err := s.CreateBoard(ctx, "other-owner", map[string]any{"title": "Competing transaction"})
					if err != nil {
						return err
					}
					if name == "revise" {
						if _, err = s.PatchThread(ctx, "other-owner", anyString(dest["thread_id"]), map[string]any{"pm_actor_id": "other-owner"}, nil); err != nil {
							return err
						}
					}
					if _, err = s.MoveBoardCard(ctx, "other-owner", "", id, primitives.MoveBoardCardInput{BoardRef: anyString(dest["ref"]), ColumnKey: "ready"}); err != nil {
						return err
					}
					if name == "revise" {
						return nil
					}
				}
				_, err := s.PatchThread(ctx, "other-owner", anyString(target["thread_id"]), map[string]any{"pm_actor_id": "other-owner"}, nil)
				return err
			}
			scope := primitives.WithRequestAccessScope(ctx, primitives.AccessScope{ActorID: reader.ActorID})
			raw, _ := json.Marshal(body)
			req := httptest.NewRequest(method, path, strings.NewReader(string(raw))).WithContext(scope)
			cacheAuthenticatedPrincipal(req, &auth.Principal{ActorID: reader.ActorID})
			opts := handlerOptions{primitiveStore: spy, authStore: env.authStore, actorRegistry: env.registry, workspaceID: "ws_main"}
			out := httptest.NewRecorder()
			switch name {
			case "patch":
				handlePatchCard(out, req, opts, id)
			case "revise":
				handleCreateCardRevision(out, req, opts, id)
			case "move", "move-cross-board", "board-lost-card-visible":
				handleMoveCardMutation(out, req, opts, "", id, "card not found")
			case "board-move":
				handleMoveBoardCard(out, req, opts, boardID, id)
			case "archive":
				handleArchiveCard(out, req, opts, id)
			case "board-archive":
				handleArchiveBoardCard(out, req, opts, boardID, id)
			case "trash":
				handleTrashCard(out, req, opts, id)
			case "board-trash":
				handleTrashBoardCard(out, req, opts, boardID, id)
			case "restore":
				handleRestoreArchivedCard(out, req, opts, id)
			case "board-patch", "board-patch-noop":
				handleUpdateBoardCard(out, req, opts, boardID, id)
			case "remove":
				handleRemoveBoardCard(out, req, opts, boardID, id)
			}
			if out.Code != status || spy.calls != 1 {
				t.Fatalf("status=%d want=%d hook calls=%d body=%s", out.Code, status, spy.calls, out.Body.String())
			}
			if name == "board-lost-card-visible" {
				if _, err := s.GetBoardCard(primitives.WithRequestAccessScope(ctx, primitives.AccessScope{ActorID: reader.ActorID}), "", id); err != nil {
					t.Fatalf("card visibility control: %v", err)
				}
			}
			if name == "board-lost-card-visible" {
				var response map[string]any
				if err := json.Unmarshal(out.Body.Bytes(), &response); err != nil {
					t.Fatal(err)
				}
				if len(response) != 2 || anyString(response["card"].(map[string]any)["title"]) != "ResponseCardSecret" {
					t.Fatalf("visible card was not freshly projected: %s", out.Body.String())
				}
				board := response["board"].(map[string]any)
				if len(board) != 2 || board["id"] != boardID {
					t.Fatalf("hidden board must be reference-only: %s", out.Body.String())
				}
			} else {
				assertIdentityOnlyMutationResponse(t, out, wantKeys)
			}
		})
	}
}

func TestCardMutationResponseFreshScope(t *testing.T) {
	requireIntegrationTest(t)
	env := newAuthIntegrationEnv(t, authIntegrationOptions{})
	reader := seedNotificationTestAgent(t, env, "reader.freshhost")
	s := env.primitiveStore.(*primitives.Store)
	ctx := context.Background()
	for _, name := range []string{"hidden-sibling", "private-removed-thread", "create", "create-keyed", "board-create", "batch-create", "batch-create-keyed"} {
		t.Run(name, func(t *testing.T) {
			board, err := s.CreateBoard(ctx, reader.ActorID, map[string]any{"title": "FreshScopeBoardSecret"})
			if err != nil {
				t.Fatal(err)
			}
			boardID := anyString(board["id"])
			work, err := s.CreateWork(ctx, reader.ActorID, boardID, map[string]any{"title": "FreshScopeCardSecret"})
			if err != nil {
				t.Fatal(err)
			}
			id := anyString(work["id"])
			var hidden string
			var hiddenThread string
			if name == "hidden-sibling" {
				sibling, err := s.CreateWork(ctx, reader.ActorID, boardID, map[string]any{"title": "HiddenSiblingSecret"})
				if err != nil {
					t.Fatal(err)
				}
				hidden, hiddenThread = anyString(sibling["id"]), anyString(sibling["thread_id"])
			}
			if name == "private-removed-thread" {
				thread, err := s.CreateThread(ctx, reader.ActorID, map[string]any{"title": "RemovedThreadSecret"})
				if err != nil {
					t.Fatal(err)
				}
				hiddenThread = anyString(thread.Thread["id"])
				hidden = hiddenThread
				refs := []string{"thread:" + hiddenThread}
				if _, err = s.UpdateBoardCard(ctx, reader.ActorID, boardID, id, primitives.UpdateBoardCardInput{Refs: &refs}); err != nil {
					t.Fatal(err)
				}
			}
			spy := &mutationResponseRaceStore{Store: s}
			spy.afterCommit = func(result primitives.BoardCardMutationResult) error {
				if name == "private-removed-thread" {
					// A separate committed writer removes the old reference, so
					// the card stays visible while the old thread becomes private.
					if _, err := s.RestoreArchivedBoardCard(ctx, "other-owner", boardID, id, primitives.RemoveBoardCardInput{}); err != nil {
						return err
					}
					refs, parent := []string{}, ""
					if _, err := s.UpdateBoardCard(ctx, "other-owner", boardID, id, primitives.UpdateBoardCardInput{Refs: &refs, ParentThreadID: &parent}); err != nil {
						return err
					}
					if _, err := s.ArchiveBoardCard(ctx, "other-owner", boardID, id, primitives.RemoveBoardCardInput{}); err != nil {
						return err
					}
				}
				thread := hiddenThread
				if thread == "" {
					thread = anyString(result.Board["thread_id"])
				}
				_, err := s.PatchThread(ctx, "other-owner", thread, map[string]any{"pm_actor_id": "other-owner"}, nil)
				return err
			}
			scope := primitives.WithRequestAccessScope(ctx, primitives.AccessScope{ActorID: reader.ActorID})
			currentBoard, err := s.GetBoard(ctx, boardID)
			if err != nil {
				t.Fatal(err)
			}
			body := map[string]any{"actor_id": reader.ActorID, "column_key": "ready", "if_board_updated_at": currentBoard["updated_at"]}
			path := "/cards/" + id + "/move"
			if strings.Contains(name, "create") {
				path = "/cards"
				body["board_id"] = boardID
				body["card"] = map[string]any{"title": "CreatedCardSecret", "summary": "CreatedSummarySecret"}
				if strings.Contains(name, "keyed") {
					body["request_key"] = "fresh-" + name
				}
				if name == "board-create" {
					path = "/boards/" + boardID + "/cards"
				}
				if strings.HasPrefix(name, "batch") {
					path = "/boards/" + boardID + "/cards/batch"
					body["items"] = []any{map[string]any{"card": map[string]any{"title": "CreatedCardSecret"}}, map[string]any{"card": map[string]any{"title": "CreatedSummarySecret"}}}
				}
			}
			method := http.MethodPost
			if name == "private-removed-thread" {
				method, path = http.MethodDelete, "/boards/"+boardID+"/cards/"+id
			}
			raw, _ := json.Marshal(body)
			req := httptest.NewRequest(method, path, strings.NewReader(string(raw))).WithContext(scope)
			cacheAuthenticatedPrincipal(req, &auth.Principal{ActorID: reader.ActorID})
			opts := handlerOptions{primitiveStore: spy, authStore: env.authStore, actorRegistry: env.registry, workspaceID: "ws_main"}
			out := httptest.NewRecorder()
			switch {
			case name == "hidden-sibling":
				handleMoveCardMutation(out, req, opts, "", id, "card not found")
			case name == "private-removed-thread":
				handleRemoveBoardCard(out, req, opts, boardID, id)
			case strings.HasPrefix(name, "batch"):
				handleBatchAddBoardCards(out, req, opts, boardID)
			case name == "board-create":
				handleAddBoardCard(out, req, opts, boardID)
			default:
				addBoardCardFromRaw(out, req, opts, boardID, body, "cards.create")
			}
			status := http.StatusOK
			if strings.Contains(name, "create") {
				status = http.StatusCreated
			}
			if out.Code != status || spy.calls != 1 {
				t.Fatalf("status=%d hook=%d body=%s", out.Code, spy.calls, out.Body.String())
			}
			if name == "batch-create-keyed" {
				var count int
				if err := env.workspace.DB().QueryRow(`SELECT count(*) FROM idempotency_replays WHERE request_key=?`, "fresh-"+name).Scan(&count); err != nil || count != 1 {
					t.Fatalf("committed batch lost receipt: count=%d err=%v", count, err)
				}
			}
			var response map[string]any
			if err := json.Unmarshal(out.Body.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
			if name == "hidden-sibling" || name == "private-removed-thread" {
				// Prove the main card and board remain visible: a fallback due
				// to losing the whole card must not mask the reported leak.
				if _, err := s.GetBoardCardCurrent(primitives.WithRequestAccessScope(ctx, primitives.AccessScope{ActorID: reader.ActorID}), "", id); err != nil {
					t.Fatal(err)
				}
				if _, err := s.GetBoard(primitives.WithRequestAccessScope(ctx, primitives.AccessScope{ActorID: reader.ActorID}), boardID); err != nil {
					t.Fatal(err)
				}
				if hidden != "" && strings.Contains(out.Body.String(), hidden) {
					t.Errorf("stale hidden resource leaked: %s", out.Body.String())
				}
				if _, exists := response["removed_thread_id"]; exists {
					t.Errorf("removed thread must be omitted: %s", out.Body.String())
				}
			} else if strings.HasPrefix(name, "batch") {
				cards, _ := response["cards"].([]any)
				if len(cards) != 2 || len(response) != 2 {
					t.Fatalf("unexpected batch response: %s", out.Body.String())
				}
				for _, record := range append(cards, response["board"]) {
					for field := range record.(map[string]any) {
						if field != "id" && field != "ref" {
							t.Errorf("private batch field %s: %s", field, out.Body.String())
						}
					}
				}
			} else {
				assertIdentityOnlyMutationResponse(t, out, []string{"card", "board"})
			}
		})
	}
}
