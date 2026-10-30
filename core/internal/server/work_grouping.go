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
	MoveWork(context.Context, string, string, primitives.MoveWorkInput) (map[string]any, error)
	WorkTree(context.Context, string, int, bool) (map[string]any, error)
	ResolveWorkPath(context.Context, string) (map[string]any, error)
	WorkWhere(context.Context, string, string) (map[string]any, error)
	WorkPath(context.Context, string) (string, error)
}

// withWorkPath adds the derived path alias to a single work item.
func withWorkPath(ctx context.Context, store workGroupingStore, item map[string]any) {
	if item == nil {
		return
	}
	id := anyString(item["id"])
	if id == "" {
		return
	}
	if path, err := store.WorkPath(ctx, id); err == nil && path != "" {
		item["path"] = path
	}
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

func cardCreationResponse(board, card map[string]any) map[string]any {
	out := map[string]any{"board": board, "card": card}
	if homes, ok := card["likely_home"].([]primitives.LikelyHome); ok {
		out["likely_home"] = homes
		out["next_actions"] = primitives.GroupingNextActions(anyString(card["ref"]), homes, "group")
	}
	return out
}
func handleWorkGrouping(w http.ResponseWriter, r *http.Request, opts handlerOptions, path string) bool {
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	treeRead := path == "/tree" || path == "/resolve" || path == "/where"
	if path != "/ungrouped" && !treeRead && (len(parts) != 2 || (parts[1] != "group" && parts[1] != "fold" && parts[1] != "unfold" && parts[1] != "promote" && parts[1] != "retire" && parts[1] != "move")) {
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
	if treeRead {
		if r.Method != http.MethodGet {
			writeError(w, 405, "method_not_allowed", "GET required")
			return true
		}
		q := r.URL.Query()
		var out map[string]any
		var err error
		switch path {
		case "/tree":
			depth := 0
			if raw := q.Get("depth"); raw != "" {
				if depth, err = strconv.Atoi(raw); err != nil {
					writeError(w, 400, "invalid_request", "depth must be 1..3")
					return true
				}
			}
			out, err = store.WorkTree(r.Context(), q.Get("root"), depth, q.Get("include_done") == "true")
		case "/resolve":
			out, err = store.ResolveWorkPath(r.Context(), q.Get("path"))
		case "/where":
			out, err = store.WorkWhere(r.Context(), q.Get("ref"), q.Get("q"))
		}
		if err != nil {
			workStoreError(w, r, err)
			return true
		}
		writeJSON(w, 200, out)
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
	for _, key := range []string{"root", "unsorted"} {
		if value, exists := raw[key]; exists {
			if _, ok := value.(bool); !ok {
				writeError(w, 400, "invalid_request", key+" must be a boolean")
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
		case "promote", "retire", "move":
			var out map[string]any
			if verb == "move" {
				out, err = store.MoveWork(ctx, actor, id, primitives.MoveWorkInput{Under: anyString(raw["under"]), Root: raw["root"] == true, Unsorted: raw["unsorted"] == true, Reason: anyString(raw["reason"])})
			} else {
				out, err = store.ChangeInitiative(ctx, actor, id, verb, anyString(raw["reason"]), anyString(raw["to_board"]), anyString(raw["under"]))
			}
			if err != nil {
				return nil, err
			}
			if work, ok := out["work"].(map[string]any); ok {
				withWorkPath(ctx, store, work)
				out["work"] = publicWork(work)
			}
			return out, nil
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
