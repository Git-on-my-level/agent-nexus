package server

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
)

// statusClientClosedRequest is nginx's conventional status for a caller that
// disconnected before core finished; the response itself is never delivered.
const statusClientClosedRequest = 499

// faultResponseWriter keeps the request next to the response so a 5xx written
// through writeError can be correlated without logging the URL, query, or body.
type faultResponseWriter struct {
	http.ResponseWriter
	r       *http.Request
	started time.Time
	logged  bool
}

func (w *faultResponseWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (w *faultResponseWriter) Flush() {
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func faultFrom(w http.ResponseWriter) *faultResponseWriter {
	for seen := 0; w != nil && seen < 8; seen++ {
		if fault, ok := w.(*faultResponseWriter); ok {
			return fault
		}
		unwrapper, ok := w.(interface{ Unwrap() http.ResponseWriter })
		if !ok {
			return nil
		}
		next := unwrapper.Unwrap()
		if next == nil || next == w {
			return nil
		}
		w = next
	}
	return nil
}

// noteServerFaultLogged marks a 5xx that already has its own diagnostic line.
// Work failures use that path because their error text can contain submitted
// values; they must not be logged a second time from writeError.
func noteServerFaultLogged(w http.ResponseWriter) {
	if fault := faultFrom(w); fault != nil {
		fault.logged = true
	}
}

// writeInternalError logs the cause once and returns a generic 500. The client
// message stays the public text; the error chain is server-side only.
func writeInternalError(w http.ResponseWriter, r *http.Request, err error, publicMessage string) {
	emitServerFault(w, r, http.StatusInternalServerError, err, publicMessage)
	writeJSON(w, http.StatusInternalServerError, map[string]any{
		"error": errorPayload("internal_error", publicMessage),
	})
}

func emitServerFault(w http.ResponseWriter, r *http.Request, status int, err error, publicMessage string) {
	if fault := faultFrom(w); fault != nil {
		if fault.logged {
			return
		}
		fault.logged = true
		if r == nil {
			r = fault.r
		}
	}
	if status >= 500 && requestCanceledByClient(r, err) {
		emitClientCanceled(w, r, publicMessage)
		return
	}
	requestID := serverFaultRequestID(w, r)
	cause := "-"
	if err != nil {
		cause = serverFaultCause(err)
	}
	log.Printf("anx-core: server error status=%d method=%s route=%s request_id=%s actor_kind=%s message=%s error=%s",
		status, serverFaultMethod(r), routeTemplate(r), requestID, serverFaultActorKind(r), sanitizeFaultText(publicMessage, 240), cause)
}

// A cancellation is the caller giving up, not a server fault. It classifies
// as a client cancel only when the request context died AND the failure is the
// cancellation itself: context.Canceled in the error chain, or no error at all
// (writeError sites that never carried a cause). A genuine failure that merely
// lands after the caller disconnected — a driver IOERR mid-scan, a rolled-back
// statement — stays a server error and keeps its diagnostics, because
// disconnects and real failures correlate exactly under overload. A
// context.Canceled while the request context is still live is an internal
// cancel bug and also stays on the server-error path, matching the 5xx the
// connected client receives. Core-side deadlines (context.DeadlineExceeded)
// are real faults and stay there too.
func requestCanceledByClient(r *http.Request, err error) bool {
	if r == nil || r.Context().Err() != context.Canceled {
		return false
	}
	return err == nil || errors.Is(err, context.Canceled)
}

// The client-canceled record keeps cancellation visible without counting it as
// a 5xx: route template, request id, the stage the request reached and how long
// it took, so slow stages stay visible. elapsed_ms is -1 when the entry point
// did not record a start time (only possible outside the route middleware).
func emitClientCanceled(w http.ResponseWriter, r *http.Request, stage string) {
	elapsed := int64(-1)
	if fault := faultFrom(w); fault != nil && !fault.started.IsZero() {
		elapsed = time.Since(fault.started).Milliseconds()
	}
	log.Printf("anx-core: client canceled status=%d method=%s route=%s request_id=%s actor_kind=%s stage=%s elapsed_ms=%d",
		statusClientClosedRequest, serverFaultMethod(r), routeTemplate(r), serverFaultRequestID(w, r), serverFaultActorKind(r), sanitizeFaultText(stage, 240), elapsed)
}

func serverFaultRequestID(w http.ResponseWriter, r *http.Request) string {
	if w != nil {
		if id := strings.TrimSpace(w.Header().Get("X-Request-ID")); uuid.Validate(id) == nil {
			return id
		}
	}
	if r != nil {
		if id := strings.TrimSpace(r.Header.Get("X-Request-ID")); uuid.Validate(id) == nil {
			if w != nil {
				w.Header().Set("X-Request-ID", id)
			}
			return id
		}
	}
	id := uuid.NewString()
	if w != nil {
		w.Header().Set("X-Request-ID", id)
	}
	return id
}

func serverFaultMethod(r *http.Request) string {
	if r == nil {
		return "-"
	}
	method := strings.TrimSpace(r.Method)
	if method == "" || len(method) > 16 {
		return "-"
	}
	for _, ch := range method {
		if ch < 'A' || ch > 'Z' {
			return "-"
		}
	}
	return method
}

func serverFaultActorKind(r *http.Request) string {
	principal, ok := cachedAuthenticatedPrincipal(r)
	if !ok {
		return "anonymous"
	}
	kind := strings.TrimSpace(principal.PrincipalKind)
	if kind == "" || len(kind) > 32 {
		return "unknown"
	}
	for _, ch := range kind {
		if ch > unicode.MaxASCII || (!unicode.IsLetter(ch) && !unicode.IsDigit(ch) && ch != '_' && ch != '-') {
			return "unknown"
		}
	}
	return kind
}

func routeTemplate(r *http.Request) string {
	if r == nil || r.URL == nil {
		return "-"
	}
	path := r.URL.Path
	if path == "" {
		return "/"
	}
	if len(path) > 512 {
		path = path[:512]
	}
	parts := strings.Split(path, "/")
	for i, part := range parts {
		if part == "" {
			continue
		}
		if _, ok := staticRouteSegments[part]; !ok {
			parts[i] = "{id}"
		}
	}
	return strings.Join(parts, "/")
}

func serverFaultCause(err error) string {
	var b strings.Builder
	appendFaultCause(&b, err, 0)
	return sanitizeFaultText(b.String(), 2000)
}

func appendFaultCause(b *strings.Builder, err error, depth int) {
	if err == nil || depth >= 12 {
		return
	}
	if b.Len() > 0 {
		b.WriteString(" | ")
	}
	fmt.Fprintf(b, "%T: %s", err, err.Error())
	switch unwrapped := err.(type) {
	case interface{ Unwrap() []error }:
		for _, next := range unwrapped.Unwrap() {
			appendFaultCause(b, next, depth+1)
		}
	case interface{ Unwrap() error }:
		appendFaultCause(b, unwrapped.Unwrap(), depth+1)
	}
}

var faultSecretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)bearer\s+\S+`),
	regexp.MustCompile(`(?i)\b(?:token|secret|password|authorization|access_token|refresh_token)=[^\s&"']+`),
	regexp.MustCompile(`(?is)-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----.*?-----END [A-Z0-9 ]*PRIVATE KEY-----`),
}

func sanitizeFaultText(value string, max int) string {
	if strings.TrimSpace(value) == "" {
		return "-"
	}
	for _, pattern := range faultSecretPatterns {
		value = pattern.ReplaceAllString(value, "[redacted]")
	}
	value = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' {
			return ' '
		}
		return r
	}, value)
	if max > 0 && len(value) > max {
		end := max
		for end > 0 && value[end]&0xC0 == 0x80 {
			end--
		}
		value = value[:end]
	}
	return value
}

// Static path words only. Anything else, including ids and secrets, becomes {id}.
var staticRouteSegments = map[string]struct{}{
	"access": {}, "access-requests": {}, "actors": {}, "admins": {}, "agents": {},
	"archive": {}, "artifacts": {}, "asks": {}, "attachments": {}, "audit": {}, "auth": {},
	"batch": {}, "blob-usage": {}, "boards": {}, "bootstrap": {}, "brief": {},
	"cards": {}, "changes": {}, "columns": {}, "commands": {}, "comments": {},
	"concepts": {}, "curation": {}, "dashboard": {}, "dev": {}, "discord": {}, "docs": {},
	"enrollment-tokens": {}, "enrollments": {}, "events": {}, "health": {},
	"home": {}, "hosts": {}, "inbox": {}, "ingress": {}, "invites": {},
	"livez": {}, "login": {}, "me": {}, "memberships": {}, "messages": {},
	"meta": {}, "metrics": {}, "move": {}, "observations": {}, "ops": {}, "options": {},
	"overview": {}, "participants": {}, "passkey": {}, "pins": {}, "pm": {},
	"preview": {}, "principals": {}, "purge": {}, "read": {}, "readyz": {},
	"rebuild": {}, "ref-edges": {}, "refs": {}, "refresh": {}, "register": {},
	"reports": {}, "resolve": {}, "restore": {}, "reveal-batch": {}, "revisions": {},
	"runs": {}, "search": {}, "secrets": {}, "sessions": {}, "status": {},
	"stream": {}, "summary": {}, "telegram": {}, "threads": {}, "timeline": {},
	"token": {}, "topics": {}, "trash": {}, "unarchive": {}, "unread": {},
	"usage": {}, "usage-summary": {}, "v1": {}, "verify": {}, "version": {},
	"work": {}, "workspace": {}, "handshake": {},
}
