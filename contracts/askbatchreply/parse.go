package askbatchreply

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

// Answer is one parsed answer: lettered choices (indexes into the question's
// Options, ascending) or the reader's own words in Text.
type Answer struct {
	Number  int
	Choices []int
	Text    string
	// Fragment is the answer as typed, such as "3ac" or "4: own words".
	Fragment string
	// AfterTextOf names the question whose own words came just before this
	// answer on the same line, when that text was cut short to read this one.
	// A transport should say so: the reader may have meant it as part of
	// those words.
	AfterTextOf int
}

// Issue names one part of a reply that could not be read, exactly as typed.
type Issue struct {
	Fragment string
	Number   int
	Reason   string
}

func (i Issue) String() string {
	if i.Fragment == "" {
		return i.Reason
	}
	return fmt.Sprintf("%q: %s", i.Fragment, i.Reason)
}

// Result is a parsed reply. Answers are ordered by question number, Issues in
// the order they appear, and Missing lists the questions left unanswered.
type Result struct {
	Answers []Answer
	Issues  []Issue
	Missing []int
	// Ignored holds trailing paragraphs (after a blank line, once answers were
	// read) that contain no answer item, such as an email sign-off. They are
	// not answers; a transport may show them so nothing vanishes silently.
	Ignored []string
}

// OK reports a reply that parsed cleanly and answers at least one question.
// Missing questions do not make a reply invalid: they stay open.
func (r Result) OK() bool { return len(r.Issues) == 0 && len(r.Answers) > 0 }

// Complete reports a clean reply that answers every question.
func (r Result) Complete() bool { return r.OK() && len(r.Missing) == 0 }

type parser struct {
	batch    Batch
	answered map[int]bool
	answers  []Answer
	issues   []Issue
	// tailOf is the question whose free text was cut short earlier on the
	// line being parsed; every answer read after it records it.
	tailOf int
}

// Parse reads a compact reply against a batch.
//
// Grammar, one item per question, separated by spaces, commas, semicolons or
// new lines:
//
//	<n><letters>   choose option(s): 1a, 3ac (letters together only when the
//	               question allows several), 2B; "1 a", "1.a" and "1) a" work too
//	<n>: <text>    answer in your own words. When this starts its line, the
//	               text runs to the end of the line. After other items on the
//	               same line ("1a 4: own words 5a") it runs until a later item
//	               such as "5a" or "6:" starts; each answer read that way is
//	               marked (Answer.AfterTextOf) so the reader can be told.
//	               Following lines that do not start an item continue it.
//
// Every part that does not parse is reported as an Issue naming it, so a
// transport can echo back exactly what it could not read. Parse never guesses:
// an unknown question, an out-of-range letter or two letters for a one-answer
// question is an issue, not a best effort.
func Parse(batch Batch, reply string) Result {
	p := &parser{batch: batch, answered: map[int]bool{}}
	var ignored []string
	lastTextIndex := -1
	lines := strings.Split(strings.ReplaceAll(reply, "\r\n", "\n"), "\n")
	for index := 0; index < len(lines); index++ {
		trimmed := strings.TrimSpace(lines[index])
		if trimmed == "" {
			lastTextIndex = -1
			continue
		}
		if lastTextIndex < 0 && len(p.answers) > 0 && index > 0 && strings.TrimSpace(lines[index-1]) == "" {
			end := index
			for end < len(lines) && strings.TrimSpace(lines[end]) != "" {
				end++
			}
			if paragraph := lines[index:end]; !p.hasItem(paragraph) {
				ignored = append(ignored, strings.TrimSpace(strings.Join(paragraph, "\n")))
				index = end - 1
				continue
			}
		}
		if lastTextIndex >= 0 && !p.startsItem(trimmed, 0) {
			// A wrapped free-text answer continues on the next line.
			p.answers[lastTextIndex].Text = strings.TrimSpace(p.answers[lastTextIndex].Text + "\n" + trimmed)
			continue
		}
		lastTextIndex = p.parseLine(trimmed)
	}
	if len(p.answers) == 0 && len(p.issues) == 0 {
		p.issues = append(p.issues, Issue{Reason: "the reply has no answers; write e.g. " + example(batch)})
	}
	sort.SliceStable(p.answers, func(i, j int) bool { return p.answers[i].Number < p.answers[j].Number })
	result := Result{Answers: p.answers, Issues: p.issues, Ignored: ignored}
	for _, q := range batch.Questions {
		if !p.answered[q.Number] {
			result.Missing = append(result.Missing, q.Number)
		}
	}
	sort.Ints(result.Missing)
	return result
}

func isSeparator(r byte) bool {
	return r == ' ' || r == '\t' || r == ',' || r == ';'
}

func isDigit(r byte) bool { return r >= '0' && r <= '9' }

func isLetter(r byte) bool { return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') }

// parseLine parses one line and returns the index into p.answers of a free
// text answer that ran to the end of the line, or -1.
func (p *parser) parseLine(s string) int {
	textAtEnd := -1
	p.tailOf = 0
	first := true
	i := 0
	for i < len(s) {
		for i < len(s) && isSeparator(s[i]) {
			i++
		}
		if i >= len(s) {
			break
		}
		textAtEnd = -1
		start := i
		leading := first
		first = false
		if !isDigit(s[i]) {
			end := nextSeparator(s, i)
			p.issue(s[start:end], 0, "not an answer; write <question><letter> such as "+example(p.batch)+", or <question>: your own words")
			i = end
			continue
		}
		j := i
		for j < len(s) && isDigit(s[j]) {
			j++
		}
		number, _ := strconv.Atoi(s[i:j])
		k := skipSpaces(s, j)
		if k < len(s) && s[k] == ':' {
			end := len(s)
			if !leading {
				end = p.freeTextEnd(s, k+1, number)
			}
			text := strings.TrimSpace(s[k+1 : end])
			if index := p.addText(strings.TrimSpace(s[start:end]), number, text); index >= 0 && end == len(s) {
				textAtEnd = index
			}
			if end < len(s) {
				p.tailOf = number
			}
			i = end
			continue
		}
		if k < len(s) && (s[k] == '.' || s[k] == ')' || s[k] == '-') {
			k = skipSpaces(s, k+1)
		}
		letterStart := k
		for k < len(s) && isLetter(s[k]) {
			k++
		}
		letters := s[letterStart:k]
		if letters == "" || (k < len(s) && !isSeparator(s[k]) && !isDigit(s[k])) {
			end := nextSeparator(s, k)
			reason := "needs an option letter after the question number, or a colon and your own words"
			if letters != "" {
				reason = "is not an option; write the question number then its letters, such as " + example(p.batch)
			}
			p.issue(s[start:end], number, reason)
			i = end
			continue
		}
		p.addChoices(s[start:k], number, letters)
		i = k
	}
	return textAtEnd
}

func skipSpaces(s string, i int) int {
	for i < len(s) && (s[i] == ' ' || s[i] == '\t') {
		i++
	}
	return i
}

func nextSeparator(s string, i int) int {
	for i < len(s) && !isSeparator(s[i]) {
		i++
	}
	return i
}

// freeTextEnd finds where a free-text answer for question `number` stops:
// the start of a later item on the same line, else the end of the line.
func (p *parser) freeTextEnd(s string, from, number int) int {
	for i := from; i < len(s); i++ {
		if !isSeparator(s[i-1]) || !isDigit(s[i]) {
			continue
		}
		if p.itemAt(s, i, number) {
			return i
		}
	}
	return len(s)
}

// hasItem reports whether any token of a paragraph starts an answer item.
func (p *parser) hasItem(lines []string) bool {
	for _, line := range lines {
		s := strings.TrimSpace(line)
		for i := 0; i < len(s); i++ {
			if (i == 0 || isSeparator(s[i-1])) && isDigit(s[i]) && p.itemAt(s, i, 0) {
				return true
			}
		}
	}
	return false
}

// startsItem reports whether a line begins with a well-formed item, which
// stops a wrapped free-text answer from swallowing it.
func (p *parser) startsItem(s string, i int) bool {
	return i < len(s) && isDigit(s[i]) && p.itemAt(s, i, 0)
}

// itemAt reports whether s[i:] starts an answer item for a question other
// than `current` that has not been answered yet: digits naming a question,
// then a colon, or option letters valid for it that end at a boundary.
func (p *parser) itemAt(s string, i, current int) bool {
	j := i
	for j < len(s) && isDigit(s[j]) {
		j++
	}
	number, err := strconv.Atoi(s[i:j])
	if err != nil || number == current || p.answered[number] {
		return false
	}
	q, ok := p.batch.Question(number)
	if !ok {
		return false
	}
	k := skipSpaces(s, j)
	if k < len(s) && s[k] == ':' {
		return true
	}
	if k != j {
		return false
	}
	letterStart := k
	for k < len(s) && isLetter(s[k]) {
		k++
	}
	if k == letterStart || (k < len(s) && !isSeparator(s[k])) {
		return false
	}
	_, problem := choicesFor(q, s[letterStart:k])
	return problem == ""
}

func (p *parser) issue(fragment string, number int, reason string) {
	p.issues = append(p.issues, Issue{Fragment: fragment, Number: number, Reason: reason})
}

// claim checks that `number` is a question that is still unanswered.
func (p *parser) claim(fragment string, number int) (Question, bool) {
	q, ok := p.batch.Question(number)
	if !ok {
		p.issue(fragment, number, fmt.Sprintf("there is no question %d; questions are %s", number, numberList(p.batch)))
		return Question{}, false
	}
	if p.answered[number] {
		p.issue(fragment, number, fmt.Sprintf("question %d is answered twice", number))
		return Question{}, false
	}
	return q, true
}

func (p *parser) addText(fragment string, number int, text string) int {
	q, ok := p.claim(fragment, number)
	if !ok {
		return -1
	}
	if text == "" {
		p.issue(fragment, number, fmt.Sprintf("write your answer to question %d after the colon", q.Number))
		return -1
	}
	p.answered[number] = true
	p.answers = append(p.answers, Answer{Number: number, Text: text, Fragment: fragment, AfterTextOf: p.tailOf})
	return len(p.answers) - 1
}

func (p *parser) addChoices(fragment string, number int, letters string) {
	q, ok := p.claim(fragment, number)
	if !ok {
		return
	}
	choices, problem := choicesFor(q, letters)
	if problem != "" {
		p.issue(fragment, number, problem)
		return
	}
	p.answered[number] = true
	p.answers = append(p.answers, Answer{Number: number, Choices: choices, Fragment: fragment, AfterTextOf: p.tailOf})
}

func choicesFor(q Question, letters string) ([]int, string) {
	if len(q.Options) == 0 {
		return nil, fmt.Sprintf("question %d has no options; answer it as %d: your own words", q.Number, q.Number)
	}
	seen := map[int]bool{}
	choices := []int{}
	for _, r := range strings.ToLower(letters) {
		index := int(r - 'a')
		if index < 0 || index >= len(q.Options) {
			return nil, fmt.Sprintf("question %d has options %s", q.Number, letterRange(len(q.Options)))
		}
		if !seen[index] {
			seen[index] = true
			choices = append(choices, index)
		}
	}
	if len(choices) > 1 && !q.MultiSelect {
		return nil, fmt.Sprintf("question %d takes one option", q.Number)
	}
	sort.Ints(choices)
	return choices, ""
}

func letterRange(n int) string {
	if n == 1 {
		return "a"
	}
	return "a-" + letter(n-1)
}

func numberList(b Batch) string {
	parts := make([]string, 0, len(b.Questions))
	for _, q := range b.Questions {
		parts = append(parts, strconv.Itoa(q.Number))
	}
	return strings.Join(parts, ", ")
}

// example is a short reply in the batch's own shape, used in hints.
func example(b Batch) string {
	parts := []string{}
	for _, q := range b.Questions {
		if len(parts) == 2 {
			break
		}
		if len(q.Options) > 0 {
			parts = append(parts, strconv.Itoa(q.Number)+"a")
		}
	}
	if len(parts) == 0 {
		return "1: your answer"
	}
	return strings.Join(parts, " ")
}

// Summary lists the parsed answers, one line per question, for the reader to
// confirm before anything is recorded, then names the questions that stay open.
func Summary(b Batch, r Result) string {
	var out strings.Builder
	for _, answer := range r.Answers {
		q, ok := b.Question(answer.Number)
		if !ok {
			continue
		}
		label := ""
		if len(answer.Choices) > 0 {
			letters := ""
			for _, c := range answer.Choices {
				letters += letter(c)
			}
			label = letters + ") "
		}
		fmt.Fprintf(&out, "%d. %s\n   %s%s\n", q.Number, oneLine(q.Title), label, answer.ResponseText(q))
	}
	if len(r.Missing) > 0 {
		parts := make([]string, 0, len(r.Missing))
		for _, n := range r.Missing {
			parts = append(parts, strconv.Itoa(n))
		}
		fmt.Fprintf(&out, "Still open: %s\n", strings.Join(parts, ", "))
	}
	for _, caution := range Cautions(r) {
		out.WriteString("Check: " + caution + "\n")
	}
	for _, paragraph := range r.Ignored {
		fmt.Fprintf(&out, "Not read as an answer: %s\n", clip(oneLine(paragraph), 80))
	}
	return strings.TrimRight(out.String(), "\n")
}

// Problems names every part of the reply that did not parse, one per line,
// followed by any answer read out of the middle of someone's own words, which
// is often why the rest of that line did not parse.
func Problems(r Result) string {
	lines := make([]string, 0, len(r.Issues))
	for _, issue := range r.Issues {
		lines = append(lines, "- "+issue.String())
	}
	for _, caution := range Cautions(r) {
		lines = append(lines, "- "+caution)
	}
	return strings.Join(lines, "\n")
}

// Cautions explains, in plain words, each answer that was read from the tail
// of another question's own words on the same line.
func Cautions(r Result) []string {
	var out []string
	for _, answer := range r.Answers {
		if answer.AfterTextOf == 0 {
			continue
		}
		out = append(out, fmt.Sprintf("%q came after your own words for question %d, so it was read as an answer to question %d and question %d's answer stops before it. If it belongs to question %d's answer, put that answer on its own line.",
			answer.Fragment, answer.AfterTextOf, answer.Number, answer.AfterTextOf, answer.AfterTextOf))
	}
	return out
}

func oneLine(s string) string {
	return strings.Join(strings.FieldsFunc(s, unicode.IsSpace), " ")
}
