package server

import (
	"context"
	"net/http"
	"strings"
	"time"

	"agent-nexus-core/internal/plans"
	"agent-nexus-core/internal/primitives"
)

// Inbox context resolves only the visible response page. Limit distinct card
// refs before plan enrichment; a many-ref ask cannot turn into workspace work.
// Non-card linked sources resolve in a second bounded budget so open asks can
// carry the possibly-moot flag without widening the card preview budget.
func enrichInboxCardSummaries(r *http.Request, opts handlerOptions, items []map[string]any) error {
	if err := enrichInboxAskStaleness(r, opts, items); err != nil {
		return err
	}
	store, ok := opts.primitiveStore.(planStore)
	if !ok {
		return nil
	}
	refs := []string{}
	selected := map[string]bool{}
	truncated := false
	mootRefs := []string{}
	mootSelected := map[string]bool{}
	mootSkipped := map[string]bool{}
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
		if inboxAskCompleted(item) || !inboxAskKind(item) {
			continue
		}
		for _, ref := range related {
			if ref == "" || mootSelected[ref] || selected[ref] || isContextRef(ref) {
				continue
			}
			if len(mootRefs) == 50 {
				mootSkipped[ref] = true
				continue
			}
			mootSelected[ref] = true
			mootRefs = append(mootRefs, ref)
		}
	}
	if len(refs) == 0 && len(mootRefs) == 0 {
		return nil
	}
	previews, err := store.ResolveRefs(r.Context(), append(append([]string{}, refs...), mootRefs...), planVisibility(r, opts), time.Now().UTC(), planStalledAfter())
	if err != nil {
		return err
	}
	resolved := map[string]primitives.RefPreview{}
	facts := map[string]plans.Fact{}
	for _, preview := range previews {
		if preview.Kind == "card" && preview.Resolvable && preview.Summary != nil {
			resolved[preview.Ref] = preview
		}
		facts[preview.Ref] = plans.Fact{Known: preview.Resolvable, Status: firstNonEmptyString(preview.Phase, preview.Status)}
	}
	// The key is emitted only for asks the resolver actually evaluated: an
	// ask over no workflow-bearing source says nothing either way, and the
	// scoped readmodel hydration this enrichment mirrors must stay byte-equal.
	for _, item := range items {
		delete(item, "possibly_moot")
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
		// A page that skipped or could not resolve a source never claims
		// mootness: an unexamined source could still be open, and the flag
		// only carries evidence.
		complete := true
		for _, ref := range related {
			if mootSkipped[ref] {
				complete = false
				break
			}
		}
		if !inboxAskCompleted(item) && inboxAskKind(item) && complete {
			if moot := primitives.PossiblyMootFromFacts(related, facts); moot.Sources > 0 {
				item["possibly_moot"] = moot
			}
		}
	}
	return nil
}

// inboxAskCompleted mirrors the primitives' completed-attention rule: an
// answered or otherwise closed item is no longer asking for anything.
func inboxAskCompleted(item map[string]any) bool {
	status := anyString(item["status"])
	return (status != "" && status != "open") || anyString(item["responded_at"]) != "" || anyString(item["completed_at"]) != ""
}

// inboxAskKind matches the kinds ask staleness applies to.
func inboxAskKind(item map[string]any) bool {
	switch strings.TrimSpace(strings.ToLower(anyString(item["kind"]))) {
	case "ask", "review", "escalate":
		return true
	}
	return false
}

func isContextRef(ref string) bool {
	switch kind, _, _ := strings.Cut(strings.ToLower(ref), ":"); kind {
	case "doc", "document", "topic", "board", "thread":
		return true
	}
	return false
}

func enrichInboxAskStaleness(r *http.Request, opts handlerOptions, items []map[string]any) error {
	if store, ok := opts.primitiveStore.(interface {
		EnrichInboxAskStaleness(context.Context, []map[string]any, time.Time) error
	}); ok {
		if err := store.EnrichInboxAskStaleness(r.Context(), items, time.Now().UTC()); err != nil {
			return err
		}
	}
	return nil
}
