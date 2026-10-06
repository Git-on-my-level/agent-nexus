package resourceaccess

import (
	"slices"
	"strings"
	"testing"
)

func TestReferenceAtomsNestedTextAndMalformedUTF8(t *testing.T) {
	for _, input := range []string{
		`{"evidence":[{"ref":" CARD :\u00a0private ","title":"secret"}]}`,
		"See [secret](CARD\u00a0:\u0085private).",
		"See [secret](card:private)\xff",
		`{"card:private":{"title":"secret"}}`,
		`{"See card:private":{"title":"secret"}}`,
		"See CARD\v:\vprivate",
	} {
		if got := ReferenceAtoms(input); !slices.Contains(got, "card:private") {
			t.Errorf("%q lost reference: %v", input, got)
		}
	}
	if got := ReferenceAtoms(`{"ref":"doc:private"}`); !slices.Contains(got, "document:private") {
		t.Fatalf("document alias lost: %v", got)
	}
}

func TestReferenceAtomsWhitespaceParity(t *testing.T) {
	for _, space := range []rune{'\t', '\n', '\v', '\f', '\r', ' ', 0x85, 0xa0, 0x1680, 0x2000, 0x2001, 0x2002, 0x2003, 0x2004, 0x2005, 0x2006, 0x2007, 0x2008, 0x2009, 0x200a, 0x2028, 0x2029, 0x202f, 0x205f, 0x3000} {
		input := "See CARD" + string(space) + ":" + string(space) + "private"
		if got := ReferenceAtoms(input); !slices.Contains(got, "card:private") {
			t.Errorf("U+%04X lost ref: %v", space, got)
		}
	}
}

func TestReferenceAtomsScalarIDs(t *testing.T) {
	for _, id := range []string{"123", "true", "null", "1e2", "{}", "[]", `"123"`} {
		if got := ReferenceAtoms(id); !slices.Contains(got, id) {
			t.Errorf("scalar ID %q lost: %v", id, got)
		}
	}
}

func TestReferenceAtomsExternalLinksAndLongIDs(t *testing.T) {
	for _, ref := range []string{"https://example.test/work/123?view=secret", "document:" + strings.Repeat("x", 5000)} {
		if got := ReferenceAtoms("Evidence [secret](" + ref + ")"); !slices.Contains(got, ref) {
			t.Errorf("reference lost: %q", ref)
		}
	}
}

func TestReferenceAtomsEncodedStructuredValue(t *testing.T) {
	if got := ReferenceAtoms(`{"payload":"{\"ref\":\"bare-id\"}"}`); !slices.Contains(got, "bare-id") {
		t.Fatalf("encoded ref lost: %v", got)
	}
}

func TestStructuredContainersAreNotScalarReferences(t *testing.T) {
	for _, value := range []string{"{}", "[]"} {
		for _, kind := range []string{"structured", "application/json; charset=utf-8", "application/example+json"} {
			if got := ContentReferenceAtomsJSON(value, kind); got != "[]" {
				t.Fatalf("container became ref: %s", got)
			}
		}
		if !slices.Contains(ReferenceAtoms(value), value) {
			t.Fatalf("scalar ID lost: %s", value)
		}
	}
}

func TestProseReferenceCandidatesPreserveLegacyIDGrammar(t *testing.T) {
	for _, id := range []string{"[]", "{}", `"quoted"`, "(nested)", "two words", "line\nbreak", "a,b;!?", strings.Repeat("[", 5000)} {
		text := "Evidence copied from DOC : " + id + ". More prose follows."
		atoms := ReferenceAtoms(text)
		found := false
		for _, atom := range atoms {
			found = found || textHasReference(atom, "document:"+id)
			if textHasReference(atom, "card:"+id) || textHasReference(atom, "document:unrelated") {
				t.Fatalf("unrelated reference matched: %q", id)
			}
		}
		if !found || len(atoms) > 6 {
			t.Fatalf("legacy ID lost or prefixes expanded: %q (%d atoms)", id, len(atoms))
		}
	}
	for _, wrap := range []string{"**", "_", "__", "~~", "`", "\"", "'"} {
		atoms := ReferenceAtoms("See " + wrap + "document:[]" + wrap)
		found := false
		for _, atom := range atoms {
			found = found || textHasReference(atom, "document:[]")
		}
		if !found {
			t.Errorf("markup %q hid legacy ID", wrap)
		}
	}
	if textHasReference(textReferencePrefix+"thread:private-2", "thread:private") {
		t.Fatal("canonical handle prefix matched")
	}
	atom := textReferencePrefix + "See document:[]"
	if got := ReferenceAtoms(atom); len(got) != 1 || got[0] != atom {
		t.Fatalf("candidate backfill is not idempotent: %v", got)
	}
}
