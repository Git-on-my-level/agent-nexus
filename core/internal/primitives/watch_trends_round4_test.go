package primitives

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"
)

func TestWatchTrendsSeekKeysMatchCanonicalTimestamps(t *testing.T) {
	t.Parallel()
	f := newTrendsFixture(t)
	work := f.card("Clock", "", nil)
	f.ask(work, "agent-1", 20*24*time.Hour, 18*24*time.Hour)
	f.run(work, 10*24*time.Hour, "acted")
	var mismatches int
	if err := f.store.db.QueryRowContext(f.ctx, `SELECT count(*) FROM watch_trend_positions p JOIN events e ON e.id=p.id WHERE p.at_key<>anx_timestamp_key(e.ts)`).Scan(&mismatches); err != nil {
		t.Fatal(err)
	}
	if mismatches != 0 {
		t.Fatalf("%d stale event seek keys", mismatches)
	}
	// An ask outside the two-period interval must not consume the ask cap.
	f.rawEvents(work, "old", "human_attention_requested", trendsAskCap+2, 20*24*time.Hour, func(int) string {
		return fmt.Sprintf(`{"payload":{"kind":"ask","title":"Old","subject_ref":%q,"response_proposals":["Proceed"]}}`, work["ref"])
	})
	f.ask(work, "agent-1", time.Hour, 0)
	flow := f.group("decisions", 7)["asks_flow"]
	if flow["truncated"] != false || trendValue(t, flow, "raised") != 1 {
		t.Fatalf("old asks consumed recent cap: %v", flow)
	}
}

// A scoped reader is denied the aggregate before and after any hidden write:
// canonical promise/ask projections can incorporate invisible contributors,
// so only the full-visibility proof admits the calculation.
func TestWatchTrendsScopedAggregatesStayDenied(t *testing.T) {
	t.Parallel()
	f := newTrendsFixture(t)
	work := f.card("Visible", "", nil)
	private := f.card("Secret", "owner", nil)
	f.rawEvents(work, "visible-promise", "message_posted", 1, 3*24*time.Hour, func(int) string {
		return fmt.Sprintf(`{"payload":{"body":"Plan","next":{"at":%q,"what":"Ship"}}}`, loopTS(f.now.Add(-2*24*time.Hour)))
	})
	ctx := WithAccessScope(f.ctx, AccessScope{ActorID: "stranger"})
	read := func() {
		t.Helper()
		for _, group := range WatchTrendGroups {
			if _, err := f.store.WatchTrends(ctx, BriefReader{ActorID: "stranger"}, group, 30, f.now); err != ErrForbidden {
				t.Fatalf("%s must deny scoped aggregates: %v", group, err)
			}
		}
	}
	read()
	f.rawEvents(work, "hidden-promise", "message_posted", 1, time.Hour, func(int) string {
		return fmt.Sprintf(`{"payload":{"body":"Plan","blocks":[%q],"next":{"at":%q,"what":"Ship later"}}}`, private["ref"], loopTS(f.now.Add(24*time.Hour)))
	})
	read()
}

// Resource visibility, rather than an auth-admin role or cached scope, admits
// the full calculation. Callbacks still cannot establish workspace-wide access.
func TestWatchTrendsRequiresEmptyDenialSet(t *testing.T) {
	t.Parallel()
	f := newTrendsFixture(t)
	denied := &Store{}
	if _, err := denied.WatchTrends(f.ctx, BriefReader{Visible: func(string, string) bool { return true }}, "decisions", 30, f.now); err != ErrForbidden {
		t.Fatal("visibility callback bypassed restriction", err)
	}
	f.ask(f.card("Other owner's work", "another-owner", nil), "agent-1", time.Hour, 0)
	for _, scope := range []AccessScope{{}, {ActorID: "owner"}, {ActorID: "owner", AuthAdmin: true}} {
		for _, group := range WatchTrendGroups {
			if out, err := f.store.WatchTrends(WithRequestAccessScope(f.ctx, scope), BriefReader{}, group, 30, f.now); err != ErrForbidden || out != nil {
				t.Fatalf("scope %+v %s: out=%v err=%v", scope, group, out, err)
			}
		}
	}
	for _, ctx := range []context.Context{context.Background(), WithRequestAccessScope(f.ctx, AccessScope{ActorID: "another-owner"})} {
		out, err := f.store.WatchTrends(ctx, BriefReader{}, "decisions", 30, f.now)
		if err != nil {
			t.Fatal(err)
		}
		if got := trendValue(t, out["panels"].([]map[string]any)[0], "raised"); got != 1 {
			t.Fatalf("full read omitted owned work: %v", got)
		}
	}
}

func TestWatchTrendsConservativelyOmitsLinkedChildren(t *testing.T) {
	t.Parallel()
	f := newTrendsFixture(t)
	child := f.card("Child promise", "", nil)
	parent := f.card("Closed parent", "", nil)
	f.group("initiatives", 30)
	f.exec(`UPDATE cards SET column_key='done' WHERE id=?`, parent["id"])
	f.exec(`UPDATE brief_initiatives SET promise_at=? WHERE card_id=?`, loopTS(f.now.Add(-time.Hour)), child["id"])
	f.exec(`INSERT INTO card_plans(card_id,body_json,updated_at) VALUES(?,?,?)`, parent["id"], fmt.Sprintf(`{"steps":[{"id":"child","title":"Child","ref":%q}]}`, child["ref"]), loopTS(f.now))
	read := func(want float64) {
		t.Helper()
		out, err := f.store.WatchTrends(WithRequestAccessScope(f.ctx, AccessScope{ActorID: "owner"}), BriefReader{}, "initiatives", 30, f.now)
		if err != nil {
			t.Fatal(err)
		}
		for _, panel := range out["panels"].([]map[string]any) {
			if panel["id"] == "promises" && trendValue(t, panel, "due") != want {
				t.Fatalf("linked-child promise: %v want=%v", panel, want)
			}
		}
	}
	read(0)
	f.exec(`DELETE FROM card_plans WHERE card_id=?`, parent["id"])
	read(1)
}

func TestWatchTrendsBoundsHydrationBeforeEligibility(t *testing.T) {
	if testing.Short() {
		t.Skip("scale-corpus budget guard; the non-short CI tier keeps the full fixture")
	}
	t.Parallel()
	f := newTrendsFixture(t)
	visible := f.card("Answerable", "", nil)
	f.ask(visible, "agent-1", 3*24*time.Hour, 0)
	orphan := f.card("Retained ask history", "", nil)
	f.exec(`WITH RECURSIVE n(i) AS (SELECT 0 UNION ALL SELECT i+1 FROM n WHERE i+1<2000)
	 INSERT INTO events(id,type,ts,actor_id,thread_id,refs_json,payload_json)
	 SELECT 'orphan-ask-'||i,'human_attention_requested',?,'agent-1',?,'[]',? FROM n`, loopTS(f.now.Add(-time.Hour)), orphan["thread_id"],
		fmt.Sprintf(`{"payload":{"kind":"ask","title":"Retained","requester_actor_id":"agent-1","subject_ref":%q,"response_proposals":["Proceed"]}}`, orphan["ref"]))
	f.exec(`DELETE FROM threads WHERE id=?`, orphan["thread_id"])
	b := &trendsBuild{briefBuild: &briefBuild{s: f.store, ctx: f.ctx, now: f.now}, w: newTrendWindow(30, f.now)}
	asks, horizon, err := b.trendAsks()
	if err != nil || len(asks) != 0 || !horizon.Equal(f.now) {
		t.Fatalf("hydration walked past the capped unanswerable page: asks=%d horizon=%v err=%v", len(asks), horizon, err)
	}
	out, err := f.store.WatchTrends(WithRequestAccessScope(f.ctx, AccessScope{ActorID: "owner"}), BriefReader{}, "decisions", 30, f.now)
	if err != nil {
		t.Fatal(err)
	}
	if panel := out["panels"].([]map[string]any)[0]; panel["truncated"] != true || panel["raised"] != (*float64)(nil) {
		t.Fatalf("incomplete history presented as complete: %v", panel)
	}
}

func TestWatchTrendsEmptyLifecycleValuesRemainReadable(t *testing.T) {
	t.Parallel()
	f := newTrendsFixture(t)
	work := f.card("Public", "", map[string]any{"cadence": map[string]any{"every_seconds": 3600}})
	f.ask(work, "agent-1", time.Hour, 0)
	f.run(work, time.Hour, "acted")
	f.group("initiatives", 30)
	read := func() string {
		t.Helper()
		all := map[string]any{}
		for _, group := range WatchTrendGroups {
			out, err := f.store.WatchTrends(WithRequestAccessScope(f.ctx, AccessScope{ActorID: "owner"}), BriefReader{}, group, 30, f.now)
			if err != nil {
				t.Fatal(err)
			}
			all[group] = out
		}
		return marshalTrends(t, all)
	}
	before := read()
	f.exec(`UPDATE events SET trashed_at=''`)
	f.exec(`UPDATE cards SET archived_at='',trashed_at=''`)
	if after := read(); after != before {
		t.Fatalf("empty lifecycle values changed trends:\nbefore %s\nafter %s", before, after)
	}
}

func marshalTrends(t *testing.T, all map[string]any) string {
	t.Helper()
	data, err := json.Marshal(all)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
