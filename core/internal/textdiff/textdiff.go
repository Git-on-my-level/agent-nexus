// Package textdiff renders bounded line diffs.
//
// Lines are compared with Myers' O(ND) algorithm after trimming the common
// prefix and suffix. The search stops after MaxEdits edits; past that budget
// the differing middle is reported as one replaced block, which is still a
// correct (if not minimal) diff, and Result.Minimal is false. Rendered output is
// capped at MaxOutputBytes. Cost is therefore O((N+M)·MaxEdits) time and
// O(MaxEdits²) memory however large or different the inputs are.
package textdiff

import (
	"fmt"
	"strings"
)

const (
	MaxEdits       = 512
	MaxOutputBytes = 256 << 10
	contextLines   = 3
)

type Result struct {
	Unified      string
	AddedLines   int
	RemovedLines int
	Minimal      bool
	Truncated    bool
}

type opKind byte

const (
	opEqual opKind = iota
	opDelete
	opInsert
)

type op struct {
	kind opKind
	a, b int // line indexes in a and b
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	// Lines compare without their terminator, so a missing final newline
	// does not turn the last line into a change.
	return strings.Split(strings.TrimSuffix(s, "\n"), "\n")
}

// Lines diffs before and after line by line and renders a unified diff with
// three lines of context.
func Lines(beforeLabel, before, afterLabel, after string) Result {
	a, b := splitLines(before), splitLines(after)
	ops, minimal := script(a, b)
	res := Result{Minimal: minimal}
	for _, o := range ops {
		switch o.kind {
		case opInsert:
			res.AddedLines++
		case opDelete:
			res.RemovedLines++
		}
	}
	if res.AddedLines == 0 && res.RemovedLines == 0 {
		return res
	}
	res.Unified, res.Truncated = render(beforeLabel, afterLabel, a, b, ops)
	return res
}

func script(a, b []string) ([]op, bool) {
	prefix := 0
	for prefix < len(a) && prefix < len(b) && a[prefix] == b[prefix] {
		prefix++
	}
	suffix := 0
	for suffix < len(a)-prefix && suffix < len(b)-prefix && a[len(a)-1-suffix] == b[len(b)-1-suffix] {
		suffix++
	}
	ops := make([]op, 0, len(a)+len(b))
	for i := 0; i < prefix; i++ {
		ops = append(ops, op{opEqual, i, i})
	}
	midA, midB := a[prefix:len(a)-suffix], b[prefix:len(b)-suffix]
	mid, minimal := myers(midA, midB)
	for _, o := range mid {
		o.a += prefix
		o.b += prefix
		ops = append(ops, o)
	}
	for i := 0; i < suffix; i++ {
		ops = append(ops, op{opEqual, len(a) - suffix + i, len(b) - suffix + i})
	}
	return ops, minimal
}

func replaceAll(a, b []string) []op {
	ops := make([]op, 0, len(a)+len(b))
	for i := range a {
		ops = append(ops, op{opDelete, i, 0})
	}
	for j := range b {
		ops = append(ops, op{opInsert, len(a), j})
	}
	return ops
}

func myers(a, b []string) ([]op, bool) {
	n, m := len(a), len(b)
	if n == 0 || m == 0 {
		return replaceAll(a, b), true
	}
	maxD := n + m
	if maxD > MaxEdits {
		maxD = MaxEdits
	}
	offset := maxD + 1
	v := make([]int, 2*maxD+3)
	var trace [][]int
	for d := 0; d <= maxD; d++ {
		snapshot := make([]int, 2*d+3)
		copy(snapshot, v[offset-d-1:offset+d+2])
		trace = append(trace, snapshot)
		for k := -d; k <= d; k += 2 {
			var x int
			if k == -d || (k != d && v[offset+k-1] < v[offset+k+1]) {
				x = v[offset+k+1]
			} else {
				x = v[offset+k-1] + 1
			}
			y := x - k
			for x < n && y < m && a[x] == b[y] {
				x++
				y++
			}
			v[offset+k] = x
			if x >= n && y >= m {
				return backtrack(trace, a, b, d, k), true
			}
		}
	}
	return replaceAll(a, b), false
}

func backtrack(trace [][]int, a, b []string, d, k int) []op {
	x, y := len(a), len(b)
	var rev []op
	for ; d > 0; d-- {
		vPrev := trace[d] // values before step d, indexed k-(d)..k+(d)
		at := func(kk int) int { return vPrev[kk+d+1] }
		var prevK int
		if k == -d || (k != d && at(k-1) < at(k+1)) {
			prevK = k + 1
		} else {
			prevK = k - 1
		}
		prevX := at(prevK)
		prevY := prevX - prevK
		for x > prevX && y > prevY {
			x--
			y--
			rev = append(rev, op{opEqual, x, y})
		}
		if x == prevX {
			y--
			rev = append(rev, op{opInsert, x, y})
		} else {
			x--
			rev = append(rev, op{opDelete, x, y})
		}
		k = prevK
	}
	for x > 0 && y > 0 {
		x--
		y--
		rev = append(rev, op{opEqual, x, y})
	}
	out := make([]op, len(rev))
	for i := range rev {
		out[i] = rev[len(rev)-1-i]
	}
	return out
}

func render(beforeLabel, afterLabel string, a, b []string, ops []op) (string, bool) {
	var sb strings.Builder
	fmt.Fprintf(&sb, "--- %s\n+++ %s\n", beforeLabel, afterLabel)
	line := func(prefix string, text string) {
		sb.WriteString(prefix)
		sb.WriteString(strings.TrimSuffix(text, "\n"))
		sb.WriteString("\n")
	}
	i := 0
	for i < len(ops) {
		for i < len(ops) && ops[i].kind == opEqual {
			i++
		}
		if i >= len(ops) {
			break
		}
		start := i - contextLines
		if start < 0 {
			start = 0
		}
		end := i
		for end < len(ops) {
			if ops[end].kind != opEqual {
				end++
				continue
			}
			run := end
			for run < len(ops) && ops[run].kind == opEqual {
				run++
			}
			if run-end > 2*contextLines || run == len(ops) {
				end += contextLines
				if end > len(ops) {
					end = len(ops)
				}
				break
			}
			end = run
		}
		aStart, bStart, aCount, bCount := -1, -1, 0, 0
		for _, o := range ops[start:end] {
			if aStart < 0 {
				aStart, bStart = o.a, o.b
			}
			if o.kind != opInsert {
				aCount++
			}
			if o.kind != opDelete {
				bCount++
			}
		}
		fmt.Fprintf(&sb, "@@ -%s +%s @@\n", hunkRange(aStart, aCount), hunkRange(bStart, bCount))
		for _, o := range ops[start:end] {
			switch o.kind {
			case opEqual:
				line(" ", a[o.a])
			case opDelete:
				line("-", a[o.a])
			case opInsert:
				line("+", b[o.b])
			}
			if sb.Len() > MaxOutputBytes {
				out := sb.String()[:MaxOutputBytes]
				if cut := strings.LastIndexByte(out, '\n'); cut > 0 {
					out = out[:cut+1]
				}
				return out, true
			}
		}
		i = end
	}
	return sb.String(), false
}

func hunkRange(start, count int) string {
	if count == 0 {
		return fmt.Sprintf("%d,0", start)
	}
	if count == 1 {
		return fmt.Sprintf("%d", start+1)
	}
	return fmt.Sprintf("%d,%d", start+1, count)
}
