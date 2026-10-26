package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	plutonium "github.com/matjam/plutonium"
	"github.com/matjam/plutonium/internal/build"
	"github.com/matjam/plutonium/internal/diag"
	"github.com/matjam/plutonium/internal/syntax/scanner"
)

// The diagnostics demo (testdata/diagdemo) prints the diagnostics of the
// programs of tests/diag, rendered from records of what the
// compiler made of them. TestDiagDemoRecords builds each program, writes the
// records to testdata/diagdemo/records.go (GO2PU_UPDATE=1) or checks that
// the file is current, and checks that rendering the records gives the
// program's golden output: the rendered text the demo prints is the text of
// the golden files.

const diagGoldens = "../../tests/diag"

// diagRecords builds the programs under diagGoldens and returns the encoded
// records of their errors, a program after another, as records.go holds
// them. The programs whose golden output is not what rendering the records
// gives are listed in skipped.
func diagRecords(t *testing.T) (encoded string, kept, skipped []string) {
	t.Helper()
	dirs, err := filepath.Glob(diagGoldens + "/*")
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(dirs)
	var progs []string
	for _, src := range dirs {
		name := filepath.Base(src)
		if _, err := os.Stat(filepath.Join(src, "command")); err == nil {
			skipped = append(skipped, name+" (another command)")
			continue
		}
		cfg := &build.Config{Output: filepath.Join(t.TempDir(), "a.out"), Stdlib: plutonium.Stdlib(), NoDWARF: true, NoTrace: true, NoTrimPath: true}
		if flags, err := os.ReadFile(filepath.Join(src, "flags")); err == nil {
			switch strings.Join(strings.Fields(string(flags)), " ") {
			case "--mode kernel":
				cfg.Freestanding, cfg.Kernel = true, true
			case "--mode freestanding":
				cfg.Freestanding = true
			default:
				skipped = append(skipped, name+" (flags)")
				continue
			}
		}
		dir := copyForBuild(t, src)
		cfg.Dir = dir
		err := build.Build(cfg)
		if err == nil {
			t.Fatalf("%s: built", name)
		}
		var recs []*record
		for _, d := range flatten(err) {
			recs = append(recs, d.sanitized(dir))
		}
		// Rendering the records must give the golden output.
		var out bytes.Buffer
		p := &diag.Printer{W: &out}
		for _, r := range recs {
			r.print(p)
		}
		want, err := os.ReadFile(filepath.Join(src, "default.out"))
		if err != nil {
			t.Fatal(err)
		}
		if out.String() != string(want) {
			skipped = append(skipped, name+" (the golden output differs from the records)")
			continue
		}
		kept = append(kept, name)
		var b strings.Builder
		b.WriteString(name + "\x1c")
		for _, r := range recs {
			b.WriteString(r.encode() + "\x1e")
		}
		progs = append(progs, b.String())
	}
	return strings.Join(progs, "\x1d"), kept, skipped
}

// A record is one error, as the demo reads it.
type record struct {
	d     *diag.Diagnostic
	plain bool
}

func flatten(err error) []*record {
	switch e := err.(type) {
	case nil:
		return nil
	case *diag.Diagnostic:
		return []*record{{d: e}}
	case *diag.List:
		var rs []*record
		for _, d := range *e {
			rs = append(rs, &record{d: d})
		}
		return rs
	case *scanner.ErrorList:
		var rs []*record
		for _, se := range *e {
			rs = append(rs, &record{d: diag.FromSyntax(se, nil)})
		}
		return rs
	case interface{ Unwrap() []error }:
		var rs []*record
		for _, c := range e.Unwrap() {
			rs = append(rs, flatten(c)...)
		}
		return rs
	}
	return []*record{{d: &diag.Diagnostic{Msg: err.Error()}, plain: true}}
}

func (r *record) print(p *diag.Printer) {
	if r.plain {
		p.Print(errors.New(r.d.Msg))
	} else {
		p.Print(r.d)
	}
}

// sanitized returns a copy of r with the directory dir in its strings
// replaced by $DIR, as the golden files have it.
func (r *record) sanitized(dir string) *record {
	fix := func(s string) string { return strings.ReplaceAll(s, dir, "$DIR") }
	d := *r.d
	d.Pos.Filename, d.Msg, d.Text = fix(d.Pos.Filename), fix(d.Msg), fix(d.Text)
	fixes := func(f *diag.Fix) *diag.Fix {
		if f == nil {
			return nil
		}
		return &diag.Fix{Intro: fix(f.Intro), Code: fix(f.Code)}
	}
	d.Fix = fixes(d.Fix)
	d.More = nil
	for _, f := range r.d.More {
		d.More = append(d.More, fixes(f))
	}
	return &record{d: &d, plain: r.plain}
}

// encode writes r as testdata/diagdemo's decode reads it.
func (r *record) encode() string {
	d := r.d
	kind := "d"
	if r.plain {
		kind = "p"
	}
	f := []string{kind, d.Pos.Filename, strconv.Itoa(d.Pos.Offset), strconv.Itoa(d.Pos.Line), strconv.Itoa(d.Pos.Column),
		string(d.Code), d.Msg, d.Text}
	fixes := append([]*diag.Fix{d.Fix}, d.More...)
	if d.Fix == nil && len(d.More) == 0 {
		fixes = nil
	}
	f = append(f, strconv.Itoa(len(fixes)))
	for _, fix := range fixes {
		if fix == nil {
			f = append(f, "0", "", "")
		} else {
			f = append(f, "1", fix.Intro, fix.Code)
		}
	}
	return strings.Join(f, "\x1f")
}

// copyForBuild copies the program src (and the packages under it) to a new
// directory.
func copyForBuild(t *testing.T, src string) string {
	t.Helper()
	dir := t.TempDir()
	var cp func(from, to string)
	cp = func(from, to string) {
		entries, err := os.ReadDir(from)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			name := e.Name()
			if e.IsDir() {
				if err := os.Mkdir(filepath.Join(to, name), 0o755); err != nil {
					t.Fatal(err)
				}
				cp(filepath.Join(from, name), filepath.Join(to, name))
				continue
			}
			if strings.HasSuffix(name, ".out") || name == "flags" || name == "command" {
				continue
			}
			data, err := os.ReadFile(filepath.Join(from, name))
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(to, name), data, 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	cp(src, dir)
	return dir
}

func TestDiagDemoRecords(t *testing.T) {
	encoded, kept, skipped := diagRecords(t)
	for _, s := range skipped {
		t.Logf("skipped: %s", s)
	}
	if len(kept) < 80 {
		t.Fatalf("only %d programs have records", len(kept))
	}
	src := "// Code generated by TestDiagDemoRecords (GO2PU_UPDATE=1); DO NOT EDIT.\n\n" +
		"package main\n\n// records holds the errors of the programs of tests/diag: " + strconv.Itoa(len(kept)) + " programs.\n" +
		"const records = " + strconv.Quote(encoded) + "\n"
	file := "testdata/diagdemo/records.go"
	if os.Getenv("GO2PU_UPDATE") != "" {
		if err := os.WriteFile(file, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	got, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("%v (run with GO2PU_UPDATE=1)", err)
	}
	if string(got) != src {
		t.Errorf("%s is stale: run this test with GO2PU_UPDATE=1", file)
	}
}
