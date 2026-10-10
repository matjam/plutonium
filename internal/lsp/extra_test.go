package lsp

import (
	"bufio"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/matjam/plutonium/internal/diag"
	"github.com/matjam/plutonium/internal/types2"
)

func TestErrorExplanations(t *testing.T) {
	c, root := openDemo(t)
	main := filepath.Join(root, "main.p")
	broken := strings.Replace(demo["main.p"], "_ = c", "_ = undefinedName", 1)
	c.edit(main, broken)
	diags := c.diagnostics(main, func(d []Diagnostic) bool {
		return slices.ContainsFunc(d, func(d Diagnostic) bool { return strings.Contains(d.Message, "undefinedName") })
	})
	var undef Diagnostic
	for _, d := range diags {
		if strings.Contains(d.Message, "undefinedName") {
			undef = d
		}
	}
	if !strings.HasPrefix(undef.Code, "E") || len(undef.Code) != 5 || strings.Contains(undef.Message, "\n") {
		t.Errorf("diagnostic code %q, message %q", undef.Code, undef.Message)
	}
	if len(undef.RelatedInformation) == 0 || undef.RelatedInformation[0].Location.Range != undef.Range {
		t.Errorf("related information = %+v", undef.RelatedInformation)
	}

	// Hovering over the error shows its explanation first.
	var h *Hover
	c.result("textDocument/hover", posParams(main, undef.Range.Start), &h)
	if h == nil || !strings.HasPrefix(h.Contents.Value, "**"+undef.Code+"**") || !strings.Contains(h.Contents.Value, "pluto explain "+undef.Code) {
		t.Errorf("hover over the error = %+v", h)
	}
	// Over an error and a name, both.
	unused := slices.IndexFunc(diags, func(d Diagnostic) bool { return strings.Contains(d.Message, "declared and not used") })
	if unused >= 0 {
		c.result("textDocument/hover", posParams(main, diags[unused].Range.Start), &h)
		if h == nil || !strings.Contains(h.Contents.Value, "---") || !strings.Contains(h.Contents.Value, "var c shapes.Color") {
			t.Errorf("hover over the unused variable = %+v", h)
		}
	}

	// A code action offers to explain it, once per code.
	var actions []CodeAction
	c.result("textDocument/codeAction", CodeActionParams{
		TextDocument: TextDocumentIdentifier{URI: uriFromPath(main)},
		Range:        undef.Range,
		Context:      CodeActionContext{Diagnostics: []Diagnostic{undef, undef, {Source: "other", Code: "E0001"}}},
	}, &actions)
	if len(actions) != 1 || actions[0].Command == nil || actions[0].Command.Command != explainCommand ||
		actions[0].Command.Arguments[0] != undef.Code {
		t.Errorf("code actions = %+v", actions)
	}

	// Closing the file (whose copy on disk has no errors) clears them.
	c.notify("textDocument/didClose", DidCloseTextDocumentParams{TextDocument: TextDocumentIdentifier{URI: uriFromPath(main)}})
	c.diagnostics(main, func(d []Diagnostic) bool { return len(d) == 0 })
}

func TestExplainMarkdown(t *testing.T) {
	d := &diag.Diagnostic{Code: diag.CheckerCode(1), Msg: "bad *T in a_b\n\tdetail line",
		Text: "first para\nsecond para", Fix: &diag.Fix{Intro: "To fix", Code: "x := 1"}}
	md := explainMarkdown(d)
	for _, want := range []string{`bad \*T in a\_b`, "detail line", "first para\n\nsecond para", "To fix:\n\n```plutonium\nx := 1\n```"} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown %q lacks %q", md, want)
		}
	}
	if got := explainMarkdown(&diag.Diagnostic{Msg: "plain"}); got != "plain\n" {
		t.Errorf("without a code: %q", got)
	}
	if got := explainPlain(d); got != "first para second para" && !strings.HasPrefix(got, "first para second para ") {
		t.Errorf("plain = %q", got)
	}
}

func TestWatchedFilesAndFolders(t *testing.T) {
	c, root := openDemo(t)
	main := filepath.Join(root, "main.p")
	shapes := filepath.Join(root, "shapes", "shapes.p")
	// shapes.p changes on disk: main.p is rechecked.
	if err := os.WriteFile(shapes, []byte(strings.Replace(demo["shapes/shapes.p"], "func Area", "func Area3", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	c.notify("workspace/didChangeWatchedFiles", DidChangeWatchedFilesParams{Changes: []FileEvent{{URI: uriFromPath(shapes), Type: 2}}})
	c.diagnostics(main, func(d []Diagnostic) bool { return len(d) > 0 })
	// A new pluto.mod drops everything; a deleted file clears its diagnostics.
	extra := filepath.Join(root, "extra.p")
	c.notify("workspace/didChangeWatchedFiles", DidChangeWatchedFilesParams{Changes: []FileEvent{
		{URI: uriFromPath(filepath.Join(root, "pluto.mod")), Type: 2},
		{URI: uriFromPath(extra), Type: 3},
		{URI: uriFromPath(main), Type: 2}, // open: the editor's copy counts
	}})
	if err := os.WriteFile(shapes, []byte(demo["shapes/shapes.p"]), 0o644); err != nil {
		t.Fatal(err)
	}
	c.notify("workspace/didChangeWatchedFiles", DidChangeWatchedFilesParams{Changes: []FileEvent{{URI: uriFromPath(shapes), Type: 2}}})
	c.diagnostics(main, func(d []Diagnostic) bool { return len(d) == 0 })

	// Removing the workspace folder leaves nothing to search.
	c.notify("workspace/didChangeWorkspaceFolders", DidChangeWorkspaceFoldersParams{Event: WorkspaceFoldersChangeEvent{
		Removed: []WorkspaceFolder{{URI: uriFromPath(root)}},
	}})
	var infos []SymbolInformation
	c.result("workspace/symbol", WorkspaceSymbolParams{Query: "Area"}, &infos)
	if len(infos) != 0 {
		t.Errorf("symbols without a workspace folder = %+v", infos)
	}
	c.notify("workspace/didChangeWorkspaceFolders", DidChangeWorkspaceFoldersParams{Event: WorkspaceFoldersChangeEvent{
		Added: []WorkspaceFolder{{URI: uriFromPath(root)}},
	}})
	c.result("workspace/symbol", WorkspaceSymbolParams{Query: "Area"}, &infos)
	if len(infos) != 1 {
		t.Errorf("symbols with the folder back = %+v", infos)
	}
	// Notifications with bad parameters are ignored.
	c.notify("textDocument/didOpen", "not an object")
	c.notify("workspace/didChangeConfiguration", DidChangeConfigurationParams{Settings: []byte(`{"other":1}`)})
	if m := c.call("textDocument/hover", "not an object"); m.Error == nil || m.Error.Code != codeInvalidParams {
		t.Errorf("hover with bad parameters: %+v", m.Error)
	}
	c.shutdown()
}

func TestReadMessage(t *testing.T) {
	for _, tc := range []struct{ name, in, err string }{
		{"no length", "Content-Type: x\r\n\r\n{}", "without Content-Length"},
		{"bad length", "Content-Length: x\r\n\r\n", "bad Content-Length"},
		{"bad header", "nonsense\r\n\r\n", "malformed header"},
		{"short body", "Content-Length: 10\r\n\r\n{}", "reading body"},
		{"eof in header", "Content-Length: 2\r\n", "reading header"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := readMessage(bufio.NewReader(strings.NewReader(tc.in)))
			if err == nil || !strings.Contains(err.Error(), tc.err) {
				t.Errorf("error %v, want %q", err, tc.err)
			}
		})
	}
	m, err := readMessage(bufio.NewReader(strings.NewReader("Content-Length: 3\r\n\r\n{x}")))
	if err != nil || m.Error == nil || m.Error.Code != codeParseError {
		t.Errorf("invalid JSON: %+v, %v", m, err)
	}
	if s := (&rpcError{Code: 1, Message: "m"}).Error(); s != "m (code 1)" {
		t.Errorf("rpcError = %q", s)
	}
}

func TestMalformedMessages(t *testing.T) {
	c := startServer(t, Options{})
	c.initialize(t.TempDir(), nil)
	// A message that is not JSON, and a response to nothing, are ignored.
	if _, err := c.w.w.Write([]byte("Content-Length: 3\r\n\r\n{x}")); err != nil {
		t.Fatal(err)
	}
	if err := c.w.send(map[string]any{"jsonrpc": "2.0", "id": 99, "result": nil}); err != nil {
		t.Fatal(err)
	}
	c.shutdown()
}

func TestText(t *testing.T) {
	src := []byte("a\né\U0001F600x\n\xffz")
	txt := newText(src)
	for _, tc := range []struct {
		off int
		pos Position
	}{
		{0, Position{0, 0}}, {2, Position{1, 0}}, {4, Position{1, 1}}, {8, Position{1, 3}},
		{10, Position{2, 0}}, {11, Position{2, 1}}, {12, Position{2, 2}},
	} {
		if got := txt.position(tc.off); got != tc.pos {
			t.Errorf("position(%d) = %v, want %v", tc.off, got, tc.pos)
		}
		if got := txt.offset(tc.pos); got != tc.off {
			t.Errorf("offset(%v) = %d, want %d", tc.pos, got, tc.off)
		}
	}
	if got := txt.offset(Position{-1, 0}); got != 0 {
		t.Errorf("offset before the text = %d", got)
	}
	if got := txt.offset(Position{9, 0}); got != len(src) {
		t.Errorf("offset after the text = %d", got)
	}
	if got := txt.offset(Position{0, 99}); got != 1 {
		t.Errorf("offset past the end of a line = %d", got)
	}
	if pathFromURI("untitled:x") != "" || pathFromURI("%zz") != "" {
		t.Error("non-file URIs have paths")
	}
	if p := pathFromURI("file:///a%20b/c.p"); p != "/a b/c.p" {
		t.Errorf("pathFromURI = %q", p)
	}
	if hasPathPrefix("/a", "") || !hasPathPrefix("/a/b", "/a") || hasPathPrefix("/ab", "/a") {
		t.Error("hasPathPrefix")
	}
}

func TestTypeKinds(t *testing.T) {
	root := writeFiles(t, map[string]string{
		"k.p": `package k

type Node struct {
	next owned[Node]
}

type Arr [2]Node

type Ref struct {
	p *int
}

type Counted struct {
	s shared[int]
}

type Plain struct {
	a int
	b [3]bool
}

type Iface interface{ M() }

type Bits packed uint8 {
	a bits[uint8, 4]
	b bits[uint8, 4]
}

type State union {
	Off
	On int
}

type Text union {
	Empty
	Some string
}

var (
	o  owned[int]
	sh shared[int]
	sy synced[int]
	v  volatile[uint32]
	s  string
	sl []int
	ch chan int
	f  func()
	n  Node
	a  Arr
	r  Ref
	c  Counted
	pl Plain
	i  Iface
	bt Bits
	us State
	ut Text
)
`,
	})
	modes, _ := modesFor(Settings{})
	w := newWorkspace(libDir(t), modes)
	_, p, err := w.fileOf(filepath.Join(root, "k.p"))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range p.errors {
		t.Logf("error: %s", e.diag.Msg)
	}
	for name, want := range map[string]string{
		"o": kindOwner, "sh": kindShared, "sy": kindShared, "v": kindValue, "s": kindView, "sl": kindView,
		"ch": kindShared, "f": "", "n": kindOwner, "a": kindOwner, "r": kindView, "c": kindValue,
		"pl": kindValue, "i": "", "bt": kindValue, "us": kindValue, "ut": kindView,
	} {
		obj := p.types.Scope().Lookup(name)
		if obj == nil {
			t.Errorf("%s not declared", name)
			continue
		}
		if got := typeKind(obj.Type(), 0); got != want {
			t.Errorf("kind of %s (%s) = %q, want %q", name, obj.Type(), got, want)
		}
	}
	if typeKind(types2.Typ[types2.Invalid], 0) != "" || kindLine(types2.Typ[types2.Invalid]) != "" {
		t.Error("invalid type has a kind")
	}
}

func TestCompletionDetails(t *testing.T) {
	root := writeFiles(t, map[string]string{
		"pluto.mod": "module example.com/c\n",
		"c.p": `package c

type Inner struct{ Deep int }

type Outer struct {
	Inner
	*Other
	Name string
}

type Other struct{ Far int }

type R ring[int, 4]

func (o *Outer) M() {}

// The parser drops declarations after a statement being typed, so
// these come first.

type Iface interface{ Do() }

const K = 1

var V = 2

func use(o Outer, p owned[Outer], r R, x Iface) {
	_ = o
}
`,
	})
	c := startServer(t, Options{})
	c.initialize(root, nil)
	path := filepath.Join(root, "c.p")
	complete := func(stmt string) map[string]CompletionItem {
		t.Helper()
		src := strings.Replace(mustRead(t, path), "\t_ = o\n", "\t"+stmt+"\n", 1)
		c.edit(path, src)
		var list CompletionList
		pos := newText([]byte(src)).position(strings.Index(src, stmt) + len(stmt))
		c.result("textDocument/completion", posParams(path, pos), &list)
		items := map[string]CompletionItem{}
		for _, it := range list.Items {
			items[it.Label] = it
		}
		return items
	}
	items := complete("o.")
	for _, name := range []string{"Deep", "Far", "Name", "Inner", "Other", "M"} {
		if _, ok := items[name]; !ok {
			t.Errorf("o. lacks %s: %v", name, items)
		}
	}
	if items["M"].Kind != CompletionMethod || items["Name"].Kind != CompletionField {
		t.Errorf("kinds: %+v %+v", items["M"], items["Name"])
	}
	if _, ok := complete("p.")["Name"]; !ok {
		t.Error("owned[Outer] does not complete like *Outer")
	}
	if _, ok := complete("r.")["Push"]; !ok {
		t.Error("ring methods missing")
	}
	if _, ok := complete("x.")["Do"]; !ok {
		t.Error("interface methods missing")
	}
	items = complete("_ = ")
	for name, kind := range map[string]int{"K": CompletionConstant, "V": CompletionVariable, "use": CompletionFunction,
		"Outer": CompletionStruct, "Iface": CompletionInterface, "len": CompletionFunction, "R": CompletionClass} {
		if items[name].Kind != kind {
			t.Errorf("%s: kind %d, want %d", name, items[name].Kind, kind)
		}
	}
}

func mustRead(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestStdlibTree(t *testing.T) {
	// A file inside a standard library tree is checked against that tree,
	// and is a package of the library.
	modes, _ := modesFor(Settings{})
	w := newWorkspace(t.TempDir(), modes)
	path := filepath.Join(libDir(t), "pluto", "strings", "strings.p")
	f, p, err := w.fileOf(path)
	if err != nil || f == nil {
		t.Fatalf("fileOf: %v", err)
	}
	if p.path != "pluto/strings" {
		t.Errorf("import path %q", p.path)
	}
	for _, e := range p.errors {
		t.Errorf("error in the library: %s", e.diag.Msg)
	}
	// Workspace packages skip testdata and hidden directories.
	root := writeFiles(t, map[string]string{
		"a/a.p": "package a\n", "testdata/t.p": "package t\n", ".hidden/h.p": "package h\n", "_x/x.p": "package x\n",
	})
	w.roots = []string{root}
	dirs := w.packageDirs()
	if len(dirs) != 1 || filepath.Base(dirs[0]) != "a" {
		t.Errorf("package dirs = %v", dirs)
	}
}
