package app

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"

	"organization-autorunner-cli/internal/config"
	"organization-autorunner-cli/internal/errnorm"
	"organization-autorunner-cli/internal/httpclient"
	contractsclient "organization-autorunner-contracts-go-client/client"
)

func (a *App) runArtifactsCommand(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, string, error) {
	if len(args) == 0 {
		return nil, "artifacts", errnorm.Usage("subcommand_required", "expected one of: list, get, create, content")
	}
	sub := strings.TrimSpace(args[0])
	switch sub {
	case "list":
		fs := newSilentFlagSet("artifacts list")
		var kindFlag, threadIDFlag, beforeFlag, afterFlag trackedString
		fs.Var(&kindFlag, "kind", "Filter by artifact kind")
		fs.Var(&threadIDFlag, "thread-id", "Filter by thread id")
		fs.Var(&beforeFlag, "created-before", "Filter by created_at upper bound")
		fs.Var(&afterFlag, "created-after", "Filter by created_at lower bound")
		if err := fs.Parse(args[1:]); err != nil {
			return nil, "artifacts list", errnorm.Usage("invalid_flags", err.Error())
		}
		if len(fs.Args()) > 0 {
			return nil, "artifacts list", errnorm.Usage("invalid_args", "unexpected positional arguments for `oar artifacts list`")
		}
		query := make([]queryParam, 0, 4)
		addSingleQuery(&query, "kind", kindFlag.value)
		addSingleQuery(&query, "thread_id", threadIDFlag.value)
		addSingleQuery(&query, "created_before", beforeFlag.value)
		addSingleQuery(&query, "created_after", afterFlag.value)
		result, err := a.invokeTypedJSON(ctx, cfg, "artifacts list", "artifacts.list", nil, query, nil)
		return result, "artifacts list", err
	case "get":
		id, err := parseIDArg(args[1:], "artifact-id", "artifact id")
		if err != nil {
			return nil, "artifacts get", err
		}
		result, callErr := a.invokeTypedJSON(ctx, cfg, "artifacts get", "artifacts.get", map[string]string{"artifact_id": id}, nil, nil)
		return result, "artifacts get", callErr
	case "create":
		body, err := a.parseJSONBodyInput(args[1:], "artifacts create")
		if err != nil {
			return nil, "artifacts create", err
		}
		result, callErr := a.invokeTypedJSON(ctx, cfg, "artifacts create", "artifacts.create", nil, nil, body)
		return result, "artifacts create", callErr
	case "content":
		id, err := parseIDArg(args[1:], "artifact-id", "artifact id")
		if err != nil {
			return nil, "artifacts content", err
		}
		result, callErr := a.invokeArtifactContent(ctx, cfg, "artifacts content", map[string]string{"artifact_id": id})
		return result, "artifacts content", callErr
	default:
		return nil, "artifacts", errnorm.Usage("unknown_subcommand", fmt.Sprintf("unknown artifacts subcommand %q", sub))
	}
}

func (a *App) invokeArtifactContent(ctx context.Context, cfg config.Resolved, commandName string, pathParams map[string]string) (*commandResult, error) {
	authCfg, err := a.cfgWithResolvedAuthToken(ctx, cfg)
	if err != nil {
		return nil, err
	}
	client, err := httpclient.New(authCfg)
	if err != nil {
		return nil, errnorm.Wrap(errnorm.KindLocal, "http_client_init_failed", "failed to initialize HTTP client", err)
	}
	headers := generatedHeaders(authCfg)
	delete(headers, "Accept")
	headers["Accept"] = "application/octet-stream, text/plain, application/json"
	callCtx, cancel := httpclient.WithTimeout(ctx, authCfg.Timeout)
	defer cancel()
	resp, body, invokeErr := client.Generated().Invoke(callCtx, "artifacts.content.get", pathParams, contractsclient.RequestOptions{Headers: headers})
	if resp != nil && resp.StatusCode >= http.StatusBadRequest {
		return nil, errnorm.FromHTTPFailure(resp.StatusCode, body)
	}
	if invokeErr != nil {
		return nil, errnorm.Wrap(errnorm.KindNetwork, "request_failed", "artifact content request failed", invokeErr)
	}

	if !authCfg.JSON {
		if len(body) > 0 {
			if _, err := a.Stdout.Write(body); err != nil {
				return nil, errnorm.Wrap(errnorm.KindLocal, "stdout_write_failed", "failed to write artifact content", err)
			}
		}
		return &commandResult{RawWritten: true}, nil
	}

	data := map[string]any{
		"status_code": resp.StatusCode,
		"headers":     normalizedHeaders(resp.Header),
		"body_base64": base64.StdEncoding.EncodeToString(body),
	}
	if utf8Body := strings.TrimSpace(string(body)); utf8Body != "" {
		data["body_text"] = utf8Body
	}
	text := fmt.Sprintf("%s status: %d\nbytes: %d", commandName, resp.StatusCode, len(body))
	return &commandResult{Text: text, Data: data}, nil
}
