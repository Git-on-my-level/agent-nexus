package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"testing"

	"agent-nexus-core/internal/auth"
	"agent-nexus-core/internal/pm"
	"agent-nexus-core/internal/primitives"
)

type round15PrincipalStore struct {
	round4PrincipalStore
	readErr error
}

func (s *round15PrincipalStore) ListPrincipals(ctx context.Context, f auth.AuthPrincipalListFilter) ([]auth.AuthPrincipalSummary, string, error) {
	if s.readErr != nil {
		return nil, "", s.readErr
	}
	return s.round4PrincipalStore.ListPrincipals(ctx, f)
}

func (s *round15PrincipalStore) GetPrincipalSummary(ctx context.Context, id string) (auth.AuthPrincipalSummary, error) {
	if s.readErr != nil {
		return auth.AuthPrincipalSummary{}, s.readErr
	}
	return s.round4PrincipalStore.GetPrincipalSummary(ctx, id)
}

func TestRound15PrincipalLookupFailureClassification(t *testing.T) {
	for _, warm := range []bool{false, true} {
		for _, tc := range []struct {
			name          string
			readErr, want error
			revoked       bool
		}{
			{name: "reader failure", readErr: errors.New("private database busy detail"), want: pm.ErrUnavailable},
			{name: "missing", readErr: auth.ErrAgentNotFound, want: pm.ErrForbidden},
			{name: "revoked", revoked: true, want: pm.ErrForbidden},
		} {
			t.Run(tc.name+map[bool]string{false: "/cold", true: "/warm"}[warm], func(t *testing.T) {
				ctx := context.Background()
				st := &round15PrincipalStore{round4PrincipalStore: round4PrincipalStore{principal: auth.AuthPrincipalSummary{ActorID: "maya", AgentID: "human", PrincipalKind: "human"}}}
				lookup := newPMPrincipalLookup(st)
				if warm {
					if _, err := lookup.findForAuthorization(ctx, "maya"); err != nil {
						t.Fatal(err)
					}
				}
				st.readErr, st.principal.Revoked = tc.readErr, tc.revoked
				_, err := lookup.findForAuthorization(ctx, "maya")
				if !errors.Is(err, tc.want) {
					t.Fatal(err)
				}
				if tc.want == pm.ErrUnavailable && err.Error() != "PM capability is not configured: principal authorization could not be read; retry the request" {
					t.Fatal(err)
				}
				st.readErr, st.principal.Revoked = nil, false
				if principal, err := lookup.findForAuthorization(ctx, "maya"); err != nil || principal.PrincipalKind != "human" {
					t.Fatalf("retry: %+v %v", principal, err)
				}
			})
		}
	}
}

func TestRound15TurnProposalOwnerReason(t *testing.T) {
	for _, humanOwner := range []bool{false, true} {
		t.Run(map[bool]string{false: "agent", true: "human"}[humanOwner], func(t *testing.T) {
			env := newPMStoreTestEnv(t)
			ctx := context.Background()
			db := env.workspace.DB()
			responder := seedMachinePrincipalForLockoutTest(t, ctx, db, "pm", "pm-actor", "pm", "pm-token")
			other := seedMachinePrincipalForLockoutTest(t, ctx, db, "other", "other-actor", "other", "other-token")
			owner := seedMachinePrincipalForLockoutTest(t, ctx, db, "leo", "leo-actor", "leo", "leo-token")
			if humanOwner {
				owner = seedHumanPrincipalForLockoutTest(t, ctx, db, "maya", "maya-actor", "maya", "maya-token")
			}
			store := env.primitiveStore.(*primitives.Store)
			board, err := store.CreateBoard(ctx, owner.ActorID, map[string]any{"title": "Proposals"})
			if err != nil {
				t.Fatal(err)
			}
			work, err := store.CreateWork(ctx, owner.ActorID, asString(board["id"]), map[string]any{"title": "Task"})
			if err != nil {
				t.Fatal(err)
			}
			rt, err := NewPMRuntime(db, store, env.authStore, PMRuntimeConfig{PM: pm.Config{WorkspaceID: "ws_main", AgentActorID: responder.ActorID}})
			if err != nil {
				t.Fatal(err)
			}
			ownerP := pm.Principal{WorkspaceID: "ws_main", ActorID: owner.ActorID, Human: humanOwner}
			c, err := rt.Service.CreateConversation(ctx, ownerP, pm.CreateConversation{RequestKey: "c", Title: "Next?"})
			if err != nil {
				t.Fatal(err)
			}
			turn, err := rt.Service.PostMessage(ctx, ownerP, c.ID, pm.MessageInput{RequestKey: "m", Text: "Help"})
			if err != nil {
				t.Fatal(err)
			}
			claimed, err := rt.Service.ClaimTurn(ctx, pm.Principal{WorkspaceID: "ws_main", ActorID: responder.ActorID}, pm.ClaimInput{})
			if err != nil || claimed.ID != turn.ID {
				t.Fatalf("claim: %+v %v", claimed, err)
			}
			input := pm.TurnProposeInput{DecisionInput: pm.DecisionInput{RequestKey: "proposal", WorkRef: asString(work["ref"]), Instruction: "Ready", Scope: "work.phase", TargetRevision: asString(work["decision_revision"]), Payload: &pm.ActionPayload{Phase: "ready"}}, LeaseToken: claimed.LeaseToken}
			raw, err := json.Marshal(input)
			if err != nil {
				t.Fatal(err)
			}
			for _, token := range []string{other.AccessToken, responder.AccessToken, responder.AccessToken} {
				req := httptest.NewRequest("POST", "/pm/turns/"+turn.ID+"/decisions", bytes.NewReader(raw))
				req.Header.Set("Authorization", "Bearer "+token)
				rr := httptest.NewRecorder()
				rt.ServeHTTP(rr, req)
				var out map[string]any
				if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
					t.Fatal(err)
				}
				if token == other.AccessToken || !humanOwner {
					if rr.Code != 403 {
						t.Fatalf("%d %s", rr.Code, rr.Body)
					}
					e := out["error"].(map[string]any)
					if token == other.AccessToken {
						if e["message"] != "PM permission denied" || e["details"] != nil {
							t.Fatal(e)
						}
					} else if e["code"] != "forbidden" || e["message"] != "Proposals need a human approver; this conversation belongs to an agent principal" || e["details"].(map[string]any)["reason"] != "conversation_owner_not_human" {
						t.Fatal(e)
					}
				} else if rr.Code != 201 && rr.Code != 200 {
					t.Fatalf("human: %d %s", rr.Code, rr.Body)
				}
			}
			var decisions int
			if err := db.QueryRow("SELECT count(*) FROM pm_records WHERE kind='decision'").Scan(&decisions); err != nil || decisions != map[bool]int{false: 0, true: 1}[humanOwner] {
				t.Fatalf("decisions=%d %v", decisions, err)
			}
		})
	}
}
