package primitives

import (
	"context"
	"errors"
	"testing"
	"time"
)

func cardLeaseFixture(t *testing.T) (*Store, context.Context, string, *time.Time) {
	t.Helper()
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ws.Close() })
	s := NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	w, err := s.CreateWork(ctx, "lead", "", map[string]any{"title": "Batch ramp"})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 25, 9, 0, 0, 0, time.UTC)
	orig := cardLeaseNow
	cardLeaseNow = func() time.Time { return now }
	t.Cleanup(func() { cardLeaseNow = orig })
	return s, ctx, workString(w["id"]), &now
}

func wantLeaseError(t *testing.T, err error, code, owner string) {
	t.Helper()
	var leaseErr *CardLeaseError
	if !errors.As(err, &leaseErr) || leaseErr.Code != code || leaseErr.Lease.Owner != owner {
		t.Fatalf("want %s held by %q, got %#v (%v)", code, owner, leaseErr, err)
	}
}

func leaseEventTypes(t *testing.T, s *Store, ctx context.Context, cardID string) []string {
	t.Helper()
	card, err := s.GetBoardCard(ctx, "", cardID)
	if err != nil {
		t.Fatal(err)
	}
	events, err := s.ListEventsByThread(ctx, workString(card["thread_id"]))
	if err != nil {
		t.Fatal(err)
	}
	out := []string{}
	for _, e := range events {
		switch typ := workString(e["type"]); typ {
		case "card_lease_acquired", "card_lease_renewed", "card_lease_released", "card_lease_expired":
			out = append(out, typ)
		}
	}
	return out
}

func TestCardLeaseLifecycleAndFencing(t *testing.T) {
	s, ctx, cardID, now := cardLeaseFixture(t)

	lease, err := s.GetCardLease(ctx, cardID)
	if err != nil || lease.State != CardLeaseStateNone || lease.Epoch != 0 {
		t.Fatalf("fresh card lease=%+v err=%v", lease, err)
	}
	got, err := s.AcquireCardLease(ctx, "lead", cardID, CardLeaseAcquireInput{Owner: "exec-a", TTL: 2 * time.Hour, Reason: "ramp step 3"})
	if err != nil || !got.Changed || got.Lease.State != CardLeaseStateHeld || got.Lease.Epoch != 1 || got.Lease.Owner != "exec-a" || got.EventID == "" {
		t.Fatalf("acquire=%+v err=%v", got, err)
	}
	if got.Lease.RemainingSeconds != 7200 {
		t.Fatalf("remaining=%d", got.Lease.RemainingSeconds)
	}

	// Another owner is refused while the lease is live, and learns who holds it.
	_, err = s.AcquireCardLease(ctx, "peer", cardID, CardLeaseAcquireInput{Owner: "exec-b", TTL: time.Hour})
	wantLeaseError(t, err, CardLeaseErrHeld, "exec-a")
	_, err = s.RenewCardLease(ctx, "peer", cardID, CardLeaseRenewInput{Owner: "exec-b"})
	wantLeaseError(t, err, CardLeaseErrHeld, "exec-a")
	_, err = s.ReleaseCardLease(ctx, "peer", cardID, CardLeaseReleaseInput{Owner: "exec-b"})
	wantLeaseError(t, err, CardLeaseErrHeld, "exec-a")

	// The holder re-acquiring extends the lease in the same epoch.
	*now = now.Add(30 * time.Minute)
	got, err = s.AcquireCardLease(ctx, "lead", cardID, CardLeaseAcquireInput{Owner: "exec-a", TTL: 2 * time.Hour})
	if err != nil || got.Lease.Epoch != 1 || got.Lease.ExpiresAt != cardLeaseStamp(now.Add(2*time.Hour)) {
		t.Fatalf("re-acquire=%+v err=%v", got, err)
	}
	// Renew without a TTL keeps the lease's TTL.
	*now = now.Add(time.Hour)
	got, err = s.RenewCardLease(ctx, "lead", cardID, CardLeaseRenewInput{Owner: "exec-a"})
	if err != nil || got.Lease.Epoch != 1 || got.Lease.TTLSeconds != 7200 || got.Lease.ExpiresAt != cardLeaseStamp(now.Add(2*time.Hour)) {
		t.Fatalf("renew=%+v err=%v", got, err)
	}

	// Past expiry the lease reads expired, renew refuses, and a new owner
	// takes it with the next epoch.
	*now = now.Add(2*time.Hour + time.Second)
	if lease, _ = s.GetCardLease(ctx, cardID); lease.State != CardLeaseStateExpired {
		t.Fatalf("expired state=%+v", lease)
	}
	_, err = s.RenewCardLease(ctx, "lead", cardID, CardLeaseRenewInput{Owner: "exec-a"})
	wantLeaseError(t, err, CardLeaseErrExpired, "exec-a")
	got, err = s.AcquireCardLease(ctx, "peer", cardID, CardLeaseAcquireInput{Owner: "exec-b", TTL: time.Hour})
	if err != nil || got.Lease.Epoch != 2 || got.Lease.Owner != "exec-b" {
		t.Fatalf("takeover=%+v err=%v", got, err)
	}
	// The stale holder cannot release the new tenure.
	_, err = s.ReleaseCardLease(ctx, "lead", cardID, CardLeaseReleaseInput{Owner: "exec-a"})
	wantLeaseError(t, err, CardLeaseErrHeld, "exec-b")

	// if_epoch is a compare-and-swap on the epoch.
	stale := int64(1)
	if _, err = s.AcquireCardLease(ctx, "peer", cardID, CardLeaseAcquireInput{Owner: "exec-b", TTL: time.Hour, IfEpoch: &stale}); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale if_epoch err=%v", err)
	}

	got, err = s.ReleaseCardLease(ctx, "peer", cardID, CardLeaseReleaseInput{Owner: "exec-b", Reason: "merged"})
	if err != nil || !got.Changed || got.Lease.State != CardLeaseStateReleased {
		t.Fatalf("release=%+v err=%v", got, err)
	}
	got, err = s.ReleaseCardLease(ctx, "peer", cardID, CardLeaseReleaseInput{Owner: "exec-b"})
	if err != nil || got.Changed {
		t.Fatalf("second release must be a no-op: %+v err=%v", got, err)
	}
	_, err = s.RenewCardLease(ctx, "peer", cardID, CardLeaseRenewInput{Owner: "exec-b"})
	wantLeaseError(t, err, CardLeaseErrNotHeld, "exec-b")

	// A released lease is takeable at once, as a new tenure.
	got, err = s.AcquireCardLease(ctx, "peer", cardID, CardLeaseAcquireInput{Owner: "exec-b", TTL: time.Hour})
	if err != nil || got.Lease.Epoch != 3 {
		t.Fatalf("acquire after release=%+v err=%v", got, err)
	}

	want := []string{"card_lease_acquired", "card_lease_renewed", "card_lease_renewed", "card_lease_acquired", "card_lease_released", "card_lease_acquired"}
	if types := leaseEventTypes(t, s, ctx, cardID); len(types) != len(want) {
		t.Fatalf("timeline lease events=%v want %v", types, want)
	}
}

func TestCardLeaseValidation(t *testing.T) {
	s, ctx, cardID, _ := cardLeaseFixture(t)
	for _, in := range []CardLeaseAcquireInput{
		{Owner: "", TTL: time.Hour},
		{Owner: "-leading-dash", TTL: time.Hour},
		{Owner: "has space", TTL: time.Hour},
		{Owner: "exec-a", TTL: 59 * time.Second},
		{Owner: "exec-a", TTL: 8 * 24 * time.Hour},
		{Owner: "exec-a", TTL: time.Hour, Reason: "two\nlines"},
	} {
		if _, err := s.AcquireCardLease(ctx, "lead", cardID, in); !errors.Is(err, ErrInvalidWorkRequest) {
			t.Fatalf("input %+v err=%v", in, err)
		}
	}
	if _, err := s.AcquireCardLease(ctx, "lead", "missing-card", CardLeaseAcquireInput{Owner: "exec-a", TTL: time.Hour}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing card err=%v", err)
	}
}

func TestCardLeaseExpiryClockRecordsOncePerEpoch(t *testing.T) {
	s, ctx, cardID, now := cardLeaseFixture(t)
	if _, err := s.AcquireCardLease(ctx, "lead", cardID, CardLeaseAcquireInput{Owner: "exec-a", TTL: time.Hour}); err != nil {
		t.Fatal(err)
	}
	if expired, err := s.ExpireDueCardLeases(ctx, "system", 10); err != nil || len(expired) != 0 {
		t.Fatalf("early sweep=%v err=%v", expired, err)
	}
	*now = now.Add(time.Hour)
	expired, err := s.ExpireDueCardLeases(ctx, "system", 10)
	if err != nil || len(expired) != 1 || expired[0].Owner != "exec-a" || expired[0].Epoch != 1 || expired[0].EventID == "" {
		t.Fatalf("sweep=%+v err=%v", expired, err)
	}
	if again, err := s.ExpireDueCardLeases(ctx, "system", 10); err != nil || len(again) != 0 {
		t.Fatalf("expiry must record once per epoch: %+v err=%v", again, err)
	}
	// A new tenure is swept again when it, too, expires.
	if _, err := s.AcquireCardLease(ctx, "peer", cardID, CardLeaseAcquireInput{Owner: "exec-b", TTL: time.Minute}); err != nil {
		t.Fatal(err)
	}
	*now = now.Add(2 * time.Minute)
	if expired, err = s.ExpireDueCardLeases(ctx, "system", 10); err != nil || len(expired) != 1 || expired[0].Epoch != 2 {
		t.Fatalf("second tenure sweep=%+v err=%v", expired, err)
	}
	// A released lease is never reported as expired.
	if _, err := s.AcquireCardLease(ctx, "peer", cardID, CardLeaseAcquireInput{Owner: "exec-c", TTL: time.Minute}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReleaseCardLease(ctx, "peer", cardID, CardLeaseReleaseInput{Owner: "exec-c"}); err != nil {
		t.Fatal(err)
	}
	*now = now.Add(2 * time.Minute)
	if expired, err = s.ExpireDueCardLeases(ctx, "system", 10); err != nil || len(expired) != 0 {
		t.Fatalf("released lease swept=%+v err=%v", expired, err)
	}
	types := leaseEventTypes(t, s, ctx, cardID)
	expiredCount := 0
	for _, typ := range types {
		if typ == "card_lease_expired" {
			expiredCount++
		}
	}
	if expiredCount != 2 {
		t.Fatalf("timeline=%v", types)
	}
}
