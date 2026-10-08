package primitives

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// An optional allowlist narrows permitted DNS names. It never disables address
// validation. Resolve inside DialContext and dial that exact validated IP, so a
// second lookup cannot rebind a public validation result to a private address.
func ValidateAskWebhookURL(raw string, allow []string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Fragment != "" || len(raw) > 2048 {
		return workInvalid("webhook requires an HTTPS URL without credentials or fragment")
	}
	if u.Port() != "" && u.Port() != "443" {
		return workInvalid("webhook port must be 443")
	}
	host := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
	if len(allow) > 0 {
		found := false
		for _, h := range allow {
			if strings.EqualFold(strings.TrimSpace(h), host) {
				found = true
			}
		}
		if !found {
			return workInvalid("webhook host is not allowlisted")
		}
	}
	if ip, err := netip.ParseAddr(host); err == nil && !publicWebhookIP(ip) {
		return workInvalid("webhook address is not public")
	}
	if host == "localhost" || strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".local") {
		return workInvalid("webhook host is not public")
	}
	return nil
}
func publicWebhookIP(ip netip.Addr) bool {
	ip = ip.Unmap()
	if ip.Is6() && !netip.MustParsePrefix("2000::/3").Contains(ip) {
		return false
	}
	if !ip.IsValid() || !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return false
	}
	for _, cidr := range []string{"0.0.0.0/8", "100.64.0.0/10", "192.0.0.0/24", "192.0.2.0/24", "192.88.99.0/24", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "240.0.0.0/4", "2001:db8::/32", "2001::/23", "64:ff9b::/96", "64:ff9b:1::/48", "2002::/16", "3fff::/20", "100::/64"} {
		if netip.MustParsePrefix(cidr).Contains(ip) {
			return false
		}
	}
	return true
}
func webhookHTTPClient() *http.Client {
	return webhookHTTPClientWithDial(net.DefaultResolver.LookupNetIP, (&net.Dialer{Timeout: 3 * time.Second}).DialContext)
}
func webhookHTTPClientWithDial(resolve func(context.Context, string, string) ([]netip.Addr, error), dial func(context.Context, string, string) (net.Conn, error)) *http.Client {
	transport := &http.Transport{Proxy: nil, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12}, TLSHandshakeTimeout: 3 * time.Second, ResponseHeaderTimeout: 5 * time.Second, MaxResponseHeaderBytes: 16 << 10, DisableKeepAlives: true}
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		ips, err := resolve(ctx, "ip", host)
		if err != nil {
			return nil, fmt.Errorf("DNS resolution failed")
		}
		if len(ips) == 0 {
			return nil, fmt.Errorf("DNS returned no addresses")
		}
		for _, ip := range ips {
			if !publicWebhookIP(ip) {
				return nil, fmt.Errorf("DNS address is not public")
			}
		}
		// DNS is evaluated anew for each retry and never again between validation
		// and connect. TLS still verifies the original hostname through Transport.
		return dial(ctx, network, net.JoinHostPort(ips[0].String(), port))
	}
	return &http.Client{Transport: transport, Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}
func WithAskWebhookAllowHosts(hosts []string) Option {
	return func(s *Store) { s.askWebhookAllowHosts = append([]string(nil), hosts...) }
}

type askWebhookJob struct {
	ID, Subscription, Ask, Actor, URL string
	Secret, Nonce                     []byte
	Attempts                          int
}

// DeliverAskWebhooks handles a fixed-size due window. It never scans the event
// ledger or recomputes Inbox. Lease timestamps also recover interrupted sends.
func (s *Store) DeliverAskWebhooks(ctx context.Context, active func(context.Context, string) bool) error {
	client := webhookHTTPClient()
	defer client.CloseIdleConnections()
	return s.deliverAskWebhooks(ctx, active, client)
}
func (s *Store) deliverAskWebhooks(ctx context.Context, active func(context.Context, string) bool, client *http.Client) error {
	if s.askWebhookEncryption == nil {
		return nil
	}
	now := time.Now().UTC()
	rows, err := s.db.QueryContext(ctx, `SELECT d.id,d.subscription_id,d.ask_id,s.actor_id,s.endpoint,s.secret,s.nonce,d.attempts FROM ask_deliveries d JOIN ask_subscriptions s ON s.id=d.subscription_id WHERE d.kind='webhook' AND d.state='pending' AND d.next_at<=? AND s.enabled=1 ORDER BY d.next_at,d.id LIMIT 20`, now.Format(time.RFC3339Nano))
	if err != nil {
		return err
	}
	jobs := []askWebhookJob{}
	for rows.Next() {
		var j askWebhookJob
		if err = rows.Scan(&j.ID, &j.Subscription, &j.Ask, &j.Actor, &j.URL, &j.Secret, &j.Nonce, &j.Attempts); err != nil {
			rows.Close()
			return err
		}
		jobs = append(jobs, j)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, j := range jobs {
		if j.Attempts >= 5 {
			if _, err = s.db.ExecContext(ctx, `UPDATE ask_deliveries SET state='failed',reason='dead_letter: retry_limit' WHERE id=? AND state='pending' AND attempts>=5`, j.ID); err != nil {
				return err
			}
			continue
		}
		result, e := s.db.ExecContext(ctx, `UPDATE ask_deliveries SET attempts=attempts+1,next_at=? WHERE id=? AND state='pending' AND next_at<=? AND attempts=?`, time.Now().Add(time.Minute).UTC().Format(time.RFC3339Nano), j.ID, now.Format(time.RFC3339Nano), j.Attempts)
		if e != nil {
			return e
		}
		n, _ := result.RowsAffected()
		if n == 0 {
			continue
		}
		j.Attempts++
		state, reason, code, latency := s.sendAskWebhook(ctx, j, active, client)
		if state == "pending" && j.Attempts >= 5 {
			state = "failed"
			reason = "dead_letter: " + reason
		}
		if _, e = s.db.ExecContext(ctx, `INSERT OR REPLACE INTO ask_delivery_attempts(delivery_id,attempt,status_code,latency_ms,at) VALUES(?,?,?,?,?)`, j.ID, j.Attempts, code, latency, time.Now().UTC().Format(time.RFC3339Nano)); e != nil {
			return e
		}
		next := time.Now().Add(time.Duration(1<<uint(j.Attempts)) * time.Second).UTC().Format(time.RFC3339Nano)
		_, e = s.db.ExecContext(ctx, `UPDATE ask_deliveries SET state=?,reason=?,status_code=?,latency_ms=?,last_at=?,next_at=? WHERE id=? AND attempts=?`, state, reason, code, latency, time.Now().UTC().Format(time.RFC3339Nano), next, j.ID, j.Attempts)
		if e != nil {
			return e
		}
	}
	return nil
}
func (s *Store) sendAskWebhook(ctx context.Context, j askWebhookJob, active func(context.Context, string) bool, client *http.Client) (string, string, int, int64) {
	if active == nil || !active(ctx, j.Actor) {
		return "failed", "recipient_inactive", 0, 0
	}
	scoped := WithReadTickSnapshot(WithAccessScope(ctx, AccessScope{ActorID: j.Actor}))
	out, err := s.AskOutcome(scoped, j.Ask)
	if err != nil {
		return "failed", "response_not_accessible", 0, 0
	}
	response := asMapValue(out["response"])
	payload := map[string]any{"ask_id": out["ask_id"], "card_ref": out["subject_ref"], "status": out["status"], "outcome": response["outcome"], "response_event_id": response["response_event_id"], "response_text": response["response_text"]}
	body, err := json.Marshal(payload)
	if err != nil || len(body) > 64<<10 {
		return "failed", "payload_too_large", 0, 0
	}
	if err = ValidateAskWebhookURL(j.URL, s.askWebhookAllowHosts); err != nil {
		return "failed", "endpoint_blocked", 0, 0
	}
	key, err := s.askWebhookEncryption.Decrypt(j.Secret, j.Nonce)
	if err != nil {
		return "failed", "secret_unavailable", 0, 0
	}
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(timestamp + "."))
	mac.Write(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, j.URL, bytes.NewReader(body))
	if err != nil {
		return "failed", "invalid_endpoint", 0, 0
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-ANX-Timestamp", timestamp)
	req.Header.Set("X-ANX-Signature", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	req.Header.Set("X-ANX-Replay-Window", "300")
	req.Header.Set("Idempotency-Key", j.ID)
	started := time.Now()
	resp, err := client.Do(req)
	latency := time.Since(started).Milliseconds()
	if err != nil {
		return "pending", "transport_failed", 0, latency
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return "delivered", "", resp.StatusCode, latency
	}
	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		return "failed", "redirect_blocked", resp.StatusCode, latency
	}
	return "pending", "http_failure", resp.StatusCode, latency
}
func (s *Store) RunAskWebhooks(ctx context.Context, active func(context.Context, string) bool) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = s.DeliverAskWebhooks(ctx, active)
		}
	}
}
