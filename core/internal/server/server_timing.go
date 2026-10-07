package server

import (
	"fmt"
	"net/http"
	"time"
)

// Names are fixed internal phases; never include resource or principal data.
func addServerTiming(w http.ResponseWriter, name string, started time.Time) {
	w.Header().Add("Server-Timing", fmt.Sprintf("%s;dur=%.3f", name, float64(time.Since(started))/float64(time.Millisecond)))
}

// Only used for finite Overview/Inbox JSON routes, never for streams. The
// inclusive core duration makes proxy overhead measurable even on Inbox,
// whose internals belong to a separate read-model workstream.
type serverTimingWriter struct {
	http.ResponseWriter
	started     time.Time
	wroteHeader bool
}

func (w *serverTimingWriter) WriteHeader(status int) {
	if !w.wroteHeader {
		w.wroteHeader = true
		addServerTiming(w.ResponseWriter, "core", w.started)
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *serverTimingWriter) Write(body []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(body)
}
