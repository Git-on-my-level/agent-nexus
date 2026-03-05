package registry

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	contractsclient "organization-autorunner-contracts-go-client/client"
)

//go:embed commands.json
var embeddedCommandsJSON []byte

//go:embed concepts.json
var embeddedConceptsJSON []byte

//go:embed help.json
var embeddedHelpJSON []byte

type Example struct {
	Title       string `json:"title"`
	Command     string `json:"command"`
	Description string `json:"description,omitempty"`
}

type BodyField struct {
	Name       string   `json:"name"`
	Type       string   `json:"type"`
	EnumValues []string `json:"enum_values,omitempty"`
	EnumPolicy string   `json:"enum_policy,omitempty"`
}

type BodySchema struct {
	Required []BodyField `json:"required,omitempty"`
	Optional []BodyField `json:"optional,omitempty"`
}

type Command struct {
	CommandID      string            `json:"command_id"`
	CLIPath        string            `json:"cli_path"`
	Group          string            `json:"group,omitempty"`
	Method         string            `json:"method"`
	Path           string            `json:"path"`
	OperationID    string            `json:"operation_id,omitempty"`
	Summary        string            `json:"summary,omitempty"`
	Description    string            `json:"description,omitempty"`
	Why            string            `json:"why,omitempty"`
	InputMode      string            `json:"input_mode,omitempty"`
	Streaming      map[string]any    `json:"streaming,omitempty"`
	OutputEnvelope string            `json:"output_envelope,omitempty"`
	ErrorCodes     []string          `json:"error_codes,omitempty"`
	Concepts       []string          `json:"concepts,omitempty"`
	Stability      string            `json:"stability,omitempty"`
	AgentNotes     string            `json:"agent_notes,omitempty"`
	Examples       []Example         `json:"examples,omitempty"`
	BodySchema     *BodySchema       `json:"body_schema,omitempty"`
	PathParams     []string          `json:"path_params,omitempty"`
	Adjacent       []string          `json:"adjacent_commands,omitempty"`
	GoMethod       string            `json:"go_method,omitempty"`
	TSMethod       string            `json:"ts_method,omitempty"`
	Extra          map[string]string `json:"-"`
}

type MetaRegistry struct {
	OpenAPIVersion  string    `json:"openapi_version"`
	ContractVersion string    `json:"contract_version"`
	GeneratedBy     string    `json:"generated_by"`
	ExtensionPrefix string    `json:"extension_prefix"`
	CommandCount    int       `json:"command_count"`
	Commands        []Command `json:"commands"`
}

type Concept struct {
	Name         string    `json:"name"`
	CommandCount int       `json:"command_count"`
	CommandIDs   []string  `json:"command_ids"`
	Commands     []Command `json:"commands,omitempty"`
}

type ConceptsRegistry struct {
	OpenAPIVersion  string    `json:"openapi_version"`
	ContractVersion string    `json:"contract_version"`
	GeneratedBy     string    `json:"generated_by"`
	ConceptCount    int       `json:"concept_count"`
	Concepts        []Concept `json:"concepts"`
}

type Group struct {
	Name         string   `json:"name"`
	CommandCount int      `json:"command_count"`
	CommandIDs   []string `json:"command_ids"`
}

type HelpRegistry struct {
	OpenAPIVersion  string    `json:"openapi_version"`
	ContractVersion string    `json:"contract_version"`
	GeneratedBy     string    `json:"generated_by"`
	GroupCount      int       `json:"group_count"`
	Groups          []Group   `json:"groups"`
	CommandCount    int       `json:"command_count"`
	Commands        []Command `json:"commands"`
}

func CommandSpecs() []contractsclient.CommandSpec {
	out := make([]contractsclient.CommandSpec, len(contractsclient.CommandRegistry))
	copy(out, contractsclient.CommandRegistry)
	return out
}

func EmbeddedCommandsJSON() []byte {
	out := make([]byte, len(embeddedCommandsJSON))
	copy(out, embeddedCommandsJSON)
	return out
}

func EmbeddedConceptsJSON() []byte {
	out := make([]byte, len(embeddedConceptsJSON))
	copy(out, embeddedConceptsJSON)
	return out
}

func EmbeddedHelpJSON() []byte {
	out := make([]byte, len(embeddedHelpJSON))
	copy(out, embeddedHelpJSON)
	return out
}

func LoadEmbedded() (MetaRegistry, error) {
	return parseMeta(embeddedCommandsJSON)
}

func LoadEmbeddedConcepts() (ConceptsRegistry, error) {
	return parseConcepts(embeddedConceptsJSON)
}

func LoadEmbeddedHelp() (HelpRegistry, error) {
	return parseHelp(embeddedHelpJSON)
}

func LoadFromFile(path string) (MetaRegistry, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return MetaRegistry{}, fmt.Errorf("read registry file: %w", err)
	}
	return parseMeta(content)
}

func parseMeta(content []byte) (MetaRegistry, error) {
	var out MetaRegistry
	if err := json.Unmarshal(content, &out); err != nil {
		return MetaRegistry{}, fmt.Errorf("decode registry metadata: %w", err)
	}
	if out.CommandCount != len(out.Commands) {
		return MetaRegistry{}, fmt.Errorf("command_count mismatch: count=%d commands=%d", out.CommandCount, len(out.Commands))
	}
	sort.Slice(out.Commands, func(i, j int) bool { return out.Commands[i].CommandID < out.Commands[j].CommandID })
	return out, nil
}

func parseConcepts(content []byte) (ConceptsRegistry, error) {
	var out ConceptsRegistry
	if err := json.Unmarshal(content, &out); err != nil {
		return ConceptsRegistry{}, fmt.Errorf("decode concepts metadata: %w", err)
	}
	if out.ConceptCount != len(out.Concepts) {
		return ConceptsRegistry{}, fmt.Errorf("concept_count mismatch: count=%d concepts=%d", out.ConceptCount, len(out.Concepts))
	}
	sort.Slice(out.Concepts, func(i, j int) bool { return out.Concepts[i].Name < out.Concepts[j].Name })
	return out, nil
}

func parseHelp(content []byte) (HelpRegistry, error) {
	var out HelpRegistry
	if err := json.Unmarshal(content, &out); err != nil {
		return HelpRegistry{}, fmt.Errorf("decode help metadata: %w", err)
	}
	if out.GroupCount != len(out.Groups) {
		return HelpRegistry{}, fmt.Errorf("group_count mismatch: count=%d groups=%d", out.GroupCount, len(out.Groups))
	}
	if out.CommandCount != len(out.Commands) {
		return HelpRegistry{}, fmt.Errorf("command_count mismatch: count=%d commands=%d", out.CommandCount, len(out.Commands))
	}
	sort.Slice(out.Groups, func(i, j int) bool { return out.Groups[i].Name < out.Groups[j].Name })
	sort.Slice(out.Commands, func(i, j int) bool { return out.Commands[i].CommandID < out.Commands[j].CommandID })
	return out, nil
}

func (m MetaRegistry) CommandByID(commandID string) (Command, bool) {
	commandID = strings.TrimSpace(commandID)
	for _, cmd := range m.Commands {
		if strings.TrimSpace(cmd.CommandID) == commandID {
			return cmd, true
		}
	}
	return Command{}, false
}

func (m MetaRegistry) CommandsByCLIPathPrefix(prefix string) []Command {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		return nil
	}
	needle := prefix + " "
	out := make([]Command, 0)
	for _, cmd := range m.Commands {
		path := strings.TrimSpace(cmd.CLIPath)
		if path == prefix || strings.HasPrefix(path, needle) {
			out = append(out, cmd)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CLIPath < out[j].CLIPath })
	return out
}
