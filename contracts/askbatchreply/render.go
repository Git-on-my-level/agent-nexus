package askbatchreply

import (
	"fmt"
	"html"
	"strconv"
	"strings"
	"unicode/utf8"
)

// RenderOptions shape a rendering for one channel.
type RenderOptions struct {
	// ContextLimit caps the situation and impact text in runes (0: no cap). A
	// chat message states the context briefly; an email (which links to
	// Decide to answer) can carry all of it.
	ContextLimit int
	// Details includes the collapsed details and evidence links.
	Details bool
	// Reasons adds the recommendation's why and each alternative's consequence
	// under its option.
	Reasons bool
	// OmitReplyHint leaves out the compact-reply instruction, for a channel
	// whose replies are not read (the reader answers elsewhere).
	OmitReplyHint bool
}

// ReplyHint is the one-line instruction that ends every rendering.
func ReplyHint(b Batch) string {
	multi := 0
	text := 0
	for _, q := range b.Questions {
		if q.MultiSelect && multi == 0 {
			multi = q.Number
		}
		if len(q.Options) == 0 && text == 0 {
			text = q.Number
		}
	}
	hint := "Reply with one line, for example: " + example(b)
	if multi > 0 {
		hint += fmt.Sprintf(" (question %d takes several letters, e.g. %dab)", multi, multi)
	}
	hint += ". Answer in your own words with <number>: your words. Questions you leave out stay open."
	return hint
}

// RenderText renders the whole batch as plain text: the shared context once,
// then the numbered questions with lettered options and the recommendation
// marked, then the reply hint.
func RenderText(b Batch, o RenderOptions) string {
	parts := []string{headerText(b, o)}
	for _, q := range b.Questions {
		parts = append(parts, questionText(q, o))
	}
	if !o.OmitReplyHint {
		parts = append(parts, ReplyHint(b))
	}
	return strings.Join(parts, "\n\n")
}

func headerText(b Batch, o RenderOptions) string {
	var out strings.Builder
	title := oneLine(b.Title)
	if title == "" {
		title = "Questions"
	}
	fmt.Fprintf(&out, "%s (%s)", title, questionCount(len(b.Questions)))
	if c := b.Context; !c.empty() {
		if s := clip(c.Situation, o.ContextLimit); s != "" {
			out.WriteString("\n\n" + s)
		}
		if s := clip(c.Impact, o.ContextLimit); s != "" {
			out.WriteString("\n\nWhy it matters: " + s)
		}
		if o.Details {
			if d := strings.TrimSpace(c.Details); d != "" {
				out.WriteString("\n\nDetails: " + d)
			}
			for _, link := range c.Evidence {
				label := strings.TrimSpace(link.Label)
				if label == "" {
					label = "Evidence"
				}
				fmt.Fprintf(&out, "\n%s: %s", label, link.URL)
			}
		}
	}
	return out.String()
}

func questionCount(n int) string {
	if n == 1 {
		return "1 question"
	}
	return strconv.Itoa(n) + " questions"
}

func questionText(q Question, o RenderOptions) string {
	var out strings.Builder
	fmt.Fprintf(&out, "%d. %s", q.Number, oneLine(q.Title))
	if q.MultiSelect {
		out.WriteString(" (pick any)")
	}
	if note := strings.TrimSpace(q.Note); note != "" {
		out.WriteString("\n   " + oneLine(note))
	}
	for i, option := range q.Options {
		fmt.Fprintf(&out, "\n   %s) %s", letter(i), oneLine(option.Text))
		if i == q.Recommended {
			out.WriteString(" (recommended)")
		}
		if o.Reasons && strings.TrimSpace(option.Why) != "" {
			out.WriteString("\n      " + oneLine(option.Why))
		}
	}
	if len(q.Options) == 0 && !o.OmitReplyHint {
		fmt.Fprintf(&out, "\n   Answer as %d: your words", q.Number)
	}
	return out.String()
}

// SplitText renders the batch as one or more plain-text messages of at most
// limit runes each, split between questions. Every message after the first
// carries the batch title and its part number so a reader (and a reply) keeps
// one batch identity; the reply hint ends the last part. A single question
// longer than the limit is cut with an ellipsis rather than split.
func SplitText(b Batch, o RenderOptions, limit int) []string {
	full := RenderText(b, o)
	if limit <= 0 || utf8.RuneCountInString(full) <= limit {
		return []string{full}
	}
	const label = 32 // room for "\n\n(part 10 of 10)"
	blocks := []string{headerText(b, o)}
	for _, q := range b.Questions {
		blocks = append(blocks, questionText(q, o))
	}
	if !o.OmitReplyHint {
		blocks = append(blocks, ReplyHint(b))
	}
	budget := limit - label
	if budget < 64 {
		budget = limit
	}
	title := oneLine(b.Title)
	var parts []string
	current := ""
	for _, block := range blocks {
		block = clip(block, budget)
		candidate := block
		if current != "" {
			candidate = current + "\n\n" + block
		}
		if utf8.RuneCountInString(candidate) <= budget {
			current = candidate
			continue
		}
		parts = append(parts, current)
		current = clip(title+" (continued)\n\n"+block, budget)
	}
	if current != "" {
		parts = append(parts, current)
	}
	if len(parts) > 1 {
		for i := range parts {
			parts[i] += fmt.Sprintf("\n\n(part %d of %d)", i+1, len(parts))
		}
	}
	return parts
}

// RenderHTML renders the same layout as RenderText as a self-contained HTML
// fragment for email bodies. Every value is escaped. Email only shows a
// batch; pass OmitReplyHint, since answers are given in Decide or a chat.
func RenderHTML(b Batch, o RenderOptions) string {
	e := html.EscapeString
	var out strings.Builder
	title := oneLine(b.Title)
	if title == "" {
		title = "Questions"
	}
	fmt.Fprintf(&out, "<h2 style=\"margin:0 0 12px\">%s <span style=\"font-weight:normal;color:#666\">(%s)</span></h2>\n", e(title), questionCount(len(b.Questions)))
	if c := b.Context; !c.empty() {
		if s := clip(c.Situation, o.ContextLimit); s != "" {
			fmt.Fprintf(&out, "<p>%s</p>\n", paragraphs(s))
		}
		if s := clip(c.Impact, o.ContextLimit); s != "" {
			fmt.Fprintf(&out, "<p><strong>Why it matters:</strong> %s</p>\n", paragraphs(s))
		}
		if o.Details {
			if d := strings.TrimSpace(c.Details); d != "" {
				fmt.Fprintf(&out, "<details><summary>Details</summary><p>%s</p></details>\n", paragraphs(d))
			}
			if len(c.Evidence) > 0 {
				out.WriteString("<ul>\n")
				for _, link := range c.Evidence {
					label := strings.TrimSpace(link.Label)
					if label == "" {
						label = link.URL
					}
					fmt.Fprintf(&out, "<li><a href=\"%s\">%s</a></li>\n", e(link.URL), e(label))
				}
				out.WriteString("</ul>\n")
			}
		}
	}
	for _, q := range b.Questions {
		fmt.Fprintf(&out, "<h3 style=\"margin:16px 0 4px\">%d. %s", q.Number, e(oneLine(q.Title)))
		if q.MultiSelect {
			out.WriteString(" <span style=\"font-weight:normal;color:#666\">(pick any)</span>")
		}
		out.WriteString("</h3>\n")
		if note := strings.TrimSpace(q.Note); note != "" {
			fmt.Fprintf(&out, "<p style=\"margin:0 0 4px;color:#444\">%s</p>\n", e(oneLine(note)))
		}
		if len(q.Options) == 0 {
			if !o.OmitReplyHint {
				fmt.Fprintf(&out, "<p style=\"margin:0\">Answer as <code>%d: your words</code></p>\n", q.Number)
			} else {
				out.WriteString("<p style=\"margin:0;color:#555\">Answer in your own words.</p>\n")
			}
			continue
		}
		out.WriteString("<ul style=\"margin:0;padding-left:20px;list-style:none\">\n")
		for i, option := range q.Options {
			fmt.Fprintf(&out, "<li><strong>%s)</strong> %s", letter(i), e(oneLine(option.Text)))
			if i == q.Recommended {
				out.WriteString(" <em>(recommended)</em>")
			}
			if o.Reasons && strings.TrimSpace(option.Why) != "" {
				fmt.Fprintf(&out, "<br><span style=\"color:#555\">%s</span>", e(oneLine(option.Why)))
			}
			out.WriteString("</li>\n")
		}
		out.WriteString("</ul>\n")
	}
	if !o.OmitReplyHint {
		fmt.Fprintf(&out, "<p style=\"margin-top:16px\">%s</p>\n", e(ReplyHint(b)))
	}
	return out.String()
}

func paragraphs(s string) string {
	return strings.ReplaceAll(html.EscapeString(strings.TrimSpace(s)), "\n", "<br>")
}

// clip shortens s to at most limit runes, ending on a word with an ellipsis.
func clip(s string, limit int) string {
	s = strings.TrimSpace(s)
	if limit <= 0 || utf8.RuneCountInString(s) <= limit {
		return s
	}
	runes := []rune(s)
	cut := string(runes[:limit-1])
	if space := strings.LastIndexAny(cut, " \n"); space > limit/2 {
		cut = cut[:space]
	}
	return strings.TrimSpace(cut) + "…"
}
