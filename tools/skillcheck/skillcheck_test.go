package skillcheck

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	plutonium "github.com/matjam/plutonium"
	"github.com/matjam/plutonium/internal/build"
	"github.com/matjam/plutonium/internal/diag"
	"github.com/matjam/plutonium/internal/load"
	"github.com/matjam/plutonium/internal/syntax/format"
	"github.com/matjam/plutonium/internal/testguard"
	"github.com/matjam/plutonium/internal/types2"
)

const skillDir = "../../skill/plutonium"

// markdownFiles returns the Markdown files of the skill.
func markdownFiles(t *testing.T) []string {
	t.Helper()
	var files []string
	err := filepath.WalkDir(skillDir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(p, ".md") {
			files = append(files, p)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(files)
	if len(files) == 0 {
		t.Fatal("no Markdown files in the skill")
	}
	return files
}

// TestSkillExamples builds and runs every checked block of the skill.
func TestSkillExamples(t *testing.T) {
	testguard.Parallel(t)
	counts := map[Kind]int{}
	var all []*Block
	for _, f := range markdownFiles(t) {
		text, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		rel, _ := filepath.Rel(skillDir, f)
		blocks, err := Extract(filepath.ToSlash(rel), string(text))
		if err != nil {
			t.Fatal(err)
		}
		all = append(all, blocks...)
	}
	for _, b := range all {
		counts[b.Kind]++
	}
	t.Logf("checked blocks: %d pluto-run, %d pluto-compile, %d pluto-error, %d pluto-test, %d pluto-names, %d pluto-absent",
		counts[Run], counts[Compile], counts[Error], counts[Test], counts[Names], counts[Absent])
	if len(all) == 0 {
		t.Fatal("the skill has no checked blocks")
	}
	for _, b := range all {
		t.Run(b.Name(), func(t *testing.T) {
			testguard.Parallel(t)
			checkBlock(t, b)
		})
	}
}

var mainRE = regexp.MustCompile(`(?m)^func main\(`)
var packageRE = regexp.MustCompile(`(?m)^package (\w+)`)

// checkBlock builds a block and checks the outcome its kind asks for.
func checkBlock(t *testing.T, b *Block) {
	dir := t.TempDir()
	for _, f := range b.Files {
		write(t, dir, f.Name, f.Text)
		checkFormatted(t, b, f)
	}
	switch b.Kind {
	case Test:
		checkTest(t, dir)
		return
	case Names, Absent:
		checkNames(t, b)
		return
	case Compile, Error:
		prepare(t, dir, b)
	}
	out := filepath.Join(t.TempDir(), "prog")
	opt := 2
	if b.Kind != Run {
		opt = 0
	}
	cfg := &build.Config{Dir: dir, Output: out, Stdlib: plutonium.Stdlib(), OptLevel: opt}
	if b.Mode != "" {
		cfg.Kernel = b.Mode == "kernel"
		cfg.Freestanding = b.Mode == "freestanding"
		cfg.CompileOnly = true
	}
	err := build.Build(cfg)
	switch b.Kind {
	case Error:
		if err == nil {
			t.Fatalf("the program built, want error %s", b.Code)
		}
		codes := codesOf(err)
		for _, c := range codes {
			if string(c) == b.Code {
				return
			}
		}
		t.Fatalf("want error %s, got %v:\n%v", b.Code, codes, err)
	case Compile:
		if err != nil {
			t.Fatalf("build: %v", err)
		}
	case Run:
		if err != nil {
			t.Fatalf("build: %v", err)
		}
		runProgram(t, out, b)
	}
}

// checkFormatted fails when a source file of a block is not what pluto fmt
// would write, so that the skill teaches formatted code. Error examples
// are exempt, since they may show deliberately odd code.
func checkFormatted(t *testing.T, b *Block, f SourceFile) {
	t.Helper()
	if b.Kind == Names || b.Kind == Absent || !strings.HasSuffix(f.Name, ".p") {
		return
	}
	got, err := format.Source([]byte(f.Text))
	if err != nil {
		return // the build reports syntax errors
	}
	if string(got) != f.Text {
		t.Errorf("%s is not formatted; pluto fmt would write:\n%s", f.Name, got)
	}
}

// checkNames looks up the qualified library names of a block: all must
// exist (Names) or none may (Absent).
func checkNames(t *testing.T, b *Block) {
	t.Helper()
	cfg := &load.Config{
		Stdlib: plutonium.Stdlib(),
		Sizes:  types2.SizesFor("gc", "amd64"),
		Tags:   load.BuildTags(false, false, nil),
	}
	cfg.Tags["gencheck"] = true
	progs := map[string]*load.Program{}
	for _, line := range strings.Split(b.Files[0].Text, "\n") {
		name := strings.TrimSpace(line)
		if i := strings.Index(name, "//"); i >= 0 {
			name = strings.TrimSpace(name[:i])
		}
		if name == "" {
			continue
		}
		pkg, rest, ok := splitQualified(name)
		if !ok {
			t.Errorf("%q: want a name like strings.Cut or vec.Vec.Push", name)
			continue
		}
		prog := progs[pkg]
		if prog == nil {
			var err error
			prog, err = load.LoadPackage(cfg, "pluto/"+pkg)
			if err != nil {
				t.Errorf("%s: package pluto/%s: %v", name, pkg, err)
				continue
			}
			progs[pkg] = prog
		}
		found := lookup(prog.Main.Types, rest)
		if b.Kind == Names && !found {
			t.Errorf("%s does not exist", name)
		}
		if b.Kind == Absent && found {
			t.Errorf("%s exists, but the text says it does not", name)
		}
	}
}

// splitQualified splits "sync/atomic.Int64.Add" into the package path
// "sync/atomic" and the rest "Int64.Add".
func splitQualified(name string) (pkg, rest string, ok bool) {
	slash := strings.LastIndex(name, "/")
	dot := strings.Index(name[slash+1:], ".")
	if dot < 0 {
		return "", "", false
	}
	i := slash + 1 + dot
	return name[:i], name[i+1:], true
}

// lookup finds Name, or Type.Method, in a package.
func lookup(pkg *types2.Package, rest string) bool {
	name, method, isMethod := strings.Cut(rest, ".")
	obj := pkg.Scope().Lookup(name)
	if obj == nil {
		return false
	}
	if !isMethod {
		return true
	}
	tn, ok := obj.(*types2.TypeName)
	if !ok {
		return false
	}
	t := types2.Unalias(tn.Type())
	if !types2.IsInterface(t) {
		t = types2.NewPointer(t)
	}
	f, _, _ := types2.LookupFieldOrMethod(t, true, pkg, method)
	return f != nil
}

func write(t *testing.T, dir, name, text string) {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

// prepare makes a snippet buildable: package main gets a main function if
// it has none, and a library package is imported from a main package.
func prepare(t *testing.T, dir string, b *Block) {
	t.Helper()
	if len(b.Files) != 1 || b.Files[0].Name != "main.p" {
		return
	}
	text := b.Files[0].Text
	m := packageRE.FindStringSubmatch(text)
	if m == nil {
		t.Fatal("no package clause")
	}
	if m[1] == "main" {
		if !mainRE.MatchString(text) {
			write(t, dir, "main.p", text+"\nfunc main() {}\n")
		}
		return
	}
	if err := os.Remove(filepath.Join(dir, "main.p")); err != nil {
		t.Fatal(err)
	}
	write(t, dir, "pluto.mod", "module skillcheck\n")
	write(t, dir, "pkg/pkg.p", text)
	write(t, dir, "main.p", "package main\n\nimport _ \"skillcheck/pkg\"\n\nfunc main() {}\n")
}

// codesOf returns the codes of the diagnostics in err.
func codesOf(err error) []diag.Code {
	var codes []diag.Code
	var walk func(error)
	walk = func(e error) {
		if d, ok := e.(*diag.Diagnostic); ok {
			codes = append(codes, d.Code)
			return
		}
		switch x := e.(type) {
		case interface{ Unwrap() []error }:
			for _, c := range x.Unwrap() {
				walk(c)
			}
		case interface{ Unwrap() error }:
			walk(x.Unwrap())
		}
	}
	walk(err)
	return codes
}

func runProgram(t *testing.T, bin string, b *Block) {
	t.Helper()
	cmd := exec.Command(bin)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	status := 0
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		status = exitErr.ExitCode()
		if status < 0 {
			t.Fatalf("the program died: %v\nstderr:\n%s", err, stderr.String())
		}
	} else if err != nil {
		t.Fatal(err)
	}
	if status != b.Exit {
		t.Errorf("exit status %d, want %d\nstdout:\n%s\nstderr:\n%s", status, b.Exit, stdout.String(), stderr.String())
	}
	if b.Output != nil && trimLines(stdout.String()) != trimLines(*b.Output) {
		t.Errorf("output differs\n--- got ---\n%s--- want ---\n%s", stdout.String(), *b.Output)
	}
}

// trimLines removes trailing blanks from every line, which editors strip from
// the expected output of a program that prints "x " before a newline.
func trimLines(s string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " \t")
	}
	return strings.Join(lines, "\n")
}

// checkTest runs the tests of the package in dir as pluto test does.
func checkTest(t *testing.T, dir string) {
	t.Helper()
	out := filepath.Join(t.TempDir(), "test")
	cfg := &build.Config{Dir: dir, Output: out, Stdlib: plutonium.Stdlib(), OptLevel: 1}
	opts := &build.TestOptions{Verbose: true, Count: 1}
	if _, err := build.BuildTest(cfg, dir, opts); err != nil {
		t.Fatalf("build: %v", err)
	}
	cmd := exec.Command(out)
	cmd.Dir = dir
	got, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("tests failed: %v\n%s", err, got)
	}
	if !bytes.Contains(got, []byte("PASS")) {
		t.Fatalf("no PASS in the test output:\n%s", got)
	}
}

// TestSkillShape checks the structure the skill promises.
func TestSkillShape(t *testing.T) {
	skill, err := os.ReadFile(filepath.Join(skillDir, "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(skill)
	if !strings.HasPrefix(text, "---\nname: plutonium\ndescription: ") {
		t.Errorf("SKILL.md must start with the frontmatter name: plutonium and a description")
	}
	front, _, ok := strings.Cut(strings.TrimPrefix(text, "---\n"), "\n---\n")
	if !ok {
		t.Fatal("SKILL.md frontmatter is not closed")
	}
	if strings.Count(front, "\n") > 0 && !strings.Contains(front, "description:") {
		t.Error("no description")
	}
	if n := strings.Count(text, "\n"); n >= 400 {
		t.Errorf("SKILL.md has %d lines, want fewer than 400", n)
	}
	for _, f := range markdownFiles(t) {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(f, "references") {
			if n := strings.Count(string(data), "\n"); n > 650 {
				t.Errorf("%s has %d lines, want about 600 at most", f, n)
			}
		}
		for _, link := range linkRE.FindAllStringSubmatch(stripCode(string(data)), -1) {
			target := link[1]
			if strings.Contains(target, "://") || strings.HasPrefix(target, "#") || strings.HasPrefix(target, "mailto:") {
				continue
			}
			target, _, _ = strings.Cut(target, "#")
			if target == "" {
				continue
			}
			if _, err := os.Stat(filepath.Join(filepath.Dir(f), target)); err != nil {
				t.Errorf("%s: broken link %q", f, link[1])
			}
		}
	}
}

var linkRE = regexp.MustCompile(`\]\(([^)\s]+)\)`)

var (
	fencedRE = regexp.MustCompile("(?s)```.*?```")
	inlineRE = regexp.MustCompile("`[^`\n]*`")
	rangeRE  = regexp.MustCompile(`E\d{4} to\s+E\d{4}`)
)

// stripCode removes fenced blocks and inline code spans from Markdown.
func stripCode(s string) string {
	return inlineRE.ReplaceAllString(fencedRE.ReplaceAllString(s, ""), "")
}

// TestExtract checks the extractor on a fixture.
func TestExtract(t *testing.T) {
	const doc = "# t\n\n```go\nignored\n```\n\n```pluto-run exit=3\npackage main\n\nfunc main() int64 { return 3 }\n```\n\n```output\nhi\n```\n\n" +
		"```pluto-error E3301\n//file: main.p\npackage main\n//file: b.p\npackage main\n```\n\n```pluto-compile\npackage main\n```\n\n" +
		"Text.\n\n```output\nnot attached\n```\n"
	blocks, err := Extract("t.md", doc)
	if err != nil {
		t.Fatal(err)
	}
	if len(blocks) != 3 {
		t.Fatalf("got %d blocks, want 3", len(blocks))
	}
	run, errb := blocks[0], blocks[1]
	if run.Kind != Run || run.Exit != 3 || run.Output == nil || *run.Output != "hi\n" || run.Line != 7 {
		t.Errorf("run block: %+v", run)
	}
	if errb.Kind != Error || errb.Code != "E3301" || len(errb.Files) != 2 || errb.Files[1].Name != "b.p" {
		t.Errorf("error block: %+v", errb)
	}
	for _, bad := range []string{
		"```pluto-run foo\nx\n```\n",
		"```pluto-error\nx\n```\n",
		"```pluto-compile\nx\n",
		"```pluto-test\npackage x\n```\n",
		"```pluto-what\nx\n```\n",
	} {
		if _, err := Extract("t.md", bad); err == nil {
			t.Errorf("no error for %q", bad)
		}
	}
	if got := fmt.Sprint(Run, Compile, Error, Test, Names, Absent); got != "pluto-run pluto-compile pluto-error pluto-test pluto-names pluto-absent" {
		t.Errorf("Kind strings: %s", got)
	}
}

var codeRE = regexp.MustCompile(`\bE\d{4}\b`)

// TestSkillCodes checks that every error code the skill mentions is in the
// compiler's catalog, so that no text names a code that does not exist.
func TestSkillCodes(t *testing.T) {
	for _, f := range markdownFiles(t) {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range codeRE.FindAllString(rangeRE.ReplaceAllString(string(data), ""), -1) {
			if diag.Lookup(diag.Code(c)) == nil {
				t.Errorf("%s mentions %s, which is not an error code", f, c)
			}
		}
	}
}

var (
	sectionRE = regexp.MustCompile(`\bsections? ((?:[A-G]|\d{1,2})\.\d{1,2}(?:(?:,| and|, and| to) (?:[A-G]|\d{1,2})\.\d{1,2})*)`)
	numberRE  = regexp.MustCompile(`(?:[A-G]|\d{1,2})\.\d{1,2}`)
	// otherDocRE matches a citation of a refinement document (abi.md section
	// 1.3), whose numbering is its own.
	otherDocRE = regexp.MustCompile(`(?:abi|memory-model|debugging|debug-allocator)\.md,? sections? [A-G0-9.]+`)
	headingRE  = regexp.MustCompile(`(?m)^#{2,4} ((?:[A-G]|\d{1,2})\.\d{1,2})\b`)
)

// TestSkillSections checks that every section number the skill cites exists
// in the specification (docs/spec/language).
func TestSkillSections(t *testing.T) {
	specFiles, err := filepath.Glob("../../docs/spec/language/*.md")
	if err != nil || len(specFiles) == 0 {
		t.Fatalf("no specification files: %v", err)
	}
	have := map[string]bool{}
	for _, f := range specFiles {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range headingRE.FindAllStringSubmatch(string(data), -1) {
			have[m[1]] = true
		}
	}
	for _, f := range markdownFiles(t) {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		text := otherDocRE.ReplaceAllString(string(data), "")
		for _, m := range sectionRE.FindAllStringSubmatch(text, -1) {
			for _, n := range numberRE.FindAllString(m[1], -1) {
				if !have[n] {
					t.Errorf("%s cites section %s, which the specification does not have", f, n)
				}
			}
		}
	}
}

// TestSingleFile builds the single-file guide and checks its shape.
func TestSingleFile(t *testing.T) {
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("no sh")
	}
	out := filepath.Join(t.TempDir(), "guide.md")
	cmd := exec.Command(sh, filepath.Join(skillDir, "tools", "build-single-file.sh"), out)
	if msg, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build-single-file.sh: %v\n%s", err, msg)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	guide := string(data)
	if strings.Contains(guide, "```pluto-") || strings.HasPrefix(guide, "---") {
		t.Error("the guide keeps frontmatter or checker fence markers")
	}
	for _, f := range markdownFiles(t) {
		if !strings.Contains(f, "references") {
			continue
		}
		if !strings.Contains(guide, "<!-- references/"+filepath.Base(f)+" -->") {
			t.Errorf("the guide lacks %s", f)
		}
	}
}
