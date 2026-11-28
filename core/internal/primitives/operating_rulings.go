package primitives

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

const OperatingRulingsPageSize = 100

var (
	operatingRulingID = regexp.MustCompile(`^R-[0-9]{8}-[0-9]{2}$`)
	rulingTopic       = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)
	rulingAlias       = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9 _.-]{0,95}$`)
)

func ValidOperatingRulingID(id string) bool {
	if !operatingRulingID.MatchString(id) || id[11:] == "00" {
		return false
	}
	_, err := time.Parse("20060102", id[2:10])
	return err == nil
}

type OperatingRulingRelation struct {
	Kind     string `json:"kind"`
	RulingID string `json:"ruling_id"`
}

type OperatingRulingProposal struct {
	RulingID         string                    `json:"ruling_id"`
	Aliases          []string                  `json:"aliases,omitempty"`
	Scope            string                    `json:"scope"`
	Topic            string                    `json:"topic"`
	Title            string                    `json:"title"`
	Body             string                    `json:"body"`
	SourceQuote      string                    `json:"source_quote"`
	Source           string                    `json:"source,omitempty"`
	StatusHint       string                    `json:"status_hint,omitempty"`
	SupersededByHint string                    `json:"superseded_by_hint,omitempty"`
	Relations        []OperatingRulingRelation `json:"relations,omitempty"`
	RequestKey       string                    `json:"idempotency_key"`
}

type OperatingRuling struct {
	OperatingRulingProposal
	CreatedBy, CreatedAt, DecidedBy, DecidedAt, Status, SupersededBy, RevokedBy, RevokedAt string
}

func (r OperatingRuling) JSON() map[string]any {
	out := StandingRuling{ID: r.RulingID, Kind: "operating", Question: r.Title, CreatedBy: r.CreatedBy, CreatedAt: r.CreatedAt, RevokedBy: r.RevokedBy, RevokedAt: r.RevokedAt}.JSON()
	out["ruling_id"], out["scope"], out["topic"], out["body"] = r.RulingID, r.Scope, r.Topic, r.Body
	out["source_quote"], out["aliases"], out["relations"] = r.SourceQuote, r.Aliases, r.Relations
	out["status"], out["active"] = r.Status, r.Status == "active"
	for k, v := range map[string]string{
		"source": r.Source, "status_hint": r.StatusHint, "superseded_by_hint": r.SupersededByHint,
		"superseded_by": r.SupersededBy, "decided_by": stripActor(r.DecidedBy), "decided_at": r.DecidedAt,
	} {
		if v != "" {
			out[k] = v
		}
	}
	return out
}

const operatingRulingColumns = `ruling_id,ruling_title,aliases_json,scope,topic,body,source_quote,source,status_hint,superseded_by_hint,relations_json,created_by,created_at,decided_by,decided_at,status,superseded_by,revoked_by,COALESCE(revoked_at,'')`

func scanOperatingRuling(row interface{ Scan(...any) error }) (OperatingRuling, error) {
	var r OperatingRuling
	var aliases, relations string
	err := row.Scan(&r.RulingID, &r.Title, &aliases, &r.Scope, &r.Topic, &r.Body, &r.SourceQuote, &r.Source, &r.StatusHint, &r.SupersededByHint, &relations, &r.CreatedBy, &r.CreatedAt, &r.DecidedBy, &r.DecidedAt, &r.Status, &r.SupersededBy, &r.RevokedBy, &r.RevokedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return r, ErrNotFound
	}
	if err != nil {
		return r, err
	}
	if err := json.Unmarshal([]byte(aliases), &r.Aliases); err != nil {
		return r, err
	}
	return r, json.Unmarshal([]byte(relations), &r.Relations)
}

func (p OperatingRulingProposal) validate() error {
	if !ValidOperatingRulingID(p.RulingID) || !rulingTopic.MatchString(p.Topic) || (p.Scope != "workspace" && p.Scope != "account") {
		return workInvalid("a valid date ruling ID, scope and topic are required")
	}
	if strings.TrimSpace(p.Title) == "" || utf8.RuneCountInString(p.Title) > 1024 || strings.TrimSpace(p.Body) == "" || utf8.RuneCountInString(p.Body) > 32768 || strings.TrimSpace(p.SourceQuote) == "" || utf8.RuneCountInString(p.SourceQuote) > 8192 || utf8.RuneCountInString(p.Source) > 2048 {
		return workInvalid("provide a title (1024 characters), body (32768 characters) and source quote (8192 characters) within their size limits, with an optional source of up to 2048 characters")
	}
	if len(p.Aliases) > 32 || len(p.Relations) > 200 {
		return workInvalid("at most 32 aliases and 200 relations are allowed")
	}
	seen := map[string]bool{p.RulingID: true}
	for _, alias := range p.Aliases {
		if !rulingAlias.MatchString(alias) || strings.TrimSpace(alias) != alias || operatingRulingID.MatchString(alias) || seen[alias] {
			return workInvalid("aliases must be distinct legacy names, not date IDs")
		}
		seen[alias] = true
	}
	seen = map[string]bool{p.RulingID: true}
	for _, rel := range p.Relations {
		if !ValidOperatingRulingID(rel.RulingID) || seen[rel.RulingID] || (rel.Kind != "supersedes" && rel.Kind != "amends" && rel.Kind != "independent") {
			return workInvalid("declare at most one valid relation per other ruling")
		}
		if rel.Kind == "supersedes" && (p.StatusHint == "revoked" || p.StatusHint == "spent") {
			return workInvalid("a retired import cannot supersede another ruling")
		}
		seen[rel.RulingID] = true
	}
	switch p.StatusHint {
	case "", "active", "superseded", "revoked", "spent", "historical":
	default:
		return workInvalid("invalid import status hint")
	}
	if p.SupersededByHint != "" && (!ValidOperatingRulingID(p.SupersededByHint) || p.SupersededByHint == p.RulingID) {
		return workInvalid("invalid import superseder hint")
	}
	return nil
}

type RulingRelationsRequired struct{ Missing []string }

var ErrRulingPeerNotVisible = errors.New("an active ruling in this topic is not visible to you; ask a human with access to resolve the relation")

func (e *RulingRelationsRequired) Error() string {
	return "declare relations for " + strings.Join(e.Missing, ", ")
}

func checkOperatingRulingCoverage(ctx context.Context, tx *accessTx, p OperatingRulingProposal) error {
	rows, err := tx.QueryContext(ctx, `SELECT ruling_id FROM standing_rulings WHERE status='active' AND ruling_id IS NOT NULL AND scope=? AND topic=? AND ruling_id<>? ORDER BY ruling_id LIMIT 201`, p.Scope, p.Topic, p.RulingID)
	if err != nil {
		return err
	}
	defer rows.Close()
	related := make(map[string]bool, len(p.Relations))
	for _, rel := range p.Relations {
		related[rel.RulingID] = true
	}
	missing := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return err
		}
		if !related[id] {
			missing = append(missing, id)
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(missing) > 0 {
		return &RulingRelationsRequired{Missing: missing}
	}
	return nil
}

func operatingRulingError(err error) error {
	var dbErr *sqlite.Error
	if errors.As(err, &dbErr) {
		switch dbErr.Code() {
		case sqlite3.SQLITE_CONSTRAINT_UNIQUE, sqlite3.SQLITE_CONSTRAINT_PRIMARYKEY:
			return ErrConflict
		case sqlite3.SQLITE_CONSTRAINT_TRIGGER:
			if strings.Contains(err.Error(), "standing ruling relation required") {
				return ErrRulingPeerNotVisible
			}
		}
	}
	return err
}

func (s *Store) ProposeOperatingRuling(ctx context.Context, actor string, p OperatingRulingProposal, now time.Time) (OperatingRuling, error) {
	if err := p.validate(); err != nil {
		return OperatingRuling{}, err
	}
	if p.Aliases == nil {
		p.Aliases = []string{}
	}
	if p.Relations == nil {
		p.Relations = []OperatingRulingRelation{}
	}
	aliases, _ := json.Marshal(p.Aliases)
	relations, _ := json.Marshal(p.Relations)
	if s.quota.enabled() && !replayOwnsQuota(ctx, s) {
		s.quotaMu.Lock()
		defer s.quotaMu.Unlock()
		if s.quota.MaxBlobBytes > 0 {
			if err := s.ensureBlobUsageLedgerInitialized(ctx); err != nil {
				return OperatingRuling{}, err
			}
		}
	}
	bytes := len(p.Title) + len(p.Body) + len(p.SourceQuote) + len(p.Source) + len(aliases) + len(relations) + 512
	if err := s.checkWorkspaceWriteQuota(ctx, 0, quotaWriteDelta{dbBytes: int64(bytes)}, blobLedgerWritePlan{}); err != nil {
		return OperatingRuling{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return OperatingRuling{}, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE standing_ruling_revision SET version=version WHERE singleton=1`); err != nil {
		return OperatingRuling{}, err
	}
	if err := checkOperatingRulingCoverage(ctx, tx, p); err != nil {
		return OperatingRuling{}, err
	}
	// Imports can refer to rows later in the index. Ordinary proposals must
	// resolve their targets through the caller's scoped view before submission.
	if p.StatusHint == "" {
		for _, rel := range p.Relations {
			var target string
			err := tx.QueryRowContext(ctx, `SELECT ruling_id FROM standing_rulings WHERE ruling_id=?`, rel.RulingID).Scan(&target)
			if errors.Is(err, sql.ErrNoRows) {
				return OperatingRuling{}, ErrNotFound
			}
			if err != nil {
				return OperatingRuling{}, err
			}
		}
	}
	created := now.UTC().Format(time.RFC3339Nano)
	_, err = tx.ExecContext(ctx, `INSERT INTO standing_rulings
	 (id,title_key,kind,question,created_by,created_at,ruling_id,ruling_title,aliases_json,scope,topic,body,source_quote,source,relations_json,status,status_hint,superseded_by_hint)
	 VALUES (?,'','operating','',?,?,?,?,?,?,?,?,?,?,?,'proposed',?,?)`,
		p.RulingID, actor, created, p.RulingID, p.Title, string(aliases), p.Scope, p.Topic, p.Body, p.SourceQuote, p.Source, string(relations), p.StatusHint, p.SupersededByHint)
	if err != nil {
		return OperatingRuling{}, operatingRulingError(err)
	}
	for _, name := range append([]string{p.RulingID}, p.Aliases...) {
		if _, err := tx.ExecContext(ctx, `INSERT INTO standing_ruling_names(name,ruling_id) VALUES (?,?)`, name, p.RulingID); err != nil {
			return OperatingRuling{}, operatingRulingError(err)
		}
	}
	return OperatingRuling{OperatingRulingProposal: p, Status: "proposed", CreatedAt: created, CreatedBy: actor}, tx.Commit()
}

func (s *Store) ConfirmOperatingRuling(ctx context.Context, actor, id string, now time.Time) (OperatingRuling, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return OperatingRuling{}, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE standing_ruling_revision SET version=version WHERE singleton=1`); err != nil {
		return OperatingRuling{}, err
	}
	r, err := scanOperatingRuling(tx.QueryRowContext(ctx, "SELECT "+operatingRulingColumns+" FROM standing_rulings WHERE ruling_id=?", id))
	if err != nil {
		return r, err
	}
	confirmedImport := r.DecidedAt != "" && ((r.Status == "superseded" && r.StatusHint == "superseded") ||
		(r.Status == "revoked" && (r.StatusHint == "revoked" || r.StatusHint == "spent")))
	if r.Status == "active" || confirmedImport {
		return r, nil
	}
	if r.Status != "proposed" {
		return r, ErrConflict
	}
	if err := checkOperatingRulingCoverage(ctx, tx, r.OperatingRulingProposal); err != nil {
		return r, err
	}
	decidedAt := now.UTC().Format(time.RFC3339Nano)
	for _, rel := range r.Relations {
		var scope, status, hint, successorHint, successor string
		err := tx.QueryRowContext(ctx, `SELECT scope,status,status_hint,superseded_by_hint,superseded_by FROM standing_rulings WHERE ruling_id=?`, rel.RulingID).Scan(&scope, &status, &hint, &successorHint, &successor)
		if errors.Is(err, sql.ErrNoRows) {
			return r, ErrNotFound
		}
		if err != nil {
			return r, err
		}
		if rel.Kind != "supersedes" {
			continue
		}
		if scope == r.Scope && status == "superseded" && successor == id {
			continue
		}
		importedPredecessor := status == "proposed" && hint == "superseded" && successorHint == id
		if scope != r.Scope || (status != "active" && !importedPredecessor) {
			return r, fmt.Errorf("%w: superseded target %s must be active and have the same scope", ErrConflict, rel.RulingID)
		}
		if _, err := tx.ExecContext(ctx, `UPDATE standing_rulings SET status='superseded',superseded_by=?,decided_by=COALESCE(NULLIF(decided_by,''),?),decided_at=COALESCE(NULLIF(decided_at,''),?) WHERE ruling_id=?`, id, actor, decidedAt, rel.RulingID); err != nil {
			return r, err
		}
	}
	r.Status, r.DecidedBy, r.DecidedAt = "active", actor, decidedAt
	var revokedAt any
	switch r.StatusHint {
	case "superseded":
		r.Status, r.SupersededBy = "superseded", r.SupersededByHint
	case "revoked", "spent":
		r.Status, r.RevokedBy, r.RevokedAt = "revoked", actor, r.DecidedAt
		revokedAt = r.RevokedAt
	}
	if _, err := tx.ExecContext(ctx, `UPDATE standing_rulings SET status=?,decided_by=?,decided_at=?,superseded_by=?,revoked_by=?,revoked_at=? WHERE ruling_id=?`, r.Status, r.DecidedBy, r.DecidedAt, r.SupersededBy, r.RevokedBy, revokedAt, id); err != nil {
		return r, operatingRulingError(err)
	}
	return r, tx.Commit()
}

func (s *Store) RevokeOperatingRuling(ctx context.Context, actor, id string, now time.Time) (OperatingRuling, error) {
	var r OperatingRuling
	err := s.db.WriteTransaction(ctx, func(ctx context.Context, tx *accessTx) error {
		// Acquire the write snapshot before observing the proposal state.
		if _, err := tx.ExecContext(ctx, `UPDATE standing_ruling_revision SET version=version WHERE singleton=1`); err != nil {
			return err
		}
		var err error
		r, err = scanOperatingRuling(tx.QueryRowContext(ctx, "SELECT "+operatingRulingColumns+" FROM standing_rulings WHERE ruling_id=?", id))
		if err != nil || r.Status == "revoked" {
			return err
		}
		r.Status, r.RevokedBy, r.RevokedAt = "revoked", actor, now.UTC().Format(time.RFC3339Nano)
		_, err = tx.ExecContext(ctx, `UPDATE standing_rulings SET status=?,revoked_by=?,revoked_at=? WHERE ruling_id=?`, r.Status, r.RevokedBy, r.RevokedAt, id)
		return err
	})
	return r, err
}
