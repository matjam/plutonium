package tests

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The tests in this file check pluto vet on testdata/vet and pluto doc on
// testdata/doc.

func TestVetUnsafeCommand(t *testing.T) {
	t.Parallel()
	dir := writeProgram(t, "package sample\n//pu:nochecks\nfunc raw() {}\nfunc safe() { raw() }\n")
	out, status := runPluto(t, dir, nil, "vet", "-unsafe")
	want := "package main\n  unsafe main.raw ./main.p:3:1\n    ./main.p:2:1: //pu:nochecks\n  safe main.safe ./main.p:4:1\n    calls main.raw\n      ./main.p:4:15: call main.raw\n"
	if status != 0 || out != want {
		t.Fatalf("status %d:\n%s", status, out)
	}
}

// goldenCommand runs pluto with args in dir and compares the output with
// dir/name.out and the exit status with status.
func goldenCommand(t *testing.T, dir, name string, status int, args ...string) {
	t.Helper()
	out, got := runPluto(t, dir, nil, args...)
	file := filepath.Join(dir, name+".out")
	if *update {
		if err := os.WriteFile(file, []byte(out), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if got != status || out != string(want) {
		t.Fatalf("status %d want %d; output:\n%s\nwant:\n%s", got, status, out, want)
	}
}

func TestVetGolden(t *testing.T) {
	t.Parallel()
	dir, err := filepath.Abs("testdata/vet")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		args   []string
		status int
	}{
		{"default", []string{"vet"}, 0},
		{"pattern", []string{"vet", "-unsafe", "./..."}, 0},
		{"tags", []string{"vet", "-unsafe", "-tags=auditextra"}, 0},
		{"freestanding", []string{"vet", "-unsafe", "-mode=freestanding"}, 0},
		{"kernel", []string{"vet", "-unsafe", "-mode=kernel"}, 0},
		{"bad_mode", []string{"vet", "-mode=bad"}, 2},
		{"bad_flag", []string{"vet", "-unknown"}, 2},
		{"stdlib", []string{"vet", "-unsafe", "pluto/bits"}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			goldenCommand(t, dir, tc.name, tc.status, tc.args...)
		})
	}
}

func TestVetSourceErrors(t *testing.T) {
	t.Parallel()
	for _, brief := range []bool{false, true} {
		mode := map[bool]string{false: "default", true: "brief"}[brief]
		t.Run(mode, func(t *testing.T) {
			dir := writeProgram(t, "package sample\nfunc f() { unknown() }\n")
			args := []string{"vet", "-unsafe"}
			if brief {
				args = append(args, "-brief")
			}
			out, status := runPluto(t, dir, nil, args...)
			// Diagnostics use the same formatter and catalog as build; the
			// exact source filename is local to this fixture.
			name := filepath.Join("testdata/vet", "error_"+mode+".out")
			out = strings.ReplaceAll(out, dir, ".")
			if *update {
				if err := os.WriteFile(name, []byte(out), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(name)
			if err != nil {
				t.Fatal(err)
			}
			if status != 1 || out != string(want) {
				t.Fatalf("status %d output:\n%s\nwant:\n%s", status, out, want)
			}
		})
	}
}

func TestDocGolden(t *testing.T) {
	t.Parallel()
	dir, err := filepath.Abs("testdata/doc")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		args   []string
		status int
	}{
		{"package", []string{"doc"}, 0},
		{"all", []string{"doc", "-all"}, 0},
		{"type", []string{"doc", ".", "Buffer"}, 0},
		{"method", []string{"doc", ".", "Buffer.Len"}, 0},
		{"alias_method", []string{"doc", ".", "BufferAlias.Len"}, 0},
		{"pointer_alias_method", []string{"doc", ".", "PointerAlias.Len"}, 0},
		{"promoted_method", []string{"doc", ".", "Larger.Len"}, 0},
		{"imported_method", []string{"doc", ".", "Foreign.Sum32"}, 0},
		{"instantiated_method", []string{"doc", ".", "StringGeneric.Get"}, 0},
		{"interface_method", []string{"doc", ".", "Reader.Read"}, 0},
		{"alias_type", []string{"doc", ".", "BufferAlias"}, 0},
		{"generic_alias_type", []string{"doc", ".", "StringGeneric"}, 0},
		{"private", []string{"doc", ".", "Buffer.consume"}, 0},
		{"constant", []string{"doc", ".", "Second"}, 0},
		{"alias", []string{"doc", ".", "OwnAlias"}, 0},
		{"tags", []string{"doc", "-tags=docextra", ".", "Extra"}, 0},
		{"kernel", []string{"doc", "-mode=kernel", ".", "Hardware"}, 0},
		{"freestanding", []string{"doc", "-mode=freestanding", ".", "Color"}, 0},
		{"module", []string{"doc", "example.com/docs", "Choice"}, 0},
		{"missing", []string{"doc", ".", "NotHere"}, 1},
		{"missing_method", []string{"doc", ".", "Buffer.NotHere"}, 1},
		{"test_only", []string{"doc", ".", "TestOnly"}, 1},
		{"bad_flag", []string{"doc", "-unknown"}, 2},
		{"bad_mode", []string{"doc", "-mode=bad"}, 2},
		{"bad_args", []string{"doc", ".", "Buffer", "Len"}, 2},
		{"help", []string{"doc", "-help"}, 0},
		{"stdlib", []string{"doc", "pluto/hash", "FNV32a"}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			goldenCommand(t, dir, tc.name, tc.status, tc.args...)
		})
	}
}

func TestDocSourceErrors(t *testing.T) {
	t.Parallel()
	dir := writeProgram(t, "package sample\nvar Broken = unknown\n")
	out, status := runPluto(t, dir, nil, "doc", "-brief")
	want := filepath.Join(dir, "main.p") + ":2:14: undefined: unknown\n"
	if status != 1 || out != want {
		t.Fatalf("status %d: %q, want %q", status, out, want)
	}
}

// TestDocLibrarySources checks that pluto doc reads the standard library
// from $PLUTOROOT when it is set, and finds its own without it.
func TestDocLibrarySources(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	// A supplied PLUTOROOT contains a distinct library implementation.
	root := t.TempDir()
	pkg := filepath.Join(root, "pluto", "sample")
	if err := os.MkdirAll(pkg, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkg, "sample.p"), []byte("// Package sample comes from PLUTOROOT.\npackage sample\ntype Text string\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ root, target, symbol, want string }{
		{root, "pluto/sample", "", "package sample // import \"pluto/sample\"\n\nPackage sample comes from PLUTOROOT.\n\nText [counted]\ntype Text string\n"},
		{"", "pluto/bits", "Len", "package bits // import \"pluto/bits\"\n\nfunc Len(x uint) int\n    Len returns the minimum number of bits to represent x.\n"},
	} {
		args := []string{"doc", tc.target}
		if tc.symbol != "" {
			args = append(args, tc.symbol)
		}
		out, status := runPluto(t, dir, []string{"PLUTOROOT=" + tc.root}, args...)
		if status != 0 || out != tc.want {
			t.Fatalf("%s: status %d: %q, want %q", tc.target, status, out, tc.want)
		}
	}
}
