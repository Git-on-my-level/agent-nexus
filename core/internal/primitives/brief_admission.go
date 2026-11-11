package primitives

import (
	"strconv"
	"strings"
)

// briefOpenRootAdmission bounds the open top-level cards one brief section
// admits: the same 200 rows the old promise window hydrated, so a poll costs
// what it did before. Past it the section reads truncated.
var briefOpenRootAdmission = 200 // var: tests shrink it

// openRootSQL is the predicate of idx_cards_open_roots: open, live,
// top-level cards outside the Unsorted tray.
func openRootSQL(card string) string {
	return card + `.parent_card_id IS NULL AND ` + card + `.needs_home=0 AND ` + card + `.trashed_at IS NULL AND ` + card + `.archived_at IS NULL AND ` + card + `.column_key NOT IN ('done','cancelled')`
}

// initiativeSource admits a brief initiative section's candidates.
//
// Open sections admit from the open top-level cards themselves. They used to
// admit only the first 200 rows of every card's promise or evidence order and
// keep the top-level ones afterwards, so in a busy workspace the children
// filled the window and Watch silently listed fewer initiatives than anx tree,
// with the section not marked truncated. Admission is an index-only read of
// idx_cards_open_roots; the section query then hydrates the admitted rows
// through the reader's scoped relation, as before. When the workspace has more
// open top-level cards than admission reads (openRootsOverflow), the section
// reads truncated and Watch shows "N+ open" instead of under-reporting. That
// bit is workspace-wide, like initiative_budget.truncated: it reveals only that
// the workspace holds over 200 open top-level cards, never which.
//
// The promise-ordered window is still unioned in, best effort as before: a
// card whose stored parent the reader cannot see is that reader's initiative
// (workIsInitiativeSQL), and only this window finds it. Known limitation: past
// 200 earlier-promised cards such a card can be omitted without truncation.
// anx tree lists stored roots only, so it is outside the open count both views
// share; admitting it exactly needs a bounded enumeration of the reader's
// hidden parents, which the access layer does not offer yet.
// The done section keeps the evidence-ordered window alone: done initiatives
// fold away and are not counted.
func (b *briefBuild) initiativeSource(where, admission string) (string, []any) {
	window := `SELECT card_id FROM (SELECT card_id FROM work_grouping_brief_positions bi WHERE ` + admission + ` LIMIT 200)`
	if strings.HasPrefix(where, "bi.open=1 ") {
		return `(SELECT card_id FROM (` + openRootsAdmissionSQL(briefOpenRootAdmission) + `)
		 UNION ` + window + `) admitted
		 CROSS JOIN brief_initiatives bi ON bi.card_id=admitted.card_id`, nil
	}
	return `(` + window + `) admitted
		 CROSS JOIN brief_initiatives bi ON bi.card_id=admitted.card_id`, nil
}

func openRootsAdmissionSQL(limit int) string {
	return `SELECT r.id AS card_id FROM main.cards r INDEXED BY idx_cards_open_roots WHERE ` + openRootSQL("r") + ` LIMIT ` + strconv.Itoa(limit)
}

// openRootsOverflow reports whether admission left open top-level cards
// unread; the section then reads truncated. One bounded index-only scan.
func (b *briefBuild) openRootsOverflow() (bool, error) {
	var n int
	err := b.s.db.QueryRowContext(b.ctx, `SELECT COUNT(*) FROM (`+openRootsAdmissionSQL(briefOpenRootAdmission+1)+`)`).Scan(&n)
	return n > briefOpenRootAdmission, err
}

// initiativeCandidates reads a section's rows in display order: promised
// (past first, then soonest), then no_promise (oldest evidence first), then
// recently done; at most BriefSectionLimit, and whether more exist.
func (b *briefBuild) initiativeCandidates(loopsOnly bool) ([]briefCardRow, bool, error) {
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
	if !loopsOnly && !truncated {
		if truncated, err = b.openRootsOverflow(); err != nil {
			return nil, false, err
		}
	}
	return rows, truncated, nil
}

// briefStoredPlacement is the stored placement when the brief shows a
// different, effective one (an unstarted initiative with active children).
func briefStoredPlacement(effective, stored string) any {
	if briefPlacement(stored) == briefPlacement(effective) {
		return nil
	}
	return briefPlacement(stored)
}

func dropNilField(item map[string]any, key string) {
	if v, ok := item[key]; ok && v == nil {
		delete(item, key)
	}
}
