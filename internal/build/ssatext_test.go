package build

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	plutonium "github.com/matjam/plutonium"
	"github.com/matjam/plutonium/internal/backend"
	"github.com/matjam/plutonium/internal/debuginfo"
	"github.com/matjam/plutonium/internal/opt"
	"github.com/matjam/plutonium/internal/ssatext"
	"github.com/matjam/plutonium/internal/testguard"
)

// TextBackend is the back end of the mixed pipeline written in Go: it reads
// a program from its text, optimises it at the given level and returns the
// object file. selfhost/demo/ssaback does the same with the Plutonium
// translation of these packages.
func textBackend(text []byte, level int) ([]byte, error) {
	p, fset, err := ssatext.Read(text)
	if err != nil {
		return nil, err
	}
	if err := opt.ForLevel(level).Run(p, nil); err != nil {
		return nil, err
	}
	obj, err := backend.Emit(&backend.Config{}, p, debuginfo.New(fset))
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := obj.Write(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// TestSSATextRoundTrip lowers programs, writes the SSA as text, reads it
// back and compiles it, and requires the object of the program that went
// through the text to equal the object of the original, at -O0 and -O2.
// (tools/go2pu does the same with the Plutonium back end, over the whole
// corpus.)
func TestSSATextRoundTrip(t *testing.T) {
	dirs := []string{"../../examples/07-hello"}
	runs, _ := filepath.Glob("testdata/run/*.p")
	dirs = append(dirs, runs[:min(len(runs), 6)]...)
	for _, d := range dirs {
		t.Run(filepath.Base(d), func(t *testing.T) {
			testguard.Parallel(t)
			dir := d
			if filepath.Ext(d) == ".p" {
				dir = t.TempDir()
				src, err := os.ReadFile(d)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, "main.p"), src, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			for _, level := range []int{0, 2} {
				t.Run(fmt.Sprintf("O%d", level), func(t *testing.T) {
					cfg := &Config{Dir: dir, Stdlib: plutonium.Stdlib(), OptLevel: level}
					l, err := Frontend(cfg)
					if err != nil {
						t.Fatal(err)
					}
					text, err := l.Text(cfg)
					if err != nil {
						t.Fatal(err)
					}
					got, err := textBackend(text, level)
					if err != nil {
						t.Fatal(err)
					}
					want, err := l.Object(cfg)
					if err != nil {
						t.Fatal(err)
					}
					if !bytes.Equal(got, want) {
						t.Errorf("the object differs: %d bytes from the text, %d from the program", len(got), len(want))
					}
				})
			}
		})
	}
}
