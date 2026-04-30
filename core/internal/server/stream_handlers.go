package server

import (
	"crypto/sha1"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"agent-nexus-core/internal/actors"
	"agent-nexus-core/internal/primitives"
)

const sseWriteTimeout = 5 * time.Second

func handleListEvents(w http.ResponseWriter, r *http.Request, opts handlerOptions) {
	if opts.primitiveStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
		return
	}
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only GET is supported")
		return
	}

	threadID := strings.TrimSpace(r.URL.Query().Get("thread_id"))
	eventTypes := parseEventTypeFilters(r)
	query := r.URL.Query()
	limit, ok := parseOptionalPositiveInt(w, query.Get("limit"), 50, 200, "limit")
	if !ok {
		return
	}
	actorKind := strings.TrimSpace(query.Get("actor_kind"))
	actorIDs, ok := eventActorIDsForKind(w, r, opts, actorKind)
	if !ok {
		return
	}
	page, err := opts.primitiveStore.ListEventsPage(r.Context(), primitives.EventListFilter{
		Types:     eventTypes,
		Preset:    strings.TrimSpace(query.Get("preset")),
		TopicID:   strings.TrimSpace(query.Get("topic_id")),
		ThreadID:  threadID,
		ActorID:   strings.TrimSpace(query.Get("actor_id")),
		ActorIDs:  actorIDs,
		ActorKind: actorKind,
		Query:     strings.TrimSpace(query.Get("q")),
		Since:     strings.TrimSpace(query.Get("since")),
		Until:     strings.TrimSpace(query.Get("until")),
		Limit:     limit,
		Cursor:    strings.TrimSpace(query.Get("cursor")),
	})
	if err != nil {
		if errors.Is(err, primitives.ErrInvalidCursor) {
			writeError(w, http.StatusBadRequest, "invalid_request", "cursor is invalid")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to list events")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"events": page.Events,
		"page_info": map[string]any{
			"next_cursor": page.NextCursor,
			"has_more":    page.NextCursor != "",
		},
	})
}

func eventActorIDsForKind(w http.ResponseWriter, r *http.Request, opts handlerOptions, actorKind string) ([]string, bool) {
	actorKind = strings.TrimSpace(strings.ToLower(actorKind))
	if actorKind == "" {
		return nil, true
	}
	if opts.actorRegistry == nil {
		writeError(w, http.StatusServiceUnavailable, "actor_registry_unavailable", "actor registry is not configured")
		return nil, false
	}
	pageLimit := 1000
	ids := make([]string, 0)
	seen := map[string]struct{}{}
	for cursor := ""; ; {
		actorList, next, err := opts.actorRegistry.List(r.Context(), actors.ActorListFilter{
			Limit:  &pageLimit,
			Cursor: cursor,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal_error", "failed to filter events by actor kind")
			return nil, false
		}
		for _, actor := range actorList {
			for _, tag := range actor.Tags {
				if strings.EqualFold(strings.TrimSpace(tag), actorKind) {
					if _, ok := seen[actor.ID]; !ok {
						seen[actor.ID] = struct{}{}
						ids = append(ids, actor.ID)
					}
					break
				}
			}
		}
		if next == "" {
			break
		}
		cursor = next
	}
	if len(ids) == 0 {
		ids = []string{"__anx_no_actor_kind_match__"}
	}
	return ids, true
}

func handleEventsStream(w http.ResponseWriter, r *http.Request, opts handlerOptions) {
	if opts.primitiveStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
		return
	}

	threadID := strings.TrimSpace(r.URL.Query().Get("thread_id"))
	eventTypes := parseEventTypeFilters(r)
	lastEventID := resolveLastEventID(r)

	controller, flusher, ok := prepareSSE(w)
	if !ok {
		writeError(w, http.StatusInternalServerError, "stream_unavailable", "streaming is not supported by this server")
		return
	}

	cursorEventID := lastEventID
	ticker := time.NewTicker(opts.streamPollInterval)
	defer ticker.Stop()

	for {
		events, err := listEventsForStream(r, opts, threadID, eventTypes)
		if err != nil {
			writeSSEErrorEvent(controller, w, flusher, "internal_error", "failed to load events for stream")
			return
		}

		events = eventsAfterID(events, cursorEventID)

		sentAny := false
		for _, event := range events {
			eventID := strings.TrimSpace(anyString(event["id"]))
			if eventID == "" {
				continue
			}
			if err := writeSSEEvent(controller, w, eventID, "event", map[string]any{"event": event}); err != nil {
				clearSSEWriteDeadline(controller)
				return
			}
			cursorEventID = eventID
			sentAny = true
		}

		if !sentAny {
			if err := writeSSEKeepalive(controller, w); err != nil {
				clearSSEWriteDeadline(controller)
				return
			}
		}
		flushSSE(controller, flusher)

		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
		}
	}
}

func handleInboxStream(w http.ResponseWriter, r *http.Request, opts handlerOptions) {
	if opts.primitiveStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
		return
	}

	horizon := opts.inboxRiskHorizon
	if horizon <= 0 {
		horizon = defaultInboxRiskHorizon
	}

	if rawDays := strings.TrimSpace(r.URL.Query().Get("risk_horizon_days")); rawDays != "" {
		days, err := strconv.Atoi(rawDays)
		if err != nil || days < 0 {
			writeError(w, http.StatusBadRequest, "invalid_request", "risk_horizon_days must be a non-negative integer")
			return
		}
		horizon = time.Duration(days) * 24 * time.Hour
	}

	lastEventID := resolveLastEventID(r)
	controller, flusher, ok := prepareSSE(w)
	if !ok {
		writeError(w, http.StatusInternalServerError, "stream_unavailable", "streaming is not supported by this server")
		return
	}

	lastDigestByItem := map[string]string{}
	firstPoll := true
	ticker := time.NewTicker(opts.streamPollInterval)
	defer ticker.Stop()

	for {
		items, err := deriveInboxItemsNoStaleEmission(r.Context(), opts, time.Now().UTC(), horizon)
		if err != nil {
			writeSSEErrorEvent(controller, w, flusher, "internal_error", "failed to derive inbox items for stream")
			return
		}
		allRecords := buildInboxStreamRecords(items)
		records := allRecords
		if firstPoll {
			records = inboxRecordsAfterID(records, lastEventID)
			firstPoll = false
		}

		sentAny := false
		currentDigestByItem := make(map[string]string, len(allRecords))
		for _, record := range allRecords {
			currentDigestByItem[record.itemID] = record.digest
		}
		for _, record := range records {
			previousDigest, seen := lastDigestByItem[record.itemID]
			if seen && previousDigest == record.digest {
				continue
			}

			if err := writeSSEEvent(controller, w, record.eventID, "inbox_item", map[string]any{"item": record.data}); err != nil {
				clearSSEWriteDeadline(controller)
				return
			}
			sentAny = true
		}
		lastDigestByItem = currentDigestByItem

		if !sentAny {
			if err := writeSSEKeepalive(controller, w); err != nil {
				clearSSEWriteDeadline(controller)
				return
			}
		}
		flushSSE(controller, flusher)

		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
		}
	}
}

type inboxStreamRecord struct {
	eventID string
	itemID  string
	digest  string
	data    map[string]any
}

func buildInboxStreamRecords(items []derivedInboxItem) []inboxStreamRecord {
	records := make([]inboxStreamRecord, 0, len(items))
	for _, item := range items {
		payload := payloadFromLocalDerivedInboxItem(item)
		itemID := strings.TrimSpace(anyString(payload["id"]))
		if itemID == "" {
			continue
		}

		dataBytes, err := json.Marshal(payload)
		if err != nil {
			continue
		}
		sum := sha1.Sum(dataBytes)
		digest := fmt.Sprintf("%x", sum[:8])
		records = append(records, inboxStreamRecord{
			eventID: itemID + "@" + digest,
			itemID:  itemID,
			digest:  digest,
			data:    payload,
		})
	}
	return records
}

func inboxRecordsAfterID(records []inboxStreamRecord, lastEventID string) []inboxStreamRecord {
	lastEventID = strings.TrimSpace(lastEventID)
	if lastEventID == "" {
		return records
	}
	for index, record := range records {
		if record.eventID == lastEventID {
			if index+1 >= len(records) {
				return []inboxStreamRecord{}
			}
			return records[index+1:]
		}
	}
	return records
}

func listEventsForStream(r *http.Request, opts handlerOptions, threadID string, eventTypes []string) ([]map[string]any, error) {
	if threadID != "" {
		events, err := opts.primitiveStore.ListEventsByThread(r.Context(), threadID)
		if err != nil {
			return nil, err
		}
		if len(eventTypes) > 0 {
			filtered := make([]map[string]any, 0, len(events))
			eventTypeSet := map[string]struct{}{}
			for _, eventType := range eventTypes {
				eventTypeSet[eventType] = struct{}{}
			}
			for _, event := range events {
				if _, ok := eventTypeSet[strings.TrimSpace(anyString(event["type"]))]; ok {
					filtered = append(filtered, event)
				}
			}
			events = filtered
		}
		sortEventsAscending(events)
		return events, nil
	}

	events, err := opts.primitiveStore.ListEvents(r.Context(), primitives.EventListFilter{Types: eventTypes})
	if err != nil {
		return nil, err
	}
	sortEventsAscending(events)
	return events, nil
}

func parseEventTypeFilters(r *http.Request) []string {
	values := r.URL.Query()
	out := make([]string, 0)
	for _, raw := range values["type"] {
		out = append(out, splitCommaSeparated(raw)...)
	}
	for _, raw := range values["types"] {
		out = append(out, splitCommaSeparated(raw)...)
	}
	return uniqueNonEmptyStrings(out)
}

func parseOptionalPositiveInt(w http.ResponseWriter, raw string, defaultValue int, maxValue int, field string) (int, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return defaultValue, true
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		writeError(w, http.StatusBadRequest, "invalid_request", field+" must be a positive integer")
		return 0, false
	}
	if maxValue > 0 && value > maxValue {
		value = maxValue
	}
	return value, true
}

func splitCommaSeparated(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		out = append(out, part)
	}
	return out
}

func sortEventsAscending(events []map[string]any) {
	sort.Slice(events, func(i, j int) bool {
		leftTS, leftHasTS := parseTimestamp(events[i]["ts"])
		rightTS, rightHasTS := parseTimestamp(events[j]["ts"])
		switch {
		case leftHasTS && rightHasTS:
			if !leftTS.Equal(rightTS) {
				return leftTS.Before(rightTS)
			}
		case leftHasTS != rightHasTS:
			return leftHasTS
		}
		return anyString(events[i]["id"]) < anyString(events[j]["id"])
	})
}

func eventsAfterID(events []map[string]any, lastEventID string) []map[string]any {
	lastEventID = strings.TrimSpace(lastEventID)
	if lastEventID == "" {
		return events
	}
	for index, event := range events {
		if strings.TrimSpace(anyString(event["id"])) == lastEventID {
			if index+1 >= len(events) {
				return []map[string]any{}
			}
			return events[index+1:]
		}
	}
	return events
}

func resolveLastEventID(r *http.Request) string {
	header := strings.TrimSpace(r.Header.Get("Last-Event-ID"))
	if header != "" {
		return header
	}
	return strings.TrimSpace(r.URL.Query().Get("last_event_id"))
}

func prepareSSE(w http.ResponseWriter) (*http.ResponseController, http.Flusher, bool) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		return nil, nil, false
	}
	controller := http.NewResponseController(w)
	clearSSEWriteDeadline(controller)
	beginSSEWrite(controller)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flushSSE(controller, flusher)
	return controller, flusher, true
}

func writeSSEEvent(controller *http.ResponseController, w http.ResponseWriter, eventID string, eventName string, payload any) error {
	beginSSEWrite(controller)
	if strings.TrimSpace(eventID) != "" {
		if _, err := io.WriteString(w, "id: "+eventID+"\n"); err != nil {
			return err
		}
	}
	if strings.TrimSpace(eventName) != "" {
		if _, err := io.WriteString(w, "event: "+eventName+"\n"); err != nil {
			return err
		}
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if _, err := io.WriteString(w, "data: "+string(data)+"\n\n"); err != nil {
		return err
	}
	return nil
}

func writeSSEKeepalive(controller *http.ResponseController, w http.ResponseWriter) error {
	beginSSEWrite(controller)
	_, err := io.WriteString(w, ": keepalive\n\n")
	return err
}

func writeSSEErrorEvent(controller *http.ResponseController, w http.ResponseWriter, flusher http.Flusher, code string, message string) {
	errorObj := errorPayload(code, message)
	_ = writeSSEEvent(controller, w, "", "error", map[string]any{
		"error": errorObj,
	})
	flushSSE(controller, flusher)
}

func beginSSEWrite(controller *http.ResponseController) {
	if controller == nil {
		return
	}
	_ = controller.SetWriteDeadline(time.Now().Add(sseWriteTimeout))
}

func clearSSEWriteDeadline(controller *http.ResponseController) {
	if controller == nil {
		return
	}
	_ = controller.SetWriteDeadline(time.Time{})
}

func flushSSE(controller *http.ResponseController, flusher http.Flusher) {
	flusher.Flush()
	clearSSEWriteDeadline(controller)
}
