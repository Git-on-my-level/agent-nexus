package scopesearch

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
)

// encoding/json expands at most six bytes per input byte (controls and <>&).
// The two fingerprints are fixed hexadecimal SHA-256 strings, Recency is a
// nonnegative int64 (19 digits), and document is the longest accepted kind.
// The scaffold includes every field name, quote, separator and brace. These
// bounds cover the last accepted Key, not the requested query or scope list.
const maxCursorJSONBytes = len(`{"b":"","q":"","a":{"Recency":,"RID":"","Kind":"","Scope":""}}`) +
	2*(sha256.Size*2) + 19 + len("document") + 6*(maxResourceIDBytes+maxScopeIDBytes)

func (t *Tokens) maxEncodedBytes() int {
	// AES-GCM adds a 12-byte nonce and a 16-byte tag: 4,825 JSON bytes
	// become at most 6,471 bytes of unpadded base64url. Seal and open use
	// this same derived bound, including the actual codec overhead.
	return base64.RawURLEncoding.EncodedLen(maxCursorJSONBytes + t.aead.NonceSize() + t.aead.Overhead())
}

// Tokens must use a deployment-local persistent secret. Encryption keeps
// resource keys and selection generations out of wire continuations.
type Tokens struct{ aead cipher.AEAD }

func NewTokens(key []byte) (*Tokens, error) {
	if len(key) != 32 {
		return nil, ErrCursor
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Tokens{aead}, nil
}

func (t *Tokens) seal(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	if len(b) > maxCursorJSONBytes {
		return "", ErrCursor
	}
	nonce := make([]byte, t.aead.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(t.aead.Seal(nonce, nonce, b, []byte("anx:scope-search:v1")))
	if len(token) > t.maxEncodedBytes() {
		return "", ErrCursor
	}
	return token, nil
}
func (t *Tokens) open(token string, v any) error {
	if len(token) > t.maxEncodedBytes() {
		return ErrCursor
	}
	b, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(b) < t.aead.NonceSize() {
		return ErrCursor
	}
	n := t.aead.NonceSize()
	plain, err := t.aead.Open(nil, b[:n], b[n:], []byte("anx:scope-search:v1"))
	if err != nil || len(plain) > maxCursorJSONBytes || json.Unmarshal(plain, v) != nil {
		return ErrCursor
	}
	return nil
}
