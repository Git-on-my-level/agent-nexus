// Package backupage provides the public-recipient age streaming used by
// backup writers. Private identities are deliberately not part of this
// package: backups are written from public recipients only, and restores run
// the decrypt path with identities on the isolated restore host.
package backupage

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"sort"
	"strings"

	"filippo.io/age"
)

// Recipients is an immutable, validated set of public age recipients.
type Recipients struct {
	values []age.Recipient
	// fingerprint identifies the public recipient set without retaining or
	// exposing the raw strings. It is empty for a nil or unconfigured set.
	fingerprint string
}

// Fingerprint returns a stable hash of the public recipient set, or "" when
// the set is nil or unconfigured. It never contains recipient material.
func (r *Recipients) Fingerprint() string {
	if r == nil {
		return ""
	}
	return r.fingerprint
}

// ParseRecipients combines the configured OPS recipient list with the separate
// REC public recipient. At least two distinct X25519 recipients are required.
// Errors deliberately never include the supplied values or parser messages,
// which can reflect malformed input.
func ParseRecipients(opsValue string, recoveryValue string) (*Recipients, error) {
	values := strings.FieldsFunc(opsValue, func(r rune) bool {
		return r == ',' || r == '\n' || r == '\r' || r == '\t' || r == ' '
	})
	if len(values) == 0 {
		return nil, errors.New("backup age encryption requires the OPS public recipient")
	}
	recoveryValue = strings.TrimSpace(recoveryValue)
	if recoveryValue == "" {
		return nil, errors.New("backup age encryption requires the recovery public recipient")
	}
	seen := make(map[string]struct{}, len(values)+1)
	recipients := &Recipients{values: make([]age.Recipient, 0, len(values)+1)}
	public := make([]string, 0, len(values)+1)
	parsePublic := func(value string) (age.Recipient, error) {
		if !strings.HasPrefix(value, "age1") {
			return nil, errors.New("backup age recipients must be public age1 X25519 keys")
		}
		recipient, err := age.ParseX25519Recipient(value)
		if err != nil {
			return nil, errors.New("backup age recipient is malformed")
		}
		return recipient, nil
	}
	for _, value := range values {
		recipient, err := parsePublic(value)
		if err != nil {
			return nil, err
		}
		if _, duplicate := seen[value]; duplicate {
			continue
		}
		seen[value] = struct{}{}
		recipients.values = append(recipients.values, recipient)
		public = append(public, value)
	}
	if _, overlap := seen[recoveryValue]; overlap {
		return nil, errors.New("backup age encryption requires the recovery recipient to be distinct from OPS")
	}
	recovery, err := parsePublic(recoveryValue)
	if err != nil {
		return nil, err
	}
	recipients.values = append(recipients.values, recovery)
	public = append(public, recoveryValue)
	if len(recipients.values) < 2 {
		return nil, errors.New("backup age encryption requires two distinct recipients")
	}
	sort.Strings(public)
	hash := sha256.New()
	io.WriteString(hash, strings.Join(public, "\n"))
	recipients.fingerprint = hex.EncodeToString(hash.Sum(nil))
	return recipients, nil
}

// Encrypt wraps dst in an authenticated age stream. Closing the returned
// writer is required to authenticate the final payload chunk.
func (r *Recipients) Encrypt(dst io.Writer) (io.WriteCloser, error) {
	if r == nil || len(r.values) < 2 {
		return nil, errors.New("backup age encryption is not configured with two recipients")
	}
	return age.Encrypt(dst, r.values...)
}
