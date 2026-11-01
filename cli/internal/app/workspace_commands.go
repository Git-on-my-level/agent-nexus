// The `anx workspace` composed read and its default text summary projection.

package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/errnorm"
)

func (a *App) runWorkspaceCommand(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, string, error) {
	if len(args) == 0 {
		args = []string{"summary"}
	}
	if isHelpToken(args[0]) {
		return nil, "workspace", errnorm.Usage("missing_subcommand", "`anx workspace` defaults to `summary`; run `anx workspace summary` explicitly for help examples")
	}
	sub := strings.TrimSpace(args[0])
	if sub == "dashboard" {
		if len(args) >= 2 && args[1] == "pins" {
			if err := validateDashboardPinsArgs(args[2:]); err != nil {
				return nil, "workspace dashboard pins", err
			}
			name := "workspace dashboard pins " + args[2]
			var body map[string]any
			if args[2] == "set" {
				refs := append([]string{}, args[3:]...)
				if len(refs) == 1 && refs[0] == "none" {
					refs = []string{}
				}
				body = map[string]any{"document_refs": refs}
			}
			result, err := a.invokeTypedJSON(ctx, cfg, name, strings.ReplaceAll(name, " ", "."), nil, nil, body)
			return result, name, err
		}

		if len(args) == 2 && args[1] == "list" {
			result, err := a.invokeTypedJSON(ctx, cfg, "workspace dashboard list", "workspace.dashboard.list", nil, nil, nil)
			return result, "workspace dashboard list", err
		}
		if len(args) != 3 || args[1] != "set" {
			return nil, "workspace dashboard set", errnorm.Usage("invalid_args", "usage: anx workspace dashboard set <doc|none>")
		}
		var ref any = args[2]
		if args[2] == "none" {
			ref = nil
		}
		result, err := a.invokeTypedJSON(ctx, cfg, "workspace dashboard set", "workspace.dashboard.set", nil, nil, map[string]any{"document_ref": ref})
		return result, "workspace dashboard set", err
	}
	if sub != "summary" {
		return nil, "workspace", errnorm.Usage("unknown_subcommand", fmt.Sprintf("unknown `anx workspace` subcommand %q; expected `summary`", sub))
	}
	if len(args) > 1 {
		return nil, "workspace summary", errnorm.Usage("invalid_args", "unexpected positional arguments for `anx workspace summary`")
	}

	generatedAt := time.Now().UTC().Format(time.RFC3339)
	boardResult, err := a.invokeTypedJSON(ctx, cfg, "boards list", "boards.list", nil, nil, nil)
	if err != nil {
		return nil, "workspace summary", err
	}
	boardBody := commandResultBody(boardResult)
	boards := asSlice(boardBody["boards"])
	activeBoardIDs := make(map[string]struct{}, len(boards))
	activeBoards := make([]any, 0, len(boards))
	for _, row := range boards {
		entry := asMap(row)
		board := asMap(entry["board"])
		if board == nil {
			board = entry
		}
		if !workspaceSummaryActive(board) {
			continue
		}
		activeBoards = append(activeBoards, row)
		for _, key := range []string{"id", "ref", "handle"} {
			if value := strings.TrimSpace(anyString(board[key])); value != "" {
				activeBoardIDs[value] = struct{}{}
				activeBoardIDs[strings.TrimPrefix(value, "board:")] = struct{}{}
				activeBoardIDs["board:"+strings.TrimPrefix(value, "board:")] = struct{}{}
			}
		}
	}
	counts := map[string]any{
		"boards":      len(activeBoards),
		"cards":       nil,
		"documents":   nil,
		"inbox_items": nil,
	}
	warnings := make([]any, 0, 3)

	if cardsResult, cardsErr := a.invokeTypedJSON(ctx, cfg, "cards list", "cards.list", nil, nil, nil); cardsErr == nil {
		activeCardCount := 0
		for _, row := range asSlice(commandResultBody(cardsResult)["cards"]) {
			card := asMap(row)
			if nested := asMap(card["card"]); nested != nil {
				card = nested
			}
			if !workspaceSummaryActive(card) {
				continue
			}
			boardKey := anyString(card["board_ref"])
			if boardKey == "" {
				boardKey = anyString(card["board_id"])
			}
			if _, activeBoard := activeBoardIDs[strings.TrimSpace(boardKey)]; activeBoard {
				activeCardCount++
			}
		}
		counts["cards"] = activeCardCount
	} else {
		warnings = append(warnings, workspaceSummaryWarning("cards", cardsErr))
	}
	if docsResult, docsErr := a.invokeTypedJSON(ctx, cfg, "docs list", "docs.list", nil, nil, nil); docsErr == nil {
		counts["documents"] = len(asSlice(commandResultBody(docsResult)["documents"]))
	} else {
		warnings = append(warnings, workspaceSummaryWarning("documents", docsErr))
	}
	if inboxResult, inboxErr := a.invokeTypedJSON(ctx, cfg, "inbox list", "inbox.list", nil, nil, nil); inboxErr == nil {
		counts["inbox_items"] = len(asSlice(commandResultBody(inboxResult)["items"]))
	} else {
		warnings = append(warnings, workspaceSummaryWarning("inbox", inboxErr))
	}

	data := map[string]any{
		"boards":       activeBoards,
		"counts":       counts,
		"generated_at": generatedAt,
	}
	if len(warnings) > 0 {
		data["warnings"] = warnings
	}
	return &commandResult{Data: data, Text: formatWorkspaceSummary(data)}, "workspace summary", nil
}

func workspaceSummaryActive(resource map[string]any) bool {
	return strings.TrimSpace(anyString(resource["archived_at"])) == "" && strings.TrimSpace(anyString(resource["trashed_at"])) == "" && anyString(resource["state"]) != "archived" && anyString(resource["state"]) != "trashed"
}

func workspaceSummaryWarning(section string, err error) map[string]any {
	normalized := errnorm.Normalize(err)
	return map[string]any{
		"section": section,
		"code":    normalized.Code,
		"message": normalized.Message,
	}
}

func formatWorkspaceSummary(data map[string]any) string {
	counts := asMap(data["counts"])
	boards := asSlice(data["boards"])
	lines := []string{
		"Workspace summary",
		fmt.Sprintf(
			"counts: boards=%s cards=%s documents=%s inbox_items=%s",
			formatCountValue(counts["boards"]),
			formatCountValue(counts["cards"]),
			formatCountValue(counts["documents"]),
			formatCountValue(counts["inbox_items"]),
		),
		fmt.Sprintf("Boards (%d):", len(boards)),
	}
	for _, raw := range boards {
		item := asMap(raw)
		if item == nil {
			continue
		}
		board := asMap(item["board"])
		if board == nil {
			board = item
		}
		summary := asMap(item["summary"])
		badges := make([]string, 0, 4)
		if cardCount := formatCountValue(summary["card_count"]); cardCount != "unknown" {
			badges = append(badges, "cards="+cardCount)
		}
		if unresolved := formatCountValue(summary["unresolved_card_count"]); unresolved != "unknown" {
			badges = append(badges, "open="+unresolved)
		}
		if docs := formatCountValue(summary["document_count"]); docs != "unknown" {
			badges = append(badges, "docs="+docs)
		}
		if latest := strings.TrimSpace(anyString(summary["latest_activity_at"])); latest != "" {
			badges = append(badges, "latest="+latest)
		}
		status := strings.TrimSpace(anyString(board["state"]))
		if status == "" {
			status = strings.TrimSpace(anyString(board["status"]))
		}
		row := compactSummary(displayListScanID(map[string]any{"board": board}, "board", false), status, anyString(board["title"]))
		if len(badges) > 0 {
			row += " [" + strings.Join(badges, ", ") + "]"
		}
		lines = append(lines, "- "+row)
	}
	if len(boards) == 0 {
		lines = append(lines, "- (none)")
		lines = append(lines, "Next: create coordination with `anx topics create --title <title>` or inspect help with `anx concepts`.")
	}
	if warnings := asSlice(data["warnings"]); len(warnings) > 0 {
		lines = append(lines, "warnings:")
		for _, raw := range warnings {
			warning := asMap(raw)
			if warning == nil {
				continue
			}
			lines = append(lines, "- "+compactSummary(anyString(warning["section"]), anyString(warning["code"]), anyString(warning["message"])))
		}
	}
	return strings.Join(lines, "\n")
}

func formatCountValue(value any) string {
	if value == nil {
		return "unknown"
	}
	if n := intValue(value); n != 0 {
		return strconv.Itoa(n)
	}
	switch typed := value.(type) {
	case int:
		return strconv.Itoa(typed)
	case int64:
		return strconv.FormatInt(typed, 10)
	case float64:
		return strconv.Itoa(int(typed))
	case json.Number:
		return typed.String()
	case string:
		if strings.TrimSpace(typed) == "" {
			return "unknown"
		}
		return strings.TrimSpace(typed)
	default:
		if fmt.Sprint(value) == "<nil>" {
			return "unknown"
		}
		return fmt.Sprint(value)
	}
}
