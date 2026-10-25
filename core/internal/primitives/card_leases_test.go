package primitives

import (
	"context"
	"errors"
	"strings"
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

func TestCardLeaseRenewAfterDefaultReminderPreservesDigest(t *testing.T) {
	for _, method := range []string{"renew", "reacquire"} {
		t.Run(method, func(t *testing.T) {
			s, ctx, cardID, now := cardLeaseFixture(t)
			if _, err := s.AcquireCardLease(ctx, "lead", cardID, CardLeaseAcquireInput{Owner: "actor:lead", TTL: 24 * time.Hour}); err != nil {
				t.Fatal(err)
			}
			// The card's default lapses while its 24-hour lease is still held.
			defaultAt := cardLeaseStamp(now.Add(-time.Hour))
			if _, err := s.db.ExecContext(ctx, `UPDATE work_tracking SET default_at=? WHERE card_id=?`, defaultAt, cardID); err != nil {
				t.Fatal(err)
			}
			if err := s.MaintainWorkTracking(ctx, *now); err != nil {
				t.Fatal(err)
			}
			var reminder, due string
			if err := s.db.QueryRowContext(ctx, `SELECT reminder_event_id,next_digest_at FROM work_tracking WHERE card_id=?`, cardID).Scan(&reminder, &due); err != nil || reminder == "" || due == "" {
				t.Fatalf("missing initial reminder/digest: %q %q %v", reminder, due, err)
			}
			if method == "renew" {
				_, err := s.RenewCardLease(ctx, "lead", cardID, CardLeaseRenewInput{Owner: "actor:lead"})
				if err != nil {
					t.Fatal(err)
				}
			} else if _, err := s.AcquireCardLease(ctx, "lead", cardID, CardLeaseAcquireInput{Owner: "actor:lead", TTL: 24 * time.Hour}); err != nil {
				t.Fatal(err)
			}
			var gotDefault, gotReminder, gotDue, owner string
			if err := s.db.QueryRowContext(ctx, `SELECT default_at,reminder_event_id,next_digest_at,fallback_owner FROM work_tracking WHERE card_id=?`, cardID).Scan(&gotDefault, &gotReminder, &gotDue, &owner); err != nil || gotDefault != defaultAt || gotReminder != reminder || gotDue != due || owner != "actor:lead" {
				t.Fatalf("lease heartbeat dropped default routing: default=%q reminder=%q due=%q owner=%q err=%v", gotDefault, gotReminder, gotDue, owner, err)
			}
			digest, err := s.TrackingDigestPreview(ctx, "lead")
			if err != nil || len(digest) != 1 || digest[0]["reason"] != "default_promise_lapsed" {
				t.Fatalf("renewed default disappeared from brief digest: %+v %v", digest, err)
			}
			if err := s.MaintainWorkTracking(ctx, now.Add(time.Minute)); err != nil {
				t.Fatal(err)
			}
			var count int
			if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM events WHERE type='message_posted' AND json_extract(payload_json,'$.payload.source')='default_promise'`).Scan(&count); err != nil || count != 1 {
				t.Fatalf("default reminded again: %d %v", count, err)
			}
		})
	}
}

func TestCardLeaseAcquireAndRenewRepointAssignee(t *testing.T) {
	s, ctx, cardID, _ := cardLeaseFixture(t)
	for _, method := range []string{"acquire", "renew"} {
		if _, err := s.db.ExecContext(ctx, `UPDATE cards SET assignee='actor:peer' WHERE id=?`, cardID); err != nil {
			t.Fatal(err)
		}
		if method == "acquire" {
			if _, err := s.AcquireCardLease(ctx, "lead", cardID, CardLeaseAcquireInput{Owner: "actor:lead", TTL: 24 * time.Hour}); err != nil {
				t.Fatal(err)
			}
		} else if _, err := s.RenewCardLease(ctx, "lead", cardID, CardLeaseRenewInput{Owner: "actor:lead"}); err != nil {
			t.Fatal(err)
		}
		var owner string
		if err := s.db.QueryRowContext(ctx, `SELECT assignee FROM cards WHERE id=?`, cardID).Scan(&owner); err != nil || owner != "actor:lead" {
			t.Fatalf("%s did not re-point assignee to the lease actor: %q %v", method, owner, err)
		}
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

// setCardLeaseColumn moves the card's board placement directly, which is all
// cardLeaseLiveWork reads.
func setCardLeaseColumn(t *testing.T, s *Store, ctx context.Context, cardID, column string) {
	t.Helper()
	res, err := s.db.ExecContext(ctx, `UPDATE ref_edges SET metadata_json=json_set(COALESCE(metadata_json,'{}'),'$.column_key',?) WHERE source_type='board' AND target_id=?`, column, cardID)
	if err != nil {
		t.Fatal(err)
	}
	if n, _ := res.RowsAffected(); n != 1 {
		t.Fatalf("card placement rows=%d", n)
	}
}

func TestCardLeaseHandoffTransfersAtomically(t *testing.T) {
	s, ctx, cardID, now := cardLeaseFixture(t)
	setCardLeaseColumn(t, s, ctx, cardID, "in_progress")
	// Nothing to hand off yet.
	_, err := s.ReleaseCardLease(ctx, "lead", cardID, CardLeaseReleaseInput{Owner: "exec-a", HandoffTo: "exec-b"})
	wantLeaseError(t, err, CardLeaseErrNotHeld, "")
	if _, err = s.AcquireCardLease(ctx, "lead", cardID, CardLeaseAcquireInput{Owner: "exec-a", TTL: 2 * time.Hour}); err != nil {
		t.Fatal(err)
	}
	for _, in := range []CardLeaseReleaseInput{
		{Owner: "exec-a", HandoffTo: "exec-a"},
		{Owner: "exec-a", HandoffTo: "has space"},
		{Owner: "exec-a", HandoffTo: "exec-b", HandoffTTL: 30 * time.Second},
		{Owner: "exec-a", HandoffTTL: time.Hour},
	} {
		if _, err = s.ReleaseCardLease(ctx, "lead", cardID, in); !errors.Is(err, ErrInvalidWorkRequest) {
			t.Fatalf("input %+v err=%v", in, err)
		}
	}
	// Only the holder can hand off.
	_, err = s.ReleaseCardLease(ctx, "peer", cardID, CardLeaseReleaseInput{Owner: "exec-x", HandoffTo: "exec-b"})
	wantLeaseError(t, err, CardLeaseErrHeld, "exec-a")

	*now = now.Add(10 * time.Minute)
	got, err := s.ReleaseCardLease(ctx, "lead", cardID, CardLeaseReleaseInput{Owner: "exec-a", HandoffTo: "exec-b", Reason: "lane rotation"})
	if err != nil || !got.Changed || got.Lease.State != CardLeaseStateHeld || got.Lease.Owner != "exec-b" || got.Lease.Epoch != 2 || got.Lease.TTLSeconds != 7200 || got.Lease.ExpiresAt != cardLeaseStamp(now.Add(2*time.Hour)) {
		t.Fatalf("handoff=%+v err=%v", got, err)
	}
	// The old owner is fenced out; the new one renews normally.
	_, err = s.RenewCardLease(ctx, "lead", cardID, CardLeaseRenewInput{Owner: "exec-a"})
	wantLeaseError(t, err, CardLeaseErrHeld, "exec-b")
	if _, err = s.RenewCardLease(ctx, "peer", cardID, CardLeaseRenewInput{Owner: "exec-b"}); err != nil {
		t.Fatal(err)
	}
	// A hand-off never leaves a pending card_unowned wake.
	if pending, err := s.TakeUnownedCardLeases(ctx, 10); err != nil || len(pending) != 0 {
		t.Fatalf("handoff left unowned=%+v err=%v", pending, err)
	}
	// The holder may hand off its own expired lease nobody took, with a TTL.
	*now = now.Add(3 * time.Hour)
	got, err = s.ReleaseCardLease(ctx, "peer", cardID, CardLeaseReleaseInput{Owner: "exec-b", HandoffTo: "exec-c", HandoffTTL: time.Hour})
	if err != nil || got.Lease.Owner != "exec-c" || got.Lease.Epoch != 3 || got.Lease.TTLSeconds != 3600 {
		t.Fatalf("expired handoff=%+v err=%v", got, err)
	}
	want := []string{"card_lease_acquired", "card_lease_released", "card_lease_acquired", "card_lease_renewed", "card_lease_released", "card_lease_acquired"}
	if types := leaseEventTypes(t, s, ctx, cardID); strings.Join(types, ",") != strings.Join(want, ",") {
		t.Fatalf("timeline=%v want %v", types, want)
	}
}

func TestCardLeaseReleaseRoutesFallbackDigestOnce(t *testing.T) {
	s, ctx, cardID, now := cardLeaseFixture(t)
	acquire := func(owner string) {
		t.Helper()
		if _, err := s.AcquireCardLease(ctx, "lead", cardID, CardLeaseAcquireInput{Owner: owner, TTL: time.Hour}); err != nil {
			t.Fatal(err)
		}
	}
	// An open backlog card also retains fallback responsibility.
	acquire("exec-a")
	if _, err := s.ReleaseCardLease(ctx, "lead", cardID, CardLeaseReleaseInput{Owner: "exec-a"}); err != nil {
		t.Fatal(err)
	}
	if pending, err := s.TakeUnownedCardLeases(ctx, 10); err != nil || len(pending) != 0 {
		t.Fatalf("idle card unowned=%+v err=%v", pending, err)
	}

	// An open --next promise makes it live.
	if _, err := s.db.ExecContext(ctx, `INSERT INTO brief_initiatives(card_id,open,promise_at) VALUES(?,1,'2026-10-25T12:00:00Z') ON CONFLICT(card_id) DO UPDATE SET open=1,promise_at=excluded.promise_at`, cardID); err != nil {
		t.Fatal(err)
	}
	acquire("exec-b")
	got, err := s.ReleaseCardLease(ctx, "lead", cardID, CardLeaseReleaseInput{Owner: "exec-b"})
	if err != nil || !got.Changed {
		t.Fatalf("release=%+v err=%v", got, err)
	}
	pending, err := s.TakeUnownedCardLeases(ctx, 10)
	if err != nil || len(pending) != 0 {
		t.Fatalf("unowned=%+v err=%v", pending, err)
	}
	if again, err := s.TakeUnownedCardLeases(ctx, 10); err != nil || len(again) != 0 {
		t.Fatalf("card_unowned must be taken once: %+v err=%v", again, err)
	}

	// A new tenure before the clock runs cancels the pending wake.
	setCardLeaseColumn(t, s, ctx, cardID, "in_progress")
	acquire("exec-c")
	if _, err = s.ReleaseCardLease(ctx, "lead", cardID, CardLeaseReleaseInput{Owner: "exec-c"}); err != nil {
		t.Fatal(err)
	}
	acquire("exec-d")
	if pending, err = s.TakeUnownedCardLeases(ctx, 10); err != nil || len(pending) != 0 {
		t.Fatalf("re-acquired card unowned=%+v err=%v", pending, err)
	}
	// A card that left live work before the clock ran is stamped silently.
	if _, err = s.ReleaseCardLease(ctx, "lead", cardID, CardLeaseReleaseInput{Owner: "exec-d"}); err != nil {
		t.Fatal(err)
	}
	setCardLeaseColumn(t, s, ctx, cardID, "done")
	if pending, err = s.TakeUnownedCardLeases(ctx, 10); err != nil || len(pending) != 0 {
		t.Fatalf("finished card unowned=%+v err=%v", pending, err)
	}
	var stale int
	if err = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM card_leases WHERE unowned_event_id<>'' AND unowned_woken_at=''`).Scan(&stale); err != nil || stale != 0 {
		t.Fatalf("pending rows=%d err=%v", stale, err)
	}

	// Expiry routes the responsibility to the daily digest, with no immediate wake.
	setCardLeaseColumn(t, s, ctx, cardID, "in_progress")
	acquire("exec-e")
	*now = now.Add(2 * time.Hour)
	expired, err := s.ExpireDueCardLeases(ctx, "system", 10)
	if err != nil || len(expired) != 1 || !expired[0].DigestOnly || expired[0].Released {
		t.Fatalf("expiry=%+v err=%v", expired, err)
	}
	// The expired owner closing its lease afterwards does not wake twice.
	if _, err = s.ReleaseCardLease(ctx, "lead", cardID, CardLeaseReleaseInput{Owner: "exec-e"}); err != nil {
		t.Fatal(err)
	}
	if pending, err = s.TakeUnownedCardLeases(ctx, 10); err != nil || len(pending) != 0 {
		t.Fatalf("post-expiry release unowned=%+v err=%v", pending, err)
	}
}

func TestCardLeaseFallbackDoesNotWaitForBriefFold(t *testing.T) {
	s, ctx, cardID, now := cardLeaseFixture(t)
	s.SetTrackingLead(func() string { return "fallback-lead" })
	if _, err := s.db.ExecContext(ctx, `DELETE FROM brief_initiatives WHERE card_id=?`, cardID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO brief_dirty(card_id) VALUES(?) ON CONFLICT DO NOTHING`, cardID); err != nil {
		t.Fatal(err)
	}
	for _, how := range []string{"release", "expiry"} {
		if _, err := s.AcquireCardLease(ctx, "lead", cardID, CardLeaseAcquireInput{Owner: "exec-a", TTL: time.Hour}); err != nil {
			t.Fatal(err)
		}
		if how == "release" {
			if _, err := s.ReleaseCardLease(ctx, "lead", cardID, CardLeaseReleaseInput{Owner: "exec-a"}); err != nil {
				t.Fatal(err)
			}
		} else {
			*now = now.Add(2 * time.Hour)
			expired, err := s.ExpireDueCardLeases(ctx, "system", 10)
			if err != nil || len(expired) != 1 || !expired[0].DigestOnly {
				t.Fatal(expired, err)
			}
		}
		work, err := s.GetWork(ctx, cardID)
		if err != nil || work["owner"] != "actor:fallback-lead" {
			t.Fatal(work, err)
		}
		pending, err := s.TakeUnownedCardLeases(ctx, 10)
		if err != nil || len(pending) != 0 {
			t.Fatal(pending, err)
		}
		var owner, due string
		if err = s.db.QueryRowContext(ctx, `SELECT fallback_owner,next_digest_at FROM work_tracking WHERE card_id=?`, cardID).Scan(&owner, &due); err != nil || owner != "actor:fallback-lead" || due == "" {
			t.Fatal(owner, due, err)
		}
	}
}

func TestCardLeaseTenureEndedTracksNewTenure(t *testing.T) {
	s, ctx, cardID, now := cardLeaseFixture(t)
	ended := func(epoch int64) bool {
		t.Helper()
		ok, err := s.CardLeaseTenureEnded(ctx, cardID, epoch)
		if err != nil {
			t.Fatal(err)
		}
		return ok
	}
	if ended(1) {
		t.Fatal("a card with no lease reported an ended tenure")
	}
	if _, err := s.AcquireCardLease(ctx, "lead", cardID, CardLeaseAcquireInput{Owner: "exec-a", TTL: time.Hour}); err != nil {
		t.Fatal(err)
	}
	if ended(1) {
		t.Fatal("a held lease reported an ended tenure")
	}
	if _, err := s.ReleaseCardLease(ctx, "lead", cardID, CardLeaseReleaseInput{Owner: "exec-a"}); err != nil {
		t.Fatal(err)
	}
	if !ended(1) {
		t.Fatal("a released lease did not report an ended tenure")
	}
	queue := func(id string, epoch int64) bool {
		t.Helper()
		ok, err := s.UpsertCardLeaseWakeup(ctx, cardID, epoch, AgentWakeup{WakeupID: id, Status: AgentWakeupStatusRequested, TargetHandle: "lead", TargetActorID: "lead", WorkspaceID: "ws_main", ThreadID: "thread-x", TriggerEventID: "event-x"})
		if err != nil {
			t.Fatal(err)
		}
		return ok
	}
	if !queue("wake-ended", 1) {
		t.Fatal("wake for an ended tenure was not queued")
	}
	// A re-acquire after the clock claimed the wake makes it stale.
	if _, err := s.AcquireCardLease(ctx, "lead", cardID, CardLeaseAcquireInput{Owner: "exec-b", TTL: time.Hour}); err != nil {
		t.Fatal(err)
	}
	if ended(1) || ended(2) {
		t.Fatal("a new tenure left the old or new epoch ended")
	}
	if queue("wake-stale", 1) || queue("wake-held", 2) {
		t.Fatal("wake queued for a card with a live tenure")
	}
	if _, err := s.GetAgentWakeup(ctx, "wake-stale"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("stale wake row exists: %v", err)
	}
	*now = now.Add(2 * time.Hour)
	if !ended(2) {
		t.Fatal("an expired lease did not report an ended tenure")
	}
}

func TestCardLeaseRenewRefusesArchivedCard(t *testing.T) {
	s, ctx, cardID, _ := cardLeaseFixture(t)
	if _, err := s.AcquireCardLease(ctx, "lead", cardID, CardLeaseAcquireInput{Owner: "exec-a", TTL: time.Hour}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE cards SET archived_at='2026-10-25T09:00:00Z' WHERE id=?`, cardID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RenewCardLease(ctx, "lead", cardID, CardLeaseRenewInput{Owner: "exec-a"}); !errors.Is(err, ErrInvalidWorkRequest) {
		t.Fatalf("renew archived err=%v", err)
	}
	// Releasing stays possible so the holder can clean up.
	if got, err := s.ReleaseCardLease(ctx, "lead", cardID, CardLeaseReleaseInput{Owner: "exec-a"}); err != nil || !got.Changed {
		t.Fatalf("release archived=%+v err=%v", got, err)
	}
}
