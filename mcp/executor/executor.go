package executor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	contracts "agent-nexus-contracts-go-client/client"

	"github.com/Git-on-my-level/agent-nexus/mcp/catalog"
	"github.com/Git-on-my-level/agent-nexus/mcp/protocol"
	"github.com/Git-on-my-level/agent-nexus/mcp/redaction"
)

const (
	defaultRequestTimeout = 30 * time.Second
	defaultListLimit      = 50
	defaultMaxListLimit   = 100
	defaultMaxRequestBody = 1 << 20
)

type AuthContext struct {
	BearerToken string
	Headers     map[string]string
}

type Options struct {
	HTTPClient        *http.Client
	Auth              AuthContext
	DefaultListLimit  int
	MaxListLimit      int
	MaxRequestBytes   int64
	RequestTimeout    time.Duration
	AdditionalHeaders map[string]string
}

type WorkspaceExecutor struct {
	client           *contracts.Client
	auth             AuthContext
	defaultListLimit int
	maxListLimit     int
	maxRequestBytes  int64
	requestTimeout   time.Duration
	headers          map[string]string
}

func NewWorkspaceExecutor(baseURL string, opts Options) *WorkspaceExecutor {
	httpClient := opts.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{}
	}
	defaultLimit := opts.DefaultListLimit
	if defaultLimit <= 0 {
		defaultLimit = defaultListLimit
	}
	maxLimit := opts.MaxListLimit
	if maxLimit <= 0 {
		maxLimit = defaultMaxListLimit
	}
	maxRequestBytes := opts.MaxRequestBytes
	if maxRequestBytes <= 0 {
		maxRequestBytes = defaultMaxRequestBody
	}
	timeout := opts.RequestTimeout
	if timeout <= 0 {
		timeout = defaultRequestTimeout
	}
	return &WorkspaceExecutor{
		client:           contracts.New(baseURL, httpClient),
		auth:             opts.Auth,
		defaultListLimit: defaultLimit,
		maxListLimit:     maxLimit,
		maxRequestBytes:  maxRequestBytes,
		requestTimeout:   timeout,
		headers:          opts.AdditionalHeaders,
	}
}

func (e *WorkspaceExecutor) CallTool(ctx context.Context, req protocol.ToolCallRequest) (protocol.ToolCallResult, error) {
	if e == nil || e.client == nil {
		return protocol.ToolCallResult{}, toolError("workspace_error", "workspace executor is not configured", protocol.ErrInternal)
	}
	ctx, cancel := context.WithTimeout(ctx, e.requestTimeout)
	defer cancel()

	arguments, err := validateArguments(req.Tool, req.Arguments, e.maxListLimit)
	if err != nil {
		return protocol.ToolCallResult{}, toolError("invalid_arguments", err.Error(), protocol.ErrInvalidParams)
	}

	query, warnings, err := e.queryParams(req.Tool, arguments.Query)
	if err != nil {
		return protocol.ToolCallResult{}, toolError("invalid_arguments", err.Error(), protocol.ErrInvalidParams)
	}
	body := arguments.Body
	headers := e.requestHeaders(arguments.IdempotencyKey)
	body, err = withIdempotencyKey(body, arguments.IdempotencyKey)
	if err != nil {
		return protocol.ToolCallResult{}, toolError("invalid_arguments", err.Error(), protocol.ErrInvalidParams)
	}
	if err := e.validateRequestBodySize(body); err != nil {
		return protocol.ToolCallResult{}, toolError("invalid_arguments", err.Error(), protocol.ErrInvalidParams)
	}

	resp, responseBody, err := e.client.Invoke(ctx, req.Tool.Metadata.CommandID, arguments.Path, contracts.RequestOptions{
		Query:   query,
		Headers: headers,
		Body:    body,
	})
	if err != nil && resp == nil {
		return protocol.ToolCallResult{}, toolError("workspace_error", "workspace request failed", protocol.ErrInternal)
	}
	if resp != nil && resp.StatusCode >= http.StatusBadRequest {
		return protocol.ToolCallResult{}, errorFromResponse(resp.StatusCode, responseBody)
	}
	if err != nil {
		return protocol.ToolCallResult{}, toolError("workspace_error", "workspace request failed", protocol.ErrInternal)
	}

	parsed, err := decodeResponseBody(responseBody)
	if err != nil {
		return protocol.ToolCallResult{}, toolError("workspace_error", "workspace returned invalid JSON", protocol.ErrInternal)
	}
	result := redaction.Value(parsed)
	return protocol.ToolCallResult{
		CommandID:  req.Tool.Metadata.CommandID,
		Status:     "ok",
		Result:     result,
		Pagination: paginationFrom(result),
		Warnings:   warnings,
	}, nil
}

type validatedArguments struct {
	Path           map[string]string
	Query          map[string]any
	Body           any
	IdempotencyKey string
}

func validateArguments(tool catalog.Tool, args map[string]any, maxListLimit int) (validatedArguments, error) {
	if args == nil {
		args = map[string]any{}
	}
	out := validatedArguments{Path: map[string]string{}, Query: map[string]any{}}
	if err := validatePath(tool, args, &out); err != nil {
		return out, err
	}
	if err := validateQuery(tool, args, maxListLimit, &out); err != nil {
		return out, err
	}
	if err := validateBody(tool, args, &out); err != nil {
		return out, err
	}
	if raw, ok := args["idempotency_key"]; ok {
		key, ok := raw.(string)
		if !ok {
			return out, errors.New("idempotency_key must be a string")
		}
		key = strings.TrimSpace(key)
		if key == "" {
			return out, errors.New("idempotency_key must not be empty when provided")
		}
		if isReadMethod(tool.Metadata.Method) {
			return out, errors.New("idempotency_key is only accepted for write operations")
		}
		out.IdempotencyKey = key
	}
	return out, nil
}

func validatePath(tool catalog.Tool, args map[string]any, out *validatedArguments) error {
	required := pathParamNames(tool.Metadata.Path)
	rawPath, hasPath := args["path"]
	if len(required) == 0 {
		if hasPath {
			pathMap, ok := rawPath.(map[string]any)
			if !ok {
				return errors.New("path must be an object")
			}
			if len(pathMap) > 0 {
				return errors.New("path arguments are not accepted for this command")
			}
		}
		return nil
	}
	if !hasPath {
		return errors.New("path is required")
	}
	pathMap, ok := rawPath.(map[string]any)
	if !ok {
		return errors.New("path must be an object")
	}
	requiredSet := map[string]bool{}
	for _, name := range required {
		requiredSet[name] = true
		raw, ok := pathMap[name]
		if !ok {
			return fmt.Errorf("missing path.%s", name)
		}
		value, ok := raw.(string)
		if !ok || strings.TrimSpace(value) == "" {
			return fmt.Errorf("path.%s must be a non-empty string", name)
		}
		out.Path[name] = value
	}
	for key := range pathMap {
		if !requiredSet[key] {
			return fmt.Errorf("unknown path parameter %q", key)
		}
	}
	return nil
}

func validateQuery(tool catalog.Tool, args map[string]any, maxListLimit int, out *validatedArguments) error {
	raw, ok := args["query"]
	if !ok {
		return nil
	}
	queryMap, ok := raw.(map[string]any)
	if !ok {
		return errors.New("query must be an object")
	}
	if len(queryMap) > 0 && !allowsQuery(tool) {
		return errors.New("query arguments are not accepted for this command")
	}
	for key, value := range queryMap {
		if strings.TrimSpace(key) == "" {
			return errors.New("query keys must not be empty")
		}
		if key == "limit" {
			limit, err := integerValue(value)
			if err != nil {
				return errors.New("query.limit must be an integer")
			}
			if limit < 1 || limit > maxListLimit {
				return fmt.Errorf("query.limit must be between 1 and %d", maxListLimit)
			}
			out.Query[key] = limit
			continue
		}
		switch v := value.(type) {
		case string:
			out.Query[key] = v
		case bool, json.Number, float64, int:
			out.Query[key] = v
		case []any:
			values := make([]string, 0, len(v))
			for _, item := range v {
				switch x := item.(type) {
				case string:
					values = append(values, x)
				case json.Number:
					values = append(values, x.String())
				case float64:
					values = append(values, strconv.FormatFloat(x, 'f', -1, 64))
				case bool:
					values = append(values, strconv.FormatBool(x))
				default:
					return fmt.Errorf("query.%s contains an unsupported value", key)
				}
			}
			out.Query[key] = values
		default:
			return fmt.Errorf("query.%s has unsupported value type", key)
		}
	}
	return nil
}

func validateBody(tool catalog.Tool, args map[string]any, out *validatedArguments) error {
	raw, ok := args["body"]
	if !ok {
		if required := requiredBodyFields(tool); len(required) > 0 {
			return errors.New("body is required")
		}
		return nil
	}
	bodyMap, ok := raw.(map[string]any)
	if !ok {
		return errors.New("body must be an object")
	}
	if !allowsBody(tool) {
		if len(bodyMap) == 0 {
			return nil
		}
		return errors.New("body arguments are not accepted for this command")
	}
	for _, field := range requiredBodyFields(tool) {
		if _, ok := bodyMap[field]; !ok {
			return fmt.Errorf("missing body.%s", field)
		}
	}
	for field, value := range bodyMap {
		if err := validateBodyFieldType(tool, field, value); err != nil {
			return err
		}
	}
	out.Body = copyMap(bodyMap)
	return nil
}

func (e *WorkspaceExecutor) queryParams(tool catalog.Tool, query map[string]any) (map[string][]string, []string, error) {
	out := map[string][]string{}
	for key, value := range query {
		switch v := value.(type) {
		case []string:
			out[key] = append(out[key], v...)
		default:
			out[key] = []string{fmt.Sprint(v)}
		}
	}
	if isListLike(tool) {
		if _, ok := out["limit"]; !ok {
			out["limit"] = []string{strconv.Itoa(e.defaultListLimit)}
			return out, []string{fmt.Sprintf("defaulted query.limit to %d", e.defaultListLimit)}, nil
		}
	}
	return out, nil, nil
}

func (e *WorkspaceExecutor) requestHeaders(idempotencyKey string) map[string]string {
	headers := map[string]string{}
	for key, value := range e.headers {
		headers[key] = value
	}
	for key, value := range e.auth.Headers {
		headers[key] = value
	}
	if strings.TrimSpace(e.auth.BearerToken) != "" {
		headers["Authorization"] = "Bearer " + strings.TrimSpace(e.auth.BearerToken)
	}
	if idempotencyKey != "" {
		headers["Idempotency-Key"] = idempotencyKey
	}
	return headers
}

func withIdempotencyKey(body any, key string) (any, error) {
	if key == "" {
		return body, nil
	}
	if body == nil {
		return map[string]any{"request_key": key}, nil
	}
	bodyMap, ok := body.(map[string]any)
	if !ok {
		return body, nil
	}
	out := copyMap(bodyMap)
	if existing, ok := out["request_key"]; ok {
		existingString, ok := existing.(string)
		if !ok || strings.TrimSpace(existingString) == "" {
			return nil, errors.New("body.request_key must be a non-empty string when idempotency_key is provided")
		}
		return out, nil
	}
	out["request_key"] = key
	return out, nil
}

func (e *WorkspaceExecutor) validateRequestBodySize(body any) error {
	if body == nil {
		return nil
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("body is not JSON-encodable: %w", err)
	}
	if int64(len(encoded)) > e.maxRequestBytes {
		return fmt.Errorf("body exceeds %d bytes", e.maxRequestBytes)
	}
	return nil
}

func decodeResponseBody(body []byte) (any, error) {
	body = bytes.TrimSpace(body)
	if len(body) == 0 {
		return map[string]any{}, nil
	}
	var parsed any
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if err := decoder.Decode(&parsed); err != nil {
		return nil, err
	}
	return parsed, nil
}

func errorFromResponse(status int, body []byte) error {
	code := "workspace_error"
	jsonRPCCode := protocol.ErrInternal
	switch status {
	case http.StatusBadRequest:
		code = "invalid_arguments"
		jsonRPCCode = protocol.ErrInvalidParams
	case http.StatusUnauthorized, http.StatusForbidden:
		code = "workspace_auth_failed"
		jsonRPCCode = protocol.ErrInvalidParams
	case http.StatusTooManyRequests:
		code = "rate_limited"
		jsonRPCCode = protocol.ErrInvalidParams
	}
	message := safeWorkspaceErrorMessage(status, body)
	return toolError(code, message, jsonRPCCode)
}

func safeWorkspaceErrorMessage(status int, body []byte) string {
	base := fmt.Sprintf("workspace returned HTTP %d", status)
	parsed, err := decodeResponseBody(body)
	if err != nil {
		safe := strings.TrimSpace(redaction.String(string(body)))
		if safe == "" || safe == redaction.RedactedEnv {
			return base
		}
		return base + ": " + safe
	}
	if m, ok := parsed.(map[string]any); ok {
		parts := make([]string, 0, 2)
		for _, key := range []string{"code", "error", "message"} {
			if value, ok := m[key]; ok {
				text := strings.TrimSpace(redaction.String(fmt.Sprint(value)))
				if text != "" {
					parts = append(parts, text)
				}
			}
			if len(parts) == 2 {
				break
			}
		}
		if len(parts) > 0 {
			return base + ": " + strings.Join(parts, ": ")
		}
	}
	return base
}

func paginationFrom(result any) map[string]any {
	m, ok := result.(map[string]any)
	if !ok {
		return nil
	}
	pagination := map[string]any{}
	for _, key := range []string{"next_cursor", "nextCursor", "cursor"} {
		if value, ok := m[key]; ok {
			pagination[key] = value
		}
	}
	if len(pagination) == 0 {
		return nil
	}
	return pagination
}

func toolError(code, message string, jsonRPCCode int) protocol.ToolError {
	return protocol.ToolError{Code: code, Message: redaction.String(message), JSONRPCCode: jsonRPCCode}
}

func allowsQuery(tool catalog.Tool) bool {
	if isReadMethod(tool.Metadata.Method) && isListLike(tool) {
		return true
	}
	props, _ := tool.InputSchema["properties"].(map[string]any)
	_, ok := props["query"]
	return ok
}

func allowsBody(tool catalog.Tool) bool {
	props, _ := tool.InputSchema["properties"].(map[string]any)
	_, ok := props["body"]
	return ok
}

func requiredBodyFields(tool catalog.Tool) []string {
	props, _ := tool.InputSchema["properties"].(map[string]any)
	body, _ := props["body"].(map[string]any)
	rawRequired, _ := body["required"].([]string)
	if len(rawRequired) > 0 {
		out := make([]string, len(rawRequired))
		copy(out, rawRequired)
		return out
	}
	rawAny, _ := body["required"].([]any)
	out := make([]string, 0, len(rawAny))
	for _, value := range rawAny {
		if s, ok := value.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func validateBodyFieldType(tool catalog.Tool, field string, value any) error {
	props, _ := tool.InputSchema["properties"].(map[string]any)
	body, _ := props["body"].(map[string]any)
	bodyProps, _ := body["properties"].(map[string]any)
	fieldSchema, _ := bodyProps[field].(map[string]any)
	if len(fieldSchema) == 0 {
		return nil
	}
	typ, _ := fieldSchema["type"].(string)
	switch typ {
	case "", "object":
		if typ == "object" {
			if _, ok := value.(map[string]any); !ok && value != nil {
				return fmt.Errorf("body.%s must be an object", field)
			}
		}
	case "string":
		if _, ok := value.(string); !ok {
			return fmt.Errorf("body.%s must be a string", field)
		}
	case "integer":
		if _, err := integerValue(value); err != nil {
			return fmt.Errorf("body.%s must be an integer", field)
		}
	case "number":
		switch value.(type) {
		case json.Number, float64, int, int64:
		default:
			return fmt.Errorf("body.%s must be a number", field)
		}
	case "boolean":
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("body.%s must be a boolean", field)
		}
	case "array":
		if _, ok := value.([]any); !ok {
			return fmt.Errorf("body.%s must be an array", field)
		}
	}
	return nil
}

func pathParamNames(path string) []string {
	var names []string
	for {
		start := strings.IndexByte(path, '{')
		if start < 0 {
			return names
		}
		end := strings.IndexByte(path[start:], '}')
		if end < 0 {
			return names
		}
		end += start
		names = append(names, path[start+1:end])
		path = path[end+1:]
	}
}

func isReadMethod(method string) bool {
	method = strings.ToUpper(strings.TrimSpace(method))
	return method == http.MethodGet || method == http.MethodHead
}

func isListLike(tool catalog.Tool) bool {
	commandID := strings.ToLower(tool.Metadata.CommandID)
	path := strings.ToLower(tool.Metadata.Path)
	if strings.Contains(commandID, ".list") || strings.HasSuffix(commandID, ".timeline") || strings.HasSuffix(commandID, ".history") {
		return true
	}
	if strings.Contains(path, "/stream/") || strings.Contains(path, "/logs") {
		return true
	}
	return false
}

func integerValue(value any) (int, error) {
	switch v := value.(type) {
	case int:
		return v, nil
	case int64:
		return int(v), nil
	case float64:
		if v != float64(int(v)) {
			return 0, errors.New("not an integer")
		}
		return int(v), nil
	case json.Number:
		n, err := v.Int64()
		return int(n), err
	case string:
		return strconv.Atoi(v)
	default:
		return 0, errors.New("not an integer")
	}
}

func copyMap(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}
