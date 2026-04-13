package secrets

import (
	"crypto/rand"
	"encoding/hex"
	"testing"
)

func TestEncryptDecryptRoundTrip(t *testing.T) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	hexKey := hex.EncodeToString(key)

	enc, err := NewEncryptor(hexKey, "v1")
	if err != nil {
		t.Fatal(err)
	}

	plaintext := []byte("sk-test-1234567890abcdef")
	ct, nonce, err := enc.Encrypt(plaintext)
	if err != nil {
		t.Fatal(err)
	}

	decrypted, err := enc.Decrypt(ct, nonce)
	if err != nil {
		t.Fatal(err)
	}
	if string(decrypted) != string(plaintext) {
		t.Fatalf("got %q, want %q", decrypted, plaintext)
	}
}

func TestEncryptProducesUniqueNonces(t *testing.T) {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	enc, err := NewEncryptor(hex.EncodeToString(key), "v1")
	if err != nil {
		t.Fatal(err)
	}

	_, nonce1, _ := enc.Encrypt([]byte("value1"))
	_, nonce2, _ := enc.Encrypt([]byte("value1"))
	if hex.EncodeToString(nonce1) == hex.EncodeToString(nonce2) {
		t.Fatal("nonces must be unique")
	}
}

func TestNewEncryptorRejectsShortKey(t *testing.T) {
	_, err := NewEncryptor("deadbeef", "v1")
	if err == nil {
		t.Fatal("expected error for short key")
	}
}

func TestNewEncryptorRejectsInvalidHex(t *testing.T) {
	_, err := NewEncryptor("not-hex-at-all-!", "v1")
	if err == nil {
		t.Fatal("expected error for invalid hex")
	}
}

func TestDecryptWithWrongKeyFails(t *testing.T) {
	key1 := make([]byte, 32)
	key2 := make([]byte, 32)
	_, _ = rand.Read(key1)
	_, _ = rand.Read(key2)

	enc1, _ := NewEncryptor(hex.EncodeToString(key1), "v1")
	enc2, _ := NewEncryptor(hex.EncodeToString(key2), "v1")

	ct, nonce, _ := enc1.Encrypt([]byte("secret"))
	_, err := enc2.Decrypt(ct, nonce)
	if err == nil {
		t.Fatal("expected decryption to fail with wrong key")
	}
}
