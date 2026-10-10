package build_test

import (
	"strings"
	"testing"
)

// TestUnionCompileErrors checks that union misuse, and the union payloads
// the compiler does not support yet, stop the build.
func TestUnionCompileErrors(t *testing.T) {
	t.Parallel()
	const decls = `package main

type State union {
	Idle
	Busy int
	Done string
}

`
	cases := []struct{ name, src, want string }{
		{"non-exhaustive switch", `func main() {
	s := State.Busy(1)
	switch s.(type) {
	case Idle, State.Busy:
	}
}
`, "missing case in type switch on s (State): Done"},
		{"owned payload", `type Box union {
	Empty
	Full owned[int]
}

func main() { _ = Box.Empty }
`, "union Box must be declared owner: its variant Full holds an owned[int], which is an owner"},
		{"owned through a generic function", `type Opt[T any] union {
	None
	Some T
}

func wrap[T any](x T) Opt[T] { return Opt[T].Some(x) }

func main() {
	var p owned[int]
	_ = wrap(p)
}
`, "cannot use owned[int] as T in wrap: owned[int] is an owner and T does not allow owners"},
		{"extern signature", `//pu:extern c_state
func cState(s State) int

func main() { _ = cState(State.Idle) }
`, "union main.State has no C equivalent"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			err := buildError(t, decls+c.src)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("got error %v, want %q", err, c.want)
			}
		})
	}
}

// TestUnionPanics checks the run-time failures of unions: an assertion of
// the wrong variant, and a tag that names no variant, which only unsafe code
// can make.
func TestUnionPanics(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, src, want string }{
		{"packed assertion", `package main

type P packed union uint8 {
	t bits[uint8, 2]
	A case 0 { x bits[uint8, 6] }
	B case 1, 2, 3 {}
}

func main() {
	p := P(1)
	println(p.(P.A).x)
}
`, "panic: union variant: main.P does not hold A\n"},
		{"invalid tag", `package main

import "unsafe"

type State union {
	Idle
	Busy int
}

func main() {
	s := State.Busy(1)
	*(*uint8)(unsafe.Pointer(&s)) = 9
	switch s.(type) {
	case Idle:
		println("idle")
	case Busy:
		println("busy")
	}
}
`, "panic: invalid tag 9 in a value of union main.State\n"},
		{"invalid tag assertion", `package main

import "unsafe"

type State union {
	Idle
	Busy int
}

func main() {
	s := State.Busy(1)
	*(*uint8)(unsafe.Pointer(&s)) = 9
	println(s.(State.Idle) == struct{}{})
}
`, "panic: union variant: main.State holds an invalid tag, not Idle\n"},
		{"packed invalid tag", `package main

type P packed union uint8 {
	t bits[uint8, 2]
	A case 0 {}
	B case 1 {}
	C case 2 {}
	D case 3 {}
}

func f(p P) {
	switch p.(type) {
	case A, B, C, D:
		println("ok")
	}
}

func main() { f(P(3)) }
`, "ok\n"},
	}
	for _, c := range cases {
		for _, level := range []int{0, 2} {
			t.Run(c.name, func(t *testing.T) {
				t.Parallel()
				out, status := compileAndRun(t, c.src, level)
				wantStatus := 2
				if !strings.HasPrefix(c.want, "panic") {
					wantStatus = 0
				}
				if status != wantStatus || !strings.HasPrefix(out, c.want) {
					t.Fatalf("-O%d: exit status %d, output %q; want %d and %q first", level, status, out, wantStatus, c.want)
				}
			})
		}
	}
}
