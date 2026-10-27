package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"agent-nexus-core/internal/auth"
	"agent-nexus-core/internal/primitives"
)

type cardGroupingProjectionStore struct {
	*primitives.Store
	failReload bool
	failEnrich bool
}

func TestCardMutationGroupingUsesActingViewer(t *testing.T) {
	requireIntegrationTest(t)
	env := newAuthIntegrationEnv(t, authIntegrationOptions{})
	viewer := seedNotificationTestAgent(t, env, "viewer.groupinghost")
	asker := seedNotificationTestAgent(t, env, "asker.groupinghost")
	s := env.primitiveStore.(*primitives.Store)
	ctx := context.Background()
	parent, err := s.CreateWork(ctx, viewer.ActorID, "", map[string]any{"title": "Viewer parent"})
	if err != nil {
		t.Fatal(err)
	}
	child, err := s.CreateWork(ctx, viewer.ActorID, "", map[string]any{"title": "Viewer child", "under": parent["ref"]})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.AppendEvent(ctx, asker.ActorID, map[string]any{
		"type": "human_attention_requested", "thread_id": child["thread_id"],
		"refs":    []string{anyString(child["ref"]), "thread:" + anyString(child["thread_id"])},
		"payload": map[string]any{"subject_ref": child["ref"], "requester_actor_id": asker.ActorID, "recipient_actor_id": viewer.ActorID, "title": "Choose?", "body": "Choose an approach", "response_proposals": []string{"Proceed", "Wait"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	for i, actor := range []string{viewer.ActorID, asker.ActorID} {
		scope := primitives.WithRequestAccessScope(ctx, primitives.AccessScope{ActorID: actor})
		before, err := s.GetBoardCardCurrent(scope, "", anyString(parent["id"]))
		if err != nil {
			t.Fatal(err)
		}
		body, _ := json.Marshal(map[string]any{"actor_id": actor, "if_updated_at": before["updated_at"], "patch": map[string]any{"due_at": fmt.Sprintf("2026-11-%02dT00:00:00Z", i+1)}})
		req := httptest.NewRequest("PATCH", "/cards/"+anyString(parent["id"]), strings.NewReader(string(body))).WithContext(scope)
		cacheAuthenticatedPrincipal(req, &auth.Principal{ActorID: actor})
		opts := handlerOptions{primitiveStore: s, authStore: env.authStore, actorRegistry: env.registry, workspaceID: "ws_main"}
		out := httptest.NewRecorder()
		handlePatchCard(out, req, opts, anyString(parent["id"]))
		if out.Code != 200 {
			t.Fatalf("patch: %d %s", out.Code, out.Body.String())
		}
		var response map[string]any
		if err := json.Unmarshal(out.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		rollup := response["card"].(map[string]any)["rollup"].(map[string]any)
		if rollup["needing_viewer"] != float64(1-i) {
			t.Fatalf("viewer=%s rollup=%#v", actor, rollup)
		}
		get := httptest.NewRequest("GET", req.URL.Path, nil).WithContext(primitives.WithRequestAccessScope(ctx, primitives.AccessScope{ActorID: actor}))
		cacheAuthenticatedPrincipal(get, &auth.Principal{ActorID: actor})
		fresh := httptest.NewRecorder()
		handleGetCard(fresh, get, opts, anyString(parent["id"]))
		if fresh.Code != 200 {
			t.Fatal(fresh.Code, fresh.Body.String())
		}
		var read map[string]any
		if err := json.Unmarshal(fresh.Body.Bytes(), &read); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(rollup, read["card"].(map[string]any)["rollup"]) {
			t.Fatal("mutation grouping differs from the acting viewer's GET")
		}
	}
}

func (s cardGroupingProjectionStore) GetBoardCardCurrent(context.Context, string, string) (map[string]any, error) {
	if s.failReload {
		return nil, errors.New("injected current-row failure")
	}
	return map[string]any{"id": "card-id", "title": "Concurrent title", "board_ref": "board:concurrent"}, nil
}

func (s cardGroupingProjectionStore) EnrichCardPlans(_ context.Context, cards []map[string]any, _ func(string, string) bool, _ time.Time) error {
	cards[0]["parent_ref"] = "card:scoped-parent"
	cards[0]["rollup"] = map[string]any{"total": 2}
	if s.failEnrich {
		return errors.New("injected enrichment failure after partial projection")
	}
	return nil
}

func TestCardMutationGroupingPreservesExistingFields(t *testing.T) {
	for _, failure := range []string{"", "reload", "enrich"} {
		t.Run("failure="+failure, func(t *testing.T) {
			store := cardGroupingProjectionStore{failReload: failure == "reload", failEnrich: failure == "enrich"}
			raw := map[string]any{
				"id": "card-id", "title": "Mutation title", "summary": "Mutation summary",
				"board_ref": "board:original", "updated_at": "original-token",
				"parent_ref": "card:stale-parent", "rollup": map[string]any{"total": 99},
			}
			want := publicCardView(raw)
			delete(want, "parent_ref")
			delete(want, "rollup")
			if failure == "" {
				want["parent_ref"] = "card:scoped-parent"
				want["rollup"] = map[string]any{"total": 2}
			}
			req := httptest.NewRequest("PATCH", "/cards/card-id", nil)
			got := cardMutationEchoView(req.Context(), req, handlerOptions{primitiveStore: store}, raw)
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("grouping must be additive; failure must omit it: got=%#v want=%#v", got, want)
			}
			if raw["parent_ref"] != "card:stale-parent" {
				t.Fatal("response projection mutated the store result")
			}
		})
	}
}
