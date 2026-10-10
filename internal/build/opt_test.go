package build_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	plutonium "github.com/matjam/plutonium"
	"github.com/matjam/plutonium/internal/build"
)

// listFunc compiles src at -O2 and returns the machine listing of function
// name (for example "main.f").
func listFunc(t *testing.T, src, name string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.p"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	var listing bytes.Buffer
	cfg := &build.Config{Dir: dir, Output: filepath.Join(dir, "prog"), Stdlib: plutonium.Stdlib(), OptLevel: 2, Listing: &listing}
	if err := build.Build(cfg); err != nil {
		t.Fatalf("build: %v", err)
	}
	text := listing.String()
	i := strings.Index(text, "\n"+name+":\n")
	if i < 0 {
		t.Fatalf("%s not in listing:\n%s", name, text)
	}
	text = text[i+1:]
	if j := strings.Index(text, "\n\n"); j >= 0 {
		text = text[:j]
	}
	return text
}

func TestBranchElim(t *testing.T) {
	const src = `package main

var data [64]int64

//pu:noinline
func step(n int64) int64 {
	steps := int64(0)
	for n != 1 {
		if n%2 == 0 {
			n = n / 2
		} else {
			n = 3*n + 1
		}
		steps++
	}
	return steps
}

//pu:noinline
func child(root int64) int64 {
	c := 2*root + 1
	if data[c] < data[c+1] {
		c++
	}
	return data[c]
}

func main() { println(step(7), child(3)) }
`
	// The parity test becomes a cmov, leaving only the loop branch; 3n+1
	// is one LEA.
	asm := listFunc(t, src, "main.step")
	if !strings.Contains(asm, "cmov") || strings.Contains(asm, "imul") || strings.Count(asm, "\tj") > 2 {
		t.Errorf("step: want branch-free code with cmov and lea\n%s", asm)
	}
	// The selected child indexes memory, so it stays a branch.
	if asm := listFunc(t, src, "main.child"); strings.Contains(asm, "cmov") {
		t.Errorf("child: selected index feeds a load; want a branch\n%s", asm)
	}
}

func TestProveBoundsChecks(t *testing.T) {
	tests := []struct {
		name, body string
		checked    bool
	}{
		{"len", `for i := 0; i < len(s); i++ { t += s[i] }`, false},
		{"globalStep3", `for i := 0; i < 100; i += 3 { t += g[i] }`, false},
		// i += 2 could step past MaxInt when nothing bounds len(s).
		{"lenStep2", `for i := 0; i < len(s); i += 2 { t += s[i] }`, true},
		{"global", `for i := 0; i < 100; i++ { t += g[i] }`, false},
		{"leq", `for i := 0; i <= len(s); i++ { t += s[i] }`, true},
		{"negStart", `for i := -1; i < len(s); i++ { t += s[i] }`, true},
		{"globalPast", `for i := 0; i < 101; i++ { t += g[i] }`, true},
		{"unbounded", `for i := 0; i < n; i++ { t += s[i] }`, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			src := "package main\n\nvar g [100]int64\n\n//pu:noinline\nfunc f(s []int64, n int) int64 {\n\tt := int64(0)\n\t" +
				tc.body + "\n\treturn t\n}\n\nfunc main() { println(f(nil, 0)) }\n"
			asm := listFunc(t, src, "main.f")
			if got := strings.Contains(asm, "panicIndex"); got != tc.checked {
				t.Errorf("bounds check present = %v, want %v\n%s", got, tc.checked, asm)
			}
		})
	}
}

func TestInlinedResultSlot(t *testing.T) {
	const src = `package main

type P struct{ a, b, c, d, e float64 }

var ps [8]P

func step(p P, k float64) P {
	p.a += p.b * k
	p.c -= p.d * k
	return p
}

//pu:noinline
func f(k float64) {
	for i := range ps {
		ps[i] = step(ps[i], k)
	}
}

func main() {
	ps[1].b = 2
	f(0.5)
	println(int64(ps[1].a * 100))
}
`
	// Once step is inlined its result buffer is split and promoted, so
	// the loop works in registers and stores straight into ps.
	if asm := listFunc(t, src, "main.f"); strings.Contains(asm, "rsp") {
		t.Errorf("f: want no stack traffic\n%s", asm)
	}
}

func TestFloatNegFold(t *testing.T) {
	const src = `package main

//pu:noinline
func f(a, b, k float64) float64 { return a*-1 + b*k }

func main() { println(int64(f(1, 2, 3))) }
`
	// a*-1 + b*k becomes b*k - a: one multiply, no negation.
	asm := listFunc(t, src, "main.f")
	if strings.Count(asm, "muls") != 1 || !strings.Contains(asm, "subs") || strings.Contains(asm, "xorps") {
		t.Errorf("f: want b*k - a\n%s", asm)
	}
}

func TestTailRec(t *testing.T) {
	const src = `package main

func fib(n int64) int64 {
	if n < 2 {
		return n
	}
	return fib(n-1) + fib(n-2)
}

func gcd(a, b int) int {
	if b == 0 {
		return a
	}
	return gcd(b, a%b)
}

func main() { println(fib(20), gcd(12, 18)) }
`
	// fib keeps one call and loops on the other; gcd is a loop.
	if asm := listFunc(t, src, "main.fib"); strings.Count(asm, "call main.fib") != 1 {
		t.Errorf("fib: want one recursive call\n%s", asm)
	}
	if asm := listFunc(t, src, "main.gcd"); strings.Contains(asm, "call") {
		t.Errorf("gcd: want a loop\n%s", asm)
	}
}

func TestFillAndCount(t *testing.T) {
	const src = `package main

var big [1000]bool

//pu:noinline
func fillAll() {
	for i := 0; i < len(big); i++ {
		big[i] = true
	}
}

//pu:noinline
func fillShort(n int) {
	for i := 0; i < n; i++ {
		big[i] = false
	}
}

//pu:noinline
func count() int {
	n := 0
	for i := 0; i < len(big); i++ {
		if big[i] {
			n++
		}
	}
	return n
}

func main() {
	fillAll()
	fillShort(3)
	println(count())
}
`
	if asm := listFunc(t, src, "main.fillAll"); !strings.Contains(asm, "rep stosb") || strings.Contains(asm, "mov1") {
		t.Errorf("fillAll: want one rep stosb\n%s", asm)
	}
	// A short or unknown length stays a loop.
	if asm := listFunc(t, src, "main.fillShort"); strings.Contains(asm, "rep stosb") {
		t.Errorf("fillShort: want a loop\n%s", asm)
	}
	// n += zext(big[i]): no branch or cmov in the loop.
	if asm := listFunc(t, src, "main.count"); strings.Contains(asm, "cmov") || strings.Count(asm, "\tj") > 2 {
		t.Errorf("count: want n += big[i]\n%s", asm)
	}
}

func TestVolatileReadOrders(t *testing.T) {
	const src = `package main

var buf [8]uint32
var status volatile[uint32]

//pu:noinline
func poll() uint32 {
	x := buf[3]
	for status&1 == 0 {
	}
	return x + buf[3]
}

//pu:noinline
func twice(p *volatile[uint32]) uint32 { return *p + *p }

func main() {
	status = 1
	println(poll(), twice(&status))
}
`
	// The plain read after the polling loop must read memory again.
	if asm := listFunc(t, src, "main.poll"); strings.Count(asm, "main.buf+12") != 2 {
		t.Errorf("poll: want buf[3] read before and after the loop\n%s", asm)
	}
	if asm := listFunc(t, src, "main.twice"); strings.Count(asm, "(%rdi)") != 2 {
		t.Errorf("twice: want two volatile reads\n%s", asm)
	}
}

func TestBoolSelect(t *testing.T) {
	const src = `package main

//pu:noinline
func isSpace(c byte) bool { return c == ' ' || c == '\n' || c == '\t' || c == '\r' }

func main() { println(isSpace(' ')) }
`
	// The branch-free chain ORs the tests instead of selecting with cmov.
	asm := listFunc(t, src, "main.isSpace")
	if strings.Contains(asm, "cmov") || strings.Count(asm, "or1") != 3 {
		t.Errorf("isSpace: want three ORs\n%s", asm)
	}
}

func TestLoadFold(t *testing.T) {
	const src = `package main

//pu:noinline
func f(p, q *[4]int64) int64 {
	a := p[1]
	q[1] = 100
	return a + q[2]
}

//pu:noinline
func g(p *[2]float64) float64 { return p[0] * p[1] }

func main() {
	var a [4]int64
	var b [2]float64
	println(f(&a, &a), int64(g(&b)))
}
`
	// q[2] folds into the add; p[1] is loaded before the store.
	asm := listFunc(t, src, "main.f")
	if !strings.Contains(asm, "add8 16(") || strings.Index(asm, "8(%rdi), %rax") > strings.Index(asm, "$100") {
		t.Errorf("f: want q[2] folded and p[1] loaded first\n%s", asm)
	}
	if asm := listFunc(t, src, "main.g"); !strings.Contains(asm, "muls8 (") && !strings.Contains(asm, "muls8 8(") {
		t.Errorf("g: want a load folded into the multiply\n%s", asm)
	}
}

func TestRotate(t *testing.T) {
	const src = `package main

//pu:noinline
func f(x uint32) uint32 { return (x >> 7) | (x << 25) ^ (x>>18 + x<<14) }

func main() { println(f(5)) }
`
	asm := listFunc(t, src, "main.f")
	if strings.Count(asm, "rol") != 2 || strings.Contains(asm, "shr") || strings.Contains(asm, "shl") {
		t.Errorf("f: want two rotates\n%s", asm)
	}
}

func TestSetccClearsFirst(t *testing.T) {
	const src = `package main

//pu:noinline
func lt(a, b int) bool { return a < b }

func main() { println(lt(1, 2)) }
`
	// setl writes one byte; clearing the register first breaks the
	// dependency on its old value.
	asm := listFunc(t, src, "main.lt")
	i, j := strings.Index(asm, "mov4 $0"), strings.Index(asm, "setl")
	if i < 0 || j < i {
		t.Errorf("lt: want mov4 $0 before setl\n%s", asm)
	}
}

func TestZeroIdiomNotLive(t *testing.T) {
	const src = `package main

var ps [8][7]float64

//pu:noinline
func f(z float64) {
	for i := range ps {
		p := &ps[i]
		a, b, c, d, e, g, h := p[0], p[1], p[2], p[3], p[4], p[5], p[6]
		x := d*0.5 - e*z
		y := e*z - c*0.5
		w := c*z - d*z
		a, b, c = (a+x)*h, (b+y)*h, (c+w)*h
		p[0], p[1], p[2], p[3], p[4], p[5], p[6] = a*0.0+b, b*0.0+c, c*0.0+a, d*0.0+g, e*0.0+a, g*0.0+b, h*0.0+c
	}
}

func main() {
	ps[1][0] = 2
	f(1)
	println(int64(ps[1][2] * 100))
}
`
	// xorps r, r clears r without reading it. Treating it as a use keeps
	// each zero live back to the function entry and forces spills.
	if asm := listFunc(t, src, "main.f"); strings.Contains(asm, "rsp") {
		t.Errorf("f: want no spills\n%s", asm)
	}
}

func TestMemOpt(t *testing.T) {
	const src = `package main

type P struct {
	x, y, m float64
	n       int64
}

var ps [8]P

//pu:noinline
func update(n int) {
	for i := 0; i < n; i++ {
		p := ps[i]
		p.x += p.y * p.m
		p.n++
		ps[i] = p
	}
}

//pu:noinline
func forward(p *P, k float64) float64 {
	p.x = k
	p.n = 3
	p.y = 1
	p.y = 2
	return p.x * p.y
}

//pu:noinline
func slots(k int64) int64 {
	var a, b [2]int64
	a[0] = k
	b[0] = k + 1
	a[1] = b[0]
	return a[0] + a[1] + b[1]
}

//pu:noinline
func merge(p *P) float64 {
	a := p.x
	p.y = 3
	return a * p.x
}

func main() {
	update(8)
	var p P
	println(int64(forward(&p, 2)), slots(1), int64(merge(&p)))
}
`
	// The second p.x is the first: the store to p.y cannot change it.
	if asm := listFunc(t, src, "main.merge"); strings.Count(asm, "(%rdi)") != 2 {
		t.Errorf("merge: want one load and one store\n%s", asm)
	}
	// p.y and p.m are stored back unchanged.
	if asm := listFunc(t, src, "main.update"); strings.Count(asm, ", main.ps") != 2 {
		t.Errorf("update: want two stores (x and n)\n%s", asm)
	}
	// p.x is forwarded past the store to p.n, and the first store to p.y
	// is dead.
	asm := listFunc(t, src, "main.forward")
	if strings.Count(asm, "(%rdi)") != 3 || strings.Contains(asm, "(%rdi), %xmm") {
		t.Errorf("forward: want three stores and no loads\n%s", asm)
	}
	// Constant indexes split the arrays once folded.
	if asm := listFunc(t, src, "main.slots"); strings.Contains(asm, "rsp") {
		t.Errorf("slots: want the arrays in registers\n%s", asm)
	}
}
