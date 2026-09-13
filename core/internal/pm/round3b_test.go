package pm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHTTPTurnClaimVisibility(t *testing.T) {
	for _, ending := range []string{"complete", "fail", "expiry"} {
		t.Run(ending, func(t *testing.T) {
			s, st, human, _ := fixture(t)
			s.deps.Dispatch = nil
			ctx := context.Background()
			c, err := s.CreateConversation(ctx, human, CreateConversation{RequestKey: "c", Title: "Claim visibility"})
			if err != nil {
				t.Fatal(err)
			}
			actor := human
			h := Handler{Service: s, Authenticate: func(*http.Request) (Principal, error) { return actor, nil }}
			request := func(method, path, body string) map[string]any {
				t.Helper()
				w := httptest.NewRecorder()
				h.ServeHTTP(w, httptest.NewRequest(method, path, strings.NewReader(body)))
				if w.Code < 200 || w.Code >= 300 {
					t.Fatalf("%s %s: %d %s", method, path, w.Code, w.Body.String())
				}
				var out map[string]any
				if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
					t.Fatal(err)
				}
				return out
			}
			messagePath := "/pm/conversations/" + c.ID + "/messages"
			message := `{"request_key":"m","text":"hello"}`
			created := request("POST", messagePath, message)
			id := created["id"].(string)
			check := func(turn map[string]any, claimed bool, at string) {
				t.Helper()
				if got, exists := turn["claimed"]; !exists || got != claimed {
					t.Fatalf("claimed: %#v", turn)
				}
				for _, key := range []string{"lease_token", "lease_owner", "lease_expires_at"} {
					if _, ok := turn[key]; ok {
						t.Fatalf("public turn leaked %s: %#v", key, turn)
					}
				}
				if at == "" {
					if _, ok := turn["claimed_at"]; ok {
						t.Fatal("invented claim time")
					}
				} else if turn["claimed_at"] != at {
					t.Fatalf("claim timestamp changed: %#v", turn)
				}
			}
			check(created, false, "")
			verify := func(claimed bool, at string) {
				t.Helper()
				actor = human
				detail := request("GET", "/pm/conversations/"+c.ID, "")
				turns := detail["turns"].([]any)
				if len(turns) != 1 {
					t.Fatal(detail)
				}
				check(turns[0].(map[string]any), claimed, at)
				check(request("GET", "/pm/turns/"+id, ""), claimed, at)
				check(request("POST", messagePath, message), claimed, at)
			}
			verify(false, "")
			agent := Principal{WorkspaceID: human.WorkspaceID, ActorID: "pm-agent"}
			actor = agent
			before := time.Now().UTC()
			claim := request("POST", "/pm/turns/claim", `{"runner_id":"runner"}`)
			at, ok := claim["claimed_at"].(string)
			stamp, err := time.Parse(time.RFC3339Nano, at)
			if !ok || err != nil || stamp.Before(before) || stamp.After(time.Now()) || claim["claimed"] != true {
				t.Fatalf("claim: %#v", claim)
			}
			token, ok := claim["lease_token"].(string)
			if !ok || token == "" {
				t.Fatal("runner did not receive completion credential")
			}
			verify(true, at)
			actor = agent
			if ending == "expiry" {
				var turn Turn
				if err := st.get(ctx, "turn", id, &turn); err != nil {
					t.Fatal(err)
				}
				old := turn.Revision
				turn.LeaseExpiresAt = time.Now().Add(-time.Second)
				turn.Revision++
				if err := st.cas(ctx, "turn", id, old, turn); err != nil {
					t.Fatal(err)
				}
			} else {
				field := "text"
				if ending == "fail" {
					field = "reason"
				}
				check(request("POST", "/pm/turns/"+id+"/"+ending, fmt.Sprintf(`{%q:"finished","lease_token":%q}`, field, token)), false, at)
			}
			verify(false, at)
		})
	}
}

func TestTurnResponseLegacyClaimAndExpiryBoundary(t *testing.T) {
	now := time.Now().UTC()
	turn := Turn{LeaseToken: "secret", LeaseOwner: "runner", LeaseExpiresAt: now}
	if leaseHeld(turn, now) {
		t.Fatal("lease held at exact expiry")
	}
	turn.LeaseExpiresAt = now.Add(time.Hour)
	raw, err := json.Marshal(turnResponse(turn, false))
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err = json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if out["claimed"] != true {
		t.Fatal(string(raw))
	}
	if _, ok := out["claimed_at"]; ok {
		t.Fatal("invented timestamp for legacy lease")
	}
	if _, ok := out["lease_token"]; ok {
		t.Fatal("leaked token")
	}
}
