package importer

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Plan builds a conservative import plan from inventory and dedupe reports.
func Plan(opts PlanOptions) (ImportPlan, error) {
	if strings.TrimSpace(opts.InventoryPath) == "" {
		return ImportPlan{}, fmt.Errorf("inventory path is required")
	}
	if strings.TrimSpace(opts.DedupePath) == "" {
		return ImportPlan{}, fmt.Errorf("dedupe path is required")
	}
	if strings.TrimSpace(opts.OutDir) == "" {
		return ImportPlan{}, fmt.Errorf("out dir is required")
	}
	if opts.CollectorThreshold <= 0 {
		opts.CollectorThreshold = 4
	}
	inventoryPath, err := filepath.Abs(opts.InventoryPath)
	if err != nil {
		return ImportPlan{}, err
	}
	dedupePath, err := filepath.Abs(opts.DedupePath)
	if err != nil {
		return ImportPlan{}, err
	}
	outDir, err := filepath.Abs(opts.OutDir)
	if err != nil {
		return ImportPlan{}, err
	}
	if err := ensureDir(outDir); err != nil {
		return ImportPlan{}, err
	}
	items, err := loadInventory(inventoryPath)
	if err != nil {
		return ImportPlan{}, err
	}
	var dedupe DedupeReport
	if err := loadJSON(dedupePath, &dedupe); err != nil {
		return ImportPlan{}, err
	}
	sourceName := strings.TrimSpace(opts.SourceName)
	if sourceName == "" {
		sourceName = deriveSourceName(inventoryPath)
	}
	skipIDs := map[string]struct{}{}
	for _, sourceID := range dedupe.RecommendedSkipIDs {
		skipIDs[sourceID] = struct{}{}
	}
	clusters := map[string][]InventoryRecord{}
	for _, item := range items {
		if _, skip := skipIDs[item.SourceID]; skip {
			continue
		}
		cluster := item.ClusterHint
		if strings.TrimSpace(cluster) == "" {
			cluster = "root"
		}
		clusters[cluster] = append(clusters[cluster], item)
	}
	plan := ImportPlan{
		CreatedAt:  utcNow(),
		SourceName: sourceName,
		Inventory:  inventoryPath,
		Dedupe:     dedupePath,
		Principles: map[string]any{
			"precision_over_recall":                     true,
			"skip_ambiguous_low_value_material":         true,
			"collector_threads_for_nontrivial_clusters": true,
			"hub_docs_for_discoverability":              true,
			"codebases_not_file_by_file":                true,
		},
		Objects:       []PlanObject{},
		Skipped:       []SkippedItem{},
		ReviewBundles: []ReviewBundle{},
		Notes: []string{
			"Docs are durable authored leaves; threads and artifacts form the strongest explicit ref backbone.",
			"Binary attachments are planned conservatively unless the environment supports a reliable upload path.",
		},
	}
	itemsByID := map[string]InventoryRecord{}
	for _, item := range items {
		itemsByID[item.SourceID] = item
	}
	for _, skippedID := range dedupe.RecommendedSkipIDs {
		if item, ok := itemsByID[skippedID]; ok {
			plan.Skipped = append(plan.Skipped, SkippedItem{SourceID: skippedID, RelPath: item.RelPath, Reason: "exact-duplicate"})
		}
	}
	clusterNames := make([]string, 0, len(clusters))
	for clusterName := range clusters {
		clusterNames = append(clusterNames, clusterName)
	}
	sort.Strings(clusterNames)
	for _, clusterName := range clusterNames {
		members := append([]InventoryRecord(nil), clusters[clusterName]...)
		sort.Slice(members, func(i, j int) bool { return members[i].SourceID < members[j].SourceID })
		clusterObjects := []PlanObject{}
		collectorThreadKey := ""
		needCollector := len(members) >= opts.CollectorThreshold
		if !needCollector {
			for _, member := range members {
				if member.Category == "image" || member.Category == "pdf" || member.Category == "office" || member.Category == "slack-json" || member.Category == "trello-json" {
					needCollector = true
					break
				}
			}
		}
		if needCollector {
			threadObj := buildCollectorThread(clusterName, members, sourceName)
			collectorThreadKey = threadObj.Key
			clusterObjects = append(clusterObjects, threadObj)
		}
		docKeys := []string{}
		artifactKeys := []string{}
		reviewIDs := []string{}
		repoMembers := map[string][]InventoryRecord{}
		for _, item := range members {
			label, reason, confidence := classify(item)
			switch label {
			case "skip":
				plan.Skipped = append(plan.Skipped, SkippedItem{SourceID: item.SourceID, RelPath: item.RelPath, Reason: reason})
			case "review_bundle":
				reviewIDs = append(reviewIDs, item.SourceID)
				plan.ReviewBundles = append(plan.ReviewBundles, ReviewBundle{SourceID: item.SourceID, RelPath: item.RelPath, Category: item.Category, Reason: reason, Confidence: confidence, ClusterHint: clusterName})
			case "code_member":
				repoKey := item.RepoRoot
				if repoKey == "" {
					repoKey = clusterName
				}
				repoMembers[repoKey] = append(repoMembers[repoKey], item)
			case "doc":
				obj := buildDoc(item, sourceName, collectorThreadKey)
				docKeys = append(docKeys, obj.Key)
				clusterObjects = append(clusterObjects, obj)
			case "artifact":
				obj := buildArtifact(item, sourceName, collectorThreadKey)
				artifactKeys = append(artifactKeys, obj.Key)
				clusterObjects = append(clusterObjects, obj)
			}
		}
		repoKeys := make([]string, 0, len(repoMembers))
		for repoKey := range repoMembers {
			repoKeys = append(repoKeys, repoKey)
		}
		sort.Strings(repoKeys)
		for _, repoKey := range repoKeys {
			repoObjs := buildRepoBundle("repo:"+repoKey, repoMembers[repoKey], sourceName, collectorThreadKey)
			for _, obj := range repoObjs {
				clusterObjects = append(clusterObjects, obj)
				if obj.Kind == "doc" {
					docKeys = append(docKeys, obj.Key)
				} else if obj.Kind == "artifact" {
					artifactKeys = append(artifactKeys, obj.Key)
				}
			}
		}
		if len(docKeys) > 0 || len(artifactKeys) > 0 || len(reviewIDs) > 0 {
			hubDoc := buildHubDoc(clusterName, docKeys, artifactKeys, reviewIDs, sourceName, collectorThreadKey)
			clusterObjects = append(clusterObjects, hubDoc)
		}
		plan.Objects = append(plan.Objects, clusterObjects...)
	}
	if err := writeJSON(filepath.Join(outDir, "plan.json"), plan); err != nil {
		return ImportPlan{}, err
	}
	previewLines := []string{
		fmt.Sprintf("# Import plan — %s", sourceName),
		"",
		fmt.Sprintf("Created: %s", plan.CreatedAt),
		"",
		"## Summary",
		"",
		fmt.Sprintf("- Planned objects: %d", len(plan.Objects)),
		fmt.Sprintf("- Skipped items: %d", len(plan.Skipped)),
		fmt.Sprintf("- Review bundles: %d", len(plan.ReviewBundles)),
		"",
		"## Planned objects",
		"",
	}
	for _, obj := range plan.Objects {
		previewLines = append(previewLines, fmt.Sprintf("- `%s` `%s` — %s", obj.Kind, obj.Key, obj.Reason))
	}
	previewLines = append(previewLines, "", "## Skipped", "")
	for _, item := range plan.Skipped {
		previewLines = append(previewLines, fmt.Sprintf("- `%s` `%s` — %s", item.SourceID, item.RelPath, item.Reason))
	}
	previewLines = append(previewLines, "", "## Review bundles", "")
	for _, item := range plan.ReviewBundles {
		previewLines = append(previewLines, fmt.Sprintf("- `%s` `%s` — %s (confidence %.2f)", item.SourceID, item.RelPath, item.Reason, item.Confidence))
	}
	if err := writeText(filepath.Join(outDir, "plan-preview.md"), strings.Join(previewLines, "\n")+"\n"); err != nil {
		return ImportPlan{}, err
	}
	return plan, nil
}

func deriveSourceName(inventoryPath string) string {
	base := filepath.Base(filepath.Dir(inventoryPath))
	if strings.TrimSpace(base) == "" || base == "." || base == string(filepath.Separator) {
		base = filepath.Base(inventoryPath)
	}
	base = strings.TrimSuffix(base, filepath.Ext(base))
	if strings.TrimSpace(base) == "" {
		return "import"
	}
	return base
}

func writeText(path string, content string) error {
	if err := ensureDir(filepath.Dir(path)); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o644)
}

func isHighValueRepoText(item InventoryRecord) bool {
	rel := strings.ToLower(item.RelPath)
	filename := strings.ToLower(item.Filename)
	if strings.Contains(rel, "/docs/generated/") {
		return false
	}
	directNames := map[string]struct{}{"readme.md": {}, "readme": {}, "agents.md": {}, "changelog.md": {}, "contributing.md": {}, "license": {}, "license.md": {}}
	if _, ok := directNames[filename]; ok {
		return true
	}
	keywords := []string{"adr", "rfc", "spec", "design", "architecture", "runbook", "guide", "manual", "proposal", "plan", "brief", "roadmap"}
	for _, keyword := range keywords {
		if strings.Contains(rel, keyword) {
			return true
		}
	}
	return strings.Contains(rel, "/docs/") && !strings.Contains(rel, "/docs/generated/")
}

func isLowValueGenerated(item InventoryRecord) bool {
	rel := strings.ToLower(item.RelPath)
	if strings.Contains(rel, "node_modules/") || strings.Contains(rel, "dist/") || strings.Contains(rel, "build/") || strings.Contains(rel, "coverage/") || strings.Contains(rel, "__pycache__/") {
		return true
	}
	if strings.HasSuffix(rel, "package-lock.json") || strings.HasSuffix(rel, "pnpm-lock.yaml") || strings.HasSuffix(rel, "yarn.lock") {
		return true
	}
	return strings.HasSuffix(rel, ".min.js") || strings.HasSuffix(rel, ".min.css")
}

func classify(item InventoryRecord) (label string, reason string, confidence float64) {
	category := item.Category
	sizeBytes := item.SizeBytes
	wordCount := item.WordCount
	if isLowValueGenerated(item) {
		return "skip", "generated-or-lockfile", 0.99
	}
	if category == "slack-json" || category == "trello-json" {
		return "review_bundle", "complex-" + category, 0.90
	}
	if category == "code" {
		return "code_member", "code-file", 0.95
	}
	if category == "image" || category == "pdf" || category == "office" || category == "binary" || category == "archive" {
		return "artifact", "immutable-" + category, 0.95
	}
	if category == "json" || category == "config" || category == "table" {
		if sizeBytes > 1_500_000 {
			return "artifact", "large-structured-" + category, 0.80
		}
		return "review_bundle", "needs-semantic-review-" + category, 0.70
	}
	if category == "markdown" || category == "text" || category == "html" {
		if wordCount < 20 && sizeBytes < 500 {
			return "skip", "too-small-or-fragmentary", 0.75
		}
		if item.RepoRoot != "" && !isHighValueRepoText(item) {
			return "review_bundle", "repo-text-needs-curation", 0.78
		}
		return "doc", "durable-" + category, 0.90
	}
	return "review_bundle", "unclassified", 0.60
}

func chooseThreadType(clusterName string, members []InventoryRecord) string {
	parts := []string{strings.ToLower(clusterName)}
	for _, member := range firstN(members, 5) {
		parts = append(parts, strings.ToLower(member.TitleHint))
	}
	text := strings.Join(parts, " ")
	switch {
	case strings.Contains(text, "incident") || strings.Contains(text, "outage") || strings.Contains(text, "sev") || strings.Contains(text, "bugfix"):
		return "incident"
	case strings.Contains(text, "project") || strings.Contains(text, "initiative") || strings.Contains(text, "roadmap") || strings.Contains(text, "launch") || strings.Contains(text, "plan"):
		return "initiative"
	case strings.Contains(text, "process") || strings.Contains(text, "runbook") || strings.Contains(text, "workflow") || strings.Contains(text, "ops"):
		return "process"
	case strings.Contains(text, "customer") || strings.Contains(text, "partner") || strings.Contains(text, "vendor") || strings.Contains(text, "relationship"):
		return "relationship"
	default:
		return "other"
	}
}

func buildCollectorThread(clusterName string, members []InventoryRecord, sourceName string) PlanObject {
	tagCandidates := []string{"import", slugify(sourceName)}
	firstPart := clusterName
	if idx := strings.Index(clusterName, ":"); idx >= 0 {
		firstPart = clusterName[idx+1:]
	}
	if firstPart != "" && firstPart != "root" {
		tagCandidates = append(tagCandidates, slugify(firstPart))
	}
	tags := uniqueSortedStrings(tagCandidates)
	return PlanObject{
		Key:        "thread_" + slugify(clusterName),
		Kind:       "thread",
		SourceIDs:  collectSourceIDs(members),
		Confidence: 0.92,
		Reason:     "collector-thread-for-discoverability",
		Create: map[string]any{
			"thread": map[string]any{
				"title":           fmt.Sprintf("Collector thread — %s", clusterName),
				"type":            chooseThreadType(clusterName, members),
				"status":          "active",
				"priority":        "p2",
				"tags":            tags,
				"cadence":         "reactive",
				"current_summary": fmt.Sprintf("Imported material cluster for %s. Use this thread as the context anchor for related docs and artifacts.", clusterName),
				"next_actions": []string{
					"review imported materials",
					"promote high-value docs and artifacts",
					"defer ambiguous or noisy material",
				},
				"key_artifacts": []string{},
				"provenance": map[string]any{
					"sources": []string{"import_plan:" + slugify(sourceName)},
					"notes":   "Collector thread created by oar import.",
				},
			},
		},
	}
}

func buildDoc(item InventoryRecord, sourceName string, collectorThreadKey string) PlanObject {
	refs := []string{}
	if collectorThreadKey != "" {
		refs = append(refs, "$REF:"+collectorThreadKey)
	}
	return PlanObject{
		Key:        "doc_" + slugify(item.SourceID+"-"+item.TitleHint),
		Kind:       "doc",
		SourceIDs:  []string{item.SourceID},
		Confidence: 0.90,
		Reason:     "durable-authored-content",
		Create: map[string]any{
			"document": map[string]any{
				"title":           item.TitleHint,
				"tags":            uniqueSortedStrings([]string{"import", slugify(sourceName), slugify(orDefault(item.ClusterHint, "root"))}),
				"source_relpath":  item.RelPath,
				"source_category": item.Category,
				"provenance": map[string]any{
					"sources": []string{"import_file:" + item.SourceID},
					"notes":   item.RelPath,
				},
			},
			"content_type": "text",
			"content":      loadTextContent(item),
			"refs":         refs,
		},
	}
}

func buildArtifact(item InventoryRecord, sourceName string, collectorThreadKey string) PlanObject {
	refs := []string{}
	if collectorThreadKey != "" {
		refs = append(refs, "$REF:"+collectorThreadKey)
	}
	content := map[string]any{
		"source_id":   item.SourceID,
		"relpath":     item.RelPath,
		"filename":    item.Filename,
		"size_bytes":  item.SizeBytes,
		"sha256":      item.SHA256,
		"mime_guess":  item.MIMEGuess,
		"import_note": "Structured artifact stub. Replace with real binary or text upload if the environment supports it.",
	}
	contentType := "structured"
	if item.IsText && strings.TrimSpace(item.Preview) != "" {
		contentType = "text"
		content = map[string]any{"text": loadTextContent(item)}
	}
	create := map[string]any{
		"artifact": map[string]any{
			"kind":            "evidence",
			"summary":         fmt.Sprintf("Imported %s — %s", item.Category, item.Filename),
			"refs":            refs,
			"source_relpath":  item.RelPath,
			"source_category": item.Category,
		},
		"content_type": contentType,
	}
	if contentType == "text" {
		create["content"] = loadTextContent(item)
	} else {
		create["content"] = content
	}
	return PlanObject{
		Key:                 "artifact_" + slugify(item.SourceID+"-"+item.Filename),
		Kind:                "artifact",
		SourceIDs:           []string{item.SourceID},
		Confidence:          0.85,
		Reason:              "immutable-" + item.Category,
		Create:              create,
		PendingBinaryUpload: !item.IsText,
	}
}

func buildRepoBundle(clusterName string, members []InventoryRecord, sourceName string, collectorThreadKey string) []PlanObject {
	repoName := clusterName
	if idx := strings.Index(clusterName, ":"); idx >= 0 {
		repoName = clusterName[idx+1:]
	}
	refs := []string{}
	if collectorThreadKey != "" {
		refs = append(refs, "$REF:"+collectorThreadKey)
	}
	topExtensions := countTopExtensions(members)
	manifest := map[string]any{
		"repo_name":      repoName,
		"member_count":   len(members),
		"top_extensions": topExtensions,
		"sample_paths":   collectRelPaths(firstN(members, 40)),
		"import_note":    "Codebase imported conservatively as a repo bundle, not file-by-file.",
	}
	artifact := PlanObject{
		Key:        "artifact_repo_manifest_" + slugify(repoName),
		Kind:       "artifact",
		SourceIDs:  collectSourceIDs(members),
		Confidence: 0.95,
		Reason:     "repo-manifest",
		Create: map[string]any{
			"artifact": map[string]any{
				"kind":            "evidence",
				"summary":         fmt.Sprintf("Repo manifest — %s", repoName),
				"refs":            refs,
				"source_category": "codebase",
				"source_repo":     repoName,
			},
			"content_type": "structured",
			"content":      manifest,
		},
	}
	doc := PlanObject{
		Key:        "doc_repo_index_" + slugify(repoName),
		Kind:       "doc",
		SourceIDs:  collectSourceIDs(members),
		Confidence: 0.90,
		Reason:     "repo-index-doc",
		Create: map[string]any{
			"document": map[string]any{
				"title":           fmt.Sprintf("Repo index — %s", repoName),
				"tags":            uniqueSortedStrings([]string{"import", slugify(sourceName), "codebase", slugify(repoName)}),
				"source_category": "codebase",
				"provenance": map[string]any{
					"sources": []string{"import_bundle:" + slugify(repoName)},
					"notes":   fmt.Sprintf("Repo bundle imported from %s", repoName),
				},
			},
			"content_type": "structured",
			"content": map[string]any{
				"summary":               fmt.Sprintf("Conservative repo import for %s", repoName),
				"repo_name":             repoName,
				"member_count":          len(members),
				"recommended_next_step": "Have an agent author a higher-level repo overview before importing individual code files.",
				"manifest_artifact_key": artifact.Key,
				"sample_paths":          collectRelPaths(firstN(members, 20)),
			},
			"refs": append(append([]string{}, refs...), "$REF:"+artifact.Key),
		},
	}
	return []PlanObject{artifact, doc}
}

func buildHubDoc(clusterName string, docKeys []string, artifactKeys []string, reviewIDs []string, sourceName string, collectorThreadKey string) PlanObject {
	refs := []string{}
	if collectorThreadKey != "" {
		refs = append(refs, "$REF:"+collectorThreadKey)
	}
	for _, key := range artifactKeys {
		refs = append(refs, "$REF:"+key)
	}
	return PlanObject{
		Key:        "doc_hub_" + slugify(clusterName),
		Kind:       "doc",
		SourceIDs:  []string{},
		Confidence: 0.95,
		Reason:     "hub-index-for-discoverability",
		Create: map[string]any{
			"document": map[string]any{
				"title":           fmt.Sprintf("Import index — %s", clusterName),
				"tags":            uniqueSortedStrings([]string{"import", "index", slugify(sourceName), slugify(clusterName)}),
				"source_category": "import-index",
				"provenance": map[string]any{
					"sources": []string{"import_plan:" + slugify(clusterName)},
					"notes":   fmt.Sprintf("Hub doc for %s", clusterName),
				},
			},
			"content_type": "structured",
			"content": map[string]any{
				"cluster":                  clusterName,
				"child_doc_keys":           docKeys,
				"child_artifact_keys":      artifactKeys,
				"review_bundle_source_ids": reviewIDs,
				"import_note":              "Hub doc created to reduce orphaning and improve discoverability.",
			},
			"refs": refs,
		},
	}
}

func uniqueSortedStrings(values []string) []string {
	set := map[string]struct{}{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		set[value] = struct{}{}
	}
	out := make([]string, 0, len(set))
	for value := range set {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func collectSourceIDs(items []InventoryRecord) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, item.SourceID)
	}
	return out
}

func collectRelPaths(items []InventoryRecord) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, item.RelPath)
	}
	return out
}

func countTopExtensions(items []InventoryRecord) []any {
	counts := map[string]int{}
	for _, item := range items {
		counts[item.Extension]++
	}
	type pair struct {
		Key   string
		Count int
	}
	pairs := make([]pair, 0, len(counts))
	for key, count := range counts {
		pairs = append(pairs, pair{Key: key, Count: count})
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].Count != pairs[j].Count {
			return pairs[i].Count > pairs[j].Count
		}
		return pairs[i].Key < pairs[j].Key
	})
	pairs = firstN(pairs, 12)
	out := make([]any, 0, len(pairs))
	for _, pair := range pairs {
		out = append(out, []any{pair.Key, pair.Count})
	}
	return out
}

func orDefault(value string, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
