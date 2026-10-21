package solve_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/matjam/plutonium/internal/escape2"
)

// Library false rejections found by the both-mode comparison of the corpus
// (piece E's triage): each program is accepted by the old analysis and
// must be by this one, with no error in the library line named.
var libraryControls = []struct {
	name, src, line string
	gap             string // a known false rejection, with the reason; the test fails when it is fixed
}{
	{
		// fmt's printer.to asserts any(dst) to *vec.Fixed[byte]: the
		// result is a direct value, never the frame box a non-direct
		// instance of D makes (fmt.p:989).
		"fmt_println", `package main

import "pluto/fmt"

func main() { fmt.Println("hi", 1) }
`, "fmt.p:989", "",
	},
	{
		// vec.Collect: AppendSeq's range body appends to v through its
		// record; realloc's same-input store must not make v itself go
		// into v's own buffer (vec.p:377-387).
		"vec_collect", `package main

import "pluto/vec"

func seq(yield func(int) bool) {
	for i := 0; i < 3; i++ {
		if !yield(i) {
			return
		}
	}
}

func main() {
	v := vec.Collect[int](seq)
	println(v.Len())
}
`, "vec.p:3", "",
	},
	{
		// A local vector pushed to (strconv/quote.p:220, 122 programs).
		"vec_push_local", `package main

import (
	"pluto/alloc"
	"pluto/vec"
)

func f() int {
	buf := vec.New[byte](alloc.Default())
	defer buf.Free()
	for i := 0; i < 3; i++ {
		buf.Push(byte(i))
	}
	return buf.Len()
}

func main() { println(f()) }
`, "main.p:", "",
	},
	{
		// A type parameter converted to an interface, with only direct
		// instances: there is no box (tp_ifaceconv and its kin).
		"typeparam_direct_box", `package main

type S struct{ n int }

func f[T any](x T) any { return x }

func main() {
	s := &S{1}
	_ = f(3)
	_ = f(s)
}
`, "main.p:", "",
	},
	{
		"strconv_unquote", `package main

import "pluto/strconv"

func main() {
	s, err := strconv.Unquote(strconv.Quote("a\tb"))
	println(s, err == nil)
}
`, "quote.p:", "",
	},
	{
		// testdata/run/gentypes.p: Insert's recursive store into t.left is
		// cut at the limits to a GE destination, which in main also covers
		// the key field's string. A string constant carries no pointer
		// (eir's constReg), so its bytes are no heap destination for a
		// frame pointer (with int keys it was accepted already).
		"tree_string_keys", `package main

type Tree[K interface{ ~int | ~string }, V any] struct {
	left, right *Tree[K, V]
	key         K
	val         V
}

func (t *Tree[K, V]) Insert(k K, v V, store []Tree[K, V], n *int) *Tree[K, V] {
	if t == nil {
		node := &store[*n]
		*n++
		node.key, node.val = k, v
		return node
	}
	switch {
	case k < t.key:
		t.left = t.left.Insert(k, v, store, n)
	case k > t.key:
		t.right = t.right.Insert(k, v, store, n)
	default:
		t.val = v
	}
	return t
}

func main() {
	var storeArr [8]Tree[string, int]
	store := storeArr[:]
	var root *Tree[string, int]
	n := 0
	for i, k := range []string{"m", "c", "x"} {
		root = root.Insert(k, i, store, &n)
	}
	println(n, root != nil)
}
`, "main.p:", "",
	},
	{
		// A type parameter whose instances are all zero-sized is boxed
		// into no memory: returning it as an interface keeps nothing
		// (testdata/run/tp_issue50264.p).
		"typeparam_zero_size_box", `package main

type hello struct{}
type none struct{}

func some[T any](v T) any { return v }

func main() {
	_ = some(hello{})
	_ = some(none{})
}
`, "main.p:", "",
	},
	{
		// A recursive As stores what it finds into *target: a store through
		// a value whose pointers sit somewhere in it lands anywhere in the
		// target's place, not past another load (domain.Key.FieldsGE), so
		// the frame target's contents are no heap destination
		// (testdata/run/errpatterns.p, testdata/lib/errors.p).
		"as_target_fields", `package main

type wrapped struct {
	op  string
	err error
}

func (w *wrapped) Error() string { return w.op }
func (w *wrapped) Unwrap() error { return w.err }

type multi struct{ errs []error }

func (m *multi) Error() string { return "multi" }

type plain struct{}

func (plain) Error() string { return "plain" }

var errPlain error = plain{}

func as[E any](err error, target *E) bool {
	if err != nil {
		if e, ok := any(err).(E); ok {
			*target = e
			return true
		}
		if x, ok := err.(interface{ Unwrap() error }); ok {
			return as(x.Unwrap(), target)
		}
	}
	return false
}

func main() {
	top := &wrapped{op: "a", err: &wrapped{op: "b", err: &multi{errs: []error{errPlain}}}}
	var m *multi
	println(as(top, &m), len(m.errs))
}
`, "main.p:", "",
	},
	{
		// An assertion to an interface fails on a box whose dynamic type
		// does not implement it: the string's frame box is not kept.
		"assert_iface_box", `package main

var keep error

func f(s string) {
	var a any = s
	if e, ok := a.(error); ok {
		keep = e
	}
}

func main() { f("x") }
`, "main.p:", "",
	},
}

func TestLibraryControls(t *testing.T) {
	if testing.Short() {
		t.Skip("loads the library")
	}
	for _, c := range libraryControls {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			f := filepath.Join(dir, "main.p")
			if err := os.WriteFile(f, []byte(c.src), 0o644); err != nil {
				t.Fatal(err)
			}
			err := escape2.Check(loadFile(t, f))
			rejected := err != nil && strings.Contains(err.Error(), c.line)
			switch {
			case rejected && c.gap == "":
				t.Errorf("rejected at %s:\n%v", c.line, err)
			case !rejected && c.gap != "":
				t.Errorf("accepted now: remove the gap (%s)", c.gap)
			}
		})
	}
}
