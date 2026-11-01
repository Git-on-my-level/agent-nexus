// Default text projections and help text for `anx work` commands: work list
// lines, last-error rendering, and pagination.

package app

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"agent-nexus-cli/internal/registry"
)

func workHelpText(topic string) (string, bool) {
	if topic == "pm card" {
		return "Usage: anx pm card <card-ref>\nRead any visible card in full inside a claimed PM turn.", true
	}
	if topic == "pm propose" {
		return "Usage: anx pm propose <card-ref> (--status <backlog|ready|in_progress|blocked|review|done> | --note <text>) --why <reason> [--evidence <ref> ...]\nProposes for human approval in Inbox. done requires evidence. Identity and conversation come from the runner.", true
	}
	if topic == "pm context" {
		meta, _ := registry.LoadEmbedded()
		var adjacent []string
		if cmd, found := commandByCLIPath(meta.Commands, mapRuntimePathToRegistryPath(topic)); found {
			for _, item := range cmd.Adjacent {
				adjacent = append(adjacent, runtimePathFromRegistryPath(commandIDToCLIPath(item)))
			}
		}
		if len(adjacent) == 0 {
			adjacent = []string{"pm card", "pm propose"}
		}
		return fmt.Sprintf("Usage: anx pm context\n\nInside a claimed turn, run with no flags. Open a card with `anx pm card <card-ref>`; propose with `anx pm propose`.\n\nAdjacent commands: `%s`\n\nOutside a claimed turn\nLegacy filters: `--work-ref`, `--query`, `--limit`, `--cursor`. Pass `next_cursor` unchanged as `--cursor`; cursors are bound to the workspace, principal and record kind. Context limits are 1..50.\n\nUse `--json` for one machine-readable envelope.\n", strings.Join(adjacent, "`, `")), true
	}

	if isInternalWorkCommand(topic) {
		return "", false
	}
	spec, exact := workCommands[topic]
	if !exact && !isWorkCommandGroup(topic) {
		return "", false
	}
	var b strings.Builder
	meta, _ := registry.LoadEmbedded()
	if cmd, found := commandByCLIPath(meta.Commands, mapRuntimePathToRegistryPath(topic)); found && exact {
		b.WriteString(formatGeneratedCommandHelp(topic, cmd, false))
		b.WriteString("\n\n")
	} else {
		fmt.Fprintf(&b, "Local Help: %s\n\n", topic)
		if exact {
			fmt.Fprintf(&b, "Side effect class: %s\n\n", commandSideEffectClass(topic))
		}
	}
	b.WriteString("Work is an existing card; projects are topics. Scope and identity come from the resolved host agent. No local tracker database.\n\n")
	if exact {
		fmt.Fprintf(&b, "%s\n\nUsage: anx %s", spec.summary, topic)
		if topic == "work create" {
			b.WriteString(" [--title <text> | --from-file <path|->] [--handle <slug>] [--under <ref|path> | --initiative] [--request-key <key>]")
		}
		if spec.idFlag != "" {
			fmt.Fprintf(&b, " <ref> (or --%s <ref>)", spec.idFlag)
		}
		if spec.body {
			b.WriteString(" --from-file <path|->")
		}
		if spec.optionalBody {
			b.WriteString(" [--from-file <path|->]")
		}
		if workCommandUsesRunnerID(topic) {
			b.WriteString(" [--runner-id <id>]")
		}
		if workCommandUsesLeaseToken(topic) {
			b.WriteString(" [--lease-token <token>]")
		}
		b.WriteString("\n")
		for _, f := range spec.filters {
			fmt.Fprintf(&b, "  --%s <value>\n", f)
		}
		if workCommandUsesRunnerID(topic) {
			b.WriteString("  --runner-id <id> (defaults to the authenticated actor id; claims are idempotent for the same runner_id)\n")
		}
		if workCommandUsesLeaseToken(topic) {
			b.WriteString("  --lease-token <token> (or ANX_PM_LEASE_TOKEN from `anx pm serve`)\n")
		}
		if spec.body || spec.optionalBody {
			b.WriteString("\nJSON body follows the central API contract; use anx meta commands for generated schemas. Server validates scope, versions and evidence.\n")
		}
		if topic == "work observations submit" {
			b.WriteString("Body: {\"observation\":{\"idempotency_key\":\"stable-report-key\",\"reader_id\":\"reader\",\"reader_revision\":\"v1\",\"observed_at\":\"RFC3339 timestamp\",\"status\":\"reported\",\"facts\":{},\"evidence\":[]}}\nPreserve source_sequence and idempotency_key on retry; received_at and actor_id are server-owned. Remote verified labels remain claims.\n")
		}
	} else {
		names := []string{}
		for name := range workCommands {
			if strings.HasPrefix(name, topic+" ") {
				names = append(names, name)
			}
		}
		sort.Strings(names)
		for _, name := range names {
			fmt.Fprintf(&b, "  anx %-24s %s\n", name, workCommands[name].summary)
		}
	}
	if strings.HasPrefix(topic, "pm") {
		b.WriteString("\nPM lists accept --limit 1..200 and opaque --cursor; preserve next_cursor and has_more. Cursors are bound to the current workspace, principal and record kind. Context limits are 1..50. An answered decision is not proof of delivery or execution; inspect pm actions get. Agent keys cannot inherit human approval authority.\n")
	} else {
		b.WriteString("\nLists preserve next_cursor; pass it unchanged with --cursor. Reading does not refresh or mutate sources.\n")
	}
	b.WriteString("\nUse --json for one machine-readable envelope.\n")
	return b.String(), true
}

func formatWorkCommandText(name string, body any) string {
	root := asMap(body)
	if name == "sessions register" || name == "sessions get" {
		session := asMap(root["session"])
		return fmt.Sprintf("session id=%s provider=%s activity=%s sequence=%v last_seen_at=%s", anyString(session["session_id"]), anyString(session["provider"]), anyString(session["activity"]), session["sequence"], anyString(session["last_seen_at"]))
	}
	if name == "work participants register" {
		return formatParticipantLine(asMap(root["participant"]))
	}
	if name == "work participants list" {
		rows := asSlice(root["participants"])
		lines := []string{fmt.Sprintf("participants count=%d", len(rows))}
		for _, row := range rows {
			lines = append(lines, formatParticipantLine(asMap(row)))
		}
		return strings.Join(appendPaginationLines(lines, root), "\n")
	}
	if name == "work list" {
		rows, _ := root["work"].([]any)
		lines := []string{fmt.Sprintf("Work: %d", len(rows))}
		for _, row := range rows {
			lines = append(lines, renderWorkLine(asMap(row)))
		}
		if cursor := anyString(root["next_cursor"]); cursor != "" {
			lines = append(lines, "next_cursor: "+cursor)
		}
		return strings.Join(lines, "\n")
	}
	if name == "work observations list" {
		rows, _ := root["observations"].([]any)
		lines := []string{fmt.Sprintf("Observations: %d", len(rows))}
		for _, row := range rows {
			observation := asMap(row)
			lines = append(lines, fmt.Sprintf("%s  status=%s verification=%s observed=%s reader=%s", anyString(observation["id"]), anyString(observation["status"]), firstNonEmpty(anyString(observation["verification"]), "reported"), anyString(observation["observed_at"]), anyString(observation["reader_id"])))
			for _, key := range []string{"uncertainty", "coverage", "error"} {
				if value, ok := observation[key]; ok && value != nil {
					raw, _ := json.Marshal(value)
					lines = append(lines, "  "+key+": "+string(raw))
				}
			}
		}
		if cursor := anyString(root["next_cursor"]); cursor != "" {
			lines = append(lines, "next_cursor: "+cursor)
		}
		return strings.Join(lines, "\n")
	}
	if name == "pm bindings list" {
		rows, _ := root["items"].([]any)
		lines := []string{fmt.Sprintf("bindings: %d", len(rows))}
		for _, row := range rows {
			item := asMap(row)
			origin := asMap(item["origin"])
			line := fmt.Sprintf("%s  %s %s/%s user=%s -> %s", anyString(item["id"]), anyString(origin["transport"]), anyString(origin["tenant_id"]), anyString(origin["channel_id"]), anyString(origin["external_user_id"]), anyString(item["actor_id"]))
			if thread := anyString(origin["thread_id"]); thread != "" {
				line += " thread=" + thread
			}
			line += fmt.Sprintf(" can_approve=%t enabled=%t revision=%s", item["can_approve"] == true, item["enabled"] == true, fmt.Sprintf("%v", item["revision"]))
			lines = append(lines, line)
		}
		return strings.Join(appendPaginationLines(lines, root), "\n")
	}
	if name == "pm context" || name == "pm turns context" {
		rows, _ := root["items"].([]any)
		lines := []string{fmt.Sprintf("context: %d", len(rows))}
		for _, row := range rows {
			item := asMap(row)
			line := firstNonEmpty(anyString(item["id"]), anyString(item["ref"]), "item")
			if workRef := anyString(item["work_ref"]); workRef != "" {
				line += "  " + workRef
			}
			if status := anyString(item["status"]); status != "" {
				line += "  status=" + status
			}
			if title := firstNonEmpty(anyString(item["title"]), anyString(item["instruction"]), anyString(item["text"])); title != "" {
				line += "  " + strings.Join(strings.Fields(title), " ")
			}
			lines = append(lines, line)
		}
		if limitations, ok := root["limitations"].([]any); ok && len(limitations) > 0 {
			encoded, _ := json.Marshal(limitations)
			lines = append(lines, "limitations: "+string(encoded))
		}
		return strings.Join(appendPaginationLines(lines, root), "\n")
	}
	if name == "pm conversations list" {
		rows, _ := root["items"].([]any)
		lines := []string{fmt.Sprintf("conversations: %d", len(rows))}
		for _, row := range rows {
			item := asMap(row)
			line := fmt.Sprintf("%s  %s", anyString(item["id"]), anyString(item["work_ref"]))
			if status := conversationListStatus(item); status != "" {
				line += "  status=" + status
			}
			if title := firstNonEmpty(anyString(item["title"]), anyString(item["instruction"])); title != "" {
				line += "  " + strings.Join(strings.Fields(title), " ")
			}
			lines = append(lines, line)
		}
		return strings.Join(appendPaginationLines(lines, root), "\n")
	}
	if name == "pm decisions list" || name == "pm actions list" {
		rows, _ := root["items"].([]any)
		lines := []string{fmt.Sprintf("%s: %d", strings.TrimPrefix(strings.TrimSuffix(name, " list"), "pm "), len(rows))}
		for _, row := range rows {
			item := asMap(row)
			line := fmt.Sprintf("%s  %s  status=%s", anyString(item["id"]), anyString(item["work_ref"]), firstNonEmpty(anyString(item["status"]), "unknown"))
			if name == "pm decisions list" {
				if flag := decisionOperatorFlags(item); flag != "" {
					line += "  " + strings.ReplaceAll(flag, "\n", "  ")
				}
			}
			if title := firstNonEmpty(anyString(item["title"]), anyString(item["instruction"])); title != "" {
				line += "  " + strings.Join(strings.Fields(title), " ")
			}
			if name == "pm actions list" {
				receipt := asMap(item["receipt"])
				line += fmt.Sprintf(" verified=%t", receipt["independently_verified"] == true)
			}
			lines = append(lines, line)
		}
		return strings.Join(appendPaginationLines(lines, root), "\n")
	}
	if name == "pm decisions get" {
		return formatPMDecisionGetText(root)
	}
	if name == "pm turns get" {
		return formatPMTurnGetText(root)
	}
	if name == "pm turns claim" {
		return formatPMTurnClaimText(root)
	}
	if name == "pm decisions dispatch" {
		return formatPMDispatchText(root, time.Time{}, "")
	}
	if name == "pm actions acknowledge" {
		return formatPMActionAcknowledgeText(root)
	}
	if name == "pm actions reconcile" {
		return formatPMActionReconcileText(root)
	}
	if name == "work get" || name == "work create" || name == "work patch" {
		work := asMap(root["work"])
		lines := []string{renderWorkLine(work)}
		if health := asMap(work["plan_health"]); anyString(health["state"]) != "" {
			line := "plan_health: " + anyString(health["state"])
			if reason := anyString(health["reason"]); reason != "" {
				line += " — " + reason
			}
			lines = append(lines, line)
		}
		for _, key := range []string{"project_ref", "next_actor", "next_action", "wake_condition"} {
			if value := anyString(work[key]); value != "" {
				lines = append(lines, key+": "+value)
			}
		}
		if moot := asMap(work["possibly_moot"]); moot["flagged"] == true {
			lines = append(lines, "possibly_moot: "+anyString(moot["reason"]))
		}
		// Keep evidence and acceptance visible; a phase is not a verification claim.
		for _, key := range []string{"source", "freshness", "definition_of_done", "blockers", "relations", "executions", "latest_observation", "refresh"} {
			if value, ok := work[key]; ok && value != nil {
				encoded, _ := json.Marshal(value)
				lines = append(lines, key+": "+string(encoded))
			}
		}
		return strings.Join(lines, "\n")
	}
	return formatPrettyBody(body)
}

func formatParticipantLine(participant map[string]any) string {
	return fmt.Sprintf("participant id=%s agent_id=%s activity=%s sequence=%v last_seen_at=%s", anyString(participant["participant_id"]), anyString(participant["agent_id"]), anyString(participant["activity"]), participant["sequence"], anyString(participant["last_seen_at"]))
}

func renderWorkLine(work map[string]any) string {
	line := fmt.Sprintf("%s  %s  phase=%s source=%s freshness=%s", firstNonEmpty(anyString(work["ref"]), anyString(work["handle"]), anyString(work["id"])), anyString(work["title"]), firstNonEmpty(anyString(work["phase"]), "unknown"), firstNonEmpty(anyString(asMap(work["source"])["authority"]), "unknown"), firstNonEmpty(anyString(asMap(work["freshness"])["status"]), "unknown"))
	// Dark and the possibly-moot flag ride the same health computation the
	// server attaches to every work read; a quiet card must not read as one
	// that is simply fine.
	if state := anyString(asMap(work["plan_health"])["state"]); state != "" {
		line += " health=" + state
	}
	moot := asMap(work["possibly_moot"])
	if moot["flagged"] == true {
		line += "  possibly_moot: " + anyString(moot["reason"])
	}
	if errText := renderWorkLastError(work); errText != "" {
		line += "  " + errText
	}
	return line
}

func renderWorkLastError(work map[string]any) string {
	for _, src := range []map[string]any{asMap(work["freshness"]), asMap(work["refresh"])} {
		errObj := asMap(src["last_error"])
		code := anyString(errObj["code"])
		msg := anyString(errObj["message"])
		switch {
		case code != "" && msg != "":
			return "last_error=" + code + ": " + msg
		case code != "":
			return "last_error=" + code
		case msg != "":
			return "last_error=" + msg
		}
		if raw := anyString(src["last_error"]); raw != "" {
			return "last_error=" + raw
		}
	}
	return ""
}

func appendPaginationLines(lines []string, root map[string]any) []string {
	if more, _ := root["has_more"].(bool); more {
		lines = append(lines, "has_more: true")
	}
	if cursor := anyString(root["next_cursor"]); cursor != "" {
		lines = append(lines, "next_cursor: "+cursor)
	}
	return lines
}
