package primitives

import (
	"context"
	"database/sql"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"agent-nexus-core/internal/resourceaccess"
)

// Watch trends: whether the workspace is getting better or worse, computed at
// read time from records core already keeps. HTTP readers must first prove full
// visibility in the calculation's read snapshot. Every family then uses one
// capped indexed position seek and hydrates canonical rows. A family reports
// where its history stops: older buckets are null rather than a guessed zero.
const (
	trendsAskCap      = 80
	trendsAnswerCap   = 60
	trendsOpenAskCap  = 70
	trendsOpenPromCap = 55
	trendsPromiseCap  = 35
	trendsCreatedCap  = 40
	trendsDoneCap     = 40
	trendsRunCap      = 170
	trendsLoopCap     = 25
	trendsSourceRows  = 5
)

// WatchTrendRanges are the supported windows in days.
var WatchTrendRanges = []int{7, 30, 90}

// WatchTrendGroups split the read so each request stays within the ordinary
// read budget: decisions (asks), initiatives (promises and work) and loops.
var WatchTrendGroups = []string{"decisions", "initiatives", "loops"}

type trendWindow struct {
	end     time.Time
	step    time.Duration
	n       int // buckets per period; the read covers 2n (previous + current)
	periodN int
}

func newTrendWindow(days int, now time.Time) trendWindow {
	w := trendWindow{end: now.UTC(), step: 24 * time.Hour, n: days}
	if days > 30 {
		w.step, w.n = 7*24*time.Hour, (days+6)/7
	}
	return w
}

func (w trendWindow) start() time.Time { return w.end.Add(-time.Duration(2*w.n) * w.step) }
func (w trendWindow) bucketStart(i int) time.Time {
	return w.end.Add(-time.Duration(2*w.n-i) * w.step)
}
func (w trendWindow) bucketEnd(i int) time.Time { return w.bucketStart(i + 1) }

// bucket returns the bucket index for t across both periods, or -1.
func (w trendWindow) bucket(t time.Time) int {
	if t.IsZero() || t.Before(w.start()) || !t.Before(w.end) {
		if t.Equal(w.end) {
			return 2*w.n - 1
		}
		return -1
	}
	return int(t.Sub(w.start()) / w.step)
}

// complete reports whether bucket i lies wholly after a capped family's
// horizon. A zero horizon means the family was not truncated.
func (w trendWindow) complete(i int, horizon time.Time) bool {
	return horizon.IsZero() || !w.bucketStart(i).Before(horizon)
}

func trendTime(raw string) time.Time {
	t, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(raw))
	if err != nil {
		return time.Time{}
	}
	return t.UTC()
}

type trendAsk struct {
	id, requester, state string
	created, answered    time.Time
}

type trendAnswer struct {
	created, answered time.Time
}

type trendPromise struct {
	open             bool
	promise, created time.Time
	done             time.Time
	cancelled        bool
}

type trendRun struct {
	at       time.Time
	subject  string
	actor    string
	infraErr bool
}

type trendLoop struct {
	refs    []string
	created time.Time
	every   time.Duration
}

type trendsBuild struct {
	*briefBuild
	w trendWindow
}

// WatchTrends returns one group of watch_trends.v1 over days (7, 30 or 90).
// Scoped callers must first prove full visibility in the calculation's read
// snapshot; any denied resource rejects the whole read.
func (s *Store) WatchTrends(ctx context.Context, reader BriefReader, group string, days int, now time.Time) (map[string]any, error) {
	// A separate callback could hide contributors beyond the canonical scope.
	if reader.Visible != nil {
		return nil, ErrForbidden
	}
	if scope, scoped := accessScopeFrom(ctx); scoped {
		// Probe canonical roots, not descendants: without a denied root there
		// cannot be a denied descendant. The root-owner primary key bounds all
		// three EXISTS seeks independently of the number of hidden resources.
		// Pin both proof and calculation to the same SQLite read snapshot so a
		// concurrent transfer cannot invalidate admission midway through a read.
		// Admission reads only the canonical root counters. Skip ordinary page
		// policy preparation (and its optional caches) before opening the snapshot.
		read, finish, err := s.db.PinReads(resourceaccess.WithoutPolicy(ctx), func(c context.Context, db resourceaccess.QueryRower) (context.Context, error) {
			var active bool
			var ledger int
			if err := db.QueryRowContext(c, `WITH `+admissionLedgerCTE(scope)+` SELECT active,ledger FROM _anx_admission_ledger`).Scan(&active, &ledger); err != nil {
				return c, err
			}
			if active || ledger < 1 {
				return c, ErrForbidden
			}
			if hook, ok := c.Value(watchTrendsProofHookKey{}).(func(context.Context)); ok {
				hook(c)
			}
			// This snapshot has no inaccessible resources. Only this admitted
			// calculation may use unscoped relations; no capability escapes it.
			return resourceaccess.WithoutPolicy(context.WithValue(c, accessScopeKey{}, struct{}{})), nil
		})
		if err != nil {
			return nil, err
		}
		defer finish()
		return s.buildWatchTrends(read, group, days, now)
	}
	return s.buildWatchTrends(ctx, group, days, now)
}

// watchTrendsProofHookKey addresses a test-only hook that runs inside the
// pinned read snapshot after the full-visibility proof; only in-package
// tests set it.
type watchTrendsProofHookKey struct{}

// buildWatchTrends is the admitted unscoped calculation, not a read API: it may
// examine every record, so only WatchTrends after full-visibility admission
// (or an equivalently authorized internal caller) may reach it.
func (s *Store) buildWatchTrends(ctx context.Context, group string, days int, now time.Time) (map[string]any, error) {
	b := &trendsBuild{briefBuild: &briefBuild{s: s, ctx: ctx, now: now.UTC(), actors: map[string]bool{}}, w: newTrendWindow(days, now)}
	var panels []map[string]any
	var err error
	switch group {
	case "decisions":
		panels, err = b.decisionPanels()
	case "initiatives":
		panels, err = b.initiativePanels()
	case "loops":
		panels, err = b.loopPanels()
	default:
		return nil, workInvalid("group must be decisions, initiatives or loops")
	}
	if err != nil {
		return nil, err
	}
	buckets := make([]string, b.w.n)
	for i := range buckets {
		buckets[i] = b.w.bucketStart(b.w.n + i).Format(time.RFC3339)
	}
	bucket := "day"
	if b.w.step > 24*time.Hour {
		bucket = "week"
	}
	return map[string]any{
		"schema":       "watch_trends.v1",
		"group":        group,
		"generated_at": b.now.Format(time.RFC3339),
		"range_days":   days,
		"bucket":       bucket,
		"buckets":      buckets,
		"panels":       panels,
	}, nil
}

func (b *trendsBuild) decisionPanels() ([]map[string]any, error) {
	asks, horizon, err := b.trendAsks()
	if err != nil {
		return nil, err
	}
	answers, answersHorizon, err := b.trendAnswers()
	if err != nil {
		return nil, err
	}
	open, openTruncated, err := b.trendOpenAsks()
	if err != nil {
		return nil, err
	}
	sources, err := b.askSourcesPanel(asks, horizon)
	if err != nil {
		return nil, err
	}
	return []map[string]any{
		b.askFlowPanel(asks, horizon, answers, answersHorizon),
		b.answerTimePanel(answers, answersHorizon),
		b.backlogPanel(asks, horizon, answers, answersHorizon, open, openTruncated),
		sources,
	}, nil
}

func (b *trendsBuild) initiativePanels() ([]map[string]any, error) {
	promises, promiseHorizon, openCutoff, err := b.trendPromises()
	if err != nil {
		return nil, err
	}
	created, createdHorizon, err := b.trendCreated()
	if err != nil {
		return nil, err
	}
	done, doneHorizon, err := b.trendDone()
	if err != nil {
		return nil, err
	}
	return []map[string]any{
		b.promisesPanel(promises, promiseHorizon, openCutoff),
		b.pastPromisePanel(promises, promiseHorizon, !openCutoff.IsZero()),
		b.workFlowPanel(created, createdHorizon, done, doneHorizon),
	}, nil
}

func (b *trendsBuild) loopPanels() ([]map[string]any, error) {
	runs, horizon, err := b.trendRuns()
	if err != nil {
		return nil, err
	}
	loops, loopsTruncated, err := b.trendLoops()
	if err != nil {
		return nil, err
	}
	panels := []map[string]any{b.loopRunsPanel(runs, horizon)}
	if len(loops) > 0 || loopsTruncated {
		panels = append(panels, b.loopHealthPanel(loops, runs, horizon, loopsTruncated))
	}
	active, err := b.agentRunsPanel(runs, horizon)
	if err != nil {
		return nil, err
	}
	return append(panels, active), nil
}

// Every capped family is read through watch_trend_positions as one indexed
// seek: newest first (soonest for open promises), bounded to the cap before
// hydration joins canonical rows. Position rows carry identity and time only;
// lifecycle, board role and thread eligibility are flags on each admitted row,
// so joins cannot walk past missing or ineligible records to fill the cap.

// admitTrendRows bounds physical positions before joins and lifecycle
// eligibility, then runs query over the capped page. Old unanswerable asks
// (for example after their thread is removed) must not make hydration walk an
// arbitrary tail before filling the cap. params is kind, an optional lower
// time bound, and the page cap (limit+1); all callers match that shape. Every
// row's first two columns are at_key and id; all columns scan as text.
// truncated reports that a further position exists beyond the cap.
func (b *trendsBuild) admitTrendRows(ctx context.Context, query string, limit int, ascending bool, params ...any) ([][]string, bool, error) {
	if len(params) != 2 && len(params) != 3 {
		return nil, false, workInvalid("invalid trend seek")
	}
	page := `SELECT * FROM watch_trend_positions WHERE kind=?`
	if len(params) == 3 {
		page += ` AND at_key>=anx_timestamp_key(?)`
	}
	direction := "DESC"
	if ascending {
		direction = "ASC"
	}
	page += ` ORDER BY at_key ` + direction + `,id ` + direction + ` LIMIT ?`
	var count int
	if err := b.s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM (`+page+`)`, params...).Scan(&count); err != nil {
		return nil, false, err
	}
	// Name the bounded temporary page explicitly in query plans; only the
	// capped index seek above reads the business position table.
	query = trendPositionAlias.ReplaceAllString(query, "bounded_trend_page.")
	query = strings.Replace(query, "FROM watch_trend_positions p ", "FROM _watch_page bounded_trend_page ", 1)
	query = `WITH _watch_page AS MATERIALIZED (` + page + `) ` + query
	params = append(append([]any{}, params...), params...)
	rows, err := b.s.db.QueryContext(ctx, query, params...)
	if err != nil {
		return nil, false, err
	}
	cols, err := rows.Columns()
	if err != nil {
		rows.Close()
		return nil, false, err
	}
	merged := [][]string{}
	for rows.Next() {
		vals := make([]sql.NullString, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err = rows.Scan(ptrs...); err != nil {
			rows.Close()
			return nil, false, err
		}
		row := make([]string, len(cols))
		for i, v := range vals {
			row[i] = v.String
		}
		merged = append(merged, row)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, false, err
	}
	if len(merged) > limit {
		return merged[:limit], true, nil
	}
	// A full page stays truncated even when hydration drops rows: the records
	// left out could change any bucket either way.
	return merged, count > limit, nil
}

var trendPositionAlias = regexp.MustCompile(`\bp\.`)

// trendHorizon is where a truncated window's history stops: the time of its
// last record. Everything before it is unknown.
func trendHorizon(truncated bool, last time.Time, end time.Time) time.Time {
	if !truncated {
		return time.Time{}
	}
	if last.IsZero() {
		return end
	}
	return last
}

// The position seek shared by every family: a time bound, newest first.
const trendPositionsDesc = ` WHERE p.kind=? AND p.at_key>=anx_timestamp_key(?) AND `

// trendAsks admits asks raised since the previous period began.
func (b *trendsBuild) trendAsks() ([]trendAsk, time.Time, error) {
	rows, truncated, err := b.admitTrendRows(b.ctx, `SELECT p.at_key,p.id,e.ts,a.requester,CASE WHEN response.id IS NULL THEN 'open' ELSE a.state END,CASE WHEN response.id IS NOT NULL THEN COALESCE(a.answered_at,'') ELSE '' END
	 FROM watch_trend_positions p CROSS JOIN events e ON e.id=p.id CROSS JOIN brief_asks a ON a.ask_id=p.id `+briefAnswerableAskJoin+` LEFT JOIN events response ON response.id=a.response_event_id AND COALESCE(response.trashed_at,'')=''`+
		trendPositionsDesc+`COALESCE(e.trashed_at,'')='' ORDER BY p.at_key DESC,p.id DESC LIMIT ?`, trendsAskCap, false,
		"ask", loopTS(b.w.start()), trendsAskCap+1)
	if err != nil {
		return nil, time.Time{}, err
	}
	out := []trendAsk{}
	var last time.Time
	for _, r := range rows {
		a := trendAsk{id: r[1], created: trendTime(r[2]), requester: stripActor(r[3]), state: r[4], answered: trendTime(r[5])}
		last = a.created
		out = append(out, a)
	}
	return out, trendHorizon(truncated, last, b.w.end), nil
}

// trendAnswers admits answers given since the previous period began, by the
// answer event; the resolution and the ask join by unique key. Measuring
// answers by when they happened keeps both periods comparable: an answer late
// in the window to an old ask counts, in its own period.
func (b *trendsBuild) trendAnswers() ([]trendAnswer, time.Time, error) {
	rows, truncated, err := b.admitTrendRows(b.ctx, `SELECT p.at_key,p.id,e.ts,COALESCE(r.resolution_type,''),COALESCE(a.created_at,''),COALESCE(r.created_at,'')
	 FROM watch_trend_positions p CROSS JOIN events e ON e.id=p.id `+briefAnswerableAskJoin+`
	 LEFT JOIN human_attention_request_resolutions r ON r.resolution_event_id=e.id LEFT JOIN brief_asks a ON a.ask_id=r.request_event_id`+
		trendPositionsDesc+`COALESCE(e.trashed_at,'')='' ORDER BY p.at_key DESC,p.id DESC LIMIT ?`, trendsAnswerCap, false,
		"answer", loopTS(b.w.start()), trendsAnswerCap+1)
	if err != nil {
		return nil, time.Time{}, err
	}
	out := []trendAnswer{}
	var last time.Time
	for _, r := range rows {
		last = trendTime(r[2])
		// A repeated response to an answered ask has no resolution of its own.
		if r[3] != "answered" {
			continue
		}
		out = append(out, trendAnswer{created: trendTime(r[4]), answered: trendTime(r[5])})
	}
	return out, trendHorizon(truncated, last, b.w.end), nil
}

// trendOpenAsks admits a bounded history of visible asks, then derives open
// state using only visible responses. If history is capped, backlog is unknown.
// Filtering resolved asks before LIMIT would permit unbounded history walks.
func (b *trendsBuild) trendOpenAsks() ([]time.Time, bool, error) {
	rows, truncated, err := b.admitTrendRows(b.ctx, `SELECT p.at_key,p.id,a.created_at,CASE WHEN response.id IS NULL THEN 'open' ELSE a.state END
	 FROM watch_trend_positions p CROSS JOIN brief_asks a ON a.ask_id=p.id CROSS JOIN events e ON e.id=p.id `+briefAnswerableAskJoin+`
	 LEFT JOIN events response ON response.id=a.response_event_id AND COALESCE(response.trashed_at,'')=''
	 WHERE p.kind=? AND COALESCE(e.trashed_at,'')='' ORDER BY p.at_key DESC,p.id DESC LIMIT ?`, trendsOpenAskCap, false,
		"open_ask", trendsOpenAskCap+1)
	if err != nil {
		return nil, false, err
	}
	out := []time.Time{}
	for _, r := range rows {
		if r[3] == "open" {
			out = append(out, trendTime(r[2]))
		}
	}
	return out, truncated, nil
}

// trendDoneAt is the per-card completion seek: the card's newest Done
// lifecycle event, bounded by the partial completion position index.
const trendDoneAt = `CASE WHEN c.column_key='done' THEN (SELECT e.ts FROM watch_trend_positions position CROSS JOIN events e ON e.id=position.id WHERE position.kind='done_event' AND position.card_id=+c.id AND COALESCE(e.trashed_at,'')='' ORDER BY position.at_key DESC,position.id DESC LIMIT 1) END`

// trendPromises admits initiatives with a promise: open ones by soonest
// promise (the overdue first), and closed ones whose promise fell in the
// window, newest first. Board role, top-level and lifecycle are flags on each
// admitted row: they describe visible records, so they may use window places.
func (b *trendsBuild) trendPromises() ([]trendPromise, time.Time, time.Time, error) {
	out := []trendPromise{}
	var horizon, openCutoff time.Time
	// Conservatively omit any linked child, including one whose parents have
	// closed. Walking arbitrarily many closed parents to prove absence would
	// break the read budget. Each incoming-ref probe stops at one row.
	// Trends may span thousands of designated boards; this bounded point
	// cohort checks the containing board by primary key rather than building
	// the workspace role set used by batched hierarchy statements.
	declaredRoot := `(c.initiative_root=1 AND EXISTS(SELECT 1 FROM boards root_board WHERE root_board.id=c.board_id AND root_board.role='initiatives' AND root_board.archived_at IS NULL AND root_board.trashed_at IS NULL))`
	hasParent := `(NOT ` + declaredRoot + ` AND (EXISTS(SELECT 1 FROM brief_plan_refs link WHERE link.ref='card:'||c.id LIMIT 1) OR EXISTS(SELECT 1 FROM brief_plan_refs link WHERE link.ref='card:'||c.handle LIMIT 1)))`
	for _, open := range []bool{true, false} {
		limit, kind, bound, order := trendsPromiseCap, "promise_closed", ` AND p.at_key>=anx_timestamp_key(?)`, `DESC`
		if open {
			limit, kind, bound, order = trendsOpenPromCap, "promise_open", ``, `ASC`
		}
		args := []any{kind}
		if !open {
			args = append(args, loopTS(b.w.start()))
		}
		rows, truncated, err := b.admitTrendRows(b.ctx, `SELECT p.at_key,p.id,
		 CASE WHEN COALESCE(c.archived_at,'')='' AND COALESCE(c.trashed_at,'')=''
		  AND (NOT EXISTS(SELECT 1 FROM boards WHERE role='initiatives' LIMIT 1) OR EXISTS(SELECT 1 FROM boards WHERE id=c.board_id AND role='initiatives' LIMIT 1))
		  AND NOT `+hasParent+` THEN 1 ELSE 0 END,
		 bi.open,COALESCE(bi.promise_at,''),c.created_at,c.column_key,COALESCE(`+trendDoneAt+`,'')
		 FROM watch_trend_positions p CROSS JOIN brief_initiatives bi ON bi.card_id=p.id CROSS JOIN cards c ON c.id=p.id
		 WHERE p.kind=?`+bound+` ORDER BY p.at_key `+order+`,p.id `+order+` LIMIT ?`, limit, open, append(args, limit+1)...)
		if err != nil {
			return nil, time.Time{}, time.Time{}, err
		}
		var last time.Time
		for _, r := range rows {
			last = trendTime(r[4])
			if r[2] != "1" {
				continue
			}
			out = append(out, trendPromise{open: r[3] == "1", promise: trendTime(r[4]), created: trendTime(r[5]), cancelled: r[6] == "cancelled", done: trendTime(r[7])})
		}
		// Closed promises are newest first: older history stops at the last
		// one read. Open ones are soonest first: later promises are missing,
		// so buckets after the last one read are unknown.
		if open && truncated {
			openCutoff = last
			if openCutoff.IsZero() {
				openCutoff = b.w.start()
			}
		} else if !open {
			horizon = trendHorizon(truncated, last, b.w.end)
		}
	}
	return out, horizon, openCutoff, nil
}

// trendCardTimes admits one card kind in the window, newest first by the
// position's order (orderExpr), and returns timeExpr for each card. The
// horizon is the last card's order time: every card not read sorts before it.
func (b *trendsBuild) trendCardTimes(kind, timeExpr, orderExpr string, limit int) ([]time.Time, time.Time, error) {
	rows, truncated, err := b.admitTrendRows(b.ctx, `SELECT p.at_key,p.id,`+timeExpr+`,`+orderExpr+`
	 FROM watch_trend_positions p CROSS JOIN cards c ON c.id=p.id`+trendPositionsDesc+`COALESCE(c.archived_at,'')='' AND COALESCE(c.trashed_at,'')=''
	 ORDER BY p.at_key DESC,p.id DESC LIMIT ?`, limit, false,
		kind, loopTS(b.w.start()), limit+1)
	if err != nil {
		return nil, time.Time{}, err
	}
	out := []time.Time{}
	var last time.Time
	for _, r := range rows {
		out = append(out, trendTime(r[2]))
		last = trendTime(r[3])
	}
	return out, trendHorizon(truncated, last, b.w.end), nil
}

// trendCreated admits cards created in the window.
func (b *trendsBuild) trendCreated() ([]time.Time, time.Time, error) {
	return b.trendCardTimes("card", "c.created_at", "c.created_at", trendsCreatedCap)
}

// trendDone admits cards that entered Done in the window. Their last update
// bounds the done time from above, so the window is ordered by it; the
// lifecycle index supplies the exact time.
func (b *trendsBuild) trendDone() ([]time.Time, time.Time, error) {
	return b.trendCardTimes("card_done", `COALESCE(`+trendDoneAt+`,c.updated_at)`, "c.updated_at", trendsDoneCap)
}

// trendRuns admits run receipts in the window, newest first.
func (b *trendsBuild) trendRuns() ([]trendRun, time.Time, error) {
	rows, truncated, err := b.admitTrendRows(b.ctx, `SELECT p.at_key,p.id,e.ts,COALESCE(json_extract(e.payload_json,'$.subject_ref'),''),COALESCE(e.actor_id,''),COALESCE(json_extract(e.payload_json,'$.payload.outcome'),'')
	 FROM watch_trend_positions p CROSS JOIN events e ON e.id=p.id`+trendPositionsDesc+`COALESCE(e.trashed_at,'')=''
	 ORDER BY p.at_key DESC,p.id DESC LIMIT ?`, trendsRunCap, false,
		"run", loopTS(b.w.start()), trendsRunCap+1)
	if err != nil {
		return nil, time.Time{}, err
	}
	out := []trendRun{}
	var last time.Time
	for _, r := range rows {
		run := trendRun{at: trendTime(r[2]), subject: r[3], actor: stripActor(r[4]), infraErr: r[5] == "infra_error"}
		last = run.at
		out = append(out, run)
	}
	return out, trendHorizon(truncated, last, b.w.end), nil
}

// trendLoops admits the most recently annotated loops; lifecycle and cadence
// are flags on each admitted row.
func (b *trendsBuild) trendLoops() ([]trendLoop, bool, error) {
	rows, truncated, err := b.admitTrendRows(b.ctx, `SELECT p.at_key,p.id,
	 CASE WHEN COALESCE(c.archived_at,'')='' AND COALESCE(c.trashed_at,'')='' AND c.column_key NOT IN ('done','cancelled') THEN 1 ELSE 0 END,
	 COALESCE(c.handle,''),c.created_at,CAST(COALESCE(json_extract(m.metadata_json,'$.cadence.every_seconds'),0) AS TEXT)
	 FROM watch_trend_positions p CROSS JOIN cards c ON c.id=p.id LEFT JOIN work_metadata m ON m.card_id=c.id
	 WHERE p.kind=? ORDER BY p.at_key DESC,p.id DESC LIMIT ?`, trendsLoopCap, false,
		"loop", trendsLoopCap+1)
	if err != nil {
		return nil, false, err
	}
	out := []trendLoop{}
	for _, r := range rows {
		every, _ := strconv.ParseFloat(r[5], 64)
		if r[2] != "1" || every <= 0 {
			continue
		}
		l := trendLoop{refs: []string{"card:" + r[1]}, created: trendTime(r[4]), every: time.Duration(every) * time.Second}
		if r[3] != "" {
			l.refs = append(l.refs, "card:"+r[3])
		}
		out = append(out, l)
	}
	return out, truncated, nil
}

// --- panels ---

type trendSeries struct {
	key, label string
	// unit is the series' own unit when it differs from the panel's summary:
	// promises kept is a share, drawn from kept and missed counts.
	unit   string
	points []*float64
}

func (b *trendsBuild) newSeries(key, label string) trendSeries {
	return trendSeries{key: key, label: label, points: make([]*float64, 2*b.w.n)}
}

func num(v float64) *float64 { return &v }

// counts fills a per-bucket count series from times, null before the horizon.
func (b *trendsBuild) counts(key, label string, times []time.Time, horizon time.Time) trendSeries {
	s := b.newSeries(key, label)
	for i := range s.points {
		if b.w.complete(i, horizon) {
			s.points[i] = num(0)
		}
	}
	for _, t := range times {
		if i := b.w.bucket(t); i >= 0 && s.points[i] != nil {
			*s.points[i]++
		}
	}
	return s
}

// periodSum totals a series over one period (0 previous, 1 current); nil when
// any bucket in that period is unknown.
func (b *trendsBuild) periodSum(s trendSeries, period int) *float64 {
	total := 0.0
	for i := period * b.w.n; i < (period+1)*b.w.n; i++ {
		if s.points[i] == nil {
			return nil
		}
		total += *s.points[i]
	}
	return &total
}

func (b *trendsBuild) panel(id, title, unit, direction string, value, previous *float64, truncated bool, series ...trendSeries) map[string]any {
	out := []map[string]any{}
	for _, s := range series {
		seriesUnit := unit
		if s.unit != "" {
			seriesUnit = s.unit
		}
		out = append(out, map[string]any{"key": s.key, "label": s.label, "unit": seriesUnit, "points": s.points[b.w.n:]})
	}
	return map[string]any{"id": id, "title": title, "unit": unit, "direction": direction, "value": value, "previous": previous, "truncated": truncated, "series": out, "rows": []map[string]any{}}
}

// askFlowPanel counts asks raised and withdrawn (from the asks raised in the
// window) beside answers given (by answer time). Each series is null before
// its own family's horizon.
func (b *trendsBuild) askFlowPanel(asks []trendAsk, horizon time.Time, answers []trendAnswer, answersHorizon time.Time) map[string]any {
	var opened, answered, withdrawn []time.Time
	for _, a := range asks {
		opened = append(opened, a.created)
		if a.state == "withdrawn" {
			withdrawn = append(withdrawn, a.answered)
		}
	}
	for _, a := range answers {
		answered = append(answered, a.answered)
	}
	o := b.counts("opened", "Raised", opened, horizon)
	a := b.counts("answered", "Answered", answered, answersHorizon)
	w := b.counts("withdrawn", "Withdrawn or retired", withdrawn, horizon)
	p := b.panel("asks_flow", "Asks raised vs answered", "count", "up_good", b.periodSum(a, 1), b.periodSum(a, 0), !horizon.IsZero() || !answersHorizon.IsZero(), o, a, w)
	p["raised"], p["raised_previous"] = b.periodSum(o, 1), b.periodSum(o, 0)
	return p
}

func median(values []float64) *float64 {
	if len(values) == 0 {
		return nil
	}
	sort.Float64s(values)
	m := len(values) / 2
	if len(values)%2 == 1 {
		return num(values[m])
	}
	return num((values[m-1] + values[m]) / 2)
}

func (b *trendsBuild) answerTimePanel(answers []trendAnswer, horizon time.Time) map[string]any {
	per := make([][]float64, 2*b.w.n)
	periods := [2][]float64{}
	for _, a := range answers {
		if a.answered.IsZero() || a.created.IsZero() || a.answered.Before(a.created) {
			continue
		}
		i := b.w.bucket(a.answered)
		if i < 0 || !b.w.complete(i, horizon) {
			continue
		}
		wait := a.answered.Sub(a.created).Seconds()
		per[i] = append(per[i], wait)
		periods[i/b.w.n] = append(periods[i/b.w.n], wait)
	}
	s := b.newSeries("median", "Median wait")
	for i := range per {
		s.points[i] = median(per[i])
	}
	// A period missing answers has no median: the ones left out could move
	// it either way.
	value, prev := median(periods[1]), median(periods[0])
	if !b.w.complete(b.w.n, horizon) {
		value = nil
	}
	if !b.w.complete(0, horizon) {
		prev = nil
	}
	return b.panel("answer_time", "Time to answer", "seconds", "down_good", value, prev, !horizon.IsZero(), s)
}

// backlogPanel rebuilds open asks at each bucket end: asks open now that were
// raised by then, plus asks raised by then and answered or withdrawn later.
// Answers are read by answer time, so an old ask answered inside the window
// still counts as open before its answer. Withdrawals are known only for asks
// raised inside the window.
func (b *trendsBuild) backlogPanel(asks []trendAsk, horizon time.Time, answers []trendAnswer, answersHorizon time.Time, open []time.Time, openTruncated bool) map[string]any {
	resolved := []trendAnswer{}
	for _, a := range asks {
		if a.state == "withdrawn" {
			resolved = append(resolved, trendAnswer{created: a.created, answered: a.answered})
		}
	}
	resolved = append(resolved, answers...)
	known := horizon
	if answersHorizon.After(known) {
		known = answersHorizon
	}
	s := b.newSeries("open", "Open asks")
	if !openTruncated {
		for i := range s.points {
			end := b.w.bucketEnd(i)
			if !b.w.complete(i, known) {
				continue
			}
			n := 0.0
			for _, t := range open {
				if !t.After(end) {
					n++
				}
			}
			for _, a := range resolved {
				if !a.created.IsZero() && !a.created.After(end) && a.answered.After(end) {
					n++
				}
			}
			s.points[i] = num(n)
		}
	}
	var value, previous *float64
	if !openTruncated {
		value = num(float64(len(open)))
		previous = s.points[b.w.n-1]
	}
	return b.panel("ask_backlog", "Open asks over time", "count", "down_good", value, previous, openTruncated || !known.IsZero(), s)
}

// promisesPanel buckets each promise by when it fell due. With more open
// promises than the read admits, the ones after openCutoff are missing, so
// those buckets are unknown.
func (b *trendsBuild) promisesPanel(promises []trendPromise, horizon, openCutoff time.Time) map[string]any {
	kept, missed := b.newSeries("kept", "Kept"), b.newSeries("missed", "Missed")
	kept.unit, missed.unit = "count", "count"
	for i := range kept.points {
		if b.w.complete(i, horizon) && (openCutoff.IsZero() || !b.w.bucketEnd(i).After(openCutoff)) {
			kept.points[i], missed.points[i] = num(0), num(0)
		}
	}
	for _, p := range promises {
		if p.cancelled || p.promise.After(b.now) {
			continue
		}
		i := b.w.bucket(p.promise)
		if i < 0 || kept.points[i] == nil {
			continue
		}
		if !p.open && !p.done.IsZero() && !p.done.After(p.promise) {
			*kept.points[i]++
		} else if p.open || !p.done.IsZero() {
			*missed.points[i]++
		}
	}
	rate := func(period int) *float64 {
		k, m := b.periodSum(kept, period), b.periodSum(missed, period)
		if k == nil || m == nil || *k+*m == 0 {
			return nil
		}
		return num(100 * *k / (*k + *m))
	}
	p := b.panel("promises", "Promises kept", "percent", "up_good", rate(1), rate(0), !horizon.IsZero() || !openCutoff.IsZero(), kept, missed)
	p["due"] = func() *float64 {
		k, m := b.periodSum(kept, 1), b.periodSum(missed, 1)
		if k == nil || m == nil {
			return nil
		}
		return num(*k + *m)
	}()
	return p
}

// pastPromisePanel is the share of open, promised initiatives past their
// promise at each bucket end, measured against each one's current promise.
func (b *trendsBuild) pastPromisePanel(promises []trendPromise, horizon time.Time, openTruncated bool) map[string]any {
	s := b.newSeries("past", "Past promise")
	counts := make([][2]float64, 2*b.w.n)
	for i := range s.points {
		end := b.w.bucketEnd(i)
		if i == 2*b.w.n-1 {
			end = b.now
		}
		past, total := 0.0, 0.0
		for _, p := range promises {
			if p.cancelled || (!p.created.IsZero() && p.created.After(end)) || (!p.open && !p.done.IsZero() && !p.done.After(end)) || (!p.open && p.done.IsZero()) {
				continue
			}
			total++
			if p.promise.Before(end) {
				past++
			}
		}
		counts[i] = [2]float64{past, total}
		if total > 0 && !openTruncated && b.w.complete(i, horizon) {
			s.points[i] = num(100 * past / total)
		}
	}
	p := b.panel("past_promise", "Initiatives past their promise", "percent", "down_good", s.points[2*b.w.n-1], s.points[b.w.n-1], openTruncated || !horizon.IsZero(), s)
	p["past"], p["total"] = nil, nil
	if last := counts[2*b.w.n-1]; !openTruncated {
		p["past"], p["total"] = last[0], last[1]
	}
	return p
}

func (b *trendsBuild) workFlowPanel(created []time.Time, createdHorizon time.Time, done []time.Time, doneHorizon time.Time) map[string]any {
	c := b.counts("started", "Started", created, createdHorizon)
	d := b.counts("finished", "Finished", done, doneHorizon)
	p := b.panel("work_flow", "Work started vs finished", "count", "up_good", b.periodSum(d, 1), b.periodSum(d, 0), !createdHorizon.IsZero() || !doneHorizon.IsZero(), c, d)
	p["started"], p["started_previous"] = b.periodSum(c, 1), b.periodSum(c, 0)
	return p
}

func (b *trendsBuild) loopRunsPanel(runs []trendRun, horizon time.Time) map[string]any {
	var all, failed []time.Time
	for _, r := range runs {
		all = append(all, r.at)
		if r.infraErr {
			failed = append(failed, r.at)
		}
	}
	a := b.counts("runs", "Runs", all, horizon)
	f := b.counts("failed", "Failed to run", failed, horizon)
	p := b.panel("loop_runs", "Loop runs", "count", "up_good", b.periodSum(a, 1), b.periodSum(a, 0), !horizon.IsZero(), a, f)
	p["failed"], p["failed_previous"] = b.periodSum(f, 1), b.periodSum(f, 0)
	return p
}

// loopHealthPanel is the share of loops that had reported within two of their
// cadences at each bucket end: the brief's own "stopped reporting" rule. A
// bucket whose lookback reaches before the read (or a capped read's horizon)
// is unknown rather than unhealthy.
//
// With more loops than the read admits, the population is incomplete and
// every share is unknown: the loops left out could be the overdue ones.
func (b *trendsBuild) loopHealthPanel(loops []trendLoop, runs []trendRun, horizon time.Time, partial bool) map[string]any {
	if partial {
		s := b.newSeries("on_time", "Reporting on time")
		return b.panel("loop_health", "Loops reporting on time", "percent", "up_good", nil, nil, true, s)
	}
	bySubject := map[string][]time.Time{}
	for _, r := range runs {
		bySubject[r.subject] = append(bySubject[r.subject], r.at)
	}
	known := b.w.start()
	if horizon.After(known) {
		known = horizon
	}
	s := b.newSeries("on_time", "Reporting on time")
	for i := range s.points {
		end := b.w.bucketEnd(i)
		if i == 2*b.w.n-1 {
			end = b.now
		}
		ok, total, unknown := 0.0, 0.0, false
		for _, l := range loops {
			if !l.created.IsZero() && l.created.After(end) {
				continue
			}
			since := end.Add(-2 * l.every)
			if since.Before(known) {
				unknown = true
				break
			}
			total++
			if trendReported(bySubject, l.refs, since, end) {
				ok++
			}
		}
		if !unknown && total > 0 {
			s.points[i] = num(100 * ok / total)
		}
	}
	return b.panel("loop_health", "Loops reporting on time", "percent", "up_good", s.points[2*b.w.n-1], s.points[b.w.n-1], !horizon.IsZero(), s)
}

func trendReported(bySubject map[string][]time.Time, refs []string, since, end time.Time) bool {
	for _, ref := range refs {
		for _, t := range bySubject[ref] {
			if t.After(since) && !t.After(end) {
				return true
			}
		}
	}
	return false
}

// askSourcesPanel ranks the principals that raised the most asks this period:
// who needs the most help.
func (b *trendsBuild) askSourcesPanel(asks []trendAsk, horizon time.Time) (map[string]any, error) {
	events := make([]trendActorEvent, 0, len(asks))
	for _, a := range asks {
		events = append(events, trendActorEvent{a.requester, a.created})
	}
	return b.rankedPanel("ask_sources", "Who asks for help most", "down_good", events, horizon)
}

// agentRunsPanel ranks the principals that recorded the most loop runs.
func (b *trendsBuild) agentRunsPanel(runs []trendRun, horizon time.Time) (map[string]any, error) {
	events := make([]trendActorEvent, 0, len(runs))
	for _, r := range runs {
		events = append(events, trendActorEvent{r.actor, r.at})
	}
	return b.rankedPanel("agent_runs", "Most active agents", "neutral", events, horizon)
}

type trendActorEvent struct {
	actor string
	at    time.Time
}

// rankedPanel counts events per principal in each period and keeps the top
// trendsSourceRows by the current period. previous is null when the read did
// not reach back through the whole previous period.
func (b *trendsBuild) rankedPanel(id, title, direction string, events []trendActorEvent, horizon time.Time) (map[string]any, error) {
	type tally struct{ current, previous float64 }
	byActor := map[string]*tally{}
	for _, e := range events {
		if e.actor == "" {
			continue
		}
		i := b.w.bucket(e.at)
		if i < 0 {
			continue
		}
		if byActor[e.actor] == nil {
			byActor[e.actor] = &tally{}
		}
		if i >= b.w.n {
			byActor[e.actor].current++
		} else {
			byActor[e.actor].previous++
		}
	}
	ids := []string{}
	for id, t := range byActor {
		if t.current > 0 {
			ids = append(ids, id)
		}
	}
	sort.Slice(ids, func(i, j int) bool {
		if byActor[ids[i]].current != byActor[ids[j]].current {
			return byActor[ids[i]].current > byActor[ids[j]].current
		}
		return ids[i] < ids[j]
	})
	if len(ids) > trendsSourceRows {
		ids = ids[:trendsSourceRows]
	}
	// A ranking from a period the read did not cover in full would present an
	// undercount as exact; the panel says truncated instead.
	if !b.w.complete(b.w.n, horizon) {
		ids = nil
	}
	for _, id := range ids {
		b.actors[id] = true
	}
	if err := b.resolveActors(); err != nil {
		return nil, err
	}
	rows := []map[string]any{}
	for _, id := range ids {
		principal := map[string]any{"id": id, "display": id}
		if p, ok := b.display[id]; ok {
			principal = p
		}
		row := map[string]any{"principal": principal, "value": byActor[id].current, "previous": byActor[id].previous}
		if !b.w.complete(0, horizon) {
			row["previous"] = nil
		}
		rows = append(rows, row)
	}
	p := b.panel(id, title, "count", direction, nil, nil, !horizon.IsZero())
	p["rows"] = rows
	return p, nil
}
