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
	if w.Code != 403 {
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
