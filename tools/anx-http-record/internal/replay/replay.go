package replay

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"agent-nexus-tools-anx-http-record/internal/compiled"
)

type Options struct {
	BaseURL     string
	HTTPClient  *http.Client
	MaxAttempts int
	BaseDelay   time.Duration
}

func Replay(ctx context.Context, run compiled.Run, opts Options) (map[string]string, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(opts.BaseURL), "/")
	if baseURL == "" {
		return nil, fmt.Errorf("base url is required")
	}
	client := opts.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	maxAttempts := opts.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = 4
	}
	baseDelay := opts.BaseDelay
	if baseDelay <= 0 {
		baseDelay = 200 * time.Millisecond
	}

	bindings := map[string]string{}
	for _, exchange := range run.Exchanges {
		_, rawBody, err := doExchange(ctx, client, baseURL, exchange, bindings, maxAttempts, baseDelay)
		if err != nil {
			return nil, err
		}

		if len(exchange.Captures) == 0 {
			continue
		}
		var parsed any
		if err := json.Unmarshal(rawBody, &parsed); err != nil {
			return nil, fmt.Errorf("seq %d parse capture response json: %w", exchange.Seq, err)
		}
		for _, capture := range exchange.Captures {
			value, ok := jsonPointer(parsed, capture.ResponsePointer)
			if !ok {
				return nil, fmt.Errorf("seq %d missing capture %s at %s", exchange.Seq, capture.Alias, capture.ResponsePointer)
			}
			text, ok := value.(string)
			if !ok || strings.TrimSpace(text) == "" {
				return nil, fmt.Errorf("seq %d capture %s is not a non-empty string", exchange.Seq, capture.Alias)
			}
			if existing := bindings[capture.Alias]; existing != "" && existing != text {
				return nil, fmt.Errorf("seq %d capture %s rebound from %s to %s", exchange.Seq, capture.Alias, existing, text)
			}
			bindings[capture.Alias] = text
		}
	}

	return bindings, nil
}

func doExchange(
	ctx context.Context,
	client *http.Client,
	baseURL string,
	exchange compiled.Exchange,
	bindings map[string]string,
	maxAttempts int,
	baseDelay time.Duration,
) (*http.Response, []byte, error) {
	var lastStatus int
	var lastBody []byte
	for attempt := 0; attempt < maxAttempts; attempt++ {
		requestURL, err := url.Parse(baseURL + substitutePlaceholders(exchange.Path, bindings))
		if err != nil {
			return nil, nil, fmt.Errorf("seq %d parse path: %w", exchange.Seq, err)
		}
		if exchange.Query != "" {
			requestURL.RawQuery = substitutePlaceholders(exchange.Query, bindings)
		}

		bodyReader, contentType, err := requestBody(exchange, bindings)
		if err != nil {
			return nil, nil, fmt.Errorf("seq %d build request body: %w", exchange.Seq, err)
		}

		req, err := http.NewRequestWithContext(ctx, exchange.Method, requestURL.String(), bodyReader)
		if err != nil {
			return nil, nil, fmt.Errorf("seq %d build request: %w", exchange.Seq, err)
		}
		applyHeaders(req.Header, exchange.RequestHeaders, bindings)
		if contentType != "" && req.Header.Get("Content-Type") == "" {
			req.Header.Set("Content-Type", contentType)
		}
		if req.Header.Get("Accept") == "" {
			req.Header.Set("Accept", "application/json")
		}

		resp, err := client.Do(req)
		if err != nil {
			return nil, nil, fmt.Errorf("seq %d perform request: %w", exchange.Seq, err)
		}
		rawBody, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			return nil, nil, fmt.Errorf("seq %d read response: %w", exchange.Seq, readErr)
		}
		if resp.StatusCode == exchange.ExpectedStatusCode {
			return resp, rawBody, nil
		}
		lastStatus = resp.StatusCode
		lastBody = rawBody
		if resp.StatusCode < 500 || resp.StatusCode >= 600 || attempt == maxAttempts-1 {
			break
		}
		timer := time.NewTimer(baseDelay * time.Duration(attempt+1))
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, nil, ctx.Err()
		case <-timer.C:
		}
	}
	return nil, nil, fmt.Errorf("seq %d expected status %d, got %d: %s", exchange.Seq, exchange.ExpectedStatusCode, lastStatus, strings.TrimSpace(string(lastBody)))
}

func requestBody(exchange compiled.Exchange, bindings map[string]string) (io.Reader, string, error) {
	switch exchange.RequestBodyKind {
	case "":
		return nil, "", nil
	case "text":
		text, ok := exchange.RequestBody.(string)
		if !ok {
			return nil, "", fmt.Errorf("text request body must be string")
		}
		return strings.NewReader(substitutePlaceholders(text, bindings)), "text/plain; charset=utf-8", nil
	case "json":
		replaced := replaceValue(exchange.RequestBody, bindings)
		raw, err := json.Marshal(replaced)
		if err != nil {
			return nil, "", err
		}
		return bytes.NewReader(raw), "application/json", nil
	default:
		return nil, "", fmt.Errorf("unsupported request body kind %q", exchange.RequestBodyKind)
	}
}

func applyHeaders(dst http.Header, headers map[string][]string, bindings map[string]string) {
	for key, values := range headers {
		for _, value := range values {
			dst.Add(key, substitutePlaceholders(value, bindings))
		}
	}
}

func replaceValue(value any, bindings map[string]string) any {
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, child := range typed {
			out[key] = replaceValue(child, bindings)
		}
		return out
	case []any:
		out := make([]any, len(typed))
		for i, child := range typed {
			out[i] = replaceValue(child, bindings)
		}
		return out
	case string:
		return substitutePlaceholders(typed, bindings)
	default:
		return value
	}
}

var placeholderPattern = regexp.MustCompile(`\{\{([A-Za-z0-9_]+)\}\}`)

func substitutePlaceholders(raw string, bindings map[string]string) string {
	return placeholderPattern.ReplaceAllStringFunc(raw, func(match string) string {
		name := strings.TrimSuffix(strings.TrimPrefix(match, "{{"), "}}")
		if value := bindings[name]; value != "" {
			return value
		}
		return match
	})
}

func jsonPointer(value any, pointer string) (any, bool) {
	if pointer == "" || pointer == "/" {
		return value, true
	}
	if !strings.HasPrefix(pointer, "/") {
		return nil, false
	}
	current := value
	parts := strings.Split(pointer, "/")[1:]
	for _, part := range parts {
		part = strings.ReplaceAll(strings.ReplaceAll(part, "~1", "/"), "~0", "~")
		obj, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		next, ok := obj[part]
		if !ok {
			return nil, false
		}
		current = next
	}
	return current, true
}
