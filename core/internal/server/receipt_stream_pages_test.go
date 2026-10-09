package server

import (
	"bufio"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/schema"
)

func TestReceiptStreamChunksContinueWithoutPollWait(t *testing.T) {
	t.Parallel()
	for _, fixture := range []struct{ hidden, visible int }{{50, 1}, {0, 2}, {10, 2}} {
		t.Run(fmt.Sprintf("hidden-%d-visible-%d", fixture.hidden, fixture.visible), func(t *testing.T) {
			s := &receiptTickStore{calls: make(chan int, 80), hiddenPages: fixture.hidden, visiblePages: fixture.visible}
			done := make(chan struct{})
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				defer close(done)
				handleAgentNotificationReceiptsStream(w, r, handlerOptions{primitiveStore: s, streamPollInterval: time.Hour})
			}))
			defer srv.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/stream/agent-notification-receipts?thread_id=thread", nil)
			if err != nil {
				t.Fatal(err)
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			reader := bufio.NewReader(resp.Body)
			if frame := round4SSEFrame(t, reader); frame != ": keepalive\n\n" {
				t.Fatalf("initial frame: %q", frame)
			}
			for i := 1; i <= fixture.visible; i++ {
				frame := round4SSEFrame(t, reader)
				if strings.Contains(frame, "event: resume") || strings.Contains(frame, "hidden-wake") || !strings.Contains(frame, "event: notification_receipt\n") {
					t.Fatalf("catch-up emitted a control or a hidden id: %q", frame)
				}
				if !strings.Contains(frame, fmt.Sprintf("wake-%d", i)) {
					t.Fatalf("lost visible receipt %d: %q", i, frame)
				}
			}
			cancel()
			resp.Body.Close()
			select {
			case <-done:
			case <-time.After(2 * time.Second):
				t.Fatal("cancelled handler did not join scanner")
			}
			if got := len(s.calls); got != fixture.hidden+fixture.visible {
				t.Fatalf("read %d pages, want %d", got, fixture.hidden+fixture.visible)
			}
		})
	}
}

func TestReceiptStreamKeepalivesContinueDuringHiddenScan(t *testing.T) {
	t.Parallel()
	s := &receiptTickStore{calls: make(chan int, 20), gate: make(chan struct{})}
	done := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(done)
		handleAgentNotificationReceiptsStream(w, r, handlerOptions{primitiveStore: s, streamPollInterval: 20 * time.Millisecond})
	}))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/stream/agent-notification-receipts?thread_id=thread", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	select {
	case <-s.calls:
	case <-ctx.Done():
		t.Fatal("scanner did not start")
	}
	reader := bufio.NewReader(resp.Body)
	for i := 0; i < 4; i++ {
		if frame := round4SSEFrame(t, reader); frame != ": keepalive\n\n" {
			t.Fatalf("hidden activity frame: %q", frame)
		}
	}
	select {
	case <-s.calls:
		t.Fatal("overlapping scan request")
	default:
	}
	cancel()
	resp.Body.Close()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("cancelled handler did not join scanner")
	}
}

func TestReceiptStreamPrivateUnknownAndTrashedResumeIDsMatch(t *testing.T) {
	t.Parallel()
	requireIntegrationTest(t)
	env := newAuthIntegrationEnv(t, authIntegrationOptions{})
	ctx := context.Background()
	owner := seedHumanPrincipalForLockoutTest(t, ctx, env.workspace.DB(), "receipt-page-owner", "receipt-page-owner-actor", "receipt-page-owner", "receipt-page-owner-token")
	reader := seedHumanPrincipalForLockoutTest(t, ctx, env.workspace.DB(), "receipt-page-reader", "receipt-page-reader-actor", "receipt-page-reader", "receipt-page-reader-token")
	store := env.primitiveStore.(*primitives.Store)
	public := seedStreamPrivacyThread(t, store, owner.ActorID, false)
	private := seedStreamPrivacyThread(t, store, owner.ActorID, true)
	privateEvent, err := store.AppendEvent(ctx, owner.ActorID, map[string]any{
		"type": "message_posted", "thread_id": private, "refs": []string{"thread:" + private},
		"payload": map[string]any{"text": "private trigger"},
	})
	if err != nil {
		t.Fatal(err)
	}
	trashedEvent, err := store.AppendEvent(ctx, owner.ActorID, map[string]any{
		"type": "message_posted", "thread_id": public, "refs": []string{"thread:" + public},
		"payload": map[string]any{"text": "trashed trigger"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.TrashEvent(ctx, owner.ActorID, anyString(trashedEvent["id"]), "gone"); err != nil {
		t.Fatal(err)
	}
	visible := receiptPageWakeup("wake-visible", public, "2026-01-01T00:00:03Z", "")
	hidden := receiptPageWakeup("wake-private", public, "2026-01-01T00:00:01Z", anyString(privateEvent["id"]))
	trashed := receiptPageWakeup("wake-trashed", public, "2026-01-01T00:00:02Z", anyString(trashedEvent["id"]))
	for _, wakeup := range []primitives.AgentWakeup{hidden, trashed, visible} {
		seedReceiptStreamWakeup(t, store, wakeup)
	}
	srv := receiptPrivacyHTTPServer(t, env, 20*time.Millisecond)
	url := srv.URL + "/stream/agent-notification-receipts?thread_id=" + public
	for _, header := range []bool{true, false} {
		want := readQuietReceiptStream(t, url, reader.AccessToken, "receipt:missing@abcdef01", header)
		for _, id := range []string{receiptStreamEventID(hidden), receiptStreamEventID(trashed)} {
			if got := readQuietReceiptStream(t, url, reader.AccessToken, id, header); got != want {
				t.Fatalf("header=%v id %s differs from unknown:\n%q\n%q", header, id, got, want)
			}
		}
	}
}

func TestReceiptStreamHiddenBacklogDoesNotWaitOnThePollTimer(t *testing.T) {
	t.Parallel()
	requireIntegrationTest(t)
	// One chunk examines 2000 candidates. 2001 hidden rows force a second chunk.
	// The poll interval is one hour, so any timer wait between those chunks misses
	// this deadline. The allowance covers the scoped page reads themselves.
	const hidden = 2001
	env := newAuthIntegrationEnv(t, authIntegrationOptions{})
	ctx := context.Background()
	owner := seedHumanPrincipalForLockoutTest(t, ctx, env.workspace.DB(), "receipt-latency-owner", "receipt-latency-owner-actor", "receipt-latency-owner", "receipt-latency-owner-token")
	reader := seedHumanPrincipalForLockoutTest(t, ctx, env.workspace.DB(), "receipt-latency-reader", "receipt-latency-reader-actor", "receipt-latency-reader", "receipt-latency-reader-token")
	store := env.primitiveStore.(*primitives.Store)
	public := seedStreamPrivacyThread(t, store, owner.ActorID, false)
	private := seedStreamPrivacyThread(t, store, owner.ActorID, true)
	privateEvent, err := store.AppendEvent(ctx, owner.ActorID, map[string]any{
		"type": "message_posted", "thread_id": private, "refs": []string{"thread:" + private},
		"payload": map[string]any{"text": "hidden backlog"},
	})
	if err != nil {
		t.Fatal(err)
	}
	tx, err := env.workspace.DB().BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	for start := 1; start <= hidden; start += 500 {
		end := start + 499
		if end > hidden {
			end = hidden
		}
		if _, err = tx.ExecContext(ctx, `WITH RECURSIVE n(i) AS (VALUES(?) UNION ALL SELECT i+1 FROM n WHERE i<?)
			INSERT INTO agent_wakeups(
				wakeup_id,status,notification_status,target_handle,target_actor_id,thread_id,trigger_event_id,
				trigger_text,refs_json,created_at,updated_at)
			SELECT printf('hidden-%06d',i),'requested','unread','target.agent','actor-target',?,?,'hidden','[]',
				printf('2026-01-01T%02d:%02d:%02dZ', i/3600, (i/60)%60, i%60), printf('2026-01-01T%02d:%02d:%02dZ', i/3600, (i/60)%60, i%60)
			FROM n`, start, end, public, anyString(privateEvent["id"])); err != nil {
			tx.Rollback()
			t.Fatal(err)
		}
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	visible := receiptPageWakeup("wake-visible", public, "2026-01-02T00:00:00Z", "")
	visible.TriggerText = "visible receipt"
	seedReceiptStreamWakeup(t, store, visible)
	srv := receiptPrivacyHTTPServer(t, env, time.Hour)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/stream/agent-notification-receipts?thread_id="+public, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+reader.AccessToken)
	started := time.Now()
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	readerBody := bufio.NewReader(resp.Body)
	for {
		frame := round4SSEFrame(t, readerBody)
		if frame == ": keepalive\n\n" {
			continue
		}
		if strings.Contains(frame, "hidden-") || strings.Contains(frame, "event: resume") || !strings.Contains(frame, "wake-visible") {
			t.Fatalf("hidden backlog emitted a control or a hidden id: %q", frame)
		}
		break
	}
	if elapsed := time.Since(started); elapsed > 15*time.Second {
		t.Fatalf("hidden backlog waited on the poll timer: %s", elapsed)
	}
}

type receiptTickStore struct {
	PrimitiveStore
	calls        chan int
	gate         <-chan struct{}
	hiddenPages  int
	visiblePages int
	pages        int
}

func (s *receiptTickStore) ResolveResourceRef(context.Context, primitives.ResourceRefInput) (primitives.ResolvedResourceRef, error) {
	return primitives.ResolvedResourceRef{Type: "thread", ID: "thread", Handle: "thread", CanonicalRef: "thread:thread"}, nil
}

func (s *receiptTickStore) GetThread(context.Context, string) (map[string]any, error) {
	return map[string]any{"id": "thread", "title": "thread"}, nil
}

func (s *receiptTickStore) ReceiptStreamCursor(context.Context, string, string, func(primitives.AgentWakeup) bool) (primitives.ReceiptStreamCursor, error) {
	return primitives.ReceiptStreamCursor{Snapshot: true}, nil
}

func (s *receiptTickStore) ListReceiptStreamPage(ctx context.Context, threadID string, cursor primitives.ReceiptStreamCursor) (primitives.ReceiptStreamPage, error) {
	select {
	case s.calls <- 1:
	case <-ctx.Done():
		return primitives.ReceiptStreamPage{}, ctx.Err()
	}
	s.pages++
	if s.gate != nil {
		select {
		case <-s.gate:
		case <-ctx.Done():
			return primitives.ReceiptStreamPage{}, ctx.Err()
		}
	}
	if s.pages > s.hiddenPages && (s.hiddenPages > 0 || s.visiblePages > 0) {
		visible := s.pages - s.hiddenPages
		return primitives.ReceiptStreamPage{
			Wakeups: []primitives.AgentWakeup{receiptPageWakeup(fmt.Sprintf("wake-%d", visible), threadID, "2026-01-01T00:00:00Z", "")},
			Cursor:  cursor,
			HasMore: visible < s.visiblePages,
		}, nil
	}
	return primitives.ReceiptStreamPage{Cursor: cursor, HasMore: true}, nil
}

func receiptPrivacyHTTPServer(t *testing.T, env authIntegrationEnv, interval time.Duration) *httptest.Server {
	t.Helper()
	contract, err := schema.Load("../../../contracts/anx-schema.yaml")
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(NewHandler("test", WithActorRegistry(env.registry), WithAuthStore(env.authStore), WithPrimitiveStore(env.primitiveStore), WithSchemaContract(contract), WithWorkspaceID("ws_main"), WithStreamPollInterval(interval)))
	t.Cleanup(srv.Close)
	return srv
}

func readQuietReceiptStream(t *testing.T, url, token, id string, header bool) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if !header {
		url += "&last_event_id=" + id
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if header {
		req.Header.Set("Last-Event-ID", id)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	reader := bufio.NewReader(resp.Body)
	var wire strings.Builder
	for i := 0; i < 3; i++ {
		frame := round4SSEFrame(t, reader)
		if frame != ": keepalive\n\n" {
			t.Fatalf("quiet stream disclosed activity: %q", frame)
		}
		wire.WriteString(frame)
	}
	return wire.String()
}

func receiptPageWakeup(id, threadID, createdAt, triggerEventID string) primitives.AgentWakeup {
	return primitives.AgentWakeup{
		WakeupID: id, Status: primitives.AgentWakeupStatusRequested, TargetHandle: "target.agent",
		TargetActorID: "actor-target", ThreadID: threadID, TriggerEventID: triggerEventID,
		CreatedAt: createdAt, UpdatedAt: createdAt, Refs: []string{"thread:" + threadID},
	}
}
