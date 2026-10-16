package primitives

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestLoopReportAdmitsLoopsBeforeLimit(t *testing.T) {
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	s := NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	// Direct fixture IDs make the ordering regression deterministic; all writes
	// still exercise the production admission triggers.
	if _, err = ws.DB().Exec(`INSERT INTO threads(id,updated_at,updated_by,body_json) VALUES('t','now','owner','{}')`); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 60; i++ {
		if _, err = ws.DB().Exec(`INSERT INTO cards(id,title,summary,column_key,thread_id,created_at,created_by,updated_at,updated_by) VALUES(?,?,'','todo','t','now','owner','now','owner')`, fmt.Sprintf("ordinary-%02d", i), "Ordinary"); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []string{"zz-cadence", "zz-goal", "zz-receipt", "zz-old", "zz-future", "zz-hidden"} {
		if _, err = ws.DB().Exec(`INSERT INTO cards(id,title,summary,column_key,thread_id,created_at,created_by,updated_at,updated_by) VALUES(?,?,'','todo','t','now','owner','now','owner')`, id, id); err != nil {
			t.Fatal(err)
		}
	}
	for _, m := range []struct{ id, raw string }{{"zz-cadence", `{"cadence":{"every_seconds":3600}}`}, {"zz-goal", `{"goal":{"metric":"score","direction":"up"}}`}} {
		if _, err = ws.DB().Exec(`INSERT INTO work_metadata(card_id,metadata_json,version,updated_at,updated_by) VALUES(?,?,1,'now','owner')`, m.id, m.raw); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now().UTC()
	private, err := s.CreateWork(ctx, "owner", "", map[string]any{"title": "Private evidence"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.PatchThread(ctx, "owner", workString(private["thread_id"]), map[string]any{"pm_actor_id": "owner"}, nil); err != nil {
		t.Fatal(err)
	}
	for _, r := range []struct {
		id     string
		age    time.Duration
		hidden bool
	}{{"zz-receipt", time.Hour, false}, {"zz-old", 31 * 24 * time.Hour, false}, {"zz-future", -time.Hour, false}, {"zz-hidden", time.Hour, true}} {
		payload := map[string]any{"run_id": r.id, "outcome": "measured"}
		if r.hidden {
			payload["effect_refs"] = []string{workString(private["ref"])}
		}
		e, err := s.AppendEvent(ctx, "owner", map[string]any{"type": "run_recorded", "summary": "Measured", "refs": []string{"card:" + r.id}, "payload": payload})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = ws.DB().Exec(`UPDATE events SET ts=? WHERE id=?`, loopTS(now.Add(-r.age)), e["id"]); err != nil {
			t.Fatal(err)
		}
	}
	check := func(actor string, limit, want int, partial bool) {
		t.Helper()
		scope := WithRequestAccessScope(ctx, AccessScope{ActorID: actor})
		report, truncated, err := s.LoopReport(scope, "", "", "", limit, now)
		if err != nil {
			t.Fatal(err)
		}
		rows := report["loops"].([]map[string]any)
		if truncated != partial || len(rows) != min(limit, want) || workMap(report["stats"])["loops"] != want {
			t.Fatalf("actor=%s report=%v truncated=%v", actor, report, truncated)
		}
		for _, row := range rows {
			if workString(row["title"]) == "Ordinary" {
				t.Fatal(row)
			}
		}
	}
	check("owner", 24, 4, false)
	check("stranger", 24, 3, false)
	check("stranger", 2, 3, true)
	// A private receipt on an otherwise visible receipt-only loop must not deny
	// that loop; receipt admission checks visible evidence before limiting.
	if _, err = s.AppendEvent(ctx, "owner", map[string]any{"type": "run_recorded", "summary": "Public measurement", "refs": []string{"card:zz-hidden"}, "payload": map[string]any{"run_id": "public", "outcome": "no_op"}}); err != nil {
		t.Fatal(err)
	}
	now = time.Now().UTC()
	check("stranger", 24, 4, false)
}

func TestLoopReportAdmissionRetainsRecentLoopsBeyondFifty(t *testing.T) {
	for _, kind := range []string{"annotation", "receipt", "mixed"} {
		t.Run(kind, func(t *testing.T) {
			ctx := context.Background()
			ws, err := initializeTestWorkspace(ctx, t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer ws.Close()
			s := NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
			if _, err = ws.DB().Exec(`INSERT INTO threads(id,updated_at,updated_by,body_json) VALUES('t','now','owner','{}')`); err != nil {
				t.Fatal(err)
			}
			now := time.Now().UTC()
			for i := 0; i < 61; i++ {
				id := fmt.Sprintf("loop-%02d", i)
				if i == 60 {
					id = "zz-latest-loop"
				}
				at := now.Add(-time.Duration(100-i) * time.Minute)
				if _, err = ws.DB().Exec(`INSERT INTO cards(id,title,summary,column_key,thread_id,created_at,created_by,updated_at,updated_by) VALUES(?,?,'','todo','t',?,'owner',?,'owner')`, id, id, loopTS(at), loopTS(at)); err != nil {
					t.Fatal(err)
				}
				if kind == "annotation" || kind == "mixed" && (i%2 == 0 || i == 60) {
					if _, err = ws.DB().Exec(`INSERT INTO work_metadata(card_id,metadata_json,updated_at,updated_by) VALUES(?,'{"goal":{"metric":"score","direction":"up"}}',?,'owner')`, id, loopTS(at)); err != nil {
						t.Fatal(err)
					}
				}
				if kind == "receipt" || kind == "mixed" && (i%2 == 1 || i == 60) {
					runs := 1
					if i == 60 {
						runs = 2
					}
					for n := 0; n < runs; n++ {
						e, err := s.AppendEvent(ctx, "owner", map[string]any{"type": "run_recorded", "summary": "Attempt", "refs": []string{"card:" + id}, "payload": map[string]any{"run_id": fmt.Sprint(n), "outcome": "infra_error"}})
						if err != nil {
							t.Fatal(err)
						}
						if _, err = ws.DB().Exec(`UPDATE events SET ts=? WHERE id=?`, loopTS(at.Add(time.Duration(n)*time.Second)), e["id"]); err != nil {
							t.Fatal(err)
						}
					}
				}
			}
			report, partial, err := s.LoopReport(WithRequestAccessScope(ctx, AccessScope{ActorID: "owner"}), "", "", "", 48, now)
			if err != nil {
				t.Fatal(err)
			}
			rows := report["loops"].([]map[string]any)
			if !partial || workMap(report["stats"])["loops"] != 50 || len(rows) != 48 || rows[0]["title"] != "zz-latest-loop" {
				t.Fatalf("latest loop lost: %v truncated=%v", report, partial)
			}
			if kind != "annotation" && workMap(rows[0]["loop"])["health"] != "failing" {
				t.Fatal("attention sort lost", rows[0])
			}
		})
	}
}
