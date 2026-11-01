// Claim-path handling for `anx pm serve`: claim capacity accounting from API
// responses, claim error classification (retryable, forbidden), and the shared
// PM error-detail/status mappers.

package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"agent-nexus-cli/internal/errnorm"
)

func claimErrorForbidden(err error) bool {
	return httpStatusFromErr(err) == http.StatusForbidden
}

func (a *App) logAgentctlOutlives(turnID, execID string) {
	if !strings.HasPrefix(execID, "exec-") {
		return
	}
	a.pmLog("pm serve: turn %s background execution %s outlives this runner; cancel it with `agentctl cancel %s`\n", turnID, execID, execID)
}

func claimErrorLabel(err error) string {
	var typed *errnorm.Error
	if errors.As(err, &typed) {
		if code := strings.TrimSpace(typed.Code); code != "" {
			return strings.ReplaceAll(code, "_", " ")
		}
		if msg := strings.TrimSpace(typed.Message); msg != "" {
			return strings.ToLower(msg)
		}
	}
	return "error"
}

func claimErrorRetryable(err error) bool {
	if err == nil {
		return true
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var typed *errnorm.Error
	if !errors.As(err, &typed) {
		return true
	}
	if typed.Kind == errnorm.KindNetwork {
		return true
	}
	status := httpStatusFromErr(typed)
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden:
		return false
	case http.StatusRequestTimeout, http.StatusConflict, http.StatusTooManyRequests,
		http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	}
	if status >= 400 && status < 500 {
		return false
	}
	if typed.Recoverable != nil {
		return *typed.Recoverable
	}
	return true
}

type pmClaimCapacity struct {
	InFlight int
	Limit    int
	Waiting  int
}

func (c pmClaimCapacity) asMap() map[string]any {
	return map[string]any{
		"claimed":   false,
		"reason":    "capacity",
		"in_flight": c.InFlight,
		"limit":     c.Limit,
		"waiting":   c.Waiting,
	}
}

func formatPMClaimCapacityText(c pmClaimCapacity) string {
	return fmt.Sprintf("No lease available: %d of %d runner leases are held; %d turn(s) are waiting.", c.InFlight, c.Limit, c.Waiting)
}

func parsePMClaimCapacity(result *commandResult, err error) (pmClaimCapacity, bool) {
	if cap, ok := pmClaimCapacityFromMap(commandResultBody(result)); ok {
		return cap, true
	}
	if err == nil {
		return pmClaimCapacity{}, false
	}
	var typed *errnorm.Error
	if !errors.As(err, &typed) || typed == nil {
		return pmClaimCapacity{}, false
	}
	if !strings.EqualFold(strings.TrimSpace(typed.Code), "busy") {
		return pmClaimCapacity{}, false
	}
	details := pmErrorDetails(typed)
	if !strings.EqualFold(anyString(details["reason"]), "capacity") {
		return pmClaimCapacity{}, false
	}
	return pmClaimCapacityFromMap(details)
}

func pmClaimCapacityFromMap(m map[string]any) (pmClaimCapacity, bool) {
	if len(m) == 0 {
		return pmClaimCapacity{}, false
	}
	if asBool(m["claimed"]) {
		return pmClaimCapacity{}, false
	}
	if !strings.EqualFold(anyString(m["reason"]), "capacity") {
		return pmClaimCapacity{}, false
	}
	waiting := intValue(m["waiting"])
	if waiting == 0 {
		waiting = intValue(m["queued"])
	}
	return pmClaimCapacity{
		InFlight: intValue(m["in_flight"]),
		Limit:    intValue(m["limit"]),
		Waiting:  waiting,
	}, true
}

func pmErrorDetails(err error) map[string]any {
	var typed *errnorm.Error
	if !errors.As(err, &typed) || typed == nil {
		return nil
	}
	details, _ := typed.Details.(map[string]any)
	parsed := asMap(details["parsed"])
	errObj := asMap(parsed["error"])
	if nested := asMap(errObj["details"]); len(nested) > 0 {
		return nested
	}
	if nested := asMap(parsed["details"]); len(nested) > 0 {
		return nested
	}
	return nil
}

func httpStatusFromErr(err error) int {
	var typed *errnorm.Error
	if !errors.As(err, &typed) {
		return 0
	}
	details, _ := typed.Details.(map[string]any)
	switch v := details["status"].(type) {
	case int:
		return v
	case float64:
		return int(v)
	}
	return 0
}
