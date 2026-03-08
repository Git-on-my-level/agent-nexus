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
}

type FieldSpec struct {
	Type     string
	Required bool
	MinItems *int
	Ref      string
}

type PacketSchema struct {
	Name   string
	Kind   string
	Fields map[string]FieldSpec
}

type ProvenanceSpec struct {
	Fields map[string]FieldSpec
}

type SnapshotSchema struct {
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
	Snapshots        map[string]SnapshotSchema
	Packets          map[string]PacketSchema
	ArtifactRefRules map[string][]string
	EventRefRules    map[string]EventRefRule
}

func (c *Contract) HasKnownTypedRefPrefix(prefix string) bool {
	_, ok := c.TypedRefPrefixes[prefix]
	return ok
}

type contractFile struct {
	Version              string `yaml:"version"`
	Enums                map[string]rawEnum
	RefFormat            rawRefFormat `yaml:"ref_format"`
	Provenance           rawProvenance
	Snapshots            rawSnapshots
	Packets              rawPackets
	ReferenceConventions rawReferenceConventions `yaml:"reference_conventions"`
}

type rawEnum struct {
	EnumPolicy string   `yaml:"enum_policy"`
	Values     []string `yaml:"values"`
}

type rawRefFormat struct {
	Prefixes map[string]string `yaml:"prefixes"`
}

type rawProvenance struct {
	Fields map[string]rawFieldSpec `yaml:"fields"`
}

type rawPackets struct {
	WorkOrder rawPacketSchema `yaml:"work_order"`
	Receipt   rawPacketSchema `yaml:"receipt"`
	Review    rawPacketSchema `yaml:"review"`
}

type rawReferenceConventions struct {
	ArtifactRefs rawArtifactRefConventions `yaml:"artifact_refs"`
	EventRefs    rawEventRefConventions    `yaml:"event_refs"`
}

type rawArtifactRefConventions struct {
	WorkOrder rawArtifactRefRule `yaml:"work_order"`
	Receipt   rawArtifactRefRule `yaml:"receipt"`
	Review    rawArtifactRefRule `yaml:"review"`
}

type rawArtifactRefRule struct {
	RefsMustInclude []string `yaml:"refs_must_include"`
}

type rawEventRefConventions struct {
	WorkOrderCreated        rawEventRefRule `yaml:"work_order_created"`
	ReceiptAdded            rawEventRefRule `yaml:"receipt_added"`
	ReviewCompleted         rawEventRefRule `yaml:"review_completed"`
	CommitmentCreated       rawEventRefRule `yaml:"commitment_created"`
	CommitmentStatusChanged rawEventRefRule `yaml:"commitment_status_changed"`
	DecisionNeeded          rawEventRefRule `yaml:"decision_needed"`
	DecisionMade            rawEventRefRule `yaml:"decision_made"`
	SnapshotUpdated         rawEventRefRule `yaml:"snapshot_updated"`
	ExceptionRaised         rawEventRefRule `yaml:"exception_raised"`
	MessagePosted           rawEventRefRule `yaml:"message_posted"`
	InboxItemAcknowledged   rawEventRefRule `yaml:"inbox_item_acknowledged"`
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

type rawSnapshots struct {
	Thread     rawSnapshotSchema `yaml:"thread"`
	Commitment rawSnapshotSchema `yaml:"commitment"`
}

type rawSnapshotSchema struct {
	Fields map[string]rawFieldSpec `yaml:"fields"`
}

type rawPacketSchema struct {
	Kind   string                  `yaml:"kind"`
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
		Snapshots: make(map[string]SnapshotSchema, 2),
		Packets:   make(map[string]PacketSchema, 3),
		ArtifactRefRules: map[string][]string{
			"work_order": append([]string(nil), file.ReferenceConventions.ArtifactRefs.WorkOrder.RefsMustInclude...),
			"receipt":    append([]string(nil), file.ReferenceConventions.ArtifactRefs.Receipt.RefsMustInclude...),
			"review":     append([]string(nil), file.ReferenceConventions.ArtifactRefs.Review.RefsMustInclude...),
		},
		EventRefRules: make(map[string]EventRefRule, 11),
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

	contract.Snapshots["thread"] = normalizeSnapshot("thread", file.Snapshots.Thread)
	contract.Snapshots["commitment"] = normalizeSnapshot("commitment", file.Snapshots.Commitment)

	contract.Packets["work_order"] = normalizePacket("work_order", file.Packets.WorkOrder)
	contract.Packets["receipt"] = normalizePacket("receipt", file.Packets.Receipt)
	contract.Packets["review"] = normalizePacket("review", file.Packets.Review)

	contract.EventRefRules["work_order_created"] = normalizeEventRefRule(file.ReferenceConventions.EventRefs.WorkOrderCreated)
	contract.EventRefRules["receipt_added"] = normalizeEventRefRule(file.ReferenceConventions.EventRefs.ReceiptAdded)
	contract.EventRefRules["review_completed"] = normalizeEventRefRule(file.ReferenceConventions.EventRefs.ReviewCompleted)
	contract.EventRefRules["commitment_created"] = normalizeEventRefRule(file.ReferenceConventions.EventRefs.CommitmentCreated)
	contract.EventRefRules["commitment_status_changed"] = normalizeEventRefRule(file.ReferenceConventions.EventRefs.CommitmentStatusChanged)
	contract.EventRefRules["decision_needed"] = normalizeEventRefRule(file.ReferenceConventions.EventRefs.DecisionNeeded)
	contract.EventRefRules["decision_made"] = normalizeEventRefRule(file.ReferenceConventions.EventRefs.DecisionMade)
	contract.EventRefRules["snapshot_updated"] = normalizeEventRefRule(file.ReferenceConventions.EventRefs.SnapshotUpdated)
	contract.EventRefRules["exception_raised"] = normalizeEventRefRule(file.ReferenceConventions.EventRefs.ExceptionRaised)
	contract.EventRefRules["message_posted"] = normalizeEventRefRule(file.ReferenceConventions.EventRefs.MessagePosted)
	contract.EventRefRules["inbox_item_acknowledged"] = normalizeEventRefRule(file.ReferenceConventions.EventRefs.InboxItemAcknowledged)

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

	sort.Strings(spec.OrderedValue)
	return spec, nil
}

func normalizePacket(name string, raw rawPacketSchema) PacketSchema {
	packet := PacketSchema{
		Name:   name,
		Kind:   raw.Kind,
		Fields: make(map[string]FieldSpec, len(raw.Fields)),
	}

	for fieldName, field := range raw.Fields {
		packet.Fields[fieldName] = FieldSpec{
			Type:     field.Type,
			Required: field.Required,
			MinItems: field.MinItems,
			Ref:      field.Ref,
		}
	}

	return packet
}

func normalizeSnapshot(name string, raw rawSnapshotSchema) SnapshotSchema {
	snapshot := SnapshotSchema{
		Name:   name,
		Fields: make(map[string]FieldSpec, len(raw.Fields)),
	}

	for fieldName, field := range raw.Fields {
		snapshot.Fields[fieldName] = FieldSpec{
			Type:     field.Type,
			Required: field.Required,
			MinItems: field.MinItems,
			Ref:      field.Ref,
		}
	}

	return snapshot
}
