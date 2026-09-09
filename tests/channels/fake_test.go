package channels

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestTelegramRecordsSendAndInjects429(t *testing.T) {
	tg := NewTelegram("tok", strings.Repeat("s", 32))
	defer tg.Close()
	tg.FailNext(429, 200)
	body := `{"chat_id":"-123","text":"hello"}`
	for i := 0; i < 2; i++ {
		req, _ := http.NewRequest(http.MethodPost, tg.URL()+"/bottok/sendMessage", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if i == 0 && resp.StatusCode != 429 {
			t.Fatalf("want 429 got %d", resp.StatusCode)
		}
		if i == 1 && resp.StatusCode != 200 {
			t.Fatalf("want 200 got %d", resp.StatusCode)
		}
	}
	if n := len(tg.Sends()); n != 2 {
		t.Fatalf("sends %d", n)
	}
	if !tg.SecretOK(strings.Repeat("s", 32)) || tg.SecretOK("wrong") {
		t.Fatal("secret check")
	}
}

func TestTelegramRejectsWrongToken(t *testing.T) {
	tg := NewTelegram("tok", "secret")
	defer tg.Close()
	resp, err := http.Post(tg.URL()+"/botOTHER/sendMessage", "application/json", strings.NewReader(`{"chat_id":"1","text":"x"}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("%d", resp.StatusCode)
	}
}

func TestDiscordRecordsSendAndInjects409(t *testing.T) {
	d := NewDiscord("bot-token", "app")
	defer d.Close()
	d.FailNext(409)
	req, _ := http.NewRequest(http.MethodPost, d.URL()+"/api/v10/channels/channel/messages", strings.NewReader(`{"content":"hi","nonce":"abc","enforce_nonce":true}`))
	req.Header.Set("Authorization", "Bot bot-token")
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 409 {
		t.Fatalf("%d", resp.StatusCode)
	}
	var body map[string]any
	if json.NewDecoder(resp.Body).Decode(&body) != nil && resp.StatusCode == 200 {
		t.Fatal("decode")
	}
	if len(d.Sends()) != 1 {
		t.Fatal(d.Sends())
	}
	stamp, sig := d.Sign(DiscordPing(), time.Now())
	if stamp == "" || sig == "" || len(d.PublicKeyHex()) != 64 {
		t.Fatal("sign")
	}
}

func TestDiscordRejectsWrongBotToken(t *testing.T) {
	d := NewDiscord("bot-token", "app")
	defer d.Close()
	req, _ := http.NewRequest(http.MethodPost, d.URL()+"/api/v10/channels/c/messages", strings.NewReader(`{"content":"x"}`))
	req.Header.Set("Authorization", "Bot other")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("%d", resp.StatusCode)
	}
}
