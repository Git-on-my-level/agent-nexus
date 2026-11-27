// HTTP handlers for changing the visibility of many events or documents in
// one request (POST /events/lifecycle and POST /docs/lifecycle). The web UI's
// bulk archive, trash, restore and unarchive send one request per chunk
// instead of one per selected record.
//
// Each item runs through the single-item handler unchanged, with its id in
// the path, so authorization, replay by request_key and the replay hash are
// exactly those of POST /events/{id}/{action} or /docs/{id}/{action}: a retry
// through either route replays instead of conflicting. On a store that can
// run a batch, every item joins one write transaction as its own savepoint:
// the batch waits for the write lock once and commits once, and an item that
// fails rolls back only itself.

package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// lifecycleBatchMaxItems bounds one request; clients chunk larger selections.
const lifecycleBatchMaxItems = 100

// lifecycleBatchBudget stops starting new items once a request has run this
// long. The whole batch holds the workspace write lock, so the budget is
// shorter than respond-many's. Items not started are reported as deferred and
// are safe to send again. A variable only so tests can force the deferred path.
var lifecycleBatchBudget = 2 * time.Second

var lifecycleBatchActions = map[string]bool{"archive": true, "unarchive": true, "trash": true, "restore": true}

// Lifecycle batch result statuses.
const (
	lifecycleBatchDone     = "done"
	lifecycleBatchFailed   = "failed"
	lifecycleBatchDeferred = "deferred"
)

type lifecycleBatchItem struct {
	id     string
	action string
	body   []byte
}

type lifecycleBatchHandler func(http.ResponseWriter, *http.Request, handlerOptions, string, string)

func registerLifecycleBatchRoutes(registerRoute func(string, routeAccessClassifier, http.HandlerFunc), opts handlerOptions) {
	// Archive and trash are recovery mutations, as on the single routes; a
	// read-only workspace refuses unarchive and restore items one by one.
	classify := exactRouteAccess(routeAccessWorkspaceBusiness, routeMutationRecovery, http.MethodPost)
	registerRoute("/events/lifecycle", classify, func(w http.ResponseWriter, r *http.Request) {
		handleLifecycleBatch(w, r, opts, "/events/", true, handleEventLifecycle)
	})
	registerRoute("/docs/lifecycle", classify, func(w http.ResponseWriter, r *http.Request) {
		handleLifecycleBatch(w, r, opts, "/docs/", false, handleDocumentLifecycle)
	})
}

func handleLifecycleBatch(w http.ResponseWriter, r *http.Request, opts handlerOptions, prefix string, events bool, handle lifecycleBatchHandler) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only POST is supported")
		return
	}
	if opts.primitiveStore == nil {
		writeError(w, http.StatusServiceUnavailable, "primitives_unavailable", "primitives store is not configured")
		return
	}
	var req struct {
		Items []map[string]json.RawMessage `json:"items"`
		// SelectedBases (events only) applies to every item that has none of
		// its own, so a bulk selection's bases are sent once, not per item.
		SelectedBases json.RawMessage `json:"selected_bases,omitempty"`
	}
	if !decodeJSONBodySyntax(w, r, &req) {
		return
	}
	items, ok := parseLifecycleBatchItems(w, req.Items, req.SelectedBases, events)
	if !ok {
		return
	}

	// Items never notify the projection maintainer inside the transaction,
	// where it would read state that may not commit; the batch notifies once.
	itemOpts := opts
	itemOpts.projectionMaintainer = nil
	results := make([]map[string]any, len(items))
	changed := false
	run := func(ctx context.Context) error {
		started := time.Now()
		// The route middleware charged the write budget for the first item;
		// every further item costs one more, so a batch never writes more
		// than the same items sent one by one could.
		limited := false
		scope := routeRateLimitScopeForRequest(r)
		for index, item := range items {
			if index > 0 && !limited && opts.rateLimiter != nil {
				allowed, _ := opts.rateLimiter.allow("write", scope, time.Now().UTC())
				limited = !allowed
			}
			if limited || ctx.Err() != nil || (index > 0 && time.Since(started) >= lifecycleBatchBudget) {
				results[index] = map[string]any{"index": index, "status": lifecycleBatchDeferred}
				continue
			}
			status, body := callLifecycleBatchItem(ctx, r, itemOpts, prefix, item, handle)
			result := map[string]any{"index": index, "http_status": status}
			if status >= 200 && status < 300 {
				result["status"] = lifecycleBatchDone
				result["response"] = body
				changed = true
			} else {
				result["status"] = lifecycleBatchFailed
				result["error"] = body["error"]
			}
			results[index] = result
		}
		return nil
	}
	if err := runInStoreBatch(r.Context(), opts, run); err != nil {
		if writePrimitiveQuotaViolationError(w, err) {
			return
		}
		// Nothing committed, so every item is safe to send again.
		w.Header().Set("Retry-After", "1")
		writeError(w, http.StatusServiceUnavailable, "storage_unavailable", "the batch was not saved; send it again")
		return
	}
	if changed {
		notifyProjectionMaintainer(opts)
	}
	writeJSON(w, http.StatusOK, map[string]any{"results": results})
}

// parseLifecycleBatchItems validates the batch shape and builds each item's
// single-route body: every field but id and action, exactly as sent.
func parseLifecycleBatchItems(w http.ResponseWriter, raw []map[string]json.RawMessage, selectedBases json.RawMessage, events bool) ([]lifecycleBatchItem, bool) {
	if len(raw) == 0 {
		writeError(w, http.StatusBadRequest, "invalid_request", "items must contain at least one item")
		return nil, false
	}
	if len(raw) > lifecycleBatchMaxItems {
		writeError(w, http.StatusBadRequest, "invalid_request", "items must contain at most 100 items; send larger selections in chunks")
		return nil, false
	}
	shared := len(selectedBases) > 0 && string(selectedBases) != "null"
	if shared && !events {
		writeError(w, http.StatusBadRequest, "invalid_request", "selected_bases applies only to events")
		return nil, false
	}
	items := make([]lifecycleBatchItem, 0, len(raw))
	seen := make(map[string]bool, len(raw))
	for _, fields := range raw {
		var id, action string
		if json.Unmarshal(fields["id"], &id) != nil || json.Unmarshal(fields["action"], &action) != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "every item needs a string id and action")
			return nil, false
		}
		id = strings.TrimSpace(id)
		if id == "" || strings.Contains(id, "/") {
			writeError(w, http.StatusBadRequest, "invalid_request", "every item needs an id")
			return nil, false
		}
		if !lifecycleBatchActions[action] {
			writeError(w, http.StatusBadRequest, "invalid_request", "action must be archive, unarchive, trash or restore")
			return nil, false
		}
		if seen[id] {
			writeError(w, http.StatusBadRequest, "invalid_request", "each id may appear once")
			return nil, false
		}
		seen[id] = true
		delete(fields, "id")
		delete(fields, "action")
		if _, own := fields["selected_bases"]; shared && !own {
			fields["selected_bases"] = selectedBases
		}
		body, err := json.Marshal(fields)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "item body must be a JSON object")
			return nil, false
		}
		items = append(items, lifecycleBatchItem{id: id, action: action, body: body})
	}
	return items, true
}

// callLifecycleBatchItem runs one item through the single-item handler on
// the batch context, with the id in the path as on the single route.
func callLifecycleBatchItem(ctx context.Context, r *http.Request, opts handlerOptions, prefix string, item lifecycleBatchItem, handle lifecycleBatchHandler) (int, map[string]any) {
	rec := &bufferedResponse{header: http.Header{}}
	// A read-only workspace admits archive and trash (they reclaim space) but
	// refuses unarchive and restore, as it does on the single routes.
	if item.action == "unarchive" || item.action == "restore" {
		if !enforceWorkspaceWriteAccess(rec, opts, routeAccessRequirement{supported: true, mutation: routeMutationBusiness}) {
			return bufferedItemResult(rec)
		}
	}
	sub := r.Clone(ctx)
	sub.URL = &url.URL{Path: prefix + item.id + "/" + item.action}
	sub.RequestURI = ""
	sub.Body = readCloser{bytes.NewReader(item.body)}
	sub.ContentLength = int64(len(item.body))
	handle(rec, sub, opts, item.id, item.action)
	return bufferedItemResult(rec)
}

func bufferedItemResult(rec *bufferedResponse) (int, map[string]any) {
	status := rec.status
	if status == 0 {
		status = http.StatusOK
	}
	body := map[string]any{}
	if len(rec.body.Bytes()) > 0 {
		if err := json.Unmarshal(rec.body.Bytes(), &body); err != nil {
			body = map[string]any{"error": errorPayload("internal_error", "unreadable item response")}
		}
	}
	return status, body
}
