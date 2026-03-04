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

type Contract struct {
	Version          string
	Enums            map[string]EnumSpec
	TypedRefPrefixes map[string]struct{}
	Provenance       ProvenanceSpec
	Snapshots        map[string]SnapshotSchema
	Packets          map[string]PacketSchema
	ArtifactRefRules map[string][]string
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
}

type rawArtifactRefConventions struct {
	WorkOrder rawArtifactRefRule `yaml:"work_order"`
	Receipt   rawArtifactRefRule `yaml:"receipt"`
	Review    rawArtifactRefRule `yaml:"review"`
}

type rawArtifactRefRule struct {
	RefsMustInclude []string `yaml:"refs_must_include"`
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

	return contract, nil
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
