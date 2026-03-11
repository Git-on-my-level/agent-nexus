package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunImportIsConfigLenient(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	raw := runCLIForTest(t, home, map[string]string{}, nil, []string{"import"})
	if !strings.Contains(raw, "Import bootstrap") {
		t.Fatalf("expected import bootstrap help, got %q", raw)
	}
	if !strings.Contains(raw, "oar help import") {
		t.Fatalf("expected import read order guidance, got %q", raw)
	}
}

func TestHelpTopicImport(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	raw := runCLIForTest(t, home, map[string]string{}, nil, []string{"help", "import"})
	if !strings.Contains(raw, "Recommended loop") {
		t.Fatalf("expected import help text, got %q", raw)
	}
	if !strings.Contains(raw, "oar import scan") {
		t.Fatalf("expected scan guidance, got %q", raw)
	}
}

func TestImportScanJSON(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	input := filepath.Join(home, "input")
	if err := os.MkdirAll(input, 0o755); err != nil {
		t.Fatalf("mkdir input: %v", err)
	}
	if err := os.WriteFile(filepath.Join(input, "note.md"), []byte("# Note\n\nImported content.\n"), 0o644); err != nil {
		t.Fatalf("write input note: %v", err)
	}

	raw := runCLIForTest(t, home, map[string]string{}, nil, []string{"--json", "import", "scan", "--input", input})
	payload := assertEnvelopeOK(t, raw)
	if payload["command"] != "import scan" {
		t.Fatalf("unexpected command field: %#v", payload["command"])
	}
	data, _ := payload["data"].(map[string]any)
	if data == nil {
		t.Fatalf("expected data in envelope: %#v", payload)
	}
	if got := int(data["file_count"].(float64)); got != 1 {
		t.Fatalf("expected file_count=1 got %d payload=%#v", got, payload)
	}
	inventoryPath := anyStringValue(data["inventory"])
	if strings.TrimSpace(inventoryPath) == "" {
		t.Fatalf("expected inventory path in response: %#v", payload)
	}
	if _, err := os.Stat(inventoryPath); err != nil {
		t.Fatalf("expected inventory file to exist: %v", err)
	}

	inventoryData, err := os.ReadFile(inventoryPath)
	if err != nil {
		t.Fatalf("read inventory: %v", err)
	}
	if !strings.Contains(string(inventoryData), "note.md") {
		t.Fatalf("expected inventory to mention note.md, got %s", string(inventoryData))
	}

	var decoded map[string]any
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		t.Fatalf("decode raw envelope: %v", err)
	}
}

func TestImportApplyPreviewIsConfigLenient(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	profilesDir := filepath.Join(home, ".config", "oar", "profiles")
	if err := os.MkdirAll(profilesDir, 0o700); err != nil {
		t.Fatalf("mkdir profiles dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(profilesDir, "default.json"), []byte("{invalid"), 0o600); err != nil {
		t.Fatalf("write malformed profile: %v", err)
	}

	planPath := filepath.Join(home, "plan.json")
	if err := os.WriteFile(planPath, []byte(`{"created_at":"2026-03-12T00:00:00Z","source_name":"test","inventory":"inventory.jsonl","dedupe":"dedupe.json","objects":[]}`), 0o644); err != nil {
		t.Fatalf("write plan: %v", err)
	}

	raw := runCLIForTest(t, home, map[string]string{}, nil, []string{"--json", "import", "apply", "--plan", planPath})
	payload := assertEnvelopeOK(t, raw)
	if payload["command"] != "import apply" {
		t.Fatalf("unexpected command: %#v", payload["command"])
	}
}

func TestImportApplyExecuteRequiresResolvedConfig(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	profilesDir := filepath.Join(home, ".config", "oar", "profiles")
	if err := os.MkdirAll(profilesDir, 0o700); err != nil {
		t.Fatalf("mkdir profiles dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(profilesDir, "default.json"), []byte("{invalid"), 0o600); err != nil {
		t.Fatalf("write malformed profile: %v", err)
	}

	planPath := filepath.Join(home, "plan.json")
	if err := os.WriteFile(planPath, []byte(`{"created_at":"2026-03-12T00:00:00Z","source_name":"test","inventory":"inventory.jsonl","dedupe":"dedupe.json","objects":[]}`), 0o644); err != nil {
		t.Fatalf("write plan: %v", err)
	}

	raw := runCLIForTest(t, home, map[string]string{}, nil, []string{"--json", "import", "apply", "--plan", planPath, "--execute"})
	payload := assertEnvelopeError(t, raw)
	errPayload, _ := payload["error"].(map[string]any)
	if anyStringValue(errPayload["code"]) != "config_resolution_failed" {
		t.Fatalf("expected config_resolution_failed, got %#v", payload)
	}
}
