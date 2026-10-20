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

	defaultAuthRowPurgeBatchSize  = 500
	defaultAuthRowPurgeMaxBatches = 1000
)

// AuthRowPurgeResult counts the rows one purge deleted.
type AuthRowPurgeResult struct {
	AccessTokens    int64
	RefreshSessions int64
	// Truncated reports that the batch budget ran out before every
	// eligible row was deleted; the next purge continues.
	Truncated bool
}

// AuthRowPurgeOptions bounds one purge. Zero values use the defaults.
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
	batches := 0
	for _, step := range []struct {
		query string
		count *int64
	}{
		{purgeExpiredAccessTokensSQL, &result.AccessTokens},
		{purgeExpiredRefreshSessionsSQL, &result.RefreshSessions},
		{purgeRevokedRefreshSessionsSQL, &result.RefreshSessions},
	} {
		for {
			if batches >= maxBatches {
				result.Truncated = true
				return result, nil
			}
			if err := ctx.Err(); err != nil {
				return result, err
			}
			res, err := s.db.ExecContext(ctx, step.query, cutoff, batchSize)
			if err != nil {
				return result, fmt.Errorf("purge expired auth rows: %w", err)
			}
			batches++
			rows, err := res.RowsAffected()
			if err != nil {
				return result, fmt.Errorf("read expired auth row purge count: %w", err)
			}
			*step.count += rows
			if rows < int64(batchSize) {
				break
			}
		}
	}
	return result, nil
}
