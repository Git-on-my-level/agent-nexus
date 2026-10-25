// Package loopstats records bounded, numeric-only maintenance diagnostics.
package loopstats

import (
	"context"
	"fmt"
	"io"
	"runtime/pprof"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

type counters struct{ runs, nanos, rows int64 }

var registry = struct {
	sync.Mutex
	loops map[string]counters
}{loops: map[string]counters{}}

type key struct{}
type pass struct{ rows atomic.Int64 }

// Start measures a single active pass, excluding the loop's sleep. Names must
// be code constants, never workspace data. Nested helpers can count candidates
// with Rows; this is logical work, not SQLite's internal VM/trigger row count.
func Start(ctx context.Context, name string) (context.Context, func()) {
	p := &pass{}
	parent := ctx
	ctx = context.WithValue(pprof.WithLabels(ctx, pprof.Labels("loop", name)), key{}, p)
	pprof.SetGoroutineLabels(ctx)
	start := time.Now()
	return ctx, func() {
		elapsed := time.Since(start)
		registry.Lock()
		c := registry.loops[name]
		c.runs++
		c.nanos += elapsed.Nanoseconds()
		c.rows += p.rows.Load()
		registry.loops[name] = c
		registry.Unlock()
		pprof.SetGoroutineLabels(parent)
	}
}

func Rows(ctx context.Context, n int) {
	if p, ok := ctx.Value(key{}).(*pass); ok {
		p.rows.Add(int64(n))
	}
}

func Write(w io.Writer) {
	registry.Lock()
	snapshot := registry.loops
	registry.loops = make(map[string]counters, len(snapshot))
	for name := range snapshot {
		registry.loops[name] = counters{}
	}
	registry.Unlock()
	names := make([]string, 0, len(snapshot))
	for name := range snapshot {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		c := snapshot[name]
		fmt.Fprintf(w, "background_loop=%s runs=%d busy_ms=%d rows=%d\n", name, c.runs, c.nanos/int64(time.Millisecond), c.rows)
	}
}

func Run(ctx context.Context, w io.Writer) {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			Write(w)
		}
	}
}

// Register keeps dormant and completed one-shot workers visible as zeroes.
func Register(names ...string) {
	registry.Lock()
	defer registry.Unlock()
	for _, name := range names {
		if _, ok := registry.loops[name]; !ok {
			registry.loops[name] = counters{}
		}
	}
}
