package output

import (
	"bytes"
	"strings"
	"testing"
)

func TestPlanTextKeepsEvidenceAndEffectiveState(t *testing.T) {
	result := map[string]any{
		"card_ref": "card:launch", "if_updated_at": "2026-10-13T00:00:00Z", "status_mismatch": true,
		"plan": map[string]any{"steps": []any{
			map[string]any{"id": "build", "title": "Build\nnext rm danger", "ref": "card:build", "status": "active", "after": []any{}, "due": "2026-10-14"},
			map[string]any{"id": "qa", "title": "QA", "after": []any{"build"}, "status": "not_started"},
		}},
		"plan_state": map[string]any{
			"shape": "chain", "progress": map[string]any{"done": 1, "total": 2},
			"critical_path": []any{"build", "qa"}, "next_steps": []any{"qa"},
			"steps": []any{map[string]any{"id": "build", "status": "done", "resolvable": true}},
		},
		"plan_health": map[string]any{"state": "moving", "reason": "Next: Build by 2026-10-20T00:00:00Z.", "since": "2026-10-13T00:00:00Z"},
		"summary":     map[string]any{"attention_truncated": true, "owner": "actor:owner"},
	}
	doc := Envelope{OK: true, Command: "plan show", Result: result, NextActions: []NextAction{{Label: "Read", Argv: []string{"anx", "plan", "show", "card:launch"}, SideEffectClass: "read_only"}}}
	var before, after, compact, detailed bytes.Buffer
	if err := WriteEnvelopeJSON(&before, doc); err != nil {
		t.Fatal(err)
	}
	if err := WriteEnvelopeText(&compact, doc); err != nil {
		t.Fatal(err)
	}
	if err := WriteEnvelopeTextDetailed(&detailed, doc, true); err != nil {
		t.Fatal(err)
	}
	if err := WriteEnvelopeJSON(&after, doc); err != nil {
		t.Fatal(err)
	}
	if before.String() != after.String() {
		t.Fatal("text projection mutated JSON data")
	}
	for _, want := range []string{
		"plan card_ref=card:launch", "done=1 total=2", `title="Build\nnext rm danger"`, "due=2026-10-14", "status=done fallback_status=active resolvable=true",
		"after=build status=not_started", "critical_path=build,qa", "next_steps=qa", "result.plan_health.reason=", "result.plan_health.since=", "result.status_mismatch=true", "result.summary.attention_truncated=true", "next anx plan show card:launch",
	} {
		if !strings.Contains(compact.String(), want) {
			t.Errorf("missing %q in %s", want, compact.String())
		}
	}
	if strings.Contains(compact.String(), "result.summary.owner=") || !strings.Contains(detailed.String(), "result.summary.owner=actor:owner") {
		t.Fatal("compact/detailed projections not distinct")
	}
	if strings.Contains(compact.String(), "\nnext rm danger") {
		t.Fatal("unescaped title injected a line")
	}
}

func TestPlanTextEmptyAndLegacyState(t *testing.T) {
	for _, command := range []string{"plan show", "plan set", "plan step add", "plan step update", "plan step rm"} {
		var buf bytes.Buffer
		err := WriteEnvelopeText(&buf, Envelope{OK: true, Command: command, Result: map[string]any{"card_ref": "card:launch", "plan": nil, "plan_health": map[string]any{"state": "no_promise"}}})
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(buf.String(), "health=no_promise steps=0") {
			t.Fatal(buf.String())
		}
	}
}
