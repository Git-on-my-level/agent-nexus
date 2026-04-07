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
	sourceRef := strings.TrimSpace(q.Get("source_ref"))
	targetRef := strings.TrimSpace(q.Get("target_ref"))
	relation := strings.TrimSpace(q.Get("relation"))

	if (sourceRef != "" && targetRef != "") || (sourceRef == "" && targetRef == "") {
		writeError(w, http.StatusBadRequest, "invalid_request", "specify exactly one of: source_ref or target_ref")
		return
	}

	var (
		edges []primitives.RefEdge
		err   error
	)
	if sourceRef != "" {
		edges, err = opts.primitiveStore.ListRefEdgesBySource(r.Context(), sourceRef, relation)
	} else {
		edges, err = opts.primitiveStore.ListRefEdgesByTarget(r.Context(), targetRef, relation)
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to list ref edges")
		return
	}

	out := make([]map[string]any, 0, len(edges))
	for _, e := range edges {
		row := map[string]any{
			"source_ref":    e.SourceRef,
			"target_ref":    e.TargetRef,
			"relation":      e.Relation,
			"discovered_at": e.DiscoveredAt,
		}
		if len(e.Metadata) > 0 {
			row["metadata"] = e.Metadata
		}
		out = append(out, row)
	}
	writeJSON(w, http.StatusOK, map[string]any{"ref_edges": out})
}
