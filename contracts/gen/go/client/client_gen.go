package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

type Example struct {
	Title       string `json:"title"`
	Command     string `json:"command"`
	Description string `json:"description,omitempty"`
}

type CommandSpec struct {
	CommandID  string    `json:"command_id"`
	CLIPath    string    `json:"cli_path"`
	Group      string    `json:"group,omitempty"`
	Method     string    `json:"method"`
	Path       string    `json:"path"`
	PathParams []string  `json:"path_params,omitempty"`
	InputMode  string    `json:"input_mode,omitempty"`
	Stability  string    `json:"stability,omitempty"`
	Concepts   []string  `json:"concepts,omitempty"`
	Adjacent   []string  `json:"adjacent_commands,omitempty"`
	Examples   []Example `json:"examples,omitempty"`
}

var CommandRegistry = []CommandSpec{
	{
		CommandID: "artifacts.create",
		CLIPath:   "artifacts create",
		Group:     "artifacts",
		Method:    "POST",
		Path:      "/artifacts",
		InputMode: "json-body",
		Stability: "beta",
		Concepts:  []string{"artifacts", "write"},
		Adjacent:  []string{"artifacts.get", "artifacts.list"},
	},
	{
		CommandID:  "artifacts.get",
		CLIPath:    "artifacts get",
		Group:      "artifacts",
		Method:     "GET",
		Path:       "/artifacts/{artifact_id}",
		PathParams: []string{"artifact_id"},
		InputMode:  "none",
		Stability:  "beta",
		Concepts:   []string{"artifacts"},
		Adjacent:   []string{"artifacts.create", "artifacts.list"},
	},
	{
		CommandID: "artifacts.list",
		CLIPath:   "artifacts list",
		Group:     "artifacts",
		Method:    "GET",
		Path:      "/artifacts",
		InputMode: "none",
		Stability: "beta",
		Concepts:  []string{"artifacts"},
		Adjacent:  []string{"artifacts.create", "artifacts.get"},
	},
	{
		CommandID:  "boards.cards.create",
		CLIPath:    "boards cards create",
		Group:      "boards",
		Method:     "POST",
		Path:       "/boards/{board_id}/cards",
		PathParams: []string{"board_id"},
		InputMode:  "json-body",
		Stability:  "beta",
		Concepts:   []string{"boards", "cards", "write"},
		Adjacent:   []string{"boards.cards.get", "boards.cards.list", "boards.create", "boards.get", "boards.list", "boards.patch", "boards.workspace"},
	},
	{
		CommandID:  "boards.cards.get",
		CLIPath:    "boards cards get",
		Group:      "boards",
		Method:     "GET",
		Path:       "/boards/{board_id}/cards/{card_id}",
		PathParams: []string{"board_id", "card_id"},
		InputMode:  "none",
		Stability:  "beta",
		Concepts:   []string{"boards", "cards"},
		Adjacent:   []string{"boards.cards.create", "boards.cards.list", "boards.create", "boards.get", "boards.list", "boards.patch", "boards.workspace"},
	},
	{
		CommandID:  "boards.cards.list",
		CLIPath:    "boards cards list",
		Group:      "boards",
		Method:     "GET",
		Path:       "/boards/{board_id}/cards",
		PathParams: []string{"board_id"},
		InputMode:  "none",
		Stability:  "beta",
		Concepts:   []string{"boards", "cards"},
		Adjacent:   []string{"boards.cards.create", "boards.cards.get", "boards.create", "boards.get", "boards.list", "boards.patch", "boards.workspace"},
	},
	{
		CommandID: "boards.create",
		CLIPath:   "boards create",
		Group:     "boards",
		Method:    "POST",
		Path:      "/boards",
		InputMode: "json-body",
		Stability: "beta",
		Concepts:  []string{"boards", "write"},
		Adjacent:  []string{"boards.cards.create", "boards.cards.get", "boards.cards.list", "boards.get", "boards.list", "boards.patch", "boards.workspace"},
	},
	{
		CommandID:  "boards.get",
		CLIPath:    "boards get",
		Group:      "boards",
		Method:     "GET",
		Path:       "/boards/{board_id}",
		PathParams: []string{"board_id"},
		InputMode:  "none",
		Stability:  "beta",
		Concepts:   []string{"boards"},
		Adjacent:   []string{"boards.cards.create", "boards.cards.get", "boards.cards.list", "boards.create", "boards.list", "boards.patch", "boards.workspace"},
	},
	{
		CommandID: "boards.list",
		CLIPath:   "boards list",
		Group:     "boards",
		Method:    "GET",
		Path:      "/boards",
		InputMode: "none",
		Stability: "beta",
		Concepts:  []string{"boards"},
		Adjacent:  []string{"boards.cards.create", "boards.cards.get", "boards.cards.list", "boards.create", "boards.get", "boards.patch", "boards.workspace"},
	},
	{
		CommandID:  "boards.patch",
		CLIPath:    "boards patch",
		Group:      "boards",
		Method:     "PATCH",
		Path:       "/boards/{board_id}",
		PathParams: []string{"board_id"},
		InputMode:  "json-body",
		Stability:  "beta",
		Concepts:   []string{"boards", "write", "concurrency"},
		Adjacent:   []string{"boards.cards.create", "boards.cards.get", "boards.cards.list", "boards.create", "boards.get", "boards.list", "boards.workspace"},
	},
	{
		CommandID:  "boards.workspace",
		CLIPath:    "boards workspace",
		Group:      "boards",
		Method:     "GET",
		Path:       "/boards/{board_id}/workspace",
		PathParams: []string{"board_id"},
		InputMode:  "none",
		Stability:  "beta",
		Concepts:   []string{"boards", "workspace"},
		Adjacent:   []string{"boards.cards.create", "boards.cards.get", "boards.cards.list", "boards.create", "boards.get", "boards.list", "boards.patch"},
	},
	{
		CommandID:  "cards.archive",
		CLIPath:    "cards archive",
		Group:      "cards",
		Method:     "POST",
		Path:       "/cards/{card_id}/archive",
		PathParams: []string{"card_id"},
		InputMode:  "json-body",
		Stability:  "beta",
		Concepts:   []string{"cards", "write"},
		Adjacent:   []string{"cards.get", "cards.list", "cards.move", "cards.patch", "cards.purge", "cards.restore"},
	},
	{
		CommandID:  "cards.get",
		CLIPath:    "cards get",
		Group:      "cards",
		Method:     "GET",
		Path:       "/cards/{card_id}",
		PathParams: []string{"card_id"},
		InputMode:  "none",
		Stability:  "beta",
		Concepts:   []string{"cards"},
		Adjacent:   []string{"cards.archive", "cards.list", "cards.move", "cards.patch", "cards.purge", "cards.restore"},
	},
	{
		CommandID: "cards.list",
		CLIPath:   "cards list",
		Group:     "cards",
		Method:    "GET",
		Path:      "/cards",
		InputMode: "none",
		Stability: "beta",
		Concepts:  []string{"cards"},
		Adjacent:  []string{"cards.archive", "cards.get", "cards.move", "cards.patch", "cards.purge", "cards.restore"},
	},
	{
		CommandID:  "cards.move",
		CLIPath:    "cards move",
		Group:      "cards",
		Method:     "POST",
		Path:       "/cards/{card_id}/move",
		PathParams: []string{"card_id"},
		InputMode:  "json-body",
		Stability:  "beta",
		Concepts:   []string{"cards", "boards", "write"},
		Adjacent:   []string{"cards.archive", "cards.get", "cards.list", "cards.patch", "cards.purge", "cards.restore"},
	},
	{
		CommandID:  "cards.patch",
		CLIPath:    "cards patch",
		Group:      "cards",
		Method:     "PATCH",
		Path:       "/cards/{card_id}",
		PathParams: []string{"card_id"},
		InputMode:  "json-body",
		Stability:  "beta",
		Concepts:   []string{"cards", "write", "concurrency"},
		Adjacent:   []string{"cards.archive", "cards.get", "cards.list", "cards.move", "cards.purge", "cards.restore"},
	},
	{
		CommandID:  "cards.purge",
		CLIPath:    "cards purge",
		Group:      "cards",
		Method:     "POST",
		Path:       "/cards/{card_id}/purge",
		PathParams: []string{"card_id"},
		InputMode:  "json-body",
		Stability:  "beta",
		Concepts:   []string{"cards", "write"},
		Adjacent:   []string{"cards.archive", "cards.get", "cards.list", "cards.move", "cards.patch", "cards.restore"},
	},
	{
		CommandID:  "cards.restore",
		CLIPath:    "cards restore",
		Group:      "cards",
		Method:     "POST",
		Path:       "/cards/{card_id}/restore",
		PathParams: []string{"card_id"},
		InputMode:  "json-body",
		Stability:  "beta",
		Concepts:   []string{"cards", "write"},
		Adjacent:   []string{"cards.archive", "cards.get", "cards.list", "cards.move", "cards.patch", "cards.purge"},
	},
	{
		CommandID: "docs.create",
		CLIPath:   "docs create",
		Group:     "docs",
		Method:    "POST",
		Path:      "/docs",
		InputMode: "json-body",
		Stability: "beta",
		Concepts:  []string{"docs", "write"},
		Adjacent:  []string{"docs.get", "docs.list", "docs.revisions.create", "docs.revisions.get", "docs.revisions.list"},
	},
	{
		CommandID:  "docs.get",
		CLIPath:    "docs get",
		Group:      "docs",
		Method:     "GET",
		Path:       "/docs/{document_id}",
		PathParams: []string{"document_id"},
		InputMode:  "none",
		Stability:  "beta",
		Concepts:   []string{"docs"},
		Adjacent:   []string{"docs.create", "docs.list", "docs.revisions.create", "docs.revisions.get", "docs.revisions.list"},
	},
	{
		CommandID: "docs.list",
		CLIPath:   "docs list",
		Group:     "docs",
		Method:    "GET",
		Path:      "/docs",
		InputMode: "none",
		Stability: "beta",
		Concepts:  []string{"docs"},
		Adjacent:  []string{"docs.create", "docs.get", "docs.revisions.create", "docs.revisions.get", "docs.revisions.list"},
	},
	{
		CommandID:  "docs.revisions.create",
		CLIPath:    "docs revisions create",
		Group:      "docs",
		Method:     "POST",
		Path:       "/docs/{document_id}/revisions",
		PathParams: []string{"document_id"},
		InputMode:  "json-body",
		Stability:  "beta",
		Concepts:   []string{"docs", "revisions", "write"},
		Adjacent:   []string{"docs.create", "docs.get", "docs.list", "docs.revisions.get", "docs.revisions.list"},
	},
	{
		CommandID:  "docs.revisions.get",
		CLIPath:    "docs revisions get",
		Group:      "docs",
		Method:     "GET",
		Path:       "/docs/{document_id}/revisions/{revision_id}",
		PathParams: []string{"document_id", "revision_id"},
		InputMode:  "none",
		Stability:  "beta",
		Concepts:   []string{"docs", "revisions"},
		Adjacent:   []string{"docs.create", "docs.get", "docs.list", "docs.revisions.create", "docs.revisions.list"},
	},
	{
		CommandID:  "docs.revisions.list",
		CLIPath:    "docs revisions list",
		Group:      "docs",
		Method:     "GET",
		Path:       "/docs/{document_id}/revisions",
		PathParams: []string{"document_id"},
		InputMode:  "none",
		Stability:  "beta",
		Concepts:   []string{"docs", "revisions"},
		Adjacent:   []string{"docs.create", "docs.get", "docs.list", "docs.revisions.create", "docs.revisions.get"},
	},
	{
		CommandID: "events.create",
		CLIPath:   "events create",
		Group:     "events",
		Method:    "POST",
		Path:      "/events",
		InputMode: "json-body",
		Stability: "beta",
		Concepts:  []string{"events", "write"},
		Adjacent:  []string{"events.list"},
	},
	{
		CommandID: "events.list",
		CLIPath:   "events list",
		Group:     "events",
		Method:    "GET",
		Path:      "/events",
		InputMode: "none",
		Stability: "beta",
		Concepts:  []string{"events"},
		Adjacent:  []string{"events.create"},
	},
	{
		CommandID:  "inbox.acknowledge",
		CLIPath:    "inbox acknowledge",
		Group:      "inbox",
		Method:     "POST",
		Path:       "/inbox/{inbox_id}/acknowledge",
		PathParams: []string{"inbox_id"},
		InputMode:  "json-body",
		Stability:  "beta",
		Concepts:   []string{"inbox", "write"},
		Adjacent:   []string{"inbox.list"},
	},
	{
		CommandID: "inbox.list",
		CLIPath:   "inbox list",
		Group:     "inbox",
		Method:    "GET",
		Path:      "/inbox",
		InputMode: "none",
		Stability: "beta",
		Concepts:  []string{"inbox"},
		Adjacent:  []string{"inbox.acknowledge"},
	},
	{
		CommandID: "meta.health",
		CLIPath:   "meta health",
		Group:     "meta",
		Method:    "GET",
		Path:      "/health",
		InputMode: "none",
		Stability: "stable",
		Concepts:  []string{"health"},
		Adjacent:  []string{"meta.readyz", "meta.version"},
	},
	{
		CommandID: "meta.readyz",
		CLIPath:   "meta readyz",
		Group:     "meta",
		Method:    "GET",
		Path:      "/readyz",
		InputMode: "none",
		Stability: "stable",
		Concepts:  []string{"health", "readiness"},
		Adjacent:  []string{"meta.health", "meta.version"},
	},
	{
		CommandID: "meta.version",
		CLIPath:   "meta version",
		Group:     "meta",
		Method:    "GET",
		Path:      "/version",
		InputMode: "none",
		Stability: "stable",
		Concepts:  []string{"compatibility"},
		Adjacent:  []string{"meta.health", "meta.readyz"},
	},
	{
		CommandID: "packets.receipts.create",
		CLIPath:   "packets receipts create",
		Group:     "packets",
		Method:    "POST",
		Path:      "/packets/receipts",
		InputMode: "json-body",
		Stability: "beta",
		Concepts:  []string{"packets", "evidence"},
		Adjacent:  []string{"packets.reviews.create", "packets.work-orders.create"},
	},
	{
		CommandID: "packets.reviews.create",
		CLIPath:   "packets reviews create",
		Group:     "packets",
		Method:    "POST",
		Path:      "/packets/reviews",
		InputMode: "json-body",
		Stability: "beta",
		Concepts:  []string{"packets", "evidence"},
		Adjacent:  []string{"packets.receipts.create", "packets.work-orders.create"},
	},
	{
		CommandID: "packets.work-orders.create",
		CLIPath:   "packets work-orders create",
		Group:     "packets",
		Method:    "POST",
		Path:      "/packets/work-orders",
		InputMode: "json-body",
		Stability: "beta",
		Concepts:  []string{"packets", "evidence"},
		Adjacent:  []string{"packets.receipts.create", "packets.reviews.create"},
	},
	{
		CommandID:  "threads.context",
		CLIPath:    "threads context",
		Group:      "threads",
		Method:     "GET",
		Path:       "/threads/{thread_id}/context",
		PathParams: []string{"thread_id"},
		InputMode:  "none",
		Stability:  "beta",
		Concepts:   []string{"threads", "inspection"},
		Adjacent:   []string{"threads.inspect", "threads.list", "threads.timeline", "threads.workspace"},
	},
	{
		CommandID:  "threads.inspect",
		CLIPath:    "threads inspect",
		Group:      "threads",
		Method:     "GET",
		Path:       "/threads/{thread_id}",
		PathParams: []string{"thread_id"},
		InputMode:  "none",
		Stability:  "beta",
		Concepts:   []string{"threads", "inspection"},
		Adjacent:   []string{"threads.context", "threads.list", "threads.timeline", "threads.workspace"},
	},
	{
		CommandID: "threads.list",
		CLIPath:   "threads list",
		Group:     "threads",
		Method:    "GET",
		Path:      "/threads",
		InputMode: "none",
		Stability: "beta",
		Concepts:  []string{"threads", "inspection"},
		Adjacent:  []string{"threads.context", "threads.inspect", "threads.timeline", "threads.workspace"},
	},
	{
		CommandID:  "threads.timeline",
		CLIPath:    "threads timeline",
		Group:      "threads",
		Method:     "GET",
		Path:       "/threads/{thread_id}/timeline",
		PathParams: []string{"thread_id"},
		InputMode:  "none",
		Stability:  "beta",
		Concepts:   []string{"threads", "timeline"},
		Adjacent:   []string{"threads.context", "threads.inspect", "threads.list", "threads.workspace"},
	},
	{
		CommandID:  "threads.workspace",
		CLIPath:    "threads workspace",
		Group:      "threads",
		Method:     "GET",
		Path:       "/threads/{thread_id}/workspace",
		PathParams: []string{"thread_id"},
		InputMode:  "none",
		Stability:  "beta",
		Concepts:   []string{"threads", "workspace"},
		Adjacent:   []string{"threads.context", "threads.inspect", "threads.list", "threads.timeline"},
	},
	{
		CommandID: "topics.create",
		CLIPath:   "topics create",
		Group:     "topics",
		Method:    "POST",
		Path:      "/topics",
		InputMode: "json-body",
		Stability: "beta",
		Concepts:  []string{"topics", "write"},
		Adjacent:  []string{"topics.get", "topics.list", "topics.patch", "topics.timeline", "topics.workspace"},
	},
	{
		CommandID:  "topics.get",
		CLIPath:    "topics get",
		Group:      "topics",
		Method:     "GET",
		Path:       "/topics/{topic_id}",
		PathParams: []string{"topic_id"},
		InputMode:  "none",
		Stability:  "beta",
		Concepts:   []string{"topics"},
		Adjacent:   []string{"topics.create", "topics.list", "topics.patch", "topics.timeline", "topics.workspace"},
	},
	{
		CommandID: "topics.list",
		CLIPath:   "topics list",
		Group:     "topics",
		Method:    "GET",
		Path:      "/topics",
		InputMode: "none",
		Stability: "beta",
		Concepts:  []string{"topics"},
		Adjacent:  []string{"topics.create", "topics.get", "topics.patch", "topics.timeline", "topics.workspace"},
	},
	{
		CommandID:  "topics.patch",
		CLIPath:    "topics patch",
		Group:      "topics",
		Method:     "PATCH",
		Path:       "/topics/{topic_id}",
		PathParams: []string{"topic_id"},
		InputMode:  "json-body",
		Stability:  "beta",
		Concepts:   []string{"topics", "write", "concurrency"},
		Adjacent:   []string{"topics.create", "topics.get", "topics.list", "topics.timeline", "topics.workspace"},
	},
	{
		CommandID:  "topics.timeline",
		CLIPath:    "topics timeline",
		Group:      "topics",
		Method:     "GET",
		Path:       "/topics/{topic_id}/timeline",
		PathParams: []string{"topic_id"},
		InputMode:  "none",
		Stability:  "beta",
		Concepts:   []string{"topics", "timeline"},
		Adjacent:   []string{"topics.create", "topics.get", "topics.list", "topics.patch", "topics.workspace"},
	},
	{
		CommandID:  "topics.workspace",
		CLIPath:    "topics workspace",
		Group:      "topics",
		Method:     "GET",
		Path:       "/topics/{topic_id}/workspace",
		PathParams: []string{"topic_id"},
		InputMode:  "none",
		Stability:  "beta",
		Concepts:   []string{"topics", "workspace"},
		Adjacent:   []string{"topics.create", "topics.get", "topics.list", "topics.patch", "topics.timeline"},
	},
}

var commandIndex = func() map[string]CommandSpec {
	index := make(map[string]CommandSpec, len(CommandRegistry))
	for _, cmd := range CommandRegistry {
		index[cmd.CommandID] = cmd
	}
	return index
}()

type RequestOptions struct {
	Query   map[string][]string
	Headers map[string]string
	Body    any
}

type Client struct {
	BaseURL    string
	HTTPClient *http.Client
}

func New(baseURL string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{}
	}
	return &Client{BaseURL: strings.TrimRight(baseURL, "/"), HTTPClient: httpClient}
}

func (c *Client) Invoke(ctx context.Context, commandID string, pathParams map[string]string, opts RequestOptions) (*http.Response, []byte, error) {
	if c == nil {
		return nil, nil, fmt.Errorf("client is nil")
	}
	if strings.TrimSpace(c.BaseURL) == "" {
		return nil, nil, fmt.Errorf("base url is required")
	}
	if c.HTTPClient == nil {
		return nil, nil, fmt.Errorf("http client is required")
	}
	cmd, ok := commandIndex[commandID]
	if !ok {
		return nil, nil, fmt.Errorf("unknown command id: %s", commandID)
	}
	path, err := renderPath(cmd.Path, pathParams)
	if err != nil {
		return nil, nil, err
	}
	urlString := c.BaseURL + path
	u, err := url.Parse(urlString)
	if err != nil {
		return nil, nil, fmt.Errorf("parse request url: %w", err)
	}
	if len(opts.Query) > 0 {
		q := u.Query()
		for key, values := range opts.Query {
			for _, value := range values {
				q.Add(key, value)
			}
		}
		u.RawQuery = q.Encode()
	}
	var body io.Reader
	if opts.Body != nil {
		encoded, err := json.Marshal(opts.Body)
		if err != nil {
			return nil, nil, fmt.Errorf("encode request body: %w", err)
		}
		body = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, cmd.Method, u.String(), body)
	if err != nil {
		return nil, nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if opts.Body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for key, value := range opts.Headers {
		if strings.TrimSpace(key) == "" {
			continue
		}
		req.Header.Set(key, value)
	}
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("perform request: %w", err)
	}
	bodyBytes, readErr := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if readErr != nil {
		return resp, nil, fmt.Errorf("read response: %w", readErr)
	}
	if resp.StatusCode >= http.StatusBadRequest {
		return resp, bodyBytes, fmt.Errorf("request failed: status=%d body=%s", resp.StatusCode, string(bodyBytes))
	}
	return resp, bodyBytes, nil
}

func renderPath(template string, pathParams map[string]string) (string, error) {
	b := template
	for {
		start := strings.IndexByte(b, '{')
		if start < 0 {
			return b, nil
		}
		end := strings.IndexByte(b[start:], '}')
		if end < 0 {
			return "", fmt.Errorf("invalid path template: %s", template)
		}
		end += start
		name := b[start+1 : end]
		value, ok := pathParams[name]
		if !ok {
			return "", fmt.Errorf("missing path param %q", name)
		}
		b = b[:start] + url.PathEscape(value) + b[end+1:]
	}
}

func (c *Client) ArtifactsCreate(ctx context.Context, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "artifacts.create", nil, opts)
}

func (c *Client) ArtifactsGet(ctx context.Context, pathParams map[string]string, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "artifacts.get", pathParams, opts)
}

func (c *Client) ArtifactsList(ctx context.Context, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "artifacts.list", nil, opts)
}

func (c *Client) BoardsCardsCreate(ctx context.Context, pathParams map[string]string, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "boards.cards.create", pathParams, opts)
}

func (c *Client) BoardsCardsGet(ctx context.Context, pathParams map[string]string, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "boards.cards.get", pathParams, opts)
}

func (c *Client) BoardsCardsList(ctx context.Context, pathParams map[string]string, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "boards.cards.list", pathParams, opts)
}

func (c *Client) BoardsCreate(ctx context.Context, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "boards.create", nil, opts)
}

func (c *Client) BoardsGet(ctx context.Context, pathParams map[string]string, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "boards.get", pathParams, opts)
}

func (c *Client) BoardsList(ctx context.Context, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "boards.list", nil, opts)
}

func (c *Client) BoardsPatch(ctx context.Context, pathParams map[string]string, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "boards.patch", pathParams, opts)
}

func (c *Client) BoardsWorkspace(ctx context.Context, pathParams map[string]string, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "boards.workspace", pathParams, opts)
}

func (c *Client) CardsArchive(ctx context.Context, pathParams map[string]string, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "cards.archive", pathParams, opts)
}

func (c *Client) CardsGet(ctx context.Context, pathParams map[string]string, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "cards.get", pathParams, opts)
}

func (c *Client) CardsList(ctx context.Context, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "cards.list", nil, opts)
}

func (c *Client) CardsMove(ctx context.Context, pathParams map[string]string, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "cards.move", pathParams, opts)
}

func (c *Client) CardsPatch(ctx context.Context, pathParams map[string]string, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "cards.patch", pathParams, opts)
}

func (c *Client) CardsPurge(ctx context.Context, pathParams map[string]string, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "cards.purge", pathParams, opts)
}

func (c *Client) CardsRestore(ctx context.Context, pathParams map[string]string, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "cards.restore", pathParams, opts)
}

func (c *Client) DocsCreate(ctx context.Context, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "docs.create", nil, opts)
}

func (c *Client) DocsGet(ctx context.Context, pathParams map[string]string, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "docs.get", pathParams, opts)
}

func (c *Client) DocsList(ctx context.Context, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "docs.list", nil, opts)
}

func (c *Client) DocsRevisionsCreate(ctx context.Context, pathParams map[string]string, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "docs.revisions.create", pathParams, opts)
}

func (c *Client) DocsRevisionsGet(ctx context.Context, pathParams map[string]string, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "docs.revisions.get", pathParams, opts)
}

func (c *Client) DocsRevisionsList(ctx context.Context, pathParams map[string]string, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "docs.revisions.list", pathParams, opts)
}

func (c *Client) EventsCreate(ctx context.Context, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "events.create", nil, opts)
}

func (c *Client) EventsList(ctx context.Context, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "events.list", nil, opts)
}

func (c *Client) InboxAcknowledge(ctx context.Context, pathParams map[string]string, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "inbox.acknowledge", pathParams, opts)
}

func (c *Client) InboxList(ctx context.Context, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "inbox.list", nil, opts)
}

func (c *Client) MetaHealth(ctx context.Context, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "meta.health", nil, opts)
}

func (c *Client) MetaReadyz(ctx context.Context, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "meta.readyz", nil, opts)
}

func (c *Client) MetaVersion(ctx context.Context, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "meta.version", nil, opts)
}

func (c *Client) PacketsReceiptsCreate(ctx context.Context, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "packets.receipts.create", nil, opts)
}

func (c *Client) PacketsReviewsCreate(ctx context.Context, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "packets.reviews.create", nil, opts)
}

func (c *Client) PacketsWorkOrdersCreate(ctx context.Context, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "packets.work-orders.create", nil, opts)
}

func (c *Client) ThreadsContext(ctx context.Context, pathParams map[string]string, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "threads.context", pathParams, opts)
}

func (c *Client) ThreadsInspect(ctx context.Context, pathParams map[string]string, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "threads.inspect", pathParams, opts)
}

func (c *Client) ThreadsList(ctx context.Context, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "threads.list", nil, opts)
}

func (c *Client) ThreadsTimeline(ctx context.Context, pathParams map[string]string, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "threads.timeline", pathParams, opts)
}

func (c *Client) ThreadsWorkspace(ctx context.Context, pathParams map[string]string, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "threads.workspace", pathParams, opts)
}

func (c *Client) TopicsCreate(ctx context.Context, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "topics.create", nil, opts)
}

func (c *Client) TopicsGet(ctx context.Context, pathParams map[string]string, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "topics.get", pathParams, opts)
}

func (c *Client) TopicsList(ctx context.Context, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "topics.list", nil, opts)
}

func (c *Client) TopicsPatch(ctx context.Context, pathParams map[string]string, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "topics.patch", pathParams, opts)
}

func (c *Client) TopicsTimeline(ctx context.Context, pathParams map[string]string, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "topics.timeline", pathParams, opts)
}

func (c *Client) TopicsWorkspace(ctx context.Context, pathParams map[string]string, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "topics.workspace", pathParams, opts)
}
