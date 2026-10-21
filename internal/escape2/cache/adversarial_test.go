package cache_test

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	plutonium "github.com/matjam/plutonium"
	"github.com/matjam/plutonium/internal/escape2/cache"
	"github.com/matjam/plutonium/internal/escape2/domain"
	"github.com/matjam/plutonium/internal/escape2/summary"
	"github.com/matjam/plutonium/internal/load"
	"github.com/matjam/plutonium/internal/types2"
)

// The adversarial suite (D44): files that are corrupt, truncated, widened,
// narrowed or swapped may cost time, never a verdict. For every program of
// the corpus and the review suites, a run with a damaged cache must give
// the diagnostics of a run without one, byte for byte.

// corpusProgram is one program of the corpus of tools/escapedigest and the
// review suites (as internal/escape2/eir's corpus test lists them).
type corpusProgram struct {
	name                 string
	dir, file            string
	freestanding, kernel bool
}

func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func corpus(t *testing.T) []corpusProgram {
	root := repoRoot(t)
	var out []corpusProgram
	examples, _ := filepath.Glob(filepath.Join(root, "examples", "*", "main.p"))
	for _, f := range examples {
		out = append(out, corpusProgram{name: "examples/" + filepath.Base(filepath.Dir(f)), dir: filepath.Dir(f)})
	}
	for _, k := range []struct {
		name                 string
		freestanding, kernel bool
	}{{"sched", false, true}, {"smoke", false, true}, {"gsuser", false, true}, {"irq", true, false}} {
		out = append(out, corpusProgram{name: "kernel/" + k.name, dir: filepath.Join(root, "testdata", "kernel", k.name), freestanding: k.freestanding, kernel: k.kernel})
	}
	out = append(out, corpusProgram{name: "cabi", dir: filepath.Join(root, "testdata", "cabi")})
	for _, d := range []string{"run", "lib", "plutonium", "trace"} {
		files, _ := filepath.Glob(filepath.Join(root, "testdata", d, "*.p"))
		for _, f := range files {
			out = append(out, corpusProgram{name: d + "/" + strings.TrimSuffix(filepath.Base(f), ".p"), file: f})
		}
	}
	diags, _ := filepath.Glob(filepath.Join(root, "cmd", "pluto", "testdata", "diag", "*", "main.p"))
	for _, f := range diags {
		out = append(out, corpusProgram{name: "diag/" + filepath.Base(filepath.Dir(f)), dir: filepath.Dir(f)})
	}
	for _, d := range []string{"escape/testdata/review2", "escape/testdata/review3", "escape2/solve/testdata/review4"} {
		files, _ := filepath.Glob(filepath.Join(root, "internal", d, "*.p"))
		d := filepath.Base(d)
		for _, f := range files {
			out = append(out, corpusProgram{name: d + "/" + strings.TrimSuffix(filepath.Base(f), ".p"), file: f})
		}
	}
	return out
}

// load loads a program as the build does; ok is false for one that does
// not load (diagnostics and review programs that fail earlier).
func (cp corpusProgram) load(t *testing.T) (*load.Program, bool) {
	t.Helper()
	dir := cp.dir
	if cp.file != "" {
		dir = t.TempDir()
		src, err := os.ReadFile(cp.file)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "main.p"), src, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	tags := load.BuildTags(cp.freestanding, cp.kernel, nil)
	if !cp.freestanding && !cp.kernel {
		tags["gencheck"] = true
	}
	cfg := &load.Config{Tags: tags, Stdlib: plutonium.Stdlib(), Sizes: types2.SizesFor("gc", "amd64")}
	prog, err := load.Load(cfg, dir)
	if err != nil {
		return nil, false
	}
	return prog, true
}

// A mutation rewrites the files of a cache directory.
type mutation struct {
	name string
	fn   func(t *testing.T, dir string)
}

func eachFile(t *testing.T, dir string, fn func(name string, data []byte) []byte) {
	t.Helper()
	for _, f := range cache.Files(dir) {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(f, fn(f, data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// eachSummary rewrites every entry's summary, writing files with valid
// checksums: what someone who knows the format can do.
func eachSummary(t *testing.T, dir string, fn func(ps domain.Paths, s *summary.Summary) *summary.Summary) {
	t.Helper()
	for _, f := range cache.Files(dir) {
		pkg, es, err := cache.ReadEntries(f)
		if err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		for i, e := range es {
			ps := domain.NewPaths()
			s, _, err := cache.DecodeAny(e.Sum, ps)
			if err != nil || s == nil {
				continue
			}
			es[i].Sum = cache.EncodeSummary(ps, fn(ps, s))
		}
		if err := cache.WriteEntries(f, pkg, es); err != nil {
			t.Fatal(err)
		}
	}
}

var mutations = []mutation{
	{"corrupt", func(t *testing.T, dir string) {
		eachFile(t, dir, func(_ string, b []byte) []byte {
			b[len(b)/2] ^= 0x5a
			return b
		})
	}},
	{"truncate", func(t *testing.T, dir string) {
		eachFile(t, dir, func(_ string, b []byte) []byte { return b[:len(b)/2] })
	}},
	{"empty", func(t *testing.T, dir string) {
		eachFile(t, dir, func(string, []byte) []byte { return nil })
	}},
	{"widen", func(t *testing.T, dir string) {
		// Every input escapes and flows to every result: wider than any
		// solve gives, so it passes the check, and callers that pass
		// their frame see an escape that is not there.
		eachSummary(t, dir, func(ps domain.Paths, s *summary.Summary) *summary.Summary {
			for in := range s.Inputs {
				k := domain.MakeKey(in, domain.Root, domain.KeyFlags{GE: true})
				s.Esc = append(s.Esc, k)
				for r := range s.Res {
					s.Res[r].From = append(s.Res[r].From, summary.ResultKey{Key: k, Field: domain.Root, FieldGE: true})
				}
			}
			return s
		})
	}},
	{"narrow", func(t *testing.T, dir string) {
		// Nothing escapes, nothing is stored, results hold nothing.
		eachSummary(t, dir, func(ps domain.Paths, s *summary.Summary) *summary.Summary {
			return &summary.Summary{Inputs: s.Inputs}
		})
	}},
	{"narrow one", func(t *testing.T, dir string) {
		// One entry less: the least a hand edit can drop.
		eachSummary(t, dir, func(ps domain.Paths, s *summary.Summary) *summary.Summary {
			switch {
			case len(s.Esc) > 0:
				s.Esc = s.Esc[1:]
			case len(s.Store) > 0:
				s.Store = s.Store[1:]
			default:
				for r := range s.Res {
					if len(s.Res[r].From) > 0 {
						s.Res[r].From = s.Res[r].From[1:]
						break
					}
				}
			}
			return s
		})
	}},
	{"swap entries", func(t *testing.T, dir string) {
		// Each function gets the summary of another with as many inputs.
		for _, f := range cache.Files(dir) {
			pkg, es, err := cache.ReadEntries(f)
			if err != nil {
				t.Fatal(err)
			}
			byInputs := map[int][]int{}
			for i, e := range es {
				_, n, _ := cache.DecodeAny(e.Sum, domain.NewPaths())
				byInputs[n] = append(byInputs[n], i)
			}
			sums := make([][]byte, len(es))
			for i, e := range es {
				sums[i] = e.Sum
			}
			for _, idx := range byInputs {
				for j, i := range idx {
					es[i].Sum = sums[idx[(j+1)%len(idx)]]
				}
			}
			if err := cache.WriteEntries(f, pkg, es); err != nil {
				t.Fatal(err)
			}
		}
	}},
	{"scramble", func(t *testing.T, dir string) {
		// A byte of each summary changed, the checksum made valid: the
		// decoder sees bytes no encoder writes, or a different summary.
		for _, f := range cache.Files(dir) {
			pkg, es, err := cache.ReadEntries(f)
			if err != nil {
				t.Fatal(err)
			}
			for i := range es {
				b := append([]byte(nil), es[i].Sum...)
				if len(b) > 0 {
					j := (i*7 + 3) % len(b)
					b[j] ^= byte(1 << (i % 8))
				}
				es[i].Sum = b
			}
			if err := cache.WriteEntries(f, pkg, es); err != nil {
				t.Fatal(err)
			}
		}
	}},
	{"swap files", func(t *testing.T, dir string) {
		// Each package's file holds another package's.
		fs := cache.Files(dir)
		data := make([][]byte, len(fs))
		for i, f := range fs {
			data[i], _ = os.ReadFile(f)
		}
		for i, f := range fs {
			if err := os.WriteFile(f, data[(i+1)%len(fs)], 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}},
}

func copyDir(t *testing.T, from, to string) {
	t.Helper()
	for _, f := range cache.Files(from) {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(to, filepath.Base(f)), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// TestAdversarial runs every mutation on the cache of every program of the
// corpus and the review suites (a sample of them unless
// PLUTO_ESCAPE_CACHE_FULL is set: the full suite takes minutes) and checks
// that the diagnostics are those of a run without a cache. It also checks
// that the mutations reach every way the driver has of refusing a file.
func TestAdversarial(t *testing.T) {
	if testing.Short() {
		t.Skip("runs the analysis many times over the corpus")
	}
	progs := corpus(t)
	stride := 40
	if os.Getenv("PLUTO_ESCAPE_CACHE_FULL") != "" {
		stride = 1
	}
	var mu sync.Mutex
	totals := map[string]*[5]int{} // by mutation: accepted, missed, rejected, redone, programs rejected
	for _, m := range mutations {
		totals[m.name] = new([5]int)
	}
	t.Run("programs", func(t *testing.T) {
		for i, cp := range progs {
			if i%stride != 0 {
				continue
			}
			t.Run(cp.name, func(t *testing.T) {
				t.Parallel()
				prog, ok := cp.load(t)
				if !ok {
					t.Skip("does not load")
				}
				cold := analyse(t, prog, "")
				base := t.TempDir()
				first := analyse(t, prog, base)
				sameAsCold(t, "writing the cache", cold, first)
				warm := analyse(t, prog, t.TempDir()+"/none")
				sameAsCold(t, "an empty cache", cold, warm)
				for _, m := range mutations {
					dir := t.TempDir()
					copyDir(t, base, dir)
					m.fn(t, dir)
					got := analyse(t, prog, dir)
					sameAsCold(t, m.name, cold, got)
					// And the run after it, from the files it rewrote.
					again := analyse(t, prog, dir)
					sameAsCold(t, m.name+", then again", cold, again)
					mu.Lock()
					tt := totals[m.name]
					tt[0] += got.stats.CacheAccepted
					tt[1] += got.stats.CacheMissed
					tt[2] += got.stats.CacheRejected
					tt[3] += got.stats.CacheRedone
					if cold.diags != "" {
						tt[4]++
					}
					mu.Unlock()
				}
			})
		}
	})
	for _, m := range mutations {
		tt := totals[m.name]
		t.Logf("%-12s SCCs accepted %d, missed %d, rejected %d, redone %d (over programs, %d of them rejected)", m.name, tt[0], tt[1], tt[2], tt[3], tt[4])
	}
	// Each refusal is exercised: damaged files are not read; a narrowed
	// summary fails the check; a widened one passes it and is redone
	// before an error is reported.
	if t.Failed() {
		return
	}
	need := []struct {
		m    string
		i    int
		what string
	}{
		{"corrupt", 1, "missed"}, {"truncate", 1, "missed"}, {"swap files", 1, "missed"},
		{"narrow", 2, "rejected"}, {"narrow one", 2, "rejected"}, {"swap entries", 2, "rejected"},
		{"widen", 0, "accepted"}, {"widen", 3, "redone"},
	}
	for _, n := range need {
		if totals[n.m][n.i] == 0 {
			t.Errorf("mutation %s: no SCC %s: the suite does not reach that case", n.m, n.what)
		}
	}
}
