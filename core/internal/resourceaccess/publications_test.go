package resourceaccess

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

func TestExternalPublicationInventoryPreservesReferencesAndUnknownFields(t *testing.T) {
	if !slices.Equal(publishedIdentityFields, []string{"authority", "connection_id", "native_id", "url", "identifier", "identifier_aliases", "aliases"}) {
		t.Fatal("publication fields changed; classify the exact fields explicitly")
	}
	for kind, paths := range ExternalKeyPublications {
		var source *OwnershipSource
		for i := range OwnershipSources {
			if OwnershipSources[i].Kind == kind {
				source = &OwnershipSources[i]
			}
		}
		if source == nil {
			t.Fatalf("publication kind missing from ownership inventory: %s", kind)
		}
		for _, path := range paths {
			column, _, _ := strings.Cut(path, ".")
			if !slices.Contains(source.Columns, column) {
				t.Fatalf("publication field not inventoried: %s.%s", kind, path)
			}
		}
	}
	entry := map[string]any{"native_id": "published-native", "url": "https://source.test/published", "aliases": []string{"published-alias"}, "identifier_aliases": []string{"published-identifier"}, "extension": map[string]any{"context_ref": "referenced-key"}}
	for _, fixture := range []struct {
		kind string
		body any
	}{
		{"work_metadata", map[string]any{"metadata_json": map[string]any{"source": entry, "source_refs": []any{entry}, "plan": map[string]any{"steps": []any{map[string]any{"ref": "published-alias"}}}}, "refresh_json": map[string]any{"ref": "refresh-key"}}},
		{"work_observation", map[string]any{"body_json": map[string]any{"facts": entry, "evidence": []any{entry}, "ref": "observation-key"}}},
		{"work_evidence_record", map[string]any{"evidence_json": entry}},
	} {
		raw, _ := json.Marshal(fixture.body)
		var atoms []string
		if err := json.Unmarshal([]byte(externalKeyReferenceAtomsJSON(string(raw), fixture.kind)), &atoms); err != nil {
			t.Fatal(err)
		}
		if !slices.Contains(atoms, "referenced-key") {
			t.Fatalf("extension ref lost: %s %+v", fixture.kind, atoms)
		}
		for _, key := range []string{"published-native", "https://source.test/published", "published-identifier"} {
			if slices.Contains(atoms, key) {
				t.Fatalf("publication became a reference: %s %+v", fixture.kind, atoms)
			}
		}
		if fixture.kind == "work_metadata" && (!slices.Contains(atoms, "published-alias") || !slices.Contains(atoms, "refresh-key")) {
			t.Fatalf("publication masked plan/refresh refs: %+v", atoms)
		}
		if fixture.kind == "work_observation" && !slices.Contains(atoms, "observation-key") {
			t.Fatalf("publication masked observation ref: %+v", atoms)
		}
	}
}
