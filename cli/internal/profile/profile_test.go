package profile

import (
	"crypto/ed25519"
	"path/filepath"
	"testing"
)

func TestSaveLoadAndListProfiles(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	if err := EnsureDirs(home); err != nil {
		t.Fatalf("ensure dirs: %v", err)
	}
	path := ProfilePath(home, "agent-one")
	jsonMode := true
	if err := Save(path, Profile{Agent: "agent-one", BaseURL: "http://127.0.0.1:8000", JSON: &jsonMode}); err != nil {
		t.Fatalf("save profile: %v", err)
	}

	loaded, ok, err := Load(path)
	if err != nil {
		t.Fatalf("load profile: %v", err)
	}
	if !ok {
		t.Fatal("expected profile to exist")
	}
	if loaded.Agent != "agent-one" || loaded.BaseURL != "http://127.0.0.1:8000" {
		t.Fatalf("unexpected profile payload: %#v", loaded)
	}
	if loaded.JSON == nil || !*loaded.JSON {
		t.Fatalf("expected persisted json mode in profile, got %#v", loaded.JSON)
	}
	if loaded.Version != ProfileVersion {
		t.Fatalf("unexpected profile version: %d", loaded.Version)
	}

	agents, err := ListAgents(home)
	if err != nil {
		t.Fatalf("list agents: %v", err)
	}
	if len(agents) != 1 || agents[0] != "agent-one" {
		t.Fatalf("unexpected agent list: %#v", agents)
	}
}

func TestGenerateSaveLoadPrivateKey(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	if err := EnsureDirs(home); err != nil {
		t.Fatalf("ensure dirs: %v", err)
	}
	publicKey, privateKey, err := GenerateEd25519KeyPair()
	if err != nil {
		t.Fatalf("generate key pair: %v", err)
	}
	if publicKey == "" {
		t.Fatal("expected public key")
	}
	if len(privateKey) != ed25519.PrivateKeySize {
		t.Fatalf("unexpected private key length: %d", len(privateKey))
	}

	keyPath := KeyPath(home, "agent-one")
	if err := SavePrivateKey(keyPath, privateKey); err != nil {
		t.Fatalf("save private key: %v", err)
	}
	loaded, err := LoadPrivateKey(keyPath)
	if err != nil {
		t.Fatalf("load private key: %v", err)
	}
	if string(loaded) != string(privateKey) {
		t.Fatal("loaded private key does not match saved key")
	}
	if filepath.Ext(keyPath) != ".ed25519" {
		t.Fatalf("unexpected key path extension: %s", keyPath)
	}
}
