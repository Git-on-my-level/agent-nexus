package plans

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// One clock: promise versus evidence (contracts/anx-schema.yaml `promises`).
// A promise is a field on things agents already write: a note's next, an
// open plan step's due, the card due_at and the wake_condition date. The fold
// below is a pure function of those facts plus reset events, so the counted
// promise can be materialized on write and only past/moving needs a clock.

// Promise states. These are the only health states a human sees.
const (
	PromisePast    = "past_promise"
	PromiseMoving  = "moving"
	PromiseNone    = "no_promise"
	PromiseDone    = "done"
	PromiseHorizon = 2 * 24 * time.Hour
	// NoPromiseFallback is the single duration fallback for objects with no
	// promise: a no_promise initiative whose last evidence is older than this
	// is "quiet" and sorts first among no_promise rows. It never creates a
	// state. TestStalenessKnobs pins this to the only knob.
	NoPromiseFallback = 7 * 24 * time.Hour
	// MaxPromiseNotes bounds the notes one fold reads per card.
	MaxPromiseNotes = 50
	maxPromiseWhat  = 160
)

// PromiseNote is one message_posted fact on the card.
type PromiseNote struct {
	EventID  string
	At       time.Time // event ts
	Author   string
	NextAt   time.Time // zero when the note carries no next
	NextWhat string
	// Evidence is true when at least one evidence ref resolved at write time.
	Evidence bool
}

// PromiseStep is an open plan step with a due date.
type PromiseStep struct {
	ID, Title string
	Due       time.Time
}

// PromiseInput is everything the fold needs for one card.
type PromiseInput struct {
	Ref   string
	Done  bool
	Notes []PromiseNote // any order; the newest MaxPromiseNotes are used
	Steps []PromiseStep // open steps only
	// StepChanges and Observations are reset events (plan-step state changes
	// and changed source observations), newest last or in any order.
	StepChanges  []time.Time
	Observations []time.Time
	CardDue      time.Time
	Wake         time.Time
}

// Promise is the counted promise.
type Promise struct {
	At        time.Time `json:"at"`
	What      string    `json:"what"`
	Source    string    `json:"source"`
	SourceRef string    `json:"source_ref,omitempty"`
	Author    string    `json:"author,omitempty"`
}

// PromiseFold is the time-independent result of folding a card's facts.
type PromiseFold struct {
	Done           bool
	Promise        *Promise
	Moves          int
	LastEvidenceAt time.Time
}

// PromiseState is the read-time view of a fold.
type PromiseState struct {
	State          string
	Promise        *Promise
	PastPromiseFor *time.Duration
	Moves          int
	LastEvidenceAt time.Time
}

// FoldPromise derives the counted promise, the move count and the last
// evidence time. Rules:
//   - Resets are a note with resolved evidence, a plan-step state change and a
//     changed source observation. last_evidence_at is the newest reset.
//   - A note promise counts for the author's latest note carrying next. It is
//     met (and stops counting) when any reset lands after the note; a reset
//     landing after its at ends the miss as well, so late evidence clears it.
//   - Step, due_at and wake promises have no made-at in the fold. A past one
//     is met when a reset landed within the horizon before its at, (at-2d, at];
//     otherwise it counts while the step or card stays open.
//   - The counted promise is the earliest unmet at.
//   - A move is a note without evidence that changes its author's previous
//     next.at; moves count since the last reset.
func FoldPromise(in PromiseInput) PromiseFold {
	out := PromiseFold{Done: in.Done}
	notes := append([]PromiseNote(nil), in.Notes...)
	sort.SliceStable(notes, func(i, j int) bool { return notes[i].At.Before(notes[j].At) })
	if len(notes) > MaxPromiseNotes {
		notes = notes[len(notes)-MaxPromiseNotes:]
	}
	resets := make([]time.Time, 0, len(in.StepChanges)+len(in.Observations)+len(notes))
	resets = append(resets, in.StepChanges...)
	resets = append(resets, in.Observations...)
	for _, n := range notes {
		if n.Evidence {
			resets = append(resets, n.At)
		}
	}
	for _, r := range resets {
		if r.After(out.LastEvidenceAt) {
			out.LastEvidenceAt = r
		}
	}
	if in.Done {
		return out
	}
	resetIn := func(from, to time.Time) bool { // (from, to]
		for _, r := range resets {
			if r.After(from) && !r.After(to) {
				return true
			}
		}
		return false
	}
	var candidates []Promise
	latest := map[string]PromiseNote{}
	previous := map[string]time.Time{}
	for _, n := range notes {
		if n.NextAt.IsZero() {
			continue
		}
		prev, ok := previous[n.Author]
		if ok && !prev.Equal(n.NextAt) && !n.Evidence && n.At.After(out.LastEvidenceAt) {
			out.Moves++
		}
		previous[n.Author] = n.NextAt
		latest[n.Author] = n
	}
	for author, n := range latest {
		if resetIn(n.At, farFuture) {
			continue
		}
		candidates = append(candidates, Promise{At: n.NextAt, What: clipWhat(n.NextWhat), Source: "note", SourceRef: eventRef(n.EventID), Author: author})
	}
	fixed := func(at time.Time, what, source, ref string) {
		if at.IsZero() || resetIn(at.Add(-PromiseHorizon), at) {
			return
		}
		candidates = append(candidates, Promise{At: at, What: clipWhat(what), Source: source, SourceRef: ref})
	}
	for _, s := range in.Steps {
		ref := ""
		if in.Ref != "" && s.ID != "" {
			ref = in.Ref + "#" + s.ID
		}
		fixed(s.Due, s.Title, "plan_step", ref)
	}
	fixed(in.CardDue, "due", "due_at", in.Ref)
	fixed(in.Wake, "wake", "wake_condition", in.Ref)
	sort.SliceStable(candidates, func(i, j int) bool {
		if !candidates[i].At.Equal(candidates[j].At) {
			return candidates[i].At.Before(candidates[j].At)
		}
		return candidates[i].Source+candidates[i].SourceRef < candidates[j].Source+candidates[j].SourceRef
	})
	if len(candidates) > 0 {
		p := candidates[0]
		out.Promise = &p
	}
	return out
}

var farFuture = time.Date(9999, 1, 1, 0, 0, 0, 0, time.UTC)

// At reads a fold at now.
func (f PromiseFold) At(now time.Time) PromiseState {
	s := PromiseState{Promise: f.Promise, Moves: f.Moves, LastEvidenceAt: f.LastEvidenceAt}
	switch {
	case f.Done:
		s.State, s.Moves = PromiseDone, 0
		s.Promise = nil
	case f.Promise == nil:
		s.State = PromiseNone
	case f.Promise.At.Before(now):
		s.State = PromisePast
		d := now.Sub(f.Promise.At).Truncate(time.Second)
		s.PastPromiseFor = &d
	default:
		s.State = PromiseMoving
	}
	return s
}

// Reason is a one-line computed explanation of the state.
func (s PromiseState) Reason(now time.Time) string {
	switch s.State {
	case PromiseDone:
		return "Done."
	case PromiseNone:
		if s.LastEvidenceAt.IsZero() {
			return "No promise and no evidence yet."
		}
		return fmt.Sprintf("No promise; last evidence %s ago.", humanDuration(now.Sub(s.LastEvidenceAt)))
	case PromisePast:
		r := fmt.Sprintf("Past its %s promise by %s.", s.Promise.Source, humanDuration(*s.PastPromiseFor))
		if s.Moves > 0 {
			r += fmt.Sprintf(" Moved %d×.", s.Moves)
		}
		return r
	default:
		return fmt.Sprintf("Next: %s by %s.", firstNonBlank(s.Promise.What, s.Promise.Source), s.Promise.At.UTC().Format(time.RFC3339))
	}
}

func firstNonBlank(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func humanDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	switch {
	case d >= 48*time.Hour:
		return fmt.Sprintf("%dd", int(d/(24*time.Hour)))
	case d >= time.Hour:
		return fmt.Sprintf("%dh", int(d/time.Hour))
	default:
		return fmt.Sprintf("%dm", int(d/time.Minute))
	}
}

func clipWhat(s string) string {
	s = strings.TrimSpace(s)
	r := []rune(s)
	if len(r) > maxPromiseWhat {
		return string(r[:maxPromiseWhat])
	}
	return s
}

func eventRef(id string) string {
	if id == "" {
		return ""
	}
	return "event:" + id
}

var promiseDurationPattern = regexp.MustCompile(`^([1-9][0-9]*)([smhd])$`)

// ParseWhen resolves the review_by grammar against from: a UTC date
// (YYYY-MM-DD, midnight UTC), a zoned RFC3339 timestamp, or a positive
// s/m/h/d duration of at most 3650d. ok is false for anything else.
func ParseWhen(raw string, from time.Time) (time.Time, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, false
	}
	if m := promiseDurationPattern.FindStringSubmatch(raw); m != nil {
		n, err := strconv.Atoi(m[1])
		if err != nil {
			return time.Time{}, false
		}
		unit := map[string]time.Duration{"s": time.Second, "m": time.Minute, "h": time.Hour, "d": 24 * time.Hour}[m[2]]
		d := time.Duration(n) * unit
		if d <= 0 || d > 3650*24*time.Hour {
			return time.Time{}, false
		}
		return from.Add(d).UTC(), true
	}
	if at, err := time.Parse(time.RFC3339Nano, raw); err == nil {
		return at.UTC(), true
	}
	if at, err := time.Parse("2006-01-02", raw); err == nil {
		return at, true
	}
	return time.Time{}, false
}

// ParseDue parses a stored due date (RFC3339 or YYYY-MM-DD).
func ParseDue(raw string) time.Time {
	if at, err := time.Parse(time.RFC3339Nano, raw); err == nil {
		return at.UTC()
	}
	if at, err := time.Parse("2006-01-02", raw); err == nil {
		return at
	}
	return time.Time{}
}
