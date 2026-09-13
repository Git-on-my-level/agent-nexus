package pm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHTTPRejectsActorInjectionAndCannotApproveByDiscussion(t *testing.T) {
	s, _, p, _ := fixture(t)
	h := Handler{Service: s, Authenticate: func(*http.Request) (Principal, error) { return p, nil }}
	for _, tc := range []struct {
		path, body string
		status     int
	}{
		{"/pm/conversations", `{"request_key":"c","title":"Review","actor_id":"admin"}`, 400},
		{"/pm/conversations", `{"request_key":"c","title":"Review"}{}`, 400},
		{"/pm/conversations", `{"request_key":"c","title":"Review"}`, 201},
		{"/pm/conversations", `{"request_key":"c","title":"Changed"}`, 409},
	} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("POST", tc.path, strings.NewReader(tc.body))
		h.ServeHTTP(w, r)
		if w.Code != tc.status {
			t.Fatalf("%d %s", w.Code, w.Body.String())
		}
	}
	h.Authenticate = func(*http.Request) (Principal, error) { return Principal{}, ErrForbidden }
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/pm/conversations", nil))
	if w.Code != 401 {
		t.Fatalf("unauthenticated %d", w.Code)
	}
}
func TestPagedConversationsBoundCursorToPrincipalAndKind(t *testing.T) {
	s, _, p, _ := fixture(t)
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		if _, err := s.CreateConversation(ctx, p, CreateConversation{RequestKey: fmt.Sprint(i), Title: fmt.Sprint(i)}); err != nil {
			t.Fatal(err)
		}
	}
	h := Handler{Service: s, Authenticate: func(*http.Request) (Principal, error) { return p, nil }}
	cursor := ""
	seen := map[string]bool{}
	for page := 0; page < 3; page++ {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", "/pm/conversations?limit=2&cursor="+cursor, nil))
		if w.Code != 200 {
			t.Fatalf("%d %s", w.Code, w.Body.String())
		}
		var out Page[Conversation]
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		for _, c := range out.Items {
			if seen[c.ID] {
				t.Fatal("duplicate across pages")
			}
			seen[c.ID] = true
		}
		if page == 0 {
			if _, err := s.DecisionPage(ctx, p, 2, out.NextCursor); err != ErrInvalid {
				t.Fatalf("cross-kind cursor %v", err)
			}
		}
		cursor = out.NextCursor
		if out.HasMore != (page < 2) {
			t.Fatalf("incorrect coverage %+v", out)
		}
	}
	if len(seen) != 5 || cursor != "" {
		t.Fatalf("incomplete pagination %d %q", len(seen), cursor)
	}
}

func TestConversationShowsNewestHistoryAndPagesBackward(t *testing.T) {
	s, _, p, _ := fixture(t)
	ctx := context.Background()
	c, err := s.CreateConversation(ctx, p, CreateConversation{RequestKey: "history", Title: "History"})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 205; i++ {
		turn := Turn{ID: fmt.Sprintf("turn-%03d", i), ConversationID: c.ID, WorkspaceID: p.WorkspaceID, ActorID: p.ActorID, Text: fmt.Sprint(i), Status: Delivered, Revision: 1}
		if _, err = s.store.insert(ctx, "turn", turn.ID, p.WorkspaceID, p.ActorID, c.ID, turn); err != nil {
			t.Fatal(err)
		}
	}
	detail, err := s.GetConversation(ctx, p, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.Turns) != 200 || detail.Turns[len(detail.Turns)-1].Text != "204" {
		t.Fatalf("newest response is invisible: %d %s", len(detail.Turns), detail.Turns[len(detail.Turns)-1].Text)
	}

	older, err := s.ConversationHistory(ctx, p, c.ID, 200, detail.NextCursor)
	if err != nil {
		t.Fatal(err)
	}
	if !detail.HasMore || len(older.Turns) != 5 || older.HasMore || older.Turns[0].Text != "0" || older.Turns[4].Text != "4" {
		t.Fatalf("older history missing %+v", older)
	}
}

func TestHTTPClaimAndFailTurns(t *testing.T) {
	s, _, p, _ := fixture(t)
	s.deps.Dispatch = nil
	ctx := context.Background()
	c, err := s.CreateConversation(ctx, p, CreateConversation{RequestKey: "http-claim", Title: "Claim"})
	if err != nil {
		t.Fatal(err)
	}
	turn, err := s.PostMessage(ctx, p, c.ID, MessageInput{RequestKey: "q", Text: "What needs my decision?"})
	if err != nil {
		t.Fatal(err)
	}
	agent := Principal{WorkspaceID: "ws", ActorID: "pm-agent"}
	h := Handler{Service: s, Authenticate: func(*http.Request) (Principal, error) { return agent, nil }}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/pm/turns/claim", strings.NewReader(`{"runner_id":"runner-a"}`)))
	if w.Code != 200 {
		t.Fatalf("claim %d %s", w.Code, w.Body.String())
	}
	var claimed Turn
	if err = json.Unmarshal(w.Body.Bytes(), &claimed); err != nil || claimed.ID != turn.ID || claimed.LeaseToken == "" {
		t.Fatalf("claim body %s", w.Body.String())
	}
	empty := httptest.NewRecorder()
	h.Authenticate = func(*http.Request) (Principal, error) {
		return Principal{WorkspaceID: "ws", ActorID: "pm-agent"}, nil
	}
	req := httptest.NewRequest("POST", "/pm/turns/claim", strings.NewReader(`{"runner_id":"runner-b"}`))
	h.ServeHTTP(empty, req)
	if empty.Code != 204 {
		t.Fatalf("second claim %d %s", empty.Code, empty.Body.String())
	}
	fail := httptest.NewRecorder()
	body := fmt.Sprintf(`{"reason":"deadline","lease_token":%q}`, claimed.LeaseToken)
	h.ServeHTTP(fail, httptest.NewRequest("POST", "/pm/turns/"+claimed.ID+"/fail", strings.NewReader(body)))
	if fail.Code != 200 || !strings.Contains(fail.Body.String(), `"failed"`) {
		t.Fatalf("fail %d %s", fail.Code, fail.Body.String())
	}
}

func TestHTTPDecisionOwnerIsTheOnlyAnsweringHuman(t *testing.T) {
	s, _, p, _ := fixture(t)
	ctx := context.Background()
	d, err := s.ProposeDecision(ctx, p, DecisionInput{RequestKey: "actor-bound", WorkRef: "work:1", Instruction: "Review", Scope: "work.annotate", TargetRevision: "r1"})
	if err != nil {
		t.Fatal(err)
	}
	second := Principal{WorkspaceID: p.WorkspaceID, ActorID: "second-human", Human: true}
	h := Handler{Service: s, Authenticate: func(*http.Request) (Principal, error) { return second, nil }}
	read := httptest.NewRecorder()
	h.ServeHTTP(read, httptest.NewRequest("GET", "/pm/decisions/"+d.ID, nil))
	if read.Code != 200 || !strings.Contains(read.Body.String(), `"can_answer":false`) {
		t.Fatalf("read %d %s", read.Code, read.Body.String())
	}
	answer := httptest.NewRecorder()
	h.ServeHTTP(answer, httptest.NewRequest("POST", "/pm/decisions/"+d.ID+"/answer", strings.NewReader(`{"revision":1,"approve":true,"text":"yes"}`)))
	if answer.Code != 403 {
		t.Fatalf("another human answered %d %s", answer.Code, answer.Body.String())
	}
}

func TestHTTPUnconfiguredPMIdentityExplainsUnavailable(t *testing.T) {
	s, _, p, _ := fixture(t)
	s.cfg.AgentActorID = ""
	h := Handler{Service: s, Authenticate: func(*http.Request) (Principal, error) { return p, nil }}
	out := httptest.NewRecorder()
	h.ServeHTTP(out, httptest.NewRequest("POST", "/pm/turns/claim", strings.NewReader(`{"runner_id":"untrusted"}`)))
	if out.Code != 503 || !strings.Contains(out.Body.String(), "ANX_PM_AGENT_ACTOR_ID") {
		t.Fatalf("%d %s", out.Code, out.Body.String())
	}
}
