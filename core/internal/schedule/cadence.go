package schedule

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

const (
	CadenceReactive = "reactive"
	CadenceDaily    = "daily"
	CadenceWeekly   = "weekly"
	CadenceMonthly  = "monthly"
	CadenceCustom   = "custom"

	CanonicalDailyCron   = "0 9 * * *"
	CanonicalWeeklyCron  = "0 9 * * 1"
	CanonicalMonthlyCron = "0 9 1 * *"
)

const cronPreviousRunLookback = 6 * 366 * 24 * time.Hour

type parsedCron struct {
	minute cronField
	hour   cronField
	dom    cronField
	month  cronField
	dow    cronField
}

type cronField struct {
	wildcard bool
	allowed  map[int]struct{}
}

func NormalizeCadence(value string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
}

func IsReactiveCadence(value string) bool {
	normalized := NormalizeCadence(value)
	return normalized == "" || normalized == CadenceReactive
}

func IsLegacyCadence(value string) bool {
	switch NormalizeCadence(value) {
	case CadenceDaily, CadenceWeekly, CadenceMonthly, CadenceCustom:
		return true
	default:
		return false
	}
}

func IsCronCadence(value string) bool {
	normalized := NormalizeCadence(value)
	if normalized == "" || normalized == CadenceReactive || IsLegacyCadence(normalized) {
		return false
	}
	_, err := parseCronExpression(normalized)
	return err == nil
}

func ValidateCadence(value string) error {
	normalized := NormalizeCadence(value)
	if normalized == "" {
		return fmt.Errorf("must be non-empty")
	}
	if normalized == CadenceReactive || IsLegacyCadence(normalized) {
		return nil
	}
	if _, err := parseCronExpression(normalized); err != nil {
		return fmt.Errorf("must be \"reactive\", a legacy preset (daily/weekly/monthly/custom), or a valid 5-field cron expression")
	}
	return nil
}

func CadencePreset(value string) string {
	normalized := NormalizeCadence(value)
	switch normalized {
	case "", CadenceReactive:
		return CadenceReactive
	case CadenceDaily, CanonicalDailyCron:
		return CadenceDaily
	case CadenceWeekly, CanonicalWeeklyCron:
		return CadenceWeekly
	case CadenceMonthly, CanonicalMonthlyCron:
		return CadenceMonthly
	case CadenceCustom:
		return CadenceCustom
	default:
		if IsCronCadence(normalized) {
			return CadenceCustom
		}
		return ""
	}
}

func CadenceMatchesFilter(threadCadence string, filterCadence string) bool {
	filter := NormalizeCadence(filterCadence)
	if filter == "" {
		return true
	}

	threadValue := NormalizeCadence(threadCadence)

	if filter == CadenceCustom {
		return CadencePreset(threadValue) == CadenceCustom
	}

	if IsCronCadence(filter) {
		if threadValue == filter {
			return true
		}
		filterPreset := CadencePreset(filter)
		if filterPreset == CadenceCustom {
			return false
		}
		return CadencePreset(threadValue) == filterPreset
	}

	filterPreset := CadencePreset(filter)
	if filterPreset == "" {
		return false
	}

	return CadencePreset(threadValue) == filterPreset
}

func PreviousCronRun(value string, now time.Time) (time.Time, bool) {
	cadence := NormalizeCadence(value)
	if !IsCronCadence(cadence) {
		return time.Time{}, false
	}

	parsed, err := parseCronExpression(cadence)
	if err != nil {
		return time.Time{}, false
	}

	nowUTC := now.UTC()
	candidate := nowUTC.Truncate(time.Minute)
	if !candidate.Before(nowUTC) {
		candidate = candidate.Add(-time.Minute)
	}

	earliest := candidate.Add(-cronPreviousRunLookback)
	for !candidate.Before(earliest) {
		if parsed.matches(candidate) {
			return candidate, true
		}
		candidate = candidate.Add(-time.Minute)
	}

	return time.Time{}, false
}

func parseCronExpression(expr string) (*parsedCron, error) {
	fields := strings.Fields(NormalizeCadence(expr))
	if len(fields) != 5 {
		return nil, fmt.Errorf("cron expression must contain exactly 5 fields")
	}

	minute, err := parseCronField(fields[0], 0, 59, false)
	if err != nil {
		return nil, fmt.Errorf("invalid minute field: %w", err)
	}
	hour, err := parseCronField(fields[1], 0, 23, false)
	if err != nil {
		return nil, fmt.Errorf("invalid hour field: %w", err)
	}
	dom, err := parseCronField(fields[2], 1, 31, false)
	if err != nil {
		return nil, fmt.Errorf("invalid day-of-month field: %w", err)
	}
	month, err := parseCronField(fields[3], 1, 12, false)
	if err != nil {
		return nil, fmt.Errorf("invalid month field: %w", err)
	}
	dow, err := parseCronField(fields[4], 0, 7, true)
	if err != nil {
		return nil, fmt.Errorf("invalid day-of-week field: %w", err)
	}

	return &parsedCron{
		minute: minute,
		hour:   hour,
		dom:    dom,
		month:  month,
		dow:    dow,
	}, nil
}

func parseCronField(field string, minValue int, maxValue int, mapSevenToZero bool) (cronField, error) {
	input := strings.TrimSpace(field)
	if input == "" {
		return cronField{}, fmt.Errorf("field cannot be empty")
	}

	result := cronField{
		wildcard: input == "*",
		allowed:  map[int]struct{}{},
	}

	parts := strings.Split(input, ",")
	for _, part := range parts {
		token := strings.TrimSpace(part)
		if token == "" {
			return cronField{}, fmt.Errorf("empty token")
		}

		base := token
		step := 1
		if strings.Contains(token, "/") {
			stepParts := strings.Split(token, "/")
			if len(stepParts) != 2 {
				return cronField{}, fmt.Errorf("invalid step token %q", token)
			}
			base = strings.TrimSpace(stepParts[0])
			stepValue, err := strconv.Atoi(strings.TrimSpace(stepParts[1]))
			if err != nil || stepValue <= 0 {
				return cronField{}, fmt.Errorf("invalid step in token %q", token)
			}
			step = stepValue
		}

		rangeStart := minValue
		rangeEnd := maxValue
		switch {
		case base == "*":
			// Use full range.
		case strings.Contains(base, "-"):
			bounds := strings.Split(base, "-")
			if len(bounds) != 2 {
				return cronField{}, fmt.Errorf("invalid range token %q", token)
			}

			start, err := strconv.Atoi(strings.TrimSpace(bounds[0]))
			if err != nil {
				return cronField{}, fmt.Errorf("invalid range start in token %q", token)
			}
			end, err := strconv.Atoi(strings.TrimSpace(bounds[1]))
			if err != nil {
				return cronField{}, fmt.Errorf("invalid range end in token %q", token)
			}
			if start > end {
				return cronField{}, fmt.Errorf("range start must be <= end in token %q", token)
			}
			rangeStart = start
			rangeEnd = end
		default:
			value, err := strconv.Atoi(base)
			if err != nil {
				return cronField{}, fmt.Errorf("invalid value token %q", token)
			}
			if step != 1 {
				return cronField{}, fmt.Errorf("step requires * or range in token %q", token)
			}
			rangeStart = value
			rangeEnd = value
		}

		for number := rangeStart; number <= rangeEnd; number += step {
			if number < minValue || number > maxValue {
				return cronField{}, fmt.Errorf("value %d outside allowed range %d-%d", number, minValue, maxValue)
			}

			normalized := number
			if mapSevenToZero && normalized == 7 {
				normalized = 0
			}

			result.allowed[normalized] = struct{}{}
		}
	}

	if len(result.allowed) == 0 {
		return cronField{}, fmt.Errorf("field has no allowed values")
	}

	return result, nil
}

func (c *parsedCron) matches(ts time.Time) bool {
	domMatches := c.dom.matches(ts.Day())
	dowMatches := c.dow.matches(int(ts.Weekday()))
	dayMatches := domMatches && dowMatches
	if !c.dom.wildcard && !c.dow.wildcard {
		dayMatches = domMatches || dowMatches
	}

	return c.minute.matches(ts.Minute()) &&
		c.hour.matches(ts.Hour()) &&
		c.month.matches(int(ts.Month())) &&
		dayMatches
}

func (f cronField) matches(value int) bool {
	_, ok := f.allowed[value]
	return ok
}
