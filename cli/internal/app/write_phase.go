package app

import (
	"net/http"
	"strings"
)

// writeTracker records whether this invocation sent a mutating request, so
// every error envelope can say which phase failed: preflight (nothing
// mutating left the process), write (the mutating request failed or its
// outcome is unknown) or postflight (a write succeeded and a later step,
// such as a readback or follow-up read, failed).
//
// A command runs its requests sequentially, so plain fields suffice; App is
// copied by value in tests, which a mutex would forbid.
type writeTracker struct {
	succeeded int
	failed    bool
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
		t.failed = true
		return
	}
	t.succeeded++
}

func (t *writeTracker) phase() (string, bool) {
	switch {
	case t.failed:
		return "write", true
	case t.succeeded > 0:
		return "postflight", true
	default:
		return "preflight", false
	}
}
