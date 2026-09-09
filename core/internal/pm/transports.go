package pm

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	defaultTelegramAPI = "https://api.telegram.org"
	defaultDiscordAPI  = "https://discord.com/api/v10"
)

// ChannelIngress is mounted only by the deployment owner. It never registers a
// webhook, polls getUpdates, or opens a Discord gateway connection.
type ChannelIngress struct {
	Service              *Service
	TelegramSecret       string
	TelegramBotID        string
	DiscordPublicKey     ed25519.PublicKey
	DiscordApplicationID string
	Now                  func() time.Time
}

func (h ChannelIngress) Telegram(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}
	if h.Service == nil || len(h.TelegramSecret) < 32 || h.TelegramBotID == "" {
		writeError(w, ErrUnavailable)
		return
	}
	secret := r.Header.Get("X-Telegram-Bot-Api-Secret-Token")
	if subtle.ConstantTimeCompare([]byte(secret), []byte(h.TelegramSecret)) != 1 {
		writeError(w, ErrForbidden)
		return
	}
	var update struct {
		UpdateID      *int64           `json:"update_id"`
		Message       *telegramMessage `json:"message"`
		CallbackQuery *struct {
			ID      string           `json:"id"`
			From    *telegramUser    `json:"from"`
			Message *telegramMessage `json:"message"`
			Data    string           `json:"data"`
		} `json:"callback_query"`
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 64*1024))
	if err != nil || json.Unmarshal(body, &update) != nil {
		writeError(w, ErrInvalid)
		return
	}
	if update.UpdateID == nil {
		writeError(w, ErrInvalid)
		return
	}
	if update.CallbackQuery != nil {
		h.telegramCallback(w, r, *update.UpdateID, update.CallbackQuery.ID, update.CallbackQuery.From, update.CallbackQuery.Message, update.CallbackQuery.Data)
		return
	}
	if update.Message == nil || update.Message.From == nil || update.Message.From.IsBot || update.Message.From.ID == 0 || update.Message.Chat.ID == 0 || update.Message.MessageID == 0 {
		writeError(w, ErrInvalid)
		return
	}
	m := update.Message
	o := telegramOrigin(h.TelegramBotID, m)
	text := m.Text
	if strings.HasPrefix(text, "/pm-approve ") || strings.HasPrefix(text, "/pm-reject ") {
		fields := strings.Fields(text)
		if len(fields) < 4 {
			writeError(w, ErrInvalid)
			return
		}
		revision, err := strconv.Atoi(fields[2])
		if err != nil {
			writeError(w, ErrInvalid)
			return
		}
		h.channelAnswer(w, r, o, fields[1], AnswerInput{Revision: revision, Approve: fields[0] == "/pm-approve", Text: strings.Join(fields[3:], " ")}, strconv.FormatInt(*update.UpdateID, 10), "telegram")
		return
	}
	if strings.HasPrefix(text, "/pm ") {
		text = strings.TrimPrefix(text, "/pm ")
	}
	h.channelReceive(w, r, o, strconv.FormatInt(*update.UpdateID, 10), text, "telegram")
}

type telegramUser struct {
	ID    int64 `json:"id"`
	IsBot bool  `json:"is_bot"`
}
type telegramMessage struct {
	MessageID int64         `json:"message_id"`
	Text      string        `json:"text"`
	ThreadID  int64         `json:"message_thread_id"`
	From      *telegramUser `json:"from"`
	Chat      struct {
		ID int64 `json:"id"`
	} `json:"chat"`
}

func telegramOrigin(botID string, m *telegramMessage) Origin {
	o := Origin{Transport: "telegram", TenantID: botID, ChannelID: strconv.FormatInt(m.Chat.ID, 10), ExternalUserID: strconv.FormatInt(m.From.ID, 10)}
	if m.ThreadID != 0 {
		o.ThreadID = strconv.FormatInt(m.ThreadID, 10)
	}
	return o
}

func (h ChannelIngress) telegramCallback(w http.ResponseWriter, r *http.Request, updateID int64, callbackID string, from *telegramUser, m *telegramMessage, data string) {
	if from == nil || from.IsBot || from.ID == 0 || m == nil || m.Chat.ID == 0 {
		writeError(w, ErrInvalid)
		return
	}
	m.From = from
	o := telegramOrigin(h.TelegramBotID, m)
	approve, id, revision, ok := parseDecisionCallback(data)
	if !ok {
		writeError(w, ErrInvalid)
		return
	}
	eventID := callbackID
	if eventID == "" {
		eventID = strconv.FormatInt(updateID, 10)
	}
	text := "Approved from Telegram"
	if !approve {
		text = "Rejected from Telegram"
	}
	h.channelAnswer(w, r, o, id, AnswerInput{Revision: revision, Approve: approve, Text: text}, eventID, "telegram")
}

func (h ChannelIngress) Discord(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}
	if h.Service == nil || len(h.DiscordPublicKey) != ed25519.PublicKeySize || h.DiscordApplicationID == "" {
		writeError(w, ErrUnavailable)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 64*1024))
	if err != nil {
		writeError(w, ErrInvalid)
		return
	}
	stamp := r.Header.Get("X-Signature-Timestamp")
	ts, err := strconv.ParseInt(stamp, 10, 64)
	if err != nil {
		writeError(w, ErrForbidden)
		return
	}
	now := time.Now()
	if h.Now != nil {
		now = h.Now()
	}
	delta := now.Sub(time.Unix(ts, 0))
	if delta > 5*time.Minute || delta < -30*time.Second {
		writeError(w, ErrForbidden)
		return
	}
	sig, err := hex.DecodeString(r.Header.Get("X-Signature-Ed25519"))
	if err != nil || !ed25519.Verify(h.DiscordPublicKey, append([]byte(stamp), body...), sig) {
		writeError(w, ErrForbidden)
		return
	}
	type user struct {
		ID  string `json:"id"`
		Bot bool   `json:"bot"`
	}
	var in struct {
		Type          int    `json:"type"`
		ID            string `json:"id"`
		ApplicationID string `json:"application_id"`
		GuildID       string `json:"guild_id"`
		ChannelID     string `json:"channel_id"`
		User          *user  `json:"user"`
		Member        *struct {
			User user `json:"user"`
		} `json:"member"`
		Data struct {
			Name          string `json:"name"`
			CustomID      string `json:"custom_id"`
			ComponentType int    `json:"component_type"`
			Options       []struct {
				Name  string          `json:"name"`
				Value json.RawMessage `json:"value"`
			} `json:"options"`
		} `json:"data"`
	}
	if json.Unmarshal(body, &in) != nil || in.ApplicationID != h.DiscordApplicationID {
		writeError(w, ErrForbidden)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if in.Type == 1 {
		_ = json.NewEncoder(w).Encode(map[string]int{"type": 1})
		return
	}
	if (in.Type != 2 && in.Type != 3) || in.ID == "" {
		writeError(w, ErrInvalid)
		return
	}
	u := in.User
	if in.Member != nil {
		u = &in.Member.User
	}
	if u == nil || u.ID == "" || u.Bot {
		writeError(w, ErrForbidden)
		return
	}
	tenant := in.ApplicationID + "/" + in.GuildID
	if in.GuildID == "" {
		tenant = in.ApplicationID + "/dm"
	}
	o := Origin{Transport: "discord", TenantID: tenant, ChannelID: in.ChannelID, ExternalUserID: u.ID}
	if in.Type == 3 {
		approve, id, revision, ok := parseDecisionCallback(in.Data.CustomID)
		if !ok {
			writeError(w, ErrInvalid)
			return
		}
		text := "Approved from Discord"
		if !approve {
			text = "Rejected from Discord"
		}
		h.channelAnswer(w, r, o, id, AnswerInput{Revision: revision, Approve: approve, Text: text}, in.ID, "discord")
		return
	}
	values := map[string]json.RawMessage{}
	for _, opt := range in.Data.Options {
		if _, exists := values[opt.Name]; exists {
			writeError(w, ErrInvalid)
			return
		}
		values[opt.Name] = opt.Value
	}
	var text string
	_ = json.Unmarshal(values["text"], &text)
	switch in.Data.Name {
	case "pm":
		h.channelReceive(w, r, o, in.ID, text, "discord")
	case "pm-approve", "pm-reject":
		var id string
		var revision int
		if json.Unmarshal(values["decision"], &id) != nil || json.Unmarshal(values["revision"], &revision) != nil {
			writeError(w, ErrInvalid)
			return
		}
		h.channelAnswer(w, r, o, id, AnswerInput{Revision: revision, Approve: in.Data.Name == "pm-approve", Text: text}, in.ID, "discord")
	default:
		writeError(w, ErrInvalid)
	}
}

func (h ChannelIngress) channelReceive(w http.ResponseWriter, r *http.Request, o Origin, eventID, text, transport string) {
	_, err := h.Service.ReceiveChannel(r.Context(), o, eventID, text)
	if err != nil {
		if errors.Is(err, ErrForbidden) {
			writeBindFirst(w, o, transport)
			return
		}
		writeError(w, err)
		return
	}
	writeChannelAck(w, transport)
}

func (h ChannelIngress) channelAnswer(w http.ResponseWriter, r *http.Request, o Origin, id string, in AnswerInput, eventID, transport string) {
	d, err := h.Service.AnswerFromChannel(r.Context(), o, id, in)
	if err != nil {
		if errors.Is(err, ErrForbidden) {
			writeBindFirst(w, o, transport)
			return
		}
		if errors.Is(err, ErrConflict) {
			current := h.freshDecision(r.Context(), o, id)
			if current.ID != "" {
				_, _ = h.Service.QueueDecisionCard(r.Context(), current, eventID)
			}
			writeFreshCard(w, current, transport)
			return
		}
		writeError(w, err)
		return
	}
	_ = d
	writeChannelAck(w, transport)
}

func (h ChannelIngress) freshDecision(ctx context.Context, o Origin, id string) Decision {
	b, err := h.Service.ResolveBinding(ctx, o)
	if err != nil {
		return Decision{}
	}
	d, err := h.Service.decision(ctx, Principal{WorkspaceID: b.WorkspaceID, ActorID: b.ActorID, Human: true}, id, "pm.read")
	if err != nil {
		return Decision{}
	}
	return d
}

func writeBindFirst(w http.ResponseWriter, o Origin, transport string) {
	w.Header().Set("Content-Type", "application/json")
	if transport == "discord" {
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{"type": 4, "data": map[string]any{"content": bindFirstText, "flags": 64, "allowed_mentions": map[string]any{"parse": []string{}}}})
		return
	}
	payload := map[string]any{"method": "sendMessage", "chat_id": o.ChannelID, "text": bindFirstText, "protect_content": true}
	if o.ThreadID != "" {
		if thread, err := strconv.ParseInt(o.ThreadID, 10, 64); err == nil {
			payload["message_thread_id"] = thread
		}
	}
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeChannelAck(w http.ResponseWriter, transport string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if transport == "discord" {
		_ = json.NewEncoder(w).Encode(map[string]any{"type": 4, "data": map[string]any{"content": "Received by PM. Follow-through is tracked in your conversation.", "flags": 64, "allowed_mentions": map[string]any{"parse": []string{}}}})
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}

func writeFreshCard(w http.ResponseWriter, d Decision, transport string) {
	if d.ID == "" || d.Origin == nil {
		writeChannelAck(w, transport)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	text := decisionCardText(d)
	if transport == "discord" {
		_ = json.NewEncoder(w).Encode(map[string]any{"type": 4, "data": map[string]any{"content": text, "flags": 64, "allowed_mentions": map[string]any{"parse": []string{}}, "components": discordDecisionComponents(d)}})
		return
	}
	payload := map[string]any{"method": "sendMessage", "chat_id": d.Origin.ChannelID, "text": text, "protect_content": true, "reply_markup": telegramDecisionMarkup(d)}
	if d.Origin.ThreadID != "" {
		if thread, err := strconv.ParseInt(d.Origin.ThreadID, 10, 64); err == nil {
			payload["message_thread_id"] = thread
		}
	}
	_ = json.NewEncoder(w).Encode(payload)
}

// HTTPSender talks to official APIs. Client injection is for transport tests;
// redirects are always disabled to avoid credential forwarding. No retries
// inside a single Send; the outbox applies backoff between attempts.
type HTTPSender struct {
	TelegramToken        string
	TelegramBotID        string
	TelegramAPIBase      string
	DiscordToken         string
	DiscordApplicationID string
	DiscordAPIBase       string
	Client               *http.Client
}

func (s HTTPSender) telegramBase() string {
	if strings.TrimSpace(s.TelegramAPIBase) != "" {
		return strings.TrimRight(s.TelegramAPIBase, "/")
	}
	return defaultTelegramAPI
}
func (s HTTPSender) discordBase() string {
	if strings.TrimSpace(s.DiscordAPIBase) != "" {
		return strings.TrimRight(s.DiscordAPIBase, "/")
	}
	return defaultDiscordAPI
}

func (s HTTPSender) Send(ctx context.Context, d Delivery) (Receipt, error) {
	if !validOrigin(d.Origin) {
		return Receipt{}, ErrInvalid
	}
	var endpoint, auth string
	var payload map[string]any
	switch d.Origin.Transport {
	case "telegram":
		if s.TelegramToken == "" || s.TelegramBotID == "" || s.TelegramBotID != d.Origin.TenantID {
			return Receipt{}, ErrUnavailable
		}
		if !validText(d.Text, 16000) || utf8.RuneCountInString(d.Text) > 4096 {
			return Receipt{}, ErrInvalid
		}
		endpoint = s.telegramBase() + "/bot" + url.PathEscape(s.TelegramToken) + "/sendMessage"
		payload = map[string]any{"chat_id": d.Origin.ChannelID, "text": d.Text, "protect_content": true, "link_preview_options": map[string]any{"is_disabled": true}}
		if d.Origin.ThreadID != "" {
			thread, err := strconv.ParseInt(d.Origin.ThreadID, 10, 64)
			if err != nil {
				return Receipt{}, ErrInvalid
			}
			payload["message_thread_id"] = thread
		}
		if kb, ok := d.ReplyMarkup["inline_keyboard"]; ok {
			payload["reply_markup"] = map[string]any{"inline_keyboard": kb}
		}
	case "discord":
		if s.DiscordToken == "" || s.DiscordApplicationID == "" || !strings.HasPrefix(d.Origin.TenantID, s.DiscordApplicationID+"/") {
			return Receipt{}, ErrUnavailable
		}
		if !validText(d.Text, 8000) || utf8.RuneCountInString(d.Text) > 2000 {
			return Receipt{}, ErrInvalid
		}
		endpoint = s.discordBase() + "/channels/" + url.PathEscape(d.Origin.ChannelID) + "/messages"
		auth = "Bot " + s.DiscordToken
		payload = map[string]any{"content": d.Text, "allowed_mentions": map[string]any{"parse": []string{}, "replied_user": false}, "nonce": strings.TrimPrefix(d.ID, "pm_")[:min(24, len(strings.TrimPrefix(d.ID, "pm_")))], "enforce_nonce": true}
		if comps, ok := d.ReplyMarkup["components"]; ok {
			payload["components"] = comps
		}
	default:
		return Receipt{}, ErrInvalid
	}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return Receipt{}, ErrInvalid
	}
	req.Header.Set("Content-Type", "application/json")
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	client := http.Client{Timeout: 15 * time.Second}
	if s.Client != nil {
		client = *s.Client
	}
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	if client.Timeout == 0 || client.Timeout > 15*time.Second {
		client.Timeout = 15 * time.Second
	}
	res, err := client.Do(req)
	if err != nil {
		return Receipt{}, fmt.Errorf("channel request failed; remote outcome unknown")
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 64*1024+1))
	if err != nil || len(raw) > 64*1024 {
		return Receipt{}, fmt.Errorf("channel response unreadable; remote outcome unknown")
	}
	if res.StatusCode == 429 || res.StatusCode == 409 {
		if id, ok := parseDeliveredID(d.Origin.Transport, d.Origin.ChannelID, raw); ok && res.StatusCode == 409 {
			return Receipt{Status: Delivered, ExternalID: id}, nil
		}
		after, hasAfter := parseRetryAfter(res, raw)
		return Receipt{}, retryLaterError{after: after, hasAfter: hasAfter, status: res.StatusCode}
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return Receipt{}, fmt.Errorf("channel returned HTTP %d; reconcile before retry", res.StatusCode)
	}
	id, ok := parseDeliveredID(d.Origin.Transport, d.Origin.ChannelID, raw)
	if !ok {
		return Receipt{}, ErrInvalid
	}
	return Receipt{Status: Delivered, ExternalID: id}, nil
}

func jsonScalarString(raw json.RawMessage) string {
	s := strings.TrimSpace(string(raw))
	return strings.Trim(s, `"`)
}

func parseDeliveredID(transport, channelID string, raw []byte) (string, bool) {
	if transport == "telegram" {
		var result struct {
			OK     bool `json:"ok"`
			Result struct {
				MessageID json.RawMessage `json:"message_id"`
				Chat      struct {
					ID json.RawMessage `json:"id"`
				} `json:"chat"`
			} `json:"result"`
		}
		if json.Unmarshal(raw, &result) != nil || !result.OK {
			return "", false
		}
		id := jsonScalarString(result.Result.MessageID)
		if id == "" || id == "0" || jsonScalarString(result.Result.Chat.ID) != channelID {
			return "", false
		}
		return id, true
	}
	var result struct {
		ID        string `json:"id"`
		ChannelID string `json:"channel_id"`
	}
	if json.Unmarshal(raw, &result) != nil || result.ID == "" || result.ChannelID != channelID {
		return "", false
	}
	return result.ID, true
}

func parseRetryAfter(res *http.Response, raw []byte) (time.Duration, bool) {
	if v := strings.TrimSpace(res.Header.Get("Retry-After")); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return time.Duration(n) * time.Second, true
		}
	}
	var tg struct {
		Parameters struct {
			RetryAfter *int `json:"retry_after"`
		} `json:"parameters"`
	}
	if json.Unmarshal(raw, &tg) == nil && tg.Parameters.RetryAfter != nil {
		return time.Duration(*tg.Parameters.RetryAfter) * time.Second, true
	}
	var dc struct {
		RetryAfter *float64 `json:"retry_after"`
	}
	if json.Unmarshal(raw, &dc) == nil && dc.RetryAfter != nil {
		return time.Duration(*dc.RetryAfter * float64(time.Second)), true
	}
	return 0, false
}
