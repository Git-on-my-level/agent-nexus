package profile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestResolveUsesDefaultProfileAndToken(t *testing.T) {
	home := t.TempDir()
	writeProfile(t, home, "leo", `{"base_url":"http://core.local","access_token":"token-1"}`)
	writeFile(t, filepath.Join(home, ".config", "anx", "default-profile"), "leo\n")

	resolved, err := Resolve(Options{}, Environment{
		Getenv:      func(string) string { return "" },
		UserHomeDir: func() (string, error) { return home, nil },
	})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if resolved.Agent != "leo" || resolved.BaseURL != "http://core.local" || resolved.AccessToken != "token-1" {
		t.Fatalf("unexpected resolution: %#v", resolved)
	}
	if resolved.Sources["agent"] != "profile:default" {
		t.Fatalf("agent source = %q", resolved.Sources["agent"])
	}
}

func TestResolveFlagsOverrideProfile(t *testing.T) {
	home := t.TempDir()
	writeProfile(t, home, "alpha", `{"base_url":"http://profile.local","access_token":"profile-token"}`)

	resolved, err := Resolve(Options{
		Profile: "alpha",
		BaseURL: "http://flag.local",
		Timeout: 7 * time.Second,
	}, Environment{
		Getenv: func(key string) string {
			if key == "ANX_ACCESS_TOKEN" {
				return "env-token"
			}
			return ""
		},
		UserHomeDir: func() (string, error) { return home, nil },
	})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if resolved.Agent != "alpha" || resolved.BaseURL != "http://flag.local" || resolved.AccessToken != "env-token" || resolved.Timeout != 7*time.Second {
		t.Fatalf("unexpected resolution: %#v", resolved)
	}
	if resolved.Sources["baseUrl"] != "flag:--base-url" {
		t.Fatalf("baseUrl source = %q", resolved.Sources["baseUrl"])
	}
}

func TestResolveRejectsAmbiguousProfiles(t *testing.T) {
	home := t.TempDir()
	writeProfile(t, home, "alpha", `{"base_url":"http://alpha.local"}`)
	writeProfile(t, home, "beta", `{"base_url":"http://beta.local"}`)

	_, err := Resolve(Options{}, Environment{
		Getenv:      func(string) string { return "" },
		UserHomeDir: func() (string, error) { return home, nil },
	})
	if err == nil || !strings.Contains(err.Error(), "multiple local profiles found") {
		t.Fatalf("expected ambiguity error, got %v", err)
	}
}

func writeProfile(t *testing.T, home, agent, content string) {
	t.Helper()
	writeFile(t, ProfilePath(home, agent), content)
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
