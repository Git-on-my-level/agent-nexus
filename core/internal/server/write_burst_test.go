package server

import (
	"agent-nexus-core/internal/plans"
	"agent-nexus-core/internal/primitives"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestWriteBurstReadLatency(t *testing.T) {
	testWriteBurstReadLatency(t, false, 0)
}

func TestPerformanceWriteBurstReadLatency(t *testing.T) {
	requirePerformanceTest(t)
	for _, legacy := range []bool{true, false} {
		t.Run(fmt.Sprintf("legacy=%v", legacy), func(t *testing.T) { testWriteBurstReadLatency(t, legacy, 1000) })
	}
}

func testWriteBurstReadLatency(t *testing.T, legacy bool, history int) {
	h := newProjectionMaintenanceTestServer(t)
	ctx := context.Background()
	board, err := h.store.CreateBoard(ctx, "actor-1", map[string]any{"title": "Burst"})
	if err != nil {
		t.Fatal(err)
	}
	boardID := board["id"].(string)
	var cards []map[string]any
	for i := 0; i < 7; i++ {
		card, err := h.store.(*primitives.Store).CreateWork(ctx, "actor-1", boardID, map[string]any{"title": fmt.Sprintf("Plan %d", i)})
		if err != nil {
			t.Fatal(err)
		}
		cards = append(cards, card)
	}
	tx, err := h.workspace.DB().BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < history; i++ {
		id := fmt.Sprintf("history-%06d", i)
		event := map[string]any{"id": id, "type": "message_posted", "ts": "2026-10-10T00:00:00Z", "actor_id": "actor-1", "thread_id": board["thread_id"], "payload": map[string]any{"body": "See " + board["ref"].(string)}, "refs": []string{}}
		raw, _ := json.Marshal(event)
		if _, err := tx.Exec(`INSERT INTO events(id,type,ts,actor_id,thread_id,payload_json) VALUES(?,'message_posted','2026-10-10T00:00:00Z','actor-1',?,?)`, id, board["thread_id"], string(raw)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if legacy {
		// Reconstruct released definitions only in this isolated synthetic workspace.
		for _, table := range []string{"boards", "cards"} {
			name := "mention_identity_" + table + "_update"
			var sql string
			if err := h.workspace.DB().QueryRow(`SELECT sql FROM sqlite_master WHERE name=?`, name).Scan(&sql); err != nil {
				t.Fatal(err)
			}
			sql = strings.Replace(sql, "AFTER UPDATE OF id,handle ON "+table+" WHEN OLD.id IS NOT NEW.id OR OLD.handle IS NOT NEW.handle BEGIN", "AFTER UPDATE ON "+table+" BEGIN", 1)
			if _, err := h.workspace.DB().Exec("DROP TRIGGER " + name); err != nil {
				t.Fatal(err)
			}
			if _, err := h.workspace.DB().Exec(sql); err != nil {
				t.Fatal(err)
			}
		}
	}

	paths := []string{"/inbox", "/cards/" + cards[0]["id"].(string), "/boards/" + boardID}
	probe := func(label string) {
		for _, path := range paths {
			start := time.Now()
			resp, err := http.Get(h.baseURL + path)
			if err != nil {
				t.Fatal(err)
			}
			body, err := io.ReadAll(resp.Body)
			resp.Body.Close()
			if err != nil || resp.StatusCode != 200 {
				t.Fatalf("%s %d %s %v", path, resp.StatusCode, body, err)
			}
			elapsed := time.Since(start)
			t.Logf("%s %s %s", label, path, elapsed)
			if !legacy && elapsed > time.Second {
				t.Fatalf("read stalled: %s %s", path, elapsed)
			}
		}
	}
	probe("baseline")
	// WAL readers must not queue behind an uncommitted writer transaction.
	writer, err := h.workspace.DB().BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	probe("writer held")
	if err := writer.Rollback(); err != nil {
		t.Fatal(err)
	}
	burstStart := time.Now()
	for _, card := range cards {
		steps := []plans.Step{}
		for j := 0; j < 19; j++ {
			steps = append(steps, plans.Step{ID: fmt.Sprintf("s%d", j), Title: "Change", Ref: fmt.Sprintf("https://example.test/pull/%d", j), Status: "not_started", After: []string{}})
		}
		start := time.Now()
		if err := h.store.(*primitives.Store).SetCardPlan(ctx, "actor-1", card["id"].(string), card["updated_at"].(string), plans.Plan{Steps: steps}); err != nil {
			t.Fatal(err)
		}
		t.Logf("plan write %s", time.Since(start))
	}
	var created []map[string]any
	var evidence []string
	for i := 0; i < 5; i++ {
		result, err := h.store.CreateBoardCard(ctx, "actor-1", boardID, primitives.AddBoardCardInput{Title: fmt.Sprintf("New %d", i), ColumnKey: "ready"})
		if err != nil {
			t.Fatal(err)
		}
		opts := h.maintainer.opts
		stored, err := emitCardLifecycleEvent(ctx, opts, "actor-1", buildCardCreatedEvent(result.Board, result.Card))
		if err != nil {
			t.Fatal(err)
		}
		created = append(created, result.Card)
		evidence = append(evidence, "event:"+stored["id"].(string))
		if legacy {
			for _, thread := range []string{result.Card["thread_id"].(string), board["thread_id"].(string)} {
				if err := refreshDerivedTopicProjection(ctx, opts, thread, time.Now(), "actor-1"); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	for i := 0; i < 2; i++ {
		current, err := h.store.GetBoard(ctx, boardID)
		if err != nil {
			t.Fatal(err)
		}
		token, resolution := current["updated_at"].(string), "done"
		refs := []string{evidence[i]}
		result, err := h.store.MoveBoardCard(ctx, "actor-1", boardID, created[i]["id"].(string), primitives.MoveBoardCardInput{ColumnKey: "done", Resolution: &resolution, ResolutionRefs: &refs, IfBoardUpdatedAt: &token})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := emitCardLifecycleEvent(ctx, h.maintainer.opts, "actor-1", buildCardMovedEvent(result.Board, created[i], result.Card, "", "", "", "")); err != nil {
			t.Fatal(err)
		}
		if legacy {
			for _, thread := range []string{result.Card["thread_id"].(string), board["thread_id"].(string)} {
				if err := refreshDerivedTopicProjection(ctx, h.maintainer.opts, thread, time.Now(), "actor-1"); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	t.Logf("burst total history=%d legacy=%v %s", history, legacy, time.Since(burstStart))
	probe("after burst")
	if err := h.maintainer.Step(ctx, time.Now()); err != nil {
		t.Fatal(err)
	}
	probe("after maintenance")
}
