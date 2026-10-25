package primitives

import (
	"agent-nexus-core/internal/askpresentation"
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"time"

	"agent-nexus-core/internal/plans"
)

// BriefSectionLimit caps every brief.v1 section.
const BriefSectionLimit = 50

const (
	briefAskCandidates = 60
	// Overfetch a fixed window before ranking effective (reader-visible) blockers.
	briefUndatedAskCandidates = 4 * briefAskCandidates
	briefDoneLimit            = 10
	briefAnsweredWindow       = 7 * 24 * time.Hour
)

// BriefReader is the principal a brief is computed for.
type BriefReader struct {
	ActorID string
	Kind    string // human | agent | service
	Display string
	// Visible applies the PM-private thread rule on top of the scoped SQL
	// relations; nil admits every row the scoped SQL returned.
	Visible func(threadID, privateOwner string) bool
}

func (r BriefReader) human() bool { return r.Kind == "" || r.Kind == "human" }

func stripActor(id string) string { return strings.TrimPrefix(strings.TrimSpace(id), "actor:") }

// Brief computes brief.v1 for one reader over the materialized facts. Every
// statement is an indexed seek with a LIMIT; joins against cards and events
// go through the reader's scoped relations, so withheld rows never appear and
// counts never reveal them. Durations are computed against now.
func (s *Store) Brief(ctx context.Context, reader BriefReader, now time.Time) (map[string]any, error) {
	now = now.UTC()
	b := &briefBuild{s: s, ctx: ctx, reader: reader, now: now, actors: map[string]bool{}}
	decisions, decisionsTruncated, err := b.decisions()
	if err != nil {
		return nil, err
	}
	yourAsks, yourTruncated, err := b.yourAsks()
	if err != nil {
		return nil, err
	}
	answered, answeredTruncated, err := b.answered()
	if err != nil {
		return nil, err
	}
	budget, suggestions, err := b.groupingBudget()
	if err != nil {
		return nil, err
	}
	initiatives, initiativesTruncated, err := b.initiatives()
	if err != nil {
		return nil, err
	}
	// The section may admit older roots than the recent budget cohort. Its
	// visible open rows are also a proven lower bound; never show less than them.
	visibleOpen := 0
	for _, item := range initiatives {
		if item["state"] != "done" {
			visibleOpen++
		}
	}
	budget["count"] = max(budget["count"].(int), visibleOpen)
	loops, loopsTruncated, err := b.initiativeSection(true)
	if err != nil {
		return nil, err
	}
	digest, err := s.TrackingDigestPreview(ctx, reader.ActorID)
	if err != nil {
		return nil, err
	}
	if err = b.resolveActors(); err != nil {
		return nil, err
	}
	b.fillPrincipals(decisions, "requested_by")
	b.fillPrincipals(yourAsks, "requested_by")
	b.fillPrincipals(answered, "answered_by")
	for _, initiative := range append(append([]map[string]any{}, initiatives...), loops...) {
		if p, ok := initiative["promise"].(map[string]any); ok {
			b.fillPrincipals([]map[string]any{p}, "author")
		}
		if n, ok := initiative["note"].(map[string]any); ok {
			b.fillPrincipals([]map[string]any{n}, "author")
		}
	}
	curationSuggestions := []map[string]any{}
	curationTruncated := false
	if !reader.human() {
		curationSuggestions, curationTruncated, err = s.CurationSuggestions(ctx, reader.Visible)
		if err != nil {
			return nil, err
		}
	}
	return map[string]any{
		"schema":               "brief.v1",
		"curation_suggestions": curationSuggestions,
		"generated_at":         now.Format(time.RFC3339),
		"principal":            map[string]any{"id": reader.ActorID, "display": firstNonEmptyString(reader.Display, reader.ActorID), "kind": firstNonEmptyString(reader.Kind, "human")},
		"decisions":            nonNilMaps(decisions),
		"initiatives":          nonNilMaps(initiatives),
		"daily_digest":         digest,
		"loops":                nonNilMaps(loops),
		"initiative_budget":    budget,
		"next_actions":         suggestions,
		"your_asks":            nonNilMaps(yourAsks),
		"answered_for_you":     nonNilMaps(answered),
		"truncated":            map[string]bool{"curation_suggestions": curationTruncated, "decisions": decisionsTruncated, "initiatives": initiativesTruncated, "loops": loopsTruncated, "your_asks": yourTruncated, "answered_for_you": answeredTruncated},
	}, nil
}

func nonNilMaps(v []map[string]any) []map[string]any {
	if v == nil {
		return []map[string]any{}
	}
	return v
}

type briefBuild struct {
	s                *Store
	ctx              context.Context
	reader           BriefReader
	now              time.Time
	actors           map[string]bool
	display          map[string]map[string]any
	topLevelIDs      []string
	allCardsAreRoots bool
}

// Legacy in-process brief callers use Visible for the PM-private rule. Apply
// the equivalent canonical scope to the new grouping projections only; retain
// their existing explicit callback semantics for note/ask evidence reads.
func (b *briefBuild) groupingContext() context.Context {
	if b.reader.Visible != nil {
		if _, scoped := accessScopeFrom(b.ctx); !scoped {
			return WithAccessScope(b.ctx, AccessScope{ActorID: stripActor(b.reader.ActorID)})
		}
	}
	return b.ctx
}

type briefAskRow struct {
	BriefAsk
	thread, pmOwner, payload string
	answeredAt, responder    string
	responseText, outcome    string
}

// briefAnswerableAskJoin is the eligibility inbox respond uses. The inbox
// projection is built per thread and writes no derived_inbox_items row when
// GetThread misses, so respond returns 404. Lifecycle leaves ask, review, and
// escalate items visible, so a present threads row is the shared predicate.
// threads.id is the primary key, so each candidate is one indexed lookup.
const briefAnswerableAskJoin = `JOIN threads brief_ask_thread ON brief_ask_thread.id=e.thread_id`

const briefAskSelect = `SELECT ` + briefAskColumns + `,COALESCE(e.thread_id,''),COALESCE(json_extract(brief_ask_thread.body_json,'$.pm_actor_id'),''),e.payload_json,
	COALESCE(a.answered_at,''),a.responder,COALESCE(json_extract(r.payload_json,'$.payload.response_text'),''),COALESCE(json_extract(r.payload_json,'$.payload.outcome'),'')
	FROM brief_asks a JOIN events e ON e.id=a.ask_id ` + briefAnswerableAskJoin + ` LEFT JOIN events r ON r.id=a.response_event_id AND a.response_event_id<>''`

// pmOwnerSQL keeps a PM-private thread out of a page or a count unless its
// owner is this reader. It is applied before LIMIT so another owner's rows
// cannot fill the window, and so counts cannot disclose those rows. The
// threads lookup is the primary key. A nil Visible admits every scoped row,
// matching the Go filter.
func (b *briefBuild) pmOwnerSQL(threadExpr string) (string, []any) {
	if b.reader.Visible == nil {
		return "1=1", nil
	}
	return `COALESCE((SELECT json_extract(t.body_json,'$.pm_actor_id') FROM threads t WHERE t.id=` + threadExpr + `),'') IN ('', ?)`, []any{stripActor(b.reader.ActorID)}
}

// askThreadOwnerSQL applies the PM-private rule to the thread row
// briefAnswerableAskJoin already required. A missing thread is not public.
func (b *briefBuild) askThreadOwnerSQL() (string, []any) {
	if b.reader.Visible == nil {
		return "1=1", nil
	}
	return `COALESCE(json_extract(brief_ask_thread.body_json,'$.pm_actor_id'),'') IN ('', ?)`, []any{stripActor(b.reader.ActorID)}
}

func (b *briefBuild) queryAsks(where string, args ...any) ([]briefAskRow, error) {
	pred, predArgs := b.askThreadOwnerSQL()
	rows, err := b.s.db.QueryContext(b.ctx, briefAskSelect+` WHERE COALESCE(e.trashed_at,'')='' AND `+pred+` AND `+where, append(predArgs, args...)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []briefAskRow{}
	for rows.Next() {
		var a briefAskRow
		var created, decide, blocks string
		if err = rows.Scan(&a.AskID, &a.InboxItemID, &a.Kind, &a.Requester, &a.Recipient, &a.SubjectRef, &a.Title, &a.Priority, &a.State, &created, &decide, &blocks, &a.BlocksCount,
			&a.thread, &a.pmOwner, &a.payload, &a.answeredAt, &a.responder, &a.responseText, &a.outcome); err != nil {
			return nil, err
		}
		a.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
		if at, e := time.Parse(time.RFC3339Nano, decide); e == nil {
			a.DecideBy = &at
		}
		var refs []any
		_ = json.Unmarshal([]byte(blocks), &refs)
		for _, r := range refs {
			if text, ok := r.(string); ok && strings.TrimSpace(text) != "" && len(a.Blocks) < 20 {
				a.Blocks = append(a.Blocks, strings.TrimSpace(text))
			}
		}
		if b.reader.Visible != nil && !b.reader.Visible(a.thread, a.pmOwner) {
			continue
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (b *briefBuild) readerIDs() (string, string) {
	id := stripActor(b.reader.ActorID)
	return id, "actor:" + id
}

// decisions are open asks addressed to the reader: recipient is the reader,
// or unaddressed asks when the reader is a human. The reader's own asks are
// your_asks, never decisions. An ask whose thread row is missing is omitted:
// the inbox projection never writes it, so respond would 404.
func (b *briefBuild) decisions() ([]map[string]any, bool, error) {
	id, prefixed := b.readerIDs()
	addressed := `(a.recipient IN (?,?)`
	if b.reader.human() {
		addressed += ` OR a.recipient=''`
	}
	addressed += `) AND a.requester NOT IN (?,?)`
	dated, err := b.queryAsks(`a.state='open' AND a.decide_by IS NOT NULL AND `+addressed+` ORDER BY a.decide_by LIMIT ?`, id, prefixed, id, prefixed, briefAskCandidates)
	if err != nil {
		return nil, false, err
	}
	undated, err := b.queryAsks(`a.state='open' AND a.decide_by IS NULL AND `+addressed+` ORDER BY a.blocks_count DESC,`+briefPrioritySQL+`,a.created_at,a.ask_id LIMIT ?`, id, prefixed, id, prefixed, briefUndatedAskCandidates)
	if err != nil {
		return nil, false, err
	}
	items, metas, truncated, err := b.renderAsks(append(dated, undated...), len(dated) == briefAskCandidates || len(undated) == briefUndatedAskCandidates)
	if err != nil {
		return nil, false, err
	}
	items, err = b.collapseBatches(items, metas)
	return items, truncated, err
}

func (b *briefBuild) yourAsks() ([]map[string]any, bool, error) {
	id, prefixed := b.readerIDs()
	rows, err := b.queryAsks(`a.requester IN (?,?) AND a.state='open' ORDER BY a.created_at DESC,a.ask_id LIMIT ?`, id, prefixed, briefAskCandidates)
	if err != nil {
		return nil, false, err
	}
	items, _, truncated, err := b.renderAsks(rows, len(rows) == briefAskCandidates)
	return items, truncated, err
}

// briefDecisionMeta is the per-ask data collapse uses to name the batch
// respond id. It is not part of the brief document.
type briefDecisionMeta struct {
	askID, thread, inboxKind, subject, batchID string
	created                                    time.Time
}

// renderAsks returns the rendered items and, in the same order, the meta
// collapse uses to address a whole batch with one inbox id.
func (b *briefBuild) renderAsks(rows []briefAskRow, candidatesTruncated bool) ([]map[string]any, []briefDecisionMeta, bool, error) {
	rows, err := b.readableProposals(rows)
	if err != nil {
		return nil, nil, false, err
	}
	asks := make([]BriefAsk, len(rows))
	index := map[string]briefAskRow{}
	for i, r := range rows {
		asks[i] = r.BriefAsk
		index[r.AskID] = r
	}
	open, err := b.openBlocks(rows)
	if err != nil {
		return nil, nil, false, err
	}
	for i := range asks {
		asks[i].Blocks = open[asks[i].AskID]
		asks[i].BlocksCount = len(asks[i].Blocks)
	}
	sortBriefAsks(asks)
	truncated := candidatesTruncated || len(asks) > BriefSectionLimit
	if len(asks) > BriefSectionLimit {
		asks = asks[:BriefSectionLimit]
	}
	subjects, err := b.cardTitles(collectSubjects(asks))
	if err != nil {
		return nil, nil, false, err
	}
	var reviewAskIDs []string
	for _, a := range asks {
		if a.Kind == "review" {
			reviewAskIDs = append(reviewAskIDs, a.AskID)
		}
	}
	accessRequests, err := b.s.AccessRequestsForEvents(b.ctx, reviewAskIDs)
	if err != nil {
		return nil, nil, false, err
	}
	out := make([]map[string]any, 0, len(asks))
	metas := make([]briefDecisionMeta, 0, len(asks))
	for _, a := range asks {
		r := index[a.AskID]
		var event map[string]any
		_ = json.Unmarshal([]byte(r.payload), &event)
		payload, _ := event["payload"].(map[string]any)
		_, isAccess := accessRequests["event:"+a.AskID]
		kind := briefDecisionKind(a.Kind, payload, isAccess)
		item := map[string]any{
			"ref": a.InboxItemID, "inbox_item_id": a.InboxItemID, "kind": kind, "title": a.Title,
			"requested_by": stripActor(a.Requester), "created_at": a.CreatedAt.UTC().Format(time.RFC3339),
			"age_seconds": int64(b.now.Sub(a.CreatedAt) / time.Second), "decide_by": nil, "overdue_for_seconds": nil,
			"blocks": a.blocksJSON(subjects), "blocks_count": a.BlocksCount, "proposals": briefProposals(payload),
		}
		askpresentation.Copy(item, payload)
		if a.AskID != "" {
			item["event_ref"] = "event:" + a.AskID
		}
		b.actors[stripActor(a.Requester)] = true
		if body := strings.TrimSpace(anyStringValue(payload["body"])); body != "" {
			item["body"] = body
		}
		if a.Priority != "" {
			item["priority"] = a.Priority
		}
		if a.DecideBy != nil {
			item["decide_by"] = a.DecideBy.UTC().Format(time.RFC3339)
			overdue := int64(0)
			if b.now.After(*a.DecideBy) {
				overdue = int64(b.now.Sub(*a.DecideBy) / time.Second)
			}
			item["overdue_for_seconds"] = overdue
		}
		if a.SubjectRef != "" {
			subject := map[string]any{"ref": a.SubjectRef}
			if title, ok := subjects[a.SubjectRef]; ok {
				subject["title"] = title
				item["subject"] = subject
			} else if !strings.HasPrefix(a.SubjectRef, "card:") {
				item["subject"] = subject
			}
		}
		if curation, ok := payload["curation"].(map[string]any); ok {
			item["curation"] = curation
		}
		out = append(out, item)
		metas = append(metas, briefDecisionMeta{
			askID: a.AskID, thread: r.thread, inboxKind: briefInboxKind(a.Kind),
			subject: a.SubjectRef, batchID: strings.TrimSpace(anyStringValue(payload["batch_id"])),
			created: briefEventTime(event, a.CreatedAt),
		})
	}
	return out, metas, truncated, nil
}

// briefEventTime is the ask event's ts, which is what the inbox projection
// sorts on when it pins a filed batch's anchor. brief_asks.created_at is the
// same instant when the trigger stored it; the event ts wins when both parse.
func briefEventTime(event map[string]any, fallback time.Time) time.Time {
	if parsed, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(anyStringValue(event["ts"]))); err == nil {
		return parsed
	}
	return fallback
}

func briefInboxKind(kind string) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "ask", "review", "escalate":
		return strings.ToLower(strings.TrimSpace(kind))
	default:
		return ""
	}
}

func curationTargets(payload string) ([]string, bool) {
	var event struct {
		Payload struct {
			Curation *struct {
				Targets []string `json:"targets"`
			} `json:"curation"`
		} `json:"payload"`
	}
	if json.Unmarshal([]byte(payload), &event) != nil || event.Payload.Curation == nil {
		return nil, false
	}
	return event.Payload.Curation.Targets, true
}

// readableProposals drops curation proposals naming any target the reader
// cannot read: cards resolve through the scoped cards relation and asks
// through the scoped events relation, both under the PM-private thread rule.
// A target of any other shape, or beyond the lookup bound, fails closed.
func (b *briefBuild) readableProposals(rows []briefAskRow) ([]briefAskRow, error) {
	cards, events := []string{}, []string{}
	for _, r := range rows {
		targets, ok := curationTargets(r.payload)
		if !ok {
			continue
		}
		for _, t := range targets {
			switch {
			case strings.HasPrefix(t, "card:"):
				cards = append(cards, t)
			case strings.HasPrefix(t, "event:") && len(events) < 1000:
				events = append(events, strings.TrimPrefix(t, "event:"))
			}
		}
	}
	if len(cards) == 0 && len(events) == 0 {
		return rows, nil
	}
	readable, err := b.cardTitles(cards)
	if err != nil {
		return nil, err
	}
	if len(events) > 0 {
		encoded, _ := json.Marshal(events)
		q, err := b.s.db.QueryContext(b.ctx, `SELECT e.id,COALESCE(e.thread_id,''),COALESCE((SELECT json_extract(t.body_json,'$.pm_actor_id') FROM threads t WHERE t.id=e.thread_id),'') FROM events e WHERE e.id IN (SELECT value FROM json_each(?)) AND COALESCE(e.trashed_at,'')=''`, string(encoded))
		if err != nil {
			return nil, err
		}
		defer q.Close()
		for q.Next() {
			var id, thread, owner string
			if err = q.Scan(&id, &thread, &owner); err != nil {
				return nil, err
			}
			if b.reader.Visible == nil || b.reader.Visible(thread, owner) {
				readable["event:"+id] = id
			}
		}
		if err = q.Err(); err != nil {
			return nil, err
		}
	}
	kept := rows[:0:0]
	for _, r := range rows {
		targets, ok := curationTargets(r.payload)
		visible := true
		for _, t := range targets {
			if _, seen := readable[t]; !seen {
				visible = false
				break
			}
		}
		if !ok || visible {
			kept = append(kept, r)
		}
	}
	return kept, nil
}

// collapseBatches folds the open members of a batch into the first (most
// urgent) member's decision. A batch is either asks filed together through
// POST /asks/batch (their pinned batch_id) or an accepted curation batch
// proposal. The member list only includes asks already on this page, so a
// batch never reveals or counts a member the reader cannot see.
//
// The kept decision's inbox_item_id is the id that answers every member.
// A filed batch uses the inbox projection's id: the oldest member of the
// full batch, withdrawn or off this page. A curation batch uses
// inbox:curation-batch:<key>.
func (b *briefBuild) collapseBatches(items []map[string]any, metas []briefDecisionMeta) ([]map[string]any, error) {
	if len(metas) < 2 || len(metas) != len(items) {
		return items, nil
	}
	ids := make([]string, len(metas))
	keys := map[string]string{}
	for i, meta := range metas {
		ids[i] = meta.askID
		if meta.batchID != "" {
			keys[meta.askID] = "filed:" + meta.batchID
		}
	}
	encoded, _ := json.Marshal(ids)
	rows, err := b.s.db.QueryContext(curationInternal(b.ctx), `SELECT ask_id,batch_key FROM curation_batch_members WHERE ask_id IN (SELECT value FROM json_each(?))`, string(encoded))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, key string
		if err = rows.Scan(&id, &key); err != nil {
			return nil, err
		}
		if keys[id] == "" {
			keys[id] = key
		}
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if len(keys) == 0 {
		return items, nil
	}
	groups := map[string][]int{}
	for i, id := range ids {
		if key := keys[id]; key != "" {
			groups[key] = append(groups[key], i)
		}
	}
	anchors, err := b.filedBatchAnchors(groups)
	if err != nil {
		return nil, err
	}
	out := make([]map[string]any, 0, len(items))
	for i, id := range ids {
		key := keys[id]
		group := groups[key]
		if key == "" || len(group) < 2 {
			out = append(out, items[i])
			continue
		}
		if group[0] != i {
			continue
		}
		list := make([]map[string]any, 0, len(group))
		for _, mi := range group {
			list = append(list, briefBatchMember(items[mi], metas[mi]))
		}
		items[i]["batch"] = map[string]any{"key": key, "count": len(group), "members": list}
		if respondID := batchRespondInboxID(key, group, metas, anchors); respondID != "" {
			items[i]["ref"] = respondID
			items[i]["inbox_item_id"] = respondID
		}
		out = append(out, items[i])
	}
	return out, nil
}

// filedBatchAnchors reads the oldest member of each filed batch that this
// page collapses, skipping a member whose thread row is missing. The inbox
// projection is per thread and writes nothing when GetThread misses, so that
// member is not an answerable anchor. One indexed seek per batch
// (idx_brief_asks_batch, LIMIT 1), at most one per decision on the page,
// then the event and thread primary keys. No INDEXED BY on threads: a scoped
// read shadows that relation. The row is the anchor even when it is
// withdrawn or outside the page; the query returns only its id and kind.
func (b *briefBuild) filedBatchAnchors(groups map[string][]int) (map[string]briefDecisionMeta, error) {
	out := map[string]briefDecisionMeta{}
	for key, group := range groups {
		batchID := strings.TrimPrefix(key, "filed:")
		if batchID == key || batchID == "" || len(group) < 2 {
			continue
		}
		var askID, kind, created string
		err := b.s.db.QueryRowContext(b.ctx, `SELECT a.ask_id, a.kind, a.created_at FROM brief_asks a INDEXED BY idx_brief_asks_batch JOIN events e ON e.id=a.ask_id `+briefAnswerableAskJoin+` WHERE a.batch_id=? AND a.batch_id<>'' ORDER BY a.created_at, a.ask_id LIMIT 1`, batchID).Scan(&askID, &kind, &created)
		if err == sql.ErrNoRows {
			continue
		}
		if err != nil {
			return nil, err
		}
		at, _ := time.Parse(time.RFC3339Nano, created)
		out[batchID] = briefDecisionMeta{
			askID: askID, inboxKind: briefInboxKind(kind), batchID: batchID, created: at,
		}
	}
	return out, nil
}

func briefBatchMember(item map[string]any, meta briefDecisionMeta) map[string]any {
	member := map[string]any{"ref": item["ref"], "inbox_item_id": item["inbox_item_id"], "title": item["title"]}
	askpresentation.Copy(member, item)
	if eventRef := strings.TrimSpace(anyStringValue(item["event_ref"])); eventRef != "" {
		member["event_ref"] = eventRef
	} else if meta.askID != "" {
		member["event_ref"] = "event:" + meta.askID
	}
	if meta.subject != "" {
		member["subject_ref"] = meta.subject
	}
	if blocks, ok := item["blocks"].([]map[string]any); ok && len(blocks) > 0 {
		copied := make([]map[string]any, 0, len(blocks))
		for _, block := range blocks {
			ref := strings.TrimSpace(anyStringValue(block["ref"]))
			if ref == "" {
				continue
			}
			entry := map[string]any{"ref": ref}
			if title := strings.TrimSpace(anyStringValue(block["title"])); title != "" {
				entry["title"] = title
			}
			copied = append(copied, entry)
		}
		if len(copied) > 0 {
			member["blocks"] = copied
		}
	}
	return member
}

// batchRespondInboxID is the single inbox id that answers the group. A filed
// batch uses the same id the inbox projection derives. anchors holds that
// oldest full-batch member; without one, the oldest open member on the page
// is the fallback. A curation batch uses a stable id the respond handler fans out.
func batchRespondInboxID(key string, group []int, metas []briefDecisionMeta, anchors map[string]briefDecisionMeta) string {
	if strings.HasPrefix(key, "filed:") {
		batchID := strings.TrimPrefix(key, "filed:")
		page := metas[group[0]]
		anchor, ok := anchors[batchID]
		if !ok {
			anchor = page
			for _, mi := range group[1:] {
				if FiledBatchAnchorBefore(metas[mi].created, true, metas[mi].askID, anchor.created, true, anchor.askID) {
					anchor = metas[mi]
				}
			}
		}
		kind := anchor.inboxKind
		if kind == "" {
			kind = page.inboxKind
		}
		// Filed batches share one thread and kind. The page member supplies
		// the thread; the anchor supplies the event id the projection pins.
		if kind == "" || page.thread == "" || anchor.askID == "" || batchID == "" {
			return ""
		}
		return HumanAttentionInboxItemID(kind, page.thread, batchID, anchor.askID)
	}
	if strings.TrimSpace(key) == "" {
		return ""
	}
	return CurationBatchInboxItemID(key)
}

func (a BriefAsk) blocksJSON(titles map[string]string) []map[string]any {
	out := make([]map[string]any, 0, len(a.Blocks))
	for _, ref := range a.Blocks {
		item := map[string]any{"ref": ref}
		if title, ok := titles[ref]; ok {
			item["title"] = title
		}
		out = append(out, item)
	}
	return out
}

func collectSubjects(asks []BriefAsk) []string {
	refs := []string{}
	for _, a := range asks {
		if a.SubjectRef != "" {
			refs = append(refs, a.SubjectRef)
		}
		refs = append(refs, a.Blocks...)
	}
	return refs
}

func briefDecisionKind(kind string, payload map[string]any, isAccessRequest bool) string {
	if _, ok := payload["curation"].(map[string]any); ok {
		return "pm_proposal"
	}
	if isAccessRequest {
		return "access"
	}
	switch kind {
	case "review", "report_review":
		return "review"
	default:
		return "ask"
	}
}

func briefProposals(payload map[string]any) []string {
	out := []string{}
	raw, _ := payload["response_proposals"].([]any)
	for _, p := range raw {
		if text := strings.TrimSpace(anyStringValue(p)); text != "" && len(out) < 6 {
			out = append(out, text)
		}
	}
	return out
}

// openBlocks drops blocks refs that reached a terminal state or that the
// reader cannot see (card refs only resolve through the scoped cards
// relation). Non-card refs pass through: they are already in the ask the
// reader can read.
func (b *briefBuild) openBlocks(rows []briefAskRow) (map[string][]string, error) {
	cardRefs := []string{}
	for _, r := range rows {
		for _, ref := range r.Blocks {
			if strings.HasPrefix(ref, "card:") {
				cardRefs = append(cardRefs, ref)
			}
		}
	}
	state, err := b.cardStates(cardRefs)
	if err != nil {
		return nil, err
	}
	out := map[string][]string{}
	for _, r := range rows {
		kept := []string{}
		for _, ref := range r.Blocks {
			if strings.HasPrefix(ref, "card:") {
				phase, ok := state[ref]
				if !ok || plans.Status(phase) == "done" || phase == "cancelled" {
					continue
				}
			}
			kept = append(kept, ref)
		}
		out[r.AskID] = kept
	}
	return out, nil
}

// cardStates resolves at most 1000 card refs (id or handle) to their phase
// through the scoped cards relation; hidden, archived and trashed cards are
// absent.
func (b *briefBuild) cardStates(refs []string) (map[string]string, error) {
	out := map[string]string{}
	keys := briefCardKeys(refs)
	if len(keys) == 0 {
		return out, nil
	}
	encoded, _ := json.Marshal(keys)
	rows, err := b.s.db.QueryContext(b.ctx, `SELECT c.id,COALESCE(c.handle,''),c.column_key FROM cards c WHERE c.id IN (SELECT value FROM json_each(?)) AND COALESCE(c.archived_at,'')='' AND COALESCE(c.trashed_at,'')=''
		UNION SELECT c.id,COALESCE(c.handle,''),c.column_key FROM cards c INDEXED BY idx_cards_handle_unique WHERE c.handle IS NOT NULL AND trim(c.handle)<>'' AND c.handle IN (SELECT value FROM json_each(?)) AND COALESCE(c.archived_at,'')='' AND COALESCE(c.trashed_at,'')=''`, string(encoded), string(encoded))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, handle, phase string
		if err = rows.Scan(&id, &handle, &phase); err != nil {
			return nil, err
		}
		out["card:"+id] = phase
		if handle != "" {
			out["card:"+handle] = phase
		}
	}
	return out, rows.Err()
}

func briefCardKeys(refs []string) []string {
	seen := map[string]bool{}
	keys := []string{}
	for _, ref := range refs {
		if key := strings.TrimPrefix(ref, "card:"); key != ref && key != "" && !seen[key] && len(keys) < 1000 {
			seen[key] = true
			keys = append(keys, key)
		}
	}
	return keys
}

// cardTitles resolves visible card titles for at most 1000 refs.
func (b *briefBuild) cardTitles(refs []string) (map[string]string, error) {
	out := map[string]string{}
	keys := briefCardKeys(refs)
	if len(keys) == 0 {
		return out, nil
	}
	encoded, _ := json.Marshal(keys)
	rows, err := b.s.db.QueryContext(b.ctx, `SELECT c.id,COALESCE(c.handle,''),c.title,COALESCE(c.thread_id,''),COALESCE((SELECT json_extract(t.body_json,'$.pm_actor_id') FROM threads t WHERE t.id=c.thread_id),'') FROM cards c WHERE c.id IN (SELECT value FROM json_each(?)) AND COALESCE(c.trashed_at,'')=''
		UNION SELECT c.id,COALESCE(c.handle,''),c.title,COALESCE(c.thread_id,''),COALESCE((SELECT json_extract(t.body_json,'$.pm_actor_id') FROM threads t WHERE t.id=c.thread_id),'') FROM cards c INDEXED BY idx_cards_handle_unique WHERE c.handle IS NOT NULL AND trim(c.handle)<>'' AND c.handle IN (SELECT value FROM json_each(?)) AND COALESCE(c.trashed_at,'')=''`, string(encoded), string(encoded))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, handle, title, thread, owner string
		if err = rows.Scan(&id, &handle, &title, &thread, &owner); err != nil {
			return nil, err
		}
		if b.reader.Visible != nil && !b.reader.Visible(thread, owner) {
			continue
		}
		out["card:"+id] = title
		if handle != "" {
			out["card:"+handle] = title
		}
	}
	return out, rows.Err()
}

func (b *briefBuild) answered() ([]map[string]any, bool, error) {
	id, prefixed := b.readerIDs()
	since := b.now.Add(-briefAnsweredWindow).Format(time.RFC3339)
	rows, err := b.queryAsks(`a.requester IN (?,?) AND a.state='answered' AND a.answered_at>=? ORDER BY a.answered_at DESC,a.ask_id LIMIT ?`, id, prefixed, since, BriefSectionLimit+1)
	if err != nil {
		return nil, false, err
	}
	truncated := len(rows) > BriefSectionLimit
	if truncated {
		rows = rows[:BriefSectionLimit]
	}
	out := make([]map[string]any, 0, len(rows))
	for _, r := range rows {
		var event map[string]any
		_ = json.Unmarshal([]byte(r.payload), &event)
		payload, _ := event["payload"].(map[string]any)
		proposals := briefProposals(payload)
		answer := strings.TrimSpace(r.responseText)
		item := map[string]any{
			"ref": r.InboxItemID, "inbox_item_id": r.InboxItemID, "title": r.Title, "answer": answer,
			"answered_at": r.answeredAt, "answered_by": stripActor(r.responder),
			"followed_recommendation": len(proposals) > 0 && answer == proposals[0],
		}
		if at, e := time.Parse(time.RFC3339Nano, r.answeredAt); e == nil {
			item["answered_at"] = at.UTC().Format(time.RFC3339)
		}
		if r.outcome != "" {
			item["outcome"] = r.outcome
		}
		b.actors[stripActor(r.responder)] = true
		out = append(out, item)
	}
	return out, truncated, nil
}

type briefCardRow struct {
	briefInitiativeRow
	handle, title, phase, thread, owner, plan, assignee string
	outcome                                             string
	progressDone, progressTotal                         int
	loopAnnotated                                       int
	loopLastRunAt                                       string
}

func (b *briefBuild) queryInitiatives(loops bool, where string, args ...any) ([]briefCardRow, error) {
	ownerExpr := "''"
	if b.reader.Visible != nil {
		ownerExpr = `COALESCE((SELECT json_extract(t.body_json,'$.pm_actor_id') FROM threads t WHERE t.id=c.thread_id),'')`
	}
	boardFilter := "(NOT EXISTS(SELECT 1 FROM boards WHERE role='initiatives') OR c.board_id IN (SELECT id FROM boards WHERE role='initiatives'))"
	groupFilter := "NOT " + workHasParentSQL("c") + " AND bi.loop_annotated=0 AND (bi.loop_last_run_at IS NULL OR bi.loop_last_run_at<'" + loopTS(b.now.Add(-loopWindow)) + "')"
	prefixArgs := []any{}
	if !loops && len(b.topLevelIDs) > 0 {
		encoded, _ := json.Marshal(b.topLevelIDs)
		groupFilter = "(c.id IN (SELECT value FROM json_each(?)) OR (" + groupFilter + "))"
		prefixArgs = append(prefixArgs, string(encoded))
	}
	if !loops && b.allCardsAreRoots {
		groupFilter = "1=1"
		prefixArgs = nil
	}
	source := ""
	if loops {
		boardFilter = "1=1"
		groupFilter = "(bi.loop_annotated=1 OR bi.loop_last_run_at>='" + loopTS(b.now.Add(-loopWindow)) + "')"
	}
	// Admit through the existing promise/evidence ordering index before the
	// reader-relative parent filter. A workspace full of grouped children must
	// not force an unbounded scan to fill a brief section. /work remains paged.
	admission := strings.TrimSuffix(where, " LIMIT ?")
	if loops {
		// Seek loop candidates before applying the promise category; ordinary
		// initiatives must add no scan work to the separate loop section.
		cutoff := loopTS(b.now.Add(-loopWindow))
		source = `(SELECT card_id FROM (SELECT card_id FROM work_grouping_brief_positions WHERE loop_annotated=1 ORDER BY loop_annotated_at DESC,card_id LIMIT 200)
		 UNION SELECT card_id FROM (SELECT card_id FROM work_grouping_brief_positions WHERE loop_last_run_at>='` + cutoff + `' ORDER BY loop_last_run_at DESC,card_id LIMIT 200)) loop_candidates
		 CROSS JOIN brief_initiatives bi ON bi.card_id=loop_candidates.card_id`
	}

	if source == "" {
		source = `(SELECT card_id FROM work_grouping_brief_positions bi WHERE ` + admission + ` LIMIT 200) admitted
		 CROSS JOIN brief_initiatives bi ON bi.card_id=admitted.card_id`
	}
	if !loops {
		groupFilter += ` AND (bi.open=0 OR (COALESCE(c.assignee,'')<>'' AND NOT EXISTS(SELECT 1 FROM work_tracking tracking WHERE tracking.card_id=c.id AND (tracking.orphaned_at<>'' OR (bi.promise_source='default' AND tracking.reminder_event_id<>''))) AND NOT EXISTS(SELECT 1 FROM card_leases lease WHERE lease.card_id=c.id AND lease.owner=c.assignee AND (lease.released_at<>'' OR lease.expires_at<='` + cardLeaseStamp(b.now) + `'))))`
	}
	loopColumns := "bi.loop_annotated,COALESCE(bi.loop_last_run_at,'')"
	if b.allCardsAreRoots {
		loopColumns = "0,''"
	}
	selectSQL := `SELECT bi.card_id,bi.open,COALESCE(bi.promise_at,''),bi.promise_what,bi.promise_source,bi.promise_source_ref,bi.promise_author,bi.promise_moves,COALESCE(bi.last_evidence_at,''),
	COALESCE(c.handle,''),c.title,c.column_key,COALESCE(c.thread_id,''),` + ownerExpr + `,COALESCE(p.body_json,''),bi.progress_done,bi.progress_total,COALESCE(c.assignee,''),` + loopColumns + `,substr(COALESCE(NULLIF(trim(json_extract(m.metadata_json,'$.goal.label')),''),NULLIF(trim(json_extract(m.metadata_json,'$.goal.metric')),''),c.summary,''),1,281)
 FROM ` + source + ` CROSS JOIN cards c ON c.id=bi.card_id LEFT JOIN card_plans p ON p.card_id=c.id LEFT JOIN work_metadata m ON m.card_id=c.id
	WHERE COALESCE(c.archived_at,'')='' AND COALESCE(c.trashed_at,'')='' AND ` + boardFilter + ` AND ` + groupFilter + ` AND `
	pred, predArgs := b.pmOwnerSQL("c.thread_id")
	queryArgs := append(prefixArgs, predArgs...)
	queryArgs = append(queryArgs, args...)
	rows, err := b.s.db.QueryContext(b.groupingContext(), selectSQL+pred+` AND `+where, queryArgs...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []briefCardRow{}
	for rows.Next() {
		var r briefCardRow
		if err = rows.Scan(&r.cardID, &r.open, &r.promiseAt, &r.what, &r.source, &r.sourceRef, &r.author, &r.moves, &r.lastEvidence, &r.handle, &r.title, &r.phase, &r.thread, &r.owner, &r.plan, &r.progressDone, &r.progressTotal, &r.assignee, &r.loopAnnotated, &r.loopLastRunAt, &r.outcome); err != nil {
			return nil, err
		}
		if b.reader.Visible != nil && !b.reader.Visible(r.thread, r.owner) {
			continue
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// initiatives: promised (past first, then soonest), then no_promise (oldest
// evidence first), then recently done; at most BriefSectionLimit in all.
func (b *briefBuild) initiatives() ([]map[string]any, bool, error) { return b.initiativeSection(false) }
func (b *briefBuild) initiativeSection(loopsOnly bool) ([]map[string]any, bool, error) {
	if loopsOnly && b.allCardsAreRoots {
		return []map[string]any{}, false, nil
	}
	limit := BriefSectionLimit + 1
	promised, err := b.queryInitiatives(loopsOnly, `bi.open=1 AND bi.promise_at IS NOT NULL ORDER BY bi.promise_at LIMIT ?`, limit)
	if err != nil {
		return nil, false, err
	}
	rows := promised
	if len(rows) < limit {
		none, err := b.queryInitiatives(loopsOnly, `bi.open=1 AND bi.promise_at IS NULL ORDER BY bi.last_evidence_at LIMIT ?`, limit-len(rows))
		if err != nil {
			return nil, false, err
		}
		rows = append(rows, none...)
	}
	if len(rows) < limit {
		done, err := b.queryInitiatives(loopsOnly, `bi.open=0 ORDER BY bi.last_evidence_at DESC LIMIT ?`, min(briefDoneLimit, limit-len(rows)))
		if err != nil {
			return nil, false, err
		}
		rows = append(rows, done...)
	}
	truncated := len(rows) > BriefSectionLimit
	if truncated {
		rows = rows[:BriefSectionLimit]
	}
	ids := make([]string, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.cardID)
	}
	counts, err := b.initiativeAskCounts(rows)
	if err != nil {
		return nil, false, err
	}
	notes, noteText, err := b.latestNotes(ids)
	if err != nil {
		return nil, false, err
	}
	moot, err := b.possiblyMoot(rows, noteText)
	if err != nil {
		return nil, false, err
	}
	participantIDs := ids
	assigned := map[string]bool{}
	if !b.reader.human() && stripActor(b.reader.ActorID) != "" {
		participantIDs = []string{}
		for _, r := range rows {
			if stripActor(r.assignee) == stripActor(b.reader.ActorID) {
				assigned[r.cardID] = true
			} else {
				participantIDs = append(participantIDs, r.cardID)
			}
		}
	}
	agentCards, err := b.participatingInitiativeCards(participantIDs)
	if err != nil {
		return nil, false, err
	}
	for id := range assigned {
		agentCards[id] = true
	}
	readerID, _ := b.readerIDs()
	out := make([]map[string]any, 0, len(rows))
	for _, r := range rows {
		ref := "card:" + firstNonEmptyString(r.handle, r.cardID)
		state := r.fold().At(b.now)
		item := map[string]any{
			"id": r.cardID, "ref": ref, "title": r.title, "owner": nil, "state": state.State, "placement": briefPlacement(r.phase),
			"promise": nil, "past_promise_for_seconds": nil, "promise_moves": state.Moves, "last_evidence_at": nil,
			"progress": briefProgress(r.progressDone, r.progressTotal), "blocked_count": counts[r.cardID].blocked, "waiting_on_you_count": counts[r.cardID].waiting,
			"outcome": briefOutcome(r.outcome), "proposed": briefPlacement(r.phase) == "backlog",
			"note": nil, "possibly_moot": possiblyMootJSON(moot[r.cardID]),
		}
		if refs := boardCardAssigneeRefs(r.assignee); len(refs) > 0 {
			item["owner"] = refs[0]
		}
		if state.Promise != nil {
			p := map[string]any{"at": state.Promise.At.UTC().Format(time.RFC3339Nano), "what": state.Promise.What, "source": state.Promise.Source}
			if state.Promise.SourceRef != "" {
				p["source_ref"] = state.Promise.SourceRef
			}
			if state.Promise.Author != "" {
				p["author"] = stripActor(state.Promise.Author)
				b.actors[stripActor(state.Promise.Author)] = true
			}
			item["promise"] = p
		}
		if !b.reader.human() && readerID != "" {
			if agentCards[r.cardID] || (state.Promise != nil && stripActor(state.Promise.Author) == readerID) {
				item["yours"] = true
			}
		}
		if state.PastPromiseFor != nil {
			item["past_promise_for_seconds"] = int64(*state.PastPromiseFor / time.Second)
		}
		if !state.LastEvidenceAt.IsZero() {
			item["last_evidence_at"] = state.LastEvidenceAt.UTC().Format(time.RFC3339)
		}
		if n, ok := notes[r.cardID]; ok {
			item["note"] = n
			b.actors[anyStringValue(n["author"])] = true
		}
		out = append(out, item)
	}
	loopIDs := make([]string, 0, len(rows))
	loopCutoff := loopTS(b.now.Add(-loopWindow))
	for _, r := range rows {
		if r.loopAnnotated != 0 || r.loopLastRunAt >= loopCutoff {
			loopIDs = append(loopIDs, r.cardID)
		}
	}
	loops, _, loopErr := b.s.loadBriefLoopStates(b.ctx, loopIDs, b.now)
	if loopErr != nil {
		return nil, false, loopErr
	}
	for i, id := range ids {
		out[i]["loop"] = loops[id]
		if h, ok := cadenceTrackingHealth(loops[id], rows[i].phase, workString(out[i]["ref"]), b.now); ok {
			out[i]["state"] = h.State
			out[i]["promise"] = nil
			if h.Promise != nil {
				out[i]["promise"] = map[string]any{"at": h.Promise.At, "what": h.Promise.What, "source": h.Promise.Source, "source_ref": h.Promise.SourceRef}
			}
			out[i]["past_promise_for_seconds"] = h.PastPromiseForSeconds
			out[i]["promise_moves"] = 0
			out[i]["last_evidence_at"] = h.LastEvidenceAt
		}
	}
	if err := b.initiativeSeries(ids, out); err != nil {
		return nil, false, err
	}
	hierarchy := []map[string]any{}
	for i, r := range rows {
		out[i]["parent_ref"] = nil
		out[i]["rollup"] = emptyWorkRollup()
		var p plans.Plan
		_ = json.Unmarshal([]byte(r.plan), &p)
		linked := false
		for _, step := range p.Steps {
			if strings.HasPrefix(step.Ref, "card:") {
				linked = true
				break
			}
		}
		if loopsOnly || linked {
			hierarchy = append(hierarchy, out[i])
		}
	}
	if err = b.s.EnrichWorkHierarchy(b.groupingContext(), hierarchy, b.reader.ActorID, b.reader.human(), b.now); err != nil {
		return nil, false, err
	}
	for _, item := range out {
		delete(item, "id")
	}
	return out, truncated, nil
}

// Assignment was already read with the admitted cards. The remaining IDs need
// only the indexed participant/session probe, never a second card hydration.
func (b *briefBuild) participatingInitiativeCards(ids []string) (map[string]bool, error) {
	out := map[string]bool{}
	if b.reader.human() || strings.TrimSpace(b.reader.ActorID) == "" || len(ids) == 0 {
		return out, nil
	}
	seen := make(map[string]bool, len(ids))
	candidates := make([]string, 0, min(len(ids), BriefSectionLimit))
	for _, raw := range ids {
		cardID := strings.TrimSpace(raw)
		if cardID == "" || seen[cardID] {
			continue
		}
		seen[cardID] = true
		candidates = append(candidates, cardID)
		if len(candidates) >= BriefSectionLimit {
			break
		}
	}
	if len(candidates) == 0 {
		return out, nil
	}
	id, prefixed := b.readerIDs()
	encoded, _ := json.Marshal(candidates)
	rows, err := b.s.db.QueryContext(b.ctx, `SELECT j.value FROM json_each(?) j
		WHERE EXISTS(SELECT 1 FROM work_participants p WHERE p.actor_id=? AND p.actor_id<>'' AND p.card_id=j.value AND p.activity<>'left' AND p.session_active=1
			AND EXISTS(SELECT 1 FROM agent_sessions s WHERE s.id=p.session_id AND s.activity<>'closed' AND +s.actor_id IN (?,?)))`,
		string(encoded), id, id, prefixed)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var cardID string
		if err = rows.Scan(&cardID); err != nil {
			return nil, err
		}
		out[cardID] = true
	}
	return out, rows.Err()
}

func briefPlacement(phase string) string {
	switch phase {
	case "backlog", "ready", "in_progress", "review", "done":
		return phase
	case "in_review":
		return "review"
	case "cancelled":
		return "done"
	case "blocked":
		return "in_progress"
	}
	if plans.Status(phase) == "done" {
		return "done"
	}
	return "backlog"
}

func briefProgress(done, total int) any {
	if total == 0 {
		return nil
	}
	return map[string]any{"done": done, "total": total}
}

type briefAskCounts struct{ blocked, waiting int }

// initiativeAskCounts counts open asks about each card (subject) and open asks
// whose blocks name it. Subject lookups use idx_brief_asks_subject; blocks
// seek at most two distinct refs per admitted card. Both counts use
// briefAnswerableAskJoin, the same eligibility as the decision list.
func (b *briefBuild) initiativeAskCounts(rows []briefCardRow) (map[string]briefAskCounts, error) {
	out := map[string]briefAskCounts{}
	if len(rows) == 0 {
		return out, nil
	}
	byRef := map[string]string{}
	refs := []string{}
	for _, r := range rows {
		for _, ref := range []string{"card:" + r.cardID, "card:" + r.handle} {
			if _, exists := byRef[ref]; ref != "card:" && !exists {
				byRef[ref] = r.cardID
				refs = append(refs, ref)
			}
		}
	}
	id, prefixed := b.readerIDs()
	encoded, _ := json.Marshal(refs)
	human := 0
	if b.reader.human() {
		human = 1
	}
	pred, predArgs := b.askThreadOwnerSQL()
	rows2, err := b.s.db.QueryContext(b.ctx, `SELECT a.subject_ref,a.recipient,a.requester FROM json_each(?) requested CROSS JOIN brief_asks a INDEXED BY idx_brief_asks_subject ON a.subject_ref=requested.value JOIN events e ON e.id=a.ask_id `+briefAnswerableAskJoin+`
		WHERE a.state='open' AND COALESCE(e.trashed_at,'')='' AND `+pred+` LIMIT 1000`, append([]any{string(encoded)}, predArgs...)...)
	if err != nil {
		return nil, err
	}
	for rows2.Next() {
		var subject, recipient, requester string
		if err = rows2.Scan(&subject, &recipient, &requester); err != nil {
			rows2.Close()
			return nil, err
		}
		c := out[byRef[subject]]
		c.blocked++
		if (recipient == id || recipient == prefixed || (recipient == "" && human == 1)) && stripActor(requester) != id {
			c.waiting++
		}
		out[byRef[subject]] = c
	}
	rows2.Close()
	if err = rows2.Err(); err != nil {
		return nil, err
	}
	rows3, err := b.s.db.QueryContext(b.ctx, briefAskBlockCountsSelect+pred+` GROUP BY k.ref`, append([]any{string(encoded)}, predArgs...)...)
	if err != nil {
		return nil, err
	}
	defer rows3.Close()
	for rows3.Next() {
		var ref string
		var n int
		if err = rows3.Scan(&ref, &n); err != nil {
			return nil, err
		}
		c := out[byRef[ref]]
		c.blocked += n
		out[byRef[ref]] = c
	}
	if err = rows3.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// Bound the block lookup by the already-admitted card refs. CROSS JOIN keeps
// scoped ask predicates from turning this into a scan of every open ask.
// initiativeAskCounts deduplicates these bounded refs before encoding them.
const briefAskBlockCountsSelect = `SELECT k.ref,COUNT(*) FROM json_each(?) requested
 CROSS JOIN brief_ask_blocks k ON k.ref=requested.value
 CROSS JOIN brief_asks a ON a.ask_id=k.ask_id
 JOIN events e ON e.id=a.ask_id ` + briefAnswerableAskJoin + `
 WHERE a.state='open' AND a.subject_ref<>k.ref AND COALESCE(e.trashed_at,'')='' AND `

// latestNotes reads the newest live note per card in one statement: one
// indexed seek per card id through idx_brief_notes_card_ts, plus that note's
// payload so a cited event ref is visible without a second query. The second
// result is the citation text (body and next step), keyed by card id.
func (b *briefBuild) latestNotes(cardIDs []string) (map[string]map[string]any, map[string]string, error) {
	out := map[string]map[string]any{}
	texts := map[string]string{}
	if len(cardIDs) == 0 {
		return out, texts, nil
	}
	encoded, _ := json.Marshal(cardIDs)
	rows, err := b.s.db.QueryContext(b.ctx, `SELECT n.card_id,n.author,n.ts,n.excerpt,e.payload_json FROM json_each(?) j CROSS JOIN brief_notes n ON n.event_id=(
		SELECT x.event_id FROM brief_notes x JOIN events ev ON ev.id=x.event_id WHERE x.card_id=j.value AND COALESCE(ev.trashed_at,'')='' ORDER BY x.ts DESC LIMIT 1)
		JOIN events e ON e.id=n.event_id`, string(encoded))
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, author, ts, excerpt, payload string
		if err = rows.Scan(&id, &author, &ts, &excerpt, &payload); err != nil {
			return nil, nil, err
		}
		at, _ := time.Parse(time.RFC3339Nano, ts)
		out[id] = map[string]any{"text": excerpt, "author": stripActor(author), "at": at.UTC().Format(time.RFC3339)}
		texts[id] = noteCitationText(payload)
	}
	return out, texts, rows.Err()
}

func possiblyMootJSON(m PossiblyMoot) map[string]any {
	out := map[string]any{"flagged": m.Flagged}
	if m.Reason != "" {
		out["reason"] = m.Reason
	}
	if m.Code != "" {
		out["code"] = m.Code
	}
	if m.AskRef != "" {
		out["ask_ref"] = m.AskRef
	}
	if m.DecidedAt != "" {
		out["decided_at"] = m.DecidedAt
	}
	return out
}

func (b *briefBuild) resolveActors() error {
	b.display = map[string]map[string]any{}
	ids := []string{}
	for id := range b.actors {
		if id != "" && len(ids) < 500 {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	encoded, _ := json.Marshal(ids)
	rows, err := b.s.db.QueryContext(b.ctx, `SELECT a.id,a.display_name,EXISTS(SELECT 1 FROM agents g WHERE g.actor_id=a.id) FROM actors a WHERE a.id IN (SELECT value FROM json_each(?))`, string(encoded))
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id, name string
		var agent bool
		if err = rows.Scan(&id, &name, &agent); err != nil {
			return err
		}
		kind := "human"
		if agent {
			kind = "agent"
		}
		b.display[id] = map[string]any{"id": id, "display": firstNonEmptyString(name, id), "kind": kind}
	}
	return rows.Err()
}

func (b *briefBuild) fillPrincipals(items []map[string]any, field string) {
	for _, item := range items {
		id, ok := item[field].(string)
		if !ok {
			continue
		}
		if p, ok := b.display[id]; ok {
			item[field] = p
		} else {
			item[field] = map[string]any{"id": id, "display": firstNonEmptyString(id, "unknown")}
		}
	}
}

// briefMootRefBudget bounds the linked sources resolved for possibly_moot.
const briefMootRefBudget = 400

// possiblyMoot evaluates the overtaken flag with the same rule as plan reads
// (moot.go). Linked-source terminal state applies to parked cards (backlog
// and ready): plan step refs and related_refs, resolved through the batched,
// reader-scoped ref facts. The latest-note ask rule applies to every open
// phase. One metadata read plus at most two ref-fact batches, and only when
// a parked card is on the page. Ask resolution is a further batched
// primary-key lookup, and only when a latest note cites an event ref.
func (b *briefBuild) possiblyMoot(rows []briefCardRow, noteText map[string]string) (map[string]PossiblyMoot, error) {
	out := map[string]PossiblyMoot{}
	parked := []string{}
	planRefs := map[string][]string{}
	texts := map[string]string{}
	for _, r := range rows {
		if r.open != 1 {
			continue
		}
		if possiblyMootPhases[r.phase] {
			parked = append(parked, r.cardID)
			var p plans.Plan
			if r.plan != "" && json.Unmarshal([]byte(r.plan), &p) == nil {
				planRefs[r.cardID] = planStepRefs(p)
			}
		}
		if nextAskOpenPhase(r.phase) && noteText[r.cardID] != "" {
			texts[r.cardID] = noteText[r.cardID]
		}
	}
	if len(parked) == 0 && len(texts) == 0 {
		return out, nil
	}
	linked := map[string][]string{}
	facts := map[string]plans.Fact{}
	if len(parked) > 0 {
		encoded, _ := json.Marshal(parked)
		rows2, err := b.s.db.QueryContext(b.ctx, `SELECT card_id,COALESCE(json_extract(metadata_json,'$.related_refs'),'[]') FROM json_each(?) requested CROSS JOIN work_metadata WHERE card_id=requested.value`, string(encoded))
		if err != nil {
			return nil, err
		}
		related := map[string][]string{}
		for rows2.Next() {
			var id, raw string
			if err = rows2.Scan(&id, &raw); err != nil {
				rows2.Close()
				return nil, err
			}
			var refs []string
			_ = json.Unmarshal([]byte(raw), &refs)
			related[id] = refs
		}
		rows2.Close()
		if err = rows2.Err(); err != nil {
			return nil, err
		}
		refs, seen := []string{}, map[string]bool{}
		for _, id := range parked {
			linked[id] = possiblyMootLinked(append(append([]string{}, planRefs[id]...), related[id]...))
			for _, ref := range linked[id] {
				if !seen[ref] && len(refs) < briefMootRefBudget {
					seen[ref] = true
					refs = append(refs, ref)
				}
			}
		}
		for start := 0; start < len(refs); start += 200 {
			end := min(start+200, len(refs))
			previews, err := b.s.readRefFacts(b.ctx, refs[start:end], b.reader.Visible)
			if err != nil {
				return nil, err
			}
			for _, row := range previews {
				facts[row.Ref] = plans.Fact{Known: row.Resolvable, Status: row.Phase, MovementAt: row.MovementAt}
			}
		}
	}
	askMoot, err := b.s.nextAskMootFromNotes(b.ctx, texts, b.reader.Visible, b.now)
	if err != nil {
		return nil, err
	}
	for _, id := range parked {
		out[id] = mergeNextAsk(PossiblyMootFromFacts(linked[id], facts), askMoot[id])
	}
	for id, moot := range askMoot {
		if _, seen := out[id]; !seen {
			out[id] = moot
		}
	}
	return out, nil
}

// Brief outcomes are a bounded single line of canonical authored text.
func briefOutcome(text string) string {
	text = strings.TrimSpace(strings.SplitN(text, "\n", 2)[0])
	runes := []rune(text)
	if len(runes) > 280 {
		text = string(runes[:280])
	}
	return text
}
