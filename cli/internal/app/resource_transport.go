package app

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	contractsclient "agent-nexus-contracts-go-client/client"

	"agent-nexus-cli/internal/authcli"
	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/errnorm"
	"agent-nexus-cli/internal/httpclient"
)

func (a *App) invokeArtifactContent(ctx context.Context, cfg config.Resolved, commandName string, pathParams map[string]string, outputPath string) (*commandResult, error) {
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

	outPath := strings.TrimSpace(outputPath)
	if outPath == "." {
		resolved, resolveErr := a.resolveArtifactContentOutputPath(ctx, authCfg, strings.TrimSpace(pathParams["artifact_id"]), resp.Headers)
		if resolveErr != nil {
			return nil, resolveErr
		}
		outPath = resolved
	}
	if outPath != "" {
		if err := os.WriteFile(outPath, body, 0o644); err != nil {
			return nil, errnorm.Wrap(errnorm.KindLocal, "artifact_content_write_failed", fmt.Sprintf("failed to write %s", outPath), err)
		}
		if !authCfg.JSON {
			return &commandResult{Text: fmt.Sprintf("wrote %d bytes to %s", len(body), outPath)}, nil
		}
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
	if outPath != "" {
		data["output_path"] = outPath
		data["bytes_written"] = len(body)
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

func (a *App) resolveArtifactContentOutputPath(ctx context.Context, cfg config.Resolved, artifactID string, headers http.Header) (string, error) {
	if filename := artifactContentDispositionFilename(headers); filename != "" {
		return filename, nil
	}
	filename, err := a.artifactFilenameFromMetadata(ctx, cfg, artifactID)
	if err != nil {
		return "", err
	}
	if filename == "" {
		return "", errnorm.Usage("artifact_filename_unavailable", "--output . requires Content-Disposition filename or artifact filename metadata")
	}
	return filename, nil
}

func artifactContentDispositionFilename(headers http.Header) string {
	raw := strings.TrimSpace(headers.Get("Content-Disposition"))
	if raw == "" {
		return ""
	}
	_, params, err := mime.ParseMediaType(raw)
	if err != nil {
		return ""
	}
	return cleanArtifactOutputFilename(firstNonEmpty(params["filename"], params["filename*"]))
}

func (a *App) artifactFilenameFromMetadata(ctx context.Context, cfg config.Resolved, artifactID string) (string, error) {
	client, err := httpclient.New(cfg)
	if err != nil {
		return "", errnorm.Wrap(errnorm.KindLocal, "http_client_init_failed", "failed to initialize HTTP client", err)
	}
	callCtx, cancel := httpclient.WithTimeout(ctx, cfg.Timeout)
	defer cancel()
	resp, invokeErr := client.RawCall(callCtx, httpclient.RawRequest{
		Method:  http.MethodGet,
		Path:    "/artifacts/" + url.PathEscape(strings.TrimSpace(artifactID)),
		Headers: generatedHeaders(cfg),
	})
	if invokeErr != nil {
		return "", errnorm.Wrap(errnorm.KindNetwork, "request_failed", "artifact metadata request failed", invokeErr)
	}
	if resp.StatusCode >= http.StatusBadRequest {
		return "", errnorm.FromHTTPFailure(resp.StatusCode, resp.Body)
	}
	parsed := parseResponseBody(resp.Body)
	body, _ := parsed.(map[string]any)
	artifact := extractNestedMap(body, "artifact")
	if artifact == nil {
		artifact = body
	}
	for _, key := range []string{"filename", "original_filename", "name"} {
		if filename := cleanArtifactOutputFilename(anyString(artifact[key])); filename != "" {
			return filename, nil
		}
	}
	return "", nil
}

func cleanArtifactOutputFilename(filename string) string {
	filename = strings.TrimSpace(filename)
	if filename == "" {
		return ""
	}
	filename = filepath.Base(filename)
	if filename == "." || filename == string(filepath.Separator) {
		return ""
	}
	return filename
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
	text := fmt.Sprintf("Validation passed for `anx %s` (%s %s).", commandName, method, path)
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
	outputPath string,
) (*commandResult, error) {
	result, err := a.invokeArtifactContent(ctx, cfg, commandName, map[string]string{pathParamName: rawID}, outputPath)
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
	return a.invokeArtifactContent(ctx, cfg, commandName, map[string]string{pathParamName: resolvedID}, outputPath)
}

func (a *App) invokeArtifactAttachmentCreate(ctx context.Context, cfg config.Resolved, refsJSON, filePath, summary, artifactJSON, actorID string) (*commandResult, error) {
	refsJSON = strings.TrimSpace(refsJSON)
	var refsProbe []any
	if err := json.Unmarshal([]byte(refsJSON), &refsProbe); err != nil || len(refsProbe) == 0 {
		return nil, errnorm.Usage("invalid_request", "--refs must be a JSON array of typed ref strings (e.g. [\"thread:<id>\"])")
	}
	cleanPath := filepath.Clean(strings.TrimSpace(filePath))
	file, err := os.Open(cleanPath)
	if err != nil {
		return nil, errnorm.Wrap(errnorm.KindLocal, "attachment_file_open_failed", fmt.Sprintf("failed to open %s", cleanPath), err)
	}
	defer file.Close()

	authCfg, err := a.cfgWithResolvedAuthToken(ctx, cfg)
	if err != nil {
		return nil, err
	}
	client, err := httpclient.New(authCfg)
	if err != nil {
		return nil, errnorm.Wrap(errnorm.KindLocal, "http_client_init_failed", "failed to initialize HTTP client", err)
	}

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	if err := mw.WriteField("refs", refsJSON); err != nil {
		return nil, errnorm.Wrap(errnorm.KindLocal, "multipart_encode_failed", "failed to encode refs field", err)
	}
	if strings.TrimSpace(summary) != "" {
		if err := mw.WriteField("summary", strings.TrimSpace(summary)); err != nil {
			return nil, errnorm.Wrap(errnorm.KindLocal, "multipart_encode_failed", "failed to encode summary field", err)
		}
	}
	if strings.TrimSpace(artifactJSON) != "" {
		if err := mw.WriteField("artifact", strings.TrimSpace(artifactJSON)); err != nil {
			return nil, errnorm.Wrap(errnorm.KindLocal, "multipart_encode_failed", "failed to encode artifact field", err)
		}
	}
	if strings.TrimSpace(actorID) != "" {
		if err := mw.WriteField("actor_id", strings.TrimSpace(actorID)); err != nil {
			return nil, errnorm.Wrap(errnorm.KindLocal, "multipart_encode_failed", "failed to encode actor_id field", err)
		}
	} else if strings.TrimSpace(cfg.ActorID) != "" {
		if err := mw.WriteField("actor_id", strings.TrimSpace(cfg.ActorID)); err != nil {
			return nil, errnorm.Wrap(errnorm.KindLocal, "multipart_encode_failed", "failed to encode actor_id field", err)
		}
	}
	part, err := mw.CreateFormFile("file", filepath.Base(cleanPath))
	if err != nil {
		return nil, errnorm.Wrap(errnorm.KindLocal, "multipart_encode_failed", "failed to create file part", err)
	}
	if _, err := io.Copy(part, file); err != nil {
		return nil, errnorm.Wrap(errnorm.KindLocal, "multipart_encode_failed", "failed to stream file bytes", err)
	}
	if err := mw.Close(); err != nil {
		return nil, errnorm.Wrap(errnorm.KindLocal, "multipart_encode_failed", "failed to finalize multipart body", err)
	}

	headers := generatedHeaders(authCfg)
	headers["Content-Type"] = mw.FormDataContentType()

	callCtx, cancel := httpclient.WithTimeout(ctx, authCfg.Timeout)
	defer cancel()
	resp, invokeErr := client.RawCall(callCtx, httpclient.RawRequest{
		Method:  http.MethodPost,
		Path:    "/artifacts/attachments",
		Headers: headers,
		Body:    body.Bytes(),
	})
	if invokeErr != nil {
		return nil, errnorm.Wrap(errnorm.KindNetwork, "request_failed", "artifacts attachments create request failed", invokeErr)
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
	text := formatTypedCommandText("artifacts.attachments.create", resp.StatusCode, headersSorted, parsedBody, authCfg.Verbose, authCfg.Headers)
	return &commandResult{Text: text, Data: data}, nil
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
		"X-ANX-CLI-Version": httpclient.CLIVersion,
	}
	if strings.TrimSpace(cfg.Agent) != "" {
		headers["X-ANX-Agent"] = strings.TrimSpace(cfg.Agent)
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
