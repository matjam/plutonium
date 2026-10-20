package main

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	plutonium "github.com/matjam/plutonium"
	"github.com/matjam/plutonium/internal/build"
	"github.com/matjam/plutonium/internal/testguard"
)

// TestTypeCheck runs the typecheck demo, built from the Go loader and type
// checker and from their translation (selfhost/internal/load, types2 and the
// packages they import), over the programs and packages of the repository:
// the examples, the programs of testdata (run, lib, plutonium, trace) and of
// the diagnostics goldens (most of which are rejected), every package of the
// standard library, and the translations under selfhost, which are Plutonium
// packages no Go program corresponds to. For each, the two builds must print
// the same text: the errors, or the declarations of the main package and a
// digest of everything the checker recorded of it and of every package it
// imports (see testdata/typecheck).
//
// It takes minutes and several GB, so it runs only with GO2PU_SLOW=1.
// TYPECHECK_ONLY=<substring> runs the directories whose names contain it.
func TestTypeCheck(t *testing.T) {
	testguard.Parallel(t)
	if os.Getenv("GO2PU_SLOW") == "" {
		t.Skip("builds the translated type checker (set GO2PU_SLOW=1)")
	}
	if _, err := exec.LookPath("ld"); err != nil {
		t.Skip("no linker")
	}
	work := t.TempDir()
	dirs := typeCheckDirs(t, work)
	if only := os.Getenv("TYPECHECK_ONLY"); only != "" {
		var keep []string
		for _, d := range dirs {
			if strings.Contains(d, only) {
				keep = append(keep, d)
			}
		}
		dirs = keep
	}
	t.Logf("%d directories", len(dirs))
	goBin := filepath.Join(work, "typecheck-go")
	if out, err := exec.Command("go", "build", "-o", goBin, "./testdata/typecheck").CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	lib, err := filepath.Abs(filepath.Join(repoRoot, "lib"))
	if err != nil {
		t.Fatal(err)
	}
	for _, level := range []int{0, 2} {
		t.Run(fmt.Sprintf("O%d", level), func(t *testing.T) {
			testguard.Parallel(t)
			puBin := filepath.Join(t.TempDir(), "typecheck-pu")
			cfg := &build.Config{Dir: filepath.Join(selfhostDir, "demo", "typecheck"), Output: puBin,
				Stdlib: plutonium.Stdlib(), OptLevel: level}
			if err := build.Build(cfg); err != nil {
				t.Fatal(err)
			}
			const batch = 20
			mismatched := 0
			for i := 0; i < len(dirs); i += batch {
				part := dirs[i:min(i+batch, len(dirs))]
				want, got := runTypeCheck(t, goBin, lib, part), runTypeCheck(t, puBin, lib, part)
				if bytes.Equal(want, got) {
					continue
				}
				for _, d := range part {
					w, g := runTypeCheck(t, goBin, lib, []string{d}), runTypeCheck(t, puBin, lib, []string{d})
					if !bytes.Equal(w, g) {
						mismatched++
						if mismatched <= 5 {
							t.Errorf("%s: the dumps differ\n%s", d, firstDifference(w, g))
						}
					}
				}
			}
			if mismatched > 0 {
				t.Errorf("%d of %d directories differ", mismatched, len(dirs))
			}
		})
	}
}

// runTypeCheck runs bin on the directories and returns its output.
func runTypeCheck(t *testing.T, bin, lib string, dirs []string) []byte {
	t.Helper()
	var stdout, stderr bytes.Buffer
	cmd := exec.Command(bin, append([]string{lib}, dirs...)...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("%s %s: %v\n%s", filepath.Base(bin), strings.Join(dirs[:min(len(dirs), 3)], " "), err, stderr.String())
	}
	return stdout.Bytes()
}

// typeCheckDirs lists the directories of the corpus, making in work the ones
// that do not exist: a directory for each single-file program, a program that
// imports each library package, and a copy of selfhost with a program that
// imports each of its packages.
func typeCheckDirs(t *testing.T, work string) []string {
	t.Helper()
	var dirs []string
	add := func(rel string, files map[string]string) {
		dir := filepath.Join(work, "gen", rel)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		for name, src := range files {
			if err := os.WriteFile(filepath.Join(dir, name), []byte(src), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		dirs = append(dirs, dir)
	}
	examples, _ := filepath.Glob(filepath.Join(repoRoot, "examples", "*", "main.p"))
	for _, f := range examples {
		d, _ := filepath.Abs(filepath.Dir(f))
		dirs = append(dirs, d)
	}
	for _, d := range []string{"run", "lib", "plutonium", "trace"} {
		files, _ := filepath.Glob(filepath.Join(repoRoot, "testdata", d, "*.p"))
		for _, f := range files {
			src, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			add(d+"/"+strings.TrimSuffix(filepath.Base(f), ".p"), map[string]string{"main.p": string(src)})
		}
	}
	goldens, _ := filepath.Glob(filepath.Join(diagGoldens, "*"))
	for _, g := range goldens {
		if _, err := os.Stat(filepath.Join(g, "command")); err == nil {
			continue
		}
		if _, err := os.Stat(filepath.Join(g, "flags")); err == nil {
			continue
		}
		if filepath.Base(g) == "invalid_utf8" {
			// A Plutonium string holds valid UTF-8, so the translated
			// checker cannot see the bytes of a literal that is not: it
			// reports a malformed constant where Go says why.
			continue
		}
		d, _ := filepath.Abs(g)
		dirs = append(dirs, d)
	}
	// Every package of the standard library, from a program that imports it.
	libRoot := filepath.Join(repoRoot, "lib")
	var libPkgs []string
	_ = filepath.WalkDir(filepath.Join(libRoot, "pluto"), func(path string, d fs.DirEntry, err error) error {
		if err != nil || !d.IsDir() {
			return err
		}
		files, _ := filepath.Glob(filepath.Join(path, "*.p"))
		for _, f := range files {
			if !strings.HasSuffix(f, "_test.p") {
				rel, _ := filepath.Rel(libRoot, path)
				libPkgs = append(libPkgs, filepath.ToSlash(rel))
				break
			}
		}
		return nil
	})
	sort.Strings(libPkgs)
	for _, p := range libPkgs {
		add("pkg/"+p, map[string]string{"main.p": "package main\n\nimport _ \"" + p + "\"\n\nfunc main() {}\n"})
	}
	// The translations: Plutonium packages of the module selfhost.
	root := filepath.Join(work, "selfhost")
	var pkgs []string
	err := filepath.WalkDir(selfhostDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(selfhostDir, path)
		if d.IsDir() {
			return os.MkdirAll(filepath.Join(root, rel), 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.HasSuffix(path, ".p") && !strings.HasSuffix(path, "_test.p") && !strings.HasPrefix(rel, "demo"+string(filepath.Separator)) {
			pkgs = append(pkgs, "selfhost/"+filepath.ToSlash(filepath.Dir(rel)))
		}
		return os.WriteFile(filepath.Join(root, rel), data, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(pkgs)
	for i, p := range pkgs {
		if i > 0 && p == pkgs[i-1] {
			continue
		}
		dir := filepath.Join(root, "typecheck_"+strings.ReplaceAll(strings.TrimPrefix(p, "selfhost/"), "/", "_"))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		src := "package main\n\nimport _ \"" + p + "\"\n\nfunc main() {}\n"
		if err := os.WriteFile(filepath.Join(dir, "main.p"), []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
		dirs = append(dirs, dir)
	}
	if len(dirs) < 150 {
		t.Fatalf("only %d directories in the corpus", len(dirs))
	}
	return dirs
}
