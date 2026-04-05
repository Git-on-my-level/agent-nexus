package server

import (
	"net/http"
	"strings"

	"organization-autorunner-core/internal/primitives"
)

func handleListRefEdges(w http.ResponseWriter, r *http.Request, opts handlerOptions) {
	if opts.primitiveStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
		return
	}
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only GET is supported")
		return
	}

	q := r.URL.Query()
	srcType := strings.TrimSpace(q.Get("source_type"))
	srcID := strings.TrimSpace(q.Get("source_id"))
	dstType := strings.TrimSpace(q.Get("target_type"))
	dstID := strings.TrimSpace(q.Get("target_id"))
	edgeTypeFilter := strings.TrimSpace(q.Get("edge_type"))

	srcOK := srcType != "" && srcID != ""
	dstOK := dstType != "" && dstID != ""
	if srcOK == dstOK {
		writeError(w, http.StatusBadRequest, "invalid_request", "specify exactly one of: source_type+source_id or target_type+target_id")
		return
	}

	var (
		edges []primitives.RefEdge
		err   error
	)
	if srcOK {
		edges, err = opts.primitiveStore.ListRefEdgesBySource(r.Context(), srcType, srcID)
	} else {
		edges, err = opts.primitiveStore.ListRefEdgesByTarget(r.Context(), dstType, dstID)
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to list ref edges")
		return
	}

	if edgeTypeFilter != "" {
		filtered := make([]primitives.RefEdge, 0, len(edges))
		for _, e := range edges {
			if strings.TrimSpace(e.EdgeType) == edgeTypeFilter {
				filtered = append(filtered, e)
			}
		}
		edges = filtered
	}

	out := make([]map[string]any, 0, len(edges))
	for _, e := range edges {
		row := map[string]any{
			"source_type": e.SourceType,
			"source_id":   e.SourceID,
			"target_type": e.TargetType,
			"target_id":   e.TargetID,
			"edge_type":   e.EdgeType,
		}
		if len(e.Metadata) > 0 {
			row["metadata"] = e.Metadata
		}
		out = append(out, row)
	}
	writeJSON(w, http.StatusOK, map[string]any{"ref_edges": out})
}
