package compiler

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"time"

	"agent-nexus-tools-anx-http-record/internal/compiled"
	"agent-nexus-tools-anx-http-record/internal/recorder"
)

const schemaVersion = 1

type Options struct {
	SourceRecording string
}

func CompileJSONL(r io.Reader, opts Options) (compiled.Run, error) {
	entries, err := readEntries(r)
	if err != nil {
		return compiled.Run{}, err
	}
	return CompileEntries(entries, opts)
}

func CompileEntries(entries []recorder.Entry, opts Options) (compiled.Run, error) {
	state := newState()
	run := compiled.Run{
		SchemaVersion:   schemaVersion,
		GeneratedAt:     time.Now().UTC().Format(time.RFC3339Nano),
		SourceRecording: opts.SourceRecording,
	}

	slices.SortFunc(entries, func(left, right recorder.Entry) int {
		switch {
		case left.Seq < right.Seq:
			return -1
		case left.Seq > right.Seq:
			return 1
		default:
			return 0
		}
	})

	for _, entry := range entries {
		if !shouldCompileEntry(entry) {
			continue
		}
		exchange, err := compileEntry(entry, state)
		if err != nil {
			return compiled.Run{}, fmt.Errorf("compile seq %d %s %s: %w", entry.Seq, entry.Method, entry.Path, err)
		}
		run.Exchanges = append(run.Exchanges, exchange)
	}

	if len(run.Exchanges) == 0 {
		return compiled.Run{}, fmt.Errorf("no replayable exchanges found")
	}
	return run, nil
}

func shouldCompileEntry(entry recorder.Entry) bool {
	if entry.Error != "" || entry.StatusCode < 200 || entry.StatusCode >= 300 {
		return false
	}
	switch strings.ToUpper(strings.TrimSpace(entry.Method)) {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		path := strings.TrimSpace(entry.Path)
		if strings.HasPrefix(path, "/auth/") {
			return false
		}
		// Replay drops bearer/session headers; skip current-principal mutations that
		// cannot succeed without the original recording's credentials.
		if path == "/agents/me" || strings.HasPrefix(path, "/agents/me/") {
			return false
		}
		return true
	default:
		return false
	}
}

func compileEntry(entry recorder.Entry, state *compileState) (compiled.Exchange, error) {
	body, bodyKind, err := parseRequestBody(entry)
	if err != nil {
		return compiled.Exchange{}, err
	}
	path := state.replacePath(entry.Path)
	query := state.replaceQuery(entry.Query)
	if body != nil {
		body = state.replaceValue(body)
	}

	captures, err := inferCaptures(entry, path, state)
	if err != nil {
		return compiled.Exchange{}, err
	}
	for _, capture := range captures {
		value, ok, err := responseValue(entry, capture.ResponsePointer)
		if err != nil {
			return compiled.Exchange{}, err
		}
		if !ok || strings.TrimSpace(value) == "" {
			return compiled.Exchange{}, fmt.Errorf("response pointer %s missing", capture.ResponsePointer)
		}
		state.bind(capture.Alias, value)
	}

	headers := filterReplayHeaders(entry.RequestHeaders)
	if len(headers) == 0 {
		headers = nil
	}
	return compiled.Exchange{
		Seq:                entry.Seq,
		Method:             entry.Method,
		Path:               path,
		Query:              query,
		RequestHeaders:     headers,
		RequestBodyKind:    bodyKind,
		RequestBody:        body,
		ExpectedStatusCode: entry.StatusCode,
		Captures:           captures,
	}, nil
}

func parseRequestBody(entry recorder.Entry) (any, string, error) {
	if entry.RequestBody == "" {
		return nil, "", nil
	}
	if entry.TruncatedRequestBody {
		return nil, "", fmt.Errorf("truncated request body is not supported for compilation")
	}

	switch entry.RequestBodyEncoding {
	case "", "utf8":
		return entry.RequestBody, "text", nil
	case "json":
		var parsed any
		if err := json.Unmarshal([]byte(entry.RequestBody), &parsed); err != nil {
			return nil, "", fmt.Errorf("parse request json: %w", err)
		}
		return parsed, "json", nil
	default:
		return nil, "", fmt.Errorf("unsupported request body encoding %q", entry.RequestBodyEncoding)
	}
}

func responseValue(entry recorder.Entry, pointer string) (string, bool, error) {
	if entry.TruncatedResponseBody || entry.ResponseBody == "" {
		return "", false, nil
	}
	if entry.ResponseBodyEncoding != "json" {
		return "", false, fmt.Errorf("capture needs json response body, got %q", entry.ResponseBodyEncoding)
	}
	var parsed any
	if err := json.Unmarshal([]byte(entry.ResponseBody), &parsed); err != nil {
		return "", false, fmt.Errorf("parse response json: %w", err)
	}
	value, ok := jsonPointer(parsed, pointer)
	if !ok {
		return "", false, nil
	}
	switch typed := value.(type) {
	case string:
		return typed, true, nil
	default:
		encoded, err := json.Marshal(typed)
		if err != nil {
			return "", false, err
		}
		return string(encoded), true, nil
	}
}

func inferCaptures(entry recorder.Entry, compiledPath string, state *compileState) ([]compiled.Capture, error) {
	if entry.TruncatedResponseBody {
		return nil, fmt.Errorf("truncated response body is not supported for capture inference")
	}
	requestBody, _, err := parseRequestBody(entry)
	if err != nil {
		return nil, err
	}
	bodyMap, _ := requestBody.(map[string]any)
	method := strings.ToUpper(strings.TrimSpace(entry.Method))

	switch {
	case method == http.MethodPost && entry.Path == "/topics":
		sourceID := nestedString(bodyMap, "topic", "id")
		if sourceID == "" {
			return nil, nil
		}
		return []compiled.Capture{
			{Alias: topicAlias(sourceID), ResponsePointer: "/topic/id"},
			{Alias: threadAlias(sourceID), ResponsePointer: "/topic/thread_id"},
		}, nil
	case method == http.MethodPost && entry.Path == "/docs":
		sourceID := nestedString(bodyMap, "document", "id")
		if sourceID == "" {
			return nil, nil
		}
		alias := documentAlias(sourceID)
		index := state.nextRevisionIndex(alias)
		return []compiled.Capture{
			{Alias: alias, ResponsePointer: "/document/id"},
			{Alias: revisionAlias(alias, index), ResponsePointer: "/revision/revision_id"},
		}, nil
	case method == http.MethodPost && strings.HasSuffix(entry.Path, "/revisions"):
		docAlias := placeholderAliasFromPath(compiledPath)
		if docAlias == "" {
			return nil, fmt.Errorf("could not infer document alias from revisions path %q", compiledPath)
		}
		index := state.nextRevisionIndex(docAlias)
		return []compiled.Capture{
			{Alias: revisionAlias(docAlias, index), ResponsePointer: "/revision/revision_id"},
		}, nil
	case method == http.MethodPost && entry.Path == "/boards":
		sourceID := nestedString(bodyMap, "board", "id")
		if sourceID == "" {
			return nil, nil
		}
		return []compiled.Capture{
			{Alias: boardAlias(sourceID), ResponsePointer: "/board/id"},
		}, nil
	case method == http.MethodPost && strings.HasSuffix(entry.Path, "/cards"):
		boardAliasName := placeholderAliasFromPath(compiledPath)
		if boardAliasName == "" {
			boardAliasName = boardAlias(pathBoardID(entry.Path))
		}
		cardAliasName := state.nextCardAlias(boardAliasName)
		return []compiled.Capture{
			{Alias: cardAliasName, ResponsePointer: "/card/id"},
		}, nil
	default:
		return nil, nil
	}
}

func filterReplayHeaders(headers map[string][]string) map[string][]string {
	if len(headers) == 0 {
		return nil
	}
	out := map[string][]string{}
	for _, key := range []string{"Content-Type", "Accept"} {
		values := headers[key]
		if len(values) == 0 {
			continue
		}
		copied := make([]string, len(values))
		copy(copied, values)
		out[key] = copied
	}
	return out
}

func readEntries(r io.Reader) ([]recorder.Entry, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	var entries []recorder.Entry
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var entry recorder.Entry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			return nil, fmt.Errorf("decode jsonl entry: %w", err)
		}
		entries = append(entries, entry)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return entries, nil
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
		switch typed := current.(type) {
		case map[string]any:
			next, ok := typed[part]
			if !ok {
				return nil, false
			}
			current = next
		case []any:
			return nil, false
		default:
			return nil, false
		}
	}
	return current, true
}

func nestedString(root map[string]any, path ...string) string {
	current := any(root)
	for _, part := range path {
		obj, ok := current.(map[string]any)
		if !ok {
			return ""
		}
		next, ok := obj[part]
		if !ok {
			return ""
		}
		current = next
	}
	text, _ := current.(string)
	return strings.TrimSpace(text)
}

func pathBoardID(path string) string {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) >= 2 && parts[0] == "boards" {
		return parts[1]
	}
	return ""
}

var placeholderPattern = regexp.MustCompile(`\{\{([A-Za-z0-9_]+)\}\}`)

func placeholderAliasFromPath(path string) string {
	matches := placeholderPattern.FindStringSubmatch(path)
	if len(matches) != 2 {
		return ""
	}
	return matches[1]
}

type compileState struct {
	bindings         map[string]string
	reverse          map[string]string
	revisionCount    map[string]int
	cardCountByBoard map[string]int
}

func newState() *compileState {
	return &compileState{
		bindings:         map[string]string{},
		reverse:          map[string]string{},
		revisionCount:    map[string]int{},
		cardCountByBoard: map[string]int{},
	}
}

func (s *compileState) bind(alias string, actual string) {
	if alias == "" || actual == "" {
		return
	}
	if current := s.bindings[alias]; current != "" && current != actual {
		panic(fmt.Sprintf("compiler alias %s rebound from %s to %s", alias, current, actual))
	}
	s.bindings[alias] = actual
	s.reverse[actual] = alias
}

func (s *compileState) nextRevisionIndex(docAlias string) int {
	current := s.revisionCount[docAlias]
	s.revisionCount[docAlias] = current + 1
	return current
}

func (s *compileState) nextCardAlias(boardAlias string) string {
	index := s.cardCountByBoard[boardAlias]
	s.cardCountByBoard[boardAlias] = index + 1
	return fmt.Sprintf("card_%s_%03d", sanitizeAliasPart(boardAlias), index+1)
}

func (s *compileState) replacePath(path string) string {
	parts := strings.Split(path, "/")
	for i, part := range parts {
		if part == "" {
			continue
		}
		if alias := s.reverse[part]; alias != "" {
			parts[i] = "{{" + alias + "}}"
		}
	}
	return strings.Join(parts, "/")
}

func (s *compileState) replaceQuery(query string) string {
	if query == "" {
		return ""
	}
	for actual, alias := range s.reverse {
		query = strings.ReplaceAll(query, actual, "{{"+alias+"}}")
	}
	return query
}

func (s *compileState) replaceValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, child := range typed {
			out[key] = s.replaceValue(child)
		}
		return out
	case []any:
		out := make([]any, len(typed))
		for i, child := range typed {
			out[i] = s.replaceValue(child)
		}
		return out
	case string:
		return s.replaceString(typed)
	default:
		return value
	}
}

func (s *compileState) replaceString(raw string) string {
	if alias := s.reverse[raw]; alias != "" {
		return "{{" + alias + "}}"
	}
	if prefix, value, ok := splitTypedRef(raw); ok {
		if alias := s.reverse[value]; alias != "" {
			return prefix + ":{{" + alias + "}}"
		}
	}
	return raw
}

func splitTypedRef(raw string) (string, string, bool) {
	separator := strings.Index(raw, ":")
	if separator <= 0 {
		return "", "", false
	}
	prefix := raw[:separator]
	switch prefix {
	case "thread", "topic", "document", "board", "card":
		return prefix, raw[separator+1:], true
	default:
		return "", "", false
	}
}

func topicAlias(sourceID string) string {
	return "topic_" + sanitizeAliasPart(sourceID)
}

func threadAlias(sourceID string) string {
	return "thread_" + sanitizeAliasPart(sourceID)
}

func documentAlias(sourceID string) string {
	return "document_" + sanitizeAliasPart(sourceID)
}

func boardAlias(sourceID string) string {
	return "board_" + sanitizeAliasPart(sourceID)
}

func revisionAlias(docAlias string, index int) string {
	return fmt.Sprintf("revision_%s_%03d", sanitizeAliasPart(docAlias), index)
}

var aliasSanitizer = regexp.MustCompile(`[^A-Za-z0-9_]+`)

func sanitizeAliasPart(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "value"
	}
	safe := aliasSanitizer.ReplaceAllString(raw, "_")
	safe = strings.Trim(safe, "_")
	if safe == "" {
		return "value"
	}
	return safe
}
