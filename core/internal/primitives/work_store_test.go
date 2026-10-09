package primitives_test

import (
	"agent-nexus-core/internal/primitives"
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

func newWorkTestStore(t *testing.T) (*primitives.Store, string) {
	t.Helper()
	ws, err := initializeTestWorkspace(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ws.Close() })
	s := primitives.NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	b, err := s.CreateBoard(context.Background(), "actor-1", map[string]any{"title": "Portfolio"})
	if err != nil {
		t.Fatal(err)
	}
	return s, b["id"].(string)
}

func newEmptyWorkTestStore(t *testing.T) *primitives.Store {
	t.Helper()
	ws, err := initializeTestWorkspace(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ws.Close() })
	return primitives.NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
}

func registerWork(t *testing.T, s *primitives.Store, b string) map[string]any {
	t.Helper()
	w, err := s.CreateWork(context.Background(), "actor-1", b, map[string]any{"title": "Ship release", "source": map[string]any{"authority": "github", "connection_id": "test", "native_id": "org/repo/issues/1"}})
	if err != nil {
		t.Fatal(err)
	}
	return w
}
func TestWorkSourceDeduplicationAndAuthority(t *testing.T) {
	t.Parallel()
	s, b := newWorkTestStore(t)
	ctx := context.Background()
	w := registerWork(t, s, b)
	again := registerWork(t, s, b)
	if w["ref"] != again["ref"] {
		t.Fatalf("duplicate commitments: %v %v", w, again)
	}
	cards, err := s.ListCards(ctx, primitives.CardListFilter{})
	if err != nil || len(cards) != 1 {
		t.Fatalf("must extend cards: %v %v", cards, err)
	}
	_, err = s.PatchWork(ctx, "actor-1", w["id"].(string), 1, map[string]any{"phase": "done"})
	if !errors.Is(err, primitives.ErrInvalidWorkRequest) {
		t.Fatalf("source status writable: %v", err)
	}
	updated, err := s.PatchWork(ctx, "actor-1", w["id"].(string), 1, map[string]any{"next_action": "Review acceptance", "priority": "p1"})
	if err != nil {
		t.Fatal(err)
	}
	if updated["next_action"] != "Review acceptance" {
		t.Fatal(updated)
	}
	_, err = s.PatchWork(ctx, "actor-1", w["id"].(string), 1, map[string]any{"next_action": "stale write"})
	if !errors.Is(err, primitives.ErrConflict) {
		t.Fatalf("CAS bypass: %v", err)
	}
}
func TestWorkObservationReplayOrderingAndFailure(t *testing.T) {
	t.Parallel()
	s, b := newWorkTestStore(t)
	ctx := context.Background()
	w := registerWork(t, s, b)
	id := w["id"].(string)
	report := func(key string, seq int, status, phase string) map[string]any {
		return map[string]any{"idempotency_key": key, "reader_id": "github", "reader_revision": "v1", "observed_at": time.Now().UTC().Format(time.RFC3339Nano), "source_sequence": seq, "status": status, "facts": map[string]any{"phase": phase, "native_status": "custom"}, "evidence": []any{map[string]any{"url": "https://example.test/evidence"}}}
	}
	good := report("second", 2, "verified", "review")
	r, err := s.SubmitWorkObservation(ctx, "actor-1", id, good)
	if err != nil {
		t.Fatal(err)
	}
	if r["observation"].(map[string]any)["verification"] != "reported" {
		t.Fatal("client self-certified verification")
	}
	duplicate, err := s.SubmitWorkObservation(ctx, "actor-1", id, good)
	if err != nil || duplicate["duplicate"] != true {
		t.Fatalf("replay: %v %v", duplicate, err)
	}
	if _, err = s.SubmitWorkObservation(ctx, "actor-1", id, report("older", 1, "reported", "in_progress")); err != nil {
		t.Fatal(err)
	}
	if _, err = s.SubmitWorkObservation(ctx, "actor-1", id, report("failed", 3, "error", "done")); err != nil {
		t.Fatal(err)
	}
	current, err := s.GetWork(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if current["phase"] != "review" {
		t.Fatalf("regressed good evidence: %v", current)
	}
	if current["freshness"].(map[string]any)["status"] != "error" {
		t.Fatal(current)
	}
	obs, _, err := s.ListWorkObservations(ctx, id, 50, "")
	if err != nil || len(obs) != 3 {
		t.Fatalf("audit lost/replayed: %v %v", obs, err)
	}
}
func TestWorkRefreshCoalescesAndUnknownIsNotHealthy(t *testing.T) {
	t.Parallel()
	s, b := newWorkTestStore(t)
	ctx := context.Background()
	w := registerWork(t, s, b)
	id := w["id"].(string)
	if w["freshness"].(map[string]any)["status"] != "unknown" {
		t.Fatal(w)
	}
	a, err := s.RequestWorkRefresh(ctx, "actor-1", id)
	if err != nil {
		t.Fatal(err)
	}
	bmap, err := s.RequestWorkRefresh(ctx, "actor-1", id)
	if err != nil {
		t.Fatal(err)
	}
	if a["requested_at"] != bmap["requested_at"] || bmap["state"] != "queued" {
		t.Fatalf("noncoalesced: %v %v", a, bmap)
	}
}

func TestWorkRejectsLegacySourceMutations(t *testing.T) {
	t.Parallel()
	s, b := newWorkTestStore(t)
	ctx := context.Background()
	w := registerWork(t, s, b)
	id := w["id"].(string)
	owner := "someone"
	_, err := s.UpdateBoardCard(ctx, "actor-1", "", id, primitives.UpdateBoardCardInput{Assignee: &owner})
	if !errors.Is(err, primitives.ErrInvalidBoardRequest) {
		t.Fatalf("external owner bypass: %v", err)
	}
	_, err = s.MoveBoardCard(ctx, "actor-1", b, id, primitives.MoveBoardCardInput{ColumnKey: "ready"})
	if !errors.Is(err, primitives.ErrInvalidBoardRequest) {
		t.Fatalf("external phase bypass: %v", err)
	}
	title := "changed"
	_, _, err = s.CreateCardRevision(ctx, "actor-1", id, primitives.CreateCardRevisionInput{Title: &title})
	if !errors.Is(err, primitives.ErrInvalidBoardRequest) {
		t.Fatalf("external revision bypass: %v", err)
	}
}
func TestWorkConflictingReplayAndCompletionEvidence(t *testing.T) {
	t.Parallel()
	s, b := newWorkTestStore(t)
	ctx := context.Background()
	w := registerWork(t, s, b)
	id := w["id"].(string)
	o := map[string]any{"idempotency_key": "key", "reader_id": "r", "reader_revision": "1", "observed_at": time.Now().UTC().Format(time.RFC3339Nano), "status": "reported", "facts": map[string]any{"phase": "review"}}
	if _, err := s.SubmitWorkObservation(ctx, "actor-1", id, o); err != nil {
		t.Fatal(err)
	}
	o["facts"] = map[string]any{"phase": "blocked"}
	if _, err := s.SubmitWorkObservation(ctx, "actor-1", id, o); !errors.Is(err, primitives.ErrConflict) {
		t.Fatalf("conflicting replay accepted: %v", err)
	}
	o["idempotency_key"] = "done"
	o["facts"] = map[string]any{"phase": "done"}
	if _, err := s.SubmitWorkObservation(ctx, "actor-1", id, o); !errors.Is(err, primitives.ErrInvalidWorkRequest) {
		t.Fatalf("unproven completion: %v", err)
	}
	o["facts"] = map[string]any{"phase": "review"}
	o["observed_at"] = time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano)
	if _, err := s.SubmitWorkObservation(ctx, "actor-1", id, o); !errors.Is(err, primitives.ErrInvalidWorkRequest) {
		t.Fatalf("future timestamp accepted: %v", err)
	}
}

func TestWorkUnsequencedOutageAfterSequencedSuccess(t *testing.T) {
	t.Parallel()
	s, b := newWorkTestStore(t)
	ctx := context.Background()
	w := registerWork(t, s, b)
	id := w["id"].(string)
	now := time.Now().UTC()
	good := map[string]any{"idempotency_key": "good", "reader_id": "reader", "reader_revision": "v1", "observed_at": now.Add(-time.Second).Format(time.RFC3339Nano), "source_sequence": 9, "status": "reported", "facts": map[string]any{"phase": "review"}}
	if _, err := s.SubmitWorkObservation(ctx, "actor-1", id, good); err != nil {
		t.Fatal(err)
	}
	failed := map[string]any{"idempotency_key": "outage", "reader_id": "reader", "reader_revision": "v1", "observed_at": now.Format(time.RFC3339Nano), "status": "error", "error": map[string]any{"code": "unreachable", "message": "fixture"}}
	if _, err := s.SubmitWorkObservation(ctx, "actor-1", id, failed); err != nil {
		t.Fatal(err)
	}
	current, err := s.GetWork(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if current["phase"] != "review" || current["freshness"].(map[string]any)["status"] != "error" {
		t.Fatalf("outage hidden: %v", current)
	}
}

func TestWorkUnchangedSequenceRefreshesWithoutProgress(t *testing.T) {
	t.Parallel()
	s, b := newWorkTestStore(t)
	ctx := context.Background()
	w := registerWork(t, s, b)
	id := w["id"].(string)
	now := time.Now().UTC()
	for i := 0; i < 2; i++ {
		o := map[string]any{"idempotency_key": fmt.Sprintf("poll-%d", i), "reader_id": "r", "reader_revision": "1", "observed_at": now.Add(time.Duration(i-1) * time.Second).Format(time.RFC3339Nano), "source_sequence": 9, "source_revision": "9", "status": "reported", "facts": map[string]any{"phase": "review"}}
		if _, err := s.SubmitWorkObservation(ctx, "actor-1", id, o); err != nil {
			t.Fatal(err)
		}
	}
	current, err := s.GetWork(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	fresh := current["freshness"].(map[string]any)
	if fresh["last_observed_at"] != now.Format(time.RFC3339Nano) {
		t.Fatalf("unchanged source failed freshness: %v", fresh)
	}
	if _, ok := fresh["meaningful_progress_at"]; ok {
		t.Fatal("poll invented progress")
	}
}

func TestWorkObservationPaginationSurvivesInsert(t *testing.T) {
	t.Parallel()
	s, b := newWorkTestStore(t)
	ctx := context.Background()
	w := registerWork(t, s, b)
	ref := w["ref"].(string)
	submit := func(key string) {
		t.Helper()
		_, err := s.SubmitWorkObservation(ctx, "actor-1", ref, map[string]any{"idempotency_key": key, "reader_id": "r", "reader_revision": "1", "observed_at": time.Now().UTC().Format(time.RFC3339Nano), "status": "reported", "facts": map[string]any{"phase": "review"}})
		if err != nil {
			t.Fatal(err)
		}
	}
	submit("one")
	submit("two")
	page, cursor, err := s.ListWorkObservations(ctx, ref, 1, "")
	if err != nil || len(page) != 1 || cursor == "" {
		t.Fatalf("first page: %v %v", page, err)
	}
	submit("three")
	older, next, err := s.ListWorkObservations(ctx, ref, 1, cursor)
	if err != nil || len(older) != 1 || older[0]["idempotency_key"] != "one" || next != "" {
		t.Fatalf("insert destabilized cursor: %v %s %v", older, next, err)
	}
	events, err := s.ListEventsByThread(ctx, w["thread_id"].(string))
	if err != nil {
		t.Fatal(err)
	}
	moved := 0
	for _, event := range events {
		if event["type"] == "card_moved" {
			moved++
		}
	}
	// Registration, the first phase change, and that change's board move.
	// Later identical polls must not add another move.
	if len(events) != 3 || moved != 1 {
		t.Fatalf("routine polls created semantic events: %d moved=%d", len(events), moved)
	}
}

func TestWorkObservationMovesExternalBoardCard(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	type step struct {
		key      string
		seq      int
		status   string
		phase    string
		revision string
		evidence bool
		replay   bool
	}
	cases := []struct {
		name         string
		native       bool
		createPhase  string
		steps        []step
		wantColumn   string
		wantPhase    string
		wantMoves    int
		wantVersion  int64
		wantDecision string
		wantDone     bool
	}{
		{
			name:        "review to in_progress",
			createPhase: "review",
			steps:       []step{{key: "move", seq: 2, status: "reported", phase: "in_progress", revision: "rev-2"}},
			wantColumn:  "in_progress", wantPhase: "in_progress", wantMoves: 1, wantVersion: 1, wantDecision: "rev-2",
		},
		{
			name:        "done with evidence",
			createPhase: "review",
			steps:       []step{{key: "done", seq: 3, status: "reported", phase: "done", revision: "rev-3", evidence: true}},
			wantColumn:  "done", wantPhase: "done", wantMoves: 1, wantVersion: 1, wantDecision: "rev-3", wantDone: true,
		},
		{
			name:        "error does not move",
			createPhase: "review",
			steps:       []step{{key: "err", seq: 4, status: "error", phase: "done", revision: "rev-4", evidence: true}},
			wantColumn:  "review", wantPhase: "review", wantMoves: 0, wantVersion: 1, wantDecision: "rev-0",
		},
		{
			name:        "older sequence does not move",
			createPhase: "review",
			steps: []step{
				{key: "new", seq: 5, status: "reported", phase: "in_progress", revision: "rev-5"},
				{key: "old", seq: 1, status: "reported", phase: "backlog", revision: "rev-1"},
			},
			wantColumn: "in_progress", wantPhase: "in_progress", wantMoves: 1, wantVersion: 1, wantDecision: "rev-5",
		},
		{
			name:        "duplicate replay does not move twice",
			createPhase: "review",
			steps: []step{
				{key: "once", seq: 6, status: "reported", phase: "blocked", revision: "rev-6"},
				{key: "once", seq: 6, status: "reported", phase: "blocked", revision: "rev-6", replay: true},
			},
			wantColumn: "blocked", wantPhase: "blocked", wantMoves: 1, wantVersion: 1, wantDecision: "rev-6",
		},
		{
			name:        "cancelled maps to backlog",
			createPhase: "review",
			steps:       []step{{key: "cancel", seq: 7, status: "reported", phase: "cancelled", revision: "rev-7"}},
			wantColumn:  "backlog", wantPhase: "cancelled", wantMoves: 1, wantVersion: 1, wantDecision: "rev-7",
		},
		{
			name:        "native card unaffected",
			native:      true,
			createPhase: "review",
			steps:       []step{{key: "native", seq: 8, status: "reported", phase: "in_progress", revision: "rev-8"}},
			wantColumn:  "review", wantPhase: "review", wantMoves: 0, wantVersion: 1, wantDecision: "1.1",
		},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, boardID := newWorkTestStore(t)
			occupantRank := ""
			if tc.wantColumn == "in_progress" && !tc.native {
				occupant, err := s.CreateWork(ctx, "actor-1", boardID, map[string]any{
					"title": "Already working", "phase": "in_progress",
					"source": map[string]any{"authority": "github", "connection_id": "c", "native_id": fmt.Sprintf("occupant-%d", i)},
				})
				if err != nil {
					t.Fatal(err)
				}
				placed, err := s.GetBoardCard(ctx, boardID, occupant["id"].(string))
				if err != nil {
					t.Fatal(err)
				}
				occupantRank = placed["rank"].(string)
			}
			source := map[string]any{"authority": "github", "connection_id": "c", "native_id": fmt.Sprintf("native-%d", i), "revision": "rev-0"}
			input := map[string]any{"title": tc.name, "phase": tc.createPhase, "source": source}
			if tc.native {
				input["source"] = map[string]any{"authority": "nexus"}
			}
			w, err := s.CreateWork(ctx, "actor-1", boardID, input)
			if err != nil {
				t.Fatal(err)
			}
			id := w["id"].(string)
			var previous map[string]any
			for _, step := range tc.steps {
				body := previous
				if !step.replay {
					body = map[string]any{
						"idempotency_key": step.key, "reader_id": "github", "reader_revision": "v1",
						"observed_at": time.Now().UTC().Format(time.RFC3339Nano), "source_sequence": step.seq,
						"status": step.status, "source_revision": step.revision,
						"facts": map[string]any{"phase": step.phase},
					}
					if step.evidence {
						body["evidence"] = []any{map[string]any{"url": "https://example.test/evidence", "kind": "issue"}}
					}
					if step.status == "error" {
						body["error"] = map[string]any{"code": "boom", "message": "failed"}
					}
					previous = body
				}
				got, err := s.SubmitWorkObservation(ctx, "actor-1", id, body)
				if err != nil {
					t.Fatal(err)
				}
				if step.replay && got["duplicate"] != true {
					t.Fatalf("expected replay: %#v", got["duplicate"])
				}
			}
			card, err := s.GetBoardCard(ctx, boardID, id)
			if err != nil {
				t.Fatal(err)
			}
			if card["column_key"] != tc.wantColumn {
				t.Fatalf("column_key=%v want %s", card["column_key"], tc.wantColumn)
			}
			if tc.wantColumn == "in_progress" && occupantRank != "" && card["rank"].(string) <= occupantRank {
				t.Fatalf("moved card rank %q is not after occupant %q", card["rank"], occupantRank)
			}
			current, err := s.GetWork(ctx, id)
			if err != nil {
				t.Fatal(err)
			}
			if current["phase"] != tc.wantPhase || current["version"] != tc.wantVersion || current["decision_revision"] != tc.wantDecision {
				t.Fatalf("work phase=%v version=%v decision=%v", current["phase"], current["version"], current["decision_revision"])
			}
			if tc.wantDone {
				if current["resolution"] != "done" {
					t.Fatalf("resolution=%v", current["resolution"])
				}
				switch refs := current["resolution_refs"].(type) {
				case []any:
					if len(refs) == 0 {
						t.Fatal("done move dropped completion evidence")
					}
				case []string:
					if len(refs) == 0 {
						t.Fatal("done move dropped completion evidence")
					}
				default:
					t.Fatalf("done move dropped completion evidence: %#v", current["resolution_refs"])
				}
			}
			events, err := s.ListEventsByThread(ctx, w["thread_id"].(string))
			if err != nil {
				t.Fatal(err)
			}
			moved := 0
			for _, event := range events {
				if event["type"] != "card_moved" {
					continue
				}
				moved++
				payload, _ := event["payload"].(map[string]any)
				if payload["column_key"] != tc.wantColumn {
					t.Fatalf("card_moved column=%v want %s", payload["column_key"], tc.wantColumn)
				}
			}
			if moved != tc.wantMoves {
				t.Fatalf("card_moved count=%d want %d", moved, tc.wantMoves)
			}
		})
	}
}

func TestWorkReferencesRemainWorkspaceScoped(t *testing.T) {
	t.Parallel()
	s, b := newWorkTestStore(t)
	ctx := context.Background()
	_, err := s.CreateWork(ctx, "actor-1", b, map[string]any{"title": "Invalid project", "project_ref": "topic:missing"})
	if !errors.Is(err, primitives.ErrInvalidWorkRequest) {
		t.Fatalf("unresolved project accepted: %v", err)
	}
	w := registerWork(t, s, b)
	_, err = s.PatchWork(ctx, "actor-1", w["ref"].(string), 1, map[string]any{"relations": []any{map[string]any{"kind": "depends_on", "ref": "card:other-workspace"}}})
	if !errors.Is(err, primitives.ErrInvalidWorkRequest) {
		t.Fatalf("unresolved dependency accepted: %v", err)
	}
}

func TestWorkMigrationRelationRoundTrip(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ws.Close() })
	s := primitives.NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	board, err := s.CreateBoard(ctx, "actor-1", map[string]any{"title": "Portfolio"})
	if err != nil {
		t.Fatal(err)
	}
	b := board["id"].(string)
	legacy := registerWork(t, s, b)
	initiative, err := s.CreateWork(ctx, "actor-1", b, map[string]any{"title": "Reliable execution initiative"})
	if err != nil {
		t.Fatal(err)
	}
	// Long-running fleet collectors have already emitted thousands of updates.
	// Fill the allocator's original bounded numeric suffix space efficiently.
	_, err = ws.DB().ExecContext(ctx, `WITH RECURSIVE numbers(n) AS (SELECT 1 UNION ALL SELECT n+1 FROM numbers WHERE n<1000)
		INSERT OR IGNORE INTO events(id,handle,type,ts,actor_id,thread_id,refs_json,payload_json)
		SELECT 'fixture-' || n, CASE WHEN n=1 THEN 'card-updated' ELSE 'card-updated-' || n END,
		'card_updated','2026-10-01T00:00:00Z','actor-1',?,'[]','{}' FROM numbers`, legacy["thread_id"])
	if err != nil {
		t.Fatal(err)
	}
	relation := map[string]any{"kind": "related", "ref": initiative["ref"], "adapter_migration": "db92c565fbc1c2fa88fe2538306f9292c8523aab5eb8210f7af5b245aa036703", "note": "Folded into initiative; archived detail retained, source remains authoritative."}
	_, err = s.PatchWork(ctx, "actor-1", legacy["ref"].(string), 1, map[string]any{"relations": []any{relation}})
	if err != nil {
		t.Fatalf("migration relation patch: %T: %v", err, err)
	}
	readback, err := s.GetWork(ctx, legacy["ref"].(string))
	if err != nil {
		t.Fatal(err)
	}
	got := readback["relations"].([]any)[0].(map[string]any)
	for key, value := range relation {
		if got[key] != value {
			t.Fatalf("relation %s = %v, want %v", key, got[key], value)
		}
	}
	if readback["version"] != int64(2) || readback["phase"] != legacy["phase"] {
		t.Fatalf("annotation changed source state or failed to advance version: %#v", readback)
	}
}

func TestCreateWorkHonorsStableCardID(t *testing.T) {
	t.Parallel()
	s, b := newWorkTestStore(t)
	w, err := s.CreateWork(context.Background(), "actor-1", b, map[string]any{"id": "card-anx-github-208", "title": "Public issue", "source": map[string]any{"authority": "github", "connection_id": "github-main", "native_id": "Git-on-my-level/agent-nexus#208"}})
	if err != nil {
		t.Fatal(err)
	}
	if w["id"] != "card-anx-github-208" && w["ref"] != "card:card-anx-github-208" {
		t.Fatalf("stable work id not preserved: id=%v ref=%v", w["id"], w["ref"])
	}
}

func TestCreateWorkMoveReplayReusesOnlyMatchingMove(t *testing.T) {
	t.Parallel()
	s, b := newWorkTestStore(t)
	ctx := context.Background()
	input := map[string]any{
		"id": "move-card-stable-id", "title": "Moved commitment",
		"source":         map[string]any{"authority": "nexus"},
		"plan":           map[string]any{"steps": []any{"copy", "verify"}},
		"workspace_move": map[string]any{"move_id": "mv_stable", "source_ref": "card:source"},
	}
	first, err := s.CreateWork(ctx, "actor-1", b, input)
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := s.CreateWork(ctx, "actor-1", b, input)
	if err != nil {
		t.Fatalf("replay same move: %v", err)
	}
	marker, _ := replayed["workspace_move"].(map[string]any)
	if first["ref"] != replayed["ref"] || replayed["plan"] == nil || marker["move_id"] != "mv_stable" {
		t.Fatalf("move replay lost identity or annotations: first=%#v replay=%#v", first, replayed)
	}
	conflicting := map[string]any{
		"id": "move-card-stable-id", "title": "Moved commitment",
		"source":         map[string]any{"authority": "nexus"},
		"workspace_move": map[string]any{"move_id": "mv_other"},
	}
	if _, err := s.CreateWork(ctx, "actor-1", b, conflicting); !errors.Is(err, primitives.ErrConflict) {
		t.Fatalf("unrelated move reused deterministic card id: %v", err)
	}
}

func TestBoardMoveBindsAndAdvancesWorkRevision(t *testing.T) {
	t.Parallel()
	s, b := newWorkTestStore(t)
	ctx := context.Background()
	w, err := s.CreateWork(ctx, "actor-1", b, map[string]any{"title": "Native revision"})
	if err != nil {
		t.Fatal(err)
	}
	id := w["id"].(string)
	version := w["version"].(int64)
	if _, err = s.MoveBoardCard(ctx, "actor-1", b, id, primitives.MoveBoardCardInput{ColumnKey: "ready"}); err != nil {
		t.Fatal(err)
	}
	updated, err := s.GetWork(ctx, id)
	if err != nil || updated["version"] != version+1 || updated["phase"] != "ready" {
		t.Fatalf("move %v %v", updated, err)
	}
	// Simulates a PM approval obtained before the ordinary board move.
	if _, err = s.MoveBoardCard(ctx, "actor-1", b, id, primitives.MoveBoardCardInput{ColumnKey: "review", IfWorkVersion: &version}); !errors.Is(err, primitives.ErrConflict) {
		t.Fatalf("stale move %v", err)
	}
	card, err := s.GetBoardCard(ctx, b, id)
	if err != nil || card["column_key"] != "ready" {
		t.Fatalf("board diverged %v %v", card, err)
	}
	version++
	if _, err = s.MoveBoardCard(ctx, "actor-1", b, id, primitives.MoveBoardCardInput{ColumnKey: "done", IfWorkVersion: &version}); !errors.Is(err, primitives.ErrInvalidBoardRequest) {
		t.Fatalf("completion bypassed evidence gate %v", err)
	}
	after, err := s.GetWork(ctx, id)
	if err != nil || after["version"] != version || after["phase"] != "ready" {
		t.Fatalf("rejected move mutated work %v %v", after, err)
	}
}

func TestCreateWorkWithoutBoardRefCreatesDefaultBoard(t *testing.T) {
	t.Parallel()
	s := newEmptyWorkTestStore(t)
	ctx := context.Background()
	boards, _, err := s.ListBoards(ctx, primitives.BoardListFilter{})
	if err != nil || len(boards) != 0 {
		t.Fatalf("expected empty workspace, got %d boards err=%v", len(boards), err)
	}
	w, err := s.CreateWork(ctx, "actor-1", "", map[string]any{"title": "First task", "source": map[string]any{"authority": "nexus"}})
	if err != nil {
		t.Fatal(err)
	}
	boardID := fmt.Sprint(w["board_id"])
	if boardID == "" || boardID == "<nil>" {
		t.Fatalf("created work has no board_id: %#v", w)
	}
	if fmt.Sprint(w["board_ref"]) == "" {
		t.Fatalf("created work has no board_ref: %#v", w)
	}
	listed, _, err := s.ListBoards(ctx, primitives.BoardListFilter{})
	if err != nil || len(listed) != 1 {
		t.Fatalf("expected one default board, got %d err=%v", len(listed), err)
	}
	card, err := s.GetBoardCard(ctx, boardID, fmt.Sprint(w["id"]))
	if err != nil || fmt.Sprint(card["title"]) != "First task" {
		t.Fatalf("card did not land on a usable board: %v %v", card, err)
	}
	again, err := s.CreateWork(ctx, "actor-1", "", map[string]any{"title": "Second task", "source": map[string]any{"authority": "nexus"}})
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(again["board_id"]) != boardID {
		t.Fatalf("second create used a different board: %v vs %v", again["board_id"], boardID)
	}
}

func TestCreateWorkWithoutBoardRefReusesExistingBoard(t *testing.T) {
	t.Parallel()
	s, existing := newWorkTestStore(t)
	ctx := context.Background()
	w, err := s.CreateWork(ctx, "actor-1", "", map[string]any{"title": "On existing board"})
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(w["board_id"]) != existing {
		t.Fatalf("expected existing board %q, got %v", existing, w["board_id"])
	}
	listed, _, err := s.ListBoards(ctx, primitives.BoardListFilter{})
	if err != nil || len(listed) != 1 {
		t.Fatalf("must not create a second board: %d err=%v", len(listed), err)
	}
}

func TestCreateWorkWithoutBoardRefConcurrent(t *testing.T) {
	t.Parallel()
	s := newEmptyWorkTestStore(t)
	ctx := context.Background()
	const n = 12
	type result struct {
		boardID string
		err     error
	}
	results := make(chan result, n)
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			w, err := s.CreateWork(ctx, "actor-1", "", map[string]any{"title": fmt.Sprintf("Concurrent %d", i), "source": map[string]any{"authority": "nexus"}})
			if err != nil {
				results <- result{err: err}
				return
			}
			results <- result{boardID: fmt.Sprint(w["board_id"])}
		}(i)
	}
	wg.Wait()
	close(results)
	boardIDs := map[string]struct{}{}
	for r := range results {
		if r.err != nil {
			t.Fatal(r.err)
		}
		boardIDs[r.boardID] = struct{}{}
	}
	if len(boardIDs) != 1 {
		t.Fatalf("concurrent creates split across boards: %v", boardIDs)
	}
	listed, _, err := s.ListBoards(ctx, primitives.BoardListFilter{})
	if err != nil || len(listed) != 1 {
		t.Fatalf("expected one board after concurrent creates, got %d err=%v", len(listed), err)
	}
}

func TestCreateWorkRejectedRequestLeavesNoDefaultBoard(t *testing.T) {
	t.Parallel()
	s := newEmptyWorkTestStore(t)
	ctx := context.Background()
	for _, input := range []map[string]any{
		{"source": map[string]any{"authority": "nexus"}},
		{"title": "Bad phase", "phase": "done", "source": map[string]any{"authority": "nexus"}},
		{"title": "Bad priority", "priority": "p9", "source": map[string]any{"authority": "nexus"}},
	} {
		if _, err := s.CreateWork(ctx, "actor-1", "", input); err == nil {
			t.Fatalf("expected rejection for %v", input)
		}
	}
	boards, _, err := s.ListBoards(ctx, primitives.BoardListFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(boards) != 0 {
		t.Fatalf("a rejected work.create provisioned a board: %v", boards)
	}
}

func TestWorkObservationOnArchivedExternalCardIsAcceptedWithoutMove(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, boardID := newWorkTestStore(t)
	w, err := s.CreateWork(ctx, "actor-1", boardID, map[string]any{
		"title": "archived external", "phase": "review",
		"source": map[string]any{"authority": "github", "connection_id": "c", "native_id": "archived-1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	id := w["id"].(string)
	if _, err = s.ArchiveBoardCard(ctx, "actor-1", boardID, id, primitives.RemoveBoardCardInput{}); err != nil {
		t.Fatal(err)
	}
	_, err = s.SubmitWorkObservation(ctx, "actor-1", id, map[string]any{
		"idempotency_key": "after-archive", "reader_id": "github", "reader_revision": "v1",
		"observed_at": time.Now().UTC().Format(time.RFC3339Nano), "source_sequence": 2,
		"status": "reported", "source_revision": "rev-2", "facts": map[string]any{"phase": "in_progress"},
	})
	if err != nil {
		t.Fatalf("observation on archived card rejected: %v", err)
	}
	current, err := s.GetWork(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if current["phase"] != "in_progress" {
		t.Fatalf("projected phase=%v", current["phase"])
	}
}
