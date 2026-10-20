package auth

import (
	"context"
	"fmt"
	"time"
)

const (
	// DefaultExpiredAuthRowRetention keeps an expired or revoked access token
	// or refresh session for a day before it is deleted. The margin also
	// covers sub-second ordering of RFC3339Nano text timestamps.
	DefaultExpiredAuthRowRetention = 24 * time.Hour

	// AuthRowPurgeInterval is how often maintenance runs the purge. Each run
	// deletes up to BatchSize*MaxBatches rows of each kind (250,000), about
	// 6 million a day per kind at the default interval.
	AuthRowPurgeInterval = time.Hour

	defaultAuthRowPurgeBatchSize  = 500
	defaultAuthRowPurgeMaxBatches = 500
)

// AuthRowPurgeResult counts the rows one purge deleted.
type AuthRowPurgeResult struct {
	AccessTokens    int64
	RefreshSessions int64
	// Truncated reports that the batch budget ran out before every
	// eligible row was deleted; the next purge continues.
	Truncated bool
}

type authRowPurgeStep struct {
	query   string
	count   *int64
	batches int
	done    bool
}

// AuthRowPurgeOptions bounds one purge. Zero values use the defaults.
// MaxBatches applies to each kind of row separately.
type AuthRowPurgeOptions struct {
	Retention  time.Duration
	BatchSize  int
	MaxBatches int
	Now        time.Time
}

// Each statement deletes at most one batch, seeking the expiry or revocation
// index instead of scanning. Every statement commits on its own, so the write
// lock is held for one batch at a time.
//
// The newest access token of each principal is kept even when expired:
// principal last-seen times are derived from it.
const (
	purgeExpiredAccessTokensSQL = `DELETE FROM auth_access_tokens WHERE id IN (
		SELECT t.id FROM auth_access_tokens t
		WHERE t.expires_at < ?
		  AND EXISTS (
			SELECT 1 FROM auth_access_tokens n
			WHERE n.agent_id = t.agent_id AND n.created_at > t.created_at
		  )
		ORDER BY t.expires_at
		LIMIT ?
	)`
	purgeExpiredRefreshSessionsSQL = `DELETE FROM auth_refresh_sessions WHERE id IN (
		SELECT r.id FROM auth_refresh_sessions r
		WHERE r.expires_at < ?
		ORDER BY r.expires_at
		LIMIT ?
	)`
	purgeRevokedRefreshSessionsSQL = `DELETE FROM auth_refresh_sessions WHERE id IN (
		SELECT r.id FROM auth_refresh_sessions r
		WHERE r.revoked_at IS NOT NULL AND r.revoked_at < ?
		ORDER BY r.revoked_at
		LIMIT ?
	)`
)

// PurgeExpiredAuthRows deletes access tokens and refresh sessions that
// expired, or refresh sessions that were revoked, before the retention
// window. Live tokens and sessions are never eligible: a row qualifies only
// when its expiry or revocation time is older than now minus retention.
func (s *Store) PurgeExpiredAuthRows(ctx context.Context, options AuthRowPurgeOptions) (AuthRowPurgeResult, error) {
	if s == nil || s.db == nil {
		return AuthRowPurgeResult{}, fmt.Errorf("auth store database is not initialized")
	}
	retention := options.Retention
	if retention <= 0 {
		retention = DefaultExpiredAuthRowRetention
	}
	batchSize := options.BatchSize
	if batchSize <= 0 {
		batchSize = defaultAuthRowPurgeBatchSize
	}
	maxBatches := options.MaxBatches
	if maxBatches <= 0 {
		maxBatches = defaultAuthRowPurgeMaxBatches
	}
	now := options.Now
	if now.IsZero() {
		now = time.Now()
	}
	cutoff := now.UTC().Add(-retention).Format(time.RFC3339Nano)

	var result AuthRowPurgeResult
	steps := []*authRowPurgeStep{
		{query: purgeExpiredAccessTokensSQL, count: &result.AccessTokens},
		{query: purgeExpiredRefreshSessionsSQL, count: &result.RefreshSessions},
		{query: purgeRevokedRefreshSessionsSQL, count: &result.RefreshSessions},
	}
	// Round-robin one batch per step, each step with its own batch budget,
	// so a large backlog in one table never starves the others.
	for active := len(steps); active > 0; {
		active = 0
		for _, step := range steps {
			if step.done {
				continue
			}
			if step.batches >= maxBatches {
				step.done = true
				result.Truncated = true
				continue
			}
			if err := ctx.Err(); err != nil {
				return result, err
			}
			res, err := s.db.ExecContext(ctx, step.query, cutoff, batchSize)
			if err != nil {
				return result, fmt.Errorf("purge expired auth rows: %w", err)
			}
			step.batches++
			rows, err := res.RowsAffected()
			if err != nil {
				return result, fmt.Errorf("read expired auth row purge count: %w", err)
			}
			*step.count += rows
			if rows < int64(batchSize) {
				step.done = true
				continue
			}
			active++
		}
	}
	return result, nil
}
