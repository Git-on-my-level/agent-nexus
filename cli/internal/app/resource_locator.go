package app

import (
	"context"
	"fmt"
	"net/url"
	"path"
	"strings"

	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/errnorm"
)

type resourceLocator struct {
	Kind    string
	ID      string
	BoardID string
	EventID string
}

func parseRawIDArg(args []string, idFlag string, idLabel string) (string, error) {
	leadingID, args := popLeadingPositional(args)
	fs := newSilentFlagSet(idLabel)
	var idArgFlag trackedString
	fs.Var(&idArgFlag, idFlag, idLabel)
	if err := fs.Parse(args); err != nil {
		return "", errnorm.Usage("invalid_flags", err.Error())
	}
	positionals := fs.Args()
	id := firstNonEmpty(strings.TrimSpace(idArgFlag.value), leadingID)
	if id == "" && len(positionals) > 0 {
		id = strings.TrimSpace(positionals[0])
		positionals = positionals[1:]
	}
	if len(positionals) > 0 {
		return "", errnorm.Usage("invalid_args", "too many positional arguments")
	}
	return id, nil
}

func parseResourceIDArg(args []string, idFlag string, idLabel string, expectedKind string) (string, error) {
	raw, err := parseRawIDArg(args, idFlag, idLabel)
	if err != nil {
		return "", err
	}
	id, err := normalizeResourceIDInput(raw, expectedKind, idLabel)
	if err != nil {
		return "", err
	}
	if err := validateID(id, idLabel); err != nil {
		return "", err
	}
	return id, nil
}

func normalizeResourceIDInput(raw string, expectedKind string, idLabel string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return raw, nil
	}
	if loc, ok, err := parseResourceLocator(raw); err != nil {
		return "", err
	} else if ok {
		if !resourceKindMatches(loc.Kind, expectedKind) {
			return "", errnorm.Usage("invalid_request", fmt.Sprintf("%s URL points to %s, not %s", idLabel, displayResourceKind(loc.Kind), displayResourceKind(expectedKind)))
		}
		return loc.ID, nil
	}
	if kind, value, _, err := parseTypedRef(raw); err == nil {
		if !resourceKindMatches(kind, expectedKind) {
			return "", errnorm.Usage("invalid_request", fmt.Sprintf("%s typed ref points to %s, not %s", idLabel, displayResourceKind(kind), displayResourceKind(expectedKind)))
		}
		return canonicalResourceKind(kind) + ":" + value, nil
	}
	return raw, nil
}

// resourceLocatorScanStart returns the index of the first path segment after the
// workspace prefix (/o/<org>/w/<workspace> or /ws/<org>/<workspace>), matching
// webWorkspaceBaseURL. When no prefix is found, 0 preserves legacy URL parsing.
func resourceLocatorScanStart(segments []string) int {
	if len(segments) >= 3 && segments[0] == "ws" {
		return 3
	}
	for idx := 0; idx+3 < len(segments); idx++ {
		if segments[idx] == "o" && segments[idx+2] == "w" {
			return idx + 4
		}
	}
	return 0
}

func parseResourceLocator(raw string) (resourceLocator, bool, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || !strings.Contains(raw, "://") {
		return resourceLocator{}, false, nil
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return resourceLocator{}, false, errnorm.Usage("invalid_request", fmt.Sprintf("invalid ANX URL %q", raw))
	}
	segments := normalizedPathSegments(parsed.Path)
	scanFrom := resourceLocatorScanStart(segments)
	for idx := scanFrom; idx < len(segments); idx++ {
		segment := segments[idx]
		switch segment {
		case "boards":
			if idx+1 >= len(segments) {
				return resourceLocator{}, false, errnorm.Usage("invalid_request", fmt.Sprintf("ANX board URL %q is missing a board id", raw))
			}
			boardID := strings.TrimSpace(segments[idx+1])
			if cardID := strings.TrimSpace(parsed.Query().Get("card")); cardID != "" {
				return resourceLocator{Kind: "card", ID: cardID, BoardID: boardID}, true, nil
			}
			return resourceLocator{Kind: "board", ID: boardID}, true, nil
		case "topics":
			if idx+1 >= len(segments) {
				return resourceLocator{}, false, errnorm.Usage("invalid_request", fmt.Sprintf("ANX topic URL %q is missing a topic id", raw))
			}
			return resourceLocator{Kind: "topic", ID: strings.TrimSpace(segments[idx+1]), EventID: eventIDFromFragment(parsed.Fragment)}, true, nil
		case "docs":
			if idx+1 >= len(segments) {
				return resourceLocator{}, false, errnorm.Usage("invalid_request", fmt.Sprintf("ANX doc URL %q is missing a document id", raw))
			}
			return resourceLocator{Kind: "document", ID: strings.TrimSpace(segments[idx+1]), EventID: eventIDFromFragment(parsed.Fragment)}, true, nil
		case "artifacts":
			if idx+1 >= len(segments) {
				return resourceLocator{}, false, errnorm.Usage("invalid_request", fmt.Sprintf("ANX artifact URL %q is missing an artifact id", raw))
			}
			return resourceLocator{Kind: "artifact", ID: strings.TrimSpace(segments[idx+1])}, true, nil
		case "threads":
			if idx+1 >= len(segments) {
				return resourceLocator{}, false, errnorm.Usage("invalid_request", fmt.Sprintf("ANX thread URL %q is missing a thread id", raw))
			}
			return resourceLocator{Kind: "thread", ID: strings.TrimSpace(segments[idx+1])}, true, nil
		}
	}
	return resourceLocator{}, false, nil
}

func typedRefLocator(raw string) (resourceLocator, bool, error) {
	kind, value, _, err := parseTypedRef(raw)
	if err != nil {
		return resourceLocator{}, false, nil
	}
	switch canonicalResourceKind(kind) {
	case "board", "card", "topic", "document", "artifact", "thread":
		return resourceLocator{Kind: canonicalResourceKind(kind), ID: value}, true, nil
	default:
		return resourceLocator{}, false, errnorm.Usage("invalid_request", fmt.Sprintf("unsupported resource ref kind %q for `anx read`", kind))
	}
}

func normalizedPathSegments(rawPath string) []string {
	parts := strings.Split(strings.Trim(rawPath, "/"), "/")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func eventIDFromFragment(fragment string) string {
	fragment = strings.TrimSpace(fragment)
	return strings.TrimPrefix(fragment, "event-")
}

func resourceKindMatches(actual string, expected string) bool {
	return canonicalResourceKind(actual) == canonicalResourceKind(expected)
}

func canonicalResourceKind(kind string) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "doc", "docs", "document", "documents":
		return "document"
	case "board", "boards":
		return "board"
	case "card", "cards":
		return "card"
	case "topic", "topics":
		return "topic"
	case "artifact", "artifacts":
		return "artifact"
	case "thread", "threads":
		return "thread"
	default:
		return strings.ToLower(strings.TrimSpace(kind))
	}
}

func displayResourceKind(kind string) string {
	switch canonicalResourceKind(kind) {
	case "document":
		return "document"
	default:
		return canonicalResourceKind(kind)
	}
}

func (a *App) runReadCommand(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, error) {
	if len(args) == 0 || isHelpToken(args[0]) {
		return &commandResult{Text: readCommandHelpText(), Data: map[string]any{"help_text": readCommandHelpText()}}, nil
	}
	if len(args) > 1 {
		return nil, errnorm.Usage("invalid_args", "unexpected positional arguments for `anx read`; pass one ANX URL or typed ref")
	}
	raw := strings.TrimSpace(args[0])
	loc, ok, err := parseResourceLocator(raw)
	if err != nil {
		return nil, err
	}
	if !ok {
		loc, ok, err = typedRefLocator(raw)
		if err != nil {
			return nil, err
		}
	}
	if !ok {
		return nil, errnorm.Usage("invalid_request", "`anx read` requires an ANX URL or typed ref such as card:implement-login, document:runbook, topic:launch, board:launch, artifact:launch-notes, or thread:launch-discussion")
	}
	switch canonicalResourceKind(loc.Kind) {
	case "card":
		result, _, err := a.runCardsCommand(ctx, []string{"get", loc.ID}, cfg)
		return result, err
	case "document":
		return a.runDocsContentCommand(ctx, []string{loc.ID}, cfg)
	case "topic":
		result, _, err := a.runTopicsCommand(ctx, []string{"get", loc.ID}, cfg)
		return result, err
	case "board":
		result, _, err := a.runBoardsCommand(ctx, []string{"get", loc.ID}, cfg)
		return result, err
	case "artifact":
		return a.runArtifactsInspectCommand(ctx, []string{loc.ID}, cfg)
	case "thread":
		result, _, err := a.runThreadsCommand(ctx, []string{"inspect", loc.ID}, cfg)
		return result, err
	default:
		return nil, errnorm.Usage("invalid_request", fmt.Sprintf("unsupported resource kind %q for `anx read`", loc.Kind))
	}
}

func (a *App) runURLCommand(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, error) {
	if len(args) == 0 || isHelpToken(args[0]) {
		return &commandResult{Text: urlCommandHelpText(), Data: map[string]any{"help_text": urlCommandHelpText()}}, nil
	}
	if len(args) != 2 {
		return nil, errnorm.Usage("invalid_args", "`anx url` expects `<type> <id-or-url-or-ref>`")
	}
	kind := canonicalResourceKind(args[0])
	if kind == "" {
		return nil, errnorm.Usage("invalid_request", "resource type is required")
	}
	id, err := normalizeResourceIDInput(args[1], kind, kind+" id")
	if err != nil {
		return nil, err
	}
	if err := validateID(id, kind+" id"); err != nil {
		return nil, err
	}
	loc := resourceLocator{Kind: kind, ID: id}
	loc, err = a.resolveLocatorForURL(ctx, cfg, loc)
	if err != nil {
		return nil, err
	}
	resourceURL, err := resourceURL(cfg, loc)
	if err != nil {
		return nil, err
	}
	return &commandResult{
		Text: resourceURL,
		Data: map[string]any{
			"url":  resourceURL,
			"kind": canonicalResourceKind(loc.Kind),
			"id":   loc.ID,
		},
	}, nil
}

func (a *App) resolveLocatorForURL(ctx context.Context, cfg config.Resolved, loc resourceLocator) (resourceLocator, error) {
	switch canonicalResourceKind(loc.Kind) {
	case "board":
		return loc, nil
	case "topic":
		return loc, nil
	case "document":
		return loc, nil
	case "artifact":
		return loc, nil
	case "card":
		cardID := loc.ID
		if strings.TrimSpace(loc.BoardID) == "" {
			result, err := a.invokeTypedJSON(ctx, cfg, "cards get", "cards.get", map[string]string{"card_id": cardID}, nil, nil)
			if err != nil {
				return loc, err
			}
			card := extractNestedMap(commandResultBody(result), "card")
			if id := publicLocatorID(card, "card"); id != "" {
				loc.ID = id
			}
			loc.BoardID = firstNonEmpty(refID(anyString(card["board_ref"])), strings.TrimSpace(anyString(card["board_handle"])))
			if publicBoardID, err := a.resolvePublicBoardLocatorID(ctx, cfg, loc.BoardID); err == nil && publicBoardID != "" {
				loc.BoardID = publicBoardID
			}
		}
	}
	return loc, nil
}

func (a *App) resolvePublicBoardLocatorID(ctx context.Context, cfg config.Resolved, boardID string) (string, error) {
	boardID = strings.TrimSpace(boardID)
	if boardID == "" {
		return "", nil
	}
	result, err := a.invokeTypedJSONWithIDResolution(ctx, cfg, "boards get", "boards.get", "board_id", boardID, boardIDLookupSpec, nil, nil)
	if err != nil {
		return "", err
	}
	board := extractNestedMap(commandResultBody(result), "board")
	return publicLocatorID(board, "board"), nil
}

func addResourceURLToResult(cfg config.Resolved, commandID string, result *commandResult) *commandResult {
	if result == nil {
		return result
	}
	body := commandResultBody(result)
	loc, ok := locatorFromCommandBody(commandID, body)
	if !ok {
		return result
	}
	resourceURL, err := resourceURL(cfg, loc)
	if err != nil || strings.TrimSpace(resourceURL) == "" {
		return result
	}
	data := asMap(result.Data)
	if data != nil {
		data["url"] = resourceURL
		if body != nil {
			body["url"] = resourceURL
			data["body"] = body
		}
		result.Data = data
	}
	if strings.TrimSpace(result.Text) != "" && !cfg.Verbose && !cfg.Headers {
		result.Text = strings.TrimRight(result.Text, "\n") + "\nURL: " + resourceURL
	}
	return result
}

func locatorFromCommandBody(commandID string, body map[string]any) (resourceLocator, bool) {
	switch strings.TrimSpace(commandID) {
	case "boards.create":
		board := extractNestedMap(body, "board")
		id := publicLocatorID(board, "board")
		return resourceLocator{Kind: "board", ID: id}, id != ""
	case "topics.create":
		topic := extractNestedMap(body, "topic")
		id := publicLocatorID(topic, "topic")
		return resourceLocator{Kind: "topic", ID: id}, id != ""
	case "docs.create":
		document := extractNestedMap(body, "document")
		id := publicLocatorID(document, "document")
		return resourceLocator{Kind: "document", ID: id}, id != ""
	case "artifacts.create":
		artifact := extractNestedMap(body, "artifact")
		id := publicLocatorID(artifact, "artifact")
		return resourceLocator{Kind: "artifact", ID: id}, id != ""
	case "cards.create":
		card := extractNestedMap(body, "card")
		id := publicLocatorID(card, "card")
		boardID := firstNonEmpty(refID(anyString(card["board_ref"])), strings.TrimSpace(anyString(card["board_handle"])))
		if boardID == "" {
			board := extractNestedMap(body, "board")
			boardID = publicLocatorID(board, "board")
		}
		return resourceLocator{Kind: "card", ID: id, BoardID: boardID}, id != "" && boardID != ""
	default:
		return resourceLocator{}, false
	}
}

func publicLocatorID(obj map[string]any, kind string) string {
	if obj == nil {
		return ""
	}
	kind = canonicalResourceKind(kind)
	if kind == "" {
		return strings.TrimSpace(anyString(obj["id"]))
	}
	if ref := refID(anyString(obj["ref"])); ref != "" {
		return ref
	}
	if handle := strings.TrimSpace(anyString(obj["handle"])); handle != "" {
		return handle
	}
	refKey := kind + "_ref"
	if ref := refID(anyString(obj[refKey])); ref != "" {
		return ref
	}
	handleKey := kind + "_handle"
	if handle := strings.TrimSpace(anyString(obj[handleKey])); handle != "" {
		return handle
	}
	return strings.TrimSpace(anyString(obj["id"]))
}

func resourceURL(cfg config.Resolved, loc resourceLocator) (string, error) {
	base, err := webWorkspaceBaseURL(cfg.BaseURL)
	if err != nil {
		return "", err
	}
	switch canonicalResourceKind(loc.Kind) {
	case "board":
		return joinURLPath(base, "boards", loc.ID), nil
	case "card":
		if strings.TrimSpace(loc.BoardID) == "" {
			return "", errnorm.Usage("invalid_request", "card URL requires board context")
		}
		boardURL := joinURLPath(base, "boards", loc.BoardID)
		sep := "?"
		if strings.Contains(boardURL, "?") {
			sep = "&"
		}
		return boardURL + sep + "card=" + url.QueryEscape(loc.ID), nil
	case "topic":
		out := joinURLPath(base, "topics", loc.ID)
		if strings.TrimSpace(loc.EventID) != "" {
			out += "#event-" + url.PathEscape(loc.EventID)
		}
		return out, nil
	case "document":
		return joinURLPath(base, "docs", loc.ID), nil
	case "artifact":
		return joinURLPath(base, "artifacts", loc.ID), nil
	case "thread":
		return joinURLPath(base, "threads", loc.ID), nil
	default:
		return "", errnorm.Usage("invalid_request", fmt.Sprintf("unsupported resource kind %q", loc.Kind))
	}
}

func webWorkspaceBaseURL(rawBase string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(rawBase))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", errnorm.Usage("invalid_request", fmt.Sprintf("base URL %q is not a valid absolute URL", rawBase))
	}
	segments := normalizedPathSegments(parsed.Path)
	if len(segments) >= 3 && segments[0] == "ws" {
		parsed.Path = path.Join("/", "o", segments[1], "w", segments[2])
		parsed.RawQuery = ""
		parsed.Fragment = ""
		return strings.TrimRight(parsed.String(), "/"), nil
	}
	for idx := 0; idx+3 < len(segments); idx++ {
		if segments[idx] == "o" && segments[idx+2] == "w" {
			parsed.Path = path.Join(append([]string{"/"}, segments[:idx+4]...)...)
			parsed.RawQuery = ""
			parsed.Fragment = ""
			return strings.TrimRight(parsed.String(), "/"), nil
		}
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return strings.TrimRight(parsed.String(), "/"), nil
}

func joinURLPath(base string, segments ...string) string {
	parsed, err := url.Parse(base)
	if err != nil {
		return strings.TrimRight(base, "/")
	}
	all := normalizedPathSegments(parsed.Path)
	for _, segment := range segments {
		if segment = strings.TrimSpace(segment); segment != "" {
			all = append(all, segment)
		}
	}
	escaped := make([]string, 0, len(all))
	for _, segment := range all {
		escaped = append(escaped, url.PathEscape(segment))
	}
	parsed.Path = "/" + strings.Join(escaped, "/")
	return parsed.String()
}

func refID(ref string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return ""
	}
	if _, value, _, err := parseTypedRef(ref); err == nil {
		return value
	}
	return ref
}

func readCommandHelpText() string {
	return strings.TrimSpace(`Local Help: read

- Kind: local helper
- Summary: Read an ANX resource from a URL or typed ref.
- Examples:
  - anx read https://anx.example/o/org/w/workspace/boards/<board-handle>?card=<card-handle>
  - anx read document:<document-handle>
  - anx read artifact:<artifact-handle>`)
}

func urlCommandHelpText() string {
	return strings.TrimSpace(`Local Help: url

- Kind: local helper
- Summary: Print a shareable ANX URL for a resource.
- Examples:
  - anx url card <card-ref-or-handle>
  - anx url board <board-ref-or-handle>
  - anx url document <doc-ref-or-handle>`)
}
