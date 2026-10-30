package storage

import (
	"context"
	"database/sql"
	"sort"
)

// installWorkTree stores the work hierarchy that every read used to derive.
// parent_card_id is the only parent edge; a card is an initiative exactly when
// it has no parent and is not waiting in the Unsorted tray (needs_home=1).
//
// The backfill freezes what reads showed before the upgrade, using the legacy
// eligibility rule: the parent is the oldest open, live card on an active
// board whose plan links the child, unless the child was a declared
// initiative. Cycles break at their oldest member and chains deeper than three
// levels are re-homed under their level-1 ancestor. Parentless cards become
// initiatives when the old rules made them one (initiatives-role board,
// top_level, a loop, children, or a plan in a workspace with no initiatives
// board); everything else waits in the tray rather than becoming one.
func installWorkTree(ctx context.Context, tx *sql.Tx) error {
	exists, err := sqliteTableExists(ctx, tx, "cards")
	if err != nil || !exists {
		return err
	}
	added := false
	for _, column := range []struct{ name, ddl string }{
		{"parent_card_id", `ALTER TABLE cards ADD COLUMN parent_card_id TEXT`},
		{"needs_home", `ALTER TABLE cards ADD COLUMN needs_home INTEGER NOT NULL DEFAULT 0`},
	} {
		has, err := sqliteTableHasColumn(ctx, tx, "cards", column.name)
		if err != nil {
			return err
		}
		if !has {
			if _, err = tx.ExecContext(ctx, column.ddl); err != nil {
				return err
			}
			added = true
		}
	}
	for _, statement := range []string{
		`CREATE INDEX IF NOT EXISTS idx_cards_parent ON cards(parent_card_id,created_at,id) WHERE parent_card_id IS NOT NULL`,
		`CREATE INDEX IF NOT EXISTS idx_cards_tree_roots ON cards(created_at,id) WHERE parent_card_id IS NULL AND needs_home=0 AND trashed_at IS NULL`,
		`CREATE INDEX IF NOT EXISTS idx_cards_tree_tray ON cards(created_at,id) WHERE needs_home=1 AND trashed_at IS NULL`,
		`DROP VIEW IF EXISTS initiative_card_positions`,
		`CREATE VIEW initiative_card_positions AS SELECT c.id FROM cards c INDEXED BY idx_cards_tree_roots WHERE c.parent_card_id IS NULL AND c.needs_home=0 AND c.trashed_at IS NULL AND c.archived_at IS NULL AND c.column_key NOT IN ('done','cancelled')`,
	} {
		if _, err = tx.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	if !added {
		return nil
	}
	return backfillWorkTree(ctx, tx)
}

type workTreeBackfillCard struct {
	id, handle, created string
	open, live          bool
	activeBoard         bool
	declared, topLevel  bool
	roleBoard, hasPlan  bool
	loop                bool
}

// BackfillWorkTree recomputes every card's stored tree position from legacy
// signals. Migration 127 runs it once; tests use it to exercise the rules.
func BackfillWorkTree(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx, `UPDATE cards SET parent_card_id=NULL,needs_home=0`); err != nil {
		return err
	}
	return backfillWorkTree(ctx, tx)
}

func backfillWorkTree(ctx context.Context, tx *sql.Tx) error {
	hasRoles, err := sqliteTableHasColumn(ctx, tx, "boards", "role")
	if err != nil {
		return err
	}
	hasRoot, err := sqliteTableHasColumn(ctx, tx, "cards", "initiative_root")
	if err != nil {
		return err
	}
	role, root := `0`, `0`
	if hasRoles {
		role = `COALESCE((SELECT b.role='initiatives' AND b.archived_at IS NULL AND b.trashed_at IS NULL FROM boards b WHERE b.id=c.board_id),0)`
	}
	if hasRoot {
		root = `c.initiative_root`
	}
	rows, err := tx.QueryContext(ctx, `SELECT c.id,COALESCE(c.handle,''),c.created_at,
	 c.column_key NOT IN ('done','cancelled'),c.archived_at IS NULL AND c.trashed_at IS NULL,
	 `+root+`=1 AND `+role+`,`+role+`,
	 COALESCE((SELECT json_extract(m.metadata_json,'$.top_level')=1 FROM work_metadata m WHERE m.card_id=c.id),0),
	 EXISTS(SELECT 1 FROM card_plans p WHERE p.card_id=c.id AND json_array_length(p.body_json,'$.steps')>0),
	 COALESCE((SELECT json_type(m.metadata_json,'$.cadence')='object' OR json_type(m.metadata_json,'$.goal')='object' FROM work_metadata m WHERE m.card_id=c.id),0),
	 COALESCE((SELECT b.archived_at IS NULL AND b.trashed_at IS NULL FROM boards b WHERE b.id=c.board_id),0)
	 FROM cards c WHERE c.trashed_at IS NULL`)
	if err != nil {
		return err
	}
	cards := map[string]*workTreeBackfillCard{}
	byRef := map[string]string{}
	for rows.Next() {
		c := &workTreeBackfillCard{}
		var roleBoard sql.NullBool
		if err = rows.Scan(&c.id, &c.handle, &c.created, &c.open, &c.live, &c.declared, &roleBoard, &c.topLevel, &c.hasPlan, &c.loop, &c.activeBoard); err != nil {
			rows.Close()
			return err
		}
		c.roleBoard = roleBoard.Valid && roleBoard.Bool
		cards[c.id] = c
		byRef["card:"+c.id] = c.id
		if c.handle != "" {
			byRef["card:"+c.handle] = c.id
		}
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return err
	}
	// Every plan link is a parent candidate; the read-time rule picked the oldest.
	links, err := tx.QueryContext(ctx, `SELECT p.card_id,json_extract(step.value,'$.ref') FROM card_plans p CROSS JOIN json_each(p.body_json,'$.steps') step WHERE substr(json_extract(step.value,'$.ref'),1,5)='card:'`)
	if err != nil {
		return err
	}
	candidates := map[string][]string{}
	for links.Next() {
		var parent string
		var ref sql.NullString
		if err = links.Scan(&parent, &ref); err != nil {
			links.Close()
			return err
		}
		child, ok := byRef[ref.String]
		if !ok || child == parent || cards[parent] == nil || !cards[parent].live || !cards[parent].open || !cards[parent].activeBoard {
			continue
		}
		candidates[child] = append(candidates[child], parent)
	}
	links.Close()
	if err = links.Err(); err != nil {
		return err
	}
	parent := map[string]string{}
	for child, list := range candidates {
		if cards[child].declared {
			continue
		}
		sort.Slice(list, func(i, j int) bool {
			a, b := cards[list[i]], cards[list[j]]
			if a.created != b.created {
				return a.created < b.created
			}
			return a.id < b.id
		})
		parent[child] = list[0]
	}
	// Legacy plan graphs may contain cycles. Break each at its oldest member.
	ids := make([]string, 0, len(cards))
	for id := range cards {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		if cards[ids[i]].created != cards[ids[j]].created {
			return cards[ids[i]].created < cards[ids[j]].created
		}
		return ids[i] < ids[j]
	})
	for _, id := range ids {
		seen := map[string]bool{id: true}
		for at := parent[id]; at != ""; at = parent[at] {
			if seen[at] {
				oldest := at
				for member := parent[at]; member != at; member = parent[member] {
					if cards[member].created < cards[oldest].created || (cards[member].created == cards[oldest].created && member < oldest) {
						oldest = member
					}
				}
				delete(parent, oldest)
				break
			}
			seen[at] = true
		}
	}
	// The tree holds at most three levels. Re-home deeper legacy work under its
	// level-1 ancestor, shallowest first, so its own subtree moves up with it.
	depth := func(id string) (int, []string) {
		chain := []string{}
		for at := parent[id]; at != "" && len(chain) <= len(cards); at = parent[at] {
			chain = append(chain, at)
		}
		return len(chain), chain
	}
	byDepth := append([]string{}, ids...)
	initial := map[string]int{}
	for _, id := range byDepth {
		initial[id], _ = depth(id)
	}
	sort.SliceStable(byDepth, func(i, j int) bool { return initial[byDepth[i]] < initial[byDepth[j]] })
	for _, id := range byDepth {
		if d, chain := depth(id); d >= 3 {
			parent[id] = chain[d-2]
		}
	}
	hasChildren := map[string]bool{}
	for _, p := range parent {
		hasChildren[p] = true
	}
	anyRoleBoard := false
	for _, c := range cards {
		anyRoleBoard = anyRoleBoard || c.roleBoard
	}
	for _, id := range ids {
		c := cards[id]
		needsHome := 0
		// Closed and archived non-initiatives also wait in the tray, so a finished
		// task never reads as a finished initiative.
		if parent[id] == "" && !c.loop && !c.topLevel && !c.roleBoard && !hasChildren[id] && (anyRoleBoard || !c.hasPlan) {
			needsHome = 1
		}
		if parent[id] == "" && needsHome == 0 {
			continue
		}
		if _, err = tx.ExecContext(ctx, `UPDATE cards SET parent_card_id=?,needs_home=? WHERE id=?`, nullString(parent[id]), needsHome, id); err != nil {
			return err
		}
	}
	return nil
}

func nullString(s string) any {
	if s == "" {
		return nil
	}
	return s
}
