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
	Series string            `json:"series"`
	Labels map[string]string `json:"labels,omitempty"`
	Range  string            `json:"range,omitempty"`
	Agg    string            `json:"agg,omitempty"`
}
type SeriesFallback struct {
	AsOf string          `json:"as_of"`
	Data json.RawMessage `json:"data"`
}

var seriesName = regexp.MustCompile(`^[a-z][a-z0-9_.-]{0,79}$`)
var seriesRange = regexp.MustCompile(`^([1-9][0-9]{0,5})(s|m|h|d)$`)

func SeriesRange(raw string) (time.Duration, error) {
	m := seriesRange.FindStringSubmatch(raw)
	if m == nil {
		return 0, fmt.Errorf("range must be a positive integer duration in s, m, h or d, up to 3650d")
	}
	n, _ := strconv.Atoi(m[1])
	units := map[string]time.Duration{"s": time.Second, "m": time.Minute, "h": time.Hour, "d": 24 * time.Hour}
	d := time.Duration(n) * units[m[2]]
	if d > 3650*24*time.Hour {
		return 0, fmt.Errorf("range exceeds 3650d")
	}
	return d, nil
}
func (v *validator) seriesBinding(panel map[string]any, path string, panelSources map[string]bool, reference func(any, string, map[string]bool)) {
	supported := map[string]bool{"chart": true, "metric": true, "metric-strip": true, "table": true, "metric-chart": true, "evidence-table": true}
	if !supported[fmt.Sprint(panel["type"])] {
		v.add(path+".source", "only charts, metrics, metric-strips and tables bind series")
	}
	source, ok := object(panel["source"], path+".source", []string{"series"}, []string{"labels", "range", "agg"}, v.add)
	if ok {
		if name, ok := source["series"].(string); !ok || !seriesName.MatchString(name) {
			v.add(path+".source.series", "must be a series name")
		}
		if raw, exists := source["range"]; exists {
			s, ok := raw.(string)
			if _, err := SeriesRange(s); !ok || err != nil {
				v.add(path+".source.range", "must be a bounded duration")
			}
		}
		if raw, exists := source["agg"]; exists {
			v.enum(raw, path+".source.agg", "last", "avg", "sum", "min", "max", "count")
		}
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
	if data, ok := panel["data"].(map[string]any); !ok || len(data) != 0 {
		v.add(path+".data", "bound panels use an empty data object; put snapshots in fallback")
	}
	if raw, exists := panel["fallback"]; exists {
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
