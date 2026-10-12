package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVetUnsafeCommand(t *testing.T) {
	t.Parallel()
	dir := writeProgram(t, "package sample\n//pu:nochecks\nfunc raw() {}\nfunc safe() { raw() }\n")
	out, status := runPluto(t, dir, "vet", "-unsafe")
	want := "package main\n  unsafe main.raw ./main.p:3:1\n    ./main.p:2:1: //pu:nochecks\n  safe main.safe ./main.p:4:1\n    calls main.raw\n      ./main.p:4:15: call main.raw\n"
	if status != 0 || out != want {
		t.Fatalf("status %d:\n%s", status, out)
	}
}

func replaceVetPath(s, dir string) string { return strings.ReplaceAll(s, dir, ".") }

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

func TestVetSourceErrors(t *testing.T) {
	t.Parallel()
	for _, brief := range []bool{false, true} {
		t.Run(map[bool]string{false: "default", true: "brief"}[brief], func(t *testing.T) {
			dir := writeProgram(t, "package sample\nfunc f() { unknown() }\n")
			args := []string{"vet", "-unsafe"}
			if brief {
				args = append(args, "-brief")
			}
			out, status := runPluto(t, dir, args...)
			// Diagnostics use the same formatter and catalog as build; the
			// exact source filename is local to this fixture.
			name := filepath.Join("testdata/vet", "error_"+map[bool]string{false: "default", true: "brief"}[brief]+".out")
			out = replaceVetPath(out, dir)
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
