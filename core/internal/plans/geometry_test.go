package plans

import (
	"fmt"
	"testing"
	"time"
)

func TestTileGeometryBoundsDepthAndEffectiveStatus(t *testing.T) {
	p := Plan{Steps: []Step{}}
	for i := 0; i < MaxSteps; i++ {
		after := []string{}
		if i > 0 {
			after = append(after, fmt.Sprintf("step-%03d", i-1))
		}
		p.Steps = append(p.Steps, Step{ID: fmt.Sprintf("step-%03d", i), Title: "Step", After: after})
	}
	p.Steps[0].Ref = "card:done"
	now := time.Now()
	state := Compute(p, map[string]Fact{"card:done": {Known: true, Status: "done"}}, now, now, 0)
	g := TileGeometry(p, state)
	if len(g.Nodes) != MaxTileNodes || g.CollapsedNodes != MaxSteps-MaxTileNodes || g.Shape != "chain" {
		t.Fatal(g)
	}
	for i, n := range g.Nodes {
		if n.Layer != i || (i == 0 && n.Status != "done") || (i > 0 && n.After[0] != g.Nodes[i-1].ID) {
			t.Fatal(n)
		}
	}
	p = Plan{Steps: []Step{{ID: "a", Title: "A"}, {ID: "b", Title: "B"}, {ID: "c", Title: "C", After: []string{"a", "b"}}}}
	g = TileGeometry(p, Compute(p, nil, now, now, 0))
	if g.Shape != "dag" || g.Nodes[2].Layer != 1 || len(g.Nodes[2].After) != 2 {
		t.Fatal(g)
	}
	p = Plan{Steps: []Step{{ID: "a", Title: "A"}, {ID: "b", Title: "B"}}}
	if g = TileGeometry(p, Compute(p, nil, now, now, 0)); g.Shape != "lanes" {
		t.Fatal(g)
	}
}
