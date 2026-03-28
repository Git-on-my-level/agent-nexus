package httpclient

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	contractsclient "organization-autorunner-contracts-go-client/client"

	"organization-autorunner-cli/internal/buildinfo"
	"organization-autorunner-cli/internal/config"
)

var CLIVersion = buildinfo.Current

type Client struct {
	baseURL     string
	httpClient  *http.Client
	accessToken string
	agent       string
	generated   *contractsclient.Client
}

type RawRequest struct {
	Method  string
	Path    string
	Headers map[string]string
	Body    []byte
}

type RawResponse struct {
	StatusCode int
	Headers    http.Header
	Body       []byte
}

func New(resolved config.Resolved) (*Client, error) {
	baseURL := strings.TrimSpace(resolved.BaseURL)
	if baseURL == "" {
		return nil, fmt.Errorf("base url is required")
	}
	if _, err := url.Parse(baseURL); err != nil {
		return nil, fmt.Errorf("parse base url: %w", err)
	}
	httpClient := &http.Client{Timeout: resolved.Timeout}
	return &Client{
		baseURL:     strings.TrimRight(baseURL, "/"),
		httpClient:  httpClient,
		accessToken: strings.TrimSpace(resolved.AccessToken),
		agent:       strings.TrimSpace(resolved.Agent),
		generated:   contractsclient.New(strings.TrimRight(baseURL, "/"), httpClient),
	}, nil
}

func (c *Client) Generated() *contractsclient.Client {
	if c == nil {
		return nil
	}
	return c.generated
}

func (c *Client) RawCall(ctx context.Context, req RawRequest) (RawResponse, error) {
	if c == nil {
		return RawResponse{}, fmt.Errorf("client is nil")
	}
	method := strings.ToUpper(strings.TrimSpace(req.Method))
	if method == "" {
		method = http.MethodGet
	}
	requestURL, err := c.resolveURL(req.Path)
	if err != nil {
		return RawResponse{}, err
	}

	var body io.Reader
	if len(req.Body) > 0 {
		body = bytes.NewReader(req.Body)
	}
	httpReq, err := http.NewRequestWithContext(ctx, method, requestURL, body)
	if err != nil {
		return RawResponse{}, fmt.Errorf("build request: %w", err)
	}

	for key, value := range c.defaultHeaders() {
		httpReq.Header.Set(key, value)
	}
	for key, value := range req.Headers {
		if strings.TrimSpace(key) == "" {
			continue
		}
		httpReq.Header.Set(key, value)
	}
	if len(req.Body) > 0 && httpReq.Header.Get("Content-Type") == "" {
		httpReq.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return RawResponse{}, fmt.Errorf("perform request: %w", err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return RawResponse{}, fmt.Errorf("read response body: %w", err)
	}
	return RawResponse{StatusCode: resp.StatusCode, Headers: resp.Header.Clone(), Body: respBody}, nil
}

func (c *Client) OpenStream(ctx context.Context, req RawRequest) (*http.Response, error) {
	if c == nil {
		return nil, fmt.Errorf("client is nil")
	}
	method := strings.ToUpper(strings.TrimSpace(req.Method))
	if method == "" {
		method = http.MethodGet
	}
	requestURL, err := c.resolveURL(req.Path)
	if err != nil {
		return nil, err
	}

	var body io.Reader
	if len(req.Body) > 0 {
		body = bytes.NewReader(req.Body)
	}
	httpReq, err := http.NewRequestWithContext(ctx, method, requestURL, body)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}

	for key, value := range c.defaultHeaders() {
		httpReq.Header.Set(key, value)
	}
	for key, value := range req.Headers {
		if strings.TrimSpace(key) == "" {
			continue
		}
		httpReq.Header.Set(key, value)
	}
	if len(req.Body) > 0 && httpReq.Header.Get("Content-Type") == "" {
		httpReq.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("perform request: %w", err)
	}
	return resp, nil
}

func (c *Client) resolveURL(rawPath string) (string, error) {
	rawPath = strings.TrimSpace(rawPath)
	if rawPath == "" {
		return "", fmt.Errorf("path is required")
	}
	if strings.HasPrefix(rawPath, "http://") || strings.HasPrefix(rawPath, "https://") {
		u, err := url.Parse(rawPath)
		if err != nil {
			return "", fmt.Errorf("parse path url: %w", err)
		}
		return u.String(), nil
	}

	base, err := url.Parse(c.baseURL)
	if err != nil {
		return "", fmt.Errorf("parse base url: %w", err)
	}
	if !strings.HasPrefix(rawPath, "/") {
		rawPath = "/" + rawPath
	}
	u, err := url.Parse(rawPath)
	if err != nil {
		return "", fmt.Errorf("parse request path: %w", err)
	}
	base.Path = path.Join(base.Path, u.Path)
	base.RawQuery = u.RawQuery
	return base.String(), nil
}

func (c *Client) defaultHeaders() map[string]string {
	headers := map[string]string{
		"Accept":            "application/json",
		"X-OAR-CLI-Version": CLIVersion,
	}
	if c.agent != "" {
		headers["X-OAR-Agent"] = c.agent
	}
	if c.accessToken != "" {
		headers["Authorization"] = "Bearer " + c.accessToken
	}
	return headers
}

func WithTimeout(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, timeout)
}
