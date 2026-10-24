package app

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"unicode"

	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/output"
)

const (
	openInitiativeTarget         = 15
	initiativeScanLimit          = 200
	askInitiativeSuggestionLimit = 50
	askInitiativeSuggestionCount = 3
	askInitiativeSuggestionPages = 4
	nearDuplicateThreshold       = 0.8
	workNearDuplicateWarning     = "work_near_duplicate"
	workOverTargetWarning        = "work_over_target"
	askAutoCreatedCardCode       = "ask_auto_created_card"
)

// workCreateWarnings reads one page of at most 200 open initiatives and warns
// when the new title nearly duplicates one, or when more than the target
// number are already open. It never fails the write: a read error yields no
// warnings.
func (a *App) workCreateWarnings(ctx context.Context, cfg config.Resolved, title string) []output.Warning {
	result, err := a.invokeRawJSON(ctx, cfg, "work list", "GET", fmt.Sprintf("/work?limit=%d", initiativeScanLimit), nil)
	if err != nil {
		return nil
	}
	var open []map[string]any
	for _, raw := range asSlice(commandResultBody(result)["work"]) {
		row := asMap(raw)
		if isOpenInitiative(row) {
			open = append(open, row)
		}
	}
	return workCreateWarningsFor(title, open)
}

// suggestOpenInitiativeCards reads newest-first work pages and returns up to
// three open initiatives other than exclude. GET /work's phase parameter is an
// exact match, and ListWork always includes closed rows, so there is no
// server filter that excludes done and cancelled. Closed rows are dropped
// here and the server's order is kept. A failed first page yields no
// suggestions; a later page failure keeps whatever was already collected.
func (a *App) suggestOpenInitiativeCards(ctx context.Context, cfg config.Resolved, exclude string) []map[string]any {
	var open []map[string]any
	seenCursor := map[string]bool{"": true}
	cursor := ""
	for page := 0; page < askInitiativeSuggestionPages && len(open) < askInitiativeSuggestionCount; page++ {
		result, err := a.invokeRawJSON(ctx, cfg, "work list", "GET", openInitiativeWorkPath(askInitiativeSuggestionLimit, cursor), nil)
		if err != nil {
			if page == 0 {
				return nil
			}
			break
		}
		body := commandResultBody(result)
		for _, raw := range asSlice(body["work"]) {
			row := asMap(raw)
			if !isOpenInitiative(row) {
				continue
			}
			ref := firstNonEmpty(anyString(row["ref"]), anyString(row["card_ref"]))
			if ref == "" || ref == exclude {
				continue
			}
			open = append(open, row)
			if len(open) == askInitiativeSuggestionCount {
				break
			}
		}
		cursor = strings.TrimSpace(anyString(body["next_cursor"]))
		if cursor == "" || seenCursor[cursor] {
			break
		}
		seenCursor[cursor] = true
	}
	return open
}

// openInitiativeWorkPath is the GET /work query for initiative suggestions.
// The list is newest-first. phase selects one exact phase and cannot exclude
// done or cancelled, so the caller filters those rows.
func openInitiativeWorkPath(limit int, cursor string) string {
	query := url.Values{}
	query.Set("limit", strconv.Itoa(limit))
	if strings.TrimSpace(cursor) != "" {
		query.Set("cursor", cursor)
	}
	return "/work?" + query.Encode()
}

func askAutoCreatedCardWarning(subjectRef, board string, rows []map[string]any) output.Warning {
	if strings.TrimSpace(board) == "" {
		board = "the default board"
	}
	msg := fmt.Sprintf("No --subject-ref: created %s on %s. Asks belong on an initiative; pass --subject-ref card:<slug>.", subjectRef, board)
	suggestions := make([]any, 0, len(rows))
	var listed []string
	for _, row := range rows {
		ref := firstNonEmpty(anyString(row["ref"]), anyString(row["card_ref"]))
		if ref == "" {
			continue
		}
		title := strings.TrimSpace(anyString(row["title"]))
		item := map[string]any{"ref": ref}
		label := ref
		if title != "" {
			item["title"] = title
			label = ref + " (" + title + ")"
		}
		suggestions = append(suggestions, item)
		listed = append(listed, label)
	}
	if len(listed) > 0 {
		msg += " Suggestions: " + strings.Join(listed, "; ") + "."
	}
	details := map[string]any{"created_ref": subjectRef, "board": board}
	if len(suggestions) > 0 {
		details["suggestions"] = suggestions
	}
	return output.Warning{Code: askAutoCreatedCardCode, Message: msg, Details: details}
}

func isOpenInitiative(row map[string]any) bool {
	if anyString(row["parent_ref"]) != "" || row["cadence"] != nil || row["goal"] != nil || row["loop"] != nil {
		return false
	}
	switch strings.ToLower(firstNonEmpty(anyString(row["phase"]), anyString(row["column_key"]), anyString(row["status"]))) {
	case "done", "resolved", "closed", "cancelled", "canceled", "archived", "trashed":
		return false
	}
	return true
}

func workCreateWarningsFor(title string, open []map[string]any) []output.Warning {
	var warnings []output.Warning
	if len(open) > openInitiativeTarget {
		warnings = append(warnings, output.Warning{
			Code:    workOverTargetWarning,
			Message: fmt.Sprintf("%d open initiatives (target 15); prefer a plan step on an existing initiative", len(open)),
			Details: map[string]any{"open": len(open), "target": openInitiativeTarget},
		})
	}
	if strings.TrimSpace(title) == "" {
		return warnings
	}
	bestScore, bestRef, bestTitle := 0.0, "", ""
	for _, row := range open {
		other := anyString(row["title"])
		if score := titleSimilarity(title, other); score > bestScore {
			bestScore, bestRef, bestTitle = score, firstNonEmpty(anyString(row["ref"]), anyString(row["card_ref"])), other
		}
	}
	if bestScore >= nearDuplicateThreshold && bestRef != "" {
		argv := []string{"anx", "plan", "step", "add", bestRef, "--title", title, "--ref", "<url>"}
		warnings = append(warnings, output.Warning{
			Code:    workNearDuplicateWarning,
			Message: fmt.Sprintf("%q matches an open initiative %s %q (%.2f); prefer: anx plan step add %s --title <title> --ref <url>", title, bestRef, bestTitle, bestScore, bestRef),
			Details: map[string]any{"match_ref": bestRef, "score": bestScore, "suggest": argv},
		})
	}
	return warnings
}

// titleSimilarity is the larger of token Jaccard and normalized Levenshtein
// over lowercased, punctuation-free titles.
func titleSimilarity(a, b string) float64 {
	na, nb := normalizeTitle(a), normalizeTitle(b)
	if na == "" || nb == "" {
		return 0
	}
	if na == nb {
		return 1
	}
	ta, tb := strings.Fields(na), strings.Fields(nb)
	set := map[string]int{}
	for _, t := range ta {
		set[t] |= 1
	}
	for _, t := range tb {
		set[t] |= 2
	}
	inter := 0
	for _, v := range set {
		if v == 3 {
			inter++
		}
	}
	jaccard := float64(inter) / float64(len(set))
	ra, rb := []rune(na), []rune(nb)
	longest := max(len(ra), len(rb))
	lev := 1 - float64(levenshtein(ra, rb))/float64(longest)
	return max(jaccard, lev)
}

func normalizeTitle(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
		default:
			b.WriteRune(' ')
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

func levenshtein(a, b []rune) int {
	if len(a) > 256 {
		a = a[:256]
	}
	if len(b) > 256 {
		b = b[:256]
	}
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(b)]
}
