package importer

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestScanDedupePlanApplyPreviewFlow(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "notes"), 0o755); err != nil {
		t.Fatalf("mkdir notes: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "repo"), 0o755); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "assets"), 0o755); err != nil {
		t.Fatalf("mkdir assets: %v", err)
	}

	note := "# Meeting Notes\n\nDecided to migrate the workspace into OAR with a hub doc and collector thread.\n"
	if err := os.WriteFile(filepath.Join(root, "notes", "meeting.md"), []byte(note), 0o644); err != nil {
		t.Fatalf("write meeting note: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "notes", "meeting-copy.md"), []byte(note), 0o644); err != nil {
		t.Fatalf("write duplicate meeting note: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "repo", "go.mod"), []byte("module example.com/repo\n\ngo 1.23.0\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "repo", "README.md"), []byte("# Repo\n\nThis repository contains importer glue logic.\n"), 0o644); err != nil {
		t.Fatalf("write readme: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "repo", "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatalf("write main.go: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "assets", "diagram.png"), []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0x00}, 0o644); err != nil {
		t.Fatalf("write png: %v", err)
	}

	outDir := filepath.Join(root, ".oar-import", "workspace")
	summary, err := Scan(ScanOptions{InputPath: root, OutDir: outDir})
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if summary.FileCount < 5 {
		t.Fatalf("expected at least five files in scan summary, got %d", summary.FileCount)
	}
	if _, err := os.Stat(summary.Inventory); err != nil {
		t.Fatalf("expected inventory file: %v", err)
	}

	report, err := Dedupe(DedupeOptions{InventoryPath: summary.Inventory, OutDir: outDir})
	if err != nil {
		t.Fatalf("dedupe: %v", err)
	}
	if len(report.RecommendedSkipIDs) == 0 {
		t.Fatalf("expected at least one exact duplicate skip recommendation")
	}

	plan, err := Plan(PlanOptions{InventoryPath: summary.Inventory, DedupePath: filepath.Join(outDir, "dedupe.json"), OutDir: outDir, SourceName: "workspace export"})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if len(plan.Objects) == 0 {
		t.Fatalf("expected planned objects")
	}
	if !hasPlannedKind(plan.Objects, "thread") {
		t.Fatalf("expected at least one collector thread in plan")
	}
	if !hasPlannedKind(plan.Objects, "doc") {
		t.Fatalf("expected at least one document in plan")
	}
	if !hasPlannedKind(plan.Objects, "artifact") {
		t.Fatalf("expected at least one artifact in plan")
	}
	if _, err := os.Stat(filepath.Join(outDir, "plan-preview.md")); err != nil {
		t.Fatalf("expected plan preview file: %v", err)
	}

	applyOut := filepath.Join(outDir, "apply")
	applyReport, err := Apply(ApplyOptions{PlanPath: filepath.Join(outDir, "plan.json"), OutDir: applyOut, Execute: false}, nil)
	if err != nil {
		t.Fatalf("apply preview: %v", err)
	}
	if len(applyReport.Results) != len(plan.Objects) {
		t.Fatalf("expected one apply result per object: got %d want %d", len(applyReport.Results), len(plan.Objects))
	}
	if _, err := os.Stat(filepath.Join(applyOut, "apply-results.json")); err != nil {
		t.Fatalf("expected apply results file: %v", err)
	}
	if _, err := os.Stat(filepath.Join(applyOut, "apply-commands.sh")); err != nil {
		t.Fatalf("expected apply driver script: %v", err)
	}
}

func TestApplyExecuteSubstitutesRefs(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	planPath := filepath.Join(root, "plan.json")
	plan := ImportPlan{
		CreatedAt:  utcNow(),
		SourceName: "test",
		Inventory:  filepath.Join(root, "inventory.jsonl"),
		Dedupe:     filepath.Join(root, "dedupe.json"),
		Objects: []PlanObject{
			{
				Key:  "thread_root",
				Kind: "thread",
				Create: map[string]any{
					"thread": map[string]any{
						"title":           "Imported root",
						"type":            "other",
						"status":          "active",
						"priority":        "p2",
						"tags":            []string{"import"},
						"cadence":         "reactive",
						"current_summary": "seed",
						"next_actions":    []string{},
						"key_artifacts":   []string{},
						"provenance": map[string]any{
							"sources": []string{"import_plan:test"},
						},
					},
				},
			},
			{
				Key:  "doc_leaf",
				Kind: "doc",
				Create: map[string]any{
					"document": map[string]any{
						"title": "Imported doc",
					},
					"content_type": "text",
					"content":      "body",
					"refs":         []string{"$REF:thread_root"},
				},
			},
		},
	}
	if err := writeJSON(planPath, plan); err != nil {
		t.Fatalf("write plan: %v", err)
	}

	callOrder := []string{}
	report, err := Apply(ApplyOptions{PlanPath: planPath, OutDir: filepath.Join(root, "apply"), Execute: true}, func(kind string, payload map[string]any) (map[string]any, error) {
		callOrder = append(callOrder, kind)
		switch kind {
		case "thread":
			return map[string]any{"thread": map[string]any{"id": "thread_123"}}, nil
		case "doc":
			refs, _ := payload["refs"].([]any)
			if len(refs) != 1 || refs[0] != "thread:thread_123" {
				t.Fatalf("expected resolved thread ref in doc payload, got %#v", payload["refs"])
			}
			return map[string]any{"document": map[string]any{"id": "doc_456"}}, nil
		default:
			t.Fatalf("unexpected kind: %s", kind)
			return nil, nil
		}
	})
	if err != nil {
		t.Fatalf("apply execute: %v", err)
	}
	if strings.Join(callOrder, ",") != "thread,doc" {
		t.Fatalf("expected dependency order thread,doc got %v", callOrder)
	}
	if report.Refs["thread_root"] != "thread:thread_123" {
		t.Fatalf("expected thread ref map to be populated, got %#v", report.Refs)
	}

	payloadPath := filepath.Join(root, "apply", "payloads", "doc", "doc_leaf.json")
	data, err := os.ReadFile(payloadPath)
	if err != nil {
		t.Fatalf("read doc payload preview: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("decode doc payload preview: %v", err)
	}
	refs, _ := decoded["refs"].([]any)
	if len(refs) != 1 || refs[0] != "thread:thread_123" {
		t.Fatalf("expected resolved ref in persisted preview payload, got %#v", decoded["refs"])
	}
}

func TestApplyExecuteDropsRefsForPendingBinaryArtifacts(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	planPath := filepath.Join(root, "plan.json")
	plan := ImportPlan{
		CreatedAt:  utcNow(),
		SourceName: "test",
		Inventory:  filepath.Join(root, "inventory.jsonl"),
		Dedupe:     filepath.Join(root, "dedupe.json"),
		Objects: []PlanObject{
			{
				Key:  "thread_root",
				Kind: "thread",
				Create: map[string]any{
					"thread": map[string]any{
						"title":           "Imported root",
						"type":            "other",
						"status":          "active",
						"priority":        "p2",
						"tags":            []string{"import"},
						"cadence":         "reactive",
						"current_summary": "seed",
						"next_actions":    []string{},
						"key_artifacts":   []string{},
						"provenance": map[string]any{
							"sources": []string{"import_plan:test"},
						},
					},
				},
			},
			{
				Key:                 "artifact_binary",
				Kind:                "artifact",
				PendingBinaryUpload: true,
				Create: map[string]any{
					"artifact": map[string]any{
						"kind":    "evidence",
						"summary": "Binary artifact",
						"refs":    []string{"$REF:thread_root"},
					},
					"content_type": "structured",
					"content": map[string]any{
						"filename": "diagram.png",
					},
				},
			},
			{
				Key:  "doc_leaf",
				Kind: "doc",
				Create: map[string]any{
					"document": map[string]any{
						"title": "Imported doc",
					},
					"content_type": "text",
					"content":      "body",
					"refs":         []string{"$REF:thread_root", "$REF:artifact_binary"},
				},
			},
		},
	}
	if err := writeJSON(planPath, plan); err != nil {
		t.Fatalf("write plan: %v", err)
	}

	callOrder := []string{}
	report, err := Apply(ApplyOptions{PlanPath: planPath, OutDir: filepath.Join(root, "apply"), Execute: true}, func(kind string, payload map[string]any) (map[string]any, error) {
		callOrder = append(callOrder, kind)
		switch kind {
		case "thread":
			return map[string]any{"thread": map[string]any{"id": "thread_123"}}, nil
		case "doc":
			refs, _ := payload["refs"].([]any)
			if len(refs) != 1 || refs[0] != "thread:thread_123" {
				t.Fatalf("expected only resolved thread ref in doc payload, got %#v", payload["refs"])
			}
			return map[string]any{"document": map[string]any{"id": "doc_456"}}, nil
		default:
			t.Fatalf("unexpected create kind: %s", kind)
			return nil, nil
		}
	})
	if err != nil {
		t.Fatalf("apply execute: %v", err)
	}
	if strings.Join(callOrder, ",") != "thread,doc" {
		t.Fatalf("expected thread,doc execution order, got %v", callOrder)
	}
	if report.Refs["artifact_binary"] != "" {
		t.Fatalf("expected skipped binary artifact to stay unresolved in ref map, got %#v", report.Refs)
	}
	var docRow *ApplyResult
	for i := range report.Results {
		if report.Results[i].Key == "doc_leaf" {
			docRow = &report.Results[i]
			break
		}
	}
	if docRow == nil || !strings.Contains(docRow.Note, "$REF:artifact_binary") {
		t.Fatalf("expected doc apply row to record dropped binary ref, got %#v", docRow)
	}
}

func TestScanPreservesDistinctRepoRootPaths(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	for _, repo := range []string{
		filepath.Join(root, "a", "service"),
		filepath.Join(root, "b", "service"),
	} {
		if err := os.MkdirAll(repo, 0o755); err != nil {
			t.Fatalf("mkdir repo: %v", err)
		}
		if err := os.WriteFile(filepath.Join(repo, "go.mod"), []byte("module example.com/service\n\ngo 1.23.0\n"), 0o644); err != nil {
			t.Fatalf("write go.mod: %v", err)
		}
		if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("# Repo\n"), 0o644); err != nil {
			t.Fatalf("write readme: %v", err)
		}
	}

	outDir := filepath.Join(root, ".oar-import", "workspace")
	summary, err := Scan(ScanOptions{InputPath: root, OutDir: outDir})
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if strings.Join(summary.RepoRoots, ",") != "a/service,b/service" {
		t.Fatalf("expected distinct relative repo roots, got %#v", summary.RepoRoots)
	}

	inventory, err := loadInventory(summary.Inventory)
	if err != nil {
		t.Fatalf("load inventory: %v", err)
	}
	seen := map[string]struct{}{}
	for _, item := range inventory {
		if item.RepoRoot != "" {
			seen[item.RepoRoot] = struct{}{}
		}
	}
	if len(seen) != 2 {
		t.Fatalf("expected two distinct repo_root values, got %#v", seen)
	}
	if _, ok := seen["a/service"]; !ok {
		t.Fatalf("expected repo_root a/service, got %#v", seen)
	}
	if _, ok := seen["b/service"]; !ok {
		t.Fatalf("expected repo_root b/service, got %#v", seen)
	}
}

func TestScanIgnoresNestedOutputDirectory(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "note.md"), []byte("# Note\n"), 0o644); err != nil {
		t.Fatalf("write source note: %v", err)
	}

	outDir := filepath.Join(root, ".oar-import", "workspace")
	if err := os.MkdirAll(filepath.Join(outDir, "stale"), 0o755); err != nil {
		t.Fatalf("mkdir stale output: %v", err)
	}
	if err := os.WriteFile(filepath.Join(outDir, "stale", "old.md"), []byte("# Old\n"), 0o644); err != nil {
		t.Fatalf("write stale output: %v", err)
	}

	summary, err := Scan(ScanOptions{InputPath: root, OutDir: outDir})
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if summary.FileCount != 1 {
		t.Fatalf("expected only source file to be scanned, got %d", summary.FileCount)
	}
	inventory, err := loadInventory(summary.Inventory)
	if err != nil {
		t.Fatalf("load inventory: %v", err)
	}
	for _, item := range inventory {
		if strings.Contains(item.RelPath, ".oar-import/") || strings.HasPrefix(item.RelPath, ".oar-import") {
			t.Fatalf("expected nested output dir to be excluded from scanned relpaths, got %#v", item)
		}
	}
}

func hasPlannedKind(objects []PlanObject, kind string) bool {
	for _, obj := range objects {
		if obj.Kind == kind {
			return true
		}
	}
	return false
}
