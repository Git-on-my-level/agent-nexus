// The `anx pm ask` one-shot flow: create a PM conversation, post a question,
// and detect an empty recent conversation.

package app

import (
	"context"
	"net/url"
	"strconv"
	"strings"
	"time"

	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/errnorm"
)

func (a *App) runPMAsk(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, error) {
	fs := newSilentFlagSet("pm ask")
	var wait trackedBool
	var workRef, title, requestKey, conversationID trackedString
	fs.Var(&wait, "wait", "Poll until the turn has a response")
	fs.Var(&workRef, "work-ref", "Optional work ref")
	fs.Var(&title, "title", "Conversation title")
	fs.Var(&requestKey, "request-key", "Stable request key")
	fs.Var(&conversationID, "conversation-id", "Existing conversation id")
	if err := fs.Parse(args); err != nil {
		return nil, errnorm.Usage("invalid_flags", err.Error())
	}
	text := strings.TrimSpace(strings.Join(fs.Args(), " "))
	if text == "" {
		return nil, errnorm.Usage("invalid_request", "a question is required; usage: anx pm ask \"What needs my decision?\"")
	}
	key := strings.TrimSpace(requestKey.value)
	if key == "" {
		key = "ask-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	convTitle := strings.TrimSpace(title.value)
	if convTitle == "" {
		convTitle = text
		if len([]rune(convTitle)) > 80 {
			convTitle = string([]rune(convTitle)[:80])
		}
	}
	convID := strings.TrimSpace(conversationID.value)
	createdConversation := false
	var conversation map[string]any
	if convID == "" {
		if reused := a.recentEmptyAskConversation(ctx, cfg, strings.TrimSpace(workRef.value), convTitle); reused != "" {
			convID = reused
		} else {
			body := map[string]any{"request_key": key + "-conversation", "title": convTitle}
			if strings.TrimSpace(workRef.value) != "" {
				body["work_ref"] = strings.TrimSpace(workRef.value)
			}
			created, err := a.invokeRawJSON(ctx, cfg, "pm conversations create", "POST", "/pm/conversations", body)
			if err != nil {
				return created, err
			}
			conversation = commandResultBody(created)
			convID = anyString(conversation["id"])
			if convID == "" {
				return nil, errnorm.New(errnorm.KindRemote, "invalid_response", "conversation create did not return an id")
			}
			createdConversation = true
		}
	}
	posted, err := a.invokeRawJSON(ctx, cfg, "pm conversations message", "POST", "/pm/conversations/"+url.PathEscape(convID)+"/messages", map[string]any{
		"request_key": key,
		"text":        text,
	})
	if err != nil {
		if createdConversation || conversationID.value == "" {
			annotatePMAskSendFailure(err, convID)
		}
		return posted, err
	}
	turn := commandResultBody(posted)
	if wait.value {
		deadline := parseTurnDeadline(turn)
		for time.Now().Before(deadline) {
			got, getErr := a.invokeRawJSON(ctx, cfg, "pm conversations get", "GET", "/pm/conversations/"+url.PathEscape(convID), nil)
			if getErr != nil {
				return got, getErr
			}
			detail := commandResultBody(got)
			if updated := turnByID(detail, anyString(turn["id"])); updated != nil {
				turn = updated
				if status := anyString(turn["status"]); status == "delivered" || status == "failed" {
					break
				}
				if resp := anyString(turn["response"]); resp != "" {
					break
				}
			}
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(2 * time.Second):
			}
		}
	}
	data := map[string]any{"conversation_id": convID, "turn": turn, "conversation": conversation}
	lines := []string{
		"conversation: " + convID,
		"turn: " + anyString(turn["id"]),
		"status: " + renderPMTurnStatus(turn),
	}
	if resp := anyString(turn["response"]); resp != "" {
		lines = append(lines, "", resp)
	} else if failure := anyString(turn["failure"]); failure != "" {
		lines = append(lines, "failure: "+failure)
	}
	return &commandResult{Data: data, Text: strings.Join(lines, "\n")}, nil
}

func annotatePMAskSendFailure(err error, convID string) {
	errnorm.AnnotateDetail(err, "conversation_id", convID)
}

func (a *App) recentEmptyAskConversation(ctx context.Context, cfg config.Resolved, workRef, title string) string {
	listed, err := a.invokeRawJSON(ctx, cfg, "pm conversations list", "GET", "/pm/conversations?limit=5", nil)
	if err != nil {
		return ""
	}
	items := asSlice(commandResultBody(listed)["items"])
	if len(items) > emptyAskConversationScanLimit {
		items = items[:emptyAskConversationScanLimit]
	}
	for _, raw := range items {
		item := asMap(raw)
		id := anyString(item["id"])
		if id == "" {
			continue
		}
		created, ok := parseRFC3339Timestamp(anyString(item["created_at"]))
		if !ok || nowFn().Sub(created) > emptyAskConversationReuseWindow || nowFn().Sub(created) < 0 {
			continue
		}
		if len(conversationLatestTurn(item)) > 0 {
			continue
		}
		gotTitle := strings.TrimSpace(anyString(item["title"]))
		gotWork := strings.TrimSpace(anyString(item["work_ref"]))
		if gotTitle != "" && gotTitle != strings.TrimSpace(title) {
			continue
		}
		if gotWork != "" && gotWork != strings.TrimSpace(workRef) {
			continue
		}
		got, err := a.invokeRawJSON(ctx, cfg, "pm conversations get", "GET", "/pm/conversations/"+url.PathEscape(id)+"?limit=1", nil)
		if err != nil {
			continue
		}
		detail := commandResultBody(got)
		if len(asSlice(detail["turns"])) > 0 || len(conversationLatestTurn(detail)) > 0 {
			continue
		}
		conv := asMap(detail["conversation"])
		gotTitle = firstNonEmpty(anyString(conv["title"]), anyString(item["title"]))
		gotWork = firstNonEmpty(anyString(conv["work_ref"]), anyString(item["work_ref"]))
		if strings.TrimSpace(gotTitle) != strings.TrimSpace(title) {
			continue
		}
		if strings.TrimSpace(gotWork) != strings.TrimSpace(workRef) {
			continue
		}
		return id
	}
	return ""
}
