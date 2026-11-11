package app

import (
	"testing"

	"agent-nexus-cli/internal/errnorm"
)

// A write saved unattributed stays visible when a later step of the same
// command fails: the error carries the skip instead of dropping it.
func TestAttributionSkipSurvivesALaterFailure(t *testing.T) {
	skips := &attributionSkips{}
	if err := skips.annotateError(errnorm.Local("boom", "later step failed")); asMap(errnorm.Normalize(err).Details)["run_attribution_skipped"] != nil {
		t.Fatalf("no skip, no annotation: %v", err)
	}
	skips.observe(map[string]any{"run": "agentctl/exec-1", "message": "run belongs to another agent", "reason": "run_owned_by_other_agent"})
	if w := skips.warnings(); len(w) != 1 || w[0].Code != "run_attribution_skipped" {
		t.Fatalf("warnings %v", w)
	}
	err := skips.annotateError(errnorm.Local("boom", "later step failed"))
	e := errnorm.Normalize(err)
	skipped := asMap(asMap(e.Details)["run_attribution_skipped"])
	if e.Code != "boom" || skipped["message"] == "" || asMap(skipped["details"])["reason"] != "run_owned_by_other_agent" {
		t.Fatalf("error must keep the skipped attribution: %#v", e)
	}
}
