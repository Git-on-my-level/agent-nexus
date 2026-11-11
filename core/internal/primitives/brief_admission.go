package primitives

import (
	"strconv"
	"strings"
)

// briefOpenRootAdmission bounds the open top-level cards one brief section
// reads. Past it the section is far beyond BriefSectionLimit and reports
// truncated anyway.
const briefOpenRootAdmission = 500

// openRootSQL is the predicate of idx_cards_open_roots: open, live,
// top-level cards outside the Unsorted tray.
func openRootSQL(card string) string {
	return card + `.parent_card_id IS NULL AND ` + card + `.needs_home=0 AND ` + card + `.trashed_at IS NULL AND ` + card + `.archived_at IS NULL AND ` + card + `.column_key NOT IN ('done','cancelled')`
}

// briefInitiativeSource admits a brief initiative section's candidates.
//
// Open sections admit from the open top-level cards themselves. They used to
// admit the first 200 rows of every card's promise or evidence order and keep
// the top-level ones afterwards, so in a busy workspace the children filled
// the window and Watch silently listed fewer initiatives than anx tree, with
// the section not marked truncated. The cost is bounded by open initiatives
// (idx_cards_open_roots), not by the workspace. The done section keeps the
// evidence-ordered window: done initiatives fold away and are not counted.
func briefInitiativeSource(where, admission string) string {
	if strings.HasPrefix(where, "bi.open=1 ") {
		return `(SELECT c0.id AS card_id FROM main.cards c0 INDEXED BY idx_cards_open_roots WHERE ` + openRootSQL("c0") + ` LIMIT ` + strconv.Itoa(briefOpenRootAdmission) + `) admitted
		 CROSS JOIN brief_initiatives bi ON bi.card_id=admitted.card_id`
	}
	return `(SELECT card_id FROM work_grouping_brief_positions bi WHERE ` + admission + ` LIMIT 200) admitted
		 CROSS JOIN brief_initiatives bi ON bi.card_id=admitted.card_id`
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
