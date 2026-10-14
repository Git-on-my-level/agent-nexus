package visualreport

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"time"
)

type SeriesSource struct {
	Series string            `json:"series,omitempty"`
	Labels map[string]string `json:"labels,omitempty"`
	Range  string            `json:"range,omitempty"`
	Agg    string            `json:"agg,omitempty"`
	// Pivot binding, accepted only for live-table: one column per series and
	// one table row per distinct combination of the row-key label values.
	Columns []SeriesColumn `json:"columns,omitempty"`
	RowKeys []SeriesRowKey `json:"row_keys,omitempty"`
}

// SeriesColumn is one pivoted table column: a series read over the binding's
// range, reduced by Agg (default last) into the cell's single value.
type SeriesColumn struct {
	Series string `json:"series"`
	Label  string `json:"label,omitempty"`
	Agg    string `json:"agg,omitempty"`
}

// SeriesRowKey selects the label whose value names one table row; Label is the
// readable column header, defaulting to the label key itself.
type SeriesRowKey struct {
	Key   string `json:"key"`
	Label string `json:"label,omitempty"`
}

type SeriesFallback struct {
	AsOf string          `json:"as_of"`
	Data json.RawMessage `json:"data"`
}

// Pivot binding bounds. Row count mirrors the served cap in
// core/internal/server/report_series.go; columns stay within the twelve-column
// table limit once the row-key columns are counted.
const (
	MaxPivotColumns = 8
	MaxPivotRowKeys = 4
)

var seriesName = regexp.MustCompile(`^[a-z][a-z0-9_.-]{0,79}$`)
var seriesRange = regexp.MustCompile(`^([1-9][0-9]{0,5})(s|m|h|d)$`)

func SeriesRange(raw string) (time.Duration, error) {
	m := seriesRange.FindStringSubmatch(raw)
	if m == nil {
		return 0, fmt.Errorf("range must be a positive integer duration in s, m, h or d, up to 3650d")
	}
	n, _ := strconv.Atoi(m[1])
	units := map[string]time.Duration{"s": time.Second, "m": time.Minute, "h": time.Hour, "d": 24 * time.Hour}
	unit := units[m[2]]
	// Bound the integer before multiplication: overflowing time.Duration can
	// otherwise turn a huge positive range into a short or negative duration.
	if time.Duration(n) > (3650*24*time.Hour)/unit {
		return 0, fmt.Errorf("range exceeds 3650d")
	}
	return time.Duration(n) * unit, nil
}
func (v *validator) seriesBinding(panel map[string]any, path string, panelSources map[string]bool, reference func(any, string, map[string]bool)) {
	supported := map[string]bool{"live-timeline": true, "chart": true, "metric": true, "metric-strip": true, "table": true, "metric-chart": true, "evidence-table": true, "live-table": true}
	if !supported[fmt.Sprint(panel["type"])] {
		v.add(path+".source", "only charts, metrics, metric-strips and tables bind series")
	}
	if panel["type"] == "live-table" {
		v.pivotBinding(panel, path)
	} else {
		v.singleBinding(panel, path)
	}
	if data, ok := panel["data"].(map[string]any); !ok || len(data) != 0 {
		v.add(path+".data", "bound panels use an empty data object; put snapshots in fallback")
	}
	if raw, exists := panel["fallback"]; exists {
		if panel["type"] == "live-timeline" {
			v.add(path+".fallback", "live-timeline does not accept a static fallback")
			return
		}
		f, ok := object(raw, path+".fallback", []string{"as_of", "data"}, nil, v.add)
		if ok {
			v.timestamp(f["as_of"], path+".fallback.as_of", false)
			snapshot := map[string]any{}
			for k, value := range panel {
				snapshot[k] = value
			}
			delete(snapshot, "source")
			delete(snapshot, "fallback")
			snapshot["data"] = f["data"]
			v.panelData(snapshot, path+".fallback", panelSources, reference)
		}
	}
}

// singleBinding validates the one-series shape shared by charts, metrics,
// strips and point tables.
func (v *validator) singleBinding(panel map[string]any, path string) {
	source, ok := object(panel["source"], path+".source", []string{"series"}, []string{"labels", "range", "agg"}, v.add)
	if !ok {
		return
	}
	if name, ok := source["series"].(string); !ok || !seriesName.MatchString(name) {
		v.add(path+".source.series", "must be a series name")
	}
	if raw, exists := source["range"]; exists {
		s, ok := raw.(string)
		if _, err := SeriesRange(s); !ok || err != nil {
			v.add(path+".source.range", "must be a bounded duration")
		}
	}
	if panel["type"] == "live-timeline" {
		if _, exists := source["agg"]; exists {
			v.add(path+".source.agg", "live-timeline preserves observations and does not accept aggregation")
		}
		if raw, exists := source["range"].(string); exists {
			if d, err := SeriesRange(raw); err == nil && d > 90*24*time.Hour {
				v.add(path+".source.range", "live-timeline range must not exceed 90d")
			}
		}
	}
	if raw, exists := source["agg"]; exists {
		v.enum(raw, path+".source.agg", "last", "avg", "sum", "min", "max", "count")
	}
	v.sourceLabels(source, path)
}

// pivotBinding validates the live-table shape: several series columns reduced
// into one cell each, and the label keys whose values name the rows.
func (v *validator) pivotBinding(panel map[string]any, path string) {
	source, ok := object(panel["source"], path+".source", []string{"columns", "row_keys"}, []string{"labels", "range"}, v.add)
	if !ok {
		return
	}
	if raw, exists := source["range"]; exists {
		s, ok := raw.(string)
		if _, err := SeriesRange(s); !ok || err != nil {
			v.add(path+".source.range", "must be a bounded duration")
		}
	}
	v.sourceLabels(source, path)
	columns := v.arr(source["columns"], path+".source.columns", MaxPivotColumns, 1)
	seenSeries := map[string]bool{}
	for i, raw := range columns {
		p := fmt.Sprintf("%s.source.columns[%d]", path, i)
		column, ok := object(raw, p, []string{"series"}, []string{"label", "agg"}, v.add)
		if !ok {
			continue
		}
		if name, ok := column["series"].(string); !ok || !seriesName.MatchString(name) {
			v.add(p+".series", "must be a series name")
		} else if seenSeries[name+fmt.Sprint(column["agg"])] {
			v.add(p, "must be unique per series and aggregation")
		} else {
			seenSeries[name+fmt.Sprint(column["agg"])] = true
		}
		if label, exists := column["label"]; exists {
			v.text(label, p+".label", 200, false)
		}
		if raw, exists := column["agg"]; exists {
			v.enum(raw, p+".agg", "last", "avg", "sum", "min", "max", "count")
		}
	}
	keys := v.arr(source["row_keys"], path+".source.row_keys", MaxPivotRowKeys, 1)
	seenKeys := map[string]bool{}
	for i, raw := range keys {
		p := fmt.Sprintf("%s.source.row_keys[%d]", path, i)
		key, ok := object(raw, p, []string{"key"}, []string{"label"}, v.add)
		if !ok {
			continue
		}
		if name, ok := key["key"].(string); !ok || !seriesName.MatchString(name) {
			v.add(p+".key", "must be a label key")
		} else if seenKeys[name] {
			v.add(p+".key", "must be unique")
		} else {
			seenKeys[name] = true
		}
		if label, exists := key["label"]; exists {
			v.text(label, p+".label", 200, false)
		}
	}
}

func (v *validator) sourceLabels(source map[string]any, path string) {
	if raw, exists := source["labels"]; exists {
		labels, ok := raw.(map[string]any)
		if !ok || len(labels) > 8 {
			v.add(path+".source.labels", "must have at most 8 labels")
		} else {
			for key, value := range labels {
				s, ok := value.(string)
				if !seriesName.MatchString(key) || !ok || len(s) > 128 {
					v.add(path+".source.labels", "contains an invalid label")
				}
			}
		}
	}
}
func (v *validator) metric(data any, path string) {
	m, ok := object(data, path, []string{"value"}, []string{"unit"}, v.add)
	if !ok {
		return
	}
	if n, ok := numeric(m["value"]); ok {
		if math.Abs(n) > 1e12 {
			v.add(path+".value", "exceeds numeric bounds")
		}
	} else {
		v.text(m["value"], path+".value", 128, false)
	}
	if value, exists := m["unit"]; exists {
		v.text(value, path+".unit", 80, true)
	}
}
