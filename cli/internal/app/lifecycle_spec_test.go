package app

import (
	"sort"
	"testing"
)

// TestLifecyclePreflightCoverage asserts that every (resource, verb) pair
// declared in `lifecycleResourceSpecs` is reachable through the merged
// `preflightFlagSpecs` and exposes the expected canonical flag set. Adding a
// new lifecycle verb to the registry without runtime-parser support, or vice
// versa, will trip this test before it ships.
func TestLifecyclePreflightCoverage(t *testing.T) {
	t.Parallel()

	merged := preflightFlagSpecs()
	for _, spec := range lifecycleResourceSpecs() {
		for _, verb := range spec.verbs {
			path := spec.resource + " " + verb
			flags, ok := merged[path]
			if !ok {
				t.Errorf("preflightFlagSpecs is missing %q", path)
				continue
			}
			if _, ok := flags[spec.idFlag]; !ok {
				t.Errorf("%q preflight is missing id flag --%s", path, spec.idFlag)
			}
			if _, ok := flags["reason"]; !ok {
				t.Errorf("%q preflight is missing --reason", path)
			}
			if _, ok := flags["from-file"]; !ok {
				t.Errorf("%q preflight is missing --from-file", path)
			}
			if _, ok := flags["dry-run"]; !ok {
				t.Errorf("%q preflight is missing --dry-run", path)
			}
			if verb != "purge" {
				if _, ok := flags["actor-id"]; !ok {
					t.Errorf("%q preflight is missing --actor-id", path)
				}
			} else {
				if _, ok := flags["actor-id"]; ok {
					t.Errorf("%q preflight must not register --actor-id for purge", path)
				}
			}
		}
	}
}

// TestLifecycleSpecRegistryNoDuplicates guards against accidentally registering
// the same resource twice (the registry is a slice so the type system won't).
func TestLifecycleSpecRegistryNoDuplicates(t *testing.T) {
	t.Parallel()

	seen := map[string]bool{}
	var resources []string
	for _, spec := range lifecycleResourceSpecs() {
		if seen[spec.resource] {
			t.Errorf("lifecycleResourceSpecs has duplicate registration for %q", spec.resource)
		}
		seen[spec.resource] = true
		resources = append(resources, spec.resource)
	}
	sort.Strings(resources)
	t.Logf("registered lifecycle resources: %v", resources)
}
