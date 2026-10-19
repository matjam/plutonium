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
	"unicode/utf8"

	plutonium "github.com/matjam/plutonium"
	"github.com/matjam/plutonium/internal/build"
	"github.com/matjam/plutonium/internal/testguard"
)

// repoRoot is the root of the repository, relative to this directory.
const repoRoot = "../.."

// TestParseCheck runs the parsecheck demo, built from the Go syntax packages
// and from their translation (selfhost/internal/syntax), over every .p and
// .go file of the repository (the tests' inputs, the library, the examples,
// the translations themselves) and over generated inputs that make the
// parser report errors, give up (too many errors, nesting past its limit)
// or meet bytes Go does not accept. The two builds must print the same
// dump for every file: the parser's sorted errors, then every node of the
// tree with its kind, position, end and detail, then the comments.
func TestParseCheck(t *testing.T) {
	testguard.Parallel(t)
	if _, err := exec.LookPath("ld"); err != nil {
		t.Skip("no linker")
	}
	goBin := filepath.Join(t.TempDir(), "parsecheck-go")
	if out, err := exec.Command("go", "build", "-o", goBin, "./testdata/parsecheck").CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	files := parseCheckFiles(t)
	t.Logf("%d files", len(files))
	for _, level := range []int{0, 2} {
		t.Run(fmt.Sprintf("O%d", level), func(t *testing.T) {
			testguard.Parallel(t)
			puBin := filepath.Join(t.TempDir(), "parsecheck-pu")
			cfg := &build.Config{Dir: filepath.Join(selfhostDir, "demo", "parsecheck"), Output: puBin,
				Stdlib: plutonium.Stdlib(), OptLevel: level}
			if err := build.Build(cfg); err != nil {
				t.Fatal(err)
			}
			const batch = 100
			mismatched := 0
			for i := 0; i < len(files); i += batch {
				part := files[i:min(i+batch, len(files))]
				want, got := runParseCheck(t, goBin, part), runParseCheck(t, puBin, part)
				if bytes.Equal(want, got) {
					continue
				}
				// Find the files that differ.
				for _, name := range part {
					w, g := runParseCheck(t, goBin, []string{name}), runParseCheck(t, puBin, []string{name})
					if errorsOnly[filepath.Base(name)] {
						w, g = errorLines(w), errorLines(g)
					}
					if !bytes.Equal(w, g) {
						mismatched++
						if mismatched <= 5 {
							t.Errorf("%s: the dumps differ\n%s", name, firstDifference(w, g))
						}
					}
				}
			}
			if mismatched > 0 {
				t.Errorf("%d of %d files differ", mismatched, len(files))
			}
		})
	}
}

// errorLines returns the lines of a dump that name the file and list its errors.
func errorLines(dump []byte) []byte {
	var out []byte
	for _, line := range bytes.SplitAfter(dump, []byte("\n")) {
		if bytes.HasPrefix(line, []byte("file ")) || bytes.HasPrefix(line, []byte("error")) {
			out = append(out, line...)
		}
	}
	return out
}

// runParseCheck runs bin on files and returns its output.
func runParseCheck(t *testing.T, bin string, files []string) []byte {
	t.Helper()
	var stdout, stderr bytes.Buffer
	cmd := exec.Command(bin, files...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("%s %s: %v\n%s", filepath.Base(bin), strings.Join(files[:min(len(files), 3)], " "), err, stderr.String())
	}
	return stdout.Bytes()
}

// firstDifference describes where two dumps differ.
func firstDifference(want, got []byte) string {
	w, g := strings.Split(string(want), "\n"), strings.Split(string(got), "\n")
	for i := 0; i < len(w) || i < len(g); i++ {
		var a, b string
		if i < len(w) {
			a = w[i]
		}
		if i < len(g) {
			b = g[i]
		}
		if a != b {
			return fmt.Sprintf("line %d\n  Go:         %s\n  Plutonium:  %s", i+1, a, b)
		}
	}
	return "no difference"
}

// parseCheckFiles lists the files to parse: the repository's .p and .go
// files, and the generated ones.
func parseCheckFiles(t *testing.T) []string {
	t.Helper()
	var files []string
	err := filepath.WalkDir(repoRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", ".claude", "node_modules":
				return filepath.SkipDir
			}
			return nil
		}
		if ext := filepath.Ext(path); ext == ".p" || ext == ".go" {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(files)
	if len(files) < 500 {
		t.Fatalf("only %d files under %s", len(files), repoRoot)
	}
	return append(files, generatedInputs(t, files)...)
}

// errorsOnly are generated inputs that are not valid UTF-8, for which only
// the errors are compared: a Plutonium string holds valid UTF-8 (language
// spec section 7.3), so the translated scanner makes the text of a literal or
// comment from the bytes with each invalid byte replaced by U+FFFD, and the
// text, with the end positions computed from its length, differs from Go's.
// The parser reports the bytes as errors either way.
var errorsOnly = map[string]bool{"badutf8.p": true}

// generatedInputs writes inputs that exercise the parser's error paths:
// real files cut off at several points and with a line removed, many
// errors in one file, nesting past the parser's limit, and unusual bytes.
func generatedInputs(t *testing.T, real []string) []string {
	t.Helper()
	dir := t.TempDir()
	var out []string
	add := func(name string, src []byte) {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, src, 0o644); err != nil {
			t.Fatal(err)
		}
		out = append(out, path)
	}
	// Every 25th real file, cut at a few points and with a line cut out.
	for i := 0; i < len(real); i += 25 {
		src, err := os.ReadFile(real[i])
		if err != nil {
			t.Fatal(err)
		}
		ext := filepath.Ext(real[i])
		for j := 1; j <= 3; j++ {
			cut := len(src) * j / 4
			for cut > 0 && cut < len(src) && !utf8.RuneStart(src[cut]) {
				cut-- // a cut inside a character would make the file invalid UTF-8
			}
			add(fmt.Sprintf("cut%d_%d%s", i, j, ext), src[:cut])
		}
		lines := bytes.SplitAfter(src, []byte("\n"))
		if len(lines) > 4 {
			k := len(lines) / 2
			add(fmt.Sprintf("line%d%s", i, ext), bytes.Join(append(append([][]byte{}, lines[:k]...), lines[k+1:]...), nil))
		}
	}
	const head = "package a\n\n"
	add("manyerrors.p", []byte(head+strings.Repeat("var = 1\nfunc ( {\n}}} ) )\n", 40)))
	add("manyerrorsline.p", []byte(head+strings.Repeat("x := ) ( ] [ } {; ", 60)))
	add("deepparens.p", []byte(head+"var x = "+strings.Repeat("(", 100010)+"1"+strings.Repeat(")", 100010)+"\n"))
	add("deepbrackets.p", []byte(head+"var x = "+strings.Repeat("[", 100010)+"1"+strings.Repeat("]", 100010)+"\n"))
	add("deepblocks.p", []byte(head+"func f() {\n"+strings.Repeat("{\n", 20000)+strings.Repeat("}\n", 20000)+"}\n"))
	add("deepunary.p", []byte(head+"var x = "+strings.Repeat("-", 100010)+"1\n"))
	add("deeptypes.p", []byte(head+"var x "+strings.Repeat("*", 100010)+"T\n"))
	add("bom.p", []byte("\ufeffpackage a\n"))
	add("bominside.p", []byte("package a\n\nvar s = \"x\ufeffy\"\n\ufeffvar t int\n"))
	add("nul.p", []byte("package a\n\nvar s = \"x\x00y\"\nvar t = 1\x00\n"))
	add("badutf8.p", []byte("package a\n\nvar s = \"\xff\xfe\"\n// \xc3\n\xc3var t int\n"))
	add("crlf.p", []byte("package a\r\n\r\nvar s = `raw\r\nstring`\r\n// comment\r\nfunc f() {\r\n}\r\n"))
	add("empty.p", nil)
	add("onlycomment.p", []byte("// nothing here"))
	add("badliterals.p", []byte(head+"var a = 0x\nvar b = 1e\nvar c = 08\nvar d = 'ab'\nvar e = ''\nvar f = \"\\q\"\nvar g = 0b12\nvar h = 1__0\nvar i = `unterminated\n"))
	add("badcomment.p", []byte(head+"/* unterminated\nvar x int\n"))
	add("linedirectives.p", []byte(head+"//line other.p:10:5\nvar x = (\n/*line :20:3*/var y int\n//line :7\nfunc f( {\n"))
	add("directives.p", []byte("//pu:build linux && amd64\n//go:build !windows\n// +build !plan9\n\n//pu:stack 1G\npackage a\n\n//pu:inline\nfunc f() {}\n\n//pu:unknown x y\nfunc g() {}\n"))
	return out
}
