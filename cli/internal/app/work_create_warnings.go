package app

import (
	"context"
	"fmt"
	"strings"
	"unicode"

	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/output"
)

const (
	openInitiativeTarget     = 15
	initiativeScanLimit      = 200
	nearDuplicateThreshold   = 0.8
	workNearDuplicateWarning = "work_near_duplicate"
	workOverTargetWarning    = "work_over_target"
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

func isOpenInitiative(row map[string]any) bool {
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
