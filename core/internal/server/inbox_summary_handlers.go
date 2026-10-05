package server

import (
	"net/http"
	"sort"
	"strconv"
	"time"
)

// Summary reuses permission and lifecycle filtering without scanning all
// workspace threads for freshness or hydrating every subject ref.
func handleGetInboxSummary(w http.ResponseWriter, r *http.Request, opts handlerOptions) {
	if opts.primitiveStore == nil {
		writeError(w, 503, "primitives_unavailable", "primitives store is not configured")
		return
	}
	limit := 5
	if values, ok := r.URL.Query()["limit"]; ok {
		var err error
		if len(values) != 1 {
			writeError(w, 400, "invalid_request", "limit must be an integer from 0 to 50")
			return
		}
		limit, err = strconv.Atoi(values[0])
		if err != nil || limit < 0 || limit > 50 {
			writeError(w, 400, "invalid_request", "limit must be an integer from 0 to 50")
			return
		}
	}
	items, err := loadVisibleInboxItems(r, opts, false)
	if err != nil {
		writeError(w, 500, "internal_error", "failed to load inbox projections")
		return
	}
	asks := make([]map[string]any, 0)
	for _, item := range items {
		if anyString(item["kind"]) == "ask" {
			asks = append(asks, item)
		}
	}
	sort.SliceStable(asks, func(i, j int) bool {
		a, b := anyString(asks[i]["priority"]), anyString(asks[j]["priority"])
		rank := func(p string) int {
			switch p {
			case "urgent", "p0", "critical":
				return 0
			case "high", "p1":
				return 1
			case "normal", "medium", "p2":
				return 2
			case "low", "p3":
				return 3
			default:
				return 4
			}
		}
		if rank(a) != rank(b) {
			return rank(a) < rank(b)
		}
		a, b = anyString(asks[i]["trigger_at"]), anyString(asks[j]["trigger_at"])
		if a != b {
			return a < b
		}
		return anyString(asks[i]["id"]) < anyString(asks[j]["id"])
	})
	count := len(asks)
	if len(asks) > limit {
		asks = asks[:limit]
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, map[string]any{"open_ask_count": count, "asks": asks, "generated_at": time.Now().UTC().Format(time.RFC3339Nano)})
}
