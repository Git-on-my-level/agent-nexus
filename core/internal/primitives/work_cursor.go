package primitives

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"agent-nexus-core/internal/scopesearch"
)

// Only authenticated ciphertext crosses the boundary, including on empty
// callback-filtered pages. No traversal state is shared between requests.
type workContinuation struct {
	Version     int
	Binding     string
	ExpiresAt   int64
	UpdatedAt   time.Time
	ID          string
	Lifecycle   bool
	LifecycleID string
}

func (s *Store) workCursorCodec(ctx context.Context, f WorkListFilter) (*scopesearch.Tokens, string, error) {
	var key []byte
	// Reuse the workspace's persistent search cursor secret, with domain
	// separation so work tokens cannot be consumed as search continuations.
	if err := s.db.QueryRowContext(ctx, `SELECT cursor_key FROM document_search_state WHERE singleton=1`).Scan(&key); err != nil {
		return nil, "", err
	}
	if len(key) != 32 {
		return nil, "", ErrInvalidCursor
	}
	derived := sha256.Sum256(append([]byte("anx:work-cursor:v1:"), key...))
	codec, err := scopesearch.NewTokens(derived[:])
	if err != nil {
		return nil, "", err
	}
	scope, scoped := accessScopeFrom(ctx)
	f.Cursor = ""
	f.Limit = 0 // Page size does not change the selected population.
	raw, err := json.Marshal(struct {
		Scope            AccessScope
		Scoped, Callback bool
		Filter           WorkListFilter
	}{scope, scoped, f.Visible != nil, f})
	if err != nil {
		return nil, "", err
	}
	binding := sha256.Sum256(raw)
	return codec, hex.EncodeToString(binding[:]), nil
}
