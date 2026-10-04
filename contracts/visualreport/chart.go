package visualreport

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

var (
	axisFormat    = regexp.MustCompile(`^[^{}<>]{0,12}\{value\}[^{}<>]{0,12}$`)
	percentFormat = regexp.MustCompile(`^(?:100|[0-9]{1,2})(?:\.[0-9])?%$`)
)

var chartTypes = []string{"line", "bar", "scatter", "pie", "heatmap", "graph", "sankey", "treemap"}

func chartNumber(value any, min, max float64) bool {
	n, ok := numeric(value)
	if !ok || n < min || n > max {
		return false
	}
	return n == 0 || math.Abs(n) >= 1e-100
}
func chartEpoch(value any) bool { return chartNumber(value, -8.64e15, 8.64e15) }
func (v *validator) chart(raw any, path string) {
	d, ok := object(raw, path, []string{"option"}, []string{"caption", "palette"}, v.add)
	if !ok {
		return
	}
	if x, ok := d["caption"]; ok {
		v.text(x, path+".caption", 2000, true)
	}
	if x, ok := d["palette"]; ok {
		v.enum(x, path+".palette", "ocean", "forest", "sunset", "categorical")
	}
	op := path + ".option"
	option, ok := object(d["option"], op, []string{"series"}, []string{"xAxis", "yAxis", "legend"}, v.add)
	if !ok {
		return
	}
	if legend, exists := option["legend"]; exists {
		if lm, good := object(legend, op+".legend", nil, []string{"show"}, v.add); good {
			if x, ok := lm["show"]; ok {
				if _, good := x.(bool); !good {
					v.add(op+".legend.show", "must be a boolean")
				}
			}
		}
	}
	axes := map[string][]any{}
	for _, dim := range []string{"xAxis", "yAxis"} {
		rawAxis, exists := option[dim]
		if !exists {
			axes[dim] = []any{}
			continue
		}
		if list, ok := rawAxis.([]any); ok {
			axes[dim] = list
		} else {
			axes[dim] = []any{rawAxis}
		}
		if len(axes[dim]) > 2 {
			v.add(op+"."+dim, "must be an array with 0–2 items")
			axes[dim] = []any{}
		}
		for i, raw := range axes[dim] {
			p := fmt.Sprintf("%s.%s[%d]", op, dim, i)
			a, ok := object(raw, p, []string{"type"}, []string{"name", "data", "position", "inverse", "axisLabel", "min", "max", "scale"}, v.add)
			if !ok {
				continue
			}
			v.enum(a["type"], p+".type", "category", "value", "time")
			if x, ok := a["name"]; ok {
				v.text(x, p+".name", 200, true)
			}
			if x, ok := a["position"]; ok {
				if dim == "xAxis" {
					v.enum(x, p+".position", "top", "bottom")
				} else {
					v.enum(x, p+".position", "left", "right")
				}
			}
			if x, ok := a["inverse"]; ok {
				if _, good := x.(bool); !good {
					v.add(p+".inverse", "must be a boolean")
				}
			}
			typ, _ := a["type"].(string)
			if typ == "category" {
				values := v.arr(a["data"], p+".data", 200, 1)
				seen := map[string]bool{}
				for j, x := range values {
					q := fmt.Sprintf("%s.data[%d]", p, j)
					v.category(x, q)
					key := fmt.Sprint(x)
					if seen[key] {
						v.add(p+".data", "category labels must be unique")
					}
					seen[key] = true
				}
				for _, key := range []string{"min", "max", "scale"} {
					if _, ok := a[key]; ok {
						v.add(p, "category axes do not accept min, max or scale")
						break
					}
				}
			} else {
				if _, ok := a["data"]; ok {
					v.add(p+".data", "only category axes accept data")
				}
				for _, key := range []string{"min", "max"} {
					if x, ok := a[key]; ok {
						if typ == "time" && !chartEpoch(x) || typ == "value" && !chartNumber(x, -1e12, 1e12) {
							v.add(p+"."+key, "must be a finite number in the supported range")
						}
					}
				}
				if x, ok := a["scale"]; ok {
					if _, good := x.(bool); !good {
						v.add(p+".scale", "must be a boolean")
					}
				}
				if lo, aok := numeric(a["min"]); aok {
					if hi, bok := numeric(a["max"]); bok && lo >= hi {
						v.add(p, "min must be smaller than max")
					}
				}
			}
			if rawLabel, ok := a["axisLabel"]; ok {
				lp := p + ".axisLabel"
				label, good := object(rawLabel, lp, nil, []string{"show", "rotate", "formatter"}, v.add)
				if good {
					if x, ok := label["show"]; ok {
						if _, good := x.(bool); !good {
							v.add(lp+".show", "must be a boolean")
						}
					}
					if x, ok := label["rotate"]; ok {
						n, good := numeric(x)
						if !good || n < -90 || n > 90 {
							v.add(lp+".rotate", "must be a finite number in the supported range")
						}
					}
					if x, ok := label["formatter"]; ok {
						if typ != "value" {
							v.add(lp+".formatter", "label formats are supported on value axes only")
						} else if s, ok := x.(string); !ok || !axisFormat.MatchString(s) {
							v.add(lp+".formatter", "must be literal text around a single {value}")
						}
					}
				}
			}
		}
	}
	series := v.arr(option["series"], op+".series", 12, 1)
	total := 0
	names := map[string]bool{}
	for i, raw := range series {
		p := fmt.Sprintf("%s.series[%d]", op, i)
		item, ok := raw.(map[string]any)
		if !ok {
			v.add(p, "must be a plain object")
			continue
		}
		typ, _ := item["type"].(string)
		v.enum(item["type"], p+".type", chartTypes...)
		supported := map[string][]string{"line": {"xAxisIndex", "yAxisIndex", "stack", "smooth", "step", "areaStyle", "symbolSize", "markLine"}, "bar": {"xAxisIndex", "yAxisIndex", "stack", "barWidth", "markLine"}, "scatter": {"xAxisIndex", "yAxisIndex", "symbolSize", "markLine"}, "pie": {"radius", "center", "roseType"}, "heatmap": {"xAxisIndex", "yAxisIndex"}, "graph": {"layout", "links", "symbolSize", "categories"}, "sankey": {"links", "orient"}}
		item, ok = object(item, p, []string{"type", "name", "data"}, supported[typ], v.add)
		if !ok {
			continue
		}
		v.text(item["name"], p+".name", 200, false)
		name, _ := item["name"].(string)
		if names[name] {
			v.add(p+".name", "series names must be unique")
		}
		names[name] = true
		if x, ok := item["stack"]; ok {
			v.text(x, p+".stack", 200, false)
		}
		if x, ok := item["smooth"]; ok {
			if _, good := x.(bool); !good {
				v.add(p+".smooth", "must be a boolean")
			}
		}
		if x, ok := item["step"]; ok {
			v.enum(x, p+".step", "start", "middle", "end")
		}
		if x, ok := item["symbolSize"]; ok {
			v.chartNum(x, p+".symbolSize", 2, 40)
		}
		if x, ok := item["barWidth"]; ok {
			v.chartNum(x, p+".barWidth", 1, 80)
		}
		if x, ok := item["areaStyle"]; ok {
			object(x, p+".areaStyle", nil, nil, v.add)
		}
		maxPoints := 200
		if typ == "graph" || typ == "sankey" {
			maxPoints = 120
		}
		points := v.arr(item["data"], p+".data", maxPoints, 1)
		total += len(points)
		if contains([]string{"line", "bar", "scatter", "heatmap"}, typ) {
			v.chartCartesian(item, p, typ, points, axes)
		} else if typ == "pie" {
			v.chartPie(item, p, points)
		} else if typ == "graph" || typ == "sankey" {
			v.chartNetwork(item, p, typ, points)
		} else if typ == "treemap" {
			count := 0
			for i, point := range points {
				v.treeNode(point, fmt.Sprintf("%s.data[%d]", p, i), 1, &count)
			}
			total += count - len(points)
		}
	}
	if total > 1200 {
		v.add(op+".series", "exceeds the total point limit")
	}
	hasComplex, hasPie := false, false
	for _, s := range series {
		m, _ := s.(map[string]any)
		t, _ := m["type"].(string)
		if contains([]string{"graph", "sankey", "treemap", "heatmap"}, t) {
			hasComplex = true
		}
		if t == "pie" {
			hasPie = true
		}
	}
	if hasComplex && len(series) != 1 {
		v.add(op+".series", "graph, sankey, treemap and heatmap charts require a single series")
	}
	if hasPie {
		for _, s := range series {
			m, _ := s.(map[string]any)
			if m["type"] != "pie" {
				v.add(op+".series", "pie series cannot share a plot with other series types")
				break
			}
		}
	}
}

func (v *validator) category(value any, path string) {
	if s, ok := value.(string); ok {
		v.text(s, path, 200, false)
	} else if !chartNumber(value, -1e12, 1e12) {
		v.add(path, "must be a finite number in the supported range")
	}
}
func (v *validator) chartNum(x any, p string, min, max float64) {
	if !chartNumber(x, min, max) {
		v.add(p, "must be a finite number in the supported range")
	}
}
func contains(items []string, value string) bool {
	for _, item := range items {
		if item == value {
			return true
		}
	}
	return false
}
func (v *validator) chartCartesian(item map[string]any, path, typ string, points []any, axes map[string][]any) {
	selected := make([]map[string]any, 2)
	for i, dim := range []string{"xAxis", "yAxis"} {
		idx := 0
		if x, ok := numeric(item[dim+"Index"]); ok {
			idx = int(x)
			if x != float64(idx) || idx < 0 || idx > 1 {
				v.add(path+"."+dim+"Index", "must be 0 or 1")
			}
		} else if _, ok := item[dim+"Index"]; ok {
			v.add(path+"."+dim+"Index", "must be 0 or 1")
		}
		list := axes[dim]
		if item[dim+"Index"] == nil {
			idx = 0
		}
		if idx >= 0 && idx < len(list) {
			selected[i], _ = list[idx].(map[string]any)
		}
		if selected[i] == nil {
			v.add(path, "cartesian series require matching xAxis and yAxis")
		}
	}
	coordinate := func(x any, axis map[string]any, p string) {
		if x == nil {
			return
		}
		typ, _ := axis["type"].(string)
		if typ == "category" {
			v.category(x, p)
			if data, ok := axis["data"].([]any); ok {
				found := false
				for _, candidate := range data {
					if fmt.Sprint(candidate) == fmt.Sprint(x) {
						found = true
					}
				}
				if !found {
					v.add(p, "must match a category label")
				}
			}
		} else if typ == "time" {
			if !chartEpoch(x) {
				v.add(p, "time coordinates must be valid epoch milliseconds")
			}
		} else if !chartNumber(x, -1e12, 1e12) {
			v.add(p, "must be a finite number in the supported range")
		}
	}
	if typ == "heatmap" {
		if selected[0] == nil || selected[1] == nil || selected[0]["type"] != "category" || selected[1]["type"] != "category" {
			v.add(path, "heatmaps require two category axes")
		}
		seen := map[string]bool{}
		for i, raw := range points {
			p := fmt.Sprintf("%s.data[%d]", path, i)
			tuple, ok := raw.([]any)
			if !ok || len(tuple) != 3 {
				v.add(p, "must be a column, row, value tuple")
				continue
			}
			for k := 0; k < 2; k++ {
				n, ok := numeric(tuple[k])
				length := 0
				if data, good := selected[k]["data"].([]any); good {
					length = len(data)
				}
				if !ok || n != float64(int(n)) || n < 0 || n >= float64(length) {
					v.add(p, "cell indices must match the category axes")
				}
			}
			if tuple[2] != nil && !chartNumber(tuple[2], -1e12, 1e12) {
				v.add(p, "must be a finite number in the supported range")
			}
			key := fmt.Sprint(tuple[0]) + "," + fmt.Sprint(tuple[1])
			if seen[key] {
				v.add(p, "heatmap cells must be unique")
			}
			seen[key] = true
		}
		return
	}
	categorical, categoryCount := -1, 0
	for i, a := range selected {
		if a["type"] == "category" {
			categorical = i
			categoryCount++
		}
	}
	for i, raw := range points {
		p := fmt.Sprintf("%s.data[%d]", path, i)
		if tuple, ok := raw.([]any); ok {
			if len(tuple) != 2 {
				v.add(p, "must be an x, y tuple")
			} else {
				for k, x := range tuple {
					coordinate(x, selected[k], fmt.Sprintf("%s[%d]", p, k))
				}
			}
		} else {
			if raw != nil && !chartNumber(raw, -1e12, 1e12) {
				v.add(p, "must be a finite number in the supported range")
			}
			if categoryCount != 1 {
				v.add(p, "scalar values require exactly one category axis")
			} else if i >= axisDataLen(selected[categorical]) {
				v.add(p, "has no matching axis category")
			}
		}
	}
	if _, ok := item["stack"]; ok && categoryCount != 1 {
		v.add(path, "stacked series require exactly one category axis")
	}
	if raw, ok := item["markLine"]; ok {
		m, good := object(raw, path+".markLine", []string{"data"}, nil, v.add)
		if good {
			for i, raw := range v.arr(m["data"], path+".markLine.data", 6, 1) {
				p := fmt.Sprintf("%s.markLine.data[%d]", path, i)
				line, good := object(raw, p, []string{"name"}, []string{"xAxis", "yAxis"}, v.add)
				if !good {
					continue
				}
				v.text(line["name"], p+".name", 200, false)
				dims := []string{}
				for _, d := range []string{"xAxis", "yAxis"} {
					if _, ok := line[d]; ok {
						dims = append(dims, d)
					}
				}
				if len(dims) != 1 {
					v.add(p, "reference lines require exactly one of xAxis or yAxis")
					continue
				}
				axis := selected[0]
				if dims[0] == "yAxis" {
					axis = selected[1]
				}
				axisType, _ := axis["type"].(string)
				if axisType == "value" {
					v.chartNum(line[dims[0]], p, -1e12, 1e12)
				} else if axisType == "time" {
					if !chartEpoch(line[dims[0]]) {
						v.add(p, "time coordinates must be valid epoch milliseconds")
					}
				} else {
					v.add(p, "reference lines require a value or time axis")
				}
			}
		}
	}
}
func axisDataLen(axis map[string]any) int {
	if data, ok := axis["data"].([]any); ok {
		return len(data)
	}
	return 0
}

func (v *validator) chartPie(item map[string]any, path string, points []any) {
	for i, raw := range points {
		p := fmt.Sprintf("%s.data[%d]", path, i)
		point, ok := object(raw, p, []string{"name", "value"}, nil, v.add)
		if ok {
			v.text(point["name"], p+".name", 200, false)
			v.chartNum(point["value"], p+".value", 0, 1e12)
		}
	}
	if x, ok := item["radius"]; ok {
		if list, ok := x.([]any); ok {
			if len(list) != 2 {
				v.add(path+".radius", "must be an array with 2–2 items")
			} else {
				for _, val := range list {
					v.percent(val, path+".radius")
				}
				a, _ := percentValue(list[0])
				b, _ := percentValue(list[1])
				if a >= b {
					v.add(path+".radius", "inner radius must be smaller than outer radius")
				}
			}
		} else {
			v.percent(x, path+".radius")
		}
	}
	if x, ok := item["center"]; ok {
		if list, ok := x.([]any); !ok || len(list) != 2 {
			v.add(path+".center", "must be an array with 2–2 items")
		} else {
			for _, val := range list {
				v.percent(val, path+".center")
			}
		}
	}
	if x, ok := item["roseType"]; ok {
		v.enum(x, path+".roseType", "radius", "area")
	}
}
func (v *validator) percent(value any, path string) {
	s, ok := value.(string)
	if !ok || !percentFormat.MatchString(s) {
		v.add(path, "must be a percentage from 0% to 100%")
		return
	}
	n, _ := strconv.ParseFloat(strings.TrimSuffix(s, "%"), 64)
	if n > 100 {
		v.add(path, "must be a percentage from 0% to 100%")
	}
}
func percentValue(value any) (float64, bool) {
	s, ok := value.(string)
	if !ok {
		return 0, false
	}
	n, err := strconv.ParseFloat(strings.TrimSuffix(s, "%"), 64)
	return n, err == nil
}

func (v *validator) chartNetwork(item map[string]any, path, typ string, points []any) {
	graph := typ == "graph"
	if graph {
		v.enum(item["layout"], path+".layout", "circular", "none")
	}
	categories := []any{}
	if graph {
		if raw, ok := item["categories"]; ok {
			categories = v.arr(raw, path+".categories", 12, 1)
		}
	}
	categoryNames, ids := map[string]bool{}, map[string]bool{}
	for i, raw := range categories {
		p := fmt.Sprintf("%s.categories[%d]", path, i)
		c, ok := object(raw, p, []string{"name"}, nil, v.add)
		if !ok {
			continue
		}
		v.text(c["name"], p+".name", 200, false)
		name, _ := c["name"].(string)
		if categoryNames[name] {
			v.add(p, "category names must be unique")
		}
		categoryNames[name] = true
	}
	if x, ok := item["orient"]; ok {
		v.enum(x, path+".orient", "horizontal", "vertical")
	}
	for i, raw := range points {
		p := fmt.Sprintf("%s.data[%d]", path, i)
		required := []string{"name"}
		optional := []string{}
		if graph {
			required = []string{"id", "name"}
			optional = []string{"value", "x", "y", "symbolSize", "category"}
		}
		node, ok := object(raw, p, required, optional, v.add)
		if !ok {
			continue
		}
		v.text(node["name"], p+".name", 200, false)
		key, _ := node["name"].(string)
		if graph {
			v.text(node["id"], p+".id", 200, false)
			key, _ = node["id"].(string)
		}
		if ids[key] {
			v.add(p, "nodes must have unique identifiers")
		}
		ids[key] = true
		for _, f := range []string{"value", "x", "y"} {
			if x, ok := node[f]; ok {
				v.chartNum(x, p+"."+f, -1e12, 1e12)
			}
		}
		if x, ok := node["symbolSize"]; ok {
			v.chartNum(x, p+".symbolSize", 2, 40)
		}
		if x, ok := node["category"]; ok {
			n, ok := numeric(x)
			if !ok || n != float64(int(n)) || n < 0 || n >= float64(len(categories)) {
				v.add(p+".category", "must index a declared category")
			}
		}
		if graph && item["layout"] == "none" {
			if !chartNumber(node["x"], -1e12, 1e12) || !chartNumber(node["y"], -1e12, 1e12) {
				v.add(p, "fixed graph nodes require x and y coordinates")
			}
		}
	}
	links := v.arr(item["links"], path+".links", 240, 0)
	incoming, outgoing := map[string]int{}, map[string][]string{}
	for id := range ids {
		outgoing[id] = []string{}
	}
	for i, raw := range links {
		p := fmt.Sprintf("%s.links[%d]", path, i)
		req := []string{"source", "target"}
		opt := []string{"value"}
		if !graph {
			req = append(req, "value")
			opt = nil
		}
		link, ok := object(raw, p, req, opt, v.add)
		if !ok {
			continue
		}
		v.text(link["source"], p+".source", 200, false)
		v.text(link["target"], p+".target", 200, false)
		source, _ := link["source"].(string)
		target, _ := link["target"].(string)
		if !ids[source] || !ids[target] {
			v.add(p, "links must reference existing nodes")
		}
		if x, ok := link["value"]; ok {
			v.chartNum(x, p+".value", 0, 1e12)
		}
		if _, ok := outgoing[source]; ok && ids[target] {
			outgoing[source] = append(outgoing[source], target)
			incoming[target]++
		}
	}
	if !graph {
		positive := false
		for _, raw := range links {
			m, _ := raw.(map[string]any)
			n, ok := numeric(m["value"])
			if ok && n > 0 {
				positive = true
			}
		}
		if !positive {
			v.add(path, "sankey charts require at least one positive flow")
		}
		queue := []string{}
		for id := range ids {
			if incoming[id] == 0 {
				queue = append(queue, id)
			}
		}
		visited := 0
		for len(queue) > 0 {
			id := queue[len(queue)-1]
			queue = queue[:len(queue)-1]
			visited++
			for _, next := range outgoing[id] {
				incoming[next]--
				if incoming[next] == 0 {
					queue = append(queue, next)
				}
			}
		}
		if visited != len(ids) {
			v.add(path, "sankey links must not contain cycles")
		}
	}
}

func (v *validator) treeNode(raw any, path string, depth int, count *int) {
	*count++
	if *count > 200 || depth > 5 {
		v.add(path, "tree exceeds the node or depth limit")
		return
	}
	node, ok := object(raw, path, []string{"name"}, []string{"value", "children"}, v.add)
	if !ok {
		return
	}
	v.text(node["name"], path+".name", 200, false)
	if x, ok := node["value"]; ok {
		v.chartNum(x, path+".value", 0, 1e12)
	}
	if rawChildren, ok := node["children"]; ok {
		if _, has := node["value"]; has {
			v.add(path, "branch values are derived from their children")
		}
		for i, child := range v.arr(rawChildren, path+".children", 200, 1) {
			v.treeNode(child, fmt.Sprintf("%s.children[%d]", path, i), depth+1, count)
		}
	} else if !chartNumber(node["value"], 0, 1e12) {
		v.add(path, "leaf nodes require a numeric value")
	}
}
