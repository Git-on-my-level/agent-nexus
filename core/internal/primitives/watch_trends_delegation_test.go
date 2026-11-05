package primitives

import (
	"strings"
	"testing"
	"time"
)

// The delegated admission keeps index seeks: no step scans a workspace table
// (root owners, sponsors, host bindings or principals); only the reader's
// small materialized allowed set and its gaps are walked.
func TestWatchTrendsDelegatedAdmissionPlanIsBounded(t *testing.T) {
	t.Parallel()
	f := newTrendsFixture(t)
	f.seedPrincipal("principal-owner", "owner", "human")
	f.seedHost("host-owner", "owner", "pm-agent", "reviewer")
	q, args := trendsAdmissionSQL(AccessScope{ActorID: "owner"})
	rows, err := f.store.db.QueryContext(f.ctx, "EXPLAIN QUERY PLAN "+q, args...)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var plan []string
	for rows.Next() {
		var id, parent, unused int
		var detail string
		if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
			t.Fatal(err)
		}
		plan = append(plan, detail)
	}
	walked := map[string]bool{"_anx_trends_allowed": true, "_anx_trends_gaps": true, "_anx_trends_reader": true, "g": true, "CONSTANT": true, "main.sqlite_schema": true}
	for _, step := range plan {
		if fields := strings.Fields(step); len(fields) > 1 && fields[0] == "SCAN" && !walked[fields[1]] && !strings.HasPrefix(fields[1], "(") {
			t.Fatalf("admission scans %q\n%s", step, strings.Join(plan, "\n"))
		}
	}

}

// delegationFixture seeds the auth rows that make "owner" a human with a
// sponsored host whose derived agents act for them, plus a second human
// ("other") with a host of their own.
func (f *trendsFixture) seedPrincipal(agentID, actorID, kind string) {
	f.t.Helper()
	now := loopTS(f.now)
	f.exec(`INSERT INTO actors(id,display_name,tags_json,created_at,metadata_json) VALUES(?,?,'[]',?,'{}') ON CONFLICT(id) DO NOTHING`, actorID, actorID, now)
	f.exec(`INSERT INTO agents(id,username,actor_id,created_at,updated_at,metadata_json) VALUES(?,?,?,?,?,json_object('principal_kind',?))`, agentID, agentID, actorID, now, now, kind)
}

func (f *trendsFixture) seedHost(hostID, sponsorActor string, agents ...string) {
	f.t.Helper()
	now := loopTS(f.now)
	f.exec(`INSERT INTO hosts(id,slug,display_name,os_user,hostname,discovered_adapters_json,created_at) VALUES(?,?,?,'u','h','[]',?)`, hostID, hostID, hostID, now)
	if sponsorActor != "" {
		f.exec(`INSERT INTO host_sponsors(host_id,sponsor_actor_id,source,recorded_at) VALUES(?,?,'device_approval',?)`, hostID, sponsorActor, now)
	}
	for _, actor := range agents {
		f.seedPrincipal(actor, actor, "agent") // derived agents own their actor
		f.exec(`INSERT INTO host_agents(host_id,name,agent_id,identity_kind) VALUES(?,?,?,'derived')`, hostID, actor, actor)
	}
}

func (f *trendsFixture) trendsErr(actor string) error {
	f.t.Helper()
	for _, group := range WatchTrendGroups {
		if _, err := f.store.WatchTrends(WithRequestAccessScope(f.ctx, AccessScope{ActorID: actor}), BriefReader{}, group, 30, f.now); err != nil {
			return err
		}
	}
	return nil
}

// Reproduces the single-human lockout: an agent acting for the only human owns
// a private thread, and before delegation the human is refused Trends.
func TestWatchTrendsSingleHumanLockedOutByUndelegatedAgent(t *testing.T) {
	t.Parallel()
	f := newTrendsFixture(t)
	f.seedPrincipal("principal-owner", "owner", "human")
	f.seedHost("host-unsponsored", "", "pm-agent")
	f.ask(f.card("Agent's private conversation", "pm-agent", nil), "pm-agent", time.Hour, 0)
	if err := f.trendsErr("owner"); err != ErrForbidden {
		t.Fatalf("undelegated agent's private thread must still block the human: %v", err)
	}
}

func TestWatchTrendsInheritsDelegatedAgentOwnership(t *testing.T) {
	t.Parallel()
	f := newTrendsFixture(t)
	f.seedPrincipal("principal-owner", "owner", "human")
	f.seedPrincipal("principal-other", "other", "human")
	f.seedHost("host-owner", "owner", "pm-agent", "reviewer")
	f.seedHost("host-other", "other", "other-agent")
	private := f.card("Agent's private conversation", "pm-agent", nil)
	f.ask(private, "pm-agent", time.Hour, 0)
	f.card("Persona's private notes", "reviewer", nil)

	// The human now counts as owner of their own agents' private roots.
	if err := f.trendsErr("owner"); err != nil {
		t.Fatalf("owner with own-agent private threads: %v", err)
	}
	out, err := f.store.WatchTrends(WithRequestAccessScope(f.ctx, AccessScope{ActorID: "owner"}), BriefReader{}, "decisions", 30, f.now)
	if err != nil || trendValue(t, out["panels"].([]map[string]any)[0], "raised") != 1 {
		t.Fatalf("delegated private ask omitted: %v %v", out, err)
	}

	// Inheritance never runs agent -> human, agent -> sibling, or to strangers.
	for _, reader := range []string{"pm-agent", "reviewer", "other", "other-agent", "stranger", ""} {
		if err := f.trendsErr(reader); err != ErrForbidden {
			t.Fatalf("%q must not inherit owner's delegation: %v", reader, err)
		}
	}

	// Ordinary reads keep the exact owner: the human gains no item-level access
	// and the agent gains none of the human's private items.
	if f.store.CanAccessResource(WithAccessScope(f.ctx, AccessScope{ActorID: "owner"}), "card", anyStringValue(private["id"])) {
		t.Fatal("trends inheritance widened the human's ordinary reads")
	}
	humanPrivate := f.card("Human's private card", "owner", nil)
	if f.store.CanAccessResource(WithAccessScope(f.ctx, AccessScope{ActorID: "pm-agent"}), "card", anyStringValue(humanPrivate["id"])) {
		t.Fatal("agent gained the human's private card")
	}
	if err := f.trendsErr("owner"); err != nil {
		t.Fatalf("own private card plus delegated agents: %v", err)
	}

	// Another human's private thread (or one of their agents') still blocks.
	for _, foreign := range []string{"other", "other-agent"} {
		blocked := f.card("Foreign private", foreign, nil)
		if err := f.trendsErr("owner"); err != ErrForbidden {
			t.Fatalf("%s's private thread must block the owner: %v", foreign, err)
		}
		f.exec(`UPDATE threads SET body_json=json_remove(body_json,'$.pm_actor_id') WHERE id=?`, blocked["thread_id"])
		if err := f.trendsErr("owner"); err != nil {
			t.Fatalf("removing %s's privacy should restore owner: %v", foreign, err)
		}
	}

	// Tombstones follow the same owner rule; unknown artifacts still block all.
	f.exec(`INSERT INTO resource_access_tombstones(kind,id,ref,owner) VALUES('card','gone','card:gone','pm-agent')`)
	if err := f.trendsErr("owner"); err != nil {
		t.Fatalf("delegated tombstone: %v", err)
	}
	f.exec(`INSERT INTO resource_access_tombstones(kind,id,ref,owner) VALUES('card','gone-other','card:gone-other','other-agent')`)
	if err := f.trendsErr("owner"); err != ErrForbidden {
		t.Fatalf("foreign tombstone must block: %v", err)
	}
	f.exec(`DELETE FROM resource_access_tombstones WHERE id='gone-other'`)

	// A sponsor that is not an active human confers nothing.
	f.exec(`UPDATE agents SET revoked_at=? WHERE id='principal-owner'`, loopTS(f.now))
	if err := f.trendsErr("owner"); err != ErrForbidden {
		t.Fatalf("revoked sponsor kept delegation: %v", err)
	}
	f.exec(`UPDATE agents SET revoked_at=NULL,metadata_json=json_object('principal_kind','agent') WHERE id='principal-owner'`)
	if err := f.trendsErr("owner"); err != ErrForbidden {
		t.Fatalf("non-human sponsor kept delegation: %v", err)
	}
}

// A legacy numeric owner never matches a delegate's text actor ID.
func TestWatchTrendsDelegationKeepsNumericOwnersDenied(t *testing.T) {
	t.Parallel()
	f := newTrendsFixture(t)
	f.seedPrincipal("principal-owner", "owner", "human")
	f.seedHost("host-owner", "owner", "42")
	f.exec(`INSERT INTO threads(id,updated_at,updated_by,body_json) VALUES('numeric-root',?,'x','{"pm_actor_id":42}')`, loopTS(f.now))
	if err := f.trendsErr("owner"); err != ErrForbidden {
		t.Fatalf("numeric JSON owner became a text grant: %v", err)
	}
	f.exec(`UPDATE threads SET body_json='{"pm_actor_id":"42"}' WHERE id='numeric-root'`)
	if err := f.trendsErr("owner"); err != nil {
		t.Fatalf("text owner of delegate: %v", err)
	}
}

// Revoked agents and agents on revoked hosts stay delegates; an agent bound to
// a pre-existing (dev-mode linked) actor never does; over-cap delegation fails
// closed.
func TestWatchTrendsDelegateEdgeCases(t *testing.T) {
	t.Parallel()
	f := newTrendsFixture(t)
	f.seedPrincipal("principal-owner", "owner", "human")
	f.seedHost("host-owner", "owner", "retired-persona")
	f.seedHost("host-archived", "owner", "old-agent")
	f.card("Retired persona's notes", "retired-persona", nil)
	f.card("Old agent's notes", "old-agent", nil)
	f.exec(`UPDATE agents SET revoked_at=? WHERE id='retired-persona'`, loopTS(f.now))
	f.exec(`UPDATE hosts SET revoked_at=? WHERE id='host-archived'`, loopTS(f.now))
	if err := f.trendsErr("owner"); err != nil {
		t.Fatalf("revoked delegates must still count: %v", err)
	}

	// Dev-mode linked actor: the principal's actor predates it (actor != id).
	f.seedPrincipal("linked-principal", "preexisting-actor", "agent")
	f.exec(`INSERT INTO host_agents(host_id,name,agent_id,identity_kind) VALUES('host-owner','linked','linked-principal','derived')`)
	linked := f.card("Pre-existing actor's private", "preexisting-actor", nil)
	if err := f.trendsErr("owner"); err != ErrForbidden {
		t.Fatalf("linked pre-existing actor counted as delegate: %v", err)
	}
	f.exec(`UPDATE threads SET body_json=json_remove(body_json,'$.pm_actor_id') WHERE id=?`, linked["thread_id"])
	if err := f.trendsErr("owner"); err != nil {
		t.Fatalf("baseline before cap check: %v", err)
	}

	// More delegates than the cap: some are dropped, so their roots deny.
	f.exec(`INSERT INTO hosts(id,slug,display_name,os_user,hostname,discovered_adapters_json,created_at) VALUES('host-fleet','fleet','fleet','u','h','[]',?)`, loopTS(f.now))
	f.exec(`INSERT INTO host_sponsors(host_id,sponsor_actor_id,source,recorded_at) VALUES('host-fleet','owner','enrollment_token',?)`, loopTS(f.now))
	f.exec(`WITH RECURSIVE n(i) AS (SELECT 0 UNION ALL SELECT i+1 FROM n WHERE i<299)
	 INSERT INTO agents(id,username,actor_id,created_at,updated_at,metadata_json) SELECT 'fleet-'||i,'fleet-'||i,'fleet-'||i,?,?,'{"principal_kind":"agent"}' FROM n`, loopTS(f.now), loopTS(f.now))
	f.exec(`INSERT INTO host_agents(host_id,name,agent_id,identity_kind) SELECT 'host-fleet',id,id,'derived' FROM agents WHERE id LIKE 'fleet-%'`)
	f.exec(`WITH RECURSIVE n(i) AS (SELECT 0 UNION ALL SELECT i+1 FROM n WHERE i<299)
	 INSERT INTO resource_access_tombstones(kind,id,ref,owner) SELECT 'card','fleet-gone-'||i,'card:fleet-gone-'||i,'fleet-'||i FROM n`)
	if err := f.trendsErr("owner"); err != ErrForbidden {
		t.Fatalf("over-cap delegation must fail closed: %v", err)
	}
}
