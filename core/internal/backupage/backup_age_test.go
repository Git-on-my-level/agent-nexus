package backupage

import (
	"bufio"
	"bytes"
	"io"
	"strings"
	"testing"

	"filippo.io/age"
)

func TestParseRecipientsRequiresTwoDistinctPublicKeys(t *testing.T) {
	ops, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	rec, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	recipients, err := ParseRecipients(ops.Recipient().String(), rec.Recipient().String())
	if err != nil {
		t.Fatal(err)
	}
	if recipients.Fingerprint() == "" {
		t.Fatal("expected a fingerprint")
	}
	if _, err := ParseRecipients("", rec.Recipient().String()); err == nil {
		t.Fatal("expected missing OPS recipient to fail")
	}
	if _, err := ParseRecipients(ops.Recipient().String(), ""); err == nil {
		t.Fatal("expected missing recovery recipient to fail")
	}
	if _, err := ParseRecipients(ops.Recipient().String(), ops.Recipient().String()); err == nil {
		t.Fatal("expected overlapping recipients to fail")
	}
	if _, err := ParseRecipients("not-an-age-key", rec.Recipient().String()); err == nil {
		t.Fatal("expected malformed recipient to fail")
	}
}

func TestEncryptProducesAgeStreamReadableByRecipient(t *testing.T) {
	ops, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	rec, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	recipients, err := ParseRecipients(ops.Recipient().String(), rec.Recipient().String())
	if err != nil {
		t.Fatal(err)
	}
	var ciphertext bytes.Buffer
	writer, err := recipients.Encrypt(&ciphertext)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write([]byte("archive payload")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(ciphertext.String(), "age-encryption.org/v1\n") {
		t.Fatal("expected an age header")
	}
	reader, err := age.Decrypt(bufio.NewReader(bytes.NewReader(ciphertext.Bytes())), rec)
	if err != nil {
		t.Fatal(err)
	}
	plaintext, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if string(plaintext) != "archive payload" {
		t.Fatalf("unexpected plaintext %q", plaintext)
	}
}

func TestEncryptRejectsUnconfiguredRecipients(t *testing.T) {
	var recipients Recipients
	if _, err := recipients.Encrypt(io.Discard); err == nil {
		t.Fatal("expected unconfigured recipients to fail")
	}
	var nilRecipients *Recipients
	if _, err := nilRecipients.Encrypt(io.Discard); err == nil {
		t.Fatal("expected nil recipients to fail")
	}
}
