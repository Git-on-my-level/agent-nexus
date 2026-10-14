package output

import (
	"fmt"
	"strings"
)

func isPlanCommand(command string) bool {
	switch command {
	case "plan show", "plan set", "plan step add", "plan step update", "plan step rm":
		return true
	}
	return false
}

func projectPlan(lines *[]string, result any) {
	root, ok := result.(map[string]any)
	if !ok {
		project(lines, "result", result)
		return
	}
	plan, _ := root["plan"].(map[string]any)
	state, _ := root["plan_state"].(map[string]any)
	health, _ := root["plan_health"].(map[string]any)
	progress, _ := state["progress"].(map[string]any)
	status := health["state"]
	if status == nil {
		status = state["health_state"]
	}
	if status == nil {
		status = state["health"]
	}
	line := "plan"
	for _, field := range []struct {
		key   string
		value any
	}{
		{"card_ref", root["card_ref"]}, {"if_updated_at", root["if_updated_at"]},
		{"shape", state["shape"]}, {"health", status}, {"steps", len(planArray(plan["steps"]))},
		{"done", progress["done"]}, {"total", progress["total"]},
	} {
		line = appendPlanField(line, field.key, field.value)
	}
	*lines = append(*lines, line)
	// Keep the reason and its timestamp: a compact status is not sufficient evidence.
	for _, key := range []string{"reason", "since"} {
		project(lines, "result.plan_health."+key, health[key])
	}
	project(lines, "result.plan_state.last_movement_at", state["last_movement_at"])
	for _, key := range []string{"critical_path", "next_steps"} {
		if state[key] != nil {
			*lines = append(*lines, appendPlanField("plan", key, state[key]))
		}
	}
	computed := map[string]map[string]any{}
	for _, raw := range planArray(state["steps"]) {
		step, _ := raw.(map[string]any)
		if id, ok := step["id"].(string); ok {
			computed[id] = step
		}
	}
	for _, raw := range planArray(plan["steps"]) {
		step, ok := raw.(map[string]any)
		if !ok {
			project(lines, "result.plan.steps", raw)
			continue
		}
		id, _ := step["id"].(string)
		row := "step"
		for _, key := range []string{"id", "title", "ref", "after", "due"} {
			row = appendPlanField(row, key, step[key])
		}
		// Source state takes precedence; the authored status remains a fallback.
		status := computed[id]["status"]
		if status == nil {
			status = step["status"]
		}
		row = appendPlanField(row, "status", status)
		row = appendPlanField(row, "fallback_status", step["status"])
		row = appendPlanField(row, "resolvable", computed[id]["resolvable"])
		*lines = append(*lines, row)
	}
	project(lines, "result.status_mismatch", root["status_mismatch"])
	if moot, ok := root["possibly_moot"].(map[string]any); ok && moot["flagged"] == true {
		project(lines, "result.possibly_moot.reason", moot["reason"])
	}
	// A warning on the executive summary is material even though its repeated
	// status, owner and step digest are omitted from this plan-specific readback.
	if summary, ok := root["summary"].(map[string]any); ok {
		project(lines, "result.summary.attention_truncated", summary["attention_truncated"])
	}
}

func appendPlanField(line, key string, value any) string {
	if value == nil {
		return line
	}
	var text string
	switch values := value.(type) {
	case []any:
		items := make([]string, len(values))
		for i, v := range values {
			items[i] = fmt.Sprint(v)
		}
		text = strings.Join(items, ",")
	case []string:
		text = strings.Join(values, ",")
	default:
		text = fmt.Sprint(value)
	}
	return line + " " + key + "=" + quote(text)
}

func planArray(value any) []any {
	rows, _ := value.([]any)
	return rows
}
