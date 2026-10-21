package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	plutonium "github.com/matjam/plutonium"
	"github.com/matjam/plutonium/internal/build"
	"github.com/matjam/plutonium/internal/testguard"
)

// A mixedProgram is a program of the test corpus, and the build settings it
// is compiled with.
type mixedProgram struct {
	name string
	dir  string // a directory with the program, or
	file string // a single .p file, built as main.p
	cfg  build.Config
}

// mixedCorpus lists the programs of the repository's tests: the examples, and
// every program in testdata/run, lib, plutonium and trace. Each is lowered
// with its default settings; a few also with the settings that change what
// the front end hands the back end (no generation checks, the debug
// allocator) or what the back end writes (no trace table, no DWARF).
func mixedCorpus(t *testing.T) []mixedProgram {
	t.Helper()
	var out []mixedProgram
	examples, _ := filepath.Glob(filepath.Join(repoRoot, "examples", "*", "main.p"))
	for _, f := range examples {
		out = append(out, mixedProgram{name: "examples/" + filepath.Base(filepath.Dir(f)), dir: filepath.Dir(f)})
	}
	// The kernel programs: sched, smoke and gsuser in kernel mode (no floating
	// point, per-CPU data), irq freestanding. (panic builds only with the
	// scenario files its test adds.)
	for _, k := range []struct {
		name string
		cfg  build.Config
	}{
		{"sched", build.Config{Kernel: true}}, {"smoke", build.Config{Kernel: true}},
		{"gsuser", build.Config{Kernel: true}}, {"irq", build.Config{Freestanding: true}},
	} {
		out = append(out, mixedProgram{name: "kernel/" + k.name, dir: filepath.Join(repoRoot, "testdata", "kernel", k.name), cfg: k.cfg})
	}
	out = append(out, mixedProgram{name: "cabi", dir: filepath.Join(repoRoot, "testdata", "cabi")})
	for _, d := range []string{"run", "lib", "plutonium", "trace"} {
		files, _ := filepath.Glob(filepath.Join(repoRoot, "testdata", d, "*.p"))
		for _, f := range files {
			out = append(out, mixedProgram{name: d + "/" + strings.TrimSuffix(filepath.Base(f), ".p"), file: f})
		}
	}
	if len(out) < 200 {
		t.Fatalf("only %d programs in the corpus", len(out))
	}
	return out
}

// mixedVariants are the settings a sample of the corpus is also built with.
var mixedVariants = []struct {
	name string
	cfg  build.Config
	// every says how many programs of the corpus use it: one in every.
	every int
}{
	{"nogencheck", build.Config{NoGenCheck: true}, 7},
	{"debugalloc", build.Config{Alloc: "debug"}, 11},
	{"notrace", build.Config{NoTrace: true}, 13},
	{"nodwarf", build.Config{NoDWARF: true}, 13},
}

// TestMixedPipeline runs the mixed pipeline of the self-hosting plan (section
// 6.3): the Go front end lowers a program and writes its SSA as text; the
// back end built from the Plutonium translation (selfhost/demo/ssaback: the
// reader, the optimiser, the code generator and the object writer) reads the
// text and writes the object file; and the object must equal, byte for byte,
// the one the Go back end writes for the same program. Every program of the
// corpus runs at -O0 and -O2.
//
// MIXED_ONLY=<substring> runs the programs whose names contain it.
func TestMixedPipeline(t *testing.T) {
	testguard.Parallel(t)
	bin := demoBinary(t, "ssaback", 2, true)
	only := os.Getenv("MIXED_ONLY")
	for i, p := range mixedCorpus(t) {
		if !strings.Contains(p.name, only) {
			continue
		}
		cases := []mixedProgram{p}
		for _, v := range mixedVariants {
			if i%v.every == 0 && !p.cfg.Kernel && !p.cfg.Freestanding {
				c := p
				c.name += "+" + v.name
				c.cfg = mixedMerge(c.cfg, v.cfg)
				cases = append(cases, c)
			}
		}
		for _, c := range cases {
			t.Run(c.name, func(t *testing.T) {
				testguard.Parallel(t)
				dir := c.dir
				if c.file != "" {
					dir = t.TempDir()
					src, err := os.ReadFile(c.file)
					if err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(filepath.Join(dir, "main.p"), src, 0o644); err != nil {
						t.Fatal(err)
					}
				}
				for _, level := range []int{0, 2} {
					t.Run(fmt.Sprintf("O%d", level), func(t *testing.T) {
						testguard.Parallel(t)
						cfg := c.cfg
						cfg.Dir, cfg.Stdlib, cfg.OptLevel = dir, plutonium.Stdlib(), level
						mixedCheck(t, bin, &cfg)
					})
				}
			})
		}
	}
}

// mixedCheck compiles the program in cfg both ways and compares the objects.
func mixedCheck(t *testing.T, bin string, cfg *build.Config) {
	t.Helper()
	// The Go compile takes a parallel slot's memory, reserved so that it
	// waits while the translated back end runs alone on a large program.
	front := testguard.Reserve(t, testguard.DefaultSlotMB)
	file, size, want := mixedGo(t, cfg)
	front()
	// The translated back end takes up to 8.5 GB on the corpus: reserve
	// its estimate, so that the run does not start more of them than fit.
	release := testguard.Reserve(t, ssabackPeakMB(size, cfg.OptLevel))
	defer release()
	args := []string{"-O", fmt.Sprint(cfg.OptLevel)}
	if cfg.NoTrace {
		args = append(args, "-notrace")
	}
	if cfg.NoDWARF {
		args = append(args, "-nodwarf")
	}
	var stdout, stderr bytes.Buffer
	cmd := exec.Command(bin, append(args, file)...)
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("ssaback: %v\n%s", err, stderr.String())
	}
	got := stdout.Bytes()
	if !bytes.Equal(got, want) {
		at := 0
		for at < len(got) && at < len(want) && got[at] == want[at] {
			at++
		}
		t.Errorf("the objects differ: %d bytes from the Plutonium back end, %d from the Go back end, the first difference at byte %d (%d bytes of text)",
			len(got), len(want), at, size)
	}
}

// mixedGo compiles the program in cfg with the Go back end, writes the SSA
// text it gives the translated back end to a file, and returns the file, the
// text's size and the Go back end's object. Nothing of the build outlives
// it, so a test waiting for memory to run the translated back end holds
// only the object.
func mixedGo(t *testing.T, cfg *build.Config) (file string, size int, want []byte) {
	t.Helper()
	l, err := build.Frontend(cfg)
	if err != nil {
		t.Fatal(err)
	}
	text, err := l.Text(cfg)
	if err != nil {
		t.Fatal(err)
	}
	file = filepath.Join(t.TempDir(), "program.ssat")
	if err := os.WriteFile(file, text, 0o644); err != nil {
		t.Fatal(err)
	}
	if keep := os.Getenv("MIXED_KEEP"); keep != "" {
		// For looking at the text and timing the back end by hand.
		name := fmt.Sprintf("%s-O%d.ssat", strings.ReplaceAll(t.Name(), "/", "_"), cfg.OptLevel)
		if err := os.WriteFile(filepath.Join(keep, name), text, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err = l.Object(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return file, len(text), want
}

// ssabackPeakMB estimates the peak memory, in MB, of the translated back
// end compiling size bytes of SSA text at the level. It holds what it
// allocates, so its peak grows faster than the text: over the corpus it
// was 60 to 90 bytes for each byte of text at -O0, and at -O2 150 bytes
// for a text of 1 MB rising to 650 for one of 5 MB or more (8.5 GB for the
// 13.7 MB of lib/strconvfloat). The estimate is above every measurement
// (docs/verification/2026-10-21-test-memory.md).
func ssabackPeakMB(size, level int) uint64 {
	perByte := uint64(100)
	if level > 0 {
		perByte = min(660, 150+130*uint64(size)>>20)
	}
	return max(uint64(size)*perByte>>20, 64)
}

// mixedMerge returns base with the settings of v added.
func mixedMerge(base, v build.Config) build.Config {
	base.NoGenCheck = base.NoGenCheck || v.NoGenCheck
	base.NoTrace = base.NoTrace || v.NoTrace
	base.NoDWARF = base.NoDWARF || v.NoDWARF
	if v.Alloc != "" {
		base.Alloc = v.Alloc
	}
	return base
}

// TestMixedPipelineChecked runs one program through the back end built with
// generation checks (the default of hosted builds; TestMixedPipeline builds
// it unchecked for speed), so that a stale pointer in the translated back end
// is a failure here.
func TestMixedPipelineChecked(t *testing.T) {
	testguard.Parallel(t)
	bin := demoBinary(t, "ssaback", 2, false)
	for _, level := range []int{0, 2} {
		t.Run(fmt.Sprintf("O%d", level), func(t *testing.T) {
			testguard.Parallel(t)
			c := build.Config{Dir: filepath.Join(repoRoot, "examples", "07-hello"), Stdlib: plutonium.Stdlib(), OptLevel: level}
			mixedCheck(t, bin, &c)
		})
	}
}

// TestSSAbackPeak checks the estimate of the translated back end's memory
// against measurements of it (size of the text, level, peak in MB).
func TestSSAbackPeak(t *testing.T) {
	testguard.Parallel(t)
	for _, m := range []struct {
		size, level int
		peakMB      uint64
	}{
		{1171059, 2, 168}, {1510112, 2, 263}, {1534754, 2, 273}, {1942952, 2, 383},
		{2902434, 2, 552}, {3474164, 2, 607}, {4052624, 2, 1991}, {4780940, 2, 1372},
		{5484439, 2, 2608}, {5580504, 2, 3059}, {6167616, 2, 3824}, {6919562, 2, 4167},
		{7369628, 2, 3465}, {13658562, 2, 8488},
		{4052624, 0, 248}, {4780940, 0, 416}, {7369628, 0, 448}, {13658562, 0, 1002},
	} {
		if got := ssabackPeakMB(m.size, m.level); got < m.peakMB {
			t.Errorf("ssabackPeakMB(%d, %d) = %d, below the %d MB measured", m.size, m.level, got, m.peakMB)
		}
	}
}
