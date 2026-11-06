package primitives

import (
	"testing"
	"time"
)

func (f *trendsFixture) seedPrincipal(agentID, actorID, kind string) {
	f.t.Helper()
	now := loopTS(f.now)
	f.exec(`INSERT INTO actors(id,display_name,tags_json,created_at,metadata_json) VALUES(?,?,'[]',?,'{}') ON CONFLICT(id) DO NOTHING`, actorID, actorID, now)
	f.exec(`INSERT INTO agents(id,username,actor_id,created_at,updated_at,metadata_json) VALUES(?,?,?,?,?,json_object('principal_kind',?))`, agentID, agentID, actorID, now, now, kind)
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

// People are never locked out of Trends: every active human is admitted
// whoever keeps an item private (another human, an agent, or a legacy artifact
// with an unknown content manifest). Agents still need full visibility, a
// revoked human is not admitted, and item-level reads keep their rules.
func TestWatchTrendsAdmitsEveryActiveHuman(t *testing.T) {
	t.Parallel()
	f := newTrendsFixture(t)
	f.seedPrincipal("principal-owner", "owner", "human")
	f.seedPrincipal("principal-operator", "operator", "human")
	f.seedPrincipal("principal-agent", "pm-agent", "agent")
	private := f.card("Operator's private board", "operator", nil)
	f.ask(private, "pm-agent", time.Hour, 0)
	f.card("Agent's private board", "pm-agent", nil)
	f.exec(`INSERT INTO artifacts(id,kind,created_at,created_by,content_type,content_hash,content_refs_json) VALUES('unknown','text',?,'owner','text/plain','unknown',NULL)`, loopTS(f.now))

	for _, human := range []string{"owner", "operator"} {
		if err := f.trendsErr(human); err != nil {
			t.Fatalf("human %q must be admitted: %v", human, err)
		}
	}
	out, err := f.store.WatchTrends(WithRequestAccessScope(f.ctx, AccessScope{ActorID: "owner"}), BriefReader{}, "decisions", 30, f.now)
	if err != nil || trendValue(t, out["panels"].([]map[string]any)[0], "raised") != 1 {
		t.Fatalf("private ask omitted from aggregates: %v %v", out, err)
	}
	for _, reader := range []string{"pm-agent", "stranger", ""} {
		if err := f.trendsErr(reader); err != ErrForbidden {
			t.Fatalf("%q must not be admitted: %v", reader, err)
		}
	}
	if f.store.CanAccessResource(WithAccessScope(f.ctx, AccessScope{ActorID: "owner"}), "card", anyStringValue(private["id"])) {
		t.Fatal("Trends admission widened item-level reads")
	}
	f.exec(`UPDATE agents SET revoked_at=? WHERE id='principal-owner'`, loopTS(f.now))
	if err := f.trendsErr("owner"); err != ErrForbidden {
		t.Fatalf("revoked human must not be admitted: %v", err)
	}
}
