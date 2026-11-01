// Normalization of incoming request field values (required timestamps and
// optional strings) shared by the board, card, document, and topic handlers.

package server

import (
	"net/http"
	"strings"
	"time"
)

func normalizeRequiredTimestamp(w http.ResponseWriter, value *string, fieldName string) (string, bool) {
	raw := strings.TrimSpace(*value)
	if _, err := time.Parse(time.RFC3339, raw); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", fieldName+" must be an RFC3339 datetime string")
		return "", false
	}
	return raw, true
}

func normalizeOptionalRequestStringPointer(raw *string) *string {
	if raw == nil {
		return nil
	}
	value := strings.TrimSpace(*raw)
	return &value
}
