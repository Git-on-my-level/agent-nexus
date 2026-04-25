package errnorm

import (
	"fmt"
	"strings"

	"agent-nexus-cli/internal/registry"
)

// enrichRemoteError replaces generic API hints with CLI-specific recovery guidance for
// agents. It only applies to errors produced by FromHTTPFailure (Details map includes HTTP status).

// enrichLocalInvalidRequest applies invalid_request message hints to locally constructed Usage
// errors (for example CLI-side --limit validation before HTTP).
func enrichLocalInvalidRequest(e *Error) {
	if e == nil || e.Kind != KindUsage || strings.TrimSpace(e.Code) != "invalid_request" {
		return
	}
	msg := strings.TrimSpace(e.Message)
	hint, recovery := enrichInvalidRequest(msg)
	if hint == "" && recovery == nil {
		return
	}
	if hint != "" {
		e.Hint = hint
	}
	if recovery != nil {
		var details map[string]any
		if m, ok := e.Details.(map[string]any); ok && m != nil {
			details = m
		} else {
			details = map[string]any{}
			e.Details = details
		}
		mergeRecovery(details, recovery)
	}
}

func enrichRemoteError(e *Error, httpStatus int) {
	if e == nil || e.Kind != KindRemote {
		return
	}
	details, ok := e.Details.(map[string]any)
	if !ok || details == nil {
		return
	}
	if _, ok := details["status"]; !ok {
		return
	}

	msg := strings.TrimSpace(e.Message)
	code := strings.TrimSpace(e.Code)

	var recovery map[string]any
	var hint string

	switch code {
	case "conflict":
		hint, recovery = enrichConflict(msg)
	case "invalid_request":
		hint, recovery = enrichInvalidRequest(msg)
	case "key_mismatch":
		hint, recovery = enrichKeyMismatch(httpStatus, msg)
	case "agent_revoked":
		hint, recovery = enrichAgentRevoked()
	}

	if hint == "" && recovery == nil {
		hint, recovery = enrichAuth(code, httpStatus, msg)
	}

	if hint != "" {
		e.Hint = hint
	}
	if recovery != nil {
		mergeRecovery(details, recovery)
	}
	// Keep the transport details map aligned with the normalized hint so JSON clients
	// that read error.details.hint see the same string as error.hint.
	if strings.TrimSpace(e.Hint) != "" {
		details["hint"] = strings.TrimSpace(e.Hint)
	}
}

func mergeRecovery(details map[string]any, rec map[string]any) {
	if len(rec) == 0 {
		return
	}
	details["anx_cli_recovery"] = rec
}

// enrichConflict maps core conflict messages (boards_handlers.go, cards_handlers.go, …) to hints.
func enrichConflict(msg string) (string, map[string]any) {
	switch msg {
	case "board has been updated; refresh and retry",
		"board membership already exists or board has changed; refresh and retry":
		return "Your if_board_updated_at token is stale (the board changed since you last read it). Fetch a fresh board: run `anx boards get --board-id <board_id> --json`, then set `if_board_updated_at` from the board's `updated_at` in that response and retry.",
			map[string]any{
				"kind":        "stale_concurrency_token",
				"field":       "if_board_updated_at",
				"refresh_cli": "anx boards get --board-id <board_id> --json",
			}
	case "card has been updated; refresh and retry":
		return "Your if_updated_at token is stale for this card. Fetch the latest card: `anx boards cards get --card-id <card_id> --json` or `anx cards get --card-id <card_id> --json`, then set `if_updated_at` from the card's `updated_at` and retry.",
			map[string]any{
				"kind":        "stale_concurrency_token",
				"field":       "if_updated_at",
				"refresh_cli": "anx boards cards get --card-id <card_id> --json",
			}
	case "topic has been updated; refresh and retry":
		return "Your if_updated_at token is stale for this topic. Run `anx topics get --topic-id <topic_id> --json`, copy `updated_at` into `if_updated_at`, and retry.",
			map[string]any{
				"kind":        "stale_concurrency_token",
				"field":       "if_updated_at",
				"refresh_cli": "anx topics get --topic-id <topic_id> --json",
			}
	case "document has been updated; refresh and retry":
		return "Your if_document_updated_at token is stale. Run `anx docs get --document-id <document_id> --json`, copy the document's `updated_at` into `if_document_updated_at` for the revision request, and retry.",
			map[string]any{
				"kind":        "stale_concurrency_token",
				"field":       "if_document_updated_at",
				"refresh_cli": "anx docs get --document-id <document_id> --json",
			}
	case "board already exists":
		return "A board with this title (or identity) already exists in the workspace. Run `anx boards list --json` to find it, or change the create payload to use a distinct title before retrying.",
			map[string]any{
				"kind":     "resource_exists",
				"resource": "board",
				"list_cli": "anx boards list --json",
			}
	default:
		return "", nil
	}
}

func enrichInvalidRequest(msg string) (string, map[string]any) {
	switch msg {
	case "if_updated_at is required":
		return "The request body must include `if_updated_at` (RFC3339). For the current value, run `anx boards cards get --card-id <card_id> --json` or `anx cards get --card-id <card_id> --json` and copy the card `updated_at` field.",
			map[string]any{
				"kind":  "missing_required_field",
				"field": "if_updated_at",
			}
	case "if_board_updated_at is required":
		return "The request body must include `if_board_updated_at` (RFC3339). Run `anx boards get --board-id <board_id> --json` and copy `board.updated_at` from the response.",
			map[string]any{
				"kind":  "missing_required_field",
				"field": "if_board_updated_at",
			}
	case "column_key is required":
		vals := registry.ColumnKeyEnumValues()
		if len(vals) == 0 {
			return "column_key is required in the JSON body for this operation. See `anx cards move --help` for the expected shape.",
				map[string]any{"kind": "missing_required_field", "field": "column_key", "help_cli": "anx cards move --help"}
		}
		return "column_key is required in the JSON body. Typical values for a board column are: " + strings.Join(vals, ", ") + ". See `anx cards move --help`.",
			map[string]any{"kind": "missing_required_field", "field": "column_key", "valid_enum_values": vals, "help_cli": "anx cards move --help"}
	}

	if hint, recovery := enrichInvalidRequestByMessage(msg); hint != "" || recovery != nil {
		return hint, recovery
	}

	lmsg := strings.ToLower(msg)
	if strings.Contains(lmsg, "column_key") && columnKeyLooksLikeBadValue(lmsg) {
		return enrichColumnKeyInvalid(msg)
	}
	if h, r := enrichSchemaStrictEnum(msg); h != "" || r != nil {
		return h, r
	}
	if h, r := enrichMustBeOneOfField(msg); h != "" || r != nil {
		return h, r
	}
	return "", nil
}

// enrichInvalidRequestByMessage maps common core invalid_request strings to CLI/agent recovery text.
func enrichInvalidRequestByMessage(msg string) (string, map[string]any) {
	msg = strings.TrimSpace(msg)
	if msg == "" {
		return "", nil
	}

	switch msg {
	case "body is required":
		return "The request body was empty or not a JSON object. Send an object on stdin or via `--from-file` (for example `anx boards cards create --help` or `anx cards create --help`).",
			map[string]any{"kind": "missing_request_body", "help_cli": "anx boards cards create --help"}
	case "patch is required":
		return "The JSON body must include a top-level `patch` object for this operation. Pipe JSON to stdin or use `--from-file`; see the command help for the expected keys.",
			map[string]any{"kind": "missing_required_field", "field": "patch"}
	case "board is required":
		return "The JSON body must include a `board` object (board create / update). See `anx boards create --help` or locate boards update under `anx help`.",
			map[string]any{"kind": "missing_required_field", "field": "board", "help_cli": "anx boards create --help"}
	case "topic is required":
		return "The JSON body must include a `topic` object. See `anx topics create --help`.",
			map[string]any{"kind": "missing_required_field", "field": "topic", "help_cli": "anx topics create --help"}
	case "document is required":
		return "The JSON body must include a `document` object. See `anx docs create --help`.",
			map[string]any{"kind": "missing_required_field", "field": "document", "help_cli": "anx docs create --help"}
	case "content is required":
		return "The JSON body must include `content` for this document operation. See `anx docs create --help` or `anx docs revision --help` as appropriate.",
			map[string]any{"kind": "missing_required_field", "field": "content", "help_cli": "anx docs create --help"}
	case "artifact is required":
		return "The JSON body must include an `artifact` object. See `anx help` for artifact create / packet topics.",
			map[string]any{"kind": "missing_required_field", "field": "artifact", "help_cli": "anx help"}
	case "event is required":
		return "The JSON body must include an `event` object for primitive event ingest. See `anx help` for the events ingest topic.",
			map[string]any{"kind": "missing_required_field", "field": "event", "help_cli": "anx help"}
	case "title is required":
		return "Card create requires a non-empty `title` (or `card.title`). See `anx boards cards create --help`.",
			map[string]any{"kind": "missing_required_field", "field": "title", "help_cli": "anx boards cards create --help"}
	case "board.title is required":
		return "Board create requires `board.title`. See `anx boards create --help`.",
			map[string]any{"kind": "missing_required_field", "field": "board.title", "help_cli": "anx boards create --help"}
	case "board.status is required":
		return "Board update requires `board.status`. Allowed values are in the boards update (PATCH board) contract — locate that topic via `anx help`.",
			map[string]any{"kind": "missing_required_field", "field": "board.status", "help_cli": "anx help"}
	case "limit must be between 1 and 1000":
		return "List `limit` must be between 1 and 1000. Adjust `--limit` (or the JSON `limit` field) and retry.",
			map[string]any{"kind": "invalid_pagination", "field": "limit", "min": 1, "max": 1000}
	case "cursor is invalid":
		return "Pagination `cursor` must be the opaque string from a previous list response (or omit it to start from the beginning). Do not invent or truncate cursor values.",
			map[string]any{"kind": "invalid_pagination", "field": "cursor"}
	case "resolution_refs require resolution":
		return "You sent `resolution_refs` without `resolution`. Either set `resolution` (done/canceled) together with refs, or clear `resolution_refs`. See `anx boards cards create --help` / `anx cards patch --help`.",
			map[string]any{"kind": "resolution_workflow", "field": "resolution_refs", "help_cli": "anx cards patch --help"}
	case "resolution requires column_key done":
		return "To set `resolution`, the card must be in column_key `done` (move it with `anx cards move` or `anx boards cards move` first).",
			map[string]any{"kind": "resolution_workflow", "field": "resolution", "help_cli": "anx cards move --help"}
	case "resolution_refs are required when resolution is set":
		return "When `resolution` is set, include non-empty `resolution_refs` consistent with that resolution. See `anx cards patch --help` for the JSON shape.",
			map[string]any{"kind": "resolution_workflow", "field": "resolution_refs", "help_cli": "anx cards patch --help"}
	case "reason is required":
		return "This trash/archive-style request requires a non-empty `reason` string in the JSON body. See the command help (for example `anx cards trash --help`) and pass `--reason` or include `reason` in stdin JSON.",
			map[string]any{"kind": "missing_required_field", "field": "reason", "help_cli": "anx cards trash --help"}
	case "items is required":
		return "Batch card operations require an `items` array in the JSON body. See `anx boards cards create-batch --help`.",
			map[string]any{"kind": "missing_required_field", "field": "items", "help_cli": "anx boards cards create-batch --help"}
	case "items must be a non-empty array":
		return "`items` must be a non-empty array of request objects. See `anx boards cards create-batch --help`.",
			map[string]any{"kind": "invalid_request_shape", "field": "items", "help_cli": "anx boards cards create-batch --help"}
	case "status is not supported on card create; column_key and resolution define lifecycle":
		return "Do not send `status` on card create. Set `column_key` (and `resolution` when closing) instead. See `anx boards cards create --help`.",
			map[string]any{"kind": "unsupported_field", "field": "status", "help_cli": "anx boards cards create --help"}
	case "JSON body must be an object":
		return "The HTTP body must be a single JSON object `{...}`, not an array or bare scalar. Pipe a JSON object on stdin or use `--from-file`.",
			map[string]any{"kind": "invalid_request_shape", "field": "body"}
	case "failed to read body":
		return "Core could not read the HTTP request body. Retry with a non-empty body on stdin, or reduce size / check the client transport.",
			map[string]any{"kind": "missing_request_body"}
	case "if_base_revision is required":
		return "Document revision requests require `if_base_revision` from the document metadata. Run `anx docs get --document-id <id> --json` and copy the correct base revision id.",
			map[string]any{"kind": "missing_required_field", "field": "if_base_revision", "help_cli": "anx docs get --document-id <document_id> --json"}
	case "specify exactly one of: source_ref or target_ref":
		return "This ref-edges request must include exactly one of `source_ref` or `target_ref` in the JSON body (not both, not neither). See `anx help` for ref-edges topics.",
			map[string]any{"kind": "invalid_request_shape", "field": "source_ref|target_ref", "help_cli": "anx help"}
	}

	if hint, recovery := enrichInvalidRequestRFCTimestamp(msg); hint != "" {
		return hint, recovery
	}
	if hint, recovery := enrichPatchNotSupportedMessage(msg); hint != "" {
		return hint, recovery
	}
	if hint, recovery := enrichCardPatchNotWritable(msg); hint != "" {
		return hint, recovery
	}
	return "", nil
}

func enrichInvalidRequestRFCTimestamp(msg string) (string, map[string]any) {
	var field string
	switch {
	case strings.HasSuffix(msg, " must be an RFC3339 datetime string"):
		field = strings.TrimSpace(strings.TrimSuffix(msg, " must be an RFC3339 datetime string"))
	case strings.HasSuffix(msg, " must be an RFC3339 timestamp"):
		field = strings.TrimSpace(strings.TrimSuffix(msg, " must be an RFC3339 timestamp"))
	default:
		return "", nil
	}
	if field == "" {
		return "", nil
	}
	hint := fmt.Sprintf("Field %q must be a full RFC3339 timestamp. Copy it exactly from a fresh GET response (including timezone offset); do not truncate or reformat.", field)
	rec := map[string]any{"kind": "invalid_timestamp_format", "field": field}
	switch field {
	case "if_board_updated_at":
		rec["refresh_cli"] = "anx boards get --board-id <board_id> --json"
		hint += " For boards, use `board.updated_at` from `anx boards get --board-id <board_id> --json`."
	case "if_updated_at":
		rec["refresh_cli"] = "anx cards get --card-id <card_id> --json"
		hint += " For cards, use `updated_at` from `anx cards get --card-id <card_id> --json` (or `anx boards cards get`); for topics use `anx topics get --topic-id <topic_id> --json`."
	case "if_document_updated_at":
		rec["refresh_cli"] = "anx docs get --document-id <document_id> --json"
		hint += " For documents, use `updated_at` from `anx docs get --document-id <document_id> --json`."
	}
	return hint, rec
}

func enrichCardPatchNotWritable(msg string) (string, map[string]any) {
	if !strings.Contains(msg, " is not writable; use the move endpoint for board placement changes") {
		return "", nil
	}
	return "That field cannot be changed via card patch. Update board placement with `anx cards move` or `anx boards cards move` (see `--help`), and use `patch.resolution` / `column_key` for lifecycle.",
		map[string]any{"kind": "use_move_endpoint", "help_cli": "anx cards move --help"}
}

func enrichPatchNotSupportedMessage(msg string) (string, map[string]any) {
	switch msg {
	case "patch.body is not supported; use patch.summary":
		return "Use `patch.summary` for card text; `patch.body` is not accepted. See `anx cards patch --help`.",
			map[string]any{"kind": "unsupported_field", "field": "patch.body", "help_cli": "anx cards patch --help"}
	case "patch.assignee is not supported; use patch.assignee_refs":
		return "Use `patch.assignee_refs` (string list), not `patch.assignee`. See `anx cards patch --help`.",
			map[string]any{"kind": "unsupported_field", "field": "patch.assignee", "help_cli": "anx cards patch --help"}
	case "patch.pinned_document_id is not supported; use patch.document_ref":
		return "Use `patch.document_ref` instead of `patch.pinned_document_id`. See `anx cards patch --help`.",
			map[string]any{"kind": "unsupported_field", "field": "patch.pinned_document_id", "help_cli": "anx cards patch --help"}
	case "patch.refs is not supported; use patch.related_refs and patch.topic_ref":
		return "Use `patch.related_refs` plus `patch.topic_ref` instead of `patch.refs`. See `anx cards patch --help`.",
			map[string]any{"kind": "unsupported_field", "field": "patch.refs", "help_cli": "anx cards patch --help"}
	case "patch.priority is not supported in the canonical card model":
		return "`patch.priority` is not part of the canonical card model; remove it or map your workflow to other fields. See `anx cards patch --help`.",
			map[string]any{"kind": "unsupported_field", "field": "patch.priority", "help_cli": "anx cards patch --help"}
	case "patch.status is not supported; use the move endpoint and patch.resolution":
		return "Do not patch `status` directly. Move the card (`anx cards move`) and use `patch.resolution` when completing. See `anx cards move --help` and `anx cards patch --help`.",
			map[string]any{"kind": "use_move_endpoint", "field": "patch.status", "help_cli": "anx cards move --help"}
	case "patch.body_markdown is not supported; use patch.summary":
		return "Use `patch.summary` instead of `patch.body_markdown`. See `anx cards patch --help`.",
			map[string]any{"kind": "unsupported_field", "field": "patch.body_markdown", "help_cli": "anx cards patch --help"}
	case "body is not supported; use summary":
		return "Card create uses `summary` for text; `body` is not accepted. See `anx boards cards create --help`.",
			map[string]any{"kind": "unsupported_field", "field": "body", "help_cli": "anx boards cards create --help"}
	case "body_markdown is not supported; use summary":
		return "Card create uses `summary`; `body_markdown` is not accepted. See `anx boards cards create --help`.",
			map[string]any{"kind": "unsupported_field", "field": "body_markdown", "help_cli": "anx boards cards create --help"}
	case "assignee is not supported; use assignee_refs":
		return "Card create uses `assignee_refs` (list of actor ref strings), not `assignee`. See `anx boards cards create --help`.",
			map[string]any{"kind": "unsupported_field", "field": "assignee", "help_cli": "anx boards cards create --help"}
	case "pinned_document_id is not supported; use document_ref":
		return "Use `document_ref` instead of `pinned_document_id` on card create. See `anx boards cards create --help`.",
			map[string]any{"kind": "unsupported_field", "field": "pinned_document_id", "help_cli": "anx boards cards create --help"}
	case "refs is not supported; use related_refs and topic_ref":
		return "Use `related_refs` and `topic_ref` instead of `refs` on card create. See `anx boards cards create --help`.",
			map[string]any{"kind": "unsupported_field", "field": "refs", "help_cli": "anx boards cards create --help"}
	case "priority is not supported in the canonical card model":
		return "`priority` is not supported on card create in the canonical model. See `anx boards cards create --help`.",
			map[string]any{"kind": "unsupported_field", "field": "priority", "help_cli": "anx boards cards create --help"}
	default:
		if strings.Contains(msg, "must not be combined with legacy aliases") {
			return "You mixed canonical fields with deprecated aliases in one request. Keep only the canonical fields named in the error, or only the legacy aliases, not both. See `anx cards patch --help` / `anx boards cards create --help`.",
				map[string]any{"kind": "invalid_request_shape", "help_cli": "anx cards patch --help"}
		}
	}
	return "", nil
}

func enrichAgentRevoked() (string, map[string]any) {
	return "This agent was revoked on the server; revoked principals cannot be reactivated. Register a new agent profile with `anx auth register` (new username/handle), then use that profile for this workspace.",
		map[string]any{
			"kind":         "agent_revoked",
			"register_cli": "anx auth register",
		}
}

// enrichSchemaStrictEnum handles core/schema.ValidateEnum errors, e.g.
// invalid value "x" for strict enum topic_status (allowed: a, b, c).
// Core may wrap as `topic.status: invalid value ...`.
func enrichSchemaStrictEnum(msg string) (string, map[string]any) {
	idx := strings.Index(msg, "invalid value ")
	if idx < 0 {
		return "", nil
	}
	tail := msg[idx:]
	mid := " for strict enum "
	suff := " (allowed: "
	i := strings.Index(tail, mid)
	j := strings.Index(tail, suff)
	if i < 0 || j <= i {
		return "", nil
	}
	enumName := strings.TrimSpace(tail[i+len(mid) : j])
	help := cliHelpForSchemaEnum(enumName)
	if help == "" {
		return "", nil
	}
	var hint string
	recovery := map[string]any{
		"kind":        "invalid_enum",
		"schema_enum": enumName,
		"help_cli":    help,
	}
	if enumName == "board_status" {
		hint = fmt.Sprintf("This value is not allowed for strict schema enum %q; allowed values are listed in the error message above. For board updates, run `anx help` and locate the boards update (PATCH board) topic.", enumName)
		recovery["help_cli"] = "anx help"
		recovery["command_topic"] = "boards update"
	} else {
		hint = fmt.Sprintf("This value is not allowed for strict schema enum %q; allowed values are listed in the error message above. See `%s` for the relevant request fields.", enumName, help)
	}
	return hint, recovery
}

func cliHelpForSchemaEnum(enumName string) string {
	switch enumName {
	case "topic_status":
		return "anx topics patch --help"
	case "topic_type":
		return "anx topics patch --help"
	case "thread_status":
		return "anx threads workspace --help"
	case "board_status":
		return "anx help"
	case "board_column_key":
		return "anx cards move --help"
	default:
		return ""
	}
}

func enrichMustBeOneOfField(msg string) (string, map[string]any) {
	l := strings.ToLower(msg)
	idx := strings.Index(l, " must be one of")
	if idx < 0 {
		return "", nil
	}
	rawField := strings.TrimSpace(msg[:idx])
	if rawField == "" {
		return "", nil
	}
	// column_key handled by enrichColumnKeyInvalid.
	if strings.EqualFold(rawField, "column_key") {
		return "", nil
	}

	switch strings.ToLower(rawField) {
	case "patch.risk":
		return enumHintFromRegistry("patch.risk", "cards patch", "patch.risk", "anx cards patch --help")
	case "risk":
		return enumHintFromRegistry("risk", "cards patch", "patch.risk", "anx cards patch --help")
	case "board.status":
		// Registry uses cli_path "boards patch"; the CLI user command is `boards update`, which does not accept `--help` — use `anx help` discovery.
		vals, ok := registry.BodyFieldEnum("boards patch", "patch.status")
		if !ok {
			return "", nil
		}
		joined := strings.Join(vals, ", ")
		hint := fmt.Sprintf("The board.status value is not allowed. Use one of: %s. For the boards update JSON body, run `anx help` and locate the boards update (PATCH board) topic.", joined)
		return hint, map[string]any{
			"kind":              "invalid_enum",
			"field":             "board.status",
			"valid_enum_values": vals,
			"help_cli":          "anx help",
			"command_topic":     "boards update",
		}
	case "resolution":
		return enumHintFromRegistry("resolution", "cards patch", "patch.resolution", "anx cards patch --help")
	case "card.status":
		// boards_store.go canonical card.status values (not all appear in commands.json).
		vals := []string{"todo", "in_progress", "done", "cancelled"}
		return compactMustBeOneOfHint("card.status", vals, "anx cards patch --help")
	case "content_type":
		vals := []string{"binary", "structured", "text"}
		return compactMustBeOneOfHint("content_type", vals, "anx docs create --help")
	case "status":
		// agent_notifications_handlers.go: status must be one of unread, read, dismissed
		if strings.Contains(l, "unread") && strings.Contains(l, "dismissed") {
			vals := []string{"dismissed", "read", "unread"}
			return compactMustBeOneOfHint("status", vals, "anx agent notifications read --help")
		}
		return "", nil
	case "patch.type":
		return enumHintFromRegistry("patch.type", "topics patch", "patch.type", "anx topics patch --help")
	default:
		return "", nil
	}
}

func enumHintFromRegistry(apiField, cliPath, bodyField, help string) (string, map[string]any) {
	vals, ok := registry.BodyFieldEnum(cliPath, bodyField)
	if !ok {
		return "", nil
	}
	return compactMustBeOneOfHint(apiField, vals, help)
}

func compactMustBeOneOfHint(apiField string, vals []string, help string) (string, map[string]any) {
	if len(vals) == 0 {
		return "", nil
	}
	joined := strings.Join(vals, ", ")
	hint := fmt.Sprintf("The %s value is not allowed. Use one of: %s. See `%s` for the JSON body shape.", apiField, joined, help)
	return hint, map[string]any{
		"kind":              "invalid_enum",
		"field":             apiField,
		"valid_enum_values": vals,
		"help_cli":          help,
	}
}

func columnKeyLooksLikeBadValue(lmsg string) bool {
	if strings.Contains(lmsg, "is required") {
		return false
	}
	// Core uses e.g. `column_key must be one of: backlog, ...` (boards_handlers.go, boards_store.go).
	return strings.Contains(lmsg, "must be one of") ||
		strings.Contains(lmsg, "invalid") ||
		strings.Contains(lmsg, "unknown") ||
		strings.Contains(lmsg, "not allowed") ||
		strings.Contains(lmsg, "not valid") ||
		strings.Contains(lmsg, "enum")
}

func enrichColumnKeyInvalid(msg string) (string, map[string]any) {
	vals := registry.ColumnKeyEnumValues()
	recovery := map[string]any{
		"kind":     "invalid_enum",
		"field":    "column_key",
		"help_cli": "anx cards move --help",
	}
	if len(vals) > 0 {
		recovery["valid_enum_values"] = vals
	}
	joined := strings.Join(vals, ", ")
	lmsg := strings.ToLower(msg)

	if len(vals) == 0 {
		return "The column_key value is not valid for this operation. Run `anx cards move --help` (or the boards cards command you used) for allowed values and required body shape.",
			recovery
	}

	// Core already returns `column_key must be one of: ...` — do not repeat that sentence in the hint.
	if strings.Contains(lmsg, "must be one of") {
		return "The column_key you sent is not a valid board column key. Use one of: " + joined + ". See `anx cards move --help` for the JSON body shape.",
			recovery
	}

	return "column_key failed validation (" + msg + "). Use one of: " + joined + ". See `anx cards move --help`.",
		recovery
}

// enrichKeyMismatch maps core auth_handlers.go key_mismatch responses to actionable CLI guidance.
func enrichKeyMismatch(httpStatus int, msg string) (string, map[string]any) {
	msg = strings.TrimSpace(msg)
	switch msg {
	case "key assertion could not be validated":
		if httpStatus != 401 {
			return "", nil
		}
		return "The server rejected your agent key assertion (profile keys do not match this core or the registration is stale). Run `anx auth token-status`, then `anx auth rotate` for this `--agent` profile, or `anx auth register` if this core expects a fresh registration. When multiple profiles exist, pass `--agent` explicitly.",
			map[string]any{
				"kind":       "key_mismatch",
				"reason":     "key_assertion_failed",
				"check_cli":  "anx auth token-status",
				"rotate_cli": "anx auth rotate",
			}
	case "actor_id does not match authenticated principal":
		if httpStatus != 403 {
			return "", nil
		}
		return "The request uses an actor_id that does not match the authenticated profile. Use the actor_id from `anx auth whoami`, or select a different `--agent` profile that owns that actor_id.",
			map[string]any{
				"kind":      "key_mismatch",
				"reason":    "actor_id_mismatch",
				"check_cli": "anx auth whoami",
			}
	default:
		return "", nil
	}
}

func enrichAuth(code string, httpStatus int, msg string) (string, map[string]any) {
	if code == "invalid_token" || code == "auth_required" {
		return authHintRecovery(httpStatus)
	}
	// 401 with no JSON error object: FromHTTPFailure keeps code remote_error and a generic message.
	if httpStatus == 401 && code == "remote_error" && strings.Contains(strings.ToLower(strings.TrimSpace(msg)), "request failed with status") {
		return authHintRecovery(httpStatus)
	}
	return "", nil
}

func authHintRecovery(httpStatus int) (string, map[string]any) {
	hint := "Authentication failed or the bearer token is no longer valid. Run `anx auth token-status` (use `--agent <profile>` if you use multiple profiles). If the token is expired or invalid, run `anx auth rotate` for that profile, or re-register with `anx auth register` if needed."
	return hint, map[string]any{
		"kind":       "auth_refresh",
		"status":     httpStatus,
		"check_cli":  "anx auth token-status",
		"rotate_cli": "anx auth rotate",
	}
}
