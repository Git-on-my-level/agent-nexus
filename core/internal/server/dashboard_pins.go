package server

import (
	"agent-nexus-core/internal/primitives"
	"context"
	"net/http"
)

func handleDashboardPins(w http.ResponseWriter, r *http.Request, opts handlerOptions) {
	store, ok := opts.primitiveStore.(*primitives.Store)
	if !ok {
		writeError(w, 503, "unavailable", "dashboard pins are unavailable")
		return
	}
	if r.Method == http.MethodPut {
		var req struct {
			DocumentRefs []string `json:"document_refs"`
			RequestKey   string   `json:"request_key"`
		}
		if !decodeJSONBody(w, r, &req) {
			return
		}
		actor, ok := resolveWriteActorID(w, r, opts, "")
		if !ok {
			return
		}
		status, data, err := executeAtomicMutation(r.Context(), opts.primitiveStore, "workspace.dashboard.pins.set", actor, req.RequestKey, req, http.StatusOK, func(ctx context.Context) (map[string]any, error) {
			if err := store.SetDashboardPins(ctx, req.DocumentRefs); err != nil {
				return nil, err
			}
			return store.DashboardPins(ctx)
		})
		if err != nil {
			workStoreError(w, r, err)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, status, data)
		return
	}
	data, err := store.DashboardPins(r.Context())
	if err != nil {
		workStoreError(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, data)
}
