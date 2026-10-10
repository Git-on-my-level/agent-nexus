package pm

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestNotOnboardedGuidanceSelectsPMProfile(t *testing.T) {
	message := ErrNotOnboarded.Error()
	if !strings.Contains(message, "anx --as pm pm install") || strings.Contains(message, "anx pm install") {
		t.Fatalf("onboarding guidance does not select the PM profile: %q", message)
	}
}

func TestOnboardingGatesBeforeConnectionAndKeepsOffline(t *testing.T) {
	s, st, human, _ := fixture(t)
	ctx := context.Background()
	if _, err := st.db.Exec(`DELETE FROM pm_presence`); err != nil {
		t.Fatal(err)
	}
	state, err := s.Presence(ctx, human)
	if err != nil || state.State != "not_onboarded" || state.LastSeen != nil {
		t.Fatalf("initial %+v %v", state, err)
	}
	if _, err = s.CreateConversation(ctx, human, CreateConversation{RequestKey: "blocked", Title: "Question", WorkRef: "card:one"}); !errors.Is(err, ErrNotOnboarded) {
		t.Fatalf("silently queued: %v", err)
	}
	if _, err = s.ProposeDecision(ctx, human, DecisionInput{WorkRef: "card:one"}); !errors.Is(err, ErrNotOnboarded) {
		t.Fatalf("proposal gate %v", err)
	}
	if _, err = s.Connect(ctx, human, ConnectionInput{Runner: "custom", Host: "computer"}); !errors.Is(err, ErrForbidden) {
		t.Fatal("human connected", err)
	}
	pm := Principal{WorkspaceID: "ws", ActorID: "pm-agent"}
	state, err = s.Connect(ctx, pm, ConnectionInput{Runner: "Hermes", Host: "laptop"})
	if err != nil || state.State != "connected" || state.Runner == nil || *state.Runner != "Hermes" || state.Host == nil || *state.Host != "laptop" {
		t.Fatalf("connection %+v %v", state, err)
	}
	if _, err = st.db.Exec(`UPDATE pm_presence SET last_seen_at=?`, time.Now().Add(-2*time.Minute).UTC().Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	state, err = s.Presence(ctx, human)
	if err != nil || state.State != "offline" || state.LastSeen == nil {
		t.Fatalf("offline %+v %v", state, err)
	}
	s.deps.Dispatch = nil
	conv, err := s.CreateConversation(ctx, human, CreateConversation{RequestKey: "offline", Title: "Question"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.PostMessage(ctx, human, conv.ID, MessageInput{RequestKey: "ask", Text: "Hello"}); err != nil {
		t.Fatalf("offline workflow: %v", err)
	}
}

func TestConnectionSelectionCASPersistenceAndExplicitOverride(t *testing.T) {
	original, st, _, _ := fixture(t)
	ctx := context.Background()
	cfg := original.cfg
	cfg.AgentActorID = ""
	a, err := NewService(st, cfg, original.deps)
	if err != nil {
		t.Fatal(err)
	}
	b, err := NewService(st, cfg, original.deps)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for i, s := range []*Service{a, b} {
		wg.Add(1)
		go func(i int, s *Service) {
			defer wg.Done()
			actor := []string{"candidate-a", "candidate-b"}[i]
			_, e := s.Connect(ctx, Principal{WorkspaceID: "ws", ActorID: actor}, ConnectionInput{Runner: "custom", Host: "laptop"})
			results <- e
		}(i, s)
	}
	wg.Wait()
	close(results)
	success := 0
	conflict := 0
	for e := range results {
		if e == nil {
			success++
		} else if errors.Is(e, ErrConflict) {
			conflict++
		} else {
			t.Fatal(e)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatalf("selection successes=%d conflicts=%d", success, conflict)
	}
	restarted, err := NewService(st, cfg, original.deps)
	if err != nil {
		t.Fatal(err)
	}
	if restarted.AgentActorID() == "" {
		t.Fatal("selection lost on restart")
	}
	if err = restarted.RequireOnboarded(ctx); err != nil {
		t.Fatal(err)
	}
	cfg.AgentActorID = "explicit"
	explicit, err := NewService(st, cfg, original.deps)
	if err != nil {
		t.Fatal(err)
	}
	if err = explicit.RequireOnboarded(ctx); !errors.Is(err, ErrNotOnboarded) {
		t.Fatalf("old actor counted: %v", err)
	}
	if _, err = explicit.Connect(ctx, Principal{WorkspaceID: "ws", ActorID: "explicit"}, ConnectionInput{Runner: "Claude Code", Host: "desktop"}); err != nil {
		t.Fatal(err)
	}
	if explicit.AgentActorID() != "explicit" {
		t.Fatal("explicit selection lost")
	}
}

func TestConnectionRejectsUnsafeLabels(t *testing.T) {
	s, _, _, _ := fixture(t)
	for _, label := range []string{"", " runner", "a\nsecret", "/private/path", "https://token@example.test", string(make([]byte, 81))} {
		if _, err := s.Connect(context.Background(), Principal{WorkspaceID: "ws", ActorID: "pm-agent"}, ConnectionInput{Runner: label, Host: "laptop"}); !errors.Is(err, ErrInvalid) {
			t.Fatalf("label %q: %v", label, err)
		}
	}
}

func TestFirstSeenMigrationAndReconnectAreDurable(t *testing.T) {
	s, st, _, _ := fixture(t)
	const first = "2020-01-01T00:00:00Z"
	if _, err := st.db.Exec(`DROP TABLE pm_presence; CREATE TABLE pm_presence(workspace_id TEXT NOT NULL,actor_id TEXT NOT NULL,last_seen_at TEXT NOT NULL,signal TEXT NOT NULL,PRIMARY KEY(workspace_id,actor_id)); INSERT INTO pm_presence VALUES('ws','pm-agent','2020-01-01T00:00:00Z','claim');`); err != nil {
		t.Fatal(err)
	}
	if _, err := NewStore(st.db); err != nil {
		t.Fatal(err)
	}
	if err := s.notePresence(context.Background(), Principal{WorkspaceID: "ws", ActorID: "pm-agent"}, "connect"); err != nil {
		t.Fatal(err)
	}
	var firstSeen, lastSeen string
	if err := st.db.QueryRow(`SELECT first_seen_at,last_seen_at FROM pm_presence WHERE workspace_id='ws' AND actor_id='pm-agent'`).Scan(&firstSeen, &lastSeen); err != nil {
		t.Fatal(err)
	}
	if firstSeen != first || lastSeen == first {
		t.Fatalf("first=%q last=%q", firstSeen, lastSeen)
	}
	if _, err := NewStore(st.db); err != nil {
		t.Fatal(err)
	}
}

func TestUpgradeBackfillsOnboardingAndNeverStrandsPendingDecision(t *testing.T) {
	original, st, human, _ := fixture(t)
	ctx := context.Background()
	decision, err := original.ProposeDecision(ctx, human, DecisionInput{RequestKey: "old-pending", WorkRef: "card:one", Instruction: `{"next_action":"Review"}`, Scope: "work.annotate", TargetRevision: "r1"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = st.db.Exec(`UPDATE pm_records SET body=json_set(body,'$.created_at','2020-01-01T00:00:00Z') WHERE kind='decision'; DROP TABLE pm_presence; DROP TABLE pm_onboarding_backfill;`); err != nil {
		t.Fatal(err)
	}
	st, err = NewStore(st.db)
	if err != nil {
		t.Fatal(err)
	}
	for _, direction := range []string{"ASC", "DESC"} {
		rows, e := st.db.Query(`EXPLAIN QUERY PLAN SELECT rtrim(COALESCE(json_extract(body,'$.created_at'),''),'Z') FROM pm_records WHERE kind=? AND workspace_id=? AND rtrim(COALESCE(json_extract(body,'$.created_at'),''),'Z')>'' ORDER BY rtrim(COALESCE(json_extract(body,'$.created_at'),''),'Z') `+direction+` LIMIT 1`, "decision", "ws")
		if e != nil {
			t.Fatal(e)
		}
		indexed := false
		for rows.Next() {
			var id, parent, unused int
			var detail string
			if e = rows.Scan(&id, &parent, &unused, &detail); e != nil {
				t.Fatal(e)
			}
			indexed = indexed || strings.Contains(detail, "pm_records_page") && strings.Contains(detail, "INDEX")
			if strings.Contains(detail, "SCAN") {
				t.Fatal(detail)
			}
		}
		e = rows.Err()
		rows.Close()
		if e != nil || !indexed {
			t.Fatalf("backfill plan indexed=%t err=%v", indexed, e)
		}
	}
	upgraded, err := NewService(st, original.cfg, original.deps)
	if err != nil {
		t.Fatal(err)
	}
	state, err := upgraded.Presence(ctx, human)
	if err != nil || state.State != "offline" || state.LastSeen == nil || *state.LastSeen != "2020-01-01T00:00:00Z" {
		t.Fatalf("upgrade state %+v %v", state, err)
	}
	var first string
	if err = st.db.QueryRow(`SELECT first_seen_at FROM pm_onboarding_backfill WHERE workspace_id='ws'`).Scan(&first); err != nil || first != "2020-01-01T00:00:00Z" {
		t.Fatalf("first seen %q %v", first, err)
	}
	cfg := original.cfg
	cfg.AgentActorID = "replacement-pm"
	replacement, err := NewService(st, cfg, original.deps)
	if err != nil {
		t.Fatal(err)
	}
	state, err = replacement.Presence(ctx, human)
	if err != nil || state.State != "not_onboarded" {
		t.Fatalf("replacement state %+v %v", state, err)
	}
	handler := Handler{Service: replacement, Authenticate: func(*http.Request) (Principal, error) { return human, nil }}
	call := func(method, path, body string, want int) string {
		t.Helper()
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest(method, path, strings.NewReader(body)))
		if w.Code != want {
			t.Fatalf("%s %s: %d %s", method, path, w.Code, w.Body.String())
		}
		return w.Body.String()
	}
	if body := call("GET", "/pm/decisions", "", 200); !strings.Contains(body, decision.ID) || !strings.Contains(body, "awaiting_answer") {
		t.Fatal(body)
	}
	call("GET", "/pm/decisions/"+decision.ID, "", 200)
	if body := call("POST", "/pm/decisions/"+decision.ID+"/answer", `{"revision":1,"approve":false,"text":"Declined"}`, 200); !strings.Contains(body, "declined") {
		t.Fatal(body)
	}
	call("POST", "/pm/decisions", `{}`, 409)
	call("POST", "/pm/conversations", `{}`, 409)
	if _, err = replacement.AnswerDecision(ctx, Principal{WorkspaceID: "ws", ActorID: "other", Human: true}, decision.ID, AnswerInput{Revision: 1, Text: "deny"}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("approval boundary %v", err)
	}
}
