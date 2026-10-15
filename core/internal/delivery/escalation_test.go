package delivery_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"agent-nexus-core/internal/delivery"
)

func newEscalator(e *env, canRead func(principal, ask string) bool) (*delivery.Escalator, *[]string) {
	pm := &[]string{}
	return &delivery.Escalator{
		Store:         e.store,
		WorkspaceName: "ws",
		DigestHourUTC: 9,
		CanRead: func(_ context.Context, p, a string) bool {
			return canRead == nil || canRead(p, a)
		},
		AskPM: func(_ context.Context, id string, _ time.Time) error {
			*pm = append(*pm, id)
			return nil
		},
		Logf: func(string, ...any) {},
	}, pm
}

func TestDateOnlyByResolvesOnce(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.addAsk(t, "date-by", baseNow, "2026-10-15", 0, "Ship?")
	var briefBy string
	if err := e.db.QueryRow(`SELECT decide_by FROM brief_asks WHERE ask_id='date-by'`).Scan(&briefBy); err != nil {
		t.Fatal(err)
	}
	esc, _ := newEscalator(e, nil)
	if err := esc.Step(ctx, time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	var deadlineBy string
	if err := e.db.QueryRow(`SELECT decide_by FROM ask_deadlines WHERE request_event_id='date-by'`).Scan(&deadlineBy); err != nil {
		t.Fatal(err)
	}
	briefAt, err := time.Parse(time.RFC3339Nano, briefBy)
	if err != nil {
		t.Fatal(err)
	}
	deadlineAt, err := time.Parse(time.RFC3339Nano, deadlineBy)
	if err != nil {
		t.Fatal(err)
	}
	want := time.Date(2026, 10, 15, 23, 59, 59, 0, time.UTC)
	if !briefAt.Equal(want) || !deadlineAt.Equal(want) {
		t.Fatalf("brief %s deadline %s", briefAt, deadlineAt)
	}
}

func TestEscalatorPushOnce(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	esc, pm := newEscalator(e, nil)
	e.setChannel(t, "alice", "email", "a@example.com", true, false)
	e.setChannel(t, "bob", "email", "b@example.com", false, false)
	// Asked 2h ago with a 1h window: overdue by 1h.
	e.addAsk(t, "ask-1", baseNow.Add(-2*time.Hour), "1h", 0, "Merge?")

	// Not yet due before ask+1h.
	if err := esc.Step(ctx, baseNow.Add(-90*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if e.count(t, `SELECT COUNT(*) FROM deliveries`) != 0 || len(*pm) != 0 {
		t.Fatal("nothing should escalate before decide_by")
	}

	if err := esc.Step(ctx, baseNow); err != nil {
		t.Fatal(err)
	}
	var payloadJSON, src, principal string
	if err := e.db.QueryRow(`SELECT payload_json, source_ref, principal_id FROM deliveries WHERE kind='push'`).Scan(&payloadJSON, &src, &principal); err != nil {
		t.Fatalf("expected push: %v", err)
	}
	var p map[string]any
	_ = json.Unmarshal([]byte(payloadJSON), &p)
	if principal != "alice" || src != "event:ask-1" || p["title"] != "Merge?" || p["ref"] != "event:ask-1" || p["type"] != "ask_overdue" {
		t.Fatalf("push %s %s %v", principal, src, p)
	}
	var escalatedAt, mode string
	if err := e.db.QueryRow(`SELECT escalated_at, escalation FROM ask_deadlines WHERE request_event_id='ask-1'`).Scan(&escalatedAt, &mode); err != nil || escalatedAt == "" || mode != "push" {
		t.Fatalf("escalated %q %q %v", escalatedAt, mode, err)
	}
	if len(*pm) != 1 || (*pm)[0] != "ask-1" {
		t.Fatalf("AskPM calls %v", *pm)
	}

	if err := esc.Step(ctx, baseNow.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if n := e.count(t, `SELECT COUNT(*) FROM deliveries WHERE kind='push'`); n != 1 {
		t.Fatalf("second step enqueued more: %d", n)
	}
	if len(*pm) != 1 {
		t.Fatalf("AskPM called again: %v", *pm)
	}
}

func TestEscalatorBlocksTriggerPush(t *testing.T) {
	e := newEnv(t)
	esc, _ := newEscalator(e, nil)
	e.setChannel(t, "alice", "email", "a@example.com", true, false)
	e.addAsk(t, "ask-b", baseNow.Add(-3*24*time.Hour), "2d", 3, "Big")
	if err := esc.Step(context.Background(), baseNow); err != nil {
		t.Fatal(err)
	}
	if n := e.count(t, `SELECT COUNT(*) FROM deliveries WHERE kind='push'`); n != 1 {
		t.Fatalf("3 blocks should push, got %d", n)
	}
}

func TestEscalatorPrivacyCanRead(t *testing.T) {
	e := newEnv(t)
	esc, pm := newEscalator(e, func(p, _ string) bool { return p == "alice" })
	e.setChannel(t, "alice", "email", "a@example.com", true, true)
	e.setChannel(t, "mallory", "email", "m@example.com", true, true)
	e.addAsk(t, "secret", baseNow.Add(-2*time.Hour), "1h", 0, "Layoff plan")
	if err := esc.Step(context.Background(), baseNow); err != nil {
		t.Fatal(err)
	}
	if n := e.count(t, `SELECT COUNT(*) FROM deliveries WHERE principal_id='mallory'`); n != 0 {
		t.Fatalf("unreadable principal got %d deliveries", n)
	}
	if n := e.count(t, `SELECT COUNT(*) FROM deliveries WHERE principal_id='alice' AND kind='push'`); n != 1 {
		t.Fatal("readable principal should get the push")
	}
	if n := e.count(t, `SELECT COUNT(*) FROM deliveries WHERE payload_json LIKE '%Layoff%' AND principal_id<>'alice'`); n != 0 {
		t.Fatal("title leaked")
	}
	if len(*pm) != 1 {
		t.Fatalf("AskPM still runs once: %v", *pm)
	}
	// Nil CanRead withholds everything (fail closed).
	e2 := newEnv(t)
	esc2 := &delivery.Escalator{Store: e2.store, Logf: func(string, ...any) {}}
	e2.setChannel(t, "alice", "email", "a@example.com", true, true)
	e2.addAsk(t, "s", baseNow.Add(-2*time.Hour), "1h", 0, "T")
	if err := esc2.Step(context.Background(), baseNow); err != nil {
		t.Fatal(err)
	}
	if e2.count(t, `SELECT COUNT(*) FROM deliveries`) != 0 {
		t.Fatal("nil CanRead must fail closed")
	}
}

func TestEscalatorLongWindowGoesToDigest(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	esc, pm := newEscalator(e, nil)
	e.setChannel(t, "alice", "email", "a@example.com", true, true)
	e.addAsk(t, "slow", baseNow.Add(-4*24*time.Hour), "3d", 0, "Roadmap?")
	e.addAsk(t, "bad-by", baseNow.Add(-4*24*time.Hour), "soon", 0, "No deadline")

	// Before the digest hour: escalation recorded, no push, no digest.
	early := time.Date(2026, 1, 10, 8, 0, 0, 0, time.UTC)
	if err := esc.Step(ctx, early); err != nil {
		t.Fatal(err)
	}
	if n := e.count(t, `SELECT COUNT(*) FROM deliveries`); n != 0 {
		t.Fatalf("before digest hour got %d deliveries", n)
	}
	var mode string
	var by any
	if err := e.db.QueryRow(`SELECT escalation FROM ask_deadlines WHERE request_event_id='slow'`).Scan(&mode); err != nil || mode != "digest" {
		t.Fatalf("mode %q %v", mode, err)
	}
	if err := e.db.QueryRow(`SELECT decide_by FROM ask_deadlines WHERE request_event_id='bad-by'`).Scan(&by); err != nil || by != nil {
		t.Fatalf("unparseable by should leave decide_by NULL, got %v", by)
	}
	if len(*pm) != 1 {
		t.Fatalf("AskPM %v", *pm)
	}

	// After the digest hour: exactly one digest.
	for i := 0; i < 3; i++ {
		if err := esc.Step(ctx, baseNow.Add(time.Duration(i)*time.Minute)); err != nil {
			t.Fatal(err)
		}
	}
	var payloadJSON string
	rows := e.count(t, `SELECT COUNT(*) FROM deliveries WHERE kind='digest'`)
	if rows != 1 || e.count(t, `SELECT COUNT(*) FROM deliveries WHERE kind='push'`) != 0 {
		t.Fatalf("digests=%d", rows)
	}
	if err := e.db.QueryRow(`SELECT payload_json FROM deliveries WHERE kind='digest'`).Scan(&payloadJSON); err != nil {
		t.Fatal(err)
	}
	var p struct {
		Day     string           `json:"day"`
		Open    int              `json:"open"`
		Overdue int              `json:"overdue"`
		Items   []map[string]any `json:"items"`
	}
	_ = json.Unmarshal([]byte(payloadJSON), &p)
	if p.Day != "2026-01-10" || p.Open != 2 || p.Overdue != 1 || len(p.Items) != 1 || p.Items[0]["title"] != "Roadmap?" || p.Items[0]["ref"] != "event:slow" {
		t.Fatalf("digest payload %s", payloadJSON)
	}

	// Next day gets a new digest.
	if err := esc.Step(ctx, baseNow.Add(24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if n := e.count(t, `SELECT COUNT(*) FROM deliveries WHERE kind='digest'`); n != 2 {
		t.Fatalf("next day digests=%d", n)
	}
}

func TestDigestOpenCountExcludesUnreadable(t *testing.T) {
	e := newEnv(t)
	esc, _ := newEscalator(e, func(p, ask string) bool {
		return p == "alice" || ask == "shared"
	})
	e.setChannel(t, "alice", "email", "a@example.com", false, true)
	e.setChannel(t, "mallory", "email", "m@example.com", false, true)
	e.addAsk(t, "shared", baseNow.Add(-4*24*time.Hour), "3d", 0, "Shared")
	e.addAsk(t, "secret", baseNow.Add(-4*24*time.Hour), "3d", 0, "Secret layoff")
	e.addAsk(t, "secret-later", baseNow, "10h", 0, "Secret later")
	if err := esc.Step(context.Background(), baseNow); err != nil {
		t.Fatal(err)
	}
	openOf := func(principal string) int {
		t.Helper()
		var payloadJSON string
		if err := e.db.QueryRow(`SELECT payload_json FROM deliveries WHERE kind='digest' AND principal_id=?`, principal).Scan(&payloadJSON); err != nil {
			t.Fatal(err)
		}
		var p struct {
			Open int `json:"open"`
		}
		_ = json.Unmarshal([]byte(payloadJSON), &p)
		if strings.Contains(payloadJSON, "layoff") && principal != "alice" {
			t.Fatalf("title leaked to %s", principal)
		}
		return p.Open
	}
	if alice, mallory := openOf("alice"), openOf("mallory"); alice != 3 || mallory != 1 {
		t.Fatalf("open counts alice=%d mallory=%d", alice, mallory)
	}
}

func TestAttentionSummaryOmitsUnreadable(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.setChannel(t, "alice", "email", "a@example.com", true, false)
	tx, err := e.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 500; i++ {
		id := fmt.Sprintf("h%04d", i)
		if _, err := tx.Exec(`INSERT INTO ask_deadlines(request_event_id, requester_actor_id, ask_ts, by_raw, decide_by, normalized) VALUES(?,?,?,?,?,1)`, id, "actor-a", rfc(baseNow), "1h", rfc(baseNow.Add(-time.Hour))); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := tx.Exec(`INSERT INTO ask_deadlines(request_event_id, requester_actor_id, ask_ts, by_raw, decide_by, normalized) VALUES('visible-ask','actor-a',?,?,?,1)`, rfc(baseNow), "10h", rfc(baseNow.Add(10*time.Hour))); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	e.store.CanRead = func(_ context.Context, _, ask string) bool { return ask == "visible-ask" }
	sum, err := e.store.AttentionSummary(ctx, baseNow)
	if err != nil {
		t.Fatal(err)
	}
	if sum["open_decisions"] != 501 || sum["overdue_decisions"] != 500 {
		t.Fatalf("workspace totals must stay unfiltered: %v", sum)
	}
	principals := sum["principals"].([]map[string]any)
	if len(principals) != 1 || principals[0]["due"] != 1 || principals[0]["overdue"] != 0 || principals[0]["earliest_decide_by"] != rfc(baseNow.Add(10*time.Hour)) {
		t.Fatalf("principal saw unreadable asks: %v", principals)
	}
	if sum["earliest_decide_by"] == principals[0]["earliest_decide_by"] {
		t.Fatal("principal earliest was the workspace earliest")
	}
}

func TestDigestRespectsReadAndPreferences(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	esc, _ := newEscalator(e, func(p, _ string) bool { return p != "mallory" })
	e.setChannel(t, "alice", "email", "a@example.com", false, true)
	e.setChannel(t, "alice", "telegram", "42", false, true)
	e.setChannel(t, "bob", "email", "b@example.com", true, false)
	e.setChannel(t, "mallory", "email", "m@example.com", false, true)
	e.addAsk(t, "slow", baseNow.Add(-4*24*time.Hour), "3d", 0, "Roadmap?")
	if err := esc.Step(ctx, baseNow); err != nil {
		t.Fatal(err)
	}
	rows, err := e.db.Query(`SELECT principal_id||'/'||channel FROM deliveries WHERE kind='digest' ORDER BY 1`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var s string
		_ = rows.Scan(&s)
		got = append(got, s)
	}
	if strings.Join(got, ",") != "alice/email,alice/telegram" {
		t.Fatalf("digests %v", got)
	}
}

func TestDigestReadsPastUnreadablePrefix(t *testing.T) {
	e := newEnv(t)
	esc, _ := newEscalator(e, func(_, ask string) bool { return ask == "readable" })
	e.setChannel(t, "alice", "email", "a@example.com", false, true)
	start := baseNow.Add(-4 * 24 * time.Hour)
	for i := 0; i < 60; i++ {
		e.addAsk(t, fmt.Sprintf("hidden-%02d", i), start.Add(time.Duration(i)*time.Second), "3d", 0, "hidden")
	}
	e.addAsk(t, "readable", start.Add(61*time.Second), "3d", 0, "Visible ask")
	if err := esc.Step(context.Background(), baseNow); err != nil {
		t.Fatal(err)
	}
	var payloadJSON string
	if err := e.db.QueryRow(`SELECT payload_json FROM deliveries WHERE kind='digest'`).Scan(&payloadJSON); err != nil {
		t.Fatalf("digest dropped the readable ask behind 60 unreadable ones: %v", err)
	}
	var p struct {
		Overdue int              `json:"overdue"`
		Items   []map[string]any `json:"items"`
	}
	if err := json.Unmarshal([]byte(payloadJSON), &p); err != nil {
		t.Fatal(err)
	}
	if p.Overdue != 1 || len(p.Items) != 1 || p.Items[0]["ref"] != "event:readable" || p.Items[0]["title"] != "Visible ask" {
		t.Fatalf("digest missed the readable ask: overdue=%d items=%v", p.Overdue, p.Items)
	}
}

func TestDigestItemCap(t *testing.T) {
	e := newEnv(t)
	esc, _ := newEscalator(e, nil)
	e.setChannel(t, "alice", "email", "a@example.com", false, true)
	for i := 0; i < 12; i++ {
		e.addAsk(t, "a"+string(rune('a'+i)), baseNow.Add(-4*24*time.Hour), "3d", 0, "t")
	}
	if err := esc.Step(context.Background(), baseNow); err != nil {
		t.Fatal(err)
	}
	var payloadJSON string
	if err := e.db.QueryRow(`SELECT payload_json FROM deliveries WHERE kind='digest'`).Scan(&payloadJSON); err != nil {
		t.Fatal(err)
	}
	var p struct {
		Overdue int              `json:"overdue"`
		Items   []map[string]any `json:"items"`
	}
	_ = json.Unmarshal([]byte(payloadJSON), &p)
	if p.Overdue != 12 || len(p.Items) != 10 {
		t.Fatalf("overdue=%d items=%d", p.Overdue, len(p.Items))
	}
}

func TestAttentionSummary(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	esc, _ := newEscalator(e, nil)
	e.setChannel(t, "alice", "email", "a@example.com", true, false)
	e.setChannel(t, "alice", "telegram", "1", true, false)
	e.setChannel(t, "bob", "email", "", true, true)                  // no address
	e.setChannel(t, "carol", "email", "c@example.com", false, false) // all off
	e.addAsk(t, "overdue-escalated", baseNow.Add(-5*time.Hour), "1h", 0, "SECRET-TITLE-1")
	e.addAsk(t, "overdue-pending", baseNow.Add(-3*time.Hour), "2h", 0, "SECRET-TITLE-2")
	e.addAsk(t, "future", baseNow, "10h", 0, "SECRET-TITLE-3")
	e.addAsk(t, "future2", baseNow, "20h", 0, "SECRET-TITLE-4")
	// Normalize all, escalate only the first by running at a time between.
	if err := esc.Step(ctx, baseNow.Add(-2*time.Hour-30*time.Minute)); err != nil {
		t.Fatal(err)
	}
	// Only the first ask (decide_by = now-4h) is due at that time; the second (now-1h) stays unescalated.
	sum, err := e.store.AttentionSummary(ctx, baseNow)
	if err != nil {
		t.Fatal(err)
	}
	if sum["open_decisions"] != 4 || sum["overdue_decisions"] != 2 {
		t.Fatalf("counts %v", sum)
	}
	if want := rfc(baseNow.Add(-time.Hour)); sum["earliest_decide_by"] != want {
		t.Fatalf("earliest %v want %v (min over unescalated)", sum["earliest_decide_by"], want)
	}
	principals := sum["principals"].([]map[string]any)
	if len(principals) != 1 || principals[0]["principal_id"] != "alice" {
		t.Fatalf("principals %v", principals)
	}
	// No read check: the workspace totals stay, and the principal's copy is not those totals.
	if principals[0]["due"] != 0 || principals[0]["overdue"] != 0 || principals[0]["earliest_decide_by"] != nil {
		t.Fatalf("nil CanRead copied workspace counts onto the principal: %v", principals[0])
	}
	e.store.CanRead = func(context.Context, string, string) bool { return true }
	sum, err = e.store.AttentionSummary(ctx, baseNow)
	if err != nil {
		t.Fatal(err)
	}
	principals = sum["principals"].([]map[string]any)
	if principals[0]["due"] != 4 || principals[0]["overdue"] != 2 || principals[0]["earliest_decide_by"] != sum["earliest_decide_by"] {
		t.Fatalf("readable principal counts %v", principals[0])
	}
	raw, _ := json.Marshal(sum)
	if strings.Contains(string(raw), "SECRET") || strings.Contains(string(raw), "event:") || strings.Contains(string(raw), "a@example.com") {
		t.Fatalf("summary leaks content: %s", raw)
	}

	// Empty ledger: nil earliest.
	e2 := newEnv(t)
	s2, err := e2.store.AttentionSummary(ctx, baseNow)
	if err != nil || s2["earliest_decide_by"] != nil || s2["open_decisions"] != 0 {
		t.Fatalf("empty summary %v %v", s2, err)
	}
}

func TestHotReadQueryPlans(t *testing.T) {
	e := newEnv(t)
	cases := []struct {
		name, query, index string
		args               []any
		bareScan           string
	}{
		{"claim pending", `SELECT delivery_id FROM deliveries WHERE channel=? AND status='pending' AND next_attempt_at<=? ORDER BY next_attempt_at, delivery_id LIMIT ?`, "idx_deliveries_claim", []any{"email", "x", 10}, "deliveries"},
		{"claim expired lease", `SELECT delivery_id FROM deliveries INDEXED BY idx_deliveries_claim WHERE channel=? AND status='claimed' AND lease_expires_at<=? ORDER BY next_attempt_at, delivery_id LIMIT ?`, "idx_deliveries_claim", []any{"email", "x", 10}, "deliveries"},
		{"escalation due", `SELECT request_event_id, ask_ts, decide_by, blocks_count FROM ask_deadlines INDEXED BY idx_ask_deadlines_due WHERE escalated_at IS NULL AND decide_by IS NOT NULL AND decide_by<=? ORDER BY decide_by, request_event_id LIMIT ?`, "idx_ask_deadlines_due", []any{"x", 50}, "ask_deadlines"},
		{"normalize pending", `SELECT request_event_id, ask_ts, by_raw FROM ask_deadlines INDEXED BY idx_ask_deadlines_pending WHERE normalized=0 LIMIT ?`, "idx_ask_deadlines_pending", []any{200}, "ask_deadlines"},
		{"overdue count", `SELECT COUNT(*) FROM ask_deadlines INDEXED BY idx_ask_deadlines_decide_by WHERE decide_by IS NOT NULL AND decide_by<=?`, "idx_ask_deadlines_decide_by", []any{"x"}, "ask_deadlines"},
		{"overdue list", `SELECT request_event_id, decide_by FROM ask_deadlines INDEXED BY idx_ask_deadlines_decide_by WHERE decide_by IS NOT NULL AND decide_by<=? ORDER BY decide_by LIMIT ? OFFSET ?`, "idx_ask_deadlines_decide_by", []any{"x", 50, 0}, "ask_deadlines"},
		{"earliest MIN", `SELECT MIN(decide_by) FROM ask_deadlines INDEXED BY idx_ask_deadlines_due WHERE escalated_at IS NULL AND decide_by IS NOT NULL`, "idx_ask_deadlines_due", nil, "ask_deadlines"},
		{"principal list", `SELECT * FROM deliveries WHERE principal_id=? ORDER BY created_at DESC, delivery_id DESC LIMIT ?`, "idx_deliveries_principal", []any{"p", 10}, "deliveries"},
		{"deadline ledger page", `SELECT request_event_id, COALESCE(decide_by,''), COALESCE(escalated_at,'') FROM ask_deadlines WHERE request_event_id>? ORDER BY request_event_id LIMIT ?`, "sqlite_autoindex_ask_deadlines_1", []any{"", 500}, "ask_deadlines"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rows, err := e.db.Query("EXPLAIN QUERY PLAN "+c.query, c.args...)
			if err != nil {
				t.Fatal(err)
			}
			defer rows.Close()
			var plan []string
			for rows.Next() {
				var id, parent, unused int
				var detail string
				if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
					t.Fatal(err)
				}
				plan = append(plan, detail)
			}
			joined := strings.Join(plan, "\n")
			if !strings.Contains(joined, c.index) {
				t.Errorf("plan does not use %s:\n%s", c.index, joined)
			}
			for _, d := range plan {
				if strings.Contains(d, "SCAN "+c.bareScan) && !strings.Contains(d, "USING") {
					t.Errorf("unindexed scan: %s", d)
				}
			}
		})
	}
}

func TestEscalatorTerminalCardBlocksDoNotCount(t *testing.T) {
	e := newEnv(t)
	esc, _ := newEscalator(e, nil)
	e.setChannel(t, "alice", "email", "a@example.com", true, false)
	// Three card refs that do not resolve to an open card: missing cards
	// count as terminal, so the ask falls back to the digest.
	payload, _ := json.Marshal(map[string]any{"payload": map[string]any{"title": "Stale blocks", "requester_actor_id": "actor-a", "by": "2d", "blocks": []string{"card:gone-1", "card:gone-2", "card:gone-3"}}})
	if _, err := e.db.Exec(`INSERT INTO events(id,type,ts,actor_id,thread_id,refs_json,payload_json) VALUES('ask-t','human_attention_requested',?,'actor-a','thread-1','[]',?)`, rfc(baseNow.Add(-3*24*time.Hour)), string(payload)); err != nil {
		t.Fatal(err)
	}
	if err := esc.Step(context.Background(), baseNow); err != nil {
		t.Fatal(err)
	}
	if n := e.count(t, `SELECT COUNT(*) FROM deliveries WHERE kind='push'`); n != 0 {
		t.Fatalf("terminal blocks must not force a push, got %d", n)
	}
	if n := e.count(t, `SELECT COUNT(*) FROM ask_deadlines WHERE request_event_id='ask-t' AND escalation='digest'`); n != 1 {
		t.Fatalf("ask should escalate to the digest")
	}
}
