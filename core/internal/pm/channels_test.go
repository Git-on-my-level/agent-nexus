package pm

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

type sendFunc func(context.Context, Delivery) (Receipt, error)

func (f sendFunc) Send(c context.Context, d Delivery) (Receipt, error) { return f(c, d) }

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func bind(t *testing.T, s *Service, p Principal, o Origin, approve bool) Binding {
	t.Helper()
	b, err := s.BindChannel(context.Background(), p, Binding{WorkspaceID: p.WorkspaceID, ActorID: p.ActorID, Origin: o, Enabled: true, CanApprove: approve})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestExplicitChannelIdentityAndDeliveryRestart(t *testing.T) {
	s, st, p, count := fixture(t)
	ctx := context.Background()
	o := Origin{Transport: "telegram", TenantID: "bot-1", ChannelID: "-123", ExternalUserID: "42"}
	if _, err := s.ReceiveChannel(ctx, o, "1", "What changed?"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("unbound %v", err)
	}
	b := bind(t, s, p, o, false)
	stranger := o
	stranger.ExternalUserID = "43"
	if _, err := s.ReceiveChannel(ctx, stranger, "1", "hello"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("shared user inherited permission %v", err)
	}
	turn, err := s.ReceiveChannel(ctx, o, "1", "What changed?")
	if err != nil {
		t.Fatal(err)
	}
	if turn.Origin == nil || *turn.Origin != o {
		t.Fatalf("channel turn missing origin %+v", turn.Origin)
	}
	if _, err = s.ReceiveChannel(ctx, o, "1", "What changed?"); err != nil || *count != 1 {
		t.Fatalf("replay %v %d", err, *count)
	}
	if _, err = claimAndComplete(t, s, ctx, Principal{WorkspaceID: "ws", ActorID: "pm-agent"}, turn.ID, "The source reports progress; verification is pending.", []string{"artifact:1"}); err != nil {
		t.Fatal(err)
	}
	d, err := s.QueueTurnDelivery(ctx, turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	sends := 0
	sender := sendFunc(func(context.Context, Delivery) (Receipt, error) { sends++; return Receipt{}, errors.New("timeout") })
	d, err = s.SendDelivery(ctx, d.ID, sender)
	if err != nil || d.Status != Unknown {
		t.Fatalf("send %v %+v", err, d)
	}
	restarted, _ := NewService(st, s.cfg, s.deps)
	if _, err = restarted.SendDelivery(ctx, d.ID, sender); err != nil || sends != 1 {
		t.Fatalf("unknown resent %d %v", sends, err)
	}
	b.Enabled = false
	if _, err = s.BindChannel(ctx, p, b); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ReceiveChannel(ctx, o, "2", "hello"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("revoked mapping %v", err)
	}
	if _, err = s.SendDelivery(ctx, d.ID, sender); !errors.Is(err, ErrForbidden) {
		t.Fatalf("revoked delivery %v", err)
	}
}
func TestChannelApprovalRequiresExactOriginAndPermission(t *testing.T) {
	s, _, p, _ := fixture(t)
	ctx := context.Background()
	o := Origin{Transport: "telegram", TenantID: "bot", ChannelID: "-123", ExternalUserID: "42"}
	b := bind(t, s, p, o, false)
	d, err := s.ProposeDecision(ctx, p, DecisionInput{RequestKey: "d", WorkRef: "work:1", Instruction: "Assign owner", Scope: "assignment", TargetRevision: "r1", Origin: &o})
	if err != nil {
		t.Fatal(err)
	}
	answer := AnswerInput{Revision: 1, Approve: true, Text: "Assign owner"}
	if _, err = s.AnswerFromChannel(ctx, o, d.ID, answer); !errors.Is(err, ErrForbidden) {
		t.Fatalf("readonly channel approval %v", err)
	}
	b.CanApprove = true
	if _, err = s.BindChannel(ctx, p, b); err != nil {
		t.Fatal(err)
	}
	moved := o
	moved.ChannelID = "-124"
	bind(t, s, p, moved, true)
	if _, err = s.AnswerFromChannel(ctx, moved, d.ID, answer); !errors.Is(err, ErrForbidden) {
		t.Fatalf("origin substitution %v", err)
	}
	if _, err = s.AnswerFromChannel(ctx, o, d.ID, answer); err != nil {
		t.Fatal(err)
	}
}
func TestTelegramWebhookAuthenticationAndDedup(t *testing.T) {
	s, _, p, count := fixture(t)
	o := Origin{Transport: "telegram", TenantID: "99", ChannelID: "-123", ExternalUserID: "42", ThreadID: "7"}
	bind(t, s, p, o, false)
	h := ChannelIngress{Service: s, TelegramSecret: strings.Repeat("s", 32), TelegramBotID: "99"}
	body := `{"update_id":123,"message":{"message_id":4,"message_thread_id":7,"chat":{"id":-123},"from":{"id":42},"text":"What changed?"}}`
	for i, secret := range []string{"wrong", h.TelegramSecret, h.TelegramSecret} {
		r := httptest.NewRequest("POST", "/webhooks/telegram", strings.NewReader(body))
		r.Header.Set("X-Telegram-Bot-Api-Secret-Token", secret)
		w := httptest.NewRecorder()
		h.Telegram(w, r)
		expected := 200
		if i == 0 {
			expected = 403
		}
		if w.Code != expected {
			t.Fatalf("status %d body %s", w.Code, w.Body.String())
		}
	}
	if *count != 1 {
		t.Fatalf("duplicate turn %d", *count)
	}
}
func TestDiscordSignedInteractionsStaleAndWrongTenant(t *testing.T) {
	s, _, p, count := fixture(t)
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	h := ChannelIngress{Service: s, DiscordPublicKey: pub, DiscordApplicationID: "app", Now: func() time.Time { return now }}
	bind(t, s, p, Origin{Transport: "discord", TenantID: "app/guild", ChannelID: "channel", ExternalUserID: "42"}, false)
	body := `{"type":2,"id":"i1","application_id":"app","guild_id":"guild","channel_id":"channel","member":{"user":{"id":"42"}},"data":{"name":"pm","options":[{"name":"text","value":"Status?"}]}}`
	for _, tc := range []struct {
		name, body string
		at         time.Time
		tamper     bool
		code       int
	}{{"valid", body, now, false, 200}, {"replay", body, now, false, 200}, {"stale", body, now.Add(-6 * time.Minute), false, 403}, {"tamper", body, now, true, 403}, {"tenant", strings.Replace(body, `"app"`, `"other"`, 1), now, false, 403}} {
		t.Run(tc.name, func(t *testing.T) {
			stamp := strconv.FormatInt(tc.at.Unix(), 10)
			sig := ed25519.Sign(priv, []byte(stamp+tc.body))
			if tc.tamper {
				sig[0] ^= 1
			}
			r := httptest.NewRequest("POST", "/webhooks/discord", strings.NewReader(tc.body))
			r.Header.Set("X-Signature-Timestamp", stamp)
			r.Header.Set("X-Signature-Ed25519", hex.EncodeToString(sig))
			w := httptest.NewRecorder()
			h.Discord(w, r)
			if w.Code != tc.code {
				t.Fatalf("%d %s", w.Code, w.Body.String())
			}
		})
	}
	if *count != 1 {
		t.Fatalf("duplicate Discord wake %d", *count)
	}
}
func TestTransportPayloadReceiptsAndNoCredentialErrors(t *testing.T) {
	for _, transport := range []string{"telegram", "discord"} {
		t.Run(transport, func(t *testing.T) {
			calls := 0
			sender := HTTPSender{TelegramToken: "secret", TelegramBotID: "bot", DiscordToken: "secret", DiscordApplicationID: "app"}
			sender.Client = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Fatal(err)
				}
				if r.Method != "POST" {
					t.Fatal(r.Method)
				}
				var raw string
				if transport == "telegram" {
					if body["chat_id"] != "-123" || body["message_thread_id"] != float64(7) {
						t.Fatalf("wrong origin %v", body)
					}
					raw = `{"ok":true,"result":{"message_id":55,"chat":{"id":-123}}}`
				} else {
					if body["allowed_mentions"] == nil || body["enforce_nonce"] != true {
						t.Fatalf("unsafe mentions/dedup %v", body)
					}
					raw = `{"id":"55","channel_id":"-123"}`
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(raw)), Header: make(http.Header)}, nil
			})}
			tenant := "bot"
			if transport == "discord" {
				tenant = "app/guild"
			}
			d := Delivery{ID: stableID("test"), Origin: Origin{Transport: transport, TenantID: tenant, ChannelID: "-123", ThreadID: "7", ExternalUserID: "42"}, Text: "@everyone Source reports done, pending verification."}
			receipt, err := sender.Send(context.Background(), d)
			if err != nil || receipt.Status != Delivered || receipt.ExternalID != "55" || receipt.IndependentlyVerified {
				t.Fatalf("receipt %v %+v", err, receipt)
			}
			sender.Client = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				calls++
				return nil, errors.New("https://secret credential detail")
			})}
			_, err = sender.Send(context.Background(), d)
			if err == nil || strings.Contains(err.Error(), "secret") {
				t.Fatalf("credential leak %v", err)
			}
			if calls != 2 {
				t.Fatalf("unexpected retry %d", calls)
			}
		})
	}
}

func TestLongReplyOutboxIsAtomicOrderedAndNeverResendsUnknown(t *testing.T) {
	s, _, p, _ := fixture(t)
	ctx := context.Background()
	o := Origin{Transport: "discord", TenantID: "app/guild", ChannelID: "channel", ExternalUserID: "42"}
	bind(t, s, p, o, false)
	turn, err := s.ReceiveChannel(ctx, o, "event", "Status?")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = claimAndComplete(t, s, ctx, Principal{WorkspaceID: "ws", ActorID: "pm-agent"}, turn.ID, strings.Repeat("Evidence 🌍. ", 400), nil); err != nil {
		t.Fatal(err)
	}
	fragments, err := s.QueueTurnDeliveries(ctx, turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(fragments) < 2 {
		t.Fatal("reply not fragmented")
	}
	calls := 0
	sender := sendFunc(func(context.Context, Delivery) (Receipt, error) { calls++; return Receipt{}, errors.New("uncertain") })
	drained, err := s.DeliverPending(ctx, sender, 50)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || len(drained) != 1 || drained[0].Status != Unknown {
		t.Fatalf("continued after uncertain fragment: %d %+v", calls, drained)
	}
	if _, err = s.DeliverPending(ctx, sender, 50); err != nil || calls != 1 {
		t.Fatalf("resent unknown: %d %v", calls, err)
	}
	if _, err = s.ReconcileDelivery(ctx, p, fragments[0].ID, Receipt{Status: Delivered, ExternalID: "message-1", EvidenceRefs: []string{"artifact:readback"}}); err != nil {
		t.Fatal(err)
	}
	sender = sendFunc(func(_ context.Context, d Delivery) (Receipt, error) {
		calls++
		if len(d.Text) > 1900 {
			t.Fatal("unbounded fragment")
		}
		return Receipt{Status: Delivered, ExternalID: d.ID}, nil
	})
	if _, err = s.DeliverPending(ctx, sender, 50); err != nil {
		t.Fatal(err)
	}
	if calls != len(fragments) {
		t.Fatalf("missing/duplicate fragment %d want %d", calls, len(fragments))
	}
}

func TestExplicitDeliveryRetryRequiresVerifiedNonDelivery(t *testing.T) {
	s, _, p, _ := fixture(t)
	ctx := context.Background()
	o := Origin{Transport: "telegram", TenantID: "bot", ChannelID: "chat", ExternalUserID: "42"}
	bind(t, s, p, o, false)
	turn, err := s.ReceiveChannel(ctx, o, "one", "Status?")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = claimAndComplete(t, s, ctx, Principal{WorkspaceID: "ws", ActorID: "pm-agent"}, turn.ID, "Response", nil); err != nil {
		t.Fatal(err)
	}
	d, err := s.QueueTurnDelivery(ctx, turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	d, err = s.SendDelivery(ctx, d.ID, sendFunc(func(context.Context, Delivery) (Receipt, error) { return Receipt{}, errors.New("lost response") }))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.RetryFailedDelivery(ctx, p, d.ID, d.Revision, "retry"); !errors.Is(err, ErrConflict) {
		t.Fatalf("unknown retried %v", err)
	}
	r := Receipt{Status: Failed, ExternalID: "lookup-1", EvidenceRefs: []string{"artifact:delivery-absent"}}
	if _, err = s.ReconcileDelivery(ctx, p, d.ID, r); !errors.Is(err, ErrInvalid) {
		t.Fatalf("unverified absence %v", err)
	}
	r.IndependentlyVerified = true
	d, err = s.ReconcileDelivery(ctx, p, d.ID, r)
	if err != nil {
		t.Fatal(err)
	}
	revision := d.Revision
	d, err = s.RetryFailedDelivery(ctx, p, d.ID, revision, "retry")
	if err != nil || d.Status != Pending {
		t.Fatalf("explicit retry %+v %v", d, err)
	}
	d, err = s.SendDelivery(ctx, d.ID, sendFunc(func(context.Context, Delivery) (Receipt, error) {
		return Receipt{Status: Delivered, ExternalID: "remote-2"}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Attempts) != 2 || d.Attempts[0].Status != Unknown || d.Attempts[1].Status != Delivered {
		t.Fatalf("attempt history lost %+v", d)
	}
	replay, err := s.RetryFailedDelivery(ctx, p, d.ID, revision, "retry")
	if err != nil || replay.Status != Delivered {
		t.Fatalf("retry approval replay reopened send %+v %v", replay, err)
	}
}

func TestDeliverPendingMarksZeroMessageRepliesDoneAndUsesIndexedOutbox(t *testing.T) {
	s, st, p, _ := fixture(t)
	ctx := context.Background()
	o := Origin{Transport: "telegram", TenantID: "bot", ChannelID: "chat", ExternalUserID: "42"}
	bind(t, s, p, o, false)
	convID := stableID("conversation", p.WorkspaceID, p.ActorID, bindingID(o))

	// Create the channel conversation via a first turn, then seed 60 delivered
	// turns whose replies split into zero messages (exceeding the 50-row LIMIT).
	first, err := s.ReceiveChannel(ctx, o, "seed-0", "Status?")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = claimAndComplete(t, s, ctx, Principal{WorkspaceID: "ws", ActorID: "pm-agent"}, first.ID, "initial", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.ExecContext(ctx, `UPDATE pm_records SET body=CAST(json_set(body,'$.response','') AS BLOB) WHERE kind='turn' AND id=?`, first.ID); err != nil {
		t.Fatal(err)
	}
	for i := 1; i < 60; i++ {
		turnID := stableID("turn", convID, "empty", strconv.Itoa(i))
		emptyTurn := Turn{
			ID:             turnID,
			ConversationID: convID,
			WorkspaceID:    p.WorkspaceID,
			ActorID:        p.ActorID,
			Text:           "Status?",
			Response:       "",
			Status:         Delivered,
			AgentActorID:   "pm-agent",
			Origin:         &o,
			CreatedAt:      time.Now().UTC(),
			Deadline:       time.Now().UTC().Add(time.Minute),
			Revision:       2,
		}
		if _, err := st.insert(ctx, "turn", turnID, p.WorkspaceID, p.ActorID, convID, emptyTurn); err != nil {
			t.Fatal(err)
		}
	}

	realTurn, err := s.ReceiveChannel(ctx, o, "real-turn", "What now?")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = claimAndComplete(t, s, ctx, Principal{WorkspaceID: "ws", ActorID: "pm-agent"}, realTurn.ID, "Actionable reply", nil); err != nil {
		t.Fatal(err)
	}

	var sent []string
	sender := sendFunc(func(_ context.Context, d Delivery) (Receipt, error) {
		sent = append(sent, d.Text)
		return Receipt{Status: Delivered, ExternalID: "msg-" + strconv.Itoa(len(sent))}, nil
	})
	drained, err := s.DeliverPending(ctx, sender, 50)
	if err != nil {
		t.Fatalf("DeliverPending failed: %v", err)
	}
	if len(drained) != 1 || len(sent) != 1 || sent[0] != "Actionable reply" {
		t.Fatalf("expected real turn delivery past 60 empty turns, got drained=%+v sent=%v", drained, sent)
	}

	var unqueued int
	if err := st.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM pm_records WHERE kind='turn' AND workspace_id=? AND json_extract(body,'$.status')='delivered' AND COALESCE(json_extract(body,'$.delivery_queued'),0)=0`, p.WorkspaceID).Scan(&unqueued); err != nil {
		t.Fatal(err)
	}
	if unqueued != 0 {
		t.Fatalf("expected all delivered turns marked delivery_queued=1, found %d unqueued", unqueued)
	}

	rows, err := st.db.QueryContext(ctx, "EXPLAIN QUERY PLAN "+deliverPendingTurnsSQL, p.WorkspaceID, 50)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	usedOutboxIndex := false
	for rows.Next() {
		var id, parent, unused int
		var detail string
		if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(detail, "SCAN") || strings.Contains(detail, "TEMP B-TREE") {
			t.Fatalf("unbounded outbox turn query plan: %s", detail)
		}
		if strings.Contains(detail, "pm_records_turn_outbox") {
			usedOutboxIndex = true
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if !usedOutboxIndex {
		t.Fatal("expected deliverPendingTurnsSQL to use pm_records_turn_outbox index")
	}
}
