package primitives

import (
	"strconv"
	"strings"
)

// briefOpenRootAdmission bounds the open top-level cards one brief section
// reads. Past it the section is far beyond BriefSectionLimit and reports
// truncated anyway.
var briefOpenRootAdmission = 500 // var: tests shrink it

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
// with the section not marked truncated. Admission reads the reader's scoped
// cards relation and skips loops and other owners' PM-private threads, so
// nothing this reader cannot list fills the window, and the overflow check
// (openRootsOverflow) counts what this reader could list. The cost is bounded
// by openRootScan rows of idx_cards_open_roots plus per-row access probes.
//
// The promise-ordered window is still unioned in, best effort as before: a
// card whose stored parent the reader cannot see is that reader's initiative
// (workIsInitiativeSQL), and only this window finds it. anx tree lists stored
// roots only, so such cards are outside the open count both views share.
// The done section keeps the evidence-ordered window alone: done initiatives
// fold away and are not counted.
func (b *briefBuild) initiativeSource(where, admission string) (string, []any) {
	window := `SELECT card_id FROM (SELECT card_id FROM work_grouping_brief_positions bi WHERE ` + admission + ` LIMIT 200)`
	if strings.HasPrefix(where, "bi.open=1 ") {
		roots, args := b.openRootsSQL(briefOpenRootAdmission)
		return `(SELECT card_id FROM (` + roots + `)
		 UNION ` + window + `) admitted
		 CROSS JOIN brief_initiatives bi ON bi.card_id=admitted.card_id`, args
	}
	return `(` + window + `) admitted
		 CROSS JOIN brief_initiatives bi ON bi.card_id=admitted.card_id`, nil
}

// openRootsSQL selects up to limit open top-level non-loop cards this reader
// may list. Identities are admitted through idx_cards_open_roots (at most
// openRootScan rows, never a wider index) and hydrated through the scoped
// cards relation, as WorkTree admits roots.
func (b *briefBuild) openRootsSQL(limit int) (string, []any) {
	pred, args := b.pmOwnerSQL("c0.thread_id")
	cutoff := loopTS(b.now.Add(-loopWindow))
	return `SELECT c0.id AS card_id FROM (SELECT r.id FROM main.cards r INDEXED BY idx_cards_open_roots WHERE ` + openRootSQL("r") + ` LIMIT ` + strconv.Itoa(openRootScan()) + `) open_roots
		 CROSS JOIN cards c0 ON c0.id=open_roots.id WHERE ` + openRootSQL("c0") +
		` AND NOT EXISTS(SELECT 1 FROM brief_initiatives loop_bi WHERE loop_bi.card_id=c0.id AND (loop_bi.loop_annotated=1 OR COALESCE(loop_bi.loop_last_run_at,'')>='` + cutoff + `'))` +
		` AND ` + pred + ` LIMIT ` + strconv.Itoa(limit), args
}

// openRootScan bounds the open top-level cards admission reads, listable or
// not; past it the section is truncated for every reader.
func openRootScan() int { return 4 * briefOpenRootAdmission }

// openRootsOverflow reports whether this reader has more listable open
// initiatives than admission reads, or the bounded scan was exhausted; the
// section then reads truncated, and Watch shows "N+ open" rather than
// under-reporting. Hidden cards are counted only in that coarse scan bound,
// so a reader learns at most that the workspace has over openRootScan open
// top-level cards.
func (b *briefBuild) openRootsOverflow() (bool, error) {
	roots, args := b.openRootsSQL(briefOpenRootAdmission + 1)
	var listable, scanned int
	err := b.s.db.QueryRowContext(b.groupingContext(), `SELECT (SELECT COUNT(*) FROM (`+roots+`)),
		(SELECT COUNT(*) FROM (SELECT 1 FROM main.cards r INDEXED BY idx_cards_open_roots WHERE `+openRootSQL("r")+` LIMIT `+strconv.Itoa(openRootScan())+`))`, args...).Scan(&listable, &scanned)
	return listable > briefOpenRootAdmission || scanned >= openRootScan(), err
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
