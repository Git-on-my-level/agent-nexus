// Liveness and readiness response helpers for /livez, /readyz, and
// /ops/health.

package server

import (
	"net/http"
)

func writeLivenessOK(w http.ResponseWriter) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// readinessErrorPayload names the failing check. Only the authenticated
// /ops/health view carries the check's raw error text: /readyz is public on
// direct-core deployments, and dependency errors can describe internals.
func readinessErrorPayload(r *http.Request, opts handlerOptions, includeDetail bool) map[string]any {
	if opts.healthCheck != nil {
		if err := opts.healthCheck(r.Context()); err != nil {
			return map[string]any{
				"ok":    false,
				"error": errorPayload("storage_unavailable", "storage health check failed"),
			}
		}
	}
	for _, check := range opts.readinessChecks {
		if check.Check == nil {
			continue
		}
		if err := check.Check(r.Context()); err != nil {
			code := firstNonEmptyString(check.Code, "service_unavailable")
			message := firstNonEmptyString(check.Message, "readiness check failed")
			checkPayload := map[string]any{"name": check.Name}
			if includeDetail {
				checkPayload["error"] = err.Error()
			}
			return map[string]any{
				"ok":    false,
				"error": errorPayload(code, message),
				"check": checkPayload,
			}
		}
	}
	return nil
}

func checkReadiness(w http.ResponseWriter, r *http.Request, opts handlerOptions) bool {
	if payload := readinessErrorPayload(r, opts, false); payload != nil {
		writeJSON(w, http.StatusServiceUnavailable, payload)
		return false
	}
	return true
}
