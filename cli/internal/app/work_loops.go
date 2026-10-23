package app

import (
	"context"
	"fmt"
	"math"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/errnorm"
)

func isLoopWork(name string) bool {
	return name == "work cadence" || name == "work goal" || name == "work receipt"
}

var receiptMetric = regexp.MustCompile(`^[a-z][a-z0-9_.-]{0,119}$`)

func loopDuration(raw string) (int64, error) {
	if strings.HasSuffix(raw, "d") {
		n, err := strconv.ParseInt(strings.TrimSuffix(raw, "d"), 10, 64)
		if err != nil || n < 1 || n > 36525 {
			return 0, fmt.Errorf("invalid duration")
		}
		return n * 86400, nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d < time.Second || d%time.Second != 0 {
		return 0, fmt.Errorf("duration must use whole s/m/h/d")
	}
	return int64(d / time.Second), nil
}

// Shared arity keeps the global scan from interpreting user-authored values.
func loopWorkValueFlags(name string) []string {
	switch name {
	case "work cadence":
		return []string{"every", "budget", "stop-rule"}
	case "work goal":
		return []string{"metric", "direction", "target", "baseline", "unit", "label", "series"}
	case "work receipt":
		return []string{"run-id", "outcome", "summary", "candidate", "decision", "score-before", "score-after", "started-at"}
	}
	return nil
}

func parseLoopWork(name string, args []string) (string, map[string]any, error) {
	fs := newSilentFlagSet(name)
	var clear trackedBool
	var id trackedString
	values := map[string]*trackedString{}
	keys := loopWorkValueFlags(name)
	if name == "work cadence" || name == "work goal" {
		fs.Var(&clear, "clear", "Clear annotation")
	}
	for _, k := range keys {
		v := new(trackedString)
		values[k] = v
		fs.Var(v, k, k)
	}
	var metrics, refs trackedStrings
	if name == "work receipt" {
		fs.Var(&metrics, "metric", "Metric name=value (repeatable)")
		fs.Var(&refs, "ref", "Effect ref or HTTP/HTTPS URL (repeatable)")
	}
	fs.Var(&id, "card-id", "Card ref, handle or id")
	leading, rest := splitLeadingPositionals(args)
	if err := fs.Parse(rest); err != nil {
		return "", nil, errnorm.Usage("invalid_flags", err.Error())
	}
	pos := append(leading, fs.Args()...)
	if len(pos) > 1 || len(pos) == 1 && id.set {
		return "", nil, errnorm.Usage("invalid_args", "pass one card")
	}
	card := id.value
	if len(pos) == 1 {
		card = pos[0]
	}
	if card == "." || card == ".." || strings.ContainsAny(card, "/\\?#%") || strings.ContainsFunc(card, unicode.IsControl) {
		return "", nil, errnorm.Usage("invalid_request", "card must be a ref, handle or id")
	}
	if strings.TrimSpace(card) == "" {
		return "", nil, errnorm.Usage("invalid_request", "card required")
	}
	bad := func(message string) (string, map[string]any, error) {
		return "", nil, errnorm.Usage("invalid_request", message)
	}
	body := map[string]any{}
	if clear.value {
		for _, v := range values {
			if v.set {
				return bad("--clear cannot be combined with value flags")
			}
		}
		body[strings.TrimPrefix(name, "work ")] = nil
		return card, body, nil
	}
	for k, v := range values {
		if v.set && strings.TrimSpace(v.value) == "" {
			return bad("--" + k + " must not be empty")
		}
	}
	number := func(key string) (float64, error) {
		n, e := strconv.ParseFloat(values[key].value, 64)
		if e != nil || math.IsNaN(n) || math.IsInf(n, 0) {
			return 0, fmt.Errorf("--%s must be finite", key)
		}
		return n, nil
	}
	switch name {
	case "work cadence":
		every, err := loopDuration(values["every"].value)
		if err != nil || every < 60 || every > 3155760000 {
			return bad("--every must be at least 60s and at most 36525d")
		}
		m := map[string]any{"every_seconds": every}
		if values["budget"].set {
			budget, err := loopDuration(values["budget"].value)
			if err != nil || budget < every || budget > 3155760000 {
				return bad("--budget must be >= --every and <=36525d")
			}
			m["progress_budget_seconds"] = budget
		}
		if values["stop-rule"].set {
			if len([]rune(values["stop-rule"].value)) > 500 {
				return bad("stop rule exceeds 500 characters")
			}
			m["stop_rule"] = values["stop-rule"].value
		}
		body["cadence"] = m
	case "work goal":
		if !receiptMetric.MatchString(values["metric"].value) {
			return bad("--metric must be a series-name of at most 120 characters")
		}
		direction := values["direction"].value
		if direction != "up" && direction != "down" {
			return bad("--direction must be up or down")
		}
		m := map[string]any{"metric": values["metric"].value, "direction": direction}
		for _, k := range []string{"target", "baseline"} {
			if values[k].set {
				n, e := number(k)
				if e != nil {
					return bad(e.Error())
				}
				m[k] = n
			}
		}
		if values["label"].set {
			if len([]rune(values["label"].value)) > 200 {
				return bad("label exceeds 200 characters")
			}
			m["label"] = values["label"].value
		}
		if values["unit"].set {
			if len([]rune(values["unit"].value)) > 80 {
				return bad("unit exceeds 80 characters")
			}
			m["unit"] = values["unit"].value
		}
		if values["series"].set {
			ref := values["series"].value
			if !strings.HasPrefix(ref, "series:") || len(ref) > 2048 {
				return bad("--series must be a series ref")
			}
			m["series_ref"] = ref
		}
		body["goal"] = m
	case "work receipt":
		runID, summary, outcome := values["run-id"].value, values["summary"].value, values["outcome"].value
		if runID == "" || len([]rune(runID)) > 200 {
			return bad("--run-id requires 1..200 characters")
		}
		if summary == "" || len([]rune(summary)) > 280 || strings.ContainsAny(summary, "\r\n") {
			return bad("--summary requires one line of 1..280 characters")
		}
		switch outcome {
		case "acted", "measured", "no_op", "infra_error":
		default:
			return bad("--outcome must be acted, measured, no_op or infra_error")
		}
		body = map[string]any{"run_id": runID, "outcome": outcome}
		if len(metrics.values) > 16 {
			return bad("at most 16 metrics")
		}
		if len(metrics.values) > 0 {
			m := map[string]any{}
			for _, raw := range metrics.values {
				k, v, ok := strings.Cut(raw, "=")
				n, e := strconv.ParseFloat(v, 64)
				if !ok || !receiptMetric.MatchString(k) || e != nil || math.IsNaN(n) || math.IsInf(n, 0) {
					return bad("--metric must be name=finite-number")
				}
				if _, has := m[k]; has {
					return bad("duplicate metric")
				}
				m[k] = n
			}
			body["metrics"] = m
		}
		experiment := map[string]any{}
		for _, k := range []string{"candidate", "decision", "score-before", "score-after"} {
			if values[k].set {
				if strings.HasPrefix(k, "score-") {
					n, e := number(k)
					if e != nil {
						return bad(e.Error())
					}
					experiment[strings.ReplaceAll(k, "-", "_")] = n
				} else {
					experiment[k] = values[k].value
				}
			}
		}
		if len(experiment) > 0 {
			if !values["candidate"].set || len([]rune(values["candidate"].value)) > 200 {
				return bad("--candidate required, at most 200 characters")
			}
			switch values["decision"].value {
			case "kept", "reverted", "rejected":
			default:
				return bad("--decision must be kept, reverted or rejected")
			}
			body["experiment"] = experiment
		}
		if len(refs.values) > 10 {
			return bad("at most 10 effect refs")
		}
		if len(refs.values) > 0 {
			body["effect_refs"] = refs.values
		}
		if values["started-at"].set {
			if _, e := time.Parse(time.RFC3339Nano, values["started-at"].value); e != nil {
				return bad("invalid --started-at")
			}
			body["started_at"] = values["started-at"].value
		}
		body = map[string]any{"type": "run_recorded", "summary": summary, "payload": body}
	}
	return card, body, nil
}
func (a *App) runLoopWork(ctx context.Context, name string, args []string, cfg config.Resolved) (*commandResult, error) {
	card, body, err := parseLoopWork(name, args)
	if err != nil {
		return nil, err
	}
	if name == "work receipt" {
		c, err := a.fetchCardBody(ctx, cfg, card)
		if err != nil {
			return nil, err
		}
		ref := firstNonEmpty(anyString(c["ref"]), "card:"+anyString(c["id"]))
		body["refs"] = []string{ref}
		body["thread_id"] = c["thread_id"]
		body["provenance"] = map[string]any{"sources": []string{"inferred"}}
		result, err := a.invokeRawJSON(ctx, cfg, name, "POST", "/events", map[string]any{"event": body})
		if err == nil {
			result.Text = anyString(asMap(commandResultBody(result)["event"])["id"])
		}
		return result, err
	}
	path := "/work/" + url.PathEscape(card)
	result, err := a.invokeRawJSON(ctx, cfg, name, "GET", path, nil)
	if err != nil {
		return nil, err
	}
	work := asMap(commandResultBody(result)["work"])
	if work["version"] == nil {
		return nil, errnorm.Internal("invalid_response", "work response missing version")
	}
	return a.invokeRawJSON(ctx, cfg, name, "PATCH", path, map[string]any{"if_version": work["version"], "patch": body})
}
