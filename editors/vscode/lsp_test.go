package vscode

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestLanguageServerClient checks how the extension starts, stops and
// restarts the language server, against stand-ins for VS Code and
// vscode-languageclient (test/lsp-check.js).
func TestLanguageServerClient(t *testing.T) {
	if code := node(t, "test/lsp-check.js"); code != 0 {
		t.Errorf("lsp-check.js: exit status %d", code)
	}
}

// TestLanguageServerProtocol runs a freshly built pluto lsp against
// vscode-jsonrpc, the library under vscode-languageclient
// (test/lsp-protocol-check.js). It is skipped when the extension's npm
// dependencies are not installed.
func TestLanguageServerProtocol(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node is not installed")
	}
	if _, err := os.Stat("node_modules/vscode-jsonrpc"); err != nil {
		t.Skip("the extension's npm dependencies are not installed (npm ci)")
	}
	pluto := filepath.Join(t.TempDir(), "pluto")
	build := exec.Command("go", "build", "-o", pluto, "github.com/matjam/plutonium/cmd/pluto")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building pluto: %v\n%s", err, out)
	}
	if code := node(t, "test/lsp-protocol-check.js", pluto); code != 0 {
		t.Errorf("lsp-protocol-check.js: exit status %d", code)
	}
}
