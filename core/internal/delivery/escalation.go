package delivery

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	// Immediate push when the ask's window was at most this long, or when it
	// blocks at least ImmediateBlocks refs (end-state §2.4 step 5).
	ImmediateWindow = 2 * time.Hour
	ImmediateBlocks = 3

	DefaultEscalationInterval = 30 * time.Second
	DefaultDigestHourUTC      = 9

	normalizeBatch = 200
	escalateBatch  = 50
	recipientCap   = 200
	digestItemCap  = 10
	digestScanCap  = 50
	// deadlinePage is the primary-key page while walking the open-ask ledger.
	// The walk continues until the ledger is exhausted, so unreadable rows
	// cannot fill the page and hide a later readable ask from a count.
	deadlinePage    = 500
	maxByDuration   = 3650 * 24 * time.Hour
	EscalationPush  = "push"
	EscalationQueue = "digest"
)

var byDuration = regexp.MustCompile(`^([1-9][0-9]*)([smhd])$`)

// ResolveBy resolves the review_by grammar (UTC date, zoned timestamp or a
// positive s/m/h/d duration from the ask time). A bare date means the end of
// that UTC day.
func ResolveBy(raw string, from time.Time) (time.Time, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, false
	}
	if t, err := time.Parse(time.RFC3339Nano, raw); err == nil {
		return t.UTC(), true
	}
	if t, err := time.Parse("2006-01-02", raw); err == nil {
		return t.Add(24*time.Hour - time.Second).UTC(), true
	}
	if m := byDuration.FindStringSubmatch(raw); m != nil {
		n, err := strconv.Atoi(m[1])
		if err != nil {
			return time.Time{}, false
		}
		unit := map[string]time.Duration{"s": time.Second, "m": time.Minute, "h": time.Hour, "d": 24 * time.Hour}[m[2]]
		d := time.Duration(n) * unit
		if d <= 0 || d > maxByDuration || d/unit != time.Duration(n) {
			return time.Time{}, false
		}
		return from.Add(d).UTC(), true
	}
	return time.Time{}, false
}

// EscalationMode applies the immediate-push rule to one overdue ask.
func EscalationMode(askTS, decideBy time.Time, blocks int) string {
	if blocks >= ImmediateBlocks || decideBy.Sub(askTS) <= ImmediateWindow {
		return EscalationPush
	}
	return EscalationQueue
}

// Escalator runs the decide_by clock: it normalizes newly recorded asks,
// escalates asks whose decide_by passed unanswered and queues one digest per
// recipient channel per day. Every read is a seek on an ask_deadlines or
// notification_channel_settings index with a fixed batch cap.
type Escalator struct {
	Store         Store
	WorkspaceName string
	// LinkBase, when set, prefixes ask links (for example the web UI origin).
	LinkBase      string
	DigestHourUTC int
	Interval      time.Duration
	// CanRead reports whether principalID may read the ask event. Content for
	// a principal is withheld unless it returns true.
	CanRead func(ctx context.Context, principalID, askEventID string) bool
	// AskPM asks the workspace PM to answer, withdraw or re-ask. Optional.
	AskPM func(ctx context.Context, askEventID string, decideBy time.Time) error
	Logf  func(format string, args ...any)
}

func (e *Escalator) Run(ctx context.Context) {
	if e == nil || e.Store.DB == nil {
		return
	}
	interval := e.Interval
	if interval <= 0 {
		interval = DefaultEscalationInterval
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if err := e.Step(ctx, time.Now().UTC()); err != nil && ctx.Err() == nil {
			e.logf("delivery escalation: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (e *Escalator) Step(ctx context.Context, now time.Time) error {
	if err := e.normalize(ctx); err != nil {
		return err
	}
	if err := e.escalate(ctx, now); err != nil {
		return err
	}
	return e.digest(ctx, now)
}

func (e *Escalator) normalize(ctx context.Context) error {
	rows, err := e.Store.DB.QueryContext(ctx, `SELECT request_event_id, ask_ts, by_raw FROM ask_deadlines INDEXED BY idx_ask_deadlines_pending WHERE normalized=0 LIMIT ?`, normalizeBatch)
	if err != nil {
		return fmt.Errorf("select pending ask deadlines: %w", err)
	}
	type pending struct{ id, ts, raw string }
	items := []pending{}
	for rows.Next() {
		var p pending
		if err := rows.Scan(&p.id, &p.ts, &p.raw); err != nil {
			rows.Close()
			return err
		}
		items = append(items, p)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, p := range items {
		var decideBy any
		if askTS, err := time.Parse(time.RFC3339Nano, p.ts); err == nil {
			if t, ok := ResolveBy(p.raw, askTS); ok {
				decideBy = stamp(t)
			}
		}
		if _, err := e.Store.DB.ExecContext(ctx, `UPDATE ask_deadlines SET decide_by=?, normalized=1 WHERE request_event_id=?`, decideBy, p.id); err != nil {
			return fmt.Errorf("normalize ask deadline: %w", err)
		}
	}
	return nil
}

type recipient struct{ principal, channel string }

func (e *Escalator) recipients(ctx context.Context, column string) ([]recipient, error) {
	rows, err := e.Store.DB.QueryContext(ctx, `SELECT principal_id, channel FROM notification_channel_settings WHERE `+column+`=1 AND address<>'' ORDER BY channel, principal_id LIMIT ?`, recipientCap)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []recipient{}
	for rows.Next() {
		var r recipient
		if err := rows.Scan(&r.principal, &r.channel); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

type dueAsk struct {
	id       string
	askTS    time.Time
	decideBy time.Time
	blocks   int
}

func (e *Escalator) escalate(ctx context.Context, now time.Time) error {
	rows, err := e.Store.DB.QueryContext(ctx, `SELECT request_event_id, ask_ts, decide_by, blocks_count FROM ask_deadlines INDEXED BY idx_ask_deadlines_due
		WHERE escalated_at IS NULL AND decide_by IS NOT NULL AND decide_by<=? ORDER BY decide_by, request_event_id LIMIT ?`, stamp(now), escalateBatch)
	if err != nil {
		return fmt.Errorf("select due asks: %w", err)
	}
	due := []dueAsk{}
	for rows.Next() {
		var a dueAsk
		var ts, by string
		if err := rows.Scan(&a.id, &ts, &by, &a.blocks); err != nil {
			rows.Close()
			return err
		}
		a.askTS, _ = time.Parse(time.RFC3339Nano, ts)
		a.decideBy, _ = time.Parse(time.RFC3339Nano, by)
		due = append(due, a)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	if len(due) == 0 {
		return nil
	}
	var pushRecipients []recipient
	for _, a := range due {
		if a.blocks >= ImmediateBlocks {
			// Blocks refs that reached a terminal state clear automatically.
			if a.blocks, err = e.openBlocks(ctx, a.id); err != nil {
				return err
			}
		}
		mode := EscalationMode(a.askTS, a.decideBy, a.blocks)
		if mode == EscalationPush {
			if pushRecipients == nil {
				if pushRecipients, err = e.recipients(ctx, "push_enabled"); err != nil {
					return err
				}
			}
			title := e.askTitle(ctx, a.id)
			for _, r := range pushRecipients {
				if e.CanRead == nil || !e.CanRead(ctx, r.principal, a.id) {
					// A principal who cannot read the ask learns nothing about it.
					continue
				}
				if _, _, err := e.Store.Enqueue(ctx, Request{
					IdempotencyKey: "push:" + a.id + ":" + r.principal + ":" + r.channel,
					PrincipalID:    r.principal,
					Channel:        r.channel,
					Kind:           KindPush,
					SourceRef:      "event:" + a.id,
					Payload: map[string]any{
						"type":         "ask_overdue",
						"workspace":    e.WorkspaceName,
						"title":        title,
						"ref":          "event:" + a.id,
						"link":         e.link(a.id),
						"decide_by":    stamp(a.decideBy),
						"blocks_count": a.blocks,
					},
				}); err != nil {
					return err
				}
			}
		}
		if e.AskPM != nil {
			if err := e.AskPM(ctx, a.id, a.decideBy); err != nil {
				e.logf("delivery escalation: ask PM for %s: %v", a.id, err)
			}
		}
		if _, err := e.Store.DB.ExecContext(ctx, `UPDATE ask_deadlines SET escalated_at=?, escalation=? WHERE request_event_id=?`, stamp(now), mode, a.id); err != nil {
			return fmt.Errorf("mark ask escalated: %w", err)
		}
	}
	return nil
}

func (e *Escalator) digest(ctx context.Context, now time.Time) error {
	hour := e.DigestHourUTC
	if hour <= 0 || hour > 23 {
		hour = DefaultDigestHourUTC
	}
	if now.Hour() < hour {
		return nil
	}
	day := now.Format("2006-01-02")
	recipients, err := e.recipients(ctx, "digest_enabled")
	if err != nil || len(recipients) == 0 {
		return err
	}
	var overdue []dueAsk
	var ledger []deadlineFact
	loaded := false
	for _, r := range recipients {
		key := "digest:" + r.principal + ":" + r.channel + ":" + day
		if _, err := e.Store.getByKey(ctx, key); err == nil {
			continue
		} else if err != ErrNotFound {
			return err
		}
		if !loaded {
			if overdue, err = e.overdue(ctx, now); err != nil {
				return err
			}
			if ledger, err = loadDeadlineFacts(ctx, e.Store.DB); err != nil {
				return err
			}
			loaded = true
		}
		items := []map[string]any{}
		visible := 0
		for _, a := range overdue {
			if e.CanRead == nil || !e.CanRead(ctx, r.principal, a.id) {
				continue
			}
			visible++
			if len(items) < digestItemCap {
				items = append(items, map[string]any{"title": e.askTitle(ctx, a.id), "ref": "event:" + a.id, "link": e.link(a.id), "decide_by": stamp(a.decideBy)})
			}
		}
		if visible == 0 {
			// Nothing overdue for this recipient today; no message.
			continue
		}
		open, _, _ := readableDeadlineCounts(ledger, now, func(id string) bool {
			return e.CanRead != nil && e.CanRead(ctx, r.principal, id)
		})
		if _, _, err := e.Store.Enqueue(ctx, Request{
			IdempotencyKey: key,
			PrincipalID:    r.principal,
			Channel:        r.channel,
			Kind:           KindDigest,
			Payload: map[string]any{
				"type":      "digest",
				"workspace": e.WorkspaceName,
				"day":       day,
				"open":      open,
				"overdue":   visible,
				"items":     items,
			},
		}); err != nil {
			return err
		}
	}
	return nil
}

func (e *Escalator) overdue(ctx context.Context, now time.Time) ([]dueAsk, error) {
	rows, err := e.Store.DB.QueryContext(ctx, `SELECT request_event_id, decide_by FROM ask_deadlines INDEXED BY idx_ask_deadlines_decide_by WHERE decide_by IS NOT NULL AND decide_by<=? ORDER BY decide_by LIMIT ?`, stamp(now), digestScanCap)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []dueAsk{}
	for rows.Next() {
		var a dueAsk
		var by string
		if err := rows.Scan(&a.id, &by); err != nil {
			return nil, err
		}
		a.decideBy, _ = time.Parse(time.RFC3339Nano, by)
		out = append(out, a)
	}
	return out, rows.Err()
}

func (e *Escalator) askTitle(ctx context.Context, id string) string {
	var title sql.NullString
	_ = e.Store.DB.QueryRowContext(ctx, `SELECT json_extract(payload_json,'$.payload.title') FROM events WHERE id=?`, id).Scan(&title)
	return title.String
}

func (e *Escalator) link(id string) string {
	base := strings.TrimRight(strings.TrimSpace(e.LinkBase), "/")
	if base == "" {
		return "event:" + id
	}
	return base + "/inbox?ref=event:" + id
}

func (e *Escalator) logf(format string, args ...any) {
	if e.Logf != nil {
		e.Logf(format, args...)
		return
	}
	log.Printf(format, args...)
}

// AttentionSummary is the heartbeat's due counts and earliest decide_by:
// counts and times only, never titles or refs. The top-level totals are the
// workspace ledger, for the control plane. Each principal's due, overdue and
// earliest_decide_by count only asks that principal can read, and are never
// a copy of the workspace totals.
func (s Store) AttentionSummary(ctx context.Context, now time.Time) (map[string]any, error) {
	var open, overdue int
	var earliest sql.NullString
	if err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM ask_deadlines`).Scan(&open); err != nil {
		return nil, err
	}
	if err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM ask_deadlines INDEXED BY idx_ask_deadlines_decide_by WHERE decide_by IS NOT NULL AND decide_by<=?`, stamp(now)).Scan(&overdue); err != nil {
		return nil, err
	}
	// Earliest unescalated deadline is the next time the clock must run.
	if err := s.DB.QueryRowContext(ctx, `SELECT MIN(decide_by) FROM ask_deadlines INDEXED BY idx_ask_deadlines_due WHERE escalated_at IS NULL AND decide_by IS NOT NULL`).Scan(&earliest); err != nil {
		return nil, err
	}
	var earliestValue any
	if earliest.Valid && earliest.String != "" {
		earliestValue = earliest.String
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT DISTINCT principal_id FROM notification_channel_settings WHERE address<>'' AND (push_enabled=1 OR digest_enabled=1) ORDER BY principal_id LIMIT ?`, recipientCap)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	principals := []map[string]any{}
	var ledger []deadlineFact
	ledgerLoaded := false
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		due, principalOverdue := 0, 0
		var principalEarliest any
		if s.CanRead != nil {
			if !ledgerLoaded {
				if ledger, err = loadDeadlineFacts(ctx, s.DB); err != nil {
					return nil, err
				}
				ledgerLoaded = true
			}
			due, principalOverdue, principalEarliest = readableDeadlineCounts(ledger, now, func(askID string) bool {
				return s.CanRead(ctx, id, askID)
			})
		}
		principals = append(principals, map[string]any{"principal_id": id, "due": due, "overdue": principalOverdue, "earliest_decide_by": principalEarliest})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return map[string]any{
		"earliest_decide_by": earliestValue,
		"open_decisions":     open,
		"overdue_decisions":  overdue,
		"principals":         principals,
		"generated_at":       stamp(now),
	}, nil
}

type deadlineFact struct {
	id        string
	decideBy  string
	escalated string
}

// loadDeadlineFacts reads the open-ask ledger in primary-key pages. It does
// not stop after a fixed window of rows, so a run of unreadable asks cannot
// hide a later readable one from a count.
func loadDeadlineFacts(ctx context.Context, db *sql.DB) ([]deadlineFact, error) {
	out := []deadlineFact{}
	after := ""
	for {
		rows, err := db.QueryContext(ctx, `SELECT request_event_id, COALESCE(decide_by,''), COALESCE(escalated_at,'') FROM ask_deadlines WHERE request_event_id>? ORDER BY request_event_id LIMIT ?`, after, deadlinePage)
		if err != nil {
			return nil, err
		}
		n := 0
		for rows.Next() {
			var f deadlineFact
			if err := rows.Scan(&f.id, &f.decideBy, &f.escalated); err != nil {
				rows.Close()
				return nil, err
			}
			out = append(out, f)
			after = f.id
			n++
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
		if n < deadlinePage {
			return out, nil
		}
	}
}

// readableDeadlineCounts counts asks readable reports. A nil-equivalent
// readable that always returns false yields zeros: unknown access is not
// the workspace total.
func readableDeadlineCounts(ledger []deadlineFact, now time.Time, readable func(askID string) bool) (open, overdue int, earliest any) {
	dueBy := stamp(now)
	var earliestText string
	for _, f := range ledger {
		if readable == nil || !readable(f.id) {
			continue
		}
		open++
		if f.decideBy != "" && f.decideBy <= dueBy {
			overdue++
		}
		if f.escalated == "" && f.decideBy != "" && (earliestText == "" || f.decideBy < earliestText) {
			earliestText = f.decideBy
		}
	}
	if earliestText != "" {
		earliest = earliestText
	}
	return open, overdue, earliest
}

const maxBlocksChecked = 20

// openBlocks counts the ask's blocks refs that are still open: card refs whose
// card is done, cancelled, archived, trashed or missing are dropped; other refs
// count as open. It reads only the ask (primary key) and at most
// maxBlocksChecked cards, and returns a count, never content.
func (e *Escalator) openBlocks(ctx context.Context, askID string) (int, error) {
	rows, err := e.Store.DB.QueryContext(ctx, `SELECT value FROM events, json_each(events.payload_json,'$.payload.blocks') WHERE events.id=? AND json_type(events.payload_json,'$.payload.blocks')='array' LIMIT ?`, askID, maxBlocksChecked)
	if err != nil {
		return 0, err
	}
	refs := []string{}
	for rows.Next() {
		var ref sql.NullString
		if err := rows.Scan(&ref); err != nil {
			rows.Close()
			return 0, err
		}
		if r := strings.TrimSpace(ref.String); r != "" {
			refs = append(refs, r)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	open := 0
	for _, ref := range refs {
		key := strings.TrimPrefix(ref, "card:")
		if key == ref {
			open++
			continue
		}
		var live int
		if err := e.Store.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM cards WHERE (id=? OR handle=?) AND column_key NOT IN ('done','cancelled') AND COALESCE(archived_at,'')='' AND COALESCE(trashed_at,'')=''`, key, key).Scan(&live); err != nil {
			return 0, err
		}
		if live > 0 {
			open++
		}
	}
	return open, nil
}
