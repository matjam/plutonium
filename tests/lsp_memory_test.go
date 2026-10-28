package tests

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// The language server frees what it makes for each message and each
// check (compiler/internal/lsp, Serve and workspace.check). These tests
// drive pluto lsp through a long editing session: TestLSPMemoryFlat holds
// its resident memory flat, and TestLSPScratchCheck runs a session with
// PLUTO_SCRATCH_CHECK=1, which makes freed memory inaccessible, so that a
// use of it ends the server.

const lspMain = `package main

import (
	"example.com/demo/util"
	"pluto/strings"
)

func main() {
	s := util.Greet("x")
	_ = strings.ToUpper(s)
	_ = len(s)
}
`

const lspUtil = `package util

type Box[T any] struct{ v T }

func (b Box[T]) Get() T { return b.v }

func Greet(name string) string {
	b := Box[string]{v: name}
	return "hello " + b.Get()
}
`

// An lspClient speaks JSON-RPC to a pluto lsp process.
type lspClient struct {
	t      *testing.T
	cmd    *exec.Cmd
	in     io.WriteCloser
	msgs   chan map[string]any
	nextID int
	stderr *strings.Builder
}

func startLSP(t *testing.T, env ...string) *lspClient {
	t.Helper()
	cmd := exec.Command(compiler(t), "lsp")
	cmd.Env = append(os.Environ(), "PLUTOROOT="+libRoot(t))
	cmd.Env = append(cmd.Env, env...)
	in, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	c := &lspClient{t: t, cmd: cmd, in: in, msgs: make(chan map[string]any, 64), stderr: &strings.Builder{}}
	cmd.Stderr = c.stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = in.Close()
		done := make(chan struct{})
		go func() { _ = cmd.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			_ = cmd.Process.Kill()
			<-done
		}
	})
	go c.readLoop(bufio.NewReader(out))
	return c
}

func (c *lspClient) readLoop(r *bufio.Reader) {
	defer close(c.msgs)
	for {
		length := -1
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			line = strings.TrimRight(line, "\r\n")
			if line == "" {
				break
			}
			if name, value, ok := strings.Cut(line, ":"); ok && strings.EqualFold(strings.TrimSpace(name), "Content-Length") {
				length, _ = strconv.Atoi(strings.TrimSpace(value))
			}
		}
		if length < 0 {
			return
		}
		body := make([]byte, length)
		if _, err := io.ReadFull(r, body); err != nil {
			return
		}
		var m map[string]any
		if err := json.Unmarshal(body, &m); err != nil {
			return
		}
		c.msgs <- m
	}
}

func (c *lspClient) send(m map[string]any) {
	c.t.Helper()
	m["jsonrpc"] = "2.0"
	body, err := json.Marshal(m)
	if err != nil {
		c.t.Fatal(err)
	}
	if _, err := fmt.Fprintf(c.in, "Content-Length: %d\r\n\r\n%s", len(body), body); err != nil {
		c.t.Fatalf("writing to the server: %v\n%s", err, c.stderr.String())
	}
}

func (c *lspClient) notify(method string, params any) {
	c.t.Helper()
	c.send(map[string]any{"method": method, "params": params})
}

// call sends a request and returns its response, skipping the server's
// notifications.
func (c *lspClient) call(method string, params any) map[string]any {
	c.t.Helper()
	c.nextID++
	id := c.nextID
	c.send(map[string]any{"id": id, "method": method, "params": params})
	timeout := time.After(2 * time.Minute)
	for {
		select {
		case m, ok := <-c.msgs:
			if !ok {
				c.t.Fatalf("the server ended during %s: %v\n%s", method, c.cmd.Wait(), c.stderr.String())
			}
			if got, ok := m["id"].(float64); ok && int(got) == id && m["method"] == nil {
				if e := m["error"]; e != nil {
					c.t.Fatalf("%s: %v", method, e)
				}
				return m
			}
		case <-timeout:
			c.t.Fatalf("no response to %s", method)
		}
	}
}

// rss returns the server's resident memory in kB.
func (c *lspClient) rss() int {
	c.t.Helper()
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", c.cmd.Process.Pid))
	if err != nil {
		c.t.Fatal(err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		if v, ok := strings.CutPrefix(line, "VmRSS:"); ok {
			n, err := strconv.Atoi(strings.TrimSuffix(strings.TrimSpace(v), " kB"))
			if err != nil {
				c.t.Fatal(err)
			}
			return n
		}
	}
	c.t.Fatal("no VmRSS")
	return 0
}

// position returns the LSP position of the first byte of the n-th
// occurrence (from 1) of what in text.
func position(text, what string, n int) map[string]any {
	off := -1
	for i := 0; i < n; i++ {
		j := strings.Index(text[off+1:], what)
		if j < 0 {
			panic("position: " + what)
		}
		off += 1 + j
	}
	line := strings.Count(text[:off], "\n")
	col := off - (strings.LastIndex(text[:off], "\n") + 1)
	return map[string]any{"line": line, "character": col}
}

// lspSession opens a module of two packages and runs cycles of editing:
// each cycle changes and saves main.p, which checks its package again
// and publishes diagnostics, and asks for hover, completion, definition
// and more. Now and then it edits, closes and reopens util.p, which
// checks both packages again, and changes the settings and folders.
// sample is called after the cycles it names.
func lspSession(t *testing.T, c *lspClient, cycles int, sample func(cycle int)) {
	dir := t.TempDir()
	write := func(name, text string) string {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
		return "file://" + p
	}
	write("pluto.mod", "module example.com/demo\n")
	mainURI := write("main.p", lspMain)
	utilURI := write("util/util.p", lspUtil)
	dirURI := "file://" + dir

	c.call("initialize", map[string]any{"processId": nil, "rootUri": dirURI, "capabilities": map[string]any{}})
	c.notify("initialized", map[string]any{})
	open := func(uri, text string, version int) {
		c.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{"uri": uri, "languageId": "plutonium", "version": version, "text": text}})
	}
	doc := func(uri string) map[string]any { return map[string]any{"uri": uri} }
	open(mainURI, lspMain, 1)
	open(utilURI, lspUtil, 1)

	for i := 1; i <= cycles; i++ {
		text := lspMain
		if i%2 == 1 {
			// An error, so that the diagnostics change every cycle.
			text = strings.Replace(text, "_ = len(s)", "var n int = s\n\t_ = n", 1)
		}
		text += fmt.Sprintf("\n// cycle %d\n", i)
		c.notify("textDocument/didChange", map[string]any{
			"textDocument":   map[string]any{"uri": mainURI, "version": i + 1},
			"contentChanges": []any{map[string]any{"text": text}},
		})
		c.notify("textDocument/didSave", map[string]any{"textDocument": doc(mainURI)})
		at := func(what string, n int) map[string]any {
			return map[string]any{"textDocument": doc(mainURI), "position": position(text, what, n)}
		}
		// The answers come from a checked package, so the session checks.
		for _, method := range []string{"textDocument/hover", "textDocument/definition"} {
			if r := c.call(method, at("Greet", 1)); r["result"] == nil {
				t.Fatalf("cycle %d: %s of Greet has no result", i, method)
			}
		}
		c.call("textDocument/completion", at("ToUpper", 1))
		c.call("textDocument/signatureHelp", at("\"x\"", 1))
		refs := at("s :=", 1)
		refs["context"] = map[string]any{"includeDeclaration": true}
		c.call("textDocument/references", refs)
		c.call("textDocument/documentSymbol", map[string]any{"textDocument": doc(mainURI)})
		if i%10 == 0 {
			c.call("textDocument/formatting", map[string]any{"textDocument": doc(mainURI), "options": map[string]any{"tabSize": 4, "insertSpaces": false}})
			c.call("workspace/symbol", map[string]any{"query": "Greet"})
			// Edit the imported package, then close and reopen it.
			util := strings.Replace(lspUtil, "hello ", fmt.Sprintf("hello %d ", i), 1)
			c.notify("textDocument/didChange", map[string]any{
				"textDocument":   map[string]any{"uri": utilURI, "version": i + 1},
				"contentChanges": []any{map[string]any{"text": util}},
			})
			c.call("textDocument/hover", map[string]any{"textDocument": doc(utilURI), "position": position(util, "Get", 2)})
			c.notify("textDocument/didClose", map[string]any{"textDocument": doc(utilURI)})
			open(utilURI, lspUtil, i+2)
		}
		if i%50 == 0 {
			c.notify("workspace/didChangeConfiguration", map[string]any{"settings": map[string]any{"plutonium": map[string]any{"tags": []string{fmt.Sprintf("t%d", i%3)}}}})
			c.notify("workspace/didChangeWorkspaceFolders", map[string]any{"event": map[string]any{
				"added":   []any{map[string]any{"uri": dirURI + "/util", "name": "util"}},
				"removed": []any{map[string]any{"uri": dirURI + "/util", "name": "util"}},
			}})
			c.notify("workspace/didChangeWatchedFiles", map[string]any{"changes": []any{map[string]any{"uri": dirURI + "/pluto.mod", "type": 2}}})
			c.call("textDocument/hover", at("ToUpper", 1))
		}
		if sample != nil {
			sample(i)
		}
	}
	c.call("shutdown", nil)
	c.notify("exit", nil)
	if err := waitExit(c); err != nil {
		t.Fatalf("the server did not exit cleanly: %v\n%s", err, c.stderr.String())
	}
}

func waitExit(c *lspClient) error {
	done := make(chan error, 1)
	go func() { done <- c.cmd.Wait() }()
	select {
	case err := <-done:
		return err
	case <-time.After(30 * time.Second):
		_ = c.cmd.Process.Kill()
		return fmt.Errorf("timed out")
	}
}

// TestLSPMemoryFlat holds the server's resident memory between the 100th
// and the 1000th cycle of a session to a small margin. A server that keeps
// what it makes for each message or check grows by several megabytes in
// that time.
func TestLSPMemoryFlat(t *testing.T) {
	if testing.Short() {
		t.Skip("a long session")
	}
	c := startLSP(t)
	var at100, at1000 int
	lspSession(t, c, 1000, func(i int) {
		switch i {
		case 100:
			at100 = c.rss()
		case 1000:
			at1000 = c.rss()
		}
	})
	t.Logf("resident memory: %d kB after 100 cycles, %d kB after 1000", at100, at1000)
	const marginKB = 8 << 10
	if at1000-at100 > marginKB {
		t.Errorf("resident memory grew by %d kB from cycle 100 to cycle 1000 (%d kB to %d kB); at most %d kB is allowed", at1000-at100, at100, at1000, marginKB)
	}
}

// TestLSPScratchCheck runs a session with the memory the server frees
// made inaccessible, so that a use of it faults.
func TestLSPScratchCheck(t *testing.T) {
	if testing.Short() {
		t.Skip("a long session")
	}
	c := startLSP(t, "PLUTO_SCRATCH_CHECK=1")
	lspSession(t, c, 120, nil)
}
