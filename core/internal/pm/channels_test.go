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
	if _, err = s.ReceiveChannel(ctx, o, "1", "What changed?"); err != nil || *count != 1 {
		t.Fatalf("replay %v %d", err, *count)
	}
	if _, err = s.CompleteTurn(ctx, Principal{WorkspaceID: "ws", ActorID: "pm-agent"}, turn.ID, "The source reports progress; verification is pending.", []string{"artifact:1"}); err != nil {
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
