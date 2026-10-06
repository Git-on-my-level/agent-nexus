package visualreport

import (
	"fmt"
	"strings"
	"time"
)

// ReviewDeadline resolves dates at midnight UTC and durations relative to the
// authored timestamp. Resolution never uses the reader's clock.
func ReviewDeadline(value string, authored time.Time) (time.Time, error) {
	if at, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return at.UTC(), nil
	}
	if at, err := time.Parse("2006-01-02", value); err == nil {
		return at, nil
	}
	if d, err := SeriesRange(value); err == nil {
		return authored.Add(d), nil
	}
	return time.Time{}, fmt.Errorf("review_by must be a UTC date, zoned timestamp or positive bounded duration")
}

func (v *validator) review(panel map[string]any, path string, generated any) {
	authored, _ := panel["authored_at"].(string)
	if raw, exists := panel["authored_at"]; exists {
		v.timestamp(raw, path+".authored_at", false)
	}
	if authored == "" {
		authored, _ = generated.(string)
	}
	base, _ := time.Parse(time.RFC3339Nano, authored)
	raw, exists := panel["review_by"]
	live := IsLive(fmt.Sprint(panel["type"])) || panel["source"] != nil
	if !exists {
		if !live && panel["authored_at"] != nil {
			v.add(path+".review_by", "is required for authored panels with authored_at")
		}
		return
	}
	value, ok := raw.(string)
	due, err := ReviewDeadline(value, base)
	if !ok || err != nil {
		v.add(path+".review_by", "must be a UTC date, zoned timestamp or positive bounded duration")
		return
	}
	if !base.IsZero() && !due.After(base) {
		v.add(path+".review_by", "must be after authored_at")
	}
}

// ValidateWrite preserves read compatibility for expired reports while rejecting
// explicitly expired deadlines when a new revision is submitted.
func ValidateWrite(content []byte, now time.Time) Result {
	result := Validate(content)
	if !result.Valid {
		return result
	}
	root := result.Report.(map[string]any)
	for i, raw := range root["panels"].([]any) {
		panel := raw.(map[string]any)
		value, exists := panel["review_by"].(string)
		if !exists {
			continue
		}
		authored, _ := panel["authored_at"].(string)
		if authored == "" {
			authored, _ = root["generated_at"].(string)
		}
		base, _ := time.Parse(time.RFC3339Nano, authored)
		due, _ := ReviewDeadline(value, base)
		if due.Before(now) && len(result.Errors) < MaxErrors {
			result.Errors = append(result.Errors, fmt.Sprintf("panels[%d].review_by: must not be in the past at write time", i))
		}
	}
	result.Valid = len(result.Errors) == 0
	return result
}

// Warnings nudge authors toward computed status without rejecting narrative.
func Warnings(content []byte) []string {
	result := Validate(content)
	if !result.Valid {
		return nil
	}
	warnings := []string{}
	for _, raw := range result.Report.(map[string]any)["panels"].([]any) {
		panel := raw.(map[string]any)
		kind, _ := panel["type"].(string)
		if IsLive(kind) || panel["source"] != nil {
			continue
		}
		alternative := ""
		data, _ := panel["data"].(map[string]any)
		switch kind {
		case "milestone-timeline":
			alternative = "live-timeline with an adapter-fed series, or live-initiatives for plan milestones"
		case "evidence-table", "table":
			for _, col := range slice(data["columns"]) {
				text := strings.ToLower(fmt.Sprint(col))
				if strings.Contains(text, "status") || strings.Contains(text, "state") || strings.Contains(text, "phase") {
					alternative = "live-cards"
				}
			}
		case "callout":
			text := strings.ToLower(fmt.Sprint(panel["title"]) + " " + fmt.Sprint(data["label"]) + " " + fmt.Sprint(data["text"]))
			if strings.Contains(text, "status") || strings.Contains(text, "state") || strings.Contains(text, "progress") || strings.Contains(text, "blocked") || data["tone"] == "warning" || data["tone"] == "critical" || data["tone"] == "success" {
				alternative = "live-initiatives or live-asks"
			}
		}
		if alternative != "" {
			warnings = append(warnings, fmt.Sprintf("Panel %s is authored status; use %s. If live data is missing, file sync work instead of maintaining status by hand.", panel["id"], alternative))
		}
	}
	return warnings
}

func slice(raw any) []any { items, _ := raw.([]any); return items }
