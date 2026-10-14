package primitives

import (
	"context"
	"strings"
	"testing"
)

// LIMIT must precede hydration without sorting an entire relationship fan-out.
func TestTopicWorkspaceCandidateQueriesSeekOrderedIndexes(t *testing.T) {
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	for _, test := range []struct{ query, index string }{
		{`SELECT target_type,target_id FROM ref_edges WHERE source_type='topic' AND source_id=? AND edge_type='ref' ORDER BY id LIMIT 101`, "idx_ref_edges_source_page"},
		{`SELECT source_type,source_id FROM ref_edges WHERE target_type='topic' AND target_id=? AND edge_type='ref' ORDER BY id LIMIT 101`, "idx_ref_edges_target_page"},
		{`SELECT id FROM documents WHERE thread_id=? ORDER BY id LIMIT 51`, "idx_documents_thread_page"},
		{`SELECT id,board_id FROM cards WHERE parent_thread_id=? AND COALESCE(archived_at,'')='' AND COALESCE(trashed_at,'')='' ORDER BY id LIMIT 51`, "idx_cards_parent_thread_page"},
	} {
		rows, err := ws.DB().QueryContext(ctx, "EXPLAIN QUERY PLAN "+test.query, "selector")
		if err != nil {
			t.Fatal(err)
		}
		var plan strings.Builder
		for rows.Next() {
			var id, parent, unused int
			var detail string
			if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
				rows.Close()
				t.Fatal(err)
			}
			plan.WriteString(detail)
			plan.WriteByte('\n')
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(plan.String(), test.index) || strings.Contains(plan.String(), "TEMP B-TREE") || strings.Contains(plan.String(), "SCAN ") {
			t.Fatalf("unbounded candidate plan for %s:\n%s", test.query, plan.String())
		}
	}
}
