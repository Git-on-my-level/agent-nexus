package server

import "strings"

type errorMetadata struct {
	Recoverable bool
	Hint        string
}

var defaultErrorMetadata = map[string]errorMetadata{
	"access_denied":              {Recoverable: false, Hint: "Verify the deployment mode and endpoint access policy, then retry with the required auth context."},
	"actor_exists":               {Recoverable: true, Hint: "Use a different actor id or read existing actors before retrying."},
	"actor_registry_unavailable": {Recoverable: false, Hint: "Actor registry is unavailable; retry later or escalate to operator."},
	"agent_revoked":              {Recoverable: false, Hint: "Revoked agents cannot authenticate; register a new agent profile."},
	"auth_unavailable":           {Recoverable: false, Hint: "Authentication is not configured on this core instance; retry later or escalate to operator."},
	"auth_required":              {Recoverable: true, Hint: "Attach a valid Bearer token and retry."},
	"cli_outdated":               {Recoverable: true, Hint: "Upgrade CLI to the minimum compatible version exposed by `/meta/handshake`."},
	"conflict":                   {Recoverable: true, Hint: "Reload current state and retry with a fresh concurrency token."},
	"dev_actor_mode_required":    {Recoverable: true, Hint: "Enable `OAR_ENABLE_DEV_ACTOR_MODE=1` only for explicit local development flows."},
	"invalid_json":               {Recoverable: true, Hint: "Provide valid JSON request body and retry."},
	"invalid_request":            {Recoverable: true, Hint: "Fix request shape/fields and retry."},
	"invalid_token":              {Recoverable: true, Hint: "Refresh or rotate credentials, then retry."},
	"key_mismatch":               {Recoverable: true, Hint: "Rotate key material and retry token exchange."},
	"meta_unavailable":           {Recoverable: false, Hint: "Generated command metadata is unavailable; retry later or escalate."},
	"method_not_allowed":         {Recoverable: true, Hint: "Use the HTTP method documented for this endpoint."},
	"not_found":                  {Recoverable: true, Hint: "Verify the target resource exists and retry."},
	"primitives_unavailable":     {Recoverable: false, Hint: "Primitive store is unavailable; retry later or escalate."},
	"schema_unavailable":         {Recoverable: false, Hint: "Schema subsystem is unavailable; retry later or escalate."},
	"storage_unavailable":        {Recoverable: true, Hint: "Retry after core storage health recovers."},
	"stream_unavailable":         {Recoverable: false, Hint: "Streaming is unavailable on this core instance; use polling endpoints."},
	"unknown_actor_id":           {Recoverable: true, Hint: "Register/select a valid actor id before mutating state."},
	"username_taken":             {Recoverable: true, Hint: "Use a different username and retry registration/update."},
	"internal_error":             {Recoverable: false, Hint: "Retry once; if it persists, escalate with logs and request context."},
}

func errorPayload(code string, message string) map[string]any {
	code = strings.TrimSpace(code)
	message = strings.TrimSpace(message)
	if message == "" {
		message = "request failed"
	}
	metadata, ok := defaultErrorMetadata[code]
	if !ok {
		metadata = errorMetadata{
			Recoverable: false,
			Hint:        "Review error details and endpoint usage, then retry if appropriate.",
		}
	}
	return map[string]any{
		"code":        code,
		"message":     message,
		"recoverable": metadata.Recoverable,
		"hint":        metadata.Hint,
	}
}
