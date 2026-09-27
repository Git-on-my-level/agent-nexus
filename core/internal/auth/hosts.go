package auth

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
)

var (
	ErrHostNotFound         = errors.New("not_found")
	ErrHostRevoked          = errors.New("host_revoked")
	ErrHostSlugTaken        = errors.New("host_slug_taken")
	ErrAgentExcluded        = errors.New("agent_excluded")
	ErrAgentHandleTaken     = errors.New("agent_handle_taken")
	ErrAdoptionProofInvalid = errors.New("adoption_proof_invalid")
	ErrAdoptionConflict     = errors.New("adoption_conflict")
	ErrEnrollmentPending    = errors.New("enrollment_pending")
	ErrEnrollmentDenied     = errors.New("enrollment_denied")
	ErrEnrollmentExpired    = errors.New("enrollment_expired")
	ErrEnrollmentConsumed   = errors.New("enrollment_consumed")
)
var hostSlugPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
var agentNamePattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)

func (s *Store) hostProofSkew() time.Duration {
	if s.maxAssertionSkew > 0 && s.maxAssertionSkew < 5*time.Minute {
		return s.maxAssertionSkew
	}
	return 5 * time.Minute
}
func validSlug(s string) bool      { return len(s) <= 63 && hostSlugPattern.MatchString(s) }
func validAgentName(s string) bool { return agentNamePattern.MatchString(s) }
func hostNow() string              { return time.Now().UTC().Format(time.RFC3339Nano) }
func hostSecret() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

const hostUserCodeAlphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

func hostUserCode() (string, error) {
	var random [5]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", err
	}
	var bits uint64
	for _, b := range random {
		bits = (bits << 8) | uint64(b)
	}
	var chars [9]byte
	for i := 7; i >= 0; i-- {
		position := i
		if i >= 4 {
			position++
		}
		chars[position] = hostUserCodeAlphabet[bits&31]
		bits >>= 5
	}
	chars[4] = '-'
	return string(chars[:]), nil
}
func hostKey(raw string) (ed25519.PublicKey, error) {
	k, err := base64.StdEncoding.DecodeString(raw)
	if err != nil || len(k) != ed25519.PublicKeySize {
		return nil, ErrInvalidRequest
	}
	return ed25519.PublicKey(k), nil
}
func hostSignature(key ed25519.PublicKey, msg, sig string) bool {
	b, e := base64.StdEncoding.DecodeString(sig)
	return e == nil && ed25519.Verify(key, []byte(msg), b)
}
func tokenHash(s string) string { return hashToken(s) }

// Host and HostAgent carry only public identity data. Private keys and poll credentials never leave storage.
type Host struct {
	ID                 string      `json:"id"`
	Ref                string      `json:"ref"`
	Handle             string      `json:"handle"`
	Slug               string      `json:"slug"`
	DisplayName        string      `json:"display_name"`
	OSUser             string      `json:"os_user"`
	Hostname           string      `json:"hostname"`
	DiscoveredAdapters []string    `json:"discovered_adapters"`
	KeyID              string      `json:"key_id"`
	ExcludedNames      []string    `json:"excluded_names"`
	Agents             []HostAgent `json:"agents"`
	CreatedAt          string      `json:"created_at"`
	RevokedAt          *string     `json:"revoked_at"`
}
type HostAgent struct {
	ID               string  `json:"id"`
	Ref              string  `json:"ref"`
	ActorID          string  `json:"actor_id"`
	HostID           *string `json:"host_id"`
	HostSlug         *string `json:"host_slug"`
	Name             string  `json:"name"`
	Handle           string  `json:"handle"`
	DisplayName      string  `json:"display_name"`
	IdentityKind     string  `json:"identity_kind"`
	State            string  `json:"state"`
	BridgeOnline     bool    `json:"bridge_online"`
	CurrentCardRef   *string `json:"current_card_ref"`
	CurrentCardTitle *string `json:"current_card_title"`
	LastProgressNote *string `json:"last_progress_note"`
	LastProgressAt   *string `json:"last_progress_at"`
	ActiveRun        any     `json:"active_run"`
	OpenAsksCount    int     `json:"open_asks_count"`
	LastSignalAt     *string `json:"last_signal_at"`
	RevokedAt        *string `json:"revoked_at"`
}
type AdoptionProof struct {
	AgentID   string `json:"agent_id"`
	KeyID     string `json:"key_id"`
	AgentName string `json:"agent_name"`
	Signature string `json:"signature"`
}
type HostEnrollmentInput struct {
	PublicKey          string          `json:"public_key"`
	RequestedSlug      string          `json:"requested_slug"`
	OSUser             string          `json:"os_user"`
	Hostname           string          `json:"hostname"`
	DiscoveredAdapters []string        `json:"discovered_adapters"`
	RequestNonce       string          `json:"request_nonce"`
	Adoptions          []AdoptionProof `json:"adoptions"`
	EnrollmentToken    string          `json:"enrollment_token"`
	Signature          string          `json:"signature"`
}
type HostEnrollment struct {
	ID                 string   `json:"id"`
	UserCode           string   `json:"user_code,omitempty"`
	RequestedSlug      string   `json:"requested_slug"`
	OSUser             string   `json:"os_user"`
	Hostname           string   `json:"hostname"`
	DiscoveredAdapters []string `json:"discovered_adapters"`
	AdoptionNames      []string `json:"adoption_names"`
	RequestingIP       string   `json:"requesting_ip,omitempty"`
	Status             string   `json:"status"`
	ExpiresAt          string   `json:"expires_at"`
	CreatedAt          string   `json:"created_at"`
}
type EnrollmentStart struct {
	EnrollmentID        string `json:"enrollment_id"`
	UserCode            string `json:"user_code"`
	PollToken           string `json:"poll_token"`
	PollIntervalSeconds int    `json:"poll_interval_seconds"`
	ExpiresAt           string `json:"expires_at"`
}
type HostEnrollmentToken struct {
	ID         string  `json:"id"`
	Label      string  `json:"label"`
	CreatedAt  string  `json:"created_at"`
	ExpiresAt  string  `json:"expires_at"`
	ConsumedAt *string `json:"consumed_at"`
	RevokedAt  *string `json:"revoked_at"`
}

func validateEnrollment(in HostEnrollmentInput) (ed25519.PublicKey, error) {
	k, err := hostKey(in.PublicKey)
	if err != nil {
		return nil, ErrInvalidRequest
	}
	if !validSlug(in.RequestedSlug) || strings.TrimSpace(in.OSUser) == "" || strings.TrimSpace(in.Hostname) == "" || in.DiscoveredAdapters == nil || in.Adoptions == nil {
		return nil, ErrInvalidRequest
	}
	nonce, err := base64.RawURLEncoding.DecodeString(in.RequestNonce)
	if err != nil || len(nonce) < 16 {
		return nil, ErrInvalidRequest
	}
	seen := map[string]bool{}
	for _, n := range in.DiscoveredAdapters {
		if !validAgentName(n) || seen[n] {
			return nil, ErrInvalidRequest
		}
		seen[n] = true
	}
	seen = map[string]bool{}
	for _, a := range in.Adoptions {
		if !validAgentName(a.AgentName) || seen[a.AgentName] || a.AgentID == "" || a.KeyID == "" || a.Signature == "" {
			return nil, ErrInvalidRequest
		}
		seen[a.AgentName] = true
	}
	return k, nil
}
func (s *Store) verifyAdoptions(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, in HostEnrollmentInput) error {
	for _, a := range in.Adoptions {
		var raw string
		var revoked, agentRevoked sql.NullString
		var kind string
		err := q.QueryRowContext(ctx, `SELECT k.public_key,k.revoked_at,p.revoked_at,`+principalKindExpr("p")+` FROM agent_keys k JOIN agents p ON p.id=k.agent_id WHERE k.id=? AND p.id=?`, a.KeyID, a.AgentID).Scan(&raw, &revoked, &agentRevoked, &kind)
		if err != nil || revoked.Valid || agentRevoked.Valid || kind != string(PrincipalKindAgent) {
			return ErrAdoptionProofInvalid
		}
		var count int
		if err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM host_agents WHERE agent_id=?`, a.AgentID).Scan(&count); err != nil {
			return err
		}
		if count > 0 {
			return ErrAdoptionConflict
		}
		key, err := hostKey(raw)
		if err != nil {
			return ErrAdoptionProofInvalid
		}
		msg := "anx-host-adopt|" + in.RequestNonce + "|" + in.PublicKey + "|" + a.AgentID + "|" + a.AgentName
		if !hostSignature(key, msg, a.Signature) {
			return ErrAdoptionProofInvalid
		}
	}
	return nil
}
func expireHostEnrollmentsTx(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `UPDATE host_enrollments SET status='expired' WHERE status IN ('pending','approved') AND expires_at<=?`, hostNow())
	return err
}
func (s *Store) slugAvailable(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, slug, except string) error {
	var count int
	err := q.QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM hosts WHERE slug=?) + (SELECT COUNT(*) FROM host_enrollments WHERE requested_slug=? AND status='approved' AND id<>? AND expires_at>?)`, slug, slug, except, hostNow()).Scan(&count)
	if err != nil {
		return err
	}
	if count > 0 {
		return ErrHostSlugTaken
	}
	return nil
}
func (s *Store) StartHostEnrollment(ctx context.Context, in HostEnrollmentInput, ip string) (EnrollmentStart, error) {
	if _, err := validateEnrollment(in); err != nil {
		return EnrollmentStart{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return EnrollmentStart{}, err
	}
	defer tx.Rollback()
	if err := expireHostEnrollmentsTx(ctx, tx); err != nil {
		return EnrollmentStart{}, err
	}
	if err := s.slugAvailable(ctx, tx, in.RequestedSlug, ""); err != nil {
		return EnrollmentStart{}, err
	}
	if err := s.verifyAdoptions(ctx, tx, in); err != nil {
		return EnrollmentStart{}, err
	}
	poll, err := hostSecret()
	if err != nil {
		return EnrollmentStart{}, err
	}
	code, err := hostUserCode()
	if err != nil {
		return EnrollmentStart{}, err
	}
	id := "henr_" + uuid.NewString()
	expires := time.Now().UTC().Add(10 * time.Minute).Format(time.RFC3339Nano)
	adapters, _ := json.Marshal(in.DiscoveredAdapters)
	adoptions, _ := json.Marshal(in.Adoptions)
	_, err = tx.ExecContext(ctx, `INSERT INTO host_enrollments(id,user_code,poll_token_hash,public_key,requested_slug,os_user,hostname,discovered_adapters_json,request_nonce,adoptions_json,requesting_ip,status,created_at,expires_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,'pending',?,?)`, id, code, tokenHash(poll), in.PublicKey, in.RequestedSlug, in.OSUser, in.Hostname, string(adapters), in.RequestNonce, string(adoptions), ip, hostNow(), expires)
	if err != nil {
		return EnrollmentStart{}, err
	}
	if err = s.recordAuthAuditEventTx(ctx, tx, AuthAuditEventInput{EventType: "host_enroll_started", Metadata: map[string]any{"enrollment_id": id, "requested_slug": in.RequestedSlug, "requesting_ip": ip}}); err != nil {
		return EnrollmentStart{}, err
	}
	if err = tx.Commit(); err != nil {
		return EnrollmentStart{}, err
	}
	return EnrollmentStart{EnrollmentID: id, UserCode: code, PollToken: poll, PollIntervalSeconds: 3, ExpiresAt: expires}, nil
}
func readEnrollment(row *sql.Row, admin bool) (HostEnrollment, error) {
	var e HostEnrollment
	var adapters, adoptions, ip, code string
	err := row.Scan(&e.ID, &code, &e.RequestedSlug, &e.OSUser, &e.Hostname, &adapters, &adoptions, &ip, &e.Status, &e.ExpiresAt, &e.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return e, ErrHostNotFound
	}
	if err != nil {
		return e, err
	}
	json.Unmarshal([]byte(adapters), &e.DiscoveredAdapters)
	var proofs []AdoptionProof
	json.Unmarshal([]byte(adoptions), &proofs)
	e.AdoptionNames = []string{}
	for _, p := range proofs {
		e.AdoptionNames = append(e.AdoptionNames, p.AgentName)
	}
	if admin {
		e.UserCode = code
		e.RequestingIP = ip
	}
	if e.DiscoveredAdapters == nil {
		e.DiscoveredAdapters = []string{}
	}
	return e, nil
}

const enrollmentSelect = `SELECT id,user_code,requested_slug,os_user,hostname,discovered_adapters_json,adoptions_json,requesting_ip,status,expires_at,created_at FROM host_enrollments WHERE id=?`

func (s *Store) PollHostEnrollment(ctx context.Context, id, poll string) (HostEnrollment, error) {
	var hash string
	err := s.db.QueryRowContext(ctx, `SELECT poll_token_hash FROM host_enrollments WHERE id=?`, id).Scan(&hash)
	if errors.Is(err, sql.ErrNoRows) {
		return HostEnrollment{}, ErrHostNotFound
	}
	if err != nil {
		return HostEnrollment{}, err
	}
	if poll == "" || hash != tokenHash(poll) {
		return HostEnrollment{}, ErrInvalidToken
	}
	e, err := readEnrollment(s.db.QueryRowContext(ctx, enrollmentSelect, id), false)
	if err != nil {
		return e, err
	}
	if expired(e.ExpiresAt) && e.Status != "completed" && e.Status != "denied" {
		e.Status = "expired"
	}
	return e, nil
}
func expired(raw string) bool {
	t, e := time.Parse(time.RFC3339Nano, raw)
	return e != nil || !time.Now().UTC().Before(t)
}
func (s *Store) PendingHostEnrollments(ctx context.Context) ([]HostEnrollment, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM host_enrollments WHERE status='pending' AND expires_at>? ORDER BY created_at`, hostNow())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	out := []HostEnrollment{}
	for _, id := range ids {
		e, err := readEnrollment(s.db.QueryRowContext(ctx, enrollmentSelect, id), true)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, nil
}
func (s *Store) DecideHostEnrollment(ctx context.Context, id string, approve bool, admin Principal) (HostEnrollment, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return HostEnrollment{}, err
	}
	defer tx.Rollback()
	if err := expireHostEnrollmentsTx(ctx, tx); err != nil {
		return HostEnrollment{}, err
	}
	e, err := readEnrollment(tx.QueryRowContext(ctx, enrollmentSelect, id), true)
	if err != nil {
		return e, err
	}
	if expired(e.ExpiresAt) {
		return e, ErrEnrollmentExpired
	}
	if e.Status != "pending" {
		return e, ErrEnrollmentConsumed
	}
	if approve {
		if err := s.slugAvailable(ctx, tx, e.RequestedSlug, id); err != nil {
			return e, err
		}
		e.Status = "approved"
	} else {
		e.Status = "denied"
	}
	_, err = tx.ExecContext(ctx, `UPDATE host_enrollments SET status=? WHERE id=? AND status='pending'`, e.Status, id)
	if err != nil {
		return e, err
	}
	event := "host_enroll_denied"
	if approve {
		event = "host_enroll_approved"
	}
	if err = s.recordAuthAuditEventTx(ctx, tx, AuthAuditEventInput{EventType: event, ActorAgentID: admin.AgentID, ActorActorID: admin.ActorID, Metadata: map[string]any{"enrollment_id": id, "slug": e.RequestedSlug}}); err != nil {
		return e, err
	}
	return e, tx.Commit()
}
func (s *Store) CreateHostEnrollmentToken(ctx context.Context, label string, expiry time.Time, admin Principal) (HostEnrollmentToken, string, error) {
	now := time.Now().UTC()
	if strings.TrimSpace(label) == "" || len(label) > 120 || expiry.Before(now.Add(10*time.Minute)) || expiry.After(now.Add(24*time.Hour)) {
		return HostEnrollmentToken{}, "", ErrInvalidRequest
	}
	secret, err := hostSecret()
	if err != nil {
		return HostEnrollmentToken{}, "", err
	}
	t := HostEnrollmentToken{ID: "htok_" + uuid.NewString(), Label: label, CreatedAt: now.Format(time.RFC3339Nano), ExpiresAt: expiry.UTC().Format(time.RFC3339Nano)}
	_, err = s.db.ExecContext(ctx, `INSERT INTO host_enrollment_tokens(id,label,token_hash,created_at,expires_at,created_by_agent_id) VALUES(?,?,?,?,?,?)`, t.ID, t.Label, tokenHash(secret), t.CreatedAt, t.ExpiresAt, admin.AgentID)
	return t, secret, err
}
func (s *Store) ListHostEnrollmentTokens(ctx context.Context) ([]HostEnrollmentToken, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,label,created_at,expires_at,consumed_at,revoked_at FROM host_enrollment_tokens ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []HostEnrollmentToken{}
	for rows.Next() {
		var t HostEnrollmentToken
		var c, r sql.NullString
		if err := rows.Scan(&t.ID, &t.Label, &t.CreatedAt, &t.ExpiresAt, &c, &r); err != nil {
			return nil, err
		}
		if c.Valid {
			t.ConsumedAt = &c.String
		}
		if r.Valid {
			t.RevokedAt = &r.String
		}
		out = append(out, t)
	}
	return out, rows.Err()
}
func (s *Store) RevokeHostEnrollmentToken(ctx context.Context, id string) (HostEnrollmentToken, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE host_enrollment_tokens SET revoked_at=? WHERE id=? AND consumed_at IS NULL AND revoked_at IS NULL`, hostNow(), id)
	if err != nil {
		return HostEnrollmentToken{}, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return HostEnrollmentToken{}, ErrHostNotFound
	}
	items, err := s.ListHostEnrollmentTokens(ctx)
	for _, t := range items {
		if t.ID == id {
			return t, err
		}
	}
	return HostEnrollmentToken{}, ErrHostNotFound
}
func (s *Store) CompleteHostEnrollment(ctx context.Context, id, poll, signature string) (Host, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Host{}, err
	}
	defer tx.Rollback()
	var in HostEnrollmentInput
	var hash, status, expiry, adapters, adoptions string
	err = tx.QueryRowContext(ctx, `SELECT poll_token_hash,public_key,requested_slug,os_user,hostname,discovered_adapters_json,request_nonce,adoptions_json,status,expires_at FROM host_enrollments WHERE id=?`, id).Scan(&hash, &in.PublicKey, &in.RequestedSlug, &in.OSUser, &in.Hostname, &adapters, &in.RequestNonce, &adoptions, &status, &expiry)
	if errors.Is(err, sql.ErrNoRows) {
		return Host{}, ErrHostNotFound
	}
	if err != nil {
		return Host{}, err
	}
	if poll == "" || hash != tokenHash(poll) {
		return Host{}, ErrInvalidToken
	}
	if expired(expiry) {
		return Host{}, ErrEnrollmentExpired
	}
	if status == "pending" {
		return Host{}, ErrEnrollmentPending
	}
	if status == "denied" {
		return Host{}, ErrEnrollmentDenied
	}
	if status != "approved" {
		return Host{}, ErrEnrollmentConsumed
	}
	json.Unmarshal([]byte(adapters), &in.DiscoveredAdapters)
	json.Unmarshal([]byte(adoptions), &in.Adoptions)
	key, err := hostKey(in.PublicKey)
	if err != nil {
		return Host{}, err
	}
	if !hostSignature(key, "anx-host-enroll-complete|"+id+"|"+poll, signature) {
		return Host{}, ErrKeyMismatch
	}
	h, err := s.createHostTx(ctx, tx, in, id, true)
	if err != nil {
		return Host{}, err
	}
	_, err = tx.ExecContext(ctx, `UPDATE host_enrollments SET status='completed',completed_host_id=? WHERE id=?`, h.ID, id)
	if err != nil {
		return Host{}, err
	}
	if err = tx.Commit(); err != nil {
		return Host{}, err
	}
	return s.GetHost(ctx, h.ID)
}
func (s *Store) CompleteHeadlessHostEnrollment(ctx context.Context, in HostEnrollmentInput) (Host, error) {
	key, err := validateEnrollment(in)
	if err != nil {
		return Host{}, err
	}
	if !hostSignature(key, "anx-host-headless-enroll|"+in.RequestNonce+"|"+in.RequestedSlug+"|"+in.PublicKey, in.Signature) {
		return Host{}, ErrKeyMismatch
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Host{}, err
	}
	defer tx.Rollback()
	if err := expireHostEnrollmentsTx(ctx, tx); err != nil {
		return Host{}, err
	}
	var id, expiry string
	var consumed, revoked sql.NullString
	err = tx.QueryRowContext(ctx, `SELECT id,expires_at,consumed_at,revoked_at FROM host_enrollment_tokens WHERE token_hash=?`, tokenHash(in.EnrollmentToken)).Scan(&id, &expiry, &consumed, &revoked)
	if err != nil || consumed.Valid || revoked.Valid || expired(expiry) {
		return Host{}, ErrInvalidToken
	}
	h, err := s.createHostTx(ctx, tx, in, "", false)
	if err != nil {
		return Host{}, err
	}
	_, err = tx.ExecContext(ctx, `UPDATE host_enrollment_tokens SET consumed_at=? WHERE id=? AND consumed_at IS NULL`, hostNow(), id)
	if err != nil {
		return Host{}, err
	}
	if err = tx.Commit(); err != nil {
		return Host{}, err
	}
	return s.GetHost(ctx, h.ID)
}
func (s *Store) createHostTx(ctx context.Context, tx *sql.Tx, in HostEnrollmentInput, except string, frozenProofs bool) (Host, error) {
	if err := s.slugAvailable(ctx, tx, in.RequestedSlug, except); err != nil {
		return Host{}, err
	}
	if !frozenProofs {
		if err := s.verifyAdoptions(ctx, tx, in); err != nil {
			return Host{}, err
		}
	} else {
		for _, proof := range in.Adoptions {
			var revoked sql.NullString
			if err := tx.QueryRowContext(ctx, `SELECT revoked_at FROM agents WHERE id=? AND NOT EXISTS(SELECT 1 FROM host_agents WHERE agent_id=?)`, proof.AgentID, proof.AgentID).Scan(&revoked); err != nil || revoked.Valid {
				return Host{}, ErrAdoptionConflict
			}
		}
	}
	id := "host_" + uuid.NewString()
	keyID := "hkey_" + uuid.NewString()
	now := hostNow()
	adapters, _ := json.Marshal(in.DiscoveredAdapters)
	_, err := tx.ExecContext(ctx, `INSERT INTO hosts(id,slug,display_name,os_user,hostname,discovered_adapters_json,created_at) VALUES(?,?,?,?,?,?,?)`, id, in.RequestedSlug, in.RequestedSlug, in.OSUser, in.Hostname, string(adapters), now)
	if err != nil {
		return Host{}, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO host_keys(id,host_id,public_key,created_at) VALUES(?,?,?,?)`, keyID, id, in.PublicKey, now)
	if err != nil {
		return Host{}, err
	}
	for _, a := range in.Adoptions {
		handle := a.AgentName + "." + in.RequestedSlug
		var count int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM agents WHERE username=? AND id<>?`, handle, a.AgentID).Scan(&count); err != nil {
			return Host{}, err
		}
		if count > 0 {
			return Host{}, ErrAgentHandleTaken
		}
		res, err := tx.ExecContext(ctx, `UPDATE agents SET username=?,updated_at=?,metadata_json=json_set(metadata_json,'$.auth_admin',false,'$.auth_method','host_assertion') WHERE id=? AND revoked_at IS NULL`, handle, now, a.AgentID)
		if err != nil {
			return Host{}, err
		}
		n, _ := res.RowsAffected()
		if n != 1 {
			return Host{}, ErrAdoptionConflict
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO host_agents(host_id,name,agent_id,identity_kind) VALUES(?,?,?,'adopted')`, id, a.AgentName, a.AgentID)
		if err != nil {
			return Host{}, err
		}
		_, err = tx.ExecContext(ctx, `UPDATE agent_keys SET revoked_at=? WHERE agent_id=? AND revoked_at IS NULL`, now, a.AgentID)
		if err != nil {
			return Host{}, err
		}
		if err := revokeChildSessions(ctx, tx, a.AgentID, now); err != nil {
			return Host{}, err
		}
		_, err = tx.ExecContext(ctx, `UPDATE actors SET display_name=? WHERE id=(SELECT actor_id FROM agents WHERE id=?)`, a.AgentName+" on "+in.RequestedSlug, a.AgentID)
		if err != nil {
			return Host{}, err
		}
		if err = s.recordAuthAuditEventTx(ctx, tx, AuthAuditEventInput{EventType: "host_agent_adopted", SubjectAgentID: a.AgentID, Metadata: map[string]any{"host_id": id, "name": a.AgentName}}); err != nil {
			return Host{}, err
		}
	}
	if err = s.recordAuthAuditEventTx(ctx, tx, AuthAuditEventInput{EventType: "host_enroll_completed", Metadata: map[string]any{"host_id": id, "slug": in.RequestedSlug}}); err != nil {
		return Host{}, err
	}
	return Host{ID: id, KeyID: keyID}, nil
}
func revokeChildSessions(ctx context.Context, tx *sql.Tx, id, now string) error {
	for _, table := range []string{"auth_access_tokens", "auth_refresh_sessions"} {
		_, err := tx.ExecContext(ctx, `UPDATE `+table+` SET revoked_at=? WHERE agent_id=? AND revoked_at IS NULL`, now, id)
		if err != nil {
			return err
		}
	}
	return nil
}
func (s *Store) GetHost(ctx context.Context, id string) (Host, error) {
	var h Host
	var adapters string
	var revoked, bridgeExpiry sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT h.id,h.slug,h.display_name,h.os_user,h.hostname,h.discovered_adapters_json,h.created_at,h.revoked_at,h.bridge_expires_at,COALESCE((SELECT id FROM host_keys WHERE host_id=h.id ORDER BY created_at DESC LIMIT 1),'') FROM hosts h WHERE h.id=? OR h.slug=?`, id, id).Scan(&h.ID, &h.Slug, &h.DisplayName, &h.OSUser, &h.Hostname, &adapters, &h.CreatedAt, &revoked, &bridgeExpiry, &h.KeyID)
	if errors.Is(err, sql.ErrNoRows) {
		return h, ErrHostNotFound
	}
	if err != nil {
		return h, err
	}
	h.Ref = "host:" + h.ID
	h.Handle = h.Slug
	if revoked.Valid {
		h.RevokedAt = &revoked.String
	}
	json.Unmarshal([]byte(adapters), &h.DiscoveredAdapters)
	if h.DiscoveredAdapters == nil {
		h.DiscoveredAdapters = []string{}
	}
	h.ExcludedNames = []string{}
	h.Agents = []HostAgent{}
	rows, err := s.db.QueryContext(ctx, `SELECT name FROM host_exclusions WHERE host_id=? ORDER BY name`, h.ID)
	if err != nil {
		return h, err
	}
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			rows.Close()
			return h, err
		}
		h.ExcludedNames = append(h.ExcludedNames, n)
	}
	rows.Close()
	rows, err = s.db.QueryContext(ctx, `SELECT ha.name,ha.identity_kind,a.id,a.actor_id,a.username,a.revoked_at FROM host_agents ha JOIN agents a ON a.id=ha.agent_id WHERE ha.host_id=? ORDER BY ha.name`, h.ID)
	if err != nil {
		return h, err
	}
	defer rows.Close()
	for rows.Next() {
		var a HostAgent
		var rev sql.NullString
		if err := rows.Scan(&a.Name, &a.IdentityKind, &a.ID, &a.ActorID, &a.Handle, &rev); err != nil {
			return h, err
		}
		a.Ref = "actor:" + a.ActorID
		a.HostID = &h.ID
		a.HostSlug = &h.Slug
		a.DisplayName = a.Name + " on " + h.Slug
		a.State = "stale"
		a.BridgeOnline = bridgeExpiry.Valid && !expired(bridgeExpiry.String) && h.RevokedAt == nil
		if rev.Valid {
			a.RevokedAt = &rev.String
		}
		h.Agents = append(h.Agents, a)
	}
	return h, rows.Err()
}
func (s *Store) ListHosts(ctx context.Context) ([]Host, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM hosts ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
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
	out := []Host{}
	for _, id := range ids {
		h, err := s.GetHost(ctx, id)
		if err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, nil
}
func (s *Store) VerifyHostProof(ctx context.Context, id, keyID, signedAt, signature, kind string, body []byte) error {
	t, err := time.Parse(time.RFC3339, signedAt)
	if err != nil || time.Since(t) > s.hostProofSkew() || time.Until(t) > s.hostProofSkew() {
		return ErrKeyMismatch
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var raw string
	var rev, kre sql.NullString
	err = tx.QueryRowContext(ctx, `SELECT k.public_key,h.revoked_at,k.revoked_at FROM hosts h JOIN host_keys k ON k.host_id=h.id WHERE (h.id=? OR h.slug=?) AND k.id=?`, id, id, keyID).Scan(&raw, &rev, &kre)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrKeyMismatch
	}
	if err != nil {
		return err
	}
	if rev.Valid || kre.Valid {
		return ErrHostRevoked
	}
	key, err := hostKey(raw)
	if err != nil {
		return ErrKeyMismatch
	}
	digest := sha256.Sum256(body)
	msg := "anx-host-" + kind + "|" + id + "|" + signedAt + "|" + base64.RawURLEncoding.EncodeToString(digest[:])
	if !hostSignature(key, msg, signature) {
		return ErrKeyMismatch
	}
	if err = s.recordAssertionUseTx(ctx, tx, msg, signature, time.Now().UTC()); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Store) PatchHost(ctx context.Context, id string, display *string, excluded *[]string, actor *Principal) (Host, error) {
	h, err := s.GetHost(ctx, id)
	if err != nil {
		return h, err
	}
	if h.RevokedAt != nil {
		return h, ErrHostRevoked
	}
	if display == nil && excluded == nil {
		return h, ErrInvalidRequest
	}
	if display != nil && (strings.TrimSpace(*display) == "" || len(*display) > 120) {
		return h, ErrInvalidRequest
	}
	if excluded != nil {
		seen := map[string]bool{}
		for _, n := range *excluded {
			if !validAgentName(n) || seen[n] {
				return h, ErrInvalidRequest
			}
			seen[n] = true
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return h, err
	}
	defer tx.Rollback()
	if display != nil {
		_, err = tx.ExecContext(ctx, `UPDATE hosts SET display_name=? WHERE id=?`, *display, h.ID)
		if err != nil {
			return h, err
		}
	}
	if excluded != nil {
		old := map[string]bool{}
		rows, err := tx.QueryContext(ctx, `SELECT name FROM host_exclusions WHERE host_id=?`, h.ID)
		if err != nil {
			return h, err
		}
		for rows.Next() {
			var n string
			rows.Scan(&n)
			old[n] = true
		}
		rows.Close()
		_, err = tx.ExecContext(ctx, `DELETE FROM host_exclusions WHERE host_id=?`, h.ID)
		if err != nil {
			return h, err
		}
		for _, n := range *excluded {
			_, err = tx.ExecContext(ctx, `INSERT INTO host_exclusions(host_id,name) VALUES(?,?)`, h.ID, n)
			if err != nil {
				return h, err
			}
			if !old[n] {
				_, err = tx.ExecContext(ctx, `UPDATE auth_access_tokens SET revoked_at=? WHERE agent_id IN (SELECT agent_id FROM host_agents WHERE host_id=? AND name=?) AND revoked_at IS NULL`, hostNow(), h.ID, n)
				if err != nil {
					return h, err
				}
				_, err = tx.ExecContext(ctx, `UPDATE auth_refresh_sessions SET revoked_at=? WHERE agent_id IN (SELECT agent_id FROM host_agents WHERE host_id=? AND name=?) AND revoked_at IS NULL`, hostNow(), h.ID, n)
				if err != nil {
					return h, err
				}
			}
		}
		audit := AuthAuditEventInput{EventType: "host_exclusions_changed", Metadata: map[string]any{"host_id": h.ID, "excluded_names": *excluded}}
		if actor != nil {
			audit.ActorAgentID = actor.AgentID
			audit.ActorActorID = actor.ActorID
		}
		if err = s.recordAuthAuditEventTx(ctx, tx, audit); err != nil {
			return h, err
		}
	}
	if err = tx.Commit(); err != nil {
		return h, err
	}
	return s.GetHost(ctx, h.ID)
}
func (s *Store) RevokeHost(ctx context.Context, id string, admin Principal) (Host, error) {
	h, err := s.GetHost(ctx, id)
	if err != nil {
		return h, err
	}
	if h.RevokedAt != nil {
		return h, nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return h, err
	}
	defer tx.Rollback()
	now := hostNow()
	for _, q := range []string{
		`UPDATE hosts SET revoked_at=? WHERE id=?`,
		`UPDATE host_keys SET revoked_at=? WHERE host_id=? AND revoked_at IS NULL`,
		`UPDATE agents SET revoked_at=? WHERE id IN (SELECT agent_id FROM host_agents WHERE host_id=?) AND revoked_at IS NULL`,
		`UPDATE agent_keys SET revoked_at=? WHERE agent_id IN (SELECT agent_id FROM host_agents WHERE host_id=?) AND revoked_at IS NULL`,
		`UPDATE auth_access_tokens SET revoked_at=? WHERE agent_id IN (SELECT agent_id FROM host_agents WHERE host_id=?) AND revoked_at IS NULL`,
		`UPDATE auth_refresh_sessions SET revoked_at=? WHERE agent_id IN (SELECT agent_id FROM host_agents WHERE host_id=?) AND revoked_at IS NULL`,
	} {
		if _, err = tx.ExecContext(ctx, q, now, h.ID); err != nil {
			return h, err
		}
	}

	if err = s.recordAuthAuditEventTx(ctx, tx, AuthAuditEventInput{EventType: "host_revoked", ActorAgentID: admin.AgentID, ActorActorID: admin.ActorID, Metadata: map[string]any{"host_id": h.ID}}); err != nil {
		return h, err
	}
	if err = tx.Commit(); err != nil {
		return h, err
	}
	return s.GetHost(ctx, h.ID)
}
func (s *Store) IssueHostAgentToken(ctx context.Context, hostID, keyID, name, signedAt, signature string, existingActorID ...string) (HostAgent, TokenBundle, error) {
	if !validAgentName(name) {
		return HostAgent{}, TokenBundle{}, ErrInvalidRequest
	}
	linkedActorID := ""
	if len(existingActorID) > 0 {
		linkedActorID = strings.TrimSpace(existingActorID[0])
	}
	if linkedActorID != "" && !s.allowDevRegisterLinkedActor {
		return HostAgent{}, TokenBundle{}, ErrInvalidRequest
	}
	t, err := time.Parse(time.RFC3339, signedAt)
	if err != nil || time.Since(t) > s.hostProofSkew() || time.Until(t) > s.hostProofSkew() {
		return HostAgent{}, TokenBundle{}, ErrKeyMismatch
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return HostAgent{}, TokenBundle{}, err
	}
	defer tx.Rollback()
	var id, slug, raw string
	var rev, kre sql.NullString
	err = tx.QueryRowContext(ctx, `SELECT h.id,h.slug,h.revoked_at,k.public_key,k.revoked_at FROM hosts h JOIN host_keys k ON k.host_id=h.id WHERE h.id=? AND k.id=?`, hostID, keyID).Scan(&id, &slug, &rev, &raw, &kre)
	if errors.Is(err, sql.ErrNoRows) {
		return HostAgent{}, TokenBundle{}, ErrKeyMismatch
	}
	if err != nil {
		return HostAgent{}, TokenBundle{}, err
	}
	if rev.Valid || kre.Valid {
		return HostAgent{}, TokenBundle{}, ErrHostRevoked
	}
	key, err := hostKey(raw)
	if err != nil {
		return HostAgent{}, TokenBundle{}, ErrKeyMismatch
	}
	msg := "anx-host-agent-token|" + hostID + "|" + keyID + "|" + name + "|" + signedAt
	if !hostSignature(key, msg, signature) {
		return HostAgent{}, TokenBundle{}, ErrKeyMismatch
	}
	var count int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM host_exclusions WHERE host_id=? AND name=?`, id, name).Scan(&count); err != nil {
		return HostAgent{}, TokenBundle{}, err
	}
	if count > 0 {
		return HostAgent{}, TokenBundle{}, ErrAgentExcluded
	}
	if err = s.recordAssertionUseTx(ctx, tx, msg, signature, time.Now().UTC()); err != nil {
		return HostAgent{}, TokenBundle{}, err
	}
	var agentID, actorID, kind string
	var agentRevoked sql.NullString
	err = tx.QueryRowContext(ctx, `SELECT a.id,a.actor_id,ha.identity_kind,a.revoked_at FROM host_agents ha JOIN agents a ON a.id=ha.agent_id WHERE ha.host_id=? AND ha.name=?`, id, name).Scan(&agentID, &actorID, &kind, &agentRevoked)
	if errors.Is(err, sql.ErrNoRows) {
		handle := name + "." + slug
		if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM agents WHERE username=?`, handle).Scan(&count); err != nil {
			return HostAgent{}, TokenBundle{}, err
		}
		if count > 0 {
			return HostAgent{}, TokenBundle{}, ErrAgentHandleTaken
		}
		agentID = "agent_" + uuid.NewString()
		actorID = agentID
		if len(existingActorID) > 0 && strings.TrimSpace(existingActorID[0]) != "" {
			if !s.allowDevRegisterLinkedActor {
				return HostAgent{}, TokenBundle{}, ErrInvalidRequest
			}
			actorID = strings.TrimSpace(existingActorID[0])
			if err := s.ensureExistingActorReadyForAgentLinkTx(ctx, tx, actorID); err != nil {
				return HostAgent{}, TokenBundle{}, err
			}
		}
		kind = "derived"
		now := hostNow()
		meta, _ := principalMetadataJSON(PrincipalKindAgent, "host_assertion", map[string]any{})
		_, err = tx.ExecContext(ctx, `INSERT INTO agents(id,username,actor_id,created_at,updated_at,metadata_json) VALUES(?,?,?,?,?,?)`, agentID, handle, actorID, now, now, meta)
		if err != nil {
			return HostAgent{}, TokenBundle{}, err
		}
		if actorID == agentID {
			actorMeta, _ := actorMetadataJSON(PrincipalKindAgent, "host_assertion", nil)
			_, err = tx.ExecContext(ctx, `INSERT INTO actors(id,display_name,tags_json,created_at,metadata_json) VALUES(?,?,'["agent"]',?,?)`, actorID, name+" on "+slug, now, actorMeta)
			if err != nil {
				return HostAgent{}, TokenBundle{}, err
			}
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO host_agents(host_id,name,agent_id,identity_kind) VALUES(?,?,?,'derived')`, id, name, agentID)
		if err != nil {
			return HostAgent{}, TokenBundle{}, err
		}
		if err = s.recordAuthAuditEventTx(ctx, tx, AuthAuditEventInput{EventType: "derived_agent_created", SubjectAgentID: agentID, SubjectActorID: actorID, Metadata: map[string]any{"host_id": id, "name": name}}); err != nil {
			return HostAgent{}, TokenBundle{}, err
		}
	} else if err != nil {
		return HostAgent{}, TokenBundle{}, err
	} else if agentRevoked.Valid {
		return HostAgent{}, TokenBundle{}, ErrAgentRevoked
	}
	if linkedActorID != "" && actorID != linkedActorID {
		return HostAgent{}, TokenBundle{}, ErrAdoptionConflict
	}
	access, err := hostSecret()
	if err != nil {
		return HostAgent{}, TokenBundle{}, err
	}
	now := time.Now().UTC()
	ttl := s.accessTokenTTL
	if ttl > 15*time.Minute {
		ttl = 15 * time.Minute
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO auth_access_tokens(id,agent_id,token_hash,created_at,expires_at) VALUES(?,?,?,?,?)`, "access_"+uuid.NewString(), agentID, tokenHash(access), now.Format(time.RFC3339Nano), now.Add(ttl).Format(time.RFC3339Nano))
	if err != nil {
		return HostAgent{}, TokenBundle{}, err
	}
	if err = tx.Commit(); err != nil {
		return HostAgent{}, TokenBundle{}, err
	}
	hostIDCopy, slugCopy := id, slug
	a := HostAgent{ID: agentID, Ref: "actor:" + actorID, ActorID: actorID, HostID: &hostIDCopy, HostSlug: &slugCopy, Name: name, Handle: name + "." + slug, DisplayName: name + " on " + slug, IdentityKind: kind, State: "stale"}
	return a, TokenBundle{AccessToken: access, TokenType: "Bearer", ExpiresIn: int64(ttl.Seconds())}, nil
}
func (s *Store) CheckInHostBridge(ctx context.Context, id, instance, checked, expires string) (map[string]string, error) {
	h, err := s.GetHost(ctx, id)
	if err != nil {
		return nil, err
	}
	if h.RevokedAt != nil {
		return nil, ErrHostRevoked
	}
	c, e1 := time.Parse(time.RFC3339, checked)
	e, e2 := time.Parse(time.RFC3339, expires)
	if instance == "" || e1 != nil || e2 != nil || !e.After(c) || e.After(time.Now().UTC().Add(5*time.Minute)) {
		return nil, ErrInvalidRequest
	}
	_, err = s.db.ExecContext(ctx, `UPDATE hosts SET bridge_instance_id=?,bridge_checked_in_at=?,bridge_expires_at=? WHERE id=?`, instance, checked, expires, h.ID)
	if err != nil {
		return nil, err
	}
	return map[string]string{"host_id": h.ID, "bridge_instance_id": instance, "checked_in_at": checked, "expires_at": expires}, nil
}
func (s *Store) IsDerivedAgent(ctx context.Context, id string) (bool, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM host_agents WHERE agent_id=?`, id).Scan(&count)
	return count > 0, err
}
func (s *Store) AgentHost(ctx context.Context, id string) (string, error) {
	var host string
	err := s.db.QueryRowContext(ctx, `SELECT host_id FROM host_agents WHERE agent_id=?`, id).Scan(&host)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrHostNotFound
	}
	return host, err
}

func (s *Store) HostOwnsActor(ctx context.Context, hostID, actorID string) (bool, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM host_agents ha JOIN agents a ON a.id=ha.agent_id JOIN hosts h ON h.id=ha.host_id WHERE h.id=? AND h.revoked_at IS NULL AND a.revoked_at IS NULL AND a.actor_id=?`, hostID, actorID).Scan(&count)
	return count == 1, err
}

func (s *Store) GetDerivedAgent(ctx context.Context, agentID string) (HostAgent, error) {
	hostID, err := s.AgentHost(ctx, agentID)
	if err != nil {
		return HostAgent{}, err
	}
	host, err := s.GetHost(ctx, hostID)
	if err != nil {
		return HostAgent{}, err
	}
	for _, agent := range host.Agents {
		if agent.ID == agentID {
			return agent, nil
		}
	}
	return HostAgent{}, ErrAgentNotFound
}
