package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	contracts "agent-nexus-contracts-go-client/client"

	"github.com/Git-on-my-level/agent-nexus/mcp/catalog"
	"github.com/Git-on-my-level/agent-nexus/mcp/executor"
	mcpprofile "github.com/Git-on-my-level/agent-nexus/mcp/internal/profile"
	"github.com/Git-on-my-level/agent-nexus/mcp/internal/stdio"
	"github.com/Git-on-my-level/agent-nexus/mcp/policy"
	"github.com/Git-on-my-level/agent-nexus/mcp/protocol"
)

func main() {
	if err := run(context.Background(), os.Args[1:], os.Stdin, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "anx-mcp: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, stdin *os.File, stdout *os.File, stderr *os.File) error {
	fs := flag.NewFlagSet("anx-mcp", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var (
		profileName = fs.String("profile", "", "ANX profile name or profile JSON path")
		agent       = fs.String("agent", "", "ANX profile/agent name")
		baseURL     = fs.String("base-url", "", "workspace core base URL")
		logLevel    = fs.String("log-level", "warn", "log level: debug, info, warn, error")
		timeout     = fs.Duration("timeout", mcpprofile.DefaultTimeout, "workspace request timeout")
	)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}

	logger := log.New(stderr, "", log.LstdFlags)
	if !logsEnabled(*logLevel) {
		logger.SetOutput(os.Stderr)
		logger.SetFlags(0)
		logger = log.New(ioDiscard{}, "", 0)
	}

	resolved, err := mcpprofile.Resolve(mcpprofile.Options{
		Profile: *profileName,
		Agent:   *agent,
		BaseURL: *baseURL,
		Timeout: *timeout,
	}, mcpprofile.Environment{})
	if err != nil {
		return err
	}
	if strings.TrimSpace(resolved.AccessToken) == "" {
		logger.Printf("starting without a bearer token; workspace authorization may fail")
	}
	logger.Printf("starting stdio server profile=%s base_url=%s", resolved.Agent, resolved.BaseURL)

	cat, err := defaultCatalog()
	if err != nil {
		return err
	}
	exec := executor.NewWorkspaceExecutor(resolved.BaseURL, executor.Options{
		HTTPClient: &http.Client{Timeout: resolved.Timeout},
		Auth: executor.AuthContext{
			BearerToken: resolved.AccessToken,
		},
		RequestTimeout: resolved.Timeout,
		AdditionalHeaders: map[string]string{
			"X-ANX-Agent":       resolved.Agent,
			"X-ANX-MCP-Client":  "anx-mcp",
			"X-ANX-MCP-Profile": resolved.Agent,
		},
	})
	server := protocol.NewServer(cat, exec, protocol.Options{Name: "anx-mcp", Version: "0.1.0"})
	return stdio.Serve(ctx, server, stdin, stdout, stdio.Options{Logger: logger})
}

func defaultCatalog() (*catalog.Catalog, error) {
	registry := catalog.CommandRegistry{Commands: make([]catalog.Command, 0, len(contracts.CommandRegistry))}
	for _, cmd := range contracts.CommandRegistry {
		registry.Commands = append(registry.Commands, catalog.Command{
			CommandID:  cmd.CommandID,
			CLIPath:    cmd.CLIPath,
			Group:      cmd.Group,
			Method:     cmd.Method,
			Path:       cmd.Path,
			InputMode:  cmd.InputMode,
			PathParams: cmd.PathParams,
		})
	}
	policy, err := catalog.LoadPolicy(strings.NewReader(policy.DefaultToolPolicyYAML))
	if err != nil {
		return nil, fmt.Errorf("load default MCP policy: %w", err)
	}
	cat, err := catalog.Build(registry, policy, catalog.BuildOptions{})
	if err != nil {
		return nil, fmt.Errorf("build default MCP catalog: %w", err)
	}
	return cat, nil
}

func logsEnabled(level string) bool {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug", "info":
		return true
	case "", "warn", "warning", "error":
		return false
	default:
		return false
	}
}

type ioDiscard struct{}

func (ioDiscard) Write(p []byte) (int, error) { return len(p), nil }
