package types2_test

import (
	"strings"
	"testing"

	"github.com/matjam/plutonium/internal/diag"
	"github.com/matjam/plutonium/internal/types2"
)

func TestOwnerDeclarations(t *testing.T) {
	pkg, _ := mustCheck(t, `package p

import "unsafe"

type File owner struct{ fd int32 }

func (f *File) Drop() {}

type Frame owner uintptr
type Pages owner [4]Frame
type Raw owner unsafe.Pointer
type Box owner *int
type Resource owner interface{ Read(p []byte) (int, error) }
type Conn owner struct {
	file File
	name string
}
type Moves owner struct{ n int } // owner without Drop: it still moves

type Vec[T owner any] owner struct{ data []T }

type Plain struct {
	n int
	p *File // a view of a File: not an owner
}

func Fill[T any](s []T, v T) {
	for i := range s {
		s[i] = v
	}
}

func use() {
	var v Vec[File]
	_ = v
	Fill([]int{1}, 2)
}

// "owner" is a keyword only before a type; elsewhere it is a name.
type owner int
type Count owner
type Pkg owner

func id[T owner, U owner any](x T, u U) (T, U) {
	var owner T = x
	return owner, u
}
`)
	for name, want := range map[string]bool{
		"File": true, "Frame": true, "Pages": true, "Raw": true, "Box": true, "Resource": true,
		"Conn": true, "Moves": true, "Vec": true, "Plain": false, "Count": false, "Pkg": false,
	} {
		obj := pkg.Scope().Lookup(name)
		if got := types2.IsOwner(obj.Type()); got != want {
			t.Errorf("IsOwner(%s) = %v, want %v", name, got, want)
		}
	}
	// An unnamed array of owners is an owner by its element.
	file := pkg.Scope().Lookup("File").Type()
	if !types2.IsOwner(types2.NewArray(file, 2)) {
		t.Error("[2]File is not an owner")
	}
	if types2.IsOwner(types2.NewPointer(file)) {
		t.Error("*File is an owner")
	}
}

// mustFailExactly checks that src fails to type-check with exactly want as
// its first error, printed as pluto prints it: the message (after the
// position), then its explanation.
func mustFailExactly(t *testing.T, src, want string) {
	t.Helper()
	_, _, err := checkSrc(t, src)
	if err == nil {
		t.Fatalf("expected an error, got none")
	}
	d := diag.FromChecker(err, nil)
	got := d.Msg + "\n" + diag.Explanation(d)
	if strings.TrimSpace(got) != strings.TrimSpace(want) {
		t.Fatalf("error:\n%s\nwant:\n%s", got, want)
	}
}

func TestOwnerErrors(t *testing.T) {
	mustFailExactly(t, `package p

type File struct{ fd int32 }

func (f *File) Drop() {}
`, `File has a Drop method but is not declared owner
    Drop runs automatically when a value is destroyed. That only makes sense for
    a type whose values have exactly one owner: if File values were copied as
    ordinary values are, every copy would run Drop and release the same
    resource.
    Go copies such values freely and its garbage collector frees what they refer
    to when nothing refers to it any more. Plutonium has no garbage collector:
    an owner frees what it owns when it is dropped, so there must be exactly
    one.
    Declare the type as an owner, so that its values move instead of being
    copied:
        type File owner struct {
            ...
        }
    (see "Owner types" in the Plutonium spec, section 7.18)
    (run "pluto explain E3201" for a longer explanation)`)

	mustFailExactly(t, `package p

type Buf owner struct{ b []byte }

type Conn struct {
	buf Buf
}
`, `Conn must be declared owner: its field buf is a Buf, which is an owner
    An owner owns memory or another resource and releases it when it is dropped,
    so it moves instead of being copied. A struct that contains one owns that
    memory too, and copying the struct would copy the owner, leaving two values
    that both free it.
    Go copies such values freely and its garbage collector frees what they refer
    to when nothing refers to it any more. Plutonium has no garbage collector:
    an owner frees what it owns when it is dropped, so there must be exactly
    one.
    Plutonium does not make Conn an owner silently; say so in its declaration:
        type Conn owner struct {
            ...
        }
    Conn values then move, and buf is dropped when a Conn is. If Conn should
    only look at a value that something else owns, store a pointer:
        buf *Buf
    (see "Owner types" in the Plutonium spec, section 7.18)
    (run "pluto explain E3202" for a longer explanation)`)

	mustFailExactly(t, `package p

type Kind owner enum {
	A
	B
}
`, `owner cannot qualify an enum type
    owner marks a type whose values own something and move instead of being
    copied. An enum is a set of integer constants, and its values are always
    copied.
    To attach a resource to a Kind, wrap both in an owner:
        type Handle owner struct {
            v  Kind
            fd int32
        }
    (see "Owner types" in the Plutonium spec, section 7.18)
    (run "pluto explain E3204" for a longer explanation)`)

	mustFail(t, `package p

type Bytes owner []byte
`, "owner cannot qualify a slice type")

	mustFail(t, `package p

type Buf owner struct{ b []byte }

type B Buf
`, "B must be declared owner: B is defined from Buf, which is an owner")

	mustFail(t, `package p

type Buf owner struct{ b []byte }

type Pair struct{ bufs [2]Buf }
`, "Pair must be declared owner: its field bufs is a [2]Buf, an array of owners")

	mustFail(t, `package p

type Buf owner struct{ b []byte }

var x struct{ b Buf }
`, "a struct type without a name cannot have owner fields: its field b is a Buf, which is an owner")

	mustFail(t, `package p

type R owner interface{ Read() int }

type RW interface {
	R
	Write() int
}
`, "RW must be declared owner: it embeds R, an owning interface")

	mustFail(t, `package p

type F owner struct{}

func (F) Drop(n int) {}
`, "Drop must have no parameters and no results")

	mustFail(t, `package p

type Buf owner struct{ b []byte }

type A = owner Buf
`, "owner can only be used in a type definition")

	mustFail(t, `package p

type Buf owner struct{ b []byte }

func Fill[T any](s []T, v T) {}

func use() { Fill([]Buf{}, Buf{}) }
`, "Buf is an owner and T does not allow owners")
}
