package server

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"agent-nexus-core/internal/primitives"
)

// The canonical request scope checks all inherited references together. The
// fallback supports alternate stores without a canonical database policy.
func inboxItemAccessible(r *http.Request, opts handlerOptions, threadID string, item map[string]any) bool {
	if recipient := anyString(item["recipient_actor_id"]); recipient != "" {
		principal, ok := cachedAuthenticatedPrincipal(r)
		if !ok || principal == nil || principal.ActorID != recipient {
			return false
		}
	}
	if check, ok := r.Context().Value(resourceAccessCheckKey{}).(func(context.Context, any) error); ok {
		if check(r.Context(), []any{"thread:" + threadID, item}) != nil {
			return false
		}
	}
	if strings.TrimSpace(threadID) != "" && !inboxSubjectRefAccessible(r, opts, "thread:"+threadID) {
		return false
	}
	refs := append([]string{anyString(item["subject_ref"])}, stringSliceAny(item["related_refs"])...)
	seen := make(map[string]bool, len(refs))
	for _, ref := range refs {
		ref = strings.TrimSpace(ref)
		if seen[ref] {
			continue
		}
		seen[ref] = true
		if !inboxSubjectRefAccessible(r, opts, ref) {
			return false
		}
	}
	return true
}

func inboxSubjectRefAccessible(r *http.Request, opts handlerOptions, ref string) bool {
	kind, id, ok := strings.Cut(ref, ":")
	if !ok || opts.primitiveStore == nil {
		return true
	}
	if kind != "card" && kind != "board" && kind != "thread" {
		return true
	}
	// Subject refs may use current handles or historical aliases. Resolve them
	// before applying access, just as live resource resolution does.
	if resolver, ok := opts.primitiveStore.(interface {
		CanonicalResolutionRefID(context.Context, string) (string, string, error)
	}); ok {
		_, resolved, err := resolver.CanonicalResolutionRefID(r.Context(), ref)
		if err != nil && !errors.Is(err, primitives.ErrNotFound) {
			return false
		}
		if err == nil && resolved != "" {
			id = resolved
		}
	}
	switch kind {
	case "thread":
		if !threadAccessible(r, opts, id) {
			return false
		}
		owners, err := opts.primitiveStore.InboxThreadAccessOwners(r.Context(), id)
		if err != nil {
			return false
		}
		for _, owner := range owners {
			if !canAccessPMThread(r, opts, map[string]any{"pm_actor_id": owner}) {
				return false
			}
		}
		return true
	case "board":
		board, err := opts.primitiveStore.GetBoard(r.Context(), id)
		if err != nil {
			return errors.Is(err, primitives.ErrNotFound)
		}
		return threadAccessible(r, opts, anyString(board["thread_id"]))
	case "card":
		card, err := opts.primitiveStore.GetBoardCard(r.Context(), "", id)
		if err != nil {
			return errors.Is(err, primitives.ErrNotFound)
		}
		threadID := strings.TrimSpace(anyString(card["thread_id"]))
		if threadID == "" {
			threadID = anyString(card["parent_thread_id"])
		}
		if !threadAccessible(r, opts, threadID) {
			return false
		}
		boardID := strings.TrimSpace(anyString(card["board_id"]))
		return boardID == "" || inboxSubjectRefAccessible(r, opts, "board:"+boardID)
	}
	return true
}
