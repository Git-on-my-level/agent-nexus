package primitives

import (
	"strings"
	"time"
)

const (
	// briefFYIScan bounds the fyi notes read before repeats collapse;
	// briefFYILimit bounds what a reader is shown.
	briefFYIScan   = 40
	briefFYILimit  = 10
	briefFYIWindow = 7 * 24 * time.Hour
)

// fillRewrites attaches the display principal of whoever rewrote an ask's
// presentation, on single decisions and on batch members.
func (b *briefBuild) fillRewrites(items []map[string]any) {
	for _, item := range items {
		if rewrite, ok := item["presentation_rewrite"].(map[string]any); ok {
			if p, ok := b.display[anyStringValue(rewrite["actor_id"])]; ok {
				rewrite["actor"] = p
			}
		}
		if batch, ok := item["batch"].(map[string]any); ok {
			if members, ok := batch["members"].([]map[string]any); ok {
				b.fillRewrites(members)
			}
		}
	}
}

// outcomeSections adds the sections that keep Decide to decisions: what agents
// told the reader without wanting an answer, and which of the reader's own
// asks someone else returned. Both are bounded index seeks.
func (b *briefBuild) outcomeSections(result map[string]any) error {
	fyi, err := b.fyi()
	if err != nil {
		return err
	}
	returned, err := b.returnedToYou()
	if err != nil {
		return err
	}
	openAsks, err := b.openAsks()
	if err != nil {
		return err
	}
	// Every earlier section is already filled, so the actor set can be reused
	// for these two without widening the first lookup.
	b.actors = map[string]bool{}
	for _, item := range fyi {
		b.actors[anyStringValue(item["author"])] = true
	}
	for _, item := range returned {
		b.actors[anyStringValue(item["returned_by"])] = true
	}
	for _, item := range openAsks {
		b.actors[anyStringValue(item["requested_by"])] = true
	}
	if err = b.resolveActors(); err != nil {
		return err
	}
	b.fillPrincipals(fyi, "author")
	b.fillPrincipals(returned, "returned_by")
	b.fillPrincipals(openAsks, "requested_by")
	result["open_asks"] = openAsks
	result["fyi"], result["returned_to_you"] = fyi, returned
	if suggestions, ok := result["curation_suggestions"].([]map[string]any); ok {
		if err = b.markSuggestionOwnership(suggestions); err != nil {
			return err
		}
	}
	upkeep, err := b.upkeep()
	if err != nil {
		return err
	}
	if upkeep != nil {
		result["upkeep"] = upkeep
	}
	return nil
}

// fyi reads the newest things agents told a human in the last week, through
// idx_brief_notes_fyi, joined to the reader's scoped cards and events so a
// withheld card never appears. A repeat of the same headline on the same card
// by the same author collapses into the newest, with a count: a daily digest
// is one row that says how many times it arrived.
func (b *briefBuild) fyi() ([]map[string]any, error) {
	out := []map[string]any{}
	if !b.reader.human() {
		return out, nil
	}
	since := b.now.Add(-briefFYIWindow).Format(time.RFC3339)
	rows, err := b.s.db.QueryContext(b.ctx, `SELECT n.event_id,n.author,n.ts,n.title,n.excerpt,c.id,COALESCE(c.handle,''),c.title,COALESCE(c.thread_id,''),COALESCE((SELECT json_extract(t.body_json,'$.pm_actor_id') FROM threads t WHERE t.id=c.thread_id),'')
 FROM brief_notes n INDEXED BY idx_brief_notes_fyi JOIN events e ON e.id=n.event_id JOIN cards c ON c.id=n.card_id
 WHERE n.kind='fyi' AND n.ts>=? AND COALESCE(e.trashed_at,'')='' AND COALESCE(c.trashed_at,'')='' ORDER BY n.ts DESC LIMIT ?`, since, briefFYIScan)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	seen := map[string]map[string]any{}
	for rows.Next() {
		var event, author, ts, title, excerpt, cardID, handle, cardTitle, thread, private string
		if err = rows.Scan(&event, &author, &ts, &title, &excerpt, &cardID, &handle, &cardTitle, &thread, &private); err != nil {
			return nil, err
		}
		if b.reader.Visible != nil && !b.reader.Visible(thread, private) {
			continue
		}
		key := stripActor(author) + "\x00" + cardID + "\x00" + AskTitlePattern(firstNonEmpty(title, excerpt))
		if prior, ok := seen[key]; ok {
			prior["repeats"] = prior["repeats"].(int) + 1
			continue
		}
		if len(out) >= briefFYILimit {
			continue
		}
		at, _ := time.Parse(time.RFC3339Nano, ts)
		item := map[string]any{"ref": "event:" + event, "title": firstNonEmpty(strings.TrimSpace(title), excerpt), "text": excerpt, "author": stripActor(author), "at": at.UTC().Format(time.RFC3339),
			"subject": map[string]any{"ref": "card:" + firstNonEmpty(handle, cardID), "title": cardTitle}, "repeats": 0}
		seen[key] = item
		out = append(out, item)
	}
	return out, rows.Err()
}

// returnedToYou lists the reader's asks that someone else closed in the last
// week, with the public reason, so an asker learns from orient that its
// question was retired and why. It seeks idx_brief_asks_answered.
func (b *briefBuild) returnedToYou() ([]map[string]any, error) {
	out := []map[string]any{}
	if b.reader.human() {
		return out, nil
	}
	id, prefixed := b.readerIDs()
	since := b.now.Add(-briefAnsweredWindow).Format(time.RFC3339)
	rows, err := b.s.db.QueryContext(b.ctx, `SELECT a.ask_id,a.inbox_item_id,a.title,a.answered_at,a.responder,COALESCE(json_extract(r.payload_json,'$.payload.reason'),''),r.type,r.id
 FROM brief_asks a JOIN events e ON e.id=a.ask_id JOIN events r ON r.id=a.response_event_id
 WHERE a.requester IN (?,?) AND a.state='withdrawn' AND a.answered_at>=? AND a.responder NOT IN (?,?,'') AND COALESCE(e.trashed_at,'')='' ORDER BY a.answered_at DESC,a.ask_id LIMIT ?`, id, prefixed, since, id, prefixed, briefFYILimit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var ask, inbox, title, at, by, reason, kind, event string
		if err = rows.Scan(&ask, &inbox, &title, &at, &by, &reason, &kind, &event); err != nil {
			return nil, err
		}
		item := map[string]any{"ref": inbox, "event_ref": "event:" + ask, "title": title, "returned_by": stripActor(by), "reason": reason}
		if t, e := time.Parse(time.RFC3339Nano, at); e == nil {
			item["returned_at"] = t.UTC().Format(time.RFC3339)
		}
		if kind == AskRetiredEventType {
			item["undo_argv"] = []string{"anx", "ask", "retire", "event:" + ask, "--undo"}
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

// openAsks lists other requesters' open asks an agent reader may see, oldest
// deadline first: what a human is being asked right now. An agent doing
// upkeep reads it to find a stale or duplicated question it can retire; its
// own asks are your_asks. One page through idx_brief_asks_open_page, scoped
// like every other ask read, so a withheld ask never appears.
func (b *briefBuild) openAsks() ([]map[string]any, error) {
	out := []map[string]any{}
	if b.reader.human() {
		return out, nil
	}
	id, prefixed := b.readerIDs()
	rows, err := b.queryAsks(`a.state='open' AND a.kind IN ('ask','review') AND a.requester NOT IN (?,?) AND a.recipient NOT IN (?,?) ORDER BY a.decide_by,a.created_at LIMIT ?`, id, prefixed, id, prefixed, BriefSectionLimit)
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		item := map[string]any{"event_ref": "event:" + r.AskID, "title": r.Title, "requested_by": stripActor(r.Requester), "age_seconds": int64(b.now.Sub(r.CreatedAt) / time.Second)}
		if r.SubjectRef != "" {
			item["subject_ref"] = r.SubjectRef
		}
		out = append(out, item)
	}
	return out, nil
}
