package auth

import (
	"context"
	"testing"
	"time"

	"organization-autorunner-core/internal/storage"
)

func TestListAuditEventsOrdersSameSecondByFixedWidthSortKey(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	workspace, err := storage.InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatalf("initialize workspace: %v", err)
	}
	defer workspace.Close()

	store := NewStore(workspace.DB())
	firstID := recordAuthAuditEventForTest(t, ctx, store, AuthAuditEventInput{
		EventType:  AuthAuditEventInviteCreated,
		OccurredAt: time.Date(2026, 3, 20, 10, 0, 0, 100_000_000, time.UTC),
	})
	secondID := recordAuthAuditEventForTest(t, ctx, store, AuthAuditEventInput{
		EventType:  AuthAuditEventInviteConsumed,
		OccurredAt: time.Date(2026, 3, 20, 10, 0, 0, 0, time.UTC),
	})

	events, nextCursor, err := store.ListAuditEvents(ctx, AuthAuditListFilter{})
	if err != nil {
		t.Fatalf("list audit events: %v", err)
	}
	if nextCursor != "" {
		t.Fatalf("expected empty nextCursor without limit, got %q", nextCursor)
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 audit events, got %#v", events)
	}
	if events[0].EventID != firstID {
		t.Fatalf("expected fixed-width sort key to keep fractional timestamp first, got %#v", events)
	}
	if events[1].EventID != secondID {
		t.Fatalf("expected whole-second timestamp second, got %#v", events)
	}
}

func TestListAuditEventsUsesKeysetCursorAcrossNewerInsert(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	workspace, err := storage.InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatalf("initialize workspace: %v", err)
	}
	defer workspace.Close()

	store := NewStore(workspace.DB())
	oldestID := recordAuthAuditEventForTest(t, ctx, store, AuthAuditEventInput{
		EventType:  AuthAuditEventBootstrapConsumed,
		OccurredAt: time.Date(2026, 3, 20, 10, 0, 0, 0, time.UTC),
	})
	newerID := recordAuthAuditEventForTest(t, ctx, store, AuthAuditEventInput{
		EventType:  AuthAuditEventPrincipalRegistered,
		OccurredAt: time.Date(2026, 3, 20, 10, 0, 1, 0, time.UTC),
	})

	firstPage, nextCursor, err := store.ListAuditEvents(ctx, AuthAuditListFilter{Limit: ptrInt(1)})
	if err != nil {
		t.Fatalf("list first audit page: %v", err)
	}
	if len(firstPage) != 1 {
		t.Fatalf("expected one event on first page, got %#v", firstPage)
	}
	if firstPage[0].EventID != newerID {
		t.Fatalf("expected newer event on first page, got %#v", firstPage)
	}
	if nextCursor == "" {
		t.Fatal("expected next cursor from first page")
	}

	insertedTopID := recordAuthAuditEventForTest(t, ctx, store, AuthAuditEventInput{
		EventType:  AuthAuditEventInviteCreated,
		OccurredAt: time.Date(2026, 3, 20, 10, 0, 2, 0, time.UTC),
	})

	secondPage, finalCursor, err := store.ListAuditEvents(ctx, AuthAuditListFilter{
		Limit:  ptrInt(1),
		Cursor: nextCursor,
	})
	if err != nil {
		t.Fatalf("list second audit page: %v", err)
	}
	if len(secondPage) != 1 {
		t.Fatalf("expected one event on second page, got %#v", secondPage)
	}
	if secondPage[0].EventID != oldestID {
		t.Fatalf("expected keyset cursor to continue with the older event, got %#v", secondPage)
	}
	if secondPage[0].EventID == newerID || secondPage[0].EventID == insertedTopID {
		t.Fatalf("expected no duplicate or shifted page after insert, got %#v", secondPage)
	}
	if finalCursor != "" {
		t.Fatalf("expected pagination to finish after second page, got %q", finalCursor)
	}
}

func recordAuthAuditEventForTest(t *testing.T, ctx context.Context, store *Store, input AuthAuditEventInput) string {
	t.Helper()

	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin auth audit tx: %v", err)
	}
	if err := store.recordAuthAuditEventTx(ctx, tx, input); err != nil {
		_ = tx.Rollback()
		t.Fatalf("record auth audit event: %v", err)
	}

	occurredAtText := input.OccurredAt.UTC().Format(time.RFC3339Nano)
	var eventID string
	if err := tx.QueryRowContext(
		ctx,
		`SELECT id
		 FROM auth_audit_events
		 WHERE event_type = ? AND occurred_at = ?
		 ORDER BY id DESC
		 LIMIT 1`,
		input.EventType,
		occurredAtText,
	).Scan(&eventID); err != nil {
		_ = tx.Rollback()
		t.Fatalf("load inserted auth audit id: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit auth audit tx: %v", err)
	}
	return eventID
}

func ptrInt(value int) *int {
	return &value
}
