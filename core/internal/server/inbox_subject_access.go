package server

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"agent-nexus-core/internal/primitives"
)

// Inbox retention follows the request lifecycle, but authorization still
// follows its subject. Keep this local until the shared card read policy lands:
// both the card's backing thread (including legacy parent_thread_id) and its
// containing board's backing thread must be readable, regardless of archive.
func inboxItemAccessible(r *http.Request, opts handlerOptions, threadID string, item map[string]any) bool {
	if !threadAccessible(r, opts, threadID) {
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
		return threadAccessible(r, opts, id)
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
