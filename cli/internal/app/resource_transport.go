package app

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"

	contractsclient "organization-autorunner-contracts-go-client/client"

	"organization-autorunner-cli/internal/authcli"
	"organization-autorunner-cli/internal/config"
	"organization-autorunner-cli/internal/errnorm"
	"organization-autorunner-cli/internal/httpclient"
)

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
	path := "/artifacts/" + url.PathEscape(strings.TrimSpace(pathParams["artifact_id"])) + "/content"
	resp, invokeErr := client.RawCall(callCtx, httpclient.RawRequest{Method: http.MethodGet, Path: path, Headers: headers})
	if invokeErr != nil {
		return nil, errnorm.Wrap(errnorm.KindNetwork, "request_failed", "artifact content request failed", invokeErr)
	}
	body := resp.Body
	if resp.StatusCode >= http.StatusBadRequest {
		return nil, errnorm.FromHTTPFailure(resp.StatusCode, body)
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
		"headers":     normalizedHeaders(resp.Headers),
		"body_base64": base64.StdEncoding.EncodeToString(body),
	}
	if utf8Body := strings.TrimSpace(string(body)); utf8Body != "" {
		data["body_text"] = utf8Body
	}
	if authCfg.Headers || authCfg.Verbose {
		text := formatArtifactContentText(resp.StatusCode, normalizedHeaders(resp.Headers), body, authCfg.Verbose, authCfg.Headers)
		return &commandResult{Text: text, Data: data}, nil
	}
	text := fmt.Sprintf("%s status: %d\nbytes: %d", commandName, resp.StatusCode, len(body))
	return &commandResult{Text: text, Data: data}, nil
}

func (a *App) invokeRawJSON(ctx context.Context, cfg config.Resolved, commandName string, method string, path string, body any) (*commandResult, error) {
	authCfg, err := a.cfgWithResolvedAuthToken(ctx, cfg)
	if err != nil {
		return nil, err
	}
	client, err := httpclient.New(authCfg)
	if err != nil {
		return nil, errnorm.Wrap(errnorm.KindLocal, "http_client_init_failed", "failed to initialize HTTP client", err)
	}
	var requestBody []byte
	if body != nil {
		requestBody, err = json.Marshal(body)
		if err != nil {
			return nil, errnorm.Wrap(errnorm.KindLocal, "request_body_encode_failed", "failed to encode request body", err)
		}
	}
	callCtx, cancel := httpclient.WithTimeout(ctx, authCfg.Timeout)
	defer cancel()
	resp, invokeErr := client.RawCall(callCtx, httpclient.RawRequest{
		Method:  method,
		Path:    path,
		Headers: generatedHeaders(authCfg),
		Body:    requestBody,
	})
	if invokeErr != nil {
		return nil, errnorm.Wrap(errnorm.KindNetwork, "request_failed", fmt.Sprintf("%s request failed", commandName), invokeErr)
	}
	responseBody := resp.Body
	if resp.StatusCode >= http.StatusBadRequest {
		return nil, errnorm.FromHTTPFailure(resp.StatusCode, responseBody)
	}
	headersSorted := normalizedHeaders(resp.Headers)
	parsedBody := parseResponseBody(responseBody)
	data := map[string]any{
		"status_code": resp.StatusCode,
		"headers":     headersSorted,
		"body":        parsedBody,
	}
	text := formatTypedCommandText(resolveMachineCommandIdentity(commandName).CommandID, resp.StatusCode, headersSorted, parsedBody, authCfg.Verbose, authCfg.Headers)
	return &commandResult{Text: text, Data: data}, nil
}

func (a *App) invokeTypedJSON(ctx context.Context, cfg config.Resolved, commandName string, commandID string, pathParams map[string]string, query []queryParam, body any) (*commandResult, error) {
	if body != nil {
		normalizedBody, err := a.normalizeMutationBodyIDs(ctx, cfg, commandID, pathParams, body)
		if err != nil {
			return nil, err
		}
		body = normalizedBody
	}

	authCfg, err := a.cfgWithResolvedAuthToken(ctx, cfg)
	if err != nil {
		return nil, err
	}
	client, err := httpclient.New(authCfg)
	if err != nil {
		return nil, errnorm.Wrap(errnorm.KindLocal, "http_client_init_failed", "failed to initialize HTTP client", err)
	}

	queryValues := queryValuesFromParams(query)

	callCtx, cancel := httpclient.WithTimeout(ctx, authCfg.Timeout)
	defer cancel()
	resp, responseBody, invokeErr := client.Generated().Invoke(callCtx, commandID, pathParams, contractsclient.RequestOptions{
		Query:   queryValues,
		Headers: generatedHeaders(authCfg),
		Body:    body,
	})
	if resp != nil && resp.StatusCode >= http.StatusBadRequest {
		return nil, errnorm.FromHTTPFailure(resp.StatusCode, responseBody)
	}
	if invokeErr != nil {
		return nil, errnorm.Wrap(errnorm.KindNetwork, "request_failed", fmt.Sprintf("%s request failed", commandName), invokeErr)
	}

	headersSorted := normalizedHeaders(resp.Header)
	parsedBody := parseResponseBody(responseBody)
	parsedBody, enriched := enrichListBodyWithShortIDs(commandID, parsedBody)
	if enriched {
		if encoded, marshalErr := json.Marshal(parsedBody); marshalErr == nil {
			responseBody = encoded
		}
	}
	data := map[string]any{
		"status_code": resp.StatusCode,
		"headers":     headersSorted,
		"body":        parsedBody,
	}
	text := formatTypedCommandText(commandID, resp.StatusCode, headersSorted, parsedBody, authCfg.Verbose, authCfg.Headers)
	return &commandResult{Text: text, Data: data}, nil
}

func validationResult(commandName string, commandID string, pathParams map[string]string, query []queryParam, body any) *commandResult {
	queryValues := queryValuesFromParams(query)
	method := strings.ToUpper(resolveCommandMethod(commandID))
	path := resolveCommandPath(commandID, pathParams, queryValues)

	data := map[string]any{
		"validated":  true,
		"command_id": commandID,
		"method":     method,
		"path":       path,
	}
	if len(pathParams) > 0 {
		data["path_params"] = pathParams
	}
	if len(queryValues) > 0 {
		data["query"] = queryValues
	}
	if body != nil {
		data["body"] = body
	}
	text := fmt.Sprintf("Validation passed for `oar %s` (%s %s).", commandName, method, path)
	return &commandResult{Text: text, Data: data}
}

func dryRunResult(commandName string, commandID string, pathParams map[string]string, query []queryParam, body any) *commandResult {
	result := validationResult(commandName, commandID, pathParams, query, body)
	if result == nil {
		return nil
	}
	data, _ := result.Data.(map[string]any)
	if data != nil {
		data["dry_run"] = true
	}
	result.Text = result.Text + " No request was sent."
	return result
}

func (a *App) invokeTypedJSONWithIDResolution(
	ctx context.Context,
	cfg config.Resolved,
	commandName string,
	commandID string,
	pathParamName string,
	rawID string,
	lookupSpec resourceIDLookupSpec,
	query []queryParam,
	body any,
) (*commandResult, error) {
	pathParams := map[string]string{pathParamName: rawID}
	result, err := a.invokeTypedJSON(ctx, cfg, commandName, commandID, pathParams, query, body)
	if err == nil {
		return result, nil
	}
	if !isResolvableResourceNotFoundError(err, lookupSpec) {
		return nil, err
	}

	resolvedID, resolveErr := a.resolveResourceIDFromList(ctx, cfg, rawID, lookupSpec)
	if resolveErr != nil {
		return nil, resolveErr
	}
	if resolvedID == rawID {
		return nil, missingResourceIDError(rawID, lookupSpec)
	}
	return a.invokeTypedJSON(ctx, cfg, commandName, commandID, map[string]string{pathParamName: resolvedID}, query, body)
}

func (a *App) invokeArtifactContentWithIDResolution(
	ctx context.Context,
	cfg config.Resolved,
	commandName string,
	pathParamName string,
	rawID string,
	lookupSpec resourceIDLookupSpec,
) (*commandResult, error) {
	result, err := a.invokeArtifactContent(ctx, cfg, commandName, map[string]string{pathParamName: rawID})
	if err == nil {
		return result, nil
	}
	if !isResolvableResourceNotFoundError(err, lookupSpec) {
		return nil, err
	}
	resolvedID, resolveErr := a.resolveResourceIDFromList(ctx, cfg, rawID, lookupSpec)
	if resolveErr != nil {
		return nil, resolveErr
	}
	if resolvedID == rawID {
		return nil, missingResourceIDError(rawID, lookupSpec)
	}
	return a.invokeArtifactContent(ctx, cfg, commandName, map[string]string{pathParamName: resolvedID})
}

func (a *App) cfgWithResolvedAuthToken(ctx context.Context, cfg config.Resolved) (config.Resolved, error) {
	svc := authcli.New(cfg)
	prof, err := svc.EnsureAccessToken(ctx)
	if err != nil {
		normalized := errnorm.Normalize(err)
		if normalized != nil && normalized.Code == "profile_not_found" {
			return cfg, nil
		}
		return config.Resolved{}, err
	}
	cfg.AccessToken = strings.TrimSpace(prof.AccessToken)
	if cfg.AccessToken == "" {
		return cfg, nil
	}
	return cfg, nil
}

func generatedHeaders(cfg config.Resolved) map[string]string {
	headers := map[string]string{
		"Accept":            "application/json",
		"X-OAR-CLI-Version": httpclient.CLIVersion,
	}
	if strings.TrimSpace(cfg.Agent) != "" {
		headers["X-OAR-Agent"] = strings.TrimSpace(cfg.Agent)
	}
	if strings.TrimSpace(cfg.AccessToken) != "" {
		headers["Authorization"] = "Bearer " + strings.TrimSpace(cfg.AccessToken)
	}
	return headers
}

func resolveCommandMethod(commandID string) string {
	spec, ok := commandSpecByID(commandID)
	if !ok {
		return http.MethodGet
	}
	return spec.Method
}

func resolveCommandPath(commandID string, pathParams map[string]string, query map[string][]string) string {
	spec, ok := commandSpecByID(commandID)
	if !ok {
		return "/"
	}
	resolved := spec.Path
	for _, param := range spec.PathParams {
		value := pathParams[param]
		resolved = strings.ReplaceAll(resolved, "{"+param+"}", url.PathEscape(value))
	}
	u := url.URL{Path: resolved}
	if len(query) > 0 {
		q := url.Values{}
		keys := make([]string, 0, len(query))
		for key := range query {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			for _, value := range query[key] {
				q.Add(key, value)
			}
		}
		u.RawQuery = q.Encode()
	}
	return u.String()
}

func normalizedHeaders(input http.Header) map[string][]string {
	out := make(map[string][]string, len(input))
	for key, values := range input {
		if strings.EqualFold(key, "Date") || strings.EqualFold(key, "Content-Length") || strings.EqualFold(key, "Connection") {
			continue
		}
		copied := append([]string(nil), values...)
		out[key] = copied
	}
	return out
}

func commandSpecByID(commandID string) (contractsclient.CommandSpec, bool) {
	commandID = strings.TrimSpace(commandID)
	for _, spec := range contractsclient.CommandRegistry {
		if strings.TrimSpace(spec.CommandID) == commandID {
			return spec, true
		}
	}
	return contractsclient.CommandSpec{}, false
}
