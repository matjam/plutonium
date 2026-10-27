// Ported from internal/lsp/extra_test.go at the go-cutover tag.

package lsp

import (
	"pluto/os"
	"pluto/path/filepath"
	"pluto/strings"
	"pluto/testing"

	"compiler/compat"
	"compiler/internal/diag"
	"compiler/internal/types2"
	"compiler/pu"
)

func TestErrorExplanations(t *testing.T) {
	c, root := openDemo(t)
	defer removeAll(root)
	main := filepath.Join(root, "main.p")
	broken := strings.Replace(demo["main.p"], "_ = c", "_ = undefinedName", 1)
	c.edit(main, broken)
	diags := c.diagnostics(main, func(d []Diagnostic) bool { return hasMessage(d, "undefinedName") })
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
		t.Errorf("related information: %d entries", len(undef.RelatedInformation))
	}

	// Hovering over the error shows its explanation first.
	h := c.hover(main, undef.Range.Start)
	if h == nil || !strings.HasPrefix(h.Contents.Value, "**"+undef.Code+"**") || !strings.Contains(h.Contents.Value, "pluto explain "+undef.Code) {
		t.Errorf("hover over the error is not its explanation: %v", h != nil)
	}
	// Over an error and a name, both.
	for _, d := range diags {
		if strings.Contains(d.Message, "declared and not used") {
			h = c.hover(main, d.Range.Start)
			if h == nil || !strings.Contains(h.Contents.Value, "---") || !strings.Contains(h.Contents.Value, "var c shapes.Color") {
				t.Errorf("hover over the unused variable: %v", h != nil)
			}
			break
		}
	}

	// A code action offers to explain it, once per code.
	other := Diagnostic{Source: "other", Code: "E0001"}
	actions := c.codeActions(main, undef.Range, []Diagnostic{undef, undef, other})
	if len(actions) != 1 || actions[0].Command == nil || actions[0].Command.Command != explainCommand ||
		actions[0].Command.Arguments[0] != undef.Code {
		t.Errorf("code actions: %d", len(actions))
	}

	// Closing the file (whose copy on disk has no errors) clears them.
	c.docNotify("textDocument/didClose", main)
	c.diagnostics(main, func(d []Diagnostic) bool { return len(d) == 0 })
}

func TestExplainMarkdown(t *testing.T) {
	fix := pu.New(diag.Fix{Intro: "To fix", Code: "x := 1"})
	d := pu.New(diag.Diagnostic{Code: diag.CheckerCode(1), Msg: "bad *T in a_b\n\tdetail line",
		Text: "first para\nsecond para", Fix: fix})
	md := explainMarkdown(d)
	for _, want := range []string{`bad \*T in a\_b`, "detail line", "first para\n\nsecond para", "To fix:\n\n```plutonium\nx := 1\n```"} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown %q lacks %q", md, want)
		}
	}
	if got := explainMarkdown(pu.New(diag.Diagnostic{Msg: "plain"})); got != "plain\n" {
		t.Errorf("without a code: %q", got)
	}
	if got := explainPlain(d); got != "first para second para" && !strings.HasPrefix(got, "first para second para ") {
		t.Errorf("plain = %q", got)
	}
}

// watchedParams returns the parameters of workspace/didChangeWatchedFiles.
func watchedParams(events []FileEvent) []byte {
	w := newJSONWriter()
	w.beginObject()
	w.key("changes")
	w.beginArray()
	for _, e := range events {
		w.beginObject()
		w.key("uri")
		w.str(e.URI)
		w.key("type")
		w.integer(e.Type)
		w.endObject()
	}
	w.endArray()
	w.endObject()
	return w.bytes()
}

// foldersParams returns the parameters of workspace/didChangeWorkspaceFolders.
func foldersParams(added, removed []string) []byte {
	w := newJSONWriter()
	w.beginObject()
	w.key("event")
	w.beginObject()
	w.key("added")
	w.beginArray()
	for _, p := range added {
		writeFolder(w, p, "")
	}
	w.endArray()
	w.key("removed")
	w.beginArray()
	for _, p := range removed {
		writeFolder(w, p, "")
	}
	w.endArray()
	w.endObject()
	w.endObject()
	return w.bytes()
}

func TestWatchedFilesAndFolders(t *testing.T) {
	c, root := openDemo(t)
	defer removeAll(root)
	main := filepath.Join(root, "main.p")
	shapes := filepath.Join(root, "shapes", "shapes.p")
	// shapes.p changes on disk: main.p is rechecked.
	os.WriteFile(shapes, compat.Bytes(strings.Replace(demo["shapes/shapes.p"], "func Area", "func Area3", 1)), 0o644) or {
		t.Fatal(err)
		return
	}
	c.notify("workspace/didChangeWatchedFiles", watchedParams([]FileEvent{{URI: uriFromPath(shapes), Type: 2}}))
	c.diagnostics(main, func(d []Diagnostic) bool { return len(d) > 0 })
	// A new pluto.mod drops everything; a deleted file clears its diagnostics.
	extra := filepath.Join(root, "extra.p")
	c.notify("workspace/didChangeWatchedFiles", watchedParams([]FileEvent{
		{URI: uriFromPath(filepath.Join(root, "pluto.mod")), Type: 2},
		{URI: uriFromPath(extra), Type: 3},
		{URI: uriFromPath(main), Type: 2}, // open: the editor's copy counts
	}))
	os.WriteFile(shapes, compat.Bytes(demo["shapes/shapes.p"]), 0o644) or {
		t.Fatal(err)
		return
	}
	c.notify("workspace/didChangeWatchedFiles", watchedParams([]FileEvent{{URI: uriFromPath(shapes), Type: 2}}))
	c.diagnostics(main, func(d []Diagnostic) bool { return len(d) == 0 })

	// Removing the workspace folder leaves nothing to search.
	c.notify("workspace/didChangeWorkspaceFolders", foldersParams(nil, []string{root}))
	infos := c.workspaceSymbols("Area")
	if len(infos) != 0 {
		t.Errorf("symbols without a workspace folder: %d", len(infos))
	}
	c.notify("workspace/didChangeWorkspaceFolders", foldersParams([]string{root}, nil))
	infos = c.workspaceSymbols("Area")
	if len(infos) != 1 {
		t.Errorf("symbols with the folder back: %d", len(infos))
	}
	// Notifications with bad parameters are ignored.
	c.notify("textDocument/didOpen", jsonText("not an object"))
	c.notify("workspace/didChangeConfiguration", compat.Bytes(`{"settings":{"other":1}}`))
	if _, rerr := c.call("textDocument/hover", jsonText("not an object")); rerr == nil || rerr.Code != codeInvalidParams {
		t.Errorf("hover with bad parameters: %v", rerr)
	}
	c.shutdown()
}

// A readCase is a message TestReadMessage reads: the input, and what its
// error says.
type readCase struct{ name, in, err string }

func TestReadMessage(t *testing.T) {
	for _, tc := range []readCase{
		{"no length", "Content-Type: x\r\n\r\n{}", "without Content-Length"},
		{"bad length", "Content-Length: x\r\n\r\n", "bad Content-Length"},
		{"bad header", "nonsense\r\n\r\n", "malformed header"},
		{"short body", "Content-Length: 10\r\n\r\n{}", "reading body"},
		{"eof in header", "Content-Length: 2\r\n", "reading header"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := readMessage(readerOf(tc.in))
			if err == nil || !strings.Contains(err.Error(), tc.err) {
				t.Errorf("error %v, want %q", err, tc.err)
			}
		})
	}
	m, err := readMessage(readerOf("Content-Length: 3\r\n\r\n{x}"))
	if err != nil || m == nil || m.Error == nil || m.Error.Code != codeParseError {
		t.Errorf("invalid JSON: error %v", err)
	}
	e := rpcError{Code: 1, Message: "m"}
	if s := e.Error(); s != "m (code 1)" {
		t.Errorf("rpcError = %q", s)
	}
}

func TestMalformedMessages(t *testing.T) {
	root := tempDir(t)
	defer removeAll(root)
	c := startServer(t, Options{})
	c.initialize(root, nil)
	// A message that is not JSON, and a response to nothing, are ignored.
	c.deliver("Content-Length: 3\r\n\r\n{x}")
	c.deliver(frame(compat.Bytes(`{"jsonrpc":"2.0","id":99,"result":null}`)))
	c.collect(0)
	c.shutdown()
}

// A positionCase is a byte offset and its position, for TestText.
type positionCase struct {
	off int
	pos Position
}

func TestText(t *testing.T) {
	src := compat.Bytes("a\né\U0001F600x\n?z")
	src[10] = 0xff // an invalid byte, which a string literal cannot hold
	txt := newText(src)
	for _, tc := range []positionCase{
		{0, Position{0, 0}}, {2, Position{1, 0}}, {4, Position{1, 1}}, {8, Position{1, 3}},
		{10, Position{2, 0}}, {11, Position{2, 1}}, {12, Position{2, 2}},
	} {
		if got := txt.position(tc.off); got != tc.pos {
			t.Errorf("position(%d) = %d:%d, want %d:%d", tc.off, got.Line, got.Character, tc.pos.Line, tc.pos.Character)
		}
		if got := txt.offset(tc.pos); got != tc.off {
			t.Errorf("offset(%d:%d) = %d, want %d", tc.pos.Line, tc.pos.Character, got, tc.off)
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

// A kindCase is a variable of the fixture of TestTypeKinds and its kind.
type kindCase struct {
	name string
	kind types2.OwnershipKind
}

// A hoverKindCase is a variable and its kind's hover line.
type hoverKindCase struct{ name, want string }

func TestTypeKinds(t *testing.T) {
	root := writeFiles(t, map[string]string{
		"k.p": `package k

type Node owner struct {
	next owned[Node]
}

type Arr owner [2]Node

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
	defer removeAll(root)
	modes, _ := modesFor(Settings{})
	w := newWorkspace(libDir(t), modes)
	_, p, err := w.fileOf(filepath.Join(root, "k.p"))
	if err != nil {
		t.Fatal(err)
	}
	if len(p.errors) != 0 {
		t.Fatalf("invalid kind fixture: %s", p.errors[0].diag.Msg)
	}
	for _, tc := range []kindCase{
		{"o", types2.KindOwner}, {"sh", types2.KindCounted}, {"sy", types2.KindCounted}, {"v", types2.KindValue},
		{"s", types2.KindCounted}, {"sl", types2.KindView}, {"ch", types2.KindCounted}, {"f", types2.KindView},
		{"n", types2.KindOwner}, {"a", types2.KindOwner}, {"r", types2.KindView}, {"c", types2.KindCounted},
		{"pl", types2.KindValue}, {"i", types2.KindView}, {"bt", types2.KindValue}, {"us", types2.KindValue},
		{"ut", types2.KindCounted},
	} {
		obj := p.types.Scope().Lookup(tc.name)
		if obj == nil {
			t.Errorf("%s not declared", tc.name)
			continue
		}
		if got := types2.KindOf(obj.Type()); got != tc.kind {
			t.Errorf("kind of %s (%s) = %q, want %q", tc.name, types2.TypeString(obj.Type(), nil), got.String(), tc.kind.String())
		}
	}
	if types2.KindOf(types2.Typ[types2.Invalid]) != types2.KindUnknown || kindLine(types2.Typ[types2.Invalid]) != "" {
		t.Error("invalid type has a kind")
	}
	for _, tc := range []hoverKindCase{
		{"sh", "**counted**: copies add counts and drops release counts"},
		{"r", "**view**: may contain borrowed references that must not outlive their source"},
		{"n", "**owner (moves)**: moves on assignment and is dropped when its lifetime ends"},
		{"pl", "**value**: copied on assignment"},
	} {
		if got := kindLine(p.types.Scope().Lookup(tc.name).Type()); got != tc.want {
			t.Errorf("hover %s = %q, want %q", tc.name, got, tc.want)
		}
	}
	weak := types2.NewWrapper(types2.WrapWeak, types2.NewWrapper(types2.WrapShared, types2.Typ[types2.Int], 0), 0)
	if got := kindLine(weak); got != "**counted**: copies add counts and drops release counts" {
		t.Errorf("weak hover = %q", got)
	}
	fields := compat.Clone([]*types2.Var{
		types2.NewField(0, nil, "Text", types2.Typ[types2.String], false),
		types2.NewField(0, nil, "View", types2.NewSlice(types2.Typ[types2.Byte]), false),
	})
	mixed := types2.NewStruct(fields, nil)
	if got := kindLine(mixed); got != "**view**: may contain borrowed references that must not outlive their source" {
		t.Errorf("mixed hover = %q", got)
	}
}

// A kindOfName is the completion kind of a name, for TestCompletionDetails.
type kindOfName struct {
	name string
	kind int
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
	defer removeAll(root)
	c := startServer(t, Options{})
	c.initialize(root, nil)
	path := filepath.Join(root, "c.p")
	complete := func(stmt string) map[string]CompletionItem {
		t.Helper()
		src := strings.Replace(readFile(t, path), "\t_ = o\n", "\t"+stmt+"\n", 1)
		c.edit(path, src)
		pos := newText(compat.Bytes(src)).position(strings.Index(src, stmt) + len(stmt))
		items := map[string]CompletionItem{}
		for _, it := range c.completion(path, pos).Items {
			items[it.Label] = it
		}
		return items
	}
	items := complete("o.")
	for _, name := range []string{"Deep", "Far", "Name", "Inner", "Other", "M"} {
		if _, ok := items[name]; !ok {
			t.Errorf("o. lacks %s: %d items", name, len(items))
		}
	}
	if items["M"].Kind != CompletionMethod || items["Name"].Kind != CompletionField {
		t.Errorf("kinds: %d %d", items["M"].Kind, items["Name"].Kind)
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
	for _, tc := range []kindOfName{{"K", CompletionConstant}, {"V", CompletionVariable}, {"use", CompletionFunction},
		{"Outer", CompletionStruct}, {"Iface", CompletionInterface}, {"len", CompletionFunction}, {"R", CompletionClass}} {
		if items[tc.name].Kind != tc.kind {
			t.Errorf("%s: kind %d, want %d", tc.name, items[tc.name].Kind, tc.kind)
		}
	}
}

func TestStdlibTree(t *testing.T) {
	// A file inside a standard library tree is checked against that tree,
	// and is a package of the library.
	modes, _ := modesFor(Settings{})
	empty := tempDir(t)
	defer removeAll(empty)
	w := newWorkspace(empty, modes)
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
	defer removeAll(root)
	w.roots = compat.Clone([]string{root})
	dirs := w.packageDirs()
	if len(dirs) != 1 || filepath.Base(dirs[0]) != "a" {
		t.Errorf("package dirs = %s", compat.Join(dirs, " "))
	}
}
