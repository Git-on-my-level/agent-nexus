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
