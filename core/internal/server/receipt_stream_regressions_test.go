package server

import (
	"bufio"
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/storage"
)

func TestReceiptStreamReconnectDeliversOfflineClaim(t *testing.T) {
	ctx := context.Background()
	ws, err := storage.InitializeWorkspace(ctx, t.TempDir())
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
	ctx := context.Background()
	ws, err := storage.InitializeWorkspace(ctx, t.TempDir())
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

func readNextReceiptStreamFrame(t *testing.T, store PrimitiveStore, threadID, resumeID string, poll time.Duration) string {
	t.Helper()
	done := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(done)
		scoped := r.WithContext(primitives.WithAccessScope(r.Context(), primitives.AccessScope{ActorID: "owner"}))
		handleAgentNotificationReceiptsStream(w, scoped, handlerOptions{primitiveStore: store, streamPollInterval: poll})
	}))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/stream/agent-notification-receipts?thread_id="+threadID, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Last-Event-ID", resumeID)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	reader := bufio.NewReader(resp.Body)
	if frame := round4SSEFrame(t, reader); frame != ": keepalive\n\n" {
		t.Fatalf("initial frame: %q", frame)
	}
	frame := round4SSEFrame(t, reader)
	cancel()
	resp.Body.Close()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("cancelled handler did not join scanner")
	}
	return frame
}
