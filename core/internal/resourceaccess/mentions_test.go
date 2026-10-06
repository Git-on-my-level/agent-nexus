package resourceaccess

import (
	"slices"
	"strings"
	"testing"
)

func TestMentionBucketsCoverCompleteMatcher(t *testing.T) {
	for _, id := range []string{"[]", "{}", `"quoted"`, "(parentheses)", "two words", "a,b;!?", "abcd", "under_score", "K", "Σ", strings.Repeat("[", 5000)} {
		for _, wrap := range []string{"", "_", "__", "**", "`", "~~"} {
			atom := textReferencePrefix + "See " + wrap + "doc:" + id + wrap + ". More text document:unrelated."
			if !textHasReference(atom, "document:"+id) {
				t.Fatalf("test case failed matcher: %q/%q", id, wrap)
			}
			if !slices.Contains(mentionBuckets(atom), "document:"+mentionBucket(id)) {
				t.Fatalf("bucket lost %q/%q", id, wrap)
			}
		}
	}
	for _, pair := range [][2]string{{"K", "K"}, {"σ", "Σ"}, {"ſ", "S"}} {
		if mentionBucket(pair[0]) != mentionBucket(pair[1]) {
			t.Fatalf("Unicode folding diverged: %v", pair)
		}
	}
}
func TestMentionBucketsMatchAllBinaryBoundaries(t *testing.T) {
	for b := 0; b < 256; b++ {
		for _, text := range []string{string([]byte{byte(b)}) + "document:[]", "document:[]" + string([]byte{byte(b)}), "document:" + string([]byte{byte(b)}) + "[]"} {
			for _, atom := range ReferenceAtoms(text) {
				if textHasReference(atom, "document:[]") && !slices.Contains(mentionBuckets(atom), "document:"+mentionBucket("[]")) {
					t.Fatalf("byte=%02x lost bucket for %q", b, text)
				}
			}
		}
	}
}
func TestMentionBucketsBoundRepeatedReferences(t *testing.T) {
	input := textReferencePrefix + strings.Repeat("document:[] ", 10000)
	// The candidate list stays constant even when prose repeats a ref. Work is a
	// single reference scan plus at most eight prefix probes per occurrence.
	if got := mentionBuckets(input); len(got) != 1 || got[0] != "document:[" {
		t.Fatalf("unexpected repeated buckets: %v", got)
	}
}
func TestAtomKeyMatchesSQLiteNOCASE(t *testing.T) {
	if AtomKey("Doc:MiXeD") != AtomKey("doc:mixed") {
		t.Fatal("ASCII fold lost")
	}
	if AtomKey("doc:K") == AtomKey("doc:K") {
		t.Fatal("NOCASE must not fold Unicode")
	}
	if AtomKey("x\x00Y") != AtomKey("X\x00y") {
		t.Fatal("NUL truncated digest")
	}
}
