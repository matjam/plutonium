package escape_test

import (
	"strings"
	"testing"
)

func TestClosureStringResultProvenance(t *testing.T) {
	for _, body := range []string{
		`return "literal"`,
		`return "hi " + name`,
		`result := name; return result`,
		`if len(name) > 0 { return name }; return "empty"`,
	} {
		t.Run(body, func(t *testing.T) {
			src := `package main
func inner(name string) func() string {
 return func() string {
  unused := func() int { return len(name) }
  _ = unused
  ` + body + `
 }
}

func outer(name string) func() string {
 return func() string { f := inner(name); return f() }
}
func main() { println(outer("world")()) }
`
			if err := check(t, src); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestClosureResultSeparation(t *testing.T) {
	const src = `package main
func inner(name string) func() (string, *int) {
 n := 7
 return func() (s string, p *int) {
  unused := func() int { return n }
  _ = unused
  s = "hello " + name
  p = &n
  return
 }
}
func outer(name string) func() string {
 return func() string { f := inner(name); s, _ := f(); return s }
}
func main() { println(outer("world")()) }
`
	if err := check(t, src); err != nil {
		t.Fatal(err)
	}
}

func TestClosureBorrowedStringResult(t *testing.T) {
	for _, value := range []string{`unsafe.String(&b[0], 2)`, `borrow(b[:])`} {
		t.Run(value, func(t *testing.T) {
			const prefix = `package main
import "unsafe"
func borrow(b []byte) string { return unsafe.String(&b[0], len(b)) }
func inner() func() string {
 b := [2]byte{'o', 'k'}
 return func() string { return `
			src := prefix + value + ` }
}

func outer() func() string {
 return func() string { f := inner(); return f() }
}
func main() { println(outer()()) }
`
			if err := check(t, src); err == nil {
				t.Fatal("borrowed region string escaped")
			}
		})
	}
}
func TestClosureResultFieldSeparation(t *testing.T) {
	const prefix = `package main
type Result struct { s string; p *int }
func inner(name string) func() Result {
 n := 7
 return func() Result { return Result{"hello " + name, &n} }
}
`
	for _, result := range []string{`s`, `p`} {
		t.Run(result, func(t *testing.T) {
			typ := "string"
			if result == "p" {
				typ = "*int"
			}
			src := prefix + `func outer(name string) func() ` + typ + ` {
 return func() ` + typ + ` { f := inner(name); return f().` + result + ` }
}
func main() { _ = outer("world")() }
`
			err := check(t, src)
			if (err == nil) != (result == "s") {
				t.Fatalf("unexpected result: %v", err)
			}
		})
	}
}
func TestTemporaryBoxViews(t *testing.T) {
	const prefix = `package main
import "pluto/alloc"
type Item struct { x int; a [2]int; p *int }
type Holder owner struct { p owned[Item] }
type SharedHolder struct { p synced[Item] }
type Other struct { x int }
func (i *Other) Get() *int { return &i.x }
type Pair struct { p synced[Item]; q synced[Other] }
type Getter interface { Get() *int }
func (i *Item) Get() *int { return &i.x }
func project(g Getter) *int { return g.Get() }
func genericProject[T Getter](g T) *int { return g.Get() }
var saved *int
var strong synced[Item]
func saveStrong[T synced[Item]](g T) { strong = synced[Item](g) }
func save[T Getter](g T) { saved = g.Get() }
func set[T Getter](g T, dst **int) { *dst = g.Get() }
func aggregateProject[T Getter](h Wrap[T]) *int { return h.p.Get() }
func deepProject[T Getter](h Wrap[Wrap[Wrap[T]]]) *int { return h.p.p.p.Get() }
func (i *Item) Identity() *Item { return i }
func identity[T interface{ Identity() *Item }](g T) *Item { return g.Identity() }
func (i *Item) Raw() *int { return i.p }
func raw[T interface{ Raw() *int }](g T) *int { return g.Raw() }
func tuple() (synced[Item], int) { return alloc.Synced[Item](), 1 }
func tupleProject[T Getter](g T, n int) *int { return g.Get() }
func arrayProject[T Getter](g [1]T) *int { return g[0].Get() }
func indirect(g Getter) *int { f := g.Get; return f() }
type Wrap[T any] struct { p T }
func wrap[T any](p T) Wrap[T] { return Wrap[T]{p} }
func pair() Pair { return Pair{alloc.Synced[Item](), alloc.Synced[Other]()} }
func choose(a, b Getter) *int { if a != nil { return a.Get() }; return b.Get() }
func mk() owned[Item] { return new(Item{x: 7}) }
func holder() Holder { return Holder{p: mk()} }
func array() [1]owned[Item] { return [1]owned[Item]{mk()} }
func refs() [1]synced[Item] { return [1]synced[Item]{alloc.Synced[Item]()} }
func sharedHolder() SharedHolder { return SharedHolder{alloc.Synced[Item]()} }
func forward[T owner any](v T) T { return v }
func relay[T owner any](v T) T { return forward(v) }
func copyValue[T any](v T, dst *T) { *dst = v }
func relayCopy[T any](v T, dst *T) { copyValue(v, dst) }
type Mixed struct { a Wrap[synced[Item]]; b Wrap[synced[Other]] }

func read(p *int) int { return *p }
`
	for _, expr := range []string{`&mk().x`, `&mk().a[1]`, `&holder().p.x`, `&array()[0].x`, `&forward(mk()).x`, `&(*mk()).x`, `&refs()[0].x`, `&sharedHolder().p.x`, `project(holder().p)`, `project(sharedHolder().p)`, `genericProject(sharedHolder().p)`, `indirect(pair().q)`, `choose(pair().p, pair().q)`, `aggregateProject(wrap(sharedHolder().p))`, `tupleProject(tuple())`, `arrayProject(refs())`, `&identity(sharedHolder().p).x`, `deepProject(wrap(wrap(wrap(sharedHolder().p))))`} {
		t.Run(expr, func(t *testing.T) {
			err := check(t, prefix+`func main() { p := `+expr+`; println(*p) }`)
			if err == nil || !strings.Contains(fullText(err), "E3009") {
				t.Fatalf("want E3009, got %v", err)
			}
		})
	}
	t.Run("distinct named aggregate payloads", func(t *testing.T) {
		if err := check(t, prefix+`func siblingLeak() *int {
 h := Mixed{wrap(alloc.Synced[Item]()), wrap(alloc.Synced[Other]())}
 _ = aggregateProject(h.a)
 return aggregateProject(h.b)
}
func main() {}`); err == nil {
			t.Fatal("named aggregate view escaped")
		}
	})

	for _, body := range []string{`save(sharedHolder().p)`, `var p *int; set(sharedHolder().p, &p); println(*p)`} {
		t.Run(body, func(t *testing.T) {
			if err := check(t, prefix+`func main() { `+body+` }`); err == nil {
				t.Fatal("borrowed generic value escaped")
			}
		})
	}

	for _, body := range []string{
		`println(read(&mk().x))`,
		`p := mk(); q := &p.x; println(*q)`,
		`h := sharedHolder(); q := &h.p.x; println(*q)`,
		`a := refs(); q := &a[0].x; println(*q)`,
		`q := sharedHolder().p; p := &q.x; println(*p)`,
		`q := refs()[0]; p := &q.x; println(*p)`,
		`q := forward(mk()); p := &q.x; println(*p)`,
		`q := relay(mk()); p := &q.x; println(*p)`,
		`q := relay(sharedHolder().p); p := &q.x; println(*p)`,
		`q := forward(sharedHolder().p); p := &q.x; println(*p)`,
		`q := wrap(sharedHolder().p); p := &q.p.x; println(*p)`,
		`saveStrong(sharedHolder().p)`,
		`q := alloc.Synced[Item](); relayCopy(sharedHolder().p, &q); p := &q.x; println(*p)`,
		`p := raw(sharedHolder().p); println(*p)`,
		`q := sharedHolder().p; saveStrong(q)`,
		`h := sharedHolder(); println(read(aggregateProject(wrap(h.p))))`,
		`if p := &mk().x; *p > 0 { println(*p) }`,
		`switch p := &mk().x; *p { case 7: println(*p) }`,
	} {
		t.Run(body, func(t *testing.T) {
			if err := check(t, prefix+`func main() { `+body+` }`); err != nil {
				t.Fatal(err)
			}
		})
	}
}
