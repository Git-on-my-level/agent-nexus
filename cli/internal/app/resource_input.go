// Shared input parsing for typed resource commands: id/ref validation,
// flag-to-JSON body building, and file/stdin body loading with decode error
// reporting.

package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"

	contractenums "agent-nexus-contracts-go-client/enums"

	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/errnorm"
)

var idPattern = regexp.MustCompile(`^[A-Za-z0-9._:@/-]+$`)

type jsonBodyInputOptions struct {
	allowDryRun      bool
	allowContentFile bool
	allowContentOnly bool
}

func (a *App) parseJSONBodyInput(args []string, commandName string) (any, error) {
	body, _, err := a.parseJSONBodyInputWithOptions(args, commandName, jsonBodyInputOptions{})
	return body, err
}

func normalizeTypedRefFlag(value string, defaultPrefix string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if strings.Contains(value, ":") {
		return value
	}
	return strings.TrimSpace(defaultPrefix) + ":" + value
}

func (a *App) parseJSONBodyInputWithOptions(args []string, commandName string, options jsonBodyInputOptions) (any, bool, error) {
	fs := newSilentFlagSet(commandName)
	var fromFileFlag, contentFileFlag trackedString
	var dryRunFlag trackedBool
	fs.Var(&fromFileFlag, "from-file", "Advanced JSON body from file path or stdin with -")
	if options.allowContentFile {
		fs.Var(&contentFileFlag, "body-file", "Load document/content field from file path or stdin with -")
	}
	if options.allowDryRun {
		fs.Var(&dryRunFlag, "dry-run", "Validate and render request without sending the mutation")
	}
	if err := fs.Parse(args); err != nil {
		return nil, false, errnorm.Usage("invalid_flags", err.Error())
	}
	if len(fs.Args()) > 0 {
		return nil, false, errnorm.Usage("invalid_args", fmt.Sprintf("unexpected positional arguments for `anx %s`", commandName))
	}
	fromFile := strings.TrimSpace(fromFileFlag.value)
	contentFile := strings.TrimSpace(contentFileFlag.value)
	if !options.allowContentFile {
		contentFile = ""
	}
	var payload []byte
	var err error
	if skipJSONStdinForContentFile(fromFile, contentFile) {
		payload = nil
	} else {
		payload, err = a.readBodyInput(fromFile)
	}
	if err != nil {
		return nil, false, err
	}
	if len(payload) == 0 {
		if options.allowContentOnly && contentFile != "" {
			body := map[string]any{"content_type": "text"}
			rawBody, err := a.applyContentFileOverride(body, contentFile, commandName)
			if err != nil {
				return nil, false, err
			}
			return rawBody, dryRunFlag.set && dryRunFlag.value, nil
		}
		return nil, false, errnorm.Usage("invalid_request", fmt.Sprintf("JSON body is required for `anx %s` (provide stdin or --from-file)", commandName))
	}
	body, err := decodeJSONPayload(payload)
	if err != nil {
		return nil, false, err
	}
	if options.allowContentFile {
		body, err = a.applyContentFileOverride(body, contentFile, commandName)
		if err != nil {
			return nil, false, err
		}
	}
	return body, dryRunFlag.set && dryRunFlag.value, nil
}

func (a *App) parseIDAndBodyInput(args []string, idFlag string, idLabel string, commandName string) (string, any, error) {
	id, body, _, err := a.parseIDAndBodyInputWithOptions(args, idFlag, idLabel, commandName, jsonBodyInputOptions{})
	return id, body, err
}

func (a *App) parseIDAndBodyInputWithOptions(args []string, idFlag string, idLabel string, commandName string, options jsonBodyInputOptions) (string, any, bool, error) {
	id, body, dryRun, _, err := a.parseIDAndBodyInputWithMetadata(args, idFlag, idLabel, commandName, options)
	return id, body, dryRun, err
}

func (a *App) parseIDAndBodyInputWithMetadata(args []string, idFlag string, idLabel string, commandName string, options jsonBodyInputOptions) (string, any, bool, bool, error) {
	// Board role edits support the documented `boards patch <board> --role ...` form.
	if commandName == "boards patch" && len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		args = append(append([]string{}, args[1:]...), args[0])
	}
	fs := newSilentFlagSet(commandName)
	var idArgFlag, fromFileFlag, contentFileFlag, roleFlag, ifUpdatedAtFlag trackedString
	var dryRunFlag trackedBool
	fs.Var(&idArgFlag, idFlag, idLabel)
	if commandName == "boards patch" {
		fs.Var(&roleFlag, "role", "Board role; empty clears")
		fs.Var(&ifUpdatedAtFlag, "if-updated-at", "Board updated_at concurrency token; use boards get when omitted")
	}
	fs.Var(&fromFileFlag, "from-file", "Advanced JSON body from file path or stdin with -")
	if options.allowContentFile {
		fs.Var(&contentFileFlag, "body-file", "Load document/content field from file path or stdin with -")
	}
	if options.allowDryRun {
		fs.Var(&dryRunFlag, "dry-run", "Validate and render request without sending the mutation")
	}
	if err := fs.Parse(args); err != nil {
		return "", nil, false, false, errnorm.Usage("invalid_flags", err.Error())
	}
	positionals := fs.Args()
	id := strings.TrimSpace(idArgFlag.value)
	if id == "" && len(positionals) > 0 {
		id = strings.TrimSpace(positionals[0])
		positionals = positionals[1:]
	}
	if err := validateID(id, idLabel); err != nil {
		return "", nil, false, false, err
	}
	if len(positionals) > 0 {
		return "", nil, false, false, errnorm.Usage("invalid_args", fmt.Sprintf("unexpected positional arguments for `anx %s`", commandName))
	}
	fromFile := strings.TrimSpace(fromFileFlag.value)
	contentFile := strings.TrimSpace(contentFileFlag.value)
	if !options.allowContentFile {
		contentFile = ""
	}
	if commandName == "boards patch" && ifUpdatedAtFlag.set && strings.TrimSpace(ifUpdatedAtFlag.value) == "" {
		return "", nil, false, false, errnorm.Usage("invalid_request", "--if-updated-at must not be empty")
	}
	var payload []byte
	var err error
	if roleFlag.set && fromFile == "" {
		payload = []byte(`{"patch":{}}`)
	} else if skipJSONStdinForContentFile(fromFile, contentFile) {
		payload = nil
	} else {
		payload, err = a.readBodyInput(fromFile)
	}
	if err != nil {
		return "", nil, false, false, err
	}
	if len(payload) == 0 {
		if options.allowContentOnly && contentFile != "" {
			body := map[string]any{"content_type": "text"}
			rawBody, err := a.applyContentFileOverride(body, contentFile, commandName)
			if err != nil {
				return "", nil, false, false, err
			}
			return id, rawBody, dryRunFlag.set && dryRunFlag.value, false, nil
		}
		return "", nil, false, false, errnorm.Usage("invalid_request", fmt.Sprintf("JSON body is required for `anx %s` (provide stdin or --from-file)", commandName))
	}
	body, err := decodeJSONPayload(payload)
	if err != nil {
		return "", nil, false, false, err
	}
	if roleFlag.set {
		object, ok := body.(map[string]any)
		if !ok {
			return "", nil, false, false, errnorm.Usage("invalid_request", "board patch body must be an object")
		}
		patch, ok := object["patch"].(map[string]any)
		if !ok {
			return "", nil, false, false, errnorm.Usage("invalid_request", "board patch body must include patch object")
		}
		patch["role"] = strings.TrimSpace(roleFlag.value)
	}
	if commandName == "boards patch" && ifUpdatedAtFlag.set {
		object, ok := body.(map[string]any)
		if !ok {
			return "", nil, false, false, errnorm.Usage("invalid_request", "board patch body must be an object")
		}
		object["if_updated_at"] = strings.TrimSpace(ifUpdatedAtFlag.value)
	}
	if options.allowContentFile {
		body, err = a.applyContentFileOverride(body, contentFile, commandName)
		if err != nil {
			return "", nil, false, false, err
		}
	}
	discoverUpdatedAt := commandName == "boards patch" && roleFlag.set && fromFile == "" && !ifUpdatedAtFlag.set
	return id, body, dryRunFlag.set && dryRunFlag.value, discoverUpdatedAt, nil
}

func parseIDArg(args []string, idFlag string, idLabel string) (string, error) {
	id, err := parseRawIDArg(args, idFlag, idLabel)
	if err != nil {
		return "", err
	}
	if err := validateID(id, idLabel); err != nil {
		return "", err
	}
	return id, nil
}

func validateID(id string, label string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return errnorm.Usage("invalid_request", fmt.Sprintf("%s is required", label))
	}
	if !idPattern.MatchString(id) {
		return errnorm.Usage("invalid_request", fmt.Sprintf("%s %q contains invalid characters", label, id))
	}
	return nil
}

func validateTypedRefShape(ref string) error {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return errnorm.Usage("invalid_request", "subject_ref is required")
	}
	idx := strings.Index(ref, ":")
	if idx <= 0 || idx >= len(ref)-1 {
		return errnorm.Usage("invalid_request", fmt.Sprintf("subject_ref %q must be a typed ref (prefix:id)", ref))
	}
	return nil
}

func (a *App) parseDerivedRebuildBodyInput(args []string, cfg config.Resolved) (any, error) {
	fs := newSilentFlagSet("derived rebuild")
	var fromFileFlag, actorIDFlag trackedString
	fs.Var(&fromFileFlag, "from-file", "Load JSON body from file path or stdin with -")
	fs.Var(&actorIDFlag, "actor-id", "Actor id")
	if err := fs.Parse(args); err != nil {
		return nil, errnorm.Usage("invalid_flags", err.Error())
	}
	if len(fs.Args()) > 0 {
		return nil, errnorm.Usage("invalid_args", "unexpected positional arguments for `anx derived rebuild`")
	}

	payload, err := a.readBodyInput(strings.TrimSpace(fromFileFlag.value))
	if err != nil {
		return nil, err
	}

	var body map[string]any
	if len(payload) == 0 {
		body = map[string]any{}
	} else {
		decoded, decodeErr := decodeJSONPayload(payload)
		if decodeErr != nil {
			return nil, decodeErr
		}
		parsed, ok := decoded.(map[string]any)
		if !ok {
			return nil, errnorm.Usage("invalid_request", "JSON body for `anx derived rebuild` must be an object")
		}
		body = parsed
	}

	actorID, err := resolveActorIDAlias(actorIDFlag.value, cfg)
	if err != nil {
		return nil, err
	}
	if actorID != "" {
		body["actor_id"] = actorID
	}
	if err := finalizeMutationActorID(body, cfg); err != nil {
		return nil, err
	}
	return body, nil
}

func (a *App) parseRespondBodyInput(args []string, cfg config.Resolved) (any, error) {
	fs := newSilentFlagSet("inbox respond")
	var fromFileFlag, inboxItemIDFlag, responseTextFlag, outcomeFlag, notifyModeFlag, actorIDFlag trackedString
	fs.Var(&fromFileFlag, "from-file", "Load JSON body from file path or stdin with -")
	fs.Var(&inboxItemIDFlag, "inbox-item-id", "Inbox item id")
	fs.Var(&responseTextFlag, "response-text", "Freeform response text")
	fs.Var(&outcomeFlag, "outcome", "Response outcome: "+strings.Join(contractenums.HumanAttentionResponseOutcomeValues, ", "))
	fs.Var(&notifyModeFlag, "notify-mode", "Notification mode: original, target, none")
	fs.Var(&actorIDFlag, "actor-id", "Actor id")
	if err := fs.Parse(args); err != nil {
		return nil, errnorm.Usage("invalid_flags", err.Error())
	}
	positionals := fs.Args()

	fromFile := strings.TrimSpace(fromFileFlag.value)
	var body map[string]any
	if fromFile != "" {
		payload, err := a.readBodyInput(fromFile)
		if err != nil {
			return nil, err
		}
		decoded, err := decodeJSONPayload(payload)
		if err != nil {
			return nil, err
		}
		decodedMap, ok := decoded.(map[string]any)
		if !ok {
			return nil, errnorm.Usage("invalid_request", "JSON body for `anx inbox respond` must be an object")
		}
		body = decodedMap
	} else {
		body = map[string]any{}
	}

	if inboxItemID := strings.TrimSpace(inboxItemIDFlag.value); inboxItemID != "" {
		body["inbox_item_id"] = inboxItemID
	}
	if strings.TrimSpace(anyString(body["inbox_item_id"])) == "" && len(positionals) > 0 {
		body["inbox_item_id"] = strings.TrimSpace(positionals[0])
		positionals = positionals[1:]
	}
	if responseText := strings.TrimSpace(responseTextFlag.value); responseText != "" {
		body["response_text"] = responseText
	}
	if outcomeFlag.set {
		body["outcome"] = strings.TrimSpace(outcomeFlag.value)
	}
	if notifyMode := strings.TrimSpace(notifyModeFlag.value); notifyMode != "" {
		body["notify_mode"] = notifyMode
	}
	if len(positionals) > 0 {
		return nil, errnorm.Usage("invalid_args", "unexpected positional arguments for `anx inbox respond`")
	}
	if err := validateID(strings.TrimSpace(anyString(body["inbox_item_id"])), "inbox item id"); err != nil {
		return nil, err
	}
	if strings.TrimSpace(anyString(body["response_text"])) == "" {
		return nil, errnorm.Usage("invalid_request", "response_text is required")
	}
	if !contractenums.IsHumanAttentionResponseOutcome(anyString(body["outcome"])) {
		return nil, errnorm.Usage("invalid_request", askOutcomeUsage)
	}
	actorID, err := resolveActorIDAlias(actorIDFlag.value, cfg)
	if err != nil {
		return nil, err
	}
	if actorID != "" {
		body["actor_id"] = actorID
	}
	if err := finalizeMutationActorID(body, cfg); err != nil {
		return nil, err
	}
	return body, nil
}

func (a *App) applyContentFileOverride(body any, contentFile string, commandName string) (any, error) {
	contentFile = strings.TrimSpace(contentFile)
	if contentFile == "" {
		return body, nil
	}
	content, err := a.readRawFile(contentFile)
	if err != nil {
		return nil, err
	}

	payload, ok := body.(map[string]any)
	if !ok {
		return nil, errnorm.Usage("invalid_request", fmt.Sprintf("JSON body for `anx %s` must be an object when --body-file content overlay is provided", commandName))
	}
	payload["content"] = string(content)
	return payload, nil
}

// skipJSONStdinForContentFile is true when the user supplied document content via
// --body-file (including `-` for stdin) without --from-file.
// In that case stdin must not be consumed as the JSON mutation body; it is reserved for
// the raw document stream read by applyContentFileOverride/readRawFile.
func skipJSONStdinForContentFile(fromFile, contentFile string) bool {
	return strings.TrimSpace(fromFile) == "" && strings.TrimSpace(contentFile) != ""
}

func (a *App) readRawFile(path string) ([]byte, error) {
	path = strings.TrimSpace(path)
	if path == "-" {
		content, err := a.readStdinBody()
		if err != nil {
			return nil, errnorm.Wrap(errnorm.KindLocal, "stdin_read_failed", "failed to read stdin", err)
		}
		return content, nil
	}
	content, err := a.readInputFile(path)
	if err != nil {
		return nil, errnorm.Wrap(errnorm.KindLocal, "file_read_failed", fmt.Sprintf("failed to read file %s", path), err)
	}
	return content, nil
}

func (a *App) readBodyInput(fromFile string) ([]byte, error) {
	fromFile = strings.TrimSpace(fromFile)
	if fromFile != "" {
		if fromFile == "-" {
			content, err := a.readStdinBody()
			if err != nil {
				return nil, errnorm.Wrap(errnorm.KindLocal, "stdin_read_failed", "failed to read stdin", err)
			}
			if len(strings.TrimSpace(string(content))) == 0 {
				return nil, nil
			}
			return content, nil
		}
		content, err := a.readInputFile(fromFile)
		if err != nil {
			return nil, errnorm.Wrap(errnorm.KindLocal, "file_read_failed", fmt.Sprintf("failed to read file %s", fromFile), err)
		}
		if len(strings.TrimSpace(string(content))) == 0 {
			return nil, nil
		}
		return content, nil
	}
	content, err := a.readStdinBody()
	if err != nil {
		return nil, errnorm.Wrap(errnorm.KindLocal, "stdin_read_failed", "failed to read stdin", err)
	}
	return content, nil
}

func decodeJSONPayload(payload []byte) (any, error) {
	var parsed any
	if err := json.Unmarshal(payload, &parsed); err != nil {
		return nil, detailedJSONDecodeError(payload, err)
	}
	return parsed, nil
}

func detailedJSONDecodeError(payload []byte, decodeErr error) error {
	parseDetail := strings.TrimSpace(decodeErr.Error())
	if parseDetail == "" {
		parseDetail = "unknown parse error"
	}
	line, column := jsonErrorLineColumn(payload, decodeErr)
	if line > 0 && column > 0 {
		return errnorm.Usage("invalid_json", fmt.Sprintf("input body must be valid JSON (line %d, column %d): %s", line, column, parseDetail))
	}
	return errnorm.Usage("invalid_json", fmt.Sprintf("input body must be valid JSON: %s", parseDetail))
}

func jsonErrorLineColumn(payload []byte, decodeErr error) (int, int) {
	var syntaxErr *json.SyntaxError
	if errors.As(decodeErr, &syntaxErr) {
		return lineColumnFromOffset(payload, syntaxErr.Offset)
	}

	var typeErr *json.UnmarshalTypeError
	if errors.As(decodeErr, &typeErr) {
		return lineColumnFromOffset(payload, typeErr.Offset)
	}

	if errors.Is(decodeErr, io.ErrUnexpectedEOF) {
		return lineColumnFromOffset(payload, int64(len(payload)))
	}
	return 0, 0
}

func lineColumnFromOffset(payload []byte, offset int64) (int, int) {
	if offset <= 0 {
		return 1, 1
	}
	if offset > int64(len(payload)) {
		offset = int64(len(payload))
	}
	line := 1
	column := 1
	limit := offset - 1
	for idx := int64(0); idx < limit && idx < int64(len(payload)); idx++ {
		if payload[idx] == '\n' {
			line++
			column = 1
			continue
		}
		column++
	}
	return line, column
}

func typedRefPrefix(ref string) string {
	ref = strings.TrimSpace(ref)
	idx := strings.Index(ref, ":")
	if idx <= 0 {
		return ""
	}
	return strings.TrimSpace(ref[:idx])
}
