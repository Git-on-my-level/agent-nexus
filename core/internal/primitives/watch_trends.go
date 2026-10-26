package primitives

import (
	"context"
	"sort"
	"strings"
	"time"
)

// Watch trends: whether the workspace is getting better or worse, computed at
// read time from records core already keeps. Every family is one indexed,
// newest-first seek with a fixed cap, read through the caller's scoped
// relations and the brief's PM-private rule, so a hidden record never reaches a
// count. A capped family reports where its history stops: buckets older than
// the oldest admitted record are null rather than a guessed zero.
const (
	trendsAskCap      = 80
	trendsAnswerCap   = 60
	trendsOpenAskCap  = 70
	trendsOpenPromCap = 55
	trendsPromiseCap  = 35
	trendsCreatedCap  = 20
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

// WatchTrends returns one group of watch_trends.v1 for the reader over days
// (7, 30 or 90).
func (s *Store) WatchTrends(ctx context.Context, reader BriefReader, group string, days int, now time.Time) (map[string]any, error) {
	// Under a canonical access scope the PM-private rule is already part of
	// every scoped relation (another owner's PM thread is a denial root), so
	// the per-row owner lookups would only repeat it. The one scope broader
	// than the rule is the PM reading as itself; that keeps the explicit rule.
	if scope, ok := accessScopeFrom(ctx); ok && scope.ActorID != "" && scope.ActorID != scope.PMActorID {
		reader.Visible = nil
	}
	b := &trendsBuild{briefBuild: &briefBuild{s: s, ctx: ctx, reader: reader, now: now.UTC(), actors: map[string]bool{}}, w: newTrendWindow(days, now)}
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
	if len(loops) > 0 {
		health := b.loopHealthPanel(loops, runs, horizon)
		health["truncated"] = health["truncated"] == true || loopsTruncated
		panels = append(panels, health)
	}
	active, err := b.agentRunsPanel(runs, horizon)
	if err != nil {
		return nil, err
	}
	return append(panels, active), nil
}

// trendAsks admits asks raised since the previous period began, newest first,
// through idx_events_ask_order and primary-key joins.
func (b *trendsBuild) trendAsks() ([]trendAsk, time.Time, error) {
	pred, args := b.askThreadOwnerSQL()
	args = append(args, loopTS(b.w.start()), trendsAskCap+1)
	rows, err := b.s.db.QueryContext(b.ctx, `SELECT e.id,e.ts,a.requester,a.state,COALESCE(a.answered_at,''),COALESCE(e.thread_id,''),COALESCE(json_extract(brief_ask_thread.body_json,'$.pm_actor_id'),'')
	 FROM events e CROSS JOIN brief_asks a ON a.ask_id=e.id `+briefAnswerableAskJoin+`
	 WHERE e.type='human_attention_requested' AND e.trashed_at IS NULL AND `+pred+` AND anx_timestamp_key(e.ts)>=anx_timestamp_key(?)
	 ORDER BY anx_timestamp_key(e.ts) DESC,e.id DESC LIMIT ?`, args...)
	if err != nil {
		return nil, time.Time{}, err
	}
	defer rows.Close()
	out := []trendAsk{}
	admitted := 0
	var oldest time.Time
	for rows.Next() {
		var a trendAsk
		var created, answered, thread, owner string
		if err = rows.Scan(&a.id, &created, &a.requester, &a.state, &answered, &thread, &owner); err != nil {
			return nil, time.Time{}, err
		}
		admitted++
		if admitted > trendsAskCap {
			if oldest.IsZero() {
				oldest = b.w.end
			}
			return out, oldest, rows.Err()
		}
		a.created, a.answered = trendTime(created), trendTime(answered)
		a.requester = stripActor(a.requester)
		if !a.created.IsZero() && (oldest.IsZero() || a.created.Before(oldest)) {
			oldest = a.created
		}
		if b.reader.Visible != nil && !b.reader.Visible(thread, owner) {
			continue
		}
		out = append(out, a)
	}
	return out, time.Time{}, rows.Err()
}

// trendAnswers admits answers given since the previous period began, newest
// first, by the answer event through idx_events_ask_order; the resolution and
// the ask join by unique key. Measuring answers by when they happened keeps
// both periods comparable: an answer late in the window to an old ask counts,
// in its own period. Left joins keep every admitted event inside the cap.
func (b *trendsBuild) trendAnswers() ([]trendAnswer, time.Time, error) {
	pred, args := b.askThreadOwnerSQL()
	args = append(args, loopTS(b.w.start()), trendsAnswerCap+1)
	rows, err := b.s.db.QueryContext(b.ctx, `SELECT COALESCE(r.resolution_type,''),COALESCE(a.created_at,''),COALESCE(r.created_at,''),e.ts,COALESCE(e.thread_id,''),COALESCE(json_extract(brief_ask_thread.body_json,'$.pm_actor_id'),'')
	 FROM events e `+briefAnswerableAskJoin+` LEFT JOIN human_attention_request_resolutions r ON r.resolution_event_id=e.id LEFT JOIN brief_asks a ON a.ask_id=r.request_event_id
	 WHERE e.type='human_attention_responded' AND e.trashed_at IS NULL AND `+pred+` AND anx_timestamp_key(e.ts)>=anx_timestamp_key(?)
	 ORDER BY anx_timestamp_key(e.ts) DESC,e.id DESC LIMIT ?`, args...)
	if err != nil {
		return nil, time.Time{}, err
	}
	defer rows.Close()
	out := []trendAnswer{}
	admitted := 0
	var oldest time.Time
	for rows.Next() {
		var kind, created, answered, at, thread, owner string
		if err = rows.Scan(&kind, &created, &answered, &at, &thread, &owner); err != nil {
			return nil, time.Time{}, err
		}
		admitted++
		if admitted > trendsAnswerCap {
			if oldest.IsZero() {
				oldest = b.w.end
			}
			return out, oldest, rows.Err()
		}
		if t := trendTime(at); !t.IsZero() && (oldest.IsZero() || t.Before(oldest)) {
			oldest = t
		}
		// A repeated response to an answered ask has no resolution of its own.
		if kind != "answered" || (b.reader.Visible != nil && !b.reader.Visible(thread, owner)) {
			continue
		}
		out = append(out, trendAnswer{created: trendTime(created), answered: trendTime(answered)})
	}
	return out, time.Time{}, rows.Err()
}

// trendOpenAsks reads the creation time of every open ask the reader can see,
// up to the cap, through idx_brief_asks_open_decide_by.
func (b *trendsBuild) trendOpenAsks() ([]time.Time, bool, error) {
	pred, args := b.askThreadOwnerSQL()
	args = append(args, trendsOpenAskCap+1)
	rows, err := b.s.db.QueryContext(b.ctx, `SELECT a.created_at,COALESCE(e.thread_id,''),COALESCE(json_extract(brief_ask_thread.body_json,'$.pm_actor_id'),'')
	 FROM brief_asks a CROSS JOIN events e ON e.id=a.ask_id `+briefAnswerableAskJoin+`
	 WHERE a.state='open' AND COALESCE(e.trashed_at,'')='' AND `+pred+` LIMIT ?`, args...)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	out := []time.Time{}
	admitted := 0
	for rows.Next() {
		var created, thread, owner string
		if err = rows.Scan(&created, &thread, &owner); err != nil {
			return nil, false, err
		}
		admitted++
		if admitted > trendsOpenAskCap {
			return out, true, rows.Err()
		}
		if b.reader.Visible != nil && !b.reader.Visible(thread, owner) {
			continue
		}
		out = append(out, trendTime(created))
	}
	return out, false, rows.Err()
}

// trendCardOwner is the PM-private owner of a card's backing thread.
const trendCardOwnerSQL = `COALESCE((SELECT json_extract(t.body_json,'$.pm_actor_id') FROM threads t WHERE t.id=c.thread_id),'')`

// eventOwner is cardOwner for an event's thread.
func (b *trendsBuild) eventOwner() string {
	if b.reader.Visible == nil {
		return "''"
	}
	return `COALESCE((SELECT json_extract(t.body_json,'$.pm_actor_id') FROM threads t WHERE t.id=e.thread_id),'')`
}

// cardOwner selects the owner only when the explicit rule applies.
func (b *trendsBuild) cardOwner() string {
	if b.reader.Visible == nil {
		return "''"
	}
	return trendCardOwnerSQL
}

// trendDoneAt is the latest entry into Done, by the lifecycle index.
const trendDoneAt = `CASE WHEN c.column_key='done' THEN (SELECT e.ts FROM events e
 WHERE e.id=(SELECT position.id FROM work_done_event_positions position WHERE position.card_id=+c.id ORDER BY anx_timestamp_key(position.ts) DESC,position.id DESC LIMIT 1)) END`

// trendPromises admits top-level initiatives with a promise: open ones by
// soonest promise (the overdue first), and closed ones whose promise fell in
// the window. Both seek idx_brief_initiatives_page through an unscoped view, so
// the cap counts every admitted row and visibility is a flag on each: a hidden
// or child initiative uses a place in the window but returns nothing about
// itself. The horizon and cutoff come from visible rows only, which errs
// towards marking more history unknown and never times a hidden promise.
func (b *trendsBuild) trendPromises() ([]trendPromise, time.Time, time.Time, error) {
	out := []trendPromise{}
	var horizon, openCutoff time.Time
	for _, open := range []bool{true, false} {
		admission := `open=1 AND promise_at IS NOT NULL ORDER BY open,promise_at`
		args := []any{}
		if !open {
			admission = `open=0 AND promise_at>=? ORDER BY open DESC,promise_at DESC`
			args = append(args, loopTS(b.w.start()))
		}
		limit := trendsPromiseCap
		if open {
			limit = trendsOpenPromCap
		}
		args = append(args, limit+1)
		pred, predArgs := b.pmOwnerSQL("c.thread_id")
		args = append(args, predArgs...)
		rows, err := b.s.db.QueryContext(b.groupingContext(), `SELECT CASE WHEN bi.card_id IS NOT NULL AND c.id IS NOT NULL AND COALESCE(c.archived_at,'')='' AND COALESCE(c.trashed_at,'')=''
		  AND (NOT EXISTS(SELECT 1 FROM boards WHERE role='initiatives') OR c.board_id IN (SELECT id FROM boards WHERE role='initiatives'))
		  AND NOT `+workHasParentSQL("c")+` AND `+pred+` THEN 1 ELSE 0 END,
		 COALESCE(bi.open,0),COALESCE(bi.promise_at,''),COALESCE(c.created_at,''),COALESCE(c.column_key,''),COALESCE(`+trendDoneAt+`,''),COALESCE(c.thread_id,''),`+b.cardOwner()+`
		 FROM (SELECT card_id FROM work_grouping_brief_positions WHERE `+admission+` LIMIT ?) admitted
		 LEFT JOIN brief_initiatives bi ON bi.card_id=admitted.card_id LEFT JOIN cards c ON c.id=admitted.card_id`, args...)
		if err != nil {
			return nil, time.Time{}, time.Time{}, err
		}
		admitted := 0
		var last time.Time
		for rows.Next() {
			var p trendPromise
			var visible bool
			var promise, created, phase, done, thread, owner string
			if err = rows.Scan(&visible, &p.open, &promise, &created, &phase, &done, &thread, &owner); err != nil {
				rows.Close()
				return nil, time.Time{}, time.Time{}, err
			}
			admitted++
			if admitted > limit {
				// Closed promises are newest first: older history stops at the
				// last one read. Open ones are soonest first: later promises
				// are missing, so buckets after the last one read are unknown.
				if last.IsZero() {
					last = b.w.end
					if open {
						last = b.w.start()
					}
				}
				if open {
					openCutoff = last
				} else {
					horizon = last
				}
				break
			}
			if !visible || (b.reader.Visible != nil && !b.reader.Visible(thread, owner)) {
				continue
			}
			p.promise, p.created, p.done, p.cancelled = trendTime(promise), trendTime(created), trendTime(done), phase == "cancelled"
			last = p.promise
			out = append(out, p)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, time.Time{}, time.Time{}, err
		}
	}
	return out, horizon, openCutoff, nil
}

// trendCreated admits cards created in the window, newest first, through the
// two grouping indexes (open and closed cards).
func (b *trendsBuild) trendCreated() ([]time.Time, time.Time, error) {
	out := []time.Time{}
	var horizon time.Time
	for _, closed := range []string{`NOT IN ('done','cancelled')`, `IN ('done','cancelled')`} {
		pred, predArgs := b.pmOwnerSQL("c.thread_id")
		args := append([]any{loopTS(b.w.start())}, predArgs...)
		args = append(args, trendsCreatedCap+1)
		rows, err := b.s.db.QueryContext(b.groupingContext(), `SELECT c.created_at,c.created_at,COALESCE(c.thread_id,''),`+b.cardOwner()+`
		 FROM cards c WHERE c.trashed_at IS NULL AND c.archived_at IS NULL AND c.column_key `+closed+` AND c.created_at>=? AND `+pred+`
		 ORDER BY c.trashed_at,c.archived_at,c.created_at DESC,c.id DESC LIMIT ?`, args...)
		if err != nil {
			return nil, time.Time{}, err
		}
		h, err := b.collectTimes(rows, trendsCreatedCap, &out)
		if err != nil {
			return nil, time.Time{}, err
		}
		if h.After(horizon) {
			horizon = h
		}
	}
	return out, horizon, nil
}

// trendDone admits cards that entered Done in the window. Their last update
// bounds the done time from above, so idx_cards_closed_work_page seeks them
// newest first; the lifecycle index supplies the exact time.
func (b *trendsBuild) trendDone() ([]time.Time, time.Time, error) {
	pred, predArgs := b.pmOwnerSQL("c.thread_id")
	args := append([]any{loopTS(b.w.start())}, predArgs...)
	args = append(args, trendsDoneCap+1)
	rows, err := b.s.db.QueryContext(b.groupingContext(), `SELECT COALESCE(`+trendDoneAt+`,c.updated_at),c.updated_at,COALESCE(c.thread_id,''),`+b.cardOwner()+`
	 FROM cards c WHERE c.trashed_at IS NULL AND c.archived_at IS NULL AND c.column_key='done' AND anx_timestamp_key(c.updated_at)>=anx_timestamp_key(?) AND `+pred+`
	 ORDER BY anx_timestamp_key(c.updated_at) DESC,c.id DESC LIMIT ?`, args...)
	if err != nil {
		return nil, time.Time{}, err
	}
	out := []time.Time{}
	horizon, err := b.collectTimes(rows, trendsDoneCap, &out)
	return out, horizon, err
}

type trendRows interface {
	Next() bool
	Scan(...any) error
	Err() error
	Close() error
}

// collectTimes reads (time, order, thread, owner) rows admitted in descending
// order. When the cap is exceeded the horizon is the last admitted row's order
// value: every row not read sorts at or before it, so its time does too.
func (b *trendsBuild) collectTimes(rows trendRows, limit int, out *[]time.Time) (time.Time, error) {
	defer rows.Close()
	admitted := 0
	horizon := b.w.end
	for rows.Next() {
		var at, order, thread, owner string
		if err := rows.Scan(&at, &order, &thread, &owner); err != nil {
			return time.Time{}, err
		}
		admitted++
		if admitted > limit {
			return horizon, rows.Err()
		}
		if t := trendTime(order); !t.IsZero() {
			horizon = t
		}
		if b.reader.Visible != nil && !b.reader.Visible(thread, owner) {
			continue
		}
		*out = append(*out, trendTime(at))
	}
	return time.Time{}, rows.Err()
}

// trendRuns admits run receipts in the window, newest first, through
// idx_events_ask_order (type, time key, id).
func (b *trendsBuild) trendRuns() ([]trendRun, time.Time, error) {
	pred, predArgs := b.pmOwnerSQL("e.thread_id")
	args := append([]any{loopTS(b.w.start())}, predArgs...)
	args = append(args, trendsRunCap+1)
	rows, err := b.s.db.QueryContext(b.ctx, `SELECT e.ts,COALESCE(json_extract(e.payload_json,'$.subject_ref'),''),COALESCE(e.actor_id,''),COALESCE(json_extract(e.payload_json,'$.payload.outcome'),''),COALESCE(e.thread_id,''),`+b.eventOwner()+`
	 FROM events e WHERE e.type='run_recorded' AND e.trashed_at IS NULL AND anx_timestamp_key(e.ts)>=anx_timestamp_key(?) AND `+pred+`
	 ORDER BY anx_timestamp_key(e.ts) DESC,e.id DESC LIMIT ?`, args...)
	if err != nil {
		return nil, time.Time{}, err
	}
	defer rows.Close()
	out := []trendRun{}
	admitted := 0
	var oldest time.Time
	for rows.Next() {
		var at, outcome, thread, owner string
		var r trendRun
		if err = rows.Scan(&at, &r.subject, &r.actor, &outcome, &thread, &owner); err != nil {
			return nil, time.Time{}, err
		}
		admitted++
		if admitted > trendsRunCap {
			if oldest.IsZero() {
				oldest = b.w.end
			}
			return out, oldest, rows.Err()
		}
		r.at, r.infraErr, r.actor = trendTime(at), outcome == "infra_error", stripActor(r.actor)
		if !r.at.IsZero() && (oldest.IsZero() || r.at.Before(oldest)) {
			oldest = r.at
		}
		if b.reader.Visible != nil && !b.reader.Visible(thread, owner) {
			continue
		}
		out = append(out, r)
	}
	return out, time.Time{}, rows.Err()
}

// trendLoops admits the most recently annotated loops through
// idx_brief_loop_annotated_recent. As with promises the view is unscoped, so
// the cap counts every admitted row and visibility is a flag.
func (b *trendsBuild) trendLoops() ([]trendLoop, bool, error) {
	pred, predArgs := b.pmOwnerSQL("c.thread_id")
	args := append([]any{trendsLoopCap + 1}, predArgs...)
	rows, err := b.s.db.QueryContext(b.groupingContext(), `SELECT CASE WHEN c.id IS NOT NULL AND COALESCE(c.archived_at,'')='' AND COALESCE(c.trashed_at,'')='' AND c.column_key NOT IN ('done','cancelled') AND `+pred+` THEN 1 ELSE 0 END,
	 COALESCE(c.id,''),COALESCE(c.handle,''),COALESCE(c.created_at,''),COALESCE(json_extract(m.metadata_json,'$.cadence.every_seconds'),0),COALESCE(c.thread_id,''),`+b.cardOwner()+`
	 FROM (SELECT card_id FROM work_grouping_brief_positions WHERE loop_annotated=1 ORDER BY loop_annotated_at DESC,card_id LIMIT ?) admitted
	 LEFT JOIN cards c ON c.id=admitted.card_id LEFT JOIN work_metadata m ON m.card_id=c.id`, args...)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	out := []trendLoop{}
	admitted := 0
	for rows.Next() {
		var visible bool
		var id, handle, created, thread, owner string
		var every float64
		if err = rows.Scan(&visible, &id, &handle, &created, &every, &thread, &owner); err != nil {
			return nil, false, err
		}
		admitted++
		if admitted > trendsLoopCap {
			return out, true, rows.Err()
		}
		if !visible || every <= 0 || (b.reader.Visible != nil && !b.reader.Visible(thread, owner)) {
			continue
		}
		l := trendLoop{refs: []string{"card:" + id}, created: trendTime(created), every: time.Duration(every) * time.Second}
		if handle != "" {
			l.refs = append(l.refs, "card:"+handle)
		}
		out = append(out, l)
	}
	return out, false, rows.Err()
}

// --- panels ---

type trendSeries struct {
	key, label string
	points     []*float64
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
		out = append(out, map[string]any{"key": s.key, "label": s.label, "points": s.points[b.w.n:]})
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
	prev := median(periods[0])
	if !b.w.complete(0, horizon) {
		prev = nil
	}
	return b.panel("answer_time", "Time to answer", "seconds", "down_good", median(periods[1]), prev, !horizon.IsZero(), s)
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
func (b *trendsBuild) loopHealthPanel(loops []trendLoop, runs []trendRun, horizon time.Time) map[string]any {
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
