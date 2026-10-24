package server

import (
	"errors"
	"net/http"
)

func hostCeremonyError(w http.ResponseWriter, r *http.Request, err error) {
	if !writeAuthContentionError(w, r, err) {
		hostError(w, r, err)
	}
}

// Auth stores roll back failed SQLite writes. Preserve the normal busy wait,
// but let callers recover when it is exhausted. Match result codes, never text
// that might contain submitted data. Context/transport errors are deliberately
// excluded: they do not establish whether a credential was already consumed.
func writeAuthContentionError(w http.ResponseWriter, r *http.Request, err error) bool {
	var coded interface{ Code() int }
	if !errors.As(err, &coded) || (coded.Code()&255 != 5 && coded.Code()&255 != 6) {
		return false
	}
	const message = "workspace writer is busy; wait and retry the request"
	emitServerFault(w, r, http.StatusServiceUnavailable, err, message)
	w.Header().Set("Retry-After", "1")
	payload := errorPayload("busy", message)
	// Bridge proof consumption and presence updates are separate transactions.
	// Even when the presence update rolls back, its proof may be consumed.
	payload["hint"] = "Wait for Retry-After and retry; sign a fresh proof for signed requests."
	writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": payload})
	return true
}
