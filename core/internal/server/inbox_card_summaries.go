package server

import (
	"net/http"
	"strings"
	"time"

	"agent-nexus-core/internal/primitives"
)

// Inbox context resolves only the visible response page. Limit distinct card
// refs before plan enrichment; a many-ref ask cannot turn into workspace work.
func enrichInboxCardSummaries(r *http.Request, opts handlerOptions, items []map[string]any) error {
	store, ok := opts.primitiveStore.(planStore)
	if !ok {
		return nil
	}
	refs := []string{}
	selected := map[string]bool{}
	truncated := false
	for _, item := range items {
		related := append([]string{anyString(item["subject_ref"])}, stringSliceAny(item["related_refs"])...)
		for _, ref := range related {
			if !strings.HasPrefix(ref, "card:") || selected[ref] {
				continue
			}
			if len(refs) == 50 {
				truncated = true
				continue
			}
			selected[ref] = true
			refs = append(refs, ref)
		}
	}
	if len(refs) == 0 {
		return nil
	}
	previews, err := store.ResolveRefs(r.Context(), refs, planVisibility(r, opts), time.Now().UTC(), planStalledAfter())
	if err != nil {
		return err
	}
	resolved := map[string]primitives.RefPreview{}
	for _, preview := range previews {
		if preview.Kind == "card" && preview.Resolvable && preview.Summary != nil {
			resolved[preview.Ref] = preview
		}
	}
	for _, item := range items {
		related := append([]string{anyString(item["subject_ref"])}, stringSliceAny(item["related_refs"])...)
		cards := []primitives.RefPreview{}
		seen := map[string]bool{}
		for _, ref := range related {
			if card, ok := resolved[ref]; ok && !seen[ref] {
				cards = append(cards, card)
				seen[ref] = true
			}
		}
		if len(cards) > 0 {
			item["related_cards"] = cards
		}
		if truncated {
			item["related_cards_truncated"] = true
		}
	}
	return nil
}
