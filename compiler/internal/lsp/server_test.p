// Ported from internal/lsp/server_test.go at the go-cutover tag.
//
// The Go tests run the server on a goroutine and talk to it over pipes. The
// Plutonium server runs on one thread (see server.p), so the client here
// drives it from the test's own thread: each message is framed as it would
// be on the wire, read back by readMessage and handed to the server's
// handle, and what the server wrote is parsed from its output buffer. The
// server's loop runs the debounce timer; the client runs it by hand (flush)
// when it waits for diagnostics.

package lsp

import (
	"pluto/errors"
	"pluto/io"
	"pluto/os"
	"pluto/path/filepath"
	"pluto/strconv"
	"pluto/strings"
	"pluto/testing"

	"compiler/compat"
	"compiler/pu"
)

// A bytesReader reads from a byte slice.
type bytesReader struct {
	b   []byte
	pos int
}

func (r *bytesReader) Read(p []byte) (int, error) {
	if r.pos >= len(r.b) {
		return 0, io.EOF
	}
	n := copy(p, r.b[r.pos:])
	r.pos += n
	return n, nil
}

// readerOf returns a reader of the server's input over the bytes of s.
func readerOf(s string) *reader {
	return newReader(pu.New(bytesReader{b: compat.Bytes(s)}))
}

// frame returns body with its Content-Length header.
func frame(body []byte) string {
	return compat.Concat("Content-Length: ", strconv.Itoa(len(body)), "\r\n\r\n", compat.String(body))
}

// A client drives a server as an editor would.
type client struct {
	t      *testing.T
	s      *server
	out    *compat.Buffer // what the server wrote, not yet read
	notes  []*jsonValue   // notifications from the server
	next   int            // notes[next:] are not yet looked at
	nextID int
	stdlib string // the standard library directory
}

// stdlibDir returns the repository's standard library, $PLUTOROOT, or "".
func stdlibDir() string {
	dir := compat.Getenv("PLUTOROOT")
	if dir == "" {
		return ""
	}
	abs := filepath.Abs(dir) or return dir
	return abs
}

// libDir is the repository's standard library: $PLUTOROOT.
func libDir(t *testing.T) string {
	t.Helper()
	dir := stdlibDir()
	if dir == "" {
		t.Fatal("PLUTOROOT is not set: it must name the standard library, the lib directory of the repository")
	}
	return dir
}

// newServer returns a server as Serve starts one, writing to out.
func newServer(opts Options, out io.Writer) *server {
	return pu.New(server{
		opts:      opts,
		out:       pu.New(writer{w: out}),
		log:       pu.New(logger{w: opts.Log, verbose: opts.Verbose}),
		docs:      map[string]*document{},
		published: map[string]string{},
	})
}

// startServer starts a server with opts (the repository's library unless
// opts says otherwise) and returns a client for it.
func startServer(t *testing.T, opts Options) client {
	t.Helper()
	if opts.StdlibDir == "" {
		opts.StdlibDir = libDir(t)
	}
	opts.InFd = -1
	out := pu.Alloc[compat.Buffer]()
	return client{t: t, s: newServer(opts, out), out: out, stdlib: opts.StdlibDir}
}

// requestJSON returns the JSON text of a request (id > 0) or a notification.
func requestJSON(id int, method string, params []byte) []byte {
	w := newJSONWriter()
	w.beginObject()
	w.key("jsonrpc")
	w.str("2.0")
	if id > 0 {
		w.key("id")
		w.integer(id)
	}
	w.key("method")
	w.str(method)
	w.key("params")
	if params == nil {
		w.null()
	} else {
		w.raw(params)
	}
	w.endObject()
	return w.bytes()
}

// deliver sends the server the bytes of a message stream, which holds one
// message, and has the server handle it.
func (c *client) deliver(stream string) {
	c.t.Helper()
	m := readMessage(readerOf(stream)) or {
		c.t.Fatalf("reading the message the client sent: %v", err)
		return
	}
	c.s.handle(m)
}

// collect parses what the server wrote: it keeps the notifications and
// returns the response to request id, or nil.
func (c *client) collect(id int) *jsonValue {
	c.t.Helper()
	data := compat.String(c.out.Bytes())
	c.out.Reset()
	var resp *jsonValue
	for len(data) > 0 {
		head := strings.Index(data, "\r\n\r\n")
		if head < 0 {
			c.t.Fatalf("the server wrote a message without a header: %q", data)
			return nil
		}
		length := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(strings.Slice(data, 0, head), "Content-Length:"))) or {
			c.t.Fatalf("the server wrote a bad header: %q", strings.Slice(data, 0, head))
			return nil
		}
		body := compat.Bytes(strings.Slice(data, head+4, head+4+length))
		data = strings.SliceFrom(data, head+4+length)
		v := parseJSON(body) or {
			c.t.Fatalf("the server wrote bad JSON: %v", err)
			return nil
		}
		if jString(v.get("method")) != "" {
			c.notes = compat.Append(c.notes, v)
			continue
		}
		if rid := v.get("id"); rid != nil && compat.String(rid.raw) == strconv.Itoa(id) {
			resp = v
		}
	}
	return resp
}

// call sends a request and returns its result, or its error.
func (c *client) call(method string, params []byte) (*jsonValue, *rpcError) {
	c.t.Helper()
	c.nextID++
	id := c.nextID
	c.deliver(frame(requestJSON(id, method, params)))
	resp := c.collect(id)
	if resp == nil {
		c.t.Fatalf("%s: no response", method)
		return nil, nil
	}
	if e := resp.get("error"); !e.isNull() {
		return nil, newError(jInt(e.get("code")), jString(e.get("message")))
	}
	return resp.get("result"), nil
}

// result calls a method that must succeed and returns its result.
func (c *client) result(method string, params []byte) *jsonValue {
	c.t.Helper()
	v, rerr := c.call(method, params)
	if rerr != nil {
		c.t.Fatalf("%s: %s", method, rerr.Error())
		return nil
	}
	return v
}

func (c *client) notify(method string, params []byte) {
	c.t.Helper()
	c.deliver(frame(requestJSON(0, method, params)))
	c.collect(0)
}

// initialize runs the handshake with root as the workspace folder, and
// returns the server's result.
func (c *client) initialize(root string, settings *Settings) *jsonValue {
	c.t.Helper()
	w := newJSONWriter()
	w.beginObject()
	w.key("processId")
	w.null()
	w.key("rootUri")
	w.str(uriFromPath(root))
	w.key("workspaceFolders")
	w.beginArray()
	writeFolder(w, root, "root")
	w.endArray()
	if settings != nil {
		w.key("initializationOptions")
		w.beginObject()
		w.key("target")
		w.str(settings.Target)
		w.key("mode")
		w.str(settings.Mode)
		w.key("tags")
		writeStrings(w, settings.Tags)
		w.endObject()
	}
	w.endObject()
	res := c.result("initialize", w.bytes())
	c.notify("initialized", compat.Bytes("{}"))
	return res
}

func writeFolder(w *jsonWriter, path, name string) {
	w.beginObject()
	w.key("uri")
	w.str(uriFromPath(path))
	if name != "" {
		w.key("name")
		w.str(name)
	}
	w.endObject()
}

// open opens a file of the workspace with its contents on disk.
func (c *client) open(path string) {
	c.t.Helper()
	src := compat.ReadFile(path) or {
		c.t.Fatal(err)
		return
	}
	c.edit(path, compat.String(src))
}

// edit opens a file with new contents in place of the file on disk.
func (c *client) edit(path, src string) {
	c.t.Helper()
	w := newJSONWriter()
	w.beginObject()
	w.key("textDocument")
	w.beginObject()
	w.key("uri")
	w.str(uriFromPath(path))
	w.key("languageId")
	w.str("plutonium")
	w.key("version")
	w.integer(1)
	w.key("text")
	w.str(src)
	w.endObject()
	w.endObject()
	c.notify("textDocument/didOpen", w.bytes())
}

// change sends a full-text change of an open document.
func (c *client) change(path string, version int, src string) {
	c.t.Helper()
	c.changeRange(path, version, nil, src)
}

// changeRange sends a change of an open document; r is nil for the whole text.
func (c *client) changeRange(path string, version int, r *Range, src string) {
	c.t.Helper()
	w := newJSONWriter()
	w.beginObject()
	w.key("textDocument")
	w.beginObject()
	w.key("uri")
	w.str(uriFromPath(path))
	w.key("version")
	w.integer(version)
	w.endObject()
	w.key("contentChanges")
	w.beginArray()
	w.beginObject()
	if r != nil {
		w.key("range")
		r.write(w)
	}
	w.key("text")
	w.str(src)
	w.endObject()
	w.endArray()
	w.endObject()
	c.notify("textDocument/didChange", w.bytes())
}

// docNotify sends a notification whose parameters are a text document.
func (c *client) docNotify(method, path string) {
	c.t.Helper()
	c.notify(method, docParams(path))
}

// flush runs the debounce timer, as the server's loop does once the
// debounce has passed.
func (c *client) flush() {
	c.t.Helper()
	if c.s.armed {
		c.s.armed = false
		c.s.diagnose()
		c.collect(0)
	}
}

// diagnostics waits for the diagnostics of a file that satisfy ok.
func (c *client) diagnostics(path string, ok func([]Diagnostic) bool) []Diagnostic {
	c.t.Helper()
	c.flush()
	for c.next < len(c.notes) {
		m := c.notes[c.next]
		c.next++
		method := jString(m.get("method"))
		if method != "textDocument/publishDiagnostics" {
			c.t.Logf("notification %s", method)
			continue
		}
		p := m.get("params")
		if pathFromURI(jString(p.get("uri"))) != path {
			continue
		}
		diags := c.diagnosticsOf(p.get("diagnostics"))
		if ok(diags) {
			return diags
		}
		c.t.Logf("diagnostics for %s: %s", filepath.Base(path), messagesOf(diags))
	}
	c.t.Fatalf("no matching diagnostics for %s", path)
	return nil
}

// messagesOf returns the messages of diags, for the log.
func messagesOf(diags []Diagnostic) string {
	var msgs []string
	for _, d := range diags {
		msgs = compat.Append(msgs, d.Message)
	}
	return compat.Join(msgs, "; ")
}

// hasMessage reports whether one of diags has sub in its message.
func hasMessage(diags []Diagnostic, sub string) bool {
	for _, d := range diags {
		if strings.Contains(d.Message, sub) {
			return true
		}
	}
	return false
}

// shutdown ends the session cleanly.
func (c *client) shutdown() {
	c.t.Helper()
	_, rerr := c.call("shutdown", nil)
	if rerr != nil {
		c.t.Fatal(rerr.Error())
		return
	}
	c.notify("exit", nil)
	if !c.s.exited || !c.s.shutdown {
		c.t.Fatal("server did not exit")
	}
}

// Results of the requests, decoded from the server's JSON.

func jElems(v *jsonValue) []*jsonValue {
	if v == nil {
		return nil
	}
	return v.elems
}

func jString(v *jsonValue) string {
	if v == nil || v.kind != jsonString {
		return ""
	}
	return v.s
}

func jInt(v *jsonValue) int {
	if v == nil || v.kind != jsonNumber {
		return 0
	}
	n := strconv.Atoi(v.s) or return 0
	return n
}

// jAt returns the value at the path of member names from v.
func jAt(v *jsonValue, names ...string) *jsonValue {
	for _, name := range names {
		v = v.get(name)
	}
	return v
}

func (c *client) rangeOf(v *jsonValue) Range {
	c.t.Helper()
	var d decoder
	var r Range
	decodeRange(&d, v, &r, "")
	if d.err != nil {
		c.t.Fatal(d.err)
	}
	return r
}

func (c *client) locationsOf(v *jsonValue) []Location {
	c.t.Helper()
	var d decoder
	var list []Location
	for _, e := range jElems(v) {
		var l Location
		decodeLocation(&d, e, &l, "")
		list = compat.Append(list, l)
	}
	if d.err != nil {
		c.t.Fatal(d.err)
	}
	return list
}

func (c *client) diagnosticsOf(v *jsonValue) []Diagnostic {
	c.t.Helper()
	var d decoder
	var list []Diagnostic
	for _, e := range jElems(v) {
		var diag Diagnostic
		decodeDiagnostic(&d, e, &diag, "")
		list = compat.Append(list, diag)
	}
	if d.err != nil {
		c.t.Fatal(d.err)
	}
	return list
}

func (c *client) hoverOf(v *jsonValue) *Hover {
	if v.isNull() {
		return nil
	}
	h := pu.New(Hover{})
	h.Contents.Kind = jString(jAt(v, "contents", "kind"))
	h.Contents.Value = jString(jAt(v, "contents", "value"))
	return h
}

func (c *client) symbolsOf(v *jsonValue) []DocumentSymbol {
	c.t.Helper()
	var list []DocumentSymbol
	for _, e := range jElems(v) {
		list = compat.Append(list, DocumentSymbol{
			Name:           jString(e.get("name")),
			Detail:         jString(e.get("detail")),
			Kind:           jInt(e.get("kind")),
			Range:          c.rangeOf(e.get("range")),
			SelectionRange: c.rangeOf(e.get("selectionRange")),
			Children:       c.symbolsOf(e.get("children")),
		})
	}
	return list
}

func (c *client) infosOf(v *jsonValue) []SymbolInformation {
	c.t.Helper()
	var list []SymbolInformation
	for _, e := range jElems(v) {
		var d decoder
		var loc Location
		decodeLocation(&d, e.get("location"), &loc, "")
		if d.err != nil {
			c.t.Fatal(d.err)
		}
		list = compat.Append(list, SymbolInformation{
			Name:          jString(e.get("name")),
			Kind:          jInt(e.get("kind")),
			Location:      loc,
			ContainerName: jString(e.get("containerName")),
		})
	}
	return list
}

func (c *client) editsOf(v *jsonValue) []TextEdit {
	c.t.Helper()
	var list []TextEdit
	for _, e := range jElems(v) {
		list = compat.Append(list, TextEdit{Range: c.rangeOf(e.get("range")), NewText: jString(e.get("newText"))})
	}
	return list
}

func (c *client) completionOf(v *jsonValue) CompletionList {
	var list CompletionList
	for _, e := range jElems(v.get("items")) {
		list.Items = compat.Append(list.Items, CompletionItem{Label: jString(e.get("label")), Kind: jInt(e.get("kind"))})
	}
	return list
}

func (c *client) signatureOf(v *jsonValue) *SignatureHelp {
	if v.isNull() {
		return nil
	}
	h := pu.New(SignatureHelp{ActiveSignature: jInt(v.get("activeSignature")), ActiveParameter: jInt(v.get("activeParameter"))})
	for _, e := range jElems(v.get("signatures")) {
		sig := SignatureInformation{Label: jString(e.get("label"))}
		if doc := e.get("documentation"); !doc.isNull() {
			sig.Documentation = pu.New(MarkupContent{Kind: jString(doc.get("kind")), Value: jString(doc.get("value"))})
		}
		for _, p := range jElems(e.get("parameters")) {
			l := jElems(p.get("label"))
			if len(l) != 2 {
				c.t.Fatalf("a parameter label of %d elements", len(l))
				return nil
			}
			sig.Parameters = compat.Append(sig.Parameters, ParameterInformation{Label: [2]int{jInt(l[0]), jInt(l[1])}})
		}
		h.Signatures = compat.Append(h.Signatures, sig)
	}
	return h
}

func (c *client) prepareOf(v *jsonValue) *PrepareRenameResult {
	if v.isNull() {
		return nil
	}
	return pu.New(PrepareRenameResult{Range: c.rangeOf(v.get("range")), Placeholder: jString(v.get("placeholder"))})
}

func (c *client) actionsOf(v *jsonValue) []CodeAction {
	c.t.Helper()
	var list []CodeAction
	for _, e := range jElems(v) {
		a := CodeAction{Title: jString(e.get("title")), Kind: jString(e.get("kind")), Diagnostics: c.diagnosticsOf(e.get("diagnostics"))}
		if cmd := e.get("command"); !cmd.isNull() {
			command := pu.New(Command{Title: jString(cmd.get("title")), Command: jString(cmd.get("command"))})
			for _, arg := range jElems(cmd.get("arguments")) {
				command.Arguments = compat.Append(command.Arguments, jString(arg))
			}
			a.Command = command
		}
		list = compat.Append(list, a)
	}
	return list
}

// Parameters of the requests.

func writeDocID(w *jsonWriter, path string) {
	w.beginObject()
	w.key("uri")
	w.str(uriFromPath(path))
	w.endObject()
}

func docParams(path string) []byte {
	w := newJSONWriter()
	w.beginObject()
	w.key("textDocument")
	writeDocID(w, path)
	w.endObject()
	return w.bytes()
}

func writePosParams(w *jsonWriter, path string, pos Position) {
	w.key("textDocument")
	writeDocID(w, path)
	w.key("position")
	pos.write(w)
}

func posParams(path string, pos Position) []byte {
	w := newJSONWriter()
	w.beginObject()
	writePosParams(w, path, pos)
	w.endObject()
	return w.bytes()
}

func refParams(path string, pos Position, includeDecl bool) []byte {
	w := newJSONWriter()
	w.beginObject()
	writePosParams(w, path, pos)
	w.key("context")
	w.beginObject()
	w.key("includeDeclaration")
	w.boolean(includeDecl)
	w.endObject()
	w.endObject()
	return w.bytes()
}

func renameParams(path string, pos Position, newName string) []byte {
	w := newJSONWriter()
	w.beginObject()
	writePosParams(w, path, pos)
	w.key("newName")
	w.str(newName)
	w.endObject()
	return w.bytes()
}

func queryParams(query string) []byte {
	w := newJSONWriter()
	w.beginObject()
	w.key("query")
	w.str(query)
	w.endObject()
	return w.bytes()
}

// jsonText returns s as the JSON text of a string.
func jsonText(s string) []byte {
	w := newJSONWriter()
	w.str(s)
	return w.bytes()
}

// The requests.

func (c *client) hover(path string, pos Position) *Hover {
	c.t.Helper()
	return c.hoverOf(c.result("textDocument/hover", posParams(path, pos)))
}

func (c *client) definition(path string, pos Position) []Location {
	c.t.Helper()
	return c.locationsOf(c.result("textDocument/definition", posParams(path, pos)))
}

func (c *client) references(path string, pos Position, includeDecl bool) []Location {
	c.t.Helper()
	return c.locationsOf(c.result("textDocument/references", refParams(path, pos, includeDecl)))
}

func (c *client) documentSymbols(path string) []DocumentSymbol {
	c.t.Helper()
	return c.symbolsOf(c.result("textDocument/documentSymbol", docParams(path)))
}

func (c *client) workspaceSymbols(query string) []SymbolInformation {
	c.t.Helper()
	return c.infosOf(c.result("workspace/symbol", queryParams(query)))
}

func (c *client) formatting(path string) []TextEdit {
	c.t.Helper()
	return c.editsOf(c.result("textDocument/formatting", docParams(path)))
}

func (c *client) completion(path string, pos Position) CompletionList {
	c.t.Helper()
	return c.completionOf(c.result("textDocument/completion", posParams(path, pos)))
}

func (c *client) signatureHelp(path string, pos Position) *SignatureHelp {
	c.t.Helper()
	return c.signatureOf(c.result("textDocument/signatureHelp", posParams(path, pos)))
}

func (c *client) prepareRename(path string, pos Position) *PrepareRenameResult {
	c.t.Helper()
	return c.prepareOf(c.result("textDocument/prepareRename", posParams(path, pos)))
}

// rename returns the edits of a rename, by document URI.
func (c *client) rename(path string, pos Position, newName string) map[string][]TextEdit {
	c.t.Helper()
	v := c.result("textDocument/rename", renameParams(path, pos, newName))
	changes := map[string][]TextEdit{}
	if ch := v.get("changes"); ch != nil {
		for i, uri := range ch.keys {
			changes[uri] = c.editsOf(ch.vals[i])
		}
	}
	return changes
}

// codeActions asks for the actions at r for the diagnostics diags.
func (c *client) codeActions(path string, r Range, diags []Diagnostic) []CodeAction {
	c.t.Helper()
	w := newJSONWriter()
	w.beginObject()
	w.key("textDocument")
	writeDocID(w, path)
	w.key("range")
	r.write(w)
	w.key("context")
	w.beginObject()
	w.key("diagnostics")
	writeDiagnostics(w, diags)
	w.endObject()
	w.endObject()
	return c.actionsOf(c.result("textDocument/codeAction", w.bytes()))
}

// tempDir returns a new directory, which the test removes with removeAll.
func tempDir(t *testing.T) string {
	t.Helper()
	dir := os.MkdirTemp("", "lsp-test-") or {
		t.Fatalf("MkdirTemp: %v", err)
		return ""
	}
	return dir
}

func removeAll(dir string) { _ = os.RemoveAll(dir) }

// writeFiles writes files (path relative to a new directory: contents)
// and returns the directory, which the test removes with removeAll.
func writeFiles(t *testing.T, files map[string]string) string {
	t.Helper()
	root := tempDir(t)
	for name, src := range files {
		p := filepath.Join(root, name)
		os.MkdirAll(filepath.Dir(p), 0o755) or {
			t.Fatal(err)
			return root
		}
		os.WriteFile(p, compat.Bytes(src), 0o644) or {
			t.Fatal(err)
			return root
		}
	}
	return root
}

// readFile returns the contents of a file.
func readFile(t *testing.T, path string) string {
	t.Helper()
	b := compat.ReadFile(path) or {
		t.Fatal(err)
		return ""
	}
	return compat.String(b)
}

// at returns the position of the n'th byte of the first occurrence of
// marker in the file at path.
func at(t *testing.T, path, marker string, n int) Position {
	t.Helper()
	src := readFile(t, path)
	i := strings.Index(src, marker)
	if i < 0 {
		t.Fatalf("%q not in %s", marker, path)
		return Position{}
	}
	return newText(compat.Bytes(src)).position(i + n)
}

// The demo module used by most tests.
var demo = map[string]string{
	"pluto.mod": "module example.com/demo\n",
	"shapes/shapes.p": `// Package shapes has shapes.
package shapes

// Color is a colour.
type Color enum {
	Red
	Green
	Blue
}

// Buffer owns memory.
type Buffer owner struct {
	data []byte
	Size int // Size is the size.
}

// Drop frees the buffer.
func (b *Buffer) Drop() {}

// Len returns the size.
func (b *Buffer) Len() int { return b.Size }

// Area computes an area.
//
//pu:noinline
func Area(w, h int) int { return w * h }
`,
	"main.p": `package main

import (
	"pluto/strings"

	"example.com/demo/shapes"
)

func main() {
	var b shapes.Buffer
	b.Size = shapes.Area(2, 3)
	c := shapes.Color.Green
	_ = c
	if strings.HasPrefix("abc", "a") {
		_ = b.Len()
	}
}
`,
}

func TestInitialize(t *testing.T) {
	c := startServer(t, Options{})
	if _, rerr := c.call("textDocument/hover", nil); rerr == nil || rerr.Code != codeServerNotInitialized {
		t.Errorf("hover before initialize: got %v, want error %d", rerr, codeServerNotInitialized)
	}
	root := tempDir(t)
	defer removeAll(root)
	caps := jAt(c.initialize(root, nil), "capabilities")
	if !jAt(caps, "hoverProvider").b || !jAt(caps, "definitionProvider").b || !jAt(caps, "referencesProvider").b ||
		!jAt(caps, "documentSymbolProvider").b || !jAt(caps, "workspaceSymbolProvider").b ||
		!jAt(caps, "documentFormattingProvider").b || !jAt(caps, "renameProvider", "prepareProvider").b ||
		jInt(jAt(caps, "textDocumentSync", "change")) != 1 || len(jElems(jAt(caps, "completionProvider", "triggerCharacters"))) == 0 {
		t.Errorf("capabilities = %s", caps.raw)
	}
	if _, rerr := c.call("initialize", compat.Bytes("{}")); rerr == nil {
		t.Error("second initialize succeeded")
	}
	if _, rerr := c.call("no/such/method", nil); rerr == nil || rerr.Code != codeMethodNotFound {
		t.Errorf("unknown method: got %v", rerr)
	}
	c.shutdown()
}

// serve runs Serve on the framed messages of input and returns what it
// wrote, and its result.
func serve(t *testing.T, input string) (string, error) {
	t.Helper()
	out := pu.Alloc[compat.Buffer]()
	err := Serve(pu.New(bytesReader{b: compat.Bytes(input)}), out, Options{StdlibDir: libDir(t), InFd: -1})
	return out.String(), err
}

func TestExitWithoutShutdown(t *testing.T) {
	root := tempDir(t)
	defer removeAll(root)
	w := newJSONWriter()
	w.beginObject()
	w.key("rootUri")
	w.str(uriFromPath(root))
	w.endObject()
	input := compat.Concat(frame(requestJSON(1, "initialize", w.bytes())), frame(requestJSON(0, "exit", nil)))
	_, err := serve(t, input)
	if !errors.Is(err, ErrExitWithoutShutdown) {
		t.Errorf("Serve = %v, want %v", err, ErrExitWithoutShutdown)
	}
}

// TestServeShutdown runs a whole session through Serve: the handshake, a
// request, and a shutdown and exit.
func TestServeShutdown(t *testing.T) {
	root := tempDir(t)
	defer removeAll(root)
	w := newJSONWriter()
	w.beginObject()
	w.key("rootUri")
	w.str(uriFromPath(root))
	w.endObject()
	input := compat.Concat(
		frame(requestJSON(1, "initialize", w.bytes())),
		frame(requestJSON(0, "initialized", compat.Bytes("{}"))),
		frame(requestJSON(2, "workspace/symbol", queryParams(""))),
		frame(requestJSON(3, "shutdown", nil)),
		frame(requestJSON(0, "exit", nil)),
	)
	out, err := serve(t, input)
	if err != nil {
		t.Fatalf("Serve = %v", err)
	}
	for _, want := range []string{`"id":1,"result":{"capabilities"`, `"id":2,"result":`, `"id":3,"result":null`} {
		if !strings.Contains(out, want) {
			t.Errorf("output %q lacks %q", out, want)
		}
	}
}

func TestServeEOF(t *testing.T) {
	// A disconnected client ends the server cleanly without requiring
	// an exit notification.
	out := pu.Alloc[compat.Buffer]()
	if err := Serve(pu.New(bytesReader{}), out, Options{StdlibDir: libDir(t), InFd: -1}); err != nil {
		t.Fatalf("Serve after client EOF = %v", err)
	}
}
