package errnorm

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

type Kind string

const (
	KindUsage    Kind = "usage"
	KindLocal    Kind = "local"
	KindNetwork  Kind = "network"
	KindRemote   Kind = "remote"
	KindInternal Kind = "internal"
)

type Error struct {
	Kind        Kind
	Code        string
	Message     string
	Recoverable *bool
	Hint        string
	Details     any
	Cause       error
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if e.Cause == nil {
		return e.Message
	}
	if e.Message == "" {
		return e.Cause.Error()
	}
	return e.Message + ": " + e.Cause.Error()
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

func New(kind Kind, code string, message string) *Error {
	return &Error{Kind: kind, Code: code, Message: message}
}

func Usage(code string, message string) *Error {
	return New(KindUsage, code, message)
}

func Local(code string, message string) *Error {
	return New(KindLocal, code, message)
}

func Network(code string, message string) *Error {
	return New(KindNetwork, code, message)
}

func Internal(code string, message string) *Error {
	return New(KindInternal, code, message)
}

func Wrap(kind Kind, code string, message string, cause error) *Error {
	return &Error{Kind: kind, Code: code, Message: message, Cause: cause}
}

func WithDetails(err *Error, details any) *Error {
	if err == nil {
		return nil
	}
	err.Details = details
	return err
}

func ExitCode(err error) int {
	if err == nil {
		return 0
	}
	var typed *Error
	if errors.As(err, &typed) && typed.Kind == KindUsage {
		return 2
	}
	return 1
}

func Normalize(err error) *Error {
	if err == nil {
		return nil
	}
	var typed *Error
	if errors.As(err, &typed) {
		if typed.Code == "" {
			typed.Code = "error"
		}
		if typed.Message == "" {
			typed.Message = typed.Error()
		}
		applyErrorMetadata(typed)
		return typed
	}
	normalized := &Error{
		Kind:    KindInternal,
		Code:    "internal_error",
		Message: err.Error(),
		Cause:   err,
	}
	applyErrorMetadata(normalized)
	return normalized
}

func FromHTTPFailure(status int, body []byte) *Error {
	code := "remote_error"
	message := fmt.Sprintf("request failed with status %d", status)
	payload := map[string]any{"status": status}

	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err == nil {
		if errObj, ok := parsed["error"].(map[string]any); ok {
			if v, ok := errObj["code"].(string); ok && v != "" {
				code = v
			}
			if v, ok := errObj["message"].(string); ok && v != "" {
				message = v
			}
			if v, ok := errObj["recoverable"].(bool); ok {
				recoverable := v
				payload["recoverable"] = recoverable
			}
			if v, ok := errObj["hint"].(string); ok && strings.TrimSpace(v) != "" {
				payload["hint"] = strings.TrimSpace(v)
			}
		}
		payload["parsed"] = parsed
	} else if len(body) > 0 {
		payload["body"] = string(body)
	}
	out := &Error{
		Kind:    KindRemote,
		Code:    code,
		Message: message,
		Details: payload,
	}
	if rawRecoverable, ok := payload["recoverable"].(bool); ok {
		recoverable := rawRecoverable
		out.Recoverable = &recoverable
	}
	if rawHint, ok := payload["hint"].(string); ok {
		out.Hint = strings.TrimSpace(rawHint)
	}
	applyErrorMetadata(out)
	return out
}

type Metadata struct {
	Recoverable bool
	Hint        string
}

var defaultMetadataByCode = map[string]Metadata{
	"actor_exists":                  {Recoverable: true, Hint: "Use a different actor id or load the existing actor with `oar actors list`."},
	"agent_revoked":                 {Recoverable: false, Hint: "Create/register a new agent profile; revoked agents cannot be reactivated."},
	"auth_registration_unavailable": {Recoverable: true, Hint: "Core auth may still be starting. Retry `oar auth register` in a few seconds, or run `oar api call --path /readyz` to confirm readiness."},
	"auth_required":                 {Recoverable: true, Hint: "Run `oar --agent <agent> auth whoami` to refresh credentials, then retry."},
	"cli_outdated":                  {Recoverable: true, Hint: "Upgrade the CLI to the minimum compatible version from `/meta/handshake`."},
	"config_resolution_failed":      {Recoverable: true, Hint: "Set --base-url or OAR_BASE_URL, select a profile with --agent or OAR_AGENT (or `oar auth default <name>` when multiple profiles exist), then run `oar doctor` if connectivity is uncertain."},
	"conflict":                      {Recoverable: true, Hint: "Reload current state and retry with a fresh `if_updated_at` value."},
	"draft_exists":                  {Recoverable: true, Hint: "Use a different draft id or discard the existing draft first."},
	"draft_not_found":               {Recoverable: true, Hint: "Run `oar draft list` to discover valid draft ids."},
	"draft_validation_failed":       {Recoverable: true, Hint: "Fix the validation errors in the payload, then run `oar draft create` again."},
	"file_read_failed":              {Recoverable: true, Hint: "Verify the file path and permissions, then retry."},
	"invalid_flags":                 {Recoverable: true, Hint: "Run `oar help` for supported flags and usage."},
	"invalid_header":                {Recoverable: true, Hint: "Use `--header key:value` with a non-empty key."},
	"invalid_json":                  {Recoverable: true, Hint: "Provide valid JSON input and retry."},
	"invalid_request":               {Recoverable: true, Hint: "Review required fields and request shape, then retry."},
	"invalid_token":                 {Recoverable: true, Hint: "Run `oar --agent <agent> auth token-status` then `oar --agent <agent> auth rotate` if needed."},
	"key_mismatch":                  {Recoverable: true, Hint: "Rotate the agent key (`oar --agent <agent> auth rotate`) and retry token minting."},
	"last_active_principal":         {Recoverable: true, Hint: "Retry only with `--allow-human-lockout --human-lockout-reason <why>` for explicit break-glass recovery; it can leave the workspace without any active human principal."},
	"method_not_allowed":            {Recoverable: true, Hint: "Use the HTTP method documented for this endpoint."},
	"network_error":                 {Recoverable: true, Hint: "Check network/core availability and retry with backoff."},
	"not_found":                     {Recoverable: true, Hint: "Verify the target id/path exists and retry."},
	"profile_not_found":             {Recoverable: true, Hint: "Run `oar --agent <agent> auth register --username <username>` to create a profile."},
	"request_failed":                {Recoverable: true, Hint: "Check connectivity and credentials, then retry."},
	"stream_connect_failed":         {Recoverable: true, Hint: "Retry with `--follow` after verifying stream endpoint availability."},
	"stream_read_failed":            {Recoverable: true, Hint: "Retry with `--follow` or use `--last-event-id` to resume."},
	"timeout_exceeded":              {Recoverable: true, Hint: "Increase `--timeout` or reduce request scope."},
	"unknown_actor_id":              {Recoverable: true, Hint: "Register/select a valid actor id before issuing writes."},
	"unknown_command":               {Recoverable: true, Hint: "Run `oar help` to list available commands."},
	"username_taken":                {Recoverable: true, Hint: "Choose a different username and retry."},
	"internal_error":                {Recoverable: false, Hint: "Retry once; if it persists, escalate with logs and request details."},
	"schema_unavailable":            {Recoverable: false, Hint: "Core schema subsystem is unavailable; retry later or escalate to operator."},
	"primitives_unavailable":        {Recoverable: false, Hint: "Core storage subsystem is unavailable; retry later or escalate to operator."},
	"actor_registry_unavailable":    {Recoverable: false, Hint: "Actor registry is unavailable on core; retry later or escalate."},
	"meta_unavailable":              {Recoverable: false, Hint: "Generated metadata is unavailable on core; retry later or escalate."},
	"storage_unavailable":           {Recoverable: true, Hint: "Retry with backoff while core storage recovers."},
}

func MetadataForCode(code string) Metadata {
	code = strings.TrimSpace(code)
	if code == "" {
		return Metadata{Recoverable: false, Hint: "Review the error details and command help, then retry if appropriate."}
	}
	if metadata, ok := defaultMetadataByCode[code]; ok {
		return metadata
	}
	return Metadata{Recoverable: false, Hint: "Review the error details and command help, then retry if appropriate."}
}

func applyErrorMetadata(err *Error) {
	if err == nil {
		return
	}
	metadata := MetadataForCode(err.Code)
	if err.Recoverable == nil {
		recoverable := metadata.Recoverable
		err.Recoverable = &recoverable
	}
	if strings.TrimSpace(err.Hint) == "" {
		err.Hint = metadata.Hint
	}
}

func RecoverableValue(err *Error) bool {
	if err == nil || err.Recoverable == nil {
		return false
	}
	return *err.Recoverable
}
