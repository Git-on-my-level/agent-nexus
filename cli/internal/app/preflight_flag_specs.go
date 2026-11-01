// Flag spec machinery for usage preflight: spec types, matching, usage text
// rendering, and the per-command spec lookup that merges manual, registry and
// runtime-flag sources.

package app

import (
	"fmt"
	"strings"

	"agent-nexus-cli/internal/errnorm"
)

type preflightFlagKind int

const (
	preflightFlagString preflightFlagKind = iota
	preflightFlagBool
)

type preflightFlagSpec struct {
	kind            preflightFlagKind
	requireNonEmpty bool
	validate        func(string) error
}

func matchPreflightFlagSpec(args []string) (string, []string, bool) {
	specs := preflightFlagSpecs()
	for width := preflightMinInt(len(args), 3); width >= 1; width-- {
		path := strings.Join(args[:width], " ")
		if _, ok := specs[path]; ok {
			return path, args[width:], true
		}
	}
	return "", nil, false
}

func preflightFlagUsage(args []string, spec map[string]preflightFlagSpec) error {
	seen := map[string]bool{}
	validationValues := map[string]string{}
	for i := 0; i < len(args); i++ {
		arg := strings.TrimSpace(args[i])
		if arg == "" {
			continue
		}
		if arg == "--" {
			break
		}
		if arg == "-" {
			continue
		}
		if !strings.HasPrefix(arg, "-") {
			continue
		}
		name, value, hasValue := strings.Cut(strings.TrimLeft(arg, "-"), "=")
		name = strings.TrimSpace(name)
		flagSpec, ok := spec[name]
		if !ok {
			return errnorm.Usage("invalid_flags", fmt.Sprintf("flag provided but not defined: -%s", name))
		}
		seen[name] = true
		switch flagSpec.kind {
		case preflightFlagBool:
			if hasValue {
				if _, err := strconvParseBool(value); err != nil {
					return errnorm.Usage("invalid_flags", fmt.Sprintf("invalid boolean value %q for --%s", value, name))
				}
			}
		default:
			if !hasValue {
				if i+1 >= len(args) || (strings.HasPrefix(args[i+1], "-") && !looksLikeStringFlagValue(args[i+1])) {
					return errnorm.Usage("invalid_flags", fmt.Sprintf("flag needs an argument: -%s", name))
				}
				if flagSpec.requireNonEmpty && strings.TrimSpace(args[i+1]) == "" {
					return errnorm.Usage("invalid_request", fmt.Sprintf("--%s must not be empty", name))
				}
				i++
				value = args[i]
			} else if flagSpec.requireNonEmpty && strings.TrimSpace(value) == "" {
				return errnorm.Usage("invalid_request", fmt.Sprintf("--%s must not be empty", name))
			}
			if flagSpec.validate != nil {
				validationValues[name] = value
			}
		}
	}
	for name, value := range validationValues {
		if err := spec[name].validate(value); err != nil {
			return err
		}
	}
	return validateLifecycleFilterFlags(seen["include-archived"], seen["archived-only"], seen["include-trashed"], seen["trashed-only"])
}

func preflightFlagSpecs() map[string]map[string]preflightFlagSpec {
	specs := map[string]map[string]preflightFlagSpec{
		"host enroll": {"name": {kind: preflightFlagString}, "exclude": {kind: preflightFlagString}, "token": {kind: preflightFlagString}, "token-stdin": {kind: preflightFlagBool}, "plan": {kind: preflightFlagBool}},
		"host revoke": {}, "host enrollments list": {}, "host enrollments approve": {}, "host enrollments deny": {},
		"host tokens list": {}, "host tokens revoke": {}, "auth admins list": {"limit": {kind: preflightFlagString}, "cursor": {kind: preflightFlagString}}, "auth admins grant": {}, "auth admins revoke": {},
	}
	addLayer := func(layer map[string]map[string]preflightFlagSpec) {
		for path, flags := range layer {
			current := specs[path]
			if current == nil {
				current = map[string]preflightFlagSpec{}
				specs[path] = current
			}
			for name, spec := range flags {
				current[name] = spec
			}
		}
	}
	addLayer(map[string]map[string]preflightFlagSpec{
		"await":      {"until": {kind: preflightFlagString}, "timeout": {kind: preflightFlagString}, "answers": {kind: preflightFlagBool}, "settle": {kind: preflightFlagString}, "all": {kind: preflightFlagBool}},
		"work start": {},
		"work block": {"again": {kind: preflightFlagBool}, "situation": {kind: preflightFlagString}, "impact": {kind: preflightFlagString}, "why": {kind: preflightFlagString}, "if-no-answer": {kind: preflightFlagString}, "details": {kind: preflightFlagString}, "alternative": {kind: preflightFlagString}, "ask": {kind: preflightFlagBool}, "recommend": {kind: preflightFlagString}, "alt": {kind: preflightFlagString}, "body": {kind: preflightFlagString}, "body-file": {kind: preflightFlagString}, "force": {kind: preflightFlagBool}, "reason": {kind: preflightFlagString}},
	})
	for _, kind := range []string{"ask", "review", "escalate"} {
		addLayer(map[string]map[string]preflightFlagSpec{kind: {
			"preview":   {kind: preflightFlagBool},
			"situation": {kind: preflightFlagString}, "impact": {kind: preflightFlagString}, "why": {kind: preflightFlagString}, "if-no-answer": {kind: preflightFlagString}, "details": {kind: preflightFlagString}, "alternative": {kind: preflightFlagString},
			"template": {kind: preflightFlagString}, "force": {kind: preflightFlagBool}, "again": {kind: preflightFlagBool}, "reason": {kind: preflightFlagString}, "supersedes": {kind: preflightFlagString}, "evidence": {kind: preflightFlagString}, "on-answer": {kind: preflightFlagString}, "webhook": {kind: preflightFlagString}, "delivery": {kind: preflightFlagString},
			"recommend": {kind: preflightFlagString}, "alt": {kind: preflightFlagString},
			"top-level": {kind: preflightFlagBool}, "under": {kind: preflightFlagString},
			"from-file": {kind: preflightFlagString}, "subject-ref": {kind: preflightFlagString},
			"thread-id": {kind: preflightFlagString}, "ref": {kind: preflightFlagString},
			"body": {kind: preflightFlagString}, "body-file": {kind: preflightFlagString},
			"title": {kind: preflightFlagString}, "request-id": {kind: preflightFlagString},
			"requester-actor-id": {kind: preflightFlagString}, "requester-agent-id": {kind: preflightFlagString},
			"requester-label": {kind: preflightFlagString}, "coverage-hint": {kind: preflightFlagString},
			"severity": {kind: preflightFlagString}, "actor-id": {kind: preflightFlagString},
			"dry-run": {kind: preflightFlagBool},
			"by":      {kind: preflightFlagString}, "blocks": {kind: preflightFlagString}, "priority": {kind: preflightFlagString},
		}})
	}
	// Lowest precedence: lifecycle verbs derived from the resource registry.
	// `manualPreflightFlagSpecs` and `localHelperTopics` may still override.
	addLayer(derivedLifecyclePreflightSpecs())
	addLayer(resourceRuntimePreflightSpecs())
	for _, topic := range localHelperTopics {
		if strings.HasPrefix(topic.Path, "skills ") {
			continue
		}
		flags := map[string]preflightFlagSpec{}
		for _, flag := range topic.Flags {
			name, kind, ok := parseLocalHelperFlagSpec(flag.Name)
			if ok {
				flags[name] = preflightFlagSpec{kind: kind}
			}
		}
		if len(flags) > 0 {
			addLayer(map[string]map[string]preflightFlagSpec{strings.Join(strings.Fields(topic.Path), " "): flags})
		}
	}
	for _, name := range []string{"work receipt", "work cadence", "work goal"} {
		flags := map[string]preflightFlagSpec{"card-id": {kind: preflightFlagString}}
		for _, key := range loopWorkValueFlags(name) {
			flags[key] = preflightFlagSpec{kind: preflightFlagString}
		}
		if name == "work receipt" {
			flags["metric"] = preflightFlagSpec{kind: preflightFlagString}
			flags["ref"] = preflightFlagSpec{kind: preflightFlagString}
		} else {
			flags["clear"] = preflightFlagSpec{kind: preflightFlagBool}
		}
		addLayer(map[string]map[string]preflightFlagSpec{name: flags})
	}
	addLayer(manualPreflightFlagSpecs())
	addLayer(skillsPreflightFlagSpecs())
	return specs
}

func preflightSpecsFromRuntimeFlags(flags []runtimeCommandFlagSpec) map[string]preflightFlagSpec {
	out := map[string]preflightFlagSpec{}
	for _, flag := range flags {
		out[flag.name] = preflightFlagSpec{kind: flag.kind}
	}
	return out
}

func parseLocalHelperFlagSpec(raw string) (string, preflightFlagKind, bool) {
	parts := strings.Fields(strings.TrimSpace(raw))
	if len(parts) == 0 {
		return "", preflightFlagString, false
	}
	name := strings.TrimPrefix(strings.TrimSpace(parts[0]), "--")
	if name == "" || strings.HasPrefix(name, "-") {
		return "", preflightFlagString, false
	}
	if len(parts) == 1 {
		return name, preflightFlagBool, true
	}
	return name, preflightFlagString, true
}
