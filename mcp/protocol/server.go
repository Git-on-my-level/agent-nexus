package protocol

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"

	"github.com/Git-on-my-level/agent-nexus/mcp/catalog"
)

const (
	jsonRPCVersion = "2.0"

	ErrParse          = -32700
	ErrInvalidRequest = -32600
	ErrMethodNotFound = -32601
	ErrInvalidParams  = -32602
	ErrInternal       = -32603
)

type Executor interface {
	CallTool(ctx context.Context, req ToolCallRequest) (ToolCallResult, error)
}

type ToolCallRequest struct {
	Tool      catalog.Tool
	Arguments map[string]any
}

type ToolCallResult struct {
	CommandID  string         `json:"command_id"`
	Status     string         `json:"status"`
	Result     any            `json:"result,omitempty"`
	Pagination map[string]any `json:"pagination,omitempty"`
	Warnings   []string       `json:"warnings,omitempty"`
}

type Server struct {
	catalog  *catalog.Catalog
	executor Executor
	name     string
	version  string
}

type Options struct {
	Name    string
	Version string
}

func NewServer(cat *catalog.Catalog, executor Executor, opts Options) *Server {
	name := opts.Name
	if name == "" {
		name = "anx-mcp"
	}
	version := opts.Version
	if version == "" {
		version = "0.1.0"
	}
	return &Server{catalog: cat, executor: executor, name: name, version: version}
}

func (s *Server) Handle(ctx context.Context, input []byte) ([]byte, error) {
	var req request
	decoder := json.NewDecoder(bytes.NewReader(input))
	decoder.UseNumber()
	if err := decoder.Decode(&req); err != nil {
		return marshalResponse(response{JSONRPC: jsonRPCVersion, Error: rpcError(ErrParse, "parse_error", "invalid JSON-RPC request", nil)})
	}
	if req.JSONRPC != jsonRPCVersion || req.Method == "" {
		return marshalResponse(response{JSONRPC: jsonRPCVersion, ID: req.ID, Error: rpcError(ErrInvalidRequest, "invalid_request", "invalid JSON-RPC request", nil)})
	}
	if req.isNotification() {
		return nil, nil
	}

	switch req.Method {
	case "initialize":
		return s.handleInitialize(req)
	case "notifications/initialized":
		// JSON-RPC notifications do not have responses. ChatGPT sends this after
		// initialize; accepting it keeps the HTTP transport compatible while leaving
		// unknown request methods strict below.
		return nil, nil
	case "tools/list":
		return s.handleToolsList(req)
	case "tools/call":
		return s.handleToolsCall(ctx, req)
	default:
		return marshalResponse(response{JSONRPC: jsonRPCVersion, ID: req.ID, Error: rpcError(ErrMethodNotFound, "method_not_found", "unknown MCP method", map[string]any{"method": req.Method})})
	}
}

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id,omitempty"`
	HasID   bool            `json:"-"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

func (r *request) UnmarshalJSON(input []byte) error {
	type requestAlias request
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(input, &raw); err != nil {
		return err
	}
	var decoded requestAlias
	if err := json.Unmarshal(input, &decoded); err != nil {
		return err
	}
	*r = request(decoded)
	_, r.HasID = raw["id"]
	return nil
}

func (r request) isNotification() bool {
	return !r.HasID
}

type response struct {
	JSONRPC string     `json:"jsonrpc"`
	ID      any        `json:"id"`
	Result  any        `json:"result,omitempty"`
	Error   *rpcErrorT `json:"error,omitempty"`
}

type rpcErrorT struct {
	Code    int            `json:"code"`
	Message string         `json:"message"`
	Data    map[string]any `json:"data,omitempty"`
}

func (s *Server) handleInitialize(req request) ([]byte, error) {
	result := map[string]any{
		"protocolVersion": "2025-03-26",
		"capabilities": map[string]any{
			"tools": map[string]any{"listChanged": false},
		},
		"serverInfo": map[string]any{
			"name":    s.name,
			"version": s.version,
		},
	}
	return marshalResponse(response{JSONRPC: jsonRPCVersion, ID: req.ID, Result: result})
}

func (s *Server) handleToolsList(req request) ([]byte, error) {
	var params struct {
		Cursor string `json:"cursor"`
		Limit  int    `json:"limit"`
	}
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return marshalResponse(response{JSONRPC: jsonRPCVersion, ID: req.ID, Error: rpcError(ErrInvalidParams, "invalid_arguments", "invalid tools/list params", nil)})
		}
	}
	limit := params.Limit
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	start := 0
	if params.Cursor != "" {
		n, err := strconv.Atoi(params.Cursor)
		if err != nil || n < 0 {
			return marshalResponse(response{JSONRPC: jsonRPCVersion, ID: req.ID, Error: rpcError(ErrInvalidParams, "invalid_arguments", "invalid cursor", nil)})
		}
		start = n
	}

	tools := s.catalog.Tools()
	if start > len(tools) {
		start = len(tools)
	}
	end := start + limit
	if end > len(tools) {
		end = len(tools)
	}
	page := tools[start:end]
	descriptors := make([]map[string]any, 0, len(page))
	for _, tool := range page {
		desc := map[string]any{
			"name":        tool.Name,
			"description": tool.Description,
			"inputSchema": tool.InputSchema,
		}
		if len(tool.OutputSchema) > 0 {
			desc["outputSchema"] = tool.OutputSchema
		}
		descriptors = append(descriptors, desc)
	}

	result := map[string]any{"tools": descriptors}
	if end < len(tools) {
		result["nextCursor"] = strconv.Itoa(end)
	}
	return marshalResponse(response{JSONRPC: jsonRPCVersion, ID: req.ID, Result: result})
}

func (s *Server) handleToolsCall(ctx context.Context, req request) ([]byte, error) {
	if s.executor == nil {
		return marshalResponse(response{JSONRPC: jsonRPCVersion, ID: req.ID, Error: rpcError(ErrInternal, "workspace_error", "tool executor is not configured", nil)})
	}
	var params struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}
	if len(req.Params) == 0 {
		return marshalResponse(response{JSONRPC: jsonRPCVersion, ID: req.ID, Error: rpcError(ErrInvalidParams, "invalid_arguments", "missing tools/call params", nil)})
	}
	decoder := json.NewDecoder(bytes.NewReader(req.Params))
	decoder.UseNumber()
	if err := decoder.Decode(&params); err != nil {
		return marshalResponse(response{JSONRPC: jsonRPCVersion, ID: req.ID, Error: rpcError(ErrInvalidParams, "invalid_arguments", "invalid tools/call params", nil)})
	}

	tool, ok := s.catalog.Lookup(params.Name)
	if !ok {
		dataCode := "tool_not_found"
		message := "tool not found"
		if s.catalog.KnowsToolName(params.Name) {
			dataCode = "tool_not_allowed"
			message = "tool not allowed"
		}
		return marshalResponse(response{JSONRPC: jsonRPCVersion, ID: req.ID, Error: rpcError(ErrInvalidParams, dataCode, message, map[string]any{"tool": params.Name})})
	}
	if params.Arguments == nil {
		params.Arguments = map[string]any{}
	}
	if err := validateEnvelope(params.Arguments); err != nil {
		return marshalResponse(response{JSONRPC: jsonRPCVersion, ID: req.ID, Error: rpcError(ErrInvalidParams, "invalid_arguments", err.Error(), nil)})
	}

	result, err := s.executor.CallTool(ctx, ToolCallRequest{Tool: tool, Arguments: params.Arguments})
	if err != nil {
		code, dataCode := executorErrorCode(err)
		return marshalResponse(response{JSONRPC: jsonRPCVersion, ID: req.ID, Error: rpcError(code, dataCode, err.Error(), map[string]any{"tool": params.Name, "command_id": tool.Metadata.CommandID})})
	}
	if result.CommandID == "" {
		result.CommandID = tool.Metadata.CommandID
	}
	if result.Status == "" {
		result.Status = "ok"
	}

	text, err := json.Marshal(result)
	if err != nil {
		return marshalResponse(response{JSONRPC: jsonRPCVersion, ID: req.ID, Error: rpcError(ErrInternal, "workspace_error", "failed to encode tool result", nil)})
	}

	mcpResult := map[string]any{
		"content": []map[string]any{
			{"type": "text", "text": string(text)},
		},
		"structuredContent": result,
	}
	return marshalResponse(response{JSONRPC: jsonRPCVersion, ID: req.ID, Result: mcpResult})
}

func validateEnvelope(args map[string]any) error {
	allowed := map[string]bool{"path": true, "query": true, "body": true, "idempotency_key": true}
	for key := range args {
		if !allowed[key] {
			return fmt.Errorf("unknown top-level argument key %q", key)
		}
	}
	if v, ok := args["path"]; ok {
		if _, ok := v.(map[string]any); !ok {
			return errors.New("path must be an object")
		}
	}
	if v, ok := args["query"]; ok {
		if _, ok := v.(map[string]any); !ok {
			return errors.New("query must be an object")
		}
	}
	if v, ok := args["body"]; ok {
		if _, ok := v.(map[string]any); !ok {
			return errors.New("body must be an object")
		}
	}
	if v, ok := args["idempotency_key"]; ok {
		if _, ok := v.(string); !ok {
			return errors.New("idempotency_key must be a string")
		}
	}
	return nil
}

func rpcError(code int, dataCode string, message string, data map[string]any) *rpcErrorT {
	if data == nil {
		data = map[string]any{}
	}
	data["code"] = dataCode
	return &rpcErrorT{Code: code, Message: message, Data: data}
}

func executorErrorCode(err error) (int, string) {
	var e ToolError
	if errors.As(err, &e) {
		if e.JSONRPCCode != 0 {
			return e.JSONRPCCode, e.Code
		}
		return ErrInvalidParams, e.Code
	}
	return ErrInternal, "workspace_error"
}

type ToolError struct {
	Code        string
	Message     string
	JSONRPCCode int
}

func (e ToolError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return e.Code
}

func marshalResponse(resp response) ([]byte, error) {
	return json.Marshal(resp)
}

func SortedToolNames(cat *catalog.Catalog) []string {
	tools := cat.Tools()
	names := make([]string, 0, len(tools))
	for _, tool := range tools {
		names = append(names, tool.Name)
	}
	sort.Strings(names)
	return names
}
