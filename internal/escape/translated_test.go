package escape_test

import (
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	plutonium "github.com/matjam/plutonium"
	"github.com/matjam/plutonium/internal/escape"
	"github.com/matjam/plutonium/internal/load"
	"github.com/matjam/plutonium/internal/types2"
)

// Bounds for the analysis of the translated compiler. Measured on a machine
// with 24 cores and 30 GB: about 100 seconds and 4.5 GB. Before the
// analysis applied each key of a callee once to the pointers passed, the same
// program ran for more than 25 minutes and over 6 GB without finishing.
const (
	translatedTimeBound = 8 * time.Minute
	translatedHeapBound = 8 << 30
)

// analyzeTranslated runs the escape analysis on the translated type checker
// (selfhost/demo/diagdemo imports selfhost/internal/types2 and its
// neighbours: about 60,000 lines) and returns the time it took and the most
// heap it had in use.
func analyzeTranslated(tb testing.TB) (time.Duration, uint64) {
	tb.Helper()
	root, err := filepath.Abs("../..")
	if err != nil {
		tb.Fatal(err)
	}
	cfg := &load.Config{
		Tags:   map[string]bool{"amd64": true, "linux": true, "hosted": true},
		Stdlib: plutonium.Stdlib(),
		Sizes:  types2.SizesFor("gc", "amd64"),
	}
	prog, err := load.Load(cfg, filepath.Join(root, "selfhost", "demo", "diagdemo"))
	if err != nil {
		tb.Fatalf("load: %v", err)
	}
	var peak atomic.Uint64
	done, finished := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(finished)
		var m runtime.MemStats
		for {
			runtime.ReadMemStats(&m)
			if m.HeapInuse > peak.Load() {
				peak.Store(m.HeapInuse)
			}
			select {
			case <-done:
				return
			case <-time.After(100 * time.Millisecond):
			}
		}
	}()
	start := time.Now()
	err = escape.Check(prog)
	elapsed := time.Since(start)
	close(done)
	<-finished
	// The translation has views the analysis rejects (the owner of the
	// translation fixes them); the test is about how long it takes to say so.
	_ = err
	return elapsed, peak.Load()
}

// TestAnalyzeTranslatedCompiler checks that the analysis finishes on the
// translated type checker in minutes and a few gigabytes. It takes minutes,
// so it runs only when PLUTO_SCALE_TEST is set (go test -run Translated, or
// go test -bench Translated).
func TestAnalyzeTranslatedCompiler(t *testing.T) {
	if os.Getenv("PLUTO_SCALE_TEST") == "" {
		t.Skip("takes minutes and several GB; set PLUTO_SCALE_TEST=1")
	}
	elapsed, peak := analyzeTranslated(t)
	t.Logf("escape analysis of the translated compiler: %v, heap in use at most %d MB", elapsed, peak>>20)
	if elapsed > translatedTimeBound {
		t.Errorf("took %v, want less than %v", elapsed, translatedTimeBound)
	}
	if peak > translatedHeapBound {
		t.Errorf("used %d MB of heap, want less than %d MB", peak>>20, translatedHeapBound>>20)
	}
}

func BenchmarkAnalyzeTranslatedCompiler(b *testing.B) {
	if os.Getenv("PLUTO_SCALE_TEST") == "" {
		b.Skip("takes minutes and several GB; set PLUTO_SCALE_TEST=1")
	}
	for i := 0; i < b.N; i++ {
		elapsed, peak := analyzeTranslated(b)
		b.ReportMetric(float64(peak>>20), "MB-heap")
		if elapsed > translatedTimeBound || peak > translatedHeapBound {
			b.Errorf("took %v and %d MB of heap, want less than %v and %d MB", elapsed, peak>>20, translatedTimeBound, translatedHeapBound>>20)
		}
	}
}
