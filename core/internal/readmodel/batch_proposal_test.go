package readmodel

import (
	"database/sql"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func batchCandidates(t *testing.T, db *sql.DB, streams []AuthorizedStream, size int) []Reference {
	t.Helper()
	q, args, err := BatchCandidatesProposal(streams, size)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := db.Query(q, args...)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []Reference
	for rows.Next() {
		var r Reference
		if err = rows.Scan(&r.Stream, &r.Candidate.Key.Sort, &r.Candidate.Key.RID, &r.Candidate.Version); err != nil {
			t.Fatal(err)
		}
		out = append(out, r)
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}
func proposalPlan(t *testing.T, db *sql.DB, q string, args []any) string {
	t.Helper()
	rows, err := db.Query("EXPLAIN QUERY PLAN "+q, args...)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var plan []string
	for rows.Next() {
		var a, b, c int
		var d string
		if err = rows.Scan(&a, &b, &c, &d); err != nil {
			t.Fatal(err)
		}
		plan = append(plan, d)
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	return strings.Join(plan, "\n")
}
func TestBatchProposalValuesContinuationAndExactSeekPlans(t *testing.T) {
	db, _, request, streams := adapterFixture(t, 2, 1)
	authorized := make([]AuthorizedStream, len(streams))
	for i, s := range streams {
		authorized[i] = AuthorizedStream{Stream: s, Generation: 1}
	}
	var emitted []int64
	for page := 0; page < 6; page++ {
		candidates := batchCandidates(t, db, authorized, 1)
		if len(candidates) < 1 || len(candidates) > 2 {
			t.Fatal(candidates)
		}
		for i, r := range candidates {
			if r.Stream < 0 || r.Stream >= len(streams) || r.Candidate.Version != 1 || i > 0 && !before(candidates[i-1].Candidate.Key, r.Candidate.Key) {
				t.Fatal(candidates)
			}
		}
		emitted = append(emitted, candidates[0].Candidate.Key.Sort)
		key := candidates[0].Candidate.Key
		authorized[candidates[0].Stream].After = &key
	}
	if !reflect.DeepEqual(emitted, []int64{0, 1, 2, 3, 4, 5}) || len(batchCandidates(t, db, authorized, 1)) != 0 {
		t.Fatal(emitted)
	}
	q, args, err := BatchCandidatesProposal(authorized, 1)
	if err != nil {
		t.Fatal(err)
	}
	plan := proposalPlan(t, db, q, args)
	if strings.Count(plan, "SEARCH scope_feed USING PRIMARY KEY") != 2 || strings.Contains(plan, "SCAN scope_feed") {
		t.Fatal(plan)
	}
	q, args, err = BatchAuthorityProposal(request.Principal, request.ScopeIDs)
	if err != nil {
		t.Fatal(err)
	}
	plan = proposalPlan(t, db, q, args)
	for _, alias := range []string{"SEARCH d ", "SEARCH m ", "SEARCH g "} {
		if !strings.Contains(plan, alias) {
			t.Fatal(plan)
		}
	}
	q, args, err = BatchBindingProposal("stranger", authorized)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := db.Query(q, args...)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for rows.Next() {
		var ordinal int
		var membership, binding sql.NullInt64
		if err = rows.Scan(&ordinal, &membership, &binding); err != nil {
			t.Fatal(err)
		}
		if membership.Valid || binding.Valid {
			t.Fatal("missing binding cannot become authorized", membership, binding)
		}
		n++
	}
	rows.Close()
	if n != 2 {
		t.Fatal(n)
	}
	q, args, err = AggregateBucketsProposal(authorized, []string{"total", "absent"})
	if err != nil {
		t.Fatal(err)
	}
	plan = proposalPlan(t, db, q, args)
	if !strings.Contains(plan, "SEARCH c USING PRIMARY KEY") || strings.Contains(plan, "SCAN c") {
		t.Fatal(plan)
	}
	rows, err = db.Query(q, args...)
	if err != nil {
		t.Fatal(err)
	}
	values := map[string]int64{}
	for rows.Next() {
		var b string
		var v int64
		if err = rows.Scan(&b, &v); err != nil {
			t.Fatal(err)
		}
		values[b] = v
	}
	err = rows.Err()
	rows.Close()
	if err != nil || !reflect.DeepEqual(values, map[string]int64{"total": 6, "absent": 0}) {
		t.Fatal(values, err)
	}
	if _, _, err = AggregateBucketsProposal(authorized, []string{"total", "total"}); !errors.Is(err, ErrProjection) {
		t.Fatal(err)
	}
}
func TestGlobalBatchRequiresSeparateDisjointIdentityCertification(t *testing.T) {
	db, _, request, streams := adapterFixture(t, 1, 2)
	if _, err := db.Exec(`DELETE FROM scope_feed;INSERT INTO scope_feed VALUES(?,1,'family-0','reader',1,1,1),(?,1,'family-0','reader',2,2,1),(?,1,'family-0','reader',3,3,1),(?,1,'family-1','reader',100,2,1)`, request.ScopeIDs[0], request.ScopeIDs[0], request.ScopeIDs[0], request.ScopeIDs[0]); err != nil {
		t.Fatal(err)
	}
	selected := []AuthorizedStream{{Stream: streams[0], Generation: 1}, {Stream: streams[1], Generation: 1}}
	var emitted []int64
	for page := 0; page < 4; page++ {
		refs := batchCandidates(t, db, selected, 1)
		if len(refs) == 0 {
			t.Fatal(refs)
		}
		r := refs[0]
		emitted = append(emitted, r.Candidate.Key.RID)
		key := r.Candidate.Key
		selected[r.Stream].After = &key
	}
	// This adversarial fixture documents why the proposal may NOT replace Read's
	// guards without a separate generation-bound disjointness certificate.
	if !reflect.DeepEqual(emitted, []int64{1, 2, 3, 2}) {
		t.Fatal(emitted)
	}
}
