package skills

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCanonicalSkillMetadataAndPacks(t *testing.T) {
	for _, role := range []string{"participant", "pm"} {
		skill, err := Get(role)
		if err != nil {
			t.Fatal(err)
		}
		disk, err := os.ReadFile(filepath.Join(skill.Name, "SKILL.md"))
		if err != nil {
			t.Fatal(err)
		}
		if string(disk) != skill.Content || Digest(disk) != skill.SHA256 {
			t.Fatal("embedded/disk/hash drift")
		}
		var manifest struct {
			SchemaVersion int `json:"schema_version"`
			Skills        []struct {
				Name    string   `json:"name"`
				Path    string   `json:"path"`
				Targets []string `json:"targets"`
			} `json:"skills"`
		}
		raw, err := Files.ReadFile(role + ".json")
		if err != nil {
			t.Fatal(err)
		}
		if err = json.Unmarshal(raw, &manifest); err != nil {
			t.Fatal(err)
		}
		count := 1
		if role == "pm" {
			count = 2
		}
		if manifest.SchemaVersion != 1 || len(manifest.Skills) != count {
			t.Fatalf("invalid pack: %+v", manifest)
		}
		if manifest.Skills[0].Name != "anx-participant" {
			t.Fatal("participant missing from pack")
		}
		for _, entry := range manifest.Skills {
			if entry.Path != "cli/skills/"+entry.Name {
				t.Fatalf("noncanonical path: %s", entry.Path)
			}
			if strings.Join(entry.Targets, ",") != "claude,codex,cursor,hermes,omp" {
				t.Fatalf("unexpected targets: %v", entry.Targets)
			}
			if _, err := Files.ReadFile(entry.Name + "/SKILL.md"); err != nil {
				t.Fatal(err)
			}
		}
	}
	if _, err := Get("unknown"); err == nil {
		t.Fatal("unknown role accepted")
	}
}

func TestSkillAuthorityBoundaries(t *testing.T) {
	participant, _ := Get("participant")
	pm, _ := Get("pm")
	for _, term := range []string{"explicit task ref", "source-owned", "raw transcripts", "ambiguous", "Reading context alone", "A finished run or closed session never completes a task"} {
		if !strings.Contains(participant.Content, term) {
			t.Errorf("participant lacks %q", term)
		}
	}
	for _, term := range []string{"explicitly designated", "ordinary existing agent", "independent goal driver", "Designation grants no new permissions", "last successful read", "excluded paths", "human decision gates", "Load anx-participant alongside"} {
		if !strings.Contains(pm.Content, term) {
			t.Errorf("PM lacks %q", term)
		}
	}
}
