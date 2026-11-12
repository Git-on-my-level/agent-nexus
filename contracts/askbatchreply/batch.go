// Package askbatchreply renders a filed ask batch as one plain-text or HTML
// message and parses the compact reply a human sends back ("1a 2b 3ac
// 4: free text"). It has no dependencies and no transport knowledge, so any
// delivery channel (chat, email, a terminal) can show a batch the same way and
// turn a reply into the per-question answers POST /inbox/{id}/respond takes.
package askbatchreply

import (
	"fmt"
	"sort"
	"strings"
)

// MaxOptions bounds the lettered options of one question (a-f). It matches the
// six response proposals an ask may carry.
const MaxOptions = 6

// Link is one evidence link of the shared batch context.
type Link struct {
	Label string
	URL   string
}

// Context is the background every question of a batch shares. It is stated
// once, above the numbered questions.
type Context struct {
	Situation string
	Impact    string
	Details   string
	Evidence  []Link
}

func (c *Context) empty() bool {
	return c == nil || (strings.TrimSpace(c.Situation) == "" && strings.TrimSpace(c.Impact) == "" &&
		strings.TrimSpace(c.Details) == "" && len(c.Evidence) == 0)
}

// Option is one lettered choice. Why carries the recommendation's reason or an
// alternative's consequence when the ask gave one.
type Option struct {
	Text string
	Why  string
}

// Question is one numbered member of a batch. Options[Recommended] is the
// asker's recommendation; Recommended is -1 when no option is marked.
type Question struct {
	Number      int
	AskRef      string
	Title       string
	Note        string
	Options     []Option
	Recommended int
	MultiSelect bool
}

// Batch is what a transport renders and parses against: the inbox item that
// answers it, the shared context and the open questions in display order.
type Batch struct {
	ID          string
	InboxItemID string
	Title       string
	Context     *Context
	Questions   []Question
}

// Question returns the question shown with number n.
func (b Batch) Question(n int) (Question, bool) {
	for _, q := range b.Questions {
		if q.Number == n {
			return q, true
		}
	}
	return Question{}, false
}

// FromInboxItem builds a Batch from an inbox item as GET /inbox returns it
// (the item's `batch` object). Questions are numbered in the batch's own order
// (`position`, else the order core lists them in). Only open members are
// listed by core, so a partially answered batch numbers its remaining
// questions by their original positions and the reader's numbers stay stable.
func FromInboxItem(item map[string]any) (Batch, error) {
	raw, _ := item["batch"].(map[string]any)
	if raw == nil {
		return Batch{}, fmt.Errorf("inbox item has no batch")
	}
	batch := Batch{
		ID:          str(raw["id"]),
		InboxItemID: str(item["id"]),
		Title:       str(item["title"]),
		Context:     contextFrom(raw["context"]),
	}
	if batch.InboxItemID == "" {
		return Batch{}, fmt.Errorf("inbox item has no id")
	}
	sections, _ := raw["asks"].([]any)
	type positioned struct {
		position int
		index    int
		q        Question
	}
	list := make([]positioned, 0, len(sections))
	for index, rawSection := range sections {
		section, _ := rawSection.(map[string]any)
		if section == nil {
			continue
		}
		q := Question{
			AskRef:      str(section["request_event_ref"]),
			Title:       str(section["title"]),
			Note:        str(section["note"]),
			MultiSelect: section["multi_select"] == true,
			Recommended: -1,
		}
		if q.AskRef == "" {
			return Batch{}, fmt.Errorf("batch question %d has no request_event_ref", index+1)
		}
		q.Options, q.Recommended = optionsFrom(section)
		position := intValue(section["position"])
		list = append(list, positioned{position: position, index: index, q: q})
	}
	if len(list) == 0 {
		return Batch{}, fmt.Errorf("batch has no open questions")
	}
	sort.SliceStable(list, func(i, j int) bool {
		if list[i].position != list[j].position {
			return list[i].position < list[j].position
		}
		return list[i].index < list[j].index
	})
	used := map[int]bool{}
	for i := range list {
		number := list[i].position
		if number <= 0 || used[number] {
			number = 0
		}
		list[i].q.Number = number
		if number > 0 {
			used[number] = true
		}
	}
	next := 1
	for i := range list {
		if list[i].q.Number == 0 {
			for used[next] {
				next++
			}
			list[i].q.Number = next
			used[next] = true
		}
		batch.Questions = append(batch.Questions, list[i].q)
	}
	return batch, nil
}

// optionsFrom reads the lettered options: response_proposals in order, with
// the recommendation's why and each alternative's consequence attached.
func optionsFrom(section map[string]any) ([]Option, int) {
	proposals, _ := section["response_proposals"].([]any)
	why := map[string]string{}
	recommendedAction := ""
	if rec, _ := section["recommendation"].(map[string]any); rec != nil {
		recommendedAction = str(rec["action"])
		why[recommendedAction] = str(rec["why"])
	}
	if alts, _ := section["alternatives"].([]any); alts != nil {
		for _, rawAlt := range alts {
			if alt, _ := rawAlt.(map[string]any); alt != nil {
				if action := str(alt["action"]); action != "" && why[action] == "" {
					why[action] = str(alt["consequence"])
				}
			}
		}
	}
	options := make([]Option, 0, len(proposals))
	seen := map[string]bool{}
	for _, rawProposal := range proposals {
		text := str(rawProposal)
		if text == "" || seen[text] || len(options) == MaxOptions {
			continue
		}
		seen[text] = true
		options = append(options, Option{Text: text, Why: why[text]})
	}
	if len(options) == 0 {
		return options, -1
	}
	// Core orders proposals recommendation first.
	return options, 0
}

func contextFrom(raw any) *Context {
	m, _ := raw.(map[string]any)
	if m == nil {
		return nil
	}
	c := &Context{Situation: str(m["situation"]), Impact: str(m["impact"]), Details: str(m["details"])}
	if links, _ := m["evidence"].([]any); links != nil {
		for _, rawLink := range links {
			if link, _ := rawLink.(map[string]any); link != nil && str(link["url"]) != "" {
				c.Evidence = append(c.Evidence, Link{Label: str(link["label"]), URL: str(link["url"])})
			}
		}
	}
	if c.empty() {
		return nil
	}
	return c
}

// ResponseText is the answer text recorded for one question: the chosen
// option(s), or the reader's own words.
func (a Answer) ResponseText(q Question) string {
	if a.Text != "" {
		return a.Text
	}
	parts := make([]string, 0, len(a.Choices))
	for _, choice := range a.Choices {
		if choice >= 0 && choice < len(q.Options) {
			parts = append(parts, q.Options[choice].Text)
		}
	}
	return strings.Join(parts, "; ")
}

// RespondBody is the POST /inbox/{inbox_item_id}/respond body for a parsed
// reply: one answer per answered question, partial when some stay open. The
// caller supplies a stable idempotency key for this one reply.
func RespondBody(batch Batch, result Result, idempotencyKey string) map[string]any {
	answers := make([]any, 0, len(result.Answers))
	for _, answer := range result.Answers {
		q, ok := batch.Question(answer.Number)
		if !ok {
			continue
		}
		answers = append(answers, map[string]any{
			"ask_ref":       q.AskRef,
			"response_text": answer.ResponseText(q),
			"outcome":       "answered",
		})
	}
	body := map[string]any{"answers": answers}
	if len(result.Missing) > 0 {
		body["partial"] = true
	}
	if idempotencyKey != "" {
		body["idempotency_key"] = idempotencyKey
	}
	return body
}

func str(v any) string {
	s, _ := v.(string)
	return strings.TrimSpace(s)
}

func intValue(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	}
	return 0
}

func letter(i int) string { return string(rune('a' + i)) }
