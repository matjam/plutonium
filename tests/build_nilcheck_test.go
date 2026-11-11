// Ported from internal/build/nilcheck_test.go at the go-cutover tag.

package tests

import (
	"strings"
	"testing"
)

// TestNilChecks checks where explicit nil checks go (language spec section
// 17.5): only where an access's offset from a pointer can reach past the
// unmapped page at address zero, and not where an earlier access at a
// small offset from the same pointer has already shown it is not nil.
func TestNilChecks(t *testing.T) {
	t.Parallel()
	const src = `package main

type big struct {
	head int
	buf  [100000]int
	tail int
}

type small struct {
	buf [256]byte
	n   int
}

type outer struct {
	pad   [8000]byte
	inner *small
}

//pu:noinline
func near(b *big) int { return b.head + b.buf[3] }

//pu:noinline
func far(b *big) int { return b.tail }

//pu:noinline
func index(b *big, i, j int) int { return b.buf[i] + b.buf[j] }

//pu:noinline
func store(b *big, v big) { *b = v }

//pu:noinline
func smallIndex(s *small, i int) byte { return s.buf[i] }

//pu:noinline
func sliceAfterLoad(s *small) []byte { return s.buf[:s.n] }

//pu:noinline
func sliceArray(p *[4]byte) []byte { return p[1:] }

//pu:noinline
func embedded(o *outer) int { return o.inner.n }

//pu:noinline
func rangeBig(p *[10000]int) int {
	s := 0
	for _, v := range p {
		s += v
	}
	return s
}

//pu:noinline
func method(b *big) int { return b.value() }

func (b big) value() int { return b.tail }

func main() {
	var b big
	var s small
	var a [4]byte
	var o outer
	o.inner = &s
	var r [10000]int
	store(&b, b)
	println(near(&b), far(&b), index(&b, 1, 2), smallIndex(&s, 3), len(sliceAfterLoad(&s)), len(sliceArray(&a)), embedded(&o), rangeBig(&r), method(&b))
}
`
	for _, c := range []struct {
		fn   string
		want int
	}{
		{"main.near", 0},           // static offsets inside the page
		{"main.far", 1},            // a field past the array
		{"main.index", 1},          // two variable indexes, one check
		{"main.store", 1},          // a whole large value
		{"main.smallIndex", 0},     // the whole array is inside the page
		{"main.sliceAfterLoad", 0}, // s.n was read through s first
		{"main.sliceArray", 1},     // slicing through a pointer always checks
		{"main.embedded", 1},       // o.inner is past the page
		{"main.rangeBig", 1},       // checked once, before the loop
		{"main.method", 1},         // a value receiver copies the large value
	} {
		got := strings.Count(listFunc(t, src, c.fn), "pluto/rt.panicNil")
		if got != c.want {
			t.Errorf("%s: %d nil checks, want %d", c.fn, got, c.want)
		}
	}
}

// TestNilChecksFreestanding checks that a freestanding build outside kernel
// mode checks every dereference it cannot prove non-nil, and every call
// through a func value or an interface (language spec section 17.5), since
// no fault reports a nil pointer there. A kernel arranges the unmapped page
// at address zero, so it checks only what a hosted build does.
func TestNilChecksFreestanding(t *testing.T) {
	t.Parallel()
	const src = `package main

type small struct {
	a, b int
}

type I interface{ M() int }

type V struct{ n int }

func (v V) M() int { return v.n }

//pu:noinline
func near(p *small) int { return p.a + p.b }

//pu:noinline
func store(p *int) { *p = 1 }

//pu:noinline
func callFunc(f func() int) int { return f() }

//pu:noinline
func callIface(i I) int { return i.M() }

//pu:noinline
func local() int {
	var s small
	p := &s
	return p.a
}

func main() {
	var s small
	var n int
	store(&n)
	_ = near(&s) + callFunc(func() int { return 1 }) + callIface(V{2}) + local()
}
`
	listings := map[string]string{}
	for _, c := range []struct {
		mode string
		fn   string
		want int
	}{
		{"freestanding", "main.near", 1},      // two fields of one pointer, one check
		{"freestanding", "main.store", 1},     // a store
		{"freestanding", "main.callFunc", 1},  // a call through a func value
		{"freestanding", "main.callIface", 1}, // a call through an interface
		{"freestanding", "main.local", 0},     // the address of a local is not nil
		{"kernel", "main.near", 0},
		{"kernel", "main.store", 0},
		{"kernel", "main.callFunc", 0},
		{"kernel", "main.callIface", 0},
	} {
		listing, ok := listings[c.mode]
		if !ok {
			var errs string
			listing, errs = modeCompile(t, src, 2, c.mode, true)
			if errs != "" {
				t.Fatalf("%s: build failed:\n%s", c.mode, errs)
			}
			listings[c.mode] = listing
		}
		got := strings.Count(funcListing(t, listing, c.fn), "pluto/rt.panicNil")
		if got != c.want {
			t.Errorf("%s %s: %d nil checks, want %d", c.mode, c.fn, got, c.want)
		}
	}
}
