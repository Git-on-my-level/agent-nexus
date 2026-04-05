package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"go/format"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

type openAPIDocument struct {
	OpenAPI    string              `yaml:"openapi"`
	Info       openAPIInfo         `yaml:"info"`
	Paths      map[string]pathItem `yaml:"paths"`
	Components openAPIComponents   `yaml:"components"`
}

type openAPIInfo struct {
	Version string `yaml:"version"`
}

type openAPIComponents struct {
	Schemas map[string]openAPISchema `yaml:"schemas"`
}

type pathItem struct {
	Get    *operation `yaml:"get"`
	Post   *operation `yaml:"post"`
	Put    *operation `yaml:"put"`
	Patch  *operation `yaml:"patch"`
	Delete *operation `yaml:"delete"`
}

type operation struct {
	OperationID string       `yaml:"operationId"`
	Summary     string       `yaml:"summary"`
	Description string       `yaml:"description"`
	CommandID   string       `yaml:"x-oar-command-id"`
	CLIPath     string       `yaml:"x-oar-cli-path"`
	Why         string       `yaml:"x-oar-why"`
	Examples    []oarExample `yaml:"x-oar-examples"`
	InputMode   string       `yaml:"x-oar-input-mode"`
	Streaming   any          `yaml:"x-oar-streaming"`
	Output      string       `yaml:"x-oar-output-envelope"`
	ErrorCodes  []string     `yaml:"x-oar-error-codes"`
	Concepts    []string     `yaml:"x-oar-concepts"`
	Stability   string       `yaml:"x-oar-stability"`
	Surface     string       `yaml:"x-oar-surface"`
	AgentNotes  string       `yaml:"x-oar-agent-notes"`
	RequestBody *requestBody `yaml:"requestBody"`
}

type requestBody struct {
	Required bool                 `yaml:"required"`
	Content  map[string]mediaType `yaml:"content"`
}

type mediaType struct {
	Schema *openAPISchema `yaml:"schema"`
}

type openAPISchema struct {
	Ref                  string                   `yaml:"$ref"`
	Type                 string                   `yaml:"type"`
	Format               string                   `yaml:"format"`
	Description          string                   `yaml:"description"`
	Enum                 []any                    `yaml:"enum"`
	Required             []string                 `yaml:"required"`
	Properties           map[string]openAPISchema `yaml:"properties"`
	Items                *openAPISchema           `yaml:"items"`
	OneOf                []openAPISchema          `yaml:"oneOf"`
	AnyOf                []openAPISchema          `yaml:"anyOf"`
	AllOf                []openAPISchema          `yaml:"allOf"`
	AdditionalProperties any                      `yaml:"additionalProperties"`
}

type oarSchemaDocument struct {
	Enums                map[string]oarEnumDef             `yaml:"enums"`
	Provenance           oarFieldContainer                 `yaml:"provenance"`
	Primitives           map[string]oarMaybeFieldContainer `yaml:"primitives"`
	Threads              map[string]oarMaybeFieldContainer `yaml:"-"`
	Packets              map[string]oarMaybeFieldContainer `yaml:"packets"`
	ReferenceConventions oarReferenceConventions           `yaml:"reference_conventions"`
}

type oarEnumDef struct {
	EnumPolicy string   `yaml:"enum_policy"`
	Values     []string `yaml:"values"`
}

type oarFieldContainer struct {
	Fields map[string]oarFieldDef `yaml:"fields"`
}

type oarFieldDef struct {
	Type        string `yaml:"type"`
	Required    bool   `yaml:"required"`
	Ref         string `yaml:"ref"`
	Description string `yaml:"description"`
}

type oarMaybeFieldContainer struct {
	Fields map[string]oarFieldDef `yaml:"fields"`
}

type oarReferenceConventions struct {
	EventRefs oarEventRefConventions `yaml:"event_refs"`
}

type oarEventRefConventions struct {
	Rules map[string]oarEventRefRule
}

func (c *oarEventRefConventions) UnmarshalYAML(value *yaml.Node) error {
	if value == nil || value.Kind != yaml.MappingNode {
		c.Rules = nil
		return nil
	}

	c.Rules = make(map[string]oarEventRefRule)
	for i := 0; i+1 < len(value.Content); i += 2 {
		keyNode := value.Content[i]
		valueNode := value.Content[i+1]
		key := strings.TrimSpace(keyNode.Value)
		if key == "" || strings.HasPrefix(key, "_") {
			continue
		}

		var rule oarEventRefRule
		if err := valueNode.Decode(&rule); err != nil {
			return err
		}
		c.Rules[key] = rule
	}
	return nil
}

type oarEventRefRule struct {
	ThreadID           string                  `yaml:"thread_id" json:"thread_id,omitempty"`
	RefsMustInclude    []string                `yaml:"refs_must_include" json:"refs_must_include,omitempty"`
	RefsConditional    string                  `yaml:"refs_conditional" json:"refs_conditional,omitempty"`
	PayloadMustInclude []string                `yaml:"payload_must_include" json:"payload_must_include,omitempty"`
	ConditionalRefs    []oarConditionalRefRule `yaml:"conditional_refs" json:"conditional_refs,omitempty"`
}

type oarConditionalRefRule struct {
	When      oarWhenCondition  `yaml:"when" json:"when"`
	MustHave  []oarRefPrefixReq `yaml:"must_have" json:"must_have,omitempty"`
	Condition string            `yaml:"condition" json:"condition,omitempty"`
}

type oarWhenCondition struct {
	PayloadField string `yaml:"payload_field" json:"payload_field"`
	Equals       string `yaml:"equals" json:"equals"`
}

type oarRefPrefixReq struct {
	Prefix string `yaml:"prefix" json:"prefix"`
}

func (c *oarMaybeFieldContainer) UnmarshalYAML(value *yaml.Node) error {
	if value == nil || value.Kind != yaml.MappingNode {
		c.Fields = nil
		return nil
	}
	type alias oarMaybeFieldContainer
	var decoded alias
	if err := value.Decode(&decoded); err != nil {
		return err
	}
	c.Fields = decoded.Fields
	return nil
}

func (d *oarSchemaDocument) UnmarshalYAML(value *yaml.Node) error {
	type alias oarSchemaDocument
	var decoded struct {
		alias           `yaml:",inline"`
		LegacySnapshots map[string]oarMaybeFieldContainer `yaml:"snapshots"`
	}
	if err := value.Decode(&decoded); err != nil {
		return err
	}
	*d = oarSchemaDocument(decoded.alias)
	d.Threads = make(map[string]oarMaybeFieldContainer, 1)
	if source, ok := d.Primitives["thread"]; ok {
		d.Threads["thread"] = source
		return nil
	}
	if source, ok := decoded.LegacySnapshots["thread"]; ok {
		d.Threads["thread"] = source
	}
	return nil
}

type oarExample struct {
	Title       string `yaml:"title" json:"title"`
	Command     string `yaml:"command" json:"command"`
	Description string `yaml:"description,omitempty" json:"description,omitempty"`
}

type command struct {
	CommandID   string       `json:"command_id"`
	CLIPath     string       `json:"cli_path"`
	Group       string       `json:"group,omitempty"`
	Method      string       `json:"method"`
	Path        string       `json:"path"`
	OperationID string       `json:"operation_id"`
	Summary     string       `json:"summary,omitempty"`
	Description string       `json:"description,omitempty"`
	Why         string       `json:"why,omitempty"`
	InputMode   string       `json:"input_mode,omitempty"`
	Streaming   any          `json:"streaming,omitempty"`
	Output      string       `json:"output_envelope,omitempty"`
	ErrorCodes  []string     `json:"error_codes,omitempty"`
	Concepts    []string     `json:"concepts,omitempty"`
	Stability   string       `json:"stability,omitempty"`
	Surface     string       `json:"surface,omitempty"`
	AgentNotes  string       `json:"agent_notes,omitempty"`
	Examples    []oarExample `json:"examples,omitempty"`
	BodySchema  *bodySchema  `json:"body_schema,omitempty"`
	PathParams  []string     `json:"path_params,omitempty"`
	Adjacent    []string     `json:"adjacent_commands,omitempty"`
	GoMethod    string       `json:"go_method"`
	TSMethod    string       `json:"ts_method"`
}

type bodySchema struct {
	Required []bodyField `json:"required,omitempty"`
	Optional []bodyField `json:"optional,omitempty"`
}

type bodyField struct {
	Name       string   `json:"name"`
	Type       string   `json:"type"`
	EnumValues []string `json:"enum_values,omitempty"`
	EnumPolicy string   `json:"enum_policy,omitempty"`
}

type conceptMeta struct {
	Name         string    `json:"name"`
	CommandCount int       `json:"command_count"`
	CommandIDs   []string  `json:"command_ids"`
	Commands     []command `json:"commands"`
}

type conceptsOutput struct {
	OpenAPIVersion  string        `json:"openapi_version"`
	ContractVersion string        `json:"contract_version"`
	GeneratedBy     string        `json:"generated_by"`
	ConceptCount    int           `json:"concept_count"`
	Concepts        []conceptMeta `json:"concepts"`
}

type groupMeta struct {
	Name         string   `json:"name"`
	CommandCount int      `json:"command_count"`
	CommandIDs   []string `json:"command_ids"`
}

type helpOutput struct {
	OpenAPIVersion  string      `json:"openapi_version"`
	ContractVersion string      `json:"contract_version"`
	GeneratedBy     string      `json:"generated_by"`
	GroupCount      int         `json:"group_count"`
	Groups          []groupMeta `json:"groups"`
	CommandCount    int         `json:"command_count"`
	Commands        []command   `json:"commands"`
}

type metaOutput struct {
	OpenAPIVersion  string    `json:"openapi_version"`
	ContractVersion string    `json:"contract_version"`
	GeneratedBy     string    `json:"generated_by"`
	ExtensionPrefix string    `json:"extension_prefix"`
	CommandCount    int       `json:"command_count"`
	Commands        []command `json:"commands"`
}

type generatorConfig struct {
	SourceLabel       string
	GoModule          string
	TSPackage         string
	EmitEventRefRules bool
}

var pathParamPattern = regexp.MustCompile(`\{([^{}]+)\}`)

func main() {
	var (
		openAPIPath       = flag.String("openapi", "../contracts/oar-openapi.yaml", "path to root OpenAPI contract")
		schemaPath        = flag.String("schema", "../contracts/oar-schema.yaml", "path to root domain schema contract")
		outDir            = flag.String("out", "../contracts/gen", "output directory for generated artifacts")
		sourceLabel       = flag.String("source-label", "contracts/oar-openapi.yaml", "display label for generated docs")
		goModule          = flag.String("go-module", "organization-autorunner-contracts-go-client", "Go module path for generated Go client")
		tsPackage         = flag.String("ts-package", "organization-autorunner-contracts-ts-client", "package name for generated TS client")
		emitEventRefRules = flag.Bool("emit-event-ref-rules", true, "whether to emit event reference rules metadata from the schema contract")
	)
	flag.Parse()

	openAPIRaw, err := os.ReadFile(*openAPIPath)
	if err != nil {
		exitf("read openapi: %v", err)
	}

	var doc openAPIDocument
	if err := yaml.Unmarshal(openAPIRaw, &doc); err != nil {
		exitf("decode openapi yaml: %v", err)
	}

	if strings.TrimSpace(doc.OpenAPI) == "" {
		exitf("openapi version is missing")
	}

	schemaRaw, err := os.ReadFile(*schemaPath)
	if err != nil {
		exitf("read schema contract: %v", err)
	}
	var schemaDoc oarSchemaDocument
	if err := yaml.Unmarshal(schemaRaw, &schemaDoc); err != nil {
		exitf("decode schema yaml: %v", err)
	}

	commands := collectCommands(doc, schemaDoc)
	if len(commands) == 0 {
		exitf("no x-oar commands found in openapi document")
	}

	config := generatorConfig{
		SourceLabel:       strings.TrimSpace(*sourceLabel),
		GoModule:          strings.TrimSpace(*goModule),
		TSPackage:         strings.TrimSpace(*tsPackage),
		EmitEventRefRules: *emitEventRefRules,
	}
	if config.SourceLabel == "" {
		config.SourceLabel = filepath.Clean(*openAPIPath)
	}
	if config.GoModule == "" {
		config.GoModule = "organization-autorunner-contracts-go-client"
	}
	if config.TSPackage == "" {
		config.TSPackage = "organization-autorunner-contracts-ts-client"
	}

	if err := generateAll(*outDir, doc, commands, schemaDoc, config); err != nil {
		exitf("generate artifacts: %v", err)
	}
}

func collectCommands(doc openAPIDocument, schemaDoc oarSchemaDocument) []command {
	paths := make([]string, 0, len(doc.Paths))
	for path := range doc.Paths {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	commands := make([]command, 0, len(paths)*2)
	for _, path := range paths {
		item := doc.Paths[path]
		for _, pair := range []struct {
			method string
			op     *operation
		}{
			{method: "GET", op: item.Get},
			{method: "POST", op: item.Post},
			{method: "PUT", op: item.Put},
			{method: "PATCH", op: item.Patch},
			{method: "DELETE", op: item.Delete},
		} {
			if pair.op == nil {
				continue
			}
			if strings.TrimSpace(pair.op.CommandID) == "" {
				continue
			}

			commandID := strings.TrimSpace(pair.op.CommandID)
			cmd := command{
				CommandID:   commandID,
				CLIPath:     strings.TrimSpace(pair.op.CLIPath),
				Group:       commandGroup(strings.TrimSpace(pair.op.CLIPath)),
				Method:      pair.method,
				Path:        path,
				OperationID: strings.TrimSpace(pair.op.OperationID),
				Summary:     strings.TrimSpace(pair.op.Summary),
				Description: strings.TrimSpace(pair.op.Description),
				Why:         strings.TrimSpace(pair.op.Why),
				InputMode:   strings.TrimSpace(pair.op.InputMode),
				Streaming:   pair.op.Streaming,
				Output:      strings.TrimSpace(pair.op.Output),
				ErrorCodes:  compactStrings(pair.op.ErrorCodes),
				Concepts:    compactStrings(pair.op.Concepts),
				Stability:   strings.TrimSpace(pair.op.Stability),
				Surface:     strings.TrimSpace(pair.op.Surface),
				AgentNotes:  strings.TrimSpace(pair.op.AgentNotes),
				Examples:    compactExamples(pair.op.Examples),
				BodySchema:  deriveBodySchema(doc, schemaDoc, commandID, pair.op),
				PathParams:  extractPathParams(path),
				GoMethod:    toPascalCase(commandID),
				TSMethod:    toCamelCase(commandID),
			}
			commands = append(commands, cmd)
		}
	}

	sort.Slice(commands, func(i, j int) bool {
		if commands[i].CommandID != commands[j].CommandID {
			return commands[i].CommandID < commands[j].CommandID
		}
		if commands[i].Method != commands[j].Method {
			return commands[i].Method < commands[j].Method
		}
		return commands[i].Path < commands[j].Path
	})

	seen := make(map[string]struct{}, len(commands))
	for _, cmd := range commands {
		if _, ok := seen[cmd.CommandID]; ok {
			exitf("duplicate x-oar-command-id detected: %s", cmd.CommandID)
		}
		seen[cmd.CommandID] = struct{}{}
	}

	commandsByGroup := map[string][]int{}
	for i := range commands {
		group := strings.TrimSpace(commands[i].Group)
		if group == "" {
			continue
		}
		commandsByGroup[group] = append(commandsByGroup[group], i)
	}
	for _, indexes := range commandsByGroup {
		sort.Slice(indexes, func(i, j int) bool {
			left := commands[indexes[i]]
			right := commands[indexes[j]]
			if left.CLIPath != right.CLIPath {
				return left.CLIPath < right.CLIPath
			}
			return left.CommandID < right.CommandID
		})
		groupCommandIDs := make([]string, 0, len(indexes))
		for _, index := range indexes {
			groupCommandIDs = append(groupCommandIDs, commands[index].CommandID)
		}
		for _, index := range indexes {
			adjacent := make([]string, 0, len(groupCommandIDs)-1)
			for _, commandID := range groupCommandIDs {
				if commandID == commands[index].CommandID {
					continue
				}
				adjacent = append(adjacent, commandID)
			}
			commands[index].Adjacent = adjacent
		}
	}

	return commands
}

type bodyFieldState struct {
	field    bodyField
	required bool
}

type oarExpansionOptions struct {
	exclude          map[string]struct{}
	forceOptional    map[string]struct{}
	forceOptionalAll bool
}

func deriveBodySchema(doc openAPIDocument, schemaDoc oarSchemaDocument, commandID string, op *operation) *bodySchema {
	if op == nil || op.RequestBody == nil {
		return nil
	}
	requestSchema := requestBodyJSONSchema(op.RequestBody)
	if requestSchema == nil {
		return nil
	}
	acc := map[string]bodyFieldState{}
	collectOpenAPISchemaFields(doc, *requestSchema, "", true, acc, map[string]struct{}{})
	applyOARSchemaOverlays(acc, schemaDoc, strings.TrimSpace(commandID))
	schema := bodySchemaFromAccumulator(acc)
	if len(schema.Required) == 0 && len(schema.Optional) == 0 {
		return nil
	}
	return &schema
}

func requestBodyJSONSchema(body *requestBody) *openAPISchema {
	if body == nil {
		return nil
	}
	if media, ok := body.Content["application/json"]; ok && media.Schema != nil {
		return media.Schema
	}
	contentTypes := make([]string, 0, len(body.Content))
	for contentType := range body.Content {
		contentTypes = append(contentTypes, contentType)
	}
	sort.Strings(contentTypes)
	for _, contentType := range contentTypes {
		if !strings.Contains(strings.ToLower(contentType), "json") {
			continue
		}
		media := body.Content[contentType]
		if media.Schema != nil {
			return media.Schema
		}
	}
	return nil
}

func collectOpenAPISchemaFields(
	doc openAPIDocument,
	schema openAPISchema,
	prefix string,
	ancestorRequired bool,
	acc map[string]bodyFieldState,
	seenRefs map[string]struct{},
) {
	schema = resolveOpenAPISchema(doc, schema, seenRefs)
	if len(schema.AllOf) > 0 {
		for _, candidate := range schema.AllOf {
			collectOpenAPISchemaFields(doc, candidate, prefix, ancestorRequired, acc, seenRefs)
		}
		return
	}

	if len(schema.Properties) > 0 || strings.TrimSpace(schema.Type) == "object" {
		if len(schema.Properties) == 0 {
			if strings.TrimSpace(prefix) != "" {
				upsertBodyField(acc, bodyField{
					Name: prefix,
					Type: openAPISchemaTypeLabel(schema),
				}, ancestorRequired)
			}
			return
		}
		requiredSet := make(map[string]struct{}, len(schema.Required))
		for _, requiredField := range schema.Required {
			requiredSet[strings.TrimSpace(requiredField)] = struct{}{}
		}
		names := make([]string, 0, len(schema.Properties))
		for name := range schema.Properties {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			child := schema.Properties[name]
			childName := strings.TrimSpace(name)
			if childName == "" {
				continue
			}
			childPrefix := childName
			if strings.TrimSpace(prefix) != "" {
				childPrefix = prefix + "." + childName
			}
			_, childRequiredBySchema := requiredSet[childName]
			childRequired := ancestorRequired && childRequiredBySchema
			resolvedChild := resolveOpenAPISchema(doc, child, seenRefs)
			if len(resolvedChild.Properties) > 0 {
				collectOpenAPISchemaFields(doc, child, childPrefix, childRequired, acc, seenRefs)
				continue
			}
			upsertBodyField(acc, bodyField{
				Name:       childPrefix,
				Type:       openAPISchemaTypeLabel(resolvedChild),
				EnumValues: openAPISchemaEnumValues(resolvedChild),
			}, childRequired)
		}
		return
	}

	if strings.TrimSpace(prefix) == "" {
		return
	}
	upsertBodyField(acc, bodyField{
		Name:       prefix,
		Type:       openAPISchemaTypeLabel(schema),
		EnumValues: openAPISchemaEnumValues(schema),
	}, ancestorRequired)
}

func resolveOpenAPISchema(doc openAPIDocument, schema openAPISchema, seenRefs map[string]struct{}) openAPISchema {
	ref := strings.TrimSpace(schema.Ref)
	if ref == "" {
		return schema
	}
	if _, seen := seenRefs[ref]; seen {
		return schema
	}
	name := strings.TrimPrefix(ref, "#/components/schemas/")
	if name == ref {
		return schema
	}
	resolved, ok := doc.Components.Schemas[name]
	if !ok {
		return schema
	}
	seenRefs[ref] = struct{}{}
	defer delete(seenRefs, ref)
	return resolveOpenAPISchema(doc, resolved, seenRefs)
}

func openAPISchemaTypeLabel(schema openAPISchema) string {
	if len(schema.OneOf) > 0 {
		parts := make([]string, 0, len(schema.OneOf))
		for _, candidate := range schema.OneOf {
			parts = append(parts, openAPISchemaTypeLabel(candidate))
		}
		return strings.Join(sortedUniqueStrings(parts), "|")
	}
	if len(schema.AnyOf) > 0 {
		parts := make([]string, 0, len(schema.AnyOf))
		for _, candidate := range schema.AnyOf {
			parts = append(parts, openAPISchemaTypeLabel(candidate))
		}
		return strings.Join(sortedUniqueStrings(parts), "|")
	}
	switch strings.TrimSpace(schema.Type) {
	case "array":
		itemType := "any"
		if schema.Items != nil {
			itemType = openAPISchemaTypeLabel(*schema.Items)
		}
		return "list<" + strings.TrimSpace(itemType) + ">"
	case "string":
		if strings.EqualFold(strings.TrimSpace(schema.Format), "date-time") {
			return "datetime"
		}
		return "string"
	case "number", "integer", "boolean", "object":
		return strings.TrimSpace(schema.Type)
	}
	if len(schema.Properties) > 0 || schema.AdditionalProperties != nil {
		return "object"
	}
	if len(schema.Enum) > 0 {
		return "string"
	}
	return "any"
}

func openAPISchemaEnumValues(schema openAPISchema) []string {
	if len(schema.Enum) == 0 {
		return nil
	}
	out := make([]string, 0, len(schema.Enum))
	for _, value := range schema.Enum {
		text := strings.TrimSpace(fmt.Sprintf("%v", value))
		if text == "" {
			continue
		}
		out = append(out, text)
	}
	return sortedUniqueStrings(out)
}

func upsertBodyField(acc map[string]bodyFieldState, field bodyField, required bool) {
	field.Name = strings.TrimSpace(field.Name)
	field.Type = strings.TrimSpace(field.Type)
	if field.Name == "" {
		return
	}
	if field.Type == "" {
		field.Type = "any"
	}
	field.EnumValues = sortedUniqueStrings(field.EnumValues)
	current, exists := acc[field.Name]
	if !exists {
		acc[field.Name] = bodyFieldState{field: field, required: required}
		return
	}
	if current.field.Type == "any" && field.Type != "any" {
		current.field.Type = field.Type
	}
	if len(current.field.EnumValues) == 0 && len(field.EnumValues) > 0 {
		current.field.EnumValues = append([]string(nil), field.EnumValues...)
	}
	if strings.TrimSpace(current.field.EnumPolicy) == "" && strings.TrimSpace(field.EnumPolicy) != "" {
		current.field.EnumPolicy = strings.TrimSpace(field.EnumPolicy)
	}
	current.required = current.required || required
	acc[field.Name] = current
}

func applyOARSchemaOverlays(acc map[string]bodyFieldState, schemaDoc oarSchemaDocument, commandID string) {
	switch commandID {
	case "events.create":
		if source, ok := schemaDoc.Primitives["event"]; ok {
			expandContainerFromOAR(acc, schemaDoc, "event", source, oarExpansionOptions{
				exclude:       map[string]struct{}{"id": {}, "ts": {}},
				forceOptional: map[string]struct{}{"actor_id": {}},
			})
		}
	case "threads.create":
		if source, ok := schemaDoc.Threads["thread"]; ok {
			expandContainerFromOAR(acc, schemaDoc, "thread", source, oarExpansionOptions{
				exclude: map[string]struct{}{"open_cards": {}},
			})
		}
	case "threads.patch":
		if source, ok := schemaDoc.Threads["thread"]; ok {
			expandContainerFromOAR(acc, schemaDoc, "patch", source, oarExpansionOptions{
				exclude:          map[string]struct{}{"open_cards": {}},
				forceOptionalAll: true,
			})
		}
	case "packets.receipts.create":
		if source, ok := schemaDoc.Packets["receipt"]; ok {
			expandContainerFromOAR(acc, schemaDoc, "packet", source, oarExpansionOptions{})
		}
	case "packets.reviews.create":
		if source, ok := schemaDoc.Packets["review"]; ok {
			expandContainerFromOAR(acc, schemaDoc, "packet", source, oarExpansionOptions{})
		}
	}
}

func expandContainerFromOAR(
	acc map[string]bodyFieldState,
	schemaDoc oarSchemaDocument,
	container string,
	source oarMaybeFieldContainer,
	options oarExpansionOptions,
) {
	container = strings.TrimSpace(container)
	if container == "" || len(source.Fields) == 0 {
		return
	}
	containerRequired := false
	if current, ok := acc[container]; ok {
		containerRequired = current.required
		delete(acc, container)
	}
	keys := make([]string, 0, len(source.Fields))
	for key := range source.Fields {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if _, skip := options.exclude[key]; skip {
			continue
		}
		field := source.Fields[key]
		required := containerRequired && field.Required
		if options.forceOptionalAll {
			required = false
		}
		if _, forceOptional := options.forceOptional[key]; forceOptional {
			required = false
		}
		path := container + "." + key
		if strings.TrimSpace(field.Ref) == "provenance" && len(schemaDoc.Provenance.Fields) > 0 {
			expandProvenanceField(acc, schemaDoc, path, required, options.forceOptionalAll)
			continue
		}
		values, policy := enumFromOARRef(schemaDoc, field.Ref)
		upsertBodyField(acc, bodyField{
			Name:       path,
			Type:       normalizeOARFieldType(field.Type),
			EnumValues: values,
			EnumPolicy: policy,
		}, required)
	}
}

func expandProvenanceField(
	acc map[string]bodyFieldState,
	schemaDoc oarSchemaDocument,
	prefix string,
	parentRequired bool,
	forceOptionalAll bool,
) {
	keys := make([]string, 0, len(schemaDoc.Provenance.Fields))
	for key := range schemaDoc.Provenance.Fields {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		field := schemaDoc.Provenance.Fields[key]
		required := parentRequired && field.Required
		if forceOptionalAll {
			required = false
		}
		values, policy := enumFromOARRef(schemaDoc, field.Ref)
		upsertBodyField(acc, bodyField{
			Name:       prefix + "." + key,
			Type:       normalizeOARFieldType(field.Type),
			EnumValues: values,
			EnumPolicy: policy,
		}, required)
	}
}

func enumFromOARRef(schemaDoc oarSchemaDocument, ref string) ([]string, string) {
	ref = strings.TrimSpace(ref)
	if !strings.HasPrefix(ref, "enums.") {
		return nil, ""
	}
	enumName := strings.TrimSpace(strings.TrimPrefix(ref, "enums."))
	if enumName == "" {
		return nil, ""
	}
	enumDef, ok := schemaDoc.Enums[enumName]
	if !ok {
		return nil, ""
	}
	return sortedUniqueStrings(enumDef.Values), strings.TrimSpace(enumDef.EnumPolicy)
}

func normalizeOARFieldType(raw string) string {
	raw = strings.TrimSpace(raw)
	raw = strings.Trim(raw, "\"")
	if raw == "" {
		return "object"
	}
	return raw
}

func normalizeOAREventRefRule(rule oarEventRefRule) oarEventRefRule {
	out := oarEventRefRule{
		ThreadID:           strings.TrimSpace(rule.ThreadID),
		RefsMustInclude:    compactStrings(rule.RefsMustInclude),
		RefsConditional:    strings.TrimSpace(rule.RefsConditional),
		PayloadMustInclude: compactStrings(rule.PayloadMustInclude),
		ConditionalRefs:    make([]oarConditionalRefRule, 0, len(rule.ConditionalRefs)),
	}

	for _, conditional := range rule.ConditionalRefs {
		out.ConditionalRefs = append(out.ConditionalRefs, oarConditionalRefRule{
			When: oarWhenCondition{
				PayloadField: strings.TrimSpace(conditional.When.PayloadField),
				Equals:       strings.TrimSpace(conditional.When.Equals),
			},
			MustHave:  normalizeOARRefPrefixReqs(conditional.MustHave),
			Condition: strings.TrimSpace(conditional.Condition),
		})
	}

	if len(out.ConditionalRefs) == 0 {
		out.ConditionalRefs = nil
	}

	return out
}

func normalizeOARRefPrefixReqs(values []oarRefPrefixReq) []oarRefPrefixReq {
	if len(values) == 0 {
		return nil
	}
	out := make([]oarRefPrefixReq, 0, len(values))
	for _, value := range values {
		prefix := strings.TrimSpace(value.Prefix)
		if prefix == "" {
			continue
		}
		out = append(out, oarRefPrefixReq{Prefix: prefix})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func bodySchemaFromAccumulator(acc map[string]bodyFieldState) bodySchema {
	required := make([]bodyField, 0)
	optional := make([]bodyField, 0)
	for _, state := range acc {
		if state.required {
			required = append(required, state.field)
			continue
		}
		optional = append(optional, state.field)
	}
	sort.Slice(required, func(i, j int) bool { return required[i].Name < required[j].Name })
	sort.Slice(optional, func(i, j int) bool { return optional[i].Name < optional[j].Name })
	return bodySchema{Required: required, Optional: optional}
}

func sortedUniqueStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
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
	if len(out) == 0 {
		return nil
	}
	return out
}

func generateAll(outDir string, doc openAPIDocument, commands []command, schemaDoc oarSchemaDocument, config generatorConfig) error {
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}

	if err := writeMeta(filepath.Join(outDir, "meta", "commands.json"), doc, commands); err != nil {
		return err
	}
	if err := writeConceptsMeta(filepath.Join(outDir, "meta", "concepts.json"), doc, commands); err != nil {
		return err
	}
	if err := writeHelpMeta(filepath.Join(outDir, "meta", "help.json"), doc, commands); err != nil {
		return err
	}
	if config.EmitEventRefRules {
		if err := writeEventRefRulesMeta(filepath.Join(outDir, "meta", "event_ref_rules.json"), doc, schemaDoc); err != nil {
			return err
		}
	}
	if err := writeMarkdown(filepath.Join(outDir, "docs", "commands.md"), doc, commands, config.SourceLabel); err != nil {
		return err
	}
	if err := writeGroupsMarkdown(filepath.Join(outDir, "docs", "command-groups.md"), doc, commands, config.SourceLabel); err != nil {
		return err
	}
	if err := writeConceptsMarkdown(filepath.Join(outDir, "docs", "concepts.md"), doc, commands, config.SourceLabel); err != nil {
		return err
	}
	if err := writeXOARAuthoringMarkdown(filepath.Join(outDir, "docs", "x-oar-authoring.md")); err != nil {
		return err
	}
	if err := writeGoClient(filepath.Join(outDir, "go"), commands, config.GoModule); err != nil {
		return err
	}
	if err := writeTSClient(filepath.Join(outDir, "ts"), commands, config.TSPackage); err != nil {
		return err
	}

	return nil
}

func writeMeta(path string, doc openAPIDocument, commands []command) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	payload := metaOutput{
		OpenAPIVersion:  doc.OpenAPI,
		ContractVersion: strings.TrimSpace(doc.Info.Version),
		GeneratedBy:     "core/cmd/contract-gen",
		ExtensionPrefix: "x-oar-",
		CommandCount:    len(commands),
		Commands:        commands,
	}

	b, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	return os.WriteFile(path, b, 0o644)
}

func writeConceptsMeta(path string, doc openAPIDocument, commands []command) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	concepts := deriveConcepts(commands)
	payload := conceptsOutput{
		OpenAPIVersion:  doc.OpenAPI,
		ContractVersion: strings.TrimSpace(doc.Info.Version),
		GeneratedBy:     "core/cmd/contract-gen",
		ConceptCount:    len(concepts),
		Concepts:        concepts,
	}
	b, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	return os.WriteFile(path, b, 0o644)
}

func writeHelpMeta(path string, doc openAPIDocument, commands []command) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	groups := deriveGroups(commands)
	payload := helpOutput{
		OpenAPIVersion:  doc.OpenAPI,
		ContractVersion: strings.TrimSpace(doc.Info.Version),
		GeneratedBy:     "core/cmd/contract-gen",
		GroupCount:      len(groups),
		Groups:          groups,
		CommandCount:    len(commands),
		Commands:        commands,
	}
	b, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	return os.WriteFile(path, b, 0o644)
}

func writeEventRefRulesMeta(path string, doc openAPIDocument, schemaDoc oarSchemaDocument) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	rules := make(map[string]oarEventRefRule, len(schemaDoc.ReferenceConventions.EventRefs.Rules))
	for eventType, rule := range schemaDoc.ReferenceConventions.EventRefs.Rules {
		eventType = strings.TrimSpace(eventType)
		if eventType == "" {
			continue
		}
		rules[eventType] = normalizeOAREventRefRule(rule)
	}

	eventTypeOpenEnum := false
	if et, ok := schemaDoc.Enums["event_type"]; ok {
		eventTypeOpenEnum = strings.TrimSpace(et.EnumPolicy) == "open"
	}

	payload := struct {
		OpenAPIVersion    string                     `json:"openapi_version"`
		ContractVersion   string                     `json:"contract_version"`
		GeneratedBy       string                     `json:"generated_by"`
		EventTypeOpenEnum bool                       `json:"event_type_open_enum"`
		RuleCount         int                        `json:"rule_count"`
		Rules             map[string]oarEventRefRule `json:"rules"`
	}{
		OpenAPIVersion:    doc.OpenAPI,
		ContractVersion:   strings.TrimSpace(doc.Info.Version),
		GeneratedBy:       "core/cmd/contract-gen",
		EventTypeOpenEnum: eventTypeOpenEnum,
		RuleCount:         len(rules),
		Rules:             rules,
	}

	b, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	return os.WriteFile(path, b, 0o644)
}

func writeMarkdown(path string, doc openAPIDocument, commands []command, sourceLabel string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	var b strings.Builder
	b.WriteString("# OAR Command Registry\n\n")
	b.WriteString(fmt.Sprintf("Generated from `%s`.\n\n", sourceLabel))
	b.WriteString(fmt.Sprintf("- OpenAPI version: `%s`\n", doc.OpenAPI))
	b.WriteString(fmt.Sprintf("- Contract version: `%s`\n", strings.TrimSpace(doc.Info.Version)))
	b.WriteString(fmt.Sprintf("- Commands: `%d`\n\n", len(commands)))

	for _, cmd := range commands {
		b.WriteString(fmt.Sprintf("## `%s`\n\n", cmd.CommandID))
		b.WriteString(fmt.Sprintf("- CLI path: `%s`\n", cmd.CLIPath))
		b.WriteString(fmt.Sprintf("- HTTP: `%s %s`\n", cmd.Method, cmd.Path))
		if cmd.Stability != "" {
			b.WriteString(fmt.Sprintf("- Stability: `%s`\n", cmd.Stability))
		}
		if cmd.Surface != "" {
			b.WriteString(fmt.Sprintf("- Surface: `%s`\n", cmd.Surface))
		}
		if cmd.InputMode != "" {
			b.WriteString(fmt.Sprintf("- Input mode: `%s`\n", cmd.InputMode))
		}
		if cmd.Why != "" {
			b.WriteString(fmt.Sprintf("- Why: %s\n", cmd.Why))
		}
		if len(cmd.Concepts) > 0 {
			b.WriteString(fmt.Sprintf("- Concepts: `%s`\n", strings.Join(cmd.Concepts, "`, `")))
		}
		if len(cmd.ErrorCodes) > 0 {
			b.WriteString(fmt.Sprintf("- Error codes: `%s`\n", strings.Join(cmd.ErrorCodes, "`, `")))
		}
		if cmd.Output != "" {
			b.WriteString(fmt.Sprintf("- Output: %s\n", cmd.Output))
		}
		if cmd.AgentNotes != "" {
			b.WriteString(fmt.Sprintf("- Agent notes: %s\n", cmd.AgentNotes))
		}
		if len(cmd.Examples) > 0 {
			b.WriteString("- Examples:\n")
			for _, ex := range cmd.Examples {
				title := strings.TrimSpace(ex.Title)
				if title == "" {
					title = "Example"
				}
				b.WriteString(fmt.Sprintf("  - %s: `%s`\n", title, strings.TrimSpace(ex.Command)))
				if strings.TrimSpace(ex.Description) != "" {
					b.WriteString(fmt.Sprintf("    - %s\n", strings.TrimSpace(ex.Description)))
				}
			}
		}
		b.WriteString("\n")
	}

	return os.WriteFile(path, []byte(b.String()), 0o644)
}

func writeGroupsMarkdown(path string, doc openAPIDocument, commands []command, sourceLabel string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	groups := deriveGroups(commands)
	commandByID := make(map[string]command, len(commands))
	for _, cmd := range commands {
		commandByID[cmd.CommandID] = cmd
	}

	var b strings.Builder
	b.WriteString("# OAR Command Groups\n\n")
	b.WriteString(fmt.Sprintf("Generated from `%s`.\n\n", sourceLabel))
	b.WriteString(fmt.Sprintf("- OpenAPI version: `%s`\n", doc.OpenAPI))
	b.WriteString(fmt.Sprintf("- Contract version: `%s`\n", strings.TrimSpace(doc.Info.Version)))
	b.WriteString(fmt.Sprintf("- Groups: `%d`\n\n", len(groups)))

	for _, group := range groups {
		b.WriteString(fmt.Sprintf("## `%s`\n\n", group.Name))
		b.WriteString(fmt.Sprintf("- Commands: `%d`\n", group.CommandCount))
		b.WriteString("- Command IDs:\n")
		for _, commandID := range group.CommandIDs {
			cmd := commandByID[commandID]
			b.WriteString(fmt.Sprintf("  - `%s` (`%s`)\n", commandID, cmd.CLIPath))
		}
		b.WriteString("\n")
	}

	return os.WriteFile(path, []byte(b.String()), 0o644)
}

func writeConceptsMarkdown(path string, doc openAPIDocument, commands []command, sourceLabel string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	concepts := deriveConcepts(commands)

	var b strings.Builder
	b.WriteString("# OAR Concepts\n\n")
	b.WriteString(fmt.Sprintf("Generated from `%s`.\n\n", sourceLabel))
	b.WriteString(fmt.Sprintf("- OpenAPI version: `%s`\n", doc.OpenAPI))
	b.WriteString(fmt.Sprintf("- Contract version: `%s`\n", strings.TrimSpace(doc.Info.Version)))
	b.WriteString(fmt.Sprintf("- Concepts: `%d`\n\n", len(concepts)))

	for _, concept := range concepts {
		b.WriteString(fmt.Sprintf("## `%s`\n\n", concept.Name))
		b.WriteString(fmt.Sprintf("- Commands: `%d`\n", concept.CommandCount))
		b.WriteString("- Command IDs:\n")
		for _, commandID := range concept.CommandIDs {
			b.WriteString(fmt.Sprintf("  - `%s`\n", commandID))
		}
		b.WriteString("\n")
	}

	return os.WriteFile(path, []byte(b.String()), 0o644)
}

func writeXOARAuthoringMarkdown(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	content := strings.TrimSpace(`
# x-oar Authoring Rules

The OpenAPI contract uses `+"`x-oar-*`"+` extensions as the single source for CLI/help/meta/doc generation.

Required for every command operation:

- `+"`x-oar-command-id`"+`: stable id (for example `+"`threads.list`"+`)
- `+"`x-oar-cli-path`"+`: CLI path (for example `+"`threads list`"+`)
- `+"`x-oar-why`"+`: non-empty purpose/decision boundary
- `+"`x-oar-input-mode`"+`: one of `+"`none|json-body|raw-stream|file-and-body`"+`
- `+"`x-oar-streaming`"+`: streaming metadata object
- `+"`x-oar-output-envelope`"+`: output notes for CLI consumers
- `+"`x-oar-error-codes`"+`: stable semantic error code list
- `+"`x-oar-concepts`"+`: related concept tags
- `+"`x-oar-stability`"+`: one of `+"`experimental|beta|stable`"+`
- `+"`x-oar-surface`"+`: one of `+"`canonical|projection|diagnostic|utility`"+`
- `+"`x-oar-agent-notes`"+`: idempotency/retry caveats

Recommended:

- include at least one `+"`x-oar-examples`"+` command per operation
- keep `+"`x-oar-command-id`"+` immutable once published
- keep concept labels lower-case and dash-separated

Surface classification:

- `+"`canonical`"+`: CRUD/list/get endpoints over canonical durable resources (topics, cards, artifacts, documents, boards, events)
- `+"`projection`"+`: operator convenience surfaces that aggregate multiple canonical resources (workspace/context endpoints, inbox)
- `+"`diagnostic`"+`: read-only tooling and inspection surfaces over backing infrastructure (for example backing-thread list/get paths)
- `+"`utility`"+`: meta/handshake, auth bootstrap, rebuild/repair, and similar non-domain endpoints
`) + "\n"
	return os.WriteFile(path, []byte(content), 0o644)
}

func writeGoClient(goOutDir string, commands []command, modulePath string) error {
	clientDir := filepath.Join(goOutDir, "client")
	if err := os.MkdirAll(clientDir, 0o755); err != nil {
		return err
	}

	goMod := fmt.Sprintf("module %s\n\ngo 1.23.0\n", modulePath)
	if err := os.WriteFile(filepath.Join(goOutDir, "go.mod"), []byte(goMod), 0o644); err != nil {
		return err
	}

	if err := os.WriteFile(filepath.Join(clientDir, "client_gen_test.go"), []byte("package client\n\nimport \"testing\"\n\nfunc TestGeneratedRegistryHasCommands(t *testing.T) {\n\tif len(CommandRegistry) == 0 {\n\t\tt.Fatal(\"expected non-empty command registry\")\n\t}\n}\n"), 0o644); err != nil {
		return err
	}

	var src bytes.Buffer
	src.WriteString("package client\n\n")
	src.WriteString("import (\n")
	src.WriteString("\t\"bytes\"\n")
	src.WriteString("\t\"context\"\n")
	src.WriteString("\t\"encoding/json\"\n")
	src.WriteString("\t\"fmt\"\n")
	src.WriteString("\t\"io\"\n")
	src.WriteString("\t\"net/http\"\n")
	src.WriteString("\t\"net/url\"\n")
	src.WriteString("\t\"strings\"\n")
	src.WriteString(")\n\n")

	src.WriteString("type Example struct {\n")
	src.WriteString("\tTitle       string `json:\"title\"`\n")
	src.WriteString("\tCommand     string `json:\"command\"`\n")
	src.WriteString("\tDescription string `json:\"description,omitempty\"`\n")
	src.WriteString("}\n\n")

	src.WriteString("type CommandSpec struct {\n")
	src.WriteString("\tCommandID  string   `json:\"command_id\"`\n")
	src.WriteString("\tCLIPath    string   `json:\"cli_path\"`\n")
	src.WriteString("\tGroup      string   `json:\"group,omitempty\"`\n")
	src.WriteString("\tMethod     string   `json:\"method\"`\n")
	src.WriteString("\tPath       string   `json:\"path\"`\n")
	src.WriteString("\tPathParams []string `json:\"path_params,omitempty\"`\n")
	src.WriteString("\tInputMode  string   `json:\"input_mode,omitempty\"`\n")
	src.WriteString("\tStability  string   `json:\"stability,omitempty\"`\n")
	src.WriteString("\tConcepts   []string `json:\"concepts,omitempty\"`\n")
	src.WriteString("\tAdjacent   []string `json:\"adjacent_commands,omitempty\"`\n")
	src.WriteString("\tExamples   []Example `json:\"examples,omitempty\"`\n")
	src.WriteString("}\n\n")

	src.WriteString("var CommandRegistry = []CommandSpec{\n")
	for _, cmd := range commands {
		src.WriteString("\t{\n")
		src.WriteString(fmt.Sprintf("\t\tCommandID: %q,\n", cmd.CommandID))
		src.WriteString(fmt.Sprintf("\t\tCLIPath: %q,\n", cmd.CLIPath))
		if cmd.Group != "" {
			src.WriteString(fmt.Sprintf("\t\tGroup: %q,\n", cmd.Group))
		}
		src.WriteString(fmt.Sprintf("\t\tMethod: %q,\n", cmd.Method))
		src.WriteString(fmt.Sprintf("\t\tPath: %q,\n", cmd.Path))
		if len(cmd.PathParams) > 0 {
			src.WriteString("\t\tPathParams: []string{")
			for i, p := range cmd.PathParams {
				if i > 0 {
					src.WriteString(", ")
				}
				src.WriteString(fmt.Sprintf("%q", p))
			}
			src.WriteString("},\n")
		}
		if cmd.InputMode != "" {
			src.WriteString(fmt.Sprintf("\t\tInputMode: %q,\n", cmd.InputMode))
		}
		if cmd.Stability != "" {
			src.WriteString(fmt.Sprintf("\t\tStability: %q,\n", cmd.Stability))
		}
		if len(cmd.Concepts) > 0 {
			src.WriteString("\t\tConcepts: []string{")
			for i, c := range cmd.Concepts {
				if i > 0 {
					src.WriteString(", ")
				}
				src.WriteString(fmt.Sprintf("%q", c))
			}
			src.WriteString("},\n")
		}
		if len(cmd.Adjacent) > 0 {
			src.WriteString("\t\tAdjacent: []string{")
			for i, adjacent := range cmd.Adjacent {
				if i > 0 {
					src.WriteString(", ")
				}
				src.WriteString(fmt.Sprintf("%q", adjacent))
			}
			src.WriteString("},\n")
		}
		if len(cmd.Examples) > 0 {
			src.WriteString("\t\tExamples: []Example{\n")
			for _, ex := range cmd.Examples {
				src.WriteString("\t\t\t{\n")
				src.WriteString(fmt.Sprintf("\t\t\t\tTitle: %q,\n", ex.Title))
				src.WriteString(fmt.Sprintf("\t\t\t\tCommand: %q,\n", ex.Command))
				if ex.Description != "" {
					src.WriteString(fmt.Sprintf("\t\t\t\tDescription: %q,\n", ex.Description))
				}
				src.WriteString("\t\t\t},\n")
			}
			src.WriteString("\t\t},\n")
		}
		src.WriteString("\t},\n")
	}
	src.WriteString("}\n\n")

	src.WriteString("var commandIndex = func() map[string]CommandSpec {\n")
	src.WriteString("\tindex := make(map[string]CommandSpec, len(CommandRegistry))\n")
	src.WriteString("\tfor _, cmd := range CommandRegistry {\n")
	src.WriteString("\t\tindex[cmd.CommandID] = cmd\n")
	src.WriteString("\t}\n")
	src.WriteString("\treturn index\n")
	src.WriteString("}()\n\n")

	src.WriteString("type RequestOptions struct {\n")
	src.WriteString("\tQuery   map[string][]string\n")
	src.WriteString("\tHeaders map[string]string\n")
	src.WriteString("\tBody    any\n")
	src.WriteString("}\n\n")

	src.WriteString("type Client struct {\n")
	src.WriteString("\tBaseURL    string\n")
	src.WriteString("\tHTTPClient *http.Client\n")
	src.WriteString("}\n\n")

	src.WriteString("func New(baseURL string, httpClient *http.Client) *Client {\n")
	src.WriteString("\tif httpClient == nil {\n")
	src.WriteString("\t\thttpClient = &http.Client{}\n")
	src.WriteString("\t}\n")
	src.WriteString("\treturn &Client{BaseURL: strings.TrimRight(baseURL, \"/\"), HTTPClient: httpClient}\n")
	src.WriteString("}\n\n")

	src.WriteString("func (c *Client) Invoke(ctx context.Context, commandID string, pathParams map[string]string, opts RequestOptions) (*http.Response, []byte, error) {\n")
	src.WriteString("\tif c == nil {\n")
	src.WriteString("\t\treturn nil, nil, fmt.Errorf(\"client is nil\")\n")
	src.WriteString("\t}\n")
	src.WriteString("\tif strings.TrimSpace(c.BaseURL) == \"\" {\n")
	src.WriteString("\t\treturn nil, nil, fmt.Errorf(\"base url is required\")\n")
	src.WriteString("\t}\n")
	src.WriteString("\tif c.HTTPClient == nil {\n")
	src.WriteString("\t\treturn nil, nil, fmt.Errorf(\"http client is required\")\n")
	src.WriteString("\t}\n")
	src.WriteString("\tcmd, ok := commandIndex[commandID]\n")
	src.WriteString("\tif !ok {\n")
	src.WriteString("\t\treturn nil, nil, fmt.Errorf(\"unknown command id: %s\", commandID)\n")
	src.WriteString("\t}\n")
	src.WriteString("\tpath, err := renderPath(cmd.Path, pathParams)\n")
	src.WriteString("\tif err != nil {\n")
	src.WriteString("\t\treturn nil, nil, err\n")
	src.WriteString("\t}\n")
	src.WriteString("\turlString := c.BaseURL + path\n")
	src.WriteString("\tu, err := url.Parse(urlString)\n")
	src.WriteString("\tif err != nil {\n")
	src.WriteString("\t\treturn nil, nil, fmt.Errorf(\"parse request url: %w\", err)\n")
	src.WriteString("\t}\n")
	src.WriteString("\tif len(opts.Query) > 0 {\n")
	src.WriteString("\t\tq := u.Query()\n")
	src.WriteString("\t\tfor key, values := range opts.Query {\n")
	src.WriteString("\t\t\tfor _, value := range values {\n")
	src.WriteString("\t\t\t\tq.Add(key, value)\n")
	src.WriteString("\t\t\t}\n")
	src.WriteString("\t\t}\n")
	src.WriteString("\t\tu.RawQuery = q.Encode()\n")
	src.WriteString("\t}\n")
	src.WriteString("\tvar body io.Reader\n")
	src.WriteString("\tif opts.Body != nil {\n")
	src.WriteString("\t\tencoded, err := json.Marshal(opts.Body)\n")
	src.WriteString("\t\tif err != nil {\n")
	src.WriteString("\t\t\treturn nil, nil, fmt.Errorf(\"encode request body: %w\", err)\n")
	src.WriteString("\t\t}\n")
	src.WriteString("\t\tbody = bytes.NewReader(encoded)\n")
	src.WriteString("\t}\n")
	src.WriteString("\treq, err := http.NewRequestWithContext(ctx, cmd.Method, u.String(), body)\n")
	src.WriteString("\tif err != nil {\n")
	src.WriteString("\t\treturn nil, nil, fmt.Errorf(\"build request: %w\", err)\n")
	src.WriteString("\t}\n")
	src.WriteString("\treq.Header.Set(\"Accept\", \"application/json\")\n")
	src.WriteString("\tif opts.Body != nil {\n")
	src.WriteString("\t\treq.Header.Set(\"Content-Type\", \"application/json\")\n")
	src.WriteString("\t}\n")
	src.WriteString("\tfor key, value := range opts.Headers {\n")
	src.WriteString("\t\tif strings.TrimSpace(key) == \"\" {\n")
	src.WriteString("\t\t\tcontinue\n")
	src.WriteString("\t\t}\n")
	src.WriteString("\t\treq.Header.Set(key, value)\n")
	src.WriteString("\t}\n")
	src.WriteString("\tresp, err := c.HTTPClient.Do(req)\n")
	src.WriteString("\tif err != nil {\n")
	src.WriteString("\t\treturn nil, nil, fmt.Errorf(\"perform request: %w\", err)\n")
	src.WriteString("\t}\n")
	src.WriteString("\tbodyBytes, readErr := io.ReadAll(resp.Body)\n")
	src.WriteString("\t_ = resp.Body.Close()\n")
	src.WriteString("\tif readErr != nil {\n")
	src.WriteString("\t\treturn resp, nil, fmt.Errorf(\"read response: %w\", readErr)\n")
	src.WriteString("\t}\n")
	src.WriteString("\tif resp.StatusCode >= http.StatusBadRequest {\n")
	src.WriteString("\t\treturn resp, bodyBytes, fmt.Errorf(\"request failed: status=%d body=%s\", resp.StatusCode, string(bodyBytes))\n")
	src.WriteString("\t}\n")
	src.WriteString("\treturn resp, bodyBytes, nil\n")
	src.WriteString("}\n\n")

	src.WriteString("func renderPath(template string, pathParams map[string]string) (string, error) {\n")
	src.WriteString("\tb := template\n")
	src.WriteString("\tfor {\n")
	src.WriteString("\t\tstart := strings.IndexByte(b, '{')\n")
	src.WriteString("\t\tif start < 0 {\n")
	src.WriteString("\t\t\treturn b, nil\n")
	src.WriteString("\t\t}\n")
	src.WriteString("\t\tend := strings.IndexByte(b[start:], '}')\n")
	src.WriteString("\t\tif end < 0 {\n")
	src.WriteString("\t\t\treturn \"\", fmt.Errorf(\"invalid path template: %s\", template)\n")
	src.WriteString("\t\t}\n")
	src.WriteString("\t\tend += start\n")
	src.WriteString("\t\tname := b[start+1 : end]\n")
	src.WriteString("\t\tvalue, ok := pathParams[name]\n")
	src.WriteString("\t\tif !ok {\n")
	src.WriteString("\t\t\treturn \"\", fmt.Errorf(\"missing path param %q\", name)\n")
	src.WriteString("\t\t}\n")
	src.WriteString("\t\tb = b[:start] + url.PathEscape(value) + b[end+1:]\n")
	src.WriteString("\t}\n")
	src.WriteString("}\n\n")

	for _, cmd := range commands {
		if len(cmd.PathParams) == 0 {
			src.WriteString(fmt.Sprintf("func (c *Client) %s(ctx context.Context, opts RequestOptions) (*http.Response, []byte, error) {\n", cmd.GoMethod))
			src.WriteString(fmt.Sprintf("\treturn c.Invoke(ctx, %q, nil, opts)\n", cmd.CommandID))
			src.WriteString("}\n\n")
			continue
		}
		src.WriteString(fmt.Sprintf("func (c *Client) %s(ctx context.Context, pathParams map[string]string, opts RequestOptions) (*http.Response, []byte, error) {\n", cmd.GoMethod))
		src.WriteString(fmt.Sprintf("\treturn c.Invoke(ctx, %q, pathParams, opts)\n", cmd.CommandID))
		src.WriteString("}\n\n")
	}

	formatted, err := format.Source(src.Bytes())
	if err != nil {
		return fmt.Errorf("format go source: %w", err)
	}

	return os.WriteFile(filepath.Join(clientDir, "client_gen.go"), formatted, 0o644)
}

func writeTSClient(tsOutDir string, commands []command, packageName string) error {
	if err := os.MkdirAll(tsOutDir, 0o755); err != nil {
		return err
	}

	metaJSON, err := json.MarshalIndent(commands, "", "  ")
	if err != nil {
		return err
	}

	var b strings.Builder
	b.WriteString("export type HttpMethod = \"GET\" | \"POST\" | \"PUT\" | \"PATCH\" | \"DELETE\";\n\n")
	b.WriteString("export interface Example {\n")
	b.WriteString("  title: string;\n")
	b.WriteString("  command: string;\n")
	b.WriteString("  description?: string;\n")
	b.WriteString("}\n\n")
	b.WriteString("export interface CommandSpec {\n")
	b.WriteString("  command_id: string;\n")
	b.WriteString("  cli_path: string;\n")
	b.WriteString("  method: HttpMethod;\n")
	b.WriteString("  path: string;\n")
	b.WriteString("  operation_id: string;\n")
	b.WriteString("  summary?: string;\n")
	b.WriteString("  description?: string;\n")
	b.WriteString("  why?: string;\n")
	b.WriteString("  group?: string;\n")
	b.WriteString("  path_params?: string[];\n")
	b.WriteString("  input_mode?: string;\n")
	b.WriteString("  streaming?: unknown;\n")
	b.WriteString("  output_envelope?: string;\n")
	b.WriteString("  error_codes?: string[];\n")
	b.WriteString("  stability?: string;\n")
	b.WriteString("  surface?: string;\n")
	b.WriteString("  agent_notes?: string;\n")
	b.WriteString("  concepts?: string[];\n")
	b.WriteString("  adjacent_commands?: string[];\n")
	b.WriteString("  examples?: Example[];\n")
	b.WriteString("  go_method: string;\n")
	b.WriteString("  ts_method: string;\n")
	b.WriteString("}\n\n")
	b.WriteString("export interface RequestOptions {\n")
	b.WriteString("  query?: Record<string, string | number | boolean | Array<string | number | boolean> | undefined>;\n")
	b.WriteString("  headers?: Record<string, string>;\n")
	b.WriteString("  body?: unknown;\n")
	b.WriteString("}\n\n")
	b.WriteString("export interface InvokeResult {\n")
	b.WriteString("  status: number;\n")
	b.WriteString("  headers: Headers;\n")
	b.WriteString("  body: string;\n")
	b.WriteString("}\n\n")

	b.WriteString("export const commandRegistry: CommandSpec[] = ")
	b.Write(metaJSON)
	b.WriteString(" as CommandSpec[];\n\n")

	b.WriteString("const commandIndex = new Map(commandRegistry.map((command) => [command.command_id, command] as const));\n\n")

	b.WriteString("function renderPath(pathTemplate: string, pathParams: Record<string, string> = {}): string {\n")
	b.WriteString("  return pathTemplate.replace(/\\{([^{}]+)\\}/g, (_match, name: string) => {\n")
	b.WriteString("    const value = pathParams[name];\n")
	b.WriteString("    if (value === undefined) {\n")
	b.WriteString("      throw new Error(`missing path param ${name}`);\n")
	b.WriteString("    }\n")
	b.WriteString("    return encodeURIComponent(value);\n")
	b.WriteString("  });\n")
	b.WriteString("}\n\n")

	b.WriteString("function withQuery(path: string, query: RequestOptions[\"query\"]): string {\n")
	b.WriteString("  if (!query) {\n")
	b.WriteString("    return path;\n")
	b.WriteString("  }\n")
	b.WriteString("  const params = new URLSearchParams();\n")
	b.WriteString("  for (const [key, value] of Object.entries(query)) {\n")
	b.WriteString("    if (value === undefined) {\n")
	b.WriteString("      continue;\n")
	b.WriteString("    }\n")
	b.WriteString("    if (Array.isArray(value)) {\n")
	b.WriteString("      for (const entry of value) {\n")
	b.WriteString("        params.append(key, String(entry));\n")
	b.WriteString("      }\n")
	b.WriteString("      continue;\n")
	b.WriteString("    }\n")
	b.WriteString("    params.set(key, String(value));\n")
	b.WriteString("  }\n")
	b.WriteString("  const encoded = params.toString();\n")
	b.WriteString("  if (!encoded) {\n")
	b.WriteString("    return path;\n")
	b.WriteString("  }\n")
	b.WriteString("  return `${path}?${encoded}`;\n")
	b.WriteString("}\n\n")

	b.WriteString("export class OarClient {\n")
	b.WriteString("  private readonly baseUrl: string;\n")
	b.WriteString("  private readonly fetchFn: typeof fetch;\n\n")
	b.WriteString("  constructor(baseUrl: string, fetchFn: typeof fetch = fetch) {\n")
	b.WriteString("    this.baseUrl = String(baseUrl || \"\").replace(/\\/+$/, \"\");\n")
	b.WriteString("    this.fetchFn = fetchFn;\n")
	b.WriteString("  }\n\n")
	b.WriteString("  async invoke(commandId: string, pathParams: Record<string, string> = {}, options: RequestOptions = {}): Promise<InvokeResult> {\n")
	b.WriteString("    if (!this.baseUrl) {\n")
	b.WriteString("      throw new Error(\"baseUrl is required\");\n")
	b.WriteString("    }\n")
	b.WriteString("    const command = commandIndex.get(commandId);\n")
	b.WriteString("    if (!command) {\n")
	b.WriteString("      throw new Error(`unknown command id: ${commandId}`);\n")
	b.WriteString("    }\n")
	b.WriteString("    const path = withQuery(renderPath(command.path, pathParams), options.query);\n")
	b.WriteString("    const response = await this.fetchFn(`${this.baseUrl}${path}`, {\n")
	b.WriteString("      method: command.method,\n")
	b.WriteString("      headers: {\n")
	b.WriteString("        accept: \"application/json\",\n")
	b.WriteString("        ...(options.body !== undefined ? { \"content-type\": \"application/json\" } : {}),\n")
	b.WriteString("        ...(options.headers ?? {}),\n")
	b.WriteString("      },\n")
	b.WriteString("      body: options.body !== undefined ? JSON.stringify(options.body) : undefined,\n")
	b.WriteString("    });\n")
	b.WriteString("    const body = await response.text();\n")
	b.WriteString("    if (!response.ok) {\n")
	b.WriteString("      throw new Error(`request failed for ${commandId}: ${response.status} ${response.statusText} ${body}`);\n")
	b.WriteString("    }\n")
	b.WriteString("    return { status: response.status, headers: response.headers, body };\n")
	b.WriteString("  }\n\n")

	for _, cmd := range commands {
		if len(cmd.PathParams) == 0 {
			b.WriteString(fmt.Sprintf("  %s(options: RequestOptions = {}): Promise<InvokeResult> {\n", cmd.TSMethod))
			b.WriteString(fmt.Sprintf("    return this.invoke(%q, {}, options);\n", cmd.CommandID))
			b.WriteString("  }\n\n")
			continue
		}
		b.WriteString(fmt.Sprintf("  %s(pathParams: Record<string, string>, options: RequestOptions = {}): Promise<InvokeResult> {\n", cmd.TSMethod))
		b.WriteString(fmt.Sprintf("    return this.invoke(%q, pathParams, options);\n", cmd.CommandID))
		b.WriteString("  }\n\n")
	}

	b.WriteString("}\n")

	if err := os.WriteFile(filepath.Join(tsOutDir, "client.ts"), []byte(b.String()), 0o644); err != nil {
		return err
	}

	index := "export * from \"./client\";\n"
	if err := os.WriteFile(filepath.Join(tsOutDir, "index.ts"), []byte(index), 0o644); err != nil {
		return err
	}

	tsconfig := `{
  "compilerOptions": {
    "target": "ES2020",
    "module": "ES2020",
    "moduleResolution": "Bundler",
    "lib": ["ES2020", "DOM"],
    "strict": true,
    "skipLibCheck": true,
    "declaration": true,
    "outDir": "./dist"
  },
  "include": ["client.ts", "index.ts"]
}
`
	if err := os.WriteFile(filepath.Join(tsOutDir, "tsconfig.json"), []byte(tsconfig), 0o644); err != nil {
		return err
	}

	pkg := `{
  "name": "` + packageName + `",
  "private": true,
  "type": "module"
}
`
	if err := os.WriteFile(filepath.Join(tsOutDir, "package.json"), []byte(pkg), 0o644); err != nil {
		return err
	}

	return nil
}

func compactStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		out = append(out, value)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func compactExamples(values []oarExample) []oarExample {
	if len(values) == 0 {
		return nil
	}
	out := make([]oarExample, 0, len(values))
	for _, value := range values {
		value.Title = strings.TrimSpace(value.Title)
		value.Command = strings.TrimSpace(value.Command)
		value.Description = strings.TrimSpace(value.Description)
		if value.Title == "" && value.Command == "" {
			continue
		}
		out = append(out, value)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func commandGroup(cliPath string) string {
	parts := strings.Fields(strings.TrimSpace(cliPath))
	if len(parts) == 0 {
		return ""
	}
	return strings.TrimSpace(parts[0])
}

func deriveGroups(commands []command) []groupMeta {
	commandSetByGroup := map[string]map[string]struct{}{}
	for _, cmd := range commands {
		group := strings.TrimSpace(cmd.Group)
		if group == "" {
			group = commandGroup(cmd.CLIPath)
		}
		if group == "" {
			continue
		}
		existing, ok := commandSetByGroup[group]
		if !ok {
			existing = map[string]struct{}{}
			commandSetByGroup[group] = existing
		}
		existing[cmd.CommandID] = struct{}{}
	}

	groups := make([]groupMeta, 0, len(commandSetByGroup))
	for groupName, commandSet := range commandSetByGroup {
		commandIDs := make([]string, 0, len(commandSet))
		for commandID := range commandSet {
			commandIDs = append(commandIDs, commandID)
		}
		sort.Strings(commandIDs)
		groups = append(groups, groupMeta{
			Name:         groupName,
			CommandCount: len(commandIDs),
			CommandIDs:   commandIDs,
		})
	}
	sort.Slice(groups, func(i, j int) bool {
		pi, ni := commandGroupSortKey(groups[i].Name)
		pj, nj := commandGroupSortKey(groups[j].Name)
		if pi != pj {
			return pi < pj
		}
		return ni < nj
	})
	return groups
}

// commandGroupSortKey orders CLI help groups: primary operator surface (topics) before
// diagnostic threads tooling; everything else lexicographically.
func commandGroupSortKey(name string) (prio int, sortName string) {
	switch strings.TrimSpace(name) {
	case "topics":
		return 0, name
	case "threads":
		return 1, name
	default:
		return 10, name
	}
}

func deriveConcepts(commands []command) []conceptMeta {
	commandsByConcept := map[string][]command{}
	for _, cmd := range commands {
		for _, concept := range cmd.Concepts {
			concept = strings.ToLower(strings.TrimSpace(concept))
			if concept == "" {
				continue
			}
			commandsByConcept[concept] = append(commandsByConcept[concept], cmd)
		}
	}

	concepts := make([]conceptMeta, 0, len(commandsByConcept))
	for conceptName, conceptCommands := range commandsByConcept {
		sort.Slice(conceptCommands, func(i, j int) bool {
			return conceptCommands[i].CommandID < conceptCommands[j].CommandID
		})
		commandIDs := make([]string, 0, len(conceptCommands))
		for _, cmd := range conceptCommands {
			commandIDs = append(commandIDs, cmd.CommandID)
		}
		concepts = append(concepts, conceptMeta{
			Name:         conceptName,
			CommandCount: len(commandIDs),
			CommandIDs:   commandIDs,
			Commands:     conceptCommands,
		})
	}
	sort.Slice(concepts, func(i, j int) bool { return concepts[i].Name < concepts[j].Name })
	return concepts
}

func extractPathParams(path string) []string {
	matches := pathParamPattern.FindAllStringSubmatch(path, -1)
	if len(matches) == 0 {
		return nil
	}

	params := make([]string, 0, len(matches))
	seen := make(map[string]struct{}, len(matches))
	for _, m := range matches {
		if len(m) < 2 {
			continue
		}
		name := strings.TrimSpace(m[1])
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		params = append(params, name)
	}
	if len(params) == 0 {
		return nil
	}
	return params
}

func toCamelCase(value string) string {
	pascal := toPascalCase(value)
	if pascal == "" {
		return "command"
	}
	if len(pascal) == 1 {
		return strings.ToLower(pascal)
	}
	return strings.ToLower(pascal[:1]) + pascal[1:]
}

func toPascalCase(value string) string {
	parts := strings.FieldsFunc(value, func(r rune) bool {
		if r >= 'a' && r <= 'z' {
			return false
		}
		if r >= 'A' && r <= 'Z' {
			return false
		}
		if r >= '0' && r <= '9' {
			return false
		}
		return true
	})
	if len(parts) == 0 {
		return "Command"
	}

	var b strings.Builder
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		lower := strings.ToLower(part)
		b.WriteString(strings.ToUpper(lower[:1]))
		if len(lower) > 1 {
			b.WriteString(lower[1:])
		}
	}
	result := b.String()
	if result == "" {
		return "Command"
	}
	if result[0] >= '0' && result[0] <= '9' {
		return "Command" + result
	}
	return result
}

func exitf(format string, args ...any) {
	_, _ = fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
