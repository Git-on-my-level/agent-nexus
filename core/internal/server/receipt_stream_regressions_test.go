package server

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"agent-nexus-core/internal/primitives"
)

func TestReceiptStreamReconnectDeliversOfflineClaim(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	store := primitives.NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	thread, err := store.CreateThread(ctx, "owner", map[string]any{"title": "offline claim"})
	if err != nil {
		t.Fatal(err)
	}
	threadID := anyString(thread.Thread["id"])
	wakeup := receiptPageWakeup("wake-offline", threadID, "2026-01-01T00:00:01Z", "")
	seedReceiptStreamWakeup(t, store, wakeup)
	loaded, err := store.GetAgentWakeup(ctx, wakeup.WakeupID)
	if err != nil {
		t.Fatal(err)
	}
	resumeID := receiptStreamEventID(loaded)
	if _, err = ws.DB().ExecContext(ctx, `UPDATE agent_wakeups SET status=? WHERE wakeup_id=?`, primitives.AgentWakeupStatusClaimed, wakeup.WakeupID); err != nil {
		t.Fatal(err)
	}
	frame := readNextReceiptStreamFrame(t, store, threadID, resumeID, time.Hour)
	if !strings.Contains(frame, "event: notification_receipt\n") || !strings.Contains(frame, "claimed") {
		t.Fatalf("reconnect skipped the offline claim: %q", frame)
	}
}

func TestReceiptStreamAcceptedResumeIsNotReemitted(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	store := primitives.NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	thread, err := store.CreateThread(ctx, "owner", map[string]any{"title": "accepted resume"})
	if err != nil {
		t.Fatal(err)
	}
	threadID := anyString(thread.Thread["id"])
	wakeup := receiptPageWakeup("wake-accepted", threadID, "2026-01-01T00:00:01Z", "")
	seedReceiptStreamWakeup(t, store, wakeup)
	loaded, err := store.GetAgentWakeup(ctx, wakeup.WakeupID)
	if err != nil {
		t.Fatal(err)
	}
	resumeID := receiptStreamEventID(loaded)
	wrapped := &noopAfterReceiptCursor{Store: store, db: ws.DB(), wakeupID: wakeup.WakeupID}
	frame := readNextReceiptStreamFrame(t, wrapped, threadID, resumeID, 30*time.Millisecond)
	if strings.Contains(frame, "event: notification_receipt") || strings.Contains(frame, resumeID) {
		t.Fatalf("accepted resume id was sent again: %q", frame)
	}
	if frame != ": keepalive\n\n" {
		t.Fatalf("expected a keepalive after the accepted resume, got %q", frame)
	}
}

type noopAfterReceiptCursor struct {
	*primitives.Store
	db       *sql.DB
	wakeupID string
	once     sync.Once
}

func TestReceiptStreamDoesNotEmitNewRestrictedTrigger(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	store := primitives.NewTestStore(ws.DB(), "")
	for _, q := range []string{
		`INSERT INTO threads(id,updated_at,updated_by,body_json) VALUES('public','now','owner','{}'),('private','now','other','{"pm_actor_id":"other"}')`,
		`INSERT INTO agent_wakeups(wakeup_id,status,notification_status,target_handle,target_actor_id,thread_id,refs_json,created_at,updated_at) VALUES('accepted','requested','unread','agent','target','public','[]','now','now')`,
	} {
		if _, err = ws.DB().Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	accepted, err := store.GetAgentWakeup(ctx, "accepted")
	if err != nil {
		t.Fatal(err)
	}
	wrapped := &restrictedAfterReceiptCursor{Store: store, db: ws.DB()}
	frame := readNextReceiptStreamFrame(t, wrapped, "public", receiptStreamEventID(accepted), 50*time.Millisecond)
	if frame != ": keepalive\n\n" {
		t.Fatalf("new restricted trigger emitted a payload or control: %q", frame)
	}
}

type restrictedAfterReceiptCursor struct {
	*primitives.Store
	db *sql.DB
}

func (s *restrictedAfterReceiptCursor) ReceiptStreamCursor(ctx context.Context, threadID, lastEventID string, accept func(primitives.AgentWakeup) bool) (primitives.ReceiptStreamCursor, error) {
	cursor, err := s.Store.ReceiptStreamCursor(ctx, threadID, lastEventID, accept)
	if err != nil {
		return cursor, err
	}
	_, err = s.db.Exec(`INSERT INTO events(id,type,ts,actor_id,thread_id,refs_json,payload_json) VALUES('secret-trigger','message_posted','now','owner','public','["thread:private"]','{}');
	 INSERT INTO agent_wakeups(wakeup_id,status,notification_status,target_handle,target_actor_id,thread_id,trigger_event_id,trigger_text,refs_json,created_at,updated_at) VALUES('secret-wake','requested','unread','agent','target','public','secret-trigger','secret-text','[]','now','now')`)
	return cursor, err
}

func (s *noopAfterReceiptCursor) ReceiptStreamCursor(ctx context.Context, threadID, lastEventID string, accept func(primitives.AgentWakeup) bool) (primitives.ReceiptStreamCursor, error) {
	cursor, err := s.Store.ReceiptStreamCursor(ctx, threadID, lastEventID, accept)
	if err != nil {
		return cursor, err
	}
	var execErr error
	s.once.Do(func() {
		_, execErr = s.db.ExecContext(ctx, `UPDATE agent_wakeups SET status=status WHERE wakeup_id=?`, s.wakeupID)
	})
	if execErr != nil {
		return cursor, execErr
	}
	return cursor, nil
}

// Capture protocol progress directly: setup/authorization must not spend a
// three-second HTTP-client deadline under the default parallel test workload.
// Cancellation on the second flush also requires the handler to join its scanner
// before returning; no wall-clock race decides whether the assertion runs.
type receiptFrameRecorder struct {
	*httptest.ResponseRecorder
	frames []string
	offset int
	cancel context.CancelFunc
}

func (w *receiptFrameRecorder) Flush() {
	w.ResponseRecorder.Flush()
	body := w.Body.String()
	if len(body) == w.offset {
		return
	} // Header-only flush.
	w.frames = append(w.frames, body[w.offset:])
	w.offset = len(body)
	if len(w.frames) >= 2 {
		w.cancel()
	}
}

func readNextReceiptStreamFrame(t *testing.T, store PrimitiveStore, threadID, resumeID string, poll time.Duration) string {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req := httptest.NewRequest(http.MethodGet, "/stream/agent-notification-receipts?thread_id="+threadID, nil)
	req = req.WithContext(primitives.WithAccessScope(ctx, primitives.AccessScope{ActorID: "owner"}))
	req.Header.Set("Last-Event-ID", resumeID)
	recorder := &receiptFrameRecorder{ResponseRecorder: httptest.NewRecorder(), cancel: cancel}
	handleAgentNotificationReceiptsStream(recorder, req, handlerOptions{primitiveStore: store, streamPollInterval: poll})
	if recorder.Code != http.StatusOK {
		t.Fatalf("status %d: %s", recorder.Code, recorder.Body.String())
	}
	if len(recorder.frames) < 2 {
		t.Fatalf("stream ended before second frame: %q", recorder.frames)
	}
	if recorder.frames[0] != ": keepalive\n\n" {
		t.Fatalf("initial frame: %q", recorder.frames[0])
	}
	return recorder.frames[1]
}
