package delivery_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"agent-nexus-core/internal/delivery"
)

func TestAskDeadlineTriggers(t *testing.T) {
	e := newEnv(t)
	e.addAsk(t, "ev-1", baseNow, "2h", 1, "Merge?")
	e.addAsk(t, "ev-2", baseNow, "2h", 1, "Other?")
	e.addAsk(t, "ev-3", baseNow, "2h", 1, "Third?")
	var by string
	var blocks, normalized int
	var requester string
	if err := e.db.QueryRow(`SELECT requester_actor_id, by_raw, blocks_count, normalized FROM ask_deadlines WHERE request_event_id='ev-1'`).Scan(&requester, &by, &blocks, &normalized); err != nil {
		t.Fatal(err)
	}
	if requester != "actor-a" || by != "2h" || blocks != 1 || normalized != 0 {
		t.Fatalf("unexpected row %q %q %d %d", requester, by, blocks, normalized)
	}

	// Resolution deletes the row and cancels pending pushes for the ask.
	if _, _, err := e.store.Enqueue(context.Background(), delivery.Request{IdempotencyKey: "k", PrincipalID: "h", Channel: "email", Kind: delivery.KindPush, SourceRef: "event:ev-1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.db.Exec(`INSERT INTO human_attention_request_resolutions(request_event_id,resolution_event_id,resolution_type,actor_id,created_at) VALUES('ev-1','res-1','answered','h',?)`, rfc(baseNow)); err != nil {
		t.Fatal(err)
	}
	if e.askRowCount(t, "ev-1") != 0 {
		t.Fatal("resolution should delete the ask deadline")
	}
	if e.askRowCount(t, "ev-2") != 1 {
		t.Fatal("other asks must be untouched")
	}
	if n := e.count(t, `SELECT COUNT(*) FROM deliveries WHERE idempotency_key='k' AND status='canceled'`); n != 1 {
		t.Fatal("pending push for resolved ask should be canceled")
	}

	if _, err := e.db.Exec(`UPDATE events SET trashed_at=? WHERE id='ev-2'`, rfc(baseNow)); err != nil {
		t.Fatal(err)
	}
	if e.askRowCount(t, "ev-2") != 0 {
		t.Fatal("trash should delete the ask deadline")
	}
	if e.askRowCount(t, "ev-3") != 1 {
		t.Fatal("ev-3 should remain")
	}
}

func TestResolveBy(t *testing.T) {
	from := time.Date(2026, 1, 10, 12, 0, 0, 0, time.UTC)
	ok := map[string]time.Time{
		"2026-01-11T08:00:00+02:00": time.Date(2026, 1, 11, 6, 0, 0, 0, time.UTC),
		"2026-01-12":                time.Date(2026, 1, 12, 23, 59, 59, 0, time.UTC),
		"30m":                       from.Add(30 * time.Minute),
		"2h":                        from.Add(2 * time.Hour),
		"3d":                        from.Add(72 * time.Hour),
		" 45s ":                     from.Add(45 * time.Second),
	}
	for raw, want := range ok {
		got, valid := delivery.ResolveBy(raw, from)
		if !valid || !got.Equal(want) {
			t.Errorf("ResolveBy(%q) = %v,%v want %v", raw, got, valid, want)
		}
	}
	for _, raw := range []string{"", "0h", "-1h", "abc", "3651d", "1.5h", "5w", "99999999999999d", "2026-13-40"} {
		if got, valid := delivery.ResolveBy(raw, from); valid {
			t.Errorf("ResolveBy(%q) should be invalid, got %v", raw, got)
		}
	}
	if _, valid := delivery.ResolveBy("3650d", from); !valid {
		t.Error("3650d should be valid")
	}
}

func TestEscalationMode(t *testing.T) {
	ask := baseNow
	cases := []struct {
		window time.Duration
		blocks int
		want   string
	}{
		{time.Hour, 0, delivery.EscalationPush},
		{2 * time.Hour, 0, delivery.EscalationPush},
		{2*time.Hour + time.Second, 0, delivery.EscalationQueue},
		{72 * time.Hour, 3, delivery.EscalationPush},
		{72 * time.Hour, 2, delivery.EscalationQueue},
		{72 * time.Hour, 0, delivery.EscalationQueue},
	}
	for _, c := range cases {
		if got := delivery.EscalationMode(ask, ask.Add(c.window), c.blocks); got != c.want {
			t.Errorf("window=%v blocks=%d got %s want %s", c.window, c.blocks, got, c.want)
		}
	}
}

func TestBackoff(t *testing.T) {
	want := []time.Duration{30 * time.Second, 60 * time.Second, 120 * time.Second, 240 * time.Second, 480 * time.Second, 960 * time.Second, 1920 * time.Second, time.Hour, time.Hour}
	for i, w := range want {
		if got := delivery.Backoff(i + 1); got != w {
			t.Errorf("Backoff(%d) = %v want %v", i+1, got, w)
		}
	}
	if delivery.Backoff(0) != 30*time.Second || delivery.Backoff(1000) != time.Hour {
		t.Error("backoff bounds")
	}
}

func enqueue(t *testing.T, e *env, key, principal, channel, kind string) delivery.Delivery {
	t.Helper()
	d, created, err := e.store.Enqueue(context.Background(), delivery.Request{IdempotencyKey: key, PrincipalID: principal, Channel: channel, Kind: kind, Payload: map[string]any{"title": "x"}})
	if err != nil || !created {
		t.Fatalf("enqueue %s: %v created=%v", key, err, created)
	}
	return d
}

func TestEnqueueIdempotentAndValidation(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	d1 := enqueue(t, e, "k1", "h1", "email", delivery.KindPush)
	d2, created, err := e.store.Enqueue(ctx, delivery.Request{IdempotencyKey: "k1", PrincipalID: "other", Channel: "email", Kind: delivery.KindPush})
	if err != nil || created || d2.DeliveryID != d1.DeliveryID || d2.PrincipalID != "h1" {
		t.Fatalf("idempotent enqueue: %+v created=%v err=%v", d2, created, err)
	}
	if n := e.count(t, `SELECT COUNT(*) FROM deliveries`); n != 1 {
		t.Fatalf("rows=%d", n)
	}
	bad := []delivery.Request{
		{PrincipalID: "h", Channel: "email", Kind: delivery.KindPush},
		{IdempotencyKey: "k", Channel: "email", Kind: delivery.KindPush},
		{IdempotencyKey: "k", PrincipalID: "h", Channel: "bridge", Kind: delivery.KindPush},
		{IdempotencyKey: "k", PrincipalID: "h", Channel: "Bad Chan", Kind: delivery.KindPush},
		{IdempotencyKey: "k", PrincipalID: "h", Channel: "email", Kind: delivery.KindWake},
	}
	for i, r := range bad {
		if _, _, err := e.store.Enqueue(ctx, r); !errors.Is(err, delivery.ErrInvalid) {
			t.Errorf("case %d: want ErrInvalid got %v", i, err)
		}
	}
	// NotBefore in the future delays; in the past is clamped to now.
	d3, _, _ := e.store.Enqueue(ctx, delivery.Request{IdempotencyKey: "later", PrincipalID: "h", Channel: "email", Kind: delivery.KindPush, NotBefore: baseNow.Add(time.Hour)})
	if d3.NextAttemptAt != rfc(baseNow.Add(time.Hour)) {
		t.Errorf("next_attempt_at %s", d3.NextAttemptAt)
	}
}

func TestClaimFiltersByRecipientSettings(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.setChannel(t, "ok-push", "email", "a@example.com", true, false)
	e.setChannel(t, "no-push", "email", "b@example.com", false, true)
	e.setChannel(t, "no-addr", "email", "", true, true)
	e.setChannel(t, "ok-digest", "email", "c@example.com", false, true)
	enqueue(t, e, "1", "ok-push", "email", delivery.KindPush)
	enqueue(t, e, "2", "no-push", "email", delivery.KindPush)
	enqueue(t, e, "3", "no-addr", "email", delivery.KindPush)
	enqueue(t, e, "4", "nobody", "email", delivery.KindPush)
	e.addAsk(t, "settings-ask", baseNow, "2h", 0, "Readable ask")
	if _, _, err := e.store.Enqueue(ctx, delivery.Request{IdempotencyKey: "5", PrincipalID: "ok-digest", Channel: "email", Kind: delivery.KindDigest, Payload: map[string]any{"type": "digest", "items": []map[string]any{{"ref": "event:settings-ask"}}}}); err != nil {
		t.Fatal(err)
	}
	enqueue(t, e, "6", "ok-push", "email", delivery.KindDigest) // digest disabled
	enqueue(t, e, "7", "ok-push", "telegram", delivery.KindPush)

	got, err := e.store.Claim(ctx, "email", "adapter-1", 50, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	addr := map[string]string{}
	for _, d := range got {
		addr[d.PrincipalID+"/"+d.Kind] = d.Address
		if d.Status != delivery.StatusClaimed || d.Attempt != 1 || d.ClaimToken == "" || d.LeaseExpiresAt != rfc(baseNow.Add(time.Minute)) {
			t.Errorf("bad claim %+v", d)
		}
	}
	if len(got) != 2 || addr["ok-push/push"] != "a@example.com" || addr["ok-digest/digest"] != "c@example.com" {
		t.Fatalf("claimed %v", addr)
	}
	if n := e.count(t, `SELECT COUNT(*) FROM deliveries WHERE status='canceled' AND channel='email'`); n != 4 {
		t.Fatalf("canceled=%d want 4", n)
	}
	// Other channel untouched.
	if n := e.count(t, `SELECT COUNT(*) FROM deliveries WHERE channel='telegram' AND status='pending'`); n != 1 {
		t.Fatal("telegram row should still be pending")
	}
	if _, err := e.store.Claim(ctx, "bridge", "x", 1, time.Minute); !errors.Is(err, delivery.ErrInvalid) {
		t.Fatal("bridge channel claim must be invalid")
	}
	if _, err := e.store.Claim(ctx, "email", "x", 1, time.Second); !errors.Is(err, delivery.ErrInvalid) {
		t.Fatal("short lease must be invalid")
	}
}

func TestClaimLimitLeaseAndReclaim(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.setChannel(t, "h", "email", "a@example.com", true, true)
	for _, k := range []string{"a", "b", "c"} {
		enqueue(t, e, k, "h", "email", delivery.KindPush)
		e.advance(time.Second)
	}
	e.advance(time.Second)
	first, err := e.store.Claim(ctx, "email", "w1", 2, time.Minute)
	if err != nil || len(first) != 2 {
		t.Fatalf("first claim %v %v", len(first), err)
	}
	// Oldest first.
	if first[0].CreatedAt >= first[1].CreatedAt {
		t.Error("expected time order")
	}
	second, _ := e.store.Claim(ctx, "email", "w2", 10, time.Minute)
	if len(second) != 1 {
		t.Fatalf("second claim should only get the unclaimed one, got %d", len(second))
	}
	if again, _ := e.store.Claim(ctx, "email", "w3", 10, time.Minute); len(again) != 0 {
		t.Fatal("live leases must not be reclaimed")
	}
	// Default limit.
	// Expire leases.
	e.advance(2 * time.Minute)
	re, err := e.store.Claim(ctx, "email", "w3", 10, time.Minute)
	if err != nil || len(re) != 3 {
		t.Fatalf("reclaim got %d err=%v", len(re), err)
	}
	for _, d := range re {
		if d.Attempt != 2 {
			t.Errorf("attempt=%d want 2", d.Attempt)
		}
	}
	// Old token no longer valid.
	var oldTok string
	for _, d := range first {
		if d.DeliveryID == re[0].DeliveryID {
			oldTok = d.ClaimToken
		}
	}
	if oldTok != "" {
		if _, err := e.store.RecordReceipt(ctx, re[0].DeliveryID, oldTok, "email", delivery.ReceiptDelivered, "", false, "w1"); !errors.Is(err, delivery.ErrConflict) {
			t.Fatalf("stale token want ErrConflict got %v", err)
		}
	}
}

func TestRecordReceipt(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.setChannel(t, "h", "email", "a@example.com", true, true)

	claimOne := func(key string) delivery.Delivery {
		enqueue(t, e, key, "h", "email", delivery.KindPush)
		got, err := e.store.Claim(ctx, "email", "w", 1, time.Minute)
		if err != nil || len(got) != 1 {
			t.Fatalf("claim: %v %d", err, len(got))
		}
		return got[0]
	}

	// Delivered.
	d := claimOne("deliver")
	if _, err := e.store.RecordReceipt(ctx, d.DeliveryID, d.ClaimToken, "telegram", delivery.ReceiptDelivered, "", false, "w"); !errors.Is(err, delivery.ErrNotFound) {
		t.Fatalf("wrong channel want ErrNotFound got %v", err)
	}
	if _, err := e.store.RecordReceipt(ctx, d.DeliveryID, "bogus", "email", delivery.ReceiptDelivered, "", false, "w"); !errors.Is(err, delivery.ErrConflict) {
		t.Fatalf("wrong token want ErrConflict got %v", err)
	}
	if _, err := e.store.RecordReceipt(ctx, d.DeliveryID, "", "email", delivery.ReceiptDelivered, "", false, "w"); !errors.Is(err, delivery.ErrInvalid) {
		t.Fatal("empty token invalid")
	}
	if _, err := e.store.RecordReceipt(ctx, "nope", "tok", "email", delivery.ReceiptDelivered, "", false, "w"); !errors.Is(err, delivery.ErrNotFound) {
		t.Fatal("missing delivery not found")
	}
	got, err := e.store.RecordReceipt(ctx, d.DeliveryID, d.ClaimToken, "email", delivery.ReceiptDelivered, "", false, "w")
	if err != nil || got.Status != delivery.StatusDelivered || got.DeliveredAt == "" || got.ClaimToken != "" {
		t.Fatalf("delivered: %+v %v", got, err)
	}
	if n := e.count(t, `SELECT COUNT(*) FROM delivery_receipts WHERE delivery_id=? AND outcome='notification_delivered' AND attempt=1`, d.DeliveryID); n != 1 {
		t.Fatal("missing delivered receipt")
	}
	// Retrying the receipt that closed this attempt returns the row.
	again, err := e.store.RecordReceipt(ctx, d.DeliveryID, d.ClaimToken, "email", delivery.ReceiptFailed, "ignored", false, "w")
	if err != nil || again.Status != delivery.StatusDelivered || again.ClaimToken != "" || again.DeliveryID != d.DeliveryID {
		t.Fatalf("replayed receipt: %+v %v", again, err)
	}
	if n := e.count(t, `SELECT COUNT(*) FROM delivery_receipts WHERE delivery_id=?`, d.DeliveryID); n != 1 {
		t.Fatalf("replay appended a receipt: %d", n)
	}
	if _, err := e.store.RecordReceipt(ctx, d.DeliveryID, "bogus", "email", delivery.ReceiptDelivered, "", false, "w"); !errors.Is(err, delivery.ErrConflict) {
		t.Fatalf("wrong token after delivery want ErrConflict got %v", err)
	}

	// Retryable failure then exhaustion.
	d = claimOne("retry")
	for attempt := 1; attempt <= delivery.DefaultMaxAttempts; attempt++ {
		long := strings.Repeat("e", 900)
		got, err = e.store.RecordReceipt(ctx, d.DeliveryID, d.ClaimToken, "email", delivery.ReceiptFailed, long, true, "w")
		if err != nil {
			t.Fatalf("attempt %d: %v", attempt, err)
		}
		if got.LastError != "" {
			t.Error("receipt echoed provider error")
		}
		var storedError string
		if err = e.db.QueryRow(`SELECT last_error FROM deliveries WHERE delivery_id=?`, d.DeliveryID).Scan(&storedError); err != nil || len(storedError) != 500 {
			t.Fatalf("stored error truncation: %d %v", len(storedError), err)
		}
		if attempt < delivery.DefaultMaxAttempts {
			if got.Status != delivery.StatusPending || got.NextAttemptAt != rfc(e.now.Add(delivery.Backoff(attempt))) {
				t.Fatalf("attempt %d: %+v want pending next=%s", attempt, got, rfc(e.now.Add(delivery.Backoff(attempt))))
			}
			// Not claimable before the backoff elapses.
			if c, _ := e.store.Claim(ctx, "email", "w", 1, time.Minute); len(c) != 0 {
				t.Fatal("claimed before backoff elapsed")
			}
			e.advance(delivery.Backoff(attempt))
			c, _ := e.store.Claim(ctx, "email", "w", 1, time.Minute)
			if len(c) != 1 || c[0].Attempt != attempt+1 {
				t.Fatalf("reclaim after backoff: %+v", c)
			}
			d = c[0]
		}
	}
	if got.Status != delivery.StatusFailed || got.FailedAt == "" {
		t.Fatalf("after max attempts want failed: %+v", got)
	}
	if n := e.count(t, `SELECT COUNT(*) FROM delivery_receipts WHERE delivery_id=? AND outcome='notification_failed'`, d.DeliveryID); n != delivery.DefaultMaxAttempts {
		t.Fatalf("failed receipts=%d", n)
	}

	// Non-retryable failure fails immediately.
	d = claimOne("fatal")
	got, err = e.store.RecordReceipt(ctx, d.DeliveryID, d.ClaimToken, "email", delivery.ReceiptFailed, "bad address", false, "w")
	if err != nil || got.Status != delivery.StatusFailed {
		t.Fatalf("fatal: %+v %v", got, err)
	}
	if again, err = e.store.RecordReceipt(ctx, d.DeliveryID, d.ClaimToken, "email", delivery.ReceiptFailed, "bad address", false, "w"); err != nil || again.Status != delivery.StatusFailed || again.FailedAt == "" {
		t.Fatalf("replayed failure: %+v %v", again, err)
	}
	if n := e.count(t, `SELECT COUNT(*) FROM delivery_receipts WHERE delivery_id=?`, d.DeliveryID); n != 1 {
		t.Fatalf("replayed failure appended a receipt: %d", n)
	}

	// ListForPrincipal hides claim tokens and includes receipts.
	list, receipts, err := e.store.ListForPrincipal(ctx, "h", 0)
	if err != nil || len(list) != 3 {
		t.Fatalf("list %d %v", len(list), err)
	}
	for _, l := range list {
		if l.ClaimToken != "" {
			t.Error("claim token leaked in list")
		}
	}
	if len(receipts[d.DeliveryID]) != 1 {
		t.Error("receipts missing in list")
	}
}

func insertWake(t *testing.T, e *env, id, status string) {
	t.Helper()
	ts := rfc(baseNow)
	if _, err := e.db.Exec(`INSERT INTO agent_wakeups(wakeup_id,status,target_handle,target_actor_id,thread_id,trigger_event_id,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?)`,
		id, status, "@bot", "agent-1", "thread-9", "ev-77", ts, ts); err != nil {
		t.Fatal(err)
	}
}

func TestWakeMirror(t *testing.T) {
	e := newEnv(t)
	insertWake(t, e, "w1", "pending")
	var channel, kind, status, ref, key string
	if err := e.db.QueryRow(`SELECT channel,kind,status,source_ref,idempotency_key FROM deliveries WHERE delivery_id='dlv_wake_w1'`).Scan(&channel, &kind, &status, &ref, &key); err != nil {
		t.Fatal(err)
	}
	if channel != "bridge" || kind != "wake" || status != "pending" || ref != "event:ev-77" || key != "wake:w1" {
		t.Fatalf("mirror row %s %s %s %s %s", channel, kind, status, ref, key)
	}
	upd := func(id, status, reason string) {
		t.Helper()
		if _, err := e.db.Exec(`UPDATE agent_wakeups SET status=?, failure_reason=?, bridge_instance_id='bridge-1', updated_at=?, completed_at=CASE WHEN ?='completed' THEN ? END, failed_at=CASE WHEN ?='failed' THEN ? END WHERE wakeup_id=?`,
			status, reason, rfc(baseNow.Add(time.Minute)), status, rfc(baseNow.Add(time.Minute)), status, rfc(baseNow.Add(time.Minute)), id); err != nil {
			t.Fatal(err)
		}
	}
	upd("w1", "claimed", "")
	var attempts int
	e.db.QueryRow(`SELECT status, attempts FROM deliveries WHERE delivery_id='dlv_wake_w1'`).Scan(&status, &attempts)
	if status != "claimed" || attempts != 1 {
		t.Fatalf("claimed: %s %d", status, attempts)
	}
	if e.count(t, `SELECT COUNT(*) FROM delivery_receipts`) != 0 {
		t.Fatal("claim should not append a receipt")
	}
	upd("w1", "completed", "")
	e.db.QueryRow(`SELECT status, delivered_at FROM deliveries WHERE delivery_id='dlv_wake_w1'`).Scan(&status, &ref)
	if status != "delivered" || ref != rfc(baseNow.Add(time.Minute)) {
		t.Fatalf("completed: %s %s", status, ref)
	}
	if n := e.count(t, `SELECT COUNT(*) FROM delivery_receipts WHERE delivery_id='dlv_wake_w1' AND outcome='notification_delivered' AND recorded_by='bridge-1'`); n != 1 {
		t.Fatal("missing delivered receipt")
	}

	insertWake(t, e, "w2", "pending")
	upd("w2", "claimed", "")
	upd("w2", "failed", "boom")
	var lastErr string
	e.db.QueryRow(`SELECT status, last_error FROM deliveries WHERE delivery_id='dlv_wake_w2'`).Scan(&status, &lastErr)
	if status != "failed" || lastErr != "boom" {
		t.Fatalf("failed: %s %s", status, lastErr)
	}
	if n := e.count(t, `SELECT COUNT(*) FROM delivery_receipts WHERE delivery_id='dlv_wake_w2' AND outcome='notification_failed' AND error='boom'`); n != 1 {
		t.Fatal("missing failed receipt")
	}
	// Wakes never leak into the human claim path.
	if _, err := e.store.Claim(context.Background(), "bridge", "x", 1, time.Minute); !errors.Is(err, delivery.ErrInvalid) {
		t.Fatal("bridge claim must be rejected")
	}
}

func TestSettingsAndChannels(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	got, err := e.store.ReplaceSettings(ctx, "h", []delivery.ChannelSetting{{Channel: "email", Address: " a@example.com ", PushEnabled: true}})
	if err != nil || len(got) != 1 || got[0].Address != "a@example.com" || !got[0].PushEnabled || got[0].DigestEnabled {
		t.Fatalf("%+v %v", got, err)
	}
	for _, bad := range [][]delivery.ChannelSetting{
		{{Channel: "bridge"}},
		{{Channel: "email"}, {Channel: "email"}},
		{{Channel: "email", Address: "a\nb"}},
		{{Channel: "email", Address: strings.Repeat("a", 321)}},
	} {
		if _, err := e.store.ReplaceSettings(ctx, "h", bad); !errors.Is(err, delivery.ErrInvalid) {
			t.Errorf("want invalid for %+v got %v", bad, err)
		}
	}
	if s, _ := e.store.Settings(ctx, "h"); len(s) != 1 {
		t.Fatal("failed replace must not clobber settings")
	}
}

func TestWebhookAskDeliveriesMirrorIntoQueue(t *testing.T) {
	e := newEnv(t)
	if _, err := e.db.Exec(`INSERT INTO ask_subscriptions(id,actor_id,ask_id,kind,label,endpoint,created_at,enabled) VALUES('sub-1','agent-1','','webhook','hook','https://hooks.example.test/x',?,1)`, rfc(baseNow)); err != nil {
		t.Fatal(err)
	}
	if _, err := e.db.Exec(`INSERT INTO ask_deliveries(id,subscription_id,ask_id,kind,next_at) VALUES('sub-1.ask-1','sub-1','ask-1','webhook',?)`, rfc(baseNow)); err != nil {
		t.Fatal(err)
	}
	if n := e.count(t, `SELECT COUNT(*) FROM deliveries WHERE idempotency_key='webhook:sub-1.ask-1' AND channel='webhook' AND kind='webhook' AND principal_id='agent-1' AND status='pending'`); n != 1 {
		t.Fatalf("webhook delivery not mirrored: %d", n)
	}
	if _, err := e.db.Exec(`UPDATE ask_deliveries SET attempts=1, state='delivered' WHERE id='sub-1.ask-1'`); err != nil {
		t.Fatal(err)
	}
	if n := e.count(t, `SELECT COUNT(*) FROM deliveries WHERE idempotency_key='webhook:sub-1.ask-1' AND status='delivered' AND attempts=1`); n != 1 {
		t.Fatalf("webhook outcome not mirrored")
	}
	if n := e.count(t, `SELECT COUNT(*) FROM delivery_receipts WHERE delivery_id='dlv_webhook_sub-1.ask-1' AND outcome='notification_delivered'`); n != 1 {
		t.Fatalf("webhook receipt missing")
	}
	if delivery.ValidChannel("webhook") || delivery.ValidChannel("bridge") {
		t.Fatal("reserved channels must not be adapter-deliverable")
	}
}
