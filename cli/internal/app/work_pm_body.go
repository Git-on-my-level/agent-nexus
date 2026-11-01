// Applying PM-specific request body fields to `anx work` commands: claim
// capacity, turn claim/lease token and runner id overlays.

package app

import (
	"strconv"
	"strings"

	"agent-nexus-cli/internal/config"
)

func applyPMClaimCapacityResult(result *commandResult, cap pmClaimCapacity) *commandResult {
	data := map[string]any{
		"status_code": 200,
		"headers":     map[string][]string{},
		"body":        cap.asMap(),
	}
	if result != nil {
		existing := asMap(result.Data)
		if _, ok := existing["status_code"]; ok {
			data["status_code"] = existing["status_code"]
		}
		if existing["headers"] != nil {
			data["headers"] = existing["headers"]
		}
		if body := asMap(existing["body"]); len(body) > 0 {
			data["body"] = mergePMClaimCapacityBody(body, cap)
		}
	}
	return &commandResult{Data: data, Text: formatPMClaimCapacityText(cap)}
}

func mergePMClaimCapacityBody(body map[string]any, cap pmClaimCapacity) map[string]any {
	out := cap.asMap()
	for k, v := range body {
		out[k] = v
	}
	out["claimed"] = false
	out["reason"] = "capacity"
	if _, ok := body["in_flight"]; !ok {
		out["in_flight"] = cap.InFlight
	}
	if _, ok := body["limit"]; !ok {
		out["limit"] = cap.Limit
	}
	if _, ok := body["waiting"]; !ok && body["queued"] == nil {
		out["waiting"] = cap.Waiting
	}
	return out
}

func workCommandUsesLeaseToken(name string) bool {
	return name == "pm turns context" || name == "pm turns propose"
}

func workCommandUsesRunnerID(name string) bool {
	return name == "pm turns claim"
}

func applyPMTurnClaimBody(parsed parsedWorkCommand, cfg config.Resolved, object map[string]any) {
	if object == nil {
		return
	}
	runner := firstNonEmpty(parsed.runnerID, anyString(object["runner_id"]), strings.TrimSpace(cfg.ActorID))
	if runner != "" {
		object["runner_id"] = runner
	}
}

func (a *App) pmLeaseToken(flagValue, bodyValue string) string {
	env := ""
	if a != nil && a.Getenv != nil {
		env = a.Getenv("ANX_PM_LEASE_TOKEN")
	}
	return firstNonEmpty(strings.TrimSpace(flagValue), strings.TrimSpace(env), strings.TrimSpace(bodyValue))
}

func applyPMLeaseToken(a *App, parsed parsedWorkCommand, object map[string]any) {
	if object == nil {
		return
	}
	token := a.pmLeaseToken(parsed.leaseToken, anyString(object["lease_token"]))
	if token != "" {
		object["lease_token"] = token
	}
}

func applyPMTurnContextBody(a *App, parsed parsedWorkCommand, object map[string]any) {
	applyPMLeaseToken(a, parsed, object)
	if q := parsed.query.Get("query"); q != "" {
		object["query"] = q
	}
	if c := parsed.query.Get("cursor"); c != "" {
		object["cursor"] = c
	}
	if l := parsed.query.Get("limit"); l != "" {
		n, err := strconv.Atoi(l)
		if err == nil {
			object["limit"] = n
		}
	}
}
