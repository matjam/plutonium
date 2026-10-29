// Ported from internal/escape2/cache/adversarial_test.go at the go-cutover tag.
// The Go file was the external package cache_test; the test is in package
// escape2 (see cache_test.p). corpusProgram and corpus are advProgram and
// advCorpus here, apart from internal/escape2/eir's. The corpus moved with
// the Go removal: cmd/pluto/testdata/diag is tests/diag, and the review
// programs are tests/testdata/escape/review2, review3 and review4. The Go
// test skipped with -short; this one runs only when PLUTO_LONG_TESTS is
// set. Its subtests ran in parallel under a mutex; here they run one at a
// time (pluto/testing has no Parallel).

package escape2

import (
	"pluto/os"
	"pluto/path/filepath"
	"pluto/strings"
	"pluto/testing"

	"compiler/compat"
	"compiler/internal/escape2/cache"
	"compiler/internal/escape2/domain"
	"compiler/internal/escape2/summary"
	"compiler/internal/load"
	"compiler/internal/scratch"
	"compiler/internal/testenv"
	"compiler/internal/types2"
	"compiler/pu"
)

// The adversarial suite (D44): files that are corrupt, truncated, widened,
// narrowed or swapped may cost time, never a verdict. For every program of
// the corpus and the review suites, a run with a damaged cache must give
// the diagnostics of a run without one, byte for byte.

// advProgram is one program of the corpus of tools/escapedigest and the
// review suites (as internal/escape2/eir's corpus test lists them).
type advProgram struct {
	name                 string
	dir, file            string
	freestanding, kernel bool
}

type advKernel struct {
	name                 string
	freestanding, kernel bool
}

func advCorpus(t *testing.T) []advProgram {
	root := testenv.Root()
	var out []advProgram
	examples := testenv.Glob(filepath.Join(root, "examples", "*", "main.p"))
	for _, f := range examples {
		out = compat.Append(out, advProgram{name: "examples/" + filepath.Base(filepath.Dir(f)), dir: filepath.Dir(f)})
	}
	for _, k := range []advKernel{{"sched", false, true}, {"smoke", false, true}, {"gsuser", false, true}, {"irq", true, false}} {
		out = compat.Append(out, advProgram{name: "kernel/" + k.name, dir: filepath.Join(root, "testdata", "kernel", k.name), freestanding: k.freestanding, kernel: k.kernel})
	}
	out = compat.Append(out, advProgram{name: "cabi", dir: filepath.Join(root, "testdata", "cabi")})
	for _, d := range []string{"run", "lib", "plutonium", "trace"} {
		files := testenv.Glob(filepath.Join(root, "testdata", d, "*.p"))
		for _, f := range files {
			out = compat.Append(out, advProgram{name: d + "/" + strings.TrimSuffix(filepath.Base(f), ".p"), file: f})
		}
	}
	diags := testenv.Glob(filepath.Join(root, "tests", "diag", "*", "main.p"))
	for _, f := range diags {
		out = compat.Append(out, advProgram{name: "diag/" + filepath.Base(filepath.Dir(f)), dir: filepath.Dir(f)})
	}
	for _, d := range []string{"review2", "review3", "review4"} {
		files := testenv.Glob(filepath.Join(root, "tests", "testdata", "escape", d, "*.p"))
		for _, f := range files {
			out = compat.Append(out, advProgram{name: d + "/" + strings.TrimSuffix(filepath.Base(f), ".p"), file: f})
		}
	}
	return out
}

// load loads a program as the build does; ok is false for one that does
// not load (diagnostics and review programs that fail earlier).
func (cp advProgram) load(t *testing.T) (*load.Program, bool) {
	t.Helper()
	dir := cp.dir
	if cp.file != "" {
		dir = testenv.TempDir(t)
		defer testenv.Remove(dir)
		src, err := compat.ReadFile(cp.file)
		if err != nil {
			t.Fatal(err.Error())
		}
		if err := os.WriteFile(filepath.Join(dir, "main.p"), src, 0o644); err != nil {
			t.Fatal(err.Error())
		}
	}
	tags := load.BuildTags(cp.freestanding, cp.kernel, nil)
	if !cp.freestanding && !cp.kernel {
		tags["gencheck"] = true
	}
	cfg := pu.New(load.Config{Tags: tags, Stdlib: testenv.Stdlib(), Sizes: types2.SizesFor("gc", "amd64")})
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
		data, err := compat.ReadFile(f)
		if err != nil {
			t.Fatal(err.Error())
		}
		if err := os.WriteFile(f, fn(f, data), 0o644); err != nil {
			t.Fatal(err.Error())
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
			t.Fatal(err.Error())
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
				s.Esc = compat.Append(s.Esc, k)
				for r := range s.Res {
					s.Res[r].From = compat.Append(s.Res[r].From, summary.ResultKey{Key: k, Field: domain.Root, FieldGE: true})
				}
			}
			return s
		})
	}},
	{"narrow", func(t *testing.T, dir string) {
		// Nothing escapes, nothing is stored, results hold nothing.
		eachSummary(t, dir, func(ps domain.Paths, s *summary.Summary) *summary.Summary {
			return pu.New(summary.Summary{Inputs: s.Inputs})
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
				t.Fatal(err.Error())
			}
			byInputs := map[int][]int{}
			for i, e := range es {
				_, n, _ := cache.DecodeAny(e.Sum, domain.NewPaths())
				byInputs[n] = compat.Append(byInputs[n], i)
			}
			sums := compat.Make[[]byte](len(es))
			for i, e := range es {
				sums[i] = e.Sum
			}
			for _, idx := range byInputs {
				for j, i := range idx {
					es[i].Sum = sums[idx[(j+1)%len(idx)]]
				}
			}
			if err := cache.WriteEntries(f, pkg, es); err != nil {
				t.Fatal(err.Error())
			}
		}
	}},
	{"scramble", func(t *testing.T, dir string) {
		// A byte of each summary changed, the checksum made valid: the
		// decoder sees bytes no encoder writes, or a different summary.
		for _, f := range cache.Files(dir) {
			pkg, es, err := cache.ReadEntries(f)
			if err != nil {
				t.Fatal(err.Error())
			}
			for i := range es {
				b := compat.Append([]byte(nil), es[i].Sum...)
				if len(b) > 0 {
					j := (i*7 + 3) % len(b)
					b[j] ^= byte(1 << (i % 8))
				}
				es[i].Sum = b
			}
			if err := cache.WriteEntries(f, pkg, es); err != nil {
				t.Fatal(err.Error())
			}
		}
	}},
	{"swap files", func(t *testing.T, dir string) {
		// Each package's file holds another package's.
		fs := cache.Files(dir)
		data := compat.Make[[]byte](len(fs))
		for i, f := range fs {
			data[i], _ = compat.ReadFile(f)
		}
		for i, f := range fs {
			if err := os.WriteFile(f, data[(i+1)%len(fs)], 0o644); err != nil {
				t.Fatal(err.Error())
			}
		}
	}},
}

func copyDir(t *testing.T, from, to string) {
	t.Helper()
	for _, f := range cache.Files(from) {
		data, err := compat.ReadFile(f)
		if err != nil {
			t.Fatal(err.Error())
		}
		if err := os.WriteFile(filepath.Join(to, filepath.Base(f)), data, 0o644); err != nil {
			t.Fatal(err.Error())
		}
	}
}

// TestAdversarial runs every mutation on the cache of every program of the
// corpus and the review suites (a sample of them unless
// PLUTO_ESCAPE_CACHE_FULL is set: the full suite takes minutes) and checks
// that the diagnostics are those of a run without a cache. It also checks
// that the mutations reach every way the driver has of refusing a file.
func TestAdversarial(t *testing.T) {
	if compat.Getenv("PLUTO_LONG_TESTS") == "" {
		t.Skip("runs the analysis many times over the corpus; set PLUTO_LONG_TESTS=1")
	}
	if genChecked {
		// Built with -gencheck=false the regions bound the test (eir
		// TestBuildCorpus: the corpus in 200 MB). With generation checks
		// each allocation of a custom allocator is registered
		// (rt.GenCustomAlloc) and scratch.Region.Release unregisters none,
		// so every program's records are kept: TestBuildCorpus passed
		// 20 GB 240 programs in.
		t.Skip("knownfail with generation checks: scratch.Region's allocations stay registered after Release; build with -gencheck=false")
	}
	progs := advCorpus(t)
	stride := 40
	if compat.Getenv("PLUTO_ESCAPE_CACHE_FULL") != "" {
		stride = 1
	}
	totals := map[string][5]int{} // by mutation: accepted, missed, rejected, redone, programs rejected
	t.Run("programs", func(t *testing.T) {
		for i, cp := range progs {
			if i%stride != 0 {
				continue
			}
			t.Run(cp.name, func(t *testing.T) {
				// The program's load and analyses are made in a region
				// of its own, released when the subtest ends (after the
				// directories are removed): nothing else frees them (Go's
				// GC did), and without the regions the test passed 24 GB
				// eight programs in. totals outlives the region and is
				// written from the heap.
				mem := scratch.NewRegion()
				defer mem.Release()
				use := mem.Use()
				defer use.Restore()
				prog, ok := cp.load(t)
				if !ok {
					t.Skip("does not load")
				}
				cold := analyse(t, prog, "")
				base := testenv.TempDir(t)
				defer testenv.Remove(base)
				first := analyse(t, prog, base)
				sameAsCold(t, "writing the cache", cold, first)
				empty := testenv.TempDir(t)
				defer testenv.Remove(empty)
				warm := analyse(t, prog, empty+"/none")
				sameAsCold(t, "an empty cache", cold, warm)
				for _, m := range mutations {
					// Removed at the end of the iteration: a defer may not
					// run twice in one call.
					dir := testenv.TempDir(t)
					copyDir(t, base, dir)
					m.fn(t, dir)
					got := analyse(t, prog, dir)
					sameAsCold(t, m.name, cold, got)
					// And the run after it, from the files it rewrote.
					again := analyse(t, prog, dir)
					sameAsCold(t, m.name+", then again", cold, again)
					tt := totals[m.name]
					tt[0] += got.stats.CacheAccepted
					tt[1] += got.stats.CacheMissed
					tt[2] += got.stats.CacheRejected
					tt[3] += got.stats.CacheRedone
					if cold.diags != "" {
						tt[4]++
					}
					h := scratch.Heap()
					totals[m.name] = tt
					h.Restore()
					testenv.Remove(dir)
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
	need := []advNeed{
		{"corrupt", 1, "missed"}, {"truncate", 1, "missed"}, {"swap files", 1, "missed"},
		{"narrow", 2, "rejected"}, {"narrow one", 2, "rejected"}, {"swap entries", 2, "rejected"},
		{"widen", 0, "accepted"}, {"widen", 3, "redone"},
	}
	for _, n := range need {
		// totals[n.m][n.i] crashes the release compiler's lowerer (an
		// array element of a map element; fixed in internal/lower).
		tt := totals[n.m]
		if tt[n.i] == 0 {
			t.Errorf("mutation %s: no SCC %s: the suite does not reach that case", n.m, n.what)
		}
	}
}

type advNeed struct {
	m    string
	i    int
	what string
}
