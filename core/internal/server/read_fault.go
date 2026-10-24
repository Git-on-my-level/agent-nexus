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
	diagnostic := readFaultDiagnostic(serverFaultClass(err))
	logErr := error(diagnostic)
	if errors.Is(err, context.Canceled) {
		// The diagnostic deliberately does not unwrap to the cause. Hand the
		// shared fault logger the class plus the cancellation sentinel: a
		// caller that gave up classifies as 499, and an internal cancel while
		// the request context is still live logs the 500 the connected client
		// receives. The unwrapped cause is never logged.
		logErr = readCancelDiagnostic{diagnostic: diagnostic}
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

// readCancelDiagnostic carries a cancellation through the shared fault logger
// without exposing the wrapped cause: classification sees context.Canceled,
// while the logged text stays the read fault's class diagnostic.
type readCancelDiagnostic struct {
	diagnostic readFaultDiagnostic
}

func (d readCancelDiagnostic) Error() string { return string(d.diagnostic) }

func (d readCancelDiagnostic) Unwrap() error { return context.Canceled }
