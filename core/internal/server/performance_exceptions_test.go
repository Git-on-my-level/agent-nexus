package server

import (
	"agent-nexus-core/internal/testutil/perfguard"
	"encoding/hex"
	"encoding/json"
	urlpkg "net/url"
	"os"
	"strings"
	"testing"
)

type baselineBudget struct {
	Method     string `json:"method"`
	Path       string `json:"path"`
	Principal  string `json:"principal"`
	P95MS      int    `json:"p95_ms"`
	MaxQueries int    `json:"max_queries"`
	MaxRows    int    `json:"max_rows"`
	Issue      string `json:"issue"`
	IssueURL   string `json:"issue_url"`
	Reason     string `json:"reason"`
}

// Existing main hazards remain finite, per-route/principal baselines. New routes
// always inherit the standard budget. A baseline does not bypass privacy, status,
// stream sampling or the exact-shape plan gate.
func performanceBaselineBudgets(t *testing.T, routes []routeBudget) map[string]baselineBudget {
	t.Helper()
	raw, err := os.ReadFile("testdata/performance_budget_allowlist.json")
	if err != nil {
		t.Fatal(err)
	}
	var entries []baselineBudget
	if err = json.Unmarshal(raw, &entries); err != nil {
		t.Fatal(err)
	}
	out := map[string]baselineBudget{}
	registered := map[string]bool{}
	for _, b := range routes {
		registered[b.Method+" "+b.Path] = true
	}
	for _, e := range entries {
		key := e.Method + " " + e.Path + " " + e.Principal
		if !registered[e.Method+" "+e.Path] || (e.Principal != "authorized" && e.Principal != "unauthorized") || e.P95MS <= 0 || e.P95MS > 1800000 || e.MaxQueries <= 0 || e.MaxQueries > 100000 || e.MaxRows <= 0 || e.MaxRows > 250000 || !reviewedPerformanceException(e.Issue, e.IssueURL, e.Reason) || out[key].Path != "" {
			t.Fatalf("invalid, duplicate or stale performance baseline %s", key)
		}
		out[key] = e
	}
	return out
}

func reviewedPerformanceException(issue, link, reason string) bool {
	// Accept a portable tracker link without coupling the core to one tracker.
	u, err := urlpkg.Parse(link)
	return err == nil && u.IsAbs() && u.Host != "" && u.Path != "" && strings.TrimSpace(issue) != "" && len(strings.TrimSpace(reason)) >= 40
}

func performancePlanExceptions(t *testing.T) map[string]bool {
	t.Helper()
	raw, err := os.ReadFile("testdata/performance_plan_allowlist.json")
	if err != nil {
		t.Fatal(err)
	}
	var entries []perfguard.PlanException
	if err = json.Unmarshal(raw, &entries); err != nil {
		t.Fatal(err)
	}
	allowed := map[string]bool{}
	for _, e := range entries {
		_, err := hex.DecodeString(e.SQLHash)
		_, planErr := hex.DecodeString(e.PlanHash)
		if err != nil || len(e.SQLHash) != 64 || planErr != nil || len(e.PlanHash) != 64 || len(e.Findings) == 0 || !reviewedPerformanceException(e.Issue, e.IssueURL, e.Reason) {
			t.Fatal("plan exception needs an exact shape hash, finding and linked P1 justification")
		}
		for _, finding := range e.Findings {
			key := e.SQLHash + "\n" + e.PlanHash + "\n" + finding
			if finding == "" || allowed[key] {
				t.Fatal("empty or duplicate exact plan finding")
			}
			allowed[key] = true
		}
	}
	return allowed
}

func TestPerformanceExceptionInventory(t *testing.T) {
	performanceBaselineBudgets(t, performanceBudgets(t))
	performancePlanExceptions(t)
}
