// Ported from internal/build/ssatext_test.go at the go-cutover tag.

package build

import (
	"pluto/bytes"
	"pluto/os"
	"pluto/path/filepath"
	"pluto/strings"
	"pluto/testing"

	"compiler/compat"
	"compiler/compat/fs"
	"compiler/internal/backend"
	"compiler/internal/debuginfo"
	"compiler/internal/opt"
	"compiler/internal/ssatext"
	"compiler/pu"
)

// textBackend is the back end of the mixed pipeline written in Go: it reads
// a program from its text, optimises it at the given level and returns the
// object file. selfhost/demo/ssaback does the same with the Plutonium
// translation of these packages.
func textBackend(text []byte, level int) ([]byte, error) {
	p, fset := ssatext.Read(text) or return
	opt.ForLevel(level).Run(p, nil) or return
	obj, err := backend.Emit(pu.New(backend.Config{}), p, debuginfo.New(fset))
	if err != nil {
		return nil, err
	}
	buf := pu.Alloc[compat.Buffer]()
	obj.Write(buf) or return
	return buf.Bytes(), nil
}

// roundTrip lowers the program in dir, writes the SSA as text, reads it
// back and compiles it, and requires the object of the program that went
// through the text to equal the object of the original at level.
func roundTrip(t *testing.T, root, dir string, level int) {
	t.Helper()
	cfg := pu.New(Config{Dir: dir, Stdlib: fs.DirFS(filepath.Join(root, "lib")), OptLevel: level})
	l, err := Frontend(cfg)
	if err != nil {
		t.Fatal(err.Error())
	}
	text, err := l.Text(cfg)
	if err != nil {
		t.Fatal(err.Error())
	}
	got, err := textBackend(text, level)
	if err != nil {
		t.Fatal(err.Error())
	}
	want, err := l.Object(cfg)
	if err != nil {
		t.Fatal(err.Error())
	}
	if !bytes.Equal(got, want) {
		t.Errorf("the object differs: %d bytes from the text, %d from the program", len(got), len(want))
	}
}

// TestSSATextRoundTrip lowers programs, writes the SSA as text, reads it
// back and compiles it, and requires the object of the program that went
// through the text to equal the object of the original, at -O0 and -O2.
// (tools/go2pu does the same with the Plutonium back end, over the whole
// corpus.) The repository is found from $PLUTOROOT, its lib directory.
func TestSSATextRoundTrip(t *testing.T) {
	lib := compat.Getenv("PLUTOROOT")
	if lib == "" {
		t.Skip("PLUTOROOT is not set: it names the repository's lib directory")
	}
	root, err := filepath.Abs(filepath.Join(lib, ".."))
	if err != nil {
		t.Fatal(err.Error())
	}
	// The first six programs of testdata/run, in name order, and the hello
	// example.
	var names []string
	names = compat.Append(names, "")
	ents, err := fs.OSReadDir(filepath.Join(root, "testdata", "run"))
	if err != nil {
		t.Fatal(err.Error())
	}
	for _, e := range ents {
		if len(names) < 7 && strings.HasSuffix(e.Name(), ".p") {
			names = compat.Append(names, e.Name())
		}
	}
	for _, name := range names {
		dir := filepath.Join(root, "examples", "07-hello")
		testName := "07-hello"
		if name != "" {
			dir = os.MkdirTemp("", "ssatext") or {
				t.Fatal(err.Error())
				return
			}
			src := compat.ReadFile(filepath.Join(root, "testdata", "run", name)) or {
				t.Fatal(err.Error())
				return
			}
			os.WriteFile(filepath.Join(dir, "main.p"), src, 0o644) or {
				t.Fatal(err.Error())
				return
			}
			testName = name
		}
		t.Run(testName, func(t *testing.T) {
			t.Run("O0", func(t *testing.T) { roundTrip(t, root, dir, 0) })
			t.Run("O2", func(t *testing.T) { roundTrip(t, root, dir, 2) })
		})
		if name != "" {
			os.RemoveAll(dir) or {
				t.Fatal(err.Error())
				return
			}
		}
	}
}
