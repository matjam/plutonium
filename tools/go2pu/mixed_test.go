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
	bin := filepath.Join(t.TempDir(), "ssaback")
	cfg := &build.Config{Dir: filepath.Join(selfhostDir, "demo", "ssaback"), Output: bin,
		Stdlib: plutonium.Stdlib(), OptLevel: 2, NoGenCheck: true}
	if err := build.Build(cfg); err != nil {
		t.Fatal(err)
	}
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
	l, err := build.Frontend(cfg)
	if err != nil {
		t.Fatal(err)
	}
	text, err := l.Text(cfg)
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "program.ssat")
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
	want, err := l.Object(cfg)
	if err != nil {
		t.Fatal(err)
	}
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
			len(got), len(want), at, len(text))
	}
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
	bin := filepath.Join(t.TempDir(), "ssaback")
	cfg := &build.Config{Dir: filepath.Join(selfhostDir, "demo", "ssaback"), Output: bin,
		Stdlib: plutonium.Stdlib(), OptLevel: 2}
	if err := build.Build(cfg); err != nil {
		t.Fatal(err)
	}
	for _, level := range []int{0, 2} {
		t.Run(fmt.Sprintf("O%d", level), func(t *testing.T) {
			testguard.Parallel(t)
			c := build.Config{Dir: filepath.Join(repoRoot, "examples", "07-hello"), Stdlib: plutonium.Stdlib(), OptLevel: level}
			mixedCheck(t, bin, &c)
		})
	}
}
