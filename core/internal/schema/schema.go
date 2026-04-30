package schema

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

type EnumPolicy string

const (
	EnumPolicyStrict EnumPolicy = "strict"
	EnumPolicyOpen   EnumPolicy = "open"
)

type EnumSpec struct {
	Policy       EnumPolicy
	Values       map[string]struct{}
	OrderedValue []string
	Groups       map[string][]string
	BackingTypes []string
	BackingKinds []string
}

type FieldSpec struct {
	Type     string
	Required bool
	MinItems *int
	Ref      string
}

type ProvenanceSpec struct {
	Fields map[string]FieldSpec
}

type ThreadSchema struct {
	Name   string
	Fields map[string]FieldSpec
}

type EventRefRule struct {
	ThreadID           string
	RefsMustInclude    []string
	RefsConditional    string
	PayloadMustInclude []string
	ConditionalRefs    []ConditionalRefRule
}

type ConditionalRefRule struct {
	When      WhenCondition
	MustHave  []RefPrefixRequirement
	Condition string
}

type WhenCondition struct {
	PayloadField string
	Equals       string
}

type RefPrefixRequirement struct {
	Prefix string
}

type Contract struct {
	Version          string
	Enums            map[string]EnumSpec
	TypedRefPrefixes map[string]struct{}
	Provenance       ProvenanceSpec
	Threads          map[string]ThreadSchema
	EventRefRules    map[string]EventRefRule
}

func (c *Contract) HasKnownTypedRefPrefix(prefix string) bool {
	_, ok := c.TypedRefPrefixes[prefix]
	return ok
}

type contractFile struct {
	Version    string `yaml:"version"`
	Enums      map[string]rawEnum
	RefFormat  rawRefFormat `yaml:"ref_format"`
	Provenance rawProvenance
	Primitives rawThreads `yaml:"primitives"`
	// LegacyThreadYAML holds deprecated schema YAML keyed as `snapshots` from pre-refactor
	// bundles; kept only so older checked-in schema files still parse. Canonical thread
	// field definitions live under `primitives.thread`.
	LegacyThreadYAML     rawThreads              `yaml:"snapshots"`
	ReferenceConventions rawReferenceConventions `yaml:"reference_conventions"`
}

type rawEnum struct {
	EnumPolicy        string              `yaml:"enum_policy"`
	Values            []string            `yaml:"values"`
	Groups            map[string][]string `yaml:"groups"`
	BackingEventTypes []string            `yaml:"backing_event_types"`
	BackingKinds      []string            `yaml:"backing_kinds"`
}

type rawRefFormat struct {
	Prefixes map[string]string `yaml:"prefixes"`
}

type rawProvenance struct {
	Fields map[string]rawFieldSpec `yaml:"fields"`
}

type rawReferenceConventions struct {
	EventRefs rawEventRefConventions `yaml:"event_refs"`
}

type rawEventRefConventions struct {
	Rules map[string]rawEventRefRule
}

func (c *rawEventRefConventions) UnmarshalYAML(value *yaml.Node) error {
	if value == nil || value.Kind != yaml.MappingNode {
		c.Rules = nil
		return nil
	}

	c.Rules = make(map[string]rawEventRefRule)
	for i := 0; i+1 < len(value.Content); i += 2 {
		keyNode := value.Content[i]
		valueNode := value.Content[i+1]
		key := strings.TrimSpace(keyNode.Value)
		if key == "" || strings.HasPrefix(key, "_") {
			continue
		}

		var rule rawEventRefRule
		if err := valueNode.Decode(&rule); err != nil {
			return err
		}
		c.Rules[key] = rule
	}
	return nil
}

type rawEventRefRule struct {
	ThreadID           string              `yaml:"thread_id"`
	RefsMustInclude    []string            `yaml:"refs_must_include"`
	RefsConditional    string              `yaml:"refs_conditional"`
	PayloadMustInclude []string            `yaml:"payload_must_include"`
	ConditionalRefs    []rawConditionalRef `yaml:"conditional_refs"`
}

type rawConditionalRef struct {
	When      rawWhenCondition  `yaml:"when"`
	MustHave  []rawRefPrefixReq `yaml:"must_have"`
	Condition string            `yaml:"condition"`
}

type rawWhenCondition struct {
	PayloadField string `yaml:"payload_field"`
	Equals       string `yaml:"equals"`
}

type rawRefPrefixReq struct {
	Prefix string `yaml:"prefix"`
}

type rawThreads struct {
	Thread rawThreadSchema `yaml:"thread"`
}

type rawThreadSchema struct {
	Fields map[string]rawFieldSpec `yaml:"fields"`
}

type rawFieldSpec struct {
	Type     string `yaml:"type"`
	Required bool   `yaml:"required"`
	MinItems *int   `yaml:"min_items"`
	Ref      string `yaml:"ref"`
}

func Load(path string) (*Contract, error) {
	bytes, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read schema file: %w", err)
	}

	var file contractFile
	if err := yaml.Unmarshal(bytes, &file); err != nil {
		return nil, fmt.Errorf("decode schema yaml: %w", err)
	}

	contract := &Contract{
		Version:          strings.TrimSpace(file.Version),
		Enums:            make(map[string]EnumSpec, len(file.Enums)),
		TypedRefPrefixes: make(map[string]struct{}),
		Provenance: ProvenanceSpec{
			Fields: make(map[string]FieldSpec, len(file.Provenance.Fields)),
		},
		Threads:       make(map[string]ThreadSchema, 1),
		EventRefRules: make(map[string]EventRefRule, len(file.ReferenceConventions.EventRefs.Rules)),
	}

	if contract.Version == "" {
		return nil, fmt.Errorf("schema version not found in %s", path)
	}

	for name, enum := range file.Enums {
		spec, err := normalizeEnum(name, enum)
		if err != nil {
			return nil, err
		}
		contract.Enums[name] = spec
	}

	for refPattern := range file.RefFormat.Prefixes {
		idx := strings.Index(refPattern, ":")
		if idx <= 0 {
			return nil, fmt.Errorf("invalid ref_format prefix pattern %q", refPattern)
		}
		prefix := strings.TrimSpace(refPattern[:idx])
		if prefix == "" {
			return nil, fmt.Errorf("invalid ref_format prefix pattern %q", refPattern)
		}
		contract.TypedRefPrefixes[prefix] = struct{}{}
	}

	for name, field := range file.Provenance.Fields {
		contract.Provenance.Fields[name] = FieldSpec{
			Type:     field.Type,
			Required: field.Required,
			MinItems: field.MinItems,
			Ref:      field.Ref,
		}
	}

	threadSource := file.Primitives
	if len(threadSource.Thread.Fields) == 0 {
		threadSource = file.LegacyThreadYAML
	}
	contract.Threads["thread"] = normalizeThread("thread", threadSource.Thread)

	for eventType, rawRule := range file.ReferenceConventions.EventRefs.Rules {
		contract.EventRefRules[eventType] = normalizeEventRefRule(rawRule)
	}

	return contract, nil
}

func normalizeEventRefRule(raw rawEventRefRule) EventRefRule {
	rule := EventRefRule{
		ThreadID:           strings.TrimSpace(raw.ThreadID),
		RefsMustInclude:    append([]string(nil), raw.RefsMustInclude...),
		RefsConditional:    strings.TrimSpace(raw.RefsConditional),
		PayloadMustInclude: append([]string(nil), raw.PayloadMustInclude...),
		ConditionalRefs:    make([]ConditionalRefRule, 0, len(raw.ConditionalRefs)),
	}

	for _, cr := range raw.ConditionalRefs {
		mustHave := make([]RefPrefixRequirement, len(cr.MustHave))
		for i, m := range cr.MustHave {
			mustHave[i] = RefPrefixRequirement{Prefix: m.Prefix}
		}
		rule.ConditionalRefs = append(rule.ConditionalRefs, ConditionalRefRule{
			When: WhenCondition{
				PayloadField: strings.TrimSpace(cr.When.PayloadField),
				Equals:       strings.TrimSpace(cr.When.Equals),
			},
			MustHave:  mustHave,
			Condition: strings.TrimSpace(cr.Condition),
		})
	}

	return rule
}

func normalizeEnum(name string, enum rawEnum) (EnumSpec, error) {
	spec := EnumSpec{
		Values:       make(map[string]struct{}, len(enum.Values)),
		OrderedValue: append([]string(nil), enum.Values...),
		Groups:       make(map[string][]string, len(enum.Groups)),
		BackingTypes: compactSortedStrings(enum.BackingEventTypes),
		BackingKinds: compactSortedStrings(enum.BackingKinds),
	}

	policy := EnumPolicy(strings.TrimSpace(enum.EnumPolicy))
	switch policy {
	case EnumPolicyStrict, EnumPolicyOpen:
		spec.Policy = policy
	default:
		return EnumSpec{}, fmt.Errorf("unsupported enum policy %q for %s", enum.EnumPolicy, name)
	}

	for _, value := range enum.Values {
		spec.Values[value] = struct{}{}
	}

	for group, values := range enum.Groups {
		group = strings.TrimSpace(group)
		if group == "" {
			continue
		}
		spec.Groups[group] = compactSortedStrings(values)
		for _, value := range spec.Groups[group] {
			if _, ok := spec.Values[value]; !ok {
				return EnumSpec{}, fmt.Errorf("enum %s group %s references unknown value %q", name, group, value)
			}
		}
	}
	for _, value := range spec.BackingTypes {
		if _, ok := spec.Values[value]; !ok {
			return EnumSpec{}, fmt.Errorf("enum %s backing_event_types references unknown value %q", name, value)
		}
	}
	for _, value := range spec.BackingKinds {
		if _, ok := spec.Values[value]; !ok {
			return EnumSpec{}, fmt.Errorf("enum %s backing_kinds references unknown value %q", name, value)
		}
	}

	sort.Strings(spec.OrderedValue)
	return spec, nil
}

func compactSortedStrings(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func normalizeThread(name string, raw rawThreadSchema) ThreadSchema {
	thread := ThreadSchema{
		Name:   name,
		Fields: make(map[string]FieldSpec, len(raw.Fields)),
	}

	for fieldName, field := range raw.Fields {
		thread.Fields[fieldName] = FieldSpec{
			Type:     field.Type,
			Required: field.Required,
			MinItems: field.MinItems,
			Ref:      field.Ref,
		}
	}

	return thread
}
