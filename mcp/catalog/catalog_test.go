package catalog

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestToolNameDerivation(t *testing.T) {
	tests := map[string]string{
		"cards.get":              "anx_cards_get",
		"docs.revisions.create":  "anx_docs_revisions_create",
		"secrets.reveal-batch":   "anx_secrets_reveal_batch",
		"agent.notifications/v1": "anx_agent_notifications_v1",
	}
	for input, want := range tests {
		if got := ToolName(input); got != want {
			t.Fatalf("ToolName(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestBuildCatalogFromRegistryAndPolicy(t *testing.T) {
	cat, err := Build(fixtureRegistry(), fixturePolicy(), BuildOptions{})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	tools := cat.Tools()
	if len(tools) != 2 {
		t.Fatalf("tool count = %d, want 2", len(tools))
	}
	if tools[0].Name != "anx_cards_get" || tools[1].Name != "anx_docs_revisions_create" {
		t.Fatalf("tools not deterministic by name: %#v", []string{tools[0].Name, tools[1].Name})
	}

	cardTool, ok := cat.Lookup("anx_cards_get")
	if !ok {
		t.Fatal("expected cards.get tool")
	}
	if cardTool.Metadata.CommandID != "cards.get" || cardTool.Metadata.Method != "GET" || cardTool.Metadata.Path != "/cards/{card_id}" || cardTool.Metadata.RiskClass != "read" {
		t.Fatalf("unexpected metadata: %#v", cardTool.Metadata)
	}
	path, ok := cardTool.InputSchema["properties"].(map[string]any)["path"].(map[string]any)
	if !ok {
		t.Fatalf("expected path schema: %#v", cardTool.InputSchema)
	}
	required := path["required"].([]string)
	if len(required) != 1 || required[0] != "card_id" {
		t.Fatalf("path required = %#v, want card_id", required)
	}

	docTool, ok := cat.Lookup("anx_docs_revisions_create")
	if !ok {
		t.Fatal("expected docs.revisions.create tool")
	}
	props := docTool.InputSchema["properties"].(map[string]any)
	if _, ok := props["idempotency_key"]; !ok {
		t.Fatalf("write tool missing idempotency_key schema: %#v", props)
	}
	body := props["body"].(map[string]any)
	bodyRequired := body["required"].([]string)
	if len(bodyRequired) != 1 || bodyRequired[0] != "content_type" {
		t.Fatalf("body required = %#v, want content_type", bodyRequired)
	}
	if _, ok := cat.Lookup("anx_secrets_reveal_batch"); ok {
		t.Fatal("gated sensitive tool should not be invokable by default")
	}
	if _, ok := cat.Lookup("anx_auth_token"); ok {
		t.Fatal("unsupported bootstrap tool should not be invokable")
	}
}

func TestBuildCatalogAllowsGatedWithExplicitPolicy(t *testing.T) {
	cat, err := Build(fixtureRegistry(), fixturePolicy(), BuildOptions{AllowedClassifications: map[string]bool{
		ClassificationExposedRead:    true,
		ClassificationExposedWrite:   true,
		ClassificationGatedSensitive: true,
	}})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	tool, ok := cat.Lookup("anx_secrets_reveal_batch")
	if !ok {
		t.Fatal("expected explicitly allowed gated sensitive tool")
	}
	if tool.Metadata.RiskClass != "sensitive" {
		t.Fatalf("risk class = %q, want sensitive", tool.Metadata.RiskClass)
	}
}

func TestBuildCatalogDetectsToolNameCollision(t *testing.T) {
	registry := CommandRegistry{Commands: []Command{
		{CommandID: "cards.get", Method: "GET", Path: "/cards/{card_id}"},
		{CommandID: "cards-get", Method: "GET", Path: "/cards/{card_id}"},
	}}
	policy := Policy{
		ValidClassifications: []string{ClassificationExposedRead},
		Commands: map[string]PolicyEntry{
			"cards.get": {Classification: ClassificationExposedRead},
			"cards-get": {Classification: ClassificationExposedRead},
		},
	}
	_, err := Build(registry, policy, BuildOptions{})
	if err == nil || !strings.Contains(err.Error(), "tool name collision") {
		t.Fatalf("expected collision error, got %v", err)
	}
}

func TestBuildCatalogRequiresCompletePolicy(t *testing.T) {
	_, err := Build(CommandRegistry{Commands: []Command{{CommandID: "cards.get"}}}, Policy{Commands: map[string]PolicyEntry{}}, BuildOptions{})
	if err == nil || !strings.Contains(err.Error(), "missing policy entry") {
		t.Fatalf("expected missing policy error, got %v", err)
	}
}

func TestLoadPolicy(t *testing.T) {
	policy, err := LoadPolicy(strings.NewReader(`
schema_version: 1
valid_classifications:
  - exposed_read
commands:
  "cards.get":
    classification: exposed_read
    reason: "card read"
`))
	if err != nil {
		t.Fatalf("LoadPolicy() error = %v", err)
	}
	if policy.Commands["cards.get"].Reason != "card read" {
		t.Fatalf("unexpected policy: %#v", policy)
	}
}

func TestBuildCatalogFromGeneratedMetadataAndDefaultPolicy(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	mcpRoot := filepath.Dir(filepath.Dir(file))

	commandsFile, err := os.Open(filepath.Join(mcpRoot, "..", "contracts", "gen", "meta", "commands.json"))
	if err != nil {
		t.Fatalf("open commands metadata: %v", err)
	}
	defer commandsFile.Close()
	registry, err := LoadCommandRegistry(commandsFile)
	if err != nil {
		t.Fatalf("LoadCommandRegistry() error = %v", err)
	}

	policyFile, err := os.Open(filepath.Join(mcpRoot, "policy", "default_tool_policy.yaml"))
	if err != nil {
		t.Fatalf("open default policy: %v", err)
	}
	defer policyFile.Close()
	policy, err := LoadPolicy(policyFile)
	if err != nil {
		t.Fatalf("LoadPolicy() error = %v", err)
	}

	cat, err := Build(registry, policy, BuildOptions{})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if _, ok := cat.Lookup("anx_cards_get"); !ok {
		t.Fatal("expected exposed cards.get tool")
	}
	if _, ok := cat.Lookup("anx_auth_token"); ok {
		t.Fatal("auth.token must not be invokable")
	}
	if _, ok := cat.Lookup("anx_secrets_reveal_batch"); ok {
		t.Fatal("secrets.reveal-batch must not be invokable by default")
	}
	if got := len(cat.Tools()); got == 0 || got >= len(registry.Commands) {
		t.Fatalf("default catalog size = %d, registry size = %d", got, len(registry.Commands))
	}
}

func fixtureRegistry() CommandRegistry {
	return CommandRegistry{Commands: []Command{
		{
			CommandID:  "cards.get",
			Group:      "cards",
			Method:     "GET",
			Path:       "/cards/{card_id}",
			Summary:    "Get card",
			Why:        "Resolve one card.",
			InputMode:  "none",
			PathParams: []string{"card_id"},
		},
		{
			CommandID: "docs.revisions.create",
			Group:     "docs",
			Method:    "POST",
			Path:      "/docs/{document_id}/revisions",
			Summary:   "Create document revision",
			InputMode: "json-body",
			PathParams: []string{
				"document_id",
			},
			BodySchema: FieldSchema{Required: []Field{{Name: "content_type", Type: "string", EnumValues: []string{"text", "structured"}}}},
		},
		{
			CommandID:  "secrets.reveal-batch",
			Group:      "secret",
			Method:     "POST",
			Path:       "/secrets/reveal-batch",
			InputMode:  "json-body",
			BodySchema: FieldSchema{Required: []Field{{Name: "names", Type: "list<string>"}}},
		},
		{
			CommandID: "auth.token",
			Group:     "auth",
			Method:    "POST",
			Path:      "/auth/token",
			InputMode: "json-body",
		},
	}}
}

func fixturePolicy() Policy {
	return Policy{
		ValidClassifications: []string{
			ClassificationExposedRead,
			ClassificationExposedWrite,
			ClassificationGatedSensitive,
			ClassificationUnsupportedBootstrapAuth,
		},
		Commands: map[string]PolicyEntry{
			"cards.get":             {Classification: ClassificationExposedRead, Reason: "card read"},
			"docs.revisions.create": {Classification: ClassificationExposedWrite, Reason: "ordinary write"},
			"secrets.reveal-batch":  {Classification: ClassificationGatedSensitive, Reason: "secret reveal"},
			"auth.token":            {Classification: ClassificationUnsupportedBootstrapAuth, Reason: "token exchange"},
		},
	}
}
