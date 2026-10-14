package pm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHTTPChatRecoveryAfterDisconnectKeepsFreshWriteGate(t *testing.T) {
	s, st, human, dispatched := fixture(t)
	h := Handler{Service: s, Authenticate: func(*http.Request) (Principal, error) { return human, nil }}
	call := func(path, body string) *httptest.ResponseRecorder {
		t.Helper()
		out := httptest.NewRecorder()
		h.ServeHTTP(out, httptest.NewRequest(http.MethodPost, path, strings.NewReader(body)))
		return out
	}
	creation := `{"request_key":"original-conversation","title":"Original question","actor_id":"human"}`
	out := call("/pm/conversations", creation)
	if out.Code != 201 {
		t.Fatal(out.Code, out.Body.String())
	}
	var c Conversation
	if err := json.Unmarshal(out.Body.Bytes(), &c); err != nil {
		t.Fatal(err)
	}
	message := `{"request_key":"original-message","text":"Keep authored question","actor_id":"human"}`
	path := "/pm/conversations/" + c.ID + "/messages"
	out = call(path, message)
	if out.Code != 202 {
		t.Fatal(out.Code, out.Body.String())
	}
	var turn Turn
	if err := json.Unmarshal(out.Body.Bytes(), &turn); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Disconnect(context.Background(), Principal{WorkspaceID: "ws", ActorID: "pm-agent"}); err != nil {
		t.Fatal(err)
	}
	// Exercise the route after a process restart, not only direct service calls.
	reopened, err := NewStore(st.db)
	if err != nil {
		t.Fatal(err)
	}
	resumed, err := NewService(reopened, s.cfg, s.deps)
	if err != nil {
		t.Fatal(err)
	}
	h.Service = resumed
	for i := 0; i < 3; i++ {
		out = call("/pm/conversations", creation)
		if out.Code != 201 {
			t.Fatal("conversation recovery blocked", out.Code, out.Body.String())
		}
		var replay Conversation
		if err := json.Unmarshal(out.Body.Bytes(), &replay); err != nil || replay.ID != c.ID {
			t.Fatal("different conversation", out.Body.String(), err)
		}
		out = call(path, message)
		if out.Code != 202 {
			t.Fatal("message recovery blocked", out.Code, out.Body.String())
		}
		var replayTurn Turn
		if err := json.Unmarshal(out.Body.Bytes(), &replayTurn); err != nil || replayTurn.ID != turn.ID || replayTurn.Status != Failed {
			t.Fatal("legacy canonical turn lost", out.Body.String(), err)
		}
	}
	if *dispatched != 1 {
		t.Fatal("recovery dispatched twice", *dispatched)
	}
	for _, tc := range []struct{ path, body, code string }{
		{"/pm/conversations", `{"request_key":"fresh","title":"Fresh conversation"}`, "pm_not_onboarded"},
		{path, `{"request_key":"fresh","text":"Fresh question"}`, "pm_not_onboarded"},
		{"/pm/conversations", strings.Replace(creation, "Original question", "Changed title", 1), "conflict"},
		{path, strings.Replace(message, "Keep authored question", "Changed question", 1), "conflict"},
	} {
		out = call(tc.path, tc.body)
		if out.Code != 409 || !strings.Contains(out.Body.String(), tc.code) {
			t.Fatal("unsafe fresh/replayed write", out.Code, out.Body.String())
		}
	}
	for kind, want := range map[string]int{"conversation": 1, "turn": 1} {
		var count int
		if err := st.db.QueryRow("SELECT count(*) FROM pm_records WHERE kind=?", kind).Scan(&count); err != nil || count != want {
			t.Fatal("duplicate records", kind, count, err)
		}
	}
	h.Authenticate = func(*http.Request) (Principal, error) {
		return Principal{WorkspaceID: "ws", ActorID: "other", Human: true}, nil
	}
	if out = call(path, message); out.Code != 403 {
		t.Fatal("principal change recovered private question", out.Code, out.Body.String())
	}
}

func TestHTTPChatRecoveryAndHistoryRecheckPinnedContextAccess(t *testing.T) {
	s, _, human, dispatched := fixture(t)
	authorized := s.deps.Authorize
	revoked := false
	s.deps.Authorize = func(ctx context.Context, p Principal, permission, ref string) error {
		if revoked && ref == "document:private" {
			return ErrForbidden
		}
		return authorized(ctx, p, permission, ref)
	}
	h := Handler{Service: s, Authenticate: func(*http.Request) (Principal, error) { return human, nil }}
	call := func(method, path, body string) *httptest.ResponseRecorder {
		out := httptest.NewRecorder()
		h.ServeHTTP(out, httptest.NewRequest(method, path, strings.NewReader(body)))
		return out
	}
	creation := `{"request_key":"pinned","title":"PRIVATE QUESTION","context_refs":["document:private"]}`
	out := call("POST", "/pm/conversations", creation)
	if out.Code != 201 {
		t.Fatal(out.Code, out.Body.String())
	}
	var c Conversation
	if err := json.Unmarshal(out.Body.Bytes(), &c); err != nil {
		t.Fatal(err)
	}
	path := "/pm/conversations/" + c.ID + "/messages"
	message := `{"request_key":"pinned-message","text":"PRIVATE AUTHORED QUESTION"}`
	out = call("POST", path, message)
	if out.Code != 202 {
		t.Fatal(out.Code, out.Body.String())
	}
	var turn Turn
	if err := json.Unmarshal(out.Body.Bytes(), &turn); err != nil {
		t.Fatal(err)
	}
	revoked = true
	for _, tc := range []struct{ method, path, body string }{
		{"POST", "/pm/conversations", creation},
		{"POST", path, message},
		{"POST", path, `{"request_key":"fresh-message","text":"Another question"}`},
		{"GET", "/pm/conversations/" + c.ID, ""},
		{"GET", "/pm/turns/" + turn.ID, ""},
	} {
		out = call(tc.method, tc.path, tc.body)
		if out.Code != 403 || strings.Contains(out.Body.String(), "PRIVATE") {
			t.Fatal("revoked pinned context leaked", tc.path, out.Code, out.Body.String())
		}
	}
	if *dispatched != 1 {
		t.Fatal("revoked request dispatched", *dispatched)
	}
}
