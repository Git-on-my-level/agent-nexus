package server

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"agent-nexus-core/internal/pm"
	"agent-nexus-core/internal/primitives"
)

func TestPMActorMismatchOnlyDisclosedToAuthenticatedAgent(t *testing.T) {
	t.Parallel()
	env := newPMStoreTestEnv(t)
	ctx := context.Background()
	db := env.workspace.DB()
	human := seedHumanPrincipalForLockoutTest(t, ctx, db, "human", "human-actor", "human", "human-token")
	agent := seedMachinePrincipalForLockoutTest(t, ctx, db, "acting", "acting-actor", "acting", "acting-token")
	if _, err := db.Exec(`UPDATE agents SET metadata_json='{"principal_kind":"agent","auth_method":"public_key"}' WHERE id=?`, agent.AgentID); err != nil {
		t.Fatal(err)
	}
	rt, err := NewPMRuntime(db, env.primitiveStore.(*primitives.Store), env.authStore, PMRuntimeConfig{PM: pm.Config{WorkspaceID: "ws_main", AgentActorID: "configured-secret-actor"}})
	if err != nil {
		t.Fatal(err)
	}
	h := NewHandler("", WithAuthStore(env.authStore), WithActorRegistry(env.registry), WithPrimitiveStore(env.primitiveStore), WithPMRuntime(rt))
	for _, path := range []string{"/pm/connect", "/pm/disconnect"} {
		for _, tc := range []struct {
			token    string
			status   int
			mismatch bool
		}{{"", 401, false}, {"invalid", 401, false}, {human.AccessToken, 403, false}, {agent.AccessToken, 403, true}} {
			req := httptest.NewRequest("POST", path, strings.NewReader(`{"runner":"custom","host":"computer"}`))
			if tc.token != "" {
				req.Header.Set("Authorization", "Bearer "+tc.token)
			}
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)
			if w.Code != tc.status {
				t.Fatalf("%s: %d %s", path, w.Code, w.Body.String())
			}
			if !tc.mismatch {
				for _, secret := range []string{"configured-secret-actor", "acting-actor", "pm_actor_mismatch", "ANX_PM_AGENT_ACTOR_ID"} {
					if strings.Contains(w.Body.String(), secret) {
						t.Fatalf("disclosure to denied caller: %s", w.Body.String())
					}
				}
				continue
			}
			var body struct {
				Error struct {
					Code        string
					Hint        string
					Recoverable bool
					Details     pm.ActorMismatchError
				}
			}
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if body.Error.Code != "pm_actor_mismatch" || body.Error.Details.ConfiguredActorID != "configured-secret-actor" || body.Error.Details.ActingActorID != agent.ActorID || body.Error.Recoverable || !strings.Contains(body.Error.Hint, "rebind ANX_PM_AGENT_ACTOR_ID") || !strings.Contains(body.Error.Hint, agent.ActorID) {
				t.Fatalf("wrong diagnostic: %s", w.Body.String())
			}
		}
	}
}
