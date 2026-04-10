package app

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	contractsclient "organization-autorunner-contracts-go-client/client"

	"organization-autorunner-cli/internal/config"
	"organization-autorunner-cli/internal/errnorm"
	"organization-autorunner-cli/internal/httpclient"
	"organization-autorunner-cli/internal/output"
	"organization-autorunner-cli/internal/streaming"
)

func (a *App) runTailStream(ctx context.Context, cfg config.Resolved, commandName string, commandID string, query []queryParam, lastEventID string, follow bool, reconnect bool, maxEvents int) (*commandResult, error) {
	if maxEvents < 0 {
		return nil, errnorm.Usage("invalid_request", "--max-events must be >= 0")
	}

	authCfg, err := a.cfgWithResolvedAuthToken(ctx, cfg)
	if err != nil {
		return nil, err
	}
	client, err := httpclient.New(authCfg)
	if err != nil {
		return nil, errnorm.Wrap(errnorm.KindLocal, "http_client_init_failed", "failed to initialize HTTP client", err)
	}

	cursor := strings.TrimSpace(lastEventID)
	received := 0

	for {
		callCtx := ctx
		headers := map[string]string{"Accept": "text/event-stream"}
		if cursor != "" {
			headers["Last-Event-ID"] = cursor
		}
		requestPath := streamPathForCommand(commandID, query, cursor)
		resp, streamErr := client.OpenStream(callCtx, httpclient.RawRequest{Method: http.MethodGet, Path: requestPath, Headers: headers})
		if streamErr != nil {
			if !follow || !reconnect {
				return nil, errnorm.Wrap(errnorm.KindNetwork, "stream_connect_failed", "failed to connect stream", streamErr)
			}
			time.Sleep(250 * time.Millisecond)
			continue
		}

		if resp.StatusCode >= http.StatusBadRequest {
			body, _ := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			return nil, errnorm.FromHTTPFailure(resp.StatusCode, body)
		}

		reader := bufio.NewReader(resp.Body)
		dropped := false
		for {
			event, readErr := streaming.ReadEvent(reader)
			if readErr != nil {
				if readErr == io.EOF {
					dropped = true
					break
				}
				if !follow && isStreamReadTimeout(readErr) {
					dropped = false
					break
				}
				_ = resp.Body.Close()
				if !follow || !reconnect {
					return nil, errnorm.Wrap(errnorm.KindNetwork, "stream_read_failed", "failed to read stream", readErr)
				}
				dropped = true
				break
			}
			if strings.TrimSpace(event.ID) != "" {
				cursor = strings.TrimSpace(event.ID)
			}
			if err := a.writeStreamEvent(commandName, commandID, event, authCfg.JSON); err != nil {
				_ = resp.Body.Close()
				return nil, err
			}
			received++
			if maxEvents > 0 && received >= maxEvents {
				_ = resp.Body.Close()
				return &commandResult{RawWritten: true}, nil
			}
		}
		_ = resp.Body.Close()
		if !follow || !reconnect || !dropped {
			return &commandResult{RawWritten: true}, nil
		}
		time.Sleep(250 * time.Millisecond)
	}
}

func streamPayload(commandID string, parsedData any) (string, any) {
	commandID = strings.TrimSpace(commandID)
	dataMap, ok := parsedData.(map[string]any)
	if !ok {
		return "data", parsedData
	}
	switch commandID {
	case "events.stream":
		if eventPayload, ok := dataMap["event"]; ok {
			return "event", eventPayload
		}
	case "inbox.stream":
		if itemPayload, ok := dataMap["item"]; ok {
			return "item", itemPayload
		}
	}
	if len(dataMap) == 1 {
		for key, value := range dataMap {
			key = strings.TrimSpace(key)
			if key == "" {
				return "data", parsedData
			}
			return key, value
		}
	}
	return "data", parsedData
}

func (a *App) writeStreamEvent(commandName string, commandID string, event streaming.Event, jsonMode bool) error {
	parsedData := parseResponseBody([]byte(event.Data))
	payloadKey, payloadValue := streamPayload(commandID, parsedData)
	frame := map[string]any{
		"id":          event.ID,
		"type":        event.Type,
		"payload_key": payloadKey,
		"payload":     payloadValue,
	}
	if payloadKey == "event" || payloadKey == "item" {
		frame[payloadKey] = payloadValue
	}
	if jsonMode {
		identity := resolveMachineCommandIdentity(commandName)
		envelope := output.Envelope{OK: true, Command: identity.Command, CommandID: identity.CommandID, Data: frame}
		if err := output.WriteEnvelopeJSON(a.Stdout, envelope); err != nil {
			return errnorm.Wrap(errnorm.KindLocal, "stdout_write_failed", "failed to write stream envelope", err)
		}
		return nil
	}
	line := fmt.Sprintf("[%s] %s", event.ID, event.Type)
	if strings.TrimSpace(event.Data) != "" {
		line += " " + strings.TrimSpace(event.Data)
	}
	if _, err := io.WriteString(a.Stdout, line+"\n"); err != nil {
		return errnorm.Wrap(errnorm.KindLocal, "stdout_write_failed", "failed to write stream event", err)
	}
	return nil
}

func streamPathForCommand(commandID string, query []queryParam, cursor string) string {
	spec, ok := commandSpecByID(commandID)
	if !ok {
		switch strings.TrimSpace(commandID) {
		case "events.stream":
			spec = contractsclient.CommandSpec{Path: "/events/stream"}
		case "inbox.stream":
			spec = contractsclient.CommandSpec{Path: "/inbox/stream"}
		default:
			return "/"
		}
	}
	u := url.URL{Path: spec.Path}
	q := url.Values{}
	for _, param := range query {
		for _, value := range param.values {
			q.Add(param.name, value)
		}
	}
	if strings.TrimSpace(cursor) != "" {
		q.Set("last_event_id", strings.TrimSpace(cursor))
	}
	u.RawQuery = q.Encode()
	return u.String()
}

func isStreamReadTimeout(err error) bool {
	if err == nil {
		return false
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "context deadline exceeded") || strings.Contains(text, "client.timeout")
}
