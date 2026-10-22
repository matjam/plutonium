package escape2

import (
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	plutonium "github.com/matjam/plutonium"
	"github.com/matjam/plutonium/internal/escape2/eir"
	"github.com/matjam/plutonium/internal/escape2/sched"
	"github.com/matjam/plutonium/internal/load"
	"github.com/matjam/plutonium/internal/types2"
)

// TestScaleTranslated measures the analysis on the translated compiler
// (plan section (e)): the time to build EIR and to analyse it, the most
// heap in use while it runs, and the SCC measurements. It takes minutes and
// gigabytes, so it runs only when PLUTO_SCALE_TEST is set. The program is
// selfhost/cmd/pluto, or the directory PLUTO_ESCAPE2_PROG names (relative
// to the repository root), such as selfhost/demo/diagdemo.
func TestScaleTranslated(t *testing.T) {
	if os.Getenv("PLUTO_SCALE_TEST") == "" {
		t.Skip("takes minutes and several GB; set PLUTO_SCALE_TEST=1")
	}
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	dir := os.Getenv("PLUTO_ESCAPE2_PROG")
	if dir == "" {
		dir = "selfhost/cmd/pluto"
	}
	cfg := &load.Config{
		Tags:   map[string]bool{"amd64": true, "linux": true, "hosted": true},
		Stdlib: plutonium.Stdlib(),
		Sizes:  types2.SizesFor("gc", "amd64"),
	}
	start := time.Now()
	prog, err := load.Load(cfg, filepath.Join(root, dir))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	t.Logf("%s: loaded in %v", dir, time.Since(start).Round(time.Millisecond))

	runtime.GC()
	var base runtime.MemStats
	runtime.ReadMemStats(&base)
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

	start = time.Now()
	p, err := eir.Build(prog)
	buildTime := time.Since(start)
	if err != nil {
		close(done)
		<-finished
		t.Fatalf("EIR: %v", err)
	}
	instrs := 0
	for _, f := range p.Funcs {
		for _, b := range f.Blocks {
			instrs += len(b.Instrs)
		}
	}
	// Stage B alone, for its time (RunWith computes it again).
	start = time.Now()
	sched.StageB(p)
	stageBTime := time.Since(start)

	var sizes []int
	var biggest sched.SCCStats
	solves, workspace := 0, 0
	resumes, retained := 0, 0
	start = time.Now()
	last := start
	var res *sched.Result
	var stopped any
	func() {
		// A function past the solver's limits stops the analysis (it is
		// an error of that function); report what was measured until then.
		defer func() { stopped = recover() }()
		res = sched.RunWith(p, sched.Options{OnSCC: func(scc []eir.FuncID, st sched.SCCStats) {
			sizes = append(sizes, len(scc))
			solves += st.Solves
			resumes += st.Resumes
			if st.RetainedPeak > retained {
				retained = st.RetainedPeak
			}
			if st.Funcs > biggest.Funcs {
				biggest = st
			}
			if w := st.ArenaBytes + st.SetBytes; w > workspace {
				workspace = w
			}
			// Progress, as it goes: every large SCC, and every 500th.
			if len(scc) >= 50 || len(sizes)%500 == 0 || time.Since(last) > time.Minute {
				t.Logf("  SCC %d: %d functions, %d solves, workspace %d MB; %v since the start of the analysis, heap %d MB",
					len(sizes), len(scc), st.Solves, (st.ArenaBytes+st.SetBytes)>>20, time.Since(start).Round(time.Second), peak.Load()>>20)
				last = time.Now()
			}
		}})
	}()
	runTime := time.Since(start)
	close(done)
	<-finished

	hist := map[string]int{}
	for _, n := range sizes {
		switch {
		case n == 1:
			hist["1"]++
		case n <= 10:
			hist["2-10"]++
		case n <= 100:
			hist["11-100"]++
		default:
			hist[">100"]++
		}
	}
	t.Logf("EIR: %d functions, %d instructions, built in %v; stage B %v", len(p.Funcs), instrs, buildTime.Round(time.Millisecond), stageBTime.Round(time.Millisecond))
	t.Logf("analysis: %v; %d SCCs done %v, largest %d functions (%d solves); %d solves in all",
		runTime.Round(time.Millisecond), len(sizes), hist, biggest.Funcs, biggest.Solves, solves)
	t.Logf("resumed solves: %d of %d; most retained solver state of one SCC: %d MB", resumes, solves, retained>>20)
	t.Logf("memory: peak SCC workspace %d MB; heap in use %d MB after loading, %d MB at the peak of EIR and analysis",
		workspace>>20, base.HeapInuse>>20, peak.Load()>>20)
	if stopped != nil {
		t.Logf("stopped after %d of the program's SCCs: %v", len(sizes), stopped)
		return
	}
	t.Logf("summaries %d MB; findings: %d", res.Stats.SummaryBytes>>20, len(res.Findings))
	for _, fd := range res.Findings {
		if fd.Code == "E3010" {
			t.Logf("  past the solver's limits (E3010): %s", p.Funcs[fd.Func].Name)
		}
	}
}
