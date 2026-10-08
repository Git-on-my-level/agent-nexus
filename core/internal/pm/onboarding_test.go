package pm

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

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
