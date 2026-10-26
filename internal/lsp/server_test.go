package lsp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// A client drives a server over pipes, as an editor would.
type client struct {
	t      *testing.T
	w      *writer
	mu     sync.Mutex
	nextID int
	waits  map[string]chan *message // by request ID
	notes  chan *message            // notifications from the server
	served chan error               // Serve's result
	stdlib string                   // the standard library directory
}

// libDir is the repository's standard library.
func libDir(t testing.TB) string {
	t.Helper()
	dir, err := filepath.Abs("../../lib")
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

// startServer starts a server with opts (the repository's library unless
// opts says otherwise) and returns a client for it.
func startServer(t *testing.T, opts Options) *client {
	t.Helper()
	if opts.StdlibDir == "" && opts.Stdlib == nil {
		opts.StdlibDir = libDir(t)
	}
	if opts.Debounce == 0 {
		opts.Debounce = 5 * time.Millisecond
	}
	c := &client{
		t:      t,
		waits:  map[string]chan *message{},
		notes:  make(chan *message, 1000),
		served: make(chan error, 1),
		stdlib: opts.StdlibDir,
	}
	var fromServer io.ReadCloser
	if compiler := os.Getenv("PLUTO_TEST_COMPILER"); compiler != "" {
		fromServer = c.exec(compiler, opts)
	} else {
		toServer, fromClient := io.Pipe()
		pr, toClient := io.Pipe()
		fromServer = pr
		c.w = &writer{w: fromClient}
		ctx, cancel := context.WithCancel(context.Background())
		go func() {
			c.served <- Serve(ctx, toServer, toClient, opts)
			_ = toClient.Close()
		}()
		t.Cleanup(func() {
			cancel()
			_ = fromClient.Close()
		})
	}
	go func() {
		r := bufio.NewReader(fromServer)
		for {
			m, err := readMessage(r)
			if err != nil {
				return
			}
			if m.Method != "" {
				c.notes <- m
				continue
			}
			c.mu.Lock()
			ch := c.waits[string(m.ID)]
			delete(c.waits, string(m.ID))
			c.mu.Unlock()
			if ch != nil {
				ch <- m
			}
		}
	}()
	t.Cleanup(func() { _ = fromServer.Close() })
	return c
}

// exec starts the server as the pluto lsp of compiler, such as the
// self-hosted compiler, with the library of opts as $PLUTOROOT, and
// returns its standard output. Serve's result is the process's: nil for
// status 0, and ErrExitWithoutShutdown for status 1 with that error on
// standard error.
func (c *client) exec(compiler string, opts Options) io.ReadCloser {
	t := c.t
	t.Helper()
	if opts.StdlibDir == "" {
		t.Skip("the pluto binary has no embedded library to extract")
	}
	args := []string{"lsp", fmt.Sprintf("--debounce=%dms", max(opts.Debounce.Milliseconds(), 1))}
	if opts.Log != nil {
		args = append(args, "-v")
	}
	cmd := exec.Command(compiler, args...)
	cmd.Env = append(os.Environ(), "PLUTOROOT="+opts.StdlibDir)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	// A pipe of our own rather than StdoutPipe, which Wait closes: the
	// reader sees all the output and then the end of it.
	stdout, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stdout = w
	err = cmd.Start()
	_ = w.Close()
	if err != nil {
		t.Fatal(err)
	}
	c.w = &writer{w: stdin}
	exited := make(chan struct{})
	go func() {
		defer close(exited)
		err := cmd.Wait()
		var exit *exec.ExitError
		switch {
		case err == nil:
		case errors.As(err, &exit) && exit.ExitCode() == 1 && strings.Contains(stderr.String(), ErrExitWithoutShutdown.Error()):
			err = ErrExitWithoutShutdown
		default:
			err = fmt.Errorf("%s lsp: %v\n%s", compiler, err, stderr.String())
		}
		c.served <- err
	}()
	t.Cleanup(func() {
		_ = stdin.Close()
		select {
		case <-exited:
		case <-time.After(10 * time.Second):
			_ = cmd.Process.Kill()
			<-exited
		}
		if opts.Log == nil && stderr.Len() > 0 {
			t.Logf("%s lsp stderr:\n%s", compiler, stderr.String())
		}
	})
	return stdout
}

// call sends a request and returns its response.
func (c *client) call(method string, params any) *message {
	c.t.Helper()
	c.mu.Lock()
	c.nextID++
	id := c.nextID
	ch := make(chan *message, 1)
	idJSON, _ := json.Marshal(id)
	c.waits[string(idJSON)] = ch
	c.mu.Unlock()
	if err := c.w.send(struct {
		JSONRPC string `json:"jsonrpc"`
		ID      int    `json:"id"`
		Method  string `json:"method"`
		Params  any    `json:"params"`
	}{"2.0", id, method, params}); err != nil {
		c.t.Fatalf("%s: %v", method, err)
	}
	select {
	case m := <-ch:
		return m
	case <-time.After(30 * time.Second):
		c.t.Fatalf("%s: no response", method)
	}
	return nil
}

// result calls a method that must succeed and decodes its result.
func (c *client) result(method string, params, result any) {
	c.t.Helper()
	m := c.call(method, params)
	if m.Error != nil {
		c.t.Fatalf("%s: %v", method, m.Error)
	}
	if err := json.Unmarshal(m.Result, result); err != nil {
		c.t.Fatalf("%s: decoding %s: %v", method, m.Result, err)
	}
}

func (c *client) notify(method string, params any) {
	c.t.Helper()
	if err := c.w.notify(method, params); err != nil {
		c.t.Fatalf("%s: %v", method, err)
	}
}

// initialize runs the handshake with root as the workspace folder.
func (c *client) initialize(root string, settings *Settings) InitializeResult {
	c.t.Helper()
	params := map[string]any{
		"processId":        nil,
		"rootUri":          uriFromPath(root),
		"workspaceFolders": []WorkspaceFolder{{URI: uriFromPath(root), Name: "root"}},
	}
	if settings != nil {
		params["initializationOptions"] = settings
	}
	var res InitializeResult
	c.result("initialize", params, &res)
	c.notify("initialized", struct{}{})
	return res
}

// open opens a file of the workspace with its contents on disk.
func (c *client) open(path string) {
	c.t.Helper()
	src, err := os.ReadFile(path)
	if err != nil {
		c.t.Fatal(err)
	}
	c.notify("textDocument/didOpen", DidOpenTextDocumentParams{TextDocument: TextDocumentItem{
		URI: uriFromPath(path), LanguageID: "plutonium", Version: 1, Text: string(src),
	}})
}

// diagnostics waits for the diagnostics of a file that satisfy ok.
func (c *client) diagnostics(path string, ok func([]Diagnostic) bool) []Diagnostic {
	c.t.Helper()
	timeout := time.After(30 * time.Second)
	for {
		select {
		case m := <-c.notes:
			if m.Method != "textDocument/publishDiagnostics" {
				c.t.Logf("notification %s %s", m.Method, m.Params)
				continue
			}
			var p PublishDiagnosticsParams
			if err := json.Unmarshal(m.Params, &p); err != nil {
				c.t.Fatal(err)
			}
			if pathFromURI(p.URI) == path {
				if ok(p.Diagnostics) {
					return p.Diagnostics
				}
				c.t.Logf("diagnostics for %s: %+v", filepath.Base(path), p.Diagnostics)
			}
		case <-timeout:
			c.t.Fatalf("no matching diagnostics for %s", path)
		}
	}
}

// shutdown ends the session cleanly.
func (c *client) shutdown() {
	c.t.Helper()
	if m := c.call("shutdown", nil); m.Error != nil {
		c.t.Fatal(m.Error)
	}
	c.notify("exit", nil)
	select {
	case err := <-c.served:
		if err != nil {
			c.t.Fatalf("Serve: %v", err)
		}
	case <-time.After(10 * time.Second):
		c.t.Fatal("server did not exit")
	}
}

// writeFiles writes files (path relative to a new directory: contents)
// and returns the directory.
func writeFiles(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	root, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	for name, src := range files {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// at returns the position of the n'th byte of the first occurrence of
// marker in the file at path.
func at(t *testing.T, path, marker string, n int) Position {
	t.Helper()
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	i := strings.Index(string(src), marker)
	if i < 0 {
		t.Fatalf("%q not in %s", marker, path)
	}
	return newText(src).position(i + n)
}

func posParams(path string, pos Position) TextDocumentPositionParams {
	return TextDocumentPositionParams{TextDocument: TextDocumentIdentifier{URI: uriFromPath(path)}, Position: pos}
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
	if m := c.call("textDocument/hover", nil); m.Error == nil || m.Error.Code != codeServerNotInitialized {
		t.Errorf("hover before initialize: got %+v, want error %d", m.Error, codeServerNotInitialized)
	}
	res := c.initialize(t.TempDir(), nil)
	caps := res.Capabilities
	if !caps.HoverProvider || !caps.DefinitionProvider || !caps.ReferencesProvider || !caps.DocumentSymbolProvider ||
		!caps.WorkspaceSymbolProvider || !caps.DocumentFormattingProvider || !caps.RenameProvider.PrepareProvider ||
		caps.TextDocumentSync.Change != 1 || len(caps.CompletionProvider.TriggerCharacters) == 0 {
		t.Errorf("capabilities = %+v", caps)
	}
	if m := c.call("initialize", map[string]any{}); m.Error == nil {
		t.Error("second initialize succeeded")
	}
	if m := c.call("no/such/method", nil); m.Error == nil || m.Error.Code != codeMethodNotFound {
		t.Errorf("unknown method: got %+v", m.Error)
	}
	c.shutdown()
}

func TestExitWithoutShutdown(t *testing.T) {
	c := startServer(t, Options{})
	c.initialize(t.TempDir(), nil)
	c.notify("exit", nil)
	if err := <-c.served; !errors.Is(err, ErrExitWithoutShutdown) {
		t.Errorf("Serve = %v, want %v", err, ErrExitWithoutShutdown)
	}
}

func TestServeEOF(t *testing.T) {
	// A disconnected client ends the server cleanly without requiring
	// an exit notification. No cancellation races with the input EOF.
	if compiler := os.Getenv("PLUTO_TEST_COMPILER"); compiler != "" {
		cmd := exec.Command(compiler, "lsp")
		cmd.Env = append(os.Environ(), "PLUTOROOT="+libDir(t))
		cmd.Stdin = strings.NewReader("")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%s lsp after client EOF = %v\n%s", compiler, err, out)
		}
		return
	}
	if err := Serve(context.Background(), strings.NewReader(""), io.Discard, Options{StdlibDir: libDir(t)}); err != nil {
		t.Fatalf("Serve after client EOF = %v", err)
	}
}
