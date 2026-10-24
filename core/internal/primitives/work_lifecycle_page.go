package primitives

import (
	"agent-nexus-core/internal/resourceaccess"
	"context"
)

func (s *Store) hiddenWorkPage(ctx context.Context, after string, limit int, visible func(string, string) bool) ([]string, string, bool, error) {
	refs := []string{}
	more := false
	for pass := 0; pass < maxWorkAccessFilterPasses; pass++ {
		ledger := `CASE WHEN ` + resourceaccess.CompactReaderSQL + ` THEN 2 WHEN NOT (` + resourceaccess.CompactReaderSQL + `) THEN 1 ELSE -1 END`
		source := `SELECT id FROM work_lifecycle_positions WHERE ledger=(` + ledger + `) AND id>?`
		args := []any{after}
		if scope, scoped := accessScopeFrom(ctx); scoped {
			if scope.ActorID != "" && scope.ActorID == scope.PMActorID {
				source += ` AND availability<>3`
			} else {
				source += ` AND availability=0 UNION ALL SELECT id FROM work_lifecycle_positions WHERE ledger=(` + ledger + `) AND availability=1 AND owner=? AND id>?`
				args = append(args, scope.ActorID, after)
			}
		}
		args = append(args, limit+1)
		rows, err := s.db.QueryContext(ctx, `SELECT c.id,COALESCE(c.handle,'') FROM (`+source+`) p CROSS JOIN cards c ON c.id=p.id ORDER BY p.id LIMIT ?`, args...)
		if err != nil {
			return nil, "", false, err
		}
		cards := []map[string]any{}
		for rows.Next() {
			var id, handle string
			if err = rows.Scan(&id, &handle); err != nil {
				rows.Close()
				return nil, "", false, err
			}
			cards = append(cards, map[string]any{"id": id, "handle": handle})
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, "", false, err
		}
		more = len(cards) > limit
		cards = cards[:min(len(cards), limit)]
		if len(cards) == 0 {
			break
		}
		after = workString(cards[len(cards)-1]["id"])
		if visible != nil {
			cards, err = s.FilterCardAccess(ctx, cards, visible)
			if err != nil {
				return nil, "", false, err
			}
		}
		for _, c := range cards {
			refs = append(refs, "card:"+workString(c["id"]))
			if h := workString(c["handle"]); h != "" {
				refs = append(refs, "card:"+h)
			}
		}
		if len(refs) > 0 || !more {
			break
		}
	}
	return uniqueSortedStrings(refs), after, more, nil
}
