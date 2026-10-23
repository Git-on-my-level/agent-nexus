package app

import (
	"net/http"
	"strings"
	"sync/atomic"
)

// writeTracker records whether this invocation sent a mutating request, so
// every error envelope can say which phase failed: preflight (nothing
// mutating left the process), write (the mutating request failed or its
// outcome is unknown) or postflight (a write succeeded and a later step,
// such as a readback or follow-up read, failed).
//
// Background heartbeats can overlap a command's completion request. Raw atomic
// fields keep recording safe while preserving App's existing value-copy usage.
type writeTracker struct {
	succeeded uint32
	failed    uint32
}

func isMutatingMethod(method string) bool {
	switch strings.ToUpper(strings.TrimSpace(method)) {
	case "", http.MethodGet, http.MethodHead, http.MethodOptions:
		return false
	}
	return true
}

// record notes one mutating request. A failure here means the request was
// attempted: either the server answered with an error or the transport
// failed in a way that leaves the outcome uncertain.
func (t *writeTracker) record(method string, err error) {
	if !isMutatingMethod(method) {
		return
	}
	if err != nil {
		atomic.StoreUint32(&t.failed, 1)
		return
	}
	atomic.StoreUint32(&t.succeeded, 1)
}

func (t *writeTracker) phase() (string, bool) {
	switch {
	case atomic.LoadUint32(&t.failed) != 0:
		return "write", true
	case atomic.LoadUint32(&t.succeeded) > 0:
		return "postflight", true
	default:
		return "preflight", false
	}
}
