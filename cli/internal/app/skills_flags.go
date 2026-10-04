package app

import (
	"fmt"
)

type skillsFlagDefinition struct {
	name        string
	kind        preflightFlagKind
	valueUsage  string
	description string
	internal    bool
}

// skillsFlagDefinitions is the single source for parsing, preflight validation
// and generated helper metadata. Keep private worker flags out of user help.
var skillsFlagDefinitions = map[string][]skillsFlagDefinition{
	"sync": {
		{name: "dry-run", kind: preflightFlagBool, description: "Inspect without writes."},
		{name: "pm", kind: preflightFlagBool, description: "Enable and remember optional PM skill delivery."},
		{name: "no-pm", kind: preflightFlagBool, description: "Disable and remember optional PM skill delivery."},
		{name: "auto-sync", kind: preflightFlagBool, description: "Enable detached automatic refresh."},
		{name: "no-auto-sync", kind: preflightFlagBool, description: "Disable detached automatic refresh."},
		{name: "home", kind: preflightFlagString, valueUsage: "<dir>", description: "Use an alternate home directory."},
		{name: "scheduled", kind: preflightFlagBool, description: "Detached internal refresh invocation.", internal: true},
	},
	"adopt": {
		{name: "expected-digest", kind: preflightFlagString, valueUsage: "<digest>", description: "Confirm the digest shown by the read-only plan."},
		{name: "role", kind: preflightFlagString, valueUsage: "<role>", description: "participant or pm; defaults to participant."},
	},
	"configure": {
		{name: "path", kind: preflightFlagString, valueUsage: "<skill-directory>", description: "Explicit destination; no harness discovery."},
		{name: "role", kind: preflightFlagString, valueUsage: "<role>", description: "participant or pm; PM supplements participant."},
		{name: "dry-run", kind: preflightFlagBool, description: "Inspect without writing."},
	},
	"status": {
		{name: "path", kind: preflightFlagString, valueUsage: "<skill-directory>", description: "Inspect one explicit local copy."},
		{name: "role", kind: preflightFlagString, valueUsage: "<role>", description: "Required with --path; participant or pm."},
		{name: "home", kind: preflightFlagString, valueUsage: "<dir>", description: "Use an alternate home directory."},
	},
	"verify": {
		{name: "path", kind: preflightFlagString, valueUsage: "<skill-directory>", description: "Explicit destination; no harness discovery."},
		{name: "role", kind: preflightFlagString, valueUsage: "<role>", description: "participant or pm."},
	},
}

func skillsFlagDefinitionFor(command, name string) (skillsFlagDefinition, bool) {
	for _, definition := range skillsFlagDefinitions[command] {
		if definition.name == name {
			return definition, true
		}
	}
	return skillsFlagDefinition{}, false
}

func skillsPreflightFlagSpecs() map[string]map[string]preflightFlagSpec {
	specs := make(map[string]map[string]preflightFlagSpec, len(skillsFlagDefinitions))
	for command, definitions := range skillsFlagDefinitions {
		flags := make(map[string]preflightFlagSpec, len(definitions))
		for _, definition := range definitions {
			flags[definition.name] = preflightFlagSpec{kind: definition.kind}
		}
		specs["skills "+command] = flags
	}
	return specs
}

func skillsLocalHelperFlags(command string) []localHelperFlag {
	definitions := skillsFlagDefinitions[command]
	flags := make([]localHelperFlag, 0, len(definitions))
	for _, definition := range definitions {
		if definition.internal {
			continue
		}
		name := "--" + definition.name
		if definition.valueUsage != "" {
			name += " " + definition.valueUsage
		}
		flags = append(flags, localHelperFlag{Name: name, Description: definition.description})
	}
	return flags
}

type parsedSkillsFlags struct {
	bools       map[string]*trackedBool
	strings     map[string]*trackedString
	positionals []string
}

func parseSkillsFlags(command string, args []string) (*parsedSkillsFlags, error) {
	definitions, ok := skillsFlagDefinitions[command]
	if !ok {
		return nil, fmt.Errorf("unknown skills command %q", command)
	}
	fs := newSilentFlagSet("skills " + command)
	parsed := &parsedSkillsFlags{bools: map[string]*trackedBool{}, strings: map[string]*trackedString{}}
	for _, definition := range definitions {
		switch definition.kind {
		case preflightFlagBool:
			value := &trackedBool{}
			parsed.bools[definition.name] = value
			fs.Var(value, definition.name, definition.description)
		case preflightFlagString:
			value := &trackedString{}
			parsed.strings[definition.name] = value
			fs.Var(value, definition.name, definition.description)
		default:
			return nil, fmt.Errorf("unsupported flag kind for skills %s --%s", command, definition.name)
		}
	}
	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	parsed.positionals = fs.Args()
	return parsed, nil
}

func (p *parsedSkillsFlags) boolValue(name string) trackedBool {
	if p == nil || p.bools[name] == nil {
		return trackedBool{}
	}
	return *p.bools[name]
}

func (p *parsedSkillsFlags) stringValue(name string) trackedString {
	if p == nil || p.strings[name] == nil {
		return trackedString{}
	}
	return *p.strings[name]
}
