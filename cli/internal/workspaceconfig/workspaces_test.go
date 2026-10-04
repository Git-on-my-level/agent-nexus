package workspaceconfig

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"agent-nexus-cli/internal/hostidentity"
)

func TestDirectoryGlobMatchingAndSpecificity(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	for _, tc := range []struct {
		name, cwd string
		rules     map[string]string
		want      string
	}{
		{"recursive includes root", filepath.Join(home, "work"), map[string]string{"~/work/**": "https://a.example"}, "~/work/**"},
		{"recursive nested", filepath.Join(home, "work", "repo", "deep"), map[string]string{"~/work/**": "https://a.example"}, "~/work/**"},
		{"single star component", filepath.Join(home, "work", "repo", "deep"), map[string]string{"~/work/*": "https://a.example"}, ""},
		{"recursive middle zero components", filepath.Join(home, "work", "repo"), map[string]string{"~/work/**/repo": "https://a.example"}, "~/work/**/repo"},
		{"character classes", filepath.Join(home, "work", "repo1"), map[string]string{"~/work/repo[12]": "https://a.example"}, "~/work/repo[12]"},
		{"longer prefix", filepath.Join(home, "work", "omi", "repo"), map[string]string{"~/work/**": "https://a.example", "~/work/omi/**": "https://b.example"}, "~/work/omi/**"},
		{"more literal characters", filepath.Join(home, "work", "omi", "repo"), map[string]string{"~/work/*/**": "https://a.example", "~/work/*/repo": "https://b.example"}, "~/work/*/repo"},
		{"deterministic tie", filepath.Join(home, "work", "repo"), map[string]string{"~/work/re*o": "https://a.example", "~/work/re?o": "https://b.example"}, "~/work/re*o"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := Catalog{File: File{Rules: tc.rules}}
			for n := 0; n < 20; n++ {
				rule, _, err := c.MatchingRule(tc.cwd, home)
				if err != nil || rule != tc.want {
					t.Fatalf("rule=%q want=%q err=%v", rule, tc.want, err)
				}
			}
		})
	}
	for _, glob := range []string{"relative/**", "~/work/[", "~/work/a**b"} {
		if _, err := NormalizeGlob(glob, home); err == nil {
			t.Fatalf("accepted invalid glob %q", glob)
		}
	}
}

func TestAliasDiscoveryCollisionsAndReadOnly(t *testing.T) {
	dir := t.TempDir()
	for _, host := range []hostidentity.Host{
		{ID: "a", WorkspaceID: "ws_a", WorkspaceSlug: "personal", BaseURL: "https://a.example"},
		{ID: "b", WorkspaceID: "ws_b", WorkspaceSlug: "personal", BaseURL: "https://b.example"},
	} {
		hostDir := hostidentity.DirAt(dir, host.WorkspaceID, host.BaseURL)
		if err := os.MkdirAll(hostDir, 0700); err != nil {
			t.Fatal(err)
		}
		data, _ := json.Marshal(host)
		if err := os.WriteFile(filepath.Join(hostDir, "host.json"), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	c, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if c.File.Aliases["personal"] != "https://a.example" || c.File.Aliases["personal-2"] != "https://b.example" {
		t.Fatalf("aliases=%v", c.File.Aliases)
	}
	if _, err := os.Stat(filepath.Join(dir, Filename)); !os.IsNotExist(err) {
		t.Fatalf("discovery wrote config: %v", err)
	}
	if err := Update(dir, func(c *Catalog) error { c.File.Default = "personal-2"; return nil }); err != nil {
		t.Fatal(err)
	}
	c, err = Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if c.File.Default != "personal-2" || c.File.AddAlias("other-name", "https://a.example/") != "personal" {
		t.Fatalf("config=%v", c.File)
	}
	if c.File.AddAlias("personal", "https://c.example") != "personal-3" {
		t.Fatalf("collision=%v", c.File.Aliases)
	}
}

func TestCorruptWorkspaceConfigAndHostFailClosed(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, Filename), []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(dir); err == nil {
		t.Fatal("ignored malformed preferences")
	}
	os.Remove(filepath.Join(dir, Filename))
	hostDir := filepath.Join(dir, "hosts", "broken")
	os.MkdirAll(hostDir, 0700)
	os.WriteFile(filepath.Join(hostDir, "host.json"), []byte("{"), 0600)
	if _, err := Load(dir); err == nil {
		t.Fatal("ignored corrupt enrollment")
	}
}

func TestWorkspaceURLs(t *testing.T) {
	for _, value := range []string{"https://core.example/ws/a/main", "http://127.0.0.1:8000"} {
		if err := ValidateURL(value); err != nil {
			t.Fatal(err)
		}
	}
	for _, value := range []string{"", "relative", "ftp://core.example", "https://user:secret@core.example", "https://core.example?token=secret", "https://core.example#x"} {
		if err := ValidateURL(value); err == nil {
			t.Fatalf("accepted %q", value)
		}
	}
}

func TestDirectoryRulesFollowSymlinkedHomeAndDirectory(t *testing.T) {
	root := t.TempDir()
	physical := filepath.Join(root, "physical")
	if err := os.MkdirAll(filepath.Join(physical, "work", "repo"), 0700); err != nil {
		t.Fatal(err)
	}
	logical := filepath.Join(root, "logical-home")
	if err := os.Symlink(physical, logical); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	c := Catalog{File: File{Rules: map[string]string{"~/work/**": "https://a.example"}}}
	rule, _, err := c.MatchingRule(filepath.Join(physical, "work", "repo"), logical)
	if err != nil || rule != "~/work/**" {
		t.Fatalf("symlinked home: rule=%s err=%v", rule, err)
	}
	// Future directories can still be mapped before they are created.
	c.File.Rules = map[string]string{filepath.Join(logical, "future", "**"): "https://a.example"}
	rule, _, err = c.MatchingRule(filepath.Join(physical, "future", "repo"), logical)
	if err != nil || rule == "" {
		t.Fatalf("future directory: rule=%s err=%v", rule, err)
	}
}

func TestDirectoryRulesRespectFilesystemCaseSemantics(t *testing.T) {
	root := t.TempDir()
	actual := filepath.Join(root, "CamelCase")
	if err := os.MkdirAll(filepath.Join(actual, "Child"), 0700); err != nil {
		t.Fatal(err)
	}
	actualInfo, err := os.Stat(actual)
	if err != nil {
		t.Fatal(err)
	}
	differentCase := filepath.Join(root, "camelcase")
	differentInfo, differentErr := os.Stat(differentCase)
	equivalent := differentErr == nil && os.SameFile(actualInfo, differentInfo)
	// No global lowercasing: a differently cased rule applies only if the volume
	// actually considers the existing directories the same filesystem object.
	c := Catalog{File: File{Default: "https://a.example", Rules: map[string]string{filepath.Join(differentCase, "**"): "https://b.example"}}}
	base, source, err := c.Resolve(filepath.Join(actual, "Child"), root)
	if err != nil {
		t.Fatal(err)
	}
	if equivalent {
		if base != "https://b.example" || source == "config:default" {
			t.Fatalf("case-insensitive volume lost rule: %s via %s", base, source)
		}
		// Cwd spelling is normalized as well, including a mixed-case child path.
		base, _, err = c.Resolve(filepath.Join(root, "CAMELCASE", "cHILD"), root)
		if err != nil || base != "https://b.example" {
			t.Fatalf("cwd case: %s err=%v", base, err)
		}
		t.Log("verified case-insensitive filesystem equivalence")
	} else {
		if base != "https://a.example" || source != "config:default" {
			t.Fatalf("case-sensitive volume folded paths: %s via %s", base, source)
		}
		if err := os.MkdirAll(filepath.Join(differentCase, "Child"), 0700); err != nil {
			t.Fatal(err)
		}
		base, _, err = c.Resolve(filepath.Join(actual, "Child"), root)
		if err != nil || base != "https://a.example" {
			t.Fatalf("distinct dirs collapsed: %s err=%v", base, err)
		}
		base, _, err = c.Resolve(filepath.Join(differentCase, "Child"), root)
		if err != nil || base != "https://b.example" {
			t.Fatalf("exact rule: %s err=%v", base, err)
		}
		t.Log("verified case-sensitive filesystem distinction")
	}
}
