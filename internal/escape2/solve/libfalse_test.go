package solve_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/matjam/plutonium/internal/escape2"
)

// Library false rejections found by the both-mode comparison of the corpus
// (piece E's triage): each program is accepted by the old analysis and
// must be by this one, with no error in the library line named.
var libraryControls = []struct {
	name, src, line string
}{
	{
		// fmt's printer.to asserts any(dst) to *vec.Fixed[byte]: the
		// result is a direct value, never the frame box a non-direct
		// instance of D makes (fmt.p:989).
		"fmt_println", `package main

import "pluto/fmt"

func main() { fmt.Println("hi", 1) }
`, "fmt.p:989",
	},
}

func TestLibraryControls(t *testing.T) {
	if testing.Short() {
		t.Skip("loads the library")
	}
	for _, c := range libraryControls {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			f := filepath.Join(dir, "main.p")
			if err := os.WriteFile(f, []byte(c.src), 0o644); err != nil {
				t.Fatal(err)
			}
			err := escape2.Check(loadFile(t, f))
			if err != nil && strings.Contains(err.Error(), c.line) {
				t.Errorf("rejected at %s:\n%v", c.line, err)
			}
		})
	}
}
