// anx-observe is an operator-owned, read-only source/JIT diagnostic runner.
// Register the same exact connections in core before using its reports there.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/netip"
	"os"
	"time"

	"agent-nexus-core/internal/observation"
)

type config struct {
	Target            observation.Target `json:"target"`
	Transport         string             `json:"transport,omitempty"`
	BaseURL           string             `json:"base_url,omitempty"`
	SourceWorkspaceID string             `json:"source_workspace_id,omitempty"`
	TokenEnv          string             `json:"token_env,omitempty"`
	AllowedNetworks   []string           `json:"allowed_networks,omitempty"`
	Binary            string             `json:"binary,omitempty"`
	Profile           string             `json:"profile,omitempty"`
	KnownHostsFile    string             `json:"known_hosts_file,omitempty"`
	IdentityFile      string             `json:"identity_file,omitempty"`
	Port              int                `json:"port,omitempty"`
	MaxPages          int                `json:"max_pages,omitempty"`
	MaxBytes          int64              `json:"max_bytes,omitempty"`
	TimeoutSeconds    int                `json:"timeout_seconds,omitempty"`
}

func load(path string, out any) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	d := json.NewDecoder(io.LimitReader(f, 2<<20))
	d.DisallowUnknownFields()
	if err = d.Decode(out); err != nil {
		return err
	}
	if err = d.Decode(new(any)); err != io.EOF {
		return errors.New("trailing configuration data")
	}
	return nil
}
func reader(c config) (observation.Reader, error) {
	timeout := time.Duration(c.TimeoutSeconds) * time.Second
	if c.Target.Source == "ssh_git" {
		return observation.NewSSHGitReader(observation.SSHConfig{WorkspaceID: c.Target.WorkspaceID, ConnectionID: c.Target.ConnectionID, Host: c.Target.Host, Repository: c.Target.Path, KnownHostsFile: c.KnownHostsFile, IdentityFile: c.IdentityFile, Port: c.Port, Timeout: timeout, MaxBytes: c.MaxBytes})
	}
	if c.Transport == "multica_cli" && c.Target.Source == "multica" {
		return observation.NewMulticaCLIReader(observation.MulticaCLIConfig{Binary: c.Binary, Profile: c.Profile, BaseURL: c.BaseURL, WorkspaceID: c.Target.WorkspaceID, ConnectionID: c.Target.ConnectionID, SourceWorkspaceID: c.SourceWorkspaceID, Timeout: timeout, MaxBytes: c.MaxBytes})
	}
	h := observation.HTTPConfig{BaseURL: c.BaseURL, WorkspaceID: c.Target.WorkspaceID, ConnectionID: c.Target.ConnectionID, SourceWorkspaceID: c.SourceWorkspaceID, Timeout: timeout, MaxBytes: c.MaxBytes, MaxPages: c.MaxPages}
	for _, network := range c.AllowedNetworks {
		p, err := netip.ParsePrefix(network)
		if err != nil {
			return nil, errors.New("invalid approved network")
		}
		h.AllowedNetworks = append(h.AllowedNetworks, p)
	}
	if c.TokenEnv != "" {
		// The operator can select only the dedicated observation credential slot.
		if c.TokenEnv != "ANX_OBSERVATION_SOURCE_TOKEN" {
			return nil, errors.New("only dedicated ANX_OBSERVATION_SOURCE_TOKEN is supported")
		}
		h.CredentialHandle = "source-read-only"
		h.ResolveCredential = func(ctx context.Context, handle string) (string, error) {
			if handle != "source-read-only" {
				return "", errors.New("unapproved handle")
			}
			return os.Getenv(c.TokenEnv), nil
		}
	}
	switch c.Target.Source {
	case "github":
		return observation.NewGitHubReader(h)
	case "multica":
		return observation.NewMulticaReader(h)
	}
	return nil, errors.New("unsupported configured source")
}
func emit(v any) error { e := json.NewEncoder(os.Stdout); e.SetIndent("", "  "); return e.Encode(v) }
func run() error {
	fs := flag.NewFlagSet("anx-observe", flag.ContinueOnError)
	configPath := fs.String("config", "", "operator-owned source JSON configuration")
	coreEnvelope := fs.Bool("core-envelope", false, "emit canonical observation envelope")
	stateRoot := fs.String("state-root", "", "private 0700 JIT runtime directory")
	policyPath := fs.String("policy", "", "approved JIT policy JSON")
	manifestPath := fs.String("manifest", "", "generated reader manifest JSON")
	artifactPath := fs.String("artifact", "", "host-native generated executable")
	sourcePath := fs.String("source", "", "C source to compile into a generated artifact")
	fixturesPath := fs.String("fixtures", "", "fixture array JSON: name,input (base64),want_valid")
	adapterID := fs.String("adapter", "", "registered adapter ID")
	revision := fs.String("revision", "", "immutable generated reader revision")
	if err := fs.Parse(os.Args[1:]); err != nil {
		return err
	}
	action := fs.Arg(0)
	if action == "" {
		fs.Usage()
		return errors.New("action required: read, capabilities, jit-stage, jit-generate, jit-validate, jit-canary, jit-activate, jit-read, jit-status, jit-suspend, jit-rollback")
	}
	var c config
	var source observation.Reader
	if *configPath != "" {
		if err := load(*configPath, &c); err != nil {
			return err
		}
		var err error
		source, err = reader(c)
		if err != nil {
			return err
		}
	}
	if action == "capabilities" {
		if source == nil {
			return errors.New("--config required")
		}
		return emit(source.Capabilities())
	}
	if action == "read" {
		if source == nil {
			return errors.New("--config required")
		}
		report, err := source.Read(context.Background(), c.Target)
		if err != nil {
			return err
		}
		if *coreEnvelope {
			return emit(report.Observation(15 * time.Minute))
		}
		return emit(report)
	}
	if *stateRoot == "" || *policyPath == "" {
		return errors.New("JIT actions require --state-root and --policy")
	}
	var policy observation.JITPolicy
	if err := load(*policyPath, &policy); err != nil {
		return err
	}
	manager, err := observation.NewJITManager(*stateRoot, policy)
	if err != nil {
		return err
	}
	switch action {
	case "jit-generate":
		var manifest observation.Manifest
		if err := load(*manifestPath, &manifest); err != nil {
			return err
		}
		srcPath := *sourcePath
		if srcPath == "" {
			return errors.New("jit-generate requires --source C file (harness output compiled in-process by tests)")
		}
		src, err := os.ReadFile(srcPath)
		if err != nil {
			return err
		}
		dir, err := os.MkdirTemp(*stateRoot, ".generate-")
		if err != nil {
			return err
		}
		defer os.RemoveAll(dir)
		if err = observation.GenerateWorkspace(observation.GenerateRequest{Workspace: dir, Manifest: manifest}); err != nil {
			return err
		}
		compiled, err := observation.CompileGeneratedC(dir, src)
		if err != nil {
			return err
		}
		b, err := os.ReadFile(compiled)
		if err != nil {
			return err
		}
		v, err := manager.Stage(manifest, b)
		if err != nil {
			return err
		}
		return emit(v)
	case "jit-stage":
		var manifest observation.Manifest
		if err := load(*manifestPath, &manifest); err != nil {
			return err
		}
		f, err := os.Open(*artifactPath)
		if err != nil {
			return err
		}
		defer f.Close()
		b, err := io.ReadAll(io.LimitReader(f, policy.MaxArtifactBytes+1))
		if err != nil {
			return err
		}
		v, err := manager.Stage(manifest, b)
		if err != nil {
			return err
		}
		return emit(v)
	case "jit-validate":
		var cases []observation.ValidationCase
		if err := load(*fixturesPath, &cases); err != nil {
			return err
		}
		if err := manager.Validate(context.Background(), *adapterID, *revision, cases); err != nil {
			return err
		}
	case "jit-canary":
		if source == nil {
			return errors.New("--config required for real-source canary")
		}
		r, err := manager.Canary(context.Background(), *adapterID, *revision, source)
		if err != nil {
			return err
		}
		return emit(r)
	case "jit-activate":
		if err := manager.Activate(*adapterID, *revision); err != nil {
			return err
		}
	case "jit-read":
		if source == nil {
			return errors.New("--config required")
		}
		r, err := manager.Read(context.Background(), *adapterID, source)
		if err != nil {
			return err
		}
		return emit(r)
	case "jit-suspend":
		if err := manager.Suspend(*adapterID, "operator request"); err != nil {
			return err
		}
	case "jit-rollback":
		if err := manager.Rollback(*adapterID); err != nil {
			return err
		}
	case "jit-status":
	default:
		return errors.New("unsupported action")
	}
	state, err := manager.Status(*adapterID)
	if err != nil {
		return err
	}
	return emit(state)
}
func main() {
	if err := run(); err != nil {
		var re *observation.ReadError
		if errors.As(err, &re) {
			_ = json.NewEncoder(os.Stderr).Encode(re)
		} else {
			fmt.Fprintln(os.Stderr, err)
		}
		os.Exit(1)
	}
}
