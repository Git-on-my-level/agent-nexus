// Package filetransport implements an opt-in, directory-scoped note transport.
// Producers never select credentials, actors, arbitrary routes or executors.
package filetransport

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"agent-nexus-cli/internal/integrations"
)

const MaxEntry = 64 << 10
const MaxPending = 100
const MaxRecords = 10000
const MaxCards = 100
const ReplayCutoff = 25 * 24 * time.Hour
const StaleAfter = 5 * time.Minute

var ErrUnsafe = errors.New("unsafe_file_transport_path")
var ErrCapacity = errors.New("file_transport_capacity")
var ErrConflict = errors.New("file_transport_id_conflict")
var uuidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
var cardIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,256}$`)

func UUID() string {
	var b [16]byte
	if _, e := rand.Read(b[:]); e != nil {
		panic(e)
	}
	b[6] = b[6]&15 | 64
	b[8] = b[8]&63 | 128
	s := hex.EncodeToString(b[:])
	return s[:8] + "-" + s[8:12] + "-" + s[12:16] + "-" + s[16:20] + "-" + s[20:]
}
func ValidUUID(s string) bool { return uuidPattern.MatchString(s) }
func Digest(b []byte) string  { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

type Next struct {
	At   string `json:"at"`
	What string `json:"what"`
}
type Entry struct {
	Version  int      `json:"schema_version"`
	ID       string   `json:"id"`
	Card     string   `json:"card_ref"`
	Text     string   `json:"text"`
	Evidence []string `json:"evidence_refs"`
	Next     *Next    `json:"next,omitempty"`
}
type Card struct {
	ID  string `json:"id"`
	Ref string `json:"ref"`
}
type Binding struct {
	ID        string `json:"directory_uuid"`
	Directory string `json:"directory"`
	Workspace string `json:"workspace_id"`
	BaseURL   string `json:"base_url"`
	Host      string `json:"host_id"`
	Actor     string `json:"actor_id"`
	Agent     string `json:"agent"`
	Cards     []Card `json:"cards"`
}

func (b Binding) Validate() error {
	if !ValidUUID(b.ID) || !filepath.IsAbs(b.Directory) || filepath.Clean(b.Directory) != b.Directory || b.Workspace == "" || b.BaseURL == "" || b.Host == "" || b.Actor == "" || b.Agent == "" || len(b.Cards) == 0 || len(b.Cards) > MaxCards {
		return ErrUnsafe
	}
	seen := map[string]bool{}
	for _, c := range b.Cards {
		if !cardIDPattern.MatchString(c.ID) || !strings.HasPrefix(c.Ref, "card:") || seen[c.ID] {
			return ErrUnsafe
		}
		seen[c.ID] = true
	}
	return nil
}
func (b Binding) Card(ref string) (Card, bool) {
	for _, c := range b.Cards {
		if c.Ref == ref || c.ID == ref || "card:"+c.ID == ref {
			return c, true
		}
	}
	return Card{}, false
}

type Record struct {
	ID           string                 `json:"id"`
	Hash         string                 `json:"sha256"`
	Raw          []byte                 `json:"original_bytes"`
	Input        Entry                  `json:"input"`
	Sequence     int64                  `json:"sequence"`
	Received     time.Time              `json:"received_at"`
	FirstAttempt time.Time              `json:"first_attempt,omitempty"`
	RetryAt      time.Time              `json:"retry_at,omitempty"`
	Attempts     int                    `json:"attempts"`
	State        string                 `json:"state"`
	Reason       string                 `json:"reason,omitempty"`
	Delivery     *integrations.Delivery `json:"delivery,omitempty"`
	Warnings     []string               `json:"warnings,omitempty"`
	Receipt      string                 `json:"event_ref,omitempty"`
}
type Snapshot struct {
	Card    Card      `json:"card"`
	AsOf    time.Time `json:"as_of"`
	Version string    `json:"resource_version"`
	Body    string    `json:"body"`
}
type State struct {
	Version       int       `json:"schema_version"`
	Binding       Binding   `json:"binding"`
	Enabled       bool      `json:"enabled"`
	Pending       []string  `json:"pending"`
	Records       int       `json:"records"`
	Failed        int       `json:"failed"`
	Sequence      int64     `json:"sequence"`
	RefreshCursor int       `json:"refresh_cursor"`
	LastContact   time.Time `json:"last_server_contact"`
	LastCode      string    `json:"last_error_code,omitempty"`
}
type Status struct {
	Version     int       `json:"schema_version"`
	Generation  string    `json:"generation"`
	AsOf        time.Time `json:"as_of"`
	LastContact time.Time `json:"last_server_contact"`
	StaleAfter  time.Time `json:"stale_after"`
	Stale       bool      `json:"stale"`
	Enabled     bool      `json:"enabled"`
	Pending     int       `json:"pending"`
	Failed      int       `json:"failed"`
	LastCode    string    `json:"last_error_code,omitempty"`
}

// Callbacks run with a pinned enrollment, bounded deadline and ordinary scoped API.
type Callbacks struct {
	Validate func(context.Context, Binding) error
	Prepare  func(context.Context, Binding, Entry, time.Time) (integrations.Delivery, []string, error)
	Send     func(context.Context, Binding, integrations.Delivery) (string, error)
	Refresh  func(context.Context, Binding, Card) (Snapshot, error)
	Classify func(error) string // retry, denied, invalid, identity
	Code     func(error) string
}

func Decode(raw []byte) (Entry, error) {
	var v Entry
	if len(raw) > MaxEntry || !utf8.Valid(raw) {
		return v, fmt.Errorf("entry_size_or_encoding")
	}
	// Duplicate keys anywhere are ambiguous; do not let the last one choose authority.
	if e := uniqueJSON(raw); e != nil {
		return v, e
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if e := d.Decode(&v); e != nil {
		return v, fmt.Errorf("invalid_entry")
	}
	if e := d.Decode(new(any)); e != io.EOF {
		return v, fmt.Errorf("invalid_entry")
	}
	if v.Version != 1 || !ValidUUID(v.ID) || v.Card == "" || len(v.Card) > 256 || strings.TrimSpace(v.Text) == "" || len(v.Text) > 4096 || strings.ContainsRune(v.Text, 0) || len(v.Evidence) > 8 {
		return v, fmt.Errorf("invalid_entry")
	}
	if v.Next != nil && (v.Next.At == "" || len(v.Next.At) > 128 || len(v.Next.What) > 4096 || strings.TrimSpace(v.Next.What) == "" || strings.ContainsRune(v.Next.What, 0)) {
		return v, fmt.Errorf("invalid_next")
	}
	return v, nil
}
func uniqueJSON(b []byte) error {
	d := json.NewDecoder(bytes.NewReader(b))
	var value func() error
	value = func() error {
		t, e := d.Token()
		if e != nil {
			return e
		}
		delim, ok := t.(json.Delim)
		if !ok {
			return nil
		}
		switch delim {
		case '{':
			seen := map[string]bool{}
			for d.More() {
				k, e := d.Token()
				if e != nil {
					return e
				}
				s, ok := k.(string)
				if !ok || seen[s] {
					return fmt.Errorf("duplicate_key")
				}
				seen[s] = true
				if e = value(); e != nil {
					return e
				}
			}
		case '[':
			for d.More() {
				if e = value(); e != nil {
					return e
				}
			}
		default:
			return fmt.Errorf("invalid_json")
		}
		_, e = d.Token()
		return e
	}
	return value()
}
