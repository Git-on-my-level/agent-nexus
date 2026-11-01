// Artifact read surfaces: content download, workspace usage summaries (v0 and
// v1), the blob usage ledger rebuild, and artifact listing.

package server

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/schema"
)

func handleGetArtifactContent(w http.ResponseWriter, r *http.Request, opts handlerOptions, artifactID string) {
	if opts.primitiveStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
		return
	}

	resolvedID, ok := resolveHTTPResourceID(w, r, opts, "artifact", artifactID, "artifact")
	if !ok {
		return
	}
	artifact, err := opts.primitiveStore.GetArtifact(r.Context(), resolvedID)
	if err != nil {
		if errors.Is(err, primitives.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "artifact content not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to load artifact content")
		return
	}
	if !requireAccessibleArtifact(w, r, opts, artifact) {
		return
	}
	delivery, err := opts.primitiveStore.GetArtifactContentHTTP(r.Context(), resolvedID)
	if err != nil {
		if errors.Is(err, primitives.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "artifact content not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to load artifact content")
		return
	}

	if delivery.ContentType != "" {
		w.Header().Set("Content-Type", delivery.ContentType)
	}
	if delivery.ContentDisposition != "" {
		w.Header().Set("Content-Disposition", delivery.ContentDisposition)
	}
	if delivery.ETag != "" {
		w.Header().Set("ETag", delivery.ETag)
	}
	if delivery.LastModified != "" {
		w.Header().Set("Last-Modified", delivery.LastModified)
	}
	if delivery.CacheControl != "" {
		w.Header().Set("Cache-Control", delivery.CacheControl)
	}
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if delivery.ContentLength >= 0 {
		w.Header().Set("Content-Length", strconv.FormatInt(delivery.ContentLength, 10))
	}

	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(delivery.Body)
}

func handleGetUsageSummary(w http.ResponseWriter, r *http.Request, opts handlerOptions) {
	if opts.primitiveStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
		return
	}

	summary, err := opts.primitiveStore.GetWorkspaceUsageSummary(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to load workspace usage summary")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"summary": summary})
}

func handleGetUsageV1Summary(w http.ResponseWriter, r *http.Request, opts handlerOptions) {
	if opts.primitiveStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
		return
	}

	summary, err := opts.primitiveStore.GetWorkspaceUsageV1Summary(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to load workspace usage summary")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"summary": summary})
}

func handleRebuildBlobUsageLedger(w http.ResponseWriter, r *http.Request, opts handlerOptions) {
	if opts.primitiveStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
		return
	}

	result, err := opts.primitiveStore.RebuildBlobUsageLedger(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to rebuild blob usage ledger")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"rebuild": result})
}

func handleListArtifacts(w http.ResponseWriter, r *http.Request, opts handlerOptions) {
	if opts.primitiveStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
		return
	}
	if opts.contract == nil {
		writeError(w, http.StatusServiceUnavailable, "schema_unavailable", "schema contract is not configured")
		return
	}

	query := r.URL.Query()
	threadID := strings.TrimSpace(query.Get("thread_id"))
	var threadIDs []string
	if threadID != "" {
		resolved, ok := resolveListFilterResourceRef(w, r, opts, "thread", threadID, "thread_id")
		if !ok {
			return
		}
		threadID = resolved.ID
		threadIDs = resolvedRefStorageCandidates(resolved)
		if !requireAccessibleThreadFilter(w, r, opts, threadID, "thread") {
			return
		}
	}

	var artifactIDs []string
	if idsCSV := strings.TrimSpace(query.Get("ids")); idsCSV != "" {
		parts := strings.Split(idsCSV, ",")
		raw := make([]string, 0, len(parts))
		for _, part := range parts {
			if id := strings.TrimSpace(part); id != "" {
				raw = append(raw, id)
			}
		}
		artifactIDs = make([]string, 0, len(raw))
		for _, rawID := range primitives.NormalizeArtifactIDFilter(raw, 48) {
			resolved, ok := resolveListFilterResourceRef(w, r, opts, "artifact", rawID, "ids")
			if !ok {
				return
			}
			artifactIDs = append(artifactIDs, resolved.ID)
		}
		artifactIDs = primitives.NormalizeArtifactIDFilter(artifactIDs, 48)
	}

	states, parseErr := ParseListLifecycleStates(query)
	if parseErr != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", parseErr.Error())
		return
	}

	var limitPtr *int
	if limitStr := strings.TrimSpace(query.Get("limit")); limitStr != "" {
		if n, err := strconv.Atoi(limitStr); err == nil && n > 0 {
			limitPtr = &n
		}
	}
	kind := strings.TrimSpace(query.Get("kind"))
	if kind != "" {
		if err := schema.ValidateEnum(opts.contract, "artifact_kind", kind); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
			return
		}
	}
	backingScope, ok := parseBackingScope(w, opts, query.Get("backing_scope"))
	if !ok {
		return
	}

	listFilter := primitives.ArtifactListFilter{
		States:        states,
		Q:             strings.TrimSpace(query.Get("q")),
		Limit:         limitPtr,
		Kind:          kind,
		BackingScope:  backingScope,
		ThreadID:      threadID,
		ThreadIDs:     threadIDs,
		CreatedBefore: strings.TrimSpace(query.Get("created_before")),
		CreatedAfter:  strings.TrimSpace(query.Get("created_after")),
	}
	if len(artifactIDs) > 0 {
		listFilter.IDs = artifactIDs
		listFilter.ThreadID = ""
	}

	artifacts, err := opts.primitiveStore.ListArtifacts(r.Context(), listFilter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to list artifacts")
		return
	}
	artifacts = filterAccessibleArtifacts(r, opts, artifacts)

	writeJSON(w, http.StatusOK, map[string]any{"artifacts": artifacts})
}
