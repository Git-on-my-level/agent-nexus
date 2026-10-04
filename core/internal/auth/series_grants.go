package auth

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"
)

var ErrSeriesForbidden = errors.New("forbidden")

const SeriesPointsPushOperation = "series.points.push"

// A non-authorizing hint lets ingress bound fresh scoped credentials before SQL.
// The complete opaque token is still hashed and authenticated by the same store.
const SeriesTokenPrefix = "anxs_"

// A capability restricts a credential to an operation on one declared resource;
// it never adds the ordinary agent or administrator permission set.
type SeriesCapability struct {
	Operation string
	Resource  string
}

func (s *Store) RequireSeriesCapability(ctx context.Context, actor Principal, capability SeriesCapability) error {
	if capability.Operation != SeriesPointsPushOperation {
		return ErrSeriesForbidden
	}
	return requireSeriesWriter(ctx, s.db, actor, capability.Resource, time.Now().UTC())
}

// Follow the explicit grant pattern: durable identity and current authority are
// checked within the mutation transaction, never trusted from a request snapshot.
func RequireSeriesAdministratorTx(ctx context.Context, tx *sql.Tx, actor Principal) error {
	if actor.SeriesAdapter != "" {
		return ErrSeriesForbidden
	}
	err := requireAdministrationTx(ctx, tx, actor, false)
	if errors.Is(err, ErrAuthAdminRequired) {
		return ErrSeriesForbidden
	}
	return err
}

func (s *Store) AuditSeriesTx(ctx context.Context, tx *sql.Tx, actor Principal, event, adapter string) error {
	var owner, ownerActor, hostID, hostSlug string
	if err := tx.QueryRowContext(ctx, `SELECT a.id,a.actor_id,h.id,h.slug FROM series_adapters d JOIN agents a ON a.id=d.agent_id JOIN hosts h ON h.id=d.host_id WHERE d.name=?`, adapter).Scan(&owner, &ownerActor, &hostID, &hostSlug); err != nil {
		return err
	}
	return s.recordAuthAuditEventTx(ctx, tx, AuthAuditEventInput{EventType: event, ActorAgentID: actor.AgentID, ActorActorID: actor.ActorID, SubjectAgentID: owner, SubjectActorID: ownerActor, Metadata: map[string]any{"adapter": adapter, "host_id": hostID, "host_slug": hostSlug}})
}

// Owner identity is authenticated through the existing access-token system.
// The resulting token has no refresh credential and no general workspace access.
func (s *Store) IssueSeriesToken(ctx context.Context, adapter string, actor Principal) (TokenBundle, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return TokenBundle{}, err
	}
	defer tx.Rollback()
	if actor.SeriesAdapter != "" {
		return TokenBundle{}, ErrSeriesForbidden
	}
	var owner string
	err = tx.QueryRowContext(ctx, `SELECT d.agent_id FROM series_adapters d JOIN agents a ON a.id=d.agent_id JOIN hosts h ON h.id=d.host_id JOIN host_agents ha ON ha.agent_id=a.id AND ha.host_id=h.id WHERE d.name=? AND d.revoked_at IS NULL AND d.deleted_at IS NULL AND a.revoked_at IS NULL AND h.revoked_at IS NULL AND a.id=? AND a.actor_id=? AND NOT EXISTS (SELECT 1 FROM host_exclusions e WHERE e.host_id=h.id AND e.name=ha.name)`, adapter, actor.AgentID, actor.ActorID).Scan(&owner)
	if errors.Is(err, sql.ErrNoRows) {
		return TokenBundle{}, ErrSeriesForbidden
	}
	if err != nil {
		return TokenBundle{}, err
	}
	token, err := generateOpaqueToken(32)
	if err != nil {
		return TokenBundle{}, err
	}
	token = SeriesTokenPrefix + token
	now := time.Now().UTC()
	_, err = tx.ExecContext(ctx, `INSERT INTO auth_access_tokens(id,agent_id,token_hash,created_at,expires_at,series_adapter) VALUES(?,?,?,?,?,?)`, "access_"+uuid.NewString(), owner, hashToken(token), now.Format(time.RFC3339Nano), now.Add(s.accessTokenTTL).Format(time.RFC3339Nano), adapter)
	if err != nil {
		return TokenBundle{}, err
	}
	if err = s.AuditSeriesTx(ctx, tx, actor, "series_write_token_issued", adapter); err != nil {
		return TokenBundle{}, err
	}
	return TokenBundle{AccessToken: token, TokenType: "Bearer", ExpiresIn: int64(s.accessTokenTTL.Seconds())}, tx.Commit()
}

func RequireSeriesWriteTx(ctx context.Context, tx *sql.Tx, actor Principal, name string, now time.Time) error {
	return requireSeriesWriter(ctx, tx, actor, name, now)
}

type seriesCapabilityReader interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func requireSeriesWriter(ctx context.Context, reader seriesCapabilityReader, actor Principal, name string, now time.Time) error {
	if actor.SeriesAdapter == "" || actor.AccessTokenID == "" {
		return ErrSeriesForbidden
	}
	var expires string
	err := reader.QueryRowContext(ctx, `SELECT t.expires_at FROM auth_access_tokens t JOIN series_adapters d ON d.name=t.series_adapter JOIN series_definitions s ON s.adapter=d.name JOIN agents a ON a.id=d.agent_id JOIN hosts h ON h.id=d.host_id JOIN host_agents ha ON ha.agent_id=a.id AND ha.host_id=h.id WHERE t.id=? AND t.agent_id=? AND t.revoked_at IS NULL AND a.actor_id=? AND a.revoked_at IS NULL AND h.revoked_at IS NULL AND d.revoked_at IS NULL AND d.deleted_at IS NULL AND d.name=? AND s.name=? AND NOT EXISTS (SELECT 1 FROM host_exclusions e WHERE e.host_id=h.id AND e.name=ha.name)`, actor.AccessTokenID, actor.AgentID, actor.ActorID, actor.SeriesAdapter, name).Scan(&expires)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrSeriesForbidden
	}
	if err != nil {
		return err
	}
	until, err := time.Parse(time.RFC3339Nano, expires)
	if err != nil {
		return err
	}
	if !now.Before(until) {
		return ErrSeriesForbidden
	}
	return nil
}
