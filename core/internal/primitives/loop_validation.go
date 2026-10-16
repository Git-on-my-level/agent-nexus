package primitives

import (
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"agent-nexus-core/internal/seriesref"
)

var loopMetricName = regexp.MustCompile(`^[a-z][a-z0-9_.-]{0,119}$`)

func loopNumber(v any) (float64, bool) {
	var n float64
	switch x := v.(type) {
	case float64:
		n = x
	case int:
		n = float64(x)
	case int64:
		n = float64(x)
	case json.Number:
		var e error
		n, e = x.Float64()
		if e != nil {
			return 0, false
		}
	default:
		return 0, false
	}
	return n, !math.IsNaN(n) && !math.IsInf(n, 0)
}
func loopKeys(m map[string]any, allowed ...string) error {
	for k := range m {
		found := false
		for _, a := range allowed {
			if a == k {
				found = true
			}
		}
		if !found {
			return workInvalid("unknown field %s", k)
		}
	}
	return nil
}
func loopText(v any, max int, required bool) bool {
	s, ok := v.(string)
	return ok && utf8.RuneCountInString(s) <= max && (!required || strings.TrimSpace(s) != "")
}
func validateLoopMetadata(m map[string]any) error {
	for _, key := range []string{"cadence", "goal"} {
		raw, exists := m[key]
		if !exists || raw == nil {
			continue
		}
		obj, ok := raw.(map[string]any)
		if !ok {
			return workInvalid("%s must be an object or null", key)
		}
		if key == "cadence" {
			if err := loopKeys(obj, "every_seconds", "progress_budget_seconds", "stop_rule"); err != nil {
				return err
			}
			every, ok := workInt(obj["every_seconds"])
			if !ok || every < 60 || every > 3155760000 {
				return workInvalid("cadence.every_seconds must be an integer from 60 to 3155760000")
			}
			if v, has := obj["progress_budget_seconds"]; has {
				n, ok := workInt(v)
				if !ok || n < every || n > 3155760000 {
					return workInvalid("cadence.progress_budget_seconds must be an integer >= every_seconds and <=3155760000")
				}
			}
			if v, has := obj["stop_rule"]; has && !loopText(v, 500, false) {
				return workInvalid("invalid cadence.stop_rule")
			}
		} else {
			if err := loopKeys(obj, "metric", "direction", "target", "baseline", "unit", "series_ref"); err != nil {
				return err
			}
			metric, ok := obj["metric"].(string)
			if !ok || !loopMetricName.MatchString(metric) {
				return workInvalid("goal.metric must be a series-name of at most 120 characters")
			}
			if obj["direction"] != "up" && obj["direction"] != "down" {
				return workInvalid("goal.direction must be up or down")
			}
			for _, k := range []string{"target", "baseline"} {
				if v, has := obj[k]; has {
					if _, ok := loopNumber(v); !ok {
						return workInvalid("goal.%s must be finite", k)
					}
				}
			}
			if v, has := obj["unit"]; has && !loopText(v, 80, false) {
				return workInvalid("invalid goal.unit")
			}
			if v, has := obj["series_ref"]; has {
				ref, ok := v.(string)
				if !ok {
					return workInvalid("invalid goal.series_ref")
				}
				if _, err := seriesref.ParseRef(ref); err != nil {
					return workInvalid("invalid goal.series_ref")
				}
			}
		}
	}
	return nil
}

// ValidateRunRecorded is shared by the HTTP boundary and canonical event writer.
func ValidateRunRecorded(event map[string]any) error {
	if !loopText(event["summary"], 280, true) || strings.ContainsAny(workString(event["summary"]), "\r\n") {
		return workInvalid("run summary must be one line of 1..280 characters")
	}
	p, ok := event["payload"].(map[string]any)
	if !ok {
		return workInvalid("run payload required")
	}
	if err := loopKeys(p, "run_id", "outcome", "started_at", "finished_at", "metrics", "experiment", "effect_refs"); err != nil {
		return err
	}
	if !loopText(p["run_id"], 200, true) {
		return workInvalid("run_id must have 1..200 characters")
	}
	switch p["outcome"] {
	case "acted", "measured", "no_op", "infra_error":
	default:
		return workInvalid("invalid run outcome")
	}
	for _, key := range []string{"started_at", "finished_at"} {
		if v, has := p[key]; has {
			ts, ok := v.(string)
			if !ok {
				return workInvalid("invalid %s", key)
			}
			if _, err := time.Parse(time.RFC3339Nano, ts); err != nil {
				return workInvalid("invalid %s", key)
			}
		}
	}
	if start, ok := p["started_at"].(string); ok {
		if end, ok := p["finished_at"].(string); ok {
			a, _ := time.Parse(time.RFC3339Nano, start)
			b, _ := time.Parse(time.RFC3339Nano, end)
			if a.After(b) {
				return workInvalid("started_at must precede finished_at")
			}
		}
	}
	if raw, has := p["metrics"]; has {
		m, ok := raw.(map[string]any)
		if !ok || len(m) > 16 {
			return workInvalid("metrics must be an object with at most 16 entries")
		}
		for k, v := range m {
			if !loopMetricName.MatchString(k) {
				return workInvalid("invalid metric name")
			}
			if _, ok := loopNumber(v); !ok {
				return workInvalid("metric values must be finite numbers")
			}
		}
	}
	if raw, has := p["experiment"]; has {
		e, ok := raw.(map[string]any)
		if !ok {
			return workInvalid("invalid experiment")
		}
		if err := loopKeys(e, "candidate", "decision", "score_before", "score_after"); err != nil {
			return err
		}
		if !loopText(e["candidate"], 200, true) {
			return workInvalid("experiment.candidate required")
		}
		switch e["decision"] {
		case "kept", "reverted", "rejected":
		default:
			return workInvalid("invalid experiment.decision")
		}
		for _, k := range []string{"score_before", "score_after"} {
			if v, has := e[k]; has {
				if _, ok := loopNumber(v); !ok {
					return workInvalid("%s must be finite", k)
				}
			}
		}
	}
	if raw, has := p["effect_refs"]; has {
		refs, err := normalizeStringSlice(raw)
		if err != nil || len(refs) > 10 {
			return workInvalid("effect_refs must have at most 10 refs")
		}
		for _, ref := range refs {
			if len(ref) > 2048 {
				return workInvalid("effect_refs entry too long")
			}
			link := ref
			if strings.HasPrefix(ref, "url:") {
				link = strings.TrimPrefix(ref, "url:")
			}
			if u, err := url.Parse(link); err == nil && (strings.EqualFold(u.Scheme, "http") || strings.EqualFold(u.Scheme, "https")) && u.Host != "" && u.User == nil {
				continue
			}
			kind, id, ok := splitTypedRef(ref)
			if !ok || id == "" {
				return workInvalid("invalid effect_refs entry: expected a typed ref or HTTP/HTTPS URL")
			}
			switch kind {
			case "actor", "artifact", "event", "thread", "topic", "document", "document_revision", "board", "card", "card_revision", "inbox", "host", "run", "series":
			default:
				return workInvalid("invalid effect_refs type %s: URLs require HTTP/HTTPS", fmt.Sprint(kind))
			}
		}
	}
	refs, err := normalizeStringSlice(event["refs"])
	if err != nil {
		return workInvalid("run requires card ref")
	}
	cards := 0
	for _, ref := range refs {
		if strings.HasPrefix(ref, "card:") {
			cards++
		}
	}
	if cards != 1 {
		return workInvalid("run requires exactly one subject card in refs")
	}
	return nil
}
