package server

import (
	"crypto/sha1"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"organization-autorunner-core/internal/primitives"
)

func handleEventsStream(w http.ResponseWriter, r *http.Request, opts handlerOptions) {
	if opts.primitiveStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
		return
	}

	threadID := strings.TrimSpace(r.URL.Query().Get("thread_id"))
	eventTypes := parseEventTypeFilters(r)
	lastEventID := resolveLastEventID(r)

	flusher, ok := prepareSSE(w)
	if !ok {
		writeError(w, http.StatusInternalServerError, "stream_unavailable", "streaming is not supported by this server")
		return
	}

	sent := map[string]struct{}{}
	firstPoll := true
	ticker := time.NewTicker(opts.streamPollInterval)
	defer ticker.Stop()

	for {
		events, err := listEventsForStream(r, opts, threadID, eventTypes)
		if err != nil {
			writeSSEErrorEvent(w, flusher, "internal_error", "failed to load events for stream")
			return
		}

		if firstPoll {
			afterEvents, resumeSeen := eventsAfterIDWithSeen(events, lastEventID)
			for seenID := range resumeSeen {
				sent[seenID] = struct{}{}
			}
			events = afterEvents
			firstPoll = false
		}

		sentAny := false
		for _, event := range events {
			eventID := strings.TrimSpace(anyString(event["id"]))
			if eventID == "" {
				continue
			}
			if _, alreadySent := sent[eventID]; alreadySent {
				continue
			}
			if err := writeSSEEvent(w, eventID, "event", map[string]any{"event": event}); err != nil {
				return
			}
			sent[eventID] = struct{}{}
			sentAny = true
		}

		if !sentAny {
			if _, err := io.WriteString(w, ": keepalive\n\n"); err != nil {
				return
			}
		}
		flusher.Flush()

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
	flusher, ok := prepareSSE(w)
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
			writeSSEErrorEvent(w, flusher, "internal_error", "failed to derive inbox items for stream")
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

			if err := writeSSEEvent(w, record.eventID, "inbox_item", map[string]any{"item": record.data}); err != nil {
				return
			}
			sentAny = true
		}
		lastDigestByItem = currentDigestByItem

		if !sentAny {
			if _, err := io.WriteString(w, ": keepalive\n\n"); err != nil {
				return
			}
		}
		flusher.Flush()

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
		itemID := strings.TrimSpace(anyString(item.Data["id"]))
		if itemID == "" {
			continue
		}

		dataBytes, err := json.Marshal(item.Data)
		if err != nil {
			continue
		}
		sum := sha1.Sum(dataBytes)
		digest := fmt.Sprintf("%x", sum[:8])
		records = append(records, inboxStreamRecord{
			eventID: itemID + "@" + digest,
			itemID:  itemID,
			digest:  digest,
			data:    item.Data,
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

func eventsAfterIDWithSeen(events []map[string]any, lastEventID string) ([]map[string]any, map[string]struct{}) {
	seen := map[string]struct{}{}
	lastEventID = strings.TrimSpace(lastEventID)
	if lastEventID == "" {
		return events, seen
	}
	for index, event := range events {
		if strings.TrimSpace(anyString(event["id"])) == lastEventID {
			for i := 0; i <= index; i++ {
				eventID := strings.TrimSpace(anyString(events[i]["id"]))
				if eventID == "" {
					continue
				}
				seen[eventID] = struct{}{}
			}
			if index+1 >= len(events) {
				return []map[string]any{}, seen
			}
			return events[index+1:], seen
		}
	}
	return events, seen
}

func resolveLastEventID(r *http.Request) string {
	header := strings.TrimSpace(r.Header.Get("Last-Event-ID"))
	if header != "" {
		return header
	}
	return strings.TrimSpace(r.URL.Query().Get("last_event_id"))
}

func prepareSSE(w http.ResponseWriter) (http.Flusher, bool) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		return nil, false
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()
	return flusher, true
}

func writeSSEEvent(w http.ResponseWriter, eventID string, eventName string, payload any) error {
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

func writeSSEErrorEvent(w http.ResponseWriter, flusher http.Flusher, code string, message string) {
	_ = writeSSEEvent(w, "", "error", map[string]any{
		"error": map[string]any{
			"code":    code,
			"message": message,
		},
	})
	flusher.Flush()
}
