package primitives

import (
	"encoding/json"
)

func (b *briefBuild) groupingBudget() (map[string]any, []map[string]any, error) {
	ctx := b.groupingContext()
	candidates, err := recentHomeCandidates(ctx, b.s.db, b.now)
	if err != nil {
		return nil, nil, err
	}
	actions := []map[string]any{}
	for _, c := range candidates {
		b.topLevelIDs = append(b.topLevelIDs, c.id)
	}
	// Admit physical routing positions before visibility. Never scan through a
	// denied prefix to fill an orient count or disclose its occupancy.
	positions, err := b.s.db.QueryContext(ctx, `SELECT id FROM work_grouping_card_positions LIMIT 201`)
	if err != nil {
		return nil, nil, err
	}
	known := map[string]bool{}
	for _, id := range b.topLevelIDs {
		known[id] = true
	}
	physical := 0
	unknown := []string{}
	for positions.Next() {
		var id string
		if err = positions.Scan(&id); err != nil {
			positions.Close()
			return nil, nil, err
		}
		physical++
		if !known[id] {
			unknown = append(unknown, id)
		}
	}
	positions.Close()
	if err = positions.Err(); err != nil {
		return nil, nil, err
	}
	if physical < 201 {
		b.allCardsAreRoots = true
		if len(unknown) > 0 {
			ids, _ := json.Marshal(unknown)
			var otherVisible bool
			if err = b.s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM json_each(?) candidate CROSS JOIN cards c ON c.id=candidate.value LIMIT 1)`, string(ids)).Scan(&otherVisible); err != nil {
				return nil, nil, err
			}
			b.allCardsAreRoots = !otherVisible
		}
	}
	_, scoped := accessScopeFrom(ctx)
	// Scoped counts are always lower bounds: a marker must not disclose whether
	// a physical window filled with hidden cards. This matches scoped previews.
	budget := map[string]any{"count": len(candidates), "target": 15, "truncated": scoped || physical > homeCandidateLimit}
	// The proof checks only positions outside the already admitted root set.
	// A full physical window never certifies completeness. Hidden positions
	// affect neither the returned count nor its conservative scoped marker.
	for _, c := range candidates {
		if len(actions) >= 10 {
			break
		}
		var m map[string]any
		_ = json.Unmarshal([]byte(c.metadata), &m)
		if m == nil {
			m = map[string]any{}
		}
		m["id"], m["ref"], m["title"], m["refs"] = c.id, c.ref, c.title, json.RawMessage(c.refs)
		homes := likelyHomes(m, candidates)
		if len(homes) == 0 && staleHome(c, b.now) {
			for i := len(candidates) - 1; i >= 0; i-- {
				target := candidates[i]
				if target.id != c.id && target.created < c.created {
					homes = []LikelyHome{{Ref: target.ref, Title: target.title, Reasons: []string{"stale_top_level"}}}
					break
				}
			}
		}
		if len(homes) == 0 {
			continue
		}
		// Fold toward the older home only; otherwise two siblings would
		// suggest reciprocal folds. related_parent always remains explicit.
		var target *homeCandidate
		for i := range candidates {
			if candidates[i].ref == homes[0].Ref {
				target = &candidates[i]
				break
			}
		}
		if target == nil || target.created > c.created || (target.created == c.created && target.id > c.id) {
			continue
		}
		if staleHome(c, b.now) {
			homes[0].Reasons = append(homes[0].Reasons, "stale_top_level")
		}
		actions = append(actions, GroupingNextActions(c.ref, homes, "fold")...)
	}
	return budget, actions, nil
}
