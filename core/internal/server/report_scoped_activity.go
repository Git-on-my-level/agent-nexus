package server

import (
	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/resourceaccess"
	reports "agent-nexus-visualreport"
	"strings"
)

func (reader *reportReader) prepareResourceRefs(refs []string) error {
	store, ok := reader.opts.primitiveStore.(*primitives.Store)
	if _, scoped := resourceaccess.PolicyFrom(reader.r.Context()); !ok || !scoped {
		return nil
	}
	pending := []string{}
	seen := map[string]bool{}
	for _, ref := range refs {
		if _, cached := reader.visibility[ref]; !cached && !seen[ref] {
			kind, _, _ := strings.Cut(ref, ":")
			if kind == "board" || kind == "topic" || kind == "document" || kind == "thread" {
				pending = append(pending, ref)
				seen[ref] = true
			}
		}
	}
	if reader.visibility == nil {
		reader.visibility = map[string]bool{}
	}
	if reader.labels == nil {
		reader.labels = map[string]string{}
	}
	for len(pending) > 0 {
		n := min(200, len(pending))
		batch := pending[:n]
		pending = pending[n:]
		snapshots, err := store.ReportSubjectSnapshots(reader.r.Context(), batch)
		if err != nil {
			return err
		}
		for _, ref := range batch {
			row := snapshots[ref]
			allowed := row != nil && reportActive(row) && !reader.hidden[ref]
			reader.visibility[ref] = allowed
			if allowed {
				reader.labels[ref] = anyString(row["title"])
			}
		}
	}
	return nil
}
func (reader *reportReader) scopeActivity(q reports.Query) error {
	if len(q.SubjectRefs) == 0 && q.Collection == "" {
		return nil
	}
	reader.activityScoped = true
	refs := append([]string{}, q.SubjectRefs...)
	if q.Collection != "" {
		store, ok := reader.opts.primitiveStore.(*primitives.Store)
		if !ok {
			return primitives.ErrNotFound
		}
		page, err := store.CollectionWork(reader.r.Context(), q.Collection, "", reader.now)
		if err != nil {
			return err
		}
		reader.activityScopePartial = page.Truncated
		selected := map[string]bool{}
		for _, ref := range refs {
			selected[ref] = true
		}
		refs = []string{}
		for _, w := range page.Work {
			ref := anyString(w["ref"])
			idRef := "card:" + anyString(w["id"])
			if len(selected) == 0 || selected[ref] || selected[idRef] {
				refs = append(refs, ref)
			}
		}
	}
	if store, ok := reader.opts.primitiveStore.(*primitives.Store); ok && len(refs) > 0 {
		subjects, err := store.ReportSubjectSnapshots(reader.r.Context(), refs)
		if err != nil {
			return err
		}
		cards, err := store.ReportWorkSnapshots(reader.r.Context(), refs)
		if err != nil {
			return err
		}
		for i, ref := range refs {
			row := subjects[ref]
			if strings.HasPrefix(ref, "card:") {
				row = cards[ref]
			}
			if canonical := anyString(row["ref"]); canonical != "" {
				refs[i] = canonical
			}
		}
	}
	reader.activityRefs = refs
	return nil
}
func (reader *reportReader) scopedEventIDs() []string {
	if !reader.activityScoped {
		return nil
	}
	store, ok := reader.opts.primitiveStore.(*primitives.Store)
	if !ok {
		reader.eventsErr = primitives.ErrNotFound
		return []string{}
	}
	ids, err := store.ReportScopedEventIDs(reader.r.Context(), reader.activityRefs)
	if err != nil {
		reader.eventsErr = err
		return []string{}
	}
	if ids == nil {
		ids = []string{}
	}
	return ids
}
func (reader *reportReader) activityMatches(item map[string]any) bool {
	if !reader.activityScoped {
		return true
	}
	subject := reportMap(item["subject"])
	ref := anyString(subject["ref"])
	for _, wanted := range reader.activityRefs {
		if wanted == ref {
			return true
		}
	}
	return false
}
