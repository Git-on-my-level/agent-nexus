package importer

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Dedupe creates exact and probable duplicate reports from an inventory.
func Dedupe(opts DedupeOptions) (DedupeReport, error) {
	if strings.TrimSpace(opts.InventoryPath) == "" {
		return DedupeReport{}, fmt.Errorf("inventory path is required")
	}
	if strings.TrimSpace(opts.OutDir) == "" {
		return DedupeReport{}, fmt.Errorf("out dir is required")
	}
	inventoryPath, err := filepath.Abs(opts.InventoryPath)
	if err != nil {
		return DedupeReport{}, err
	}
	outDir, err := filepath.Abs(opts.OutDir)
	if err != nil {
		return DedupeReport{}, err
	}
	if err := ensureDir(outDir); err != nil {
		return DedupeReport{}, err
	}
	items, err := loadInventory(inventoryPath)
	if err != nil {
		return DedupeReport{}, err
	}
	exact := exactDuplicateGroups(items)
	exactDrops := map[string]struct{}{}
	for _, group := range exact {
		for _, sourceID := range group.Drop {
			exactDrops[sourceID] = struct{}{}
		}
	}
	probable := probableDuplicateGroups(items, exactDrops)
	recommendedSkip := make([]string, 0, len(exactDrops))
	for sourceID := range exactDrops {
		recommendedSkip = append(recommendedSkip, sourceID)
	}
	sort.Strings(recommendedSkip)
	report := DedupeReport{
		CreatedAt:          utcNow(),
		Inventory:          inventoryPath,
		ExactDuplicates:    exact,
		ProbableDuplicates: probable,
		RecommendedSkipIDs: recommendedSkip,
	}
	if err := writeJSON(filepath.Join(outDir, "dedupe.json"), report); err != nil {
		return DedupeReport{}, err
	}
	return report, nil
}

func exactDuplicateGroups(items []InventoryRecord) []ExactDuplicateGroup {
	groups := map[string][]InventoryRecord{}
	for _, item := range items {
		key := item.SHA256
		if item.IsText && strings.TrimSpace(item.NormalizedTextSHA256) != "" {
			key = item.NormalizedTextSHA256
		}
		if strings.TrimSpace(key) == "" {
			continue
		}
		groups[key] = append(groups[key], item)
	}
	result := make([]ExactDuplicateGroup, 0)
	for key, members := range groups {
		if len(members) < 2 {
			continue
		}
		keep := members[0]
		for _, candidate := range members[1:] {
			if keepScore(candidate) > keepScore(keep) {
				keep = candidate
			}
		}
		drop := make([]string, 0, len(members)-1)
		memberIDs := make([]string, 0, len(members))
		for _, member := range members {
			memberIDs = append(memberIDs, member.SourceID)
			if member.SourceID != keep.SourceID {
				drop = append(drop, member.SourceID)
			}
		}
		sort.Strings(memberIDs)
		sort.Strings(drop)
		result = append(result, ExactDuplicateGroup{
			Hash:    key,
			Keep:    keep.SourceID,
			Members: memberIDs,
			Drop:    drop,
			Reason:  "exact-content-duplicate",
		})
	}
	sort.Slice(result, func(i, j int) bool {
		return strings.Join(result[i].Members, ",") < strings.Join(result[j].Members, ",")
	})
	return result
}

func keepScore(item InventoryRecord) string {
	isMarkdown := "0"
	if item.Extension == ".md" || item.Extension == ".markdown" || item.Extension == ".org" || item.Extension == ".rst" {
		isMarkdown = "1"
	}
	isText := "0"
	if item.IsText {
		isText = "1"
	}
	previewLen := fmt.Sprintf("%08d", len(item.Preview))
	relPenalty := fmt.Sprintf("%08d", 99999999-min(99999999, len(item.RelPath)))
	return isMarkdown + isText + previewLen + relPenalty
}

func probableDuplicateGroups(items []InventoryRecord, exactDropped map[string]struct{}) []ProbableDuplicateGroup {
	type bucketKey struct {
		Title    string
		Category string
		Cluster  string
	}
	buckets := map[bucketKey][]InventoryRecord{}
	for _, item := range items {
		if _, dropped := exactDropped[item.SourceID]; dropped {
			continue
		}
		title := canonicalTitle(item.TitleHint)
		category := strings.TrimSpace(item.Category)
		cluster := strings.TrimSpace(item.ClusterHint)
		if title == "" || category == "binary" || category == "archive" || category == "image" {
			continue
		}
		key := bucketKey{Title: title, Category: category, Cluster: cluster}
		buckets[key] = append(buckets[key], item)
	}
	result := make([]ProbableDuplicateGroup, 0)
	for key, members := range buckets {
		if len(members) < 2 {
			continue
		}
		sort.Slice(members, func(i, j int) bool { return members[i].SourceID < members[j].SourceID })
		previewOpenings := map[string]struct{}{}
		for _, member := range members {
			opening := strings.ToLower(strings.TrimSpace(first160(member.Preview)))
			previewOpenings[opening] = struct{}{}
		}
		if len(previewOpenings) == 1 || len(previewOpenings) <= max(1, len(members)/2) {
			memberIDs := make([]string, 0, len(members))
			for _, member := range members {
				memberIDs = append(memberIDs, member.SourceID)
			}
			result = append(result, ProbableDuplicateGroup{
				Title:       key.Title,
				Category:    key.Category,
				ClusterHint: key.Cluster,
				Members:     memberIDs,
				Reason:      "same-title-similar-opening",
				Action:      "review-before-import",
			})
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].ClusterHint != result[j].ClusterHint {
			return result[i].ClusterHint < result[j].ClusterHint
		}
		return result[i].Title < result[j].Title
	})
	return result
}

func first160(value string) string {
	if len(value) <= 160 {
		return value
	}
	return value[:160]
}

func ensureDir(path string) error {
	return os.MkdirAll(path, 0o755)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
