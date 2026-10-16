package server

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/testutil/perfguard"
)

func TestLoopReceiptReplayBriefAndReport(t *testing.T) {
	h := newPrimitivesTestServer(t)
	ctx := context.Background()
	s := h.primitiveStore.(*primitives.Store)
	b, err := s.CreateBoard(ctx, "actor-1", map[string]any{"title": "Loops"})
	if err != nil {
		t.Fatal(err)
	}
	w, err := s.CreateWork(ctx, "actor-1", anyString(b["id"]), map[string]any{"title": "Optimizer", "cadence": map[string]any{"every_seconds": 3600}, "goal": map[string]any{"metric": "score", "direction": "up", "baseline": 0.}})
	if err != nil {
		t.Fatal(err)
	}
	event := map[string]any{"type": "run_recorded", "summary": "Kept candidate", "refs": []string{anyString(w["ref"])}, "payload": map[string]any{"run_id": "one", "outcome": "acted", "metrics": map[string]any{"score": 0.8}, "experiment": map[string]any{"candidate": "one", "decision": "kept", "score_after": 0.8}}}
	original, err := s.AppendEvent(ctx, "actor-1", event)
	if err != nil {
		t.Fatal(err)
	}
	event["refs"] = []string{"card:" + anyString(w["id"])}
	replay, err := s.AppendEvent(ctx, "actor-2", event)
	if err != nil || original["id"] != replay["id"] {
		t.Fatal(replay, err)
	}
	data, partial, err := s.LoopReport(ctx, "", anyString(w["ref"]), "", 24, time.Now().UTC())
	if err != nil || partial {
		t.Fatal(data, partial, err)
	}
	if original["ts"] != original["payload"].(map[string]any)["finished_at"] {
		t.Fatal("default finish must equal event time", original)
	}
	loop := data["loop"].(map[string]any)
	if loop["health"] != "healthy" {
		t.Fatal(loop)
	}
	goal := loop["goal"].(map[string]any)
	if goal["latest"] != 0.8 || goal["best"] != 0.8 {
		t.Fatal(goal)
	}
	if len(data["runs"].([]map[string]any)) != 1 {
		t.Fatal(data)
	}
	brief := readBriefV1(t, s)
	initiatives := brief["initiatives"].([]any)
	if len(initiatives) != 1 || initiatives[0].(map[string]any)["loop"] == nil {
		t.Fatal(brief)
	}
	overview, _, err := s.LoopReport(ctx, anyString(b["ref"]), "", "healthy", 24, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if overview["stats"].(map[string]any)["effects_7d"] != 1 || overview["stats"].(map[string]any)["improved_7d"] != 1 {
		t.Fatal(overview)
	}
}

// Ten thousand canonical events exercise density independently of returned rows.
// Setup is outside the measurement and uses a disposable database only.
func TestLoopBriefCost10KEvents(t *testing.T) {
	if testing.Short() {
		t.Skip("10k-event scale fixture")
	}
	h := newPrimitivesTestServer(t)
	s := h.primitiveStore.(*primitives.Store)
	ctx := context.Background()
	b, err := s.CreateBoard(ctx, "actor-1", map[string]any{"title": "Loops"})
	if err != nil {
		t.Fatal(err)
	}
	w, err := s.CreateWork(ctx, "actor-1", anyString(b["id"]), map[string]any{"title": "Dense loop", "cadence": map[string]any{"every_seconds": 3600}, "goal": map[string]any{"metric": "score", "direction": "up"}})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	tx, err := h.workspace.DB().BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	for i := 0; i < 10000; i++ {
		payload, _ := json.Marshal(map[string]any{"subject_ref": "card:" + anyString(w["id"]), "summary": "Measured", "payload": map[string]any{"run_id": fmt.Sprint(i), "outcome": "measured", "metrics": map[string]any{"score": float64(i)}}})
		refs, _ := json.Marshal([]string{"card:" + anyString(w["id"])})
		_, err = tx.ExecContext(ctx, `INSERT INTO events(id,type,ts,actor_id,thread_id,refs_json,payload_json) VALUES(?,'run_recorded',?,'actor-1',?,?,?)`, fmt.Sprintf("scale-loop-%05d", i), now.Add(-time.Duration(10000-i)*time.Second).Format(time.RFC3339Nano), w["thread_id"], string(refs), string(payload))
		if err != nil {
			t.Fatal(err)
		}
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	for done := false; !done; {
		done, err = s.RefreshBriefInitiatives(ctx, 200)
		if err != nil {
			t.Fatal(err)
		}
	}
	pool, capture, err := perfguard.Open("file:" + h.workspace.Layout().DatabasePath + "?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	measured := primitives.NewTestStore(pool, h.workspace.Layout().ArtifactContentDir)
	var budget struct {
		Events  int    `json:"fixture_events"`
		Queries int    `json:"max_queries"`
		Rows    int    `json:"max_rows"`
		VM      uint64 `json:"max_vm_steps"`
		Latency int    `json:"max_latency_ms"`
	}
	raw, err := os.ReadFile("testdata/loop_brief_budget.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(raw, &budget); err != nil {
		t.Fatal(err)
	}
	if budget.Events != 10000 {
		t.Fatal("fixture budget drift")
	}
	// Measure full Brief, not only the enrichment helper.
	durations := []time.Duration{}
	for i := 0; i < 6; i++ {
		capture.Start()
		start := time.Now()
		result, err := measured.Brief(ctx, primitives.BriefReader{Kind: "human"}, now)
		elapsed := time.Since(start)
		statements, queries, returned := capture.Stop()
		work := capture.Work()
		if capture.WorkError() != nil {
			t.Fatal(capture.WorkError())
		}
		if queries > budget.Queries || returned > budget.Rows || work.VMSteps > budget.VM {
			t.Fatalf("SQL=%d rows=%d VM=%d", queries, returned, work.VMSteps)
		}
		t.Logf("brief sample=%d SQL=%d rows=%d VM=%d fullscan=%d sorts=%d elapsed=%s", i, queries, returned, work.VMSteps, work.FullScanSteps, work.Sorts, elapsed)
		if i == 0 {
			found := false
			for _, statement := range statements {
				if !strings.Contains(statement.SQL, "best_before") {
					continue
				}
				found = true
				details, err := perfguard.Explain(ctx, h.workspace.DB(), statement)
				if err != nil {
					t.Fatal(err)
				}
				plan := strings.Join(details, "\n")
				if !strings.Contains(plan, "idx_events_run_subject_ts") || !strings.Contains(plan, "idx_events_run_effect_ts") || strings.Contains(plan, "SCAN events") {
					t.Fatal(plan)
				}
			}
			if !found {
				t.Fatal("missing loop fact query")
			}
		}
		if err != nil {
			t.Fatal(err)
		}
		durations = append(durations, elapsed)
		items := result["initiatives"].([]map[string]any)
		loop := items[0]["loop"].(map[string]any)
		if loop["runs_7d"].(map[string]any)["measured"] != float64(10000) {
			t.Fatal(loop)
		}
		if len(loop["goal"].(map[string]any)["points"].([]map[string]any)) != 60 {
			t.Fatal(loop)
		}
		if elapsed > time.Duration(budget.Latency)*time.Millisecond {
			t.Fatalf("brief exceeded 1s: %s", elapsed)
		}
	}
	t.Logf("full brief / 10k events: first=%s warm=%v", durations[0], durations[1:])
}

func TestLoopReceiptsRespectPrivateProvenance(t *testing.T) {
	h := newPrimitivesTestServer(t)
	s := h.primitiveStore.(*primitives.Store)
	ctx := context.Background()
	card, err := s.CreateWork(ctx, "owner", "", map[string]any{"title": "Public loop", "cadence": map[string]any{"every_seconds": 3600}, "goal": map[string]any{"metric": "score", "direction": "up"}})
	if err != nil {
		t.Fatal(err)
	}
	private, err := s.CreateWork(ctx, "owner", "", map[string]any{"title": "Private proof"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.PatchThread(ctx, "owner", anyString(private["thread_id"]), map[string]any{"pm_actor_id": "owner"}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err = s.AppendEvent(ctx, "owner", map[string]any{"type": "run_recorded", "summary": "Private result", "refs": []string{anyString(card["ref"])}, "payload": map[string]any{"run_id": "private", "outcome": "acted", "metrics": map[string]any{"score": 99.}, "effect_refs": []string{anyString(private["ref"])}}}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.AppendEvent(ctx, "owner", map[string]any{"type": "run_recorded", "summary": "Public measurement", "refs": []string{anyString(card["ref"])}, "payload": map[string]any{"run_id": "public", "outcome": "measured", "metrics": map[string]any{"score": 1.}}}); err != nil {
		t.Fatal(err)
	}
	for _, actor := range []string{"owner", "stranger"} {
		scope := primitives.WithRequestAccessScope(ctx, primitives.AccessScope{ActorID: actor})
		scope, closeRead, err := s.BeginResourceRead(scope)
		if err != nil {
			t.Fatal(err)
		}
		data, _, err := s.LoopReport(scope, "", anyString(card["ref"]), "", 24, time.Now().UTC())
		closeRead()
		if err != nil {
			t.Fatal(err)
		}
		runs := data["runs"].([]map[string]any)
		want := 1
		if actor == "owner" {
			want = 2
		}
		if len(runs) != want {
			t.Fatal(actor, data)
		}
		if actor == "stranger" {
			raw, _ := json.Marshal(data)
			if strings.Contains(string(raw), "Private") || strings.Contains(string(raw), "99") || data["loop"].(map[string]any)["last_effect_at"].(*string) != nil {
				t.Fatal(string(raw))
			}
		}
	}
}
func TestLoopReceiptHTTPValidationAndReplay(t *testing.T) {
	h := newPrimitivesTestServer(t)
	responseActor := postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Actor One","created_at":"2026-03-04T10:00:00Z"}}`, 201)
	responseActor.Body.Close()
	s := h.primitiveStore.(*primitives.Store)
	card, err := s.CreateWork(context.Background(), "actor-1", "", map[string]any{"title": "HTTP loop"})
	if err != nil {
		t.Fatal(err)
	}
	makeBody := func(extra bool) string {
		payload := map[string]any{"run_id": "http-once", "outcome": "no_op"}
		if extra {
			payload["unknown"] = true
		}
		body, _ := json.Marshal(map[string]any{"actor_id": "actor-1", "event": map[string]any{"type": "run_recorded", "summary": "No change", "refs": []string{anyString(card["ref"])}, "provenance": map[string]any{"sources": []string{"inferred"}}, "payload": payload}})
		return string(body)
	}
	response := postJSONExpectStatus(t, h.baseURL+"/events", makeBody(true), 400)
	response.Body.Close()
	var original string
	for i := 0; i < 2; i++ {
		response := postJSONExpectStatus(t, h.baseURL+"/events", makeBody(false), 201)
		var body map[string]any
		err = json.NewDecoder(response.Body).Decode(&body)
		response.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		id := anyString(body["event"].(map[string]any)["id"])
		if i == 0 {
			original = id
		} else if id != original {
			t.Fatal(original, id)
		}
	}
}

func TestLoopConcurrentReceiptReplay(t *testing.T) {
	h := newPrimitivesTestServer(t)
	store := h.primitiveStore.(*primitives.Store)
	ctx := context.Background()
	card, err := store.CreateWork(ctx, "owner", "", map[string]any{"title": "Concurrent loop"})
	if err != nil {
		t.Fatal(err)
	}
	type response struct {
		id  string
		err error
	}
	responses := make(chan response, 8)
	for i := 0; i < 8; i++ {
		go func() {
			event, err := store.AppendEvent(ctx, "owner", map[string]any{"type": "run_recorded", "summary": "Measured once", "refs": []string{anyString(card["ref"])}, "payload": map[string]any{"run_id": "stable", "outcome": "measured"}})
			responses <- response{anyString(event["id"]), err}
		}()
	}
	original := ""
	for i := 0; i < 8; i++ {
		r := <-responses
		if r.err != nil {
			t.Fatal(r.err)
		}
		if original == "" {
			original = r.id
		}
		if r.id != original {
			t.Fatal(original, r.id)
		}
	}
}
