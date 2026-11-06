package primitives

import (
	"sort"
	"time"
)

type trendActorEvent struct {
	actor string
	at    time.Time
}

// rankedPanel counts events per principal in each period and keeps the top
// trendsSourceRows by the current period. previous is null when the read did
// not reach back through the whole previous period.
func (b *trendsBuild) rankedPanel(id, title, direction string, events []trendActorEvent, horizon time.Time) (map[string]any, error) {
	type tally struct{ current, previous float64 }
	byActor := map[string]*tally{}
	for _, e := range events {
		if e.actor == "" {
			continue
		}
		i := b.w.bucket(e.at)
		if i < 0 {
			continue
		}
		if byActor[e.actor] == nil {
			byActor[e.actor] = &tally{}
		}
		if i >= b.w.n {
			byActor[e.actor].current++
		} else {
			byActor[e.actor].previous++
		}
	}
	ids := []string{}
	for id, t := range byActor {
		if t.current > 0 {
			ids = append(ids, id)
		}
	}
	sort.Slice(ids, func(i, j int) bool {
		if byActor[ids[i]].current != byActor[ids[j]].current {
			return byActor[ids[i]].current > byActor[ids[j]].current
		}
		return ids[i] < ids[j]
	})
	if len(ids) > trendsSourceRows {
		ids = ids[:trendsSourceRows]
	}
	// A ranking from a period the read did not cover in full would present an
	// undercount as exact; the panel says truncated instead.
	if !b.w.complete(b.w.n, horizon) {
		ids = nil
	}
	for _, id := range ids {
		b.actors[id] = true
	}
	if err := b.resolveActors(); err != nil {
		return nil, err
	}
	rows := []map[string]any{}
	for _, id := range ids {
		principal := map[string]any{"id": id, "display": id}
		if p, ok := b.display[id]; ok {
			principal = p
		}
		row := map[string]any{"principal": principal, "value": byActor[id].current, "previous": byActor[id].previous}
		if !b.w.complete(0, horizon) {
			row["previous"] = nil
		}
		rows = append(rows, row)
	}
	p := b.panel(id, title, "count", direction, nil, nil, !horizon.IsZero())
	p["rows"] = rows
	return p, nil
}
