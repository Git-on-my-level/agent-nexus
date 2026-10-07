// Package scopestream executes bounded exact-audience SSE ticks. Only trusted
// repositories may implement its transaction-scoped snapshot interfaces.
package scopestream

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
)

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
	if len(b) > 192*1024 {
		return "", ErrBudget
	}
	nonce := make([]byte, t.aead.NonceSize())
	if base64.RawURLEncoding.EncodedLen(len(b)+len(nonce)+t.aead.Overhead()) > MaxTokenBytes {
		return "", ErrBudget
	}
	if _, err = rand.Read(nonce); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(t.aead.Seal(nonce, nonce, b, []byte("anx:scope-stream:v1"))), nil
}
func (t *Tokens) open(token string, v any) error {
	if len(token) > MaxTokenBytes {
		return ErrCursor
	}
	b, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(b) < t.aead.NonceSize() {
		return ErrCursor
	}
	n := t.aead.NonceSize()
	plain, err := t.aead.Open(nil, b[:n], b[n:], []byte("anx:scope-stream:v1"))
	if err != nil || json.Unmarshal(plain, v) != nil {
		return ErrCursor
	}
	return nil
}
