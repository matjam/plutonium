// Ported from internal/escape2/scale_test.go at the go-cutover tag.
// Differences from the Go test, which only logs measurements:
//   - The program is compiler/cmd/pluto (selfhost/cmd/pluto before the Go
//     removal).
//   - Plutonium has no runtime.MemStats and no recover. The heap sampler is
//     the process's peak resident memory (VmHWM in /proc/self/status) after
//     loading and at the end, and a function past the solver's limits
//     stops the test with the solver's panic instead of being logged.
//   - The OnSCC callback is a function, not a literal (see scaleLog), and
//     its progress lines go to standard error.
//   - PLUTO_STACKSAMPLE (sample_test.go, runtime.Stack) has no Plutonium
//     form and is not ported.

package escape2

import (
	"pluto/fmt"
	"pluto/os"
	"pluto/path/filepath"
	"pluto/strings"
	"pluto/testing"

	"compiler/compat"
	"compiler/internal/escape2/eir"
	"compiler/internal/escape2/sched"
	"compiler/internal/load"
	"compiler/internal/testenv"
	"compiler/internal/types2"
	"compiler/pu"
)

// TestScaleTranslated measures the analysis on the translated compiler
// (plan section (e)): the time to build EIR and to analyse it, the most
// heap in use while it runs, and the SCC measurements. It takes minutes and
// gigabytes, so it runs only when PLUTO_SCALE_TEST is set. The program is
// selfhost/cmd/pluto, or the directory PLUTO_ESCAPE2_PROG names (relative
// to the repository root), such as selfhost/demo/diagdemo.
func TestScaleTranslated(t *testing.T) {
	if compat.Getenv("PLUTO_SCALE_TEST") == "" {
		t.Skip("takes minutes and several GB; set PLUTO_SCALE_TEST=1")
	}
	root := testenv.Root()
	dir := compat.Getenv("PLUTO_ESCAPE2_PROG")
	if dir == "" {
		dir = "compiler/cmd/pluto"
	}
	cfg := pu.New(load.Config{
		Tags:   map[string]bool{"amd64": true, "linux": true, "hosted": true},
		Stdlib: testenv.Stdlib(),
		Sizes:  types2.SizesFor("gc", "amd64"),
	})
	start := compat.Now()
	prog, err := load.Load(cfg, filepath.Join(root, dir))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	t.Logf("%s: loaded in %v", dir, compat.Since(start))
	base := peakRSS()

	start = compat.Now()
	p, err := eir.Build(prog)
	buildTime := compat.Since(start)
	if err != nil {
		t.Fatalf("EIR: %v", err)
	}
	instrs := 0
	for _, f := range p.Funcs {
		for _, b := range f.Blocks {
			instrs += len(b.Instrs)
		}
	}
	// Stage B alone, for its time (RunWith computes it again).
	start = compat.Now()
	sched.StageB(p)
	stageBTime := compat.Since(start)

	scale = scaleLog{}
	start = compat.Now()
	scale.start, scale.last = start, start
	res := sched.RunWith(p, sched.Options{OnSCC: scaleSCC})
	runTime := compat.Since(start)
	sizes, biggest, solves, workspace, resumes, retained := scale.sizes, scale.biggest, scale.solves, scale.workspace, scale.resumes, scale.retained

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
	t.Logf("EIR: %d functions, %d instructions, built in %v; stage B %v", len(p.Funcs), instrs, buildTime, stageBTime)
	t.Logf("analysis: %v; %d SCCs done %v, largest %d functions (%d solves); %d solves in all",
		runTime, len(sizes), hist, biggest.Funcs, biggest.Solves, solves)
	t.Logf("resumed solves: %d of %d; most retained solver state of one SCC: %d MB", resumes, solves, retained>>20)
	t.Logf("memory: peak SCC workspace %d MB; peak RSS %d MB after loading, %d MB at the end of EIR and analysis",
		workspace>>20, base>>20, peakRSS()>>20)
	t.Logf("summaries %d MB; findings: %d", res.Stats.SummaryBytes>>20, len(res.Findings))
	for _, fd := range res.Findings {
		if fd.Code == "E3010" {
			t.Logf("  past the solver's limits (E3010): %s", p.Funcs[fd.Func].Name)
		}
	}
}

// scaleLog is what scaleSCC was told of the SCCs. In Go these were
// variables of the test that the OnSCC function literal used; sched keeps
// its options (the Result's environment holds them), so a function that
// uses a frame's variables cannot be given to it. For the same reason the
// progress lines go to standard error, not to t.Logf.
type scaleLog struct {
	sizes             []int
	biggest           sched.SCCStats
	solves, workspace int
	resumes, retained int
	start, last       compat.Time
}

var scale scaleLog

func scaleSCC(scc []eir.FuncID, st sched.SCCStats) {
	scale.sizes = compat.Append(scale.sizes, len(scc))
	scale.solves += st.Solves
	scale.resumes += st.Resumes
	if st.RetainedPeak > scale.retained {
		scale.retained = st.RetainedPeak
	}
	if st.Funcs > scale.biggest.Funcs {
		scale.biggest = st
	}
	if w := st.ArenaBytes + st.SetBytes; w > scale.workspace {
		scale.workspace = w
	}
	// Progress, as it goes: every large SCC, and every 500th.
	if len(scc) >= 50 || len(scale.sizes)%500 == 0 || compat.Since(scale.last) > 60_000_000_000 {
		fmt.Fprintf(os.Stderr, "  SCC %d: %d functions, %d solves, workspace %d MB; %v since the start of the analysis, peak RSS %d MB\n",
			len(scale.sizes), len(scc), st.Solves, (st.ArenaBytes+st.SetBytes)>>20, compat.Since(scale.start), peakRSS()>>20)
		scale.last = compat.Now()
	}
}

// peakRSS returns the process's peak resident memory in bytes (VmHWM), or
// 0 if /proc does not say.
func peakRSS() int {
	data, err := compat.ReadFile("/proc/self/status")
	if err != nil {
		return 0
	}
	for _, l := range compat.Split(compat.String(data), "\n") {
		if v, ok := strings.CutPrefix(l, "VmHWM:"); ok {
			n := 0
			for _, c := range v {
				if c >= '0' && c <= '9' {
					n = n*10 + int(c-'0')
				}
			}
			return n << 10
		}
	}
	return 0
}
