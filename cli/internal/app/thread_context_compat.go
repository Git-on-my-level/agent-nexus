package app

import (
	"context"
	"net/http"
	"net/url"
	"strings"

	"organization-autorunner-cli/internal/config"
	"organization-autorunner-cli/internal/errnorm"
	"organization-autorunner-cli/internal/httpclient"
)

func (a *App) runThreadsContextCommand(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, error) {
	selection, err := parseThreadContextSelectionArgs(args, "threads context")
	if err != nil {
		return nil, err
	}
	threadIDs, err := a.resolveThreadContextSelection(ctx, cfg, "threads context", selection, true)
	if err != nil {
		return nil, err
	}

	if len(threadIDs) == 1 {
		statusCode, headers, body, err := a.loadThreadContextEnvelope(ctx, cfg, threadIDs[0], selection)
		if err != nil {
			return nil, err
		}
		data := map[string]any{
			"status_code": statusCode,
			"headers":     headers,
			"body":        body,
		}
		result := &commandResult{Data: data}
		result.Text = formatTypedCommandText(
			"threads.context",
			statusCode,
			headers,
			body,
			cfg.Verbose,
			cfg.Headers,
		)
		return result, nil
	}

	resolvedThreadIDs := make([]string, 0, len(threadIDs))
	threadRecords := make([]any, 0, len(threadIDs))
	contexts := make([]any, 0, len(threadIDs))
	recentEvents := make([]any, 0, len(threadIDs)*4)
	keyArtifacts := make([]any, 0, len(threadIDs)*2)
	openCommitments := make([]any, 0, len(threadIDs)*2)
	documents := make([]any, 0, len(threadIDs)*2)
	seenContextThreadIDs := make(map[string]struct{}, len(threadIDs))
	statusCode := http.StatusOK
	headers := map[string][]string{"Content-Type": {"application/json"}}
	capturedTransport := false

	for _, threadID := range threadIDs {
		threadStatus, threadHeaders, body, err := a.loadThreadContextEnvelope(ctx, cfg, threadID, selection)
		if err != nil {
			return nil, err
		}
		if !capturedTransport {
			if threadStatus > 0 {
				statusCode = threadStatus
			}
			if len(threadHeaders) > 0 {
				headers = threadHeaders
			}
			capturedTransport = true
		}

		thread := asMap(body["thread"])
		resolvedContextThreadID := strings.TrimSpace(anyString(body["thread_id"]))
		if thread != nil {
			resolvedContextThreadID = firstNonEmpty(strings.TrimSpace(anyString(thread["id"])), resolvedContextThreadID)
		}
		if resolvedContextThreadID == "" {
			resolvedContextThreadID = strings.TrimSpace(threadID)
		}
		if resolvedContextThreadID != "" {
			if _, exists := seenContextThreadIDs[resolvedContextThreadID]; exists {
				continue
			}
			seenContextThreadIDs[resolvedContextThreadID] = struct{}{}
			resolvedThreadIDs = append(resolvedThreadIDs, resolvedContextThreadID)
		}

		if thread != nil {
			threadRecords = append(threadRecords, thread)
		}
		contexts = append(contexts, body)
		recentEvents = append(recentEvents, asSlice(body["recent_events"])...)
		keyArtifacts = append(keyArtifacts, asSlice(body["key_artifacts"])...)
		openCommitments = append(openCommitments, asSlice(body["open_commitments"])...)
		documents = append(documents, asSlice(body["documents"])...)
	}
	resolvedThreadIDs = normalizeIDFilters(resolvedThreadIDs)
	if len(resolvedThreadIDs) == 0 {
		resolvedThreadIDs = threadIDs
	}

	aggregateBody := map[string]any{
		"thread_ids":       resolvedThreadIDs,
		"thread_count":     len(resolvedThreadIDs),
		"threads":          uniqueMapsByID(threadRecords),
		"contexts":         contexts,
		"recent_events":    uniqueMapsByID(recentEvents),
		"key_artifacts":    uniqueContextArtifactItems(keyArtifacts),
		"open_commitments": uniqueMapsByID(openCommitments),
		"documents":        uniqueMapsByID(documents),
		"contexts_generated": true,
		"full_id":            selection.fullID,
	}
	sortEventsByCreatedAt(asSlice(aggregateBody["recent_events"]))
	addThreadContextCollaborationSummary(aggregateBody)

	data := map[string]any{
		"status_code": statusCode,
		"headers":     headers,
		"body":        aggregateBody,
	}
	result := &commandResult{Data: data}
	result.Text = formatTypedCommandText(
		"threads.context",
		statusCode,
		headers,
		aggregateBody,
		cfg.Verbose,
		cfg.Headers,
	)
	return result, nil
}

func (a *App) loadThreadContextEnvelope(ctx context.Context, cfg config.Resolved, threadID string, selection threadContextSelection) (int, map[string][]string, map[string]any, error) {
	authCfg, err := a.cfgWithResolvedAuthToken(ctx, cfg)
	if err != nil {
		return 0, nil, nil, err
	}
	client, err := httpclient.New(authCfg)
	if err != nil {
		return 0, nil, nil, errnorm.Wrap(errnorm.KindLocal, "http_client_init_failed", "failed to initialize HTTP client", err)
	}
	queryValues := queryValuesFromParams(threadContextQuery(selection))
	path := "/threads/" + url.PathEscape(strings.TrimSpace(threadID)) + "/context"
	if encoded := url.Values(queryValues).Encode(); encoded != "" {
		path += "?" + encoded
	}
	resp, invokeErr := client.RawCall(ctx, httpclient.RawRequest{
		Method:  http.MethodGet,
		Path:    path,
		Headers: generatedHeaders(authCfg),
	})
	if invokeErr != nil {
		return 0, nil, nil, errnorm.Wrap(errnorm.KindNetwork, "request_failed", "threads context request failed", invokeErr)
	}
	if resp.StatusCode == http.StatusNotFound {
		bodyText := strings.ToLower(strings.TrimSpace(string(resp.Body)))
		if strings.Contains(bodyText, "endpoint not found") {
			return resp.StatusCode, normalizedHeaders(resp.Headers), nil, errnorm.FromHTTPFailure(resp.StatusCode, resp.Body)
		}

		resolvedThreadID, resolveErr := a.resolveResourceIDFromList(ctx, cfg, threadID, threadIDLookupSpec)
		if resolveErr != nil {
			return 0, nil, nil, resolveErr
		}
		if strings.TrimSpace(resolvedThreadID) != strings.TrimSpace(threadID) {
			path = "/threads/" + url.PathEscape(strings.TrimSpace(resolvedThreadID)) + "/context"
			if encoded := url.Values(queryValues).Encode(); encoded != "" {
				path += "?" + encoded
			}
			resp, invokeErr = client.RawCall(ctx, httpclient.RawRequest{
				Method:  http.MethodGet,
				Path:    path,
				Headers: generatedHeaders(authCfg),
			})
			if invokeErr != nil {
				return 0, nil, nil, errnorm.Wrap(errnorm.KindNetwork, "request_failed", "threads context request failed", invokeErr)
			}
			if resp.StatusCode == http.StatusNotFound {
				bodyText := strings.ToLower(strings.TrimSpace(string(resp.Body)))
				if strings.Contains(bodyText, "endpoint not found") {
					return resp.StatusCode, normalizedHeaders(resp.Headers), nil, errnorm.FromHTTPFailure(resp.StatusCode, resp.Body)
				}
			}
			if resp.StatusCode >= http.StatusBadRequest {
				return resp.StatusCode, normalizedHeaders(resp.Headers), nil, errnorm.FromHTTPFailure(resp.StatusCode, resp.Body)
			}
		}
	}
	if resp.StatusCode >= http.StatusBadRequest {
		return resp.StatusCode, normalizedHeaders(resp.Headers), nil, errnorm.FromHTTPFailure(resp.StatusCode, resp.Body)
	}

	body := asMap(parseResponseBody(resp.Body))
	if body == nil {
		body = map[string]any{}
	}
	if thread := asMap(body["thread"]); thread != nil {
		if tid := strings.TrimSpace(anyString(thread["id"])); tid != "" {
			body["thread_id"] = tid
		}
	}
	if strings.TrimSpace(anyString(body["thread_id"])) == "" {
		body["thread_id"] = strings.TrimSpace(threadID)
	}
	if selection.fullID {
		body["full_id"] = true
	}
	addThreadContextCollaborationSummary(body)
	return resp.StatusCode, normalizedHeaders(resp.Headers), body, nil
}

func (a *App) loadThreadContextFromInspect(ctx context.Context, cfg config.Resolved, threadID string, selection threadContextSelection) (int, map[string][]string, map[string]any, error) {
	inspectResult, err := a.invokeTypedJSONWithIDResolution(
		ctx,
		cfg,
		"threads inspect",
		"threads.inspect",
		"thread_id",
		threadID,
		threadIDLookupSpec,
		nil,
		nil,
	)
	if err != nil {
		return 0, nil, nil, err
	}

	data := asMap(inspectResult.Data)
	body := extractNestedMap(data, "body")
	if body == nil {
		body = map[string]any{}
	}
	contextBody := extractNestedMap(body, "context")
	if contextBody == nil {
		contextBody = map[string]any{}
	}
	if thread := extractNestedMap(body, "thread"); thread != nil {
		contextBody["thread"] = thread
		if tid := strings.TrimSpace(anyString(thread["id"])); tid != "" {
			contextBody["thread_id"] = tid
		}
	}
	if strings.TrimSpace(anyString(contextBody["thread_id"])) == "" {
		contextBody["thread_id"] = strings.TrimSpace(threadID)
	}
	if selection.fullID {
		contextBody["full_id"] = true
	}
	if asSlice(contextBody["recent_events"]) == nil {
		contextBody["recent_events"] = asSlice(body["recent_events"])
	}
	if asSlice(contextBody["key_artifacts"]) == nil {
		contextBody["key_artifacts"] = asSlice(body["key_artifacts"])
	}
	if asSlice(contextBody["open_commitments"]) == nil {
		contextBody["open_commitments"] = asSlice(body["open_commitments"])
	}
	if asSlice(contextBody["documents"]) == nil {
		contextBody["documents"] = asSlice(body["documents"])
	}
	addThreadContextCollaborationSummary(contextBody)

	statusCode := intValue(data["status_code"])
	if statusCode == 0 {
		statusCode = http.StatusOK
	}
	return statusCode, headerValues(data["headers"]), contextBody, nil
}
