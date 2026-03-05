package registry

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"

	contractsclient "organization-autorunner-contracts-go-client/client"
)

//go:embed commands.json
var embeddedCommandsJSON []byte

type MetaRegistry struct {
	OpenAPIVersion  string           `json:"openapi_version"`
	ContractVersion string           `json:"contract_version"`
	GeneratedBy     string           `json:"generated_by"`
	ExtensionPrefix string           `json:"extension_prefix"`
	CommandCount    int              `json:"command_count"`
	Commands        []map[string]any `json:"commands"`
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

func LoadEmbedded() (MetaRegistry, error) {
	return parseMeta(embeddedCommandsJSON)
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
	return out, nil
}
