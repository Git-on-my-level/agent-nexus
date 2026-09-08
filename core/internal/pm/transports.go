package pm

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
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
		UpdateID *int64 `json:"update_id"`
		Message  *struct {
			MessageID int64  `json:"message_id"`
			Text      string `json:"text"`
			ThreadID  int64  `json:"message_thread_id"`
			From      *struct {
				ID    int64 `json:"id"`
				IsBot bool  `json:"is_bot"`
			} `json:"from"`
			Chat struct {
				ID int64 `json:"id"`
			} `json:"chat"`
		} `json:"message"`
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 64*1024))
	if err != nil || json.Unmarshal(body, &update) != nil {
		writeError(w, ErrInvalid)
		return
	}
	if update.UpdateID == nil || update.Message == nil || update.Message.From == nil || update.Message.From.IsBot || update.Message.From.ID == 0 || update.Message.Chat.ID == 0 || update.Message.MessageID == 0 {
		writeError(w, ErrInvalid)
		return
	}
	m := update.Message
	o := Origin{Transport: "telegram", TenantID: h.TelegramBotID, ChannelID: strconv.FormatInt(m.Chat.ID, 10), ExternalUserID: strconv.FormatInt(m.From.ID, 10)}
	if m.ThreadID != 0 {
		o.ThreadID = strconv.FormatInt(m.ThreadID, 10)
	}
	text := m.Text
	// Approval has explicit syntax and binds the exact displayed revision.
	// Natural language (including "yes") remains a discussion turn.
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
		_, err = h.Service.AnswerFromChannel(r.Context(), o, fields[1], AnswerInput{Revision: revision, Approve: fields[0] == "/pm-approve", Text: strings.Join(fields[3:], " ")})
		if err != nil {
			writeError(w, err)
			return
		}
	} else {
		if strings.HasPrefix(text, "/pm ") {
			text = strings.TrimPrefix(text, "/pm ")
		}
		_, err = h.Service.ReceiveChannel(r.Context(), o, strconv.FormatInt(*update.UpdateID, 10), text)
		if err != nil {
			writeError(w, err)
			return
		}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
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
			Name    string `json:"name"`
			Options []struct {
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
	if in.Type != 2 || in.ID == "" {
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
		_, err = h.Service.ReceiveChannel(r.Context(), o, in.ID, text)
	case "pm-approve", "pm-reject":
		var id string
		var revision int
		if json.Unmarshal(values["decision"], &id) != nil || json.Unmarshal(values["revision"], &revision) != nil {
			writeError(w, ErrInvalid)
			return
		}
		_, err = h.Service.AnswerFromChannel(r.Context(), o, id, AnswerInput{Revision: revision, Approve: in.Data.Name == "pm-approve", Text: text})
	default:
		err = ErrInvalid
	}
	if err != nil {
		writeError(w, err)
		return
	}
	// An ephemeral transport ACK is not the PM answer. The actual response is
	// sent by the durable outbox to the bound channel using bot REST credentials.
	_ = json.NewEncoder(w).Encode(map[string]any{"type": 4, "data": map[string]any{"content": "Received by PM. Follow-through is tracked in your conversation.", "flags": 64, "allowed_mentions": map[string]any{"parse": []string{}}}})
}

// HTTPSender talks to official APIs. Client injection is for transport tests;
// redirects are always disabled to avoid credential forwarding. No retries.
type HTTPSender struct {
	TelegramToken        string
	TelegramBotID        string
	DiscordToken         string
	DiscordApplicationID string
	Client               *http.Client
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
		endpoint = "https://api.telegram.org/bot" + url.PathEscape(s.TelegramToken) + "/sendMessage"
		payload = map[string]any{"chat_id": d.Origin.ChannelID, "text": d.Text, "protect_content": true, "link_preview_options": map[string]any{"is_disabled": true}}
		if d.Origin.ThreadID != "" {
			thread, err := strconv.ParseInt(d.Origin.ThreadID, 10, 64)
			if err != nil {
				return Receipt{}, ErrInvalid
			}
			payload["message_thread_id"] = thread
		}
	case "discord":
		if s.DiscordToken == "" || s.DiscordApplicationID == "" || !strings.HasPrefix(d.Origin.TenantID, s.DiscordApplicationID+"/") {
			return Receipt{}, ErrUnavailable
		}
		if !validText(d.Text, 8000) || utf8.RuneCountInString(d.Text) > 2000 {
			return Receipt{}, ErrInvalid
		}
		endpoint = "https://discord.com/api/v10/channels/" + url.PathEscape(d.Origin.ChannelID) + "/messages"
		auth = "Bot " + s.DiscordToken
		// Discord's short nonce is stable per durable intent. This helps recent
		// deduplication, but does not justify retrying an unknown result later.
		payload = map[string]any{"content": d.Text, "allowed_mentions": map[string]any{"parse": []string{}, "replied_user": false}, "nonce": strings.TrimPrefix(d.ID, "pm_")[:min(24, len(strings.TrimPrefix(d.ID, "pm_")))], "enforce_nonce": true}
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
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return Receipt{}, fmt.Errorf("channel returned HTTP %d; reconcile before retry", res.StatusCode)
	}
	var id string
	if d.Origin.Transport == "telegram" {
		var result struct {
			OK     bool `json:"ok"`
			Result struct {
				MessageID int64 `json:"message_id"`
				Chat      struct {
					ID int64 `json:"id"`
				} `json:"chat"`
			} `json:"result"`
		}
		if json.Unmarshal(raw, &result) != nil || !result.OK || result.Result.MessageID == 0 || strconv.FormatInt(result.Result.Chat.ID, 10) != d.Origin.ChannelID {
			return Receipt{}, ErrInvalid
		}
		id = strconv.FormatInt(result.Result.MessageID, 10)
	} else {
		var result struct {
			ID        string `json:"id"`
			ChannelID string `json:"channel_id"`
		}
		if json.Unmarshal(raw, &result) != nil || result.ID == "" || result.ChannelID != d.Origin.ChannelID {
			return Receipt{}, ErrInvalid
		}
		id = result.ID
	}
	return Receipt{Status: Delivered, ExternalID: id}, nil
}
