package commandcenter_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	cc "agent-nexus-core/internal/commandcenter"
	"agent-nexus-core/internal/storage"
)

func fixture(t *testing.T) *cc.Store {
	t.Helper()
	w, e := storage.InitializeWorkspace(context.Background(), t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = w.Close() })
	return cc.NewStore(w.DB(), cc.SQLIdentities{DB: w.DB()})
}
func observation(agent, external string, at time.Time) cc.Run {
	return cc.Run{Launcher: "agentctl", ExternalID: external, HostID: "host-1", AgentID: agent, Adapter: "codex", State: "running", Liveness: "alive", Labels: []string{"anx.card.fix-login"}, LastObservedAt: at.UTC().Format(time.RFC3339Nano)}
}
func TestRunUpsertAndFilters(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	now := time.Now().UTC()
	in := observation("agent-1", "exec-one", now)
	first, created, replayed, e := s.UpsertRun(ctx, in)
	if e != nil || !created || replayed || first.CardRef == nil || *first.CardRef != "card:fix-login" {
		t.Fatalf("first upsert: %+v %v %v %v", first, created, replayed, e)
	}
	same, c, r, e := s.UpsertRun(ctx, in)
	if e != nil || c || !r || same.ID != first.ID {
		t.Fatalf("replay: %+v %v %v %v", same, c, r, e)
	}
	terminal := in
	terminal.State = "completed"
	terminal.Liveness = "stale"
	terminal.LastObservedAt = now.Add(time.Minute).Format(time.RFC3339Nano)
	done, _, _, e := s.UpsertRun(ctx, terminal)
	if e != nil || done.State != "completed" {
		t.Fatalf("complete: %+v %v", done, e)
	}
	late := in
	late.LastObservedAt = now.Add(2 * time.Minute).Format(time.RFC3339Nano)
	late.Liveness = "alive"
	got, _, _, e := s.UpsertRun(ctx, late)
	if e != nil || got.State != "completed" || got.Liveness != "alive" || got.LastObservedAt != late.LastObservedAt {
		t.Fatalf("late liveness: %+v %v", got, e)
	}
	conflict := in
	conflict.AgentID = "other"
	if _, _, _, e = s.UpsertRun(ctx, conflict); !errors.Is(e, cc.ErrIdentityConflict) {
		t.Fatalf("identity conflict: %v", e)
	}
	regression := terminal
	regression.State = "failed"
	regression.LastObservedAt = now.Add(3 * time.Minute).Format(time.RFC3339Nano)
	if _, _, _, e = s.UpsertRun(ctx, regression); !errors.Is(e, cc.ErrStateRegression) {
		t.Fatalf("terminal regression: %v", e)
	}
	invalid := in
	invalid.ExternalID = "exec-two"
	invalid.Labels = []string{"anx.card.a", "anx.card.b"}
	if _, _, _, e = s.UpsertRun(ctx, invalid); !errors.Is(e, cc.ErrInvalid) {
		t.Fatalf("ambiguous label: %v", e)
	}
	explicit := "card:explicit"
	invalid.CardRef = &explicit
	if _, _, _, e = s.UpsertRun(ctx, invalid); e != nil {
		t.Fatalf("explicit link: %v", e)
	}
	for _, f := range []cc.Filter{{CardRef: "card:fix-login", Limit: 50}, {AgentID: "agent-1", Limit: 50}, {HostID: "host-1", Limit: 50}, {State: "completed", Limit: 50}} {
		runs, _, e := s.ListRuns(ctx, f)
		if e != nil || len(runs) == 0 {
			t.Fatalf("filter %+v: %v %v", f, runs, e)
		}
	}
	active := true
	runs, _, e := s.ListRuns(ctx, cc.Filter{Active: &active, Limit: 50})
	if e != nil || len(runs) != 1 || runs[0].ExternalID != "exec-two" {
		t.Fatalf("active filter: %+v %v", runs, e)
	}
	provisional := observation("agent-1", "exec-provisional", now.Add(10*time.Minute))
	provisional.State = "unknown"
	provisional.Liveness = "unknown"
	provisional.Labels = []string{}
	if _, _, _, e = s.UpsertRun(ctx, provisional); e != nil {
		t.Fatal(e)
	}
	callback := observation("agent-1", "exec-provisional", now.Add(9*time.Minute))
	filled, _, _, e := s.UpsertRun(ctx, callback)
	if e != nil || filled.State != "running" || filled.LastObservedAt != provisional.LastObservedAt || filled.CardRef == nil {
		t.Fatalf("provisional callback: %+v %v", filled, e)
	}
}
func TestPresenceAndDerivedStates(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	now := time.Now().UTC()
	db := s.DB
	agents := []struct{ id, actor string }{{"working", "actor-working"}, {"waiting", "actor-waiting"}, {"idle", "actor-idle"}, {"stale", "actor-stale"}}
	for _, a := range agents {
		meta, _ := json.Marshal(map[string]any{"principal_kind": "agent", "identity_kind": "derived", "host_id": "host-1", "host_slug": "host", "name": "codex"})
		_, e := db.ExecContext(ctx, "INSERT INTO actors(id,display_name,tags_json,created_at,metadata_json) VALUES(?,?,'[]',?,'{}')", a.actor, a.id, now.Format(time.RFC3339Nano))
		if e != nil {
			t.Fatal(e)
		}
		_, e = db.ExecContext(ctx, "INSERT INTO agents(id,username,actor_id,created_at,updated_at,metadata_json) VALUES(?,?,?,?,?,?)", a.id, a.id+".host", a.actor, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano), string(meta))
		if e != nil {
			t.Fatal(e)
		}
	}
	note := "Implementing"
	card := "card:fix-login"
	p, e := s.PatchPresence(ctx, "working", cc.PresencePatch{SetNote: true, Note: &note, SetCard: true, CardRef: &card}, now)
	if e != nil || p.Note == nil || *p.Note != note {
		t.Fatalf("presence: %+v %v", p, e)
	}
	run := observation("working", "exec-work", now)
	if _, _, _, e = s.UpsertRun(ctx, run); e != nil {
		t.Fatal(e)
	}
	_, e = db.ExecContext(ctx, "INSERT INTO events(id,type,ts,actor_id,refs_json,payload_json) VALUES('ask-1','human_attention_requested',?,'actor-waiting','[]',?)", now.Format(time.RFC3339Nano), `{"payload":{"kind":"ask","requester_agent_id":"waiting","requester_actor_id":"actor-waiting"}}`)
	if e != nil {
		t.Fatal(e)
	}
	_, e = db.ExecContext(ctx, "INSERT INTO events(id,type,ts,actor_id,refs_json,payload_json) VALUES('idle-1','message_posted',?,'actor-idle','[]','{}')", now.Format(time.RFC3339Nano))
	if e != nil {
		t.Fatal(e)
	}
	roster, e := s.Roster(ctx, now.Add(time.Minute))
	if e != nil {
		t.Fatal(e)
	}
	want := map[string]string{"working": "working", "waiting": "waiting_on_human", "idle": "idle", "stale": "stale"}
	for _, a := range roster {
		if a.State != want[a.ID] {
			t.Errorf("%s state=%s want=%s", a.ID, a.State, want[a.ID])
		}
	}
	_, e = db.ExecContext(ctx, "INSERT INTO events(id,type,ts,actor_id,refs_json,payload_json) VALUES('response-1','human_attention_responded',?,'actor-idle','[]',?)", now.Add(2*time.Minute).Format(time.RFC3339Nano), `{"payload":{"request_event_id":"ask-1"}}`)
	if e != nil {
		t.Fatal(e)
	}
	roster, e = s.Roster(ctx, now.Add(3*time.Minute))
	if e != nil {
		t.Fatal(e)
	}
	for _, a := range roster {
		if a.ID == "waiting" && a.State != "idle" {
			t.Fatalf("answered ask state=%s", a.State)
		}
	}
	cleared, e := s.PatchPresence(ctx, "working", cc.PresencePatch{SetCard: true, SetNote: true}, now.Add(31*time.Minute))
	if e != nil || cleared.CurrentCardRef != nil || cleared.Note != nil {
		t.Fatalf("clear: %+v %v", cleared, e)
	}
	run.State = "completed"
	run.Liveness = "stale"
	run.LastObservedAt = now.Add(32 * time.Minute).Format(time.RFC3339Nano)
	if _, _, _, e = s.UpsertRun(ctx, run); e != nil {
		t.Fatal(e)
	}
	roster, e = s.Roster(ctx, now.Add(62*time.Minute))
	if e != nil {
		t.Fatal(e)
	}
	for _, a := range roster {
		if a.ID == "working" && a.State != "idle" {
			t.Fatalf("terminal run state=%s", a.State)
		}
	}
	roster, e = s.Roster(ctx, now.Add(26*time.Hour))
	if e != nil {
		t.Fatal(e)
	}
	for _, a := range roster {
		if a.ID == "working" && a.State != "stale" {
			t.Fatalf("expired signal state=%s", a.State)
		}
	}
}
