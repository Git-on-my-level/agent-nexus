package catalog

import (
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	ClassificationExposedRead              = "exposed_read"
	ClassificationExposedWrite             = "exposed_write"
	ClassificationGatedAdmin               = "gated_admin"
	ClassificationGatedSensitive           = "gated_sensitive"
	ClassificationAdapted                  = "adapted"
	ClassificationUnsupportedInteractive   = "unsupported_interactive"
	ClassificationUnsupportedStreaming     = "unsupported_streaming"
	ClassificationUnsupportedBootstrapAuth = "unsupported_bootstrap_auth"
	ClassificationUnsupportedShellShaped   = "unsupported_shell_shaped"
	ClassificationUnsupportedOther         = "unsupported_other"
)

var defaultAllowedClassifications = map[string]bool{
	ClassificationExposedRead:  true,
	ClassificationExposedWrite: true,
	ClassificationAdapted:      true,
}

var toolNameUnsafe = regexp.MustCompile(`[^a-zA-Z0-9_]`)

type CommandRegistry struct {
	Commands []Command `json:"commands"`
}

type Command struct {
	CommandID      string      `json:"command_id"`
	CLIPath        string      `json:"cli_path"`
	Group          string      `json:"group"`
	Method         string      `json:"method"`
	Path           string      `json:"path"`
	OperationID    string      `json:"operation_id"`
	Summary        string      `json:"summary"`
	Why            string      `json:"why"`
	InputMode      string      `json:"input_mode"`
	PathParams     []string    `json:"path_params"`
	BodySchema     FieldSchema `json:"body_schema"`
	OutputEnvelope string      `json:"output_envelope"`
}

type FieldSchema struct {
	Required []Field `json:"required"`
	Optional []Field `json:"optional"`
}

type Field struct {
	Name       string   `json:"name"`
	Type       string   `json:"type"`
	EnumValues []string `json:"enum_values"`
}

type Policy struct {
	SchemaVersion        int                    `yaml:"schema_version"`
	SourceCommands       string                 `yaml:"source_commands"`
	PolicyIntent         string                 `yaml:"policy_intent"`
	ValidClassifications []string               `yaml:"valid_classifications"`
	Commands             map[string]PolicyEntry `yaml:"commands"`
}

type PolicyEntry struct {
	Classification string `yaml:"classification"`
	Reason         string `yaml:"reason"`
}

type BuildOptions struct {
	AllowedClassifications map[string]bool
}

type Catalog struct {
	toolsByName map[string]Tool
	tools       []Tool
}

type Tool struct {
	Name         string         `json:"name"`
	Description  string         `json:"description,omitempty"`
	InputSchema  map[string]any `json:"inputSchema"`
	OutputSchema map[string]any `json:"outputSchema,omitempty"`
	Metadata     Metadata       `json:"metadata"`
}

type Metadata struct {
	CommandID      string `json:"command_id"`
	Method         string `json:"method"`
	Path           string `json:"path"`
	Group          string `json:"group,omitempty"`
	Classification string `json:"classification"`
	RiskClass      string `json:"risk_class"`
	PolicyReason   string `json:"policy_reason,omitempty"`
}

func LoadCommandRegistry(r io.Reader) (CommandRegistry, error) {
	var registry CommandRegistry
	if err := json.NewDecoder(r).Decode(&registry); err != nil {
		return CommandRegistry{}, err
	}
	return registry, nil
}

func LoadPolicy(r io.Reader) (Policy, error) {
	var policy Policy
	if err := yaml.NewDecoder(r).Decode(&policy); err != nil {
		return Policy{}, err
	}
	if policy.Commands == nil {
		policy.Commands = map[string]PolicyEntry{}
	}
	return policy, nil
}

func Build(registry CommandRegistry, policy Policy, opts BuildOptions) (*Catalog, error) {
	allowed := opts.AllowedClassifications
	if allowed == nil {
		allowed = defaultAllowedClassifications
	}

	valid := map[string]bool{}
	for _, classification := range policy.ValidClassifications {
		valid[classification] = true
	}

	seenCommands := map[string]bool{}
	toolsByName := map[string]Tool{}
	tools := make([]Tool, 0, len(registry.Commands))

	for _, command := range registry.Commands {
		if strings.TrimSpace(command.CommandID) == "" {
			return nil, fmt.Errorf("command missing command_id")
		}
		if seenCommands[command.CommandID] {
			return nil, fmt.Errorf("duplicate command_id %q", command.CommandID)
		}
		seenCommands[command.CommandID] = true

		entry, ok := policy.Commands[command.CommandID]
		if !ok {
			return nil, fmt.Errorf("missing policy entry for %s", command.CommandID)
		}
		if len(valid) > 0 && !valid[entry.Classification] {
			return nil, fmt.Errorf("invalid classification %q for %s", entry.Classification, command.CommandID)
		}
		if !allowed[entry.Classification] {
			continue
		}

		name := ToolName(command.CommandID)
		if existing, ok := toolsByName[name]; ok {
			return nil, fmt.Errorf("tool name collision %q for %s and %s", name, existing.Metadata.CommandID, command.CommandID)
		}

		tool := Tool{
			Name:         name,
			Description:  toolDescription(command),
			InputSchema:  inputSchema(command),
			OutputSchema: outputSchema(),
			Metadata: Metadata{
				CommandID:      command.CommandID,
				Method:         command.Method,
				Path:           command.Path,
				Group:          command.Group,
				Classification: entry.Classification,
				RiskClass:      riskClass(entry.Classification),
				PolicyReason:   entry.Reason,
			},
		}
		toolsByName[name] = tool
		tools = append(tools, tool)
	}

	for commandID := range policy.Commands {
		if !seenCommands[commandID] {
			return nil, fmt.Errorf("policy entry %s is not in command registry", commandID)
		}
	}

	sort.Slice(tools, func(i, j int) bool {
		return tools[i].Name < tools[j].Name
	})

	return &Catalog{toolsByName: toolsByName, tools: tools}, nil
}

func ToolName(commandID string) string {
	name := strings.ReplaceAll(commandID, ".", "_")
	name = strings.ReplaceAll(name, "-", "_")
	name = toolNameUnsafe.ReplaceAllString(name, "_")
	name = strings.Trim(name, "_")
	if name == "" {
		name = "command"
	}
	return "anx_" + name
}

func (c *Catalog) Tools() []Tool {
	out := make([]Tool, len(c.tools))
	copy(out, c.tools)
	return out
}

func (c *Catalog) Lookup(name string) (Tool, bool) {
	tool, ok := c.toolsByName[name]
	return tool, ok
}

func toolDescription(command Command) string {
	switch {
	case strings.TrimSpace(command.Summary) != "" && strings.TrimSpace(command.Why) != "":
		return strings.TrimSpace(command.Summary) + ". " + strings.TrimSpace(command.Why)
	case strings.TrimSpace(command.Summary) != "":
		return strings.TrimSpace(command.Summary)
	default:
		return command.CommandID
	}
}

func inputSchema(command Command) map[string]any {
	properties := map[string]any{}
	required := []string{}

	if len(command.PathParams) > 0 {
		pathProps := map[string]any{}
		pathRequired := make([]string, 0, len(command.PathParams))
		for _, name := range command.PathParams {
			pathProps[name] = map[string]any{"type": "string"}
			pathRequired = append(pathRequired, name)
		}
		properties["path"] = objectSchema(pathProps, pathRequired, false)
		required = append(required, "path")
	}

	if command.InputMode == "query" {
		properties["query"] = map[string]any{
			"type":                 "object",
			"additionalProperties": true,
		}
	}

	if len(command.BodySchema.Required) > 0 || len(command.BodySchema.Optional) > 0 || command.InputMode == "json-body" {
		bodyProps := map[string]any{}
		bodyRequired := make([]string, 0, len(command.BodySchema.Required))
		for _, field := range command.BodySchema.Required {
			bodyProps[field.Name] = jsonSchemaForField(field)
			bodyRequired = append(bodyRequired, field.Name)
		}
		for _, field := range command.BodySchema.Optional {
			bodyProps[field.Name] = jsonSchemaForField(field)
		}
		properties["body"] = objectSchema(bodyProps, bodyRequired, true)
		if len(bodyRequired) > 0 {
			required = append(required, "body")
		}
	}

	if command.Method != "GET" && command.Method != "HEAD" {
		properties["idempotency_key"] = map[string]any{
			"type":        "string",
			"description": "Optional client-supplied idempotency key for write calls.",
		}
	}

	if len(properties) == 0 {
		return objectSchema(map[string]any{}, nil, false)
	}
	return objectSchema(properties, required, false)
}

func objectSchema(properties map[string]any, required []string, additional bool) map[string]any {
	schema := map[string]any{
		"type":                 "object",
		"properties":           properties,
		"additionalProperties": additional,
	}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

func jsonSchemaForField(field Field) map[string]any {
	schema := map[string]any{}
	switch field.Type {
	case "string":
		schema["type"] = "string"
	case "integer", "int":
		schema["type"] = "integer"
	case "number", "float":
		schema["type"] = "number"
	case "boolean", "bool":
		schema["type"] = "boolean"
	case "object":
		schema["type"] = "object"
		schema["additionalProperties"] = true
	default:
		if strings.HasPrefix(field.Type, "list<") {
			schema["type"] = "array"
			inner := strings.TrimSuffix(strings.TrimPrefix(field.Type, "list<"), ">")
			if inner != "any" && inner != "" {
				schema["items"] = jsonSchemaForField(Field{Type: inner})
			}
		} else if field.Type != "any" && field.Type != "" {
			schema["description"] = "ANX field type: " + field.Type
		}
	}
	if len(field.EnumValues) > 0 {
		values := make([]any, 0, len(field.EnumValues))
		for _, value := range field.EnumValues {
			values = append(values, value)
		}
		schema["enum"] = values
	}
	if len(schema) == 0 {
		schema["description"] = "Any JSON value."
	}
	return schema
}

func outputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"command_id": map[string]any{"type": "string"},
			"status":     map[string]any{"type": "string"},
			"result": map[string]any{
				"type":                 "object",
				"additionalProperties": true,
			},
			"pagination": map[string]any{
				"type":                 "object",
				"additionalProperties": true,
			},
			"warnings": map[string]any{
				"type":  "array",
				"items": map[string]any{"type": "string"},
			},
		},
		"required":             []string{"command_id", "status"},
		"additionalProperties": true,
	}
}

func riskClass(classification string) string {
	switch classification {
	case ClassificationExposedRead:
		return "read"
	case ClassificationExposedWrite:
		return "write"
	case ClassificationGatedAdmin:
		return "admin"
	case ClassificationGatedSensitive:
		return "sensitive"
	case ClassificationAdapted:
		return "adapted"
	default:
		if strings.HasPrefix(classification, "unsupported_") {
			return "unsupported"
		}
		return "unknown"
	}
}
