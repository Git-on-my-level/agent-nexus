package readmodel

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
)

// CursorCodec requires a stable server-owned 256-bit secret. Tokens conceal
// allocation keys as well as authenticate them. The AAD separates feed cursors
// from directory, search and stream tokens even if a deployment shares a key.
type CursorCodec struct{ aead cipher.AEAD }

func NewCursorCodec(key []byte) (*CursorCodec, error) {
	if len(key) != 32 {
		return nil, ErrCursor
	}
	b, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	a, err := cipher.NewGCM(b)
	if err != nil {
		return nil, err
	}
	return &CursorCodec{a}, nil
}

type continuation struct {
	Binding [32]byte
	Heads   []*Key
}

var cursorAAD = []byte("anx.readmodel.page.v1")

func binding(s Snapshot) [32]byte {
	// Directory position/coverage and stream order are also part of the binding;
	// a cursor cannot silently change the selection on the next request.
	b, _ := json.Marshal(struct {
		Binding string
		Scopes  []Scope
		Streams []Stream
		More    bool
		Next    string
	}{s.Binding, s.Scopes, s.Streams, s.MoreScopes, s.DirectoryContinuation})
	return sha256.Sum256(b)
}
func (c *CursorCodec) encode(s Snapshot, heads []*Key) (string, error) {
	b, err := json.Marshal(continuation{binding(s), heads})
	if err != nil {
		return "", err
	}
	nonce := make([]byte, c.aead.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(c.aead.Seal(nonce, nonce, b, cursorAAD)), nil
}
func (c *CursorCodec) decode(token string, s Snapshot) ([]*Key, error) {
	if len(token) > 32*1024 {
		return nil, ErrCursor
	}
	b, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(b) < c.aead.NonceSize()+c.aead.Overhead() {
		return nil, ErrCursor
	}
	n := c.aead.NonceSize()
	plain, err := c.aead.Open(nil, b[:n], b[n:], cursorAAD)
	if err != nil {
		return nil, ErrCursor
	}
	var v continuation
	if json.Unmarshal(plain, &v) != nil || v.Binding != binding(s) || len(v.Heads) != len(s.Streams) {
		return nil, ErrCursor
	}
	for _, key := range v.Heads {
		if key != nil && key.RID < 1 {
			return nil, ErrCursor
		}
	}
	return v.Heads, nil
}
