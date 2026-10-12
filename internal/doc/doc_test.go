package doc

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/matjam/plutonium/internal/load"
	"github.com/matjam/plutonium/internal/syntax/format"
	"github.com/matjam/plutonium/internal/types2"
)

func TestSourceUnchanged(t *testing.T) {
	t.Parallel()
	fs := fstest.MapFS{"pluto/sample/source.p": {Data: []byte(`// Package sample has documentation.
package sample
// Constants share documentation.
const ( First = iota; Second )
const Typed int = 3
type Generic[T any] struct { Value T }
// Value keeps its source documentation.
func (g *Generic[T]) ValueAt(n int = 1) T { return g.Value }
var Public, private = 1, 2
`)}}
	prog, err := load.LoadPackage(&load.Config{Stdlib: fs, Sizes: types2.SizesFor("gc", "amd64")}, "pluto/sample")
	if err != nil {
		t.Fatal(err)
	}
	source := func() string {
		var out bytes.Buffer
		for _, f := range prog.Main.Files {
			if err := format.Node(&out, prog.Fset, f); err != nil {
				t.Fatal(err)
			}
		}
		return out.String()
	}
	before := source()
	var first bytes.Buffer
	if err := Write(&first, prog, "", true); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(first.String(), "const Typed int = 3") {
		t.Fatal(first.String())
	}
	if !strings.Contains(first.String(), "func (g *Generic[T]) ValueAt(n int = 1) T") {
		t.Fatal(first.String())
	}
	for _, symbol := range []string{"Generic", "Generic.ValueAt", "private", "Typed"} {
		if err := Write(io.Discard, prog, symbol, false); err != nil {
			t.Fatal(err)
		}
	}
	var again bytes.Buffer
	if err := Write(&again, prog, "", true); err != nil {
		t.Fatal(err)
	}
	if source() != before || first.String() != again.String() {
		t.Fatal("documentation changed shared source or output")
	}
	if err := Write(io.Discard, prog, "Generic.Missing", false); err == nil {
		t.Fatal("missing method succeeded")
	}
	if err := Write(failingWriter{}, prog, "", false); !errors.Is(err, errWrite) {
		t.Fatalf("write error = %v", err)
	}
	if err := Write(shortWriter{}, prog, "", false); !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("short write = %v", err)
	}
}

var errWrite = errors.New("writer failed")

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errWrite }

type shortWriter struct{}

func (shortWriter) Write(b []byte) (int, error) { return len(b) - 1, nil }
