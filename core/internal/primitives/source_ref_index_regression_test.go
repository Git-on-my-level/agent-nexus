package primitives

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"agent-nexus-core/internal/plans"
	"agent-nexus-core/internal/storage"
)

func TestIndexedAliasesAreGenericAndBounded(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	s := NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	board, err := s.CreateBoard(ctx, "actor", map[string]any{"title": "Index test"})
	if err != nil {
		t.Fatal(err)
	}
	evidence := map[string]any{"authority": "arbitrary-provider", "connection_id": "connection", "native_id": "opaque", "identifier": "ANY-PREFIX-42", "aliases": []string{"custom-reference"}, "status": "done", "title": "Public source"}
	card, err := s.CreateWork(ctx, "actor", board["id"].(string), map[string]any{"title": "Evidence", "source_refs": []any{evidence}})
	if err != nil {
		t.Fatal(err)
	}
	for _, ref := range []string{"ANY-PREFIX-42", "custom-reference", "arbitrary-provider:opaque"} {
		items, err := s.ResolveRefs(ctx, []string{ref}, nil, time.Now(), 0)
		if err != nil {
			t.Fatal(err)
		}
		if !items[0].Resolvable || items[0].Authority != "arbitrary-provider" || items[0].NativeID != "opaque" || items[0].Status != "done" {
			t.Fatal(items)
		}
	}
	other, err := s.ResolveRefs(ctx, []string{"other-provider:opaque"}, nil, time.Now(), 0)
	if err != nil || other[0].Resolvable || other[0].Status != "" || other[0].Authority != "" {
		t.Fatalf("typed identity broadened: %v %v", other, err)
	}
	unknown, err := s.ResolveRefs(ctx, []string{"UNPUBLISHED-42"}, nil, time.Now(), 0)
	if err != nil || unknown[0].Resolvable || unknown[0].Authority != "" {
		t.Fatalf("unknown=%v err=%v", unknown, err)
	}
	// An indexed lookup must not decode unrelated workspace evidence.
	for i := 0; i < 1000; i++ {
		if _, err = s.db.ExecContext(ctx, `INSERT INTO work_evidence_index(card_id,lookup_key,evidence_id) VALUES(?,?,(SELECT id FROM work_evidence_records WHERE card_id=? LIMIT 1))`, card["id"], fmt.Sprintf("unrelated-%d", i), card["id"]); err != nil {
			t.Fatal(err)
		}
	}
	items, err := s.ResolveRefs(ctx, []string{"custom-reference"}, nil, time.Now(), 0)
	if err != nil || items[0].Title != "Public source" {
		t.Fatalf("items=%v err=%v", items, err)
	}
	rows, err := s.db.QueryContext(ctx, `EXPLAIN QUERY PLAN SELECT evidence_id FROM work_evidence_index INDEXED BY idx_work_evidence_lookup WHERE lookup_key IN (SELECT value FROM json_each(?)) LIMIT 33`, `["custom-reference"]`)
	if err != nil {
		t.Fatal(err)
	}
	indexed := false
	for rows.Next() {
		var id, parent, unused int
		var detail string
		if err = rows.Scan(&id, &parent, &unused, &detail); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(detail, "SEARCH") && strings.Contains(detail, "idx_work_evidence_lookup") {
			indexed = true
		}
	}
	rows.Close()
	if !indexed {
		t.Fatal("alias query did not SEARCH the alias index")
	}
	// Overloaded aliases fail conservatively even when all rows share identity.
	raw, _ := json.Marshal(evidence)
	for i := 0; i < 33; i++ {
		if _, err = s.db.ExecContext(ctx, `INSERT INTO work_evidence_records(card_id,slot,evidence_json) VALUES(?,?,?)`, card["id"], fmt.Sprintf("overload-%d", i), string(raw)); err != nil {
			t.Fatal(err)
		}
		if _, err = s.db.ExecContext(ctx, `INSERT INTO work_evidence_index(card_id,lookup_key,evidence_id) VALUES(?,?,(SELECT id FROM work_evidence_records WHERE card_id=? AND slot=?))`, card["id"], "overloaded-alias", card["id"], fmt.Sprintf("overload-%d", i)); err != nil {
			t.Fatal(err)
		}
	}
	items, err = s.ResolveRefs(ctx, []string{"overloaded-alias"}, nil, time.Now(), 0)
	if err != nil || items[0].Resolvable {
		t.Fatalf("items=%v err=%v", items, err)
	}
	// Transactional clearing removes aliases as well as canonical annotations.
	work, err := s.GetWork(ctx, card["id"].(string))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.PatchWork(ctx, "actor", card["id"].(string), work["version"].(int64), map[string]any{"source_refs": []any{}}); err != nil {
		t.Fatal(err)
	}
	items, err = s.ResolveRefs(ctx, []string{"custom-reference"}, nil, time.Now(), 0)
	if err != nil || items[0].Resolvable {
		t.Fatalf("items=%v err=%v", items, err)
	}
}

func TestAggregatedPlanRefsRespectBudgetAndBatchGuard(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	s := NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	ps := map[string]plans.Plan{}
	for i := 0; i < 21; i++ {
		p := plans.Plan{Steps: []plans.Step{}}
		for j := 0; j < 200; j++ {
			p.Steps = append(p.Steps, plans.Step{ID: fmt.Sprintf("step-%d", j), Title: "Step", Ref: fmt.Sprintf("card:missing-%d-%d", i, j)})
		}
		ps[fmt.Sprintf("plan-%02d", i)] = p
	}
	facts, err := s.planFacts(ctx, ps, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(facts) != maxPlanRefBudget || !planRefsTruncated(ps["plan-00"], facts) || !planRefsTruncated(ps["plan-20"], facts) {
		t.Fatalf("facts=%d; budget/truncation incorrect", len(facts))
	}
	if _, err = s.readRefFacts(ctx, make([]string, 201), nil); err == nil {
		t.Fatal("internal batch bypassed the public 200-ref guard")
	}
}

func TestSourceRefIndexBackfillsExistingMetadata(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dir := t.TempDir()
	ws, err := storage.InitializeWorkspace(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	s := NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	card, err := s.CreateWork(ctx, "actor", "", map[string]any{"title": "Before migration", "source_refs": []any{map[string]any{"authority": "generic", "connection_id": "conn", "native_id": "one", "aliases": []string{"preexisting-alias"}, "status": "done"}}})
	if err != nil {
		t.Fatal(err)
	}
	for _, sql := range []string{"DROP TRIGGER work_evidence_metadata_insert", "DROP TRIGGER work_evidence_metadata_update", "DROP TRIGGER work_evidence_metadata_delete", "DROP VIEW work_evidence_keys", "DROP VIEW work_evidence_entries", "DROP TABLE work_evidence_index", "DELETE FROM schema_migrations WHERE version=65"} {
		if _, err = ws.DB().ExecContext(ctx, sql); err != nil {
			t.Fatal(err)
		}
	}
	ws.Close()
	ws, err = storage.InitializeWorkspace(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	s = NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	rows, err := s.ResolveRefs(ctx, []string{"preexisting-alias"}, nil, time.Now(), 0)
	if err != nil || rows[0].Status != "done" {
		t.Fatalf("backfill: %v %v card=%v", rows, err, card["id"])
	}
}
func TestSourceURLLookupStaysBounded(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	s := NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	for i := 0; i < 40; i++ {
		_, err = s.CreateWork(ctx, "actor", "", map[string]any{"title": "Source", "source": map[string]any{"authority": "generic", "connection_id": "conn", "native_id": fmt.Sprintf("source-%d", i), "url": "https://source.test/shared"}})
		if err != nil {
			t.Fatal(err)
		}
	}
	checks := 0
	items, err := s.ResolveRefs(WithAccessScope(ctx, AccessScope{ActorID: "selected-pm", PMActorID: "selected-pm"}), []string{"https://source.test/shared"}, func(string, string) bool { checks++; return true }, time.Now(), 0)
	if err != nil || items[0].Resolvable || checks > 2*(2+33) {
		t.Fatalf("unbounded URL: items=%v checks=%d err=%v", items, checks, err)
	}
}

// Provider identity interpretation belongs in the publisher. A URL is a
// literal key even when its spelling resembles a different provider identity.
func TestProviderSpellingsUseOnlyPublishedKeys(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	s := NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	_, err = s.CreateWork(ctx, "actor", "", map[string]any{"title": "Generic publication", "source_refs": []any{map[string]any{"authority": "tracker", "connection_id": "conn", "native_id": "opaque-id", "aliases": []string{"team/repo#42", "github:team/repo#42", "https://github.com/team/repo/pull/42"}, "status": "done", "title": "Published tracker title"}}})
	if err != nil {
		t.Fatal(err)
	}
	refs := []string{"team/repo#42", "github:team/repo#42", "https://github.com/team/repo/pull/42", "https://github.com/team/repo/issues/42", "github:team/repo#999", "TEAM/repo#42"}
	items, err := s.ResolveRefs(ctx, refs, nil, time.Now(), 0)
	if err != nil {
		t.Fatal(err)
	}
	for i, item := range items {
		if i < 3 {
			if !item.Resolvable || item.Authority != "tracker" || item.NativeID != "opaque-id" || item.Status != "done" || item.Title != "Published tracker title" || item.Source != "evidence" || item.URL != "" {
				t.Fatalf("provider spelling overrode literal evidence: %+v", item)
			}
		} else if item.Resolvable || item.Authority != "" || item.Title != "" || item.URL != "" || item.Source != "" {
			t.Fatalf("unpublished key inferred: %+v", item)
		}
	}
}
