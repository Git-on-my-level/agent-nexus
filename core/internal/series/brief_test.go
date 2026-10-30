package series

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"agent-nexus-core/internal/primitives"
)

func briefRows(t testing.TB, store *primitives.Store, ctx context.Context, now time.Time) map[string]map[string]any {
	t.Helper()
	body, err := store.Brief(ctx, primitives.BriefReader{ActorID: "human", Kind: "human"}, now)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Initiatives []map[string]any `json:"initiatives"`
	}
	if err = json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	result := map[string]map[string]any{}
	for _, row := range decoded.Initiatives {
		result[row["title"].(string)] = row
	}
	return result
}

func TestBriefSeriesVisibilityFlatnessAndBounds(t *testing.T) {
	s, _, _, writer := fixture(t)
	store := primitives.NewTestStore(s.DB, t.TempDir())
	ctx := context.Background()
	now := time.Now().UTC().Truncate(24 * time.Hour).Add(12 * time.Hour)
	// Keep 'now' no later than the wall clock for token validation on Push.
	now = now.Add(-24 * time.Hour)
	work := func(title string, refs []string) {
		t.Helper()
		card, err := store.CreateWork(ctx, "owner", "", map[string]any{"title": title, "initiative": true})
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := json.Marshal(refs)
		if _, err = s.DB.Exec(`UPDATE cards SET refs_json=? WHERE id=?`, string(raw), card["id"]); err != nil {
			t.Fatal(err)
		}
	}
	private, err := store.CreateWork(ctx, "owner", "", map[string]any{"title": "Private source"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.PatchThread(ctx, "owner", fmt.Sprint(private["thread_id"]), map[string]any{"pm_actor_id": "owner"}, nil); err != nil {
		t.Fatal(err)
	}
	for _, loop := range []string{"flat", "moving", "tolerance", "gap", "short", "old", "future", "private"} {
		for d := 0; d < 7; d++ {
			if loop == "gap" && d == 2 || loop == "short" && d > 1 {
				continue
			}
			value := 10.0
			if loop == "moving" && d == 2 {
				value = 20
			}
			if loop == "tolerance" && d == 2 {
				value += 0.000001
			}
			at := now.Add(-time.Duration(d) * 24 * time.Hour)
			if loop == "old" {
				at = at.Add(-48 * time.Hour)
			}
			labels := map[string]string{"loop": loop}
			if loop == "private" {
				labels["ref"] = fmt.Sprint(private["ref"])
			}
			// Push validates authority against the real clock, independently of point time.
			if err = s.Push(ctx, "builds", Point{TS: at.Format(time.RFC3339Nano), Labels: labels, Value: &value}, writer, time.Now()); err != nil {
				t.Fatal(err)
			}
		}
		if loop == "future" {
			value := 10.0
			if err = s.Push(ctx, "builds", Point{TS: now.Add(time.Hour).Format(time.RFC3339Nano), Labels: map[string]string{"loop": loop}, Value: &value}, writer, time.Now()); err != nil {
				t.Fatal(err)
			}
		}
		work(loop, []string{"series:builds?loop=" + loop})
	}
	if _, err = s.DB.Exec(`INSERT INTO series_labels(series,labels) VALUES('builds','{"loop":"empty"}')`); err != nil {
		t.Fatal(err)
	}
	// Retention may split recent and archived summaries; read each day once.
	if _, err = s.DB.Exec(`INSERT INTO series_daily SELECT * FROM series_live_daily WHERE series='builds' AND labels='{"loop":"flat"}' AND day=?`, now.Add(-5*24*time.Hour).Truncate(24*time.Hour).UnixNano()); err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB.Exec(`DELETE FROM series_live_daily WHERE series='builds' AND labels='{"loop":"flat"}' AND day=?`, now.Add(-5*24*time.Hour).Truncate(24*time.Hour).UnixNano()); err != nil {
		t.Fatal(err)
	}
	work("hidden primary", []string{"series:builds?loop=private", "series:builds?loop=flat"})
	work("no refill", []string{"series:builds?loop=private&_baseline=1", "series:builds?loop=private&_baseline=2", "series:builds?loop=private&_baseline=3", "series:builds?loop=private&_baseline=4", "series:builds?loop=private&_baseline=5", "series:builds?loop=flat"})
	work("empty", []string{"series:builds?loop=empty"})
	work("none", nil)
	work("ambiguous", []string{"series:builds"})
	work("bounds", []string{"bad", "series:builds?loop=flat", "series:builds?loop=flat", "series:builds?loop=short", "series:builds?loop=gap", "series:builds?loop=empty", "series:builds?loop=moving", "series:builds?loop=tolerance"})
	work("primary", []string{"series:builds?loop=short", "series:builds?loop=flat"})
	work("fixed options", []string{"series:builds?loop=flat&_range=1h&_baseline=100"})
	value := "green"
	if err = s.Push(ctx, "health", Point{TS: now.Format(time.RFC3339Nano), State: &value}, writer, time.Now()); err != nil {
		t.Fatal(err)
	}
	work("state", []string{"series:health"})
	for drained := false; !drained; {
		var err error
		drained, err = store.RefreshBriefInitiatives(context.Background(), 200)
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, actor := range []string{"owner", "human", "other"} {
		t.Run(actor, func(t *testing.T) {
			scope := primitives.WithRequestAccessScope(ctx, primitives.AccessScope{ActorID: actor})
			scope, closeRead, e := store.BeginResourceRead(scope)
			if e != nil {
				t.Fatal(e)
			}
			defer closeRead()
			rows := briefRows(t, store, scope, now)
			for _, name := range []string{"flat", "moving", "tolerance", "gap", "short", "old", "future", "empty", "ambiguous", "primary", "state", "fixed options"} {
				row := rows[name]
				if row == nil || len(row["series"].([]any)) == 0 {
					t.Fatalf("%s missing: %+v", name, row)
				}
				signal := row["metric_flat"].(map[string]any)
				want := name == "flat" || name == "tolerance" || name == "fixed options"
				if signal["flat"] != want || signal["window_seconds"] != float64(604800) {
					t.Fatalf("%s signal=%+v want flat=%v", name, signal, want)
				}
				if want && signal["for_seconds"].(float64) < 259200 {
					t.Fatalf("short flat span: %+v", signal)
				}
			}
			if rows["none"]["metric_flat"] != nil || len(rows["none"]["series"].([]any)) != 0 {
				t.Fatal(rows["none"])
			}
			if got := rows["bounds"]["series"].([]any); len(got) != 2 || got[0].(map[string]any)["ref"] != "series:builds?loop=flat" {
				t.Fatal(got)
			}
			if got := rows["flat"]["series"].([]any)[0].(map[string]any); got["last_value"] != float64(10) || got["last_at"] != now.Format(time.RFC3339Nano) {
				t.Fatal(got)
			}
			if got := rows["state"]["series"].([]any)[0].(map[string]any); got["last_value"] != "green" {
				t.Fatal(got)
			}
			for _, name := range []string{"empty", "ambiguous"} {
				entry := rows[name]["series"].([]any)[0].(map[string]any)
				if entry["last_value"] != nil || entry["last_at"] != nil {
					t.Fatalf("%s invented data: %+v", name, entry)
				}
			}
			if actor != "owner" {
				if len(rows["no refill"]["series"].([]any)) != 0 {
					t.Fatal("hidden candidates refilled beyond budget")
				}
				if rows["hidden primary"]["metric_flat"].(map[string]any)["flat"] != true {
					t.Fatal("first visible series was not primary")
				}
			}
			if actor == "owner" {
				if len(rows["private"]["series"].([]any)) != 1 {
					t.Fatal("owner lost series")
				}
			} else {
				if rows["private"] == nil || len(rows["private"]["series"].([]any)) != 0 || rows["private"]["metric_flat"] != nil {
					t.Fatalf("private series leaked: %+v", rows["private"])
				}
				raw, _ := json.Marshal(rows)
				if strings.Contains(string(raw), "Private source") {
					t.Fatal("private source leaked")
				}
			}
		})
	}
}
