package primitives_test

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	p "agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/testutil/perfguard"
)

func TestPerformanceCurationSuggestionsBudget(t *testing.T) {
	if testing.Short() || os.Getenv("ANX_PERFORMANCE_TEST") != "1" {
		t.Skip("advisory scale gate")
	}
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	s := p.NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	thread, err := s.CreateThread(ctx, "owner", map[string]any{"title": "Suggestions"})
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`WITH RECURSIVE n(i) AS(VALUES(1) UNION ALL SELECT i+1 FROM n WHERE i<51) INSERT INTO events(id,type,ts,actor_id,thread_id,refs_json,payload_json) SELECT 'suggestion-'||i,'curation_suggested','2026-10-25T00:00:00Z','owner',?,'[]',json_object('payload',json_object('title','Group delivery','curation',json_object('key','k'||i,'action','batch','targets',json_array('event:a','event:b')))) FROM n`,
		`INSERT INTO curation_proposals(key,proposal_event_id,action,source,status,targets_json,created_at) SELECT id,id,'batch','formula','open','[]',ts FROM events WHERE type='curation_suggested'`,
	} {
		var args []any
		if strings.Contains(q, "'owner',?") {
			args = []any{thread.Thread["id"]}
		}
		if _, err = ws.DB().ExecContext(ctx, q, args...); err != nil {
			t.Fatal(err)
		}
	}
	db, capture, err := perfguard.Open("file:" + ws.Layout().DatabasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	measured := p.NewTestStore(db, ws.Layout().ArtifactContentDir)
	var budget struct {
		Queries, Rows int
		VM, Scan      uint64
	}
	raw, err := os.ReadFile("testdata/curation_suggestions_budget.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(raw, &budget); err != nil {
		t.Fatal(err)
	}
	for _, size := range []int{0, 4096} {
		if size > 0 {
			if _, err = ws.DB().Exec(`WITH RECURSIVE n(i) AS(VALUES(1) UNION ALL SELECT i+1 FROM n WHERE i<4096) INSERT INTO events(id,type,ts,actor_id,refs_json,payload_json) SELECT 'absent-'||i,'curation_suggested','2026-10-24T00:00:00Z','owner','[]','{}' FROM n`); err != nil {
				t.Fatal(err)
			}
			if _, err = ws.DB().Exec(`WITH RECURSIVE n(i) AS(VALUES(1) UNION ALL SELECT i+1 FROM n WHERE i<4096) INSERT INTO curation_proposals(key,proposal_event_id,action,source,status,created_at) SELECT 'old-'||i,'absent-'||i,'batch','formula','dismissed','2026-10-24T00:00:00Z' FROM n`); err != nil {
				t.Fatal(err)
			}
		}
		scope := p.WithRequestAccessScope(ctx, p.AccessScope{ActorID: "owner"})
		if _, _, err = measured.CurationSuggestions(scope, nil); err != nil {
			t.Fatal(err)
		}
		capture.Start()
		suggestions, truncated, err := measured.CurationSuggestions(scope, nil)
		statements, queries, rows := capture.Stop()
		work := capture.Work()
		if err != nil || capture.WorkError() != nil || len(suggestions) != 50 || !truncated {
			t.Fatal(len(suggestions), truncated, err, capture.WorkError())
		}
		t.Logf("unrelated=%d queries=%d rows=%d vm=%d scans=%d", size, queries, rows, work.VMSteps, work.FullScanSteps)
		if queries > budget.Queries || rows > budget.Rows || work.VMSteps > budget.VM || work.FullScanSteps > budget.Scan {
			t.Fatalf("budget exceeded: %d %d %+v", queries, rows, work)
		}
		plan, err := perfguard.Explain(ctx, ws.DB(), statements[0])
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(strings.Join(plan, "\n"), "idx_curation_proposals_open_page") {
			t.Fatal(plan)
		}
	}
}
