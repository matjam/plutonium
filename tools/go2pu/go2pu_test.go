package main

import (
	"bytes"
	"errors"
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

// The checked-in translations under selfhost/, and where they come from.
var translations = []struct {
	dir  string // Go package, relative to this directory
	demo bool   // a main package under testdata, translated to selfhost/demo
}{
	{dir: "../../internal/objfile"},
	{dir: "../../internal/ssa"},
	{dir: "../../internal/arch/amd64"},
	{dir: "../../internal/opt"}, // magic.go is ported by hand (math/big)
	{dir: "testdata/objdemo", demo: true},
	{dir: "testdata/ssademo", demo: true},
	{dir: "testdata/amd64demo", demo: true},
	{dir: "testdata/optdemo", demo: true},
	{dir: "testdata/rewrites", demo: true},
	{dir: "testdata/scopes", demo: true}, // WaitGroups rewritten to Scopes
	{dir: "testdata/pudemo", demo: true}, // internal/pu, translated name for name
}

const selfhostDir = "../../selfhost"

func testConfig() *Config {
	cfg := &Config{OutDir: selfhostDir, Module: "selfhost", PlutoRoot: "../../lib", Imports: map[string]string{}}
	for _, tr := range translations {
		if tr.demo {
			name := filepath.Base(tr.dir)
			cfg.Imports[goModule+"/tools/go2pu/testdata/"+name] = "selfhost/demo/" + name
		}
	}
	cfg.Imports[goModule+"/tools/go2pu/testdata/handfix"] = "selfhost/demo/handfix"
	return cfg
}

// TestTranslationsUpToDate translates each package again and checks that
// selfhost/ holds exactly the result. Regenerate the translations by
// running this test with GO2PU_UPDATE=1.
func TestTranslationsUpToDate(t *testing.T) {
	cfg := testConfig()
	update := os.Getenv("GO2PU_UPDATE") != ""
	for _, tr := range translations {
		res, err := TranslateDir(cfg, tr.dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, n := range res.Notes {
			t.Logf("%s: %s", res.GoPath, n)
		}
		if update {
			if err := res.Write(selfhostDir); err != nil {
				t.Fatal(err)
			}
			continue
		}
		dir, err := res.Dir(selfhostDir)
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range res.Files {
			got, err := os.ReadFile(filepath.Join(dir, f.Name))
			if err != nil {
				t.Errorf("%v (run with GO2PU_UPDATE=1)", err)
				continue
			}
			if !bytes.Equal(got, f.Src) {
				t.Errorf("%s/%s is stale: run this test with GO2PU_UPDATE=1", dir, f.Name)
			}
		}
	}
}

// TestDifferential builds each demo with Go and its translation with the
// Plutonium compiler, at -O0 and -O2, and compares their output.
func TestDifferential(t *testing.T) {
	testguard.Parallel(t)
	if _, err := exec.LookPath("ld"); err != nil {
		t.Skip("no linker")
	}
	for _, tr := range translations {
		if !tr.demo {
			continue
		}
		name := filepath.Base(tr.dir)
		// The demos and their levels build and run in parallel, after
		// TestTranslationsUpToDate (which is not parallel) has checked
		// or regenerated the translations.
		t.Run(name, func(t *testing.T) {
			testguard.Parallel(t)
			goBin := filepath.Join(t.TempDir(), "go-"+name)
			if out, err := exec.Command("go", "build", "-o", goBin, "./"+tr.dir).CombinedOutput(); err != nil {
				t.Fatalf("go build: %v\n%s", err, out)
			}
			want := run(t, goBin)
			for _, level := range []int{0, 2} {
				t.Run(fmt.Sprintf("O%d", level), func(t *testing.T) {
					testguard.Parallel(t)
					puBin := filepath.Join(t.TempDir(), "pu-"+name)
					cfg := &build.Config{Dir: filepath.Join(selfhostDir, "demo", name), Output: puBin,
						Stdlib: plutonium.Stdlib(), OptLevel: level}
					if err := build.Build(cfg); err != nil {
						t.Fatal(err)
					}
					if got := run(t, puBin); got != want {
						t.Errorf("output differs\n--- Plutonium ---\n%s\n--- Go ---\n%s", got, want)
					}
				})
			}
		})
	}
}

func run(t *testing.T, bin string) string {
	t.Helper()
	var stdout, stderr bytes.Buffer
	cmd := exec.Command(bin)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("%s: %v\n%s", filepath.Base(bin), err, stderr.String())
	}
	return stdout.String()
}

// TestCompat runs the compat package's own tests.
func TestCompat(t *testing.T) {
	testguard.Parallel(t)
	if _, err := exec.LookPath("ld"); err != nil {
		t.Skip("no linker")
	}
	bin := filepath.Join(t.TempDir(), "compat.test")
	cfg := &build.Config{Output: bin, Stdlib: plutonium.Stdlib()}
	if _, err := build.BuildTest(cfg, filepath.Join(selfhostDir, "compat"), &build.TestOptions{}); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(bin).CombinedOutput()
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
}

// TestHandFixNotes checks that each construct go2pu cannot translate is
// reported.
func TestHandFixNotes(t *testing.T) {
	res, err := TranslateDir(testConfig(), "testdata/handfix")
	if err != nil {
		t.Fatal(err)
	}
	all := strings.Join(res.Notes, "\n")
	for _, want := range []string{
		"closure escapes its frame (returned)",
		"closure escapes its frame (passed to a function that keeps it)",
		"in an interface: Plutonium boxes it",
		"recover:",
		"channel type",
		"range over a function",
		"defer in a loop",
		"sort comparison uses its indexes",
		"encoding/binary.BigEndian.PutUint32: no translation",
		`string literal "\x7fELF\xff" is not valid UTF-8: keep binary data in a []byte`,
		"complex: Plutonium has no complex numbers",
		"pluto/os has no Hostname",
		"strconv.AppendQuote: Plutonium's takes a vec.Appender destination",
		`no Plutonium package for import "iter"`,
	} {
		if !strings.Contains(all, want) {
			t.Errorf("no note containing %q in:\n%s", want, all)
		}
	}
	if res.OutPath != "selfhost/demo/handfix" {
		t.Errorf("output path %q", res.OutPath)
	}
	// x is captured by a closure that outlives main, so it moves to the
	// heap. Maps translate as they are, deleting while ranging included.
	src := string(res.Files[0].Src)
	for _, want := range []string{"x := pu.New(1)", "(*x)++", "m := map[string]int{\"a\": 1}", "for k := range m {\n\t\tdelete(m, k)", `compat.Runes("é")`, "task /* go2pu: closure escapes its frame (passed to a function that keeps it) */ func(v int) { ch <- v }(c())"} {
		if !strings.Contains(src, want) {
			t.Errorf("translation lacks %q:\n%s", want, src)
		}
	}
}

func TestMapFlag(t *testing.T) {
	m := mapFlag{}
	if err := m.Set("a/b=c/d"); err != nil || m["a/b"] != "c/d" {
		t.Fatalf("Set: %v %v", err, m)
	}
	if err := m.Set("nope"); err == nil {
		t.Fatal("Set without = succeeded")
	}
	if !strings.Contains(m.String(), "a/b") {
		t.Fatalf("String: %s", m.String())
	}
}

func TestMapPath(t *testing.T) {
	tc := &translator{cfg: testConfig()}
	for from, want := range map[string]string{
		goModule:                   "selfhost",
		goModule + "/internal/ssa": "selfhost/internal/ssa",
		"strings":                  "pluto/strings",
		"debug/elf":                "selfhost/elf",
		"encoding/binary":          "",
		"reflect":                  "",
		goModule + "/tools/go2pu/testdata/ssademo": "selfhost/demo/ssademo",
	} {
		if got := tc.mapPath(from); got != want {
			t.Errorf("mapPath(%q) = %q, want %q", from, got, want)
		}
	}
}

func TestErrors(t *testing.T) {
	if _, err := TranslateDir(testConfig(), "testdata/nonexistent"); err == nil {
		t.Error("translating a missing directory succeeded")
	}
	if _, _, err := findGoMod(string(filepath.Separator)); err == nil {
		t.Error("found a go.mod above /")
	}
	r := &Result{GoPath: "x", OutPath: "nomodule"}
	if _, err := r.Dir("out"); err == nil {
		t.Error("Dir of a path outside a module succeeded")
	}
	if err := r.Write("out"); err == nil {
		t.Error("Write of a path outside a module succeeded")
	}
	var notDir *os.PathError
	if _, err := goFiles("go2pu_test.go"); !errors.As(err, &notDir) {
		t.Errorf("goFiles of a file: %v", err)
	}
}

func TestExports(t *testing.T) {
	names := map[string]bool{}
	addExports(names, "package x\n\nfunc F() {}\nfunc (b *Buf) Write() {}\nfunc (v *Vec[T]) Push() {}\ntype T int\nconst C = 1\nvar (\n\tV = 2\n\tw = 3\n)\nfunc lower() {}\n")
	for _, want := range []string{"F", "Buf.Write", "Vec.Push", "T", "C", "V"} {
		if !names[want] {
			t.Errorf("missing %s in %v", want, names)
		}
	}
	if names["w"] || names["lower"] {
		t.Errorf("unexported names listed: %v", names)
	}
	ex := plutoExports("../../lib")
	if !ex["pluto/strings"]["HasPrefix"] || !ex["pluto/hash"]["Map.Set"] {
		t.Errorf("standard library exports not found")
	}
}
