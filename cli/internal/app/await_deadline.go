package app

import (
	"context"
	"errors"
	"strings"
	"time"

	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/errnorm"
)

// An overnight wait for a human answer used to need a shell loop re-running
// anx await and grepping for code=timeout, because every wait ended with the
// same exit 8 a network timeout uses. --deadline names an absolute end: the
// same command can be re-armed after any interruption without extending the
// wait, and reaching it with no answer exits 14 deadline_reached.

// awaitDeadlineFloor lets a re-armed wait whose deadline has just passed
// still collect answers that are already waiting.
const awaitDeadlineFloor = 5 * time.Second

func parseAwaitDeadline(flag trackedString, timeoutSet bool, now time.Time) (time.Time, bool, error) {
	if !flag.set {
		return time.Time{}, false, nil
	}
	if timeoutSet {
		return time.Time{}, false, errnorm.Usage("invalid_flags", "use --deadline or --timeout, not both; --deadline is absolute and safe to re-arm")
	}
	raw := strings.TrimSpace(flag.value)
	if at, err := time.Parse(time.RFC3339, raw); err == nil {
		return at, true, nil
	}
	if at, ok := parsePromiseWhen(raw, now); ok {
		return at, true, nil
	}
	return time.Time{}, false, errnorm.Usage("invalid_request", "--deadline must be an RFC3339 time, a UTC date (YYYY-MM-DD) or a positive duration like 8h; for a re-armable wait pass an absolute time")
}

// runAwaitUntil reruns the same await with the remaining time as its timeout
// and reports the end of that time as deadline_reached.
func (a *App) runAwaitUntil(ctx context.Context, args []string, cfg config.Resolved, deadline time.Time) (*commandResult, error) {
	rest := make([]string, 0, len(args)+2)
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--deadline" || arg == "-deadline" {
			i++
			continue
		}
		if strings.HasPrefix(arg, "--deadline=") || strings.HasPrefix(arg, "-deadline=") {
			continue
		}
		rest = append(rest, arg)
	}
	wait := max(time.Until(deadline), awaitDeadlineFloor)
	result, err := a.runAwait(ctx, append(rest, "--timeout", wait.Round(time.Millisecond).String()), cfg)
	var e *errnorm.Error
	if err != nil && errors.As(err, &e) && e.Code == "timeout" {
		details, _ := e.Details.(map[string]any)
		if details == nil {
			details = map[string]any{}
		}
		details["deadline"] = deadline.UTC().Format(time.RFC3339)
		details["rearm"] = "rerun the same command; the deadline is absolute, so re-arming never extends it"
		reached := errnorm.WithDetails(errnorm.New(errnorm.KindLocal, "deadline_reached", "deadline reached with no answer"), details)
		return nil, reached
	}
	return result, err
}
