package cache_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	plutonium "github.com/matjam/plutonium"
	"github.com/matjam/plutonium/internal/escape2/cache"
	"github.com/matjam/plutonium/internal/escape2/eir"
	"github.com/matjam/plutonium/internal/escape2/sched"
	"github.com/matjam/plutonium/internal/load"
	"github.com/matjam/plutonium/internal/types2"
)

// timing is one run's measurements.
type timing struct {
	build, analyse, save time.Duration
	stats                sched.Stats
}

func (a *timing) add(b timing) {
	a.build += b.build
	a.analyse += b.analyse
	a.save += b.save
	a.stats.Funcs += b.stats.Funcs
	a.stats.SCCs += b.stats.SCCs
	a.stats.Iterations += b.stats.Iterations
	a.stats.CacheAccepted += b.stats.CacheAccepted
	a.stats.CacheMissed += b.stats.CacheMissed
	a.stats.CacheRejected += b.stats.CacheRejected
	a.stats.CacheRedone += b.stats.CacheRedone
}

func measureRun(t *testing.T, prog *load.Program, dir string) timing {
	t.Helper()
	var tm timing
	start := time.Now()
	p, err := eir.Build(prog)
	if err != nil {
		t.Fatal(err)
	}
	tm.build = time.Since(start)
	var opts sched.Options
	var c *cache.Cache
	if dir != "" {
		if c, err = cache.Open(dir, cache.Config{}); err != nil {
			t.Fatal(err)
		}
		opts.Cache = c
	}
	start = time.Now()
	res := sched.RunWith(p, opts)
	tm.analyse = time.Since(start)
	tm.stats = res.Stats
	if c != nil {
		start = time.Now()
		if err := c.Save(res); err != nil {
			t.Fatal(err)
		}
		tm.save = time.Since(start)
	}
	return tm
}

func logTiming(t *testing.T, what string, tm timing) {
	t.Helper()
	s := tm.stats
	t.Logf("%-22s EIR %8v  analysis %8v  save %7v  | %d funcs, %d SCCs, %d solves; SCCs accepted %d, missed %d, rejected %d, redone %d",
		what, tm.build.Round(time.Millisecond), tm.analyse.Round(time.Millisecond), tm.save.Round(time.Millisecond),
		s.Funcs, s.SCCs, s.Iterations, s.CacheAccepted, s.CacheMissed, s.CacheRejected, s.CacheRedone)
}

// TestMeasure measures the analysis without a cache, writing one, and from
// one (warm), on the corpus (PLUTO_ESCAPE_CACHE_MEASURE=corpus) or on one
// program directory relative to the repository root
// (PLUTO_ESCAPE_CACHE_MEASURE=selfhost/demo/diagdemo). Each program runs
// alone, one after another.
func TestMeasure(t *testing.T) {
	what := os.Getenv("PLUTO_ESCAPE_CACHE_MEASURE")
	if what == "" {
		t.Skip("set PLUTO_ESCAPE_CACHE_MEASURE=corpus or a program directory")
	}
	// Programs are loaded for each pass and dropped: the corpus does not
	// fit in memory at once.
	root := repoRoot(t)
	var progs []corpusProgram
	if what == "corpus" {
		progs = corpus(t)
	}
	each := func(fn func(*load.Program)) {
		if what != "corpus" {
			cfg := &load.Config{
				Tags:   map[string]bool{"amd64": true, "linux": true, "hosted": true},
				Stdlib: plutonium.Stdlib(),
				Sizes:  types2.SizesFor("gc", "amd64"),
			}
			prog, err := load.Load(cfg, filepath.Join(root, what))
			if err != nil {
				t.Fatal(err)
			}
			fn(prog)
			return
		}
		for _, cp := range progs {
			if prog, ok := cp.load(t); ok {
				fn(prog)
			}
		}
	}
	var cold, write, warm, warmAgain timing
	n := 0
	dir := t.TempDir() // one directory for all: the corpus shares the library
	each(func(prog *load.Program) {
		n++
		cold.add(measureRun(t, prog, ""))
		write.add(measureRun(t, prog, dir))
	})
	each(func(prog *load.Program) { warm.add(measureRun(t, prog, dir)) })
	each(func(prog *load.Program) { warmAgain.add(measureRun(t, prog, dir)) })
	var size int64
	for _, f := range cache.Files(dir) {
		if fi, err := os.Stat(f); err == nil {
			size += fi.Size()
		}
	}
	t.Logf("%s: %d programs; cache %d files, %d KB", what, n, len(cache.Files(dir)), size>>10)
	logTiming(t, "cold, no cache", cold)
	logTiming(t, "first pass, writing", write)
	logTiming(t, "warm", warm)
	logTiming(t, "warm again", warmAgain)
}
