// Package delivery is the durable delivery queue: one queue for agent wakes
// (channel "bridge", mirrored from agent_wakeups by triggers) and human
// notifications (kinds "push" and "digest" on adapter-declared channels).
//
// Rows are keyed by an idempotency key, claimed under a bounded lease, retried
// with exponential backoff and closed by receipts. Receipts are queue rows, not
// timeline events: delivering a digest never writes to the event log.
package delivery

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

const (
	KindWake   = "wake"
	KindPush   = "push"
	KindDigest = "digest"

	ChannelBridge  = "bridge"
	ChannelWebhook = "webhook"

	StatusPending   = "pending"
	StatusClaimed   = "claimed"
	StatusDelivered = "delivered"
	StatusFailed    = "failed"
	StatusCanceled  = "canceled"

	OutcomeDelivered = "notification_delivered"
	OutcomeFailed    = "notification_failed"

	// Receipt outcomes an adapter reports.
	ReceiptDelivered = "delivered"
	ReceiptFailed    = "failed"
	ReceiptSkipped   = "skipped"

	MaxClaimLimit       = 50
	DefaultClaimLimit   = 10
	MinLease            = 10 * time.Second
	MaxLease            = 10 * time.Minute
	DefaultLease        = 2 * time.Minute
	DefaultMaxAttempts  = 6
	MaxListLimit        = 50
	MaxChannelsPerHuman = 16
	maxErrorLength      = 500
	maxAddressLength    = 320
	baseBackoff         = 30 * time.Second
	maxBackoff          = time.Hour
)

var (
	ErrInvalid   = errors.New("invalid delivery request")
	ErrNotFound  = errors.New("delivery not found")
	ErrConflict  = errors.New("delivery claim is not current")
	ErrForbidden = errors.New("channel is not delivered by this principal")

	channelName = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,31}$`)
)

// ValidChannel reports whether name is an adapter-deliverable channel name.
// The bridge and webhook channels are reserved: agent wakes are claimed
// through the wake routes and ask webhooks are signed and sent by core.
func ValidChannel(name string) bool {
	return channelName.MatchString(name) && name != ChannelBridge && name != ChannelWebhook
}

type Delivery struct {
	DeliveryID     string         `json:"delivery_id"`
	PrincipalID    string         `json:"principal_id"`
	Channel        string         `json:"channel"`
	Kind           string         `json:"kind"`
	Status         string         `json:"status"`
	Payload        map[string]any `json:"payload"`
	SourceRef      string         `json:"source_ref,omitempty"`
	Attempt        int            `json:"attempt"`
	MaxAttempts    int            `json:"max_attempts"`
	NextAttemptAt  string         `json:"next_attempt_at"`
	ClaimToken     string         `json:"claim_token,omitempty"`
	LeaseExpiresAt string         `json:"lease_expires_at,omitempty"`
	LastError      string         `json:"last_error,omitempty"`
	Address        string         `json:"address,omitempty"`
	Recipient      *Recipient     `json:"recipient,omitempty"`
	CreatedAt      string         `json:"created_at"`
	UpdatedAt      string         `json:"updated_at"`
	DeliveredAt    string         `json:"delivered_at,omitempty"`
	FailedAt       string         `json:"failed_at,omitempty"`
}

type Receipt struct {
	DeliveryID string `json:"delivery_id"`
	Outcome    string `json:"outcome"`
	Attempt    int    `json:"attempt"`
	Error      string `json:"error,omitempty"`
	CreatedAt  string `json:"created_at"`
}

// Request enqueues one delivery. A repeated IdempotencyKey returns the
// existing row unchanged.
type Request struct {
	IdempotencyKey string
	PrincipalID    string
	Channel        string
	Kind           string
	Payload        map[string]any
	SourceRef      string
	NotBefore      time.Time
}

type ChannelSetting struct {
	Channel       string `json:"channel"`
	Address       string `json:"address"`
	PushEnabled   bool   `json:"push_enabled"`
	DigestEnabled bool   `json:"digest_enabled"`
	UpdatedAt     string `json:"updated_at,omitempty"`
}

type Store struct {
	DB  *sql.DB
	Now func() time.Time
	// CanRead reports whether principalID may read the ask event. Per-principal
	// attention counts include an ask only when it returns true. Nil fails
	// closed for those counts; the workspace totals stay unfiltered.
	CanRead func(ctx context.Context, principalID, askEventID string) bool
}

func (s Store) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

// Fixed-width timestamps keep lexical order equal to time order.
func stamp(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05.000000000Z") }

func newID(prefix string) string {
	var b [12]byte
	_, _ = rand.Read(b[:])
	return prefix + hex.EncodeToString(b[:])
}

// Enqueue inserts a delivery once per idempotency key.
// QueueWriter permits an owning store to atomically enqueue with its
// domain CAS through the same scoped transaction.
type QueueWriter interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func (s Store) Enqueue(ctx context.Context, req Request) (Delivery, bool, error) {
	return EnqueueTx(ctx, s.DB, req, s.now())
}

// EnqueueTx retains ordinary delivery validation and idempotency; the caller
// owns transaction commit and must pass its scoped writer.
func EnqueueTx(ctx context.Context, q QueueWriter, req Request, now time.Time) (Delivery, bool, error) {
	req.IdempotencyKey = strings.TrimSpace(req.IdempotencyKey)
	req.PrincipalID = strings.TrimSpace(req.PrincipalID)
	if req.IdempotencyKey == "" || req.PrincipalID == "" || !ValidChannel(req.Channel) || (req.Kind != KindPush && req.Kind != KindDigest) {
		return Delivery{}, false, ErrInvalid
	}
	payload, err := json.Marshal(nonNil(req.Payload))
	if err != nil {
		return Delivery{}, false, err
	}
	notBefore := now
	if req.NotBefore.After(now) {
		notBefore = req.NotBefore.UTC()
	}
	result, err := q.ExecContext(ctx, `INSERT INTO deliveries(delivery_id, idempotency_key, principal_id, channel, kind, status, payload_json, source_ref, max_attempts, next_attempt_at, created_at, updated_at)
		VALUES(?,?,?,?,?,'pending',?,?,?,?,?,?) ON CONFLICT(idempotency_key) DO NOTHING`,
		newID("dlv_"), req.IdempotencyKey, req.PrincipalID, req.Channel, req.Kind, string(payload), strings.TrimSpace(req.SourceRef), DefaultMaxAttempts, stamp(notBefore), stamp(now), stamp(now))
	if err != nil {
		return Delivery{}, false, fmt.Errorf("enqueue delivery: %w", err)
	}
	n, _ := result.RowsAffected()
	d, err := scanDelivery(q.QueryRowContext(ctx, `SELECT `+deliveryColumns+` FROM deliveries WHERE idempotency_key=?`, req.IdempotencyKey))
	return d, n > 0, err
}

const deliveryColumns = `delivery_id, principal_id, channel, kind, status, payload_json, source_ref, attempts, max_attempts, next_attempt_at, claim_token, lease_expires_at, last_error, created_at, updated_at, delivered_at, failed_at`

type scanner interface{ Scan(...any) error }

func scanDelivery(row scanner) (Delivery, error) {
	var d Delivery
	var payload string
	err := row.Scan(&d.DeliveryID, &d.PrincipalID, &d.Channel, &d.Kind, &d.Status, &payload, &d.SourceRef, &d.Attempt, &d.MaxAttempts, &d.NextAttemptAt, &d.ClaimToken, &d.LeaseExpiresAt, &d.LastError, &d.CreatedAt, &d.UpdatedAt, &d.DeliveredAt, &d.FailedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Delivery{}, ErrNotFound
	}
	if err != nil {
		return Delivery{}, err
	}
	d.Payload = map[string]any{}
	_ = json.Unmarshal([]byte(payload), &d.Payload)
	return d, nil
}

func (s Store) getByKey(ctx context.Context, key string) (Delivery, error) {
	return scanDelivery(s.DB.QueryRowContext(ctx, `SELECT `+deliveryColumns+` FROM deliveries WHERE idempotency_key=?`, key))
}

func (s Store) Get(ctx context.Context, id string) (Delivery, error) {
	return scanDelivery(s.DB.QueryRowContext(ctx, `SELECT `+deliveryColumns+` FROM deliveries WHERE delivery_id=?`, strings.TrimSpace(id)))
}

// ChannelsForAgent returns the channels the agent's active adapter
// declarations deliver. Bounded by the agent's own declarations.
func (s Store) ChannelsForAgent(ctx context.Context, agentID string) ([]string, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT DISTINCT channel FROM (`+activeDeclarations+`) WHERE agent_id=? ORDER BY channel LIMIT 64`, strings.TrimSpace(agentID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// Claim leases up to limit due deliveries on one channel: pending rows whose
// next attempt is due, and claimed rows whose lease expired. Each candidate
// set is one seek on idx_deliveries_claim. The address comes from the
// recipient's channel setting (primary-key lookup); rows whose recipient has
// since removed the channel, or whose kind the channel no longer accepts, are
// canceled instead of handed out. A push whose recipient can no longer read
// its ask (Store.CanRead, checked live per claimed row) is canceled too, and
// a canceled row's payload is cleared. On a principal-addressed channel there is
// no address: the recipient must still be an active human, and a human signed
// in through an external identity grant is identified by that grant.
func (s Store) Claim(ctx context.Context, channel, claimer string, limit int, lease time.Duration) ([]Delivery, error) {
	if !ValidChannel(channel) {
		return nil, ErrInvalid
	}
	if limit <= 0 {
		limit = DefaultClaimLimit
	}
	if limit > MaxClaimLimit {
		limit = MaxClaimLimit
	}
	if lease <= 0 {
		lease = DefaultLease
	}
	if lease < MinLease || lease > MaxLease {
		return nil, ErrInvalid
	}
	opts, err := s.ChannelOptions(ctx)
	if err != nil {
		return nil, err
	}
	options, declared := opts[channel]
	principalAddressed := declared && options.Addressing == AddressingPrincipal
	now := s.now()
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	rows, err := tx.QueryContext(ctx, `SELECT delivery_id FROM (
			SELECT delivery_id, next_attempt_at AS due FROM deliveries WHERE channel=? AND status='pending' AND next_attempt_at<=? ORDER BY next_attempt_at, delivery_id LIMIT ?)
		UNION ALL SELECT delivery_id FROM (
			SELECT delivery_id, lease_expires_at AS due FROM deliveries INDEXED BY idx_deliveries_claim WHERE channel=? AND status='claimed' AND lease_expires_at<=? ORDER BY next_attempt_at, delivery_id LIMIT ?)
		LIMIT ?`, channel, stamp(now), limit, channel, stamp(now), limit, limit)
	if err != nil {
		return nil, fmt.Errorf("select claimable deliveries: %w", err)
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]Delivery, 0, len(ids))
	for _, id := range ids {
		d, err := scanDelivery(tx.QueryRowContext(ctx, `SELECT `+deliveryColumns+` FROM deliveries WHERE delivery_id=?`, id))
		if err != nil {
			return nil, err
		}
		var address string
		var push, digest int
		err = tx.QueryRowContext(ctx, `SELECT address, push_enabled, digest_enabled FROM notification_channel_settings WHERE principal_id=? AND channel=?`, d.PrincipalID, channel).Scan(&address, &push, &digest)
		saved := err == nil
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		toggled := (d.Kind == KindPush && push == 1) || (d.Kind == KindDigest && digest == 1)
		var recipient *Recipient
		cancel := ""
		switch {
		case declared && !options.Accepts(d.Kind):
			cancel = "kind not accepted by channel"
		case principalAddressed:
			address = ""
			if saved && !toggled {
				cancel = "channel disabled by recipient"
				break
			}
			active, identity, err := activeHuman(ctx, tx, d.PrincipalID)
			if err != nil {
				return nil, err
			}
			if !active {
				cancel = "recipient is not an active human"
			}
			recipient = identity
		case !saved || strings.TrimSpace(address) == "" || !toggled:
			cancel = "channel disabled by recipient"
		}
		if cancel == "" && s.CanRead != nil && d.Kind == KindPush && strings.HasPrefix(d.SourceRef, "event:") &&
			!s.CanRead(ctx, d.PrincipalID, strings.TrimPrefix(d.SourceRef, "event:")) {
			// Access changed since the push was queued: hand out nothing.
			cancel = "recipient can no longer read the ask"
		}
		if cancel != "" {
			// The payload is dropped with the delivery: nothing it held is
			// handed out or kept once it can no longer be delivered.
			if _, err := tx.ExecContext(ctx, `UPDATE deliveries SET status='canceled', payload_json='{}', last_error=?, updated_at=? WHERE delivery_id=?`, cancel, stamp(now), id); err != nil {
				return nil, err
			}
			continue
		}
		token := newID("dct_")
		expires := now.Add(lease)
		// Re-check claimability in the write so a concurrent claimer can never
		// hold the same live lease.
		result, err := tx.ExecContext(ctx, `UPDATE deliveries SET status='claimed', attempts=attempts+1, claimed_by=?, claim_token=?, lease_expires_at=?, updated_at=?
			WHERE delivery_id=? AND ((status='pending' AND next_attempt_at<=?) OR (status='claimed' AND lease_expires_at<=?))`,
			strings.TrimSpace(claimer), token, stamp(expires), stamp(now), id, stamp(now), stamp(now))
		if err != nil {
			return nil, err
		}
		if n, _ := result.RowsAffected(); n == 0 {
			continue
		}
		d.Status = StatusClaimed
		d.Attempt++
		d.ClaimToken = token
		d.LeaseExpiresAt = stamp(expires)
		d.Address = address
		d.Recipient = recipient
		d.UpdatedAt = stamp(now)
		out = append(out, d)
	}
	return out, tx.Commit()
}

// Backoff is the delay before attempt n+1 after n failed attempts.
func Backoff(attempts int) time.Duration {
	if attempts < 1 {
		attempts = 1
	}
	d := baseBackoff
	for i := 1; i < attempts && d < maxBackoff; i++ {
		d *= 2
	}
	if d > maxBackoff {
		d = maxBackoff
	}
	return d
}

// RecordReceipt closes the current claim. A delivered outcome is final; a
// retryable failure returns the row to pending after Backoff until
// max_attempts is spent, then it fails. A skipped outcome closes the row as
// canceled without a failure: the adapter chose not to send (for example the
// recipient muted this kind outside the workspace) and appends no receipt. Receipts are appended to
// delivery_receipts, never to the event log. The caller must hold the claim
// token; the claimer identity is checked by the caller against the channel.
// A retry of the receipt that already delivered or failed this attempt
// returns that row and does not append another receipt. The claim token
// stays on the row so that retry can be recognized; callers do not receive it.
func (s Store) RecordReceipt(ctx context.Context, deliveryID, claimToken, channel, outcomeName string, errText string, retryable bool, recordedBy string) (Delivery, error) {
	deliveryID = strings.TrimSpace(deliveryID)
	claimToken = strings.TrimSpace(claimToken)
	if deliveryID == "" || claimToken == "" || (outcomeName != ReceiptDelivered && outcomeName != ReceiptFailed && outcomeName != ReceiptSkipped) {
		return Delivery{}, ErrInvalid
	}
	if len(errText) > maxErrorLength {
		errText = errText[:maxErrorLength]
	}
	now := s.now()
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return Delivery{}, err
	}
	defer func() { _ = tx.Rollback() }()
	d, err := scanDelivery(tx.QueryRowContext(ctx, `SELECT `+deliveryColumns+` FROM deliveries WHERE delivery_id=?`, deliveryID))
	if err != nil {
		return Delivery{}, err
	}
	if d.Channel != channel {
		// Do not confirm that another channel's delivery exists.
		return Delivery{}, ErrNotFound
	}
	if d.Status == StatusCanceled && d.ClaimToken == claimToken {
		if outcomeName != ReceiptSkipped {
			// Canceled under this claim (skipped, or the recipient turned
			// the channel off): a different outcome must not read as success.
			return Delivery{}, ErrConflict
		}
		d.ClaimToken = ""
		return d, nil
	}
	if (d.Status == StatusDelivered || d.Status == StatusFailed) && d.ClaimToken == claimToken {
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM delivery_receipts WHERE delivery_id=? AND attempt=?`, deliveryID, d.Attempt).Scan(&n); err != nil {
			return Delivery{}, err
		}
		if n > 0 {
			d.ClaimToken = ""
			return d, nil
		}
	}
	if d.Status != StatusClaimed || d.ClaimToken != claimToken {
		return Delivery{}, ErrConflict
	}
	if outcomeName == ReceiptSkipped {
		if errText == "" {
			errText = "skipped by adapter"
		}
		if _, err := tx.ExecContext(ctx, `UPDATE deliveries SET status='canceled', lease_expires_at='', last_error=?, updated_at=? WHERE delivery_id=?`, errText, stamp(now), deliveryID); err != nil {
			return Delivery{}, fmt.Errorf("record skipped delivery: %w", err)
		}
		updated, err := scanDelivery(tx.QueryRowContext(ctx, `SELECT `+deliveryColumns+` FROM deliveries WHERE delivery_id=?`, deliveryID))
		if err != nil {
			return Delivery{}, err
		}
		updated.ClaimToken = ""
		return updated, tx.Commit()
	}
	outcome := OutcomeDelivered
	switch {
	case outcomeName == ReceiptDelivered:
		_, err = tx.ExecContext(ctx, `UPDATE deliveries SET status='delivered', lease_expires_at='', last_error='', delivered_at=?, updated_at=? WHERE delivery_id=?`, stamp(now), stamp(now), deliveryID)
	case retryable && d.Attempt < d.MaxAttempts:
		outcome = OutcomeFailed
		_, err = tx.ExecContext(ctx, `UPDATE deliveries SET status='pending', claim_token='', lease_expires_at='', last_error=?, next_attempt_at=?, updated_at=? WHERE delivery_id=?`, errText, stamp(now.Add(Backoff(d.Attempt))), stamp(now), deliveryID)
	default:
		outcome = OutcomeFailed
		_, err = tx.ExecContext(ctx, `UPDATE deliveries SET status='failed', lease_expires_at='', last_error=?, failed_at=?, updated_at=? WHERE delivery_id=?`, errText, stamp(now), stamp(now), deliveryID)
	}
	if err != nil {
		return Delivery{}, fmt.Errorf("record delivery receipt: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO delivery_receipts(delivery_id, outcome, attempt, error, recorded_by, created_at) VALUES(?,?,?,?,?,?)`,
		deliveryID, outcome, d.Attempt, errText, strings.TrimSpace(recordedBy), stamp(now)); err != nil {
		return Delivery{}, fmt.Errorf("append delivery receipt: %w", err)
	}
	updated, err := scanDelivery(tx.QueryRowContext(ctx, `SELECT `+deliveryColumns+` FROM deliveries WHERE delivery_id=?`, deliveryID))
	if err != nil {
		return Delivery{}, err
	}
	updated.ClaimToken = ""
	return updated, tx.Commit()
}

// ListForPrincipal returns the principal's own deliveries, newest first, with
// their receipts. One seek on idx_deliveries_principal, bounded by limit.
func (s Store) ListForPrincipal(ctx context.Context, principalID string, limit int) ([]Delivery, map[string][]Receipt, error) {
	if limit <= 0 || limit > MaxListLimit {
		limit = MaxListLimit
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT `+deliveryColumns+` FROM deliveries WHERE principal_id=? ORDER BY created_at DESC, delivery_id DESC LIMIT ?`, strings.TrimSpace(principalID), limit)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	out := []Delivery{}
	for rows.Next() {
		d, err := scanDelivery(rows)
		if err != nil {
			return nil, nil, err
		}
		d.ClaimToken = ""
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	rows.Close()
	receipts := map[string][]Receipt{}
	if len(out) == 0 {
		return out, receipts, nil
	}
	ids := make([]string, 0, len(out))
	for _, d := range out {
		ids = append(ids, d.DeliveryID)
	}
	encoded, _ := json.Marshal(ids)
	// One query for every listed delivery: a seek per id on
	// idx_delivery_receipts_delivery, newest ten receipts each.
	rrows, err := s.DB.QueryContext(ctx, `SELECT delivery_id, outcome, attempt, error, created_at FROM (
			SELECT delivery_id, outcome, attempt, error, created_at, ROW_NUMBER() OVER (PARTITION BY delivery_id ORDER BY receipt_id DESC) AS n
			FROM delivery_receipts WHERE delivery_id IN (SELECT value FROM json_each(?)))
		WHERE n<=10 ORDER BY delivery_id, n`, string(encoded))
	if err != nil {
		return nil, nil, err
	}
	defer rrows.Close()
	for rrows.Next() {
		var r Receipt
		if err := rrows.Scan(&r.DeliveryID, &r.Outcome, &r.Attempt, &r.Error, &r.CreatedAt); err != nil {
			return nil, nil, err
		}
		receipts[r.DeliveryID] = append(receipts[r.DeliveryID], r)
	}
	if err := rrows.Err(); err != nil {
		return nil, nil, err
	}
	return out, receipts, nil
}

func (s Store) Settings(ctx context.Context, principalID string) ([]ChannelSetting, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT channel, address, push_enabled, digest_enabled, updated_at FROM notification_channel_settings WHERE principal_id=? ORDER BY channel LIMIT ?`, strings.TrimSpace(principalID), MaxChannelsPerHuman)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ChannelSetting{}
	for rows.Next() {
		var c ChannelSetting
		var push, digest int
		if err := rows.Scan(&c.Channel, &c.Address, &push, &digest, &c.UpdatedAt); err != nil {
			return nil, err
		}
		c.PushEnabled, c.DigestEnabled = push == 1, digest == 1
		out = append(out, c)
	}
	return out, rows.Err()
}

// ReplaceSettings replaces the principal's channel list. Channels must be
// valid names; addresses are opaque channel identities, bounded in size.
func (s Store) ReplaceSettings(ctx context.Context, principalID string, settings []ChannelSetting) ([]ChannelSetting, error) {
	principalID = strings.TrimSpace(principalID)
	if principalID == "" || len(settings) > MaxChannelsPerHuman {
		return nil, ErrInvalid
	}
	seen := map[string]bool{}
	for i := range settings {
		settings[i].Channel = strings.TrimSpace(settings[i].Channel)
		settings[i].Address = strings.TrimSpace(settings[i].Address)
		if !ValidChannel(settings[i].Channel) || seen[settings[i].Channel] || len(settings[i].Address) > maxAddressLength || strings.ContainsAny(settings[i].Address, "\x00\r\n") {
			return nil, ErrInvalid
		}
		seen[settings[i].Channel] = true
	}
	now := stamp(s.now())
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `DELETE FROM notification_channel_settings WHERE principal_id=?`, principalID); err != nil {
		return nil, err
	}
	for _, c := range settings {
		if _, err := tx.ExecContext(ctx, `INSERT INTO notification_channel_settings(principal_id, channel, address, push_enabled, digest_enabled, updated_at) VALUES(?,?,?,?,?,?)`,
			principalID, c.Channel, c.Address, boolInt(c.PushEnabled), boolInt(c.DigestEnabled), now); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.Settings(ctx, principalID)
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func nonNil(m map[string]any) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	return m
}

// DeclaredChannels lists channels delivered by at least one active adapter.
func (s Store) DeclaredChannels(ctx context.Context) ([]string, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT DISTINCT channel FROM (`+activeDeclarations+`) ORDER BY channel LIMIT 64`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
