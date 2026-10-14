package pm

import (
	"strings"
	"testing"
)

func TestProposalLookupIndexedPlan(t *testing.T) {
	_, st, _, _ := fixture(t)
	// Keep these predicates identical to both preflight and transactional
	// admission, including the independent pending-human guard.
	for _, q := range []string{proposalCandidateSQL, pendingHumanProposalSQL} {
		rows, err := st.db.Query("EXPLAIN QUERY PLAN "+q, "ws", "human", "work:missing", "work.phase")
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for rows.Next() {
			var id, parent, unused int
			var detail string
			if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
				t.Fatal(err)
			}
			t.Log(detail)
			if strings.Contains(detail, "SCAN") || strings.Contains(detail, "TEMP B-TREE") {
				t.Fatal("unbounded proposal lookup", detail)
			}
			if strings.Contains(detail, "pm_records_proposal_") {
				found = true
			}
		}
		if err := rows.Close(); err != nil {
			t.Fatal(err)
		}
		if !found {
			t.Fatal("missing proposal index")
		}
	}
}
