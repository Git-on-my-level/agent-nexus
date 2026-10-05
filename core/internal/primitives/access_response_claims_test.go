package primitives

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"agent-nexus-core/internal/storage"
)

func TestResourceAccessResponseClaimsFollowCurrentOwnership(t *testing.T) {
	for _, relation := range []string{"request", "response", "stored-payload"} {
		t.Run(relation, func(t *testing.T) {
			ctx := context.Background()
			ws, err := storage.InitializeWorkspace(ctx, t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer ws.Close()
			s := NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
			private, err := s.CreateWork(ctx, "owner", "", map[string]any{"title": "private evidence"})
			if err != nil {
				t.Fatal(err)
			}
			setOwner := func(owner string) {
				t.Helper()
				if _, err := s.PatchThread(ctx, "owner", anyStringValue(private["thread_id"]), map[string]any{"pm_actor_id": owner}, nil); err != nil {
					t.Fatal(err)
				}
			}
			setOwner("owner")
			event := func(which, typ string) map[string]any {
				payload := map[string]any{}
				if relation == which {
					payload["subject_ref"] = private["ref"]
				}
				return map[string]any{"type": typ, "refs": []string{}, "payload": payload}
			}
			request, err := s.AppendEvent(ctx, "owner", event("request", "human_attention_requested"))
			if err != nil {
				t.Fatal(err)
			}
			requestID := anyStringValue(request["id"])
			owner := WithAccessScope(ctx, AccessScope{ActorID: "owner", PMActorID: "selected"})
			response, _, err := s.AppendHumanAttentionResponse(owner, "owner", requestID, "claim-inbox", "claim-key", "claim-hash", event("response", "human_attention_responded"), nil)
			if err != nil {
				t.Fatal(err)
			}
			if relation == "stored-payload" {
				response["related_refs"] = []string{anyStringValue(private["ref"])}
			}
			if err := s.SaveHumanAttentionResponseResult(owner, requestID, response); err != nil {
				t.Fatal(err)
			}
			expected, err := json.Marshal(response)
			if err != nil {
				t.Fatal(err)
			}
			assertVisible := func(actor string, visible bool) {
				t.Helper()
				c := WithAccessScope(ctx, AccessScope{ActorID: actor, PMActorID: "selected"})
				replay, err := s.HumanAttentionResponseReplay(c, "owner", "claim-key", "claim-hash")
				if (visible && err != nil) || (!visible && !errors.Is(err, ErrNotFound)) {
					t.Fatalf("%s replay visibility=%v err=%v", actor, visible, err)
				}
				if visible {
					actual, err := json.Marshal(replay)
					if err != nil || string(actual) != string(expected) {
						t.Fatalf("stored response changed: %s err=%v", actual, err)
					}
				}
				claimed, err := s.HumanAttentionResponseClaimed(c, "claim-inbox")
				if err != nil || claimed != visible {
					t.Fatalf("%s claim=%v err=%v", actor, claimed, err)
				}
				_, err = s.HumanAttentionResponseRequest(c, "claim-inbox")
				if (visible && err != nil) || (!visible && !errors.Is(err, ErrNotFound)) {
					t.Fatalf("%s request visibility=%v err=%v", actor, visible, err)
				}
				if !visible {
					if _, err := s.HumanAttentionResponseReplay(c, "owner", "claim-key", "different-hash"); !errors.Is(err, ErrNotFound) {
						t.Fatalf("private hash oracle: %v", err)
					}
					if err := s.SaveHumanAttentionResponseResult(c, requestID, map[string]any{"public": true}); !errors.Is(err, ErrNotFound) {
						t.Fatalf("private response overwritten: %v", err)
					}
				}
			}
			for _, actor := range []string{"stranger", "unauthorized-agent"} {
				assertVisible(actor, false)
			}
			assertVisible("owner", true)
			assertVisible("selected", true)
			setOwner("other")
			assertVisible("owner", false)
			setOwner("owner")
			assertVisible("owner", true)
		})
	}
}
