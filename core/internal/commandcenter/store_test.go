package commandcenter_test

import (
	"context"
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
	now := time.Now().UTC().Add(-11 * time.Minute)
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

func TestRunFutureObservationAndTerminalClockSkew(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	now := time.Now().UTC()
	future := observation("agent-1", "future-run", now.Add(cc.RunObservationFutureSkew+time.Minute))
	if _, _, _, err := s.UpsertRun(ctx, future); !errors.Is(err, cc.ErrInvalid) {
		t.Fatalf("future observation accepted: %v", err)
	}
	var count int
	if err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM runs`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("future observation persisted: count=%d err=%v", count, err)
	}
	running := observation("agent-1", "skewed-run", now.Add(time.Minute))
	if _, _, _, err := s.UpsertRun(ctx, running); err != nil {
		t.Fatal(err)
	}
	completed := running
	completed.State = "completed"
	completed.Liveness = "stale"
	completed.LastObservedAt = now.Format(time.RFC3339Nano)
	closed, _, replayed, err := s.UpsertRun(ctx, completed)
	if err != nil || replayed || closed.State != "completed" || closed.Liveness != "stale" || closed.EndedAt == nil {
		t.Fatalf("terminal update lost to modest skew: %#v replay=%v err=%v", closed, replayed, err)
	}
	if closed.LastObservedAt != running.LastObservedAt {
		t.Fatalf("run observation regressed: %#v", closed)
	}
}
func TestPresenceAndDerivedStates(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	now := time.Now().UTC().Add(-63 * time.Minute)
	db := s.DB
	agents := []struct{ id, actor string }{{"working", "actor-working"}, {"waiting", "actor-waiting"}, {"idle", "actor-idle"}, {"stale", "actor-stale"}}
	if _, e := db.ExecContext(ctx, `INSERT INTO hosts(id,slug,display_name,os_user,hostname,discovered_adapters_json,created_at) VALUES('host-1','host','Host','test','host','["generic"]',?)`, now.Format(time.RFC3339Nano)); e != nil {
		t.Fatal(e)
	}
	for _, a := range agents {
		_, e := db.ExecContext(ctx, "INSERT INTO actors(id,display_name,tags_json,created_at,metadata_json) VALUES(?,?,'[]',?,'{}')", a.actor, a.id, now.Format(time.RFC3339Nano))
		if e != nil {
			t.Fatal(e)
		}
		_, e = db.ExecContext(ctx, "INSERT INTO agents(id,username,actor_id,created_at,updated_at,metadata_json) VALUES(?,?,?,?,?,'{\"principal_kind\":\"agent\"}')", a.id, a.id+".host", a.actor, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
		if e != nil {
			t.Fatal(e)
		}
		_, e = db.ExecContext(ctx, `INSERT INTO host_agents(host_id,name,agent_id,identity_kind) VALUES('host-1',?,?,'derived')`, a.id, a.id)
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
	_, e = db.ExecContext(ctx, "INSERT INTO events(id,type,ts,actor_id,refs_json,payload_json) VALUES('ask-1','human_attention_requested',?,'actor-waiting','[]',?)", now.Format(time.RFC3339Nano), `{"payload":{"kind":"ask","title":"Approve launch","severity":"high","requester_agent_id":"waiting","requester_actor_id":"actor-waiting"}}`)
	if e != nil {
		t.Fatal(e)
	}
	_, e = db.ExecContext(ctx, `INSERT INTO derived_inbox_items(id,thread_id,category,trigger_at,source_event_id,generated_at,data_json) VALUES('inbox-ask-1','thread-1','ask',?,'ask-1',?,'{}')`, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
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
		if a.ID == "waiting" {
			if a.WaitingAsk == nil || a.WaitingAsk.ID != "ask-1" || a.WaitingAsk.InboxItemID == nil || *a.WaitingAsk.InboxItemID != "inbox-ask-1" || a.WaitingAsk.Title != "Approve launch" || a.WaitingAsk.Severity == nil || *a.WaitingAsk.Severity != "high" || a.WaitingAsk.CreatedAt == "" {
				t.Fatalf("waiting ask summary: %#v", a.WaitingAsk)
			}
		}
	}
	staleRunRoster, e := s.Roster(ctx, now.Add(10*time.Minute))
	if e != nil {
		t.Fatal(e)
	}
	for _, a := range staleRunRoster {
		if a.ID == "working" && a.ActiveRun != nil {
			t.Fatalf("stale run remains active: %#v", a.ActiveRun)
		}
	}
	detail, e := s.AgentDetail(ctx, "waiting", now.Add(time.Minute))
	if e != nil || len(detail.OpenAsks) != 1 || detail.OpenAsks[0].InboxItemID == nil || *detail.OpenAsks[0].InboxItemID != "inbox-ask-1" {
		t.Fatalf("open asks detail: %#v %v", detail.OpenAsks, e)
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

func TestHostIdentitySourceAndBridgePresence(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	now := time.Now().UTC()
	at := now.Format(time.RFC3339Nano)
	expires := now.Add(2 * time.Minute).Format(time.RFC3339Nano)
	for _, query := range []string{
		`INSERT INTO hosts(id,slug,display_name,os_user,hostname,discovered_adapters_json,created_at,bridge_checked_in_at,bridge_expires_at) VALUES('host-1','m5-mbp','Mac','david','m5','["codex"]','` + at + `','` + at + `','` + expires + `')`,
		`INSERT INTO actors(id,display_name,tags_json,created_at,metadata_json) VALUES('actor-1','Misleading','["agent"]','` + at + `','{}')`,
		`INSERT INTO agents(id,username,actor_id,created_at,updated_at,metadata_json) VALUES('agent-1','reviewer.m5-mbp','actor-1','` + at + `','` + at + `','{"principal_kind":"agent","identity_kind":"derived","host_id":"wrong-host","name":"wrong"}')`,
		`INSERT INTO host_agents(host_id,name,agent_id,identity_kind) VALUES('host-1','reviewer','agent-1','adopted')`,
	} {
		if _, e := s.DB.ExecContext(ctx, query); e != nil {
			t.Fatal(e)
		}
	}
	identity, e := s.Identity(ctx, "agent-1")
	if e != nil || identity.HostID != "host-1" || identity.HostSlug != "m5-mbp" || identity.Name != "reviewer" || identity.Kind != "adopted" || identity.DisplayName != "reviewer on m5-mbp" || identity.Adapter != "generic" {
		t.Fatalf("canonical host identity: %+v %v", identity, e)
	}
	roster, e := s.Roster(ctx, now)
	if e != nil || len(roster) != 1 || !roster[0].BridgeOnline {
		t.Fatalf("host bridge check-in should make child online: %+v %v", roster, e)
	}
	if _, e = s.DB.ExecContext(ctx, `INSERT INTO host_exclusions(host_id,name) VALUES('host-1','reviewer')`); e != nil {
		t.Fatal(e)
	}
	roster, e = s.Roster(ctx, now)
	if e != nil || roster[0].BridgeOnline {
		t.Fatalf("excluded child should be offline: %+v %v", roster, e)
	}
	if _, e = s.DB.ExecContext(ctx, `DELETE FROM host_exclusions WHERE host_id='host-1'`); e != nil {
		t.Fatal(e)
	}
	if _, e = s.DB.ExecContext(ctx, `UPDATE hosts SET revoked_at=? WHERE id='host-1'`, at); e != nil {
		t.Fatal(e)
	}
	roster, e = s.Roster(ctx, now)
	if e != nil || roster[0].BridgeOnline {
		t.Fatalf("revoked host should be offline: %+v %v", roster, e)
	}
}

func TestProvisionalPersonaAdapterResolvedByLauncher(t *testing.T) {
	s := fixture(t)
	ctx := context.Background()
	provisional, e := s.Provisional(ctx, cc.Identity{AgentID: "agent-1", HostID: "host-1", Adapter: "generic"}, "exec-persona")
	if e != nil || provisional.Adapter != "generic" || provisional.State != "unknown" {
		t.Fatalf("provisional persona adapter: %+v %v", provisional, e)
	}
	observed := observation("agent-1", "exec-persona", time.Now().UTC().Add(time.Second))
	observed.Adapter = "codex"
	resolved, _, _, e := s.UpsertRun(ctx, observed)
	if e != nil || resolved.Adapter != "codex" || resolved.ID != provisional.ID {
		t.Fatalf("launcher adapter observation: %+v %v", resolved, e)
	}
	stored, e := s.GetRun(ctx, resolved.ID)
	if e != nil || stored.Adapter != "codex" {
		t.Fatalf("stored launcher adapter: %+v %v", stored, e)
	}
}
