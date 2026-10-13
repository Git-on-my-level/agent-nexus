package pm

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestHTTPDecisionReplayPreservesOriginalProjection(t *testing.T) {
	s, _, p, _ := fixture(t)
	ctx := context.Background()
	d, err := s.ProposeDecision(ctx, p, DecisionInput{RequestKey: "http-proposal", WorkRef: "work:1", Instruction: "Assign owner", Scope: "assignment", TargetRevision: "r1"})
	if err != nil {
		t.Fatal(err)
	}
	h := Handler{Service: s, Authenticate: func(*http.Request) (Principal, error) { return p, nil }}
	body := `{"request_key":"http-answer","revision":1,"approve":true,"text":"Original answer"}`
	answer := func() *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("POST", "/pm/decisions/"+d.ID+"/answer", strings.NewReader(body)))
		return w
	}
	first := answer()
	if first.Code != 200 {
		t.Fatalf("initial answer: %d %s", first.Code, first.Body.String())
	}
	s.deps.DecisionWork = func(context.Context, Principal, string) (DecisionWork, error) {
		return DecisionWork{Revision: "r2", Phase: "done"}, nil
	}
	again := answer()
	if again.Code != 200 || again.Body.String() != first.Body.String() {
		t.Fatalf("changed HTTP receipt: %d %s vs %s", again.Code, again.Body.String(), first.Body.String())
	}
	s.deps.Authorize = func(context.Context, Principal, string, string) error { return ErrForbidden }
	if revoked := answer(); revoked.Code != 403 {
		t.Fatalf("revoked replay: %d %s", revoked.Code, revoked.Body.String())
	}
}

func TestKeyedDecisionAnswerIsAtomicAndReplaysOriginalResult(t *testing.T) {
	s, st, p, _ := fixture(t)
	ctx := context.Background()
	d, err := s.ProposeDecision(ctx, p, DecisionInput{RequestKey: "proposal", WorkRef: "work:1", Instruction: "Assign owner", Scope: "assignment", TargetRevision: "r1"})
	if err != nil {
		t.Fatal(err)
	}
	in := AnswerInput{RequestKey: "answer", ActorID: p.ActorID, Revision: 1, Approve: true, Text: "Keep this exact answer"}
	// Abort precisely at replay insertion: neither the answer nor its action
	// may survive a partial commit.
	if _, err = st.db.Exec(`CREATE TRIGGER reject_pm_replay BEFORE INSERT ON idempotency_replays BEGIN SELECT RAISE(ABORT,'injected replay failure'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err = s.AnswerDecision(ctx, p, d.ID, in); err == nil {
		t.Fatal("expected injected failure")
	}
	var unchanged Decision
	if err = st.get(ctx, "decision", d.ID, &unchanged); err != nil || unchanged.Revision != 1 || unchanged.Status != AwaitingAnswer {
		t.Fatalf("partial answer: %+v %v", unchanged, err)
	}
	var count int
	if err = st.db.QueryRow(`SELECT count(*) FROM pm_records WHERE kind='action'`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("partial action: %d %v", count, err)
	}
	if _, err = st.db.Exec(`DROP TRIGGER reject_pm_replay`); err != nil {
		t.Fatal(err)
	}
	type reply struct {
		d   Decision
		err error
	}
	results := make(chan reply, 8)
	start := make(chan struct{})
	for i := 0; i < 8; i++ {
		go func() { <-start; d, err := s.AnswerDecision(ctx, p, d.ID, in); results <- reply{d, err} }()
	}
	close(start)
	first := <-results
	if first.err != nil {
		t.Fatal(first.err)
	}
	for i := 1; i < 8; i++ {
		next := <-results
		if next.err != nil || !reflect.DeepEqual(next.d, first.d) {
			t.Fatalf("different replay: %+v %+v", first, next)
		}
	}
	s.deps.DecisionWork = func(context.Context, Principal, string) (DecisionWork, error) {
		return DecisionWork{Revision: "r2", Phase: "done"}, nil
	}
	again, err := s.AnswerDecision(ctx, p, d.ID, in)
	if err != nil || !reflect.DeepEqual(again, first.d) {
		t.Fatalf("original result not replayed: %+v %v", again, err)
	}
	changed := in
	changed.Text = "Different answer"
	if _, err = s.AnswerDecision(ctx, p, d.ID, changed); !errors.Is(err, ErrConflict) {
		t.Fatalf("changed key reuse: %v", err)
	}
	if err = st.db.QueryRow(`SELECT count(*) FROM pm_records WHERE kind='action'`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("action count: %d %v", count, err)
	}
	s.deps.Authorize = func(context.Context, Principal, string, string) error { return ErrForbidden }
	if _, err = s.AnswerDecision(ctx, p, d.ID, in); !errors.Is(err, ErrForbidden) {
		t.Fatalf("replay bypassed revoked permission: %v", err)
	}
}

func TestPMReplayAfterDisconnectDoesNotDispatchAgain(t *testing.T) {
	s, _, p, count := fixture(t)
	ctx := context.Background()
	c, err := s.CreateConversation(ctx, p, CreateConversation{RequestKey: "conversation", ActorID: p.ActorID, Title: "Original conversation"})
	if err != nil {
		t.Fatal(err)
	}
	message := MessageInput{RequestKey: "message", ActorID: p.ActorID, Text: "Exact original question"}
	turn, err := s.PostMessage(ctx, p, c.ID, message)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.notePresence(ctx, Principal{WorkspaceID: p.WorkspaceID, ActorID: s.AgentActorID()}, "disconnect"); err != nil {
		t.Fatal(err)
	}
	recovered, err := s.CreateConversation(ctx, p, CreateConversation{RequestKey: "conversation", ActorID: p.ActorID, Title: "Original conversation"})
	if err != nil || !reflect.DeepEqual(recovered, c) {
		t.Fatalf("conversation replay: %+v %v", recovered, err)
	}
	again, err := s.PostMessage(ctx, p, c.ID, message)
	if err != nil || again.ID != turn.ID || again.Text != turn.Text || *count != 1 {
		t.Fatalf("turn replay: %+v %v dispatches=%d", again, err, *count)
	}
	message.Text = "Changed question"
	if _, err = s.PostMessage(ctx, p, c.ID, message); !errors.Is(err, ErrConflict) {
		t.Fatalf("changed message: %v", err)
	}
	if _, err = s.PostMessage(ctx, p, c.ID, MessageInput{ActorID: "another-account", RequestKey: "message", Text: turn.Text}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("account switch was not rejected: %v", err)
	}
}

func TestConcurrentPMRecoveryCreatesOneConversationAndDispatch(t *testing.T) {
	s, st, p, dispatches := fixture(t)
	ctx := context.Background()
	start := make(chan struct{})
	results := make(chan error, 8)
	for i := 0; i < 8; i++ {
		go func() {
			<-start
			c, err := s.CreateConversation(ctx, p, CreateConversation{RequestKey: "same-conversation", Title: "Recovered question", ActorID: p.ActorID})
			if err == nil {
				_, err = s.PostMessage(ctx, p, c.ID, MessageInput{RequestKey: "same-message", Text: "Keep the same question", ActorID: p.ActorID})
			}
			results <- err
		}()
	}
	close(start)
	for i := 0; i < 8; i++ {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	if *dispatches != 1 {
		t.Fatalf("dispatches: %d", *dispatches)
	}
	for _, kind := range []string{"conversation", "turn"} {
		var count int
		if err := st.db.QueryRow(`SELECT count(*) FROM pm_records WHERE kind=?`, kind).Scan(&count); err != nil || count != 1 {
			t.Fatalf("%s count=%d error=%v", kind, count, err)
		}
	}
}
