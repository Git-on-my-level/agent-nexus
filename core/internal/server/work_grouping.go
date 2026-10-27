package server

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"agent-nexus-core/internal/primitives"
)

type workGroupingStore interface {
	ChangeInitiative(context.Context, string, string, string, string, string, string) (map[string]any, error)
	GroupWork(context.Context, string, string, string, string) (map[string]any, error)
	FoldWork(context.Context, string, string, string, string) (map[string]any, error)
	UnfoldWork(context.Context, string, string) (map[string]any, error)
	UngroupedWork(context.Context, int) ([]map[string]any, bool, error)
}

func workRequestViewer(r *http.Request) (string, bool) {
	if p, ok := cachedAuthenticatedPrincipal(r); ok && p != nil {
		return p.ActorID, p.PrincipalKind == "" || p.PrincipalKind == "human"
	}
	return "", true
}

func groupingWorkResponse(item map[string]any) map[string]any {
	out := map[string]any{"work": publicWork(item)}
	if homes, ok := item["likely_home"].([]primitives.LikelyHome); ok {
		out["likely_home"] = homes
		out["next_actions"] = primitives.GroupingNextActions(anyString(item["ref"]), homes, "group")
	}
	return out
}

func handleWorkGrouping(w http.ResponseWriter, r *http.Request, opts handlerOptions, path string) bool {
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	if path != "/ungrouped" && (len(parts) != 2 || (parts[1] != "group" && parts[1] != "fold" && parts[1] != "unfold" && parts[1] != "promote" && parts[1] != "retire")) {
		return false
	}
	store, ok := opts.primitiveStore.(workGroupingStore)
	if !ok {
		writeError(w, 503, "work_unavailable", "grouping is unavailable")
		return true
	}
	if path == "/ungrouped" {
		if r.Method != http.MethodGet {
			writeError(w, 405, "method_not_allowed", "GET required")
			return true
		}
		limit := 20
		if raw := r.URL.Query().Get("limit"); raw != "" {
			var err error
			limit, err = strconv.Atoi(raw)
			if err != nil {
				writeError(w, 400, "invalid_request", "limit must be 1..50")
				return true
			}
		}
		items, more, err := store.UngroupedWork(r.Context(), limit)
		if err != nil {
			workStoreError(w, r, err)
			return true
		}
		// The tray is deliberately a compact projection, not full work reads.
		for _, item := range items {
			delete(item, "id")
		}
		writeJSON(w, 200, map[string]any{"work": items, "has_more": more})
		return true
	}
	if r.Method != http.MethodPost {
		writeError(w, 405, "method_not_allowed", "POST required")
		return true
	}
	var raw map[string]any
	if !decodeJSONBody(w, r, &raw) {
		return true
	}
	actor, ok := resolveWriteActorID(w, r, opts, anyString(raw["actor_id"]))
	if !ok {
		return true
	}
	for _, key := range []string{"under", "into", "after", "request_key", "reason", "to_board"} {
		if value, exists := raw[key]; exists {
			if _, ok := value.(string); !ok {
				writeError(w, 400, "invalid_request", key+" must be a string")
				return true
			}
		}
	}
	id, ok := resolveHTTPResourceID(w, r, opts, "card", parts[0], "card")
	if !ok {
		return true
	}
	verb := parts[1]
	status, response, err := executeAtomicMutation(r.Context(), opts.primitiveStore, "work."+verb+":"+id, actor, anyString(raw["request_key"]), raw, 200, func(ctx context.Context) (map[string]any, error) {
		var item map[string]any
		var err error
		switch verb {
		case "promote", "retire":
			return store.ChangeInitiative(ctx, actor, id, verb, anyString(raw["reason"]), anyString(raw["to_board"]), anyString(raw["under"]))
		case "group":
			item, err = store.GroupWork(ctx, actor, id, anyString(raw["under"]), anyString(raw["after"]))
		case "fold":
			item, err = store.FoldWork(ctx, actor, id, anyString(raw["into"]), anyString(raw["after"]))
		case "unfold":
			item, err = store.UnfoldWork(ctx, actor, id)
		}
		if err != nil {
			return nil, err
		}
		return groupingWorkResponse(item), nil
	})
	if err != nil {
		workStoreError(w, r, err)
		return true
	}
	writeJSON(w, status, response)
	return true
}
