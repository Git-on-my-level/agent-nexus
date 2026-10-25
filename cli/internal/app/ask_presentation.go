package app

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

type askRecommendation struct {
	Action string `json:"action" yaml:"action"`
	Why    string `json:"why" yaml:"why"`
}
type askAlternative struct {
	Action      string `json:"action" yaml:"action"`
	Consequence string `json:"consequence" yaml:"consequence"`
}
type askPresentation struct {
	Situation      string             `json:"situation,omitempty" yaml:"situation"`
	Impact         string             `json:"impact,omitempty" yaml:"impact"`
	Recommendation *askRecommendation `json:"recommendation,omitempty" yaml:"recommendation"`
	Alternatives   []askAlternative   `json:"alternatives,omitempty" yaml:"alternatives"`
	IfNoAnswer     string             `json:"if_no_answer,omitempty" yaml:"if_no_answer"`
	Details        string             `json:"details,omitempty" yaml:"details"`
}

func (p askPresentation) apply(payload map[string]any) {
	// Force can preserve a legacy ask without publishing an invalid partial object.
	if p.Recommendation != nil && (strings.TrimSpace(p.Recommendation.Action) == "" || strings.TrimSpace(p.Recommendation.Why) == "") {
		p.Recommendation = nil
	}
	raw, _ := json.Marshal(p)
	var fields map[string]any
	_ = json.Unmarshal(raw, &fields)
	for k, v := range fields {
		payload[k] = v
	}
}

// These intentionally narrow patterns flag concrete technical tokens, rather
// than attempting to grade English. Explanations use "token (plain words)".
var askJargon = regexp.MustCompile(`\b[a-zA-Z][a-zA-Z0-9]*_[a-zA-Z0-9_]+\b|\b[a-z]+[A-Z][a-zA-Z0-9]*\b|\b(?:[A-Za-z]{1,4}[0-9]{3,}|[0-9a-fA-F]{7,64})\b|(?:/|~/|\./)(?:[A-Za-z0-9_.-]+/)*[A-Za-z0-9_.-]+|\b(?:[A-Za-z0-9_.-]+/){2,}[A-Za-z0-9_.-]+|\b[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+\.[A-Za-z0-9]+|\b[A-Za-z0-9_.-]+\.(?:go|py|ts|tsx|js|jsx|svelte|json|ya?ml|md|sql|sh)\b|\b[A-Z]{2,}\b`)
var askHexToken = regexp.MustCompile(`^[0-9a-fA-F]{7,64}$`)
var askAcronym = regexp.MustCompile(`^[A-Z]{2,}$`)
var askDecisionTitle = regexp.MustCompile(`(?i)^(?:approve|choose|decide|allow|accept|reject|fund|pause|resume|ship|delay|stop|start|keep|replace|use|make|should|can|do|will|which|what|may)\b`)

// A concrete risk statement can also name the decision, as in the deletion
// example; vague status labels still warn.
var askRiskTitle = regexp.MustCompile(`(?i)\b(?:can|could|may) (?:reappear|return|fail|lose|break|expose|leak|delay|cost|block)\b`)

func askTokenExplained(token, text string) bool {
	// A short parenthetical containing two plain words is enough; details/evidence
	// do not count as explanations of copy visible on the decision face.
	re := regexp.MustCompile(regexp.QuoteMeta(token) + `\s*\(([^()]{3,100})\)`)
	for _, match := range re.FindAllStringSubmatch(text, -1) {
		words := strings.Fields(match[1])
		if len(words) >= 2 && len(askJargonTokens(match[1])) == 0 {
			return true
		}
	}
	// Common expansion: "service level agreement (SLA)".
	if askAcronym.MatchString(token) {
		re = regexp.MustCompile(`([A-Za-z]+(?: [A-Za-z]+){1,7})\s*\(` + regexp.QuoteMeta(token) + `\)`)
		for _, match := range re.FindAllStringSubmatch(text, -1) {
			words := strings.Fields(match[1])
			initials := ""
			for _, w := range words {
				initials += strings.ToUpper(w[:1])
			}
			if strings.HasSuffix(initials, token) {
				return true
			}
		}
	}
	return false
}
func lintAskPresentation(title string, p askPresentation, options []any) (warnings []string, errors []string) {
	warnings = []string{}
	errors = []string{}
	for _, f := range []struct{ name, value string }{{"situation", p.Situation}, {"impact", p.Impact}} {
		if strings.TrimSpace(f.value) == "" {
			errors = append(errors, "Missing "+f.name+"; add --"+f.name+" or frontmatter in --from-file ask.md")
		}
	}
	if p.Recommendation == nil || strings.TrimSpace(p.Recommendation.Action) == "" {
		errors = append(errors, "Missing recommendation; add --recommend or recommendation.action in --from-file ask.md")
	}
	if p.Recommendation == nil || strings.TrimSpace(p.Recommendation.Why) == "" {
		errors = append(errors, "Missing recommendation.why; add --why or recommendation.why in --from-file ask.md")
	}
	if utf8.RuneCountInString(title) > 80 {
		warnings = append(warnings, fmt.Sprintf("Title %q exceeds 80 characters; shorten it to the decision", title))
	}
	if !strings.HasSuffix(strings.TrimSpace(title), "?") && !askDecisionTitle.MatchString(title) && !askRiskTitle.MatchString(title) {
		warnings = append(warnings, fmt.Sprintf("Title %q is not a question or decision; name what the reader should decide", title))
	}
	if len(strings.Fields(p.Situation)) > 60 {
		warnings = append(warnings, fmt.Sprintf("Situation %q exceeds 60 words; keep one or two sentences", p.Situation))
	}
	fields := []struct{ name, text string }{{"title", title}, {"situation", p.Situation}, {"impact", p.Impact}}
	for i, v := range options {
		fields = append(fields, struct{ name, text string }{fmt.Sprintf("option %d", i+1), anyString(v)})
	}
	visible := ""
	counts := map[string]int{}
	for _, f := range fields {
		visible += " " + f.text
		for _, token := range askJargonTokens(f.text) {
			counts[token]++
		}
	}
	for _, f := range fields {
		seen := map[string]bool{}
		for _, token := range askJargonTokens(f.text) {
			if seen[token] || (askAcronym.MatchString(token) && !askHexToken.MatchString(token) && counts[token] < 2) || askTokenExplained(token, visible) {
				continue
			}
			seen[token] = true
			warnings = append(warnings, fmt.Sprintf("Unexplained jargon %q in %s; replace it with plain words or explain it in parentheses; move identifiers to details/evidence", token, f.name))
		}
	}
	return warnings, errors
}
func renderAskPreview(title, body string, p askPresentation, options []any, evidence []askEvidenceLink) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n\n", title)
	if p.Situation != "" {
		fmt.Fprintf(&b, "Situation: %s\n", p.Situation)
	} else if body != "" {
		fmt.Fprintf(&b, "%s\n", body)
	}
	if p.Impact != "" {
		fmt.Fprintf(&b, "Impact: %s\n", p.Impact)
	}
	if p.Recommendation != nil {
		fmt.Fprintf(&b, "I recommend: %s\nWhy: %s\n", p.Recommendation.Action, p.Recommendation.Why)
	}
	for _, alt := range p.Alternatives {
		fmt.Fprintf(&b, "Alternative: %s. %s\n", alt.Action, alt.Consequence)
	}
	if p.IfNoAnswer != "" {
		fmt.Fprintf(&b, "If no answer: %s\n", p.IfNoAnswer)
	}
	for _, option := range options {
		fmt.Fprintf(&b, "Option: %s\n", anyString(option))
	}
	if p.Details != "" || len(evidence) > 0 || (p.Situation != "" && body != "") {
		b.WriteString("\nDetails and evidence (collapsed in Decide)\n")
		if p.Details != "" {
			b.WriteString(p.Details + "\n")
		}
		if p.Situation != "" && body != "" {
			b.WriteString(body + "\n")
		}
		for _, e := range evidence {
			fmt.Fprintf(&b, "%s: %s\n", e.Label, e.URL)
		}
	}
	return b.String()
}

// Scan flags without mistaking a string flag value for --preview.
func hasAskPreview(args []string) bool {
	preview := false
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--preview" {
			preview = true
			continue
		}
		if value, ok := strings.CutPrefix(arg, "--preview="); ok {
			preview, _ = strconv.ParseBool(value)
			continue
		}
		if strings.HasPrefix(arg, "--") && !strings.Contains(arg, "=") && arg != "--force" && arg != "--dry-run" {
			i++
		}
	}
	return preview
}

// Hard payload bounds match the additive API contract; an override cannot make
// an oversized request valid at the server.
func (p askPresentation) validateBounds() error {
	for _, f := range []struct {
		name, value string
		max         int
	}{{"situation", p.Situation, 4000}, {"impact", p.Impact, 4000}, {"if_no_answer", p.IfNoAnswer, 4000}, {"details", p.Details, 32000}} {
		if utf8.RuneCountInString(f.value) > f.max {
			return fmt.Errorf("%s exceeds %d characters", f.name, f.max)
		}
	}
	if p.Recommendation != nil && (utf8.RuneCountInString(p.Recommendation.Action) > 240 || utf8.RuneCountInString(p.Recommendation.Why) > 4000) {
		return fmt.Errorf("recommendation action/why exceeds 240/4000 characters")
	}
	if len(p.Alternatives) > 5 {
		return fmt.Errorf("alternatives exceeds 5 entries")
	}
	for _, alt := range p.Alternatives {
		if strings.TrimSpace(alt.Action) == "" || strings.TrimSpace(alt.Consequence) == "" || utf8.RuneCountInString(alt.Action) > 240 || utf8.RuneCountInString(alt.Consequence) > 4000 {
			return fmt.Errorf("alternative requires non-empty action (at most 240 characters) and consequence (at most 4000)")
		}
	}
	return nil
}

func askJargonTokens(text string) []string {
	var tokens []string
	for _, span := range askJargon.FindAllStringIndex(text, -1) {
		token := text[span[0]:span[1]]
		if strings.Trim(token, "0123456789") == "" {
			continue
		}
		// A single slash joining English words (privacy/compliance, and/or) is
		// ordinary prose, not an absolute path starting in the middle of a word.
		if strings.HasPrefix(token, "/") && span[0] > 0 {
			prev := text[span[0]-1]
			if (prev >= 'a' && prev <= 'z') || (prev >= 'A' && prev <= 'Z') {
				continue
			}
		}
		tokens = append(tokens, token)
	}
	return tokens
}
