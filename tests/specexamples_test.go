package tests

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// TestSpecExamples compiles the code examples of the specification, so
// that the spec and the compiler cannot drift apart (roadmap item 185).
//
// Every block fenced as ```go in the spec files is an example. A block is
// skipped by rule when it is not source: it elides code with "…" or
// "...", or it lists declarations without bodies, as an API listing does.
// A complete file (one with a package clause) is compiled as it is. Other
// blocks are wrapped: declarations go to package level, statements into
// the body of a function, and the imports the block uses are added.
// tests/testdata/specexamples/examples.txt holds what a rule cannot
// decide: the declarations a block assumes, blocks to skip, and the
// blocks that fail for a known reason.
//
// A line whose comment begins "error", "illegal" or "invalid" is a
// compile error (spec chapter 2). The lines of a block are independent
// alternatives (section 1.3), so the block is compiled once for each such
// line, with the other marked lines left out: the build must report an
// error on that line, with the code the comment states, and on no other
// line. When a block has several marked lines, the statements after the
// last of them are a fix: those units leave the fix out, and one more
// unit builds the fix with no marked line. A block whose entry says
// "together" states several errors of one program instead (section 1.3):
// it is compiled once, whole, and every marked line must get the code its
// comment states, and no other line may get a diagnostic. A block with no
// marked line must build cleanly. The builds stop
// after the object file (-c), so every static check runs: types, moves,
// escapes and races. No program runs.
//
// The race check finds the parties of a program from package
// initialization and main (section 19.3), so a task that nothing starts is
// not checked. A wrapped block with no main that starts a task (a task
// statement, a Task call, time.AfterFunc) or declares an interrupt handler
// gets a main that starts them: it installs each handler that the block
// does not install, and calls each entry function, a function nothing in
// the block or its entry uses. An entry function with parameters needs a
// main: line in the block's entry. Such a block builds as a program of
// its own.
func TestSpecExamples(t *testing.T) {
	blocks := specBlocks(t)
	data := readExampleData(t, filepath.Join("testdata", "specexamples", "examples.txt"))

	// Match the data entries to the blocks.
	used := map[string]bool{}
	for _, b := range blocks {
		if e, ok := data[b.key]; ok {
			b.entry = e
			used[b.key] = true
		}
	}
	var stale []string
	for k := range data {
		if !used[k] {
			stale = append(stale, k)
		}
	}
	sort.Strings(stale)
	for _, k := range stale {
		t.Errorf("examples.txt: entry %q matches no block of the spec", k)
	}

	// An entry's with: lines name blocks whose declarations it assumes.
	byKey := map[string]*specBlock{}
	for _, b := range blocks {
		byKey[b.key] = b
	}
	for _, b := range blocks {
		if b.entry == nil {
			continue
		}
		var ctx strings.Builder
		for _, k := range b.entry.with {
			o := byKey[k]
			if o == nil {
				t.Errorf("examples.txt: %q: with: %q matches no block", b.key, k)
				continue
			}
			if pkg := o.packageName(); o.isComplete() && pkg != "main" && pkg != b.packageName() {
				// A file of another package builds beside the block,
				// which imports it.
				b.deps = append(b.deps, o)
				continue
			}
			ctx.WriteString(o.declarations())
		}
		b.entry.pre = ctx.String() + b.entry.pre
		if b.entry.together && len(b.marks()) < 2 {
			t.Errorf("examples.txt: %q: together: the block has fewer than two marked lines", b.key)
		}
	}

	var units []*exampleUnit
	counts := map[string]int{}
	known := map[string]int{}
	for _, b := range blocks {
		if why := b.skipReason(); why != "" {
			counts["skipped"]++
			if b.entry != nil && b.entry.skip != "" {
				counts["skipped by entry"]++
			}
			continue
		}
		counts["checked"]++
		if b.entry != nil && b.entry.known != "" {
			known[knownCause(b.entry.known)]++
		}
		units = append(units, b.units()...)
	}
	var causes []string
	for k, n := range known {
		causes = append(causes, fmt.Sprintf("%s: %d", k, n))
	}
	sort.Strings(causes)
	t.Logf("spec examples: %d blocks, %d checked, %d skipped (%d by an entry of examples.txt), %d builds; known failures: %s",
		len(blocks), counts["checked"], counts["skipped"], counts["skipped by entry"], len(units), strings.Join(causes, ", "))

	c := compiler(t)
	runExampleUnits(t, c, units)

	// Report each block once, with the result of all its builds.
	byBlock := map[*specBlock][]*exampleUnit{}
	for _, u := range units {
		byBlock[u.block] = append(byBlock[u.block], u)
	}
	for _, b := range blocks {
		us := byBlock[b]
		if len(us) == 0 {
			continue
		}
		var problems []string
		for _, u := range us {
			problems = append(problems, u.problems...)
		}
		name := b.key
		switch {
		case b.entry != nil && b.entry.known != "":
			if len(problems) == 0 {
				t.Errorf("%s:%d: %s\n\tnow passes: remove its known failure from examples.txt (%s)", b.file, b.line, name, b.entry.known)
			}
		case len(problems) > 0:
			var src strings.Builder
			for _, u := range us {
				if len(u.problems) > 0 {
					fmt.Fprintf(&src, "\n--- %s:\n%s", u.what, numbered(u.src))
				}
			}
			t.Errorf("%s:%d: %s\n\t%s%s", b.file, b.line, name, strings.Join(problems, "\n\t"), src.String())
		}
	}
}

// specExampleFiles are the spec files whose examples the test compiles.
func specExampleFiles(t *testing.T) []string {
	t.Helper()
	spec := filepath.Join("..", "docs", "spec")
	files := []string{filepath.Join(spec, "language.md")}
	chapters, err := filepath.Glob(filepath.Join(spec, "language", "*.md"))
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(chapters)
	files = append(files, chapters...)
	for _, f := range []string{"abi.md", "memory-model.md", "debugging.md", "debug-allocator.md"} {
		files = append(files, filepath.Join(spec, f))
	}
	return files
}

// A specBlock is one ```go block of the spec.
type specBlock struct {
	file    string   // the spec file, relative to the repository
	line    int      // the line of the first line of code
	section string   // the number of the section it is in, or ""
	lines   []string // its lines
	key     string   // its name in examples.txt
	entry   *exampleEntry
	deps    []*specBlock // files of other packages the block imports
}

var fenceOpen = regexp.MustCompile("^\\s*```go\\s*$")

// specBlocks returns the ```go blocks of the spec files.
func specBlocks(t *testing.T) []*specBlock {
	t.Helper()
	var blocks []*specBlock
	for _, path := range specExampleFiles(t) {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		rel := strings.TrimPrefix(filepath.ToSlash(path), "../")
		base := filepath.Base(path)
		section := ""
		seen := map[string]int{}
		var cur *specBlock
		indent := ""
		for i, line := range strings.Split(string(data), "\n") {
			if cur != nil {
				if strings.TrimSpace(line) == "```" {
					first := ""
					for _, l := range cur.lines {
						if s := strings.TrimSpace(l); s != "" {
							first = s
							break
						}
					}
					k := fmt.Sprintf("%s %s | %s", base, cur.section, first)
					seen[k]++
					if n := seen[k]; n > 1 {
						k += " #" + strconv.Itoa(n)
					}
					cur.key = k
					blocks = append(blocks, cur)
					cur = nil
					continue
				}
				cur.lines = append(cur.lines, strings.TrimPrefix(line, indent))
				continue
			}
			if m := headingForm.FindStringSubmatch(line); m != nil {
				section = m[1]
				continue
			}
			if fenceOpen.MatchString(line) {
				indent = line[:len(line)-len(strings.TrimLeft(line, " "))]
				cur = &specBlock{file: rel, line: i + 2, section: section}
			}
		}
		if cur != nil {
			t.Fatalf("%s:%d: unterminated code block", rel, cur.line-1)
		}
	}
	if len(blocks) == 0 {
		t.Fatal("found no code blocks in the spec")
	}
	return blocks
}

var (
	// elision matches "..." standing for left-out code: alone or between
	// spaces, brackets and commas, but not a variadic parameter or
	// argument, which has a name or type beside it.
	elision = regexp.MustCompile(`(^|[\s({\[,])\.\.\.($|[\s)}\],;])`)
	// bodylessFunc matches a function declaration without a body.
	bodylessFunc = regexp.MustCompile(`^func\s*(\([^{]*\)\s*)?\w+[^{]*[^{(\s]\s*$`)
	// funcType matches a function type, written "func(" as gofmt
	// writes it: a signature without a name or a body.
	funcType = regexp.MustCompile(`^func\([^{]*$`)
	// signatureLine matches a line that shows a function's signature,
	// "copy(dst, src []T) int" or "rt.PanicFault(kind rt.FaultKind, ...)",
	// which is not a statement.
	signatureLine = regexp.MustCompile(`^\w+\([^()]*\) [\w\[\]*]+$|^[\w.]+\(\w+ [\w.*\[\]]+[,)]`)
)

// skipReason returns why the block is not compiled, or "" to compile it.
func (b *specBlock) skipReason() string {
	if b.entry != nil && b.entry.skip != "" {
		return b.entry.skip
	}
	types := 0
	for _, l := range b.lines {
		if s := strings.TrimSpace(codeOf(l)); s != "" {
			if !funcType.MatchString(s) {
				types = -1
				break
			}
			types++
		}
	}
	if types > 0 {
		return "it lists function types"
	}
	depth := 0
	for i, l := range b.lines {
		code := codeOf(l)
		// An array length written [...] is not an elision.
		if strings.Contains(code, "…") || elision.MatchString(strings.ReplaceAll(code, "[...]", "[n]")) {
			return "it elides code"
		}
		if depth == 0 && bodylessFunc.MatchString(code) && !b.directiveBefore(i) && b.isComplete() && b.packageName() != "main" {
			return "it lists the API of a package: declarations without bodies"
		}
		if depth == 0 && signatureLine.MatchString(strings.TrimSpace(code)) {
			return "it lists function signatures"
		}
		depth += bracketDepth(code)
	}
	return ""
}

// directiveBefore reports whether a //pu: directive comes right before
// line i, as //pu:extern does before a function implemented elsewhere.
func (b *specBlock) directiveBefore(i int) bool {
	for j := i - 1; j >= 0; j-- {
		s := strings.TrimSpace(b.lines[j])
		if strings.HasPrefix(s, "//pu:") || strings.HasPrefix(s, "//go:") {
			return true
		}
		if !strings.HasPrefix(s, "//") {
			return false
		}
	}
	return false
}

// codeOf returns line without its // comment.
func codeOf(line string) string {
	if i := commentStart(line); i >= 0 {
		return line[:i]
	}
	return line
}

// commentStart returns the index of the // that starts the comment of
// line, or -1. It skips string, rune and raw string literals.
func commentStart(line string) int {
	var quote byte
	for i := 0; i < len(line); i++ {
		ch := line[i]
		switch {
		case quote != 0:
			if ch == '\\' && quote != '`' {
				i++
			} else if ch == quote {
				quote = 0
			}
		case ch == '"' || ch == '\'' || ch == '`':
			quote = ch
		case ch == '/' && i+1 < len(line) && line[i+1] == '/':
			return i
		}
	}
	return -1
}

var errorCode = regexp.MustCompile(`\bE\d{4}\b`)

// errorMark returns the error codes a line's comment states and whether
// the comment marks the line as a compile error. A comment on a line of
// its own marks nothing.
func errorMark(line string) (codes []string, marked bool) {
	i := commentStart(line)
	if s := strings.TrimSpace(line); strings.HasPrefix(s, "//pu:") || strings.HasPrefix(s, "//go:") {
		// A directive is code: its comment is the next //.
		i = strings.Index(line, "//")
		if j := commentStart(line[i+2:]); j >= 0 {
			i += 2 + j
		} else {
			return nil, false
		}
	} else if i < 0 || strings.TrimSpace(line[:i]) == "" {
		return nil, false
	}
	c := strings.TrimSpace(line[i+2:])
	for _, w := range []string{"error", "illegal", "invalid"} {
		if strings.HasPrefix(c, w) {
			return errorCode.FindAllString(c, -1), true
		}
	}
	return nil, false
}

// isComplete reports whether the block is a whole source file: its first
// line of code is a package clause.
func (b *specBlock) isComplete() bool {
	for _, l := range b.lines {
		s := strings.TrimSpace(codeOf(l))
		if s == "" || strings.HasPrefix(s, "//") {
			continue
		}
		return strings.HasPrefix(s, "package ")
	}
	return false
}

// packageName returns the name in the block's package clause.
func (b *specBlock) packageName() string {
	for _, l := range b.lines {
		if f := strings.Fields(codeOf(l)); len(f) == 2 && f[0] == "package" {
			return f[1]
		}
	}
	return ""
}

// An exampleUnit is one build of a block, a package directory.
type exampleUnit struct {
	block    *specBlock
	what     string           // "the block", "the error on line N" or "the errors on lines N, M"
	src      string           // the source of main.p
	lineOf   map[int]int      // a line of src to a line of the block, from 1
	want     map[int][]string // block lines that must have an error, to the codes they must report
	at       map[int]int      // a block line to the marked line whose error it may report
	tasks    bool             // its main starts the block's tasks, so it builds as a program of its own
	setup    []string         // problems of the wrapping, found before the build
	problems []string
}

// A mark is a line marked as a compile error: a line whose comment says
// so, or a comment on a line of its own that says so of the lines below
// it, up to a blank line.
type mark struct {
	line  int // the marked line, from 0
	codes []string
	lines []int // the lines an error may be on, which other units leave out
}

// marks returns the marks of the block.
func (b *specBlock) marks() []mark {
	var ms []mark
	for i, l := range b.lines {
		if codes, ok := errorMark(l); ok {
			ms = append(ms, mark{i, codes, statementLines(b.lines, i)})
			continue
		}
		if codes, ok := headingMark(l); ok {
			m := mark{line: i, codes: codes}
			for j := i + 1; j < len(b.lines); j++ {
				s := strings.TrimSpace(b.lines[j])
				if s == "" || strings.HasPrefix(s, "//") {
					break
				}
				m.lines = append(m.lines, j)
			}
			if len(m.lines) > 0 {
				ms = append(ms, m)
			}
		}
	}
	return ms
}

// headingMark reports whether line is a comment on a line of its own that
// marks the lines below it as a compile error, and the codes it states.
func headingMark(line string) (codes []string, marked bool) {
	c, ok := strings.CutPrefix(strings.TrimSpace(line), "//")
	if !ok || strings.HasPrefix(c, "pu:") || strings.HasPrefix(c, "go:") {
		return nil, false
	}
	c = strings.TrimSpace(c)
	for _, w := range []string{"error", "illegal", "invalid"} {
		if strings.HasPrefix(c, w) {
			return errorCode.FindAllString(c, -1), true
		}
	}
	return nil, false
}

// units returns the builds that check the block.
func (b *specBlock) units() []*exampleUnit {
	ms := b.marks()
	if len(ms) == 0 {
		return []*exampleUnit{b.unit(nil, nil)}
	}
	if b.entry != nil && b.entry.together {
		// The marked lines are errors of one program: one build of the
		// whole block wants them all.
		return []*exampleUnit{b.unit(nil, ms)}
	}
	// A block with several marked lines shows alternatives (section 1.3),
	// and the statements after the last of them show the fix, another
	// alternative. The unit of each marked line leaves out the fix, and
	// one more unit builds the fix with no marked line.
	var us []*exampleUnit
	var after, all []int
	if len(ms) > 1 {
		last := 0
		for _, m := range ms {
			all = append(all, m.lines...)
			for _, j := range m.lines {
				last = max(last, j)
			}
		}
		depth := 0
		for j, l := range b.lines {
			if j > last && (depth == 0 || len(after) > 0) {
				after = append(after, j)
			}
			depth = max(depth+bracketDepth(codeOf(l)), 0)
		}
		us = append(us, b.unit(all, nil))
	}
	for k := range ms {
		drop := append([]int(nil), after...)
		for j, m := range ms {
			if j != k {
				drop = append(drop, m.lines...)
			}
		}
		us = append(us, b.unit(drop, ms[k:k+1]))
	}
	return us
}

// unit wraps the block, leaving out the lines of drop, and wants the errors
// of the marks ms (none for a clean build).
func (b *specBlock) unit(drop []int, ms []mark) *exampleUnit {
	u := &exampleUnit{block: b, what: "the block", lineOf: map[int]int{}, want: map[int][]string{}, at: map[int]int{}}
	var nums []string
	for _, m := range ms {
		u.want[m.line+1] = m.codes
		u.at[m.line+1] = m.line + 1
		for _, j := range m.lines {
			u.at[j+1] = m.line + 1
		}
		nums = append(nums, strconv.Itoa(m.line+1))
	}
	switch len(nums) {
	case 0:
	case 1:
		u.what = "the error on line " + nums[0]
	default:
		u.what = "the errors on lines " + strings.Join(nums, ", ")
	}
	lines := make([]string, len(b.lines))
	copy(lines, b.lines)
	gone := map[int]bool{}
	for _, d := range drop {
		gone[d] = true
	}
	w := wrapper{marked: len(b.marks()) > 0}
	e := b.entry
	if e == nil {
		e = &exampleEntry{}
	}
	if b.isComplete() {
		for i, l := range lines {
			if gone[i] {
				w.add(&w.top, "", 0)
				continue
			}
			w.add(&w.top, l, i+1)
		}
		u.src, u.lineOf = w.file(e, true)
		return u
	}
	w.wrap(lines, gone, e)
	u.src, u.lineOf = w.file(e, false)
	u.tasks, u.setup = w.tasks, w.problems
	return u
}

// declarations returns the package-level declarations of the block, with
// its var and const declarations and without the lines marked as errors,
// for a block that assumes them.
func (b *specBlock) declarations() string {
	gone := map[int]bool{}
	for _, m := range b.marks() {
		for _, j := range m.lines {
			gone[j] = true
		}
	}
	var w wrapper
	if b.isComplete() {
		return ""
	}
	w.wrap(b.lines, gone, &exampleEntry{valuesTop: true})
	var out strings.Builder
	for _, l := range w.top {
		if s := strings.TrimSpace(l.text); strings.HasPrefix(s, "import") || strings.HasPrefix(s, "func main(") {
			continue
		}
		out.WriteString(l.text + "\n")
	}
	return out.String()
}

// statementLines returns the lines of the statement that starts on line
// i: up to the line where its brackets balance.
func statementLines(lines []string, i int) []int {
	depth := 0
	var out []int
	for j := i; j < len(lines); j++ {
		out = append(out, j)
		depth += bracketDepth(codeOf(lines[j]))
		if depth <= 0 {
			break
		}
	}
	return out
}

// bracketDepth returns the change in bracket depth over line, outside
// literals.
func bracketDepth(line string) int {
	d := 0
	var quote byte
	for i := 0; i < len(line); i++ {
		ch := line[i]
		switch {
		case quote != 0:
			if ch == '\\' && quote != '`' {
				i++
			} else if ch == quote {
				quote = 0
			}
		case ch == '"' || ch == '\'' || ch == '`':
			quote = ch
		case ch == '(' || ch == '[' || ch == '{':
			d++
		case ch == ')' || ch == ']' || ch == '}':
			d--
		}
	}
	return d
}

// A wrapper builds the source of a unit: package-level lines and the
// lines of the function body, each with the block line it comes from.
type wrapper struct {
	top, body []srcLine
	marked    bool     // the block has error lines
	tasks     bool     // the added main starts the block's tasks
	problems  []string // why the wrapper cannot build the unit as it should
}

type srcLine struct {
	text string
	from int // the block line, from 1, or 0
}

func (w *wrapper) add(to *[]srcLine, text string, from int) {
	*to = append(*to, srcLine{text, from})
}

var (
	// topDecl matches the first line of a declaration that goes to
	// package level.
	topDecl = regexp.MustCompile(`^(type|import|package)\b|^func\s*(\w|\([^)]*\)\s*\w+\s*[\[(])`)
	// valueDecl matches a var or const declaration, which goes to package
	// level when the block has no statements.
	valueDecl = regexp.MustCompile(`^(var|const)\b`)
	// shortDecl matches a short variable declaration and captures its
	// names.
	shortDecl = regexp.MustCompile(`^\s*([\w\s,]+?)\s*:=`)
	// varDecl matches a one-name or several-name var declaration and
	// captures the names.
	varDecl = regexp.MustCompile(`^\s*var\s+(\w+(?:\s*,\s*\w+)*)(\s*=|\s+[\w\[*(<]|\s*$)`)
)

// wrap splits the block into items at the lines of depth zero and puts
// each at package level or in the body.
func (w *wrapper) wrap(lines []string, gone map[int]bool, e *exampleEntry) {
	type item struct {
		first, last int
		top, value  bool
		names       []string // the names it declares in the body
	}
	var items []item
	depth := 0
	pending := -1 // the start of comments and directives that lead an item
	for i, l := range lines {
		s := strings.TrimSpace(l)
		switch {
		case depth > 0:
			items[len(items)-1].last = i
		case s == "":
			pending = -1
		case strings.HasPrefix(s, "//"):
			if pending < 0 {
				pending = i
			}
		default:
			start := i
			if pending >= 0 {
				start = pending
			}
			pending = -1
			items = append(items, item{first: start, last: i, top: topDecl.MatchString(s), value: valueDecl.MatchString(s)})
		}
		depth += bracketDepth(codeOf(l))
		if depth < 0 {
			depth = 0
		}
	}
	// A bare interface type stands for the type it shows: it is declared
	// with a name.
	for k := range items {
		if ifaceLit.MatchString(codeOf(lines[firstCode(lines, items[k].first)])) {
			items[k].top = true
		}
	}
	types := typeNames(append(strings.Split(e.pre, "\n"), lines...))
	statements := e.body != ""
	exprs, calls, others := 0, 0, 0
	for _, it := range items {
		if !it.top && !it.value {
			statements = true
			code := strings.TrimSpace(codeOf(lines[firstCode(lines, it.first)]))
			if isExpression(code) {
				exprs++
				if w.isCall(code, types) {
					calls++
				}
			} else {
				others++
			}
		}
	}
	// A block whose statements are all expressions, not all of them
	// calls, lists expressions: each expression that is not a call is used
	// as the value of a blank assignment. A conversion is not a call.
	exprList := others == 0 && exprs > calls

	// The lines of a block are alternatives (spec section 1.3): a
	// statement that declares again a name an earlier one declared starts
	// a new scope, and so does the first statement that declared it.
	starts := map[int]bool{}
	declaredBy := map[string]int{}
	for k := range items {
		it := &items[k]
		if it.top || it.value && (!statements || e.valuesTop) || allGone(gone, firstCode(lines, it.first), it.last) {
			continue
		}
		code := firstCodeLine(lines[it.first : it.last+1])
		it.names, _ = declaredNames(lines[it.first : it.last+1])
		short := !valueDecl.MatchString(strings.TrimSpace(code))
		again, fresh := []string{}, 0
		for _, n := range it.names {
			if _, ok := declaredBy[n]; ok {
				again = append(again, n)
			} else {
				fresh++
			}
		}
		if len(again) > 0 && (!short || fresh == 0) {
			starts[k] = true
			for _, n := range again {
				starts[declaredBy[n]] = true
			}
		}
		for _, n := range it.names {
			if _, ok := declaredBy[n]; !ok || starts[k] {
				declaredBy[n] = k
			}
		}
	}
	open := false
	for k, it := range items {
		to := &w.body
		if it.top || (it.value && (!statements || e.valuesTop)) {
			to = &w.top
		}
		if to == &w.body && starts[k] {
			if open {
				w.add(to, "}", 0)
			}
			w.add(to, "{", 0)
			open = true
		}
		for i := it.first; i <= it.last; i++ {
			if gone[i] {
				continue
			}
			text := lines[i]
			if i == firstCode(lines, it.first) {
				code := strings.TrimSpace(codeOf(text))
				if exprList && to == &w.body && !it.value && isExpression(code) && !w.isCall(code, types) {
					text = "_ = " + strings.TrimLeft(text, " \t")
				}
				if ifaceLit.MatchString(code) {
					text = fmt.Sprintf("type specType%d ", k) + strings.TrimLeft(text, " \t")
				}
			}
			if code := codeOf(text); i == firstCode(lines, it.first) && bodylessFunc.MatchString(code) && !directiveAbove(lines, i) {
				// A declaration without a body stands for a function the
				// example assumes.
				text = strings.TrimRight(code, " \t") + " { panic(0) }" + text[len(code):]
			}
			w.add(to, text, i+1)
		}
		if to == &w.body && !allGone(gone, firstCode(lines, it.first), it.last) {
			// A name the block declares and does not use would be an
			// error in a function body.
			for _, n := range it.names {
				w.add(to, "_ = &"+n, firstCode(lines, it.first)+1)
			}
		}
	}
	if open {
		w.add(&w.body, "}", 0)
	}
}

var (
	// ifaceLit matches the first line of a bare interface type.
	ifaceLit = regexp.MustCompile(`^interface\s*\{`)
	// typeDecl matches a type declaration and captures the name.
	typeDecl = regexp.MustCompile(`^\s*type\s+(\w+)`)
	// predeclaredTypes are the type names a conversion may use.
	predeclaredTypes = map[string]bool{
		"bool": true, "byte": true, "rune": true, "string": true, "error": true, "any": true,
		"int": true, "int8": true, "int16": true, "int32": true, "int64": true,
		"uint": true, "uint8": true, "uint16": true, "uint32": true, "uint64": true, "uintptr": true,
		"float32": true, "float64": true, "complex64": true, "complex128": true,
	}
)

// typeNames returns the names of the types the block declares.
func typeNames(lines []string) map[string]bool {
	names := map[string]bool{}
	for _, l := range lines {
		if m := typeDecl.FindStringSubmatch(codeOf(l)); m != nil {
			names[m[1]] = true
		}
	}
	return names
}

// isCall reports whether an expression statement is a call, and not a
// conversion to a type of the block or a predeclared type. In a block
// with no error lines, a call of a built-in function that yields a value
// is a value too: the block lists the forms of the call.
func (w *wrapper) isCall(code string, types map[string]bool) bool {
	if !callForm.MatchString(code) {
		return false
	}
	name := code[:strings.IndexAny(code, "([")]
	if strings.HasSuffix(name, ".") {
		return false // a type assertion, x.(T)
	}
	if !w.marked && valueBuiltins[name] {
		return false
	}
	return !predeclaredTypes[name] && !types[name]
}

// valueBuiltins are the built-in functions whose calls yield a value.
var valueBuiltins = map[string]bool{
	"new": true, "make": true, "len": true, "cap": true, "min": true, "max": true,
	"complex": true, "real": true, "imag": true, "append": true,
}

// allGone reports whether every line from first to last is left out.
func allGone(gone map[int]bool, first, last int) bool {
	for i := first; i <= last; i++ {
		if !gone[i] {
			return false
		}
	}
	return true
}

var (
	// callForm matches a call statement, "f(x)" or "a.b.f(x)".
	callForm = regexp.MustCompile(`^[\w.]+(\[[^\]]*\])?\(.*\)$`)
	// stmtStart matches the start of a statement that is not an
	// expression.
	stmtStart = regexp.MustCompile(`^(if|for|switch|select|return|go|task|defer|break|continue|goto|fallthrough|var|const|type|\w+:$)\b|^\w+:\s|^\{`)
	// assignOp matches an assignment, increment or send.
	assignOp = regexp.MustCompile(`(^|[^=!<>:])(:=|=|\+=|-=|\*=|/=|%=|&=|\|=|\^=|<<=|>>=|&\^=)([^=]|$)|\+\+$|--$|\w\s*<-`)
)

// isExpression reports whether a statement's first line is an
// expression.
func isExpression(code string) bool {
	return !stmtStart.MatchString(code) && !assignOp.MatchString(code)
}

// firstCode returns the first line from i on that is not a comment.
func firstCode(lines []string, i int) int {
	for j := i; j < len(lines); j++ {
		if s := strings.TrimSpace(lines[j]); s != "" && !strings.HasPrefix(s, "//") {
			return j
		}
	}
	return i
}

// depthAt returns the bracket depth at the start of line i.
func depthAt(lines []string, i int) int {
	d := 0
	for _, l := range lines[:i] {
		d += bracketDepth(codeOf(l))
	}
	return d
}

// directiveAbove reports whether a //pu: or //go: directive leads line i.
func directiveAbove(lines []string, i int) bool {
	b := &specBlock{lines: lines}
	return b.directiveBefore(i)
}

// declaredNames returns the names a statement declares with := or var,
// and the rest of its first line after them.
func declaredNames(stmt []string) ([]string, string) {
	first := firstCodeLine(stmt)
	var m []string
	var at []int
	if at = varDecl.FindStringSubmatchIndex(first); at == nil {
		at = shortDecl.FindStringSubmatchIndex(first)
	}
	if at == nil {
		return nil, ""
	}
	m = []string{first[at[0]:at[1]], first[at[2]:at[3]]}
	rest := first[at[3]:]
	var names []string
	for _, n := range strings.Split(m[1], ",") {
		n = strings.TrimSpace(n)
		if !identForm.MatchString(n) {
			// Not a list of names: a for, if or switch header, whose
			// names are in the scope of that statement.
			return nil, ""
		}
		if n == "_" || n == "var" {
			continue
		}
		names = append(names, n)
	}
	return names, rest
}

// firstCodeLine returns the first line of stmt that is not a comment,
// without its comment.
func firstCodeLine(stmt []string) string {
	return codeOf(stmt[firstCode(stmt, 0)])
}

var identForm = regexp.MustCompile(`^[A-Za-z_]\w*$`)

// stdPackages are the library packages a wrapped block may use without
// importing them; the wrapper adds the import.
var stdPackages = map[string]string{
	"alloc": "pluto/alloc", "arena": "pluto/arena", "atomic": "pluto/sync/atomic",
	"bits": "pluto/bits", "bytes": "pluto/bytes", "cmp": "pluto/cmp",
	"errors": "pluto/errors", "flag": "pluto/flag", "fmt": "pluto/fmt",
	"hash": "pluto/hash", "io": "pluto/io", "iter": "pluto/iter", "math": "pluto/math",
	"os": "pluto/os", "rt": "pluto/rt", "slices": "pluto/slices", "sort": "pluto/sort",
	"strconv": "pluto/strconv", "strings": "pluto/strings", "sync": "pluto/sync",
	"syscall": "pluto/syscall", "time": "pluto/time", "unicode": "pluto/unicode",
	"unsafe": "unsafe", "utf8": "pluto/unicode/utf8", "container": "pluto/container", "vec": "pluto/vec",
	"x86": "pluto/arch/x86", "heap": "pluto/container/heap", "debug": "pluto/alloc/debug",
}

var qualified = regexp.MustCompile(`\b([a-z]\w*)\.[A-Za-z_]`)

// file returns the source of the unit and the map from its lines to the
// block's.
func (w *wrapper) file(e *exampleEntry, complete bool) (string, map[int]int) {
	var out []srcLine
	if complete {
		out = append(out, w.top...)
		for _, l := range strings.Split(e.pre, "\n") {
			if l != "" {
				out = append(out, srcLine{l, 0})
			}
		}
		return render(out)
	}
	all := e.pre + "\n" + e.body + "\n" + e.tail
	for _, l := range w.top {
		all += "\n" + codeOf(l.text)
	}
	for _, l := range w.body {
		all += "\n" + codeOf(l.text)
	}
	imported := map[string]bool{}
	hasMain := false
	for _, l := range w.top {
		s := strings.TrimSpace(l.text)
		if strings.HasPrefix(s, "import") || (strings.HasPrefix(s, `"pluto/`) && strings.HasSuffix(s, `"`)) {
			for name, path := range stdPackages {
				if strings.Contains(s, `"`+path+`"`) {
					imported[name] = true
				}
			}
		}
		if strings.HasPrefix(s, "func main(") {
			hasMain = true
		}
	}
	var mainLines []srcLine
	if !hasMain {
		// The main function goes in before the imports are worked out,
		// since it may use a package the block does not.
		mainLines = w.mainFunc(e, all)
		for _, l := range mainLines {
			all += "\n" + l.text
		}
	}
	out = append(out, srcLine{"package main", 0}, srcLine{"", 0})
	var imports []string
	for _, m := range qualified.FindAllStringSubmatch(stringsOut(all), -1) {
		if path, ok := stdPackages[m[1]]; ok && !imported[m[1]] {
			imported[m[1]] = true
			imports = append(imports, path)
		}
	}
	sort.Strings(imports)
	// The block's own imports come first, since a file's imports precede
	// its other declarations.
	var rest []srcLine
	inImport := false
	for _, l := range w.top {
		s := strings.TrimSpace(l.text)
		if strings.HasPrefix(s, "import") {
			inImport = strings.HasSuffix(s, "(")
			out = append(out, l)
			continue
		}
		if inImport {
			out = append(out, l)
			inImport = s != ")"
			continue
		}
		rest = append(rest, l)
	}
	for _, p := range imports {
		out = append(out, srcLine{fmt.Sprintf("import %q", p), 0})
	}
	for _, l := range strings.Split(e.pre, "\n") {
		out = append(out, srcLine{l, 0})
	}
	out = append(out, rest...)
	if len(w.body) > 0 || e.body != "" || e.tail != "" {
		sig := "func specExample() {"
		if e.sig != "" {
			sig = e.sig + " {"
		}
		out = append(out, srcLine{"", 0}, srcLine{sig, 0})
		for _, l := range strings.Split(e.body, "\n") {
			if l != "" {
				out = append(out, srcLine{l, 0})
			}
		}
		out = append(out, w.body...)
		for _, l := range strings.Split(e.tail, "\n") {
			if l != "" {
				out = append(out, srcLine{l, 0})
			}
		}
		out = append(out, srcLine{"}", 0})
	}
	if !hasMain {
		out = append(out, srcLine{"", 0})
		out = append(out, mainLines...)
	}
	return render(out)
}

// startsTask matches a line that starts a task or installs an interrupt
// handler: a party of the race rules (section 19.3).
var startsTask = regexp.MustCompile(`^\s*task\s|\.Task\(|\b(InterruptGate|TrapGate|AfterFunc)\(`)

// entryFunc matches a package-level function declaration and captures its
// name, its type parameters and its parameters.
var entryFunc = regexp.MustCompile(`^func\s+(\w+)\s*(\[[^\]]*\])?\(([^)]*)\)`)

// mainFunc returns the main function the wrapper adds to a block that
// declares none. The race check finds the parties of a program from
// package initialization and main (section 19.3), so a block whose static
// checks depend on its tasks must be part of a program that starts them:
// main calls the block's entry functions, the functions nothing in the
// block or its entry calls, or runs the main: lines of its entry. all is
// the code of the unit, for finding the calls.
func (w *wrapper) mainFunc(e *exampleEntry, all string) []srcLine {
	if e.main != "" {
		w.tasks = true
		out := []srcLine{{"func main() {", 0}}
		for _, l := range strings.Split(e.main, "\n") {
			if l != "" {
				out = append(out, srcLine{l, 0})
			}
		}
		return append(out, srcLine{"}", 0})
	}
	code := stringsOut(all)
	uses := func(name string) int {
		return len(regexp.MustCompile(`\b`+name+`\b`).FindAllString(code, -1))
	}
	// An interrupt handler the block declares and does not install is
	// installed, so that it is a party (section 20.10).
	var calls []string
	handlers := map[string]bool{}
	directive := ""
	for _, l := range w.top {
		s := strings.TrimSpace(l.text)
		if m := interruptDirective.FindStringSubmatch(s); m != nil {
			directive = "InterruptGate"
			if m[1] == "trap" {
				directive = "TrapGate"
			}
			continue
		}
		if m := entryFunc.FindStringSubmatch(codeOf(l.text)); m != nil && directive != "" {
			handlers[m[1]] = true
			if uses(m[1]) == 1 {
				calls = append(calls, fmt.Sprintf("\t_ = x86.%s(%s, 8, 0)", directive, m[1]))
			}
		}
		if !strings.HasPrefix(s, "//") {
			directive = ""
		}
	}
	starts := len(calls) > 0
	for _, l := range strings.Split(code, "\n") {
		if startsTask.MatchString(l) {
			starts = true
			break
		}
	}
	if !starts {
		return []srcLine{{"func main() {}", 0}}
	}
	installs := len(calls)
	entry := func(name, tparams, params string) {
		if handlers[name] || uses(name) > 1 {
			return // a handler, or called or used by the block: not an entry
		}
		if tparams != "" || strings.TrimSpace(params) != "" {
			w.problems = append(w.problems, fmt.Sprintf("the block starts tasks and its entry function %s takes parameters: give its entry a main: that calls it", name))
			return
		}
		calls = append(calls, "\t"+name+"()")
	}
	for _, l := range w.top {
		if l.from == 0 {
			continue
		}
		if m := entryFunc.FindStringSubmatch(codeOf(l.text)); m != nil {
			entry(m[1], m[2], m[3])
		}
	}
	if len(w.body) > 0 || e.body != "" || e.tail != "" {
		sig := e.sig
		if sig == "" {
			sig = "func specExample()"
		}
		if m := entryFunc.FindStringSubmatch(sig); m != nil {
			entry(m[1], m[2], m[3])
		}
	}
	if len(calls) == installs && installs == 0 && len(w.problems) == 0 {
		w.problems = append(w.problems, "the block starts tasks and has no entry function that main can call: give its entry a main:")
	}
	w.tasks = len(calls) > 0
	out := []srcLine{{"func main() {", 0}}
	for _, c := range calls {
		out = append(out, srcLine{c, 0})
	}
	return append(out, srcLine{"}", 0})
}

// interruptDirective matches the directive of an interrupt handler and
// captures its argument.
var interruptDirective = regexp.MustCompile(`^//pu:interrupt\b\s*(\w*)`)

// stringsOut blanks the string literals of src, so that a qualified name
// in a string adds no import.
func stringsOut(src string) string {
	var b strings.Builder
	var quote byte
	for i := 0; i < len(src); i++ {
		ch := src[i]
		switch {
		case quote != 0:
			if ch == '\\' && quote != '`' {
				i++
			} else if ch == quote {
				quote = 0
			}
			b.WriteByte(' ')
			continue
		case ch == '"' || ch == '`':
			quote = ch
		}
		b.WriteByte(ch)
	}
	return b.String()
}

func render(lines []srcLine) (string, map[int]int) {
	var b strings.Builder
	m := map[int]int{}
	for i, l := range lines {
		b.WriteString(l.text)
		b.WriteByte('\n')
		if l.from > 0 {
			m[i+1] = l.from
		}
	}
	return b.String(), m
}

// numbered returns src with line numbers, for a failure message.
func numbered(src string) string {
	var b strings.Builder
	for i, l := range strings.Split(strings.TrimSuffix(src, "\n"), "\n") {
		fmt.Fprintf(&b, "%4d  %s\n", i+1, l)
	}
	return b.String()
}

// exampleDiag matches the first line of a diagnostic of a build:
// "path/main.p:12:5: msg". The explanation follows on indented lines and
// ends with the error code: (run "pluto explain E3301" ...).
var exampleDiag = regexp.MustCompile(`^(\S*?)(\w+\.p):(\d+):(\d+): (.*)$`)

// exampleDiagnostics returns the diagnostics of a build's output by the line of
// main.p they are on, each its message and codes. A diagnostic of another
// file is on line 0.
func exampleDiagnostics(out, file string) map[int][]string {
	got := map[int][]string{}
	line, msg := -1, ""
	flush := func() {
		if line >= 0 {
			got[line] = append(got[line], msg)
		}
		line = -1
	}
	for _, l := range strings.Split(out, "\n") {
		if m := exampleDiag.FindStringSubmatch(l); m != nil {
			if strings.HasPrefix(m[5], "\t") {
				// A note that points at a related line, such as the other
				// declaration of a name, belongs to the diagnostic above.
				continue
			}
			flush()
			line = 0
			if m[2] == file && !strings.Contains(m[1], "/lib/") {
				line, _ = strconv.Atoi(m[3])
			}
			msg = m[5]
			continue
		}
		if line >= 0 && strings.HasPrefix(l, "    ") {
			if c := explainCode.FindStringSubmatch(l); c != nil {
				msg += " [" + c[1] + "]"
			}
		}
	}
	flush()
	return got
}

var explainCode = regexp.MustCompile(`pluto explain (E\d{4})`)

// batchSize is the number of clean blocks built together as the packages
// of one program.
const batchSize = 40

// runExampleUnits builds the units and records their problems. Units that
// want no error build first in batches, one package per unit, and a batch
// that fails builds its units one by one to find which fail. The builds
// run a few at a time, as skillcheck's do, so that they do not slow the
// timing-sensitive tests beside them in go test ./...
func runExampleUnits(t *testing.T, c string, units []*exampleUnit) {
	slots := make(chan struct{}, 3)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var single []*exampleUnit
	var batches [][]*exampleUnit
	var clean []*exampleUnit
	for _, u := range units {
		e := u.block.entry
		if len(u.want) > 0 || e != nil && e.known != "" || u.block.mode() != "" || u.block.isComplete() || u.tasks || len(u.setup) > 0 {
			single = append(single, u)
			continue
		}
		clean = append(clean, u)
	}
	for i := 0; i < len(clean); i += batchSize {
		batches = append(batches, clean[i:min(i+batchSize, len(clean))])
	}
	run := func(f func()) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			slots <- struct{}{}
			defer func() { <-slots }()
			f()
		}()
	}
	for _, us := range batches {
		run(func() {
			if buildBatch(t, c, us) {
				return
			}
			mu.Lock()
			defer mu.Unlock()
			for _, u := range us {
				run(func() { buildUnit(t, c, u) })
			}
		})
	}
	for _, u := range single {
		run(func() { buildUnit(t, c, u) })
	}
	// Units added by a failed batch start before that batch's run
	// returns, so one Wait sees them all.
	wg.Wait()
}

// buildArgs returns the arguments of a build of a unit in mode.
func buildArgs(dir, mode string) []string {
	args := []string{"build", "--brief=false", "-O", "0", "-c", "-o", filepath.Join(dir, "out.o")}
	if mode != "" {
		args = append(args, "--mode", mode, "--gencheck=false")
	}
	return append(args, ".")
}

// buildBatch builds units that want no error as the packages of one
// program and reports whether it built cleanly.
func buildBatch(t *testing.T, c string, us []*exampleUnit) bool {
	dir, err := os.MkdirTemp("", "specex")
	if err != nil {
		t.Error(err)
		return false
	}
	defer os.RemoveAll(dir)
	var main strings.Builder
	main.WriteString("package main\n\n")
	for i, u := range us {
		pkg := fmt.Sprintf("b%03d", i)
		src := strings.Replace(u.src, "package main\n", "package "+pkg+"\n", 1)
		if err := os.Mkdir(filepath.Join(dir, pkg), 0o755); err != nil {
			t.Error(err)
			return false
		}
		if err := os.WriteFile(filepath.Join(dir, pkg, pkg+".p"), []byte(src), 0o644); err != nil {
			t.Error(err)
			return false
		}
		fmt.Fprintf(&main, "import _ %q\n", "specex/"+pkg)
	}
	main.WriteString("\nfunc main() {}\n")
	if err := os.WriteFile(filepath.Join(dir, "main.p"), []byte(main.String()), 0o644); err != nil {
		t.Error(err)
		return false
	}
	if err := os.WriteFile(filepath.Join(dir, "pluto.mod"), []byte("module specex\n"), 0o644); err != nil {
		t.Error(err)
		return false
	}
	_, code := runPluto(t, dir, nil, buildArgs(dir, "")...)
	return code == 0
}

// buildUnit builds one unit and records its problems.
func buildUnit(t *testing.T, c string, u *exampleUnit) {
	dir, err := os.MkdirTemp("", "specex")
	if err != nil {
		t.Error(err)
		return
	}
	defer os.RemoveAll(dir)
	file := "main.p"
	if pkg := u.block.packageName(); pkg != "" && pkg != "main" {
		// A file of another package builds as a package that a program
		// imports.
		file = pkg + ".p"
		main := fmt.Sprintf("package main\n\nimport _ %q\n\nfunc main() {}\n", "specex/"+pkg)
		if err := os.WriteFile(filepath.Join(dir, "main.p"), []byte(main), 0o644); err != nil {
			t.Error(err)
			return
		}
		if err := os.WriteFile(filepath.Join(dir, "pluto.mod"), []byte("module specex\n"), 0o644); err != nil {
			t.Error(err)
			return
		}
		dir = filepath.Join(dir, pkg)
		if err := os.Mkdir(dir, 0o755); err != nil {
			t.Error(err)
			return
		}
	}
	src := u.src
	build := dir
	if file != "main.p" {
		build = filepath.Dir(dir)
	}
	for _, d := range u.block.deps {
		pkg := d.packageName()
		if err := os.Mkdir(filepath.Join(build, pkg), 0o755); err != nil {
			t.Error(err)
			return
		}
		dsrc := strings.Join(d.lines, "\n") + "\n"
		if d.entry != nil {
			dsrc += d.entry.pre
		}
		if err := os.WriteFile(filepath.Join(build, pkg, pkg+".p"), []byte(dsrc), 0o644); err != nil {
			t.Error(err)
			return
		}
		// The import goes on the line of the package clause, so that the
		// lines keep their numbers.
		src = packageClause.ReplaceAllString(src, fmt.Sprintf("$1; import %q", "specex/"+pkg))
	}
	if err := os.WriteFile(filepath.Join(dir, file), []byte(src), 0o644); err != nil {
		t.Error(err)
		return
	}
	if u.block.mode() != "" && !strings.Contains(u.src, "//pu:panic") {
		// A kernel or freestanding build needs a panic handler, and most
		// examples of them do not show one.
		if err := os.WriteFile(filepath.Join(build, "specpanic.p"), []byte(kernelPanic), 0o644); err != nil {
			t.Error(err)
			return
		}
	}
	out, code := runPluto(t, build, nil, buildArgs(build, u.block.mode())...)
	u.problems = append(append([]string(nil), u.setup...), checkUnitOutput(u, out, code, file)...)
}

// packageClause matches the package clause of a file.
var packageClause = regexp.MustCompile(`(?m)^(package \w+)$`)

// kernelPanic is the panic handler of a kernel or freestanding build
// whose block has none.
const kernelPanic = "package main\n\nimport \"pluto/rt\"\n\n//pu:panic\nfunc specPanic(p *rt.Panic) {\n\tfor {\n\t}\n}\n"

// kernelDirective matches a directive that only a kernel build accepts.
var kernelDirective = regexp.MustCompile(`^\s*//pu:(percpu|interrupt|panic|scheduler)\b`)

// mode returns the build mode of the block: the one its entry gives, or
// kernel when it uses a directive of kernel mode.
func (b *specBlock) mode() string {
	if b.entry != nil && b.entry.mode != "" {
		return b.entry.mode
	}
	for _, l := range b.lines {
		if kernelDirective.MatchString(l) {
			return "kernel"
		}
	}
	return ""
}

// checkUnitOutput compares a unit's build output with what it wants.
func checkUnitOutput(u *exampleUnit, out string, code int, file string) []string {
	var problems []string
	got := map[int][]string{} // block line to messages
	for n, msgs := range exampleDiagnostics(out, file) {
		line := u.lineOf[n]
		if m, ok := u.at[line]; ok {
			line = m
		}
		got[line] = append(got[line], msgs...)
	}
	for line, msgs := range got {
		if _, ok := u.want[line]; ok {
			continue
		}
		where := fmt.Sprintf("line %d", line)
		if line == 0 {
			where = "the wrapping"
		}
		for _, msg := range msgs {
			problems = append(problems, fmt.Sprintf("%s: unexpected error on %s: %s", u.what, where, msg))
		}
	}
	for line, codes := range u.want {
		msgs, ok := got[line]
		if !ok {
			problems = append(problems, fmt.Sprintf("%s: no error reported on line %d", u.what, line))
			continue
		}
		joined := strings.Join(msgs, "\n")
		for _, code := range codes {
			if !strings.Contains(joined, code) {
				problems = append(problems, fmt.Sprintf("%s: want %s on line %d, got: %s", u.what, code, line, joined))
			}
		}
	}
	if code != 0 && len(got) == 0 {
		problems = append(problems, fmt.Sprintf("%s: the build failed without a diagnostic of main.p:\n%s", u.what, out))
	}
	if code == 0 && len(u.want) > 0 && len(problems) == 0 {
		problems = append(problems, fmt.Sprintf("%s: the build succeeded", u.what))
	}
	return problems
}

// An exampleEntry is what examples.txt says about a block.
type exampleEntry struct {
	skip      string   // why the block is not compiled
	known     string   // why the block fails: a roadmap item or "spec example wrong"
	mode      string   // the build mode: kernel or freestanding
	pre       string   // package-level declarations the block assumes
	body      string   // statements that start the function body
	tail      string   // statements that end the function body
	sig       string   // the signature of the function the statements go in
	main      string   // the body of the main function the wrapper adds
	valuesTop bool     // var and const declarations go to package level
	together  bool     // the marked lines are errors of one program, built together
	with      []string // the blocks whose declarations the block assumes
}

// knownCause returns the cause of a known failure: "item N" or "spec
// example wrong".
func knownCause(known string) string {
	m := knownForm.FindStringSubmatch(known)
	if m == nil {
		return known
	}
	if m[1][0] >= '0' && m[1][0] <= '9' {
		return "item " + m[1]
	}
	return m[1]
}

var knownForm = regexp.MustCompile(`^(\d+|spec example wrong|no item):? `)

// readExampleData reads examples.txt: entries that start with a line
// "== FILE SECTION | FIRST LINE", followed by "key: value" lines and
// indented lines that continue pre: or body:.
func readExampleData(t *testing.T, path string) map[string]*exampleEntry {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	data := map[string]*exampleEntry{}
	var e *exampleEntry
	var multi *string
	n := 0
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		n++
		line := sc.Text()
		if strings.HasPrefix(line, "\t") && multi != nil {
			*multi += strings.TrimPrefix(line, "\t") + "\n"
			continue
		}
		multi = nil
		s := strings.TrimSpace(line)
		if s == "" || strings.HasPrefix(s, "#") {
			continue
		}
		if k, ok := strings.CutPrefix(s, "== "); ok {
			if data[k] != nil {
				t.Errorf("%s:%d: a second entry for %q", path, n, k)
			}
			e = &exampleEntry{}
			data[k] = e
			continue
		}
		if e == nil {
			t.Errorf("%s:%d: %q outside an entry", path, n, s)
			continue
		}
		key, val, _ := strings.Cut(s, ":")
		val = strings.TrimSpace(val)
		switch key {
		case "skip":
			e.skip = val
		case "known":
			if !knownForm.MatchString(val) {
				t.Errorf("%s:%d: a known failure starts with its roadmap item, \"spec example wrong\" or \"no item\": %q", path, n, val)
			}
			e.known = val
		case "mode":
			e.mode = val
		case "sig":
			e.sig = val
		case "with":
			e.with = append(e.with, val)
		case "values":
			e.valuesTop = val == "top"
		case "together":
			if val != "" {
				t.Errorf("%s:%d: together takes no value: %q", path, n, val)
			}
			e.together = true
		case "pre":
			multi = &e.pre
		case "body":
			multi = &e.body
		case "tail":
			multi = &e.tail
		case "main":
			multi = &e.main
		default:
			t.Errorf("%s:%d: unknown key %q", path, n, key)
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return data
}
