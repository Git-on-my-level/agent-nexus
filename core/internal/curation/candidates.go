// Package curation generates formula fallback suggestions for agent review.
// Agents decide whether to apply or dismiss them. The generators use caller-supplied
// snapshots so they never touch storage and cannot act on anything themselves.
package curation

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// Action names the kind of change a proposal suggests.
type Action string

const (
	// ActionWithdraw suggests withdrawing an ask that has been overtaken.
	ActionWithdraw Action = "withdraw"
	// ActionMerge suggests merging a duplicate initiative into another.
	ActionMerge Action = "merge"
	// ActionBatch suggests filing related asks together.
	ActionBatch Action = "batch"
)

// Bounds keep every generator O(n^2) at worst on a fixed n; callers pass at
// most this many items and generators truncate anything beyond it.
const (
	MaxAsks                   = 200
	MaxCards                  = 200
	MaxBatchMembers           = 10
	MinBatchMembers           = 2
	MaxTitleRunes             = 160
	DefaultDuplicateThreshold = 0.8
)

// AskInfo is the minimal view of an ask the generators need.
type AskInfo struct {
	ID               string
	Title            string
	ThreadID         string
	SubjectRef       string
	RelatedRefs      []string
	RequesterActorID string
	// BatchID is non-empty when the ask is already filed in a batch.
	BatchID   string
	CreatedAt time.Time
	// Restricted marks an ask under an access policy narrower than the
	// workspace; a formula proposal never names it.
	Restricted bool
	// Proposal marks an ask that is itself a curation proposal; it is never a
	// target, so proposals cannot recurse.
	Proposal bool
}

// Ref returns the event reference for the ask.
func (a AskInfo) Ref() string { return "event:" + a.ID }

// CardInfo is the minimal view of an initiative card the generators need.
type CardInfo struct {
	Ref         string
	Title       string
	Phase       string
	RelatedRefs []string
	CreatedAt   time.Time
	// Restricted cards are skipped so a proposal never names them.
	Restricted bool
}

// MootResult is the caller's verdict on whether an ask has been overtaken.
type MootResult struct {
	Flagged bool
	// Reason carries counts only, never source refs or titles.
	Reason  string
	Sources int
}

// Candidate is one proposed curation. It is a suggestion, never an action.
type Candidate struct {
	// Key is a stable dedupe key so re-running never files a proposal twice.
	Key     string
	Action  Action
	Targets []string
	Title   string
	Body    string
	// Rationale is the formula's one-sentence reason.
	Rationale string
	Score     float64
	// ResponseProposals retains legacy labels for older candidate readers.
	// Suggestions never use these labels to ask a human.
	ResponseProposals []string
	ThreadID          string
	SubjectRef        string
}

// Key returns a stable identifier for an action over a set of targets. Target
// order is irrelevant so the same proposal always dedupes to the same key.
func Key(action Action, targets []string) string {
	sorted := append([]string(nil), targets...)
	sort.Strings(sorted)
	sum := sha256.Sum256([]byte(string(action) + "\n" + strings.Join(sorted, "\n")))
	return "cur_" + hex.EncodeToString(sum[:])[:24]
}

var stopwords = map[string]struct{}{
	"a": {}, "an": {}, "the": {}, "and": {}, "or": {}, "of": {}, "for": {},
	"to": {}, "in": {}, "on": {}, "with": {}, "is": {}, "are": {}, "be": {},
	"by": {}, "at": {}, "from": {}, "this": {}, "that": {},
}

// tokens lowercases and splits on non-letters/digits, dropping stopwords and
// single-rune tokens, so cosmetic title differences do not hide duplicates.
func tokens(s string) []string {
	fields := strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		if utf8.RuneCountInString(f) < 2 {
			continue
		}
		if _, stop := stopwords[f]; stop {
			continue
		}
		out = append(out, f)
	}
	return out
}

// Similarity returns a token-set Dice coefficient in [0,1]. Equal normalized
// token sequences score 1; text with no meaningful tokens scores 0.
func Similarity(a, b string) float64 {
	ta, tb := tokens(a), tokens(b)
	if len(ta) == 0 || len(tb) == 0 {
		return 0
	}
	if strings.Join(ta, " ") == strings.Join(tb, " ") {
		return 1
	}
	sa := make(map[string]struct{}, len(ta))
	for _, t := range ta {
		sa[t] = struct{}{}
	}
	sb := make(map[string]struct{}, len(tb))
	for _, t := range tb {
		sb[t] = struct{}{}
	}
	shared := 0
	for t := range sa {
		if _, ok := sb[t]; ok {
			shared++
		}
	}
	return 2 * float64(shared) / float64(len(sa)+len(sb))
}

// truncateRunes shortens s to at most max runes, ending in an ellipsis. It
// counts runes so multibyte titles are never cut mid-character.
func truncateRunes(s string, max int) string {
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	r := []rune(s)
	return string(r[:max-1]) + "…"
}

// oneLine collapses whitespace so titles stay a single line.
func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

func sortCandidates(c []Candidate) {
	sort.Slice(c, func(i, j int) bool { return c[i].Key < c[j].Key })
}

func capAsks(asks []AskInfo) []AskInfo {
	if len(asks) > MaxAsks {
		return asks[:MaxAsks]
	}
	return asks
}

// MootCandidates proposes withdrawing asks whose linked sources have all been
// overtaken, as judged by moot. Rationale and body never name source refs, so
// a proposal cannot leak a source the reader may not see.
func MootCandidates(asks []AskInfo, moot func(refs []string) MootResult) []Candidate {
	if moot == nil {
		return nil
	}
	var out []Candidate
	for _, a := range capAsks(asks) {
		if a.Restricted || a.Proposal {
			continue
		}
		// The subject card is the ask's container, not a source: a closed
		// subject already withdraws the ask through the lifecycle sweep, so
		// only the related sources decide whether the ask was overtaken.
		subject := a.SubjectRef
		if strings.HasPrefix(subject, "card:") {
			subject = ""
		}
		refs := dedupeRefs(append([]string{subject}, a.RelatedRefs...))
		if len(refs) == 0 {
			continue
		}
		res := moot(refs)
		if !res.Flagged || res.Sources <= 0 {
			continue
		}
		reason := strings.TrimSpace(strings.TrimRight(strings.TrimSpace(res.Reason), "."))
		for _, r := range refs {
			if strings.Contains(reason, r) {
				reason = "" // never echo a source ref, even if the caller did
				break
			}
		}
		var rationale string
		if reason == "" {
			rationale = fmt.Sprintf("Overtaken: %d linked sources suggest this is no longer needed.", res.Sources)
		} else {
			rationale = "Overtaken: " + reason + "."
		}
		title := oneLine(a.Title)
		target := a.Ref()
		out = append(out, Candidate{
			Key:               Key(ActionWithdraw, []string{target}),
			Action:            ActionWithdraw,
			Targets:           []string{target},
			Title:             truncateRunes(fmt.Sprintf("Withdraw %q?", title), MaxTitleRunes),
			Body:              fmt.Sprintf("%s\n\nAsk: %s\n\nWithdraw it, or keep it open if it still matters.", rationale, title),
			Rationale:         rationale,
			Score:             1,
			ResponseProposals: []string{"Withdraw", "Keep"},
			ThreadID:          a.ThreadID,
			SubjectRef:        a.SubjectRef,
		})
	}
	sortCandidates(out)
	return out
}

func dedupeRefs(in []string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, r := range in {
		r = strings.TrimSpace(r)
		if r == "" {
			continue
		}
		if _, ok := seen[r]; ok {
			continue
		}
		seen[r] = struct{}{}
		out = append(out, r)
	}
	return out
}

var closedPhases = map[string]struct{}{
	"done": {}, "cancelled": {}, "canceled": {}, "archived": {}, "resolved": {},
}

var contextKinds = map[string]struct{}{
	"doc": {}, "document": {}, "topic": {}, "board": {}, "thread": {},
}

// workRefs keeps refs that identify shared work; context refs (docs, topics,
// boards, threads) are shared too widely to signal duplication.
func workRefs(in []string) map[string]struct{} {
	out := map[string]struct{}{}
	for _, r := range in {
		r = strings.TrimSpace(r)
		if r == "" {
			continue
		}
		kind, _, _ := strings.Cut(r, ":")
		if _, ctx := contextKinds[strings.ToLower(kind)]; ctx {
			continue
		}
		out[r] = struct{}{}
	}
	return out
}

// refOverlap is the Jaccard overlap of work refs, counted only when at least
// two refs are shared so one common link is not mistaken for duplication.
func refOverlap(a, b []string) float64 {
	sa, sb := workRefs(a), workRefs(b)
	shared := 0
	for r := range sa {
		if _, ok := sb[r]; ok {
			shared++
		}
	}
	if shared < 2 {
		return 0
	}
	return float64(shared) / float64(len(sa)+len(sb)-shared)
}

// DuplicateCandidates proposes merging open cards that look like the same
// initiative. Each card appears in at most one proposal, highest score first,
// and the older card is the one kept.
func DuplicateCandidates(cards []CardInfo, threshold float64) []Candidate {
	if threshold <= 0 {
		threshold = DefaultDuplicateThreshold
	}
	if len(cards) > MaxCards {
		cards = cards[:MaxCards]
	}
	var open []CardInfo
	for _, c := range cards {
		if c.Restricted || c.Ref == "" {
			continue
		}
		if _, closed := closedPhases[strings.ToLower(strings.TrimSpace(c.Phase))]; closed {
			continue
		}
		open = append(open, c)
	}
	type pair struct {
		dup, kept CardInfo
		score     float64
	}
	var pairs []pair
	for i := 0; i < len(open); i++ {
		for j := i + 1; j < len(open); j++ {
			a, b := open[i], open[j]
			score := Similarity(a.Title, b.Title)
			if o := refOverlap(a.RelatedRefs, b.RelatedRefs); o > score {
				score = o
			}
			if score < threshold {
				continue
			}
			kept, dup := a, b
			if b.CreatedAt.Before(a.CreatedAt) || (b.CreatedAt.Equal(a.CreatedAt) && b.Ref < a.Ref) {
				kept, dup = b, a
			}
			pairs = append(pairs, pair{dup: dup, kept: kept, score: score})
		}
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].score != pairs[j].score {
			return pairs[i].score > pairs[j].score
		}
		if pairs[i].kept.Ref != pairs[j].kept.Ref {
			return pairs[i].kept.Ref < pairs[j].kept.Ref
		}
		return pairs[i].dup.Ref < pairs[j].dup.Ref
	})
	used := map[string]struct{}{}
	var out []Candidate
	for _, p := range pairs {
		if _, ok := used[p.dup.Ref]; ok {
			continue
		}
		if _, ok := used[p.kept.Ref]; ok {
			continue
		}
		used[p.dup.Ref], used[p.kept.Ref] = struct{}{}, struct{}{}
		targets := []string{p.dup.Ref, p.kept.Ref}
		dupTitle, keptTitle := oneLine(p.dup.Title), oneLine(p.kept.Title)
		rationale := fmt.Sprintf("These initiatives look alike (%.0f%% match).", p.score*100)
		out = append(out, Candidate{
			Key:     Key(ActionMerge, targets),
			Action:  ActionMerge,
			Targets: targets,
			Title:   truncateRunes(fmt.Sprintf("Merge %q into %q?", dupTitle, keptTitle), MaxTitleRunes),
			Body: fmt.Sprintf("%s\n\n- Duplicate: %s\n- Kept: %s\n\nThe older card is kept.",
				rationale, dupTitle, keptTitle),
			Rationale:         rationale,
			Score:             p.score,
			ResponseProposals: []string{"Merge", "Keep both"},
			SubjectRef:        p.kept.Ref,
		})
	}
	sortCandidates(out)
	return out
}

// BatchCandidates proposes filing open asks that share a subject (or, for asks
// without a subject, a thread) into one batch. Large groups are split into
// chunks of MaxBatchMembers so a single batch stays reviewable.
func BatchCandidates(asks []AskInfo) []Candidate {
	subjects := map[string][]AskInfo{}
	threads := map[string][]AskInfo{}
	for _, a := range capAsks(asks) {
		if a.Restricted || a.Proposal || a.BatchID != "" {
			continue
		}
		switch {
		case a.SubjectRef != "":
			subjects[a.SubjectRef] = append(subjects[a.SubjectRef], a)
		case a.ThreadID != "":
			threads[a.ThreadID] = append(threads[a.ThreadID], a)
		}
	}
	var out []Candidate
	emit := func(groups map[string][]AskInfo, bySubject bool) {
		for key, members := range groups {
			sort.SliceStable(members, func(i, j int) bool {
				if !members[i].CreatedAt.Equal(members[j].CreatedAt) {
					return members[i].CreatedAt.Before(members[j].CreatedAt)
				}
				return members[i].ID < members[j].ID
			})
			for start := 0; start < len(members); start += MaxBatchMembers {
				end := start + MaxBatchMembers
				if end > len(members) {
					end = len(members)
				}
				chunk := members[start:end]
				if len(chunk) < MinBatchMembers {
					continue
				}
				subject := key
				if !bySubject {
					subject = chunk[0].SubjectRef
				}
				targets := make([]string, len(chunk))
				var body strings.Builder
				body.WriteString("These open asks look related:\n\n")
				for i, m := range chunk {
					targets[i] = m.Ref()
					body.WriteString("- " + oneLine(m.Title) + "\n")
				}
				rationale := "These asks share a subject."
				if !bySubject {
					rationale = "These asks share a thread."
				}
				out = append(out, Candidate{
					Key:               Key(ActionBatch, targets),
					Action:            ActionBatch,
					Targets:           targets,
					Title:             truncateRunes(fmt.Sprintf("Batch %d related asks?", len(chunk)), MaxTitleRunes),
					Body:              body.String() + "\n" + rationale,
					Rationale:         rationale,
					Score:             1,
					ResponseProposals: []string{"Batch", "Keep separate"},
					ThreadID:          chunk[0].ThreadID,
					SubjectRef:        subject,
				})
			}
		}
	}
	emit(subjects, true)
	emit(threads, false)
	sortCandidates(out)
	return out
}
