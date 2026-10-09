package primitives_test

import (
	"agent-nexus-core/internal/plans"
	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/testsql"
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestEffectiveHealthInputsAgreeAcrossSurfaces(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	s := primitives.NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	now := time.Now().UTC()
	old := now.Add(-7 * 24 * time.Hour).Format(time.RFC3339Nano)
	for _, withPlan := range []bool{false, true} {
		t.Run(fmt.Sprint(withPlan), func(t *testing.T) {
			card, err := s.CreateWork(ctx, "actor", "", map[string]any{"title": "Source", "source": map[string]any{"authority": "tracker", "connection_id": "one", "native_id": fmt.Sprint(withPlan)}})
			if err != nil {
				t.Fatal(err)
			}
			if withPlan {
				if err = s.SetCardPlan(ctx, "actor", card["id"].(string), card["updated_at"].(string), plans.Plan{Steps: []plans.Step{{ID: "ship", Title: "Ship"}}}); err != nil {
					t.Fatal(err)
				}
			}
			// Make canonical card and plan edits old. Source activity alone is recent.
			if _, err = ws.DB().Exec(`UPDATE cards SET created_at=?,updated_at=? WHERE id=?`, old, old, card["id"]); err != nil {
				t.Fatal(err)
			}
			if _, err = ws.DB().Exec(`UPDATE card_plans SET updated_at=? WHERE card_id=?`, old, card["id"]); err != nil {
				t.Fatal(err)
			}
			_, err = s.SubmitWorkObservation(ctx, "actor", card["id"].(string), map[string]any{"idempotency_key": "activity", "reader_id": "tracker", "reader_revision": "v1", "observed_at": now.Format(time.RFC3339Nano), "source_activity_at": now.Add(-time.Hour).Format(time.RFC3339Nano), "status": "reported", "facts": map[string]any{"phase": "in_progress"}, "evidence": []any{}})
			if err != nil {
				t.Fatal(err)
			}
			work, err := s.GetWork(ctx, card["id"].(string))
			if err != nil {
				t.Fatal(err)
			}
			_, err = s.PatchWork(ctx, "actor", card["id"].(string), work["version"].(int64), map[string]any{"due_at": now.Add(-24 * time.Hour).Format(time.RFC3339Nano)})
			if err != nil {
				t.Fatal(err)
			}
			for _, cleared := range []bool{false, true} {
				if cleared {
					work, _ = s.GetWork(ctx, card["id"].(string))
					if _, err = s.PatchWork(ctx, "actor", card["id"].(string), work["version"].(int64), map[string]any{"due_at": nil}); err != nil {
						t.Fatal(err)
					}
				}
				rawCard, err := s.GetBoardCard(ctx, "", card["id"].(string))
				if err != nil {
					t.Fatal(err)
				}
				work, err = s.GetWork(ctx, card["id"].(string))
				if err != nil {
					t.Fatal(err)
				}
				if err = s.EnrichCardPlans(ctx, []map[string]any{rawCard, work}, nil, now, 72*time.Hour); err != nil {
					t.Fatal(err)
				}
				preview, err := s.ResolveRefs(ctx, []string{card["ref"].(string)}, nil, now, 72*time.Hour)
				if err != nil {
					t.Fatal(err)
				}
				a, b, c := rawCard["plan_health"].(plans.Health), work["plan_health"].(plans.Health), *preview[0].PlanHealth
				expected := "no_plan"
				if withPlan {
					expected = "on_track"
				}
				if !cleared {
					expected = "at_risk"
				}
				if !reflect.DeepEqual(a, b) || !reflect.DeepEqual(a, c) || a.State != expected {
					t.Fatalf("plan=%v cleared=%v raw=%+v work=%+v preview=%+v expected=%s", withPlan, cleared, a, b, c, expected)
				}
			}
		})
	}
}

func TestGenericAliasesAndIndexedLookupIgnoreUnrelatedEvidence(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	db, counter := testsql.Open("file:" + ws.Layout().DatabasePath)
	defer db.Close()
	s := primitives.NewTestStore(db, ws.Layout().ArtifactContentDir)
	card, err := s.CreateWork(ctx, "actor", "", map[string]any{"title": "Evidence", "source_refs": []any{map[string]any{"authority": "arbitrary-source", "connection_id": "one", "native_id": "opaque-uuid", "identifier_aliases": []string{"ACME-42", "ticket_42", "acme:ticket/42", "FOO", "Älias"}, "status": "done", "title": "Milestone"}}})
	if err != nil {
		t.Fatal(err)
	}
	// Unrelated malformed evidence must never be decoded by one requested lookup.
	unrelated, err := s.CreateWork(ctx, "actor", "", map[string]any{"title": "Unrelated"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = ws.DB().Exec(`INSERT INTO work_evidence_records(card_id,slot,evidence_json) VALUES(?,'corrupt','not JSON')`, unrelated["id"]); err != nil {
		t.Fatal(err)
	}
	if _, err = ws.DB().Exec(`INSERT INTO work_evidence_index(card_id,lookup_key,evidence_id) VALUES(?,'unrelated',(SELECT id FROM work_evidence_records WHERE card_id=? AND slot='corrupt'))`, unrelated["id"], unrelated["id"]); err != nil {
		t.Fatal(err)
	}
	lower, err := s.CreateWork(ctx, "actor", "", map[string]any{"title": "Lower", "source_refs": []any{map[string]any{"authority": "arbitrary-source", "connection_id": "one", "native_id": "other-uuid", "identifier_aliases": []string{"foo"}}}})
	if err != nil {
		t.Fatal(err)
	}
	_ = lower
	refs := []string{"ACME-42", "ticket_42", "unpublished-42", "acme:ticket/42", "FOO", "foo", "Älias", "älias"}
	counter.Reset()
	got, err := s.ResolveRefs(ctx, refs, nil, time.Now(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if !got[3].Resolvable || got[4].NativeID != "opaque-uuid" || got[5].NativeID != "other-uuid" || !got[6].Resolvable || got[7].Resolvable {
		t.Fatal("aliases must match exactly", got)
	}
	if !got[0].Resolvable || got[0].Authority != "arbitrary-source" || got[0].NativeID != "opaque-uuid" || got[0].Status != "done" || !got[1].Resolvable || got[2].Resolvable {
		t.Fatal(got)
	}
	if err = s.SetCardPlan(ctx, "actor", card["id"].(string), card["updated_at"].(string), plans.Plan{Steps: []plans.Step{{ID: "milestone", Title: "Milestone", Ref: refs[3]}}}); err != nil {
		t.Fatal(err)
	}
	// Removing source refs invalidates aliases transactionally.
	work, _ := s.GetWork(ctx, card["id"].(string))
	if _, err = s.PatchWork(ctx, "actor", card["id"].(string), work["version"].(int64), map[string]any{"source_refs": []any{}}); err != nil {
		t.Fatal(err)
	}
	got, err = s.ResolveRefs(ctx, refs, nil, time.Now(), 0)
	if err != nil || got[0].Resolvable || got[1].Resolvable {
		t.Fatalf("removed alias survived %+v %v", got, err)
	}
	var actual testsql.ReadQuery
	for _, read := range counter.Reads() {
		if strings.Contains(read.SQL, "WITH candidates AS") {
			actual = read
			break
		}
	}
	if actual.SQL == "" {
		t.Fatal("evidence query not captured")
	}
	rows, err := ws.DB().Query("EXPLAIN QUERY PLAN "+actual.SQL, actual.Args...)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	exactIndex := false
	for rows.Next() {
		var id, parent, unused int
		var detail string
		if err = rows.Scan(&id, &parent, &unused, &detail); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(detail, "idx_work_evidence_lookup") {
			exactIndex = true
		}
	}
	if !exactIndex {
		t.Fatal("evidence lookup does not use the lookup index")
	}
}

// The legacy health field reports every state it has a name for. It used to
// flatten no_plan, at_risk and done into "on_track", which is how seven
// unplanned initiatives came to render green. Only "stale" is still renamed,
// to the "stalled" spelling older clients know, and only a state this core
// does not recognise falls back to "on_track".
func TestLegacyHealthAndDigestVocabulary(t *testing.T) {
	t.Parallel()
	for state, want := range map[string]string{
		"no_plan":  "no_plan",
		"stale":    "stalled",
		"stalled":  "stalled",
		"blocked":  "blocked",
		"at_risk":  "at_risk",
		"on_track": "on_track",
		"done":     "done",
		"":         "on_track",
		"invented": "on_track",
	} {
		if legacy := plans.LegacyHealth(state); legacy != want {
			t.Fatalf("LegacyHealth(%q)=%q want %q", state, legacy, want)
		}
	}
	for _, state := range []string{"no_plan", "stale", "blocked", "at_risk", "on_track", "done"} {
		if legacy := plans.LegacyHealth(state); legacy == "on_track" && state != "on_track" {
			t.Fatalf("%s must not be reported as on_track", state)
		}
	}
	d := primitives.OverviewChanges{Items: []primitives.OverviewChange{}}
	d.Add(primitives.OverviewChange{Kind: "initiative_stale", Ref: "card:initiative", Title: "Initiative"})
	raw, _ := json.Marshal(d)
	var old struct{ Items []struct{ Kind string } }
	if err := json.Unmarshal(raw, &old); err != nil {
		t.Fatal(err)
	}
	if len(old.Items) != 1 || old.Items[0].Kind != "initiative_stalled" || d.Items[0].KindV2 != "initiative_stale" {
		t.Fatal(string(raw))
	}
}

func TestObservationEvidenceInheritsItsObservationTimestamp(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	s := primitives.NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	now := time.Now().UTC()
	old := now.Add(-7 * 24 * time.Hour).Format(time.RFC3339Nano)
	card, err := s.CreateWork(ctx, "actor", "", map[string]any{"title": "Source", "source": map[string]any{"authority": "tracker", "connection_id": "one", "native_id": "source"}, "source_refs": []any{map[string]any{"authority": "tracker", "connection_id": "one", "native_id": "target", "identifier_aliases": []string{"TARGET-42"}, "status": "in_progress", "observed_at": now.Add(-24 * time.Hour).Format(time.RFC3339Nano)}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = ws.DB().Exec(`UPDATE cards SET updated_at=?,created_at=? WHERE id=?`, old, old, card["id"]); err != nil {
		t.Fatal(err)
	}
	_, err = s.SubmitWorkObservation(ctx, "actor", card["id"].(string), map[string]any{"idempotency_key": "new-evidence", "reader_id": "tracker", "reader_revision": "v1", "observed_at": now.Format(time.RFC3339Nano), "status": "reported", "facts": map[string]any{}, "evidence": []any{map[string]any{"authority": "tracker", "connection_id": "one", "native_id": "target", "identifier_aliases": []string{"TARGET-42"}, "status": "done"}}})
	if err != nil {
		t.Fatal(err)
	}
	// A source poll alone must not advance the containing card's activity.
	if _, err = ws.DB().Exec(`UPDATE cards SET updated_at=? WHERE id=?`, old, card["id"]); err != nil {
		t.Fatal(err)
	}
	got, err := s.ResolveRefs(ctx, []string{"TARGET-42"}, nil, now, 0)
	if err != nil || got[0].Status != "done" || got[0].ObservedAt != now.Format(time.RFC3339Nano) {
		t.Fatalf("latest observation evidence lost: %+v %v", got, err)
	}
}

func TestReferencedSourcePlanEditsCountAsActivity(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	s := primitives.NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	now := time.Now().UTC()
	old := now.Add(-7 * 24 * time.Hour).Format(time.RFC3339Nano)
	child, err := s.CreateWork(ctx, "actor", "", map[string]any{"title": "Child", "source": map[string]any{"authority": "tracker", "connection_id": "one", "native_id": "child"}})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.SetCardPlan(ctx, "actor", child["id"].(string), child["updated_at"].(string), plans.Plan{Steps: []plans.Step{{ID: "build", Title: "Build"}}}); err != nil {
		t.Fatal(err)
	}
	parent, err := s.CreateWork(ctx, "actor", "", map[string]any{"title": "Parent"})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.SetCardPlan(ctx, "actor", parent["id"].(string), parent["updated_at"].(string), plans.Plan{Steps: []plans.Step{{ID: "child", Title: "Child", Ref: child["ref"].(string)}}}); err != nil {
		t.Fatal(err)
	}
	if _, err = ws.DB().Exec(`UPDATE cards SET updated_at=?,created_at=?`, old, old); err != nil {
		t.Fatal(err)
	}
	if _, err = ws.DB().Exec(`UPDATE work_metadata SET updated_at=?`, old); err != nil {
		t.Fatal(err)
	}
	if _, err = ws.DB().Exec(`UPDATE card_plans SET updated_at=?`, old); err != nil {
		t.Fatal(err)
	}
	before, err := s.ResolveRefs(ctx, []string{parent["ref"].(string)}, nil, now, 0)
	if err != nil || before[0].PlanHealth.State != "stale" {
		t.Fatalf("before=%+v %v", before, err)
	}
	if _, err = ws.DB().Exec(`UPDATE card_plans SET updated_at=? WHERE card_id=?`, now.Format(time.RFC3339Nano), child["id"]); err != nil {
		t.Fatal(err)
	}
	after, err := s.ResolveRefs(ctx, []string{parent["ref"].(string)}, nil, now, 0)
	if err != nil || after[0].PlanHealth.State != "on_track" {
		t.Fatalf("child plan activity missing=%+v %v", after, err)
	}
}

func TestNativeCardAndSourceURLLookupUsesIndexes(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	db, counter := testsql.Open("file:" + ws.Layout().DatabasePath)
	defer db.Close()
	s := primitives.NewTestStore(db, ws.Layout().ArtifactContentDir)
	sourceURL := "https://tracker.example/tickets/opaque"
	card, err := s.CreateWork(ctx, "actor", "", map[string]any{"title": "Indexed source", "source": map[string]any{"authority": "tracker", "connection_id": "one", "native_id": "opaque", "url": sourceURL}})
	if err != nil {
		t.Fatal(err)
	}
	counter.Reset()
	filtered, err := s.FilterCardAccess(ctx, []map[string]any{card, {"id": card["ref"]}}, func(string, string) bool { return true })
	if err != nil || len(filtered) != 2 {
		t.Fatalf("access matches: %v %v", filtered, err)
	}
	got, err := s.ResolveRefs(ctx, []string{card["ref"].(string), "card:" + card["id"].(string), sourceURL}, nil, time.Now(), 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range got {
		if !item.Resolvable || item.ID != card["id"] {
			t.Fatalf("missing indexed match: %+v", item)
		}
	}
	var access testsql.ReadQuery
	for _, read := range counter.Reads() {
		if strings.Contains(read.SQL, "WITH access_ids AS") {
			access = read
			break
		}
	}
	if access.SQL == "" {
		t.Fatal("access query not captured")
	}
	accessRows, err := ws.DB().Query("EXPLAIN QUERY PLAN "+access.SQL, access.Args...)
	if err != nil {
		t.Fatal(err)
	}
	accessHandle, accessID := false, false
	for accessRows.Next() {
		var id, parent, unused int
		var detail string
		if err = accessRows.Scan(&id, &parent, &unused, &detail); err != nil {
			accessRows.Close()
			t.Fatal(err)
		}
		if detail == "SCAN c" || strings.Contains(detail, "SCAN cards") {
			accessRows.Close()
			t.Fatal("access workspace scan: " + detail)
		}
		accessHandle = accessHandle || (strings.Contains(detail, "SEARCH cards") && strings.Contains(detail, "idx_cards_handle_unique"))
		accessID = accessID || (strings.Contains(detail, "SEARCH cards") && strings.Contains(detail, "sqlite_autoindex_cards_1"))
	}
	err = accessRows.Err()
	accessRows.Close()
	if err != nil || !accessHandle || !accessID {
		t.Fatalf("access indexes: handle=%v id=%v error=%v", accessHandle, accessID, err)
	}
	var actual testsql.ReadQuery
	for _, read := range counter.Reads() {
		if strings.Contains(read.SQL, "WITH requested_cards AS") {
			actual = read
			break
		}
	}
	if actual.SQL == "" || !strings.Contains(actual.SQL, "LIMIT 200") {
		t.Fatal("bounded card query not captured")
	}
	rows, err := ws.DB().Query("EXPLAIN QUERY PLAN "+actual.SQL, actual.Args...)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	sourceIndex, handleIndex, idIndex := false, false, false
	for rows.Next() {
		var id, parent, unused int
		var detail string
		if err = rows.Scan(&id, &parent, &unused, &detail); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(detail, "SCAN c ") || detail == "SCAN c" || strings.Contains(detail, "SCAN work_metadata") {
			t.Fatal("workspace scan: " + detail)
		}
		sourceIndex = sourceIndex || (strings.Contains(detail, "SEARCH") && strings.Contains(detail, "idx_work_source_url"))
		handleIndex = handleIndex || (strings.Contains(detail, "SEARCH cards") && strings.Contains(detail, "idx_cards_handle_unique"))
		idIndex = idIndex || (strings.Contains(detail, "SEARCH cards") && strings.Contains(detail, "sqlite_autoindex_cards_1"))
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	if !sourceIndex || !handleIndex || !idIndex {
		t.Fatalf("missing indexes: source=%v handle=%v id=%v", sourceIndex, handleIndex, idIndex)
	}
}
