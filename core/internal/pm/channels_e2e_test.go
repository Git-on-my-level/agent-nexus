package pm

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	channels "agent-nexus-test-channels"
)

func TestChannelE2ETelegramBoundTurnReplyDedupAndUnbound(t *testing.T) {
	s, _, p, _ := fixture(t)
	tg := channels.NewTelegram("secret", strings.Repeat("s", 32))
	defer tg.Close()
	o := Origin{Transport: "telegram", TenantID: "99", ChannelID: "-123", ExternalUserID: "42", ThreadID: "7"}
	bind(t, s, p, o, true)
	h := ChannelIngress{Service: s, TelegramSecret: strings.Repeat("s", 32), TelegramBotID: "99"}
	sender := HTTPSender{TelegramToken: "secret", TelegramBotID: "99", TelegramAPIBase: tg.URL()}
	postTG := func(updateID int64, text string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/pm/ingress/telegram", strings.NewReader(string(channels.TelegramMessage(updateID, -123, 42, 4, text, 7))))
		r.Header.Set("X-Telegram-Bot-Api-Secret-Token", h.TelegramSecret)
		w := httptest.NewRecorder()
		h.Telegram(w, r)
		return w
	}
	if w := postTG(1, "what needs my decision?"); w.Code != 200 {
		t.Fatalf("ingress %d %s", w.Code, w.Body.String())
	}
	if w := postTG(1, "what needs my decision?"); w.Code != 200 {
		t.Fatalf("dup %d %s", w.Code, w.Body.String())
	}
	turns, err := s.ConversationHistory(context.Background(), p, stableID("conversation", p.WorkspaceID, p.ActorID, bindingID(o)), 20, "")
	if err != nil || len(turns.Turns) != 1 {
		t.Fatalf("turns %d %v", len(turns.Turns), err)
	}
	if _, err = s.CompleteTurn(context.Background(), Principal{WorkspaceID: "ws", ActorID: "pm-agent"}, turns.Turns[0].ID, "Nothing is waiting on you.", nil); err != nil {
		t.Fatal(err)
	}
	if _, err = s.DeliverPending(context.Background(), sender, 50); err != nil {
		t.Fatal(err)
	}
	if n := len(tg.Sends()); n != 1 {
		t.Fatalf("want 1 send got %d", n)
	}
	if _, err = s.DeliverPending(context.Background(), sender, 50); err != nil {
		t.Fatal(err)
	}
	if n := len(tg.Sends()); n != 1 {
		t.Fatalf("duplicate reply %d", n)
	}

	unbound := httptest.NewRequest("POST", "/pm/ingress/telegram", strings.NewReader(string(channels.TelegramMessage(2, -123, 99, 5, "hello", 7))))
	unbound.Header.Set("X-Telegram-Bot-Api-Secret-Token", h.TelegramSecret)
	uw := httptest.NewRecorder()
	h.Telegram(uw, unbound)
	if uw.Code != 200 || !strings.Contains(uw.Body.String(), "not bound") {
		t.Fatalf("unbound %d %s", uw.Code, uw.Body.String())
	}
	after, err := s.ConversationHistory(context.Background(), p, stableID("conversation", p.WorkspaceID, p.ActorID, bindingID(o)), 20, "")
	if err != nil || len(after.Turns) != 1 {
		t.Fatalf("unbound created a turn: %d %v", len(after.Turns), err)
	}
}

func TestChannelE2EDiscordInteractionAndComponent(t *testing.T) {
	s, _, p, _ := fixture(t)
	fake := channels.NewDiscord("secret", "app")
	defer fake.Close()
	o := Origin{Transport: "discord", TenantID: "app/guild", ChannelID: "channel", ExternalUserID: "42"}
	bind(t, s, p, o, true)
	h := ChannelIngress{Service: s, DiscordPublicKey: fake.PublicKey, DiscordApplicationID: "app", Now: func() time.Time { return time.Now().UTC() }}
	sender := HTTPSender{DiscordToken: "secret", DiscordApplicationID: "app", DiscordAPIBase: fake.URL() + "/api/v10"}
	post := func(body []byte) *httptest.ResponseRecorder {
		stamp, sig := fake.Sign(body, time.Now().UTC())
		r := httptest.NewRequest("POST", "/pm/ingress/discord", strings.NewReader(string(body)))
		r.Header.Set("X-Signature-Timestamp", stamp)
		r.Header.Set("X-Signature-Ed25519", sig)
		w := httptest.NewRecorder()
		h.Discord(w, r)
		return w
	}
	body := channels.DiscordCommand("i1", "app", "guild", "channel", "42", "pm", map[string]any{"text": "what needs my decision?"})
	if w := post(body); w.Code != 200 {
		t.Fatalf("ingress %d %s", w.Code, w.Body.String())
	}
	if w := post(body); w.Code != 200 {
		t.Fatalf("dup %d %s", w.Code, w.Body.String())
	}
	turns, err := s.ConversationHistory(context.Background(), p, stableID("conversation", p.WorkspaceID, p.ActorID, bindingID(o)), 20, "")
	if err != nil || len(turns.Turns) != 1 {
		t.Fatalf("turns %d %v", len(turns.Turns), err)
	}
	if _, err = s.CompleteTurn(context.Background(), Principal{WorkspaceID: "ws", ActorID: "pm-agent"}, turns.Turns[0].ID, "Nothing is waiting on you.", nil); err != nil {
		t.Fatal(err)
	}
	if _, err = s.DeliverPending(context.Background(), sender, 50); err != nil {
		t.Fatal(err)
	}
	if n := len(fake.Sends()); n != 1 {
		t.Fatalf("want 1 send got %d %+v", n, fake.Sends())
	}

	unbound := channels.DiscordCommand("i2", "app", "guild", "channel", "99", "pm", map[string]any{"text": "hello"})
	w := post(unbound)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "not bound") {
		t.Fatalf("unbound %d %s", w.Code, w.Body.String())
	}
}

func TestChannelE2EDecisionApproveRejectAndStaleCard(t *testing.T) {
	s, _, p, _ := fixture(t)
	tg := channels.NewTelegram("secret", strings.Repeat("s", 32))
	defer tg.Close()
	o := Origin{Transport: "telegram", TenantID: "bot", ChannelID: "-123", ExternalUserID: "42"}
	bind(t, s, p, o, true)
	h := ChannelIngress{Service: s, TelegramSecret: strings.Repeat("s", 32), TelegramBotID: "bot"}
	sender := HTTPSender{TelegramToken: "secret", TelegramBotID: "bot", TelegramAPIBase: tg.URL()}
	d, err := s.ProposeDecision(context.Background(), p, DecisionInput{RequestKey: "d1", WorkRef: "work:1", Instruction: "Assign owner", Scope: "assignment", TargetRevision: "r1", Origin: &o})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.DeliverPending(context.Background(), sender, 50); err != nil {
		t.Fatal(err)
	}
	sends := tg.Sends()
	if len(sends) != 1 {
		t.Fatalf("card sends %d", len(sends))
	}
	raw, _ := json.Marshal(sends[0].Body["reply_markup"])
	if !strings.Contains(string(raw), "Approve") || !strings.Contains(string(raw), "a:"+d.ID+":1") {
		t.Fatalf("markup %s", raw)
	}
	stale := httptest.NewRequest("POST", "/pm/ingress/telegram", strings.NewReader(string(channels.TelegramCallback(10, -123, 42, "a:"+d.ID+":0", 0))))
	stale.Header.Set("X-Telegram-Bot-Api-Secret-Token", h.TelegramSecret)
	sw := httptest.NewRecorder()
	h.Telegram(sw, stale)
	if sw.Code != 200 || !strings.Contains(sw.Body.String(), "Revision 1") {
		t.Fatalf("stale %d %s", sw.Code, sw.Body.String())
	}
	ok := httptest.NewRequest("POST", "/pm/ingress/telegram", strings.NewReader(string(channels.TelegramCallback(11, -123, 42, "a:"+d.ID+":1", 0))))
	ok.Header.Set("X-Telegram-Bot-Api-Secret-Token", h.TelegramSecret)
	ow := httptest.NewRecorder()
	h.Telegram(ow, ok)
	if ow.Code != 200 {
		t.Fatalf("approve %d %s", ow.Code, ow.Body.String())
	}
	got, err := s.decision(context.Background(), p, d.ID, "pm.read")
	if err != nil || got.Status != Answered {
		t.Fatalf("decision %+v %v", got, err)
	}
}

func TestChannelE2EDiscordComponentApprove(t *testing.T) {
	s, _, p, _ := fixture(t)
	fake := channels.NewDiscord("secret", "app")
	defer fake.Close()
	o := Origin{Transport: "discord", TenantID: "app/guild", ChannelID: "channel", ExternalUserID: "42"}
	bind(t, s, p, o, true)
	h := ChannelIngress{Service: s, DiscordPublicKey: fake.PublicKey, DiscordApplicationID: "app", Now: func() time.Time { return time.Now().UTC() }}
	sender := HTTPSender{DiscordToken: "secret", DiscordApplicationID: "app", DiscordAPIBase: fake.URL() + "/api/v10"}
	d, err := s.ProposeDecision(context.Background(), p, DecisionInput{RequestKey: "dx", WorkRef: "work:1", Instruction: "Ship", Scope: "assignment", TargetRevision: "r1", Origin: &o})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.DeliverPending(context.Background(), sender, 50); err != nil {
		t.Fatal(err)
	}
	if len(fake.Sends()) != 1 {
		t.Fatalf("sends %d", len(fake.Sends()))
	}
	body := channels.DiscordComponent("c1", "app", "guild", "channel", "42", "pm:a:"+d.ID+":1")
	stamp, sig := fake.Sign(body, time.Now().UTC())
	r := httptest.NewRequest("POST", "/pm/ingress/discord", strings.NewReader(string(body)))
	r.Header.Set("X-Signature-Timestamp", stamp)
	r.Header.Set("X-Signature-Ed25519", sig)
	w := httptest.NewRecorder()
	h.Discord(w, r)
	if w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	got, err := s.decision(context.Background(), p, d.ID, "pm.read")
	if err != nil || got.Status != Answered {
		t.Fatalf("decision %+v %v", got, err)
	}
}

func TestChannelE2ERetry429ThenDeliverOnceAndRestart(t *testing.T) {
	s, st, p, _ := fixture(t)
	tg := channels.NewTelegram("secret", strings.Repeat("s", 32))
	defer tg.Close()
	tg.FailNext(429)
	o := Origin{Transport: "telegram", TenantID: "bot", ChannelID: "chat", ExternalUserID: "42"}
	bind(t, s, p, o, false)
	turn, err := s.ReceiveChannel(context.Background(), o, "one", "Status?")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.CompleteTurn(context.Background(), Principal{WorkspaceID: "ws", ActorID: "pm-agent"}, turn.ID, "Response", nil); err != nil {
		t.Fatal(err)
	}
	sender := HTTPSender{TelegramToken: "secret", TelegramBotID: "bot", TelegramAPIBase: tg.URL()}
	drained, err := s.DeliverPending(context.Background(), sender, 50)
	if err != nil || len(drained) != 1 || drained[0].Status != Pending {
		t.Fatalf("429 drain %+v %v", drained, err)
	}
	if n := len(tg.Sends()); n != 1 {
		t.Fatalf("first sends %d", n)
	}
	restarted, err := NewService(st, s.cfg, s.deps)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = restarted.DeliverPending(context.Background(), sender, 50); err != nil {
		t.Fatal(err)
	}
	if n := len(tg.Sends()); n != 2 {
		t.Fatalf("retry sends %d", n)
	}
	if tg.Sends()[1].Status != 200 {
		t.Fatalf("second %d", tg.Sends()[1].Status)
	}
	if _, err = restarted.DeliverPending(context.Background(), sender, 50); err != nil {
		t.Fatal(err)
	}
	if n := len(tg.Sends()); n != 2 {
		t.Fatalf("restart duplicate %d", n)
	}
}

func TestChannelE2EDiscord409RetriesThenDeliversOnce(t *testing.T) {
	s, _, p, _ := fixture(t)
	fake := channels.NewDiscord("secret", "app")
	defer fake.Close()
	fake.FailNext(409)
	o := Origin{Transport: "discord", TenantID: "app/guild", ChannelID: "channel", ExternalUserID: "42"}
	bind(t, s, p, o, false)
	turn, err := s.ReceiveChannel(context.Background(), o, "one", "Status?")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.CompleteTurn(context.Background(), Principal{WorkspaceID: "ws", ActorID: "pm-agent"}, turn.ID, "Response", nil); err != nil {
		t.Fatal(err)
	}
	sender := HTTPSender{DiscordToken: "secret", DiscordApplicationID: "app", DiscordAPIBase: fake.URL() + "/api/v10"}
	drained, err := s.DeliverPending(context.Background(), sender, 50)
	if err != nil || len(drained) != 1 || drained[0].Status != Pending {
		t.Fatalf("409 drain %+v %v", drained, err)
	}
	if _, err = s.DeliverPending(context.Background(), sender, 50); err != nil {
		t.Fatal(err)
	}
	if n := len(fake.Sends()); n != 2 || fake.Sends()[1].Status != 200 {
		t.Fatalf("sends %+v", fake.Sends())
	}
}

func TestDeliveryBackoffCapsAndHonorsRetryAfter(t *testing.T) {
	if got := deliveryBackoff(1, 0, true); got != 0 {
		t.Fatalf("retry_after 0 => %s", got)
	}
	if got := deliveryBackoff(10, 0, false); got != deliveryBackoffCap {
		t.Fatalf("cap %s", got)
	}
	if got := deliveryBackoff(1, 0, false); got != time.Second {
		t.Fatalf("exp %s", got)
	}
}

func TestBindingPageListsExactIdentities(t *testing.T) {
	s, _, p, _ := fixture(t)
	o := Origin{Transport: "telegram", TenantID: "bot", ChannelID: "chat", ExternalUserID: "42"}
	bind(t, s, p, o, true)
	page, err := s.BindingPage(context.Background(), p)
	if err != nil || len(page.Items) != 1 || page.Items[0].Origin != o {
		t.Fatalf("%+v %v", page, err)
	}
}

func TestParseDecisionCallback(t *testing.T) {
	approve, id, rev, ok := parseDecisionCallback("a:pm_abc:3")
	if !ok || !approve || id != "pm_abc" || rev != 3 {
		t.Fatalf("%v %s %d %v", approve, id, rev, ok)
	}
	approve, id, rev, ok = parseDecisionCallback("pm:r:pm_abc:1")
	if !ok || approve || id != "pm_abc" || rev != 1 {
		t.Fatalf("reject %v %s %d", approve, id, rev)
	}
}

func TestTelegramSlashApproveStillWorks(t *testing.T) {
	s, _, p, _ := fixture(t)
	o := Origin{Transport: "telegram", TenantID: "99", ChannelID: "-123", ExternalUserID: "42"}
	bind(t, s, p, o, true)
	d, err := s.ProposeDecision(context.Background(), p, DecisionInput{RequestKey: "cmd", WorkRef: "work:1", Instruction: "Assign owner", Scope: "assignment", TargetRevision: "r1", Origin: &o})
	if err != nil {
		t.Fatal(err)
	}
	h := ChannelIngress{Service: s, TelegramSecret: strings.Repeat("s", 32), TelegramBotID: "99"}
	body := channels.TelegramMessage(3, -123, 42, 8, "/pm-approve "+d.ID+" 1 Assign owner", 0)
	r := httptest.NewRequest("POST", "/pm/ingress/telegram", strings.NewReader(string(body)))
	r.Header.Set("X-Telegram-Bot-Api-Secret-Token", h.TelegramSecret)
	w := httptest.NewRecorder()
	h.Telegram(w, r)
	if w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	got, err := s.decision(context.Background(), p, d.ID, "pm.read")
	if err != nil || got.Status != Answered {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestHTTPSenderUsesConfiguredBaseAnd429(t *testing.T) {
	tg := channels.NewTelegram("secret", strings.Repeat("s", 32))
	defer tg.Close()
	tg.FailNext(429)
	sender := HTTPSender{TelegramToken: "secret", TelegramBotID: "bot", TelegramAPIBase: tg.URL()}
	d := Delivery{ID: stableID("t"), Origin: Origin{Transport: "telegram", TenantID: "bot", ChannelID: strconv.Itoa(-123), ExternalUserID: "42"}, Text: "hello"}
	_, err := sender.Send(context.Background(), d)
	if _, _, ok := retryAfterOf(err); !ok {
		t.Fatalf("429 %v", err)
	}
	receipt, err := sender.Send(context.Background(), d)
	if err != nil || receipt.Status != Delivered || receipt.ExternalID == "" {
		t.Fatalf("%v %+v", err, receipt)
	}
}
