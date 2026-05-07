package app

// Single source of truth for resource lifecycle command surface.
//
// Every resource that exposes some subset of {archive, unarchive, trash, restore,
// purge} registers a `lifecycleResourceSpec` here. The registry drives:
//
//   - preflight flag coverage (`derivedLifecyclePreflightSpecs`), so that
//     `manualPreflightFlagSpecs` no longer has to enumerate every (resource,
//     verb) pair by hand and can no longer drift silently from the runtime
//     parsers.
//   - test-time coverage assertions (`TestLifecyclePreflightCoverage`) that
//     fail loudly when a new lifecycle verb is added without preflight wiring.
//   - runtime dispatch (`runLifecycleVerb` in lifecycle_runtime.go).

// lifecycleResourceSpec declares the lifecycle command surface for one
// resource. The registry below is the canonical list.
type lifecycleResourceSpec struct {
	resource string
	idFlag   string
	verbs    []string
}

func lifecycleResourceSpecs() []lifecycleResourceSpec {
	return []lifecycleResourceSpec{
		{resource: "artifacts", idFlag: "artifact-id", verbs: []string{"archive", "unarchive", "trash", "restore", "purge"}},
		{resource: "boards", idFlag: "board-id", verbs: []string{"archive", "unarchive", "trash", "restore", "purge"}},
		{resource: "docs", idFlag: "document-id", verbs: []string{"archive", "unarchive", "trash", "restore", "purge"}},
		{resource: "events", idFlag: "event-id", verbs: []string{"archive", "unarchive", "trash", "restore"}},
		{resource: "cards", idFlag: "card-id", verbs: []string{"archive", "trash", "restore", "purge"}},
		{resource: "topics", idFlag: "topic-id", verbs: []string{"archive", "unarchive", "trash", "restore"}},
	}
}

func lifecycleSpecFor(resource string) (lifecycleResourceSpec, bool) {
	for _, spec := range lifecycleResourceSpecs() {
		if spec.resource == resource {
			return spec, true
		}
	}
	return lifecycleResourceSpec{}, false
}

// derivedLifecyclePreflightSpecs builds the preflight flag map for every
// (resource, verb) pair declared in the registry. It is meant to be merged
// into `preflightFlagSpecs` as the lowest-precedence layer, so any
// command-specific override in `manualPreflightFlagSpecs` still wins.
func derivedLifecyclePreflightSpecs() map[string]map[string]preflightFlagSpec {
	valueFlag := preflightFlagSpec{kind: preflightFlagString}
	boolFlag := preflightFlagSpec{kind: preflightFlagBool}

	out := map[string]map[string]preflightFlagSpec{}
	for _, spec := range lifecycleResourceSpecs() {
		for _, verb := range spec.verbs {
			flags := map[string]preflightFlagSpec{
				spec.idFlag:   valueFlag,
				"reason":      valueFlag,
				"from-file":   valueFlag,
				"dry-run":     boolFlag,
			}
			if verb != "purge" {
				flags["actor-id"] = valueFlag
			}
			out[spec.resource+" "+verb] = flags
		}
	}
	return out
}
