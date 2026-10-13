package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

type blockedCompletedWriter struct {
	status           int
	header           http.Header
	entered, release chan struct{}
	once             sync.Once
}

func (w *blockedCompletedWriter) Header() http.Header    { return w.header }
func (w *blockedCompletedWriter) WriteHeader(status int) { w.status = status }
func (w *blockedCompletedWriter) Write(body []byte) (int, error) {
	w.once.Do(func() { close(w.entered) })
	<-w.release
	return len(body), nil
}

func TestCompletedInboxReleasesSnapshotBeforeNetworkWrite(t *testing.T) {
	requireIntegrationTest(t)
	h := newPrimitivesTestServer(t)
	db := h.workspace.DB()
	db.SetMaxOpenConns(1)
	now := time.Now().UTC()
	if _, err := db.ExecContext(context.Background(), `INSERT INTO events(id,handle,type,ts,actor_id,refs_json,payload_json) VALUES('completed-snapshot','completed-snapshot','human_attention_responded',?,'owner','[]','{}')`, now.Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/inbox?status=completed", nil)
	opts := handlerOptions{primitiveStore: h.primitiveStore}
	attachResourceAccessScope(req, opts)
	writer := &blockedCompletedWriter{header: http.Header{}, entered: make(chan struct{}), release: make(chan struct{})}
	done := make(chan struct{})
	go func() { defer close(done); handleGetCompletedInbox(writer, req, opts, now) }()
	select {
	case <-writer.entered:
		inUse := db.Stats().InUse
		close(writer.release)
		<-done
		if writer.status != http.StatusOK {
			t.Fatalf("response status=%d; want 200", writer.status)
		}
		if inUse != 0 {
			t.Fatalf("network Write retained %d database connections; want 0", inUse)
		}
	case <-time.After(5 * time.Second):
		close(writer.release)
		<-done
		t.Fatal("response did not reach network Write")
	}
}
