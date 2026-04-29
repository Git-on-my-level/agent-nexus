package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/schema"
)

type completedPageCursor struct {
	TS string `json:"ts"`
	ID string `json:"id"`
}

func encodeCompletedPageCursor(ts, id string) string {
	raw, err := json.Marshal(completedPageCursor{TS: ts, ID: id})
	if err != nil {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}

func decodeCompletedPageCursor(raw string) (ts string, id string, ok bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", "", false
	}
	buf, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return "", "", false
	}
	var cur completedPageCursor
	if err := json.Unmarshal(buf, &cur); err != nil {
		return "", "", false
	}
	cur.TS = strings.TrimSpace(cur.TS)
	cur.ID = strings.TrimSpace(cur.ID)
	if cur.TS == "" || cur.ID == "" {
		return "", "", false
	}
	return cur.TS, cur.ID, true
}

func resolveHumanAttentionRequestEvent(ctx context.Context, store PrimitiveStore, respPayload map[string]any) map[string]any {
	if respPayload == nil {
		return nil
	}
	ref := strings.TrimSpace(anyString(respPayload["request_event_ref"]))
	if ref == "" {
		return nil
	}
	prefix, id, err := schema.SplitTypedRef(ref)
	if err != nil || prefix != "event" || strings.TrimSpace(id) == "" {
		return nil
	}
	ev, err := store.GetEvent(ctx, id)
	if err != nil || ev == nil {
		return nil
	}
	if strings.TrimSpace(anyString(ev["type"])) != humanAttentionRequestedEventType {
		return nil
	}
	return ev
}

func completedInboxItemPayload(ctx context.Context, store PrimitiveStore, respEvt map[string]any, reqEvtOptional map[string]any) map[string]any {
	respID := strings.TrimSpace(anyString(respEvt["id"]))
	threadID := strings.TrimSpace(anyString(respEvt["thread_id"]))
	payload, _ := respEvt["payload"].(map[string]any)
	inboxItemID := strings.TrimSpace(anyString(payload["inbox_item_id"]))
	kind := canonicalHumanAttentionKind(anyString(payload["kind"]))
	if kind == "" {
		kind = "unknown"
	}
	responseText := strings.TrimSpace(anyString(payload["response_text"]))
	summary := strings.TrimSpace(anyString(respEvt["summary"]))
	subjectRef := strings.TrimSpace(anyString(payload["subject_ref"]))
	requestTitle := ""
	requestBody := ""
	var proposals []any
	requestMissing := reqEvtOptional == nil

	if reqEvtOptional != nil {
		requestMissing = false
		reqPayload, _ := reqEvtOptional["payload"].(map[string]any)
		requestTitle = strings.TrimSpace(anyString(reqPayload["title"]))
		requestBody = strings.TrimSpace(anyString(reqPayload["body"]))
		if rp, ok := reqPayload["response_proposals"]; ok && rp != nil {
			if slice, ok := rp.([]any); ok {
				proposals = slice
			}
		}
	}

	title := requestTitle
	if title == "" {
		title = summary
	}
	if title == "" && responseText != "" {
		title = truncateHead(responseText, 120)
	}
	if title == "" {
		title = "Human response recorded"
	}

	relatedRefs := extractRelatedRefsFromCompletedPayload(payload, respEvt)

	if proposals == nil {
		proposals = []any{}
	}

	row := map[string]any{
		"id":                       "completed:" + respID,
		"status":                   "completed",
		"inbox_item_id":            inboxItemID,
		"kind":                     kind,
		"thread_id":                threadID,
		"subject_ref":              subjectRef,
		"title":                    title,
		"body":                     requestBody,
		"related_refs":             typedRefStringsToAnyList(relatedRefs),
		"response_proposals":       proposals,
		"request_event_ref":        strings.TrimSpace(anyString(payload["request_event_ref"])),
		"response_event_ref":       "event:" + respID,
		"response_text":            responseText,
		"response_summary":         summary,
		"responded_at":             respEvt["ts"],
		"responding_actor_id":      strings.TrimSpace(anyString(payload["responding_actor_id"])),
		"requester_actor_id":       strings.TrimSpace(anyString(payload["requester_actor_id"])),
		"requester_agent_id":       strings.TrimSpace(anyString(payload["requester_agent_id"])),
		"requester_label":          strings.TrimSpace(anyString(payload["requester_label"])),
		"original_request_missing": requestMissing,
	}
	return row
}

func truncateHead(s string, max int) string {
	s = strings.TrimSpace(s)
	if max <= 0 || len(s) <= max {
		return s
	}
	return strings.TrimSpace(s[:max]) + "…"
}

func extractRelatedRefsFromCompletedPayload(payload map[string]any, respEvt map[string]any) []string {
	var merged []string
	if payload != nil {
		if rr, err := extractStringSlice(payload["related_refs"]); err == nil {
			for _, r := range rr {
				merged = append(merged, strings.TrimSpace(r))
			}
		}
	}
	if refs, err := extractStringSlice(respEvt["refs"]); err == nil {
		for _, r := range refs {
			merged = append(merged, strings.TrimSpace(r))
		}
	}
	return mergeUniqueSortedRefs(merged...)
}

func parseCompletedInboxLimit(rawQuery string, defaultLimit int, maxLimit int) int {
	rawQuery = strings.TrimSpace(rawQuery)
	if rawQuery == "" {
		return defaultLimit
	}
	v, err := strconv.Atoi(rawQuery)
	if err != nil || v <= 0 {
		return defaultLimit
	}
	if v > maxLimit {
		return maxLimit
	}
	return v
}

func parseWindowDays(raw string, defaultDays int) (days int, ok bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return defaultDays, true
	}
	v, err := strconv.Atoi(raw)
	if err != nil || v < 0 {
		return 0, false
	}
	return v, true
}

func completedSinceRFC3339Nano(now time.Time, windowDays int) string {
	if windowDays <= 0 {
		return ""
	}
	return now.Add(-time.Duration(windowDays) * 24 * time.Hour).UTC().Format(time.RFC3339Nano)
}

func normalizeCompletedKindFilter(raw string) string {
	raw = strings.TrimSpace(strings.ToLower(raw))
	switch raw {
	case "", "all":
		return ""
	case "ask", "review", "escalate", "unknown":
		return raw
	default:
		return "__invalid__"
	}
}

func handleGetCompletedInbox(w http.ResponseWriter, r *http.Request, opts handlerOptions, now time.Time) {
	if opts.primitiveStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
		return
	}

	requestedLimit := parseCompletedInboxLimit(r.URL.Query().Get("limit"), 50, 100)
	probeLimit := requestedLimit + 1

	windowDays, windowOk := parseWindowDays(r.URL.Query().Get("window_days"), 30)
	if !windowOk {
		writeError(w, http.StatusBadRequest, "invalid_request", "window_days must be a non-negative integer")
		return
	}

	kindFilter := normalizeCompletedKindFilter(r.URL.Query().Get("completed_kind"))
	if kindFilter == "__invalid__" {
		writeError(w, http.StatusBadRequest, "invalid_request", "completed_kind must be one of ask, review, escalate, unknown, or all")
		return
	}

	var cursorTS, cursorID string
	if rawCur := strings.TrimSpace(r.URL.Query().Get("cursor")); rawCur != "" {
		ts, id, ok := decodeCompletedPageCursor(rawCur)
		if !ok {
			writeError(w, http.StatusBadRequest, "invalid_request", "cursor is invalid")
			return
		}
		cursorTS = ts
		cursorID = id
	}

	params := primitives.HumanAttentionRespondedPageParams{
		CursorTS:         cursorTS,
		CursorID:         cursorID,
		Limit:            probeLimit,
		KindFilter:       kindFilter,
		SinceRFC3339Nano: completedSinceRFC3339Nano(now, windowDays),
	}

	events, err := opts.primitiveStore.ListHumanAttentionRespondedPage(r.Context(), params)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to load completed inbox history")
		return
	}

	hasMore := len(events) > requestedLimit
	if hasMore {
		events = events[:requestedLimit]
	}

	payloadItems := make([]map[string]any, 0, len(events))
	for _, evt := range events {
		payload, _ := evt["payload"].(map[string]any)
		reqEvt := resolveHumanAttentionRequestEvent(r.Context(), opts.primitiveStore, payload)
		payloadItems = append(payloadItems, completedInboxItemPayload(r.Context(), opts.primitiveStore, evt, reqEvt))
	}

	resp := map[string]any{
		"status":       "completed",
		"items":        payloadItems,
		"generated_at": now.Format(time.RFC3339Nano),
	}
	if hasMore && len(events) > 0 {
		last := events[len(events)-1]
		next := encodeCompletedPageCursor(strings.TrimSpace(anyString(last["ts"])), strings.TrimSpace(anyString(last["id"])))
		if next != "" {
			resp["next_cursor"] = next
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

func handleGetCompletedInboxItem(w http.ResponseWriter, r *http.Request, opts handlerOptions, inboxItemID string, now time.Time) {
	if opts.primitiveStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
		return
	}

	respEventID := strings.TrimPrefix(strings.TrimSpace(inboxItemID), "completed:")
	respEventID = strings.TrimSpace(respEventID)
	if respEventID == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "completed inbox id requires response event id")
		return
	}

	ev, err := opts.primitiveStore.GetEvent(r.Context(), respEventID)
	if err != nil || ev == nil {
		writeError(w, http.StatusNotFound, "not_found", "completed inbox item not found")
		return
	}
	if strings.TrimSpace(anyString(ev["type"])) != humanAttentionRespondedEventType {
		writeError(w, http.StatusNotFound, "not_found", "completed inbox item not found")
		return
	}
	if strings.TrimSpace(anyString(ev["trashed_at"])) != "" {
		writeError(w, http.StatusNotFound, "not_found", "completed inbox item not found")
		return
	}

	payload, _ := ev["payload"].(map[string]any)
	reqEvt := resolveHumanAttentionRequestEvent(r.Context(), opts.primitiveStore, payload)
	item := completedInboxItemPayload(r.Context(), opts.primitiveStore, ev, reqEvt)

	writeJSON(w, http.StatusOK, map[string]any{
		"item":         item,
		"generated_at": now.Format(time.RFC3339Nano),
	})
}
