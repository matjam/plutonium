package skillcheck

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

const skillDir = "../../skill/plutonium"

// A compiler is the pluto binary the checks run, with the environment that
// points it at the repository's library.
type compiler struct {
	path string
	env  []string
}

// plutoCompiler returns the compiler to check the skill with:
// $PLUTO_TEST_COMPILER, such as the self-hosted compiler, or else pluto on
// PATH, either with the repository's lib as $PLUTOROOT. It skips the test
// when there is neither.
func plutoCompiler(t *testing.T) *compiler {
	t.Helper()
	path := os.Getenv("PLUTO_TEST_COMPILER")
	if path == "" {
		var err error
		if path, err = exec.LookPath("pluto"); err != nil {
			t.Skip("no pluto compiler: set PLUTO_TEST_COMPILER or put pluto on PATH")
		}
	}
	lib, err := filepath.Abs("../../lib")
	if err != nil {
		t.Fatal(err)
	}
	return &compiler{path: path, env: append(os.Environ(), "PLUTOROOT="+lib, "PLUTO_BRIEF=")}
}

// command returns a command that runs the compiler with args in dir.
func (c *compiler) command(dir string, args ...string) *exec.Cmd {
	cmd := exec.Command(c.path, args...)
	cmd.Dir = dir
	cmd.Env = c.env
	return cmd
}

// run runs the compiler with args in dir and returns its combined output.
func (c *compiler) run(dir string, stdin string, args ...string) (string, error) {
	cmd := c.command(dir, args...)
	cmd.Stdin = strings.NewReader(stdin)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

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
	t.Parallel()
	c := plutoCompiler(t)
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
	// The blocks build in parallel but only a few at a time, so that the
	// check does not slow down the timing-sensitive tests that run beside
	// it in go test ./...
	slots := make(chan struct{}, 3)
	for _, b := range all {
		t.Run(b.Name(), func(t *testing.T) {
			t.Parallel()
			slots <- struct{}{}
			defer func() { <-slots }()
			checkBlock(t, c, b)
		})
	}
}

var mainRE = regexp.MustCompile(`(?m)^func main\(`)
var packageRE = regexp.MustCompile(`(?m)^package (\w+)`)

// checkBlock builds a block and checks the outcome its kind asks for.
func checkBlock(t *testing.T, c *compiler, b *Block) {
	dir := t.TempDir()
	for _, f := range b.Files {
		write(t, dir, f.Name, f.Text)
		checkFormatted(t, c, b, f)
	}
	switch b.Kind {
	case Test:
		checkTest(t, c, dir)
		return
	case Names, Absent:
		checkNames(t, c, b)
		return
	case Compile, Error:
		prepare(t, dir, b)
	}
	out := filepath.Join(t.TempDir(), "prog")
	opt := "2"
	if b.Kind != Run {
		opt = "0"
	}
	args := []string{"build", "--brief=false", "-O", opt, "-o", out}
	if b.Mode != "" {
		args = append(args, "--mode", b.Mode, "--gencheck=false", "-c")
	}
	msg, err := c.run(dir, "", append(args, ".")...)
	switch b.Kind {
	case Error:
		if err == nil {
			t.Fatalf("the program built, want error %s", b.Code)
		}
		codes := codesOf(msg)
		for _, c := range codes {
			if c == b.Code {
				return
			}
		}
		t.Fatalf("want error %s, got %v:\n%s", b.Code, codes, msg)
	case Compile:
		if err != nil {
			t.Fatalf("build: %v\n%s", err, msg)
		}
	case Run:
		if err != nil {
			t.Fatalf("build: %v\n%s", err, msg)
		}
		runProgram(t, out, b)
	}
}

// checkFormatted fails when a source file of a block is not what pluto fmt
// would write, so that the skill teaches formatted code. Error examples
// are exempt, since they may show deliberately odd code.
func checkFormatted(t *testing.T, c *compiler, b *Block, f SourceFile) {
	t.Helper()
	if b.Kind == Names || b.Kind == Absent || !strings.HasSuffix(f.Name, ".p") {
		return
	}
	cmd := c.command("", "fmt")
	cmd.Stdin = strings.NewReader(f.Text)
	got, err := cmd.Output()
	if err != nil {
		return // the build reports syntax errors
	}
	if string(got) != f.Text {
		t.Errorf("%s is not formatted; pluto fmt would write:\n%s", f.Name, got)
	}
}

// checkNames looks up the qualified library names of a block: all must
// exist (Names) or none may (Absent).
func checkNames(t *testing.T, c *compiler, b *Block) {
	t.Helper()
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
		found, err := c.lookup(pkg, rest)
		if err != nil {
			t.Errorf("%s: package pluto/%s: %v", name, pkg, err)
			continue
		}
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

// lookup finds Name, or Type.Method, in package pluto/pkg, as the
// checked package has them for the hosted build: a top-level name in the
// listing of pluto doc -all, a method of Type (declared, promoted, or of an
// interface) by asking pluto doc for Type.Method, and a field of Type in
// the declaration that pluto doc -all prints for Type.
func (c *compiler) lookup(pkg, rest string) (bool, error) {
	names, err := c.topLevel(pkg)
	if err != nil {
		return false, err
	}
	name, member, isMember := strings.Cut(rest, ".")
	if !names[name] {
		return false, nil
	}
	if !isMember {
		return true, nil
	}
	if _, err := c.run("", "", "doc", "pluto/"+pkg, rest); err == nil {
		return true, nil
	}
	out, err := c.run("", "", "doc", "-all", "pluto/"+pkg, name)
	if err != nil {
		return false, fmt.Errorf("pluto doc %s: %v\n%s", name, err, out)
	}
	return hasField(out, name, member), nil
}

var (
	listingMu    sync.Mutex
	listingCache = map[string]map[string]bool{}
	// topLevelRE matches the declaration that starts an entry of pluto
	// doc: a function (not a method), type, constant or variable.
	topLevelRE = regexp.MustCompile(`(?m)^(?:func|type|const|var) ([A-Za-z_][A-Za-z0-9_]*)`)
)

// topLevel returns the names that package pluto/pkg declares at top level.
func (c *compiler) topLevel(pkg string) (map[string]bool, error) {
	listingMu.Lock()
	defer listingMu.Unlock()
	if names, ok := listingCache[pkg]; ok {
		return names, nil
	}
	out, err := c.run("", "", "doc", "-all", "pluto/"+pkg)
	if err != nil {
		return nil, fmt.Errorf("pluto doc: %v\n%s", err, out)
	}
	names := map[string]bool{}
	for _, m := range topLevelRE.FindAllStringSubmatch(out, -1) {
		names[m[1]] = true
	}
	listingCache[pkg] = names
	return names, nil
}

// fieldRE matches a field line of a struct declaration as pluto doc prints
// it: its names, or the type of an embedded field.
var fieldRE = regexp.MustCompile(`^\t(?:([A-Za-z_][A-Za-z0-9_]*(?:, [A-Za-z_][A-Za-z0-9_]*)*)\s|\*?(?:[A-Za-z_][A-Za-z0-9_]*\.)?([A-Za-z_][A-Za-z0-9_]*)(?:\[|$|\s))`)

// hasField reports whether the struct declaration of typ in the output of
// pluto doc has a field named field.
func hasField(doc, typ, field string) bool {
	_, decl, ok := strings.Cut(doc, "\ntype "+typ+" ")
	if !ok {
		return false
	}
	decl, _, _ = strings.Cut(decl, "\n}")
	for _, line := range strings.Split(decl, "\n") {
		m := fieldRE.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		if m[2] == field {
			return true
		}
		for _, n := range strings.Split(m[1], ", ") {
			if n == field {
				return true
			}
		}
	}
	return false
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

// explainRE matches the line under a diagnostic that names its code.
var explainRE = regexp.MustCompile(`\(run "pluto explain (E\d{4})" for a longer explanation\)|\(explained above; run "pluto explain (E\d{4})" for more\)`)

// codesOf returns the codes of the diagnostics that pluto build printed.
func codesOf(out string) []string {
	var codes []string
	for _, m := range explainRE.FindAllStringSubmatch(out, -1) {
		codes = append(codes, m[1]+m[2])
	}
	return codes
}

func runProgram(t *testing.T, bin string, b *Block) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	if ctx.Err() != nil {
		t.Fatalf("the program did not finish in 40 seconds\nstdout:\n%s", stdout.String())
	}
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

// checkTest runs the tests of the package in dir with pluto test.
func checkTest(t *testing.T, c *compiler, dir string) {
	t.Helper()
	got, err := c.run(dir, "", "test", "-O", "1", "-v", "-count", "1", ".")
	if err != nil {
		t.Fatalf("tests failed: %v\n%s", err, got)
	}
	if !strings.Contains(got, "PASS") {
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
	pc := plutoCompiler(t)
	list, err := pc.run("", "", "explain")
	if err != nil {
		t.Fatalf("pluto explain: %v\n%s", err, list)
	}
	known := map[string]bool{}
	for _, line := range strings.Split(list, "\n") {
		if f := strings.Fields(line); len(f) > 0 && codeRE.MatchString(f[0]) {
			known[f[0]] = true
		}
	}
	if len(known) == 0 {
		t.Fatalf("pluto explain listed no codes:\n%s", list)
	}
	for _, f := range markdownFiles(t) {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range codeRE.FindAllString(rangeRE.ReplaceAllString(string(data), ""), -1) {
			if !known[c] {
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

// TestGeneratedFresh checks that the generated references match their
// sources. It is opt-in (SKILLCHECK_FRESH=1) because it fails whenever the
// library or the diagnostics catalog changes without a regeneration, which
// other work should not have to wait for: run skill/plutonium/tools/regenerate.sh.
func TestGeneratedFresh(t *testing.T) {
	if os.Getenv("SKILLCHECK_FRESH") == "" {
		t.Skip("set SKILLCHECK_FRESH=1 to compare the generated references with their sources")
	}
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("no go command")
	}
	pc := plutoCompiler(t)
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	out := t.TempDir()
	for _, args := range [][]string{
		{"run", "./skill/plutonium/tools/genstdlib", "-pluto", pc.path, "-lib", filepath.Join(root, "lib"), "-out", out},
		{"run", "./skill/plutonium/tools/gendiag", "-pluto", pc.path, "-out", filepath.Join(out, "diagnostics.md")},
	} {
		cmd := exec.Command(goBin, args...)
		cmd.Dir = root
		if msg, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("go %v: %v\n%s", args, err, msg)
		}
	}
	entries, err := os.ReadDir(out)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		want, err := os.ReadFile(filepath.Join(out, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(filepath.Join(skillDir, "references", e.Name()))
		if err != nil || !bytes.Equal(got, want) {
			t.Errorf("references/%s is stale: run skill/plutonium/tools/regenerate.sh", e.Name())
		}
	}
}
