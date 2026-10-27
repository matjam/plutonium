// Ported from internal/lsp/features_test.go at the go-cutover tag. The
// test of the extracted standard library (TestDefinitionExtractedStdlib)
// is left out: the compiler no longer embeds the library, so there is no
// extraction to test.

package lsp

import (
	"pluto/path/filepath"
	"pluto/slices"
	"pluto/strings"
	"pluto/testing"

	"compiler/compat"
	"compiler/pu"
)

// openDemo starts a server on the demo module with main.p open and waits
// for its first diagnostics. The test removes the directory it returns.
func openDemo(t *testing.T) (client, string) {
	t.Helper()
	root := writeFiles(t, demo)
	c := startServer(t, Options{})
	c.initialize(root, nil)
	main := filepath.Join(root, "main.p")
	c.open(main)
	c.diagnostics(main, func([]Diagnostic) bool { return true })
	return c, root
}

func TestDiagnostics(t *testing.T) {
	c, root := openDemo(t)
	defer removeAll(root)
	main := filepath.Join(root, "main.p")

	// A type error.
	broken := strings.Replace(demo["main.p"], "_ = c", "_ = undefinedName", 1)
	c.change(main, 2, broken)
	diags := c.diagnostics(main, func(d []Diagnostic) bool { return len(d) > 0 })
	if len(diags) != 2 { // the name, and c is now unused
		t.Fatalf("diagnostics = %s", messagesOf(diags))
	}
	undef := -1
	for i := range diags {
		if strings.Contains(diags[i].Message, "undefinedName") {
			undef = i
		}
	}
	src := newText(compat.Bytes(broken))
	i := strings.Index(broken, "undefinedName")
	if undef < 0 {
		t.Errorf("no undefined name diagnostic in %s", messagesOf(diags))
	} else if d := diags[undef]; d.Range != src.rangeOf(i, i+len("undefinedName")) || d.Severity != SeverityError || d.Source != "pluto" {
		t.Errorf("undefined name diagnostic: %s, severity %d, source %s", d.Message, d.Severity, d.Source)
	}

	// A syntax error.
	c.change(main, 3, strings.Replace(demo["main.p"], "func main() {", "func main() {{", 1))
	diags = c.diagnostics(main, func(d []Diagnostic) bool { return hasMessage(d, "expected") })
	if len(diags) == 0 {
		t.Fatal("no syntax error")
	}

	// A Plutonium rule: complex numbers are not part of the language.
	c.change(main, 4, demo["main.p"]+"\nvar z = 1i\n")
	c.diagnostics(main, func(d []Diagnostic) bool { return hasMessage(d, "not permitted in Plutonium") })

	// An error in another package is reported against its file, and an
	// edit there rechecks the open package.
	shapes := filepath.Join(root, "shapes", "shapes.p")
	c.open(shapes)
	c.change(shapes, 2, strings.Replace(demo["shapes/shapes.p"], "func Area", "func Area2", 1))
	c.diagnostics(main, func(d []Diagnostic) bool { return hasMessage(d, "Area") })

	// Fixing both clears the diagnostics, on save.
	c.change(shapes, 3, demo["shapes/shapes.p"])
	c.change(main, 5, demo["main.p"])
	c.docNotify("textDocument/didSave", main)
	c.diagnostics(main, func(d []Diagnostic) bool { return len(d) == 0 })

	c.docNotify("textDocument/didClose", shapes)
	c.shutdown()
}

func TestDiagnosticsIncrementalChange(t *testing.T) {
	c, root := openDemo(t)
	defer removeAll(root)
	main := filepath.Join(root, "main.p")
	// A client that sends ranges despite full sync.
	r := Range{Start: at(t, main, "_ = c", 4), End: at(t, main, "_ = c", 5)}
	c.changeRange(main, 2, &r, "missing")
	c.diagnostics(main, func(d []Diagnostic) bool { return hasMessage(d, "missing") })
}

// A directiveCase is a diagnostic TestDiagnosticsDirectives expects: its
// code and message, and the comment it is about.
type directiveCase struct {
	code, message, comment string
	found                  bool
}

// TestDiagnosticsDirectives checks that a //pu: comment the compiler would
// ignore is reported: a misspelt directive, a removed one and a misplaced one.
func TestDiagnosticsDirectives(t *testing.T) {
	c, root := openDemo(t)
	defer removeAll(root)
	main := filepath.Join(root, "main.p")
	bad := demo["main.p"] + "\n//pu:interupt\nfunc handler() {}\n\n//pu:alloc\nfunc build() {}\n\n//pu:percpu\nfunc notAVariable() {}\n"
	c.change(main, 2, bad)
	diags := c.diagnostics(main, func(d []Diagnostic) bool { return len(d) >= 3 })
	want := []directiveCase{
		{code: "E2600", message: "unknown directive //pu:interupt (did you mean //pu:interrupt?)", comment: "//pu:interupt"},
		{code: "E2601", message: "//pu:alloc was removed: kernel code may allocate strings anywhere the allocator may be called", comment: "//pu:alloc"},
		{code: "E2602", message: "//pu:percpu is in the wrong place: it goes in the doc comment of a package-level variable, not of a function", comment: "//pu:percpu"},
	}
	src := newText(compat.Bytes(bad))
	for _, d := range diags {
		for i := range want {
			w := &want[i]
			if w.code != d.Code || w.found {
				continue
			}
			off := strings.Index(bad, w.comment)
			if d.Message != w.message || d.Severity != SeverityError || d.Range.Start != src.rangeOf(off, off).Start {
				t.Errorf("%s: %s at %d:%d", d.Code, d.Message, d.Range.Start.Line, d.Range.Start.Character)
			}
			w.found = true
		}
	}
	for _, w := range want {
		if !w.found {
			t.Errorf("no %s diagnostic in %s", w.code, messagesOf(diags))
		}
	}
	c.shutdown()
}

func TestBuildModes(t *testing.T) {
	root := writeFiles(t, map[string]string{
		"k/kernel.p":  "//pu:build kernel\n\npackage k\n\nvar K int = \"no\"\n",
		"k/hosted.p":  "//pu:build hosted\n\npackage k\n\nvar H = 1\n",
		"k/never.p":   "//pu:build never\n\npackage k\n",
		"k/badtag.p":  "//pu:build (\n\npackage k\n",
		"k/common.p":  "package k\n",
		"other/x.p":   "package other\n",
		"other/y.p":   "package wrong\n",
		"loose/lp.p":  "package loose\n\nimport \"example.com/x\"\n\nvar _ = x.Y\n",
		"loose/lq.p":  "package loose\n\nimport \"pluto/nosuchpkg\"\n",
		"cycle/a/a.p": "package a\n",
	})
	defer removeAll(root)
	logs := pu.Alloc[compat.Buffer]()
	c := startServer(t, Options{Log: logs, Verbose: true})
	c.initialize(root, nil)

	// A kernel file is checked in kernel mode.
	kernel := filepath.Join(root, "k", "kernel.p")
	c.open(kernel)
	c.diagnostics(kernel, func(d []Diagnostic) bool { return hasMessage(d, "cannot use") })
	// A file no mode selects says so.
	never := filepath.Join(root, "k", "never.p")
	c.open(never)
	c.diagnostics(never, func(d []Diagnostic) bool {
		return len(d) == 1 && d[0].Severity == SeverityInformation && strings.Contains(d[0].Message, "not part of")
	})
	// Packages disagreeing about their name, and bad imports.
	other := filepath.Join(root, "other", "y.p")
	c.open(other)
	c.diagnostics(other, func(d []Diagnostic) bool { return len(d) > 0 })
	loose := filepath.Join(root, "loose", "lp.p")
	c.open(loose)
	c.diagnostics(loose, func(d []Diagnostic) bool { return hasMessage(d, "no pluto.mod") })
	c.diagnostics(filepath.Join(root, "loose", "lq.p"), func(d []Diagnostic) bool { return hasMessage(d, "not found") })

	// Switching the configured mode to kernel rechecks.
	c.notify("workspace/didChangeConfiguration", compat.Bytes(`{"settings":{"plutonium":{"target":"x86_64-none","mode":"kernel"}}}`))
	c.diagnostics(never, func(d []Diagnostic) bool {
		return len(d) == 1 && strings.Contains(d[0].Message, "kernel build")
	})
	c.notify("workspace/didChangeConfiguration", compat.Bytes(`{"settings":{"plutonium":{"mode":"bogus"}}}`))
	c.shutdown()
	if !strings.Contains(logs.String(), "level=DEBUG") {
		t.Errorf("the verbose log has no debug lines: %q", logs.String())
	}
}

// A modesCase is a case of TestModesFor.
type modesCase struct {
	name string
	s    Settings
	want string // first mode, or "error"
}

func TestModesFor(t *testing.T) {
	for _, tc := range []modesCase{
		{"default", Settings{}, "hosted"},
		{"none", Settings{Target: "x86_64-none"}, "freestanding"},
		{"kernel", Settings{Target: "x86_64-none", Mode: "kernel"}, "kernel"},
		{"hosted on none", Settings{Target: "x86_64-none", Mode: "hosted"}, "error"},
		{"bad target", Settings{Target: "arm"}, "error"},
		{"bad mode", Settings{Mode: "x"}, "error"},
		{"explicit hosted", Settings{Mode: "hosted", Tags: []string{"debug"}}, "hosted"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			modes, err := modesFor(tc.s)
			got := "error"
			if err == nil {
				got = modes[0].name
				if len(modes) != 3 {
					t.Errorf("%d modes", len(modes))
				}
			}
			if got != tc.want {
				t.Errorf("first mode %s, want %s", got, tc.want)
			}
		})
	}
}

// A hoverCase is a case of TestHover.
type hoverCase struct {
	name, path, marker string
	n                  int
	want               []string
}

func TestHover(t *testing.T) {
	c, root := openDemo(t)
	defer removeAll(root)
	main := filepath.Join(root, "main.p")
	shapes := filepath.Join(root, "shapes", "shapes.p")
	hover := func(path, marker string, n int) string {
		t.Helper()
		h := c.hover(path, at(t, path, marker, n))
		if h == nil {
			return ""
		}
		return h.Contents.Value
	}
	for _, tc := range []hoverCase{
		{"owner variable", main, "var b", 4, []string{"var b shapes.Buffer", "owner (moves)", "moves on assignment and is dropped"}},
		{"field", main, "b.Size", 2, []string{"field Size int", "Size is the size.", "**value**"}},
		{"function with directive", main, "shapes.Area", 7, []string{"func shapes.Area(w int, h int) int", "Area computes an area.", "`//pu:noinline`: never inline"}},
		{"enum member", main, "Color.Green", 6, []string{"const Color.Green shapes.Color = 1"}},
		{"enum type", main, "Color.Green", 0, []string{"type Color enum uint8 {", "Red   = 0", "Blue  = 2", "Color is a colour."}},
		{"package", main, "shapes.Buffer", 0, []string{"package shapes (\"example.com/demo/shapes\")", "Package shapes has shapes."}},
		{"stdlib function", main, "HasPrefix", 0, []string{"func strings.HasPrefix(s string, prefix string) bool", "HasPrefix"}},
		{"method", main, "b.Len", 2, []string{"func (*shapes.Buffer).Len() int", "Len returns the size."}},
		{"view", shapes, "data []byte", 0, []string{"field data []byte", "**view**"}},
		{"directive", shapes, "//pu:noinline", 6, []string{"`//pu:noinline`: never inline this function"}},
		{"plain comment", shapes, "// Area computes", 4, nil},
		{"keyword", main, "func main", 1, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := hover(tc.path, tc.marker, tc.n)
			if tc.want == nil && got != "" {
				t.Errorf("hover = %q, want none", got)
			}
			for _, w := range tc.want {
				if !strings.Contains(got, w) {
					t.Errorf("hover = %q, want it to contain %q", got, w)
				}
			}
		})
	}
}

func TestDefinition(t *testing.T) {
	c, root := openDemo(t)
	defer removeAll(root)
	main := filepath.Join(root, "main.p")
	shapes := filepath.Join(root, "shapes", "shapes.p")
	def := func(marker string, n int) []Location {
		t.Helper()
		return c.definition(main, at(t, main, marker, n))
	}
	// Into another package of the module.
	locs := def("shapes.Area", 7)
	if len(locs) != 1 || pathFromURI(locs[0].URI) != shapes || locs[0].Range.Start != at(t, shapes, "Area(w", 0) {
		t.Errorf("definition of Area: %d locations", len(locs))
	}
	// A local variable.
	locs = def("_ = c", 4)
	if len(locs) != 1 || locs[0].Range.Start != at(t, main, "c :=", 0) {
		t.Errorf("definition of c: %d locations", len(locs))
	}
	// Into the standard library.
	locs = def("HasPrefix", 2)
	lib := filepath.Join(c.stdlib, "pluto", "strings")
	if len(locs) != 1 || filepath.Dir(pathFromURI(locs[0].URI)) != lib {
		t.Fatalf("definition of HasPrefix: %d locations", len(locs))
	}
	// An import path and a package name go to the package.
	locs = def("\"pluto/strings\"", 3)
	if len(locs) != 1 || filepath.Dir(pathFromURI(locs[0].URI)) != lib {
		t.Errorf("definition of the import: %d locations", len(locs))
	}
	locs = def("shapes.Buffer", 1)
	if len(locs) != 1 || pathFromURI(locs[0].URI) != shapes {
		t.Errorf("definition of shapes: %d locations", len(locs))
	}
	// Nothing at a keyword or a predeclared name.
	if locs := def("func main", 0); len(locs) != 0 {
		t.Errorf("definition of func: %d locations", len(locs))
	}

	// From inside the standard library file, references stay there.
	stdLocs := def("HasPrefix", 2)
	if len(stdLocs) != 1 {
		t.Fatalf("definition of HasPrefix: %d locations", len(stdLocs))
	}
	stdFile := pathFromURI(stdLocs[0].URI)
	refs := c.references(stdFile, at(t, stdFile, "func HasPrefix", 5), true)
	if len(refs) < 2 {
		t.Errorf("references to HasPrefix from the library: %d", len(refs))
	}
}

func TestReferences(t *testing.T) {
	c, root := openDemo(t)
	defer removeAll(root)
	main := filepath.Join(root, "main.p")
	shapes := filepath.Join(root, "shapes", "shapes.p")
	// Size: declared in shapes, used in shapes and main.
	locs := c.references(shapes, at(t, shapes, "Size int", 0), true)
	if len(locs) != 3 {
		t.Errorf("references to Size: %d", len(locs))
	}
	locs = c.references(main, at(t, main, "b.Size", 2), false)
	if len(locs) != 2 {
		t.Errorf("references to Size without the declaration: %d", len(locs))
	}
	var files []string
	for _, l := range locs {
		files = compat.Append(files, filepath.Base(pathFromURI(l.URI)))
	}
	if !slices.Equal(files, []string{"main.p", "shapes.p"}) {
		t.Errorf("files = %s", compat.Join(files, " "))
	}
	if locs := c.references(main, at(t, main, "func main", 0), true); len(locs) != 0 {
		t.Errorf("references to a keyword: %d", len(locs))
	}
}

func TestSymbols(t *testing.T) {
	c, root := openDemo(t)
	defer removeAll(root)
	shapes := filepath.Join(root, "shapes", "shapes.p")
	syms := c.documentSymbols(shapes)
	var names []string
	for _, s := range syms {
		names = compat.Append(names, s.Name)
	}
	if !slices.Equal(names, []string{"Color", "Buffer", "(*Buffer).Drop", "(*Buffer).Len", "Area"}) {
		t.Errorf("symbols = %s", compat.Join(names, " "))
	}
	if len(syms) == 5 {
		if syms[0].Kind != SymbolEnum || len(syms[0].Children) != 3 || syms[0].Children[1].Name != "Green" {
			t.Errorf("Color: %s, kind %d, %d children", syms[0].Name, syms[0].Kind, len(syms[0].Children))
		}
		if syms[1].Kind != SymbolStruct || len(syms[1].Children) != 2 || !strings.HasPrefix(syms[1].Detail, "owner") {
			t.Errorf("Buffer: %s, kind %d, %d children, detail %q", syms[1].Name, syms[1].Kind, len(syms[1].Children), syms[1].Detail)
		}
		if syms[4].Kind != SymbolFunction || syms[4].Detail != "func(w int, h int) int" {
			t.Errorf("Area: %s, kind %d, detail %q", syms[4].Name, syms[4].Kind, syms[4].Detail)
		}
	}

	infos := c.workspaceSymbols("gren")
	if len(infos) != 1 || infos[0].Name != "Green" || infos[0].ContainerName != "example.com/demo/shapes.Color" {
		t.Errorf("workspace symbols for gren: %d", len(infos))
	}
	infos = c.workspaceSymbols("")
	if len(infos) < 8 {
		t.Errorf("all workspace symbols: %d", len(infos))
	}
}

func TestFormatting(t *testing.T) {
	root := writeFiles(t, map[string]string{
		"a.p":   "package a\n\nfunc F()   int {\nreturn 1\n}\n\nvar X = 1\n",
		"b.p":   "package a\n\nvar Y = 2\n",
		"bad.p": "package a\n\nfunc {\n",
	})
	defer removeAll(root)
	c := startServer(t, Options{})
	c.initialize(root, nil)
	format := func(name string) []TextEdit {
		return c.formatting(filepath.Join(root, name))
	}
	edits := format("a.p")
	want := []TextEdit{{Range: Range{Start: Position{2, 0}, End: Position{4, 0}}, NewText: "func F() int {\n\treturn 1\n"}}
	if !slices.Equal(edits, want) {
		t.Errorf("edits: %d, the first %q", len(edits), editText(edits))
	}
	if edits := format("b.p"); len(edits) != 0 {
		t.Errorf("formatted file: %d edits", len(edits))
	}
	if edits := format("bad.p"); len(edits) != 0 {
		t.Errorf("file with syntax errors: %d edits", len(edits))
	}
	w := newJSONWriter()
	w.beginObject()
	w.key("textDocument")
	w.beginObject()
	w.key("uri")
	w.str("untitled:1")
	w.endObject()
	w.endObject()
	if _, rerr := c.call("textDocument/formatting", w.bytes()); rerr == nil {
		t.Error("formatting an untitled document succeeded")
	}
}

// editText returns the new text of the first of edits, for messages.
func editText(edits []TextEdit) string {
	if len(edits) == 0 {
		return ""
	}
	return edits[0].NewText
}

// A replaceCase is a case of TestReplaceEdits.
type replaceCase struct{ src, out, newText string }

func TestReplaceEdits(t *testing.T) {
	for _, tc := range []replaceCase{
		{"a\nb\nc\n", "a\nB\nc\n", "B\n"},
		{"a\nb\n", "a\nb\nc\n", "c\n"},
		{"x", "y", "y"},
		{"a\nb\nc\n", "a\nc\n", ""},
	} {
		edits := replaceEdits(compat.Bytes(tc.src), compat.Bytes(tc.out))
		if len(edits) != 1 || edits[0].NewText != tc.newText {
			t.Errorf("replaceEdits(%q, %q): %d edits, the first %q", tc.src, tc.out, len(edits), editText(edits))
			continue
		}
		// Applying the edit gives out.
		txt := newText(compat.Bytes(tc.src))
		start, end := txt.offset(edits[0].Range.Start), txt.offset(edits[0].Range.End)
		if got := strings.Slice(tc.src, 0, start) + edits[0].NewText + strings.SliceFrom(tc.src, end); got != tc.out {
			t.Errorf("applying the edit to %q gives %q, want %q", tc.src, got, tc.out)
		}
	}
}

// A completionCase is a case of TestCompletion: the completions after the
// marker in the source must include want and not notWant.
type completionCase struct {
	name, src, marker string
	want, notWant     []string
}

// complete edits path to src and returns the labels of the completions
// after the first marker.
func (c *client) complete(path, src, marker string) []string {
	c.t.Helper()
	c.edit(path, src)
	pos := newText(compat.Bytes(src)).position(strings.Index(src, marker) + len(marker))
	var labels []string
	for _, it := range c.completion(path, pos).Items {
		labels = compat.Append(labels, it.Label)
	}
	return labels
}

// checkLabels reports the labels of want that got lacks and the labels of
// notWant that it has.
func checkLabels(t *testing.T, got, want, notWant []string) {
	t.Helper()
	for _, w := range want {
		if !slices.Contains(got, w) {
			t.Errorf("completions %s lack %s", compat.Join(got, " "), w)
		}
	}
	for _, w := range notWant {
		if slices.Contains(got, w) {
			t.Errorf("completions %s include %s", compat.Join(got, " "), w)
		}
	}
}

func TestCompletion(t *testing.T) {
	root := writeFiles(t, demo)
	defer removeAll(root)
	c := startServer(t, Options{})
	c.initialize(root, nil)
	main := filepath.Join(root, "main.p")
	body := func(stmt string) string {
		return strings.Replace(demo["main.p"], "\t_ = c\n", "\t_ = c\n\t"+stmt+"\n", 1)
	}
	for _, tc := range []completionCase{
		{"package members", body("strings.Has"), "strings.Has", []string{"HasPrefix", "HasSuffix"}, []string{"Index"}},
		{"package after dot", body("shapes."), "shapes.", []string{"Area", "Buffer", "Color"}, []string{"data"}},
		{"fields and methods", body("b."), "b.", []string{"Size", "Len", "Drop"}, []string{"data"}},
		{"enum members", body("_ = shapes.Color."), "shapes.Color.", []string{"Red", "Green", "Blue"}, nil},
		{"scope", body("_ = st"), "_ = st", []string{"strings"}, []string{"shapes"}},
		{"locals and keywords", body("x := b"), "x := b", []string{"b", "break", "bool"}, nil},
		{"task is a keyword", body("t"), "\tt", []string{"task"}, nil},
		{"go is not offered", body("g"), "\tg", []string{"goto"}, []string{"go"}},
		{"value of a call", body("_ = strings.FieldsIter(\"a\")."), "FieldsIter(\"a\").", []string{"Next"}, nil},
		{"in a comment", body("// strings."), "// strings.", nil, []string{"HasPrefix"}},
		{"in a string", body("_ = \"strings."), "\"strings.", nil, []string{"HasPrefix"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			checkLabels(t, c.complete(main, tc.src, tc.marker), tc.want, tc.notWant)
		})
	}
}

func TestSignatureHelp(t *testing.T) {
	root := writeFiles(t, demo)
	defer removeAll(root)
	c := startServer(t, Options{})
	c.initialize(root, nil)
	main := filepath.Join(root, "main.p")
	help := func(src, marker string) *SignatureHelp {
		t.Helper()
		c.edit(main, src)
		pos := newText(compat.Bytes(src)).position(strings.Index(src, marker) + len(marker))
		return c.signatureHelp(main, pos)
	}
	h := help(demo["main.p"], "shapes.Area(2, ")
	if h == nil || len(h.Signatures) != 1 || h.ActiveParameter != 1 {
		t.Fatalf("signature help: %v", h != nil)
	}
	sig := h.Signatures[0]
	if sig.Label != "Area(w int, h int) int" || sig.Documentation == nil || !strings.Contains(sig.Documentation.Value, "computes") {
		t.Errorf("signature = %q", sig.Label)
	}
	if p := sig.Parameters[1].Label; strings.Slice(sig.Label, p[0], p[1]) != "h int" {
		t.Errorf("second parameter label %d to %d", p[0], p[1])
	}
	if h := help(demo["main.p"], "func main("); h != nil {
		t.Errorf("signature help outside a call: %d signatures", len(h.Signatures))
	}
}

// An argCase is a case of TestSignatureInfo.
type argCase struct {
	src  string
	want int
}

func TestSignatureInfo(t *testing.T) {
	for _, tc := range []argCase{
		{"a, b", 1}, {"f(a, b), ", 1}, {"\"a,b\", 'c', `d,`, ", 3}, {"x // a, b\n", 0},
	} {
		if got := argIndex(compat.Bytes(tc.src), 0, len(tc.src)); got != tc.want {
			t.Errorf("argIndex(%q) = %d, want %d", tc.src, got, tc.want)
		}
	}
}

// A renameCase is a rename TestRename expects to fail.
type renameCase struct {
	name, path, marker string
	n                  int
	newName            string
}

func TestRename(t *testing.T) {
	c, root := openDemo(t)
	defer removeAll(root)
	main := filepath.Join(root, "main.p")
	shapes := filepath.Join(root, "shapes", "shapes.p")
	prep := c.prepareRename(main, at(t, main, "b.Size", 3))
	if prep == nil || prep.Placeholder != "Size" {
		t.Errorf("prepareRename: %v", prep != nil)
	}
	edit := c.rename(main, at(t, main, "b.Size", 2), "Bytes")
	if len(edit[uriFromPath(main)]) != 1 || len(edit[uriFromPath(shapes)]) != 2 {
		t.Errorf("rename edits: %d in main.p, %d in shapes.p", len(edit[uriFromPath(main)]), len(edit[uriFromPath(shapes)]))
	}
	for _, tc := range []renameCase{
		{"stdlib", main, "HasPrefix", 0, "X"},
		{"predeclared", main, "_ = c", 0, "X"},
		{"package", main, "shapes.Area", 0, "X"},
		{"keyword", main, "func main", 0, "X"},
		{"bad name", main, "b.Size", 2, "1x"},
		{"conflict", main, "c :=", 0, "b"},
		{"method conflict", shapes, "Len()", 0, "Drop"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v, rerr := c.call("textDocument/rename", renameParams(tc.path, at(t, tc.path, tc.marker, tc.n), tc.newName))
			if rerr == nil {
				t.Errorf("rename succeeded: %s", v.raw)
			}
		})
	}
	if _, rerr := c.call("textDocument/prepareRename", posParams(main, at(t, main, "HasPrefix", 0))); rerr == nil {
		t.Error("prepareRename of a library function succeeded")
	}
}
