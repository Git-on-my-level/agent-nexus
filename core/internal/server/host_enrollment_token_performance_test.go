package server

import (
	"context"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"agent-nexus-core/internal/testutil/perfguard"
)

// The route budget cannot say what matters about this read.
//
// `GET /auth/hosts/enrollment-tokens/{token_id}` exists so a surface watching
// its own grant does not read the workspace's whole token history on every
// tick. The gate's row count includes authentication, so budgeting it at one
// row fails legitimate requests — it is budgeted like its sibling admin reads
// instead, and the claim that actually needs defending is pinned here: one
// statement against the token table, by primary key, with a cost that does not
// move when the workspace holds thousands of tokens.
func TestPerformanceHostEnrollmentTokenLookup(t *testing.T) {
	requirePerformanceTest(t)
	env := newPerformanceEnv(t)

	// Enough history that a list read would be obvious in the numbers.
	const tokens = 4096
	tx, err := env.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	stmt, err := tx.Prepare(`INSERT INTO host_enrollment_tokens(id,label,token_hash,created_at,expires_at,created_by_agent_id) VALUES(?,'scale',?,?,?,'scale-owner')`)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < tokens; i++ {
		if _, err = stmt.Exec(fmt.Sprintf("htok-noise-%d", i), fmt.Sprintf("hash-noise-%d", i), perfguard.Timestamp, "2030-01-01T00:00:00Z"); err != nil {
			t.Fatal(err)
		}
	}
	stmt.Close()
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}

	var budget routeBudget
	for _, b := range performanceBudgets(t) {
		if b.Path == "/auth/hosts/enrollment-tokens/{token_id}" && b.Method == "GET" {
			budget = b
		}
	}
	if budget.MaxQueries == 0 {
		t.Fatal("missing enrollment token lookup budget")
	}

	handler, capture, _, closePool := env.fresh(t)
	defer closePool()

	/*
	 * One unmeasured request first.
	 *
	 * The first authenticated read in a process builds the canonical ownership
	 * denial closure, which costs hundreds of thousands of VM steps on this
	 * fixture and belongs to SCA-663, not to this route — the sibling admin
	 * reads carry first_read baselines for exactly that. The route gate warms
	 * it incidentally by running other routes first; this test is on its own,
	 * so it warms it deliberately rather than measuring someone else's cost or
	 * taking a baseline exception a new read is not allowed.
	 */
	warm := httptest.NewRequest("GET", "/auth/hosts/enrollment-tokens/scale-target-token", nil)
	warm.Header.Set("Authorization", "Bearer "+env.principals[0].AccessToken)
	warmRecorder := httptest.NewRecorder()
	handler.ServeHTTP(warmRecorder, warm)
	if warmRecorder.Code != 200 {
		t.Fatalf("warm-up: %d %s", warmRecorder.Code, warmRecorder.Body.String())
	}

	for sample := 0; sample < 6; sample++ {
		req := httptest.NewRequest("GET", "/auth/hosts/enrollment-tokens/scale-target-token", nil)
		req.Header.Set("Authorization", "Bearer "+env.principals[0].AccessToken)
		w := httptest.NewRecorder()
		capture.Start()
		start := time.Now()
		handler.ServeHTTP(w, req)
		elapsed := time.Since(start)
		statements, queries, rows := capture.Stop()
		work := capture.Work()
		t.Logf("sample=%d queries=%d rows=%d vm=%d elapsed=%s", sample, queries, rows, work.VMSteps, elapsed)
		if err := capture.WorkError(); err != nil {
			t.Fatal(err)
		}
		if w.Code != 200 || !strings.Contains(w.Body.String(), `"scale-target-token"`) {
			t.Fatalf("token lookup: %d %s", w.Code, w.Body.String())
		}
		// The secret is never readable, so it can never be returned.
		if strings.Contains(w.Body.String(), "synthetic-token-hash") {
			t.Fatal("token hash reached the response")
		}
		if queries > budget.MaxQueries || rows > budget.MaxRows || work.VMSteps > budget.MaxVMSteps || elapsed > time.Duration(budget.LatencyMS)*time.Millisecond {
			for _, statement := range statements {
				t.Logf("SQL: %s", statement.SQL)
			}
			t.Fatal("enrollment token lookup exceeds its budget")
		}

		// The claim the budget cannot make: one read of the token table, keyed.
		reads := 0
		for _, statement := range statements {
			if !strings.Contains(statement.SQL, "host_enrollment_tokens") {
				continue
			}
			reads++
			if !strings.Contains(statement.SQL, "WHERE id=?") {
				t.Fatalf("token read is not keyed by id: %s", statement.SQL)
			}
			plan, err := perfguard.Explain(context.Background(), env.db, statement)
			if err != nil {
				t.Fatal(err)
			}
			/*
			 * The plan also contains the shared ownership-denial closure, which
			 * wraps every authorized read; what belongs to this route is the
			 * single primary-key search on the token table. SQLite names it by
			 * the query's alias, so the index is what identifies it.
			 */
			joined := strings.Join(plan, "\n")
			if strings.Contains(joined, "SCAN host_enrollment_tokens") {
				t.Fatalf("token lookup scans the table: %+v", plan)
			}
			if !strings.Contains(joined, "USING INDEX sqlite_autoindex_host_enrollment_tokens_1 (id=?)") {
				t.Fatalf("token lookup lacks an indexed point plan: %+v", plan)
			}
		}
		if reads != 1 {
			t.Fatalf("expected exactly one token-table read, got %d", reads)
		}
	}
}
