package auth

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"log"
	"strings"
	"time"
)

func (s *Store) IssueTokenFromWorkspaceHumanGrant(ctx context.Context, identity WorkspaceHumanGrantIdentity) (Agent, TokenBundle, error) {
	if s == nil || s.db == nil {
		return Agent{}, TokenBundle{}, fmt.Errorf("auth store database is not initialized")
	}

	subject := strings.TrimSpace(identity.Subject)
	issuer := strings.TrimSpace(identity.Issuer)
	jti := strings.TrimSpace(identity.JTI)
	if subject == "" || issuer == "" || jti == "" {
		return Agent{}, TokenBundle{}, fmt.Errorf("%w: workspace grant subject, issuer, and jti are required", ErrInvalidRequest)
	}

	agentID, actorID, username := stableExternalGrantPrincipalIDs(issuer, subject)
	now := time.Now().UTC()
	nowText := now.Format(time.RFC3339Nano)

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Agent{}, TokenBundle{}, fmt.Errorf("begin workspace human grant transaction: %w", err)
	}

	if err := consumeWorkspaceHumanGrantJTITx(ctx, tx, jti, nowText); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		if err == ErrExternalGrantReplay {
			return Agent{}, TokenBundle{}, ErrExternalGrantReplay
		}
		return Agent{}, TokenBundle{}, err
	}

	actorDisplayName := pickExternalGrantDisplayName(identity.DisplayName, identity.Email, username)
	actorMetadataJSON, err := actorMetadataJSON(PrincipalKindHuman, AuthMethodExternalGrant, map[string]any{
		"external_subject": subject,
		"external_issuer":  issuer,
		"email":            strings.TrimSpace(identity.Email),
	})
	if err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return Agent{}, TokenBundle{}, fmt.Errorf("encode external grant actor metadata: %w", err)
	}
	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO actors(id, display_name, tags_json, created_at, metadata_json)
		 VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET
		 	display_name = excluded.display_name,
		 	tags_json = excluded.tags_json,
		 	metadata_json = excluded.metadata_json`,
		actorID,
		actorDisplayName,
		`["agent","human","external_grant"]`,
		nowText,
		actorMetadataJSON,
	); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return Agent{}, TokenBundle{}, fmt.Errorf("upsert external grant actor: %w", err)
	}

	agentMetadataJSON, err := principalMetadataJSON(PrincipalKindHuman, AuthMethodExternalGrant, map[string]any{
		"external_subject": subject,
		"external_issuer":  issuer,
		"email":            strings.TrimSpace(identity.Email),
		"display_name":     strings.TrimSpace(identity.DisplayName),
	})
	if err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return Agent{}, TokenBundle{}, fmt.Errorf("encode external grant agent metadata: %w", err)
	}
	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO agents(id, username, actor_id, created_at, updated_at, revoked_at, metadata_json)
		 VALUES (?, ?, ?, ?, ?, NULL, ?)
		 ON CONFLICT(id) DO UPDATE SET
		 	updated_at = excluded.updated_at,
		 	metadata_json = excluded.metadata_json`,
		agentID,
		username,
		actorID,
		nowText,
		nowText,
		agentMetadataJSON,
	); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return Agent{}, TokenBundle{}, fmt.Errorf("upsert external grant agent: %w", err)
	}

	var revokedAt sql.NullString
	if err := tx.QueryRowContext(ctx, `SELECT revoked_at FROM agents WHERE id = ?`, agentID).Scan(&revokedAt); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return Agent{}, TokenBundle{}, fmt.Errorf("load external grant principal: %w", err)
	}
	if revokedAt.Valid {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return Agent{}, TokenBundle{}, ErrAgentRevoked
	}

	tokens, _, err := s.issueTokenBundleTx(ctx, tx, agentID, now)
	if err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
		return Agent{}, TokenBundle{}, err
	}

	if err := tx.Commit(); err != nil {
		return Agent{}, TokenBundle{}, fmt.Errorf("commit workspace human grant transaction: %w", err)
	}

	agent, err := s.GetAgent(ctx, agentID)
	if err != nil {
		return Agent{}, TokenBundle{}, err
	}
	return agent, tokens, nil
}

func (s *Store) PurgeConsumedGrantJTIs(ctx context.Context, retention time.Duration) (int64, error) {
	if s == nil || s.db == nil {
		return 0, fmt.Errorf("auth store database is not initialized")
	}
	if retention <= 0 {
		retention = DefaultConsumedGrantJTIRetention
	}
	cutoff := time.Now().UTC().Add(-retention).Format(time.RFC3339Nano)
	result, err := s.db.ExecContext(
		ctx,
		`DELETE FROM consumed_grant_jtis WHERE consumed_at < ?`,
		cutoff,
	)
	if err != nil {
		return 0, fmt.Errorf("purge consumed grant jtis: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("read consumed grant purge rows affected: %w", err)
	}
	return rows, nil
}

func consumeWorkspaceHumanGrantJTITx(ctx context.Context, tx *sql.Tx, jti string, consumedAt string) error {
	result, err := tx.ExecContext(
		ctx,
		`INSERT INTO consumed_grant_jtis(jti, consumed_at)
		 VALUES (?, ?)
		 ON CONFLICT(jti) DO NOTHING`,
		jti,
		consumedAt,
	)
	if err != nil {
		return fmt.Errorf("record consumed grant jti: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read consumed grant jti rows affected: %w", err)
	}
	if rows == 0 {
		return ErrExternalGrantReplay
	}
	return nil
}

func stableExternalGrantPrincipalIDs(issuer string, subject string) (string, string, string) {
	digest := sha256.Sum256([]byte(strings.TrimSpace(issuer) + "\n" + strings.TrimSpace(subject)))
	hexDigest := hex.EncodeToString(digest[:])
	usernameSuffix := hexDigest
	if len(usernameSuffix) > 54 {
		usernameSuffix = usernameSuffix[:54]
	}
	return "agent_ext_" + hexDigest, "actor_ext_" + hexDigest, "external." + usernameSuffix
}

func pickExternalGrantDisplayName(displayName string, email string, fallback string) string {
	for _, value := range []string{displayName, email, fallback} {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return "external-user"
}
