package app

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"organization-autorunner-cli/internal/config"
	"organization-autorunner-cli/internal/errnorm"
	"organization-autorunner-cli/internal/importer"
)

var importSubcommandSpec = subcommandSpec{
	command:  "import",
	valid:    []string{"scan", "dedupe", "plan", "apply"},
	examples: []string{"oar import", "oar import scan --input ./workspace.zip", "oar import plan --inventory ./.oar-import/workspace/inventory.jsonl"},
}

func init() {
	runtimeHelpManualDocTopics = append(runtimeHelpManualDocTopics, runtimeHelpDocTopic{
		Path:    "import",
		Kind:    "manual",
		Summary: "Prescriptive import guide for building low-duplication, discoverable OAR graphs from external material.",
	})
	localHelperTopics = append(localHelperTopics,
		localHelperTopic{
			Path:        "import scan",
			Summary:     "Scan a folder or zip archive into a normalized inventory with text cache, repo-root hints, and cluster hints.",
			JSONShape:   "`input`, `scan_root`, `extracted_root`, `inventory`, `file_count`, `counts_by_category`, `counts_by_cluster_hint`, `repo_roots`",
			Composition: "Pure local filesystem helper. Expands `.zip` inputs, ignores obvious generated junk, fingerprints files, caches readable text, and emits `inventory.jsonl` plus `scan-summary.json`.",
			Examples: []string{
				"oar import scan --input ./workspace.zip",
				"oar import scan --input ./vault --out ./.oar-import/vault",
			},
			Flags: []localHelperFlag{
				{Name: "--input <path>", Description: "Directory or `.zip` archive to scan."},
				{Name: "--out <dir>", Description: "Output directory. Defaults to `./.oar-import/<source-name>`."},
				{Name: "--max-preview-bytes <n>", Description: "Maximum bytes to keep for preview extraction."},
				{Name: "--max-text-cache-bytes <n>", Description: "Maximum text-file size cached verbatim for later doc creation."},
			},
		},
		localHelperTopic{
			Path:        "import dedupe",
			Summary:     "Create exact and probable duplicate reports from a scan inventory with conservative skip recommendations.",
			JSONShape:   "`inventory`, `exact_duplicates`, `probable_duplicates`, `recommended_skip_ids`",
			Composition: "Pure local helper. Uses normalized text hashes for readable content and raw SHA-256 for everything else; exact drops are recommended, probable duplicates are review-only.",
			Examples: []string{
				"oar import dedupe --inventory ./.oar-import/workspace/inventory.jsonl",
				"oar import dedupe ./.oar-import/workspace/inventory.jsonl --out ./.oar-import/workspace",
			},
			Flags: []localHelperFlag{
				{Name: "--inventory <path>", Description: "Inventory produced by `oar import scan`. Positional form also supported."},
				{Name: "--out <dir>", Description: "Output directory. Defaults to the inventory directory."},
			},
		},
		localHelperTopic{
			Path:        "import plan",
			Summary:     "Build a conservative import plan that prefers collector threads, hub docs, dedupe-first writes, and low orphan rates.",
			JSONShape:   "`source_name`, `inventory`, `dedupe`, `principles`, `objects`, `skipped`, `review_bundles`, `notes`",
			Composition: "Pure local helper. Classifies inventory items into docs, artifacts, repo bundles, review bundles, and collector/hub structures. It writes `plan.json` plus `plan-preview.md` without sending requests.",
			Examples: []string{
				"oar import plan --inventory ./.oar-import/workspace/inventory.jsonl",
				"oar import plan --inventory ./.oar-import/workspace/inventory.jsonl --dedupe ./.oar-import/workspace/dedupe.json --source-name 'workspace export'",
			},
			Flags: []localHelperFlag{
				{Name: "--inventory <path>", Description: "Inventory produced by `oar import scan`. Positional form also supported."},
				{Name: "--dedupe <path>", Description: "Dedupe report. Defaults to sibling `dedupe.json`."},
				{Name: "--out <dir>", Description: "Output directory. Defaults to the inventory directory."},
				{Name: "--source-name <name>", Description: "High-signal human name used in titles, tags, and provenance. Defaults from the inventory directory."},
				{Name: "--collector-threshold <n>", Description: "Minimum cluster size that triggers a collector thread."},
			},
		},
		localHelperTopic{
			Path:        "import apply",
			Summary:     "Write payload previews for a plan and optionally execute topic/artifact/doc creates in dependency order.",
			JSONShape:   "`plan`, `execute`, `results`, `refs`",
			Composition: "Local helper with optional network writes. Always writes payload previews first; when `--execute` is set it creates topics, then artifacts, then docs, substituting `$REF:<key>` placeholders after upstream IDs are known.",
			Examples: []string{
				"oar import apply --plan ./.oar-import/workspace/plan.json",
				"oar import apply --plan ./.oar-import/workspace/plan.json --execute --agent importer",
			},
			Flags: []localHelperFlag{
				{Name: "--plan <path>", Description: "Plan produced by `oar import plan`. Positional form also supported."},
				{Name: "--out <dir>", Description: "Output directory for payload previews and apply results. Defaults to `<plan-dir>/apply`."},
				{Name: "--execute", Description: "Actually call `topics create`, `artifacts create`, and `docs create`. Default is preview-only."},
			},
		},
	)
}

func (a *App) runImportCommand(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, string, error) {
	if len(args) == 0 || isHelpToken(args[0]) {
		data := importBootstrapData()
		return &commandResult{Text: importUsageText(), Data: data}, "import", nil
	}
	sub := importSubcommandSpec.normalize(args[0])
	switch sub {
	case "scan":
		result, err := a.runImportScan(args[1:])
		return result, "import scan", err
	case "dedupe":
		result, err := a.runImportDedupe(args[1:])
		return result, "import dedupe", err
	case "plan":
		result, err := a.runImportPlan(args[1:])
		return result, "import plan", err
	case "apply":
		result, err := a.runImportApply(ctx, args[1:], cfg)
		return result, "import apply", err
	default:
		return nil, "import", importSubcommandSpec.unknownError(args[0])
	}
}

func importUsageText() string {
	return strings.TrimSpace(`Import guide

Use `+"`oar import`"+` to turn external material into a clean OAR graph. The goal is not to dump files into the system. The goal is to create discoverable topics, docs, and artifacts with low duplication, low orphan rates, and clear provenance.

Object model

- `+"`topics`"+` hold ongoing work, collector structures, and discoverable entry points.
- `+"`docs`"+` hold narrative knowledge, summaries, and hub content.
- `+"`artifacts`"+` hold raw or attached evidence.
- Import should create a graph that people and agents can navigate, not just a pile of uploaded files.

Read in this order

1. `+"`oar help import`"+` — doctrine, quality bars, and the recommended loop.
2. `+"`oar help import scan`"+` — inventory and text-cache generation.
3. `+"`oar help import plan`"+` — classification, collector threads, hub docs, and review bundles.
4. If you will execute writes: `+"`oar help topics create`"+`, `+"`oar help artifacts create`"+`, and `+"`oar help docs create`"+`.
5. Optional graph/provenance reference: `+"`oar help provenance`"+`.

Operating stance

- High precision beats high recall.
- Exact duplicates should be skipped before writes.
- Ambiguous or noisy material should be skipped or deferred to review bundles.
- Imported material should usually get a discoverable entry point: a collector thread, a hub doc, or both.
- Codebases should not become one OAR object per source file.
- Binary attachments should be preserved conservatively; if reliable raw upload is not available, keep explicit pending work instead of pretending they were imported cleanly.
- Prefer preview-first planning over eager execution.

Recommended loop

1. `+"`oar import scan --input <dir-or-zip>`"+`
2. `+"`oar import dedupe --inventory ./.oar-import/<source>/inventory.jsonl`"+`
3. `+"`oar import plan --inventory ./.oar-import/<source>/inventory.jsonl`"+`
4. Review `+"`plan-preview.md`"+`, `+"`skipped`"+`, and `+"`review_bundles`"+`.
5. `+"`oar import apply --plan ./.oar-import/<source>/plan.json`"+` for payload previews.
6. `+"`oar import apply --plan ./.oar-import/<source>/plan.json --execute`"+` only after the plan looks clean.

Subcommands

  import scan      Build normalized inventory + text cache from a folder or zip
  import dedupe    Find exact duplicates and probable duplicate review clusters
  import plan      Build a conservative OAR-native import plan
  import apply     Write payload previews and optionally execute creates

Output conventions

- Default workdir is `+"`./.oar-import/<source-name>`"+`.
- `+"`scan`"+` writes `+"`inventory.jsonl`"+` and `+"`scan-summary.json`"+`.
- `+"`dedupe`"+` writes `+"`dedupe.json`"+`.
- `+"`plan`"+` writes `+"`plan.json`"+` and `+"`plan-preview.md`"+`.
- `+"`apply`"+` writes payload previews plus `+"`apply-results.json`"+` and `+"`apply-commands.sh`"+`.
`) + "\n"
}

func importBootstrapData() map[string]any {
	return map[string]any{
		"summary": "Bootstrap an agent-led import with dedupe-first planning and graph-aware OAR write conventions.",
		"read_in_order": []map[string]any{
			{"command": "oar help import", "why": "Read doctrine, quality bars, and the recommended loop."},
			{"command": "oar help import scan", "why": "Understand inventory, cluster hints, and text-cache generation."},
			{"command": "oar help import plan", "why": "Understand conservative classification, collector threads, hub docs, and review bundles."},
			{"command": "oar help topics create", "why": "Confirm topic payload shape before executing writes."},
			{"command": "oar help artifacts create", "why": "Confirm artifact payload shape before executing writes."},
			{"command": "oar help docs create", "why": "Confirm document payload shape before executing writes."},
		},
		"principles": map[string]any{
			"precision_over_recall":             true,
			"dedupe_before_writes":              true,
			"skip_or_review_ambiguous_material": true,
			"prefer_discoverable_graphs":        true,
			"codebases_not_file_by_file":        true,
			"preview_first":                     true,
		},
		"recommended_loop": []map[string]any{
			{"step": 1, "command": "oar import scan --input <dir-or-zip>"},
			{"step": 2, "command": "oar import dedupe --inventory ./.oar-import/<source>/inventory.jsonl"},
			{"step": 3, "command": "oar import plan --inventory ./.oar-import/<source>/inventory.jsonl"},
			{"step": 4, "command": "Review plan-preview.md, skipped, and review_bundles"},
			{"step": 5, "command": "oar import apply --plan ./.oar-import/<source>/plan.json"},
			{"step": 6, "command": "oar import apply --plan ./.oar-import/<source>/plan.json --execute"},
		},
		"subcommands": []string{"scan", "dedupe", "plan", "apply"},
	}
}

func (a *App) runImportScan(args []string) (*commandResult, error) {
	fs := newSilentFlagSet("import scan")
	var inputFlag trackedString
	var outFlag trackedString
	var previewBytes trackedInt
	var textCacheBytes trackedInt
	fs.Var(&inputFlag, "input", "Directory or .zip archive to scan")
	fs.Var(&outFlag, "out", "Output directory")
	fs.Var(&previewBytes, "max-preview-bytes", "Maximum preview bytes per text file")
	fs.Var(&textCacheBytes, "max-text-cache-bytes", "Maximum text-file size cached verbatim")
	if err := fs.Parse(args); err != nil {
		return nil, errnorm.Usage("invalid_flags", err.Error())
	}
	positionals := append([]string(nil), fs.Args()...)
	inputPath := strings.TrimSpace(inputFlag.value)
	if inputPath == "" && len(positionals) > 0 {
		inputPath = strings.TrimSpace(positionals[0])
		positionals = positionals[1:]
	}
	if len(positionals) > 0 {
		return nil, errnorm.Usage("invalid_args", "unexpected positional arguments for `oar import scan`")
	}
	if inputPath == "" {
		return nil, errnorm.Usage("invalid_request", "--input is required")
	}
	outDir := strings.TrimSpace(outFlag.value)
	if outDir == "" {
		outDir = defaultImportWorkdir(inputPath)
	}
	opts := importer.ScanOptions{InputPath: inputPath, OutDir: outDir}
	if previewBytes.set {
		opts.MaxPreviewBytes = previewBytes.value
	}
	if textCacheBytes.set {
		opts.MaxTextCacheBytes = int64(textCacheBytes.value)
	}
	summary, err := importer.Scan(opts)
	if err != nil {
		return nil, errnorm.Wrap(errnorm.KindLocal, "import_scan_failed", "import scan failed", err)
	}
	text := formatImportScanText(summary)
	data := map[string]any{
		"input":                  summary.Input,
		"scan_root":              summary.ScanRoot,
		"extracted_root":         summary.ExtractedRoot,
		"inventory":              summary.Inventory,
		"file_count":             summary.FileCount,
		"repo_roots":             summary.RepoRoots,
		"counts_by_category":     summary.CountsByCategory,
		"counts_by_cluster_hint": summary.CountsByClusterHint,
		"next": []string{
			fmt.Sprintf("oar import dedupe --inventory %s", summary.Inventory),
			fmt.Sprintf("oar import plan --inventory %s", summary.Inventory),
		},
	}
	return &commandResult{Text: text, Data: data}, nil
}

func (a *App) runImportDedupe(args []string) (*commandResult, error) {
	fs := newSilentFlagSet("import dedupe")
	var inventoryFlag trackedString
	var outFlag trackedString
	fs.Var(&inventoryFlag, "inventory", "Inventory produced by import scan")
	fs.Var(&outFlag, "out", "Output directory")
	if err := fs.Parse(args); err != nil {
		return nil, errnorm.Usage("invalid_flags", err.Error())
	}
	positionals := append([]string(nil), fs.Args()...)
	inventoryPath := strings.TrimSpace(inventoryFlag.value)
	if inventoryPath == "" && len(positionals) > 0 {
		inventoryPath = strings.TrimSpace(positionals[0])
		positionals = positionals[1:]
	}
	if len(positionals) > 0 {
		return nil, errnorm.Usage("invalid_args", "unexpected positional arguments for `oar import dedupe`")
	}
	if inventoryPath == "" {
		return nil, errnorm.Usage("invalid_request", "--inventory is required")
	}
	outDir := strings.TrimSpace(outFlag.value)
	if outDir == "" {
		outDir = filepath.Dir(inventoryPath)
	}
	report, err := importer.Dedupe(importer.DedupeOptions{InventoryPath: inventoryPath, OutDir: outDir})
	if err != nil {
		return nil, errnorm.Wrap(errnorm.KindLocal, "import_dedupe_failed", "import dedupe failed", err)
	}
	text := formatImportDedupeText(report, outDir)
	data := map[string]any{
		"inventory":                 report.Inventory,
		"exact_duplicates":          report.ExactDuplicates,
		"probable_duplicates":       report.ProbableDuplicates,
		"recommended_skip_ids":      report.RecommendedSkipIDs,
		"dedupe_report":             filepath.Join(outDir, "dedupe.json"),
		"next":                      []string{fmt.Sprintf("oar import plan --inventory %s --dedupe %s", report.Inventory, filepath.Join(outDir, "dedupe.json"))},
		"exact_duplicate_groups":    len(report.ExactDuplicates),
		"probable_duplicate_groups": len(report.ProbableDuplicates),
	}
	return &commandResult{Text: text, Data: data}, nil
}

func (a *App) runImportPlan(args []string) (*commandResult, error) {
	fs := newSilentFlagSet("import plan")
	var inventoryFlag trackedString
	var dedupeFlag trackedString
	var outFlag trackedString
	var sourceNameFlag trackedString
	var collectorThreshold trackedInt
	fs.Var(&inventoryFlag, "inventory", "Inventory produced by import scan")
	fs.Var(&dedupeFlag, "dedupe", "Dedupe report path")
	fs.Var(&outFlag, "out", "Output directory")
	fs.Var(&sourceNameFlag, "source-name", "High-signal source name used in titles and tags")
	fs.Var(&collectorThreshold, "collector-threshold", "Minimum cluster size that triggers a collector thread")
	if err := fs.Parse(args); err != nil {
		return nil, errnorm.Usage("invalid_flags", err.Error())
	}
	positionals := append([]string(nil), fs.Args()...)
	inventoryPath := strings.TrimSpace(inventoryFlag.value)
	if inventoryPath == "" && len(positionals) > 0 {
		inventoryPath = strings.TrimSpace(positionals[0])
		positionals = positionals[1:]
	}
	if len(positionals) > 0 {
		return nil, errnorm.Usage("invalid_args", "unexpected positional arguments for `oar import plan`")
	}
	if inventoryPath == "" {
		return nil, errnorm.Usage("invalid_request", "--inventory is required")
	}
	outDir := strings.TrimSpace(outFlag.value)
	if outDir == "" {
		outDir = filepath.Dir(inventoryPath)
	}
	dedupePath := strings.TrimSpace(dedupeFlag.value)
	if dedupePath == "" {
		dedupePath = filepath.Join(filepath.Dir(inventoryPath), "dedupe.json")
	}
	opts := importer.PlanOptions{InventoryPath: inventoryPath, DedupePath: dedupePath, OutDir: outDir, SourceName: strings.TrimSpace(sourceNameFlag.value)}
	if collectorThreshold.set {
		opts.CollectorThreshold = collectorThreshold.value
	}
	plan, err := importer.Plan(opts)
	if err != nil {
		return nil, errnorm.Wrap(errnorm.KindLocal, "import_plan_failed", "import plan failed", err)
	}
	text := formatImportPlanText(plan, outDir)
	data := map[string]any{
		"source_name":          plan.SourceName,
		"inventory":            plan.Inventory,
		"dedupe":               plan.Dedupe,
		"plan_path":            filepath.Join(outDir, "plan.json"),
		"plan_preview":         filepath.Join(outDir, "plan-preview.md"),
		"objects":              plan.Objects,
		"skipped":              plan.Skipped,
		"review_bundles":       plan.ReviewBundles,
		"planned_object_count": len(plan.Objects),
		"skipped_count":        len(plan.Skipped),
		"review_bundle_count":  len(plan.ReviewBundles),
		"next": []string{
			fmt.Sprintf("oar import apply --plan %s", filepath.Join(outDir, "plan.json")),
			fmt.Sprintf("oar import apply --plan %s --execute", filepath.Join(outDir, "plan.json")),
		},
	}
	return &commandResult{Text: text, Data: data}, nil
}

func (a *App) runImportApply(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, error) {
	fs := newSilentFlagSet("import apply")
	var planFlag trackedString
	var outFlag trackedString
	var executeFlag trackedBool
	fs.Var(&planFlag, "plan", "Plan produced by import plan")
	fs.Var(&outFlag, "out", "Output directory")
	fs.Var(&executeFlag, "execute", "Execute creates instead of writing previews only")
	if err := fs.Parse(args); err != nil {
		return nil, errnorm.Usage("invalid_flags", err.Error())
	}
	positionals := append([]string(nil), fs.Args()...)
	planPath := strings.TrimSpace(planFlag.value)
	if planPath == "" && len(positionals) > 0 {
		planPath = strings.TrimSpace(positionals[0])
		positionals = positionals[1:]
	}
	if len(positionals) > 0 {
		return nil, errnorm.Usage("invalid_args", "unexpected positional arguments for `oar import apply`")
	}
	if planPath == "" {
		return nil, errnorm.Usage("invalid_request", "--plan is required")
	}
	outDir := strings.TrimSpace(outFlag.value)
	if outDir == "" {
		outDir = filepath.Join(filepath.Dir(planPath), "apply")
	}
	execute := executeFlag.set && executeFlag.value
	createFn := importer.CreateFunc(nil)
	if execute {
		createFn = func(kind string, payload map[string]any) (map[string]any, error) {
			var result *commandResult
			var err error
			switch kind {
			case "thread":
				result, err = a.invokeTypedJSON(ctx, cfg, "topics create", "topics.create", nil, nil, payload)
			case "artifact":
				result, err = a.invokeTypedJSON(ctx, cfg, "artifacts create", "artifacts.create", nil, nil, payload)
			case "doc":
				result, err = a.invokeTypedJSON(ctx, cfg, "docs create", "docs.create", nil, nil, payload)
			default:
				return nil, errnorm.Usage("invalid_request", fmt.Sprintf("unsupported import object kind %q", kind))
			}
			if err != nil {
				return nil, err
			}
			return extractCommandBody(result), nil
		}
	}
	report, err := importer.Apply(importer.ApplyOptions{PlanPath: planPath, OutDir: outDir, Execute: execute}, createFn)
	if err != nil {
		return nil, errnorm.Wrap(errnorm.KindLocal, "import_apply_failed", "import apply failed", err)
	}
	text := formatImportApplyText(report, outDir)
	data := map[string]any{
		"plan":                        report.Plan,
		"execute":                     report.Execute,
		"results":                     report.Results,
		"refs":                        report.Refs,
		"apply_results":               filepath.Join(outDir, "apply-results.json"),
		"apply_commands":              filepath.Join(outDir, "apply-commands.sh"),
		"payload_dir":                 filepath.Join(outDir, "payloads"),
		"created_count":               countApplyStatus(report.Results, "created"),
		"preview_only_count":          countApplyStatus(report.Results, "preview-only"),
		"pending_binary_upload_count": countApplyStatus(report.Results, "pending-binary-upload"),
	}
	return &commandResult{Text: text, Data: data}, nil
}

func defaultImportWorkdir(inputPath string) string {
	base := filepath.Base(strings.TrimSpace(inputPath))
	base = strings.TrimSuffix(base, filepath.Ext(base))
	base = strings.TrimSpace(base)
	if base == "" {
		base = "import"
	}
	return filepath.Join(".oar-import", slugifyImport(base))
}

func slugifyImport(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	cleaned := make([]rune, 0, len(value))
	lastDash := false
	for _, r := range value {
		isAlphaNum := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if isAlphaNum {
			cleaned = append(cleaned, r)
			lastDash = false
			continue
		}
		if !lastDash {
			cleaned = append(cleaned, '-')
			lastDash = true
		}
	}
	result := strings.Trim(string(cleaned), "-")
	if result == "" {
		return "import"
	}
	return result
}

func extractCommandBody(result *commandResult) map[string]any {
	if result == nil {
		return nil
	}
	data, _ := result.Data.(map[string]any)
	if data == nil {
		return nil
	}
	body, _ := data["body"].(map[string]any)
	if body != nil {
		return body
	}
	flattened, _ := flattenEnvelopeData(result.Data, false).(map[string]any)
	return flattened
}

func countApplyStatus(results []importer.ApplyResult, status string) int {
	count := 0
	for _, result := range results {
		if result.Status == status {
			count++
		}
	}
	return count
}

func formatImportScanText(summary importer.ScanSummary) string {
	var lines []string
	lines = append(lines, "Import scan complete")
	lines = append(lines, "")
	lines = append(lines, "Input: "+summary.Input)
	lines = append(lines, "Scan root: "+summary.ScanRoot)
	if strings.TrimSpace(summary.ExtractedRoot) != "" {
		lines = append(lines, "Extracted root: "+summary.ExtractedRoot)
	}
	lines = append(lines, fmt.Sprintf("Files: %d", summary.FileCount))
	lines = append(lines, "Inventory: "+summary.Inventory)
	if len(summary.RepoRoots) > 0 {
		lines = append(lines, "Repo roots: "+strings.Join(summary.RepoRoots, ", "))
	}
	if len(summary.CountsByCategory) > 0 {
		lines = append(lines, "By category: "+formatCountMap(summary.CountsByCategory))
	}
	lines = append(lines, "", "Next:")
	lines = append(lines, "  oar import dedupe --inventory "+summary.Inventory)
	lines = append(lines, "  oar import plan --inventory "+summary.Inventory)
	return strings.Join(lines, "\n")
}

func formatImportDedupeText(report importer.DedupeReport, outDir string) string {
	lines := []string{
		"Import dedupe complete",
		"",
		"Inventory: " + report.Inventory,
		fmt.Sprintf("Exact duplicate groups: %d", len(report.ExactDuplicates)),
		fmt.Sprintf("Probable duplicate groups: %d", len(report.ProbableDuplicates)),
		fmt.Sprintf("Recommended exact skips: %d", len(report.RecommendedSkipIDs)),
		"Report: " + filepath.Join(outDir, "dedupe.json"),
		"",
		"Next:",
		"  oar import plan --inventory " + report.Inventory + " --dedupe " + filepath.Join(outDir, "dedupe.json"),
	}
	return strings.Join(lines, "\n")
}

func formatImportPlanText(plan importer.ImportPlan, outDir string) string {
	lines := []string{
		"Import plan complete",
		"",
		"Source: " + plan.SourceName,
		"Inventory: " + plan.Inventory,
		"Dedupe: " + plan.Dedupe,
		fmt.Sprintf("Planned objects: %d", len(plan.Objects)),
		fmt.Sprintf("Skipped items: %d", len(plan.Skipped)),
		fmt.Sprintf("Review bundles: %d", len(plan.ReviewBundles)),
		"Plan: " + filepath.Join(outDir, "plan.json"),
		"Preview: " + filepath.Join(outDir, "plan-preview.md"),
		"",
		"Next:",
		"  oar import apply --plan " + filepath.Join(outDir, "plan.json"),
		"  oar import apply --plan " + filepath.Join(outDir, "plan.json") + " --execute",
	}
	return strings.Join(lines, "\n")
}

func formatImportApplyText(report importer.ApplyReport, outDir string) string {
	lines := []string{
		"Import apply complete",
		"",
		"Plan: " + report.Plan,
		fmt.Sprintf("Execute: %t", report.Execute),
		fmt.Sprintf("Created: %d", countApplyStatus(report.Results, "created")),
		fmt.Sprintf("Preview-only: %d", countApplyStatus(report.Results, "preview-only")),
		fmt.Sprintf("Pending binary upload: %d", countApplyStatus(report.Results, "pending-binary-upload")),
		"Results: " + filepath.Join(outDir, "apply-results.json"),
		"Payloads: " + filepath.Join(outDir, "payloads"),
		"Driver script: " + filepath.Join(outDir, "apply-commands.sh"),
	}
	if len(report.Refs) > 0 {
		keys := make([]string, 0, len(report.Refs))
		for key := range report.Refs {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		refs := make([]string, 0, len(keys))
		for _, key := range keys {
			refs = append(refs, key+"="+report.Refs[key])
		}
		lines = append(lines, "Created refs: "+strings.Join(refs, ", "))
	}
	return strings.Join(lines, "\n")
}

func formatCountMap(counts map[string]int) string {
	keys := make([]string, 0, len(counts))
	for key := range counts {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%s=%d", key, counts[key]))
	}
	return strings.Join(parts, ", ")
}
