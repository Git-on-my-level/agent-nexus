package app

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"sort"
	"strings"

	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/errnorm"
)

// Recovery never submits another move. A fresh token does not make replay of a
// committed move safe: it advances the version, rank and lifecycle events again.
func (a *App) recoverCardMove(ctx context.Context, cfg config.Resolved, commandID string, params map[string]string, body any, cause error) (*commandResult, error) {
	var remote *errnorm.Error
	if commandID != "cards.move" || !errors.As(cause, &remote) {
		return nil, cause
	}
	details, _ := remote.Details.(map[string]any)
	if remote.Code != "outcome_unknown" && details["status"] != http.StatusServiceUnavailable {
		return nil, cause
	}
	// Work's point reader retains canonical column/resolution fields without
	// loading the card's unbounded revision history.
	read, err := a.invokeTypedJSON(ctx, cfg, "work get", "work.get", map[string]string{"card_ref": params["card_id"]}, nil, nil)
	if err != nil {
		return nil, cause
	}
	card := asMap(commandResultBody(read)["work"])
	request, ok := body.(map[string]any)
	if !ok || !cardMoveReadbackMatches(card, request) {
		return nil, cause
	}
	data := asMap(read.Data)
	// Work's version is the annotation/move fence, not the card revision version.
	// Return only canonical card fields; do not mislabel the work token.
	view := map[string]any{}
	for _, key := range []string{"ref", "title", "column_key", "rank", "resolution", "resolution_refs", "updated_at", "board_ref", "head_revision_ref", "head_revision_number", "assignee_refs"} {
		if value, ok := card[key]; ok {
			view[key] = value
		}
	}
	response := map[string]any{"card": view, "recovered": true, "recovery": "readback", "work_version": card["version"]}
	data["body"] = response
	read.Text = formatTypedCommandText("cards.move", http.StatusOK, normalizedHeaders(http.Header{}), response, cfg.Verbose, cfg.Headers)
	data["status_code"] = http.StatusOK
	return read, nil
}

func cardMoveReadbackMatches(card, request map[string]any) bool {
	phase := strings.TrimSpace(anyString(request["column_key"]))
	if phase == "" || anyString(card["column_key"]) != phase {
		return false
	}
	// A column read cannot establish the requested rank. Retain the original
	// non-retryable uncertainty rather than silently losing a placement change.
	for _, key := range []string{"before_card_id", "after_card_id", "before_thread_id", "after_thread_id"} {
		if strings.TrimSpace(anyString(request[key])) != "" {
			return false
		}
	}
	if resolution, ok := request["resolution"]; ok && resolution != nil && anyString(resolution) != anyString(card["resolution"]) {
		return false
	}
	if refs, ok := request["resolution_refs"]; ok {
		want, got := stringList(refs), stringList(card["resolution_refs"])
		sort.Strings(want)
		sort.Strings(got)
		if !slices.Equal(want, got) {
			return false
		}
	}
	return true
}
