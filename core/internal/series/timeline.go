package series

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"
)

// Timeline preserves individual adapter observations, including events sharing
// a chart bucket. Each existing label set uses one indexed LIMIT 101 seek, so a
// busy stream never needs to aggregate its complete history to show recent events.
func (s Store) Timeline(ctx context.Context, name string, labels map[string]string, window time.Duration, now time.Time) (Result, bool, error) {
	result := Result{Streams: []Stream{}, Resolution: "raw"}
	if window < time.Second || window > Retention {
		return result, false, fmt.Errorf("%w: timeline range must be between 1s and 90d", ErrInvalid)
	}
	if _, err := Labels(labels); err != nil {
		return result, false, err
	}
	tx, err := s.DB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return result, false, err
	}
	defer tx.Rollback()
	err = tx.QueryRowContext(ctx, `SELECT s.name,s.kind,s.unit,d.name,h.slug,h.id,d.agent_id,d.expected_interval,d.last_push,d.revoked_at FROM series_definitions s JOIN series_adapters d ON d.name=s.adapter JOIN hosts h ON h.id=d.host_id WHERE s.name=?`, name).Scan(&result.Name, &result.Kind, &result.Unit, &result.Adapter, &result.Host, &result.HostID, &result.AgentID, &result.ExpectedInterval, &result.LastPush, &result.RevokedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return result, false, ErrNotFound
	}
	if err != nil {
		return result, false, err
	}
	filter := ""
	args := []any{name}
	keys := []string{}
	for key := range labels {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		filter += ` AND json_extract(labels,?)=?`
		args = append(args, `$."`+key+`"`, labels[key])
	}
	groups, err := tx.QueryContext(ctx, `SELECT labels FROM series_labels WHERE series=?`+filter+` ORDER BY labels LIMIT 100`, args...)
	if err != nil {
		return result, false, err
	}
	labelSets := []string{}
	for groups.Next() {
		var label string
		if err := groups.Scan(&label); err != nil {
			groups.Close()
			return result, false, err
		}
		labelSets = append(labelSets, label)
	}
	err = groups.Err()
	groups.Close()
	if err != nil {
		return result, false, err
	}
	latest, err := lastPoints(ctx, tx, name, labelSets, now.UnixNano())
	if err != nil {
		return result, false, err
	}
	truncated := false
	for _, encoded := range labelSets {
		stream := Stream{Labels: map[string]string{}, Points: []Observation{}, Stale: true}
		if err := json.Unmarshal([]byte(encoded), &stream.Labels); err != nil {
			return result, false, err
		}
		if last := latest[encoded]; last.Valid {
			stream.LastPoint = time.Unix(0, last.Int64).UTC().Format(time.RFC3339Nano)
			staleAt := time.Unix(0, last.Int64).Add(time.Duration(result.ExpectedInterval) * 2 * time.Second)
			stream.Stale = now.After(staleAt) || result.RevokedAt != nil
			if stream.Stale {
				if result.RevokedAt != nil {
					if revoked, err := time.Parse(time.RFC3339Nano, *result.RevokedAt); err == nil && revoked.Before(staleAt) {
						staleAt = revoked
					}
				}
				stream.StaleSince = staleAt.UTC().Format(time.RFC3339Nano)
			}
		}
		rows, err := tx.QueryContext(ctx, `SELECT ts,value,state FROM series_points WHERE series=? AND labels=? AND ts>=? AND ts<=? ORDER BY ts DESC LIMIT 101`, name, encoded, now.Add(-window).UnixNano(), now.UnixNano())
		if err != nil {
			return result, false, err
		}
		for rows.Next() {
			var stamp int64
			var value sql.NullFloat64
			var state sql.NullString
			if err := rows.Scan(&stamp, &value, &state); err != nil {
				rows.Close()
				return result, false, err
			}
			if len(stream.Points) == 100 {
				truncated = true
				break
			}
			point := Observation{TS: time.Unix(0, stamp).UTC().Format(time.RFC3339Nano)}
			if value.Valid {
				v := value.Float64
				point.Value = &v
			}
			if state.Valid {
				v := state.String
				point.State = &v
			}
			stream.Points = append(stream.Points, point)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return result, false, err
		}
		result.Streams = append(result.Streams, stream)
	}
	return result, truncated, tx.Commit()
}
