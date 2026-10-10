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
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"time"
)

// Options configure a server.
type Options struct {
	// StdlibDir is the directory holding the standard library's pluto/
	// directory, such as $PLUTOROOT. If it is empty, Stdlib is extracted
	// into CacheDir and used from there, so that go to definition can open
	// its files.
	StdlibDir string
	Stdlib    fs.FS
	// CacheDir is where Stdlib is extracted; the default is the user's
	// cache directory.
	CacheDir string
	// Debounce is how long the server waits after an edit before it
	// publishes diagnostics; the default is 150ms.
	Debounce time.Duration
	Version  string
	Log      *slog.Logger // nil discards the log
}

// ErrExitWithoutShutdown is returned by Serve when the client sends exit
// without a shutdown request first; the process should then exit with
// status 1.
var ErrExitWithoutShutdown = errors.New("exit without shutdown")

type server struct {
	opts     Options
	out      *writer
	log      *slog.Logger
	ws       *workspace
	settings Settings

	initialized bool
	shutdown    bool
	exited      bool

	docs      map[string]*document // open documents by path
	timer     *time.Timer          // debounces diagnostics after edits
	published map[string]string    // path -> diagnostics last published, as JSON
}

type document struct {
	uri     string
	version int
}

// Serve runs a server that reads requests from in and writes responses to
// out until the client sends exit, in reaches end of file, or ctx is done.
func Serve(ctx context.Context, in io.Reader, out io.Writer, opts Options) error {
	if opts.Debounce == 0 {
		opts.Debounce = 150 * time.Millisecond
	}
	log := opts.Log
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	s := &server{
		opts:      opts,
		out:       &writer{w: out},
		log:       log,
		docs:      map[string]*document{},
		published: map[string]string{},
		timer:     time.NewTimer(time.Hour),
	}
	s.timer.Stop()
	defer s.timer.Stop()

	done := make(chan struct{})
	defer close(done)
	msgs := make(chan *message)
	readErr := make(chan error, 1)
	go func() {
		r := bufio.NewReader(in)
		for {
			m, err := readMessage(r)
			if err != nil {
				readErr <- err
				return
			}
			select {
			case msgs <- m:
			case <-done:
				return
			}
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-readErr:
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		case m := <-msgs:
			s.handle(m)
			if s.exited {
				if !s.shutdown {
					return ErrExitWithoutShutdown
				}
				return nil
			}
		case <-s.timer.C:
			s.diagnose()
		}
	}
}

// handle dispatches one message.
func (s *server) handle(m *message) {
	if m.Error != nil && m.Method == "" && len(m.ID) == 0 {
		s.log.Warn("malformed message", "error", m.Error.Message)
		return
	}
	if m.Method == "" {
		return // a response; the server sends no requests
	}
	start := time.Now()
	if !m.isRequest() {
		s.notification(m.Method, m.Params)
		s.log.Debug("notification", "method", m.Method, "took", time.Since(start))
		return
	}
	result, rerr := s.request(m.Method, m.Params)
	if err := s.out.reply(m.ID, result, rerr); err != nil {
		s.log.Error("writing response", "error", err)
	}
	s.log.Debug("request", "method", m.Method, "took", time.Since(start))
}

// handler is a request handler.
type handler func(s *server, params json.RawMessage) (any, *rpcError)

// handlers lists the requests the server answers after initialize.
var handlers = map[string]handler{
	"textDocument/hover":          withParams((*server).hover),
	"textDocument/definition":     withParams((*server).definition),
	"textDocument/references":     withParams((*server).references),
	"textDocument/documentSymbol": withParams((*server).documentSymbols),
	"workspace/symbol":            withParams((*server).workspaceSymbols),
	"textDocument/formatting":     withParams((*server).formatting),
	"textDocument/completion":     withParams((*server).completion),
	"textDocument/signatureHelp":  withParams((*server).signatureHelp),
	"textDocument/prepareRename":  withParams((*server).prepareRename),
	"textDocument/rename":         withParams((*server).rename),
}

// withParams adapts a handler that takes decoded parameters.
func withParams[P any, R any](f func(*server, *P) (R, *rpcError)) handler {
	return func(s *server, raw json.RawMessage) (any, *rpcError) {
		var p P
		if len(raw) > 0 {
			if err := json.Unmarshal(raw, &p); err != nil {
				return nil, errorf(codeInvalidParams, "%v", err)
			}
		}
		return f(s, &p)
	}
}

func (s *server) request(method string, params json.RawMessage) (any, *rpcError) {
	switch {
	case method == "initialize":
		if s.initialized {
			return nil, errorf(codeInvalidRequest, "initialize sent twice")
		}
		var p InitializeParams
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, errorf(codeInvalidParams, "%v", err)
		}
		return s.initialize(&p)
	case !s.initialized:
		return nil, errorf(codeServerNotInitialized, "the server is not initialized")
	case s.shutdown:
		return nil, errorf(codeInvalidRequest, "the server is shutting down")
	case method == "shutdown":
		s.shutdown = true
		return nil, nil
	}
	h := handlers[method]
	if h == nil {
		return nil, errorf(codeMethodNotFound, "method %q not supported", method)
	}
	return s.safely(method, func() (any, *rpcError) { return h(s, params) })
}

// safely runs a handler, turning a panic into an error response so that
// one bad request does not end the session.
func (s *server) safely(method string, f func() (any, *rpcError)) (result any, rerr *rpcError) {
	defer func() {
		if r := recover(); r != nil {
			s.log.Error("handler panicked", "method", method, "panic", r)
			result, rerr = nil, errorf(codeInternalError, "%s failed: %v", method, r)
		}
	}()
	return f()
}

func (s *server) notification(method string, params json.RawMessage) {
	if method == "exit" {
		s.exited = true
		return
	}
	if !s.initialized || s.shutdown {
		return
	}
	decode := func(v any) bool {
		if err := json.Unmarshal(params, v); err != nil {
			s.log.Warn("bad notification parameters", "method", method, "error", err)
			return false
		}
		return true
	}
	switch method {
	case "initialized":
	case "textDocument/didOpen":
		var p DidOpenTextDocumentParams
		if decode(&p) {
			s.didOpen(&p)
		}
	case "textDocument/didChange":
		var p DidChangeTextDocumentParams
		if decode(&p) {
			s.didChange(&p)
		}
	case "textDocument/didSave":
		var p DidSaveTextDocumentParams
		if decode(&p) {
			s.didSave(&p)
		}
	case "textDocument/didClose":
		var p DidCloseTextDocumentParams
		if decode(&p) {
			s.didClose(&p)
		}
	case "workspace/didChangeConfiguration":
		var p DidChangeConfigurationParams
		if decode(&p) {
			s.didChangeConfiguration(&p)
		}
	case "workspace/didChangeWatchedFiles":
		var p DidChangeWatchedFilesParams
		if decode(&p) {
			s.didChangeWatchedFiles(&p)
		}
	case "workspace/didChangeWorkspaceFolders":
		var p DidChangeWorkspaceFoldersParams
		if decode(&p) {
			s.didChangeWorkspaceFolders(&p)
		}
	}
}

func (s *server) initialize(p *InitializeParams) (any, *rpcError) {
	if len(p.InitializationOptions) > 0 && string(p.InitializationOptions) != "null" {
		if err := json.Unmarshal(p.InitializationOptions, &s.settings); err != nil {
			return nil, errorf(codeInvalidParams, "initializationOptions: %v", err)
		}
	}
	modes, err := modesFor(s.settings)
	if err != nil {
		s.showMessage(1, fmt.Sprintf("Plutonium: %v; using the host target", err))
		modes, _ = modesFor(Settings{Tags: s.settings.Tags})
	}
	stdRoot := s.opts.StdlibDir
	if stdRoot == "" && s.opts.Stdlib != nil {
		cache := s.opts.CacheDir
		if cache == "" {
			if dir, err := os.UserCacheDir(); err == nil {
				cache = filepath.Join(dir, "pluto")
			} else {
				cache = filepath.Join(os.TempDir(), "pluto-cache")
			}
		}
		if stdRoot, err = extractStdlib(s.opts.Stdlib, cache); err != nil {
			s.log.Error("extracting the standard library", "error", err)
			s.showMessage(1, fmt.Sprintf("Plutonium: cannot extract the standard library: %v", err))
		}
	}
	s.ws = newWorkspace(stdRoot, modes)
	for _, f := range p.WorkspaceFolders {
		s.addRoot(f.URI)
	}
	if len(s.ws.roots) == 0 && p.RootURI != "" {
		s.addRoot(p.RootURI)
	}
	if len(s.ws.roots) == 0 && p.RootPath != "" {
		s.ws.roots = append(s.ws.roots, filepath.Clean(p.RootPath))
	}
	s.initialized = true
	s.log.Info("initialized", "stdlib", stdRoot, "roots", s.ws.roots, "mode", modes[0].name)
	return InitializeResult{
		ServerInfo: ServerInfo{Name: "pluto lsp", Version: s.opts.Version},
		Capabilities: ServerCapabilities{
			PositionEncoding:           "utf-16",
			TextDocumentSync:           TextDocumentSyncOptions{OpenClose: true, Change: 1, Save: SaveOptions{}},
			HoverProvider:              true,
			DefinitionProvider:         true,
			ReferencesProvider:         true,
			DocumentSymbolProvider:     true,
			WorkspaceSymbolProvider:    true,
			DocumentFormattingProvider: true,
			CompletionProvider:         CompletionOptions{TriggerCharacters: []string{"."}},
			SignatureHelpProvider:      SignatureHelpOptions{TriggerCharacters: []string{"(", ","}, RetriggerCharacters: []string{","}},
			RenameProvider:             RenameOptions{PrepareProvider: true},
			Workspace:                  WorkspaceServerCapabilty{WorkspaceFolders: WorkspaceFoldersCapability{Supported: true, ChangeNotifications: true}},
		},
	}, nil
}

func (s *server) addRoot(uri string) {
	if p := pathFromURI(uri); p != "" && !slices.Contains(s.ws.roots, p) {
		s.ws.roots = append(s.ws.roots, p)
	}
}

func (s *server) showMessage(typ int, msg string) {
	if err := s.out.notify("window/showMessage", ShowMessageParams{Type: typ, Message: msg}); err != nil {
		s.log.Error("writing notification", "error", err)
	}
}

// Document synchronisation.

func (s *server) didOpen(p *DidOpenTextDocumentParams) {
	path := pathFromURI(p.TextDocument.URI)
	if path == "" {
		return
	}
	s.docs[path] = &document{uri: p.TextDocument.URI, version: p.TextDocument.Version}
	s.setText(path, []byte(p.TextDocument.Text))
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
		if c.Range == nil {
			src = []byte(c.Text)
			continue
		}
		// A client that sends incremental changes despite full sync.
		t := newText(src)
		start, end := t.offset(c.Range.Start), t.offset(c.Range.End)
		src = slices.Concat(src[:start:start], []byte(c.Text), src[end:])
	}
	s.setText(path, src)
	s.timer.Reset(s.opts.Debounce)
}

func (s *server) didSave(p *DidSaveTextDocumentParams) {
	path := pathFromURI(p.TextDocument.URI)
	if p.Text != nil && s.docs[path] != nil {
		s.setText(path, []byte(*p.Text))
	}
	s.timer.Stop()
	s.diagnose()
}

func (s *server) didClose(p *DidCloseTextDocumentParams) {
	path := pathFromURI(p.TextDocument.URI)
	if s.docs[path] == nil {
		return
	}
	delete(s.docs, path)
	delete(s.ws.overlay, path)
	s.ws.invalidate(filepath.Dir(path))
	if !s.ws.inWorkspace(path) {
		s.publish(path, nil)
	}
	s.diagnose()
}

func (s *server) setText(path string, src []byte) {
	s.ws.overlay[path] = src
	s.ws.invalidate(filepath.Dir(path))
}

func (s *server) didChangeConfiguration(p *DidChangeConfigurationParams) {
	var wrapper struct {
		Plutonium *Settings `json:"plutonium"`
	}
	if err := json.Unmarshal(p.Settings, &wrapper); err != nil || wrapper.Plutonium == nil {
		return
	}
	modes, err := modesFor(*wrapper.Plutonium)
	if err != nil {
		s.showMessage(1, fmt.Sprintf("Plutonium: %v", err))
		return
	}
	s.settings = *wrapper.Plutonium
	s.ws.modes = modes
	s.ws.invalidateAll()
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
	s.timer.Reset(s.opts.Debounce)
}

func (s *server) didChangeWorkspaceFolders(p *DidChangeWorkspaceFoldersParams) {
	for _, f := range p.Event.Removed {
		path := pathFromURI(f.URI)
		s.ws.roots = slices.DeleteFunc(s.ws.roots, func(r string) bool { return r == path })
	}
	for _, f := range p.Event.Added {
		s.addRoot(f.URI)
	}
}

// fileAt returns the checked file of a document and the byte offset of a
// position in it.
func (s *server) fileAt(uri string, pos Position) (*srcFile, int, *rpcError) {
	path := pathFromURI(uri)
	if path == "" {
		return nil, 0, errorf(codeInvalidParams, "not a file URI: %s", uri)
	}
	f, _, err := s.ws.fileOf(path)
	if err != nil || f == nil {
		return nil, 0, nil
	}
	return f, f.offset(pos), nil
}
