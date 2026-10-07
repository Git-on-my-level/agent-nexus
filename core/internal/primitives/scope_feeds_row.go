package primitives

import (
	"bytes"
	"encoding/json"
	"io"
	"unicode/utf8"
)

// DecodeScopeInboxRow shapes the detached row returned by an already-admitted
// ordered repository capability. It restores column-authoritative transport
// fields and exact numbers. It grants no identity, audience or scope authority;
// the repository must first validate the private envelope and joined tuple.
func DecodeScopeInboxRow(raw json.RawMessage) (DerivedInboxItem, error) {
	if len(raw) == 0 || len(raw) > MaxScopeInboxPayloadBytes || !utf8.Valid(raw) {
		return DerivedInboxItem{}, ErrScopeInboxProjection
	}
	var item DerivedInboxItem
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	d.DisallowUnknownFields()
	if d.Decode(&item) != nil || !validScopeInboxItem(item) {
		return DerivedInboxItem{}, ErrScopeInboxProjection
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return DerivedInboxItem{}, ErrScopeInboxProjection
	}
	rehydrateDerivedInboxDataFromColumns(&item)
	return item, nil
}
