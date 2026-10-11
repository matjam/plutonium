package lsp

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/matjam/plutonium/internal/testguard"
)

// TestHoverPortable checks the hover text of the predeclared constraint
// portable (spec section 8.9), and that a type argument that is not portable
// is reported as a diagnostic.
func TestHoverPortable(t *testing.T) {
	testguard.Parallel(t)
	const src = `package main

type Msg struct {
	n    int
	name string
}

func Send[T portable](v T) {}

func main() {
	Send(Msg{})
}
`
	root := writeFiles(t, map[string]string{"pluto.mod": "module example.com/p\n", "main.p": src})
	c := startServer(t, Options{})
	c.initialize(root, nil)
	main := filepath.Join(root, "main.p")
	c.open(main)
	diags := c.diagnostics(main, func(d []Diagnostic) bool { return len(d) > 0 })
	if len(diags) != 1 || !strings.Contains(diags[0].Message, "Msg has field name of type string, which is not portable") {
		t.Errorf("diagnostics = %+v", diags)
	}
	var h *Hover
	c.result("textDocument/hover", posParams(main, at(t, main, "portable", 2)), &h)
	if h == nil {
		t.Fatal("no hover on portable")
	}
	for _, want := range []string{"portable", "constraint", "between address spaces", "section 8.9", "func Send[T portable](v T)"} {
		if !strings.Contains(h.Contents.Value, want) {
			t.Errorf("hover = %q, want it to contain %q", h.Contents.Value, want)
		}
	}
}
