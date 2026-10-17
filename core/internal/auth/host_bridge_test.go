package auth

import (
	"context"
	"fmt"
	"testing"
	"time"

	"agent-nexus-core/internal/storage"
)

func bridgeFixture(t *testing.T) (*Store, string, []string) {
	t.Helper()
	ctx := context.Background()
	ws, e := storage.InitializeWorkspace(ctx, t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { ws.Close() })
	db := ws.DB()
	if _, e = db.Exec(`INSERT INTO hosts(id,slug,display_name,os_user,hostname,discovered_adapters_json,created_at) VALUES('host','test','Test','user','machine','[]','now')`); e != nil {
		t.Fatal(e)
	}
	ids := []string{"actor-a", "actor-b", "actor-c"}
	for i, id := range ids {
		if _, e = db.Exec(`INSERT INTO actors(id,display_name,tags_json,created_at,metadata_json) VALUES(?,?,'[]','now','{}')`, id, id); e != nil {
			t.Fatal(e)
		}
		if _, e = db.Exec(`INSERT INTO agents(id,username,actor_id,metadata_json,created_at,updated_at) VALUES(?,?,?,'{}','now','now')`, fmt.Sprint("agent", i), fmt.Sprint("agent", i), id); e != nil {
			t.Fatal(e)
		}
		if _, e = db.Exec(`INSERT INTO host_agents(host_id,name,agent_id,identity_kind) VALUES('host',?,?,'derived')`, fmt.Sprint("agent", i), fmt.Sprint("agent", i)); e != nil {
			t.Fatal(e)
		}
	}
	return NewStore(db), "host", ids
}
func TestHostBridgeSnapshotOrderingAndPerAgentWake(t *testing.T) {
	s, id, actors := bridgeFixture(t)
	ctx := context.Background()
	now := time.Now().UTC()
	input := BridgeCheckIn{InstanceID: "one", Sequence: 1, ConfigRevision: 1, Version: "test", Agents: []BridgeAgent{{actors[0], []BridgeJob{{"answers", true, true}}}, {actors[1], []BridgeJob{{"wake", true, true}}}}}
	put := func(at time.Time, in BridgeCheckIn) error {
		_, e := s.CheckInHostBridge(ctx, id, at.Format(time.RFC3339Nano), in)
		return e
	}
	if e := put(now, input); e != nil {
		t.Fatal(e)
	}
	host, e := s.GetHost(ctx, id)
	if e != nil || len(host.Agents) != 3 || host.Agents[0].BridgeOnline || !host.Agents[1].BridgeOnline || host.Agents[2].BridgeOnline {
		t.Fatal(host, e)
	}
	for _, agent := range []struct {
		id     string
		online bool
	}{{"agent0", false}, {"agent1", true}, {"agent2", false}} {
		p, e := s.PrincipalForActor(ctx, actors[map[string]int{"agent0": 0, "agent1": 1, "agent2": 2}[agent.id]])
		if e != nil || p.HostBridgeOnline != agent.online {
			t.Fatal(p, e)
		}
	}
	if e = put(now.Add(time.Second), input); e == nil {
		t.Fatal("same sequence accepted")
	}
	input.Sequence = 2
	if e = put(now.Add(-4*time.Minute), input); e != nil {
		t.Fatal("sequence advance rejected after clock step", e)
	}
	input.InstanceID = "two"
	input.Sequence = 1
	input.Agents = []BridgeAgent{}
	if e = put(now.Add(time.Second), input); e != nil {
		t.Fatal(e)
	}
	input.InstanceID = "one"
	input.Sequence = 3
	if e = put(now.Add(2*time.Second), input); e == nil {
		t.Fatal("retired instance reclaimed presence")
	}
	status, e := s.HostBridgeStatus(ctx, []string{id, "unknown"})
	if e != nil || len(status) != 1 || status[0].Bridge == nil || len(status[0].Agents) != 0 {
		t.Fatal(status, e)
	}
}
func TestHostBridgeRejectsForeignExcludedRevokedAndOversize(t *testing.T) {
	s, id, actors := bridgeFixture(t)
	ctx := context.Background()
	base := BridgeCheckIn{InstanceID: "one", Sequence: 1, Version: "test", Agents: []BridgeAgent{{actors[0], []BridgeJob{{"wake", true, true}}}}}
	put := func(in BridgeCheckIn) error {
		_, e := s.CheckInHostBridge(ctx, id, time.Now().UTC().Format(time.RFC3339Nano), in)
		return e
	}
	bad := base
	bad.Agents = []BridgeAgent{{"other-actor", []BridgeJob{{"answers", true, true}}}}
	if put(bad) == nil {
		t.Fatal("foreign actor accepted")
	}
	bad = base
	bad.Agents = append(bad.Agents, bad.Agents[0])
	if put(bad) == nil {
		t.Fatal("duplicate actor accepted")
	}
	bad = base
	bad.Version = "private path\nsecret"
	if put(bad) == nil {
		t.Fatal("arbitrary version text accepted")
	}
	bad = base
	bad.Agents = make([]BridgeAgent, 257)
	if put(bad) == nil {
		t.Fatal("oversize accepted")
	}
	if _, e := s.db.Exec(`INSERT INTO host_exclusions(host_id,name) VALUES('host','agent0')`); e != nil {
		t.Fatal(e)
	}
	if put(base) == nil {
		t.Fatal("excluded accepted")
	}
	if _, e := s.db.Exec(`DELETE FROM host_exclusions`); e != nil {
		t.Fatal(e)
	}
	if _, e := s.db.Exec(`UPDATE hosts SET revoked_at='now' WHERE id='host'`); e != nil {
		t.Fatal(e)
	}
	if put(base) == nil {
		t.Fatal("revoked accepted")
	}
}

func TestHostBridgeAcceptsSequenceGapsAfterLostResponses(t *testing.T) {
	s, id, _ := bridgeFixture(t)
	ctx := context.Background()
	now := time.Now().UTC()
	in := BridgeCheckIn{InstanceID: "first", Sequence: 1, Version: "test", Agents: []BridgeAgent{}}
	if _, e := s.CheckInHostBridge(ctx, id, now.Format(time.RFC3339Nano), in); e != nil {
		t.Fatal(e)
	}
	in.Sequence = 3
	if _, e := s.CheckInHostBridge(ctx, id, now.Add(time.Second).Format(time.RFC3339Nano), in); e != nil {
		t.Fatal("unobserved response wedged same instance", e)
	}
	in.InstanceID = "replacement"
	in.Sequence = 2
	if _, e := s.CheckInHostBridge(ctx, id, now.Add(2*time.Second).Format(time.RFC3339Nano), in); e != nil {
		t.Fatal("dropped first replacement packet wedged instance", e)
	}
	in.InstanceID = "first"
	in.Sequence = 100
	if _, e := s.CheckInHostBridge(ctx, id, now.Add(3*time.Second).Format(time.RFC3339Nano), in); e == nil {
		t.Fatal("retired daemon reclaimed liveness")
	}
}

func TestHostBridgeDelayedUnknownInstanceCannotRetireLiveInstance(t *testing.T) {
	s, id, _ := bridgeFixture(t)
	ctx := context.Background()
	now := time.Now().UTC()
	put := func(instance string, seq int64, at time.Time) error {
		_, e := s.CheckInHostBridge(ctx, id, at.Format(time.RFC3339Nano), BridgeCheckIn{InstanceID: instance, Sequence: seq, Version: "test", Agents: []BridgeAgent{}})
		return e
	}
	// A's first packet is in flight. B has already replaced its process and
	// arrives first; A has never been recorded in the retired-instance table.
	if e := put("B", 1, now); e != nil {
		t.Fatal(e)
	}
	if e := put("A", 1, now.Add(-time.Second)); e == nil {
		t.Fatal("late A retired live B")
	}
	if e := put("A", 2, now); e == nil {
		t.Fatal("equal timestamp replaced B")
	}
	if e := put("B", 2, now.Add(-4*time.Minute)); e != nil {
		t.Fatal("same instance clock correction rejected", e)
	}
	// A must also remain rejected after B's clock correction: keep the high-water.
	if e := put("A", 3, now.Add(-time.Second)); e == nil {
		t.Fatal("clock correction lowered replacement fence")
	}
	if e := put("B", 3, now.Add(-3*time.Minute)); e != nil {
		t.Fatal("B was retired by a rejected packet", e)
	}
	rows, e := s.HostBridgeStatus(ctx, []string{id})
	if e != nil || len(rows) != 1 || rows[0].Bridge.InstanceID != "B" || rows[0].Bridge.Sequence != 3 {
		t.Fatal(rows, e)
	}
	if e := put("C", 1, now.Add(time.Second)); e != nil {
		t.Fatal("newer replacement rejected", e)
	}
	if e := put("B", 4, now.Add(2*time.Second)); e == nil {
		t.Fatal("retired instance reclaimed presence")
	}
}
