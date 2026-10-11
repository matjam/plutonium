package types2_test

import (
	"testing"

	"github.com/matjam/plutonium/internal/testguard"
	"github.com/matjam/plutonium/internal/types2"
)

const portableDecls = `package p

import "unsafe"

type Color enum uint8 {
	Red
	Green
}

type Flags packed uint16 {
	mode bits[uint, 4]
	on   bits[bool, 1]
}

type Header struct {
	kind Color
	len  uint32
	f    Flags
}

type Shape union {
	Empty
	Dot struct{ x, y int32 }
	Box struct{ w, h float64 }
}

type Frame struct {
	hdr   Header
	data  [64]byte
	ids   ring[uint32, 8]
	reg   volatile[uint32]
	page  aligned[[16]byte, 16]
	shape Shape
	addr  uintptr
	ok    bool
	r     rune
}

type Named uint16

func Send[T portable](v T) {}

func SendAll[T portable](vs [4]T) {}

func Same[T interface{ comparable; portable }](a, b T) bool { Send(a); return a == b }

type Stringer interface{ String() string }

func Show[T interface{ portable; Stringer }](v T) string { Send(v); return v.String() }

type Packet[T portable] struct {
	hdr  Header
	body T
}

type Msg struct {
	n int
	s string
}

func (m Msg) String() string { return m.s }

type File owner struct{ fd int32 }

type Handle owner uintptr

type Reader interface{ Read() int }

var sink unsafe.Pointer

`

func TestPortableAccepts(t *testing.T) {
	testguard.Parallel(t)
	mustCheck(t, portableDecls+`
func Forward[T portable](v T) { Send(v) }

func ForwardWide[T interface{ portable; ~int | ~uint8 }](v T) { Send(v) }

func ok() {
	_ = Same(1, 2)
	_ = Same(Header{}, Header{})
	_ = Show(Color.Red)
	Send(true)
	Send(7)
	Send(uint8(1))
	Send(1.5)
	Send(float32(2))
	Send(uintptr(0x1000))
	Send(Color.Red)
	Send(Flags{})
	Send(Header{})
	Send(Shape.Dot{x: 1})
	Send(Frame{})
	Send(Named(3))
	Send([3]Header{})
	Send[[2][2]int]([2][2]int{})
	Send(struct{ a, b int }{})
	SendAll([4]uint16{})
	var p Packet[Header]
	_ = p
	var q Packet[[8]byte]
	_ = q
	Forward(Frame{})
	ForwardWide(5)
	ForwardWide(uint8(5))
}
`)
}

func TestPortableErrors(t *testing.T) {
	testguard.Parallel(t)
	cases := []struct{ name, body, want string }{
		{"string", "Send(\"text\")", "string is not portable: a string's bytes live in the sender's address space"},
		{"string field", "Send(Msg{})", "Msg has field s of type string, which is not portable: a string's bytes live in the sender's address space"},
		{"pointer", "var x int; Send(&x)", "*int is not portable: a pointer holds an address"},
		{"pointer field", "type R struct{ a int; p *int }\n\tSend(R{})", "R has field p of type *int, which is not portable: a pointer holds an address"},
		{"slice", "Send([]byte(nil))", "[]byte is not portable: a slice refers to memory"},
		{"func", "Send(func() {})", "func() is not portable: a function value refers to code"},
		{"map", "Send(map[int]int(nil))", "map[int]int is not portable: a map is a reference"},
		{"chan", "Send(make(chan int))", "chan int is not portable: a channel is a reference"},
		{"interface", "Send(any(1))", "any is not portable: an interface value refers to a type"},
		{"named interface", "var r Reader; Send(r)", "Reader is not portable: an interface value refers to a type"},
		{"unsafe pointer", "Send(sink)", "unsafe.Pointer is not portable: an unsafe.Pointer holds an address"},
		{"owner struct", "Send(File{})", "File is not portable: an owner must be dropped exactly once"},
		{"owner of a number", "Send(Handle(1))", "Handle is not portable: an owner must be dropped exactly once"},
		{"owned", "var b owned[int]\n\tSend(b)", "owned[int] is not portable: an owned box"},
		{"shared", "type S struct{ b shared[int] }\n\tSend(S{})", "S has field b of type shared[int], which is not portable: a shared box is a counted reference"},
		{"synced", "var b synced[int]\n\tSend(b)", "synced[int] is not portable: a synced box is a counted reference"},
		{"nested", "type In struct{ n int; m Msg }\n\ttype Out struct{ a [2]In }\n\tSend(Out{})", "Out has field a[].m.s of type string, which is not portable"},
		{"array element", "Send([4][]int{})", "[4][]int has element [] of type []int, which is not portable"},
		{"union variant", "type U union {\n\t\tA int\n\t\tB string\n\t}\n\tSend(U.A(1))", "U has variant B of type string, which is not portable"},
		{"ring element", "Send(ring[*int, 4]{})", "ring[*int, 4] has element [] of type *int, which is not portable"},
		{"explicit instance", "Send[string](\"x\")", "string is not portable"},
		{"generic type", "var p Packet[string]\n\t_ = p", "string is not portable"},
		{"generic field", "Send(Packet[Header]{})\n\tSend(struct{ p Packet[Header]; m Msg }{})", "has field m.s of type string"},
		{"comparable and portable", "_ = Same(Msg{}, Msg{})", "Msg has field s of type string"},
		{"portable with methods", "_ = Show(Msg{})", "Msg has field s of type string"},
		{"nested generic", "type G[T any] struct{ v T }\n\tSend(G[string]{})", "G[string] has field v of type string"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			testguard.Parallel(t)
			mustFail(t, portableDecls+"\nfunc bad() {\n\t"+c.body+"\n}\n", c.want)
		})
	}
}

func TestPortableTypeParams(t *testing.T) {
	testguard.Parallel(t)
	cases := []struct{ name, src, want string }{
		{"unconstrained", "func F[T any](v T) { Send(v) }", "T is not portable: the type parameter's constraint does not require a portable type"},
		{"comparable only", "func F[T comparable](v T) { Send(v) }", "T is not portable"},
		{"integer constraint", "func F[T ~int | ~string](v T) { Send(v) }", "T is not portable"},
		{"owner constraint", "func F[T owner portable](v T) { Send(v) }", "T is not portable: an owner must be dropped"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			testguard.Parallel(t)
			mustFail(t, portableDecls+"\n"+c.src+"\n", c.want)
		})
	}
}

func TestPortableOutsideConstraints(t *testing.T) {
	testguard.Parallel(t)
	cases := []struct{ name, src, want string }{
		{"variable", "package p\nvar x portable", "cannot use type portable outside a type constraint: interface is (or embeds) portable"},
		{"embedded", "package p\ntype C interface{ portable }\nvar x C", "outside a type constraint: interface is (or embeds) portable"},
		{"union term", "package p\ntype C interface{ int | portable }", "cannot use portable in union"},
		{"union of embedder", "package p\ntype P interface{ portable }\ntype C interface{ int | P }", "embeds portable"},
		{"conversion", "package p\nfunc f(x int) { _ = portable(x) }", "cannot use interface portable in conversion"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			testguard.Parallel(t)
			mustFail(t, c.src, c.want)
		})
	}
}

func TestPortableAPI(t *testing.T) {
	testguard.Parallel(t)
	pkg, _ := mustCheck(t, portableDecls)
	for name, want := range map[string]bool{
		"Header": true, "Frame": true, "Shape": true, "Named": true, "Color": true,
		"Msg": false, "File": false, "Handle": false, "Reader": false,
	} {
		T := pkg.Scope().Lookup(name).Type()
		if got := types2.Portable(T); got != want {
			t.Errorf("Portable(%s) = %v, want %v", name, got, want)
		}
	}
	msg := pkg.Scope().Lookup("Msg").Type()
	c := types2.PortableCauseOf(msg)
	if c == nil {
		t.Fatal("no cause for Msg")
	}
	if path, noun := c.Where(); path != "s" || noun != "field" || c.Component().String() != "string" {
		t.Errorf("cause = %q %q %s", path, noun, c.Component())
	}
	if types2.PortableCauseOf(pkg.Scope().Lookup("Header").Type()) != nil {
		t.Error("cause for Header")
	}
	if types2.Portable(types2.NewPointer(types2.Typ[types2.Int])) || types2.Portable(types2.Typ[types2.String]) {
		t.Error("pointer or string is portable")
	}
	if c := types2.PortableCauseOf(types2.NewMap(types2.Typ[types2.Int], types2.Typ[types2.Int])); c == nil || c.Reason() == "" {
		t.Error("map[int]int is portable")
	}
	if !types2.Portable(types2.NewArray(types2.Typ[types2.Uint8], 4)) {
		t.Error("[4]uint8 is not portable")
	}
}
