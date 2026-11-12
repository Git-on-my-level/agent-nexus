package app

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"agent-nexus-askbatchreply"

	"agent-nexus-cli/internal/workspaceconfig"
)

// askBatchContext is the optional top-level "context" of asks.json: the
// background every question shares, stated once. When it is present each ask
// needs only its question, recommendation and alternatives, and this block is
// linted once instead of every ask's situation and impact.
type askBatchContext struct {
	Title     string            `json:"title"`
	Situation string            `json:"situation"`
	Impact    string            `json:"impact"`
	Details   string            `json:"details"`
	Evidence  []askEvidenceLink `json:"evidence"`
}

func decodeAskBatchContext(raw any) (*askBatchContext, error) {
	if raw == nil {
		return nil, nil
	}
	object, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("context must be an object with situation, impact, details and evidence")
	}
	for key := range object {
		switch key {
		case "title", "situation", "impact", "details", "evidence":
		default:
			return nil, fmt.Errorf("context.%s is not a context field; use title, situation, impact, details or evidence", key)
		}
	}
	encoded, err := json.Marshal(object)
	if err != nil {
		return nil, err
	}
	var block askBatchContext
	if err := json.Unmarshal(encoded, &block); err != nil {
		return nil, fmt.Errorf("context: %v", err)
	}
	block.Title = strings.TrimSpace(block.Title)
	block.Situation = strings.TrimSpace(block.Situation)
	block.Impact = strings.TrimSpace(block.Impact)
	block.Details = strings.TrimSpace(block.Details)
	return &block, nil
}

// lint applies the per-ask situation and impact rules once, to the shared
// context. hard violations cannot be overridden with --force.
func (c *askBatchContext) lint(forges []workspaceconfig.Forge) (warnings, errors, hard []string) {
	over := func(path, value string, limit int) {
		if n := utf8.RuneCountInString(value); n > limit {
			hard = append(hard, fmt.Sprintf("%s is %d characters; the limit is %d", path, n, limit))
		}
	}
	over("context.title", c.Title, 160)
	over("context.situation", c.Situation, 4000)
	over("context.impact", c.Impact, 4000)
	over("context.details", c.Details, 32000)
	if strings.ContainsAny(c.Title, "\r\n") {
		hard = append(hard, "context.title must be one line")
	}
	if c.Situation == "" {
		// The server refuses a context without a situation.
		hard = append(hard, "context.situation is required; it is the background every question shares")
	}
	if c.Impact == "" {
		errors = append(errors, "Missing context.impact; say why these questions matter")
	}
	words := len(strings.Fields(c.Situation)) + len(strings.Fields(c.Impact))
	if words < 40 {
		warnings = append(warnings, "Context situation and impact together are under 40 words; add the baseline, what changed and why it matters")
	}
	if len(strings.Fields(c.Situation)) > 250 {
		warnings = append(warnings, "Context situation exceeds 250 words; focus on the baseline, what changed and the key numbers")
	}
	warnings = append(warnings, askJargonWarnings([]askLintField{{"context situation", c.Situation}, {"context impact", c.Impact}})...)
	authoringWarnings, authoringErrors := lintAskAuthoring(c.Details, nil, c.Evidence, forges)
	if evidenceShapeInvalid(c.Evidence) {
		hard = append(hard, authoringErrors...)
	} else {
		errors = append(errors, authoringErrors...)
	}
	return append(warnings, authoringWarnings...), errors, hard
}

func (c *askBatchContext) payload() map[string]any {
	out := map[string]any{"situation": c.Situation}
	for key, value := range map[string]string{"title": c.Title, "impact": c.Impact, "details": c.Details} {
		if value != "" {
			out[key] = value
		}
	}
	if len(c.Evidence) > 0 {
		out["evidence"] = c.Evidence
	}
	return out
}

// askBatchPreview renders the batch the way a chat transport shows
// it, using the same shared renderer, so --dry-run shows the human's view.
func askBatchPreview(entries []askBatchEntry, context *askBatchContext) string {
	batch := askbatchreply.Batch{Title: entries[0].Title}
	if context != nil {
		batch.Context = &askbatchreply.Context{Situation: context.Situation, Impact: context.Impact, Details: context.Details}
		for _, link := range context.Evidence {
			batch.Context.Evidence = append(batch.Context.Evidence, askbatchreply.Link{Label: link.Label, URL: link.URL})
		}
		if context.Title != "" {
			batch.Title = context.Title
		}
	}
	for index, entry := range entries {
		question := askbatchreply.Question{Number: index + 1, Title: entry.Title, Note: entry.Note, MultiSelect: entry.MultiSelect, Recommended: 0}
		why := map[string]string{}
		if entry.Recommendation != nil {
			why[entry.Recommend] = entry.Recommendation.Why
		}
		for _, alt := range entry.Alternatives {
			why[alt.Action] = alt.Consequence
		}
		seen := map[string]bool{}
		for _, option := range append([]string{entry.Recommend}, entry.Alts...) {
			if option = strings.TrimSpace(option); option != "" && !seen[option] {
				seen[option] = true
				question.Options = append(question.Options, askbatchreply.Option{Text: option, Why: why[option]})
			}
		}
		batch.Questions = append(batch.Questions, question)
	}
	return askbatchreply.RenderText(batch, askbatchreply.RenderOptions{Details: true, Reasons: true})
}
