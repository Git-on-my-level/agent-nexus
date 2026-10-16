package primitives

import (
	"context"
	"database/sql"
	"testing"
	"time"
)

func TestLoopProjectionPrivacyEpochAndDirtyInvalidation(t *testing.T) {
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	s := NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	card, err := s.CreateWork(ctx, "owner", "", map[string]any{"title": "Public loop", "cadence": map[string]any{"every_seconds": 3600}, "goal": map[string]any{"metric": "score", "direction": "up"}})
	if err != nil {
		t.Fatal(err)
	}
	id := workString(card["id"])
	private, err := s.CreateWork(ctx, "owner", "", map[string]any{"title": "Private proof"})
	if err != nil {
		t.Fatal(err)
	}
	setOwner := func(owner string) {
		t.Helper()
		if _, err = s.PatchThread(ctx, "owner", workString(private["thread_id"]), map[string]any{"pm_actor_id": owner}, nil); err != nil {
			t.Fatal(err)
		}
	}
	setOwner("owner")
	receipt := func(run string, score float64, refs []string) {
		t.Helper()
		payload := map[string]any{"run_id": run, "outcome": "measured", "metrics": map[string]any{"score": score}}
		if refs != nil {
			payload["effect_refs"] = refs
		}
		if _, err = s.AppendEvent(ctx, "owner", map[string]any{"type": "run_recorded", "summary": "Measured", "refs": []string{workString(card["ref"])}, "payload": payload}); err != nil {
			t.Fatal(err)
		}
	}
	receipt("public", 1, nil)
	receipt("private", 99, []string{workString(private["ref"])})
	drain := func() {
		t.Helper()
		for done := false; !done; {
			done, err = s.RefreshBriefInitiatives(ctx, 200)
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	drain()
	owner := WithRequestAccessScope(ctx, AccessScope{ActorID: "owner"})
	stranger := WithRequestAccessScope(ctx, AccessScope{ActorID: "stranger"})
	cache, err := s.cachedLoopFacts(owner, []string{id}, time.Now().UTC())
	if err != nil || len(cache) != 1 {
		t.Fatal(cache, err)
	}
	cache, err = s.cachedLoopFacts(stranger, []string{id}, time.Now().UTC())
	if err != nil || len(cache) != 0 {
		t.Fatal("private receipt projection escaped", cache, err)
	}
	for _, column := range []string{"loop_facts_json", "loop_valid_from", "loop_valid_until"} {
		var raw sql.NullString
		if err = s.db.QueryRowContext(stranger, `SELECT `+column+` FROM brief_initiatives WHERE card_id=?`, id).Scan(&raw); err != nil || raw.Valid {
			t.Fatal("backing column escaped masking", column, raw, err)
		}
	}
	if _, err = s.GetWork(stranger, id); err != nil {
		t.Fatal("projection privacy restricted canonical card", err)
	}
	state, _, err := s.loadLoopStates(stranger, []string{id}, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if workMap(state[id]["goal"])["latest"] != 1. {
		t.Fatal(state)
	}
	setOwner("")
	cache, err = s.cachedLoopFacts(stranger, []string{id}, time.Now().UTC())
	if err != nil || len(cache) != 1 {
		t.Fatal("epoch did not release projection", cache, err)
	}
	setOwner("owner")
	cache, err = s.cachedLoopFacts(stranger, []string{id}, time.Now().UTC())
	if err != nil || len(cache) != 0 {
		t.Fatal("epoch retained stale projection grant", cache, err)
	}
	receipt("new", 2, nil)
	cache, err = s.cachedLoopFacts(owner, []string{id}, time.Now().UTC())
	if err != nil || len(cache) != 0 {
		t.Fatal("dirty projection reused", cache, err)
	}
	drain()
	cache, err = s.cachedLoopFacts(owner, []string{id}, time.Now().UTC())
	if err != nil || len(cache) != 1 || len(cache[id].Runs) != 3 {
		t.Fatal(cache, err)
	}
}

func TestLoopProjectionExpiresAtWindowBoundary(t *testing.T) {
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	s := NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	card, err := s.CreateWork(ctx, "owner", "", map[string]any{"title": "Expiring count", "cadence": map[string]any{"every_seconds": 86400}})
	if err != nil {
		t.Fatal(err)
	}
	id := workString(card["id"])
	event, err := s.AppendEvent(ctx, "owner", map[string]any{"type": "run_recorded", "summary": "Measured", "refs": []string{workString(card["ref"])}, "payload": map[string]any{"run_id": "one", "outcome": "measured"}})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if _, err = ws.DB().Exec(`UPDATE events SET ts=? WHERE id=?`, loopTS(now.Add(-7*24*time.Hour+time.Hour)), event["id"]); err != nil {
		t.Fatal(err)
	}
	for done := false; !done; {
		done, err = s.RefreshBriefInitiatives(ctx, 200)
		if err != nil {
			t.Fatal(err)
		}
	}
	now = time.Now().UTC()
	cache, err := s.cachedLoopFacts(ctx, []string{id}, now)
	if err != nil || len(cache) != 1 {
		t.Fatal(cache, err)
	}
	cache, err = s.cachedLoopFacts(ctx, []string{id}, now.Add(2*time.Hour))
	if err != nil || len(cache) != 0 {
		t.Fatal("expired count cached", cache, err)
	}
	state, _, err := s.loadLoopStates(ctx, []string{id}, now.Add(2*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if workMap(state[id]["runs_7d"])["measured"] != 0 {
		t.Fatal(state)
	}
}

func TestLoopProjectionFutureReceiptActivation(t *testing.T) {
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	s := NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	card, err := s.CreateWork(ctx, "owner", "", map[string]any{"title": "Future receipt", "cadence": map[string]any{"every_seconds": 3600}})
	if err != nil {
		t.Fatal(err)
	}
	id := workString(card["id"])
	event, err := s.AppendEvent(ctx, "owner", map[string]any{"type": "run_recorded", "summary": "Measured", "refs": []string{workString(card["ref"])}, "payload": map[string]any{"run_id": "future", "outcome": "measured"}})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if _, err = ws.DB().Exec(`UPDATE events SET ts=? WHERE id=?`, loopTS(now.Add(time.Hour)), event["id"]); err != nil {
		t.Fatal(err)
	}
	for done := false; !done; {
		done, err = s.RefreshBriefInitiatives(ctx, 200)
		if err != nil {
			t.Fatal(err)
		}
	}
	cache, err := s.cachedLoopFacts(ctx, []string{id}, time.Now().UTC())
	if err != nil || len(cache) != 1 || len(cache[id].Runs) != 0 {
		t.Fatal(cache, err)
	}
	cache, err = s.cachedLoopFacts(ctx, []string{id}, now.Add(2*time.Hour))
	if err != nil || len(cache) != 0 {
		t.Fatal("future receipt did not invalidate facts", cache, err)
	}
	state, _, err := s.loadLoopStates(ctx, []string{id}, now.Add(2*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if workMap(state[id]["runs_7d"])["measured"] != 1. {
		t.Fatal(state)
	}
}
