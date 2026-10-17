// Package bridge owns the single OS-user host service, local registration and
// migration state. It never stores credentials or canonical workspace data.
package bridge

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"agent-nexus-cli/internal/integrations"
)

const MaxAgents = 256
const Label = "io.agent-nexus.bridge"
const Unit = "anx-bridge.service"

type Policy struct {
	MaxAttempts int           `json:"max_attempts"`
	Timeout     time.Duration `json:"command_timeout"`
}
type Runtime struct {
	Argv     []string          `json:"argv"`
	CWD      string            `json:"cwd"`
	Env      map[string]string `json:"env,omitempty"`
	Adapter  string            `json:"adapter,omitempty"`
	Agentctl string            `json:"agentctl,omitempty"`
}
type Agent struct {
	Name         string          `json:"name"`
	ActorID      string          `json:"actor_id"`
	Jobs         map[string]bool `json:"jobs"`
	RegistryDirs []string        `json:"registry_dirs"`
	Policy       Policy          `json:"execution_policy"`
	Runtime      *Runtime        `json:"runtime,omitempty"`
}
type Workspace struct {
	ID        string  `json:"workspace_id"`
	BaseURL   string  `json:"base_url"`
	HostID    string  `json:"host_id"`
	ConfigDir string  `json:"config_dir"`
	Agents    []Agent `json:"agents"`
}
type Registry struct {
	Version    int         `json:"schema_version"`
	Revision   int64       `json:"revision"`
	Workspaces []Workspace `json:"workspaces"`
}

func Root(home string) string { return filepath.Join(home, ".config", "anx", "bridge") }
func Namespace(base, actor string) string {
	v := sha256.Sum256([]byte(strings.TrimRight(base, "/") + "\n" + actor))
	return hex.EncodeToString(v[:16])
}
func AgentKey(w Workspace, a Agent) string { return w.BaseURL + "\x00" + w.HostID + "\x00" + a.ActorID }
func Load(root string) (Registry, error) {
	r := Registry{Version: 1, Workspaces: []Workspace{}}
	raw, e := integrations.PrivateRead(filepath.Join(root, "registry.json"))
	if errors.Is(e, os.ErrNotExist) {
		return r, nil
	}
	if e != nil {
		return r, e
	}
	if len(raw) > 2<<20 {
		return r, fmt.Errorf("bridge registry too large")
	}
	e = json.Unmarshal(raw, &r)
	if e == nil {
		e = r.Validate()
	}
	return r, e
}
func (r Registry) Validate() error {
	if r.Version != 1 || len(r.Workspaces) > 32 {
		return fmt.Errorf("invalid bridge registry version or workspace count")
	}
	seen := map[string]bool{}
	workspaces := map[string]bool{}
	count := 0
	for _, w := range r.Workspaces {
		wk := w.BaseURL + "\x00" + w.HostID
		if workspaces[wk] {
			return fmt.Errorf("duplicate workspace enrollment")
		}
		workspaces[wk] = true
		if w.ID == "" || w.HostID == "" || w.BaseURL == "" || !filepath.IsAbs(w.ConfigDir) {
			return fmt.Errorf("unbound workspace")
		}
		for _, a := range w.Agents {
			count++
			k := AgentKey(w, a)
			if seen[k] || a.Name == "" || a.ActorID == "" || a.Policy.MaxAttempts < 1 || a.Policy.MaxAttempts > 20 || a.Policy.Timeout <= 0 {
				return fmt.Errorf("invalid or duplicate bridge agent")
			}
			seen[k] = true
			for kind, enabled := range a.Jobs {
				switch kind {
				case "answers", "wake", "git", "spool":
				case "files":
					if enabled {
						return fmt.Errorf("file transport is not enabled in this release")
					}
				default:
					return fmt.Errorf("unknown bridge job")
				}
			}
			if len(a.RegistryDirs) > 16 {
				return fmt.Errorf("too many resume namespace roots")
			}
			if a.Jobs["wake"] && (a.Runtime == nil || len(a.Runtime.Argv) == 0 || !filepath.IsAbs(a.Runtime.CWD)) {
				return fmt.Errorf("wake job needs exact runtime argv/cwd")
			}
			for _, dir := range a.RegistryDirs {
				if !filepath.IsAbs(dir) {
					return fmt.Errorf("resume namespace must be absolute")
				}
			}
		}
	}
	if count > MaxAgents {
		return fmt.Errorf("bridge registration capacity exceeded")
	}
	return nil
}
func Update(ctx context.Context, root string, fn func(*Registry) error) error {
	if e := os.MkdirAll(root, 0700); e != nil {
		return e
	}
	unlock, e := integrations.Lock(ctx, filepath.Join(root, "registry.lock"))
	if e != nil {
		return e
	}
	defer unlock()
	r, e := Load(root)
	if e != nil {
		return e
	}
	if e = fn(&r); e != nil {
		return e
	}
	r.Revision++
	if e = r.Validate(); e != nil {
		return e
	}
	return integrations.PrivateWrite(filepath.Join(root, "registry.json"), integrations.JSON(r))
}
func Register(r *Registry, w Workspace, a Agent) error {
	for i := range r.Workspaces {
		v := &r.Workspaces[i]
		if v.BaseURL != w.BaseURL || v.HostID != w.HostID {
			continue
		}
		if v.ID != w.ID {
			return fmt.Errorf("workspace identity changed")
		}
		for j := range v.Agents {
			old := &v.Agents[j]
			if old.ActorID != a.ActorID {
				continue
			}
			if old.Name != a.Name {
				return fmt.Errorf("agent identity changed")
			}
			for _, dir := range old.RegistryDirs {
				found := false
				for _, d := range a.RegistryDirs {
					if d == dir {
						found = true
					}
				}
				if !found {
					a.RegistryDirs = append(a.RegistryDirs, dir)
				}
			}
			*old = a
			return nil
		}
		v.Agents = append(v.Agents, a)
		return nil
	}
	w.Agents = []Agent{a}
	r.Workspaces = append(r.Workspaces, w)
	return nil
}
