package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/errnorm"
	"agent-nexus-cli/internal/workspaceconfig"
)

// Only workspace-independent commands may run while selection is ambiguous.
// In particular, config show and doctor must expose selection failures.
func workspaceIndependentCommand(args []string) bool {
	if len(args) == 0 || hasHelpToken(args) {
		return true
	}
	switch args[0] {
	case "help", "version", "update", "install", "skills", "concepts", "primitives":
		return true
	case "config":
		return len(args) > 1 && args[1] != "show"
	case "host":
		return len(args) > 1 && args[1] == "discover"
	case "debug":
		if len(args) < 2 || args[1] != "meta" {
			return false
		}
		if len(args) == 2 {
			return true
		} // Metadata group help.
		switch metaSubcommandSpec.normalize(args[2]) {
		case "commands", "command", "concepts", "concept", "docs", "doc", "skill":
			return true
		}
		return false
	case "import":
		return isConfigLenientImportCommand(args[1:])
	case "bridge":
		return len(args) > 1 && args[1] != "doctor"
	}
	return isWorkCommandGroup(strings.Join(args, " "))
}

func (a *App) workspaceHome() string {
	home, _ := a.UserHomeDir()
	return home
}

func (a *App) workspaceCWD() (string, error) {
	if a.Getwd != nil {
		return a.Getwd()
	}
	return os.Getwd()
}

func (a *App) workspaceCatalog(cfg config.Resolved) (workspaceconfig.Catalog, string, error) {
	dir, err := a.configDir(cfg)
	// UserHomeDir is injectable and also permits offline orientation when HOME
	// isn't exported. Host authentication still requires an explicit config dir.
	if err != nil && cfg.ConfigDir == "" {
		if home := a.workspaceHome(); home != "" {
			dir = filepath.Join(home, ".config", "anx")
			err = nil
		}
	}
	if err != nil {
		return workspaceconfig.Catalog{}, "", err
	}
	catalog, err := workspaceconfig.Load(dir)
	return catalog, dir, err
}

func (a *App) resolveWorkspace(cfg config.Resolved, alias *string) (config.Resolved, error) {
	if alias == nil && cfg.Sources["base_url"] != "default" {
		return cfg, nil
	}
	catalog, _, err := a.workspaceCatalog(cfg)
	if err != nil {
		return cfg, errnorm.Wrap(errnorm.KindLocal, "workspace_config_invalid", "cannot read workspace config: "+err.Error(), err)
	}
	if alias != nil {
		base, ok := catalog.File.Aliases[*alias]
		if !ok {
			return cfg, errnorm.Usage("workspace_unknown", "unknown workspace alias "+*alias+"; run anx config workspaces")
		}
		cfg.BaseURL = base
		cfg.Sources["base_url"] = "flag:--workspace"
		return cfg, nil
	}
	cwd, err := a.workspaceCWD()
	if err != nil {
		return cfg, err
	}
	base, source, err := catalog.Resolve(cwd, a.workspaceHome())
	if err != nil {
		var ambiguous *workspaceconfig.AmbiguousError
		if errors.As(err, &ambiguous) {
			cfg.BaseURL = ""
			cfg.Sources["base_url"] = "unresolved:ambiguous"
			message := err.Error()
			details := map[string]any{"workspaces": ambiguous.Workspaces}
			if cfg.ConfigDir != "" {
				details["config_dir"] = cfg.ConfigDir
				quoted := "'" + strings.ReplaceAll(cfg.ConfigDir, "'", "'\\''") + "'"
				message = strings.ReplaceAll(message, "anx config", "anx --config-dir "+quoted+" config")
			}
			return cfg, errnorm.WithDetails(errnorm.Usage("workspace_ambiguous", message), details)
		}
		return cfg, errnorm.Wrap(errnorm.KindLocal, "workspace_config_invalid", "cannot resolve workspace: "+err.Error(), err)
	}
	if base != "" {
		cfg.BaseURL = base
	}
	cfg.Sources["base_url"] = source
	return cfg, nil
}
