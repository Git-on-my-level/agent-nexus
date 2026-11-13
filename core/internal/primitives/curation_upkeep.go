package primitives

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"agent-nexus-core/internal/plans"
)

// curationUpkeepScan bounds every upkeep read of the open-suggestion ledger.
const curationUpkeepScan = 200

// DismissSupersededBatchSuggestions closes open batch suggestions whose asks
// are all covered by a larger open batch suggestion. A suggestion's key is its
// exact target set, so one more ask on a subject used to leave "Batch 2",
// "Batch 3" … "Batch 7" open side by side. Only the largest is worth a
// reviewer's time. It reads at most curationUpkeepScan open rows and returns
// how many it closed.
func (s *Store) DismissSupersededBatchSuggestions(ctx context.Context) (int, error) {
	rows, err := s.db.QueryContext(curationInternal(ctx), `SELECT key,targets_json FROM curation_proposals WHERE status='open' AND action='batch' ORDER BY created_at DESC,key LIMIT ?`, curationUpkeepScan)
	if err != nil {
		return 0, err
	}
	type suggestion struct {
		key     string
		targets map[string]bool
	}
	open := []suggestion{}
	for rows.Next() {
		var key, raw string
		if err = rows.Scan(&key, &raw); err != nil {
			rows.Close()
			return 0, err
		}
		var targets []string
		_ = json.Unmarshal([]byte(raw), &targets)
		set := map[string]bool{}
		for _, t := range targets {
			set[t] = true
		}
		open = append(open, suggestion{key, set})
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return 0, err
	}
	closed := 0
	for _, small := range open {
		for _, large := range open {
			if len(large.targets) <= len(small.targets) {
				continue
			}
			covered := true
			for t := range small.targets {
				if !large.targets[t] {
					covered = false
					break
				}
			}
			if !covered {
				continue
			}
			ok, err := s.ResolveCurationProposal(ctx, small.key, CurationProposalDismissed)
			if err != nil {
				return closed, err
			}
			if ok {
				closed++
			}
			break
		}
	}
	return closed, nil
}

// markSuggestionOwnership sets `yours` on each curation suggestion: true when
// the reader asked one of the target asks or is assigned one of the target
// cards. An agent tends its own plot first; the rest are someone else's to
// review. Two batched lookups over the page's targets.
func (b *briefBuild) markSuggestionOwnership(suggestions []map[string]any) error {
	asks, cards := []string{}, []string{}
	for _, s := range suggestions {
		s["yours"] = false
		for _, target := range anyStringSlice(s["targets"]) {
			if id, ok := strings.CutPrefix(target, "event:"); ok {
				asks = append(asks, id)
			} else if id, ok := strings.CutPrefix(target, "card:"); ok {
				cards = append(cards, id)
			}
		}
	}
	if len(asks)+len(cards) == 0 {
		return nil
	}
	me := stripActor(b.reader.ActorID)
	mine := map[string]bool{}
	askJSON, _ := json.Marshal(asks)
	cardJSON, _ := json.Marshal(cards)
	rows, err := b.s.db.QueryContext(b.ctx, `SELECT 'event:'||a.ask_id,a.requester FROM brief_asks a WHERE a.ask_id IN (SELECT value FROM json_each(?))
 UNION ALL SELECT 'card:'||c.id,COALESCE(c.assignee,'') FROM cards c WHERE c.id IN (SELECT value FROM json_each(?))
 UNION ALL SELECT 'card:'||c.handle,COALESCE(c.assignee,'') FROM cards c WHERE c.handle IS NOT NULL AND c.handle IN (SELECT value FROM json_each(?))`, string(askJSON), string(cardJSON), string(cardJSON))
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var ref, owner string
		if err = rows.Scan(&ref, &owner); err != nil {
			return err
		}
		for _, candidate := range append(boardCardAssigneeRefs(owner), owner) {
			if stripActor(candidate) == me {
				mine[ref] = true
			}
		}
	}
	if err = rows.Err(); err != nil {
		return err
	}
	for _, s := range suggestions {
		for _, target := range anyStringSlice(s["targets"]) {
			if mine[target] {
				s["yours"] = true
			}
		}
	}
	return nil
}

// upkeep reports whether housekeeping is being done: how many suggestions are
// open, how long the oldest has waited, and whether that is longer than
// plans.NoPromiseFallback, the one duration fallback the product allows.
// Watch turns `untended` into one line telling the human that no agent is
// tending the workspace. A scoped
// reader gets nothing, so the count never reveals rows it cannot see.
func (b *briefBuild) upkeep() (map[string]any, error) {
	if _, scoped := accessScopeFrom(b.ctx); scoped {
		return nil, nil
	}
	var count int
	var oldest string
	err := b.s.db.QueryRowContext(curationInternal(b.ctx), `SELECT COUNT(*),COALESCE(MIN(created_at),'') FROM (SELECT created_at FROM curation_proposals WHERE status='open' ORDER BY created_at,key LIMIT ?)`, BriefSectionLimit+1).Scan(&count, &oldest)
	if err != nil {
		return nil, err
	}
	out := map[string]any{"open_suggestions": min(count, BriefSectionLimit), "truncated": count > BriefSectionLimit, "oldest_waiting_seconds": nil, "untended": false}
	if at, e := time.Parse(time.RFC3339Nano, oldest); e == nil {
		out["oldest_waiting_seconds"] = int64(b.now.Sub(at) / time.Second)
		out["untended"] = b.now.Sub(at) > plans.NoPromiseFallback
	}
	return out, nil
}
