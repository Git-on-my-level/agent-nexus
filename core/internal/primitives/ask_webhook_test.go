package primitives

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"testing"
)

type askRoundTrip func(*http.Request) (*http.Response, error)

func (f askRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestAskWebhookSignatureRetryDeadLetter(t *testing.T) {
	t.Parallel()
	s, ws, _, ask := askDeliveryFixture(t)
	ctx := context.Background()
	sub, err := s.CreateAskSubscription(ctx, "requester", anyStringValue(ask["ref"]), AskSubscriptionInput{Kind: "webhook", Label: "service", URL: "https://example.com/hook"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = answerDeliveryFixture(s, ask, "needs_context"); err != nil {
		t.Fatal(err)
	}
	attempts := 0
	key := ""
	client := &http.Client{Transport: askRoundTrip(func(r *http.Request) (*http.Response, error) {
		attempts++
		body, _ := io.ReadAll(r.Body)
		mac := hmac.New(sha256.New, []byte(anyStringValue(sub["secret"])))
		mac.Write([]byte(r.Header.Get("X-ANX-Timestamp") + "."))
		mac.Write(body)
		if got := r.Header.Get("X-ANX-Signature"); got != "sha256="+hex.EncodeToString(mac.Sum(nil)) {
			t.Fatalf("signature %s", got)
		}
		if !strings.Contains(string(body), "needs_context") || strings.Contains(string(body), anyStringValue(sub["secret"])) {
			t.Fatalf("payload %s", body)
		}
		if key != "" && key != r.Header.Get("Idempotency-Key") {
			t.Fatal("retry key changed")
		}
		key = r.Header.Get("Idempotency-Key")
		return &http.Response{StatusCode: 503, Body: io.NopCloser(strings.NewReader("sensitive response body")), Header: http.Header{}}, nil
	})}
	for i := 0; i < 7; i++ {
		if _, err = ws.DB().Exec(`UPDATE ask_deliveries SET next_at='2000-01-01T00:00:00Z'`); err != nil {
			t.Fatal(err)
		}
		if err = s.deliverAskWebhooks(ctx, func(context.Context, string) bool { return true }, client); err != nil {
			t.Fatal(err)
		}
	}
	if attempts != 5 {
		t.Fatalf("attempts=%d", attempts)
	}
	out, err := s.AskOutcome(ctx, anyStringValue(ask["ref"]))
	if err != nil {
		t.Fatal(err)
	}
	delivery := out["delivery"].([]map[string]any)[0]
	if delivery["state"] != "failed" || len(delivery["log"].([]map[string]any)) != 5 || strings.Contains(workJSON(delivery), "sensitive response") {
		t.Fatalf("deadletter %#v", delivery)
	}
}
func TestAskWebhookDialPinsDNSAndRejectsRebinding(t *testing.T) {
	t.Parallel()
	resolved, dialed := 0, 0
	client := webhookHTTPClientWithDial(func(context.Context, string, string) ([]netip.Addr, error) {
		resolved++
		if resolved == 1 {
			return []netip.Addr{netip.MustParseAddr("93.184.216.34")}, nil
		}
		return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
	}, func(_ context.Context, _, address string) (net.Conn, error) {
		dialed++
		if address != "93.184.216.34:443" {
			t.Fatalf("not pinned %s", address)
		}
		return nil, fmt.Errorf("test dial")
	})
	for i := 0; i < 2; i++ {
		_, _ = client.Get("https://example.com/answer")
	}
	if resolved != 2 || dialed != 1 {
		t.Fatalf("resolve=%d dial=%d", resolved, dialed)
	}
	if client.CheckRedirect(&http.Request{}, nil) != http.ErrUseLastResponse {
		t.Fatal("redirect followed")
	}
	if client.Transport.(*http.Transport).Proxy != nil {
		t.Fatal("proxy bypass")
	}
}
