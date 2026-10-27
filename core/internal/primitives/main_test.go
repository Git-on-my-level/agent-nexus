package primitives_test

import (
	"flag"
	"fmt"
	"os"
	"runtime"
	"testing"
)

// The fast tier (-short, ANX_TEST_FAST) runs beside other developer work on
// shared machines. Sizing the Go runtime to every hardware thread made the
// package fight itself for the cores it actually gets: on an 18-CPU host with
// most cores busy, 18 runnable Ps burn well over a thousand CPU-seconds of
// pure scheduler churn while SQLite work stretches 10-90x, and the package
// lands near the go test timeout. Six Ps keep this suite near its serial CPU
// cost under that load (measured: 535s wall / 2615s CPU at 18 Ps versus
// 364s / 1213s when capped at 6), and cost nothing on CI's smaller runners,
// where NumCPU is already at or below the cap. The non-short CI tier keeps
// the default.
const fastTierGOMAXPROCS = 6

func TestMain(m *testing.M) {
	flag.Parse()
	if testing.Short() && runtime.GOMAXPROCS(0) > fastTierGOMAXPROCS {
		// Cap the parallel-test cap too: go test derives its default from the
		// original GOMAXPROCS before TestMain runs, and 18 concurrently
		// released tests on fewer Ps reintroduces the scheduling churn.
		if p := flag.Lookup("test.parallel"); p != nil {
			_ = p.Value.Set(fmt.Sprint(fastTierGOMAXPROCS))
		}
		runtime.GOMAXPROCS(fastTierGOMAXPROCS)
	}
	code := m.Run()
	if err := cleanupTestWorkspace(); err != nil {
		fmt.Fprintln(os.Stderr, "remove test template:", err)
		code = 1
	}
	os.Exit(code)
}
