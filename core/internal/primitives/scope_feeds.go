package primitives

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"unicode/utf8"

	"agent-nexus-core/internal/scopes"
)

const MaxScopeInboxPayloadBytes = 16 * 1024

var ErrScopeInboxProjection = errors.New("invalid scoped inbox projection")

// scopeInboxEnvelope is stored projection data, never an HTTP response. Its
// private identity tuple must match the transaction-authorized RID registry at
// hydration. A owns the capture hook and the bounded registry join. These pure
// codecs neither certify a generation nor select the new reader.
type scopeInboxEnvelope struct {
	Format   int                `json:"format"`
	Identity scopeInboxIdentity `json:"identity"`
	Item     DerivedInboxItem   `json:"item"`
}

// ResourceIdentity deliberately omits RID/storage keys when marshaled. The
// persisted envelope has its own private format; no transport uses this type.
type scopeInboxIdentity struct {
	Scope    scopes.ID `json:"scope"`
	Resource string    `json:"resource"`
	RID      int64     `json:"rid"`
	Version  int64     `json:"version"`
}

func inboxIdentity(i scopes.ResourceIdentity) scopeInboxIdentity {
	return scopeInboxIdentity{i.ScopeID, i.ResourceID, i.RID, i.CanonicalVersion}
}

// EncodeScopeInbox captures the canonical derived row, including mirrored
// columns, without looking up sources, interpreting provenance, or mutating the
// caller's map. The trusted writer must supply the registry tuple and commit
// this detached payload atomically with the source and feed/counter delta.
func EncodeScopeInbox(identity scopes.ResourceIdentity, item DerivedInboxItem) (json.RawMessage, error) {
	if !validScopeInboxIdentity(identity) || identity.CanonicalID != item.ID || !validScopeInboxItem(item) {
		return nil, ErrScopeInboxProjection
	}
	// Persist the same column-authoritative shape as the existing derived store.
	item.Data = stripInboxDataForStore(item)
	raw, err := json.Marshal(scopeInboxEnvelope{Format: 1, Identity: inboxIdentity(identity), Item: item})
	if err != nil || len(raw) > MaxScopeInboxPayloadBytes {
		return nil, ErrScopeInboxProjection
	}
	return raw, nil
}

// DecodeScopeInbox refuses stale, cross-scope, wrong-RID and wrong-canonical-ID
// payloads before transport shaping. identity must come from an exact registry
// join in the same authorized snapshot as feed admission, not from this JSON.
func DecodeScopeInbox(identity scopes.ResourceIdentity, raw json.RawMessage) (DerivedInboxItem, error) {
	if !validScopeInboxIdentity(identity) || len(raw) == 0 || len(raw) > MaxScopeInboxPayloadBytes || !utf8.Valid(raw) {
		return DerivedInboxItem{}, ErrScopeInboxProjection
	}
	var envelope scopeInboxEnvelope
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	d.DisallowUnknownFields()
	if d.Decode(&envelope) != nil || envelope.Format != 1 || envelope.Identity != inboxIdentity(identity) ||
		envelope.Item.ID != identity.CanonicalID || !validScopeInboxItem(envelope.Item) {
		return DerivedInboxItem{}, ErrScopeInboxProjection
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return DerivedInboxItem{}, ErrScopeInboxProjection
	}
	rehydrateDerivedInboxDataFromColumns(&envelope.Item)
	return envelope.Item, nil
}

func validScopeInboxIdentity(i scopes.ResourceIdentity) bool {
	return i.Kind == "inbox" && i.RID > 0 && i.CanonicalVersion > 0 &&
		scopeInboxText(string(i.ScopeID), 512) && scopeInboxText(i.ResourceID, 479) && scopeInboxText(i.CanonicalID, 512)
}

func scopeInboxText(s string, max int) bool {
	return s != "" && len(s) <= max && utf8.ValidString(s) && !strings.ContainsRune(s, 0)
}

func validScopeInboxItem(item DerivedInboxItem) bool {
	if !scopeInboxText(item.ID, 512) || !scopeInboxText(item.ThreadID, 512) || !scopeInboxText(item.Category, 128) || !scopeInboxText(item.TriggerAt, 128) {
		return false
	}
	for _, s := range []string{item.SourceEventID, item.SourceCardID} {
		if s != "" && !scopeInboxText(s, 512) {
			return false
		}
	}
	for _, s := range []string{item.GeneratedAt, item.DueAt, item.SourceHash} {
		if s != "" && !scopeInboxText(s, 128) {
			return false
		}
	}
	budget := MaxScopeInboxPayloadBytes
	return scopeInboxValue(item.Data, 0, &budget)
}

// Only detached JSON-shaped canonical data is accepted. Reject lossy UTF-8,
// cycles/deep nesting and oversized caller maps before json.Marshal allocates.
func scopeInboxValue(value any, depth int, budget *int) bool {
	if depth > 32 || *budget < 1 {
		return false
	}
	*budget -= 1
	switch v := value.(type) {
	case nil, bool, int, int64, float64:
		return true
	case json.Number:
		*budget -= len(v)
		if len(v) == 0 || *budget < 0 || v[0] != '-' && (v[0] < '0' || v[0] > '9') {
			return false
		}
		return json.Valid([]byte(v))
	case string:
		*budget -= len(v)
		return *budget >= 0 && utf8.ValidString(v) && !strings.ContainsRune(v, 0)
	case map[string]any:
		for k, v := range v {
			if !scopeInboxValue(k, depth+1, budget) || !scopeInboxValue(v, depth+1, budget) {
				return false
			}
		}
		return true
	case []any:
		for _, v := range v {
			if !scopeInboxValue(v, depth+1, budget) {
				return false
			}
		}
		return true
	case []string:
		for _, v := range v {
			if !scopeInboxValue(v, depth+1, budget) {
				return false
			}
		}
		return true
	default:
		return false
	}
}

// ScopeInboxSortKey preserves the legacy SQL comparator exactly: trimmed
// category rank ASC, stored trigger text DESC, canonical ID ASC (BINARY). A
// timestamp conversion or RID tie-break would change pagination for legacy
// equal timestamps/IDs. The key contains a private canonical ID: persist it as
// BLOB and encrypt it in continuations; never serialize it in an HTTP response.
// A must integrate a typed BLOB candidate/seek adapter before using this key.
func ScopeInboxSortKey(item DerivedInboxItem) ([]byte, error) {
	if !scopeInboxText(item.ID, 512) || !scopeInboxText(item.Category, 128) || !scopeInboxText(item.TriggerAt, 128) {
		return nil, ErrScopeInboxProjection
	}
	key := make([]byte, 0, 2+2*len(item.TriggerAt)+len(item.ID))
	key = append(key, byte(derivedInboxCategoryOrder(item.Category)))
	// Invert bytes for descending text. A larger terminator makes a shorter
	// prefix sort AFTER its extensions, including fractional timestamps. Every
	// byte has a marker so an inverted byte cannot collide with the terminator.
	for _, b := range []byte(item.TriggerAt) {
		key = append(key, 1, ^b)
	}
	key = append(key, 2)
	return append(key, []byte(item.ID)...), nil
}

// Empty shadow DDL/query proposals for A's migration/template registry. They
// are never executed by this package or installed in a production workspace.
const ScopeInboxOrderSchemaProposal = `CREATE TABLE scope_inbox_order (
 scope_id TEXT NOT NULL,generation INTEGER NOT NULL,family TEXT NOT NULL,audience_key TEXT NOT NULL,
 order_key BLOB NOT NULL CHECK(typeof(order_key)='blob' AND length(order_key)<=770),
 rid INTEGER NOT NULL,version INTEGER NOT NULL,
 PRIMARY KEY(scope_id,generation,family,audience_key,order_key,rid)
) WITHOUT ROWID;
CREATE UNIQUE INDEX scope_inbox_order_resource ON scope_inbox_order(scope_id,generation,family,audience_key,rid);`

const ScopeInboxOrderStartProposal = `SELECT order_key,rid,version FROM scope_inbox_order
 WHERE scope_id=? AND generation=? AND family=? AND audience_key=? ORDER BY order_key,rid LIMIT ?`
const ScopeInboxOrderAfterProposal = `SELECT order_key,rid,version FROM scope_inbox_order
 WHERE scope_id=? AND generation=? AND family=? AND audience_key=? AND (order_key,rid)>(?,?)
 ORDER BY order_key,rid LIMIT ?`
