// Document revision history reads and public revision views, plus the
// document field validation helpers (document IDs, content types) and the
// package-wide firstNonEmptyString and backing-thread helpers.

package server

import (
	"errors"
	"net/http"
	"path/filepath"
	"strings"

	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/schema"
)

func handleListDocumentHistory(w http.ResponseWriter, r *http.Request, opts handlerOptions, documentID string) {
	if opts.primitiveStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
		return
	}
	var ok bool
	documentID, ok = resolveHTTPResourceID(w, r, opts, "document", documentID, "document")
	if !ok {
		return
	}
	documentRef, documentHandle := resolvedPublicIdentity(r.Context(), opts, "document", documentID)

	history, err := opts.primitiveStore.ListDocumentHistory(r.Context(), documentID)
	if err != nil {
		if errors.Is(err, primitives.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "document not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to load document history")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"document_ref":    documentRef,
		"document_handle": documentHandle,
		"document_id":     documentID,
		"revisions":       publicDocumentRevisionsView(history),
	})
}

func handleGetDocumentRevision(w http.ResponseWriter, r *http.Request, opts handlerOptions, documentID string, revisionID string) {
	if opts.primitiveStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
		return
	}
	resolved, err := opts.primitiveStore.ResolveResourceRef(r.Context(), primitives.ResourceRefInput{Type: "document", Ref: documentID})
	if err == nil {
		documentID = resolved.ID
	} else if errors.Is(err, primitives.ErrInvalidResourceRef) || strings.Contains(documentID, ":") {
		writeError(w, http.StatusBadRequest, "invalid_request", "document is invalid")
		return
	} else if !errors.Is(err, primitives.ErrNotFound) {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to resolve document")
		return
	}
	documentRef, documentHandle := resolvedPublicIdentity(r.Context(), opts, "document", documentID)
	revisionID = strings.TrimSpace(revisionID)
	if revisionID == "" || strings.Contains(revisionID, "/") {
		writeError(w, http.StatusBadRequest, "invalid_request", "revision_id is required")
		return
	}
	if resolved, err := opts.primitiveStore.ResolveResourceRef(r.Context(), primitives.ResourceRefInput{Type: "document_revision", Ref: revisionID}); err == nil {
		revisionID = resolved.ID
	}

	revision, err := opts.primitiveStore.GetDocumentRevision(r.Context(), documentID, revisionID)
	if err != nil {
		if errors.Is(err, primitives.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "document revision not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to load document revision")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"document_ref":    documentRef,
		"document_handle": documentHandle,
		"document_id":     documentID,
		"revision":        publicDocumentRevisionView(revision),
	})
}

func publicDocumentRevisionsView(revisions []map[string]any) []map[string]any {
	out := make([]map[string]any, 0, len(revisions))
	for _, revision := range revisions {
		out = append(out, publicDocumentRevisionView(revision))
	}
	return out
}

func publicDocumentRevisionView(revision map[string]any) map[string]any {
	out := copyStringAnyMap(revision)
	for _, key := range []string{"document_id", "artifact_id", "prev_revision_id", "thread_id"} {
		delete(out, key)
	}
	return out
}

func validateDocumentID(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return errors.New("document_id is required")
	}
	if filepath.IsAbs(value) {
		return errors.New("document_id must not be absolute")
	}
	if value == "." || value == ".." {
		return errors.New("document_id must not be . or ..")
	}
	if strings.Contains(value, "/") || strings.Contains(value, `\`) {
		return errors.New("document_id must not contain path separators")
	}
	return nil
}

func validateDocumentContentType(contentType string) error {
	switch strings.TrimSpace(contentType) {
	case "text", "structured", "binary":
		return nil
	default:
		return errors.New("content_type must be one of: text, structured, binary")
	}
}

func firstNonEmptyString(values ...any) string {
	for _, value := range values {
		text, _ := value.(string)
		text = strings.TrimSpace(text)
		if text != "" {
			return text
		}
	}
	return ""
}

func documentBackingThreadID(document map[string]any) string {
	if threadID := strings.TrimSpace(anyString(document["thread_id"])); threadID != "" {
		return threadID
	}
	subjectRef := strings.TrimSpace(anyString(document["subject_ref"]))
	if subjectRef == "" {
		return ""
	}
	prefix, id, err := schema.SplitTypedRef(subjectRef)
	if err != nil || prefix != "thread" {
		return ""
	}
	return strings.TrimSpace(id)
}
