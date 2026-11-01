// Shared JSON response writing and version header helpers used by handlers
// across this package.

package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
)

func writeError(w http.ResponseWriter, status int, code string, message string) {
	if status >= 500 {
		emitServerFault(w, nil, status, nil, message)
	}
	writeJSON(w, status, map[string]any{
		"error": errorPayload(code, message),
	})
}

func writeJSON(w http.ResponseWriter, status int, payload map[string]any) {
	started := time.Now()
	body, err := json.Marshal(payload)
	if err != nil {
		// Marshal errors can echo payload values. Log the failure, not the text.
		emitServerFault(w, nil, http.StatusInternalServerError, errors.New("failed to encode response"), "failed to encode response")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":{"code":"internal_error","message":"failed to encode response","recoverable":false,"hint":"Retry once; if it persists, escalate with logs and request context."}}`))
		return
	}
	if w.Header().Get("Server-Timing") != "" {
		addServerTiming(w, "serialize", started)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

func setVersionHeaders(w http.ResponseWriter, opts handlerOptions, schemaVersion string) {
	w.Header().Set("X-ANX-Core-Version", strings.TrimSpace(opts.coreVersion))
	w.Header().Set("X-ANX-API-Version", strings.TrimSpace(opts.apiVersion))
	w.Header().Set("X-ANX-Schema-Version", strings.TrimSpace(schemaVersion))
	if strings.TrimSpace(opts.minCLIVersion) != "" {
		w.Header().Set("X-ANX-Min-CLI-Version", strings.TrimSpace(opts.minCLIVersion))
	}
	if strings.TrimSpace(opts.recommendedCLIVersion) != "" {
		w.Header().Set("X-ANX-Recommended-CLI-Version", strings.TrimSpace(opts.recommendedCLIVersion))
	}
}
