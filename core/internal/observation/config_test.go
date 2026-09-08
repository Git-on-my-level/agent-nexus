package observation

import (
	"strings"
	"testing"
)

func TestOperatorRegistryIsBoundedAndStrict(t *testing.T) {
	raw := `{"version":1,"registrations":[{"card_ref":"card:fixture","target":{"workspace_id":"w","connection_id":"c","source":"github","kind":"issue","repository":"o/r","native_id":"1"},"policy":{"interval_seconds":60,"stale_after_seconds":120,"timeout_seconds":10,"max_backoff_seconds":3600},"connection":{"transport":"https","max_pages":3}}]}`
	entries, err := LoadRegistrations(strings.NewReader(raw), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Target.CanonicalNativeID() != "o/r#1" {
		t.Fatalf("bad registry: %+v", entries)
	}
	if _, err := LoadRegistrations(strings.NewReader(strings.Replace(raw, `"max_pages":3`, `"secret":"fixture-token"`, 1)), nil); err == nil {
		t.Fatal("unknown credential data accepted")
	}
	if _, err := LoadRegistrations(strings.NewReader(strings.Replace(raw, `"version":1`, `"version":99`, 1)), nil); err == nil {
		t.Fatal("unknown config version accepted")
	}
}
