// Document lifecycle handlers: trash, archive, unarchive, restore, and purge,
// including the shared lifecycle mutation path they dispatch to.

package server

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"agent-nexus-core/internal/primitives"
)

func handleTrashDocument(w http.ResponseWriter, r *http.Request, opts handlerOptions, documentID string) {
	handleDocumentLifecycle(w, r, opts, documentID, "trash")
}
func handleArchiveDocument(w http.ResponseWriter, r *http.Request, opts handlerOptions, documentID string) {
	handleDocumentLifecycle(w, r, opts, documentID, "archive")
}
func handleUnarchiveDocument(w http.ResponseWriter, r *http.Request, opts handlerOptions, documentID string) {
	handleDocumentLifecycle(w, r, opts, documentID, "unarchive")
}
func handleRestoreDocument(w http.ResponseWriter, r *http.Request, opts handlerOptions, documentID string) {
	handleDocumentLifecycle(w, r, opts, documentID, "restore")
}

func handleDocumentLifecycle(w http.ResponseWriter, r *http.Request, opts handlerOptions, documentID, action string) {
	if opts.primitiveStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
		return
	}
	var ok bool
	documentID, ok = resolveHTTPResourceID(w, r, opts, "document", documentID, "document")
	if !ok {
		return
	}
	var req struct {
		ActorID     string  `json:"actor_id"`
		RequestKey  string  `json:"request_key"`
		IfUpdatedAt *string `json:"if_updated_at"`
		Reason      string  `json:"reason"`
	}
	if !decodeJSONBody(w, r, &req) {
		return
	}
	// Bind the submitted body, including its original timestamp representation.
	original := req
	if req.IfUpdatedAt != nil {
		normalized, valid := normalizeRequiredTimestamp(w, req.IfUpdatedAt, "if_updated_at")
		if !valid {
			return
		}
		req.IfUpdatedAt = &normalized
	} else if strings.TrimSpace(req.RequestKey) != "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "if_updated_at is required for keyed lifecycle requests")
		return
	}
	actorID, ok := resolveWriteActorID(w, r, opts, req.ActorID)
	if !ok {
		return
	}
	// Recheck current access before returning a receipt containing private content.
	if _, _, err := opts.primitiveStore.GetDocument(r.Context(), documentID); err != nil {
		if errors.Is(err, primitives.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "document not found")
		} else {
			writeError(w, http.StatusInternalServerError, "internal_error", "failed to load document")
		}
		return
	}
	status, response, err := executeAtomicMutation(r.Context(), opts.primitiveStore, "docs."+action, actorID, req.RequestKey, struct {
		ID   string
		Body any
	}{documentID, original}, http.StatusOK, func(ctx context.Context) (map[string]any, error) {
		var document, revision map[string]any
		var err error
		if store, ok := opts.primitiveStore.(interface {
			ApplyDocumentLifecycle(context.Context, string, string, string, string, *string) (map[string]any, map[string]any, error)
		}); ok {
			document, revision, err = store.ApplyDocumentLifecycle(ctx, actorID, documentID, action, req.Reason, req.IfUpdatedAt)
		} else {
			// Legacy stores retain existing calls. New token-aware lifecycle
			// requests fail closed unless the store can check under its writer.
			if req.IfUpdatedAt != nil && action != "archive" {
				return nil, errors.New("atomic lifecycle base checking is unavailable")
			}
			switch action {
			case "trash":
				document, revision, err = opts.primitiveStore.TrashDocument(ctx, actorID, documentID, req.Reason)
			case "archive":
				document, revision, err = opts.primitiveStore.ArchiveDocumentIfUpdatedAt(ctx, actorID, documentID, req.IfUpdatedAt)
			case "unarchive":
				document, revision, err = opts.primitiveStore.UnarchiveDocument(ctx, actorID, documentID)
			case "restore":
				document, revision, err = opts.primitiveStore.RestoreDocument(ctx, actorID, documentID, req.Reason)
			default:
				return nil, primitives.ErrInvalidDocumentRequest
			}
		}
		if err != nil {
			return nil, err
		}
		return map[string]any{"document": document, "revision": revision}, nil
	})
	if err != nil {
		if writePrimitiveQuotaViolationError(w, err) {
			return
		}
		switch {
		case errors.Is(err, primitives.ErrNotFound):
			writeError(w, http.StatusNotFound, "not_found", "document not found")
		case errors.Is(err, primitives.ErrConflict):
			writeError(w, http.StatusConflict, "conflict", "document has been updated; refresh and retry")
		case errors.Is(err, primitives.ErrAlreadyTrashed):
			writeError(w, http.StatusConflict, "already_trashed", "document is trashed")
		case errors.Is(err, primitives.ErrNotArchived):
			writeError(w, http.StatusConflict, "not_archived", "document is not archived")
		case errors.Is(err, primitives.ErrNotTrashed):
			writeError(w, http.StatusConflict, "not_trashed", "document is not currently trashed")
		case errors.Is(err, primitives.ErrInvalidDocumentRequest):
			writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		default:
			writeError(w, http.StatusInternalServerError, "internal_error", "failed to "+action+" document")
		}
		return
	}
	notifyProjectionMaintainer(opts)
	writeJSON(w, status, response)
}

func handlePurgeDocument(w http.ResponseWriter, r *http.Request, opts handlerOptions, documentID string) {
	if opts.primitiveStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
		return
	}
	var resolvedDocOK bool
	documentID, resolvedDocOK = resolveHTTPResourceID(w, r, opts, "document", documentID, "document")
	if !resolvedDocOK {
		return
	}

	principal, ok := requireAuthenticatedPrincipal(w, r, opts)
	if !ok {
		return
	}
	if !isHumanPrincipal(principal) {
		writeError(w, http.StatusForbidden, "human_only", "only human principals may permanently delete documents")
		return
	}

	err := opts.primitiveStore.PurgeDocument(r.Context(), documentID)
	if err != nil {
		if writePrimitiveQuotaViolationError(w, err) {
			return
		}
		if errors.Is(err, primitives.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "document not found")
			return
		}
		if errors.Is(err, primitives.ErrNotTrashed) {
			writeError(w, http.StatusConflict, "not_trashed", "document is not currently trashed")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to permanently delete document")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"purged": true, "document_id": documentID})
}
