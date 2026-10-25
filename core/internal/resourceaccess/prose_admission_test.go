package resourceaccess

import (
	"crypto/sha256"
	"encoding"
	"slices"
	"strings"
	"testing"
)

func TestProsePrefixesCoverCompleteMatcher(t *testing.T) {
	for _, id := range []string{"[]", "{}", `"quoted"`, "(parentheses)", "two words", "a,b;!?", "under_score", "Σ", "K", "a\x00b", strings.Repeat("[", 5000), strings.Repeat("long-prefix-", 20)} {
		for _, wrap := range []string{"", "_", "__", "**", "`", "~~"} {
			atom := textReferencePrefix + "See " + wrap + "doc:" + id + wrap + ". More text document:unrelated."
			ref := "document:" + id
			if !textHasReference(atom, ref) {
				t.Fatalf("bad matcher fixture: %q/%q", id, wrap)
			}
			prefixes := prosePrefixes(ref)
			found := false
			for _, p := range proseStarts(atom) {
				for {
					if slices.Contains(p.Keys, proseKey(ref)) {
						found = true
						break
					}
					if !slices.Contains(prefixes, p.Prefix) || p.Prefix == "" {
						break
					}
					h := sha256.New()
					if err := h.(encoding.BinaryUnmarshaler).UnmarshalBinary(p.State); err != nil {
						t.Fatal(err)
					}
					p = nextProseProbe(strings.TrimPrefix(atom, textReferencePrefix), p, h)
				}
			}
			if !found {
				t.Fatalf("prefix lost canonical match %q/%q", id, wrap)
			}
		}
	}
	for b := 0; b < 256; b++ {
		for _, text := range []string{string([]byte{byte(b)}) + "document:[]", "document:[]" + string([]byte{byte(b)}), "document:" + string([]byte{byte(b)}) + "[]"} {
			for _, atom := range ReferenceAtoms(text) {
				if textHasReference(atom, "document:[]") {
					found := false
					for _, p := range proseStarts(atom) {
						found = found || slices.Contains(p.Keys, proseKey("document:[]"))
					}
					if !found {
						t.Fatalf("byte=%02x lost complete boundary matcher", b)
					}
				}
			}
		}
	}
}

func TestProseProbeStopsAtMissingPrefix(t *testing.T) {
	atom := textReferencePrefix + strings.Repeat("document:[] ", 10000)
	probes := proseStarts(atom)
	if len(probes) != 10000 {
		t.Fatalf("starts=%d", len(probes))
	}
	for _, p := range probes {
		if len(p.Keys) > proseChunkRunes {
			t.Fatalf("unbounded initial probe: %d", len(p.Keys))
		}
		if !slices.Contains(p.Keys, proseKey("document:[]")) {
			t.Fatal("short reference missing")
		}
	}
}
