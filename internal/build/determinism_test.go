package build_test

import (
	"bytes"
	"debug/elf"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	plutonium "github.com/matjam/plutonium"
	"github.com/matjam/plutonium/internal/build"
	"github.com/matjam/plutonium/internal/testguard"
)

// frameStore matches a store to the stack frame, which the spill program
// must contain for the test to cover spilling.
var frameStore = regexp.MustCompile(`, -?\d+\(%r[sb]p\)`)

// TestDeterministic compiles programs several times, each time in a new
// directory, and checks that the listing and the object file are
// byte-identical each time: self-hosting compares the output of two
// compiler stages, so nothing may depend on map iteration order or on
// where the source is. spills.p keeps many values live across calls;
// ifaces.p and generics.p exercise type descriptors, itabs and
// instantiation.
func TestDeterministic(t *testing.T) {
	testguard.Parallel(t)
	const runs = 4
	for _, name := range []string{"spills", "ifaces", "generics"} {
		src, err := os.ReadFile(filepath.Join("..", "..", "testdata", "run", name+".p"))
		if err != nil {
			t.Fatal(err)
		}
		for _, level := range []int{0, 2} {
			t.Run(name+"/O"+string(rune('0'+level)), func(t *testing.T) {
				testguard.Parallel(t)
				var firstAsm, firstObj []byte
				for run := range runs {
					asm, obj := compileObject(t, t.TempDir(), src, level)
					if run == 0 {
						firstAsm, firstObj = asm, obj
						if name == "spills" && !frameStore.Match(asm) {
							t.Fatal("listing has no stores to the stack frame: the program no longer spills")
						}
						continue
					}
					if !bytes.Equal(asm, firstAsm) {
						t.Fatalf("run %d: listing differs from run 0", run)
					}
					if !bytes.Equal(obj, firstObj) {
						t.Fatalf("run %d: object file differs from run 0", run)
					}
				}
			})
		}
	}
}

// compileObject compiles src as a main package in dir to an object file and
// returns its listing and the object's bytes.
func compileObject(t *testing.T, dir string, src []byte, optLevel int) (asm, obj []byte) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "main.p"), src, 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "main.o")
	var listing bytes.Buffer
	cfg := &build.Config{Dir: dir, Output: out, Stdlib: plutonium.Stdlib(), OptLevel: optLevel,
		Listing: &listing, CompileOnly: true}
	if err := buildCfg(cfg); err != nil {
		t.Fatalf("build: %v", err)
	}
	obj, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	return listing.Bytes(), obj
}

// reproModule is a module whose main package, in a subdirectory, imports a
// package of the module that panics, so that its trace names files in two
// directories of the module.
var reproModule = map[string]string{
	"pluto.mod": "module example.com/repro\n",
	"cmd/repro/main.p": `package main

import "example.com/repro/geom"

func main() {
	println(geom.Area(3, 4))
	println(geom.Area(-1, 4))
}
`,
	"geom/geom.p": `package geom

func Area(w, h int) int {
	if w < 0 {
		panic("negative width")
	}
	return w * h
}
`,
}

// TestReproducible builds the same programs from two different
// directories, at -O0 and -O2, with and without DWARF, and checks that the
// linked executables are byte-identical: the trace table and DWARF name
// source files relative to the module root and the standard library, and
// nothing else in the output depends on where the source or the work
// directory is. It also checks the paths a panic prints, and that
// --trimpath=false records the directory, so differs.
func TestReproducible(t *testing.T) {
	testguard.Parallel(t)
	cabi, err := readTree(filepath.Join("..", "..", "testdata", "cabi"))
	if err != nil {
		t.Fatal(err)
	}
	programs := []struct {
		name  string
		files map[string]string
		dir   string // main package, relative to the root
		cc    bool   // has C files
	}{
		{"module", reproModule, "cmd/repro", false},
		{"cabi", cabi, ".", true},
	}
	for _, p := range programs {
		for _, level := range []int{0, 2} {
			for _, noDWARF := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/O%d/g=%v", p.name, level, !noDWARF), func(t *testing.T) {
					testguard.Parallel(t)
					if p.cc {
						needCC(t)
					}
					cfg := build.Config{OptLevel: level, NoDWARF: noDWARF}
					// The second root is deeper, so that no path
					// accidentally has the same length.
					a := buildTree(t, filepath.Join(t.TempDir(), "a"), p.files, p.dir, cfg)
					b := buildTree(t, filepath.Join(t.TempDir(), "elsewhere", "b"), p.files, p.dir, cfg)
					if !bytes.Equal(a, b) {
						t.Fatalf("executables built in two directories differ (%d and %d bytes)", len(a), len(b))
					}
					checkNoBuildID(t, a)
				})
			}
		}
	}

	// The trace names the module's files relative to its root.
	root := filepath.Join(t.TempDir(), "m")
	buildTree(t, root, reproModule, "cmd/repro", build.Config{OptLevel: 2})
	got, status := runProg(t, filepath.Join(root, "prog"))
	want := "12\npanic: negative width\n" +
		"    example.com/repro/geom.Area (./geom/geom.p:5)\n" +
		"    example.com/repro/cmd/repro.main (./cmd/repro/main.p:7)\n"
	if status != 2 || got != want {
		t.Errorf("status %d, output:\n%s\nwant:\n%s", status, got, want)
	}

	// --trimpath=false records the absolute path, so the directory shows.
	cfg := build.Config{OptLevel: 2, NoTrimPath: true}
	a := buildTree(t, filepath.Join(t.TempDir(), "a"), reproModule, "cmd/repro", cfg)
	b := buildTree(t, filepath.Join(t.TempDir(), "elsewhere", "b"), reproModule, "cmd/repro", cfg)
	if bytes.Equal(a, b) {
		t.Errorf("--trimpath=false: executables built in two directories are identical")
	}
}

// buildTree writes files under root, builds the main package in root/dir
// with cfg's settings to root/prog and returns the executable.
func buildTree(t *testing.T, root string, files map[string]string, dir string, cfg build.Config) []byte {
	t.Helper()
	for name, src := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cfg.Dir = filepath.Join(root, filepath.FromSlash(dir))
	cfg.Output = filepath.Join(root, "prog")
	cfg.Stdlib = plutonium.Stdlib()
	if err := buildCfg(&cfg); err != nil {
		t.Fatalf("build: %v", err)
	}
	data, err := os.ReadFile(cfg.Output)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// readTree reads the regular files under dir, by slash-separated relative
// name.
func readTree(dir string) (map[string]string, error) {
	files := map[string]string{}
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		files[filepath.ToSlash(rel)] = string(data)
		return nil
	})
	return files, err
}

// checkNoBuildID checks that the executable has no build ID note, which ld
// adds or not depending on how it was configured.
func checkNoBuildID(t *testing.T, exe []byte) {
	t.Helper()
	f, err := elf.NewFile(bytes.NewReader(exe))
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range f.Sections {
		if strings.Contains(s.Name, "build-id") {
			t.Errorf("executable has section %s", s.Name)
		}
	}
}
