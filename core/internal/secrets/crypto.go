package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"strings"
)

type Encryptor struct {
	gcm   cipher.AEAD
	keyID string
}

func NewEncryptor(hexKey string, keyID string) (*Encryptor, error) {
	key, err := hex.DecodeString(strings.TrimSpace(hexKey))
	if err != nil {
		return nil, fmt.Errorf("decode secrets key: %w", err)
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("secrets key must be 32 bytes (64 hex chars), got %d bytes", len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("create AES cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create GCM: %w", err)
	}
	return &Encryptor{gcm: gcm, keyID: keyID}, nil
}

func (e *Encryptor) Encrypt(plaintext []byte) (ciphertext []byte, nonce []byte, err error) {
	nonce = make([]byte, e.gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, nil, fmt.Errorf("generate nonce: %w", err)
	}
	ciphertext = e.gcm.Seal(nil, nonce, plaintext, nil)
	return ciphertext, nonce, nil
}

func (e *Encryptor) Decrypt(ciphertext []byte, nonce []byte) ([]byte, error) {
	plaintext, err := e.gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("decrypt: %w", err)
	}
	return plaintext, nil
}

func (e *Encryptor) KeyID() string {
	return e.keyID
}
