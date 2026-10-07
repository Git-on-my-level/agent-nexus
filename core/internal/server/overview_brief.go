package server

import (
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"agent-nexus-core/internal/commandcenter"
	"agent-nexus-core/internal/plans"
	"agent-nexus-core/internal/primitives"
)

/*
The morning brief: five answers, computed, above the fold.

	What needs my decision?      brief.decisions
	What changed since I looked? brief.since_last_look
	What is at risk?             brief.at_risk
	Is the machine running?      brief.machine
	Where is each initiative?    brief.initiatives

Every value here is derived from rows this request has already loaded and
already filtered — the visit work snapshot, the assembled needs_you rows, the
digest and the roster. The brief issues no query of its own, so it adds no
per-request database cost and cannot see further than the caller can. A count
it reports is a count of authorized rows: where the caller cannot see
something, the number is lower, never a hint that something is hidden.
*/

// How many rows each brief section shows before "+N more".
const briefSectionLimit = 5

// Window for the machine section's throughput number.
const briefThroughputWindow = 24 * time.Hour

// Severity order for the at-risk section, worst first.
var briefRiskOrder = map[string]int{"blocked": 0, "at_risk": 1, "stale": 2}

/*
Initiative order, worst first, matching the client's ATTENTION_ORDER so a
tile, a brief row and a list row cannot disagree about which initiative is
more urgent. no_plan sorts last because it is a gap to fill rather than a fire
to put out — the section's state counts are what make it visible.
*/
var briefInitiativeOrder = []string{"blocked", "at_risk", "stale", "on_track", "done", "no_plan"}

// briefSignal is the computed ranking input for one needs_you row. It is built
// where the row is built, from that row's own source, and keyed by row id.
type briefSignal struct {
	Kind     string // task | ask | decision
	At       string // age anchor: when this started waiting
	Priority string
	DueAt    string
	Phase    string
	Blocks   int // open rows, visible to this caller, that depend on this one
}

type briefInputs struct {
	now              time.Time
	since            *string
	work             []map[string]any
	workTruncated    bool
	needsRows        []map[string]any
	needsTruncated   bool
	needsOK          bool
	signals          map[string]briefSignal
	changes          []primitives.OverviewChange
	changesTruncated bool
	initiatives      []map[string]any
	roster           []commandcenter.Summary
	rosterOK         bool
	rosterTruncated  bool
	newAsks          []map[string]any
	dependents       map[string]int
	/*
	 * Whether this request produced the visit work snapshot the brief reads.
	 * A store that answers Overview without one can still rank the needs_you
	 * rows, but it cannot know what is off track or what closed today — and
	 * reporting "0 at risk, 0 finished" from rows nobody counted would be the
	 * same kind of confident wrong answer the brief exists to remove.
	 */
	workAvailable bool
}

/*
recordWorkSignals derives the ranking inputs for the task rows needs_you
builds from work. The same predicate decides a needs_you row there — a human
next actor, or a blocked phase — so a row and its signal cannot disagree about
which card they describe.
*/
func (in *briefInputs) recordWorkSignals(work []map[string]any) {
	for _, w := range work {
		ref := anyString(w["ref"])
		if ref == "" {
			continue
		}
		in.signals["task:"+ref] = briefSignal{
			Kind:     "task",
			At:       anyString(w["updated_at"]),
			Priority: anyString(w["priority"]),
			DueAt:    anyString(w["due_at"]),
			Phase:    anyString(w["phase"]),
			Blocks:   in.dependents[ref],
		}
	}
}

/*
briefDependents counts, per card ref, how many other open rows declare a
depends_on relation on it. One pass over rows the handler already has; no
query. A dependent the caller cannot see is not in `work`, so it is not
counted — the number under-reports rather than leaking.
*/
func briefDependents(work []map[string]any) map[string]int {
	out := map[string]int{}
	for _, w := range work {
		phase := anyString(w["phase"])
		if phase == "done" || phase == "cancelled" {
			continue
		}
		relations, _ := w["relations"].([]any)
		for _, raw := range relations {
			relation, ok := raw.(map[string]any)
			if !ok || anyString(relation["kind"]) != "depends_on" {
				continue
			}
			if ref := anyString(relation["ref"]); strings.HasPrefix(ref, "card:") {
				out[ref]++
			}
		}
	}
	return out
}

// briefText reads an optional roster string without a nil check at each use.
func briefText(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func briefParseTime(raw string) (time.Time, bool) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return time.Time{}, false
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02"} {
		if at, err := time.Parse(layout, value); err == nil {
			return at.UTC(), true
		}
	}
	return time.Time{}, false
}

// briefAge renders a duration the way an operator says it: "4h", "3d", "2w".
func briefAge(d time.Duration) string {
	switch {
	case d < time.Hour:
		minutes := int(d.Minutes())
		if minutes < 1 {
			return "now"
		}
		return strconv.Itoa(minutes) + "m"
	case d < 48*time.Hour:
		return strconv.Itoa(int(d.Hours())) + "h"
	case d < 14*24*time.Hour:
		return strconv.Itoa(int(d.Hours()/24)) + "d"
	default:
		return strconv.Itoa(int(d.Hours()/(24*7))) + "w"
	}
}

func briefPriorityWeight(priority string) int {
	switch strings.ToLower(strings.TrimSpace(priority)) {
	case "critical", "urgent":
		return 320
	case "high":
		return 200
	case "medium", "normal":
		return 80
	case "low":
		return 20
	}
	return 0
}

func briefPlural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

/*
briefRank scores one decision row and writes the one-line reason a reader
needs to accept the ranking: what it blocks, whether it is late, how
important it is and how long it has been sitting. Blocking open work
outranks everything, because it is the only signal that says other work
cannot proceed until this is answered.
*/
func briefRank(signal briefSignal, now time.Time) (int, string, map[string]any) {
	score := 0
	parts := []string{}
	facts := map[string]any{"kind": signal.Kind, "blocks": signal.Blocks}

	if signal.Blocks > 0 {
		score += 1000 * signal.Blocks
		parts = append(parts, "blocks "+strconv.Itoa(signal.Blocks)+briefPlural(signal.Blocks, " card", " cards"))
	}
	if due, ok := briefParseTime(signal.DueAt); ok {
		facts["due_at"] = due.Format(time.RFC3339Nano)
		if now.After(due) {
			score += 600
			parts = append(parts, "overdue "+briefAge(now.Sub(due)))
		} else if due.Sub(now) <= 48*time.Hour {
			score += 300
			parts = append(parts, "due in "+briefAge(due.Sub(now)))
		}
	}
	if weight := briefPriorityWeight(signal.Priority); weight >= 200 {
		score += weight
		facts["priority"] = strings.ToLower(signal.Priority)
		parts = append(parts, strings.ToLower(signal.Priority)+" priority")
	} else {
		score += weight
		if signal.Priority != "" && signal.Priority != "none" {
			facts["priority"] = strings.ToLower(signal.Priority)
		}
	}
	if signal.Phase == "blocked" {
		score += 150
		facts["phase"] = "blocked"
		parts = append(parts, "blocked")
	}
	if at, ok := briefParseTime(signal.At); ok {
		age := now.Sub(at)
		if age < 0 {
			age = 0
		}
		hours := int(age.Hours())
		if hours > 720 {
			hours = 720
		}
		score += hours
		facts["age_hours"] = int(age.Hours())
		facts["since"] = at.Format(time.RFC3339Nano)
		word := " old"
		if signal.Kind == "task" {
			word = " without a change"
		}
		parts = append(parts, briefAge(age)+word)
	}
	// Three reasons is the most a single line carries without being skimmed past.
	if len(parts) > 3 {
		parts = parts[:3]
	}
	return score, strings.Join(parts, " · "), facts
}

func briefDecisions(in briefInputs) map[string]any {
	if !in.needsOK {
		return map[string]any{"status": "unavailable", "message": "Decisions could not be ranked.", "items": []map[string]any{}, "count": 0, "more": 0, "href": "/inbox?mailbox=needs-you"}
	}
	type ranked struct {
		row   map[string]any
		score int
		at    time.Time
		id    string
	}
	rows := make([]ranked, 0, len(in.needsRows))
	for _, row := range in.needsRows {
		id := anyString(row["id"])
		signal := in.signals[id]
		score, reason, facts := briefRank(signal, in.now)
		item := map[string]any{"id": id, "title": firstNonEmptyString(row["title"], "Needs you"), "href": anyString(row["href"]), "reason": reason, "signals": facts}
		if source := anyString(row["source"]); source != "" {
			item["source"] = source
		}
		at, _ := briefParseTime(signal.At)
		rows = append(rows, ranked{row: item, score: score, at: at, id: id})
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].score != rows[j].score {
			return rows[i].score > rows[j].score
		}
		// Oldest first among equals, then by id so the order never wobbles.
		if !rows[i].at.Equal(rows[j].at) {
			if rows[i].at.IsZero() || rows[j].at.IsZero() {
				return rows[j].at.IsZero()
			}
			return rows[i].at.Before(rows[j].at)
		}
		return rows[i].id < rows[j].id
	})
	items := []map[string]any{}
	for _, row := range rows {
		if len(items) == briefSectionLimit {
			break
		}
		items = append(items, row.row)
	}
	return map[string]any{"status": "ok", "count": len(rows), "items": items, "more": len(rows) - len(items), "truncated": in.needsTruncated, "href": "/inbox?mailbox=needs-you"}
}

// briefChangeGroups is the digest's shape: ordered groups, each with a count
// and the first few items, so the section reads as "4 finished, 1 newly
// blocked" before it reads as a list.
var briefChangeGroups = []struct{ key, label string }{
	{"completed", "Finished"},
	{"newly_blocked", "Newly blocked"},
	{"new_asks", "New asks"},
	{"decisions", "New decisions"},
	{"answered", "Asks answered"},
	{"steps", "Plan steps done"},
	{"initiatives", "Initiatives off track"},
	{"cancelled", "Cancelled"},
	{"updated", "Updated"},
}

func briefSinceLastLook(in briefInputs) map[string]any {
	out := map[string]any{"since": in.since, "groups": []map[string]any{}, "total": 0, "truncated": in.changesTruncated || in.workTruncated || !in.workAvailable}
	if in.since == nil {
		// No previous visit to compare against. Saying so beats showing a
		// confident "nothing changed" the first time someone opens the page.
		out["first_visit"] = true
		return out
	}
	since, ok := briefParseTime(*in.since)
	if !ok {
		return out
	}
	grouped := map[string][]map[string]any{}
	add := func(key string, item map[string]any) {
		grouped[key] = append(grouped[key], item)
	}
	for _, w := range in.work {
		at, ok := briefParseTime(anyString(w["updated_at"]))
		if !ok || !at.After(since) || at.After(in.now) {
			continue
		}
		ref := anyString(w["ref"])
		item := map[string]any{"ref": ref, "title": anyString(w["title"]), "href": briefTaskHref(ref), "at": at.Format(time.RFC3339Nano)}
		switch anyString(w["phase"]) {
		case "done":
			add("completed", item)
		case "cancelled":
			add("cancelled", item)
		case "blocked":
			add("newly_blocked", item)
		default:
			add("updated", item)
		}
	}
	for _, ask := range in.newAsks {
		at, ok := briefParseTime(firstNonEmptyString(ask["created_at"], ask["updated_at"]))
		if !ok || !at.After(since) || at.After(in.now) {
			continue
		}
		id := "inbox:" + anyString(ask["id"])
		add("new_asks", map[string]any{"ref": id, "title": firstNonEmptyString(ask["title"], "Request"), "href": "/inbox?mailbox=needs-you&item=" + url.QueryEscape(id), "at": at.Format(time.RFC3339Nano)})
	}
	for _, change := range in.changes {
		// Not every digest kind carries a timestamp; an absent one is omitted
		// rather than published as an empty date.
		item := map[string]any{"ref": change.Ref, "title": change.Title, "href": briefChangeHref(change)}
		if change.TS != "" {
			item["at"] = change.TS
		}
		switch change.Kind {
		case "step_completed":
			add("steps", item)
		case "ask_answered":
			add("answered", item)
		case "decision_created":
			add("decisions", item)
		default:
			// initiative_stalled / initiative_blocked and anything a newer
			// core adds: an initiative that stopped being fine.
			add("initiatives", item)
		}
	}
	groups := []map[string]any{}
	total := 0
	for _, spec := range briefChangeGroups {
		items := grouped[spec.key]
		if len(items) == 0 {
			continue
		}
		total += len(items)
		sort.SliceStable(items, func(i, j int) bool { return anyString(items[i]["at"]) > anyString(items[j]["at"]) })
		shown := items
		if len(shown) > briefSectionLimit {
			shown = shown[:briefSectionLimit]
		}
		groups = append(groups, map[string]any{"key": spec.key, "label": spec.label, "count": len(items), "items": shown, "more": len(items) - len(shown)})
	}
	out["groups"] = groups
	out["total"] = total
	return out
}

func briefTaskHref(ref string) string {
	if ref == "" {
		return ""
	}
	return "/tasks/" + url.PathEscape(strings.TrimPrefix(ref, "card:"))
}

func briefChangeHref(change primitives.OverviewChange) string {
	if strings.HasPrefix(change.Ref, "card:") {
		return briefTaskHref(change.Ref)
	}
	return "/inbox?mailbox=needs-you"
}

func briefAtRisk(in briefInputs) map[string]any {
	if !in.workAvailable {
		return map[string]any{"status": "unavailable", "message": "Risk could not be computed for this reader.", "items": []map[string]any{}, "count": 0, "more": 0}
	}
	type risk struct {
		item  map[string]any
		order int
		at    time.Time
		ref   string
	}
	seen := map[string]bool{}
	risks := []risk{}
	consider := func(ref, title, state, reason string, progress any, at string) {
		order, tracked := briefRiskOrder[state]
		if !tracked || ref == "" || seen[ref] || reason == "" {
			return
		}
		seen[ref] = true
		item := map[string]any{"ref": ref, "title": title, "href": briefTaskHref(ref), "state": state, "reason": reason}
		if progress != nil {
			item["progress"] = progress
		}
		anchor, _ := briefParseTime(at)
		if !anchor.IsZero() {
			item["since"] = anchor.Format(time.RFC3339Nano)
		}
		risks = append(risks, risk{item: item, order: order, at: anchor, ref: ref})
	}
	for _, initiative := range in.initiatives {
		health, _ := initiative["health"].(map[string]any)
		state, reason := anyString(health["state"]), anyString(health["reason"])
		since := ""
		if detailed, ok := initiative["plan_health"].(plans.Health); ok && detailed.Since != nil {
			since = *detailed.Since
		}
		consider(anyString(initiative["ref"]), anyString(initiative["title"]), state, reason, initiative["progress"], firstNonEmptyString(since, initiative["updated_at"]))
	}
	/*
	 * Work that is off track without being an initiative. A card sitting in the
	 * blocked column is off track whether or not anyone wrote a plan for it, and
	 * recorded blockers are the reason, in the words whoever recorded them used.
	 */
	for _, w := range in.work {
		phase := anyString(w["phase"])
		if phase != "blocked" {
			continue
		}
		reason := "Task is in the blocked column."
		if blockers, _ := w["blockers"].([]any); len(blockers) > 0 {
			named := []string{}
			for _, raw := range blockers {
				if text := strings.TrimSpace(anyString(raw)); text != "" {
					named = append(named, text)
				}
			}
			if len(named) > 0 {
				reason = "Blocked on " + named[0]
				if len(named) > 1 {
					reason += " (+" + strconv.Itoa(len(named)-1) + " more)"
				}
			}
		}
		consider(anyString(w["ref"]), anyString(w["title"]), "blocked", reason, nil, anyString(w["updated_at"]))
	}
	sort.SliceStable(risks, func(i, j int) bool {
		if risks[i].order != risks[j].order {
			return risks[i].order < risks[j].order
		}
		// Longest-standing risk first; an unknown anchor sorts after a known one.
		if !risks[i].at.Equal(risks[j].at) {
			if risks[i].at.IsZero() || risks[j].at.IsZero() {
				return risks[j].at.IsZero()
			}
			return risks[i].at.Before(risks[j].at)
		}
		return risks[i].ref < risks[j].ref
	})
	items := []map[string]any{}
	for _, entry := range risks {
		if len(items) == briefSectionLimit {
			break
		}
		items = append(items, entry.item)
	}
	return map[string]any{"status": "ok", "count": len(risks), "items": items, "more": len(risks) - len(items), "truncated": in.workTruncated}
}

/*
briefAgentState mirrors the client's three-way split of core's silence
(#317): an agent with no recent signal is "stale" only while it is holding a
run or a card, "offline" when it simply is not running, and "inactive" when it
has never checked in. The brief needs the same split so "stuck" means
"something is riding on this silence" and not "most agents are asleep".
*/
func briefAgentState(agent commandcenter.Summary) string {
	if reported := strings.TrimSpace(agent.State); reported != "" && reported != "stale" {
		return reported
	}
	if agent.ActiveRun != nil || briefText(agent.CurrentCardRef) != "" {
		return "stale"
	}
	if briefText(agent.LastSignalAt) != "" {
		return "offline"
	}
	return "inactive"
}

func briefMachine(in briefInputs) map[string]any {
	/*
	 * Throughput comes from the work rows, not the roster: "finished" is a card
	 * reaching done, which is a fact about work. The roster only knows when an
	 * agent last spoke.
	 */
	finished, owners := 0, map[string]bool{}
	for _, w := range in.work {
		phase := anyString(w["phase"])
		if phase != "done" && phase != "cancelled" {
			continue
		}
		at, ok := briefParseTime(anyString(w["updated_at"]))
		if !ok || in.now.Sub(at) > briefThroughputWindow || at.After(in.now) {
			continue
		}
		finished++
		if owner := anyString(w["owner"]); owner != "" {
			owners[owner] = true
		}
	}
	out := map[string]any{
		"status":              "ok",
		"finished_24h":        finished,
		"agents_finished_24h": len(owners),
		"truncated":           in.workTruncated,
		"href":                "/agents",
	}
	if !in.workAvailable {
		out["finished_24h"], out["agents_finished_24h"] = nil, nil
		out["throughput_status"] = "unavailable"
	}
	if !in.rosterOK {
		// Throughput still holds; only the live side of the question is missing.
		out["working"] = nil
		out["waiting"] = nil
		out["stuck"] = nil
		out["stuck_items"] = []map[string]any{}
		out["roster_status"] = "unavailable"
		out["message"] = "Agent presence could not be loaded."
		return out
	}
	working, waiting := 0, 0
	stuck := []map[string]any{}
	for _, agent := range in.roster {
		if briefText(agent.RevokedAt) != "" {
			continue
		}
		switch briefAgentState(agent) {
		case "working":
			working++
		case "waiting_on_human":
			waiting++
		case "stale":
			reason := "No signal during an open run."
			if card := briefText(agent.CurrentCardRef); card != "" {
				reason = "No signal while holding " + firstNonEmptyString(briefText(agent.CurrentCardTitle), card) + "."
			}
			if agent.LastSignalAt != nil {
				if at, ok := briefParseTime(*agent.LastSignalAt); ok {
					reason = "Silent " + briefAge(in.now.Sub(at)) + ": " + strings.ToLower(reason[:1]) + reason[1:]
				}
			}
			row := map[string]any{"ref": agent.Ref, "title": firstNonEmptyString(agent.DisplayName, agent.Handle, agent.Name, agent.ID), "href": briefAgentHref(agent), "reason": reason}
			if agent.LastSignalAt != nil {
				row["last_signal_at"] = *agent.LastSignalAt
			}
			stuck = append(stuck, row)
		}
	}
	sort.SliceStable(stuck, func(i, j int) bool { return anyString(stuck[i]["title"]) < anyString(stuck[j]["title"]) })
	shown := stuck
	if len(shown) > briefSectionLimit {
		shown = shown[:briefSectionLimit]
	}
	out["working"] = working
	out["waiting"] = waiting
	out["stuck"] = len(stuck)
	out["stuck_items"] = shown
	out["roster_status"] = "ok"
	out["roster_truncated"] = in.rosterTruncated
	return out
}

func briefAgentHref(agent commandcenter.Summary) string {
	key := firstNonEmptyString(agent.Handle, agent.ID)
	if key == "" {
		return "/agents"
	}
	return "/agents/" + url.PathEscape(key)
}

func briefInitiatives(in briefInputs) map[string]any {
	rank := map[string]int{}
	for i, state := range briefInitiativeOrder {
		rank[state] = i
	}
	type entry struct {
		item  map[string]any
		order int
		ref   string
	}
	byState := map[string]int{}
	rows := []entry{}
	for _, initiative := range in.initiatives {
		health, _ := initiative["health"].(map[string]any)
		state := anyString(health["state"])
		if state == "" {
			state = "no_plan"
		}
		byState[state]++
		order, known := rank[state]
		if !known {
			order = len(briefInitiativeOrder)
		}
		ref := anyString(initiative["ref"])
		item := map[string]any{
			"ref":      ref,
			"title":    anyString(initiative["title"]),
			"href":     briefTaskHref(ref),
			"state":    state,
			"reason":   anyString(health["reason"]),
			"progress": initiative["progress"],
			"phase":    anyString(initiative["phase"]),
		}
		if step, ok := initiative["next_step"].(*plans.NextStep); ok && step != nil {
			item["next_step"] = step.Title
		}
		rows = append(rows, entry{item: item, order: order, ref: ref})
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].order != rows[j].order {
			return rows[i].order < rows[j].order
		}
		return rows[i].ref < rows[j].ref
	})
	items := []map[string]any{}
	for _, row := range rows {
		if len(items) == briefSectionLimit {
			break
		}
		items = append(items, row.item)
	}
	return map[string]any{"status": "ok", "count": len(rows), "items": items, "more": len(rows) - len(items), "by_state": byState, "truncated": in.workTruncated, "href": "/tasks"}
}

func buildOverviewBrief(in briefInputs) map[string]any {
	return map[string]any{
		"status":           "ok",
		"generated_at":     in.now.Format(time.RFC3339Nano),
		"decisions":        briefDecisions(in),
		"since_last_look":  briefSinceLastLook(in),
		"at_risk":          briefAtRisk(in),
		"machine":          briefMachine(in),
		"initiatives":      briefInitiatives(in),
		"section_limit":    briefSectionLimit,
		"throughput_hours": int(briefThroughputWindow.Hours()),
	}
}
