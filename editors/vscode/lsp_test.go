package vscode

import (
	"os"
	"os/exec"
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

// TestLanguageServerProtocol runs pluto lsp against
// vscode-jsonrpc, the library under vscode-languageclient
// (test/lsp-protocol-check.js), and then starts it through the extension
// with the installed vscode-languageclient (test/lsp-client-check.js). It
// is skipped when the extension's npm dependencies are not installed, and
// when there is no pluto to run (see plutoBinary).
func TestLanguageServerProtocol(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node is not installed")
	}
	if _, err := os.Stat("node_modules/vscode-jsonrpc"); err != nil {
		t.Skip("the extension's npm dependencies are not installed (npm ci)")
	}
	pluto := plutoBinary(t)
	if code := node(t, "test/lsp-protocol-check.js", pluto); code != 0 {
		t.Errorf("lsp-protocol-check.js: exit status %d", code)
	}
	if code := node(t, "test/lsp-client-check.js", pluto); code != 0 {
		t.Errorf("lsp-client-check.js: exit status %d", code)
	}
}
