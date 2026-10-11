package lower_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/matjam/plutonium/internal/ssa"
)

func TestCSignatureErrors(t *testing.T) {
	const header = "package main\n\nimport \"unsafe\"\n\nvar _ unsafe.Pointer\n\ntype S struct{ A int }\n\n"
	cases := []struct {
		name, src, want string
	}{
		{"string param", "//pu:extern f\nfunc f(s string)\n",
			"//pu:extern function f: parameter 1: string has no C equivalent; pass unsafe.StringData(s) and len(s)"},
		{"slice param", "//pu:extern f\nfunc f(b []byte)\n",
			"parameter 1: slice []byte has no C equivalent"},
		{"slice in struct", "type T struct{ B []int }\n\n//pu:extern f\nfunc f(t T)\n",
			"parameter 1: field B: slice []int has no C equivalent"},
		{"string in result struct", "type T struct{ N int; S string }\n\n//pu:extern f\nfunc f() T\n",
			"result: field S: string has no C equivalent"},
		{"interface result", "//pu:extern f\nfunc f() error\n",
			"result: interface error has no C equivalent"},
		{"interface in results", "//pu:extern f\nfunc f() (int, any)\n",
			"result 2: interface any has no C equivalent"},
		{"func param", "//pu:extern f\nfunc f(cb func())\n",
			"func value func() has no C equivalent"},
		{"variadic", "//pu:extern f\nfunc f(n int, xs ...int)\n",
			"variadic functions cannot cross to C"},
		{"array param", "//pu:extern f\nfunc f(a [4]int32)\n",
			"array [4]int32 cannot be passed to or from C by value"},
		{"empty struct", "//pu:extern f\nfunc f(e struct{})\n",
			"zero-size type struct{} has no C equivalent"},
		{"over-aligned", "//pu:extern f\nfunc f(a aligned[int64, 16])\n",
			"aligned to more than 8 bytes"},
		{"extern with body", "//pu:extern f\nfunc f() {}\n",
			"//pu:extern function f must not have a body"},
		{"export slice", "//pu:export g\nfunc g(b []byte) int { return len(b) }\n",
			"//pu:export function g: parameter 1: slice []byte has no C equivalent"},
		{"export variadic", "//pu:export g\nfunc g(xs ...int) {}\n",
			"//pu:export function g: variadic functions cannot cross to C"},
		{"export name used twice", "type P struct{ A, B int32 }\n\n//pu:export g\nfunc g1(p P) int { return 0 }\n\n//pu:export g\nfunc g2(p P) int { return 1 }\n",
			"//pu:export g: main.g1 and main.g2 are both exported under one symbol"},
		{"export name used twice, unwrapped", "//pu:export g\nfunc g1(a, b int64) int64 { return a + b }\n\n//pu:export g\nfunc g2(a, b int64) int64 { return a - b }\n",
			"//pu:export g: main.g1 and main.g2 are both exported under one symbol"},
		{"export name used twice, one wrapped", "type P struct{ A, B int32 }\n\n//pu:export g\nfunc g1(a, b int64) int64 { return a + b }\n\n//pu:export g\nfunc g2(p P) int { return 0 }\n",
			"//pu:export g: main.g1 and main.g2 are both exported under one symbol"},
		{"export interface method receiver", "type R struct{ s string }\n\n//pu:export g\nfunc (r R) g() {}\n",
			"//pu:export function g: parameter 1: field s: string has no C equivalent"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := lowerError(t, header+c.src+"\nfunc main() {}\n")
			if !strings.Contains(got, c.want) {
				t.Errorf("error %q, want it to contain %q", got, c.want)
			}
		})
	}
}

// TestCCallLowering checks how calls to C functions are lowered: System V
// eightbytes, a struct forced to the stack when its eightbytes do not all fit,
// a hidden result pointer, and the internal ABI kept where it agrees.
func TestCCallLowering(t *testing.T) {
	prog := lowerSource(t, `package main

type I3 struct {
	A int64
	B int8
	C int16
}

type M2 struct {
	A int64
	B float64
}

type Big struct{ A, B, C int64 }

//pu:extern c_spill
func cSpill(a, b, c, d, e int64, s I3, f int64) int64

//pu:extern c_mixed
func cMixed(m M2) M2

//pu:extern c_big
func cBig(x int8) Big

//pu:extern c_plain
func cPlain(a int64, x float64, p *int) float64

//pu:export pu_mixed
func puMixed(m M2) M2 { return m }

func main() {
	println(cSpill(1, 2, 3, 4, 5, I3{}, 6), cMixed(M2{}).A, cBig(1).C, cPlain(1, 2, nil))
}
`)
	calls := map[string]*ssa.Value{}
	var wrapper *ssa.Func
	for _, f := range prog.Funcs {
		if f.Name == "pu_mixed" {
			wrapper = f
		}
		if f.Name != "main.main" {
			continue
		}
		for _, b := range f.Blocks {
			for _, v := range b.Values {
				if v.Op == ssa.OpCall {
					calls[v.Aux.(*ssa.Sym).Name] = v
				}
			}
		}
	}
	describe := func(v *ssa.Value) string {
		var parts []string
		for i, a := range v.Args[:len(v.Args)-1] {
			s := a.Type.String()
			if v.Call.OnStack != nil && v.Call.OnStack[i] {
				s += "@stack"
			}
			parts = append(parts, s)
		}
		return fmt.Sprintf("abi%d (%s) -> %v", v.Call.ABI, strings.Join(parts, " "), v.Call.Results)
	}
	want := map[string]string{
		// I3 needs two integer registers; only R9 is left, so it goes to the
		// stack whole and f takes R9.
		"c_spill": "abi1 (i64 i64 i64 i64 i64 i64@stack i64@stack i64) -> [i64]",
		"c_mixed": "abi1 (i64 f64) -> [i64 f64]",
		"c_big":   "abi1 (ptr i32) -> [ptr]",
		"c_plain": "abi0 (i64 f64 ptr) -> [f64]",
	}
	for name, w := range want {
		v := calls[name]
		if v == nil {
			t.Errorf("no call to %s", name)
			continue
		}
		if got := describe(v); got != w {
			t.Errorf("%s: %s, want %s", name, got, w)
		}
	}
	if wrapper == nil {
		t.Fatal("no export wrapper pu_mixed")
	}
	if wrapper.ABI != ssa.ABIC || fmt.Sprint(wrapper.Params, wrapper.Results) != "[i64 f64] [i64 f64]" {
		t.Errorf("pu_mixed wrapper: abi %d params %v results %v", wrapper.ABI, wrapper.Params, wrapper.Results)
	}
}
