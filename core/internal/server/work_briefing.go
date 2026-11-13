package server

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
)

// WorkCheckpointStore is separate so existing WorkStore implementations remain compatible.
type WorkCheckpointStore interface {
	PutWorkCheckpoint(context.Context, string, string, int64, map[string]any) (map[string]any, error)
}

func handleWorkContext(w http.ResponseWriter, r *http.Request, store WorkStore, id, actor, action string, raw map[string]any) {
	if action == "checkpoint" {
		version, ok := raw["if_version"].(float64)
		if !ok || version < 0 || version > 9007199254740991 || version != float64(int64(version)) {
			writeError(w, 400, "invalid_request", "if_version must be the nonnegative checkpoint version (zero when absent)")
			return
		}
		for key := range raw {
			if key != "if_version" && key != "checkpoint" && key != "actor_id" {
				writeError(w, 400, "invalid_request", "unknown checkpoint request field")
				return
			}
		}
		content, ok := raw["checkpoint"].(map[string]any)
		if !ok {
			writeError(w, 400, "invalid_request", "checkpoint object required")
			return
		}
		checkpoints, ok := store.(WorkCheckpointStore)
		if !ok {
			writeError(w, 503, "work_unavailable", "checkpoint store is not configured")
			return
		}
		result, err := checkpoints.PutWorkCheckpoint(r.Context(), actor, id, int64(version), content)
		if err != nil {
			workStoreError(w, r, err)
			return
		}
		writeJSON(w, 200, result)
		return
	}
	work, err := store.GetWork(r.Context(), id)
	if err != nil {
		workStoreError(w, r, err)
		return
	}
	result := buildWorkBriefing(publicWork(work))
	encoded, err := json.Marshal(result)
	if err != nil || len(encoded) > 65536 {
		writeError(w, 400, "invalid_request", "briefing exceeds 65536 bytes; inspect work and checkpoint separately")
		return
	}
	writeJSON(w, 200, result)
}

// buildWorkBriefing does no I/O, persists nothing and never interprets a report
// as a verification. Section omission is explicit; history stays at its source.
func buildWorkBriefing(work map[string]any) map[string]any {
	omitted := []string{}
	remaining := 44000 // Leaves room for the 16 KiB checkpoint and envelope keys.
	pick := func(source map[string]any, keys []string, prefix string) map[string]any {
		out := map[string]any{}
		for _, key := range keys {
			value, exists := source[key]
			if !exists {
				continue
			}
			// Bound each optional field without cutting a reference or sentence in half.
			encoded, err := json.Marshal(value)
			if err != nil || len(encoded) > 8192 || len(encoded)+len(key)+8 > remaining {
				omitted = append(omitted, prefix+key)
				continue
			}
			remaining -= len(encoded) + len(key) + 8
			out[key] = value
		}
		return out
	}
	item := pick(work, []string{"ref", "title", "summary", "definition_of_done", "source", "phase", "owner", "next_actor", "next_action", "blockers", "relations", "executions", "source_refs", "version", "decision_revision", "resolution_refs"}, "work.")
	item["freshness"] = pick(asMapForBrief(work["freshness"]), []string{"status", "last_observed_at", "source_activity_at", "meaningful_progress_at", "stale_after_seconds"}, "work.freshness.")
	var observation any
	if latest, ok := work["latest_observation"].(map[string]any); ok {
		observation = pick(latest, []string{"id", "reader_id", "reader_revision", "source_revision", "source_sequence", "observed_at", "received_at", "status", "verification", "source_activity_at", "meaningful_progress_at", "evidence", "uncertainty", "coverage", "facts"}, "observation.")
	}
	refresh := pick(asMapForBrief(work["refresh"]), []string{"state", "requested_at", "last_attempt_at", "last_success_at", "next_due_at"}, "refresh.")
	// Error text is intentionally not copied: external diagnostics may contain secrets.
	var checkpoint any
	if current, ok := work["checkpoint"].(map[string]any); ok && len(current) > 0 {
		checkpoint = current
	}
	sort.Strings(omitted)
	return map[string]any{
		"schema_version": 1, "work": item, "checkpoint": checkpoint, "observation": observation, "refresh": refresh, "omitted": omitted,
		"coverage": map[string]any{"scope": "single_work", "sources": "stored_observations_only", "history": "not_loaded", "decisions": "not_loaded_use_pm_context", "checkpoint": "attributed_report_not_verified", "artifacts": "references_only_not_backups", "ownership": "unchanged", "complete": len(omitted) == 0},
	}
}
func asMapForBrief(v any) map[string]any { m, _ := v.(map[string]any); return m }
