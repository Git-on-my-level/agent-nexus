package pm

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"agent-nexus-core/internal/blob"
	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/storage"
)

type messageReplayBackend struct {
	blob.Backend
	failPromotion bool
}
type messageReplayStage struct {
	blob.StagedWrite
	fail bool
}

func (s messageReplayStage) Promote() error {
	if s.fail {
		return errors.New("injected promotion failure")
	}
	return s.StagedWrite.Promote()
}
func (b *messageReplayBackend) Write(ctx context.Context, hash string, data []byte) (blob.StagedWrite, error) {
	s, err := b.Backend.Write(ctx, hash, data)
	if err != nil {
		return nil, err
	}
	return messageReplayStage{s, b.failPromotion}, nil
}

func messageReplayFixture(t *testing.T) (*Service, *Store, *primitives.Store, Principal, Conversation, *messageReplayBackend) {
	t.Helper()
	ctx := context.Background()
	ws, err := storage.InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ws.Close() })
	db := ws.DB()
	db.SetMaxOpenConns(1)
	b := &messageReplayBackend{Backend: blob.NewFilesystemBackend(ws.Layout().ArtifactContentDir)}
	ps := primitives.NewStore(db, b, ws.Layout().ArtifactContentDir, primitives.WithWorkspaceQuota(primitives.WorkspaceQuota{MaxArtifacts: 1000, MaxBlobBytes: 1 << 30}))
	st, err := NewStore(db)
	if err != nil {
		t.Fatal(err)
	}
	bridge := NexusBridge{Store: ps, ActorID: "system", Ready: func(ctx context.Context, _, _ string) error {
		// A raw auth-style read with a one-connection pool proves that readiness
		// is outside the bound writer; inside it this would deadlock.
		var n int
		return db.QueryRowContext(ctx, "SELECT 1").Scan(&n)
	}}
	s, err := NewService(st, Config{WorkspaceID: "ws", AgentActorID: "pm", AgentHandle: "pm", TurnTimeout: time.Minute, MaxOutputBytes: 8000, MaxQueued: 20}, Dependencies{
		Authorize:    func(context.Context, Principal, string, string) error { return nil },
		EnsureThread: bridge.EnsureThread, ReplayMessage: bridge.ReplayMessage,
		Dispatch: bridge.Dispatch, PreflightDispatch: bridge.PreflightDispatch, PersistDispatch: bridge.PersistDispatch,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.notePresence(ctx, Principal{WorkspaceID: "ws", ActorID: "pm"}, "connect"); err != nil {
		t.Fatal(err)
	}
	p := Principal{WorkspaceID: "ws", ActorID: "human", Human: true}
	c, err := s.CreateConversation(ctx, p, CreateConversation{RequestKey: "conversation", Title: "Question", ContextRefs: []string{"document:private"}})
	if err != nil {
		t.Fatal(err)
	}
	return s, st, ps, p, c, b
}

func TestMessageOriginalReceiptConcurrentRestartAndLegacyCompatibility(t *testing.T) {
	s, st, _, p, c, _ := messageReplayFixture(t)
	ctx := context.Background()
	in := MessageInput{ActorID: p.ActorID, ReplayKey: "receipt", RequestKey: "message", Text: "PRIVATE QUESTION"}
	h := Handler{Service: s, Authenticate: func(*http.Request) (Principal, error) { return p, nil }}
	call := func(conversation string, in MessageInput) *httptest.ResponseRecorder {
		raw, _ := json.Marshal(in)
		out := httptest.NewRecorder()
		h.ServeHTTP(out, httptest.NewRequest("POST", "/pm/conversations/"+conversation+"/messages", strings.NewReader(string(raw))))
		return out
	}
	results := make(chan *httptest.ResponseRecorder, 8)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); results <- call(c.ID, in) }()
	}
	wg.Wait()
	close(results)
	var original string
	var turn Turn
	for out := range results {
		if out.Code != 202 {
			t.Fatal(out.Code, out.Body.String())
		}
		if original == "" {
			original = out.Body.String()
			if err := json.Unmarshal(out.Body.Bytes(), &turn); err != nil {
				t.Fatal(err)
			}
		}
		if out.Body.String() != original {
			t.Fatal("receipt differs", out.Body.String(), original)
		}
	}
	for _, table := range []string{"events", "artifacts", "agent_wakeups", "blob_usage_ledger", "idempotency_replays"} {
		var count int
		if err := st.db.QueryRow("SELECT count(*) FROM " + table).Scan(&count); err != nil || count != 1 {
			t.Fatal(table, count, err)
		}
	}
	for _, mutate := range []func(*MessageInput){
		func(in *MessageInput) { in.Text = "changed" },
		func(in *MessageInput) { in.RequestKey = "changed" },
		func(in *MessageInput) { in.ActorID = "" },
		func(in *MessageInput) { in.ReplayKey = "different-receipt" },
	} {
		changed := in
		mutate(&changed)
		if out := call(c.ID, changed); out.Code != 409 {
			t.Fatal("changed request accepted", out.Code, out.Body.String())
		}
	}
	other, err := s.CreateConversation(ctx, p, CreateConversation{RequestKey: "other", Title: "Other"})
	if err != nil {
		t.Fatal(err)
	}
	if out := call(other.ID, in); out.Code != 409 {
		t.Fatal("changed target accepted", out.Code, out.Body.String())
	}
	turn.Status, turn.Response, turn.Revision = Reported, "New canonical response", 3
	turn.LeaseToken, turn.TerminalLeaseHash = "PRIVATE LEASE", "PRIVATE HASH"
	if err = st.cas(ctx, "turn", turn.ID, 2, turn); err != nil {
		t.Fatal(err)
	}
	if out := call(c.ID, in); out.Code != 202 || out.Body.String() != original {
		t.Fatal("receipt advanced", out.Code, out.Body.String())
	}
	legacy := in
	legacy.ReplayKey = ""
	if out := call(c.ID, legacy); out.Code != 202 || !strings.Contains(out.Body.String(), "New canonical response") || strings.Contains(out.Body.String(), "PRIVATE LEASE") || strings.Contains(out.Body.String(), "PRIVATE HASH") {
		t.Fatal("legacy projection broken", out.Code, out.Body.String())
	}
	if _, err = s.Disconnect(ctx, Principal{WorkspaceID: "ws", ActorID: "pm"}); err != nil {
		t.Fatal(err)
	}
	reopened, err := NewStore(st.db)
	if err != nil {
		t.Fatal(err)
	}
	resumed, err := NewService(reopened, s.cfg, s.deps)
	if err != nil {
		t.Fatal(err)
	}
	h.Service = resumed
	if out := call(c.ID, in); out.Code != 202 || out.Body.String() != original {
		t.Fatal("restart/disconnect receipt lost", out.Code, out.Body.String())
	}
	fresh := in
	fresh.RequestKey, fresh.ReplayKey = "fresh", "fresh-receipt"
	if out := call(c.ID, fresh); out.Code != 409 || !strings.Contains(out.Body.String(), "pm_not_onboarded") {
		t.Fatal("fresh setup gate bypassed", out.Code, out.Body.String())
	}
	resumed.deps.Authorize = func(context.Context, Principal, string, string) error { return ErrForbidden }
	if out := call(c.ID, in); out.Code != 403 || strings.Contains(out.Body.String(), "PRIVATE") {
		t.Fatal("revoked access leaked", out.Code, out.Body.String())
	}
}

func TestMessageOriginalReceiptRollsBackAllDispatchWrites(t *testing.T) {
	for name, trigger := range map[string]string{
		"turn":      `CREATE TRIGGER fail_send BEFORE INSERT ON pm_records WHEN NEW.kind='turn' BEGIN SELECT RAISE(ABORT,'turn failure'); END`,
		"sending":   `CREATE TRIGGER fail_send BEFORE UPDATE ON pm_records WHEN NEW.kind='turn' BEGIN SELECT RAISE(ABORT,'sending failure'); END`,
		"event":     `CREATE TRIGGER fail_send BEFORE INSERT ON events BEGIN SELECT RAISE(ABORT,'event failure'); END`,
		"artifact":  `CREATE TRIGGER fail_send BEFORE INSERT ON artifacts BEGIN SELECT RAISE(ABORT,'artifact failure'); END`,
		"ledger":    `CREATE TRIGGER fail_send BEFORE INSERT ON blob_usage_ledger BEGIN SELECT RAISE(ABORT,'ledger failure'); END`,
		"queue":     `CREATE TRIGGER fail_send BEFORE INSERT ON agent_wakeups BEGIN SELECT RAISE(ABORT,'queue failure'); END`,
		"receipt":   `CREATE TRIGGER fail_send BEFORE INSERT ON idempotency_replays BEGIN SELECT RAISE(ABORT,'receipt failure'); END`,
		"promotion": "", "quota": "", "readiness": "",
	} {
		t.Run(name, func(t *testing.T) {
			s, st, ps, p, c, backend := messageReplayFixture(t)
			ctx := context.Background()
			if _, err := st.db.Exec(`DELETE FROM blob_usage_totals; DELETE FROM blob_usage_ledger`); err != nil {
				t.Fatal(err)
			}
			if trigger != "" {
				if _, err := st.db.Exec(trigger); err != nil {
					t.Fatal(err)
				}
			}
			if name == "promotion" {
				backend.failPromotion = true
			}
			if name == "quota" {
				primitives.WithWorkspaceQuota(primitives.WorkspaceQuota{MaxUploadBytes: 1})(ps)
			}
			preflight := s.deps.PreflightDispatch
			if name == "readiness" {
				s.deps.PreflightDispatch = func(context.Context, DispatchRequest) error { return ErrUnavailable }
			}
			in := MessageInput{RequestKey: "message", ReplayKey: "receipt", Text: "Keep this question"}
			if _, err := s.PostMessage(ctx, p, c.ID, in); err == nil {
				t.Fatal("failure accepted")
			}
			for _, table := range []string{"events", "artifacts", "agent_wakeups", "blob_usage_ledger", "blob_usage_totals", "idempotency_replays"} {
				var count int
				if err := st.db.QueryRow("SELECT count(*) FROM " + table).Scan(&count); err != nil || count != 0 {
					t.Fatal("escaped rollback", table, count, err)
				}
			}
			var count int
			if err := st.db.QueryRow(`SELECT count(*) FROM pm_records WHERE kind='turn'`).Scan(&count); err != nil || count != 0 {
				t.Fatal("turn escaped", count, err)
			}
			if trigger != "" {
				if _, err := st.db.Exec(`DROP TRIGGER fail_send`); err != nil {
					t.Fatal(err)
				}
			}
			backend.failPromotion = false
			primitives.WithWorkspaceQuota(primitives.WorkspaceQuota{MaxArtifacts: 1000, MaxBlobBytes: 1 << 30})(ps)
			s.deps.PreflightDispatch = preflight
			if _, err := s.PostMessage(ctx, p, c.ID, in); err != nil {
				t.Fatal("exact retry failed", err)
			}
		})
	}
}

func TestMessageReceiptRejectsUnauditedCallbacksAndLegacyIDs(t *testing.T) {
	s, _, p, _ := fixture(t)
	in := MessageInput{RequestKey: "message", ReplayKey: "receipt", Text: "Question"}
	c, err := s.CreateConversation(context.Background(), p, CreateConversation{RequestKey: "conversation", Title: "Question"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.PostMessage(context.Background(), p, c.ID, in); !errors.Is(err, ErrUnavailable) {
		t.Fatal("unaudited dispatch accepted", err)
	}
	for _, kind := range []string{"turn", "event", "artifact", "queue"} {
		t.Run(kind, func(t *testing.T) {
			s, st, ps, p, c, _ := messageReplayFixture(t)
			ctx := context.Background()
			id := stableID("turn", c.ID, in.RequestKey)
			wake := stableID("wake", id)
			var err error
			switch kind {
			case "turn":
				legacy := in
				legacy.ReplayKey = ""
				_, err = s.PostMessage(ctx, p, c.ID, legacy)
			case "event":
				_, err = ps.AppendEvent(ctx, p.ActorID, map[string]any{"id": id, "type": "message_posted", "thread_id": c.ThreadID, "refs": []string{"thread:" + c.ThreadID}, "payload": map[string]any{"text": "Unrelated event"}})
			case "artifact":
				_, err = ps.CreateArtifact(ctx, p.ActorID, map[string]any{"id": wake, "kind": "report", "refs": []string{"thread:" + c.ThreadID}}, "Unrelated artifact", "text")
			case "queue":
				_, err = ps.UpsertAgentWakeup(ctx, primitives.AgentWakeup{WakeupID: wake, TargetActorID: "pm", Status: "requested"})
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, err = s.PostMessage(ctx, p, c.ID, in); !errors.Is(err, ErrConflict) {
				t.Fatal("legacy ID adopted", err)
			}
			var receipts, turns int
			if err = st.db.QueryRow(`SELECT count(*) FROM idempotency_replays`).Scan(&receipts); err != nil || receipts != 0 {
				t.Fatal("manufactured receipt", receipts, err)
			}
			wantTurns := 0
			if kind == "turn" {
				wantTurns = 1
			}
			if err = st.db.QueryRow(`SELECT count(*) FROM pm_records WHERE kind='turn'`).Scan(&turns); err != nil || turns != wantTurns {
				t.Fatal("collision admitted turn", turns, err)
			}
		})
	}
}
