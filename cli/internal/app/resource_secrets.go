package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"strings"

	"organization-autorunner-cli/internal/config"
	"organization-autorunner-cli/internal/errnorm"
	"organization-autorunner-cli/internal/httpclient"
)

var secretSubcommandSpec = subcommandSpec{
	command: "secret",
	valid:   []string{"list", "create", "get", "delete", "exec"},
	examples: []string{
		"oar secret list",
		"oar secret create OPENAI_API_KEY",
		"oar secret get OPENAI_API_KEY --reveal",
		"oar secret delete OPENAI_API_KEY",
		"oar secret exec --secret OPENAI_API_KEY -- ./my-agent.sh",
	},
	aliases: map[string]string{
		"ls": "list",
		"rm": "delete",
	},
}

func (a *App) runSecretCommand(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, string, error) {
	if len(args) == 0 || isHelpToken(args[0]) {
		return nil, "secret", secretSubcommandSpec.requiredError()
	}
	sub := secretSubcommandSpec.normalize(args[0])
	switch sub {
	case "list":
		return a.runSecretList(ctx, cfg)
	case "create":
		return a.runSecretCreate(ctx, args[1:], cfg)
	case "get":
		return a.runSecretGet(ctx, args[1:], cfg)
	case "delete":
		return a.runSecretDelete(ctx, args[1:], cfg)
	case "exec":
		return a.runSecretExec(ctx, args[1:], cfg)
	default:
		return nil, "secret", secretSubcommandSpec.unknownError(args[0])
	}
}

func (a *App) runSecretList(ctx context.Context, cfg config.Resolved) (*commandResult, string, error) {
	authCfg, err := a.cfgWithResolvedAuthToken(ctx, cfg)
	if err != nil {
		return nil, "secret list", err
	}
	client, err := httpclient.New(authCfg)
	if err != nil {
		return nil, "secret list", errnorm.Wrap(errnorm.KindLocal, "http_client_init_failed", "failed to initialize HTTP client", err)
	}

	callCtx, cancel := httpclient.WithTimeout(ctx, authCfg.Timeout)
	defer cancel()

	resp, err := client.RawCall(callCtx, httpclient.RawRequest{
		Method:  "GET",
		Path:    "/secrets",
		Headers: generatedHeaders(authCfg),
	})
	if err != nil {
		return nil, "secret list", errnorm.Wrap(errnorm.KindNetwork, "request_failed", "secret list request failed", err)
	}
	if resp.StatusCode >= 400 {
		return nil, "secret list", errnorm.FromHTTPFailure(resp.StatusCode, resp.Body)
	}

	parsed := parseResponseBody(resp.Body)
	text := formatSecretListText(parsed)
	return &commandResult{Text: text, Data: map[string]any{"status_code": resp.StatusCode, "body": parsed}}, "secret list", nil
}

func (a *App) runSecretCreate(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, string, error) {
	fs := newSilentFlagSet("secret create")
	var fromStdin trackedBool
	var descFlag trackedString
	fs.Var(&fromStdin, "from-stdin", "Read secret value from stdin")
	fs.Var(&descFlag, "description", "Optional description for the secret")
	if err := fs.Parse(args); err != nil {
		return nil, "secret create", errnorm.Usage("invalid_flags", err.Error())
	}
	positionals := fs.Args()
	if len(positionals) == 0 {
		return nil, "secret create", errnorm.Usage("invalid_args", "secret name is required: oar secret create <NAME>")
	}
	name := strings.TrimSpace(positionals[0])
	if name == "" {
		return nil, "secret create", errnorm.Usage("invalid_args", "secret name must not be empty")
	}
	if len(positionals) > 1 {
		return nil, "secret create", errnorm.Usage("invalid_args", "too many positional arguments")
	}

	var value string
	if fromStdin.set && fromStdin.value {
		data, err := io.ReadAll(a.Stdin)
		if err != nil {
			return nil, "secret create", errnorm.Wrap(errnorm.KindLocal, "stdin_read_failed", "failed to read from stdin", err)
		}
		value = strings.TrimRight(string(data), "\n\r")
	} else {
		fmt.Fprint(a.Stderr, "Enter secret value: ")
		data, err := io.ReadAll(a.Stdin)
		if err != nil {
			return nil, "secret create", errnorm.Wrap(errnorm.KindLocal, "stdin_read_failed", "failed to read value", err)
		}
		value = strings.TrimRight(string(data), "\n\r")
	}
	if value == "" {
		return nil, "secret create", errnorm.Usage("invalid_request", "secret value must not be empty")
	}

	body := map[string]any{"name": name, "value": value}
	if descFlag.set && strings.TrimSpace(descFlag.value) != "" {
		body["description"] = strings.TrimSpace(descFlag.value)
	}

	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return nil, "secret create", errnorm.Wrap(errnorm.KindLocal, "request_body_encode_failed", "failed to encode request body", err)
	}

	authCfg, err := a.cfgWithResolvedAuthToken(ctx, cfg)
	if err != nil {
		return nil, "secret create", err
	}
	client, err := httpclient.New(authCfg)
	if err != nil {
		return nil, "secret create", errnorm.Wrap(errnorm.KindLocal, "http_client_init_failed", "failed to initialize HTTP client", err)
	}

	callCtx, cancel := httpclient.WithTimeout(ctx, authCfg.Timeout)
	defer cancel()

	resp, err := client.RawCall(callCtx, httpclient.RawRequest{
		Method:  "POST",
		Path:    "/secrets",
		Headers: generatedHeaders(authCfg),
		Body:    bodyBytes,
	})
	if err != nil {
		return nil, "secret create", errnorm.Wrap(errnorm.KindNetwork, "request_failed", "secret create request failed", err)
	}
	if resp.StatusCode >= 400 {
		return nil, "secret create", errnorm.FromHTTPFailure(resp.StatusCode, resp.Body)
	}

	parsed := parseResponseBody(resp.Body)
	text := fmt.Sprintf("Secret %q created.", name)
	return &commandResult{Text: text, Data: map[string]any{"status_code": resp.StatusCode, "body": parsed}}, "secret create", nil
}

func (a *App) runSecretGet(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, string, error) {
	fs := newSilentFlagSet("secret get")
	var revealFlag trackedBool
	fs.Var(&revealFlag, "reveal", "Show the decrypted secret value")
	if err := fs.Parse(args); err != nil {
		return nil, "secret get", errnorm.Usage("invalid_flags", err.Error())
	}
	positionals := fs.Args()
	if len(positionals) == 0 {
		return nil, "secret get", errnorm.Usage("invalid_args", "secret name or ID is required: oar secret get <NAME_OR_ID>")
	}
	nameOrID := strings.TrimSpace(positionals[0])
	if len(positionals) > 1 {
		return nil, "secret get", errnorm.Usage("invalid_args", "too many positional arguments")
	}

	authCfg, err := a.cfgWithResolvedAuthToken(ctx, cfg)
	if err != nil {
		return nil, "secret get", err
	}
	client, err := httpclient.New(authCfg)
	if err != nil {
		return nil, "secret get", errnorm.Wrap(errnorm.KindLocal, "http_client_init_failed", "failed to initialize HTTP client", err)
	}

	if !revealFlag.set || !revealFlag.value {
		callCtx, cancel := httpclient.WithTimeout(ctx, authCfg.Timeout)
		defer cancel()
		resp, err := client.RawCall(callCtx, httpclient.RawRequest{
			Method:  "GET",
			Path:    "/secrets",
			Headers: generatedHeaders(authCfg),
		})
		if err != nil {
			return nil, "secret get", errnorm.Wrap(errnorm.KindNetwork, "request_failed", "request failed", err)
		}
		if resp.StatusCode >= 400 {
			return nil, "secret get", errnorm.FromHTTPFailure(resp.StatusCode, resp.Body)
		}
		parsed := parseResponseBody(resp.Body)
		secretMeta := findSecretByNameOrID(parsed, nameOrID)
		if secretMeta == nil {
			return nil, "secret get", errnorm.Wrap(errnorm.KindRemote, "not_found", fmt.Sprintf("secret %q not found", nameOrID), nil)
		}
		text := formatSecretMetadataText(secretMeta)
		return &commandResult{Text: text, Data: map[string]any{"status_code": resp.StatusCode, "body": map[string]any{"secret": secretMeta}}}, "secret get", nil
	}

	secretID, resolveErr := a.resolveSecretID(ctx, client, authCfg, nameOrID)
	if resolveErr != nil {
		return nil, "secret get", resolveErr
	}

	callCtx, cancel := httpclient.WithTimeout(ctx, authCfg.Timeout)
	defer cancel()
	resp, err := client.RawCall(callCtx, httpclient.RawRequest{
		Method:  "POST",
		Path:    fmt.Sprintf("/secrets/%s/reveal", url.PathEscape(secretID)),
		Headers: generatedHeaders(authCfg),
	})
	if err != nil {
		return nil, "secret get", errnorm.Wrap(errnorm.KindNetwork, "request_failed", "reveal request failed", err)
	}
	if resp.StatusCode >= 400 {
		return nil, "secret get", errnorm.FromHTTPFailure(resp.StatusCode, resp.Body)
	}
	parsed := parseResponseBody(resp.Body)
	value := ""
	if m, ok := parsed.(map[string]any); ok {
		if v, ok := m["value"].(string); ok {
			value = v
		}
	}
	return &commandResult{Text: value, Data: map[string]any{"status_code": resp.StatusCode, "body": parsed}}, "secret get", nil
}

func (a *App) runSecretDelete(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, string, error) {
	if len(args) == 0 {
		return nil, "secret delete", errnorm.Usage("invalid_args", "secret name or ID is required: oar secret delete <NAME_OR_ID>")
	}
	nameOrID := strings.TrimSpace(args[0])
	if len(args) > 1 {
		return nil, "secret delete", errnorm.Usage("invalid_args", "too many positional arguments")
	}

	authCfg, err := a.cfgWithResolvedAuthToken(ctx, cfg)
	if err != nil {
		return nil, "secret delete", err
	}
	client, err := httpclient.New(authCfg)
	if err != nil {
		return nil, "secret delete", errnorm.Wrap(errnorm.KindLocal, "http_client_init_failed", "failed to initialize HTTP client", err)
	}

	secretID, resolveErr := a.resolveSecretID(ctx, client, authCfg, nameOrID)
	if resolveErr != nil {
		return nil, "secret delete", resolveErr
	}

	callCtx, cancel := httpclient.WithTimeout(ctx, authCfg.Timeout)
	defer cancel()
	resp, err := client.RawCall(callCtx, httpclient.RawRequest{
		Method:  "DELETE",
		Path:    fmt.Sprintf("/secrets/%s", url.PathEscape(secretID)),
		Headers: generatedHeaders(authCfg),
	})
	if err != nil {
		return nil, "secret delete", errnorm.Wrap(errnorm.KindNetwork, "request_failed", "delete request failed", err)
	}
	if resp.StatusCode >= 400 {
		return nil, "secret delete", errnorm.FromHTTPFailure(resp.StatusCode, resp.Body)
	}
	parsed := parseResponseBody(resp.Body)
	text := fmt.Sprintf("Secret %q deleted.", nameOrID)
	return &commandResult{Text: text, Data: map[string]any{"status_code": resp.StatusCode, "body": parsed}}, "secret delete", nil
}

func (a *App) runSecretExec(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, string, error) {
	var secretNames []string
	var cmdArgs []string
	foundSeparator := false
	for i := 0; i < len(args); i++ {
		if args[i] == "--" {
			cmdArgs = args[i+1:]
			foundSeparator = true
			break
		}
		if args[i] == "--secret" || args[i] == "-secret" {
			if i+1 >= len(args) {
				return nil, "secret exec", errnorm.Usage("invalid_flags", "--secret requires a value")
			}
			secretNames = append(secretNames, strings.TrimSpace(args[i+1]))
			i++
			continue
		}
		if strings.HasPrefix(args[i], "--secret=") {
			secretNames = append(secretNames, strings.TrimSpace(strings.TrimPrefix(args[i], "--secret=")))
			continue
		}
	}

	if !foundSeparator {
		return nil, "secret exec", errnorm.Usage("invalid_args", "usage: oar secret exec --secret NAME [--secret NAME2] -- <command> [args...]")
	}
	if len(secretNames) == 0 {
		return nil, "secret exec", errnorm.Usage("invalid_args", "at least one --secret NAME is required")
	}
	if len(cmdArgs) == 0 {
		return nil, "secret exec", errnorm.Usage("invalid_args", "command is required after '--'")
	}

	authCfg, err := a.cfgWithResolvedAuthToken(ctx, cfg)
	if err != nil {
		return nil, "secret exec", err
	}
	client, err := httpclient.New(authCfg)
	if err != nil {
		return nil, "secret exec", errnorm.Wrap(errnorm.KindLocal, "http_client_init_failed", "failed to initialize HTTP client", err)
	}

	batchBody, err := json.Marshal(map[string]any{"names": secretNames})
	if err != nil {
		return nil, "secret exec", errnorm.Wrap(errnorm.KindLocal, "request_body_encode_failed", "failed to encode request body", err)
	}

	callCtx, cancel := httpclient.WithTimeout(ctx, authCfg.Timeout)
	defer cancel()
	resp, err := client.RawCall(callCtx, httpclient.RawRequest{
		Method:  "POST",
		Path:    "/secrets/reveal-batch",
		Headers: generatedHeaders(authCfg),
		Body:    batchBody,
	})
	if err != nil {
		return nil, "secret exec", errnorm.Wrap(errnorm.KindNetwork, "request_failed", "batch reveal request failed", err)
	}
	if resp.StatusCode >= 400 {
		return nil, "secret exec", errnorm.FromHTTPFailure(resp.StatusCode, resp.Body)
	}

	parsed := parseResponseBody(resp.Body)
	envPairs := extractSecretEnvPairs(parsed)
	if len(envPairs) == 0 {
		return nil, "secret exec", errnorm.Wrap(errnorm.KindRemote, "no_secrets", "no secrets resolved", nil)
	}

	childEnv := os.Environ()
	for k, v := range envPairs {
		childEnv = append(childEnv, fmt.Sprintf("%s=%s", k, v))
	}

	cmd := exec.CommandContext(ctx, cmdArgs[0], cmdArgs[1:]...)
	cmd.Env = childEnv
	cmd.Stdin = a.Stdin
	cmd.Stdout = a.Stdout
	cmd.Stderr = a.Stderr

	execErr := cmd.Run()
	if execErr != nil {
		if exitErr, ok := execErr.(*exec.ExitError); ok {
			return &commandResult{RawWritten: true}, "secret exec", errnorm.Wrap(errnorm.KindLocal, "child_exit", fmt.Sprintf("child process exited with code %d", exitErr.ExitCode()), execErr)
		}
		return nil, "secret exec", errnorm.Wrap(errnorm.KindLocal, "exec_failed", "failed to run child process", execErr)
	}

	return &commandResult{RawWritten: true}, "secret exec", nil
}

func (a *App) resolveSecretID(ctx context.Context, client *httpclient.Client, cfg config.Resolved, nameOrID string) (string, error) {
	if strings.HasPrefix(nameOrID, "sec_") {
		return nameOrID, nil
	}

	callCtx, cancel := httpclient.WithTimeout(ctx, cfg.Timeout)
	defer cancel()
	resp, err := client.RawCall(callCtx, httpclient.RawRequest{
		Method:  "GET",
		Path:    "/secrets",
		Headers: generatedHeaders(cfg),
	})
	if err != nil {
		return "", errnorm.Wrap(errnorm.KindNetwork, "request_failed", "failed to list secrets for name resolution", err)
	}
	if resp.StatusCode >= 400 {
		return "", errnorm.FromHTTPFailure(resp.StatusCode, resp.Body)
	}

	parsed := parseResponseBody(resp.Body)
	secret := findSecretByNameOrID(parsed, nameOrID)
	if secret == nil {
		return "", errnorm.Wrap(errnorm.KindRemote, "not_found", fmt.Sprintf("secret %q not found", nameOrID), nil)
	}
	if id, ok := secret["id"].(string); ok {
		return id, nil
	}
	return "", errnorm.Wrap(errnorm.KindRemote, "not_found", fmt.Sprintf("secret %q has no id", nameOrID), nil)
}

func findSecretByNameOrID(body any, nameOrID string) map[string]any {
	m, ok := body.(map[string]any)
	if !ok {
		return nil
	}
	secrets, ok := m["secrets"].([]any)
	if !ok {
		return nil
	}
	for _, s := range secrets {
		sm, ok := s.(map[string]any)
		if !ok {
			continue
		}
		if name, ok := sm["name"].(string); ok && name == nameOrID {
			return sm
		}
		if id, ok := sm["id"].(string); ok && id == nameOrID {
			return sm
		}
	}
	return nil
}

func formatSecretListText(body any) string {
	m, ok := body.(map[string]any)
	if !ok {
		return "No secrets."
	}
	secrets, ok := m["secrets"].([]any)
	if !ok || len(secrets) == 0 {
		return "No secrets."
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("Secrets (%d):\n", len(secrets)))
	for _, s := range secrets {
		sm, ok := s.(map[string]any)
		if !ok {
			continue
		}
		name, _ := sm["name"].(string)
		desc, _ := sm["description"].(string)
		updatedAt, _ := sm["updated_at"].(string)
		line := fmt.Sprintf("  %-30s", name)
		if desc != "" {
			line += "  " + desc
		}
		if updatedAt != "" {
			line += "  (updated " + updatedAt + ")"
		}
		b.WriteString(line + "\n")
	}
	return b.String()
}

func formatSecretMetadataText(secret map[string]any) string {
	name, _ := secret["name"].(string)
	id, _ := secret["id"].(string)
	desc, _ := secret["description"].(string)
	createdAt, _ := secret["created_at"].(string)
	updatedAt, _ := secret["updated_at"].(string)

	var b strings.Builder
	b.WriteString(fmt.Sprintf("Name:        %s\n", name))
	b.WriteString(fmt.Sprintf("ID:          %s\n", id))
	if desc != "" {
		b.WriteString(fmt.Sprintf("Description: %s\n", desc))
	}
	b.WriteString(fmt.Sprintf("Created:     %s\n", createdAt))
	b.WriteString(fmt.Sprintf("Updated:     %s\n", updatedAt))
	b.WriteString("Value:       ********** (use --reveal to show)")
	return b.String()
}

func extractSecretEnvPairs(body any) map[string]string {
	m, ok := body.(map[string]any)
	if !ok {
		return nil
	}
	secrets, ok := m["secrets"].([]any)
	if !ok {
		return nil
	}
	result := make(map[string]string, len(secrets))
	for _, s := range secrets {
		sm, ok := s.(map[string]any)
		if !ok {
			continue
		}
		name, _ := sm["name"].(string)
		value, _ := sm["value"].(string)
		if name != "" {
			result[name] = value
		}
	}
	return result
}
