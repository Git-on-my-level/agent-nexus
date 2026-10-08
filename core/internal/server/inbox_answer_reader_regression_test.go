package server

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"testing"
	"time"

	"agent-nexus-core/internal/primitives"
)

// Both readers must remove the ask and retain its card reference in history,
// regardless of whether the human chose a proposal or wrote their own answer.
func TestInboxAnswerReaderModes(t *testing.T) {
	requireIntegrationTest(t)
	for _, scoped := range []bool{false, true} {
		for _, answer := range []string{"Approve", "Use a smaller rollout first"} {
			t.Run(fmt.Sprintf("scoped=%t/answer=%s", scoped, answer), func(t *testing.T) {
				env := newAuthIntegrationEnv(t, authIntegrationOptions{scopedInboxReader: scoped})
				ctx := context.Background()
				owner := seedHumanPrincipalForLockoutTest(t, ctx, env.workspace.DB(), "answer-owner", "answer-owner-actor", "answer-owner", "answer-owner-token")
				store := env.primitiveStore.(*primitives.Store)
				ask, _ := seedInboxResponsePrivacyAsk(t, store, owner.ActorID, "card")
				if scoped {
					for done := false; !done; {
						var err error
						done, err = env.workspace.MaintainScopeInboxBatch(ctx)
						if err != nil {
							t.Fatal(err)
						}
					}
				}
				before := scopeInboxHTTP(t, env, owner.AccessToken, "/inbox?limit=50")
				if len(before["items"].([]any)) != 1 {
					t.Fatalf("missing ask: %#v", before)
				}
				status, result := hostHTTP(t, http.MethodPost, env.server.URL+"/inbox/"+url.PathEscape(ask.ID)+"/respond", owner.AccessToken, map[string]any{"response_text": answer, "outcome": "answered", "notify_mode": "none"})
				if status != http.StatusCreated {
					t.Fatalf("response: %d %#v", status, result)
				}
				// Rebuilding the projection must not resurrect the answer either.
				if err := refreshDerivedTopicProjection(ctx, handlerOptions{primitiveStore: store}, ask.ThreadID, time.Now().UTC(), owner.ActorID); err != nil {
					t.Fatal(err)
				}
				after := scopeInboxHTTP(t, env, owner.AccessToken, "/inbox?limit=50")
				if len(after["items"].([]any)) != 0 {
					t.Fatalf("answered ask reappeared: %#v", after)
				}
				history := scopeInboxHTTP(t, env, owner.AccessToken, "/inbox?status=completed&limit=50")
				rows := history["items"].([]any)
				if len(rows) != 1 {
					t.Fatalf("missing completed answer: %#v", history)
				}
				row := rows[0].(map[string]any)
				if row["inbox_item_id"] != ask.ID || row["response_text"] != answer || row["subject_ref"] != ask.Data["subject_ref"] {
					t.Fatalf("lost original decision/answer: %#v", row)
				}
				if scoped && store.ScopeInboxDiagnostics().Served == 0 {
					t.Fatal("scoped reader was not exercised")
				}
			})
		}
	}
}
