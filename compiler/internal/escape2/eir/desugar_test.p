// Ported from internal/escape2/eir/desugar_test.go at the go-cutover tag.
// The Go file was the external package eir_test.

package eir

import (
	"pluto/sort"
	"pluto/strings"
	"pluto/testing"

	"compiler/compat"
)

// regsRE matched the registers of a call's string: "(r1, r2) -> (r3)",
// with `\((r\d+(, r\d+)*)?\) -> \((r\d+(, r\d+)*)?\)`.
// compiler/internal/regexp has no ReplaceAllString; stripRegs removes
// what that expression matches.
func stripRegs(s string) string {
	var b compat.Builder
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] != '(' {
			continue
		}
		k := regList(s, i+1)
		if !strings.HasPrefix(strings.SliceFrom(s, k), ") -> (") {
			continue
		}
		k = regList(s, k+6)
		if k >= len(s) || s[k] != ')' {
			continue
		}
		b.WriteString(strings.Slice(s, start, i))
		start = k + 1
		i = k
	}
	b.WriteString(strings.SliceFrom(s, start))
	return b.String()
}

// regList returns the offset after the register list "r1, r2" at s[k:],
// or k if there is none.
func regList(s string, k int) int {
	reg := func(j int) int { // the offset after "r\d+" at j, or -1
		if j+1 >= len(s) || s[j] != 'r' || s[j+1] < '0' || s[j+1] > '9' {
			return -1
		}
		j += 2
		for j < len(s) && s[j] >= '0' && s[j] <= '9' {
			j++
		}
		return j
	}
	e := reg(k)
	if e < 0 {
		return k
	}
	for strings.HasPrefix(strings.SliceFrom(s, e), ", ") {
		n := reg(e + 2)
		if n < 0 {
			break
		}
		e = n
	}
	return e
}

// TestStripRegs checks stripRegs, which stands in for the Go test's
// regsRE.ReplaceAllString.
func TestStripRegs(t *testing.T) {
	for _, c := range []struct{ in, out string }{
		{"main.f(r1, r2) -> (r3) [defer]", "main.f [defer]"},
		{"g() -> ()", "g"},
		{"h(r1) -> (r2, r10)x", "hx"},
		{"k(r1, s) -> (r2)", "k(r1, s) -> (r2)"},
	} {
		if got := stripRegs(c.in); got != c.out {
			t.Errorf("stripRegs(%q) = %q, want %q", c.in, got, c.out)
		}
	}
}

// calls builds src and returns, for each function of package main (and
// the synthesized functions it makes), the calls it makes, one string each:
// "callee [implicit]".
func calls(t *testing.T, src string) map[string][]string {
	t.Helper()
	prog := loadSrc(t, src)
	p, err := Build(prog)
	if err != nil {
		t.Fatalf("build: %s", err.Error())
	}
	out := map[string][]string{}
	for _, f := range p.Funcs {
		if f.Pkg != prog.Main.Types && (f.Key.Synth == nil || !strings.Contains(f.Name, "main.")) {
			continue
		}
		name := f.Name
		if f.Key.Lit != nil {
			name = "lit"
		}
		if f.Key.Range != nil {
			name = "range body"
		}
		for _, b := range f.Blocks {
			for i := range b.Instrs {
				in := &b.Instrs[i]
				if in.Op != OpCall {
					continue
				}
				s := p.InstrString(in)
				if j := strings.Index(s, "call "); j >= 0 {
					s = strings.SliceFrom(s, j+5)
				}
				out[name] = compat.Append(out[name], stripRegs(s))
			}
		}
	}
	for k := range out {
		sort.Strings(out[k])
	}
	return out
}

// want checks that function fn makes each of the calls in want (a
// substring of the call's string each).
func want(t *testing.T, got map[string][]string, fn string, want ...string) {
	t.Helper()
	for _, w := range want {
		found := false
		for _, c := range got[fn] {
			if strings.Contains(c, w) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("%s: no call %q; calls:\n\t%s", fn, w, compat.Join(got[fn], "\n\t"))
		}
	}
}

func TestDesugarImplicitDrops(t *testing.T) {
	got := calls(t, `package main

type R owner struct{ p *int }

func (r *R) Drop() {}

type Pair owner struct {
	a R
	b R
}

func use(r *R) {}

func f(r R) {
	var x R
	use(&x)
	x = R{}
	var y Pair
	use(&y.a)
}

func main() {
	var r R
	f(r)
}
`)
	want(t, got, "main.f", "main.R (drop) [drop]", "main.Pair (drop) [drop]")
	want(t, got, "main.Pair (drop)", "main.R (drop) [drop]")
	want(t, got, "main.R (drop)", "main.(*R).Drop [wrapper]")
}

func TestDesugarIndexSugar(t *testing.T) {
	got := calls(t, `package main

type V struct{ s []*int }

func (v *V) Index(i int) *int       { return v.s[i] }
func (v *V) SetIndex(i int, p *int) { v.s[i] = p }
func (v *V) Len() int               { return len(v.s) }
func (v *V) All() func(yield func(int, *int) bool) {
	return func(yield func(int, *int) bool) {}
}

func f(v *V, p *int) int {
	v[0] = p
	q := v[1]
	_ = q
	for i, x := range v {
		_, _ = i, x
	}
	return len(v)
}

func main() {}
`)
	want(t, got, "main.f", "main.(*V).SetIndex [setindex]", "main.(*V).Index [index]", "main.(*V).Len [len]", "main.(*V).All [all]", "[rangeiter]")
}

func TestDesugarRangeFunc(t *testing.T) {
	got := calls(t, `package main

func seq(yield func(*int) bool) {
	x := 1
	yield(&x)
}

func keep(p *int) {}

func f() {
	for p := range seq {
		keep(p)
	}
}

func main() { f() }
`)
	want(t, got, "main.f", "main.seq [rangeiter]")
	want(t, got, "range body", "main.keep")
}

func TestDesugarDeferAndTask(t *testing.T) {
	got := calls(t, `package main

type T struct{}

func (T) M(n int) {}

func g(n int) {}

func f(t T) {
	defer g(1)
	defer t.M(2)
	task g(3)
}

func main() {}
`)
	want(t, got, "main.f", "main.g [defer]", "main.(T).M [defer]", "main.g [task]")
}

func TestDesugarMethodValuesAndExpressions(t *testing.T) {
	got := calls(t, `package main

type Inner struct{}

func (*Inner) M() {}

type Outer struct{ Inner }

type I interface{ M() }

func f(o *Outer, i I) {
	h := o.M
	h()
	e := (*Outer).M
	e(o)
	k := i.M
	k()
	var j I = o
	j.M()
}

func main() {}
`)
	want(t, got, "main.f", "func r", "iface")
	want(t, got, "*main.Inner.M (method value)", "main.(*Inner).M [wrapper]")
	want(t, got, "(*main.Outer).M (method expression)", "main.(*Inner).M [wrapper]")
	want(t, got, "main.I.M (interface method value)", "iface")
	want(t, got, "*main.Outer.M (interface adapter)", "main.(*Inner).M [wrapper]")
}

func TestDesugarUnionVariantValue(t *testing.T) {
	got := calls(t, `package main

type U union {
	A(*int)
	B
}

func apply(f func(*int) U, p *int) U { return f(p) }

func main() {
	x := 1
	apply(U.A, &x)
}
`)
	want(t, got, "main.main", "main.apply")
	want(t, got, "main.apply", "func r")
}

func TestDesugarContract(t *testing.T) {
	got := calls(t, `package main

import "pluto/math"

func main() {
	_ = math.Sqrt(2)
}
`)
	want(t, got, "main.main", "contract pluto/math.Sqrt")
}

func TestDesugarMaps(t *testing.T) {
	got := calls(t, `package main

func main() {
	m := map[string]*int{}
	x := 1
	m["a"] = &x
	_ = m["a"]
	delete(m, "a")
	for k, v := range m {
		_, _ = k, v
	}
	_ = len(m)
}
`)
	want(t, got, "main.main", "pluto/rt.mapMake", "pluto/rt.mapAssign [setindex]", "pluto/rt.mapAccess [index]",
		"pluto/rt.mapDelete", "pluto/rt.mapLen [len]", "Next", "Start")
}
