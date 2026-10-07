package server

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"agent-nexus-core/internal/commandcenter"
	"agent-nexus-core/internal/plans"
	"agent-nexus-core/internal/primitives"
)

var briefNow = time.Date(2026, 10, 8, 8, 12, 0, 0, time.UTC)

func briefAt(d time.Duration) string {
	return briefNow.Add(-d).Format(time.RFC3339Nano)
}

func briefStr(value string) *string { return &value }

// briefCard builds the shape overviewWork hands the handler, not the public
// projection: the brief reads the internal rows.
func briefCard(ref, title, phase string, updated time.Duration, extra map[string]any) map[string]any {
	card := map[string]any{"id": ref, "ref": ref, "title": title, "phase": phase, "updated_at": briefAt(updated)}
	for k, v := range extra {
		card[k] = v
	}
	return card
}

func briefDependsOn(ref string) []any {
	return []any{map[string]any{"kind": "depends_on", "ref": ref}}
}

func briefNeedsRow(id, title string) map[string]any {
	return map[string]any{"id": id, "title": title, "href": "/tasks/x", "source": "Waiting on you"}
}

func briefSection(t *testing.T, brief map[string]any, key string) map[string]any {
	t.Helper()
	section, ok := brief[key].(map[string]any)
	if !ok {
		t.Fatalf("brief has no %s section: %v", key, brief)
	}
	return section
}

func briefItems(t *testing.T, section map[string]any) []map[string]any {
	t.Helper()
	items, ok := section["items"].([]map[string]any)
	if !ok {
		t.Fatalf("section has no items: %v", section)
	}
	return items
}

// Blocking open work is the only signal that says other work cannot proceed,
// so it must outrank a louder-looking but harmless row.
func TestBriefRanksBlockingWorkAboveAgeAndPriority(t *testing.T) {
	gate := briefCard("card:gate", "Approve the pricing change", "in_progress", 2*time.Hour, map[string]any{"priority": "low"})
	ancient := briefCard("card:ancient", "Confirm the logo", "in_progress", 40*24*time.Hour, map[string]any{"priority": "high"})
	work := []map[string]any{
		gate,
		ancient,
		briefCard("card:a", "Build the page", "in_progress", time.Hour, map[string]any{"relations": briefDependsOn("card:gate")}),
		briefCard("card:b", "Wire the API", "in_progress", time.Hour, map[string]any{"relations": briefDependsOn("card:gate")}),
		// A finished dependent is not waiting on anything.
		briefCard("card:c", "Old migration", "done", time.Hour, map[string]any{"relations": briefDependsOn("card:gate")}),
	}
	in := briefInputs{now: briefNow, workAvailable: true, work: work, needsOK: true, dependents: briefDependents(work), signals: map[string]briefSignal{}}
	in.recordWorkSignals(work)
	in.needsRows = []map[string]any{
		briefNeedsRow("task:card:ancient", "Confirm the logo"),
		briefNeedsRow("task:card:gate", "Approve the pricing change"),
	}

	section := briefSection(t, buildOverviewBrief(in), "decisions")
	items := briefItems(t, section)
	if len(items) != 2 || items[0]["id"] != "task:card:gate" {
		t.Fatalf("blocking row did not rank first: %v", items)
	}
	reason := items[0]["reason"].(string)
	if !strings.HasPrefix(reason, "blocks 2 cards") {
		t.Fatalf("reason does not name what it blocks: %q", reason)
	}
	if signals := items[0]["signals"].(map[string]any); signals["blocks"] != 2 {
		t.Fatalf("done dependent counted as blocked work: %v", signals)
	}
	if !strings.Contains(items[1]["reason"].(string), "high priority") {
		t.Fatalf("priority missing from reason: %v", items[1])
	}
}

func TestBriefDecisionReasonNamesOverdueAndAge(t *testing.T) {
	work := []map[string]any{
		briefCard("card:late", "Sign the renewal", "blocked", 72*time.Hour, map[string]any{"due_at": briefAt(48 * time.Hour), "priority": "critical"}),
	}
	in := briefInputs{now: briefNow, workAvailable: true, work: work, needsOK: true, dependents: briefDependents(work), signals: map[string]briefSignal{}}
	in.recordWorkSignals(work)
	in.needsRows = []map[string]any{briefNeedsRow("task:card:late", "Sign the renewal")}
	in.signals["ask:fresh"] = briefSignal{Kind: "ask", At: briefAt(30 * time.Minute)}
	in.needsRows = append(in.needsRows, briefNeedsRow("ask:fresh", "Quick question"))

	items := briefItems(t, briefSection(t, buildOverviewBrief(in), "decisions"))
	if items[0]["id"] != "task:card:late" {
		t.Fatalf("overdue critical row did not rank first: %v", items)
	}
	reason := items[0]["reason"].(string)
	for _, want := range []string{"overdue 2d", "critical priority"} {
		if !strings.Contains(reason, want) {
			t.Fatalf("reason %q is missing %q", reason, want)
		}
	}
	// Three clauses is the line's budget; a fourth would be skimmed past.
	if strings.Count(reason, " · ") > 2 {
		t.Fatalf("reason carries more than three clauses: %q", reason)
	}
}

/*
The ranking must not reorder itself between two reads of the same data, or a
reader cannot trust "the top one is the top one".
*/
func TestBriefRankingIsStableForEquivalentRows(t *testing.T) {
	work := []map[string]any{
		briefCard("card:one", "One", "in_progress", 5*time.Hour, nil),
		briefCard("card:two", "Two", "in_progress", 5*time.Hour, nil),
		briefCard("card:three", "Three", "in_progress", 5*time.Hour, nil),
	}
	in := briefInputs{now: briefNow, workAvailable: true, work: work, needsOK: true, dependents: briefDependents(work), signals: map[string]briefSignal{}}
	in.recordWorkSignals(work)
	in.needsRows = []map[string]any{
		briefNeedsRow("task:card:two", "Two"),
		briefNeedsRow("task:card:three", "Three"),
		briefNeedsRow("task:card:one", "One"),
	}
	first := briefItems(t, briefSection(t, buildOverviewBrief(in), "decisions"))
	second := briefItems(t, briefSection(t, buildOverviewBrief(in), "decisions"))
	for i := range first {
		if first[i]["id"] != second[i]["id"] {
			t.Fatalf("order wobbled: %v vs %v", first, second)
		}
	}
	if first[0]["id"] != "task:card:one" {
		t.Fatalf("equal rows did not break ties by id: %v", first)
	}
}

/*
A reader who cannot see a dependent must get a smaller number, not a hint
that something was withheld. The brief reads only the rows it is given, so
denying access removes a dependent from the count and nothing else: no
"1 hidden card" anywhere in the payload.
*/
func TestBriefCountsNeverLeakRowsTheReaderCannotSee(t *testing.T) {
	gate := briefCard("card:gate", "Approve", "in_progress", 3*time.Hour, nil)
	open := briefCard("card:open", "Open dependent", "in_progress", time.Hour, map[string]any{"relations": briefDependsOn("card:gate")})
	secret := briefCard("card:secret", "Private dependent", "blocked", time.Hour, map[string]any{"relations": briefDependsOn("card:gate")})

	build := func(work []map[string]any) map[string]any {
		in := briefInputs{now: briefNow, workAvailable: true, work: work, needsOK: true, dependents: briefDependents(work), signals: map[string]briefSignal{}}
		in.recordWorkSignals(work)
		in.needsRows = []map[string]any{briefNeedsRow("task:card:gate", "Approve")}
		return buildOverviewBrief(in)
	}
	full := build([]map[string]any{gate, open, secret})
	scoped := build([]map[string]any{gate, open})

	blocksIn := func(brief map[string]any) any {
		items := briefItems(t, briefSection(t, brief, "decisions"))
		return items[0]["signals"].(map[string]any)["blocks"]
	}
	if blocksIn(full) != 2 || blocksIn(scoped) != 1 {
		t.Fatalf("blocks count is not scoped to visible rows: full=%v scoped=%v", blocksIn(full), blocksIn(scoped))
	}
	// The denied card contributes nothing anywhere: no title, no ref, no count.
	for _, needle := range []string{"card:secret", "Private dependent", "hidden"} {
		if strings.Contains(briefJSON(t, scoped), needle) {
			t.Fatalf("scoped brief leaked %q", needle)
		}
	}
	risk := briefSection(t, scoped, "at_risk")
	if risk["count"] != 0 {
		t.Fatalf("a denied blocked card was counted as risk: %v", risk)
	}
}

func TestBriefSinceLastLookGroupsAndSeparatesFirstVisit(t *testing.T) {
	since := briefAt(12 * time.Hour)
	work := []map[string]any{
		briefCard("card:done-1", "Shipped the proxy", "done", 2*time.Hour, nil),
		briefCard("card:done-2", "Shipped the badge", "done", 3*time.Hour, nil),
		briefCard("card:blocked", "Lost the credential", "blocked", time.Hour, nil),
		briefCard("card:moved", "Drafting", "in_progress", 4*time.Hour, nil),
		// Older than the baseline: not news.
		briefCard("card:old", "Last week", "done", 36*time.Hour, nil),
	}
	in := briefInputs{
		now:     briefNow,
		since:   &since,
		work:    work,
		needsOK: true,
		signals: map[string]briefSignal{},
		changes: []primitives.OverviewChange{
			{Kind: "step_completed", Ref: "card:init", Title: "Draft the plan", StepID: "a"},
			{Kind: "ask_answered", Ref: "event:1", Title: "Ask answered", TS: briefAt(time.Hour)},
			{Kind: "initiative_stalled", Ref: "card:init", Title: "Initiative"},
		},
		newAsks: []map[string]any{
			{"id": "ask-1", "title": "Approve the budget", "created_at": briefAt(90 * time.Minute)},
			{"id": "ask-old", "title": "From yesterday", "created_at": briefAt(30 * time.Hour)},
		},
	}
	section := briefSection(t, buildOverviewBrief(in), "since_last_look")
	if section["total"] != 8 {
		t.Fatalf("unexpected digest total: %v", section)
	}
	counts := map[string]int{}
	order := []string{}
	for _, group := range section["groups"].([]map[string]any) {
		counts[group["key"].(string)] = group["count"].(int)
		order = append(order, group["key"].(string))
	}
	for key, want := range map[string]int{"completed": 2, "newly_blocked": 1, "new_asks": 1, "answered": 1, "steps": 1, "initiatives": 1, "updated": 1} {
		if counts[key] != want {
			t.Fatalf("group %s=%d want %d (%v)", key, counts[key], want, counts)
		}
	}
	if order[0] != "completed" || order[1] != "newly_blocked" {
		t.Fatalf("groups are not in reading order: %v", order)
	}
	if section["first_visit"] != nil {
		t.Fatalf("a visit with a baseline is not a first visit: %v", section)
	}

	in.since = nil
	first := briefSection(t, buildOverviewBrief(in), "since_last_look")
	if first["first_visit"] != true || first["total"] != 0 {
		t.Fatalf("no baseline must read as a first visit, not as no change: %v", first)
	}
}

func TestBriefMachineCountsStuckOnlyWhenWorkRidesOnTheSilence(t *testing.T) {
	roster := []commandcenter.Summary{
		{ID: "a", Handle: "builder", DisplayName: "Builder", State: "working"},
		{ID: "b", Handle: "waiter", DisplayName: "Waiter", State: "waiting_on_human"},
		// Silent while holding a card: the only alarming case.
		{ID: "c", Handle: "holder", DisplayName: "Holder", CurrentCardRef: briefStr("card:held"), CurrentCardTitle: briefStr("Migrate the index"), LastSignalAt: briefStr(briefAt(5 * time.Hour))},
		// Silent, holding nothing: normal.
		{ID: "d", Handle: "asleep", DisplayName: "Asleep", LastSignalAt: briefStr(briefAt(40 * 24 * time.Hour))},
		// Never checked in.
		{ID: "e", Handle: "never", DisplayName: "Never"},
		// Revoked identities are not part of the fleet.
		{ID: "f", Handle: "gone", DisplayName: "Gone", State: "working", RevokedAt: briefStr(briefAt(time.Hour))},
	}
	work := []map[string]any{
		briefCard("card:1", "One", "done", 2*time.Hour, map[string]any{"owner": "actor:builder"}),
		briefCard("card:2", "Two", "done", 6*time.Hour, map[string]any{"owner": "actor:builder"}),
		briefCard("card:3", "Three", "cancelled", 20*time.Hour, map[string]any{"owner": "actor:holder"}),
		briefCard("card:4", "Four", "done", 30*time.Hour, map[string]any{"owner": "actor:builder"}),
		briefCard("card:5", "Five", "in_progress", time.Hour, map[string]any{"owner": "actor:builder"}),
	}
	in := briefInputs{now: briefNow, workAvailable: true, work: work, needsOK: true, signals: map[string]briefSignal{}, roster: roster, rosterOK: true}
	machine := briefSection(t, buildOverviewBrief(in), "machine")
	if machine["working"] != 1 || machine["waiting"] != 1 || machine["stuck"] != 1 {
		t.Fatalf("silence split is wrong: %v", machine)
	}
	if machine["finished_24h"] != 3 || machine["agents_finished_24h"] != 2 {
		t.Fatalf("throughput is wrong: %v", machine)
	}
	stuck := machine["stuck_items"].([]map[string]any)
	if len(stuck) != 1 || !strings.Contains(stuck[0]["reason"].(string), "Migrate the index") {
		t.Fatalf("stuck row does not say what is riding on the silence: %v", stuck)
	}
	if stuck[0]["href"] != "/agents/holder" {
		t.Fatalf("stuck row does not link to its agent: %v", stuck)
	}

	// A roster read failure must not take throughput down with it.
	in.rosterOK = false
	degraded := briefSection(t, buildOverviewBrief(in), "machine")
	if degraded["roster_status"] != "unavailable" || degraded["working"] != nil {
		t.Fatalf("roster failure not reported: %v", degraded)
	}
	if degraded["finished_24h"] != 3 {
		t.Fatalf("throughput lost with the roster: %v", degraded)
	}
}

/*
The bug this section exists to fix: seven initiatives with no plan reported
on_track, which rendered green. no_plan must be visible as a gap and must
never be counted as healthy.
*/
func TestBriefInitiativesReportNoPlanHonestly(t *testing.T) {
	planless := map[string]any{"ref": "card:i1", "title": "Hosted onboarding", "phase": "in_progress", "updated_at": briefAt(time.Hour), "progress": map[string]any{"done": 0, "total": 3}, "health": map[string]any{"status": "no_plan", "state": "no_plan", "reason": "Initiative has no plan steps."}}
	blockedSince := briefAt(50 * time.Hour)
	blocked := map[string]any{"ref": "card:i2", "title": "Billing", "phase": "in_progress", "updated_at": briefAt(time.Hour), "progress": map[string]any{"done": 1, "total": 4}, "health": map[string]any{"status": "blocked", "state": "blocked", "reason": "An unfinished step or dependency is blocked."}, "plan_health": plans.Health{State: "blocked", Reason: "An unfinished step or dependency is blocked.", Since: &blockedSince}}
	healthy := map[string]any{"ref": "card:i3", "title": "Docs", "phase": "in_progress", "updated_at": briefAt(time.Hour), "progress": map[string]any{"done": 2, "total": 2}, "health": map[string]any{"status": "on_track", "state": "on_track", "reason": "Open steps are progressing."}}

	in := briefInputs{now: briefNow, workAvailable: true, needsOK: true, signals: map[string]briefSignal{}, initiatives: []map[string]any{planless, healthy, blocked}}
	brief := buildOverviewBrief(in)
	section := briefSection(t, brief, "initiatives")
	byState := section["by_state"].(map[string]int)
	if byState["no_plan"] != 1 || byState["blocked"] != 1 || byState["on_track"] != 1 {
		t.Fatalf("state counts hide the planless initiative: %v", byState)
	}
	items := briefItems(t, section)
	if items[0]["state"] != "blocked" || items[0]["ref"] != "card:i2" {
		t.Fatalf("initiatives are not worst first: %v", items)
	}
	for _, item := range items {
		if item["ref"] == "card:i1" && item["state"] != "no_plan" {
			t.Fatalf("planless initiative reported as %v", item["state"])
		}
		if item["reason"] == "" {
			t.Fatalf("initiative row has no computed reason: %v", item)
		}
	}

	// At risk means off track with a reason. A planless initiative is a gap to
	// fill, and a healthy one is not a risk; neither belongs here.
	risk := briefSection(t, brief, "at_risk")
	riskItems := briefItems(t, risk)
	if risk["count"] != 1 || riskItems[0]["ref"] != "card:i2" {
		t.Fatalf("at_risk is not computed from off-track rows only: %v", risk)
	}
	if riskItems[0]["since"] != blockedSince {
		t.Fatalf("risk row lost its computed anchor: %v", riskItems[0])
	}
	if !strings.Contains(riskItems[0]["reason"].(string), "blocked") {
		t.Fatalf("risk row has no computed reason: %v", riskItems[0])
	}
}

func TestBriefAtRiskExplainsBlockedWorkWithRecordedBlockers(t *testing.T) {
	work := []map[string]any{
		briefCard("card:stuck", "Restore the replica", "blocked", 8*time.Hour, map[string]any{"blockers": []any{"waiting on vendor access", "needs a maintenance window"}}),
		briefCard("card:quiet", "Blocked, unexplained", "blocked", 2*time.Hour, nil),
		briefCard("card:fine", "Moving", "in_progress", time.Hour, nil),
	}
	in := briefInputs{now: briefNow, workAvailable: true, work: work, needsOK: true, dependents: briefDependents(work), signals: map[string]briefSignal{}}
	items := briefItems(t, briefSection(t, buildOverviewBrief(in), "at_risk"))
	if len(items) != 2 {
		t.Fatalf("expected both blocked cards: %v", items)
	}
	if items[0]["reason"] != "Blocked on waiting on vendor access (+1 more)" {
		t.Fatalf("recorded blockers not used as the reason: %v", items[0])
	}
	if items[1]["reason"] != "Task is in the blocked column." {
		t.Fatalf("unexplained block lost its computed reason: %v", items[1])
	}
}

func TestBriefSectionsShowTopRowsAndCountTheRest(t *testing.T) {
	work := []map[string]any{}
	rows := []map[string]any{}
	for i := 0; i < 9; i++ {
		ref := "card:r" + string(rune('a'+i))
		work = append(work, briefCard(ref, "Row", "blocked", time.Duration(i+1)*time.Hour, nil))
		rows = append(rows, briefNeedsRow("task:"+ref, "Row"))
	}
	in := briefInputs{now: briefNow, workAvailable: true, work: work, needsOK: true, dependents: briefDependents(work), signals: map[string]briefSignal{}, needsRows: rows}
	in.recordWorkSignals(work)
	brief := buildOverviewBrief(in)
	for _, key := range []string{"decisions", "at_risk"} {
		section := briefSection(t, brief, key)
		if len(briefItems(t, section)) != briefSectionLimit {
			t.Fatalf("%s did not cap at the section limit: %v", key, section)
		}
		if section["count"] != 9 || section["more"] != 9-briefSectionLimit {
			t.Fatalf("%s lost the remainder: %v", key, section)
		}
	}
}

func TestBriefReportsUnavailableNeedsWithoutInventingARanking(t *testing.T) {
	in := briefInputs{now: briefNow, workAvailable: true, signals: map[string]briefSignal{}, needsOK: false, needsRows: []map[string]any{briefNeedsRow("task:card:x", "Hidden")}}
	section := briefSection(t, buildOverviewBrief(in), "decisions")
	if section["status"] != "unavailable" || section["count"] != 0 {
		t.Fatalf("a failed needs read must not produce a ranking: %v", section)
	}
}

func briefJSON(t *testing.T, brief map[string]any) string {
	t.Helper()
	raw, err := json.Marshal(brief)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

/*
A store that answers Overview without the visit work snapshot can still rank
what is waiting, but it must not report "nothing is at risk" or "nothing
finished today" from rows it never counted.
*/
func TestBriefWithoutWorkSnapshotSaysSoRatherThanReportingZero(t *testing.T) {
	in := briefInputs{now: briefNow, workAvailable: false, needsOK: true, signals: map[string]briefSignal{}, dependents: map[string]int{}}
	in.needsRows = []map[string]any{briefNeedsRow("inbox:ask-1", "Approve the budget")}
	in.signals["inbox:ask-1"] = briefSignal{Kind: "ask", At: briefAt(3 * time.Hour)}
	brief := buildOverviewBrief(in)
	if len(briefItems(t, briefSection(t, brief, "decisions"))) != 1 {
		t.Fatal("ranking needs only the needs_you rows")
	}
	risk := briefSection(t, brief, "at_risk")
	if risk["status"] != "unavailable" {
		t.Fatalf("risk reported as computed without the rows to compute it: %v", risk)
	}
	machine := briefSection(t, brief, "machine")
	if machine["finished_24h"] != nil || machine["throughput_status"] != "unavailable" {
		t.Fatalf("throughput reported from rows nobody counted: %v", machine)
	}
}
