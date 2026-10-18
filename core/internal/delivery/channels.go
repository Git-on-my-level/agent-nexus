package delivery

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
)

const (
	// AddressingSettings channels deliver to the address a person saves in
	// notification settings and stay off until they do.
	AddressingSettings = "settings"
	// AddressingPrincipal channels need no address: they are on for every
	// human principal unless that principal saves both toggles off, and the
	// adapter resolves the recipient itself.
	AddressingPrincipal = "principal"

	maxLabelLength = 80
)

// ErrDeclarationConflict reports a channel whose active declarations would
// disagree on addressing or kinds.
var ErrDeclarationConflict = errors.New("channel is already declared with different addressing or kinds")

// ChannelOptions is how one channel accepts deliveries. Every active
// declaration of a channel agrees on Addressing and Kinds.
type ChannelOptions struct {
	Addressing string   `json:"addressing"`
	Kinds      []string `json:"kinds"`
	Label      string   `json:"label,omitempty"`
}

// Accepts reports whether the channel takes deliveries of kind.
func (o ChannelOptions) Accepts(kind string) bool {
	for _, k := range o.Kinds {
		if k == kind {
			return true
		}
	}
	return false
}

// NormalizeChannelOptions validates declaration options and applies the
// defaults: settings addressing, both kinds, no label.
func NormalizeChannelOptions(addressing string, kinds []string, label string) (ChannelOptions, error) {
	out := ChannelOptions{Addressing: strings.TrimSpace(addressing), Label: strings.TrimSpace(label)}
	if out.Addressing == "" {
		out.Addressing = AddressingSettings
	}
	if out.Addressing != AddressingSettings && out.Addressing != AddressingPrincipal {
		return ChannelOptions{}, fmt.Errorf("%w: addressing must be settings or principal", ErrInvalid)
	}
	if len(out.Label) > maxLabelLength || strings.ContainsAny(out.Label, "\x00\r\n") {
		return ChannelOptions{}, fmt.Errorf("%w: label is too long", ErrInvalid)
	}
	if len(kinds) == 0 {
		kinds = []string{KindPush, KindDigest}
	}
	seen := map[string]bool{}
	for _, k := range kinds {
		k = strings.TrimSpace(k)
		if (k != KindPush && k != KindDigest) || seen[k] {
			return ChannelOptions{}, fmt.Errorf("%w: kinds must be push and/or digest", ErrInvalid)
		}
		seen[k] = true
	}
	// Canonical order keeps stored values comparable.
	for _, k := range []string{KindPush, KindDigest} {
		if seen[k] {
			out.Kinds = append(out.Kinds, k)
		}
	}
	return out, nil
}

func encodeKinds(kinds []string) string { return strings.Join(kinds, ",") }

func decodeKinds(raw string) []string {
	out := []string{}
	for _, k := range strings.Split(raw, ",") {
		if k = strings.TrimSpace(k); k == KindPush || k == KindDigest {
			out = append(out, k)
		}
	}
	return out
}

// activeDeclarations is every active channel declaration: channels of
// host-enrolled adapters, and the one channel a grant-created agent may
// declare. Both branches are bounded by the declaration tables, which are
// small and indexed by adapter or agent.
const activeDeclarations = `SELECT a.agent_id AS agent_id, c.adapter AS source, c.channel AS channel, c.addressing AS addressing, c.kinds AS kinds, c.label AS label
	FROM adapter_channels c JOIN series_adapters a ON a.name=c.adapter WHERE a.revoked_at IS NULL AND a.deleted_at IS NULL
	UNION ALL SELECT g.agent_id, 'grant:'||g.agent_id, g.channel, g.addressing, g.kinds, g.label
	FROM grant_channel_declarations g JOIN agents ag ON ag.id=g.agent_id WHERE g.revoked_at IS NULL AND ag.revoked_at IS NULL`

type queryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

type execQueryer interface {
	queryer
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

// ChannelOptions returns the options of every declared channel, at most 64.
func (s Store) ChannelOptions(ctx context.Context) (map[string]ChannelOptions, error) {
	return channelOptions(ctx, s.DB)
}

func channelOptions(ctx context.Context, q queryer) (map[string]ChannelOptions, error) {
	rows, err := q.QueryContext(ctx, `SELECT channel, addressing, kinds, label FROM (`+activeDeclarations+`) ORDER BY channel, label DESC LIMIT 256`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]ChannelOptions{}
	for rows.Next() {
		var channel, addressing, kinds, label string
		if err := rows.Scan(&channel, &addressing, &kinds, &label); err != nil {
			return nil, err
		}
		if _, ok := out[channel]; ok {
			continue
		}
		if len(out) >= 64 {
			break
		}
		out[channel] = ChannelOptions{Addressing: addressing, Kinds: decodeKinds(kinds), Label: label}
	}
	return out, rows.Err()
}

// checkChannelAgreement rejects options that disagree with another active
// declaration of the same channel. source names the declaration being
// written so it does not conflict with its own previous row.
func checkChannelAgreement(ctx context.Context, q queryer, channel, source string, opts ChannelOptions) error {
	rows, err := q.QueryContext(ctx, `SELECT addressing, kinds FROM (`+activeDeclarations+`) WHERE channel=? AND source<>? LIMIT 64`, channel, source)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var addressing, kinds string
		if err := rows.Scan(&addressing, &kinds); err != nil {
			return err
		}
		if addressing != opts.Addressing || kinds != encodeKinds(opts.Kinds) {
			return fmt.Errorf("%w: %s", ErrDeclarationConflict, channel)
		}
	}
	return rows.Err()
}

// SetAdapterChannelsTx records the channels a host adapter declaration
// delivers and how they accept deliveries.
func SetAdapterChannelsTx(ctx context.Context, tx execQueryer, adapter string, channels []string, opts ChannelOptions) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM adapter_channels WHERE adapter=?`, adapter); err != nil {
		return err
	}
	for _, c := range channels {
		if err := checkChannelAgreement(ctx, tx, c, adapter, opts); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO adapter_channels(adapter, channel, addressing, kinds, label) VALUES(?,?,?,?,?)`,
			adapter, c, opts.Addressing, encodeKinds(opts.Kinds), opts.Label); err != nil {
			return err
		}
	}
	return nil
}

// GrantDeclaration is the one channel an agent created by an identity grant
// may deliver. An empty Channel revokes the agent's declaration.
type GrantDeclaration struct {
	Channel    string   `json:"channel"`
	Addressing string   `json:"addressing,omitempty"`
	Kinds      []string `json:"kinds,omitempty"`
	Label      string   `json:"label,omitempty"`
}

// ValidateGrantDeclaration checks a grant's declaration without writing it.
func ValidateGrantDeclaration(d GrantDeclaration) error {
	channel := strings.TrimSpace(d.Channel)
	if channel == "" {
		return nil
	}
	if !ValidChannel(channel) {
		return fmt.Errorf("%w: delivers channel is invalid", ErrInvalid)
	}
	_, err := NormalizeChannelOptions(d.Addressing, d.Kinds, d.Label)
	return err
}

// UpsertGrantDeclarationTx writes or revokes the declaration owned by a
// grant-created agent. The caller has verified the grant and created the
// agent in the same transaction. Revoking the agent itself disables the
// declaration through activeDeclarations; it is never undone here.
func UpsertGrantDeclarationTx(ctx context.Context, tx execQueryer, agentID, issuer string, d GrantDeclaration, now string) error {
	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		return ErrInvalid
	}
	channel := strings.TrimSpace(d.Channel)
	if channel == "" {
		_, err := tx.ExecContext(ctx, `UPDATE grant_channel_declarations SET revoked_at=COALESCE(revoked_at,?), updated_at=? WHERE agent_id=?`, now, now, agentID)
		return err
	}
	if !ValidChannel(channel) {
		return fmt.Errorf("%w: delivers channel is invalid", ErrInvalid)
	}
	opts, err := NormalizeChannelOptions(d.Addressing, d.Kinds, d.Label)
	if err != nil {
		return err
	}
	if err := checkChannelAgreement(ctx, tx, channel, "grant:"+agentID, opts); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO grant_channel_declarations(agent_id, issuer, channel, addressing, kinds, label, created_at, updated_at, revoked_at)
		VALUES(?,?,?,?,?,?,?,?,NULL)
		ON CONFLICT(agent_id) DO UPDATE SET issuer=excluded.issuer, channel=excluded.channel, addressing=excluded.addressing, kinds=excluded.kinds, label=excluded.label, updated_at=excluded.updated_at, revoked_at=NULL`,
		agentID, strings.TrimSpace(issuer), channel, opts.Addressing, encodeKinds(opts.Kinds), opts.Label, now, now)
	return err
}

// Recipient identifies the human behind a principal-addressed delivery by
// the external identity grant that signed them in.
type Recipient struct {
	ExternalIssuer  string `json:"external_issuer"`
	ExternalSubject string `json:"external_subject"`
}

type rowQueryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

// activeHuman reports whether principalID is an active human principal and,
// for humans signed in through an external identity grant, that identity.
// One seek on idx_agents_actor (agents.actor_id is unique).
func activeHuman(ctx context.Context, q rowQueryer, principalID string) (bool, *Recipient, error) {
	var method, issuer, subject sql.NullString
	err := q.QueryRowContext(ctx, `SELECT json_extract(metadata_json,'$.auth_method'), json_extract(metadata_json,'$.external_issuer'), json_extract(metadata_json,'$.external_subject')
		FROM agents WHERE actor_id=? AND revoked_at IS NULL AND json_extract(metadata_json,'$.principal_kind')='human' LIMIT 1`, principalID).Scan(&method, &issuer, &subject)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil, nil
	}
	if err != nil {
		return false, nil, err
	}
	if method.String != "external_grant" || strings.TrimSpace(issuer.String) == "" || strings.TrimSpace(subject.String) == "" {
		return true, nil, nil
	}
	return true, &Recipient{ExternalIssuer: issuer.String, ExternalSubject: subject.String}, nil
}

// principalRecipients lists active human principals for whom a
// principal-addressed channel is on: no saved setting, or a saved setting
// whose toggle condition holds. One walk of idx_agents_active_humans with a
// primary-key probe per human, bounded by limit.
func principalRecipients(ctx context.Context, q queryer, channel, enabled string, limit int) ([]string, error) {
	if limit <= 0 {
		return nil, nil
	}
	rows, err := q.QueryContext(ctx, `SELECT DISTINCT a.actor_id FROM agents a INDEXED BY idx_agents_active_humans
		LEFT JOIN notification_channel_settings s ON s.principal_id=a.actor_id AND s.channel=?
		WHERE a.revoked_at IS NULL AND json_extract(a.metadata_json,'$.principal_kind')='human' AND (s.principal_id IS NULL OR `+enabled+`)
		ORDER BY a.actor_id LIMIT ?`, channel, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func sortedChannels(opts map[string]ChannelOptions) []string {
	out := make([]string, 0, len(opts))
	for c := range opts {
		out = append(out, c)
	}
	sort.Strings(out)
	return out
}
