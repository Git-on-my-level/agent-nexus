package loopstats

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestAccountingResetsCountsAndKeepsNames(t *testing.T) {
	registry.Lock()
	registry.loops = map[string]counters{}
	registry.Unlock()
	Register("dormant")
	ctx, finish := Start(context.Background(), "test_loop")
	Rows(ctx, 7)
	finish()
	var b bytes.Buffer
	Write(&b)
	if !strings.Contains(b.String(), "background_loop=test_loop runs=1 busy_ms=") || !strings.Contains(b.String(), " rows=7\n") || !strings.Contains(b.String(), "background_loop=dormant runs=0 busy_ms=0 rows=0") {
		t.Fatal(b.String())
	}
	b.Reset()
	Write(&b)
	if !strings.Contains(b.String(), "background_loop=test_loop runs=0 busy_ms=0 rows=0") {
		t.Fatal(b.String())
	}
}
