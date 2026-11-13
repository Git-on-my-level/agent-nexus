package resourceaccess

import (
	"encoding/json"
	"strings"
	"testing"
)

func manifestFor(t *testing.T, value any) string {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(ContentReferenceAtomsJSON(string(encoded), "structured"))
}

func TestWithoutProseDropsBodiesAndKeepsReferences(t *testing.T) {
	body := "Ship card:abc with document:legacy id and see https://example.test/x. " + strings.Repeat("detail ", 200)
	manifest := manifestFor(t, map[string]any{
		"title":              "Fix bug",
		"summary":            body,
		"definition_of_done": []string{"card:abc", "bare-identifier"},
	})
	if !strings.Contains(manifest, body) || !strings.Contains(manifest, textReferencePrefix+body) {
		t.Fatal("fixture manifest did not store the complete prose body")
	}
	pruned := WithoutProse(manifest)
	for _, keep := range []string{"card:abc", "document:legacy", "https://example.test/x", "bare-identifier"} {
		if !strings.Contains(pruned, keep) {
			t.Errorf("pruned manifest lost reference %q: %s", keep, pruned)
		}
	}
	for _, drop := range []string{body, textReferencePrefix, "Fix bug"} {
		if strings.Contains(pruned, drop) {
			t.Errorf("pruned manifest retained prose %q", drop)
		}
	}
	if len(pruned) >= len(manifest) {
		t.Fatalf("pruned manifest %d bytes is not smaller than %d", len(pruned), len(manifest))
	}
	if again := WithoutProse(pruned); again != pruned {
		t.Errorf("WithoutProse is not idempotent: %s", again)
	}
}

func TestWithoutProseLeavesMalformedManifestAlone(t *testing.T) {
	for _, manifest := range []string{"not json", "", `{"not":"an array"}`} {
		if got := WithoutProse(manifest); got != manifest {
			t.Errorf("WithoutProse(%q) = %q", manifest, got)
		}
	}
}

func TestWithoutProseKeepsBareAndTypedIdentifiers(t *testing.T) {
	manifest := `["card:abc","document:legacy id with spaces","abc-123","https://example.test/path","$anx-ref-text$See card:abc"]`
	got := WithoutProse(manifest)
	var atoms []string
	if err := json.Unmarshal([]byte(got), &atoms); err != nil {
		t.Fatal(err)
	}
	want := []string{"card:abc", "document:legacy id with spaces", "abc-123", "https://example.test/path"}
	if strings.Join(atoms, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("atoms = %q, want %q", atoms, want)
	}
}
