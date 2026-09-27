package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAsPrecedenceAndLegacyProfileIgnored(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".config", "anx", "profiles")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "old.json"), []byte(`{"agent":"old","base_url":"http://old"}`), 0600); err != nil {
		t.Fatal(err)
	}
	get := func(k string) string {
		switch k {
		case "ANX_AS":
			return "env-agent"
		case "ANX_AGENT":
			return "legacy-agent"
		}
		return ""
	}
	flag := "flag-agent"
	got, err := Resolve(Overrides{As: &flag}, Environment{Getenv: get, UserHomeDir: func() (string, error) { return home, nil }})
	if err != nil {
		t.Fatal(err)
	}
	if got.As != "flag-agent" || got.IdentitySource != "flag:--as" || got.Agent != "flag-agent" {
		t.Fatalf("unexpected config: %+v", got)
	}
	if got.BaseURL != "http://127.0.0.1:8000" {
		t.Fatalf("legacy profile selected: %+v", got)
	}
}
