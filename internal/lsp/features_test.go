package lsp

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

// openDemo starts a server on the demo module with main.p open and waits
// for its first diagnostics.
func openDemo(t *testing.T) (*client, string) {
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
	main := filepath.Join(root, "main.p")
	uri := uriFromPath(main)

	// A type error.
	broken := strings.Replace(demo["main.p"], "_ = c", "_ = undefinedName", 1)
	c.notify("textDocument/didChange", DidChangeTextDocumentParams{
		TextDocument:   VersionedTextDocumentIdentifier{URI: uri, Version: 2},
		ContentChanges: []TextDocumentContentChangeEvent{{Text: broken}},
	})
	diags := c.diagnostics(main, func(d []Diagnostic) bool { return len(d) > 0 })
	if len(diags) != 2 { // the name, and c is now unused
		t.Fatalf("diagnostics = %+v", diags)
	}
	var undef *Diagnostic
	for i := range diags {
		if strings.Contains(diags[i].Message, "undefinedName") {
			undef = &diags[i]
		}
	}
	src := newText([]byte(broken))
	i := strings.Index(broken, "undefinedName")
	if undef == nil || undef.Range != src.rangeOf(i, i+len("undefinedName")) || undef.Severity != SeverityError || undef.Source != "pluto" {
		t.Errorf("undefined name diagnostic = %+v", undef)
	}

	// A syntax error.
	c.notify("textDocument/didChange", DidChangeTextDocumentParams{
		TextDocument:   VersionedTextDocumentIdentifier{URI: uri, Version: 3},
		ContentChanges: []TextDocumentContentChangeEvent{{Text: strings.Replace(demo["main.p"], "func main() {", "func main() {{", 1)}},
	})
	diags = c.diagnostics(main, func(d []Diagnostic) bool {
		return slices.ContainsFunc(d, func(d Diagnostic) bool { return strings.Contains(d.Message, "expected") })
	})
	if len(diags) == 0 {
		t.Fatal("no syntax error")
	}

	// A Plutonium rule: complex numbers are not part of the language.
	c.notify("textDocument/didChange", DidChangeTextDocumentParams{
		TextDocument:   VersionedTextDocumentIdentifier{URI: uri, Version: 4},
		ContentChanges: []TextDocumentContentChangeEvent{{Text: demo["main.p"] + "\nvar z = 1i\n"}},
	})
	c.diagnostics(main, func(d []Diagnostic) bool {
		return slices.ContainsFunc(d, func(d Diagnostic) bool { return strings.Contains(d.Message, "not permitted in Plutonium") })
	})

	// An error in another package is reported against its file, and an
	// edit there rechecks the open package.
	shapes := filepath.Join(root, "shapes", "shapes.p")
	c.open(shapes)
	c.notify("textDocument/didChange", DidChangeTextDocumentParams{
		TextDocument:   VersionedTextDocumentIdentifier{URI: uriFromPath(shapes), Version: 2},
		ContentChanges: []TextDocumentContentChangeEvent{{Text: strings.Replace(demo["shapes/shapes.p"], "func Area", "func Area2", 1)}},
	})
	c.diagnostics(main, func(d []Diagnostic) bool {
		return slices.ContainsFunc(d, func(d Diagnostic) bool { return strings.Contains(d.Message, "Area") })
	})

	// Fixing both clears the diagnostics, on save.
	c.notify("textDocument/didChange", DidChangeTextDocumentParams{
		TextDocument:   VersionedTextDocumentIdentifier{URI: uriFromPath(shapes), Version: 3},
		ContentChanges: []TextDocumentContentChangeEvent{{Text: demo["shapes/shapes.p"]}},
	})
	c.notify("textDocument/didChange", DidChangeTextDocumentParams{
		TextDocument:   VersionedTextDocumentIdentifier{URI: uri, Version: 5},
		ContentChanges: []TextDocumentContentChangeEvent{{Text: demo["main.p"]}},
	})
	c.notify("textDocument/didSave", DidSaveTextDocumentParams{TextDocument: TextDocumentIdentifier{URI: uri}})
	c.diagnostics(main, func(d []Diagnostic) bool { return len(d) == 0 })

	c.notify("textDocument/didClose", DidCloseTextDocumentParams{TextDocument: TextDocumentIdentifier{URI: uriFromPath(shapes)}})
	c.shutdown()
}

func TestDiagnosticsIncrementalChange(t *testing.T) {
	c, root := openDemo(t)
	main := filepath.Join(root, "main.p")
	// A client that sends ranges despite full sync.
	r := Range{Start: at(t, main, "_ = c", 4), End: at(t, main, "_ = c", 5)}
	c.notify("textDocument/didChange", DidChangeTextDocumentParams{
		TextDocument:   VersionedTextDocumentIdentifier{URI: uriFromPath(main), Version: 2},
		ContentChanges: []TextDocumentContentChangeEvent{{Range: &r, Text: "missing"}},
	})
	c.diagnostics(main, func(d []Diagnostic) bool {
		return slices.ContainsFunc(d, func(d Diagnostic) bool { return strings.Contains(d.Message, "missing") })
	})
}

// TestDiagnosticsDirectives checks that a //pu: comment the compiler would
// ignore is reported: a misspelt directive, a removed one and a misplaced one.
func TestDiagnosticsDirectives(t *testing.T) {
	c, root := openDemo(t)
	main := filepath.Join(root, "main.p")
	bad := demo["main.p"] + "\n//pu:interupt\nfunc handler() {}\n\n//pu:alloc\nfunc build() {}\n\n//pu:percpu\nfunc notAVariable() {}\n"
	c.notify("textDocument/didChange", DidChangeTextDocumentParams{
		TextDocument:   VersionedTextDocumentIdentifier{URI: uriFromPath(main), Version: 2},
		ContentChanges: []TextDocumentContentChangeEvent{{Text: bad}},
	})
	diags := c.diagnostics(main, func(d []Diagnostic) bool { return len(d) >= 3 })
	want := map[string][2]string{ // code: message, the comment it is about
		"E2600": {"unknown directive //pu:interupt (did you mean //pu:interrupt?)", "//pu:interupt"},
		"E2601": {"//pu:alloc was removed: kernel code may allocate strings anywhere the allocator may be called", "//pu:alloc"},
		"E2602": {"//pu:percpu is in the wrong place: it goes in the doc comment of a package-level variable, not of a function", "//pu:percpu"},
	}
	src := newText([]byte(bad))
	for _, d := range diags {
		w, ok := want[d.Code]
		if !ok {
			continue
		}
		i := strings.Index(bad, w[1])
		if d.Message != w[0] || d.Severity != SeverityError || d.Range.Start != src.rangeOf(i, i).Start {
			t.Errorf("%s: %+v", d.Code, d)
		}
		delete(want, d.Code)
	}
	for code := range want {
		t.Errorf("no %s diagnostic in %+v", code, diags)
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
	c := startServer(t, Options{Log: slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelDebug}))})
	c.initialize(root, nil)

	// A kernel file is checked in kernel mode.
	kernel := filepath.Join(root, "k", "kernel.p")
	c.open(kernel)
	c.diagnostics(kernel, func(d []Diagnostic) bool {
		return slices.ContainsFunc(d, func(d Diagnostic) bool { return strings.Contains(d.Message, "cannot use") })
	})
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
	c.diagnostics(loose, func(d []Diagnostic) bool {
		return slices.ContainsFunc(d, func(d Diagnostic) bool { return strings.Contains(d.Message, "no pluto.mod") })
	})
	c.diagnostics(filepath.Join(root, "loose", "lq.p"), func(d []Diagnostic) bool {
		return slices.ContainsFunc(d, func(d Diagnostic) bool { return strings.Contains(d.Message, "not found") })
	})

	// Switching the configured mode to kernel rechecks.
	c.notify("workspace/didChangeConfiguration", DidChangeConfigurationParams{Settings: []byte(`{"plutonium":{"target":"x86_64-none","mode":"kernel"}}`)})
	c.diagnostics(never, func(d []Diagnostic) bool {
		return len(d) == 1 && strings.Contains(d[0].Message, "kernel build")
	})
	c.notify("workspace/didChangeConfiguration", DidChangeConfigurationParams{Settings: []byte(`{"plutonium":{"mode":"bogus"}}`)})
	c.shutdown()
}

func TestModesFor(t *testing.T) {
	for _, tc := range []struct {
		name string
		s    Settings
		want string // first mode, or "error"
	}{
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

func TestHover(t *testing.T) {
	c, root := openDemo(t)
	main := filepath.Join(root, "main.p")
	shapes := filepath.Join(root, "shapes", "shapes.p")
	hover := func(path, marker string, n int) string {
		t.Helper()
		var h *Hover
		c.result("textDocument/hover", posParams(path, at(t, path, marker, n)), &h)
		if h == nil {
			return ""
		}
		return h.Contents.Value
	}
	for _, tc := range []struct {
		name, path, marker string
		n                  int
		want               []string
	}{
		{"owner variable", main, "var b", 4, []string{"var b shapes.Buffer", "owner type", "moves on assignment, freed automatically"}},
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
	main := filepath.Join(root, "main.p")
	shapes := filepath.Join(root, "shapes", "shapes.p")
	def := func(marker string, n int) []Location {
		t.Helper()
		var locs []Location
		c.result("textDocument/definition", posParams(main, at(t, main, marker, n)), &locs)
		return locs
	}
	// Into another package of the module.
	locs := def("shapes.Area", 7)
	if len(locs) != 1 || pathFromURI(locs[0].URI) != shapes || locs[0].Range.Start != at(t, shapes, "Area(w", 0) {
		t.Errorf("definition of Area = %+v", locs)
	}
	// A local variable.
	locs = def("_ = c", 4)
	if len(locs) != 1 || locs[0].Range.Start != at(t, main, "c :=", 0) {
		t.Errorf("definition of c = %+v", locs)
	}
	// Into the standard library.
	locs = def("HasPrefix", 2)
	lib := filepath.Join(c.stdlib, "pluto", "strings")
	if len(locs) != 1 || filepath.Dir(pathFromURI(locs[0].URI)) != lib {
		t.Fatalf("definition of HasPrefix = %+v", locs)
	}
	// An import path and a package name go to the package.
	locs = def("\"pluto/strings\"", 3)
	if len(locs) != 1 || filepath.Dir(pathFromURI(locs[0].URI)) != lib {
		t.Errorf("definition of the import = %+v", locs)
	}
	locs = def("shapes.Buffer", 1)
	if len(locs) != 1 || pathFromURI(locs[0].URI) != shapes {
		t.Errorf("definition of shapes = %+v", locs)
	}
	// Nothing at a keyword or a predeclared name.
	if locs := def("func main", 0); len(locs) != 0 {
		t.Errorf("definition of func = %+v", locs)
	}

	// From inside the standard library file, references stay there.
	stdFile := pathFromURI(def("HasPrefix", 2)[0].URI)
	var refs []Location
	c.result("textDocument/references", ReferenceParams{
		TextDocumentPositionParams: posParams(stdFile, at(t, stdFile, "func HasPrefix", 5)),
		Context:                    ReferenceContext{IncludeDeclaration: true},
	}, &refs)
	if len(refs) < 2 {
		t.Errorf("references to HasPrefix from the library = %+v", refs)
	}
}

func TestDefinitionExtractedStdlib(t *testing.T) {
	root := writeFiles(t, demo)
	cache := t.TempDir()
	c := startServer(t, Options{Stdlib: os.DirFS(libDir(t)), CacheDir: cache})
	c.initialize(root, nil)
	main := filepath.Join(root, "main.p")
	var locs []Location
	c.result("textDocument/definition", posParams(main, at(t, main, "HasPrefix", 2)), &locs)
	if len(locs) != 1 || !hasPathPrefix(pathFromURI(locs[0].URI), cache) {
		t.Fatalf("definition of HasPrefix = %+v, want a file under %s", locs, cache)
	}
	st, err := os.Stat(pathFromURI(locs[0].URI))
	if err != nil || st.Mode().Perm()&0o222 != 0 {
		t.Errorf("extracted file: %v, mode %v; want read-only", err, st.Mode())
	}
	// A second extraction reuses the first.
	dir, err := extractStdlib(os.DirFS(libDir(t)), cache)
	if err != nil || !hasPathPrefix(pathFromURI(locs[0].URI), dir) {
		t.Errorf("second extraction: %s, %v", dir, err)
	}
	if entries, _ := os.ReadDir(cache); len(entries) != 1 {
		t.Errorf("cache holds %d entries, want 1", len(entries))
	}

	// A new library removes copies a week old, and keeps newer ones.
	old := filepath.Join(cache, "stdlib-old")
	recent := filepath.Join(cache, "stdlib-recent")
	for _, d := range []string{old, recent} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	weekAgo := time.Now().Add(-8 * 24 * time.Hour)
	if err := os.Chtimes(old, weekAgo, weekAgo); err != nil {
		t.Fatal(err)
	}
	if _, err := extractStdlib(fstest.MapFS{"pluto/x/x.p": {Data: []byte("package x\n")}}, cache); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(old); err == nil {
		t.Error("the stale copy is still there")
	}
	if _, err := os.Stat(recent); err != nil {
		t.Error("the recent copy was removed")
	}
	if _, err := os.Stat(dir); err != nil {
		t.Error("the current copy was removed")
	}
}

func TestReferences(t *testing.T) {
	c, root := openDemo(t)
	main := filepath.Join(root, "main.p")
	shapes := filepath.Join(root, "shapes", "shapes.p")
	refs := func(path, marker string, n int, decl bool) []Location {
		t.Helper()
		var locs []Location
		c.result("textDocument/references", ReferenceParams{
			TextDocumentPositionParams: posParams(path, at(t, path, marker, n)),
			Context:                    ReferenceContext{IncludeDeclaration: decl},
		}, &locs)
		return locs
	}
	// Size: declared in shapes, used in shapes and main.
	locs := refs(shapes, "Size int", 0, true)
	if len(locs) != 3 {
		t.Errorf("references to Size = %+v", locs)
	}
	locs = refs(main, "b.Size", 2, false)
	if len(locs) != 2 {
		t.Errorf("references to Size without the declaration = %+v", locs)
	}
	var files []string
	for _, l := range locs {
		files = append(files, filepath.Base(pathFromURI(l.URI)))
	}
	if !slices.Equal(files, []string{"main.p", "shapes.p"}) {
		t.Errorf("files = %v", files)
	}
	if locs := refs(main, "func main", 0, true); len(locs) != 0 {
		t.Errorf("references to a keyword = %+v", locs)
	}
}

func TestSymbols(t *testing.T) {
	c, root := openDemo(t)
	shapes := filepath.Join(root, "shapes", "shapes.p")
	var syms []DocumentSymbol
	c.result("textDocument/documentSymbol", DocumentSymbolParams{TextDocument: TextDocumentIdentifier{URI: uriFromPath(shapes)}}, &syms)
	var names []string
	for _, s := range syms {
		names = append(names, s.Name)
	}
	if !slices.Equal(names, []string{"Color", "Buffer", "(*Buffer).Drop", "(*Buffer).Len", "Area"}) {
		t.Errorf("symbols = %v", names)
	}
	if len(syms) == 5 {
		if syms[0].Kind != SymbolEnum || len(syms[0].Children) != 3 || syms[0].Children[1].Name != "Green" {
			t.Errorf("Color = %+v", syms[0])
		}
		if syms[1].Kind != SymbolStruct || len(syms[1].Children) != 2 || !strings.HasPrefix(syms[1].Detail, "owner") {
			t.Errorf("Buffer = %+v", syms[1])
		}
		if syms[4].Kind != SymbolFunction || syms[4].Detail != "func(w int, h int) int" {
			t.Errorf("Area = %+v", syms[4])
		}
	}

	var infos []SymbolInformation
	c.result("workspace/symbol", WorkspaceSymbolParams{Query: "gren"}, &infos)
	if len(infos) != 1 || infos[0].Name != "Green" || infos[0].ContainerName != "example.com/demo/shapes.Color" {
		t.Errorf("workspace symbols for gren = %+v", infos)
	}
	c.result("workspace/symbol", WorkspaceSymbolParams{Query: ""}, &infos)
	if len(infos) < 8 {
		t.Errorf("all workspace symbols = %+v", infos)
	}
}

func TestFormatting(t *testing.T) {
	root := writeFiles(t, map[string]string{
		"a.p":   "package a\n\nfunc F()   int {\nreturn 1\n}\n\nvar X = 1\n",
		"b.p":   "package a\n\nvar Y = 2\n",
		"bad.p": "package a\n\nfunc {\n",
	})
	c := startServer(t, Options{})
	c.initialize(root, nil)
	format := func(name string) []TextEdit {
		var edits []TextEdit
		c.result("textDocument/formatting", DocumentFormattingParams{TextDocument: TextDocumentIdentifier{URI: uriFromPath(filepath.Join(root, name))}}, &edits)
		return edits
	}
	edits := format("a.p")
	want := []TextEdit{{Range: Range{Start: Position{2, 0}, End: Position{4, 0}}, NewText: "func F() int {\n\treturn 1\n"}}
	if !slices.Equal(edits, want) {
		t.Errorf("edits = %+v, want %+v", edits, want)
	}
	if edits := format("b.p"); len(edits) != 0 {
		t.Errorf("formatted file: edits %+v", edits)
	}
	if edits := format("bad.p"); len(edits) != 0 {
		t.Errorf("file with syntax errors: edits %+v", edits)
	}
	if m := c.call("textDocument/formatting", DocumentFormattingParams{TextDocument: TextDocumentIdentifier{URI: "untitled:1"}}); m.Error == nil {
		t.Error("formatting an untitled document succeeded")
	}
}

func TestReplaceEdits(t *testing.T) {
	for _, tc := range []struct{ src, out, newText string }{
		{"a\nb\nc\n", "a\nB\nc\n", "B\n"},
		{"a\nb\n", "a\nb\nc\n", "c\n"},
		{"x", "y", "y"},
		{"a\nb\nc\n", "a\nc\n", ""},
	} {
		edits := replaceEdits([]byte(tc.src), []byte(tc.out))
		if len(edits) != 1 || edits[0].NewText != tc.newText {
			t.Errorf("replaceEdits(%q, %q) = %+v", tc.src, tc.out, edits)
			continue
		}
		// Applying the edit gives out.
		txt := newText([]byte(tc.src))
		start, end := txt.offset(edits[0].Range.Start), txt.offset(edits[0].Range.End)
		if got := tc.src[:start] + edits[0].NewText + tc.src[end:]; got != tc.out {
			t.Errorf("applying %+v to %q gives %q, want %q", edits[0], tc.src, got, tc.out)
		}
	}
}

// edit opens a file with new contents in place of the file on disk.
func (c *client) edit(path, src string) {
	c.t.Helper()
	c.notify("textDocument/didOpen", DidOpenTextDocumentParams{TextDocument: TextDocumentItem{
		URI: uriFromPath(path), LanguageID: "plutonium", Version: 1, Text: src,
	}})
}

func TestCompletion(t *testing.T) {
	root := writeFiles(t, demo)
	c := startServer(t, Options{})
	c.initialize(root, nil)
	main := filepath.Join(root, "main.p")
	complete := func(src, marker string) []string {
		t.Helper()
		c.edit(main, src)
		var list CompletionList
		pos := newText([]byte(src)).position(strings.Index(src, marker) + len(marker))
		c.result("textDocument/completion", posParams(main, pos), &list)
		var labels []string
		for _, it := range list.Items {
			labels = append(labels, it.Label)
		}
		return labels
	}
	body := func(stmt string) string {
		return strings.Replace(demo["main.p"], "\t_ = c\n", "\t_ = c\n\t"+stmt+"\n", 1)
	}
	for _, tc := range []struct {
		name, src, marker string
		want, notWant     []string
	}{
		{"package members", body("strings.Has"), "strings.Has", []string{"HasPrefix", "HasSuffix"}, []string{"Index"}},
		{"package after dot", body("shapes."), "shapes.", []string{"Area", "Buffer", "Color"}, []string{"data"}},
		{"fields and methods", body("b."), "b.", []string{"Size", "Len", "Drop"}, []string{"data"}},
		{"enum members", body("_ = shapes.Color."), "shapes.Color.", []string{"Red", "Green", "Blue"}, nil},
		{"scope", body("_ = st"), "_ = st", []string{"strings"}, []string{"shapes"}},
		{"locals and keywords", body("x := b"), "x := b", []string{"b", "break", "bool"}, nil},
		{"value of a call", body("_ = strings.FieldsIter(\"a\")."), "FieldsIter(\"a\").", []string{"Next"}, nil},
		{"in a comment", body("// strings."), "// strings.", nil, []string{"HasPrefix"}},
		{"in a string", body("_ = \"strings."), "\"strings.", nil, []string{"HasPrefix"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := complete(tc.src, tc.marker)
			for _, w := range tc.want {
				if !slices.Contains(got, w) {
					t.Errorf("completions %v lack %s", got, w)
				}
			}
			for _, w := range tc.notWant {
				if slices.Contains(got, w) {
					t.Errorf("completions %v include %s", got, w)
				}
			}
		})
	}
}

func TestSignatureHelp(t *testing.T) {
	root := writeFiles(t, demo)
	c := startServer(t, Options{})
	c.initialize(root, nil)
	main := filepath.Join(root, "main.p")
	help := func(src, marker string) *SignatureHelp {
		t.Helper()
		c.edit(main, src)
		var h *SignatureHelp
		pos := newText([]byte(src)).position(strings.Index(src, marker) + len(marker))
		c.result("textDocument/signatureHelp", posParams(main, pos), &h)
		return h
	}
	h := help(demo["main.p"], "shapes.Area(2, ")
	if h == nil || len(h.Signatures) != 1 || h.ActiveParameter != 1 {
		t.Fatalf("signature help = %+v", h)
	}
	sig := h.Signatures[0]
	if sig.Label != "Area(w int, h int) int" || sig.Documentation == nil || !strings.Contains(sig.Documentation.Value, "computes") {
		t.Errorf("signature = %+v", sig)
	}
	if p := sig.Parameters[1].Label; sig.Label[p[0]:p[1]] != "h int" {
		t.Errorf("second parameter label %v", p)
	}
	if h := help(demo["main.p"], "func main("); h != nil {
		t.Errorf("signature help outside a call = %+v", h)
	}
}

func TestSignatureInfo(t *testing.T) {
	for _, tc := range []struct{ src, want string }{
		{"a, b", "1"}, {"f(a, b), ", "1"}, {"\"a,b\", 'c', `d,`, ", "3"}, {"x // a, b\n", "0"},
	} {
		if got := argIndex([]byte(tc.src), 0, len(tc.src)); string(rune('0'+got)) != tc.want {
			t.Errorf("argIndex(%q) = %d, want %s", tc.src, got, tc.want)
		}
	}
}

func TestRename(t *testing.T) {
	c, root := openDemo(t)
	main := filepath.Join(root, "main.p")
	shapes := filepath.Join(root, "shapes", "shapes.p")
	var prep *PrepareRenameResult
	c.result("textDocument/prepareRename", posParams(main, at(t, main, "b.Size", 3)), &prep)
	if prep == nil || prep.Placeholder != "Size" {
		t.Errorf("prepareRename = %+v", prep)
	}
	var edit WorkspaceEdit
	c.result("textDocument/rename", RenameParams{TextDocumentPositionParams: posParams(main, at(t, main, "b.Size", 2)), NewName: "Bytes"}, &edit)
	if len(edit.Changes[uriFromPath(main)]) != 1 || len(edit.Changes[uriFromPath(shapes)]) != 2 {
		t.Errorf("rename edits = %+v", edit)
	}
	for _, tc := range []struct {
		name, path, marker string
		n                  int
		newName            string
	}{
		{"stdlib", main, "HasPrefix", 0, "X"},
		{"predeclared", main, "_ = c", 0, "X"},
		{"package", main, "shapes.Area", 0, "X"},
		{"keyword", main, "func main", 0, "X"},
		{"bad name", main, "b.Size", 2, "1x"},
		{"conflict", main, "c :=", 0, "b"},
		{"method conflict", shapes, "Len()", 0, "Drop"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := c.call("textDocument/rename", RenameParams{TextDocumentPositionParams: posParams(tc.path, at(t, tc.path, tc.marker, tc.n)), NewName: tc.newName})
			if m.Error == nil {
				t.Errorf("rename succeeded: %s", m.Result)
			}
		})
	}
	m := c.call("textDocument/prepareRename", posParams(main, at(t, main, "HasPrefix", 0)))
	if m.Error == nil {
		t.Error("prepareRename of a library function succeeded")
	}
}
