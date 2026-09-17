// Package channels is a local fake of the Telegram Bot API and Discord REST API.
// It speaks the real wire shapes so core/CLI tests can prove ingress, delivery,
// 409/429 handling and retry without touching live bots or webhooks.
package channels

import (
	"crypto/subtle"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
)

// Telegram is a Bot API fake. It never contacts api.telegram.org.
type Telegram struct {
	Token  string
	Secret string

	mu     sync.Mutex
	sends  []Outbound
	nextID int64
	fail   []int
	server *httptest.Server
}

// Outbound is one recorded Bot API / Discord REST send.
type Outbound struct {
	Method    string
	Path      string
	Auth      string
	Status    int
	Body      map[string]any
	MessageID string
}

func NewTelegram(token, secret string) *Telegram {
	t := &Telegram{Token: token, Secret: secret, nextID: 100}
	t.server = httptest.NewServer(http.HandlerFunc(t.serve))
	return t
}

func (t *Telegram) URL() string { return t.server.URL }

func (t *Telegram) Close() { t.server.Close() }

// FailNext queues HTTP statuses (409, 429, …) for subsequent sendMessage calls.
func (t *Telegram) FailNext(status ...int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.fail = append(t.fail, status...)
}

func (t *Telegram) Sends() []Outbound {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]Outbound, len(t.sends))
	copy(out, t.sends)
	return out
}

func (t *Telegram) serve(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	prefix := "/bot" + t.Token + "/"
	if !strings.HasPrefix(r.URL.Path, prefix) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	method := strings.TrimPrefix(r.URL.Path, prefix)
	raw, err := io.ReadAll(io.LimitReader(r.Body, 64*1024))
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	var body map[string]any
	if len(raw) > 0 && json.Unmarshal(raw, &body) != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	status := 200
	if len(t.fail) > 0 && (method == "sendMessage" || method == "answerCallbackQuery") {
		status = t.fail[0]
		t.fail = t.fail[1:]
	}
	id := atomic.AddInt64(&t.nextID, 1)
	msgID := strconv.FormatInt(id, 10)
	rec := Outbound{Method: method, Path: r.URL.Path, Status: status, Body: body, MessageID: msgID}
	t.sends = append(t.sends, rec)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	switch status {
	case 429:
		_, _ = w.Write([]byte(`{"ok":false,"error_code":429,"description":"Too Many Requests: retry after 1","parameters":{"retry_after":0}}`))
	case 409:
		_, _ = w.Write([]byte(`{"ok":false,"error_code":409,"description":"Conflict: message is not modified","parameters":{"retry_after":0}}`))
	default:
		if method == "sendMessage" {
			chat := telegramChatID(body["chat_id"])
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok": true,
				"result": map[string]any{
					"message_id": id,
					"chat":       map[string]any{"id": chat},
				},
			})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": true})
	}
}

func telegramChatID(v any) any {
	switch x := v.(type) {
	case string:
		if n, err := strconv.ParseInt(x, 10, 64); err == nil {
			return n
		}
		return x
	case float64:
		return int64(x)
	case json.Number:
		if n, err := x.Int64(); err == nil {
			return n
		}
		return string(x)
	default:
		return v
	}
}

// SecretOK reports whether a webhook secret matches the fake's configured token.
func (t *Telegram) SecretOK(got string) bool {
	return subtle.ConstantTimeCompare([]byte(got), []byte(t.Secret)) == 1
}

// Update is a Telegram webhook body for a normal text message.
func TelegramMessage(updateID, chatID, userID, messageID int64, text string, threadID int64) []byte {
	msg := map[string]any{
		"message_id": messageID,
		"chat":       map[string]any{"id": chatID},
		"from":       map[string]any{"id": userID, "is_bot": false},
		"text":       text,
	}
	if threadID != 0 {
		msg["message_thread_id"] = threadID
	}
	b, _ := json.Marshal(map[string]any{"update_id": updateID, "message": msg})
	return b
}

// Callback is a Telegram callback_query update (inline keyboard).
func TelegramCallback(updateID, chatID, userID int64, data string, threadID int64) []byte {
	msg := map[string]any{"message_id": 1, "chat": map[string]any{"id": chatID}}
	if threadID != 0 {
		msg["message_thread_id"] = threadID
	}
	b, _ := json.Marshal(map[string]any{
		"update_id": updateID,
		"callback_query": map[string]any{
			"id":      strconv.FormatInt(updateID, 10),
			"from":    map[string]any{"id": userID, "is_bot": false},
			"message": msg,
			"data":    data,
		},
	})
	return b
}
