package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

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
			out, status := runPluto(t, dir, tc.args...)
			name := filepath.Join(dir, tc.name+".out")
			if *update {
				if err := os.WriteFile(name, []byte(out), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(name)
			if err != nil {
				t.Fatal(err)
			}
			if status != tc.status || out != string(want) {
				t.Fatalf("status %d want %d; output:\n%s\nwant:\n%s", status, tc.status, out, want)
			}
		})
	}
}

func TestDocSourceErrors(t *testing.T) {
	t.Parallel()
	dir := writeProgram(t, "package sample\nvar Broken = unknown\n")
	out, status := runPluto(t, dir, "doc", "-brief")
	want := filepath.Join(dir, "main.p") + ":2:14: undefined: unknown\n"
	if status != 1 || out != want {
		t.Fatalf("status %d: %q, want %q", status, out, want)
	}
}

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
		cmd := exec.Command(plutoBinary(t), args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), runMainEnv+"=1", "PLUTOROOT="+tc.root)
		var out bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &out
		err := cmd.Run()
		var ee *exec.ExitError
		if errors.As(err, &ee) || err != nil || out.String() != tc.want {
			t.Fatalf("%s: %v: %q, want %q", tc.target, err, out.String(), tc.want)
		}
	}
}
