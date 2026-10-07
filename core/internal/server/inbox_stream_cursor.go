package server

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"

	"agent-nexus-core/internal/primitives"
)

type inboxStreamCursor struct {
	Scope       string
	Category    int
	Trigger, ID string
}

func inboxCursorScope(r *http.Request) string {
	if principal, ok := cachedAuthenticatedPrincipal(r); ok && principal != nil {
		return principal.AgentID
	}
	return "anonymous"
}

func encodeInboxStreamCursor(r *http.Request, filter primitives.DerivedInboxListFilter) string {
	if filter.BeforeID == "" {
		return ""
	}
	raw, _ := json.Marshal(inboxStreamCursor{inboxCursorScope(r), filter.BeforeCategory, filter.BeforeTrigger, filter.BeforeID})
	return base64.RawURLEncoding.EncodeToString(raw)
}

func decodeInboxStreamCursor(r *http.Request, eventID string) (primitives.DerivedInboxListFilter, error) {
	filter := primitives.DerivedInboxListFilter{}
	cursor := strings.TrimPrefix(eventID, "inbox-page:")
	if cursor == "" {
		return filter, nil
	}
	var before inboxStreamCursor
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if len(cursor) > 2048 || err != nil || json.Unmarshal(raw, &before) != nil || before.Scope != inboxCursorScope(r) || before.ID == "" {
		return filter, primitives.ErrInvalidCursor
	}
	filter.BeforeCategory, filter.BeforeTrigger, filter.BeforeID = before.Category, before.Trigger, before.ID
	return filter, nil
}
