package importer

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Apply writes payload previews from a plan and optionally executes them via createFn.
func Apply(opts ApplyOptions, createFn CreateFunc) (ApplyReport, error) {
	if strings.TrimSpace(opts.PlanPath) == "" {
		return ApplyReport{}, fmt.Errorf("plan path is required")
	}
	if strings.TrimSpace(opts.OutDir) == "" {
		return ApplyReport{}, fmt.Errorf("out dir is required")
	}
	planPath, err := filepath.Abs(opts.PlanPath)
	if err != nil {
		return ApplyReport{}, err
	}
	outDir, err := filepath.Abs(opts.OutDir)
	if err != nil {
		return ApplyReport{}, err
	}
	if err := ensureDir(outDir); err != nil {
		return ApplyReport{}, err
	}
	var plan ImportPlan
	if err := loadJSON(planPath, &plan); err != nil {
		return ApplyReport{}, err
	}
	objects := append([]PlanObject(nil), plan.Objects...)
	sort.Slice(objects, func(i, j int) bool {
		return applyOrder(objects[i]) < applyOrder(objects[j]) || (applyOrder(objects[i]) == applyOrder(objects[j]) && objects[i].Key < objects[j].Key)
	})
	keyToRef := map[string]string{}
	results := make([]ApplyResult, 0, len(objects))
	previewDir := filepath.Join(outDir, "payloads")
	if err := ensureDir(previewDir); err != nil {
		return ApplyReport{}, err
	}
	for _, obj := range objects {
		payloadValue := substituteRefs(cloneValue(obj.Create), keyToRef)
		prunedRefs := []string{}
		if opts.Execute {
			payloadValue, prunedRefs = pruneUnresolvedRefs(payloadValue)
			if unresolved := collectUnresolvedRefs(payloadValue); len(unresolved) > 0 {
				return ApplyReport{}, fmt.Errorf("plan object %s still contains unresolved refs after pruning: %s", obj.Key, strings.Join(unresolved, ", "))
			}
		}
		payload, _ := payloadValue.(map[string]any)
		payloadPath := filepath.Join(previewDir, obj.Kind, obj.Key+".json")
		if err := writeJSON(payloadPath, payload); err != nil {
			return ApplyReport{}, err
		}
		row := ApplyResult{Key: obj.Key, Kind: obj.Kind, Payload: payloadPath, Status: "preview-only", Reason: obj.Reason}
		if len(prunedRefs) > 0 {
			row.Note = "Dropped unresolved refs during execute: " + strings.Join(prunedRefs, ", ")
		}
		if obj.Kind == "artifact" && obj.PendingBinaryUpload {
			if strings.TrimSpace(anyString(payload["content_type"])) == "structured" {
				row.Status = "pending-binary-upload"
				row.Note = joinNotes(row.Note, "Plan preserved artifact relationship metadata, but this object still needs a real binary upload path if raw bytes must be preserved.")
				results = append(results, row)
				continue
			}
		}
		if !opts.Execute {
			results = append(results, row)
			continue
		}
		if createFn == nil {
			return ApplyReport{}, fmt.Errorf("create function is required when execute=true")
		}
		response, err := createFn(obj.Kind, payload)
		if err != nil {
			return ApplyReport{}, err
		}
		row.Status = "created"
		row.Response = response
		if ref := entityRef(obj.Kind, response); strings.TrimSpace(ref) != "" {
			keyToRef[obj.Key] = ref
		}
		results = append(results, row)
	}
	report := ApplyReport{CreatedAt: utcNow(), Plan: planPath, Execute: opts.Execute, Results: results, Refs: keyToRef}
	if err := writeJSON(filepath.Join(outDir, "apply-results.json"), report); err != nil {
		return ApplyReport{}, err
	}
	if err := writeDriverScript(filepath.Join(outDir, "apply-commands.sh"), objects, previewDir); err != nil {
		return ApplyReport{}, err
	}
	return report, nil
}

func applyOrder(obj PlanObject) int {
	switch obj.Kind {
	case "thread":
		return 0
	case "artifact":
		return 1
	case "doc":
		return 2
	default:
		return 99
	}
}

func entityRef(kind string, response map[string]any) string {
	switch kind {
	case "thread":
		// Import execute uses `topics create`; API envelope is `{ topic }`. Keep `thread` for tests and older mocks.
		topic := asMap(response["topic"])
		if topic != nil {
			id := firstNonEmpty(anyString(topic["id"]), anyString(topic["topic_id"]))
			if id != "" {
				return "topic:" + id
			}
		}
		thread := asMap(response["thread"])
		if thread == nil {
			return ""
		}
		id := firstNonEmpty(anyString(thread["id"]), anyString(thread["thread_id"]))
		if id == "" {
			return ""
		}
		return "thread:" + id
	case "artifact":
		artifact := asMap(response["artifact"])
		if artifact == nil {
			return ""
		}
		id := firstNonEmpty(anyString(artifact["id"]), anyString(artifact["artifact_id"]))
		if id == "" {
			return ""
		}
		return "artifact:" + id
	case "doc":
		document := asMap(response["document"])
		if document == nil {
			return ""
		}
		return firstNonEmpty(anyString(document["id"]), anyString(document["document_id"]))
	default:
		return ""
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func writeDriverScript(path string, objects []PlanObject, previewDir string) error {
	lines := []string{
		"#!/usr/bin/env bash",
		"set -euo pipefail",
		"# Preview commands. If payloads still contain $REF: placeholders, rerun `oar import apply --execute`.",
	}
	for _, obj := range objects {
		payloadPath := filepath.Join(previewDir, obj.Kind, obj.Key+".json")
		switch obj.Kind {
		case "thread":
			lines = append(lines, fmt.Sprintf("oar --json topics create --from-file %s", shellQuote(payloadPath)))
		case "artifact":
			lines = append(lines, fmt.Sprintf("oar --json artifacts create --from-file %s", shellQuote(payloadPath)))
		case "doc":
			lines = append(lines, fmt.Sprintf("oar --json docs create --from-file %s", shellQuote(payloadPath)))
		}
	}
	if err := writeText(path, strings.Join(lines, "\n")+"\n"); err != nil {
		return err
	}
	return os.Chmod(path, 0o755)
}

func shellQuote(path string) string {
	return "'" + strings.ReplaceAll(path, "'", "'\\''") + "'"
}

func pruneUnresolvedRefs(value any) (any, []string) {
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		pruned := []string{}
		for k, v := range typed {
			if k == "refs" {
				cleaned, refs := pruneRefList(v)
				out[k] = cleaned
				pruned = append(pruned, refs...)
				continue
			}
			cleaned, refs := pruneUnresolvedRefs(v)
			out[k] = cleaned
			pruned = append(pruned, refs...)
		}
		return out, uniqueStrings(pruned)
	case []any:
		out := make([]any, len(typed))
		pruned := []string{}
		for i, item := range typed {
			cleaned, refs := pruneUnresolvedRefs(item)
			out[i] = cleaned
			pruned = append(pruned, refs...)
		}
		return out, uniqueStrings(pruned)
	case []string:
		out := make([]string, len(typed))
		copy(out, typed)
		return out, nil
	default:
		return typed, nil
	}
}

func pruneRefList(value any) (any, []string) {
	switch typed := value.(type) {
	case []string:
		out := make([]string, 0, len(typed))
		pruned := []string{}
		for _, item := range typed {
			if strings.HasPrefix(strings.TrimSpace(item), "$REF:") {
				pruned = append(pruned, strings.TrimSpace(item))
				continue
			}
			out = append(out, item)
		}
		return out, uniqueStrings(pruned)
	case []any:
		out := make([]any, 0, len(typed))
		pruned := []string{}
		for _, item := range typed {
			text, ok := item.(string)
			if ok && strings.HasPrefix(strings.TrimSpace(text), "$REF:") {
				pruned = append(pruned, strings.TrimSpace(text))
				continue
			}
			out = append(out, item)
		}
		return out, uniqueStrings(pruned)
	default:
		return value, nil
	}
}

func collectUnresolvedRefs(value any) []string {
	switch typed := value.(type) {
	case map[string]any:
		out := []string{}
		for _, item := range typed {
			out = append(out, collectUnresolvedRefs(item)...)
		}
		return uniqueStrings(out)
	case []any:
		out := []string{}
		for _, item := range typed {
			out = append(out, collectUnresolvedRefs(item)...)
		}
		return uniqueStrings(out)
	case []string:
		out := []string{}
		for _, item := range typed {
			if strings.HasPrefix(strings.TrimSpace(item), "$REF:") {
				out = append(out, strings.TrimSpace(item))
			}
		}
		return uniqueStrings(out)
	case string:
		if strings.HasPrefix(strings.TrimSpace(typed), "$REF:") {
			return []string{strings.TrimSpace(typed)}
		}
		return nil
	default:
		return nil
	}
}

func uniqueStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func joinNotes(parts ...string) string {
	filtered := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		filtered = append(filtered, part)
	}
	return strings.Join(filtered, " ")
}
