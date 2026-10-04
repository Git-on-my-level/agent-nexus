package plans

const MaxTileNodes = 24

type GeometryNode struct {
	ID     string   `json:"id"`
	Status string   `json:"status"`
	Layer  int      `json:"layer"`
	After  []string `json:"after"`
}
type Geometry struct {
	Shape          string         `json:"shape"`
	Nodes          []GeometryNode `json:"nodes"`
	TotalNodes     int            `json:"total_nodes"`
	CollapsedNodes int            `json:"collapsed_nodes"`
}

// TileGeometry consumes effective state, so clients never infer workflow or
// dependency semantics. Deterministic topological order preserves dependencies.
func TileGeometry(p Plan, state State) Geometry {
	out := Geometry{Shape: state.Shape, Nodes: []GeometryNode{}, TotalNodes: len(p.Steps)}
	steps, statuses := map[string]Step{}, map[string]string{}
	for _, step := range p.Steps {
		steps[step.ID] = step
	}
	for _, step := range state.Steps {
		statuses[step.ID] = step.Status
	}
	layers, included := map[string]int{}, map[string]bool{}
	for _, id := range order(p) {
		step := steps[id]
		for _, dep := range step.After {
			if layers[dep]+1 > layers[id] {
				layers[id] = layers[dep] + 1
			}
		}
		if len(out.Nodes) >= MaxTileNodes {
			continue
		}
		after := []string{}
		for _, dep := range step.After {
			if included[dep] {
				after = append(after, dep)
			}
		}
		out.Nodes = append(out.Nodes, GeometryNode{ID: id, Status: statuses[id], Layer: layers[id], After: after})
		included[id] = true
	}
	out.CollapsedNodes = out.TotalNodes - len(out.Nodes)
	return out
}
