package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"agent-nexus-core/internal/auth"
	"agent-nexus-core/internal/primitives"
)

type failedCardProjectionStore struct {
	*primitives.Store
	calls      int
	failReload bool
}

func (s *failedCardProjectionStore) GetBoardCardCurrent(ctx context.Context, boardID, identifier string) (map[string]any, error) {
	if s.failReload {
		s.calls++
		return nil, errors.New("injected scoped reload failure")
	}
	return s.Store.GetBoardCardCurrent(ctx, boardID, identifier)
}

func (s *failedCardProjectionStore) EnrichCardPlans(context.Context, []map[string]any, func(string, string) bool, time.Time) error {
	s.calls++
	return errors.New("injected projection failure with sensitive details")
}

func assertIdentityOnlyCardEcho(t *testing.T, card, original map[string]any) {
	t.Helper()
	want := map[string]any{"id": original["id"], "ref": original["ref"]}
	if version, ok := original["version"]; ok {
		want["version"] = version
	}
	// Compare JSON because handler responses decode integer versions to float64.
	gotJSON, _ := json.Marshal(card)
	wantJSON, _ := json.Marshal(want)
	if string(gotJSON) != string(wantJSON) {
		t.Fatalf("echo must contain only the mutation's identity/version: got %s want %s", gotJSON, wantJSON)
	}
}

func TestCardMutationEchoAfterConcurrentPrivateBoardMove(t *testing.T) {
	requireIntegrationTest(t)
	env := newAuthIntegrationEnv(t, authIntegrationOptions{})
	s := env.primitiveStore.(*primitives.Store)
	ctx := context.Background()
	public, err := s.CreateBoard(ctx, "owner", map[string]any{"title": "Public board"})
	if err != nil {
		t.Fatal(err)
	}
	private, err := s.CreateBoard(ctx, "owner", map[string]any{"title": "Private board"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.PatchThread(ctx, "owner", anyString(private["thread_id"]), map[string]any{"pm_actor_id": "owner"}, nil); err != nil {
		t.Fatal(err)
	}
	created, err := s.CreateBoardCard(ctx, "owner", anyString(public["id"]), primitives.AddBoardCardInput{Title: "ScopeSensitiveTitle", Body: "ScopeSensitiveSummary", ColumnKey: "ready"})
	if err != nil {
		t.Fatal(err)
	}
	viewer := primitives.WithRequestAccessScope(ctx, primitives.AccessScope{ActorID: "reader"})
	// This is the mutation that commits before a competing transaction changes
	// visibility. Retain its raw result exactly as a lifecycle handler does.
	due := "2026-11-15T00:00:00Z"
	committed, err := s.UpdateBoardCard(viewer, "reader", "", anyString(created.Card["id"]), primitives.UpdateBoardCardInput{DueAt: &due})
	if err != nil {
		t.Fatal(err)
	}
	moveFinished := make(chan error, 1)
	go func() {
		_, err := s.MoveBoardCard(ctx, "owner", "", anyString(created.Card["id"]), primitives.MoveBoardCardInput{BoardRef: anyString(private["ref"]), ColumnKey: "ready"})
		moveFinished <- err
	}()
	if err := <-moveFinished; err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetBoardCard(viewer, "", anyString(created.Card["id"])); !errors.Is(err, primitives.ErrNotFound) {
		t.Fatalf("private move did not revoke access: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/cards/"+anyString(created.Card["id"])+"/restore", nil).WithContext(viewer)
	cacheAuthenticatedPrincipal(req, &auth.Principal{ActorID: "reader"})
	assertIdentityOnlyCardEcho(t, cardMutationEchoView(req, handlerOptions{primitiveStore: s}, committed.Card), committed.Card)
}

func TestCardMutationProjectionFailureCommitBoundary(t *testing.T) {
	requireIntegrationTest(t)
	env := newAuthIntegrationEnv(t, authIntegrationOptions{})
	owner := seedNotificationTestAgent(t, env, "owner.projectionhost")
	env.workspace.DB().SetMaxOpenConns(1)
	s := env.primitiveStore.(*primitives.Store)
	ctx := context.Background()
	for _, failReload := range []bool{false, true} {
		for _, mutation := range []string{"patch", "revise", "move", "archive"} {
			for _, keyed := range []bool{false, true} {
				name := mutation + "/unkeyed"
				if keyed {
					name = mutation + "/keyed"
				}
				if failReload {
					name += "/reload"
				} else {
					name += "/enrichment"
				}
				t.Run(name, func(t *testing.T) {
					work, err := s.CreateWork(ctx, owner.ActorID, "", map[string]any{"title": "Projection boundary"})
					if err != nil {
						t.Fatal(err)
					}
					id := anyString(work["id"])
					before, err := s.GetBoardCard(ctx, "", id)
					if err != nil {
						t.Fatal(err)
					}
					board, err := s.GetBoard(ctx, anyString(before["board_id"]))
					if err != nil {
						t.Fatal(err)
					}
					body := map[string]any{"actor_id": owner.ActorID}
					if keyed {
						body["request_key"] = name
					}
					method, path, wantStatus := http.MethodPost, "/cards/"+id+"/"+mutation, http.StatusOK
					switch mutation {
					case "patch":
						method, path = http.MethodPatch, "/cards/"+id
						body["if_updated_at"] = before["updated_at"]
						body["patch"] = map[string]any{"due_at": "2026-11-15T00:00:00Z"}
					case "revise":
						path, wantStatus = "/cards/"+id+"/revisions", http.StatusCreated
						body["if_base_revision"] = before["head_revision_ref"]
						body["revision"] = map[string]any{"title": "Revised title"}
					case "move":
						body["column_key"] = "ready"
						body["if_board_updated_at"] = board["updated_at"]
						body["if_version"] = work["version"]
					case "archive":
						if keyed {
							body["if_version"] = work["version"]
							body["if_lifecycle"] = map[string]any{"archived_at": nil, "trashed_at": nil}
							body["if_latest_observation_id"] = ""
						}
					}
					spy := &failedCardProjectionStore{Store: s, failReload: failReload}
					opts := handlerOptions{primitiveStore: spy, authStore: env.authStore, actorRegistry: env.registry, workspaceID: "ws_main"}
					raw, _ := json.Marshal(body)
					scope := primitives.WithRequestAccessScope(ctx, primitives.AccessScope{ActorID: owner.ActorID})
					req := httptest.NewRequest(method, path, strings.NewReader(string(raw))).WithContext(scope)
					cacheAuthenticatedPrincipal(req, &auth.Principal{ActorID: owner.ActorID})
					out := httptest.NewRecorder()
					switch mutation {
					case "patch":
						handlePatchCard(out, req, opts, id)
					case "revise":
						handleCreateCardRevision(out, req, opts, id)
					case "move":
						handleMoveCardMutation(out, req, opts, "", id, "card not found")
					case "archive":
						handleArchiveBoardCard(out, req, opts, "", id)
					}
					if spy.calls != 1 {
						t.Fatalf("projection not exercised: calls=%d status=%d body=%s", spy.calls, out.Code, out.Body.String())
					}
					after, err := s.GetBoardCard(ctx, "", id)
					if err != nil {
						t.Fatal(err)
					}
					if keyed {
						if out.Code != http.StatusInternalServerError || !reflect.DeepEqual(before, after) {
							t.Fatalf("keyed projection failure must roll back: status=%d body=%s before=%v after=%v", out.Code, out.Body.String(), before, after)
						}
						var replays int
						if err := env.workspace.DB().QueryRow(`SELECT count(*) FROM idempotency_replays WHERE request_key=?`, name).Scan(&replays); err != nil || replays != 0 {
							t.Fatalf("failed projection retained replay: count=%d err=%v", replays, err)
						}
					} else {
						if out.Code != wantStatus || reflect.DeepEqual(before, after) {
							t.Fatalf("committed mutation must remain successful: status=%d body=%s", out.Code, out.Body.String())
						}
						var response map[string]any
						if err := json.Unmarshal(out.Body.Bytes(), &response); err != nil {
							t.Fatal(err)
						}
						assertIdentityOnlyCardEcho(t, response["card"].(map[string]any), after)
					}
				})
			}
		}
	}
}
