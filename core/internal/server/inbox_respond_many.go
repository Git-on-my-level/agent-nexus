// HTTP handler for responding to many inbox items in one request
// (POST /inbox/responses). Decide's bulk Skip uses it so clearing a backlog is
// one request per chunk instead of one per decision.
//
// Each item runs through handleRespondInboxItem unchanged, so authorization,
// replay by idempotency key, ask batches, access requests and requester wake
// behave exactly as they do for POST /inbox/{inbox_id}/respond. Items are
// independent: one failing never rolls back another, and the response
// reports each item's own status and body.

package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	// respondManyMaxItems bounds one request; clients chunk larger selections.
	respondManyMaxItems = 100
	// respondManyBudget stops starting new items once a request has run this
	// long, so a slow store never pushes a request past proxy timeouts. Items
	// not started are reported as deferred and are safe to send again.
	respondManyBudget = 15 * time.Second
)

// respondManyItem is one item's request: the single-item respond body plus
// the item id and an optional quiet fallback key.
type respondManyItem struct {
	inboxRespondRequest
	// QuietIdempotencyKey, when set, lets an item whose requester cannot be
	// woken (notification_target_required) be recorded without a wake under
	// this separate key, as Decide's single skip does client-side.
	QuietIdempotencyKey string `json:"quiet_idempotency_key,omitempty"`
}

type respondManyRequest struct {
	Items []respondManyItem `json:"items"`
}

// respondManyResult statuses.
const (
	respondManyResponded = "responded"
	respondManyClosed    = "closed"
	respondManyFailed    = "failed"
	respondManyDeferred  = "deferred"
)

// closedElsewhereMessages are core's conflicts for an item that was already
// closed some other way: nothing is left to respond to.
var closedElsewhereMessages = map[string]bool{
	"Ask is no longer open":                    true,
	"this ask batch has already been answered": true,
}

func handleRespondInboxItems(w http.ResponseWriter, r *http.Request, opts handlerOptions) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "only POST is supported")
		return
	}
	if _, ok := requireHumanPrincipal(w, r, opts); !ok {
		return
	}
	var req respondManyRequest
	if !decodeJSONBodySyntax(w, r, &req) {
		return
	}
	if len(req.Items) == 0 {
		writeError(w, http.StatusBadRequest, "invalid_request", "items must contain at least one item")
		return
	}
	if len(req.Items) > respondManyMaxItems {
		writeError(w, http.StatusBadRequest, "invalid_request", "items must contain at most 100 items; send larger selections in chunks")
		return
	}
	seen := make(map[string]bool, len(req.Items))
	for _, item := range req.Items {
		id := strings.TrimSpace(item.InboxItemID)
		if id == "" || strings.Contains(id, "/") {
			writeError(w, http.StatusBadRequest, "invalid_request", "every item needs an inbox_item_id")
			return
		}
		if seen[id] {
			writeError(w, http.StatusBadRequest, "invalid_request", "each inbox_item_id may appear once")
			return
		}
		seen[id] = true
	}

	started := time.Now()
	results := make([]map[string]any, 0, len(req.Items))
	// Results follow request order and carry the item's index, never its id,
	// so a response discloses nothing the caller could not already see.
	for index, item := range req.Items {
		id := strings.TrimSpace(item.InboxItemID)
		if r.Context().Err() != nil || time.Since(started) >= respondManyBudget {
			results = append(results, map[string]any{"index": index, "status": respondManyDeferred})
			continue
		}
		result := respondOneOfMany(r, opts, id, item)
		result["index"] = index
		results = append(results, result)
	}
	writeJSON(w, http.StatusOK, map[string]any{"results": results})
}

// respondOneOfMany answers one item through the single-item handler and
// classifies its outcome.
func respondOneOfMany(r *http.Request, opts handlerOptions, id string, item respondManyItem) map[string]any {
	single := item.inboxRespondRequest
	single.InboxItemID = id
	status, body := callRespondInboxItem(r, opts, id, single)
	if errorCode(body) == "notification_target_required" {
		if quiet := strings.TrimSpace(item.QuietIdempotencyKey); quiet != "" && single.NotifyMode != "none" {
			single.NotifyMode = "none"
			single.NotifyTargetActorID = ""
			single.NotifyTargetAgentID = ""
			single.IdempotencyKey = quiet
			status, body = callRespondInboxItem(r, opts, id, single)
		}
	}
	result := map[string]any{"http_status": status}
	switch {
	case status >= 200 && status < 300:
		result["status"] = respondManyResponded
		result["response"] = body
	case status == http.StatusConflict && errorCode(body) == "conflict" && closedElsewhereMessages[errorMessage(body)]:
		result["status"] = respondManyClosed
		result["error"] = body["error"]
	default:
		result["status"] = respondManyFailed
		result["error"] = body["error"]
	}
	return result
}

func callRespondInboxItem(r *http.Request, opts handlerOptions, id string, single inboxRespondRequest) (int, map[string]any) {
	raw, err := json.Marshal(single)
	if err != nil {
		return http.StatusInternalServerError, map[string]any{"error": errorPayload("internal_error", "failed to encode item")}
	}
	sub := r.Clone(r.Context())
	sub.URL = &url.URL{Path: "/inbox/" + id + "/respond"}
	sub.RequestURI = ""
	sub.Body = readCloser{bytes.NewReader(raw)}
	sub.ContentLength = int64(len(raw))
	rec := &bufferedResponse{header: http.Header{}}
	handleRespondInboxItem(rec, sub, opts, id)
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

type readCloser struct{ *bytes.Reader }

func (readCloser) Close() error { return nil }

// bufferedResponse captures one item's response inside a batch request.
type bufferedResponse struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func (b *bufferedResponse) Header() http.Header { return b.header }
func (b *bufferedResponse) Write(p []byte) (int, error) {
	if b.status == 0 {
		b.status = http.StatusOK
	}
	return b.body.Write(p)
}
func (b *bufferedResponse) WriteHeader(status int) {
	if b.status == 0 {
		b.status = status
	}
}

func errorCode(body map[string]any) string {
	errBody, _ := body["error"].(map[string]any)
	return strings.TrimSpace(anyString(errBody["code"]))
}

func errorMessage(body map[string]any) string {
	errBody, _ := body["error"].(map[string]any)
	return strings.TrimSpace(anyString(errBody["message"]))
}
