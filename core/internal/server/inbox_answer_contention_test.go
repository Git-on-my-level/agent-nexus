package server

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/url"
	"testing"
	"time"
)

// A held writer lease forces the answer transaction to queue. WAL readers must
// remain able to poll, and the durable replay save must commit once released.
func TestInboxAnswerWaitsForWriterLease(t *testing.T) {
	requireIntegrationTest(t)
	h, itemID := responseSecurityFixture(t)
	held, err := h.workspace.DB().BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Rollback()
	type result struct {
		status int
		body   []byte
		err    error
	}
	done := make(chan result, 1)
	start := time.Now()
	go func() {
		req, err := http.NewRequest("POST", h.baseURL+"/inbox/"+url.PathEscape(itemID)+"/respond", bytes.NewBufferString(`{"idempotency_key":"held-writer","response_text":"Approved after waiting","outcome":"answered","notify_mode":"none"}`))
		if err != nil {
			done <- result{err: err}
			return
		}
		req.Header.Set("Authorization", "Bearer "+h.humanAccessToken)
		req.Header.Set("Content-Type", "application/json")
		resp, err := (&http.Client{Timeout: 5 * time.Second}).Do(req)
		if err != nil {
			done <- result{err: err}
			return
		}
		defer resp.Body.Close()
		data, err := io.ReadAll(resp.Body)
		done <- result{resp.StatusCode, data, err}
	}()
	var early *result
	select {
	case r := <-done:
		early = &r
	case <-time.After(150 * time.Millisecond):
	}
	if err = held.Rollback(); err != nil {
		t.Fatal(err)
	}
	var r result
	if early != nil {
		r = *early
	} else {
		r = <-done
	}
	if early != nil || r.err != nil || r.status != http.StatusCreated {
		t.Fatalf("answer failed to wait: early=%t status=%d err=%v body=%s", early != nil, r.status, r.err, r.body)
	}
	var count int
	if err = h.workspace.DB().QueryRow(`SELECT count(*) FROM human_attention_response_claims WHERE response_json<>'{}'`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("durable response count=%d err=%v", count, err)
	}
	t.Logf("answer waited for writer and saved replay: %s", time.Since(start))
}
