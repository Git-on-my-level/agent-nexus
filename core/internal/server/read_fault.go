package server

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// Read failures may wrap canonical text, resource identities or driver values.
// Log only diagnostic classes and numeric database codes, never Error() text.
func writeReadInternalError(w http.ResponseWriter, r *http.Request, err error, publicMessage string) {
	logErr := error(readFaultDiagnostic(serverFaultClass(err)))
	if errors.Is(err, context.Canceled) {
		// The diagnostic deliberately does not unwrap to the cause; hand the
		// cancellation itself to the shared fault logger so a caller that gave
		// up classifies as 499 instead of a 500. The cause is never logged.
		logErr = err
	}
	emitServerFault(w, r, http.StatusInternalServerError, logErr, publicMessage)
	writeJSON(w, http.StatusInternalServerError, map[string]any{
		"error": errorPayload("internal_error", publicMessage),
	})
}

func serverFaultClass(err error) string {
	types := []string{}
	code := 0
	class := "store_error"
	// Bound the complete walk as well as its depth, including joined errors.
	remaining := 32
	var visit func(error, int)
	visit = func(cause error, depth int) {
		if cause == nil || depth >= 12 || remaining == 0 {
			return
		}
		remaining--
		types = append(types, fmt.Sprintf("%T", cause))
		if coded, ok := cause.(interface{ Code() int }); ok {
			code = coded.Code()
		}
		switch cause {
		case context.Canceled:
			class = "request_canceled"
		case context.DeadlineExceeded:
			class = "deadline_exceeded"
		case sql.ErrNoRows:
			class = "no_rows"
		}
		switch unwrapped := cause.(type) {
		case interface{ Unwrap() []error }:
			for _, next := range unwrapped.Unwrap() {
				if remaining == 0 {
					break
				}
				visit(next, depth+1)
			}
		case interface{ Unwrap() error }:
			visit(unwrapped.Unwrap(), depth+1)
		}
	}
	visit(err, 0)
	return fmt.Sprintf("class=%s error_types=%s store_code=%d", class, strings.Join(types, ","), code)
}

// Deliberately do not unwrap to the original error: the shared fault logger
// must only see these diagnostic classes, never arbitrary stored error text.
type readFaultDiagnostic string

func (d readFaultDiagnostic) Error() string { return string(d) }
