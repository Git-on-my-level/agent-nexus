package app

import (
	"strings"
	"testing"
)

// orient shows an agent the upkeep about its own work, the asks returned to
// it and each goal's reading; --upkeep adds everyone's suggestions and what
// other agents have waiting on a human.
func TestOrientOutcomeLines(t *testing.T) {
	brief := map[string]any{
		"principal": map[string]any{"id": "me", "display": "me", "kind": "agent"},
		"curation_suggestions": []any{
			map[string]any{"key": "mine", "action": "batch", "title": "Batch 2 related asks?", "yours": true, "apply_argv": []any{"anx", "api", "createCurationProposal"}},
			map[string]any{"key": "theirs", "action": "merge", "title": "Merge A into B?", "yours": false},
		},
		"returned_to_you": []any{map[string]any{"event_ref": "event:a1", "title": "Rerun?", "reason": "Already ran", "returned_by": map[string]any{"display": "lead on host"}, "undo_argv": []any{"anx", "ask", "retire", "event:a1", "--undo"}}},
		"open_asks":       []any{map[string]any{"event_ref": "event:b2", "title": "Cover refunds?", "requested_by": map[string]any{"display": "lead on host"}, "age_seconds": float64(7200), "subject_ref": "card:macros"}},
		"initiatives": []any{
			map[string]any{"ref": "card:churn", "state": "moving", "yours": true, "measure": map[string]any{"label": "Monthly churn", "unit": "%", "target": float64(10), "latest": float64(12.4), "previous": float64(13.1), "age_seconds": float64(4 * 86400)}},
			map[string]any{"ref": "card:nps", "state": "moving", "yours": true, "measure": map[string]any{"label": "NPS", "target": float64(50), "series_ref": "series:nps.weekly?team=a"}},
			map[string]any{"ref": "card:hosts", "state": "moving", "yours": true, "measure": map[string]any{"label": "Hosts", "unit": "hosts", "target": float64(7)}},
		},
	}
	text := renderOrientText(brief, "me")
	for _, want := range []string{
		`suggestion: mine batch "Batch 2 related asks?"`,
		"upkeep: 1 suggestions about other owners' work · anx orient --upkeep",
		`returned to you:  event:a1 "Rerun?" by lead on host: Already ran`,
		"next anx ask retire event:a1 --undo",
		"Monthly churn 12.4% → 10% (was 13.1%) · read 4d ago",
		"NPS: no reading (target 50) · publish it: anx series push nps.weekly <value>",
		"Hosts: no reading (target 7 hosts) · link a series: anx work measure card:hosts --series series:<name>",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("orient lacks %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "Merge A into B?") || strings.Contains(text, "asked of humans") {
		t.Errorf("the default view must leave other owners' upkeep out:\n%s", text)
	}
	brief["upkeep_view"] = true
	text = renderOrientText(brief, "me")
	for _, want := range []string{`suggestion: theirs merge "Merge A into B?"`, `asked of humans:  event:b2 "Cover refunds?" by lead on host · 2h · on card:macros`, "next anx ask retire <ask>"} {
		if !strings.Contains(text, want) {
			t.Errorf("orient --upkeep lacks %q:\n%s", want, text)
		}
	}
	text = renderOrientText(map[string]any{"principal": brief["principal"], "upkeep_view": true}, "me")
	for _, want := range []string{"upkeep: no open suggestions", "asked of humans:  none by other agents"} {
		if !strings.Contains(text, want) {
			t.Errorf("an empty upkeep view must say so, lacks %q:\n%s", want, text)
		}
	}
}
