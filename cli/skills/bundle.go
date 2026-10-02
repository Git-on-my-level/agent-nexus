// Package skills contains the canonical, portable ANX skill sources. The same
// bytes are embedded in the CLI, shipped in archives, and consumed by agentctl.
package skills

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
)

//go:embed anx-participant/SKILL.md anx-pm/SKILL.md catalog.json participant.json pm.json
var Files embed.FS

type Skill struct {
	Role    string `json:"role"`
	Name    string `json:"name"`
	Version string `json:"version"`
	SHA256  string `json:"content_sha256"`
	Content string `json:"-"`
}

func Digest(content []byte) string {
	sum := sha256.Sum256(content)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func Get(role string) (Skill, error) {
	var catalog struct {
		SchemaVersion int     `json:"schema_version"`
		Skills        []Skill `json:"skills"`
	}
	data, err := Files.ReadFile("catalog.json")
	if err != nil {
		return Skill{}, err
	}
	if err = json.Unmarshal(data, &catalog); err != nil {
		return Skill{}, err
	}
	if catalog.SchemaVersion != 1 {
		return Skill{}, fmt.Errorf("unsupported bundled skill catalog version")
	}
	for _, skill := range catalog.Skills {
		if skill.Role != role {
			continue
		}
		content, err := Files.ReadFile(skill.Name + "/SKILL.md")
		if err != nil {
			return Skill{}, err
		}
		if Digest(content) != skill.SHA256 || !strings.Contains(string(content), "Skill contract: "+skill.Version+".") || !strings.Contains(string(content), "\nname: "+skill.Name+"\n") {
			return Skill{}, fmt.Errorf("bundled skill metadata does not match content")
		}
		skill.Content = string(content)
		return skill, nil
	}
	return Skill{}, fmt.Errorf("unknown skill role %q; use participant or pm", role)
}
