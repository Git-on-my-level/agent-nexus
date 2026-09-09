package channels

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Discord is a REST + interaction fake. It never contacts discord.com.
type Discord struct {
	Token         string
	ApplicationID string
	PrivateKey    ed25519.PrivateKey
	PublicKey     ed25519.PublicKey

	mu     sync.Mutex
	sends  []Outbound
	nextID int64
	fail   []int
	server *httptest.Server
}

func NewDiscord(token, applicationID string) *Discord {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		panic(err)
	}
	d := &Discord{Token: token, ApplicationID: applicationID, PrivateKey: priv, PublicKey: pub, nextID: 2000}
	d.server = httptest.NewServer(http.HandlerFunc(d.serve))
	return d
}

func (d *Discord) URL() string { return d.server.URL }

func (d *Discord) Close() { d.server.Close() }

func (d *Discord) PublicKeyHex() string { return hex.EncodeToString(d.PublicKey) }

func (d *Discord) FailNext(status ...int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.fail = append(d.fail, status...)
}

func (d *Discord) Sends() []Outbound {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]Outbound, len(d.sends))
	copy(out, d.sends)
	return out
}

func (d *Discord) serve(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	want := "Bot " + d.Token
	if subtleAuth(r.Header.Get("Authorization"), want) != 1 {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if !strings.HasPrefix(r.URL.Path, "/api/v10/channels/") || !strings.HasSuffix(r.URL.Path, "/messages") {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, 64*1024))
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	var body map[string]any
	if json.Unmarshal(raw, &body) != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	channelID := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/v10/channels/"), "/messages")
	d.mu.Lock()
	defer d.mu.Unlock()
	status := 200
	if len(d.fail) > 0 {
		status = d.fail[0]
		d.fail = d.fail[1:]
	}
	id := atomic.AddInt64(&d.nextID, 1)
	msgID := strconv.FormatInt(id, 10)
	d.sends = append(d.sends, Outbound{Method: "POST", Path: r.URL.Path, Auth: "Bot", Status: status, Body: body, MessageID: msgID})
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	switch status {
	case 429:
		_, _ = w.Write([]byte(`{"retry_after":0,"global":false,"message":"You are being rate limited."}`))
	case 409:
		_, _ = w.Write([]byte(`{"message":"Unknown Message","code":10008,"retry_after":0}`))
	default:
		_ = json.NewEncoder(w).Encode(map[string]any{"id": msgID, "channel_id": channelID})
	}
}

func subtleAuth(got, want string) int {
	if len(got) != len(want) {
		return 0
	}
	return subtleConst(got, want)
}

func subtleConst(a, b string) int {
	var v uint8
	for i := 0; i < len(a); i++ {
		v |= a[i] ^ b[i]
	}
	if v == 0 {
		return 1
	}
	return 0
}

// Sign returns Discord interaction headers for a body posted to core ingress.
func (d *Discord) Sign(body []byte, at time.Time) (timestamp, signature string) {
	stamp := strconv.FormatInt(at.Unix(), 10)
	sig := ed25519.Sign(d.PrivateKey, append([]byte(stamp), body...))
	return stamp, hex.EncodeToString(sig)
}

func DiscordPing() []byte {
	b, _ := json.Marshal(map[string]any{"type": 1})
	return b
}

func DiscordCommand(id, applicationID, guildID, channelID, userID, name string, options map[string]any) []byte {
	opts := make([]map[string]any, 0, len(options))
	for k, v := range options {
		opts = append(opts, map[string]any{"name": k, "value": v})
	}
	in := map[string]any{
		"type": 2, "id": id, "application_id": applicationID, "channel_id": channelID,
		"member": map[string]any{"user": map[string]any{"id": userID, "bot": false}},
		"data":   map[string]any{"name": name, "options": opts},
	}
	if guildID != "" {
		in["guild_id"] = guildID
	}
	b, _ := json.Marshal(in)
	return b
}

func DiscordComponent(id, applicationID, guildID, channelID, userID, customID string) []byte {
	in := map[string]any{
		"type": 3, "id": id, "application_id": applicationID, "channel_id": channelID,
		"member": map[string]any{"user": map[string]any{"id": userID, "bot": false}},
		"data":   map[string]any{"custom_id": customID, "component_type": 2},
	}
	if guildID != "" {
		in["guild_id"] = guildID
	}
	b, _ := json.Marshal(in)
	return b
}
