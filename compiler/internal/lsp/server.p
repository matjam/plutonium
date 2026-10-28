// Translated by go2pu from internal/lsp/server.go; edited by hand since.
//
// The Go server reads messages on a goroutine and selects between them
// and a debounce timer. This one runs on one thread: when diagnostics are
// pending and no input is buffered, it polls the input descriptor until
// the debounce deadline, and diagnoses if nothing arrives by then. Handler
// results are encoded to JSON text as they are produced, and a handler
// that panics ends the server (Plutonium has no recover).

// Package lsp implements a Language Server Protocol server for Plutonium:
// diagnostics, hover, go to definition, references, symbols, formatting,
// completion, signature help and rename, over JSON-RPC on a pair of
// streams (pluto lsp runs it on standard input and output).
//
// The server type-checks with internal/types2 only; it never lowers or
// generates code, so diagnostics stay fast. Open documents are kept in an
// overlay, so unsaved edits are what gets checked.
package lsp

import (
	"compiler/compat"
	"compiler/internal/scratch"
	"compiler/pu"
	"pluto/io"
	"pluto/path/filepath"
	"pluto/slices"
	"pluto/strings"
	"pluto/syscall"
	"pluto/time"
)

// Options configure a server.
type Options struct {
	// StdlibDir is the directory holding the standard library's pluto/
	// directory, such as $PLUTOROOT.
	StdlibDir string
	// InFd is the file descriptor the input reads from, which the server
	// polls while diagnostics are pending; -1 if there is none, and then
	// pending diagnostics wait for the next message.
	InFd int
	// Debounce is how long the server waits after an edit before it
	// publishes diagnostics; the default is 150ms.
	Debounce compat.Duration
	Version  string
	// Log receives the log; nil discards it. Verbose adds a line for every
	// message and how long it took.
	Log     io.Writer
	Verbose bool
}

// ErrExitWithoutShutdown is returned by Serve when the client sends exit
// without a shutdown request first; the process should then exit with
// status 1.
var ErrExitWithoutShutdown = compat.NewError("exit without shutdown")

// A logger writes log lines in the form of slog's text handler, without
// the time.
type logger struct {
	w       io.Writer
	verbose bool
}

// logValue quotes a log attribute's value if it needs it, as slog does.
func logValue(s string) string {
	if s == "" || strings.ContainsAny(s, " \t\n\"=") {
		return compat.Sprintfb("%q", compat.ArgS(s))
	}
	return s
}

func (l *logger) line(level, msg, attrs string) {
	if l.w == nil {
		return
	}
	// slog's text format, which the VS Code extension parses; the time is
	// in UTC, since pluto/time has no local time zone.
	now := time.Now()
	year, month, day := now.Date()
	hour, min, sec := now.Clock()
	stamp := compat.Sprintfb("%04d-%02d-%02dT%02d:%02d:%02d.%03dZ", compat.ArgI(int64(year)), compat.ArgI(int64(month)), compat.ArgI(int64(day)), compat.ArgI(int64(hour)), compat.ArgI(int64(min)), compat.ArgI(int64(sec)), compat.ArgI(int64(now.Nanosecond()/1000000)))
	text := compat.Concat("time=", stamp, " level=", level, " msg=", logValue(msg))
	if attrs != "" {
		text = compat.Concat(text, " ", attrs)
	}
	_, _ = l.w.Write(compat.Bytes(compat.Concat(text, "\n")))
}

func (l *logger) debug(msg, attrs string) {
	if l.verbose {
		l.line("DEBUG", msg, attrs)
	}
}

func (l *logger) info(msg, attrs string)  { l.line("INFO", msg, attrs) }
func (l *logger) warn(msg, attrs string)  { l.line("WARN", msg, attrs) }
func (l *logger) error(msg, attrs string) { l.line("ERROR", msg, attrs) }

type server struct {
	opts     Options
	out      *writer
	log      *logger
	ws       *workspace
	settings Settings

	initialized bool
	shutdown    bool
	exited      bool

	docs      map[string]*document // open documents by path
	published map[string]uint64    // path -> hash of the diagnostics last published, as JSON
	paths     map[string]string    // the keys of docs and published, interned in the heap

	// The memory of the settings and modes, and of the workspace folders,
	// once a change has replaced what initialize put in the heap.
	settingsMem *scratch.Region
	rootsMem    *scratch.Region

	// The debounce timer: armed after an edit, it fires debounce after
	// armedAt.
	armed   bool
	armedAt compat.Time
}

type document struct {
	uri     string
	version int
	mem     *scratch.Region // holds the document; closing it frees it
}

// resetTimer arms the debounce timer, as time.Timer's Reset does.
func (s *server) resetTimer() {
	s.armed = true
	s.armedAt = compat.Now()
}

func (s *server) stopTimer() { s.armed = false }

// Serve runs a server that reads requests from in and writes responses to
// out until the client sends exit or in reaches end of file.
func Serve(in io.Reader, out io.Writer, opts Options) error {
	if opts.Debounce == 0 {
		opts.Debounce = 150 * 1000000
	}
	s := pu.New(server{
		opts:      opts,
		out:       pu.New(writer{w: out}),
		log:       pu.New(logger{w: opts.Log, verbose: opts.Verbose}),
		docs:      map[string]*document{},
		published: map[string]uint64{},
	})
	r := newReader(in)
	for {
		if s.armed && !r.buffered() {
			left := opts.Debounce - compat.Since(s.armedAt)
			if left <= 0 {
				s.armed = false
				mem := scratch.NewRegion()
				use := mem.Use()
				s.diagnose()
				use.Restore()
				mem.Release()
				continue
			}
			if opts.InFd >= 0 {
				ms := int((left + 999999) / 1000000)
				fds := [1]syscall.PollFd{{Fd: int32(opts.InFd), Events: syscall.POLLIN}}
				n, err := syscall.Poll(fds[:], ms)
				if err == nil && n == 0 {
					continue // the deadline passed; the next turn diagnoses
				}
				if err != nil && err == syscall.EINTR {
					continue
				}
			}
		}
		// Each message is read and handled in memory of its own, freed
		// once it is handled: what outlives it (the workspace's packages
		// and texts, the open documents, the settings) goes to the heap
		// or to a region of its own.
		mem := scratch.NewRegion()
		use := mem.Use()
		m, err := readMessage(r)
		if err != nil {
			use.Restore()
			if err == io.EOF {
				err = nil
			} else {
				err = scratch.Error(err)
			}
			mem.Release()
			return err
		}
		s.handle(m)
		use.Restore()
		mem.Release()
		if s.exited {
			if !s.shutdown {
				return ErrExitWithoutShutdown
			}
			return nil
		}
	}
}

// handle dispatches one message.
func (s *server) handle(m *message) {
	if m.Error != nil && m.Method == "" && len(m.ID) == 0 {
		s.log.warn("malformed message", compat.Concat("error=", logValue(m.Error.Message)))
		return
	}
	if m.Method == "" {
		return // a response; the server sends no requests
	}
	start := compat.Now()
	if !m.isRequest() {
		s.notification(m.Method, m.Params, m.HasParams)
		s.log.debug("notification", compat.Concat("method=", logValue(m.Method), " took=", compat.Since(start).String()))
		return
	}
	result, rerr := s.request(m.Method, m.Params, m.HasParams)
	if err := s.out.reply(m.ID, result, rerr); err != nil {
		s.log.error("writing response", compat.Concat("error=", logValue(err.Error())))
	}
	s.log.debug("request", compat.Concat("method=", logValue(m.Method), " took=", compat.Since(start).String()))
}

// invalidParams is the error of parameters that do not decode.
func invalidParams(err error) *rpcError {
	return newError(codeInvalidParams, err.Error())
}

// request answers a request, with the JSON text of its result.
func (s *server) request(method string, params *jsonValue, hasParams bool) ([]byte, *rpcError) {
	switch {
	case method == "initialize":
		if s.initialized {
			return nil, newError(codeInvalidRequest, "initialize sent twice")
		}
		if !hasParams {
			return nil, newError(codeInvalidParams, "unexpected end of JSON input")
		}
		var p InitializeParams
		var d decoder
		decodeInitializeParams(&d, params, &p)
		if d.err != nil {
			return nil, invalidParams(d.err)
		}
		return s.initialize(&p)
	case !s.initialized:
		return nil, newError(codeServerNotInitialized, "the server is not initialized")
	case s.shutdown:
		return nil, newError(codeInvalidRequest, "the server is shutting down")
	case method == "shutdown":
		s.shutdown = true
		return nil, nil
	}
	// The handlers; absent parameters decode as zero ones, as the Go
	// version's withParams does.
	var d decoder
	w := newJSONWriter()
	switch method {
	case "textDocument/hover":
		var p TextDocumentPositionParams
		decodePositionParams(&d, params, &p, "TextDocumentPositionParams")
		if d.err != nil {
			return nil, invalidParams(d.err)
		}
		res, rerr := s.hover(&p)
		if rerr != nil {
			return nil, rerr
		}
		res.write(w)
	case "textDocument/definition":
		var p TextDocumentPositionParams
		decodePositionParams(&d, params, &p, "TextDocumentPositionParams")
		if d.err != nil {
			return nil, invalidParams(d.err)
		}
		res, rerr := s.definition(&p)
		if rerr != nil {
			return nil, rerr
		}
		writeLocations(w, res)
	case "textDocument/references":
		var p ReferenceParams
		decodeReferenceParams(&d, params, &p)
		if d.err != nil {
			return nil, invalidParams(d.err)
		}
		res, rerr := s.references(&p)
		if rerr != nil {
			return nil, rerr
		}
		writeLocations(w, res)
	case "textDocument/documentSymbol":
		var p DocumentSymbolParams
		decodeDocumentParams(&d, params, &p.TextDocument, "DocumentSymbolParams")
		if d.err != nil {
			return nil, invalidParams(d.err)
		}
		res, rerr := s.documentSymbols(&p)
		if rerr != nil {
			return nil, rerr
		}
		writeDocumentSymbols(w, res)
	case "workspace/symbol":
		var p WorkspaceSymbolParams
		decodeWorkspaceSymbolParams(&d, params, &p)
		if d.err != nil {
			return nil, invalidParams(d.err)
		}
		res, rerr := s.workspaceSymbols(&p)
		if rerr != nil {
			return nil, rerr
		}
		writeSymbolInformation(w, res)
	case "textDocument/formatting":
		var p DocumentFormattingParams
		decodeDocumentParams(&d, params, &p.TextDocument, "DocumentFormattingParams")
		if d.err != nil {
			return nil, invalidParams(d.err)
		}
		res, rerr := s.formatting(&p)
		if rerr != nil {
			return nil, rerr
		}
		writeTextEdits(w, res)
	case "textDocument/completion":
		var p TextDocumentPositionParams
		decodePositionParams(&d, params, &p, "TextDocumentPositionParams")
		if d.err != nil {
			return nil, invalidParams(d.err)
		}
		res, rerr := s.completion(&p)
		if rerr != nil {
			return nil, rerr
		}
		res.write(w)
	case "textDocument/signatureHelp":
		var p TextDocumentPositionParams
		decodePositionParams(&d, params, &p, "TextDocumentPositionParams")
		if d.err != nil {
			return nil, invalidParams(d.err)
		}
		res, rerr := s.signatureHelp(&p)
		if rerr != nil {
			return nil, rerr
		}
		res.write(w)
	case "textDocument/prepareRename":
		var p TextDocumentPositionParams
		decodePositionParams(&d, params, &p, "TextDocumentPositionParams")
		if d.err != nil {
			return nil, invalidParams(d.err)
		}
		res, rerr := s.prepareRename(&p)
		if rerr != nil {
			return nil, rerr
		}
		res.write(w)
	case "textDocument/rename":
		var p RenameParams
		decodeRenameParams(&d, params, &p)
		if d.err != nil {
			return nil, invalidParams(d.err)
		}
		res, rerr := s.rename(&p)
		if rerr != nil {
			return nil, rerr
		}
		res.write(w)
	case "textDocument/codeAction":
		var p CodeActionParams
		decodeCodeActionParams(&d, params, &p)
		if d.err != nil {
			return nil, invalidParams(d.err)
		}
		res, rerr := s.codeAction(&p)
		if rerr != nil {
			return nil, rerr
		}
		writeCodeActions(w, res)
	default:
		return nil, newError(codeMethodNotFound, compat.Sprintfb("method %q not supported", compat.ArgS(method)))
	}
	return w.bytes(), nil
}

func (s *server) notification(method string, params *jsonValue, hasParams bool) {
	if method == "exit" {
		s.exited = true
		return
	}
	if !s.initialized || s.shutdown || method == "initialized" {
		return
	}
	var d decoder
	if !hasParams {
		switch method {
		case "textDocument/didOpen", "textDocument/didChange", "textDocument/didSave", "textDocument/didClose",
			"workspace/didChangeConfiguration", "workspace/didChangeWatchedFiles", "workspace/didChangeWorkspaceFolders":
			d.err = compat.NewError("unexpected end of JSON input")
		}
	}
	switch method {
	case "textDocument/didOpen":
		var p DidOpenTextDocumentParams
		decodeDidOpen(&d, params, &p)
		if d.err == nil {
			s.didOpen(&p)
		}
	case "textDocument/didChange":
		var p DidChangeTextDocumentParams
		decodeDidChange(&d, params, &p)
		if d.err == nil {
			s.didChange(&p)
		}
	case "textDocument/didSave":
		var p DidSaveTextDocumentParams
		decodeDidSave(&d, params, &p)
		if d.err == nil {
			s.didSave(&p)
		}
	case "textDocument/didClose":
		var p DidCloseTextDocumentParams
		decodeDocumentParams(&d, params, &p.TextDocument, "DidCloseTextDocumentParams")
		if d.err == nil {
			s.didClose(&p)
		}
	case "workspace/didChangeConfiguration":
		var p DidChangeConfigurationParams
		decodeDidChangeConfiguration(&d, params, &p)
		if d.err == nil {
			s.didChangeConfiguration(&p)
		}
	case "workspace/didChangeWatchedFiles":
		var p DidChangeWatchedFilesParams
		decodeDidChangeWatchedFiles(&d, params, &p)
		if d.err == nil {
			s.didChangeWatchedFiles(&p)
		}
	case "workspace/didChangeWorkspaceFolders":
		var p DidChangeWorkspaceFoldersParams
		decodeDidChangeWorkspaceFolders(&d, params, &p)
		if d.err == nil {
			s.didChangeWorkspaceFolders(&p)
		}
	default:
		return
	}
	if d.err != nil {
		s.log.warn("bad notification parameters", compat.Concat("method=", logValue(method), " error=", logValue(d.err.Error())))
	}
}

func (s *server) initialize(p *InitializeParams) ([]byte, *rpcError) {
	// The server's state outlives the message: it goes to the heap.
	defer scratch.Heap().Restore()
	if opts := p.InitializationOptions; !opts.isNull() {
		var d decoder
		settings := s.settings
		decodeSettings(&d, opts, &settings, "")
		s.settings = settings.clone()
		if d.err != nil {
			return nil, newError(codeInvalidParams, compat.Concat("initializationOptions: ", d.err.Error()))
		}
	}
	modes, err := modesFor(s.settings)
	if err != nil {
		s.showMessage(1, compat.Concat("Plutonium: ", err.Error(), "; using the host target"))
		modes, _ = modesFor(Settings{Tags: s.settings.Tags})
	}
	stdRoot := s.opts.StdlibDir
	s.ws = newWorkspace(stdRoot, modes)
	for _, f := range p.WorkspaceFolders {
		s.addRoot(f.URI)
	}
	if len(s.ws.roots) == 0 && p.RootURI != "" {
		s.addRoot(p.RootURI)
	}
	if len(s.ws.roots) == 0 && p.RootPath != "" {
		s.ws.roots = compat.Append(s.ws.roots, compat.Concat(filepath.Clean(p.RootPath)))
	}
	s.initialized = true
	s.log.info("initialized", compat.Concat("stdlib=", logValue(stdRoot), " roots=", logValue(compat.Concat("[", compat.Join(s.ws.roots, " "), "]")), " mode=", logValue(modes[0].name)))
	w := newJSONWriter()
	writeInitializeResult(w, s.opts.Version)
	return w.bytes(), nil
}

func (s *server) addRoot(uri string) {
	p := pathFromURI(uri)
	if p == "" {
		return
	}
	for _, r := range s.ws.roots {
		if r == p {
			return
		}
	}
	s.ws.roots = compat.Append(s.ws.roots, compat.Concat(p))
}

func (s *server) showMessage(typ int, msg string) {
	w := newJSONWriter()
	ShowMessageParams{Type: typ, Message: msg}.write(w)
	if err := s.out.notify("window/showMessage", w.bytes()); err != nil {
		s.log.error("writing notification", compat.Concat("error=", logValue(err.Error())))
	}
}

// Document synchronisation.

func (s *server) didOpen(p *DidOpenTextDocumentParams) {
	path := pathFromURI(p.TextDocument.URI)
	if path == "" {
		return
	}
	mem := scratch.NewRegion() // the document outlives the message
	use := mem.Use()
	doc := pu.New(document{uri: compat.Concat(p.TextDocument.URI), version: p.TextDocument.Version, mem: mem})
	use.Restore()
	if old := s.docs[path]; old != nil {
		old.mem.Release()
	}
	s.docs[s.intern(path)] = doc
	s.setText(path, compat.Bytes(p.TextDocument.Text))
	s.diagnose()
}

func (s *server) didChange(p *DidChangeTextDocumentParams) {
	path := pathFromURI(p.TextDocument.URI)
	doc := s.docs[path]
	if doc == nil || len(p.ContentChanges) == 0 {
		return
	}
	doc.version = p.TextDocument.Version
	src := s.ws.overlay[path]
	for _, c := range p.ContentChanges {
		if !c.HasRange {
			src = compat.Bytes(c.Text)
			continue
		}
		// A client that sends incremental changes despite full sync.
		t := newText(src)
		start, end := t.offset(c.Range.Start), t.offset(c.Range.End)
		next := compat.MakeCap[byte](0, start+len(c.Text)+len(src[end:]))
		next = compat.Append(next, src[:start]...)
		next = compat.AppendString(next, c.Text)
		next = compat.Append(next, src[end:]...)
		src = next
	}
	s.setText(path, src)
	s.resetTimer()
}

func (s *server) didSave(p *DidSaveTextDocumentParams) {
	path := pathFromURI(p.TextDocument.URI)
	if p.HasText && s.docs[path] != nil {
		s.setText(path, compat.Bytes(p.Text))
	}
	s.stopTimer()
	s.diagnose()
}

func (s *server) didClose(p *DidCloseTextDocumentParams) {
	path := pathFromURI(p.TextDocument.URI)
	doc := s.docs[path]
	if doc == nil {
		return
	}
	delete(s.docs, path)
	doc.mem.Release()
	s.ws.dropOverlay(path)
	s.ws.invalidate(filepath.Dir(path))
	s.diagnose()
}

func (s *server) setText(path string, src []byte) {
	s.ws.setOverlay(path, src)
	s.ws.invalidate(filepath.Dir(path))
}

// intern returns path in memory that outlives the message being handled,
// the same copy for the same path every time, so that the keys of the
// server's maps take no more memory than the files they name.
func (s *server) intern(path string) string {
	if p, ok := s.paths[path]; ok {
		return p
	}
	h := scratch.Heap()
	if s.paths == nil {
		s.paths = map[string]string{}
	}
	p := compat.Concat(path)
	s.paths[p] = p
	h.Restore()
	return p
}

func (s *server) didChangeConfiguration(p *DidChangeConfigurationParams) {
	// The Go version decodes the settings into a struct with a
	// *Settings "plutonium" member, and ignores them if that fails or the
	// member is null.
	v := p.Settings
	if v == nil {
		return
	}
	var d decoder
	if !d.object(v, "struct { Plutonium *lsp.Settings }", "") {
		return
	}
	pv := v.get("plutonium")
	if pv.isNull() {
		return
	}
	var settings Settings
	decodeSettings(&d, pv, &settings, "plutonium")
	if d.err != nil {
		return
	}
	if _, err := modesFor(settings); err != nil {
		s.showMessage(1, compat.Concat("Plutonium: ", err.Error()))
		return
	}
	// The settings outlive the message. The old ones are freed once no
	// cached package's key names one of their modes.
	mem := scratch.NewRegion()
	use := mem.Use()
	s.settings = settings.clone()
	s.ws.modes, _ = modesFor(s.settings)
	use.Restore()
	old := s.settingsMem
	s.settingsMem = mem
	s.ws.invalidateAll()
	old.Release()
	s.diagnose()
}

func (s *server) didChangeWatchedFiles(p *DidChangeWatchedFilesParams) {
	for _, c := range p.Changes {
		path := pathFromURI(c.URI)
		if path == "" || s.docs[path] != nil {
			continue // the editor's copy is what counts
		}
		if filepath.Base(path) == "pluto.mod" {
			s.ws.invalidateAll()
			continue
		}
		s.ws.invalidate(filepath.Dir(path))
		if c.Type == 3 { // deleted
			s.publish(path, nil)
		}
	}
	s.resetTimer()
}

func (s *server) didChangeWorkspaceFolders(p *DidChangeWorkspaceFoldersParams) {
	roots := compat.Clone(s.ws.roots)
	for _, f := range p.Event.Removed {
		path := pathFromURI(f.URI)
		kept := roots[:0]
		for _, r := range roots {
			if r != path {
				kept = compat.Append(kept, r)
			}
		}
		roots = kept
	}
	for _, f := range p.Event.Added {
		if path := pathFromURI(f.URI); path != "" && !slices.Contains(roots, path) {
			roots = compat.Append(roots, path)
		}
	}
	// The folders outlive the message: they get memory of their own, and
	// the memory of the old ones is freed.
	mem := scratch.NewRegion()
	use := mem.Use()
	kept := compat.Make[string](len(roots))
	for i, r := range roots {
		kept[i] = compat.Concat(r)
	}
	use.Restore()
	old := s.rootsMem
	s.ws.roots = kept
	s.rootsMem = mem
	old.Release()
}

// fileAt returns the checked file of a document and the byte offset of a
// position in it.
func (s *server) fileAt(uri string, pos Position) (*srcFile, int, *rpcError) {
	path := pathFromURI(uri)
	if path == "" {
		return nil, 0, newError(codeInvalidParams, compat.Concat("not a file URI: ", uri))
	}
	f, _, err := s.ws.fileOf(path)
	if err != nil || f == nil {
		return nil, 0, nil
	}
	return f, f.offset(pos), nil
}
