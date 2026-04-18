package proxy

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"agent-nexus-tools-anx-http-record/internal/recorder"
)

func TestRecordsSeqInResponseCompletionOrder(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/slow":
			time.Sleep(80 * time.Millisecond)
			_, _ = w.Write([]byte(`{"path":"slow"}`))
		case "/fast":
			time.Sleep(10 * time.Millisecond)
			_, _ = w.Write([]byte(`{"path":"fast"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()

	handler := newTestHandler(t, &output, upstream.URL, false, 1<<20)
	server := httptest.NewServer(handler)
	defer server.Close()

	var wg sync.WaitGroup
	for _, path := range []string{"/slow", "/fast"} {
		wg.Add(1)
		go func(path string) {
			defer wg.Done()
			resp, err := http.Get(server.URL + path)
			if err != nil {
				t.Errorf("get %s: %v", path, err)
				return
			}
			defer resp.Body.Close()
			_, _ = io.Copy(io.Discard, resp.Body)
		}(path)
	}
	wg.Wait()

	entries := readEntries(t, output.Bytes())
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	if entries[0].Seq != 1 || entries[0].Path != "/fast" {
		t.Fatalf("expected first entry to be seq=1 path=/fast, got seq=%d path=%s", entries[0].Seq, entries[0].Path)
	}
	if entries[1].Seq != 2 || entries[1].Path != "/slow" {
		t.Fatalf("expected second entry to be seq=2 path=/slow, got seq=%d path=%s", entries[1].Seq, entries[1].Path)
	}
}

func TestRedactsSensitiveHeadersAndJSONBodyKeys(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := strings.TrimSpace(r.Header.Get("Authorization")); got == "" {
			t.Fatalf("expected upstream auth header")
		}
		w.Header().Set("Set-Cookie", "session=upstream")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"secret","status":"ok"}`))
	}))
	defer upstream.Close()

	handler := newTestHandler(t, &output, upstream.URL, false, 1<<20)
	server := httptest.NewServer(handler)
	defer server.Close()

	req, err := http.NewRequest(http.MethodPost, server.URL+"/auth/agents/register", strings.NewReader(`{"bootstrap_token":"top-secret","name":"casey"}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer super-secret")
	req.Header.Set("Cookie", "session=local")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)

	entries := readEntries(t, output.Bytes())
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	entry := entries[0]
	if got := headerValue(entry.RequestHeaders, "Authorization"); got != "REDACTED" {
		t.Fatalf("expected Authorization redacted, got %q", got)
	}
	if got := headerValue(entry.RequestHeaders, "Cookie"); got != "REDACTED" {
		t.Fatalf("expected Cookie redacted, got %q", got)
	}
	if got := headerValue(entry.ResponseHeaders, "Set-Cookie"); got != "REDACTED" {
		t.Fatalf("expected Set-Cookie redacted, got %q", got)
	}
	if !entry.RequestBodyRedacted || !strings.Contains(entry.RequestBody, `"bootstrap_token":"REDACTED"`) {
		t.Fatalf("expected request body redaction, got %+v", entry)
	}
	if !entry.ResponseBodyRedacted || !strings.Contains(entry.ResponseBody, `"access_token":"REDACTED"`) {
		t.Fatalf("expected response body redaction, got %+v", entry)
	}
}

func TestSkipsDefaultProbeAndSSEPaths(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/readyz":
			_, _ = w.Write([]byte("ok"))
		case "/events/stream":
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte("data: ping\n\n"))
		default:
			_, _ = w.Write([]byte(`{"ok":true}`))
		}
	}))
	defer upstream.Close()

	handler := newTestHandler(t, &output, upstream.URL, false, 1<<20)
	server := httptest.NewServer(handler)
	defer server.Close()

	for _, path := range []string{"/readyz", "/events/stream", "/topics"} {
		resp, err := http.Get(server.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
	}

	entries := readEntries(t, output.Bytes())
	if len(entries) != 1 {
		t.Fatalf("expected only non-filtered path to be recorded, got %d entries", len(entries))
	}
	if entries[0].Path != "/topics" {
		t.Fatalf("expected /topics to be recorded, got %s", entries[0].Path)
	}
}

func TestMutationsOnlySkipsReads(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer upstream.Close()

	handler := newTestHandler(t, &output, upstream.URL, true, 1<<20)
	server := httptest.NewServer(handler)
	defer server.Close()

	for _, tc := range []struct {
		method string
		path   string
	}{
		{method: http.MethodGet, path: "/topics"},
		{method: http.MethodPost, path: "/topics"},
	} {
		req, err := http.NewRequest(tc.method, server.URL+tc.path, nil)
		if err != nil {
			t.Fatal(err)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
	}

	entries := readEntries(t, output.Bytes())
	if len(entries) != 1 || entries[0].Method != http.MethodPost {
		t.Fatalf("expected only POST recorded, got %+v", entries)
	}
}

func TestMarksTruncationWhenBodyExceedsLimit(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("0123456789"))
	}))
	defer upstream.Close()

	handler := newTestHandler(t, &output, upstream.URL, false, 4)
	server := httptest.NewServer(handler)
	defer server.Close()

	req, err := http.NewRequest(http.MethodPost, server.URL+"/topics", strings.NewReader("abcdefghij"))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "text/plain")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	entries := readEntries(t, output.Bytes())
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if !entries[0].TruncatedRequestBody || !entries[0].TruncatedResponseBody {
		t.Fatalf("expected truncation flags, got %+v", entries[0])
	}
	if entries[0].RequestBody != "abcd" {
		t.Fatalf("expected truncated request body sample, got %q", entries[0].RequestBody)
	}
	if entries[0].ResponseBody != "0123" {
		t.Fatalf("expected truncated response body sample, got %q", entries[0].ResponseBody)
	}
}

func TestPreservesTruncatedJSONBodyFragment(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","access_token":"very-secret-response-token"}`))
	}))
	defer upstream.Close()

	handler := newTestHandler(t, &output, upstream.URL, false, 48)
	server := httptest.NewServer(handler)
	defer server.Close()

	req, err := http.NewRequest(http.MethodPost, server.URL+"/auth/token", strings.NewReader(`{"status":"ok","bootstrap_token":"very-secret-request-token"}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	entries := readEntries(t, output.Bytes())
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	entry := entries[0]
	if !entry.TruncatedRequestBody || !entry.TruncatedResponseBody {
		t.Fatalf("expected both bodies truncated, got %+v", entry)
	}
	if entry.RequestBodyEncoding != "json_fragment" {
		t.Fatalf("expected request json_fragment encoding, got %q", entry.RequestBodyEncoding)
	}
	if entry.ResponseBodyEncoding != "json_fragment" {
		t.Fatalf("expected response json_fragment encoding, got %q", entry.ResponseBodyEncoding)
	}
	if entry.RequestBody == "" || entry.ResponseBody == "" {
		t.Fatalf("expected preserved body fragments, got %+v", entry)
	}
	if strings.Contains(entry.RequestBody, "very-secret-request-token") {
		t.Fatalf("expected request fragment redacted, got %q", entry.RequestBody)
	}
	if strings.Contains(entry.ResponseBody, "very-secret-response-token") {
		t.Fatalf("expected response fragment redacted, got %q", entry.ResponseBody)
	}
}

func newTestHandler(t *testing.T, output *bytes.Buffer, upstreamURL string, mutationsOnly bool, maxBodyBytes int64) http.Handler {
	t.Helper()

	parsed, err := url.Parse(upstreamURL)
	if err != nil {
		t.Fatal(err)
	}
	handler, err := NewHandler(Options{
		Upstream:      parsed,
		Recorder:      recorder.NewJSONLWriter(output),
		MaxBodyBytes:  maxBodyBytes,
		MutationsOnly: mutationsOnly,
	})
	if err != nil {
		t.Fatal(err)
	}
	return handler
}

func readEntries(t *testing.T, raw []byte) []recorder.Entry {
	t.Helper()

	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return nil
	}

	entries := make([]recorder.Entry, 0, len(lines))
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var entry recorder.Entry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("unmarshal entry %q: %v", line, err)
		}
		entries = append(entries, entry)
	}
	return entries
}

func headerValue(headers map[string][]string, key string) string {
	values := headers[key]
	if len(values) == 0 {
		return ""
	}
	return values[0]
}
