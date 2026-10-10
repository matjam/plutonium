// Copyright 2023 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package build_test

// TestRangeGen is Go's range-over-func torture test, test/rangegen.go,
// adapted: it generates functions with nested loops and every kind of
// break, continue, return and goto out of them, once ranging over a
// function and once over a slice, and checks that both log the same
// trace. Plutonium has no select statement, rejects a defer that runs once
// per iteration, and grows no slice with append, so the generated code has
// no select or defer cases and logs into a fixed array; the nesting is not
// as deep, to keep the program small. The generated program runs with Go
// too, and prints "ok" there as well.

import (
	"bytes"
	"fmt"
	"testing"
)

func TestRangeGen(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("slow")
	}
	b := new(bytes.Buffer)
	tests := ""
	p := func(format string, args ...any) { fmt.Fprintf(b, format, args...) }
	p("package main\n\n")
	// Functions with one exit each, up to two loops deep; then functions
	// with every exit, up to three, run once per exit.
	const maxDepth = 3
	for i := 1; i < maxDepth; i++ {
		code := rangeGen(new(bytes.Buffer), "", "", "", i, func(c int) bool { return true })
		for j := 0; j < code; j++ {
			s := fmt.Sprintf("%d_%d", i, j)
			code0 := rangeGen(b, "testFunc"+s, "", "yield2", i, func(c int) bool { return c == j })
			code1 := rangeGen(b, "testSlice"+s, "_, ", "slice2", i, func(c int) bool { return c == j })
			if code0 != code1 {
				t.Fatal("bad generator")
			}
			tests += "\ttest" + s + "()\n"
			p(rangeGenTest, "test"+s, j, "testFunc"+s, "testSlice"+s)
		}
	}
	for i := 1; i <= maxDepth; i++ {
		s := fmt.Sprintf("%d", i)
		code := rangeGen(b, "testFunc"+s, "", "yield2", i, func(c int) bool { return true })
		code1 := rangeGen(b, "testSlice"+s, "_, ", "slice2", i, func(c int) bool { return true })
		if code != code1 {
			t.Fatal("bad generator")
		}
		tests += "\ttestAll" + s + "()\n"
		p(rangeGenTestAll, "testAll"+s, code, "testFunc"+s, "testSlice"+s)
	}
	p(rangeGenMain, tests)
	src := b.String()

	for _, level := range []int{0, 2} {
		t.Run(fmt.Sprintf("O%d", level), func(t *testing.T) {
			t.Parallel()
			got, status := compileAndRun(t, src, level)
			if status != 0 || got != "ok\n" {
				t.Errorf("status %d, output:\n%s", status, got)
			}
		})
	}
}

// rangeGen writes a function with depth nested loops over rangeExpr, with
// a numbered exit at each point allowed accepts, and returns the number of
// exits.
func rangeGen(b *bytes.Buffer, name, prefix, rangeExpr string, depth int, allowed func(int) bool) int {
	fmt.Fprintf(b, "func %s(o *output, code int) int {\n", name)
	code := rangeGenLoop(b, 0, prefix, rangeExpr, depth, 0, allowed)
	fmt.Fprintf(b, "\treturn 0\n}\n\n")
	return code
}

func rangeGenLoop(b *bytes.Buffer, d int, prefix, rangeExpr string, depth, code int, allowed func(int) bool) int {
	p := func(format string, args ...any) { fmt.Fprintf(b, format, args...) }
	s := fmt.Sprint(d)
	p("\to.log(`top%s`)\n", s)
	p("\tl%sa := 0\n", s)
	p("goto L%sa; L%sa:\to.log(`L%sa`)\n", s, s, s)
	p("\tif l%sa++; l%sa >= 2 { o.log(`loop L%sa`); return -1 }\n", s, s, s)
	p("\tl%sfor := 0\n", s)
	p("goto L%sfor; L%sfor: for f := 0; f < 1; f++ { o.log(`L%sfor`)\n", s, s, s)
	p("\tif l%sfor++; l%sfor >= 2 { o.log(`loop L%sfor`); return -1 }\n", s, s, s)
	p("\tl%ssw := 0\n", s)
	p("goto L%ssw; L%ssw: switch { default: o.log(`L%ssw`)\n", s, s, s)
	p("\tif l%ssw++; l%ssw >= 2 { o.log(`loop L%ssw`); return -1 }\n", s, s, s)
	p("\tl%s := 0\n", s)
	p("goto L%s; L%s:\tfor %s i%s := range %s {\n", s, s, prefix, s, rangeExpr)
	p("\to.log1(`L%s top`, i%s)\n", s, s)
	p("\tif l%s++; l%s >= 4 { o.log(`loop L%s`); return -1 }\n", s, s, s)
	tests := func() {
		if code++; allowed(code) {
			p("\tif code == %v { break }\n", code)
		}
		if code++; allowed(code) {
			p("\tif code == %v { continue }\n", code)
		}
		if code++; allowed(code) {
			p("\tswitch { case code == %v: continue }\n", code)
		}
		if code++; allowed(code) {
			p("\tif code == %v { return %[1]v }\n", code)
		}
		if code++; allowed(code) {
			p("\tif code == %v { switch { default: break } }\n", code)
		}
		for i := d; i > 0; i-- {
			if code++; allowed(code) {
				p("\tif code == %v { break L%d }\n", code, i)
			}
			if code++; allowed(code) {
				p("\tif code == %v { continue L%d }\n", code, i)
			}
			if code++; allowed(code) {
				p("\tif code == %v { break L%dsw }\n", code, i)
			}
			if code++; allowed(code) {
				p("\tif code == %v { break L%dfor }\n", code, i)
			}
			if code++; allowed(code) {
				p("\tif code == %v { continue L%dfor }\n", code, i)
			}
			if code++; allowed(code) {
				p("\tif code == %v { goto L%da }\n", code, i)
			}
			if code++; allowed(code) {
				p("\tif code == %v { goto L%d }\n", code, i)
			}
			if code++; allowed(code) {
				p("\tif code == %v { goto L%db }\n", code, i)
			}
		}
	}
	tests()
	if d < depth {
		code = rangeGenLoop(b, d+1, prefix, rangeExpr, depth, code, allowed)
		tests()
	}
	p("\to.log(`L%s bot`)\n", s)
	p("\t}\n")
	p("\to.log(`L%ssw bot`)\n", s)
	p("\t}\n")
	p("\to.log(`L%sfor bot`)\n", s)
	p("\t}\n")
	p("\to.log(`done%s`)\n", s)
	p("goto L%sb; L%sb: o.log(`L%sb`)\n", s, s, s)
	return code
}

var rangeGenTest = `
func %s() {
	c := %d
	var outFunc, outSlice output
	run(%s, c, &outFunc)
	run(%s, c, &outSlice)
	if !outFunc.eq(&outSlice) {
		println("mismatch", "%[3]s", "%[4]s", c)
		println()
		println("func:")
		outFunc.print()
		println()
		println("slice:")
		outSlice.print()
		panic("mismatch")
	}
}
`

var rangeGenTestAll = `
func %s() {
	for c := 0; c < %d; c++ {
		var outFunc, outSlice output
		run(%s, c, &outFunc)
		run(%s, c, &outSlice)
		if !outFunc.eq(&outSlice) {
			println("mismatch", "%[3]s", "%[4]s", c)
			println()
			println("func:")
			outFunc.print()
			println()
			println("slice:")
			outSlice.print()
			panic("mismatch")
		}
	}
}
`

var rangeGenMain = `
func main() {
%s	println("ok")
}

func yield2(yield func(int) bool) { _ = yield(1) && yield(2) }

var slice2 = []int{1, 2}

type entry struct {
	s string
	i int
}

type output struct {
	ret   int
	trace [256]entry
	n     int
}

func (o *output) log(x string) {
	o.trace[o.n] = entry{s: x}
	o.n++
}

func (o *output) log1(x string, y int) {
	o.trace[o.n] = entry{s: x}
	o.trace[o.n+1] = entry{i: y}
	o.n += 2
}

func (o *output) eq(p *output) bool {
	if o.ret != p.ret || o.n != p.n {
		return false
	}
	for i := 0; i < o.n; i++ {
		if o.trace[i] != p.trace[i] {
			return false
		}
	}
	return true
}

func (o *output) print() {
	println("ret", o.ret, "trace-len", o.n)
	for i := 0; i < o.n; i++ {
		print("#", i, " ", o.trace[i].s, o.trace[i].i, "\n")
	}
}

func run(f func(*output, int) int, i int, o *output) {
	o.ret = f(o, i)
}
`
